package service

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/cachex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
)

type ChannelHealthState string

const (
	ChannelHealthStateHealthy ChannelHealthState = "healthy"
	ChannelHealthStateOpen    ChannelHealthState = "open"
	ChannelHealthStateProbing ChannelHealthState = "probing"
	ChannelHealthStateWarming ChannelHealthState = "warming"

	ginKeyChannelHealthAttempt = "channel_health_attempt"

	channelHealthIsolationCacheNamespace = "new-api:channel_health:isolation:v1"
	channelHealthProbeLockNamespace      = "new-api:channel_health:probe_lock:v1"
)

type ChannelAttemptMeta struct {
	ChannelID   int
	ChannelName string
	ModelName   string
	RequestKind string
	Cancel      func()
	Release     func()
	Probe       bool
}

type ChannelAttemptResult struct {
	Error      *types.NewAPIError
	StatusCode int
}

type ChannelHealthProbeFunc func(ctx context.Context, channel *model.Channel) error

type AttemptHandle struct {
	channelID int
	attemptID int64
}

type ChannelHealthSnapshot struct {
	ChannelID          int                `json:"channel_id"`
	State              ChannelHealthState `json:"state"`
	Reason             string             `json:"reason"`
	OpenedAt           int64              `json:"opened_at"`
	NextProbeAt        int64              `json:"next_probe_at"`
	ProbeInProgress    bool               `json:"probe_in_progress"`
	ConsecutiveFailure int                `json:"consecutive_failure"`
	ProbeSuccesses     int                `json:"probe_successes"`
	ProbeFailures      int                `json:"probe_failures"`
	Inflight           int                `json:"inflight"`
	WindowSamples      int                `json:"window_samples"`
	WindowFailures     int                `json:"window_failures"`
	ErrorRate          float64            `json:"error_rate"`
	WarmupStartedAt    int64              `json:"warmup_started_at"`
	WarmupEndsAt       int64              `json:"warmup_ends_at"`
	WarmupPercent      int                `json:"warmup_percent"`
}

type channelAttemptState struct {
	meta              ChannelAttemptMeta
	startedAt         time.Time
	firstResponseSeen bool
	stuck             bool
	cancelled         bool
}

type channelHealthSample struct {
	at      time.Time
	failed  bool
	reason  string
	status  int
	errCode string
}

type channelHealthStateData struct {
	channelID          int
	state              ChannelHealthState
	reason             string
	openedAt           time.Time
	nextProbeAt        time.Time
	probeInProgress    bool
	consecutiveFailure int
	probeSuccesses     int
	probeFailures      int
	probeBackoff       time.Duration
	warmupStartedAt    time.Time
	warmupEndsAt       time.Time
	inflight           map[int64]*channelAttemptState
	samples            []channelHealthSample
}

var channelHealth = struct {
	sync.Mutex
	nextAttemptID int64
	channels      map[int]*channelHealthStateData
	now           func() time.Time
	workerOnce    sync.Once
	probeFunc     ChannelHealthProbeFunc
	cacheOnce     sync.Once
	cache         *cachex.HybridCache[ChannelHealthSnapshot]
}{
	channels: make(map[int]*channelHealthStateData),
	now:      time.Now,
}

func init() {
	model.SetChannelRuntimeStateFunc(func(channelID int, mode model.ChannelRuntimeStateMode) (bool, int) {
		switch mode {
		case model.ChannelRuntimeStateProbe:
			return IsChannelProbeAvailable(channelID), GetChannelInflight(channelID)
		case model.ChannelRuntimeStateClaimProbe:
			return MarkChannelProbing(channelID), GetChannelInflight(channelID)
		default:
			return IsChannelAvailable(channelID), GetChannelInflight(channelID)
		}
	})
	relaycommon.MarkChannelHealthFirstResponseFunc = MarkChannelHealthFirstResponse
}

func getChannelHealthIsolationCache() *cachex.HybridCache[ChannelHealthSnapshot] {
	channelHealth.cacheOnce.Do(func() {
		channelHealth.cache = cachex.NewHybridCache[ChannelHealthSnapshot](cachex.HybridCacheConfig[ChannelHealthSnapshot]{
			Namespace:  cachex.Namespace(channelHealthIsolationCacheNamespace),
			Redis:      common.RDB,
			RedisCodec: cachex.JSONCodec[ChannelHealthSnapshot]{},
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			Memory: func() *hot.HotCache[string, ChannelHealthSnapshot] {
				return hot.NewHotCache[string, ChannelHealthSnapshot](hot.LRU, 10_000).
					WithTTL(channelHealthIsolationTTL(defaultChannelHealthSetting())).
					WithJanitor().
					Build()
			},
		})
	})
	return channelHealth.cache
}

