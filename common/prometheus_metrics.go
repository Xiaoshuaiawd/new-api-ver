package common

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	prometheusNamespace = "newapi"

	ChannelRequestResultSuccess = "success"
	ChannelRequestResultError   = "error"

	maxPrometheusErrorMessageRunes = 240
)

type ChannelMetricLabels struct {
	ChannelID   string
	ChannelName string
	ChannelType string
	RequestKind string
}

type ChannelAttemptResult struct {
	Result     string
	StatusCode int
	ErrorCode  string
}

type ChannelErrorDetail struct {
	ModelName    string
	ErrorType    string
	ErrorCode    string
	StatusCode   int
	ErrorMessage string
}

type HTTPMetricLabels struct {
	RouteTag string
	Method   string
}

type PrometheusMetrics struct {
	httpInflight                 *prometheus.GaugeVec
	httpRequests                 *prometheus.CounterVec
	httpDuration                 *prometheus.HistogramVec
	channelInflight              *prometheus.GaugeVec
	channelRequests              *prometheus.CounterVec
	channelModelRequests         *prometheus.CounterVec
	channelRequestsLast1m        prometheus.Collector
	channelSuccessRPMLast1m      prometheus.Collector
	channelErrors                *prometheus.CounterVec
	channelLastErrorTimestamp    prometheus.Collector
	channelAttemptDuration       *prometheus.HistogramVec
	channelFirstResponseDuration *prometheus.HistogramVec
	channelUpstreamLatency       *prometheus.HistogramVec
	systemPerformanceRejections  *prometheus.CounterVec
	systemStatusCollector        prometheus.Collector
	now                          func() time.Time
}

type channelAttemptObserver struct {
	metrics   *PrometheusMetrics
	labels    ChannelMetricLabels
	modelName string
	start     time.Time
	active    bool
}

type httpRequestObserver struct {
	metrics *PrometheusMetrics
	labels  HTTPMetricLabels
	start   time.Time
	active  bool
}

type systemStatusCollector struct {
	cpuUsageDesc       *prometheus.Desc
	memoryUsageDesc    *prometheus.Desc
	diskUsageDesc      *prometheus.Desc
	monitorEnabledDesc *prometheus.Desc
	thresholdDesc      *prometheus.Desc
}

type channelSlidingWindowCollector struct {
	desc    *prometheus.Desc
	now     func() time.Time
	mu      sync.Mutex
	windows map[string]*channelSlidingWindow
}

type channelSlidingWindow struct {
	labels     ChannelMetricLabels
	timestamps []time.Time
}

type channelLastErrorTimestampCollector struct {
	desc   *prometheus.Desc
	mu     sync.Mutex
	errors map[string]channelLastErrorSnapshot
}

type channelLastErrorSnapshot struct {
	labels       ChannelMetricLabels
	modelName    string
	errorType    string
	errorCode    string
	statusCode   string
	errorMessage string
	occurredAt   time.Time
}

var (
	defaultPrometheusMetrics     *PrometheusMetrics
	defaultPrometheusMetricsOnce sync.Once
)

