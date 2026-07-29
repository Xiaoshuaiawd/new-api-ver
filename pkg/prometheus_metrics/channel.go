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

type channelMetrics struct {
	attempts  *prometheus.CounterVec
	retries   *prometheus.CounterVec
	inflight  *prometheus.GaugeVec
	duration  *prometheus.HistogramVec
	firstByte *prometheus.HistogramVec
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
	}
	for name, collector := range map[string]prometheus.Collector{
		"channel attempts": metrics.attempts,
		"channel retries":  metrics.retries,
		"channel inflight": metrics.inflight,
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
	}
	return metrics, nil
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

func (a *ChannelAttempt) Done(success bool, details ErrorDetails) {
	if a == nil || a.runtime == nil {
		return
	}
	a.done.Do(func() {
		channelID := strconv.Itoa(a.meta.ChannelID)
		channelType := strconv.Itoa(a.meta.ChannelType)
		stream := strconv.FormatBool(a.meta.Stream)
		result, errorType := outcomeLabels(success, details)

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
	})
}