func channelHealthIsolationTTL(setting operation_setting.ChannelHealthSetting) time.Duration {
	seconds := setting.WindowSeconds + setting.ProbeBackoffMaxSeconds + setting.ProbeIntervalSeconds + setting.WarmupDurationSeconds
	if seconds <= 0 {
		seconds = 600
	}
	return time.Duration(seconds) * time.Second
}

func defaultChannelHealthSetting() operation_setting.ChannelHealthSetting {
	setting := operation_setting.GetChannelHealthSetting()
	if setting == nil {
		return operation_setting.ChannelHealthSetting{}
	}
	normalized := *setting
	if normalized.WindowSeconds <= 0 {
		normalized.WindowSeconds = 180
	}
	if normalized.MinSamples <= 0 {
		normalized.MinSamples = 10
	}
	if normalized.MinFailures <= 0 {
		normalized.MinFailures = 5
	}
	if normalized.ErrorRateThreshold <= 0 {
		normalized.ErrorRateThreshold = 0.40
	}
	if normalized.ConsecutiveFailureThreshold <= 0 {
		normalized.ConsecutiveFailureThreshold = 5
	}
	if normalized.FirstResponseTimeoutSeconds <= 0 {
		normalized.FirstResponseTimeoutSeconds = 45
	}
	if normalized.StuckInflightThreshold <= 0 {
		normalized.StuckInflightThreshold = 3
	}
	if normalized.SingleStuckTimeoutSeconds <= 0 {
		normalized.SingleStuckTimeoutSeconds = 75
	}
	if normalized.ProbeIntervalSeconds <= 0 {
		normalized.ProbeIntervalSeconds = 30
	}
	if normalized.ProbeTimeoutSeconds <= 0 {
		normalized.ProbeTimeoutSeconds = 30
	}
	if normalized.ProbeSuccessesToRecover <= 0 {
		normalized.ProbeSuccessesToRecover = 2
	}
	if normalized.ProbeBackoffMaxSeconds <= 0 {
		normalized.ProbeBackoffMaxSeconds = 300
	}
	if normalized.WarmupDurationSeconds <= 0 {
		normalized.WarmupDurationSeconds = 60
	}
	if normalized.WarmupStartPercent <= 0 {
		normalized.WarmupStartPercent = 10
	}
	if normalized.WarmupStartPercent > 100 {
		normalized.WarmupStartPercent = 100
	}
	if normalized.WarmupStepPercent <= 0 {
		normalized.WarmupStepPercent = 30
	}
	if normalized.WarmupStepPercent > 100 {
		normalized.WarmupStepPercent = 100
	}
	return normalized
}

func channelHealthEnabled() bool {
	setting := operation_setting.GetChannelHealthSetting()
	return setting != nil && setting.Enabled
}

func channelHealthNow() time.Time {
	if channelHealth.now == nil {
		return time.Now()
	}
	return channelHealth.now()
}

func getOrCreateChannelHealthLocked(channelID int) *channelHealthStateData {
	if channelHealth.channels == nil {
		channelHealth.channels = make(map[int]*channelHealthStateData)
	}
	state, ok := channelHealth.channels[channelID]
	if ok {
		return state
	}
	state = &channelHealthStateData{
		channelID: channelID,
		state:     ChannelHealthStateHealthy,
		inflight:  make(map[int64]*channelAttemptState),
	}
	channelHealth.channels[channelID] = state
	return state
}

func ResetChannelHealthForTest() {
	channelHealth.Lock()
	defer channelHealth.Unlock()

	channelHealth.nextAttemptID = 0
	channelHealth.channels = make(map[int]*channelHealthStateData)
	channelHealth.now = time.Now
	channelHealth.workerOnce = sync.Once{}
	channelHealth.probeFunc = nil
	channelHealth.cacheOnce = sync.Once{}
	channelHealth.cache = nil
}

func SetChannelHealthNowFuncForTest(now func() time.Time) {
	channelHealth.Lock()
	defer channelHealth.Unlock()
	if now == nil {
		channelHealth.now = time.Now
		return
	}
	channelHealth.now = now
}

func SetChannelHealthProbeFunc(fn ChannelHealthProbeFunc) {
	channelHealth.Lock()
	defer channelHealth.Unlock()
	channelHealth.probeFunc = fn
}

func IsChannelAvailable(channelID int) bool {
	if channelID <= 0 || !channelHealthEnabled() {
		return true
	}

	now := channelHealthNow()
	if snapshot, found := getChannelHealthIsolationSnapshot(channelID, now); found {
		return isChannelHealthSnapshotAvailable(snapshot, now)
	}

	channelHealth.Lock()
	state, ok := channelHealth.channels[channelID]
	if ok {
		available := isChannelAvailableLocked(state, now, defaultChannelHealthSetting())
		channelHealth.Unlock()
		return available
	}
	channelHealth.Unlock()

	return true
}

