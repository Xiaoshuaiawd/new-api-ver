package feishualert

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestNewServiceValidatesWebhookURL(t *testing.T) {
	tests := []struct {
		name       string
		webhookURL string
		wantError  bool
	}{
		{name: "valid", webhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/test-token"},
		{name: "http", webhookURL: "http://open.feishu.cn/open-apis/bot/v2/hook/test-token", wantError: true},
		{name: "wrong host", webhookURL: "https://example.com/open-apis/bot/v2/hook/test-token", wantError: true},
		{name: "wrong path", webhookURL: "https://open.feishu.cn/open-apis/other/test-token", wantError: true},
		{name: "empty token", webhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/", wantError: true},
		{name: "userinfo", webhookURL: "https://user:pass@open.feishu.cn/open-apis/bot/v2/hook/test-token", wantError: true},
		{name: "query", webhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/test-token?secret=value", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, err := NewService(ServiceConfig{WebhookURL: test.webhookURL})
			if test.wantError {
				require.Error(t, err)
				assert.Nil(t, service)
				assert.NotContains(t, err.Error(), "test-token")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, service)
		})
	}
}

func TestServiceDeliversCriticalAlertAndPublishesMetrics(t *testing.T) {
	var calls atomic.Int32
	var delivered CardMessage
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "application/json; charset=utf-8", request.Header.Get("Content-Type"))
		require.NoError(t, common.DecodeJson(request.Body, &delivered))
		return jsonResponse(http.StatusOK, `{"code":0,"msg":"success"}`), nil
	})
	service := newTestService(t, transport, nil)

	request := alertRequest(t, validWebhookMessage("firing", "critical"))
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	assert.Equal(t, int32(1), calls.Load())
	assert.Equal(t, "red", delivered.Card.Header.Template)
	assert.Contains(t, delivered.Card.Body.Elements[0].Content, "<at id=all></at>")

	metrics := scrapeServiceMetrics(t, service)
	assert.Contains(t, metrics, `newapi_feishu_alert_requests_total{result="accepted"} 1`)
	assert.Contains(t, metrics, `newapi_feishu_alert_deliveries_total{result="success",severity="critical",status="firing"} 1`)
	assert.Contains(t, metrics, `newapi_feishu_alerts_total{severity="critical",status="firing"} 1`)
	assert.Contains(t, metrics, `newapi_feishu_alert_webhook_configured 1`)
	assert.NotContains(t, metrics, "TestAlert")
	assert.NotContains(t, metrics, "channel_id")
}

func TestServiceDeliversFiringAndResolvedCards(t *testing.T) {
	var delivered []CardMessage
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var card CardMessage
		require.NoError(t, common.DecodeJson(request.Body, &card))
		delivered = append(delivered, card)
		return jsonResponse(http.StatusOK, `{"code":0,"msg":"success"}`), nil
	})
	service := newTestService(t, transport, nil)

	for _, status := range []string{"firing", "resolved"} {
		response := httptest.NewRecorder()
		service.Handler().ServeHTTP(response, alertRequest(t, validWebhookMessage(status, "critical")))
		require.Equal(t, http.StatusNoContent, response.Code)
	}

	require.Len(t, delivered, 2)
	assert.Equal(t, "red", delivered[0].Card.Header.Template)
	assert.Contains(t, delivered[0].Card.Body.Elements[0].Content, "<at id=all></at>")
	assert.Equal(t, "green", delivered[1].Card.Header.Template)
	assert.NotContains(t, delivered[1].Card.Body.Elements[0].Content, "<at id=all></at>")

	metrics := scrapeServiceMetrics(t, service)
	assert.Contains(t, metrics, `newapi_feishu_alert_deliveries_total{result="success",severity="critical",status="firing"} 1`)
	assert.Contains(t, metrics, `newapi_feishu_alert_deliveries_total{result="success",severity="critical",status="resolved"} 1`)
}

func TestServiceRejectsInvalidAlertmanagerRequests(t *testing.T) {
	var calls atomic.Int32
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return jsonResponse(http.StatusOK, `{"code":0}`), nil
	})
	service := newTestService(t, transport, nil)

	tests := []struct {
		name       string
		request    func(t *testing.T) *http.Request
		statusCode int
	}{
		{
			name: "method",
			request: func(t *testing.T) *http.Request {
				return httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
			},
			statusCode: http.StatusMethodNotAllowed,
		},
		{
			name: "content type",
			request: func(t *testing.T) *http.Request {
				request := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", strings.NewReader(`{}`))
				request.Header.Set("Content-Type", "text/plain")
				return request
			},
			statusCode: http.StatusUnsupportedMediaType,
		},
		{
			name: "json",
			request: func(t *testing.T) *http.Request {
				request := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", strings.NewReader(`{"version":`))
				request.Header.Set("Content-Type", "application/json")
				return request
			},
			statusCode: http.StatusBadRequest,
		},
		{
			name: "version",
			request: func(t *testing.T) *http.Request {
				message := validWebhookMessage("firing", "warning")
				message.Version = "3"
				return alertRequest(t, message)
			},
			statusCode: http.StatusBadRequest,
		},
		{
			name: "empty alerts",
			request: func(t *testing.T) *http.Request {
				message := validWebhookMessage("firing", "warning")
				message.Alerts = nil
				return alertRequest(t, message)
			},
			statusCode: http.StatusBadRequest,
		},
		{
			name: "too large",
			request: func(t *testing.T) *http.Request {
				body := `{"version":"4","status":"firing","alerts":[{"annotations":{"summary":"` + strings.Repeat("x", maxAlertmanagerBodyBytes) + `"}}]}`
				request := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				return request
			},
			statusCode: http.StatusRequestEntityTooLarge,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			service.Handler().ServeHTTP(response, test.request(t))
			assert.Equal(t, test.statusCode, response.Code)
		})
	}

	assert.Equal(t, int32(0), calls.Load())
	metrics := scrapeServiceMetrics(t, service)
	assert.Contains(t, metrics, `newapi_feishu_alert_requests_total{result="rejected"} 6`)
}

