package service

import (
	"context"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/dto"
	prometheusmetrics "github.com/QuantumNous/new-api/pkg/prometheus_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

const channelRuntimeWindowSeconds int64 = 60

type channelRuntimeMetrics = dto.ChannelRuntimeMetrics

type channelRuntimeBucket struct {
	second int64
	count  int
}

type channelRuntimeState struct {
	concurrency  int
	successRPM   int
	lastActivity int64
	windowSecond int64
	buckets      [channelRuntimeWindowSeconds]channelRuntimeBucket
}

type channelRuntimeTracker struct {
	mu          sync.Mutex
	now         func() time.Time
	lastCleanup int64
	channels    map[int]*channelRuntimeState
}

type ChannelRuntimeAttempt struct {
	tracker   *channelRuntimeTracker
	channelID int
	done      sync.Once
}

type ChannelAttemptMeta = prometheusmetrics.ChannelAttemptMeta
type ChannelAttemptOutcome = prometheusmetrics.ChannelAttemptOutcome

var defaultChannelRuntimeTracker = newChannelRuntimeTracker(time.Now)

func newChannelRuntimeTracker(now func() time.Time) *channelRuntimeTracker {
	return &channelRuntimeTracker{
		now:      now,
		channels: make(map[int]*channelRuntimeState),
	}
}

func (state *channelRuntimeState) advanceWindow(now int64) {
	if state == nil || now <= state.windowSecond {
		return
	}

	elapsed := now - state.windowSecond
	if elapsed >= channelRuntimeWindowSeconds {
		clear(state.buckets[:])
		state.successRPM = 0
		state.windowSecond = now
		return
	}

	cutoff := now - channelRuntimeWindowSeconds
	for second := state.windowSecond + 1; second <= now; second++ {
		bucket := &state.buckets[second%channelRuntimeWindowSeconds]
		if bucket.count > 0 && bucket.second <= cutoff {
			state.successRPM -= bucket.count
			*bucket = channelRuntimeBucket{}
		}
	}
	state.windowSecond = now
}

func (tracker *channelRuntimeTracker) cleanupLocked(now int64) {
	if tracker.lastCleanup != 0 && now-tracker.lastCleanup < channelRuntimeWindowSeconds {
		return
	}
	tracker.lastCleanup = now
	cutoff := now - channelRuntimeWindowSeconds
	for channelID, state := range tracker.channels {
		state.advanceWindow(now)
		if state.concurrency == 0 && state.successRPM == 0 && state.lastActivity <= cutoff {
			delete(tracker.channels, channelID)
		}
	}
}

func (tracker *channelRuntimeTracker) begin(channelID int) *ChannelRuntimeAttempt {
	attempt := &ChannelRuntimeAttempt{}
	if tracker == nil || channelID <= 0 {
		return attempt
	}

	now := tracker.now().Unix()
	tracker.mu.Lock()
	tracker.cleanupLocked(now)
	state := tracker.channels[channelID]
	if state == nil {
		state = &channelRuntimeState{windowSecond: now}
		tracker.channels[channelID] = state
	}
	state.advanceWindow(now)
	state.lastActivity = now
	state.concurrency++
	tracker.mu.Unlock()

	attempt.tracker = tracker
	attempt.channelID = channelID
	return attempt
}

func (tracker *channelRuntimeTracker) snapshot(channelIDs []int) map[int]dto.ChannelRuntimeMetrics {
	metrics := make(map[int]dto.ChannelRuntimeMetrics, len(channelIDs))
	if tracker == nil {
		return metrics
	}

	now := tracker.now().Unix()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.cleanupLocked(now)

	for _, channelID := range channelIDs {
		current := dto.ChannelRuntimeMetrics{}
		state := tracker.channels[channelID]
		if state != nil {
			state.advanceWindow(now)
			current.Concurrency = state.concurrency
			current.RPM = state.successRPM
		}
		metrics[channelID] = current
	}
	return metrics
}

func (tracker *channelRuntimeTracker) track(channelID int, operation func() bool) {
	attempt := tracker.begin(channelID)
	success := false
	defer func() {
		attempt.Done(success)
	}()
	success = operation()
}

func (attempt *ChannelRuntimeAttempt) Done(success bool) {
	if attempt == nil || attempt.tracker == nil || attempt.channelID <= 0 {
		return
	}

	attempt.done.Do(func() {
		second := attempt.tracker.now().Unix()
		attempt.tracker.mu.Lock()
		defer attempt.tracker.mu.Unlock()
		state := attempt.tracker.channels[attempt.channelID]
		if state == nil {
			return
		}
		state.advanceWindow(second)
		state.lastActivity = second
		if state.concurrency > 0 {
			state.concurrency--
		}
		if !success {
			return
		}
		bucket := &state.buckets[second%channelRuntimeWindowSeconds]
		if bucket.second != second {
			state.successRPM -= bucket.count
			bucket.second = second
			bucket.count = 0
		}
		bucket.count++
		state.successRPM++
	})
}

func BeginChannelRuntimeAttempt(channelID int) *ChannelRuntimeAttempt {
	return defaultChannelRuntimeTracker.begin(channelID)
}

func TrackChannelRuntimeAttempt(channelID int, operation func() bool) {
	defaultChannelRuntimeTracker.track(channelID, operation)
}

func ObserveChannelFirstByte(channelID, channelType int, duration time.Duration) {
	prometheusmetrics.ObserveChannelFirstByte(channelID, channelType, duration)
}

func recordChannelTokenMetrics(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) {
	if relayInfo == nil || relayInfo.ChannelMeta == nil || usage == nil {
		return
	}

	input := usage.InputTokens
	if input <= 0 {
		input = usage.PromptTokens
	}
	output := usage.OutputTokens
	if output <= 0 {
		output = usage.CompletionTokens
	}
	cacheRead := usage.PromptTokensDetails.CachedTokens
	if cacheRead <= 0 && usage.InputTokensDetails != nil {
		cacheRead = usage.InputTokensDetails.CachedTokens
	}
	if cacheRead <= 0 {
		cacheRead = usage.PromptCacheHitTokens
	}

	prometheusmetrics.RecordChannelTokens(relayInfo.ChannelId, relayInfo.ChannelType, prometheusmetrics.ChannelTokenUsage{
		Input:     input,
		Output:    output,
		CacheRead: cacheRead,
	})
}

func WithChannelFirstByteTrace(ctx context.Context, channelID, channelType int) context.Context {
	if ctx == nil || channelID <= 0 {
		return ctx
	}

	var (
		startMu sync.Mutex
		started time.Time
		done    sync.Once
	)
	ensureStarted := func() time.Time {
		startMu.Lock()
		defer startMu.Unlock()
		if started.IsZero() {
			started = time.Now()
		}
		return started
	}
	trace := &httptrace.ClientTrace{
		GetConn: func(string) {
			ensureStarted()
		},
		GotFirstResponseByte: func() {
			done.Do(func() {
				ObserveChannelFirstByte(channelID, channelType, time.Since(ensureStarted()))
			})
		},
	}
	return httptrace.WithClientTrace(ctx, trace)
}

func TrackChannelAttempt(meta ChannelAttemptMeta, operation func() ChannelAttemptOutcome) {
	TrackChannelAttemptWithFirstTokenObserver(meta, func(func()) ChannelAttemptOutcome {
		return operation()
	})
}

func TrackChannelAttemptWithFirstTokenObserver(meta ChannelAttemptMeta, operation func(markFirstToken func()) ChannelAttemptOutcome) {
	runtimeAttempt := defaultChannelRuntimeTracker.begin(meta.ChannelID)
	prometheusAttempt := prometheusmetrics.BeginChannelAttempt(meta)
	outcome := ChannelAttemptOutcome{}
	defer func() {
		if recovered := recover(); recovered != nil {
			runtimeAttempt.Done(false)
			prometheusAttempt.Done(prometheusmetrics.ChannelAttemptOutcome{})
			panic(recovered)
		}
		runtimeAttempt.Done(outcome.Success)
		prometheusAttempt.Done(outcome)
	}()
	outcome = operation(prometheusAttempt.MarkFirstToken)
}

func GetChannelRuntimeMetrics(channelIDs []int) map[int]dto.ChannelRuntimeMetrics {
	return defaultChannelRuntimeTracker.snapshot(channelIDs)
}