func IsChannelProbeAvailable(channelID int) bool {
	if channelID <= 0 || !channelHealthEnabled() {
		return true
	}

	now := channelHealthNow()
	channelHealth.Lock()
	state, ok := channelHealth.channels[channelID]
	if ok {
		available := isChannelProbeAvailableLocked(state, now)
		channelHealth.Unlock()
		return available
	}
	channelHealth.Unlock()

	snapshot, found := getChannelHealthIsolationSnapshot(channelID, now)
	if !found {
		return true
	}
	return snapshot.State == ChannelHealthStateHealthy
}

func isChannelProbeAvailableLocked(state *channelHealthStateData, now time.Time) bool {
	if state == nil {
		return true
	}
	if state.state == ChannelHealthStateHealthy {
		return true
	}
	if state.state != ChannelHealthStateOpen && state.state != ChannelHealthStateProbing {
		return false
	}
	if state.probeInProgress {
		return false
	}
	if !state.nextProbeAt.IsZero() && now.Before(state.nextProbeAt) {
		return false
	}
	return true
}

func isChannelAvailableLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) bool {
	if state == nil {
		return true
	}
	if state.state == ChannelHealthStateHealthy {
		return true
	}
	if state.state == ChannelHealthStateWarming {
		if isChannelWarmupCompleteLocked(state, now) {
			markChannelHealthyLocked(state)
			deleteChannelHealthIsolation(state.channelID)
			return true
		}
		percent := channelWarmupPercentLocked(state, now, setting)
		return percent >= 100 || common.GetRandomInt(100) < percent
	}
	return false
}

func isChannelWarmupCompleteLocked(state *channelHealthStateData, now time.Time) bool {
	return state.warmupEndsAt.IsZero() || !now.Before(state.warmupEndsAt)
}

func getChannelHealthIsolationSnapshot(channelID int, now time.Time) (ChannelHealthSnapshot, bool) {
	snapshot, found, err := getChannelHealthIsolationCache().Get(channelHealthCacheKey(channelID))
	if err != nil {
		common.SysError(fmt.Sprintf("channel health isolation cache get failed: channel_id=%d, err=%v", channelID, err))
		return ChannelHealthSnapshot{}, false
	}
	if !found {
		return ChannelHealthSnapshot{}, false
	}
	if snapshot.State == ChannelHealthStateWarming {
		snapshot.WarmupPercent = channelWarmupPercentFromSnapshot(snapshot, now, defaultChannelHealthSetting())
		if snapshot.WarmupEndsAt <= 0 || now.Unix() >= snapshot.WarmupEndsAt {
			snapshot.State = ChannelHealthStateHealthy
			snapshot.Reason = ""
			snapshot.OpenedAt = 0
			snapshot.NextProbeAt = 0
			snapshot.WarmupStartedAt = 0
			snapshot.WarmupEndsAt = 0
			snapshot.WarmupPercent = 100
			deleteChannelHealthIsolation(channelID)
		}
	}
	return snapshot, true
}

func isChannelHealthSnapshotAvailable(snapshot ChannelHealthSnapshot, now time.Time) bool {
	switch snapshot.State {
	case ChannelHealthStateHealthy:
		return true
	case ChannelHealthStateWarming:
		if snapshot.WarmupEndsAt <= 0 || now.Unix() >= snapshot.WarmupEndsAt {
			return true
		}
		percent := channelWarmupPercentFromSnapshot(snapshot, now, defaultChannelHealthSetting())
		if percent >= 100 {
			return true
		}
		return common.GetRandomInt(100) < percent
	default:
		return false
	}
}

func GetChannelInflight(channelID int) int {
	if channelID <= 0 || !channelHealthEnabled() {
		return 0
	}

	channelHealth.Lock()
	defer channelHealth.Unlock()

	state, ok := channelHealth.channels[channelID]
	if !ok {
		return 0
	}
	return len(state.inflight)
}

func RecordAttemptStart(meta ChannelAttemptMeta) AttemptHandle {
	if meta.ChannelID <= 0 || !channelHealthEnabled() {
		return AttemptHandle{}
	}

	channelHealth.Lock()
	defer channelHealth.Unlock()

	channelHealth.nextAttemptID++
	handle := AttemptHandle{
		channelID: meta.ChannelID,
		attemptID: channelHealth.nextAttemptID,
	}
	state := getOrCreateChannelHealthLocked(meta.ChannelID)
	if state.inflight == nil {
		state.inflight = make(map[int64]*channelAttemptState)
	}
	state.inflight[handle.attemptID] = &channelAttemptState{
		meta:      meta,
		startedAt: channelHealthNow(),
	}
	if state.state == ChannelHealthStateProbing {
		state.inflight[handle.attemptID].meta.Probe = true
		state.probeInProgress = true
		persistChannelHealthIsolationLocked(state, channelHealthNow(), defaultChannelHealthSetting())
	}
	return handle
}