func NewPrometheusMetrics(registerer prometheus.Registerer) (*PrometheusMetrics, error) {
	m := &PrometheusMetrics{
		now: time.Now,
		httpInflight: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: prometheusNamespace,
				Subsystem: "http",
				Name:      "inflight_requests",
				Help:      "Current in-flight HTTP requests handled by route tag.",
			},
			[]string{"route_tag"},
		),
		httpRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: prometheusNamespace,
				Subsystem: "http",
				Name:      "requests_total",
				Help:      "Total HTTP requests handled by route tag, method, and status class.",
			},
			[]string{"route_tag", "method", "status_class"},
		),
		httpDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: prometheusNamespace,
				Subsystem: "http",
				Name:      "request_duration_seconds",
				Help:      "HTTP request duration in seconds by route tag and method.",
				Buckets:   durationBuckets(),
			},
			[]string{"route_tag", "method"},
		),
		channelInflight: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: prometheusNamespace,
				Subsystem: "channel",
				Name:      "inflight_requests",
				Help:      "Current in-flight upstream requests per channel.",
			},
			[]string{"channel_id", "channel_name", "channel_type", "request_kind"},
		),
		channelRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: prometheusNamespace,
				Subsystem: "channel",
				Name:      "requests_total",
				Help:      "Total upstream channel attempts by result.",
			},
			[]string{"channel_id", "channel_name", "channel_type", "request_kind", "result"},
		),
		channelModelRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: prometheusNamespace,
				Subsystem: "channel_model",
				Name:      "requests_total",
				Help:      "Total upstream channel attempts by model and result.",
			},
			[]string{"channel_id", "channel_name", "channel_type", "request_kind", "model_name", "result"},
		),
		channelErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: prometheusNamespace,
				Subsystem: "channel",
				Name:      "errors_total",
				Help:      "Total upstream channel errors by error code and HTTP status code.",
			},
			[]string{"channel_id", "channel_name", "channel_type", "request_kind", "error_code", "status_code"},
		),
		channelAttemptDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: prometheusNamespace,
				Subsystem: "channel",
				Name:      "attempt_duration_seconds",
				Help:      "End-to-end duration of each upstream channel attempt in seconds.",
				Buckets:   durationBuckets(),
			},
			[]string{"channel_id", "channel_name", "channel_type", "request_kind", "result"},
		),
		channelFirstResponseDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: prometheusNamespace,
				Subsystem: "channel",
				Name:      "first_response_duration_seconds",
				Help:      "Latency from request start to first streamed response chunk in seconds.",
				Buckets:   durationBuckets(),
			},
			[]string{"channel_id", "channel_name", "channel_type", "request_kind"},
		),
		channelUpstreamLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: prometheusNamespace,
				Subsystem: "channel",
				Name:      "upstream_latency_seconds",
				Help:      "Latency of outbound upstream requests measured around client.Do.",
				Buckets:   durationBuckets(),
			},
			[]string{"channel_id", "channel_name", "channel_type", "request_kind"},
		),
		systemPerformanceRejections: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: prometheusNamespace,
				Subsystem: "system",
				Name:      "performance_rejections_total",
				Help:      "Total relay requests rejected by system performance protection.",
			},
			[]string{"reason"},
		),
		systemStatusCollector: newSystemStatusCollector(),
	}
	m.channelRequestsLast1m = newChannelSlidingWindowCollector(
		"requests_last_1m",
		"Exact count of upstream requests during the last 60 seconds per channel.",
		func() time.Time {
			return m.now()
		},
	)
	m.channelSuccessRPMLast1m = newChannelSlidingWindowCollector(
		"success_rpm_last_1m",
		"Exact count of successful upstream requests during the last 60 seconds per channel.",
		func() time.Time {
			return m.now()
		},
	)
	m.channelLastErrorTimestamp = newChannelLastErrorTimestampCollector()

	collectors := []prometheus.Collector{
		m.httpInflight,
		m.httpRequests,
		m.httpDuration,
		m.channelInflight,
		m.channelRequests,
		m.channelModelRequests,
		m.channelRequestsLast1m,
		m.channelSuccessRPMLast1m,
		m.channelErrors,
		m.channelLastErrorTimestamp,
		m.channelAttemptDuration,
		m.channelFirstResponseDuration,
		m.channelUpstreamLatency,
		m.systemPerformanceRejections,
		m.systemStatusCollector,
	}

	for _, collector := range collectors {
		if err := registerer.Register(collector); err != nil {
			return nil, err
		}
	}

	return m, nil
}

func InitPrometheusMetrics() {
	defaultPrometheusMetricsOnce.Do(func() {
		metrics, err := NewPrometheusMetrics(prometheus.DefaultRegisterer)
		if err != nil {
			SysError("failed to initialize prometheus metrics: " + err.Error())
			return
		}
		defaultPrometheusMetrics = metrics
	})
}

func BeginChannelAttempt(c *gin.Context, requestKind string) *channelAttemptObserver {
	if defaultPrometheusMetrics == nil {
		return &channelAttemptObserver{}
	}
	labels, ok := ChannelLabelsFromContext(c, requestKind)
	if !ok {
		return &channelAttemptObserver{}
	}
	return defaultPrometheusMetrics.BeginChannelAttemptWithModel(labels, ChannelModelNameFromContext(c))
}

func ObserveChannelUpstreamLatency(c *gin.Context, requestKind string, duration time.Duration) {
	if defaultPrometheusMetrics == nil {
		return
	}
	labels, ok := ChannelLabelsFromContext(c, requestKind)
	if !ok {
		return
	}
	defaultPrometheusMetrics.channelUpstreamLatency.WithLabelValues(
		labels.ChannelID,
		labels.ChannelName,
		labels.ChannelType,
		labels.RequestKind,
	).Observe(duration.Seconds())
}

