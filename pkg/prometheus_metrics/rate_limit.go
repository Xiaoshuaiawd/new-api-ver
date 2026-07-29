package prometheusmetrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

type rateLimitMetrics struct {
	rejections *prometheus.CounterVec
}

func registerRateLimitMetrics(registry prometheus.Registerer) (*rateLimitMetrics, error) {
	metrics := &rateLimitMetrics{
		rejections: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_rate_limit_rejections_total",
			Help: "Total requests rejected by an application rate limit.",
		}, []string{"scope", "reason"}),
	}
	if err := registry.Register(metrics.rejections); err != nil {
		return nil, fmt.Errorf("register rate limit rejections: %w", err)
	}
	return metrics, nil
}

func (r *Runtime) RecordRateLimitRejection(scope, reason string) {
	if r == nil || r.rateLimit == nil {
		return
	}
	r.rateLimit.rejections.WithLabelValues(
		normalizeRateLimitScope(scope),
		normalizeRateLimitReason(reason),
	).Inc()
}

func RecordRateLimitRejection(scope, reason string) {
	runtime := defaultRuntime.Load()
	if runtime != nil {
		runtime.RecordRateLimitRejection(scope, reason)
	}
}

func normalizeRateLimitScope(scope string) string {
	switch scope {
	case "global", "user", "token", "channel":
		return scope
	default:
		return "global"
	}
}

func normalizeRateLimitReason(reason string) string {
	switch reason {
	case "request_count", "total_request_count", "successful_request_count", "concurrency":
		return reason
	default:
		return "other"
	}
}
