package service

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	channelHealthDegradedTrafficPercent  = 30
)

// ChannelHealthStateV2 represents the simplified 3-state health model
type ChannelHealthStateV2 string

const (
	ChannelHealthStateV2Healthy     ChannelHealthStateV2 = "healthy"
	ChannelHealthStateV2Degraded    ChannelHealthStateV2 = "degraded"
	ChannelHealthStateV2Unavailable ChannelHealthStateV2 = "unavailable"
)

// translateStateToV2 converts the internal runtime state to the simplified
// three-state API exposed to operators.
func translateStateToV2(state ChannelHealthState, degraded bool) ChannelHealthStateV2 {
	if degraded && state == ChannelHealthStateHealthy {
		return ChannelHealthStateV2Degraded
	}
	switch state {
	case ChannelHealthStateHealthy:
		return ChannelHealthStateV2Healthy
	case ChannelHealthStateWarming:
		return ChannelHealthStateV2Degraded
	case ChannelHealthStateOpen, ChannelHealthStateProbing:
		return ChannelHealthStateV2Unavailable
	default:
		return ChannelHealthStateV2Healthy
	}
}

const (
	ChannelHealthEventTypeOpened      = "opened"
	ChannelHealthEventTypeProbing     = "probing"
	ChannelHealthEventTypeWarming     = "warming"
	ChannelHealthEventTypeDegraded    = "degraded"
	ChannelHealthEventTypeRestored    = "degradation_recovered"
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

type ChannelHealthProbeFunc func(ctx context.Context, channel *model.Channel, modelName string) error

type ChannelRuntimeControlResult struct {
	ChannelID       int                   `json:"channel_id"`
	AffinityDeleted int                   `json:"affinity_deleted"`
	Snapshot        ChannelHealthSnapshot `json:"snapshot"`
}

type ChannelHealthEvent struct {
	Type         string                     `json:"type"`
	ChannelID    int                        `json:"channel_id"`
	ModelName    string                     `json:"model_name,omitempty"`
	Group        string                     `json:"group,omitempty"`
	State        string                     `json:"state"`
	StateV2      string                     `json:"state_v2"`
	Reason       string                     `json:"reason,omitempty"`
	OccurredAt   int64                      `json:"occurred_at"`
	Snapshot     ChannelHealthEventSnapshot `json:"snapshot,omitempty"`
	AlertSent    bool                       `json:"alert_sent"`
	AlertSubject string                     `json:"alert_subject,omitempty"`
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
	IsolationCount         int                            `json:"isolation_count"`
	RecoveryCount          int                            `json:"recovery_count"`
	ProbeFailureCount      int                            `json:"probe_failure_count"`
	AverageFirstResponseMs float64                        `json:"average_first_response_ms"`
	TopFailingChannels     []ChannelHealthChannelCount    `json:"top_failing_channels"`
	Events                 []ChannelHealthEvent           `json:"events"`
	SelectionSummary       []ChannelSelectionTraceSummary `json:"selection_summary"`
}

type ChannelHealthEventSnapshot struct {
	ActiveInflight         int     `json:"active_inflight"`
	StuckInflight          int     `json:"stuck_inflight"`
	WindowSamples          int     `json:"window_samples"`
	WindowFailures         int     `json:"window_failures"`
	ErrorRate              float64 `json:"error_rate"`
	AverageFirstResponseMs float64 `json:"average_first_response_ms"`
	P95FirstResponseMs     float64 `json:"p95_first_response_ms"`
	ProbeBackoffSeconds    int     `json:"probe_backoff_seconds"`
	NextProbeAt            int64   `json:"next_probe_at"`
	ProbeInProgress        bool    `json:"probe_in_progress"`
	WarmupPercent          int     `json:"warmup_percent"`
	WarmupThrottlePercent  int     `json:"warmup_throttle_percent,omitempty"`
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
	ChannelID              int                  `json:"channel_id"`
	ModelName              string               `json:"model_name,omitempty"`
	State                  ChannelHealthState   `json:"state"`
	StateV2                ChannelHealthStateV2 `json:"state_v2"`
	TrafficPercent         int                  `json:"traffic_percent"`
	Reason                 string               `json:"reason"`
	OpenedAt               int64                `json:"opened_at"`
	NextProbeAt            int64                `json:"next_probe_at"`
	ProbeInProgress        bool                 `json:"probe_in_progress"`
	ConsecutiveFailure     int                  `json:"consecutive_failure"`
	ProbeSuccesses         int                  `json:"probe_successes"`
	ProbeFailures          int                  `json:"probe_failures"`
	Inflight               int                  `json:"inflight"`
	WindowSamples          int                  `json:"window_samples"`
	WindowFailures         int                  `json:"window_failures"`
	ErrorRate              float64              `json:"error_rate"`
	AverageFirstResponseMs float64              `json:"average_first_response_ms"`
	P95FirstResponseMs     float64              `json:"p95_first_response_ms"`
	RuntimeAvailable       bool                 `json:"runtime_available"`
	AvailabilityReason     string               `json:"availability_reason,omitempty"`
	ProbeAvailable         bool                 `json:"probe_available"`
	ProbeUnavailableReason string               `json:"probe_unavailable_reason,omitempty"`
	WarmupStartedAt        int64                `json:"warmup_started_at"`
	WarmupEndsAt           int64                `json:"warmup_ends_at"`
	WarmupPercent          int                  `json:"warmup_percent"`
	WarmupThrottlePercent  int                  `json:"warmup_throttle_percent,omitempty"`
}

type channelAttemptState struct {
	meta              ChannelAttemptMeta
	startedAt         time.Time
	firstResponseSeen bool
	firstResponse     time.Duration
	stuck             bool
	cancelled         bool
}

type channelHealthSample struct {
	at            time.Time
	failed        bool
	reason        string
	status        int
	errCode       string
	firstResponse time.Duration
}

type channelHealthStateData struct {
	channelID             int
	modelName             string
	group                 string
	state                 ChannelHealthState
	reason                string
	openedAt              time.Time
	nextProbeAt           time.Time
	probeInProgress       bool
	consecutiveFailure    int
	probeSuccesses        int
	probeFailures         int
	probeBackoff          time.Duration
	warmupStartedAt       time.Time
	warmupEndsAt          time.Time
	warmupThrottlePercent int
	degraded              bool
	firstResponseTotal    time.Duration
	firstResponseCount    int
	inflight              map[int64]*channelAttemptState
	samples               []channelHealthSample
}

// channelHealthShardCount must be a power of two so that channelHealthShardFor
// can map a channelID to a shard with a cheap bitmask instead of a modulo.
const channelHealthShardCount = 32

// channelHealthPendingOp is a single isolation-cache mutation collected while a
// shard lock is held and flushed to Redis only after the lock is released, so a
// slow Redis round-trip never stalls the shard's hot path.
type channelHealthPendingOp struct {
	scope    channelHealthScope
	snapshot ChannelHealthSnapshot
	ttl      time.Duration
	isDelete bool
}

// channelHealthShard owns the runtime health state for every channel whose ID
// hashes to it. All per-channel mutation happens under the shard's Mutex; the
// Redis isolation writes those mutations imply are queued on pending and, once
// the lock is released, appended to flushQueue and drained in FIFO order by a
// single drainer goroutine.
//
// The single-drainer FIFO is what keeps Redis consistent with memory. Because
// batches are enqueued while the shard lock is held (in unlockAndFlush) and a
// lone drainer replays them strictly in enqueue order, the last isolation write
// to reach Redis for a scope always corresponds to the last in-memory
// transition for that scope. A naive "flush after unlock" (which this replaced)
// let two goroutines that transitioned the same channel under the lock reorder
// their Set/Delete on the way to Redis, so a stale Open could overwrite a newer
// Healthy delete and re-isolate a recovered channel.
type channelHealthShard struct {
	sync.Mutex
	channels map[string]*channelHealthStateData
	// pending accumulates isolation ops for the current lock hold; it is moved
	// onto flushQueue as one batch by unlockAndFlush.
	pending []channelHealthPendingOp
	// flushQueue holds batches awaiting Redis I/O, oldest first. flushing is
	// true iff a drainer goroutine is actively replaying the queue. Both are
	// guarded by the shard Mutex.
	flushQueue [][]channelHealthPendingOp
	flushing   bool
}

// queueIsolationPersist records that scope's isolation snapshot must be written
// to Redis. Callers must hold the shard lock; the actual SetWithTTL happens in
// unlockAndFlush.
func (s *channelHealthShard) queueIsolationPersist(scope channelHealthScope, snapshot ChannelHealthSnapshot, ttl time.Duration) {
	s.pending = append(s.pending, channelHealthPendingOp{scope: scope, snapshot: snapshot, ttl: ttl})
}

// queueIsolationDelete records that scope's isolation entry must be removed from
// Redis. Callers must hold the shard lock; the actual DeleteMany happens in
// unlockAndFlush.
func (s *channelHealthShard) queueIsolationDelete(scope channelHealthScope) {
	s.pending = append(s.pending, channelHealthPendingOp{scope: scope, isDelete: true})
}

// unlockAndFlush appends the ops collected during the current lock hold as one
// batch, then releases the shard lock and drains the flush queue if no other
// goroutine is already doing so. All Redis I/O happens after the lock is
// released (so a slow round-trip never stalls the shard's hot path), yet a
// single drainer replays batches in strict enqueue order, so the last
// isolation write to reach Redis for a scope matches the last in-memory
// transition for that scope.
func (s *channelHealthShard) unlockAndFlush() {
	batch := s.pending
	s.pending = nil
	if len(batch) == 0 {
		// No isolation I/O to do; if a drainer is already running it owns the
		// queue, otherwise there is nothing to drain.
		s.Unlock()
		return
	}
	s.flushQueue = append(s.flushQueue, batch)
	if s.flushing {
		// A drainer is active; it will pick up this batch in order. Handing off
		// keeps ordering intact and lets this goroutine return without blocking
		// on Redis.
		s.Unlock()
		return
	}
	s.flushing = true
	s.Unlock()
	s.drainFlushQueue()
}

// drainFlushQueue replays queued isolation batches to the cache in FIFO order.
// Exactly one goroutine per shard runs this at a time (guarded by s.flushing).
// It pops one batch under the lock, performs that batch's I/O with the lock
// released, then loops; batches enqueued meanwhile are drained by the same
// goroutine, preserving global order for the shard.
func (s *channelHealthShard) drainFlushQueue() {
	cache := getChannelHealthIsolationCache()
	for {
		s.Lock()
		if len(s.flushQueue) == 0 {
			s.flushing = false
			s.Unlock()
			return
		}
		batch := s.flushQueue[0]
		s.flushQueue[0] = nil
		s.flushQueue = s.flushQueue[1:]
		if len(s.flushQueue) == 0 {
			s.flushQueue = nil
		}
		s.Unlock()

		for _, op := range batch {
			if op.isDelete {
				if _, err := cache.DeleteMany([]string{channelHealthCacheKey(op.scope)}); err != nil {
					common.SysError(fmt.Sprintf("channel health isolation cache delete failed: channel_id=%d, model=%s, err=%v", op.scope.channelID, op.scope.modelName, err))
				}
				continue
			}
			if err := cache.SetWithTTL(channelHealthCacheKey(op.scope), op.snapshot, op.ttl); err != nil {
				common.SysError(fmt.Sprintf("channel health isolation cache set failed: channel_id=%d, model=%s, err=%v", op.scope.channelID, op.scope.modelName, err))
			}
		}
	}
}

var channelHealthShards [channelHealthShardCount]channelHealthShard

// channelHealthShardFor returns the shard that owns channelID. Sharding is keyed
// on channelID (not the full scope key) so that every model-level scope of one
// channel lands on the same shard, letting the runtime-control operations mutate
// all of a channel's scopes atomically under a single lock.
func channelHealthShardFor(channelID int) *channelHealthShard {
	return &channelHealthShards[uint(channelID)&(channelHealthShardCount-1)]
}

// nextChannelHealthAttemptID is a process-wide monotonic counter for attempt
// handles. It is independent of any channel state, so an atomic keeps it off the
// shard locks entirely.
var nextChannelHealthAttemptID int64

// channelHealthNowFunc / channelHealthProbeFuncPtr / channelHealthNotifyFuncPtr
// are read on hot paths and written only at startup or in tests, so they live in
// atomics rather than behind a lock.
var (
	channelHealthNowFunc       atomic.Pointer[func() time.Time]
	channelHealthProbeFuncPtr  atomic.Pointer[ChannelHealthProbeFunc]
	channelHealthNotifyFuncPtr atomic.Pointer[func(event ChannelHealthEvent)]
)

// channelHealthEventLog holds the cross-channel event ring buffer and alert
// dedup map. It has its own lock; when a state transition under a shard lock
// records an event, the lock order is always shard -> eventLog (never the
// reverse), which is why the event-reading paths take eventLog alone.
var channelHealthEventLog = struct {
	sync.Mutex
	events      []ChannelHealthEvent
	lastAlertAt map[string]time.Time
}{
	lastAlertAt: make(map[string]time.Time),
}

// channelHealthCache holds the lazily-initialized isolation cache singleton.
var channelHealthCache = struct {
	once  sync.Once
	cache *cachex.HybridCache[ChannelHealthSnapshot]
}{}

var channelHealthProbeWorkerOnce sync.Once
var channelHealthProbeWaitGroup sync.WaitGroup

// loadChannelHealthProbeFunc returns the registered probe callback, or nil if
// none has been set. It reads the atomic so callers never touch a shard lock.
func loadChannelHealthProbeFunc() ChannelHealthProbeFunc {
	if fn := channelHealthProbeFuncPtr.Load(); fn != nil {
		return *fn
	}
	return nil
}

// loadChannelHealthNotifyFunc returns the registered notify callback, or nil if
// none has been set (production then falls back to NotifyRootUser).
func loadChannelHealthNotifyFunc() func(event ChannelHealthEvent) {
	if fn := channelHealthNotifyFuncPtr.Load(); fn != nil {
		return *fn
	}
	return nil
}

func init() {
	for i := range channelHealthShards {
		channelHealthShards[i].channels = make(map[string]*channelHealthStateData)
	}
	defaultNow := time.Now
	channelHealthNowFunc.Store(&defaultNow)

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
	model.SetChannelRuntimeWeightFunc(func(channelID int, modelName string, weight int, inflight int) int {
		return adjustChannelHealthWeight(channelID, modelName, weight, inflight)
	})
	relaycommon.MarkChannelHealthFirstResponseFunc = MarkChannelHealthFirstResponse
}

func getChannelHealthIsolationCache() *cachex.HybridCache[ChannelHealthSnapshot] {
	channelHealthCache.once.Do(func() {
		channelHealthCache.cache = cachex.NewHybridCache[ChannelHealthSnapshot](cachex.HybridCacheConfig[ChannelHealthSnapshot]{
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
	return channelHealthCache.cache
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
	if normalized.DegradationThreshold <= 0 || normalized.DegradationThreshold >= normalized.ErrorRateThreshold {
		normalized.DegradationThreshold = normalized.ErrorRateThreshold / 2
	}
	if normalized.ConsecutiveFailureThreshold <= 0 {
		normalized.ConsecutiveFailureThreshold = 5
	}
	if normalized.FirstResponseTimeoutSeconds <= 0 {
		normalized.FirstResponseTimeoutSeconds = 45
	}
	if normalized.SlowFirstResponseSeconds <= 0 {
		normalized.SlowFirstResponseSeconds = 18
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
	if normalized.MaxIsolationSeconds <= 0 {
		normalized.MaxIsolationSeconds = 1800
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
	if fn := channelHealthNowFunc.Load(); fn != nil {
		return (*fn)()
	}
	return time.Now()
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

func channelRuntimeControlScopes(channel *model.Channel, setting operation_setting.ChannelHealthSetting) []channelHealthScope {
	if channel == nil || channel.Id <= 0 {
		return nil
	}
	scopes := make([]channelHealthScope, 0, 1)
	seen := make(map[string]struct{})
	appendScope := func(scope channelHealthScope) {
		if scope.channelID <= 0 {
			return
		}
		key := channelHealthScopeKey(scope)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		scopes = append(scopes, scope)
	}
	appendScope(channelHealthScope{channelID: channel.Id})
	if !setting.ModelLevelEnabled {
		return scopes
	}
	for _, modelName := range channelRuntimeControlModelNames(channel) {
		appendScope(channelHealthScopeFor(channel.Id, modelName, setting))
	}
	return scopes
}

func channelRuntimeControlModelNames(channel *model.Channel) []string {
	if channel == nil || channel.Id <= 0 {
		return nil
	}
	seen := make(map[string]struct{})
	addModelName := func(modelName string) {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			return
		}
		seen[modelName] = struct{}{}
	}
	if model.DB != nil {
		var abilityModels []string
		if err := model.DB.Model(&model.Ability{}).
			Where("channel_id = ? and enabled = ?", channel.Id, true).
			Distinct("model").
			Pluck("model", &abilityModels).Error; err != nil {
			common.SysError(fmt.Sprintf("load channel runtime control models failed: channel_id=%d, err=%v", channel.Id, err))
		}
		for _, modelName := range abilityModels {
			addModelName(modelName)
		}
	}
	for _, modelName := range channel.GetModels() {
		addModelName(modelName)
	}
	modelNames := make([]string, 0, len(seen))
	for modelName := range seen {
		modelNames = append(modelNames, modelName)
	}
	sort.Strings(modelNames)
	return modelNames
}

// getOrCreateChannelHealthLocked looks up (or creates) the state for scope.
// The caller must hold shard's lock, and shard must be the one that owns
// scope.channelID (channelHealthShardFor(scope.channelID)).
func getOrCreateChannelHealthLocked(shard *channelHealthShard, scope channelHealthScope) *channelHealthStateData {
	if shard.channels == nil {
		shard.channels = make(map[string]*channelHealthStateData)
	}
	key := channelHealthScopeKey(scope)
	state, ok := shard.channels[key]
	if ok {
		return state
	}
	state = &channelHealthStateData{
		channelID: scope.channelID,
		modelName: scope.modelName,
		state:     ChannelHealthStateHealthy,
		inflight:  make(map[int64]*channelAttemptState),
	}
	shard.channels[key] = state
	return state
}

func ResetChannelHealthForTest() {
	for i := range channelHealthShards {
		shard := &channelHealthShards[i]
		shard.Lock()
		shard.channels = make(map[string]*channelHealthStateData)
		shard.pending = nil
		shard.flushQueue = nil
		shard.flushing = false
		shard.Unlock()
	}

	atomic.StoreInt64(&nextChannelHealthAttemptID, 0)
	defaultNow := time.Now
	channelHealthNowFunc.Store(&defaultNow)
	channelHealthProbeFuncPtr.Store(nil)
	channelHealthNotifyFuncPtr.Store(nil)

	channelHealthEventLog.Lock()
	channelHealthEventLog.events = nil
	channelHealthEventLog.lastAlertAt = make(map[string]time.Time)
	channelHealthEventLog.Unlock()

	// Purge the isolation cache contents so snapshots do not leak between
	// tests (the pre-refactor reset cleared cacheOnce/cache for the same
	// reason). Purging through the stable singleton — rather than swapping the
	// cache pointer/sync.Once — avoids racing the unlocked read in
	// getChannelHealthIsolationCache from any background probe goroutine a prior
	// test may have left running.
	if err := getChannelHealthIsolationCache().Purge(); err != nil {
		common.SysError(fmt.Sprintf("channel health isolation cache purge failed: %v", err))
	}

	ResetChannelSelectionTraceSummaryForTest()
}

// clearChannelHealthInMemoryForTest drops every in-memory shard state without
// touching the isolation cache, so tests can verify the cache-backed recovery
// path after the local state has been evicted.
func clearChannelHealthInMemoryForTest() {
	for i := range channelHealthShards {
		shard := &channelHealthShards[i]
		shard.Lock()
		shard.channels = make(map[string]*channelHealthStateData)
		shard.pending = nil
		shard.Unlock()
	}
}

// channelHealthStateForTest returns the in-memory state for a channel scope, or
// nil when none exists. It exists so tests can assert on internal state without
// reaching into shard internals directly.
func channelHealthStateForTest(scope channelHealthScope) *channelHealthStateData {
	shard := channelHealthShardFor(scope.channelID)
	shard.Lock()
	defer shard.Unlock()
	return shard.channels[channelHealthScopeKey(scope)]
}

func SetChannelHealthNowFuncForTest(now func() time.Time) {
	if now == nil {
		defaultNow := time.Now
		channelHealthNowFunc.Store(&defaultNow)
		return
	}
	channelHealthNowFunc.Store(&now)
}

func SetChannelHealthProbeFunc(fn ChannelHealthProbeFunc) {
	if fn == nil {
		channelHealthProbeFuncPtr.Store(nil)
		return
	}
	channelHealthProbeFuncPtr.Store(&fn)
}

func SetChannelHealthEventNotifyFuncForTest(fn func(event ChannelHealthEvent)) {
	if fn == nil {
		channelHealthNotifyFuncPtr.Store(nil)
		return
	}
	channelHealthNotifyFuncPtr.Store(&fn)
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
	shard := channelHealthShardFor(channelID)
	if snapshot, found := getChannelHealthIsolationSnapshot(scope, now); found {
		if snapshot.State == ChannelHealthStateHealthy {
			shard.Lock()
			if state, ok := shard.channels[channelHealthScopeKey(scope)]; ok && state.state == ChannelHealthStateWarming && isChannelWarmupCompleteLocked(state, now) {
				completeChannelWarmupLocked(state, now, setting, "warmup complete")
			}
			shard.unlockAndFlush()
			return true
		}
		if shouldHydrateChannelHealthSnapshotForProbe(snapshot, now, setting) {
			shard.Lock()
			state := hydrateChannelHealthStateFromSnapshotLocked(shard, scope, snapshot, now, setting)
			persistChannelHealthIsolationLocked(state, now, setting)
			shard.unlockAndFlush()
		}
		return isChannelHealthSnapshotAvailable(snapshot, now)
	}

	shard.Lock()
	state, ok := shard.channels[channelHealthScopeKey(scope)]
	if ok {
		available := isChannelAvailableLocked(state, now, setting)
		shard.unlockAndFlush()
		return available
	}
	shard.unlockAndFlush()

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
	shard := channelHealthShardFor(channelID)
	shard.Lock()
	state, ok := shard.channels[channelHealthScopeKey(scope)]
	if ok {
		available := isChannelProbeAvailableLocked(state, now)
		shard.unlockAndFlush()
		return available
	}
	shard.unlockAndFlush()

	snapshot, found := getChannelHealthIsolationSnapshot(scope, now)
	if !found {
		return true
	}
	if snapshot.State == ChannelHealthStateHealthy {
		return true
	}
	if !shouldHydrateChannelHealthSnapshotForProbe(snapshot, now, setting) {
		return false
	}
	shard.Lock()
	state = hydrateChannelHealthStateFromSnapshotLocked(shard, scope, snapshot, now, setting)
	available := isChannelProbeAvailableLocked(state, now)
	persistChannelHealthIsolationLocked(state, now, setting)
	shard.unlockAndFlush()
	return available
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
			completeChannelWarmupLocked(state, now, setting, "warmup complete")
			return true
		}
		percent := channelWarmupPercentWithOptionsLocked(state, now, setting, false)
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
			snapshot.StateV2 = ChannelHealthStateV2Healthy
			snapshot.TrafficPercent = 100
			deleteChannelHealthIsolationDirect(scope)
		}
	}
	switch snapshot.State {
	case ChannelHealthStateHealthy:
		if snapshot.StateV2 == ChannelHealthStateV2Degraded {
			if snapshot.TrafficPercent <= 0 {
				snapshot.TrafficPercent = channelHealthDegradedTrafficPercent
			}
		} else {
			snapshot.StateV2 = ChannelHealthStateV2Healthy
			snapshot.TrafficPercent = 100
		}
	case ChannelHealthStateWarming:
		snapshot.StateV2 = ChannelHealthStateV2Degraded
		snapshot.TrafficPercent = snapshot.WarmupPercent
	case ChannelHealthStateOpen, ChannelHealthStateProbing:
		snapshot.StateV2 = ChannelHealthStateV2Unavailable
		snapshot.TrafficPercent = 0
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

func shouldHydrateChannelHealthSnapshotForProbe(snapshot ChannelHealthSnapshot, now time.Time, setting operation_setting.ChannelHealthSetting) bool {
	if snapshot.State != ChannelHealthStateOpen && snapshot.State != ChannelHealthStateProbing {
		return false
	}
	if snapshot.OpenedAt > 0 && setting.MaxIsolationSeconds > 0 {
		openedAt := time.Unix(snapshot.OpenedAt, 0)
		if now.Sub(openedAt) >= time.Duration(setting.MaxIsolationSeconds)*time.Second {
			return true
		}
	}
	return snapshot.NextProbeAt <= 0 || now.Unix() >= snapshot.NextProbeAt
}

func hydrateChannelHealthStateFromSnapshotLocked(shard *channelHealthShard, scope channelHealthScope, snapshot ChannelHealthSnapshot, now time.Time, setting operation_setting.ChannelHealthSetting) *channelHealthStateData {
	state := getOrCreateChannelHealthLocked(shard, scope)
	state.state = snapshot.State
	state.reason = snapshot.Reason
	state.openedAt = unixToTime(snapshot.OpenedAt)
	state.nextProbeAt = unixToTime(snapshot.NextProbeAt)
	state.probeInProgress = snapshot.ProbeInProgress
	state.consecutiveFailure = snapshot.ConsecutiveFailure
	state.probeSuccesses = snapshot.ProbeSuccesses
	state.probeFailures = snapshot.ProbeFailures
	state.warmupStartedAt = unixToTime(snapshot.WarmupStartedAt)
	state.warmupEndsAt = unixToTime(snapshot.WarmupEndsAt)
	state.warmupThrottlePercent = snapshot.WarmupThrottlePercent
	state.degraded = snapshot.State == ChannelHealthStateHealthy && snapshot.StateV2 == ChannelHealthStateV2Degraded
	if state.inflight == nil {
		state.inflight = make(map[int64]*channelAttemptState)
	}
	if state.state == ChannelHealthStateOpen || state.state == ChannelHealthStateProbing {
		if shouldHydrateChannelHealthSnapshotForProbe(snapshot, now, setting) {
			state.nextProbeAt = now
			state.probeInProgress = false
		}
	}
	return state
}

func GetChannelInflight(channelID int) int {
	return GetChannelInflightForModel(channelID, "")
}

func GetChannelInflightForModel(channelID int, modelName string) int {
	if channelID <= 0 || !channelHealthEnabled() {
		return 0
	}

	scope := channelHealthScopeFor(channelID, modelName, defaultChannelHealthSetting())
	shard := channelHealthShardFor(channelID)
	shard.Lock()
	defer shard.Unlock()

	state, ok := shard.channels[channelHealthScopeKey(scope)]
	if !ok {
		return 0
	}
	return channelActiveInflightCountLocked(state)
}

func RecordAttemptStart(meta ChannelAttemptMeta) AttemptHandle {
	if meta.ChannelID <= 0 || !channelHealthEnabled() {
		return AttemptHandle{}
	}

	handle := AttemptHandle{
		channelID: meta.ChannelID,
		modelName: strings.TrimSpace(meta.ModelName),
		attemptID: atomic.AddInt64(&nextChannelHealthAttemptID, 1),
	}
	setting := defaultChannelHealthSetting()
	scope := channelHealthScopeFor(meta.ChannelID, meta.ModelName, setting)
	handle.modelName = scope.modelName
	shard := channelHealthShardFor(meta.ChannelID)
	shard.Lock()
	defer shard.unlockAndFlush()

	state := getOrCreateChannelHealthLocked(shard, scope)
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

	shard := channelHealthShardFor(handle.channelID)
	shard.Lock()
	defer shard.Unlock()

	scope := channelHealthScopeFor(handle.channelID, handle.modelName, defaultChannelHealthSetting())
	state, ok := shard.channels[channelHealthScopeKey(scope)]
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
	attempt.firstResponse = latency
	state.firstResponseTotal += latency
	state.firstResponseCount++
}

func RecordAttemptFinish(handle AttemptHandle, result ChannelAttemptResult) {
	if handle.channelID <= 0 || handle.attemptID <= 0 || !channelHealthEnabled() {
		return
	}

	shouldSample, failed := classifyChannelAttemptResult(result)

	shard := channelHealthShardFor(handle.channelID)
	shard.Lock()

	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	scope := channelHealthScopeFor(handle.channelID, handle.modelName, setting)
	state, ok := shard.channels[channelHealthScopeKey(scope)]
	if !ok {
		shard.unlockAndFlush()
		return
	}
	attempt, ok := state.inflight[handle.attemptID]
	if !ok {
		shard.unlockAndFlush()
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
	if shouldSample && !attempt.cancelled {
		recordChannelHealthSampleLocked(state, now, setting, failed, reason, status, errCode, attempt.firstResponse)
		if attempt.meta.Probe {
			clearChannelID, shouldClearAffinity = recordProbeAttemptResultLocked(state, now, setting, !failed, reason)
		} else {
			clearChannelID, shouldClearAffinity = evaluateChannelHealthLocked(state, now, setting)
		}
	}
	shard.unlockAndFlush()

	if release != nil {
		release()
	}
	if shouldClearAffinity {
		ClearChannelAffinityByChannelID(clearChannelID)
	}
}

// isChannelGatewayErrorStatusCode reports whether code is an upstream gateway
// failure (502 Bad Gateway, 503 Service Unavailable, 504 Gateway Timeout).
// These are the only responses that count as a channel-health failure: they mean
// the upstream itself is unusable (e.g. "no available upstream account"), as
// opposed to rate limiting (429), model/quota errors (4xx), or a slow-but-working
// upstream. Runtime isolation therefore fires only on gateway errors.
func isChannelGatewayErrorStatusCode(code int) bool {
	return code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout
}

// classifyChannelAttemptResult returns (shouldSample, failed) for an attempt.
//
//   - Gateway errors (502/503/504) are sampled and counted as failures — the sole
//     trigger for the error-rate and consecutive-failure isolation paths. They are
//     matched before the sampling gate because 504 is classified always-skip-retry,
//     which the gate below would otherwise drop.
//   - Any other error that reached upstream (429, 500, 408, connection errors, …)
//     is sampled but never a failure, so it dilutes the gateway error rate and
//     resets the consecutive counter instead of isolating the channel.
//   - Client/local/skip-retry errors are not sampled at all.
//   - Successful responses are always sampled, never failures.
func classifyChannelAttemptResult(result ChannelAttemptResult) (bool, bool) {
	if result.Error != nil {
		if isChannelGatewayErrorStatusCode(result.Error.StatusCode) {
			return true, true
		}
		if !shouldSampleChannelHealthAttempt(result.Error) {
			return false, false
		}
		return true, false
	}
	return true, false
}

// shouldSampleChannelHealthAttempt reports whether a non-gateway error should
// enter the sliding health window (as a non-failure sample). Client-side, local,
// and always-skip-retry errors are excluded; anything that represents a genuine
// upstream response is sampled so the gateway error rate has an accurate
// denominator.
func shouldSampleChannelHealthAttempt(err *types.NewAPIError) bool {
	if err == nil {
		return false
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

func recordChannelHealthSampleLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting, failed bool, reason string, status int, errCode string, firstResponse time.Duration) {
	cutoff := now.Add(-time.Duration(setting.WindowSeconds) * time.Second)
	samples := state.samples[:0]
	for _, sample := range state.samples {
		if sample.at.After(cutoff) || sample.at.Equal(cutoff) {
			samples = append(samples, sample)
		}
	}
	samples = append(samples, channelHealthSample{
		at:            now,
		failed:        failed,
		reason:        reason,
		status:        status,
		errCode:       errCode,
		firstResponse: firstResponse,
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
			completeChannelWarmupLocked(state, now, setting, "warmup complete")
			return 0, false
		}
		if shouldReopenWarmingChannelLocked(state, now, setting) {
			return openChannelLocked(state, now, setting, "warmup unhealthy window")
		}
		stats := channelHealthWindowStatsLocked(state, now, setting)
		if shouldThrottleWarmupLocked(stats, setting) || state.consecutiveFailure > 0 || (setting.StuckDetectionEnabled && channelActiveInflightCountLocked(state) >= setting.StuckInflightThreshold) {
			state.warmupThrottlePercent = setting.WarmupStartPercent
			if state.warmupThrottlePercent <= 0 {
				state.warmupThrottlePercent = 1
			}
			state.reason = "warming throttled"
			persistChannelHealthIsolationLocked(state, now, setting)
			return 0, false
		}
		if stats.samples >= setting.MinSamples && stats.failures == 0 && stats.slowFirstResponses == 0 {
			state.warmupThrottlePercent = 0
			state.reason = "warming"
		}
		persistChannelHealthIsolationLocked(state, now, setting)
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

	if samples >= setting.MinSamples {
		errorRate := float64(failures) / float64(samples)
		if failures > 0 && errorRate >= setting.DegradationThreshold {
			wasDegraded := state.degraded
			state.degraded = true
			state.reason = fmt.Sprintf("degraded: error_rate %.2f%%", errorRate*100)
			persistChannelHealthIsolationLocked(state, now, setting)
			if !wasDegraded {
				recordChannelHealthEventLocked(setting, ChannelHealthEventTypeDegraded, state, state.reason, now)
			}
			return 0, false
		}
		if state.degraded && errorRate < setting.DegradationThreshold {
			state.degraded = false
			state.reason = ""
			persistChannelHealthIsolationLocked(state, now, setting)
			recordChannelHealthEventLocked(setting, ChannelHealthEventTypeRestored, state, "degradation recovered", now)
		}
	}
	return 0, false
}

type channelHealthWindowStats struct {
	samples                int
	failures               int
	firstResponseSamples   int
	slowFirstResponses     int
	averageFirstResponseMs float64
	p95FirstResponseMs     float64
}

func channelHealthWindowCountsLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) (int, int) {
	stats := channelHealthWindowStatsWithOptionsLocked(state, now, setting, false)
	return stats.samples, stats.failures
}

func channelHealthWindowStatsLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) channelHealthWindowStats {
	return channelHealthWindowStatsWithOptionsLocked(state, now, setting, true)
}

func channelHealthWindowStatsWithOptionsLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting, includeP95 bool) channelHealthWindowStats {
	cutoff := now.Add(-time.Duration(setting.WindowSeconds) * time.Second)
	stats := channelHealthWindowStats{}
	var firstResponseTotal time.Duration
	firstResponses := make([]time.Duration, 0)
	for _, sample := range state.samples {
		if sample.at.Before(cutoff) {
			continue
		}
		stats.samples++
		if sample.failed {
			stats.failures++
		}
		if sample.firstResponse > 0 {
			stats.firstResponseSamples++
			firstResponseTotal += sample.firstResponse
			if includeP95 {
				firstResponses = append(firstResponses, sample.firstResponse)
			}
			if setting.FirstResponseTimeoutSeconds > 0 && sample.firstResponse >= time.Duration(setting.FirstResponseTimeoutSeconds)*time.Second {
				stats.slowFirstResponses++
			}
		}
	}
	if stats.firstResponseSamples > 0 {
		stats.averageFirstResponseMs = float64(firstResponseTotal.Microseconds()) / 1000.0 / float64(stats.firstResponseSamples)
	}
	if includeP95 && len(firstResponses) > 0 {
		sort.Slice(firstResponses, func(i, j int) bool {
			return firstResponses[i] < firstResponses[j]
		})
		index := int(float64(len(firstResponses))*0.95 + 0.5)
		if index <= 0 {
			index = 1
		}
		if index > len(firstResponses) {
			index = len(firstResponses)
		}
		stats.p95FirstResponseMs = float64(firstResponses[index-1].Microseconds()) / 1000.0
	}
	return stats
}

func shouldReopenWarmingChannelLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) bool {
	if state == nil {
		return false
	}
	if state.consecutiveFailure >= setting.ConsecutiveFailureThreshold {
		return true
	}
	stats := channelHealthWindowStatsLocked(state, now, setting)
	if stats.samples < setting.MinSamples || stats.failures < setting.MinFailures {
		return false
	}
	errorRate := float64(stats.failures) / float64(stats.samples)
	return errorRate >= setting.ErrorRateThreshold
}

func shouldThrottleWarmupLocked(stats channelHealthWindowStats, setting operation_setting.ChannelHealthSetting) bool {
	if stats.samples > 0 && stats.failures > 0 {
		errorRate := float64(stats.failures) / float64(stats.samples)
		if errorRate >= setting.ErrorRateThreshold {
			return true
		}
	}
	if stats.firstResponseSamples == 0 || setting.FirstResponseTimeoutSeconds <= 0 {
		return false
	}
	if stats.averageFirstResponseMs >= float64(setting.FirstResponseTimeoutSeconds*1000) {
		return true
	}
	slowRatio := float64(stats.slowFirstResponses) / float64(stats.firstResponseSamples)
	return slowRatio >= setting.ErrorRateThreshold
}

func channelRecoveryWindowHealthyLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) (bool, string) {
	stats := channelHealthWindowStatsLocked(state, now, setting)
	if stats.samples >= setting.MinSamples && stats.failures >= setting.MinFailures {
		errorRate := float64(stats.failures) / float64(stats.samples)
		if errorRate >= setting.ErrorRateThreshold {
			return false, fmt.Sprintf("recovery blocked: error_rate %.2f%% over %ds (%d/%d)", errorRate*100, setting.WindowSeconds, stats.failures, stats.samples)
		}
	}
	minLatencySamples := setting.ProbeSuccessesToRecover
	if minLatencySamples <= 0 {
		minLatencySamples = 2
	}
	if stats.firstResponseSamples >= minLatencySamples && setting.FirstResponseTimeoutSeconds > 0 {
		if stats.averageFirstResponseMs >= float64(setting.FirstResponseTimeoutSeconds*1000) {
			return false, fmt.Sprintf("recovery blocked: first_response %.0fms over %ds", stats.averageFirstResponseMs, setting.FirstResponseTimeoutSeconds)
		}
		slowRatio := float64(stats.slowFirstResponses) / float64(stats.firstResponseSamples)
		if slowRatio >= setting.ErrorRateThreshold {
			return false, fmt.Sprintf("recovery blocked: first_response slow_ratio %.2f%%", slowRatio*100)
		}
	}
	return true, ""
}

func OpenChannel(channelID int, reason string) {
	OpenChannelForModel(channelID, "", reason)
}

func OpenChannelForModel(channelID int, modelName string, reason string) {
	if channelID <= 0 || !channelHealthEnabled() {
		return
	}

	shard := channelHealthShardFor(channelID)
	shard.Lock()

	setting := defaultChannelHealthSetting()
	scope := channelHealthScopeFor(channelID, modelName, setting)
	state := getOrCreateChannelHealthLocked(shard, scope)
	clearChannelID, shouldClearAffinity := openChannelLocked(state, channelHealthNow(), setting, reason)
	shard.unlockAndFlush()

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
	setting := defaultChannelHealthSetting()
	scopes := channelRuntimeControlScopes(channel, setting)

	shard := channelHealthShardFor(channelID)
	shard.Lock()
	now := channelHealthNow()
	shouldClearAffinity := false
	snapshot := ChannelHealthSnapshot{ChannelID: channelID}
	for i, scope := range scopes {
		state := getOrCreateChannelHealthLocked(shard, scope)
		_, shouldClear := openChannelLocked(state, now, setting, reason)
		shouldClearAffinity = shouldClearAffinity || shouldClear
		if duration > 0 {
			state.nextProbeAt = now.Add(duration)
			persistChannelHealthIsolationLocked(state, now, setting)
		}
		if i == 0 {
			snapshot = buildChannelHealthSnapshotLocked(state, now, setting)
		}
	}
	shard.unlockAndFlush()

	deleted := 0
	if shouldClearAffinity {
		deleted = ClearChannelAffinityByChannelID(channelID)
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
	setting := defaultChannelHealthSetting()
	scopes := channelRuntimeControlScopes(channel, setting)

	shard := channelHealthShardFor(channelID)
	shard.Lock()
	now := channelHealthNow()
	snapshot := ChannelHealthSnapshot{ChannelID: channelID, State: ChannelHealthStateHealthy, RuntimeAvailable: true, ProbeAvailable: true, WarmupPercent: 100}
	for i, scope := range scopes {
		state := getOrCreateChannelHealthLocked(shard, scope)
		resetChannelRuntimeStateLocked(state)
		markChannelHealthyLocked(state)
		deleteChannelHealthIsolationLocked(scope)
		if i == 0 {
			snapshot = buildChannelHealthSnapshotLocked(state, now, setting)
		}
	}
	shard.unlockAndFlush()

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
	setting := defaultChannelHealthSetting()
	scopes := channelRuntimeControlScopes(channel, setting)

	shard := channelHealthShardFor(channelID)
	shard.Lock()
	now := channelHealthNow()
	snapshot := ChannelHealthSnapshot{ChannelID: channelID}
	for i, scope := range scopes {
		state := getOrCreateChannelHealthLocked(shard, scope)
		if state.state == ChannelHealthStateHealthy || state.state == ChannelHealthStateWarming {
			state.state = ChannelHealthStateOpen
			state.degraded = false
			state.reason = "operator requested probe"
			state.openedAt = now
		}
		state.nextProbeAt = now
		state.probeInProgress = false
		persistChannelHealthIsolationLocked(state, now, setting)
		if i == 0 {
			snapshot = buildChannelHealthSnapshotLocked(state, now, setting)
		}
	}
	if len(scopes) == 0 {
		state := getOrCreateChannelHealthLocked(shard, channelHealthScopeFor(channelID, "", setting))
		state.state = ChannelHealthStateOpen
		state.degraded = false
		state.reason = "operator requested probe"
		state.openedAt = now
		state.nextProbeAt = now
		state.probeInProgress = false
		persistChannelHealthIsolationLocked(state, now, setting)
		snapshot = buildChannelHealthSnapshotLocked(state, now, setting)
	}
	shard.unlockAndFlush()

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

	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	scope := channelHealthScopeFor(channelID, modelName, setting)
	shard := channelHealthShardFor(channelID)
	key := channelHealthScopeKey(scope)

	// Cold path: if this instance has never seen the channel, pull its isolation
	// snapshot from the cache and hydrate it. The snapshot read (Redis GET, plus a
	// possible warmup-expiry delete) is done outside the shard lock so it never
	// blocks the shard on a Redis round-trip.
	shard.Lock()
	_, inMemory := shard.channels[key]
	shard.Unlock()
	if !inMemory {
		if snapshot, found := getChannelHealthIsolationSnapshot(scope, now); found && snapshot.State != ChannelHealthStateHealthy {
			shard.Lock()
			hydrateChannelHealthStateFromSnapshotLocked(shard, scope, snapshot, now, setting)
			shard.unlockAndFlush()
		}
	}

	shard.Lock()
	defer shard.unlockAndFlush()

	state, ok := shard.channels[key]
	if !ok {
		state = getOrCreateChannelHealthLocked(shard, scope)
	}
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
	recordChannelHealthEventLocked(setting, ChannelHealthEventTypeProbing, state, "probe started", now)
	return true
}

func openChannelLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting, reason string) (int, bool) {
	if state == nil {
		return 0, false
	}
	wasAvailable := state.state == ChannelHealthStateHealthy || state.state == ChannelHealthStateWarming
	state.state = ChannelHealthStateOpen
	state.degraded = false
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

	shard := channelHealthShardFor(channelID)
	shard.Lock()
	defer shard.unlockAndFlush()

	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	state := getOrCreateChannelHealthLocked(shard, channelHealthScopeFor(channelID, modelName, setting))
	recordProbeAttemptResultLocked(state, now, setting, success, reason)
}

func recordProbeAttemptResultLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting, success bool, reason string) (int, bool) {
	if state == nil {
		return 0, false
	}
	state.probeInProgress = false
	if success && (state.state == ChannelHealthStateHealthy || state.state == ChannelHealthStateWarming) {
		persistChannelHealthIsolationLocked(state, now, setting)
		return 0, false
	}
	if success {
		state.probeSuccesses++
		state.probeFailures = 0
		if state.probeSuccesses >= setting.ProbeSuccessesToRecover {
			if healthy, recoveryReason := channelRecoveryWindowHealthyLocked(state, now, setting); !healthy {
				state.state = ChannelHealthStateProbing
				state.reason = recoveryReason
				state.nextProbeAt = now.Add(time.Duration(setting.ProbeIntervalSeconds) * time.Second)
				persistChannelHealthIsolationLocked(state, now, setting)
				return 0, false
			}
			if setting.WarmupEnabled {
				state.state = ChannelHealthStateWarming
				state.degraded = false
				state.reason = "warming"
				state.nextProbeAt = time.Time{}
				state.consecutiveFailure = 0
				state.probeSuccesses = 0
				state.probeBackoff = 0
				state.samples = nil
				state.warmupThrottlePercent = 0
				state.warmupStartedAt = now
				state.warmupEndsAt = now.Add(time.Duration(setting.WarmupDurationSeconds) * time.Second)
				persistChannelHealthIsolationLocked(state, now, setting)
				recordChannelHealthEventLocked(setting, ChannelHealthEventTypeWarming, state, "probe recovered into warming", now)
			} else {
				markChannelHealthyLocked(state)
				deleteChannelHealthIsolationLocked(channelHealthScope{channelID: state.channelID, modelName: state.modelName})
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
	state.degraded = false
	state.reason = reason
	state.warmupStartedAt = time.Time{}
	state.warmupEndsAt = time.Time{}
	state.probeSuccesses = 0
	state.probeFailures++
	state.probeBackoff = nextProbeBackoffDuration(state.probeBackoff, setting, reason)
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
	state.warmupThrottlePercent = 0
	state.degraded = false
	state.samples = nil
}

func completeChannelWarmupLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting, reason string) {
	if state == nil {
		return
	}
	markChannelHealthyLocked(state)
	deleteChannelHealthIsolationLocked(channelHealthScope{channelID: state.channelID, modelName: state.modelName})
	recordChannelHealthEventLocked(setting, ChannelHealthEventTypeRecovered, state, reason, now)
}

// recordChannelHealthEventLocked is called while the state's shard lock is held.
// It builds the event snapshot from state under that lock, then takes the
// separate event-log lock to append the event and evaluate alert dedup. The
// lock order is always shard -> eventLog; no path takes the shard lock while
// holding eventLog, so this cannot deadlock.
func recordChannelHealthEventLocked(setting operation_setting.ChannelHealthSetting, eventType string, state *channelHealthStateData, reason string, now time.Time) {
	if state == nil || !setting.EventsEnabled {
		return
	}
	event := ChannelHealthEvent{
		Type:       eventType,
		ChannelID:  state.channelID,
		ModelName:  state.modelName,
		Group:      state.group,
		State:      string(state.state),
		StateV2:    string(translateStateToV2(state.state, state.degraded)),
		Reason:     reason,
		OccurredAt: now.Unix(),
		Snapshot:   buildChannelHealthEventSnapshotLocked(state, now, setting),
	}
	alertKey := fmt.Sprintf("%s:%s", eventType, channelHealthScopeKey(channelHealthScope{channelID: state.channelID, modelName: state.modelName}))
	minInterval := time.Duration(setting.AlertMinIntervalSeconds) * time.Second
	if minInterval <= 0 {
		minInterval = 60 * time.Second
	}

	channelHealthEventLog.Lock()
	if channelHealthEventLog.lastAlertAt == nil {
		channelHealthEventLog.lastAlertAt = make(map[string]time.Time)
	}
	if channelHealthEventAlertable(eventType) {
		if last, ok := channelHealthEventLog.lastAlertAt[alertKey]; !ok || now.Sub(last) >= minInterval {
			channelHealthEventLog.lastAlertAt[alertKey] = now
			event.AlertSent = true
			event.AlertSubject = channelHealthAlertSubject(event)
		}
	}
	channelHealthEventLog.events = append(channelHealthEventLog.events, event)
	if len(channelHealthEventLog.events) > 1000 {
		channelHealthEventLog.events = append([]ChannelHealthEvent(nil), channelHealthEventLog.events[len(channelHealthEventLog.events)-1000:]...)
	}
	channelHealthEventLog.Unlock()

	if event.AlertSent {
		go notifyChannelHealthEvent(event, loadChannelHealthNotifyFunc())
	}
}

func channelHealthEventAlertable(eventType string) bool {
	switch eventType {
	case ChannelHealthEventTypeOpened, ChannelHealthEventTypeRecovered, ChannelHealthEventTypeProbeFailed:
		return true
	default:
		return false
	}
}

func buildChannelHealthEventSnapshotLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) ChannelHealthEventSnapshot {
	if state == nil {
		return ChannelHealthEventSnapshot{}
	}
	stats := channelHealthWindowStatsLocked(state, now, setting)
	errorRate := 0.0
	if stats.samples > 0 {
		errorRate = float64(stats.failures) / float64(stats.samples)
	}
	return ChannelHealthEventSnapshot{
		ActiveInflight:         channelActiveInflightCountLocked(state),
		StuckInflight:          channelStuckInflightCountLocked(state),
		WindowSamples:          stats.samples,
		WindowFailures:         stats.failures,
		ErrorRate:              errorRate,
		AverageFirstResponseMs: stats.averageFirstResponseMs,
		P95FirstResponseMs:     stats.p95FirstResponseMs,
		ProbeBackoffSeconds:    int(state.probeBackoff.Seconds()),
		NextProbeAt:            unixOrZero(state.nextProbeAt),
		ProbeInProgress:        state.probeInProgress,
		WarmupPercent:          channelWarmupPercentLocked(state, now, setting),
		WarmupThrottlePercent:  state.warmupThrottlePercent,
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
	case ChannelHealthEventTypeProbing:
		return fmt.Sprintf("Channel #%d%s runtime probing", event.ChannelID, modelPart)
	case ChannelHealthEventTypeWarming:
		return fmt.Sprintf("Channel #%d%s runtime warming", event.ChannelID, modelPart)
	case ChannelHealthEventTypeDegraded:
		return fmt.Sprintf("Channel #%d%s runtime degraded", event.ChannelID, modelPart)
	case ChannelHealthEventTypeRestored:
		return fmt.Sprintf("Channel #%d%s runtime degradation recovered", event.ChannelID, modelPart)
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
	channelHealthEventLog.Lock()
	defer channelHealthEventLog.Unlock()

	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	events := make([]ChannelHealthEvent, 0, len(channelHealthEventLog.events))
	for i := len(channelHealthEventLog.events) - 1; i >= 0 && len(events) < limit; i-- {
		event := channelHealthEventLog.events[i]
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
		if filter.State != "" {
			switch filter.State {
			case string(ChannelHealthStateV2Healthy), string(ChannelHealthStateV2Degraded), string(ChannelHealthStateV2Unavailable):
				if event.StateV2 != filter.State {
					continue
				}
			default:
				if event.State != filter.State {
					continue
				}
			}
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
		SelectionSummary:       GetChannelSelectionTraceSummary(filter),
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
	var total time.Duration
	count := 0
	// Snapshot each shard under its own lock in turn; the aggregate does not need
	// a globally consistent view, only per-shard consistency.
	for shard := range channelHealthShards {
		s := &channelHealthShards[shard]
		s.Lock()
		for _, state := range s.channels {
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
		s.Unlock()
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

	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	firstResponseTimeout := time.Duration(setting.FirstResponseTimeoutSeconds) * time.Second
	singleStuckTimeout := time.Duration(setting.SingleStuckTimeoutSeconds) * time.Second
	clearChannelIDs := make([]int, 0)
	cancelStuckAttempts := make([]func(), 0)

	// Each shard is locked in turn so a slow shard never blocks the others; the
	// stuck-attempt cancels and affinity clears they imply are collected and run
	// after every shard lock has been released.
	for i := range channelHealthShards {
		shard := &channelHealthShards[i]
		shard.Lock()
		for _, state := range shard.channels {
			pruneChannelInflightLocked(state, now, setting)
			stuckCount := 0
			var maxAge time.Duration
			for _, attempt := range state.inflight {
				if attempt == nil || attempt.cancelled || attempt.firstResponseSeen {
					continue
				}
				age := now.Sub(attempt.startedAt)
				if age < firstResponseTimeout {
					continue
				}
				attempt.stuck = true
				if attempt.meta.Probe {
					state.probeInProgress = false
				}
				stuckCount++
				if age > maxAge {
					maxAge = age
				}
			}
			if stuckCount >= setting.StuckInflightThreshold || (stuckCount > 0 && maxAge >= singleStuckTimeout) {
				if !setting.StuckDetectionEnabled {
					continue
				}
				for _, attempt := range state.inflight {
					if !attempt.stuck || attempt.cancelled {
						continue
					}
					attempt.cancelled = true
					if attempt.meta.Cancel != nil {
						cancelStuckAttempts = append(cancelStuckAttempts, attempt.meta.Cancel)
					}
				}
				if channelID, shouldClear := openChannelLocked(state, now, setting, fmt.Sprintf("stuck inflight=%d max_age=%s", stuckCount, maxAge.Round(time.Second))); shouldClear {
					clearChannelIDs = append(clearChannelIDs, channelID)
				}
			}
		}
		shard.unlockAndFlush()
	}

	for _, cancel := range cancelStuckAttempts {
		cancel()
	}
	for _, channelID := range clearChannelIDs {
		ClearChannelAffinityByChannelID(channelID)
	}
}

func RunChannelHealthProbeWorker() {
	channelHealthProbeWorkerOnce.Do(func() {
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
	probeFn := loadChannelHealthProbeFunc()
	if probeFn == nil {
		return
	}
	type probeTarget struct {
		channelID int
		modelName string
	}

	// Phase 1: collect probe candidates per shard without mutating state or
	// touching Redis, so the shard lock is held only for the in-memory scan.
	candidates := make([]probeTarget, 0)
	for i := range channelHealthShards {
		shard := &channelHealthShards[i]
		shard.Lock()
		for _, state := range shard.channels {
			if state.state != ChannelHealthStateOpen && state.state != ChannelHealthStateProbing {
				continue
			}
			if state.probeInProgress {
				continue
			}
			if !state.nextProbeAt.IsZero() && now.Before(state.nextProbeAt) && !isChannelIsolationPastMaxLocked(state, now, setting) {
				continue
			}
			candidates = append(candidates, probeTarget{channelID: state.channelID, modelName: state.modelName})
		}
		shard.Unlock()
	}

	// Phase 2: for each candidate acquire the cross-instance probe lock (Redis
	// SetNX) OUTSIDE any shard lock, then re-lock the shard and re-check the
	// state still needs probing before committing the transition. The re-check
	// covers the window between the two lock acquisitions, where a request or
	// runtime-control call may have changed the state.
	targets := make([]probeTarget, 0, len(candidates))
	for _, candidate := range candidates {
		if !tryAcquireChannelHealthProbeLock(candidate.channelID, candidate.modelName, setting) {
			continue
		}
		scope := channelHealthScope{channelID: candidate.channelID, modelName: candidate.modelName}
		shard := channelHealthShardFor(candidate.channelID)
		shard.Lock()
		state, ok := shard.channels[channelHealthScopeKey(scope)]
		stillDue := ok && state != nil &&
			(state.state == ChannelHealthStateOpen || state.state == ChannelHealthStateProbing) &&
			!state.probeInProgress &&
			(state.nextProbeAt.IsZero() || !now.Before(state.nextProbeAt) || isChannelIsolationPastMaxLocked(state, now, setting))
		if !stillDue {
			shard.unlockAndFlush()
			releaseChannelHealthProbeLock(candidate.channelID, candidate.modelName)
			continue
		}
		state.probeInProgress = true
		state.state = ChannelHealthStateProbing
		persistChannelHealthIsolationLocked(state, now, setting)
		recordChannelHealthEventLocked(setting, ChannelHealthEventTypeProbing, state, "probe started", now)
		shard.unlockAndFlush()
		targets = append(targets, candidate)
	}

	for _, target := range targets {
		channelHealthProbeWaitGroup.Add(1)
		go func(target probeTarget) {
			defer channelHealthProbeWaitGroup.Done()
			runChannelHealthProbe(target.channelID, target.modelName, probeFn, setting)
		}(target)
	}
}

func runChannelHealthProbe(channelID int, modelName string, probeFn ChannelHealthProbeFunc, setting operation_setting.ChannelHealthSetting) {
	defer releaseChannelHealthProbeLock(channelID, modelName)

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

	err = probeFn(ctx, channel, modelName)
	if err != nil {
		RecordProbeResultForModel(channelID, modelName, false, err.Error())
		return
	}
	RecordProbeResultForModel(channelID, modelName, true, "")
}

func tryAcquireChannelHealthProbeLock(channelID int, modelName string, setting operation_setting.ChannelHealthSetting) bool {
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
	ok, err := common.RDB.SetNX(ctx, channelHealthProbeLockKey(channelID, modelName), common.GetTimeString(), ttl).Result()
	if err != nil {
		common.SysError(fmt.Sprintf("channel health probe lock failed: channel_id=%d, err=%v", channelID, err))
		return false
	}
	return ok
}

func releaseChannelHealthProbeLock(channelID int, modelName string) {
	if channelID <= 0 || !common.RedisEnabled || common.RDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := common.RDB.Del(ctx, channelHealthProbeLockKey(channelID, modelName)).Err(); err != nil {
		common.SysError(fmt.Sprintf("channel health probe unlock failed: channel_id=%d, err=%v", channelID, err))
	}
}

func channelHealthProbeLockKey(channelID int, modelName string) string {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return fmt.Sprintf("%s:%d", channelHealthProbeLockNamespace, channelID)
	}
	return fmt.Sprintf("%s:%d:model:%s", channelHealthProbeLockNamespace, channelID, modelName)
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

	shard := channelHealthShardFor(channelID)
	shard.Lock()
	defer shard.unlockAndFlush()

	state, ok := shard.channels[channelHealthScopeKey(scope)]
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
		ChannelID:        channelID,
		State:            ChannelHealthStateHealthy,
		StateV2:          ChannelHealthStateV2Healthy,
		TrafficPercent:   100,
		RuntimeAvailable: true,
		ProbeAvailable:   true,
		WarmupPercent:    100,
	}
}

func GetChannelHealthSnapshotForChannelDisplay(channel *model.Channel) ChannelHealthSnapshot {
	if channel == nil {
		return ChannelHealthSnapshot{}
	}
	snapshot := GetChannelHealthSnapshotForDisplay(channel.Id)
	if channel.Status == common.ChannelStatusEnabled {
		return snapshot
	}
	snapshot.RuntimeAvailable = false
	snapshot.ProbeAvailable = false
	switch channel.Status {
	case common.ChannelStatusManuallyDisabled:
		snapshot.AvailabilityReason = "channel manually disabled"
		snapshot.ProbeUnavailableReason = "channel manually disabled"
	case common.ChannelStatusAutoDisabled:
		snapshot.AvailabilityReason = "channel database status auto disabled"
		snapshot.ProbeUnavailableReason = "channel database status auto disabled"
	default:
		snapshot.AvailabilityReason = fmt.Sprintf("channel database status %d", channel.Status)
		snapshot.ProbeUnavailableReason = snapshot.AvailabilityReason
	}
	return snapshot
}

func GetChannelHealthSnapshots() []ChannelHealthSnapshot {
	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	snapshots := make([]ChannelHealthSnapshot, 0)
	for i := range channelHealthShards {
		shard := &channelHealthShards[i]
		shard.Lock()
		for _, state := range shard.channels {
			snapshots = append(snapshots, buildChannelHealthSnapshotLocked(state, now, setting))
		}
		shard.unlockAndFlush()
	}
	return snapshots
}

func buildChannelHealthSnapshotLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) ChannelHealthSnapshot {
	return buildChannelHealthSnapshotWithOptionsLocked(state, now, setting, true)
}

func buildChannelHealthSnapshotWithOptionsLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting, includeP95 bool) ChannelHealthSnapshot {
	if state.state == ChannelHealthStateWarming && isChannelWarmupCompleteLocked(state, now) {
		completeChannelWarmupLocked(state, now, setting, "warmup complete")
	}
	pruneChannelInflightLocked(state, now, setting)
	stats := channelHealthWindowStatsWithOptionsLocked(state, now, setting, includeP95)
	errorRate := 0.0
	if stats.samples > 0 {
		errorRate = float64(stats.failures) / float64(stats.samples)
	}
	warmupPercent := channelWarmupPercentWithOptionsLocked(state, now, setting, includeP95)
	runtimeAvailable, availabilityReason := channelRuntimeAvailabilityLocked(state, now, warmupPercent)
	probeAvailable, probeUnavailableReason := channelProbeAvailabilityLocked(state, now, setting)

	stateV2 := translateStateToV2(state.state, state.degraded)
	trafficPercent := 100
	if state.degraded && state.state == ChannelHealthStateHealthy {
		trafficPercent = channelHealthDegradedTrafficPercent
	} else if state.state == ChannelHealthStateWarming {
		trafficPercent = warmupPercent
	} else if state.state == ChannelHealthStateOpen || state.state == ChannelHealthStateProbing {
		trafficPercent = 0
	}

	return ChannelHealthSnapshot{
		ChannelID:              state.channelID,
		ModelName:              state.modelName,
		State:                  state.state,
		StateV2:                stateV2,
		TrafficPercent:         trafficPercent,
		Reason:                 state.reason,
		OpenedAt:               unixOrZero(state.openedAt),
		NextProbeAt:            unixOrZero(state.nextProbeAt),
		ProbeInProgress:        state.probeInProgress,
		ConsecutiveFailure:     state.consecutiveFailure,
		ProbeSuccesses:         state.probeSuccesses,
		ProbeFailures:          state.probeFailures,
		Inflight:               channelActiveInflightCountLocked(state),
		WindowSamples:          stats.samples,
		WindowFailures:         stats.failures,
		ErrorRate:              errorRate,
		AverageFirstResponseMs: stats.averageFirstResponseMs,
		P95FirstResponseMs:     stats.p95FirstResponseMs,
		RuntimeAvailable:       runtimeAvailable,
		AvailabilityReason:     availabilityReason,
		ProbeAvailable:         probeAvailable,
		ProbeUnavailableReason: probeUnavailableReason,
		WarmupStartedAt:        unixOrZero(state.warmupStartedAt),
		WarmupEndsAt:           unixOrZero(state.warmupEndsAt),
		WarmupPercent:          warmupPercent,
		WarmupThrottlePercent:  state.warmupThrottlePercent,
	}
}

func channelActiveInflightCountLocked(state *channelHealthStateData) int {
	if state == nil {
		return 0
	}
	count := 0
	for _, attempt := range state.inflight {
		if attempt == nil || attempt.cancelled {
			continue
		}
		count++
	}
	return count
}

func channelStuckInflightCountLocked(state *channelHealthStateData) int {
	if state == nil {
		return 0
	}
	count := 0
	for _, attempt := range state.inflight {
		if attempt == nil || !attempt.stuck {
			continue
		}
		count++
	}
	return count
}

func channelRuntimeAvailabilityLocked(state *channelHealthStateData, now time.Time, warmupPercent int) (bool, string) {
	if state == nil || state.state == ChannelHealthStateHealthy {
		return true, ""
	}
	switch state.state {
	case ChannelHealthStateWarming:
		if isChannelWarmupCompleteLocked(state, now) {
			return true, ""
		}
		if warmupPercent >= 100 {
			return true, "warming complete"
		}
		return true, fmt.Sprintf("warming retained traffic %d%%", warmupPercent)
	case ChannelHealthStateProbing:
		if state.probeInProgress {
			return false, "probe in progress"
		}
		return false, strings.TrimSpace(state.reason)
	case ChannelHealthStateOpen:
		reason := strings.TrimSpace(state.reason)
		if reason == "" {
			reason = "runtime isolated"
		}
		if !state.nextProbeAt.IsZero() && now.Before(state.nextProbeAt) {
			return false, fmt.Sprintf("%s; next probe at %s", reason, state.nextProbeAt.Format(time.RFC3339))
		}
		return false, reason
	default:
		return true, ""
	}
}

func channelProbeAvailabilityLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) (bool, string) {
	if state == nil || state.state == ChannelHealthStateHealthy {
		return true, ""
	}
	if state.state != ChannelHealthStateOpen && state.state != ChannelHealthStateProbing {
		return false, fmt.Sprintf("state %s is not probeable", state.state)
	}
	if state.probeInProgress {
		return false, "probe lock in progress"
	}
	if !state.nextProbeAt.IsZero() && now.Before(state.nextProbeAt) && !isChannelIsolationPastMaxLocked(state, now, setting) {
		return false, fmt.Sprintf("waiting next probe at %s", state.nextProbeAt.Format(time.RFC3339))
	}
	return true, ""
}

func adjustChannelHealthWeight(channelID int, modelName string, weight int, _ int) int {
	if weight <= 0 || channelID <= 0 || !channelHealthEnabled() {
		return weight
	}
	snapshot, ok := getChannelHealthSnapshotForWeight(channelID, modelName)
	if !ok {
		return weight
	}
	adjusted := weight
	setting := defaultChannelHealthSetting()
	runtimeDegraded := snapshot.State == ChannelHealthStateHealthy && snapshot.StateV2 == ChannelHealthStateV2Degraded
	if runtimeDegraded {
		percent := snapshot.TrafficPercent
		if percent <= 0 || percent > 100 {
			percent = channelHealthDegradedTrafficPercent
		}
		adjusted = adjusted * percent / 100
	}
	if snapshot.State == ChannelHealthStateOpen || snapshot.State == ChannelHealthStateProbing {
		percent := setting.WarmupStartPercent
		if percent <= 0 {
			percent = 1
		}
		if percent > 100 {
			percent = 100
		}
		adjusted = adjusted * percent / 100
	}
	if snapshot.State == ChannelHealthStateWarming {
		percent := snapshot.WarmupPercent
		if percent <= 0 {
			percent = setting.WarmupStartPercent
		}
		if percent > 100 {
			percent = 100
		}
		adjusted = adjusted * percent / 100
	}
	if snapshot.ErrorRate > 0 && !runtimeDegraded {
		errorPenalty := 1 - snapshot.ErrorRate
		if errorPenalty < 0.1 {
			errorPenalty = 0.1
		}
		adjusted = int(float64(adjusted) * errorPenalty)
	}
	// Slow first response degrades the channel's selection weight rather than
	// isolating it: a channel that still answers, just slowly, stays usable but
	// loses share to faster peers. The bar is SlowFirstResponseSeconds (default
	// 18s), independent of the 45s FirstResponseTimeoutSeconds that governs
	// stuck-request isolation and probe recovery.
	slowSeconds := setting.SlowFirstResponseSeconds
	if slowSeconds <= 0 {
		slowSeconds = setting.FirstResponseTimeoutSeconds
	}
	if slowSeconds > 0 && snapshot.AverageFirstResponseMs >= float64(slowSeconds*1000) {
		adjusted = adjusted / 2
	}
	if weight > 0 && adjusted <= 0 {
		return 1
	}
	return adjusted
}

func getChannelHealthSnapshotForWeight(channelID int, modelName string) (ChannelHealthSnapshot, bool) {
	now := channelHealthNow()
	setting := defaultChannelHealthSetting()
	scope := channelHealthScopeFor(channelID, modelName, setting)
	if snapshot, found := getChannelHealthIsolationSnapshot(scope, now); found {
		return snapshot, true
	}

	shard := channelHealthShardFor(channelID)
	shard.Lock()
	defer shard.unlockAndFlush()

	state, ok := shard.channels[channelHealthScopeKey(scope)]
	if !ok {
		return ChannelHealthSnapshot{}, false
	}
	return buildChannelHealthSnapshotWithOptionsLocked(state, now, setting, false), true
}

func resetChannelRuntimeStateLocked(state *channelHealthStateData) {
	if state == nil {
		return
	}
	state.inflight = make(map[int64]*channelAttemptState)
	state.probeInProgress = false
	state.probeBackoff = 0
}

func pruneChannelInflightLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) {
	if state == nil || len(state.inflight) == 0 {
		return
	}
	retention := channelInflightRetention(setting)
	for id, attempt := range state.inflight {
		if attempt == nil {
			delete(state.inflight, id)
			continue
		}
		if attempt.cancelled && now.Sub(attempt.startedAt) >= retention {
			delete(state.inflight, id)
		}
	}
}

func channelInflightRetention(setting operation_setting.ChannelHealthSetting) time.Duration {
	seconds := setting.SingleStuckTimeoutSeconds
	if setting.FirstResponseTimeoutSeconds > seconds {
		seconds = setting.FirstResponseTimeoutSeconds
	}
	if setting.ProbeTimeoutSeconds > seconds {
		seconds = setting.ProbeTimeoutSeconds
	}
	if seconds <= 0 {
		seconds = 75
	}
	return time.Duration(seconds*2) * time.Second
}

func isChannelIsolationPastMaxLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) bool {
	if state == nil || state.openedAt.IsZero() || setting.MaxIsolationSeconds <= 0 {
		return false
	}
	return now.Sub(state.openedAt) >= time.Duration(setting.MaxIsolationSeconds)*time.Second
}

func nextProbeBackoffDuration(current time.Duration, setting operation_setting.ChannelHealthSetting, reason string) time.Duration {
	interval := time.Duration(setting.ProbeIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	maxBackoff := time.Duration(setting.ProbeBackoffMaxSeconds) * time.Second
	if maxBackoff <= 0 {
		maxBackoff = 300 * time.Second
	}
	if isPermanentProbeFailure(reason) {
		return maxBackoff
	}
	if current <= 0 {
		current = interval
	} else {
		current *= 2
	}
	if current > maxBackoff {
		return maxBackoff
	}
	if current < interval {
		return interval
	}
	return current
}

func isPermanentProbeFailure(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return false
	}
	permanentMarkers := []string{
		"401",
		"403",
		"unauthorized",
		"forbidden",
		"invalid api key",
		"invalid_api_key",
		"insufficient_quota",
		"quota insufficient",
		"balance",
		"model not found",
		"model_not_found",
		"not support",
		"unsupported",
	}
	for _, marker := range permanentMarkers {
		if strings.Contains(reason, marker) {
			return true
		}
	}
	return false
}

func channelWarmupPercentLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) int {
	return channelWarmupPercentWithOptionsLocked(state, now, setting, true)
}

func channelWarmupPercentWithOptionsLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting, includeP95 bool) int {
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
	stats := channelHealthWindowStatsWithOptionsLocked(state, now, setting, includeP95)
	if shouldThrottleWarmupLocked(stats, setting) || (setting.StuckDetectionEnabled && channelActiveInflightCountLocked(state) >= setting.StuckInflightThreshold) {
		state.warmupThrottlePercent = setting.WarmupStartPercent
		if state.warmupThrottlePercent <= 0 {
			state.warmupThrottlePercent = 1
		}
	}
	if stats.samples >= setting.MinSamples && stats.failures == 0 && stats.slowFirstResponses == 0 && state.warmupThrottlePercent == 0 {
		percent += setting.WarmupStepPercent
		if percent > 100 {
			percent = 100
		}
	}
	if state.warmupThrottlePercent > 0 && percent > state.warmupThrottlePercent {
		percent = state.warmupThrottlePercent
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
	percent := channelWarmupPercentLocked(state, now, setting)
	if snapshot.WarmupThrottlePercent > 0 && percent > snapshot.WarmupThrottlePercent {
		return snapshot.WarmupThrottlePercent
	}
	return percent
}

// persistChannelHealthIsolationLocked queues the isolation write implied by the
// current state. It is always called while the state's shard lock is held, so it
// resolves that shard from state.channelID (same shard the caller holds, because
// sharding is keyed on channelID) and appends to its pending buffer instead of
// touching Redis inline. The queued op is drained by unlockAndFlush.
func persistChannelHealthIsolationLocked(state *channelHealthStateData, now time.Time, setting operation_setting.ChannelHealthSetting) {
	if state == nil {
		return
	}
	snapshot := buildChannelHealthSnapshotLocked(state, now, setting)
	scope := channelHealthScope{channelID: snapshot.ChannelID, modelName: snapshot.ModelName}
	shard := channelHealthShardFor(snapshot.ChannelID)
	if snapshot.State == ChannelHealthStateHealthy && snapshot.StateV2 != ChannelHealthStateV2Degraded {
		shard.queueIsolationDelete(scope)
		return
	}
	shard.queueIsolationPersist(scope, snapshot, channelHealthIsolationTTL(setting))
}

// deleteChannelHealthIsolationLocked queues an isolation delete while the scope's
// shard lock is held; the Redis DeleteMany runs later in unlockAndFlush.
func deleteChannelHealthIsolationLocked(scope channelHealthScope) {
	if scope.channelID <= 0 {
		return
	}
	channelHealthShardFor(scope.channelID).queueIsolationDelete(scope)
}

// deleteChannelHealthIsolationDirect deletes an isolation entry from Redis inline.
// It is only for callers that hold no shard lock (the read-path warmup-expiry
// cleanup in getChannelHealthIsolationSnapshot); under-lock callers must use
// deleteChannelHealthIsolationLocked so the network round-trip stays out of the
// critical section.
func deleteChannelHealthIsolationDirect(scope channelHealthScope) {
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

func unixToTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.Unix(value, 0)
}