func StartChannelHealthAttemptForContext(c *gin.Context) AttemptHandle {
	if c == nil {
		return AttemptHandle{}
	}
	if !channelHealthEnabled() {
		return AttemptHandle{}
	}
	channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	if channelID <= 0 {
		return AttemptHandle{}
	}
	var cancel context.CancelFunc
	var release func()
	requestPath := ""
	if c.Request != nil {
		if c.Request.URL != nil {
			requestPath = c.Request.URL.Path
		}
		parentCtx := c.Request.Context()
		attemptCtx, attemptCancel := context.WithCancel(parentCtx)
		c.Request = c.Request.WithContext(attemptCtx)
		cancel = attemptCancel
		release = func() {
			if c.Request != nil {
				c.Request = c.Request.WithContext(parentCtx)
			}
			attemptCancel()
		}
	}
	handle := RecordAttemptStart(ChannelAttemptMeta{
		ChannelID:   channelID,
		ChannelName: common.GetContextKeyString(c, constant.ContextKeyChannelName),
		ModelName:   c.GetString("original_model"),
		RequestKind: common.MetricsRequestKindFromPath(requestPath),
		Cancel:      cancel,
		Release:     release,
		Probe:       IsChannelProbeAvailable(channelID) && !IsChannelAvailable(channelID),
	})
	if handle.channelID > 0 {
		c.Set(ginKeyChannelHealthAttempt, handle)
	}
	return handle
}

func MarkChannelHealthFirstResponse(c *gin.Context) {
	handle, ok := getChannelHealthAttemptFromContext(c)
	if !ok {
		return
	}
	RecordFirstResponse(handle)
}

func FinishChannelHealthAttemptForContext(c *gin.Context, result ChannelAttemptResult) {
	handle, ok := getChannelHealthAttemptFromContext(c)
	if !ok {
		return
	}
	RecordAttemptFinish(handle, result)
	c.Set(ginKeyChannelHealthAttempt, AttemptHandle{})
}

func getChannelHealthAttemptFromContext(c *gin.Context) (AttemptHandle, bool) {
	if c == nil {
		return AttemptHandle{}, false
	}
	v, ok := c.Get(ginKeyChannelHealthAttempt)
	if !ok {
		return AttemptHandle{}, false
	}
	handle, ok := v.(AttemptHandle)
	if !ok || handle.channelID <= 0 || handle.attemptID <= 0 {
		return AttemptHandle{}, false
	}
	return handle, true
}

func RecordFirstResponse(handle AttemptHandle) {
	if handle.channelID <= 0 || handle.attemptID <= 0 || !channelHealthEnabled() {
		return
	}

	channelHealth.Lock()
	defer channelHealth.Unlock()

	state, ok := channelHealth.channels[handle.channelID]
	if !ok {
		return
	}
	attempt, ok := state.inflight[handle.attemptID]
	if !ok {
		return
	}
	attempt.firstResponseSeen = true
}

func RecordAttemptFinish(handle AttemptHandle, result ChannelAttemptResult) {
	if handle.channelID <= 0 || handle.attemptID <= 0 || !channelHealthEnabled() {
		return
	}

	shouldSample, failed := classifyChannelAttemptResult(result)

	channelHealth.Lock()

	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	state, ok := channelHealth.channels[handle.channelID]
	if !ok {
		channelHealth.Unlock()
		return
	}
	attempt, ok := state.inflight[handle.attemptID]
	if !ok {
		channelHealth.Unlock()
		return
	}
	delete(state.inflight, handle.attemptID)
	release := attempt.meta.Release

	reason := ""
	status := result.StatusCode
	errCode := ""
	if result.Error != nil {
		reason = result.Error.ErrorWithStatusCode()
		status = result.Error.StatusCode
		errCode = string(result.Error.GetErrorCode())
	}
	clearChannelID := 0
	shouldClearAffinity := false
	if shouldSample {
		recordChannelHealthSampleLocked(state, now, setting, failed, reason, status, errCode)
		if attempt.meta.Probe {
			clearChannelID, shouldClearAffinity = recordProbeAttemptResultLocked(state, now, setting, !failed, reason)
		} else {
			clearChannelID, shouldClearAffinity = evaluateChannelHealthLocked(state, now, setting)
		}
	}
	channelHealth.Unlock()

	if release != nil {
		release()
	}
	if shouldClearAffinity {
		ClearChannelAffinityByChannelID(clearChannelID)
	}
}

