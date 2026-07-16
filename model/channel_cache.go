package model

import (
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

var group2model2channels map[string]map[string][]int // enabled channel
var channelsIDM map[int]*Channel                     // all channels include disabled
// channel2advancedCustomConfig caches parsed Advanced Custom (type 58) configs so
// path-aware selection avoids re-parsing JSON per request. Refreshed on full sync.
var channel2advancedCustomConfig map[int]*dto.AdvancedCustomConfig
var channelSyncLock sync.RWMutex

type ChannelSelectionOptions struct {
	RequireImageInputSupport bool
}

type runtimeSelectionCandidate struct {
	ChannelID       int
	Priority        int64
	ModelName       string
	Weight          int
	EffectiveWeight int
	Inflight        int
	Probe           bool
	Channel         *Channel
}

type runtimeSelectionSnapshot struct {
	normalAvailable bool
	normalInflight  int
	probeAvailable  bool
	probeInflight   int
	healthState     string
	weights         map[int]int
}

const runtimeSmoothSelectionShardCount = 32
const runtimeSmoothSelectionShardCapacity = 512

type runtimeSmoothSelectionState struct {
	current  int
	lastUsed time.Time
}

type runtimeSmoothSelectionShard struct {
	sync.Mutex
	current map[string]runtimeSmoothSelectionState
}

var runtimeSmoothSelection [runtimeSmoothSelectionShardCount]runtimeSmoothSelectionShard

func ResetChannelRuntimeSelectionStateForTest() {
	for i := range runtimeSmoothSelection {
		shard := &runtimeSmoothSelection[i]
		shard.Lock()
		shard.current = make(map[string]runtimeSmoothSelectionState)
		shard.Unlock()
	}
}

func InitChannelCache() {
	if !common.MemoryCacheEnabled {
		InvalidatePricingCache()
		return
	}
	newChannelId2channel := make(map[int]*Channel)
	newChannel2advancedCustomConfig := make(map[int]*dto.AdvancedCustomConfig)
	var channels []*Channel
	DB.Find(&channels)
	for _, channel := range channels {
		newChannelId2channel[channel.Id] = channel
		if channel.Type == constant.ChannelTypeAdvancedCustom {
			if config := channel.GetOtherSettings().AdvancedCustom; config != nil {
				newChannel2advancedCustomConfig[channel.Id] = config
			}
		}
	}
	var abilities []*Ability
	DB.Find(&abilities)
	groups := make(map[string]bool)
	for _, ability := range abilities {
		groups[ability.Group] = true
	}
	newGroup2model2channels := make(map[string]map[string][]int)
	for group := range groups {
		newGroup2model2channels[group] = make(map[string][]int)
	}
	for _, channel := range channels {
		if channel.Status != common.ChannelStatusEnabled {
			continue // skip disabled channels
		}
		groups := strings.Split(channel.Group, ",")
		for _, group := range groups {
			models := strings.Split(channel.Models, ",")
			for _, model := range models {
				if _, ok := newGroup2model2channels[group][model]; !ok {
					newGroup2model2channels[group][model] = make([]int, 0)
				}
				newGroup2model2channels[group][model] = append(newGroup2model2channels[group][model], channel.Id)
			}
		}
	}

	// sort by priority
	for group, model2channels := range newGroup2model2channels {
		for model, channels := range model2channels {
			sort.Slice(channels, func(i, j int) bool {
				return newChannelId2channel[channels[i]].GetPriority() > newChannelId2channel[channels[j]].GetPriority()
			})
			newGroup2model2channels[group][model] = channels
		}
	}

	channelSyncLock.Lock()
	group2model2channels = newGroup2model2channels
	//channelsIDM = newChannelId2channel
	for i, channel := range newChannelId2channel {
		if channel.ChannelInfo.IsMultiKey {
			channel.Keys = channel.GetKeys()
			if channel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
				if oldChannel, ok := channelsIDM[i]; ok {
					// 存在旧的渠道，如果是多key且轮询，保留轮询索引信息
					if oldChannel.ChannelInfo.IsMultiKey && oldChannel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
						channel.ChannelInfo.MultiKeyPollingIndex = oldChannel.ChannelInfo.MultiKeyPollingIndex
					}
				}
			}
		}
	}
	channelsIDM = newChannelId2channel
	channel2advancedCustomConfig = newChannel2advancedCustomConfig
	channelSyncLock.Unlock()
	// Lock ordering: InvalidatePricingCache acquires updatePricingLock, and
	// GetPricing (holding updatePricingLock) nests channelSyncLock.RLock via
	// loadPricingAdvancedCustomConfigs. channelSyncLock MUST be released before
	// invalidating the pricing cache, otherwise the reversed order deadlocks.
	InvalidatePricingCache()
	common.SysLog("channels synced from database")
}

