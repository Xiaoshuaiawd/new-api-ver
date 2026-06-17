package service

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
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

const (
	ChannelHealthEventTypeOpened      = "opened"
	ChannelHealthEventTypeRecovered   = "recovered"
	ChannelHealthEventTypeProbeFailed = "probe_failed"
)

type ChannelAttemptMeta struct {
	ChannelID   int
	ChannelName string
	ModelName   string
	Group       string
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

type ChannelRuntimeControlResult struct {
	ChannelID       int                   `json:"channel_id"`
	AffinityDeleted int                   `json:"affinity_deleted"`
	Snapshot        ChannelHealthSnapshot `json:"snapshot"`
}

type ChannelHealthEvent struct {
	Type         string `json:"type"`
	ChannelID    int    `json:"channel_id"`
	ModelName    string `json:"model_name,omitempty"`
	Group        string `json:"group,omitempty"`
	State        string `json:"state"`
	Reason       string `json:"reason,omitempty"`
	OccurredAt   int64  `json:"occurred_at"`
	AlertSent    bool   `json:"alert_sent"`
	AlertSubject string `json:"alert_subject,omitempty"`
}

type ChannelHealthEventFilter struct {
	ChannelID int
	ModelName string
	Group     string
	Type      string
	State     string
	Limit     int
}

type ChannelHealthReport struct {
	IsolationCount         int                         `json:"isolation_count"`
	RecoveryCount          int                         `json:"recovery_count"`
	ProbeFailureCount      int                         `json:"probe_failure_count"`
	AverageFirstResponseMs float64                     `json:"average_first_response_ms"`
	TopFailingChannels     []ChannelHealthChannelCount `json:"top_failing_channels"`
	Events                 []ChannelHealthEvent        `json:"events"`
}

type ChannelHealthChannelCount struct {
	ChannelID int    `json:"channel_id"`
	ModelName string `json:"model_name,omitempty"`
	Group     string `json:"group,omitempty"`
	Count     int    `json:"count"`
}

type AttemptHandle struct {
	channelID int
	modelName string
	attemptID int64
}

type ChannelHealthSnapshot struct {
	ChannelID              int                `json:"channel_id"`
	ModelName              string             `json:"model_name,omitempty"`
	State                  ChannelHealthState `json:"state"`
	Reason                 string             `json:"reason"`
	OpenedAt               int64              `json:"opened_at"`
	NextProbeAt            int64              `json:"next_probe_at"`
	ProbeInProgress        bool               `json:"probe_in_progress"`
	ConsecutiveFailure     int                `json:"consecutive_failure"`
	ProbeSuccesses         int                `json:"probe_successes"`
	ProbeFailures          int                `json:"probe_failures"`
	Inflight               int                `json:"inflight"`
	WindowSamples          int                `json:"window_samples"`
	WindowFailures         int                `json:"window_failures"`
	ErrorRate              float64            `json:"error_rate"`
	AverageFirstResponseMs float64            `json:"average_first_response_ms"`
	WarmupStartedAt        int64              `json:"warmup_started_at"`
	WarmupEndsAt           int64              `json:"warmup_ends_at"`
	WarmupPercent          int                `json:"warmup_percent"`
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
	modelName          string
	group              string
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
	firstResponseTotal time.Duration
	firstResponseCount int
	inflight           map[int64]*channelAttemptState
	samples            []channelHealthSample
}

var channelHealth = struct {
	sync.Mutex
	nextAttemptID int64
	channels      map[string]*channelHealthStateData
	now           func() time.Time
	workerOnce    sync.Once
	probeFunc     ChannelHealthProbeFunc
	cacheOnce     sync.Once
	cache         *cachex.HybridCache[ChannelHealthSnapshot]
	events        []ChannelHealthEvent
	lastAlertAt   map[string]time.Time
	notifyFunc    func(event ChannelHealthEvent)
}{
	channels:    make(map[string]*channelHealthStateData),
	now:         time.Now,
	lastAlertAt: make(map[string]time.Time),
}

func init() {
	model.SetChannelRuntimeStateFunc(func(channelID int, modelName string, mode model.ChannelRuntimeStateMode) (bool, int) {
		switch mode {
		case model.ChannelRuntimeStateProbe:
			return IsChannelProbeAvailableForModel(channelID, modelName), GetChannelInflightForModel(channelID, modelName)
		case model.ChannelRuntimeStateClaimProbe:
			return MarkChannelProbingForModel(channelID, modelName), GetChannelInflightForModel(channelID, modelName)
		default:
			return IsChannelAvailableForModel(channelID, modelName), GetChannelInflightForModel(channelID, modelName)
		}
	})
	model.SetChannelRuntimeHealthStateFunc(func(channelID int) string {
		return string(GetChannelHealthSnapshotForDisplay(channelID).State)
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

type channelHealthScope struct {
	channelID int
	modelName string
}

func channelHealthScopeFor(channelID int, modelName string, setting operation_setting.ChannelHealthSetting) channelHealthScope {
	scope := channelHealthScope{channelID: channelID}
	if setting.ModelLevelEnabled {
		scope.modelName = strings.TrimSpace(modelName)
	}
	return scope
}

func channelHealthScopeKey(scope channelHealthScope) string {
	if scope.modelName == "" {
		return fmt.Sprintf("%d", scope.channelID)
	}
	return fmt.Sprintf("%d:model:%s", scope.channelID, scope.modelName)
}

func getOrCreateChannelHealthLocked(scope channelHealthScope) *channelHealthStateData {
	if channelHealth.channels == nil {
		channelHealth.channels = make(map[string]*channelHealthStateData)
	}
	key := channelHealthScopeKey(scope)
	state, ok := channelHealth.channels[key]
	if ok {
		return state
	}
	state = &channelHealthStateData{
		channelID: scope.channelID,
		modelName: scope.modelName,
		state:     ChannelHealthStateHealthy,
		inflight:  make(map[int64]*channelAttemptState),
	}
	channelHealth.channels[key] = state
	return state
}

func ResetChannelHealthForTest() {
	channelHealth.Lock()
	defer channelHealth.Unlock()

	channelHealth.nextAttemptID = 0
	channelHealth.channels = make(map[string]*channelHealthStateData)
	channelHealth.now = time.Now
	channelHealth.workerOnce = sync.Once{}
	channelHealth.probeFunc = nil
	channelHealth.cacheOnce = sync.Once{}
	channelHealth.cache = nil
	channelHealth.events = nil
	channelHealth.lastAlertAt = make(map[string]time.Time)
	channelHealth.notifyFunc = nil
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

func SetChannelHealthEventNotifyFuncForTest(fn func(event ChannelHealthEvent)) {
	channelHealth.Lock()
	defer channelHealth.Unlock()
	channelHealth.notifyFunc = fn
}

func IsChannelAvailable(channelID int) bool {
	return IsChannelAvailableForModel(channelID, "")
}

func IsChannelAvailableForModel(channelID int, modelName string) bool {
	if channelID <= 0 || !channelHealthEnabled() {
		return true
	}

	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	scope := channelHealthScopeFor(channelID, modelName, setting)
	if snapshot, found := getChannelHealthIsolationSnapshot(scope, now); found {
		return isChannelHealthSnapshotAvailable(snapshot, now)
	}

	channelHealth.Lock()
	state, ok := channelHealth.channels[channelHealthScopeKey(scope)]
	if ok {
		available := isChannelAvailableLocked(state, now, setting)
		channelHealth.Unlock()
		return available
	}
	channelHealth.Unlock()

	return true
}

func IsChannelProbeAvailable(channelID int) bool {
	return IsChannelProbeAvailableForModel(channelID, "")
}

func IsChannelProbeAvailableForModel(channelID int, modelName string) bool {
	if channelID <= 0 || !channelHealthEnabled() {
		return true
	}

	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	scope := channelHealthScopeFor(channelID, modelName, setting)
	channelHealth.Lock()
	state, ok := channelHealth.channels[channelHealthScopeKey(scope)]
	if ok {
		available := isChannelProbeAvailableLocked(state, now)
		channelHealth.Unlock()
		return available
	}
	channelHealth.Unlock()

	snapshot, found := getChannelHealthIsolationSnapshot(scope, now)
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
			deleteChannelHealthIsolation(channelHealthScope{channelID: state.channelID, modelName: state.modelName})
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

func getChannelHealthIsolationSnapshot(scope channelHealthScope, now time.Time) (ChannelHealthSnapshot, bool) {
	snapshot, found, err := getChannelHealthIsolationCache().Get(channelHealthCacheKey(scope))
	if err != nil {
		common.SysError(fmt.Sprintf("channel health isolation cache get failed: channel_id=%d, model=%s, err=%v", scope.channelID, scope.modelName, err))
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
			deleteChannelHealthIsolation(scope)
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
	return GetChannelInflightForModel(channelID, "")
}

func GetChannelInflightForModel(channelID int, modelName string) int {
	if channelID <= 0 || !channelHealthEnabled() {
		return 0
	}

	channelHealth.Lock()
	defer channelHealth.Unlock()

	scope := channelHealthScopeFor(channelID, modelName, defaultChannelHealthSetting())
	state, ok := channelHealth.channels[channelHealthScopeKey(scope)]
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
		modelName: strings.TrimSpace(meta.ModelName),
		attemptID: channelHealth.nextAttemptID,
	}
	setting := defaultChannelHealthSetting()
	scope := channelHealthScopeFor(meta.ChannelID, meta.ModelName, setting)
	handle.modelName = scope.modelName
	state := getOrCreateChannelHealthLocked(scope)
	if group := strings.TrimSpace(meta.Group); group != "" {
		state.group = group
	}
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
		persistChannelHealthIsolationLocked(state, channelHealthNow(), setting)
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
		Group:       common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
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

	scope := channelHealthScopeFor(handle.channelID, handle.modelName, defaultChannelHealthSetting())
	state, ok := channelHealth.channels[channelHealthScopeKey(scope)]
	if !ok {
		return
	}
	attempt, ok := state.inflight[handle.attemptID]
	if !ok {
		return
	}
	if attempt.firstResponseSeen {
		return
	}
	now := channelHealthNow()
	attempt.firstResponseSeen = true
	latency := now.Sub(attempt.startedAt)
	if latency < 0 {
		latency = 0
	}
	state.firstResponseTotal += latency
	state.firstResponseCount++
}

func RecordAttemptFinish(handle AttemptHandle, result ChannelAttemptResult) {
	if handle.channelID <= 0 || handle.attemptID <= 0 || !channelHealthEnabled() {
		return
	}

	shouldSample, failed := classifyChannelAttemptResult(result)

	channelHealth.Lock()

	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	scope := channelHealthScopeFor(handle.channelID, handle.modelName, setting)
	state, ok := channelHealth.channels[channelHealthScopeKey(scope)]
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
			deleteChannelHealthIsolation(channelHealthScope{channelID: state.channelID, modelName: state.modelName})
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
	OpenChannelForModel(channelID, "", reason)
}

func OpenChannelForModel(channelID int, modelName string, reason string) {
	if channelID <= 0 || !channelHealthEnabled() {
		return
	}

	channelHealth.Lock()

	setting := defaultChannelHealthSetting()
	scope := channelHealthScopeFor(channelID, modelName, setting)
	state := getOrCreateChannelHealthLocked(scope)
	clearChannelID, shouldClearAffinity := openChannelLocked(state, channelHealthNow(), setting, reason)
	channelHealth.Unlock()

	if shouldClearAffinity {
		ClearChannelAffinityByChannelID(clearChannelID)
	}
}

func ForceOpenChannelRuntime(channelID int, reason string, duration time.Duration) (ChannelRuntimeControlResult, error) {
	if channelID <= 0 {
		return ChannelRuntimeControlResult{}, fmt.Errorf("invalid channel_id")
	}
	if !channelHealthEnabled() {
		return ChannelRuntimeControlResult{}, fmt.Errorf("channel health guard is disabled")
	}
	channel, err := model.CacheGetChannel(channelID)
	if err != nil || channel == nil {
		return ChannelRuntimeControlResult{}, fmt.Errorf("channel not found")
	}
	if strings.TrimSpace(reason) == "" {
		reason = "operator forced runtime isolation"
	}

	channelHealth.Lock()
	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	state := getOrCreateChannelHealthLocked(channelHealthScopeFor(channelID, "", setting))
	clearChannelID, shouldClearAffinity := openChannelLocked(state, now, setting, reason)
	if duration > 0 {
		state.nextProbeAt = now.Add(duration)
		persistChannelHealthIsolationLocked(state, now, setting)
	}
	snapshot := buildChannelHealthSnapshotLocked(state, now, setting)
	channelHealth.Unlock()

	deleted := 0
	if shouldClearAffinity {
		deleted = ClearChannelAffinityByChannelID(clearChannelID)
	}
	return ChannelRuntimeControlResult{
		ChannelID:       channelID,
		AffinityDeleted: deleted,
		Snapshot:        snapshot,
	}, nil
}

func ClearChannelRuntimeIsolation(channelID int) (ChannelRuntimeControlResult, error) {
	if channelID <= 0 {
		return ChannelRuntimeControlResult{}, fmt.Errorf("invalid channel_id")
	}
	if !channelHealthEnabled() {
		return ChannelRuntimeControlResult{}, fmt.Errorf("channel health guard is disabled")
	}
	channel, err := model.CacheGetChannel(channelID)
	if err != nil || channel == nil {
		return ChannelRuntimeControlResult{}, fmt.Errorf("channel not found")
	}
	if channel.Status != common.ChannelStatusEnabled {
		return ChannelRuntimeControlResult{}, fmt.Errorf("channel database status is not enabled")
	}

	channelHealth.Lock()
	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	state := getOrCreateChannelHealthLocked(channelHealthScopeFor(channelID, "", setting))
	markChannelHealthyLocked(state)
	deleteChannelHealthIsolation(channelHealthScopeFor(channelID, "", setting))
	snapshot := buildChannelHealthSnapshotLocked(state, now, setting)
	channelHealth.Unlock()

	return ChannelRuntimeControlResult{
		ChannelID: channelID,
		Snapshot:  snapshot,
	}, nil
}

func ForceChannelRuntimeProbeNow(channelID int) (ChannelRuntimeControlResult, error) {
	if channelID <= 0 {
		return ChannelRuntimeControlResult{}, fmt.Errorf("invalid channel_id")
	}
	if !channelHealthEnabled() {
		return ChannelRuntimeControlResult{}, fmt.Errorf("channel health guard is disabled")
	}
	channel, err := model.CacheGetChannel(channelID)
	if err != nil || channel == nil {
		return ChannelRuntimeControlResult{}, fmt.Errorf("channel not found")
	}
	if channel.Status != common.ChannelStatusEnabled {
		return ChannelRuntimeControlResult{}, fmt.Errorf("channel database status is not enabled")
	}

	channelHealth.Lock()
	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	state := getOrCreateChannelHealthLocked(channelHealthScopeFor(channelID, "", setting))
	if state.state == ChannelHealthStateHealthy {
		state.state = ChannelHealthStateOpen
		state.reason = "operator requested probe"
		state.openedAt = now
	}
	state.nextProbeAt = now
	state.probeInProgress = false
	persistChannelHealthIsolationLocked(state, now, setting)
	snapshot := buildChannelHealthSnapshotLocked(state, now, setting)
	channelHealth.Unlock()

	return ChannelRuntimeControlResult{
		ChannelID: channelID,
		Snapshot:  snapshot,
	}, nil
}

func MarkChannelProbing(channelID int) bool {
	return MarkChannelProbingForModel(channelID, "")
}

func MarkChannelProbingForModel(channelID int, modelName string) bool {
	if channelID <= 0 || !channelHealthEnabled() {
		return false
	}

	channelHealth.Lock()
	defer channelHealth.Unlock()

	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	state := getOrCreateChannelHealthLocked(channelHealthScopeFor(channelID, modelName, setting))
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
	state.state = ChannelHealthStateProbing
	state.probeInProgress = true
	persistChannelHealthIsolationLocked(state, now, setting)
	return true
}

func openChannelLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting, reason string) (int, bool) {
	if state == nil {
		return 0, false
	}
	wasAvailable := state.state == ChannelHealthStateHealthy || state.state == ChannelHealthStateWarming
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
	if wasAvailable {
		recordChannelHealthEventLocked(setting, ChannelHealthEventTypeOpened, state, reason, now)
	}
	return state.channelID, wasAvailable
}

func RecordProbeResult(channelID int, success bool, reason string) {
	RecordProbeResultForModel(channelID, "", success, reason)
}

func RecordProbeResultForModel(channelID int, modelName string, success bool, reason string) {
	if channelID <= 0 || !channelHealthEnabled() {
		return
	}

	channelHealth.Lock()
	defer channelHealth.Unlock()

	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	state := getOrCreateChannelHealthLocked(channelHealthScopeFor(channelID, modelName, setting))
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
				recordChannelHealthEventLocked(setting, ChannelHealthEventTypeRecovered, state, "probe recovered", now)
			} else {
				markChannelHealthyLocked(state)
				deleteChannelHealthIsolation(channelHealthScope{channelID: state.channelID, modelName: state.modelName})
				recordChannelHealthEventLocked(setting, ChannelHealthEventTypeRecovered, state, "probe recovered", now)
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
	recordChannelHealthEventLocked(setting, ChannelHealthEventTypeProbeFailed, state, reason, now)
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

func recordChannelHealthEventLocked(setting operation_setting.ChannelHealthSetting, eventType string, state *channelHealthStateData, reason string, now time.Time) {
	if state == nil || !setting.EventsEnabled {
		return
	}
	if channelHealth.lastAlertAt == nil {
		channelHealth.lastAlertAt = make(map[string]time.Time)
	}
	event := ChannelHealthEvent{
		Type:       eventType,
		ChannelID:  state.channelID,
		ModelName:  state.modelName,
		Group:      state.group,
		State:      string(state.state),
		Reason:     reason,
		OccurredAt: now.Unix(),
	}
	alertKey := fmt.Sprintf("%s:%s", eventType, channelHealthScopeKey(channelHealthScope{channelID: state.channelID, modelName: state.modelName}))
	minInterval := time.Duration(setting.AlertMinIntervalSeconds) * time.Second
	if minInterval <= 0 {
		minInterval = 60 * time.Second
	}
	if last, ok := channelHealth.lastAlertAt[alertKey]; !ok || now.Sub(last) >= minInterval {
		channelHealth.lastAlertAt[alertKey] = now
		event.AlertSent = true
		event.AlertSubject = channelHealthAlertSubject(event)
	}
	channelHealth.events = append(channelHealth.events, event)
	if len(channelHealth.events) > 1000 {
		channelHealth.events = append([]ChannelHealthEvent(nil), channelHealth.events[len(channelHealth.events)-1000:]...)
	}
	if event.AlertSent {
		notify := channelHealth.notifyFunc
		go notifyChannelHealthEvent(event, notify)
	}
}

func notifyChannelHealthEvent(event ChannelHealthEvent, notify func(event ChannelHealthEvent)) {
	if notify != nil {
		notify(event)
		return
	}
	if model.DB == nil {
		return
	}
	NotifyRootUser(formatNotifyType(event.ChannelID, common.ChannelStatusAutoDisabled), event.AlertSubject, channelHealthAlertContent(event))
}

func channelHealthAlertSubject(event ChannelHealthEvent) string {
	modelPart := ""
	if event.ModelName != "" {
		modelPart = fmt.Sprintf(" model %s", event.ModelName)
	}
	switch event.Type {
	case ChannelHealthEventTypeOpened:
		return fmt.Sprintf("Channel #%d%s runtime isolated", event.ChannelID, modelPart)
	case ChannelHealthEventTypeRecovered:
		return fmt.Sprintf("Channel #%d%s runtime recovered", event.ChannelID, modelPart)
	case ChannelHealthEventTypeProbeFailed:
		return fmt.Sprintf("Channel #%d%s probe failed", event.ChannelID, modelPart)
	default:
		return fmt.Sprintf("Channel #%d%s health event", event.ChannelID, modelPart)
	}
}

func channelHealthAlertContent(event ChannelHealthEvent) string {
	if event.Reason == "" {
		return event.AlertSubject
	}
	return fmt.Sprintf("%s: %s", event.AlertSubject, event.Reason)
}

func GetChannelHealthEvents(filter ChannelHealthEventFilter) []ChannelHealthEvent {
	channelHealth.Lock()
	defer channelHealth.Unlock()

	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	events := make([]ChannelHealthEvent, 0, len(channelHealth.events))
	for i := len(channelHealth.events) - 1; i >= 0 && len(events) < limit; i-- {
		event := channelHealth.events[i]
		if filter.ChannelID > 0 && event.ChannelID != filter.ChannelID {
			continue
		}
		if filter.ModelName != "" && event.ModelName != filter.ModelName {
			continue
		}
		if filter.Group != "" && event.Group != filter.Group {
			continue
		}
		if filter.Type != "" && event.Type != filter.Type {
			continue
		}
		if filter.State != "" && event.State != filter.State {
			continue
		}
		events = append(events, event)
	}
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return events
}

func GetChannelHealthReport(filter ChannelHealthEventFilter) ChannelHealthReport {
	events := GetChannelHealthEvents(filter)
	counts := make(map[string]ChannelHealthChannelCount)
	report := ChannelHealthReport{
		AverageFirstResponseMs: averageFirstResponseMsForFilter(filter),
		Events:                 events,
	}
	for _, event := range events {
		switch event.Type {
		case ChannelHealthEventTypeOpened:
			report.IsolationCount++
			key := channelHealthScopeKey(channelHealthScope{channelID: event.ChannelID, modelName: event.ModelName}) + ":" + event.Group
			count := counts[key]
			count.ChannelID = event.ChannelID
			count.ModelName = event.ModelName
			count.Group = event.Group
			count.Count++
			counts[key] = count
		case ChannelHealthEventTypeRecovered:
			report.RecoveryCount++
		case ChannelHealthEventTypeProbeFailed:
			report.ProbeFailureCount++
		}
	}
	report.TopFailingChannels = make([]ChannelHealthChannelCount, 0, len(counts))
	for _, count := range counts {
		report.TopFailingChannels = append(report.TopFailingChannels, count)
	}
	sort.Slice(report.TopFailingChannels, func(i, j int) bool {
		if report.TopFailingChannels[i].Count == report.TopFailingChannels[j].Count {
			return report.TopFailingChannels[i].ChannelID < report.TopFailingChannels[j].ChannelID
		}
		return report.TopFailingChannels[i].Count > report.TopFailingChannels[j].Count
	})
	if len(report.TopFailingChannels) > 10 {
		report.TopFailingChannels = report.TopFailingChannels[:10]
	}
	return report
}

func averageFirstResponseMsForFilter(filter ChannelHealthEventFilter) float64 {
	channelHealth.Lock()
	defer channelHealth.Unlock()

	var total time.Duration
	count := 0
	for _, state := range channelHealth.channels {
		if state == nil || state.firstResponseCount <= 0 {
			continue
		}
		if filter.ChannelID > 0 && state.channelID != filter.ChannelID {
			continue
		}
		if filter.ModelName != "" && state.modelName != filter.ModelName {
			continue
		}
		if filter.Group != "" && state.group != filter.Group {
			continue
		}
		if filter.State != "" && string(state.state) != filter.State {
			continue
		}
		total += state.firstResponseTotal
		count += state.firstResponseCount
	}
	if count == 0 {
		return 0
	}
	return float64(total.Microseconds()) / 1000.0 / float64(count)
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
		modelName string
		probeFn   ChannelHealthProbeFunc
	}
	targets := make([]probeTarget, 0)

	channelHealth.Lock()
	for _, state := range channelHealth.channels {
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
		if !tryAcquireChannelHealthProbeLock(state.channelID, setting) {
			continue
		}
		state.probeInProgress = true
		targets = append(targets, probeTarget{channelID: state.channelID, modelName: state.modelName, probeFn: channelHealth.probeFunc})
	}
	channelHealth.Unlock()

	for _, target := range targets {
		go runChannelHealthProbe(target.channelID, target.modelName, target.probeFn, setting)
	}
}

func runChannelHealthProbe(channelID int, modelName string, probeFn ChannelHealthProbeFunc, setting operation_setting.ChannelHealthSetting) {
	defer releaseChannelHealthProbeLock(channelID)

	channel, err := model.CacheGetChannel(channelID)
	if err != nil || channel == nil {
		RecordProbeResultForModel(channelID, modelName, false, fmt.Sprintf("probe load channel failed: %v", err))
		return
	}
	if channel.Status != common.ChannelStatusEnabled {
		RecordProbeResultForModel(channelID, modelName, false, fmt.Sprintf("probe skipped: channel status %d", channel.Status))
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
		RecordProbeResultForModel(channelID, modelName, false, err.Error())
		return
	}
	RecordProbeResultForModel(channelID, modelName, true, "")
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
	return GetChannelHealthSnapshotForModel(channelID, "")
}

func GetChannelHealthSnapshotForModel(channelID int, modelName string) (ChannelHealthSnapshot, bool) {
	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	scope := channelHealthScopeFor(channelID, modelName, setting)
	if snapshot, found := getChannelHealthIsolationSnapshot(scope, now); found {
		return snapshot, true
	}

	channelHealth.Lock()
	defer channelHealth.Unlock()

	state, ok := channelHealth.channels[channelHealthScopeKey(scope)]
	if !ok {
		return ChannelHealthSnapshot{}, false
	}
	return buildChannelHealthSnapshotLocked(state, now, setting), true
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
		deleteChannelHealthIsolation(channelHealthScope{channelID: state.channelID, modelName: state.modelName})
	}
	samples, failures := channelHealthWindowCountsLocked(state, now, setting)
	errorRate := 0.0
	if samples > 0 {
		errorRate = float64(failures) / float64(samples)
	}
	averageFirstResponseMs := 0.0
	if state.firstResponseCount > 0 {
		averageFirstResponseMs = float64(state.firstResponseTotal.Microseconds()) / 1000.0 / float64(state.firstResponseCount)
	}
	warmupPercent := channelWarmupPercentLocked(state, now, setting)
	return ChannelHealthSnapshot{
		ChannelID:              state.channelID,
		ModelName:              state.modelName,
		State:                  state.state,
		Reason:                 state.reason,
		OpenedAt:               unixOrZero(state.openedAt),
		NextProbeAt:            unixOrZero(state.nextProbeAt),
		ProbeInProgress:        state.probeInProgress,
		ConsecutiveFailure:     state.consecutiveFailure,
		ProbeSuccesses:         state.probeSuccesses,
		ProbeFailures:          state.probeFailures,
		Inflight:               len(state.inflight),
		WindowSamples:          samples,
		WindowFailures:         failures,
		ErrorRate:              errorRate,
		AverageFirstResponseMs: averageFirstResponseMs,
		WarmupStartedAt:        unixOrZero(state.warmupStartedAt),
		WarmupEndsAt:           unixOrZero(state.warmupEndsAt),
		WarmupPercent:          warmupPercent,
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
		deleteChannelHealthIsolation(channelHealthScope{channelID: snapshot.ChannelID, modelName: snapshot.ModelName})
		return
	}
	scope := channelHealthScope{channelID: snapshot.ChannelID, modelName: snapshot.ModelName}
	if err := getChannelHealthIsolationCache().SetWithTTL(channelHealthCacheKey(scope), snapshot, channelHealthIsolationTTL(setting)); err != nil {
		common.SysError(fmt.Sprintf("channel health isolation cache set failed: channel_id=%d, model=%s, err=%v", snapshot.ChannelID, snapshot.ModelName, err))
	}
}

func deleteChannelHealthIsolation(scope channelHealthScope) {
	if scope.channelID <= 0 {
		return
	}
	if _, err := getChannelHealthIsolationCache().DeleteMany([]string{channelHealthCacheKey(scope)}); err != nil {
		common.SysError(fmt.Sprintf("channel health isolation cache delete failed: channel_id=%d, model=%s, err=%v", scope.channelID, scope.modelName, err))
	}
}

func channelHealthCacheKey(scope channelHealthScope) string {
	return channelHealthScopeKey(scope)
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}
