package prometheusmetrics

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/types"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	relayDurationBuckets = []float64{
		0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 60, 120, 300, 600,
	}
	ttftBuckets = []float64{
		0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 60, 120,
	}
	streamDurationBuckets = []float64{
		1, 5, 10, 30, 60, 120, 300, 600, 1200, 1800, 3600,
	}
)

type relayMetrics struct {
	requests       *prometheus.CounterVec
	duration       *prometheus.HistogramVec
	inflight       *prometheus.GaugeVec
	streamTTFT     *prometheus.HistogramVec
	streamDuration *prometheus.HistogramVec
}

type RelayOutcome struct {
	Success bool
	Error   ErrorDetails
	TTFT    time.Duration
}

type RelayAttempt struct {
	runtime     *Runtime
	relayFormat string
	started     time.Time
	stream      bool
	streamOnce  sync.Once
	done        sync.Once
}

func registerRelayMetrics(registry prometheus.Registerer) (*relayMetrics, error) {
	metrics := &relayMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_relay_requests_total",
			Help: "Total final Relay requests.",
		}, []string{"relay_format", "stream", "result", "error_type"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "newapi_relay_duration_seconds",
			Help:    "Final Relay request duration in seconds.",
			Buckets: relayDurationBuckets,
		}, []string{"relay_format", "stream", "result"}),
		inflight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "newapi_relay_inflight",
			Help: "Current Relay requests.",
		}, []string{"relay_format", "stream"}),
		streamTTFT: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "newapi_stream_ttft_seconds",
			Help:    "Successful streaming Relay time to first response in seconds.",
			Buckets: ttftBuckets,
		}, []string{"relay_format"}),
		streamDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "newapi_stream_duration_seconds",
			Help:    "Streaming Relay duration in seconds.",
			Buckets: streamDurationBuckets,
		}, []string{"relay_format", "result"}),
	}
	for name, collector := range map[string]prometheus.Collector{
		"Relay requests":        metrics.requests,
		"Relay duration":        metrics.duration,
		"Relay inflight":        metrics.inflight,
		"stream TTFT":           metrics.streamTTFT,
		"stream total duration": metrics.streamDuration,
	} {
		if err := registry.Register(collector); err != nil {
			return nil, fmt.Errorf("register %s metric: %w", name, err)
		}
	}
	return metrics, nil
}

func BeginRelayAttempt(relayFormat types.RelayFormat) *RelayAttempt {
	attempt := &RelayAttempt{}
	runtime := defaultRuntime.Load()
	if runtime == nil || !runtime.Enabled || runtime.relay == nil {
		return attempt
	}
	attempt.runtime = runtime
	attempt.relayFormat = normalizeRelayFormat(relayFormat)
	attempt.started = time.Now()
	return attempt
}

func (a *RelayAttempt) SetStream(stream bool) {
	if a == nil || a.runtime == nil {
		return
	}
	a.streamOnce.Do(func() {
		a.stream = stream
		a.runtime.relay.inflight.WithLabelValues(a.relayFormat, strconv.FormatBool(stream)).Inc()
	})
}

func (a *RelayAttempt) Done(outcome RelayOutcome) {
	if a == nil || a.runtime == nil {
		return
	}
	a.done.Do(func() {
		a.SetStream(false)
		stream := strconv.FormatBool(a.stream)
		result, errorType := outcomeLabels(outcome.Success, outcome.Error)
		duration := time.Since(a.started)

		a.runtime.relay.inflight.WithLabelValues(a.relayFormat, stream).Dec()
		a.runtime.relay.requests.WithLabelValues(a.relayFormat, stream, result, errorType).Inc()
		a.runtime.relay.duration.WithLabelValues(a.relayFormat, stream, result).Observe(duration.Seconds())
		if a.stream {
			a.runtime.relay.streamDuration.WithLabelValues(a.relayFormat, result).Observe(duration.Seconds())
			if outcome.Success && outcome.TTFT > 0 {
				a.runtime.relay.streamTTFT.WithLabelValues(a.relayFormat).Observe(outcome.TTFT.Seconds())
			}
		}
	})
}

func normalizeRelayFormat(format types.RelayFormat) string {
	switch format {
	case types.RelayFormatOpenAI,
		types.RelayFormatClaude,
		types.RelayFormatGemini,
		types.RelayFormatOpenAIResponses,
		types.RelayFormatOpenAIResponsesCompaction,
		types.RelayFormatOpenAIAlphaSearch,
		types.RelayFormatOpenAIAudio,
		types.RelayFormatOpenAIImage,
		types.RelayFormatOpenAIRealtime,
		types.RelayFormatRerank,
		types.RelayFormatEmbedding,
		types.RelayFormatTask,
		types.RelayFormatMjProxy:
		return string(format)
	default:
		return "other"
	}
}