func SyncChannelCache(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing channels from database")
		InitChannelCache()
	}
}

func GetRandomSatisfiedChannel(group string, model string, retry int, requestPath string) (*Channel, error) {
	return GetRandomSatisfiedChannelWithTrace(group, model, retry, requestPath, nil, nil)
}

func GetRandomSatisfiedChannelWithTrace(group string, model string, retry int, requestPath string, excludedChannelIDs map[int]struct{}, traceFn ChannelSelectionTraceFunc, selectionOptions ...ChannelSelectionOptions) (*Channel, error) {
	options := ChannelSelectionOptions{}
	if len(selectionOptions) > 0 {
		options = selectionOptions[0]
	}
	// if memory cache is disabled, get channel directly from database
	if !common.MemoryCacheEnabled {
		return GetChannelWithOptions(group, model, retry, requestPath, excludedChannelIDs, options)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	// First, try to find channels with the exact model name.
	priorityChannels := filterChannelsBySelectionOptions(filterChannelsByRequestPathAndModel(group2model2channels[group][model], requestPath, model), options, traceFn, group, model)

	// If no channels found, try to find channels with the normalized model name.
	if len(priorityChannels) == 0 {
		normalizedModel := ratio_setting.FormatMatchingModelName(model)
		priorityChannels = filterChannelsBySelectionOptions(filterChannelsByRequestPathAndModel(group2model2channels[group][normalizedModel], requestPath, model), options, traceFn, group, normalizedModel)
	}

	if len(priorityChannels) == 0 {
		return nil, nil
	}
	channels := filterExcludedChannels(priorityChannels, excludedChannelIDs)

	uniquePriorities := make(map[int]bool)
	for _, channelId := range priorityChannels {
		if channel, ok := channelsIDM[channelId]; ok {
			uniquePriorities[int(channel.GetPriority())] = true
		} else {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId)
		}
	}
	var sortedUniquePriorities []int
	for priority := range uniquePriorities {
		sortedUniquePriorities = append(sortedUniquePriorities, priority)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sortedUniquePriorities)))

	if retry >= len(sortedUniquePriorities) {
		retry = len(sortedUniquePriorities) - 1
	}

	runtimeSnapshots := make(map[int]*runtimeSelectionSnapshot, len(channels))
	var retainedProbeCandidates []runtimeSelectionCandidate
	for priorityIndex := retry; priorityIndex < len(sortedUniquePriorities); priorityIndex++ {
		priority := int64(sortedUniquePriorities[priorityIndex])
		normalCandidates, err := collectRuntimeCandidates(group, model, channels, priority, false, traceFn, runtimeSnapshots)
		if err != nil {
			return nil, err
		}
		probeCandidates, err := collectRuntimeCandidates(group, model, channels, priority, true, traceFn, runtimeSnapshots)
		if err != nil {
			return nil, err
		}
		probeCandidates = filterRuntimeProbeCandidates(probeCandidates, normalCandidates)
		if len(normalCandidates) > 0 {
			candidates := make([]runtimeSelectionCandidate, 0, len(retainedProbeCandidates)+len(normalCandidates)+len(probeCandidates))
			candidates = append(candidates, retainedProbeCandidates...)
			candidates = append(candidates, normalCandidates...)
			candidates = append(candidates, probeCandidates...)
			selected, err := selectRuntimeCandidateWithProbeClaim(group, model, candidates)
			if err != nil {
				return nil, err
			}
			if selected.Channel == nil {
				return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", selected.ChannelID)
			}
			return selected.Channel, nil
		}
		retainedProbeCandidates = append(retainedProbeCandidates, probeCandidates...)
		if priorityIndex+1 < len(sortedUniquePriorities) {
			recordChannelSelectionTrace(traceFn, ChannelSelectionTraceEvent{
				Stage:    "priority",
				Action:   "fallback",
				Group:    group,
				Model:    model,
				Priority: priority,
				Reason:   "priority has no runtime available channels",
			})
		}
	}

	if len(retainedProbeCandidates) == 0 {
		return nil, errors.New(fmt.Sprintf("no available healthy channel found, group: %s, model: %s", group, model))
	}
	selected, err := selectRuntimeCandidateWithProbeClaim(group, model, retainedProbeCandidates)
	if err != nil {
		return nil, err
	}
	recordChannelSelectionTrace(traceFn, ChannelSelectionTraceEvent{
		Stage:     "probe",
		Action:    "fallback",
		Group:     group,
		Model:     model,
		ChannelID: selected.ChannelID,
		Priority:  selected.Priority,
		Reason:    "using retained due probe candidate",
		Probe:     true,
	})
	return selected.Channel, nil
}