func ObserveChannelFirstResponseDuration(c *gin.Context, requestKind string, duration time.Duration) {
	if defaultPrometheusMetrics == nil {
		return
	}
	labels, ok := ChannelLabelsFromContext(c, requestKind)
	if !ok {
		return
	}
	defaultPrometheusMetrics.ObserveChannelFirstResponseDuration(labels, duration)
}

func ObserveChannelLastError(c *gin.Context, requestKind string, detail ChannelErrorDetail) {
	if defaultPrometheusMetrics == nil {
		return
	}
	labels, ok := ChannelLabelsFromContext(c, requestKind)
	if !ok {
		return
	}
	defaultPrometheusMetrics.ObserveChannelLastError(labels, detail)
}

func ObserveSystemPerformanceRejection(reason string) {
	if defaultPrometheusMetrics == nil {
		return
	}
	defaultPrometheusMetrics.ObserveSystemPerformanceRejection(reason)
}

func GetPrometheusHTTPObserver(routeTag string, method string) *httpRequestObserver {
	if defaultPrometheusMetrics == nil {
		return &httpRequestObserver{}
	}
	return defaultPrometheusMetrics.BeginHTTPRequest(HTTPMetricLabels{
		RouteTag: routeTag,
		Method:   method,
	})
}

func PrometheusHandler() gin.HandlerFunc {
	handler := promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{})
	return func(c *gin.Context) {
		config := GetPrometheusConfig()
		if config.BearerToken != "" {
			token := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
			if token != config.BearerToken {
				c.Header("WWW-Authenticate", `Bearer realm="metrics"`)
				c.AbortWithStatus(http.StatusUnauthorized)
				return
			}
		}
		handler.ServeHTTP(c.Writer, c.Request)
	}
}

func ChannelLabelsFromContext(c *gin.Context, requestKind string) (ChannelMetricLabels, bool) {
	if c == nil {
		return ChannelMetricLabels{}, false
	}
	channelID := c.GetInt("channel_id")
	if channelID <= 0 {
		return ChannelMetricLabels{}, false
	}
	channelName := strings.TrimSpace(c.GetString("channel_name"))
	if channelName == "" {
		channelName = "unknown"
	}
	channelType := c.GetInt("channel_type")
	return ChannelMetricLabels{
		ChannelID:   strconv.Itoa(channelID),
		ChannelName: channelName,
		ChannelType: strconv.Itoa(channelType),
		RequestKind: normalizeRequestKind(requestKind),
	}, true
}

func ChannelModelNameFromContext(c *gin.Context) string {
	if c == nil {
		return "unknown"
	}
	return normalizePrometheusLabelValue(c.GetString("original_model"), "unknown", 160)
}

func MetricsRequestKindFromPath(path string) string {
	path = strings.TrimSpace(path)
	switch {
	case path == "":
		return "relay"
	case strings.HasPrefix(path, "/v1/realtime"):
		return "realtime"
	case strings.HasPrefix(path, "/pg/"):
		return "playground"
	case strings.HasPrefix(path, "/suno/"),
		strings.HasPrefix(path, "/kling/"),
		strings.HasPrefix(path, "/jimeng"),
		strings.HasPrefix(path, "/v1/video"),
		strings.HasPrefix(path, "/v1/videos"):
		return "task"
	case strings.HasPrefix(path, "/mj/") || strings.Contains(path, "/mj/"):
		return "midjourney"
	default:
		return "relay"
	}
}

func MetricsRouteTag(path string, routeTag string) string {
	routeTag = strings.TrimSpace(routeTag)
	if routeTag != "" {
		return routeTag
	}
	config := GetPrometheusConfig()
	switch {
	case strings.TrimSpace(path) == config.Path:
		return "metrics"
	case strings.HasPrefix(path, "/api"):
		return "api"
	case strings.HasPrefix(path, "/v1"),
		strings.HasPrefix(path, "/v1beta"),
		strings.HasPrefix(path, "/pg"),
		strings.HasPrefix(path, "/mj"),
		strings.Contains(path, "/mj/"),
		strings.HasPrefix(path, "/suno"),
		strings.HasPrefix(path, "/kling"),
		strings.HasPrefix(path, "/jimeng"):
		return "relay"
	default:
		return "web"
	}
}

func (m *PrometheusMetrics) BeginChannelAttempt(labels ChannelMetricLabels) *channelAttemptObserver {
	return m.BeginChannelAttemptWithModel(labels, "")
}

