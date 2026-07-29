package prometheusmetrics

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var channelDurationBuckets = []float64{
	0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 60, 120, 300, 600,
}

var channelFirstTokenWaitThresholds = []time.Duration{30 * time.Second, 60 * time.Second}

const (
	ChannelTokenTypeInput     = "input"
	ChannelTokenTypeOutput    = "output"
	ChannelTokenTypeCacheRead = "cache_read"
)

type channelMetrics struct {
	attempts          *prometheus.CounterVec
	retries           *prometheus.CounterVec
	inflight          *prometheus.GaugeVec
	duration          *prometheus.HistogramVec
	firstByte         *prometheus.HistogramVec
	ttft              *prometheus.HistogramVec
	tokens            *prometheus.CounterVec
	firstTokenWaiters *channelFirstTokenWaitTracker
}

type ChannelAttemptMeta struct {
	ChannelID   int
	ChannelType int
	Stream      bool
	RetryIndex  int
	RetryReason string
}

type ChannelAttempt struct {
	runtime          *Runtime
	meta             ChannelAttemptMeta
	started          time.Time
	done             sync.Once
	firstTokenWaiter *channelFirstTokenWaiter
}

type ChannelAttemptOutcome struct {
	Success bool
	Error   ErrorDetails
	TTFT    time.Duration
}

type ChannelTokenUsage struct {
	Input     int
	Output    int
	CacheRead int
}

type channelFirstTokenWaitKey struct {
	channelID   int
	channelType int
	threshold   time.Duration
}

type channelFirstTokenWaiter struct {
	tracker     *channelFirstTokenWaitTracker
	channelID   int
	channelType int
	started     time.Time
	done        sync.Once
}

type channelFirstTokenWaitTracker struct {
	mu      sync.Mutex
	now     func() time.Time
	waiters map[*channelFirstTokenWaiter]struct{}
}

type channelFirstTokenWaitCollector struct {
	desc    *prometheus.Desc
	tracker *channelFirstTokenWaitTracker
}

func newChannelFirstTokenWaitTracker(now func() time.Time) *channelFirstTokenWaitTracker {
	return &channelFirstTokenWaitTracker{
		now:     now,
		waiters: make(map[*channelFirstTokenWaiter]struct{}),
	}
}

func (tracker *channelFirstTokenWaitTracker) Begin(channelID, channelType int) *channelFirstTokenWaiter {
	if tracker == nil || channelID <= 0 {
		return &channelFirstTokenWaiter{}
	}
	waiter := &channelFirstTokenWaiter{
		tracker:     tracker,
		channelID:   channelID,
		channelType: channelType,
		started:     tracker.now(),
	}
	tracker.mu.Lock()
	tracker.waiters[waiter] = struct{}{}
	tracker.mu.Unlock()
	return waiter
}

func (tracker *channelFirstTokenWaitTracker) Snapshot() map[channelFirstTokenWaitKey]int {
	if tracker == nil {
		return nil
	}
	now := tracker.now()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	counts := make(map[channelFirstTokenWaitKey]int)
	for waiter := range tracker.waiters {
		for _, threshold := range channelFirstTokenWaitThresholds {
			if now.Sub(waiter.started) >= threshold {
				counts[channelFirstTokenWaitKey{
					channelID:   waiter.channelID,
					channelType: waiter.channelType,
					threshold:   threshold,
				}]++
			}
		}
	}
	return counts
}

func (waiter *channelFirstTokenWaiter) Done() {
	if waiter == nil || waiter.tracker == nil {
		return
	}
	waiter.done.Do(func() {
		waiter.tracker.mu.Lock()
		delete(waiter.tracker.waiters, waiter)
		waiter.tracker.mu.Unlock()
	})
}

func newChannelFirstTokenWaitCollector(tracker *channelFirstTokenWaitTracker) *channelFirstTokenWaitCollector {
	return &channelFirstTokenWaitCollector{
		desc: prometheus.NewDesc(
			"newapi_channel_stream_first_token_waiting",
			"Current streaming channel attempts still waiting for their first valid response content beyond the threshold.",
			[]string{"channel_id", "channel_type", "threshold_seconds"},
			nil,
		),
		tracker: tracker,
	}
}

func (collector *channelFirstTokenWaitCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- collector.desc
}

func (collector *channelFirstTokenWaitCollector) Collect(ch chan<- prometheus.Metric) {
	for key, count := range collector.tracker.Snapshot() {
		ch <- prometheus.MustNewConstMetric(
			collector.desc,
			prometheus.GaugeValue,
			float64(count),
			strconv.Itoa(key.channelID),
			strconv.Itoa(key.channelType),
			strconv.FormatInt(int64(key.threshold/time.Second), 10),
		)
	}
}