func collectRuntimeCandidates(group string, modelName string, channels []int, targetPriority int64, probe bool, traceFn ChannelSelectionTraceFunc, snapshotArgs ...map[int]*runtimeSelectionSnapshot) ([]runtimeSelectionCandidate, error) {
	snapshots := map[int]*runtimeSelectionSnapshot(nil)
	if len(snapshotArgs) > 0 {
		snapshots = snapshotArgs[0]
	}
	if snapshots == nil {
		snapshots = make(map[int]*runtimeSelectionSnapshot, len(channels))
	}
	candidates := make([]runtimeSelectionCandidate, 0)
	for _, channelId := range channels {
		channel, ok := channelsIDM[channelId]
		if !ok {
			return candidates, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId)
		}
		if channel.GetPriority() != targetPriority {
			continue
		}
		if !channel.hasRuntimeAvailableKey(modelName) {
			recordChannelSelectionTrace(traceFn, ChannelSelectionTraceEvent{
				Stage:     "runtime",
				Action:    "skip",
				Group:     group,
				Model:     modelName,
				ChannelID: channel.Id,
				Priority:  channel.GetPriority(),
				Reason:    "no runtime available channel key",
				Probe:     probe,
			})
			continue
		}
		snapshot := snapshots[channel.Id]
		if snapshot == nil {
			normalAvailable, normalInflight := getChannelRuntimeState(channel.Id, modelName)
			probeAvailable, probeInflight := getChannelProbeRuntimeState(channel.Id, modelName)
			snapshot = &runtimeSelectionSnapshot{
				normalAvailable: normalAvailable,
				normalInflight:  normalInflight,
				probeAvailable:  probeAvailable,
				probeInflight:   probeInflight,
				healthState:     runtimeTraceHealthState(channel.Id),
				weights:         make(map[int]int),
			}
			snapshots[channel.Id] = snapshot
		}
		available, inflight := snapshot.normalAvailable, snapshot.normalInflight
		if probe {
			available, inflight = snapshot.probeAvailable, snapshot.probeInflight
		}
		if !available {
			recordChannelSelectionTrace(traceFn, ChannelSelectionTraceEvent{
				Stage:       "runtime",
				Action:      "skip",
				Group:       group,
				Model:       modelName,
				ChannelID:   channel.Id,
				Priority:    channel.GetPriority(),
				HealthState: snapshot.healthState,
				Reason:      "runtime unavailable",
				Probe:       probe,
			})
			continue
		}
		weightKey := channel.GetWeight() * 2
		if probe {
			weightKey++
		}
		effectiveWeight, ok := snapshot.weights[weightKey]
		if !ok {
			effectiveWeight = runtimeCandidateEffectiveWeight(channel.Id, modelName, channel.GetWeight(), inflight, probe)
			snapshot.weights[weightKey] = effectiveWeight
		}
		candidates = append(candidates, runtimeSelectionCandidate{
			ChannelID:       channel.Id,
			Priority:        channel.GetPriority(),
			ModelName:       modelName,
			Weight:          channel.GetWeight(),
			EffectiveWeight: effectiveWeight,
			Inflight:        inflight,
			Probe:           probe,
			Channel:         channel,
		})
	}
	return candidates, nil
}

