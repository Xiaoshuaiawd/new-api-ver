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

const (
	ChannelTokenTypeInput     = "input"
	ChannelTokenTypeOutput    = "output"
	ChannelTokenTypeCacheRead = "cache_read"
)

type channelMetrics struct {
	attempts  *prometheus.CounterVec
	retries   *prometheus.CounterVec
	inflight  *prometheus.GaugeVec
	duration  *prometheus.HistogramVec
	firstByte *prometheus.HistogramVec
	ttft      *prometheus.HistogramVec
	tokens    *prometheus.CounterVec
}

type ChannelAttemptMeta struct {
	ChannelID   int
	ChannelType int
	Stream      bool
	RetryIndex  int
	RetryReason string
}

type ChannelAttempt struct {
	runtime *Runtime
	meta    ChannelAttemptMeta
	started time.Time
	done    sync.Once
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
	}
	for name, collector := range map[string]prometheus.Collector{
		"channel attempts": metrics.attempts,
		"channel retries":  metrics.retries,
		"channel inflight": metrics.inflight,
		"channel tokens":   metrics.tokens,
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
	if runtime == nil || !runtime.Enabled || runtime.channel == nil || runtime.channel.firstByte == nil || channelID <= 0 || duration <= 0 {
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
	if meta.RetryIndex > 0 {
		runtime.channel.retries.WithLabelValues(
			channelID,
			channelType,
			normalizeErrorType(meta.RetryReason),
		).Inc()
	}
	return attempt
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