func classifyChannelAttemptResult(result ChannelAttemptResult) (bool, bool) {
	if result.Error != nil {
		if !ShouldRecordChannelHealthFailure(nil, result.Error) {
			return false, false
		}
		return true, true
	}
	failed := result.StatusCode == http.StatusRequestTimeout ||
		result.StatusCode == http.StatusTooManyRequests ||
		result.StatusCode >= http.StatusInternalServerError
	return true, failed
}

func ShouldRecordChannelHealthFailure(_ *gin.Context, err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if types.IsChannelError(err) {
		return true
	}
	if ShouldDisableChannel(err) {
		return true
	}
	if types.IsSkipRetryError(err) {
		return false
	}
	code := err.StatusCode
	if code == http.StatusRequestTimeout || code == http.StatusTooManyRequests {
		return true
	}
	if code >= http.StatusInternalServerError {
		return true
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

func recordChannelHealthSampleLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting, failed bool, reason string, status int, errCode string) {
	cutoff := now.Add(-time.Duration(setting.WindowSeconds) * time.Second)
	samples := state.samples[:0]
	for _, sample := range state.samples {
		if sample.at.After(cutoff) || sample.at.Equal(cutoff) {
			samples = append(samples, sample)
		}
	}
	samples = append(samples, channelHealthSample{
		at:      now,
		failed:  failed,
		reason:  reason,
		status:  status,
		errCode: errCode,
	})
	state.samples = samples
	if failed {
		state.consecutiveFailure++
	} else {
		state.consecutiveFailure = 0
	}
}

func evaluateChannelHealthLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) (int, bool) {
	if state == nil || state.state == ChannelHealthStateOpen || state.state == ChannelHealthStateProbing {
		return 0, false
	}
	if state.state == ChannelHealthStateWarming {
		if isChannelWarmupCompleteLocked(state, now) {
			markChannelHealthyLocked(state)
			deleteChannelHealthIsolation(state.channelID)
			return 0, false
		}
		if state.consecutiveFailure > 0 {
			return openChannelLocked(state, now, setting, "warmup failure")
		}
		return 0, false
	}

	samples, failures := channelHealthWindowCountsLocked(state, now, setting)
	if samples >= setting.MinSamples && failures >= setting.MinFailures {
		errorRate := float64(failures) / float64(samples)
		if errorRate >= setting.ErrorRateThreshold {
			return openChannelLocked(state, now, setting, fmt.Sprintf("error_rate %.2f%% over %ds (%d/%d)", errorRate*100, setting.WindowSeconds, failures, samples))
		}
	}

	if state.consecutiveFailure >= setting.ConsecutiveFailureThreshold {
		return openChannelLocked(state, now, setting, fmt.Sprintf("consecutive_failures %d", state.consecutiveFailure))
	}
	return 0, false
}

func channelHealthWindowCountsLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) (int, int) {
	cutoff := now.Add(-time.Duration(setting.WindowSeconds) * time.Second)
	samples := 0
	failures := 0
	for _, sample := range state.samples {
		if sample.at.Before(cutoff) {
			continue
		}
		samples++
		if sample.failed {
			failures++
		}
	}
	return samples, failures
}

func OpenChannel(channelID int, reason string) {
	if channelID <= 0 || !channelHealthEnabled() {
		return
	}

	channelHealth.Lock()

	setting := defaultChannelHealthSetting()
	state := getOrCreateChannelHealthLocked(channelID)
	clearChannelID, shouldClearAffinity := openChannelLocked(state, channelHealthNow(), setting, reason)
	channelHealth.Unlock()

	if shouldClearAffinity {
		ClearChannelAffinityByChannelID(clearChannelID)
	}
}

func MarkChannelProbing(channelID int) bool {
	if channelID <= 0 || !channelHealthEnabled() {
		return false
	}

	channelHealth.Lock()
	defer channelHealth.Unlock()

	now := channelHealthNow()
	state := getOrCreateChannelHealthLocked(channelID)
	if state.state == ChannelHealthStateHealthy {
		return true
	}
	if state.state != ChannelHealthStateOpen && state.state != ChannelHealthStateProbing {
		return false
	}
	if !state.nextProbeAt.IsZero() && now.Before(state.nextProbeAt) {
		return false
	}
	if state.probeInProgress {
		return false
	}
	setting := defaultChannelHealthSetting()
	state.state = ChannelHealthStateProbing
	state.probeInProgress = true
	persistChannelHealthIsolationLocked(state, now, setting)
	return true
}

func openChannelLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting, reason string) (int, bool) {
	if state == nil {
		return 0, false
	}
	wasAvailable := state.state == ChannelHealthStateHealthy
	state.state = ChannelHealthStateOpen
	state.reason = reason
	state.openedAt = now
	state.nextProbeAt = now.Add(time.Duration(setting.ProbeIntervalSeconds) * time.Second)
	state.probeSuccesses = 0
	state.warmupStartedAt = time.Time{}
	state.warmupEndsAt = time.Time{}
	if state.probeBackoff <= 0 {
		state.probeBackoff = time.Duration(setting.ProbeIntervalSeconds) * time.Second
	}
	common.SysLog(fmt.Sprintf("channel health opened: channel_id=%d reason=%s", state.channelID, reason))
	persistChannelHealthIsolationLocked(state, now, setting)
	return state.channelID, wasAvailable
}

func RecordProbeResult(channelID int, success bool, reason string) {
	if channelID <= 0 || !channelHealthEnabled() {
		return
	}

	channelHealth.Lock()
	defer channelHealth.Unlock()

	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	state := getOrCreateChannelHealthLocked(channelID)
	recordProbeAttemptResultLocked(state, now, setting, success, reason)
}

func recordProbeAttemptResultLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting, success bool, reason string) (int, bool) {
	if state == nil {
		return 0, false
	}
	state.probeInProgress = false
	if success {
		state.probeSuccesses++
		state.probeFailures = 0
		if state.probeSuccesses >= setting.ProbeSuccessesToRecover {
			if setting.WarmupEnabled {
				state.state = ChannelHealthStateWarming
				state.reason = "warming"
				state.nextProbeAt = time.Time{}
				state.consecutiveFailure = 0
				state.probeSuccesses = 0
				state.probeBackoff = 0
				state.samples = nil
				state.warmupStartedAt = now
				state.warmupEndsAt = now.Add(time.Duration(setting.WarmupDurationSeconds) * time.Second)
				persistChannelHealthIsolationLocked(state, now, setting)
			} else {
				markChannelHealthyLocked(state)
				deleteChannelHealthIsolation(state.channelID)
			}
		} else {
			state.state = ChannelHealthStateProbing
			state.nextProbeAt = now.Add(time.Duration(setting.ProbeIntervalSeconds) * time.Second)
			persistChannelHealthIsolationLocked(state, now, setting)
		}
		return 0, false
	}

	state.state = ChannelHealthStateOpen
	state.reason = reason
	state.warmupStartedAt = time.Time{}
	state.warmupEndsAt = time.Time{}
	state.probeSuccesses = 0
	state.probeFailures++
	if state.probeBackoff <= 0 {
		state.probeBackoff = time.Duration(setting.ProbeIntervalSeconds) * time.Second
	} else {
		state.probeBackoff *= 2
	}
	maxBackoff := time.Duration(setting.ProbeBackoffMaxSeconds) * time.Second
	if state.probeBackoff > maxBackoff {
		state.probeBackoff = maxBackoff
	}
	state.nextProbeAt = now.Add(state.probeBackoff)
	persistChannelHealthIsolationLocked(state, now, setting)
	return 0, false
}

func markChannelHealthyLocked(state *channelHealthStateData) {
	state.state = ChannelHealthStateHealthy
	state.reason = ""
	state.openedAt = time.Time{}
	state.nextProbeAt = time.Time{}
	state.consecutiveFailure = 0
	state.probeSuccesses = 0
	state.probeFailures = 0
	state.probeBackoff = 0
	state.warmupStartedAt = time.Time{}
	state.warmupEndsAt = time.Time{}
	state.samples = nil
}

func CheckChannelHealthStuckRequests() {
	if !channelHealthEnabled() {
		return
	}

	channelHealth.Lock()

	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	firstResponseTimeout := time.Duration(setting.FirstResponseTimeoutSeconds) * time.Second
	singleStuckTimeout := time.Duration(setting.SingleStuckTimeoutSeconds) * time.Second
	clearChannelIDs := make([]int, 0)
	cancelStuckAttempts := make([]func(), 0)

	for _, state := range channelHealth.channels {
		stuckCount := 0
		var maxAge time.Duration
		for _, attempt := range state.inflight {
			if attempt.firstResponseSeen {
				continue
			}
			age := now.Sub(attempt.startedAt)
			if age < firstResponseTimeout {
				continue
			}
			attempt.stuck = true
			stuckCount++
			if age > maxAge {
				maxAge = age
			}
		}
		if stuckCount >= setting.StuckInflightThreshold || (stuckCount > 0 && maxAge >= singleStuckTimeout) {
			if channelID, shouldClear := openChannelLocked(state, now, setting, fmt.Sprintf("stuck inflight=%d max_age=%s", stuckCount, maxAge.Round(time.Second))); shouldClear {
				clearChannelIDs = append(clearChannelIDs, channelID)
			}
			for _, attempt := range state.inflight {
				if !attempt.stuck || attempt.cancelled || attempt.meta.Cancel == nil {
					continue
				}
				attempt.cancelled = true
				cancelStuckAttempts = append(cancelStuckAttempts, attempt.meta.Cancel)
			}
		}
	}
	channelHealth.Unlock()

	for _, cancel := range cancelStuckAttempts {
		cancel()
	}
	for _, channelID := range clearChannelIDs {
		ClearChannelAffinityByChannelID(channelID)
	}
}