func filterRuntimeProbeCandidates(probeCandidates []runtimeSelectionCandidate, normalCandidates []runtimeSelectionCandidate) []runtimeSelectionCandidate {
	if len(probeCandidates) == 0 || len(normalCandidates) == 0 {
		return probeCandidates
	}
	normalByChannelID := make(map[int]struct{}, len(normalCandidates))
	for _, candidate := range normalCandidates {
		normalByChannelID[candidate.ChannelID] = struct{}{}
	}
	filtered := probeCandidates[:0]
	for _, candidate := range probeCandidates {
		if _, normal := normalByChannelID[candidate.ChannelID]; normal {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func selectRuntimeCandidateWithProbeClaim(group string, modelName string, candidates []runtimeSelectionCandidate) (runtimeSelectionCandidate, error) {
	for len(candidates) > 0 {
		selectedIndex, err := smoothWeightedRuntimeCandidateIndex(group, modelName, candidates)
		if err != nil {
			return runtimeSelectionCandidate{}, err
		}
		selected := candidates[selectedIndex]
		if !selected.Probe || claimChannelProbeRuntimeState(selected.ChannelID, modelName) {
			return selected, nil
		}
		candidates = append(candidates[:selectedIndex], candidates[selectedIndex+1:]...)
	}
	return runtimeSelectionCandidate{}, errors.New("channel not found")
}

func smoothWeightedRuntimeCandidateIndex(group string, modelName string, candidates []runtimeSelectionCandidate) (int, error) {
	if len(candidates) == 0 {
		return -1, errors.New("channel not found")
	}

	totalWeight := 0
	for i := range candidates {
		if candidates[i].EffectiveWeight <= 0 {
			candidates[i].EffectiveWeight = 1
		}
		totalWeight += candidates[i].EffectiveWeight
	}
	if totalWeight <= 0 {
		return -1, errors.New("channel not found")
	}

	shard := runtimeSmoothSelectionShardFor(group, modelName)
	shard.Lock()
	defer shard.Unlock()
	if shard.current == nil {
		shard.current = make(map[string]runtimeSmoothSelectionState)
	}

	now := time.Now()
	bestIndex := -1
	bestCurrent := 0
	for i, candidate := range candidates {
		key := runtimeSmoothSelectionKey(group, modelName, candidate)
		state := shard.current[key]
		current := state.current + candidate.EffectiveWeight
		state.current = current
		state.lastUsed = now
		shard.current[key] = state
		if bestIndex < 0 ||
			current > bestCurrent ||
			(current == bestCurrent && candidate.ChannelID < candidates[bestIndex].ChannelID) {
			bestIndex = i
			bestCurrent = current
		}
	}
	if bestIndex < 0 {
		return -1, errors.New("channel not found")
	}
	key := runtimeSmoothSelectionKey(group, modelName, candidates[bestIndex])
	state := shard.current[key]
	state.current -= totalWeight
	state.lastUsed = now
	shard.current[key] = state
	if len(shard.current) > runtimeSmoothSelectionShardCapacity {
		oldestKey := ""
		var oldest time.Time
		for currentKey, currentState := range shard.current {
			if oldestKey == "" || currentState.lastUsed.Before(oldest) {
				oldestKey = currentKey
				oldest = currentState.lastUsed
			}
		}
		if oldestKey != "" {
			delete(shard.current, oldestKey)
		}
	}
	return bestIndex, nil
}

func runtimeSmoothSelectionShardFor(group string, modelName string) *runtimeSmoothSelectionShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(group))
	_, _ = h.Write([]byte{'\x00'})
	_, _ = h.Write([]byte(modelName))
	return &runtimeSmoothSelection[int(h.Sum32())&(runtimeSmoothSelectionShardCount-1)]
}

func runtimeSmoothSelectionKey(group string, modelName string, candidate runtimeSelectionCandidate) string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%t", group, modelName, candidate.Priority, candidate.ChannelID, candidate.Probe)
}

func runtimeCandidateEffectiveWeight(channelID int, modelName string, weight int, inflight int, probe bool) int {
	adjusted := adjustRuntimeChannelWeight(channelID, modelName, weight, inflight)
	if probe {
		adjusted = retainedProbeTrafficWeight(adjusted)
	}
	if adjusted <= 0 {
		return 1
	}
	return adjusted
}

func retainedProbeTrafficWeight(weight int) int {
	if weight <= 0 {
		return 1
	}
	retained := weight / 10
	if retained <= 0 {
		return 1
	}
	return retained
}

func adjustedChannelWeight(weight int, inflight int) int {
	if inflight <= 0 {
		return weight
	}
	adjusted := weight / (1 + inflight)
	if weight > 0 && adjusted <= 0 {
		return 1
	}
	return adjusted
}

// filterChannelsByRequestPathAndModel restricts candidates by request path and
// model. Only Advanced Custom (type 58) channels are path-checked: they are kept
// only when one of their configured routes matches requestPath and model. All
// other channel types always pass. When requestPath is empty, filtering is skipped.
// Caller must hold channelSyncLock (read lock). The cached slice is never mutated.
func filterChannelsByRequestPathAndModel(channels []int, requestPath string, model string) []int {
	if requestPath == "" || len(channels) == 0 {
		return channels
	}
	filtered := make([]int, 0, len(channels))
	for _, channelId := range channels {
		channel, ok := channelsIDM[channelId]
		if !ok {
			// keep it so the downstream consistency error is raised as before
			filtered = append(filtered, channelId)
			continue
		}
		if channel.Type != constant.ChannelTypeAdvancedCustom {
			filtered = append(filtered, channelId)
			continue
		}
		if config := channel2advancedCustomConfig[channelId]; config != nil && config.SupportsPathForModel(requestPath, model) {
			filtered = append(filtered, channelId)
		}
	}
	return filtered
}