func registerChannelMetrics(registry prometheus.Registerer, histogramEnabled bool) (*channelMetrics, error) {
	metrics := &channelMetrics{
		attempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_channel_attempts_total",
			Help: "Total channel execution attempts.",
		}, []string{"channel_id", "channel_type", "stream", "result", "error_type"}),
		retries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_channel_retries_total",
			Help: "Total channel retry attempts after the first attempt.",
		}, []string{"channel_id", "channel_type", "reason"}),
		inflight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "newapi_channel_inflight",
			Help: "Current channel execution attempts.",
		}, []string{"channel_id", "channel_type"}),
		tokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_channel_tokens_total",
			Help: "Normalized channel token usage by fixed token type.",
		}, []string{"channel_id", "channel_type", "token_type"}),
		firstTokenWaiters: newChannelFirstTokenWaitTracker(time.Now),
	}
	for name, collector := range map[string]prometheus.Collector{
		"channel attempts":                   metrics.attempts,
		"channel retries":                    metrics.retries,
		"channel inflight":                   metrics.inflight,
		"channel tokens":                     metrics.tokens,
		"channel stream first token waiting": newChannelFirstTokenWaitCollector(metrics.firstTokenWaiters),
	} {
		if err := registry.Register(collector); err != nil {
			return nil, fmt.Errorf("register %s metric: %w", name, err)
		}
	}
	if histogramEnabled {
		metrics.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "newapi_channel_duration_seconds",
			Help:    "Channel execution attempt duration in seconds.",
			Buckets: channelDurationBuckets,
		}, []string{"channel_id", "channel_type", "stream", "result"})
		if err := registry.Register(metrics.duration); err != nil {
			return nil, fmt.Errorf("register channel duration metric: %w", err)
		}
		metrics.firstByte = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "newapi_channel_first_byte_seconds",
			Help:    "Time from channel transport connection acquisition to the first response header byte in seconds.",
			Buckets: ttftBuckets,
		}, []string{"channel_id", "channel_type"})
		if err := registry.Register(metrics.firstByte); err != nil {
			return nil, fmt.Errorf("register channel first byte metric: %w", err)
		}
		metrics.ttft = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "newapi_channel_ttft_seconds",
			Help:    "Successful streaming channel time to first response in seconds.",
			Buckets: ttftBuckets,
		}, []string{"channel_id", "channel_type"})
		if err := registry.Register(metrics.ttft); err != nil {
			return nil, fmt.Errorf("register channel TTFT metric: %w", err)
		}
	}
	return metrics, nil
}

func RecordChannelTokens(channelID, channelType int, usage ChannelTokenUsage) {
	runtime := defaultRuntime.Load()
	if runtime == nil || !runtime.Enabled || runtime.channel == nil || runtime.channel.tokens == nil || channelID <= 0 {
		return
	}
	if usage.Input < 0 || usage.Output < 0 || usage.CacheRead < 0 {
		return
	}

	channelIDLabel := strconv.Itoa(channelID)
	channelTypeLabel := strconv.Itoa(channelType)
	runtime.channel.tokens.WithLabelValues(channelIDLabel, channelTypeLabel, ChannelTokenTypeInput).Add(float64(usage.Input))
	runtime.channel.tokens.WithLabelValues(channelIDLabel, channelTypeLabel, ChannelTokenTypeOutput).Add(float64(usage.Output))
	runtime.channel.tokens.WithLabelValues(channelIDLabel, channelTypeLabel, ChannelTokenTypeCacheRead).Add(float64(usage.CacheRead))
}

func ObserveChannelFirstByte(channelID, channelType int, duration time.Duration) {
	runtime := defaultRuntime.Load()
	if runtime == nil || !runtime.Enabled || runtime.channel == nil || runtime.channel.firstByte == nil || channelID <= 0 || duration < 0 {
		return
	}
	runtime.channel.firstByte.WithLabelValues(
		strconv.Itoa(channelID),
		strconv.Itoa(channelType),
	).Observe(duration.Seconds())
}

func BeginChannelAttempt(meta ChannelAttemptMeta) *ChannelAttempt {
	attempt := &ChannelAttempt{}
	runtime := defaultRuntime.Load()
	if runtime == nil || !runtime.Enabled || runtime.channel == nil || meta.ChannelID <= 0 {
		return attempt
	}

	attempt.runtime = runtime
	attempt.meta = meta
	attempt.started = time.Now()
	channelID := strconv.Itoa(meta.ChannelID)
	channelType := strconv.Itoa(meta.ChannelType)
	runtime.channel.inflight.WithLabelValues(channelID, channelType).Inc()
	if meta.Stream {
		attempt.firstTokenWaiter = runtime.channel.firstTokenWaiters.Begin(meta.ChannelID, meta.ChannelType)
	}
	if meta.RetryIndex > 0 {
		runtime.channel.retries.WithLabelValues(
			channelID,
			channelType,
			normalizeErrorType(meta.RetryReason),
		).Inc()
	}
	return attempt
}

func (a *ChannelAttempt) MarkFirstToken() {
	if a == nil {
		return
	}
	a.firstTokenWaiter.Done()
}

func (a *ChannelAttempt) Done(outcome ChannelAttemptOutcome) {
	if a == nil || a.runtime == nil {
		return
	}
	a.done.Do(func() {
		channelID := strconv.Itoa(a.meta.ChannelID)
		channelType := strconv.Itoa(a.meta.ChannelType)
		stream := strconv.FormatBool(a.meta.Stream)
		result, errorType := outcomeLabels(outcome.Success, outcome.Error)

		a.MarkFirstToken()
		a.runtime.channel.inflight.WithLabelValues(channelID, channelType).Dec()
		a.runtime.channel.attempts.WithLabelValues(
			channelID,
			channelType,
			stream,
			result,
			errorType,
		).Inc()
		if a.runtime.channel.duration != nil {
			a.runtime.channel.duration.WithLabelValues(
				channelID,
				channelType,
				stream,
				result,
			).Observe(time.Since(a.started).Seconds())
		}
		if a.runtime.channel.ttft != nil && a.meta.Stream && outcome.Success && outcome.TTFT > 0 {
			a.runtime.channel.ttft.WithLabelValues(channelID, channelType).Observe(outcome.TTFT.Seconds())
		}
	})
}