func RunChannelHealthProbeWorker() {
	channelHealth.workerOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				CheckChannelHealthStuckRequests()
				RunDueChannelHealthProbes()
			}
		}()
	})
}

func RunDueChannelHealthProbes() {
	if !channelHealthEnabled() {
		return
	}

	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	type probeTarget struct {
		channelID int
		probeFn   ChannelHealthProbeFunc
	}
	targets := make([]probeTarget, 0)

	channelHealth.Lock()
	for channelID, state := range channelHealth.channels {
		if state.state != ChannelHealthStateOpen && state.state != ChannelHealthStateProbing {
			continue
		}
		if state.probeInProgress {
			continue
		}
		if !state.nextProbeAt.IsZero() && now.Before(state.nextProbeAt) {
			continue
		}
		if channelHealth.probeFunc == nil {
			continue
		}
		if !tryAcquireChannelHealthProbeLock(channelID, setting) {
			continue
		}
		state.probeInProgress = true
		targets = append(targets, probeTarget{channelID: channelID, probeFn: channelHealth.probeFunc})
	}
	channelHealth.Unlock()

	for _, target := range targets {
		go runChannelHealthProbe(target.channelID, target.probeFn, setting)
	}
}

func runChannelHealthProbe(channelID int, probeFn ChannelHealthProbeFunc, setting operation_setting.ChannelHealthSetting) {
	defer releaseChannelHealthProbeLock(channelID)

	channel, err := model.CacheGetChannel(channelID)
	if err != nil || channel == nil {
		RecordProbeResult(channelID, false, fmt.Sprintf("probe load channel failed: %v", err))
		return
	}
	if channel.Status != common.ChannelStatusEnabled {
		RecordProbeResult(channelID, false, fmt.Sprintf("probe skipped: channel status %d", channel.Status))
		return
	}

	timeout := time.Duration(setting.ProbeTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err = probeFn(ctx, channel)
	if err != nil {
		RecordProbeResult(channelID, false, err.Error())
		return
	}
	RecordProbeResult(channelID, true, "")
}

func tryAcquireChannelHealthProbeLock(channelID int, setting operation_setting.ChannelHealthSetting) bool {
	if channelID <= 0 {
		return false
	}
	if !common.RedisEnabled || common.RDB == nil {
		return true
	}
	ttl := time.Duration(setting.ProbeTimeoutSeconds+5) * time.Second
	if ttl <= 0 {
		ttl = 35 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ok, err := common.RDB.SetNX(ctx, channelHealthProbeLockKey(channelID), common.GetTimeString(), ttl).Result()
	if err != nil {
		common.SysError(fmt.Sprintf("channel health probe lock failed: channel_id=%d, err=%v", channelID, err))
		return false
	}
	return ok
}

func releaseChannelHealthProbeLock(channelID int) {
	if channelID <= 0 || !common.RedisEnabled || common.RDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := common.RDB.Del(ctx, channelHealthProbeLockKey(channelID)).Err(); err != nil {
		common.SysError(fmt.Sprintf("channel health probe unlock failed: channel_id=%d, err=%v", channelID, err))
	}
}

func channelHealthProbeLockKey(channelID int) string {
	return fmt.Sprintf("%s:%d", channelHealthProbeLockNamespace, channelID)
}

func GetChannelHealthSnapshot(channelID int) (ChannelHealthSnapshot, bool) {
	now := channelHealthNow()
	if snapshot, found := getChannelHealthIsolationSnapshot(channelID, now); found {
		return snapshot, true
	}

	channelHealth.Lock()
	defer channelHealth.Unlock()

	state, ok := channelHealth.channels[channelID]
	if !ok {
		return ChannelHealthSnapshot{}, false
	}
	return buildChannelHealthSnapshotLocked(state, now, defaultChannelHealthSetting()), true
}

func GetChannelHealthSnapshotForDisplay(channelID int) ChannelHealthSnapshot {
	if snapshot, ok := GetChannelHealthSnapshot(channelID); ok {
		return snapshot
	}
	return ChannelHealthSnapshot{
		ChannelID:     channelID,
		State:         ChannelHealthStateHealthy,
		WarmupPercent: 100,
	}
}

func GetChannelHealthSnapshots() []ChannelHealthSnapshot {
	channelHealth.Lock()
	defer channelHealth.Unlock()

	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	snapshots := make([]ChannelHealthSnapshot, 0, len(channelHealth.channels))
	for _, state := range channelHealth.channels {
		snapshots = append(snapshots, buildChannelHealthSnapshotLocked(state, now, setting))
	}
	return snapshots
}

func buildChannelHealthSnapshotLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) ChannelHealthSnapshot {
	if state.state == ChannelHealthStateWarming && isChannelWarmupCompleteLocked(state, now) {
		markChannelHealthyLocked(state)
		deleteChannelHealthIsolation(state.channelID)
	}
	samples, failures := channelHealthWindowCountsLocked(state, now, setting)
	errorRate := 0.0
	if samples > 0 {
		errorRate = float64(failures) / float64(samples)
	}
	warmupPercent := channelWarmupPercentLocked(state, now, setting)
	return ChannelHealthSnapshot{
		ChannelID:          state.channelID,
		State:              state.state,
		Reason:             state.reason,
		OpenedAt:           unixOrZero(state.openedAt),
		NextProbeAt:        unixOrZero(state.nextProbeAt),
		ProbeInProgress:    state.probeInProgress,
		ConsecutiveFailure: state.consecutiveFailure,
		ProbeSuccesses:     state.probeSuccesses,
		ProbeFailures:      state.probeFailures,
		Inflight:           len(state.inflight),
		WindowSamples:      samples,
		WindowFailures:     failures,
		ErrorRate:          errorRate,
		WarmupStartedAt:    unixOrZero(state.warmupStartedAt),
		WarmupEndsAt:       unixOrZero(state.warmupEndsAt),
		WarmupPercent:      warmupPercent,
	}
}

func channelWarmupPercentLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) int {
	if state == nil {
		return 100
	}
	if state.state == ChannelHealthStateHealthy {
		return 100
	}
	if state.state != ChannelHealthStateWarming {
		return 0
	}
	if isChannelWarmupCompleteLocked(state, now) {
		return 100
	}
	duration := state.warmupEndsAt.Sub(state.warmupStartedAt)
	if duration <= 0 {
		return 100
	}
	stepDuration := duration / 3
	if stepDuration <= 0 {
		return 100
	}
	elapsed := now.Sub(state.warmupStartedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	stepCount := int(elapsed / stepDuration)
	if stepCount < 0 {
		stepCount = 0
	}
	percent := setting.WarmupStartPercent + stepCount*setting.WarmupStepPercent
	if percent < 1 {
		percent = 1
	}
	if percent > 100 {
		percent = 100
	}
	return percent
}

func channelWarmupPercentFromSnapshot(snapshot ChannelHealthSnapshot, now time.Time, setting operation_setting.ChannelHealthSetting) int {
	if snapshot.State == ChannelHealthStateHealthy {
		return 100
	}
	if snapshot.State != ChannelHealthStateWarming {
		return 0
	}
	if snapshot.WarmupEndsAt <= 0 || now.Unix() >= snapshot.WarmupEndsAt {
		return 100
	}
	if snapshot.WarmupStartedAt <= 0 || snapshot.WarmupEndsAt <= snapshot.WarmupStartedAt {
		if snapshot.WarmupPercent > 0 {
			return snapshot.WarmupPercent
		}
		return setting.WarmupStartPercent
	}
	startedAt := time.Unix(snapshot.WarmupStartedAt, 0)
	endsAt := time.Unix(snapshot.WarmupEndsAt, 0)
	state := &channelHealthStateData{
		state:           ChannelHealthStateWarming,
		warmupStartedAt: startedAt,
		warmupEndsAt:    endsAt,
	}
	return channelWarmupPercentLocked(state, now, setting)
}

func persistChannelHealthIsolationLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) {
	if state == nil {
		return
	}
	snapshot := buildChannelHealthSnapshotLocked(state, now, setting)
	if snapshot.State == ChannelHealthStateHealthy {
		deleteChannelHealthIsolation(snapshot.ChannelID)
		return
	}
	if err := getChannelHealthIsolationCache().SetWithTTL(channelHealthCacheKey(snapshot.ChannelID), snapshot, channelHealthIsolationTTL(setting)); err != nil {
		common.SysError(fmt.Sprintf("channel health isolation cache set failed: channel_id=%d, err=%v", snapshot.ChannelID, err))
	}
}

func deleteChannelHealthIsolation(channelID int) {
	if channelID <= 0 {
		return
	}
	if _, err := getChannelHealthIsolationCache().DeleteMany([]string{channelHealthCacheKey(channelID)}); err != nil {
		common.SysError(fmt.Sprintf("channel health isolation cache delete failed: channel_id=%d, err=%v", channelID, err))
	}
}

func channelHealthCacheKey(channelID int) string {
	return fmt.Sprintf("%d", channelID)
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}