func (m *PrometheusMetrics) BeginChannelAttemptWithModel(labels ChannelMetricLabels, modelName string) *channelAttemptObserver {
	labels = normalizeChannelMetricLabels(labels)
	modelName = normalizePrometheusLabelValue(modelName, "unknown", 160)
	m.channelInflight.WithLabelValues(
		labels.ChannelID,
		labels.ChannelName,
		labels.ChannelType,
		labels.RequestKind,
	).Inc()
	return &channelAttemptObserver{
		metrics:   m,
		labels:    labels,
		modelName: modelName,
		start:     time.Now(),
		active:    true,
	}
}

func (m *PrometheusMetrics) BeginHTTPRequest(labels HTTPMetricLabels) *httpRequestObserver {
	labels = normalizeHTTPMetricLabels(labels)
	m.httpInflight.WithLabelValues(labels.RouteTag).Inc()
	return &httpRequestObserver{
		metrics: m,
		labels:  labels,
		start:   time.Now(),
		active:  true,
	}
}

func (m *PrometheusMetrics) ObserveSystemPerformanceRejection(reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unknown"
	}
	m.systemPerformanceRejections.WithLabelValues(reason).Inc()
}

func (m *PrometheusMetrics) ObserveChannelLastError(labels ChannelMetricLabels, detail ChannelErrorDetail) {
	labels = normalizeChannelMetricLabels(labels)
	detail = normalizeChannelErrorDetail(detail)

	collector, ok := m.channelLastErrorTimestamp.(*channelLastErrorTimestampCollector)
	if !ok {
		return
	}
	collector.Record(labels, detail, m.now())
}

func (m *PrometheusMetrics) ObserveChannelFirstResponseDuration(labels ChannelMetricLabels, duration time.Duration) {
	if duration <= 0 {
		return
	}
	labels = normalizeChannelMetricLabels(labels)
	m.channelFirstResponseDuration.WithLabelValues(
		labels.ChannelID,
		labels.ChannelName,
		labels.ChannelType,
		labels.RequestKind,
	).Observe(duration.Seconds())
}

func (o *channelAttemptObserver) Done(result ChannelAttemptResult) {
	if o == nil || o.metrics == nil || !o.active {
		return
	}
	o.active = false

	labels := o.labels
	res := strings.TrimSpace(result.Result)
	if res == "" {
		res = ChannelRequestResultError
	}

	o.metrics.channelInflight.WithLabelValues(
		labels.ChannelID,
		labels.ChannelName,
		labels.ChannelType,
		labels.RequestKind,
	).Dec()

	o.metrics.channelRequests.WithLabelValues(
		labels.ChannelID,
		labels.ChannelName,
		labels.ChannelType,
		labels.RequestKind,
		res,
	).Inc()
	o.metrics.channelModelRequests.WithLabelValues(
		labels.ChannelID,
		labels.ChannelName,
		labels.ChannelType,
		labels.RequestKind,
		o.modelName,
		res,
	).Inc()
	if collector, ok := o.metrics.channelRequestsLast1m.(*channelSlidingWindowCollector); ok {
		collector.Record(labels)
	}
	if res == ChannelRequestResultSuccess {
		if collector, ok := o.metrics.channelSuccessRPMLast1m.(*channelSlidingWindowCollector); ok {
			collector.Record(labels)
		}
	}

	o.metrics.channelAttemptDuration.WithLabelValues(
		labels.ChannelID,
		labels.ChannelName,
		labels.ChannelType,
		labels.RequestKind,
		res,
	).Observe(time.Since(o.start).Seconds())

	if res == ChannelRequestResultError {
		errorCode := strings.TrimSpace(result.ErrorCode)
		if errorCode == "" {
			errorCode = "unknown"
		}
		statusCode := "0"
		if result.StatusCode > 0 {
			statusCode = strconv.Itoa(result.StatusCode)
		}
		o.metrics.channelErrors.WithLabelValues(
			labels.ChannelID,
			labels.ChannelName,
			labels.ChannelType,
			labels.RequestKind,
			errorCode,
			statusCode,
		).Inc()
	}
}

func (o *httpRequestObserver) Done(statusCode int) {
	if o == nil || o.metrics == nil || !o.active {
		return
	}
	o.active = false

	labels := normalizeHTTPMetricLabels(o.labels)
	if statusCode <= 0 {
		statusCode = http.StatusOK
	}
	statusClass := strconv.Itoa(statusCode/100) + "xx"

	o.metrics.httpInflight.WithLabelValues(labels.RouteTag).Dec()
	o.metrics.httpRequests.WithLabelValues(labels.RouteTag, labels.Method, statusClass).Inc()
	o.metrics.httpDuration.WithLabelValues(labels.RouteTag, labels.Method).Observe(time.Since(o.start).Seconds())
}