func filterExcludedChannels(channels []int, excludedChannelIDs map[int]struct{}) []int {
	if len(channels) == 0 || len(excludedChannelIDs) == 0 {
		return channels
	}
	filtered := make([]int, 0, len(channels))
	for _, channelId := range channels {
		if _, excluded := excludedChannelIDs[channelId]; excluded {
			continue
		}
		filtered = append(filtered, channelId)
	}
	return filtered
}

func filterChannelsBySelectionOptions(channels []int, options ChannelSelectionOptions, traceFn ChannelSelectionTraceFunc, group string, modelName string) []int {
	if !options.RequireImageInputSupport || len(channels) == 0 {
		return channels
	}
	filtered := make([]int, 0, len(channels))
	for _, channelId := range channels {
		channel, ok := channelsIDM[channelId]
		if !ok {
			filtered = append(filtered, channelId)
			continue
		}
		if channel.SupportsImageInput() {
			filtered = append(filtered, channelId)
			continue
		}
		recordChannelSelectionTrace(traceFn, ChannelSelectionTraceEvent{
			Stage:     "capability",
			Action:    "skip",
			Group:     group,
			Model:     modelName,
			ChannelID: channel.Id,
			Priority:  channel.GetPriority(),
			Reason:    "image input unsupported",
		})
	}
	return filtered
}

func CacheGetChannel(id int) (*Channel, error) {
	if !common.MemoryCacheEnabled {
		return GetChannelById(id, true)
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	return c, nil
}

func CacheGetChannelInfo(id int) (*ChannelInfo, error) {
	if !common.MemoryCacheEnabled {
		channel, err := GetChannelById(id, true)
		if err != nil {
			return nil, err
		}
		return &channel.ChannelInfo, nil
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	return &c.ChannelInfo, nil
}

func CacheUpdateChannelStatus(id int, status int) {
	if !common.MemoryCacheEnabled {
		return
	}
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if channel, ok := channelsIDM[id]; ok {
		channel.Status = status
	}
	if status != common.ChannelStatusEnabled {
		// delete the channel from group2model2channels
		for group, model2channels := range group2model2channels {
			for model, channels := range model2channels {
				for i, channelId := range channels {
					if channelId == id {
						// remove the channel from the slice
						group2model2channels[group][model] = append(channels[:i], channels[i+1:]...)
						break
					}
				}
			}
		}
	}
}

func CacheUpdateChannel(channel *Channel) {
	if !common.MemoryCacheEnabled {
		return
	}
	channelSyncLock.Lock()
	if channel == nil {
		channelSyncLock.Unlock()
		return
	}

	if channelsIDM == nil {
		channelsIDM = make(map[int]*Channel)
	}
	if oldChannel, ok := channelsIDM[channel.Id]; ok {
		logger.LogDebug(nil, "CacheUpdateChannel before: id=%d, name=%s, status=%d, polling_index=%d", channel.Id, channel.Name, channel.Status, oldChannel.ChannelInfo.MultiKeyPollingIndex)
	}
	channelsIDM[channel.Id] = channel
	if channel2advancedCustomConfig == nil {
		channel2advancedCustomConfig = make(map[int]*dto.AdvancedCustomConfig)
	}
	delete(channel2advancedCustomConfig, channel.Id)
	if channel.Type == constant.ChannelTypeAdvancedCustom {
		if config := channel.GetOtherSettings().AdvancedCustom; config != nil {
			channel2advancedCustomConfig[channel.Id] = config
		}
	}
	logger.LogDebug(nil, "CacheUpdateChannel after: id=%d, name=%s, status=%d, polling_index=%d", channel.Id, channel.Name, channel.Status, channel.ChannelInfo.MultiKeyPollingIndex)
	// Lock ordering: do NOT hold channelSyncLock while calling
	// InvalidatePricingCache. GetPricing acquires updatePricingLock first and then
	// channelSyncLock.RLock (via loadPricingAdvancedCustomConfigs); acquiring
	// updatePricingLock while holding channelSyncLock would be an AB-BA deadlock.
	channelSyncLock.Unlock()
	InvalidatePricingCache()
}
