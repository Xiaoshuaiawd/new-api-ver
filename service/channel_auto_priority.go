package service

import (
	"context"
	"math"
	"sort"
	"strings"
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
	for i := range candidates {
		candidates[i].priority = priorities[candidates[i].bucket]
		candidates[i].weight = channelAutoPriorityWeight(
			GetChannelHealthSnapshotForDisplay(candidates[i].channel.Id),
			setting,
			healthSetting.MinSamples,
		)
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
	return setting
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