func TestServiceClassifiesDeliveryFailuresWithoutLeakingWebhook(t *testing.T) {
	tests := []struct {
		name      string
		transport roundTripFunc
		result    string
	}{
		{
			name: "http error",
			transport: func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusTooManyRequests, `{"code":11232,"msg":"rate limited"}`), nil
			},
			result: "http_error",
		},
		{
			name: "feishu error",
			transport: func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, `{"code":19022,"msg":"Ip Not Allowed"}`), nil
			},
			result: "feishu_error",
		},
		{
			name: "invalid response",
			transport: func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, `not-json`), nil
			},
			result: "feishu_error",
		},
		{
			name: "network error",
			transport: func(request *http.Request) (*http.Response, error) {
				return nil, errors.New("dial failed for " + request.URL.String())
			},
			result: "network_error",
		},
		{
			name: "timeout",
			transport: func(request *http.Request) (*http.Response, error) {
				<-request.Context().Done()
				return nil, request.Context().Err()
			},
			result: "timeout",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			client := &http.Client{Transport: test.transport, Timeout: 10 * time.Millisecond}
			service := newTestServiceWithClient(t, client, &logs)
			response := httptest.NewRecorder()
			service.Handler().ServeHTTP(response, alertRequest(t, validWebhookMessage("firing", "warning")))

			assert.Equal(t, http.StatusBadGateway, response.Code)
			assert.NotContains(t, response.Body.String(), "very-secret-token")
			assert.NotContains(t, logs.String(), "very-secret-token")
			metrics := scrapeServiceMetrics(t, service)
			assert.Contains(t, metrics, `newapi_feishu_alert_deliveries_total{result="`+test.result+`",severity="warning",status="firing"} 1`)
		})
	}
}

func TestServiceHealthEndpoints(t *testing.T) {
	service := newTestService(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"code":0}`), nil
	}), nil)

	for _, path := range []string{"/healthz", "/readyz"} {
		response := httptest.NewRecorder()
		service.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusOK, response.Code)
	}
}

func newTestService(t *testing.T, transport http.RoundTripper, logs *bytes.Buffer) *Service {
	t.Helper()
	return newTestServiceWithClient(t, &http.Client{Transport: transport, Timeout: time.Second}, logs)
}

func newTestServiceWithClient(t *testing.T, client *http.Client, logs *bytes.Buffer) *Service {
	t.Helper()
	if logs == nil {
		logs = &bytes.Buffer{}
	}
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	service, err := NewService(ServiceConfig{
		WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/very-secret-token",
		HTTPClient: client,
		Registry:   prometheus.NewRegistry(),
		Logger:     slog.New(slog.NewJSONHandler(logs, nil)),
		Now:        func() time.Time { return time.Date(2026, time.July, 29, 15, 30, 0, 0, time.UTC) },
		Location:   shanghai,
	})
	require.NoError(t, err)
	return service
}

func validWebhookMessage(status, severity string) WebhookMessage {
	now := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	return WebhookMessage{
		Version:  "4",
		Status:   status,
		Receiver: "new-api-webhook-" + severity,
		CommonLabels: map[string]string{
			"alertname": "TestAlert",
			"severity":  severity,
			"cluster":   "default",
			"job":       "new-api",
		},
		Alerts: []Alert{
			{
				Status: status,
				Labels: map[string]string{
					"alertname":  "TestAlert",
					"severity":   severity,
					"channel_id": "12",
				},
				Annotations: map[string]string{
					"summary":     "测试告警",
					"description": "测试说明",
				},
				StartsAt: now.Add(-5 * time.Minute),
				EndsAt:   now,
			},
		},
	}
}

func alertRequest(t *testing.T, message WebhookMessage) *http.Request {
	t.Helper()
	payload, err := common.Marshal(message)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	return request
}

func jsonResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func scrapeServiceMetrics(t *testing.T, service *Service) string {
	t.Helper()
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	return response.Body.String()
}
