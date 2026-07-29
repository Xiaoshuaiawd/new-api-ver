package prometheusmetrics

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var requestDurationBuckets = []float64{
	0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 60, 120, 300, 600,
}

type httpMetrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

func registerHTTPMetrics(registry prometheus.Registerer) (*httpMetrics, error) {
	metrics := &httpMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_http_requests_total",
			Help: "Total tagged business HTTP requests.",
		}, []string{"route", "method", "status_class"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "newapi_http_request_duration_seconds",
			Help:    "Tagged business HTTP request duration in seconds.",
			Buckets: requestDurationBuckets,
		}, []string{"route", "method", "status_class"}),
	}
	if err := registry.Register(metrics.requests); err != nil {
		return nil, fmt.Errorf("register HTTP request counter: %w", err)
	}
	if err := registry.Register(metrics.duration); err != nil {
		return nil, fmt.Errorf("register HTTP request duration: %w", err)
	}
	return metrics, nil
}

func (r *Runtime) ObserveHTTPRequest(route, method, statusClass string, duration time.Duration) {
	if r == nil || r.http == nil {
		return
	}
	labels := []string{route, method, statusClass}
	r.http.requests.WithLabelValues(labels...).Inc()
	r.http.duration.WithLabelValues(labels...).Observe(duration.Seconds())
}
