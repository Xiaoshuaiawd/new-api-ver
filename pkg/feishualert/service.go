package feishualert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	maxAlertmanagerBodyBytes = 1 << 20
	maxFeishuResponseBytes   = 64 << 10
	feishuWebhookPathPrefix  = "/open-apis/bot/v2/hook/"
)

type ServiceConfig struct {
	WebhookURL string
	HTTPClient *http.Client
	Registry   *prometheus.Registry
	Logger     *slog.Logger
	Now        func() time.Time
	Location   *time.Location
}

type Service struct {
	webhookURL string
	client     *http.Client
	logger     *slog.Logger
	now        func() time.Time
	location   *time.Location
	handler    http.Handler
	ready      atomic.Bool
	metrics    serviceMetrics
}

type serviceMetrics struct {
	requests         *prometheus.CounterVec
	deliveries       *prometheus.CounterVec
	deliveryDuration prometheus.Histogram
	alerts           *prometheus.CounterVec
	configured       prometheus.Gauge
}

type feishuResponse struct {
	Code *int   `json:"code"`
	Msg  string `json:"msg"`
}

func NewService(config ServiceConfig) (*Service, error) {
	if err := validateWebhookURL(config.WebhookURL); err != nil {
		return nil, err
	}
	registry := config.Registry
	if registry == nil {
		registry = prometheus.NewRegistry()
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	location := config.Location
	if location == nil {
		location = time.UTC
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	} else {
		clientCopy := *client
		client = &clientCopy
	}
	if client.Timeout <= 0 {
		client.Timeout = 10 * time.Second
	}
	if client.CheckRedirect == nil {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	metrics, err := registerServiceMetrics(registry)
	if err != nil {
		return nil, err
	}
	metrics.configured.Set(1)
	service := &Service{
		webhookURL: strings.TrimSpace(config.WebhookURL),
		client:     client,
		logger:     logger,
		now:        now,
		location:   location,
		metrics:    metrics,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/alerts", service.handleAlerts)
	mux.HandleFunc("/healthz", service.handleHealth)
	mux.HandleFunc("/readyz", service.handleReady)
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	service.handler = mux
	service.ready.Store(true)
	return service, nil
}

func (service *Service) Handler() http.Handler {
	return service.handler
}

func (service *Service) handleAlerts(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		service.rejectRequest(response, http.StatusMethodNotAllowed, "仅支持 POST 请求")
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		service.rejectRequest(response, http.StatusUnsupportedMediaType, "Content-Type 必须为 application/json")
		return
	}

	request.Body = http.MaxBytesReader(response, request.Body, maxAlertmanagerBodyBytes)
	var message WebhookMessage
	if err := common.DecodeJson(request.Body, &message); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			service.rejectRequest(response, http.StatusRequestEntityTooLarge, "请求体超过限制")
			return
		}
		service.rejectRequest(response, http.StatusBadRequest, "无法解析 Alertmanager 请求")
		return
	}
	if message.Version != "4" {
		service.rejectRequest(response, http.StatusBadRequest, "仅支持 Alertmanager Webhook v4")
		return
	}
	if len(message.Alerts) == 0 {
		service.rejectRequest(response, http.StatusBadRequest, "告警列表不能为空")
		return
	}

	card, meta, err := BuildCard(message, service.now(), service.location)
	if err != nil {
		service.rejectRequest(response, http.StatusBadRequest, "无法生成飞书告警卡片")
		return
	}
	service.metrics.requests.WithLabelValues("accepted").Inc()
	service.metrics.alerts.WithLabelValues(meta.Severity, meta.Status).Add(float64(meta.AlertCount))

	started := time.Now()
	result, statusCode, feishuCode := service.deliver(request.Context(), card)
	service.metrics.deliveryDuration.Observe(time.Since(started).Seconds())
	service.metrics.deliveries.WithLabelValues(result, meta.Severity, meta.Status).Inc()
	if result != "success" {
		service.logger.Error(
			"飞书告警投递失败",
			"result", result,
			"http_status", statusCode,
			"feishu_code", feishuCode,
			"severity", meta.Severity,
			"status", meta.Status,
			"alert_count", meta.AlertCount,
		)
		http.Error(response, "飞书告警投递失败", http.StatusBadGateway)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (service *Service) deliver(ctx context.Context, card CardMessage) (string, int, int) {
	payload, err := common.Marshal(card)
	if err != nil {
		return "feishu_error", 0, 0
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, service.webhookURL, bytes.NewReader(payload))
	if err != nil {
		return "network_error", 0, 0
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")

	response, err := service.client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return "timeout", 0, 0
		}
		return "network_error", 0, 0
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxFeishuResponseBytes))
		return "http_error", response.StatusCode, 0
	}

	var result feishuResponse
	if err := common.DecodeJson(io.LimitReader(response.Body, maxFeishuResponseBytes), &result); err != nil || result.Code == nil {
		return "feishu_error", response.StatusCode, 0
	}
	if *result.Code != 0 {
		return "feishu_error", response.StatusCode, *result.Code
	}
	return "success", response.StatusCode, 0
}

func (service *Service) handleHealth(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	response.WriteHeader(http.StatusOK)
}

func (service *Service) handleReady(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !service.ready.Load() {
		response.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	response.WriteHeader(http.StatusOK)
}

func (service *Service) rejectRequest(response http.ResponseWriter, statusCode int, message string) {
	service.metrics.requests.WithLabelValues("rejected").Inc()
	http.Error(response, message, statusCode)
}

func validateWebhookURL(rawURL string) error {
	parsedURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("飞书 Webhook URL 格式无效")
	}
	if parsedURL.Scheme != "https" {
		return fmt.Errorf("飞书 Webhook URL 必须使用 HTTPS")
	}
	if parsedURL.Hostname() != "open.feishu.cn" || parsedURL.Port() != "" {
		return fmt.Errorf("飞书 Webhook URL 主机无效")
	}
	if parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return fmt.Errorf("飞书 Webhook URL 包含不允许的组件")
	}
	token := strings.TrimPrefix(parsedURL.EscapedPath(), feishuWebhookPathPrefix)
	if token == parsedURL.EscapedPath() || token == "" || strings.Contains(token, "/") {
		return fmt.Errorf("飞书 Webhook URL 路径无效")
	}
	return nil
}

func registerServiceMetrics(registry *prometheus.Registry) (serviceMetrics, error) {
	metrics := serviceMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_feishu_alert_requests_total",
			Help: "Total Alertmanager webhook requests received by the Feishu alert bridge.",
		}, []string{"result"}),
		deliveries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_feishu_alert_deliveries_total",
			Help: "Total Feishu alert delivery attempts by fixed result, severity, and status.",
		}, []string{"result", "severity", "status"}),
		deliveryDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "newapi_feishu_alert_delivery_duration_seconds",
			Help:    "Feishu alert delivery latency in seconds.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
		}),
		alerts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_feishu_alerts_total",
			Help: "Total alerts converted for Feishu delivery.",
		}, []string{"severity", "status"}),
		configured: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "newapi_feishu_alert_webhook_configured",
			Help: "Whether the Feishu webhook URL was loaded and validated.",
		}),
	}
	collectors := []prometheus.Collector{
		metrics.requests,
		metrics.deliveries,
		metrics.deliveryDuration,
		metrics.alerts,
		metrics.configured,
	}
	for _, collector := range collectors {
		if err := registry.Register(collector); err != nil {
			return serviceMetrics{}, fmt.Errorf("注册飞书告警指标失败: %w", err)
		}
	}
	return metrics, nil
}