func newSystemStatusCollector() prometheus.Collector {
	return &systemStatusCollector{
		cpuUsageDesc: prometheus.NewDesc(
			prometheus.BuildFQName(prometheusNamespace, "system", "cpu_usage_percent"),
			"Current host CPU usage percent from the cached system monitor.",
			nil, nil,
		),
		memoryUsageDesc: prometheus.NewDesc(
			prometheus.BuildFQName(prometheusNamespace, "system", "memory_usage_percent"),
			"Current host memory usage percent from the cached system monitor.",
			nil, nil,
		),
		diskUsageDesc: prometheus.NewDesc(
			prometheus.BuildFQName(prometheusNamespace, "system", "disk_usage_percent"),
			"Current host disk usage percent from the cached system monitor.",
			nil, nil,
		),
		monitorEnabledDesc: prometheus.NewDesc(
			prometheus.BuildFQName(prometheusNamespace, "system", "monitor_enabled"),
			"Whether the built-in system performance monitor is enabled.",
			nil, nil,
		),
		thresholdDesc: prometheus.NewDesc(
			prometheus.BuildFQName(prometheusNamespace, "system", "threshold_percent"),
			"Configured system performance protection threshold percent by resource.",
			[]string{"resource"}, nil,
		),
	}
}

func (c *systemStatusCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.cpuUsageDesc
	ch <- c.memoryUsageDesc
	ch <- c.diskUsageDesc
	ch <- c.monitorEnabledDesc
	ch <- c.thresholdDesc
}

func (c *systemStatusCollector) Collect(ch chan<- prometheus.Metric) {
	status := GetSystemStatus()
	config := GetPerformanceMonitorConfig()
	enabled := 0.0
	if config.Enabled {
		enabled = 1
	}

	ch <- prometheus.MustNewConstMetric(c.cpuUsageDesc, prometheus.GaugeValue, status.CPUUsage)
	ch <- prometheus.MustNewConstMetric(c.memoryUsageDesc, prometheus.GaugeValue, status.MemoryUsage)
	ch <- prometheus.MustNewConstMetric(c.diskUsageDesc, prometheus.GaugeValue, status.DiskUsage)
	ch <- prometheus.MustNewConstMetric(c.monitorEnabledDesc, prometheus.GaugeValue, enabled)
	ch <- prometheus.MustNewConstMetric(c.thresholdDesc, prometheus.GaugeValue, float64(config.CPUThreshold), "cpu")
	ch <- prometheus.MustNewConstMetric(c.thresholdDesc, prometheus.GaugeValue, float64(config.MemoryThreshold), "memory")
	ch <- prometheus.MustNewConstMetric(c.thresholdDesc, prometheus.GaugeValue, float64(config.DiskThreshold), "disk")
}

func normalizeChannelMetricLabels(labels ChannelMetricLabels) ChannelMetricLabels {
	if strings.TrimSpace(labels.ChannelID) == "" {
		labels.ChannelID = "0"
	}
	if strings.TrimSpace(labels.ChannelName) == "" {
		labels.ChannelName = "unknown"
	}
	if strings.TrimSpace(labels.ChannelType) == "" {
		labels.ChannelType = "0"
	}
	labels.RequestKind = normalizeRequestKind(labels.RequestKind)
	return labels
}

func normalizeHTTPMetricLabels(labels HTTPMetricLabels) HTTPMetricLabels {
	labels.RouteTag = strings.TrimSpace(labels.RouteTag)
	if labels.RouteTag == "" {
		labels.RouteTag = "web"
	}
	labels.Method = strings.ToUpper(strings.TrimSpace(labels.Method))
	if labels.Method == "" {
		labels.Method = http.MethodGet
	}
	return labels
}

func normalizeChannelErrorDetail(detail ChannelErrorDetail) ChannelErrorDetail {
	detail.ModelName = normalizePrometheusLabelValue(detail.ModelName, "unknown", 120)
	detail.ErrorType = normalizePrometheusLabelValue(detail.ErrorType, "unknown", 80)
	detail.ErrorCode = normalizePrometheusLabelValue(detail.ErrorCode, "unknown", 120)
	detail.ErrorMessage = normalizePrometheusErrorMessage(detail.ErrorMessage)
	return detail
}

func normalizeRequestKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "relay"
	}
	return kind
}

