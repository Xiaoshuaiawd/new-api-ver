package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

const channelAutoPriorityMultiplierPrecision = 1_000_000

type ChannelAutoPriorityApplySummary struct {
	UpdatedChannels int                            `json:"updated_channels"`
	SkippedChannels int                            `json:"skipped_channels"`
	Items           []ChannelAutoPriorityApplyItem `json:"items"`
}

type ChannelAutoPriorityApplyItem struct {
	ChannelID  int     `json:"channel_id"`
	Name       string  `json:"name"`
	Multiplier float64 `json:"multiplier"`
	Priority   int64   `json:"priority"`
	Weight     uint    `json:"weight"`
	Reason     string  `json:"reason,omitempty"`
}

type channelAutoPriorityCandidate struct {
	channel  *model.Channel
	snapshot ChannelMultiplierSnapshot
	bucket   int64
	priority int64
	weight   uint
	reason   string
}

type channelAutoPriorityLatencyStats struct {
	Samples     int
	SlowSamples int
	SlowRatio   float64
}

var channelAutoPriorityLatencyGuard = struct {
	sync.Mutex
	degraded map[int]bool
}{
	degraded: map[int]bool{},
}

func ApplyChannelAutoPriority(ctx context.Context) (ChannelAutoPriorityApplySummary, error) {
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		return ChannelAutoPriorityApplySummary{}, err
	}

	candidates := make([]channelAutoPriorityCandidate, 0, len(channels))
	summary := ChannelAutoPriorityApplySummary{}
	now := time.Now().Unix()
	for _, channel := range channels {
		if channel == nil || channel.Status != common.ChannelStatusEnabled || !hasChannelMultiplierMonitorConfig(channel) {
			continue
		}
		snapshot, ok := GetChannelMultiplierSnapshot(channel.Id)
		if !ok || !isChannelAutoPriorityMultiplierSnapshotValid(snapshot, now) {
			summary.SkippedChannels++
			summary.Items = append(summary.Items, ChannelAutoPriorityApplyItem{
				ChannelID: channel.Id,
				Name:      channel.Name,
				Reason:    "missing valid multiplier snapshot",
			})
			continue
		}
		candidates = append(candidates, channelAutoPriorityCandidate{
			channel:  channel,
			snapshot: snapshot,
			bucket:   normalizeChannelAutoPriorityMultiplier(snapshot.Multiplier),
		})
	}
	if len(candidates) == 0 {
		return summary, nil
	}

	priorities := channelAutoPriorityPriorities(candidates)
	setting := normalizeChannelAutoPrioritySetting(*operation_setting.GetChannelAutoPrioritySetting())
	healthSetting := *operation_setting.GetChannelHealthSetting()
	lowestPriority := channelAutoPriorityLowestPriority(priorities)
	latencyStats, err := loadChannelAutoPriorityLatencyStats(ctx, channelAutoPriorityCandidateIDs(candidates), setting, time.Now())
	if err != nil {
		return ChannelAutoPriorityApplySummary{}, err
	}
	for i := range candidates {
		candidates[i].priority = priorities[candidates[i].bucket]
		candidates[i].weight = channelAutoPriorityWeight(
			GetChannelHealthSnapshotForDisplay(candidates[i].channel.Id),
			setting,
			healthSetting.MinSamples,
		)
		applyChannelAutoPriorityLatencyGuard(&candidates[i], latencyStats[candidates[i].channel.Id], setting, lowestPriority)
	}

	err = model.DB.Transaction(func(tx *gorm.DB) error {
		for _, candidate := range candidates {
			if err := tx.Model(&model.Channel{}).
				Where("id = ?", candidate.channel.Id).
				Updates(map[string]any{
					"priority": candidate.priority,
					"weight":   candidate.weight,
				}).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.Ability{}).
				Where("channel_id = ?", candidate.channel.Id).
				Updates(map[string]any{
					"priority": candidate.priority,
					"weight":   candidate.weight,
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ChannelAutoPriorityApplySummary{}, err
	}
	model.InitChannelCache()

	summary.UpdatedChannels = len(candidates)
	for _, candidate := range candidates {
		summary.Items = append(summary.Items, ChannelAutoPriorityApplyItem{
			ChannelID:  candidate.channel.Id,
			Name:       candidate.channel.Name,
			Multiplier: candidate.snapshot.Multiplier,
			Priority:   candidate.priority,
			Weight:     candidate.weight,
			Reason:     candidate.reason,
		})
	}
	return summary, nil
}

func ApplyChannelAutoPriorityIfEnabled(ctx context.Context) (ChannelAutoPriorityApplySummary, bool, error) {
	if !operation_setting.GetChannelAutoPrioritySetting().Enabled {
		return ChannelAutoPriorityApplySummary{}, false, nil
	}
	summary, err := ApplyChannelAutoPriority(ctx)
	return summary, true, err
}

func hasChannelMultiplierMonitorConfig(channel *model.Channel) bool {
	cfg := GetChannelMultiplierMonitorConfig(channel)
	return cfg.Enabled &&
		cfg.Format != "" &&
		strings.TrimSpace(channelMultiplierBaseURL(channel, cfg)) != "" &&
		strings.TrimSpace(cfg.Username) != "" &&
		strings.TrimSpace(cfg.Password) != ""
}

func isChannelAutoPriorityMultiplierSnapshotValid(snapshot ChannelMultiplierSnapshot, now int64) bool {
	return snapshot.State == ChannelMultiplierSnapshotHealthy &&
		normalizeMultiplier(snapshot.Multiplier) > 0 &&
		(snapshot.ExpiresAt == 0 || snapshot.ExpiresAt >= now)
}

func normalizeChannelAutoPriorityMultiplier(multiplier float64) int64 {
	return int64(math.Round(multiplier * channelAutoPriorityMultiplierPrecision))
}

func channelAutoPriorityPriorities(candidates []channelAutoPriorityCandidate) map[int64]int64 {
	buckets := make(map[int64]struct{})
	for _, candidate := range candidates {
		buckets[candidate.bucket] = struct{}{}
	}
	sortedBuckets := make([]int64, 0, len(buckets))
	for bucket := range buckets {
		sortedBuckets = append(sortedBuckets, bucket)
	}
	sort.Slice(sortedBuckets, func(i, j int) bool {
		return sortedBuckets[i] < sortedBuckets[j]
	})

	priorities := make(map[int64]int64, len(sortedBuckets))
	for index, bucket := range sortedBuckets {
		priorities[bucket] = int64(len(sortedBuckets) - index)
	}
	return priorities
}

func channelAutoPriorityLowestPriority(priorities map[int64]int64) int64 {
	lowest := int64(0)
	for _, priority := range priorities {
		if lowest == 0 || priority < lowest {
			lowest = priority
		}
	}
	return lowest
}

func channelAutoPriorityCandidateIDs(candidates []channelAutoPriorityCandidate) []int {
	ids := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.channel != nil {
			ids = append(ids, candidate.channel.Id)
		}
	}
	return ids
}

func normalizeChannelAutoPrioritySetting(setting operation_setting.ChannelAutoPrioritySetting) operation_setting.ChannelAutoPrioritySetting {
	if setting.MinWeight <= 0 {
		setting.MinWeight = operation_setting.ChannelAutoPriorityDefaultMinWeight
	}
	if setting.MaxWeight <= 0 {
		setting.MaxWeight = operation_setting.ChannelAutoPriorityDefaultMaxWeight
	}
	if setting.MaxWeight < setting.MinWeight {
		setting.MinWeight, setting.MaxWeight = setting.MaxWeight, setting.MinWeight
	}
	if setting.LatencyThresholdSeconds <= 0 {
		setting.LatencyThresholdSeconds = operation_setting.ChannelAutoPriorityDefaultLatencyThresholdSeconds
	}
	if setting.LatencyWindowMinutes <= 0 {
		setting.LatencyWindowMinutes = operation_setting.ChannelAutoPriorityDefaultLatencyWindowMinutes
	}
	if setting.LatencyMinSamples <= 0 {
		setting.LatencyMinSamples = operation_setting.ChannelAutoPriorityDefaultLatencyMinSamples
	}
	if setting.LatencySlowRatioThreshold <= 0 || setting.LatencySlowRatioThreshold > 1 {
		setting.LatencySlowRatioThreshold = operation_setting.ChannelAutoPriorityDefaultLatencySlowRatioThreshold
	}
	if setting.LatencyRecoveryRatioThreshold < 0 || setting.LatencyRecoveryRatioThreshold > 1 {
		setting.LatencyRecoveryRatioThreshold = operation_setting.ChannelAutoPriorityDefaultLatencyRecoveryRatioThreshold
	}
	if setting.LatencyRecoveryRatioThreshold > setting.LatencySlowRatioThreshold {
		setting.LatencyRecoveryRatioThreshold = setting.LatencySlowRatioThreshold
	}
	if setting.LatencyRetainedWeightPercent <= 0 || setting.LatencyRetainedWeightPercent > 100 {
		setting.LatencyRetainedWeightPercent = operation_setting.ChannelAutoPriorityDefaultLatencyRetainedWeightPercent
	}
	if setting.LatencyPriorityPenalty < 0 {
		setting.LatencyPriorityPenalty = operation_setting.ChannelAutoPriorityDefaultLatencyPriorityPenalty
	}
	return setting
}

func loadChannelAutoPriorityLatencyStats(ctx context.Context, channelIDs []int, setting operation_setting.ChannelAutoPrioritySetting, now time.Time) (map[int]channelAutoPriorityLatencyStats, error) {
	stats := make(map[int]channelAutoPriorityLatencyStats)
	if !setting.LatencyGuardEnabled || len(channelIDs) == 0 || model.LOG_DB == nil {
		return stats, nil
	}
	setting = normalizeChannelAutoPrioritySetting(setting)
	cutoff := now.Add(-time.Duration(setting.LatencyWindowMinutes) * time.Minute).Unix()
	thresholdMs := float64(setting.LatencyThresholdSeconds) * 1000
	var logs []struct {
		ChannelID int    `gorm:"column:channel_id"`
		Other     string `gorm:"column:other"`
	}
	if err := model.LOG_DB.WithContext(ctx).
		Model(&model.Log{}).
		Select("channel_id, other").
		Where("type = ? AND channel_id IN ? AND created_at >= ?", model.LogTypeConsume, channelIDs, cutoff).
		Find(&logs).Error; err != nil {
		return nil, err
	}

	for _, log := range logs {
		var other map[string]any
		if err := common.UnmarshalJsonStr(log.Other, &other); err != nil {
			continue
		}
		frt := channelAutoPriorityFirstResponseMs(other["frt"])
		if frt <= 0 {
			continue
		}
		stat := stats[log.ChannelID]
		stat.Samples++
		if frt > thresholdMs {
			stat.SlowSamples++
		}
		stats[log.ChannelID] = stat
	}
	for channelID, stat := range stats {
		if stat.Samples > 0 {
			stat.SlowRatio = float64(stat.SlowSamples) / float64(stat.Samples)
			stats[channelID] = stat
		}
	}
	return stats, nil
}

func channelAutoPriorityFirstResponseMs(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func applyChannelAutoPriorityLatencyGuard(candidate *channelAutoPriorityCandidate, stats channelAutoPriorityLatencyStats, setting operation_setting.ChannelAutoPrioritySetting, lowestPriority int64) {
	if candidate == nil || candidate.channel == nil || !setting.LatencyGuardEnabled {
		return
	}
	if stats.Samples < setting.LatencyMinSamples {
		setChannelAutoPriorityLatencyDegraded(candidate.channel.Id, false)
		return
	}

	wasDegraded := isChannelAutoPriorityLatencyDegraded(candidate.channel.Id)
	degraded := stats.SlowRatio >= setting.LatencySlowRatioThreshold ||
		(wasDegraded && stats.SlowRatio > setting.LatencyRecoveryRatioThreshold)
	setChannelAutoPriorityLatencyDegraded(candidate.channel.Id, degraded)
	if !degraded {
		return
	}

	candidate.priority -= int64(setting.LatencyPriorityPenalty)
	if candidate.priority < lowestPriority {
		candidate.priority = lowestPriority
	}
	retainedWeight := int(math.Round(float64(candidate.weight) * float64(setting.LatencyRetainedWeightPercent) / 100.0))
	if retainedWeight < setting.MinWeight {
		retainedWeight = setting.MinWeight
	}
	candidate.weight = uint(retainedWeight)
	candidate.reason = fmt.Sprintf("slow first response ratio %.0f%% (%d/%d)", stats.SlowRatio*100, stats.SlowSamples, stats.Samples)
}

func isChannelAutoPriorityLatencyDegraded(channelID int) bool {
	channelAutoPriorityLatencyGuard.Lock()
	defer channelAutoPriorityLatencyGuard.Unlock()
	return channelAutoPriorityLatencyGuard.degraded[channelID]
}

func setChannelAutoPriorityLatencyDegraded(channelID int, degraded bool) {
	channelAutoPriorityLatencyGuard.Lock()
	defer channelAutoPriorityLatencyGuard.Unlock()
	if degraded {
		channelAutoPriorityLatencyGuard.degraded[channelID] = true
		return
	}
	delete(channelAutoPriorityLatencyGuard.degraded, channelID)
}

func resetChannelAutoPriorityLatencyGuardForTest() {
	channelAutoPriorityLatencyGuard.Lock()
	defer channelAutoPriorityLatencyGuard.Unlock()
	channelAutoPriorityLatencyGuard.degraded = map[int]bool{}
}

func channelAutoPriorityWeight(snapshot ChannelHealthSnapshot, setting operation_setting.ChannelAutoPrioritySetting, minSamples int) uint {
	score := channelAutoPriorityStabilityScore(snapshot, minSamples)
	span := setting.MaxWeight - setting.MinWeight
	weight := setting.MinWeight + int(math.Round(float64(span)*score))
	if weight < setting.MinWeight {
		weight = setting.MinWeight
	}
	if weight > setting.MaxWeight {
		weight = setting.MaxWeight
	}
	return uint(weight)
}

func channelAutoPriorityStabilityScore(snapshot ChannelHealthSnapshot, minSamples int) float64 {
	if minSamples <= 0 {
		minSamples = 10
	}
	if snapshot.State == ChannelHealthStateHealthy && snapshot.WindowSamples < minSamples {
		return 0.5
	}

	stateScore := 0.5
	switch snapshot.State {
	case ChannelHealthStateHealthy:
		stateScore = 0.7
	case ChannelHealthStateWarming:
		stateScore = 0.45
	case ChannelHealthStateProbing:
		stateScore = 0.3
	case ChannelHealthStateOpen:
		stateScore = 0.1
	}

	errorScore := 0.1
	if snapshot.WindowSamples > 0 {
		errorScore = (1 - clampFloat(snapshot.ErrorRate, 0, 1)) * 0.2
	}

	latencyScore := 0.05
	if snapshot.AverageFirstResponseMs > 0 {
		latencyScore = clampFloat(1-(snapshot.AverageFirstResponseMs/5000), 0, 1) * 0.1
	}
	return clampFloat(stateScore+errorScore+latencyScore, 0, 1)
}

func clampFloat(value float64, min float64, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