func durationBuckets() []float64 {
	return []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60}
}

func newChannelSlidingWindowCollector(metricName string, help string, now func() time.Time) *channelSlidingWindowCollector {
	return &channelSlidingWindowCollector{
		desc: prometheus.NewDesc(
			prometheus.BuildFQName(prometheusNamespace, "channel", metricName),
			help,
			[]string{"channel_id", "channel_name", "channel_type", "request_kind"},
			nil,
		),
		now:     now,
		windows: make(map[string]*channelSlidingWindow),
	}
}

func newChannelLastErrorTimestampCollector() *channelLastErrorTimestampCollector {
	return &channelLastErrorTimestampCollector{
		desc: prometheus.NewDesc(
			prometheus.BuildFQName(prometheusNamespace, "channel", "last_error_timestamp_seconds"),
			"Unix timestamp of the latest upstream channel error for each channel, request kind, and model.",
			[]string{"channel_id", "channel_name", "channel_type", "request_kind", "model_name", "error_type", "error_code", "status_code", "error_message"},
			nil,
		),
		errors: make(map[string]channelLastErrorSnapshot),
	}
}

func (c *channelSlidingWindowCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

func (c *channelLastErrorTimestampCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

func (c *channelSlidingWindowCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	for _, window := range c.windows {
		window.prune(now)
		ch <- prometheus.MustNewConstMetric(
			c.desc,
			prometheus.GaugeValue,
			float64(len(window.timestamps)),
			window.labels.ChannelID,
			window.labels.ChannelName,
			window.labels.ChannelType,
			window.labels.RequestKind,
		)
	}
}

func (c *channelLastErrorTimestampCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, snapshot := range c.errors {
		ch <- prometheus.MustNewConstMetric(
			c.desc,
			prometheus.GaugeValue,
			float64(snapshot.occurredAt.Unix()),
			snapshot.labels.ChannelID,
			snapshot.labels.ChannelName,
			snapshot.labels.ChannelType,
			snapshot.labels.RequestKind,
			snapshot.modelName,
			snapshot.errorType,
			snapshot.errorCode,
			snapshot.statusCode,
			snapshot.errorMessage,
		)
	}
}

func (c *channelSlidingWindowCollector) Record(labels ChannelMetricLabels) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	key := channelSlidingWindowKey(labels)
	window, ok := c.windows[key]
	if !ok {
		window = &channelSlidingWindow{
			labels: labels,
		}
		c.windows[key] = window
	}
	window.timestamps = append(window.timestamps, now)
	window.prune(now)
}

func (c *channelLastErrorTimestampCollector) Record(labels ChannelMetricLabels, detail ChannelErrorDetail, occurredAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	statusCode := "0"
	if detail.StatusCode > 0 {
		statusCode = strconv.Itoa(detail.StatusCode)
	}
	key := channelLastErrorKey(labels, detail.ModelName)
	c.errors[key] = channelLastErrorSnapshot{
		labels:       labels,
		modelName:    detail.ModelName,
		errorType:    detail.ErrorType,
		errorCode:    detail.ErrorCode,
		statusCode:   statusCode,
		errorMessage: detail.ErrorMessage,
		occurredAt:   occurredAt,
	}
}

func (w *channelSlidingWindow) prune(now time.Time) {
	cutoff := now.Add(-1 * time.Minute)
	index := 0
	for index < len(w.timestamps) && w.timestamps[index].Before(cutoff) {
		index++
	}
	if index == 0 {
		return
	}
	w.timestamps = append([]time.Time(nil), w.timestamps[index:]...)
}

func channelSlidingWindowKey(labels ChannelMetricLabels) string {
	return strings.Join([]string{
		labels.ChannelID,
		labels.ChannelName,
		labels.ChannelType,
		labels.RequestKind,
	}, "\x00")
}

func channelLastErrorKey(labels ChannelMetricLabels, modelName string) string {
	return strings.Join([]string{
		labels.ChannelID,
		labels.ChannelName,
		labels.ChannelType,
		labels.RequestKind,
		modelName,
	}, "\x00")
}

func normalizePrometheusLabelValue(value string, fallback string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	return truncateRunes(value, maxRunes)
}

func normalizePrometheusErrorMessage(message string) string {
	fields := strings.Fields(strings.TrimSpace(message))
	if len(fields) == 0 {
		return "unknown"
	}
	return truncateRunes(strings.Join(fields, " "), maxPrometheusErrorMessageRunes)
}

func truncateRunes(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}
