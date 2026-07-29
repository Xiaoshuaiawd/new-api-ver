package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	prometheusmetrics "github.com/QuantumNous/new-api/pkg/prometheus_metrics"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrometheusHTTPRecordsOnlyTaggedBusinessTemplateRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtime, err := prometheusmetrics.NewRuntime(
		prometheusmetrics.Config{Enabled: true, AllowPublic: true},
		"v-test",
		nil,
		nil,
	)
	require.NoError(t, err)

	engine := gin.New()
	engine.Use(PrometheusHTTP(runtime))
	engine.POST("/api/users/:id", RouteTag("api"), func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})
	engine.GET("/assets/:name", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	engine.GET("/metrics", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	requests := []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/users/123?token=secret-token", nil),
		httptest.NewRequest(http.MethodGet, "/assets/app.js", nil),
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
		httptest.NewRequest(http.MethodGet, "/missing/456", nil),
	}
	for _, request := range requests {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
	}

	metrics := scrapeRuntimeMetrics(t, runtime)
	assert.Contains(t, metrics, "newapi_http_requests_total{method=\"POST\",route=\"/api/users/:id\",status_class=\"2xx\"} 1")
	assert.Contains(t, metrics, "newapi_http_request_duration_seconds_count{method=\"POST\",route=\"/api/users/:id\",status_class=\"2xx\"} 1")
	assert.NotContains(t, metrics, "/api/users/123")
	assert.NotContains(t, metrics, "secret-token")
	assert.NotContains(t, metrics, "/assets/")
	assert.NotContains(t, metrics, "route=\"/metrics\"")
	assert.NotContains(t, metrics, "/missing/")
}

func TestPrometheusHTTPRecordsRecoveredPanicsAsServerErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtime, err := prometheusmetrics.NewRuntime(
		prometheusmetrics.Config{Enabled: true, AllowPublic: true},
		"v-test",
		nil,
		nil,
	)
	require.NoError(t, err)

	engine := gin.New()
	engine.Use(PrometheusHTTP(runtime))
	engine.Use(gin.CustomRecovery(func(c *gin.Context, _ any) {
		c.Status(http.StatusInternalServerError)
	}))
	engine.GET("/api/panic", RouteTag("api"), func(_ *gin.Context) {
		panic("expected test panic")
	})

	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/panic", nil))
	require.Equal(t, http.StatusInternalServerError, response.Code)

	metrics := scrapeRuntimeMetrics(t, runtime)
	assert.Contains(t, metrics, "newapi_http_requests_total{method=\"GET\",route=\"/api/panic\",status_class=\"5xx\"} 1")
}

func TestHTTPStatusClassUsesFixedLabels(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{status: 0, want: "other"},
		{status: 199, want: "other"},
		{status: 200, want: "2xx"},
		{status: 302, want: "3xx"},
		{status: 404, want: "4xx"},
		{status: 503, want: "5xx"},
		{status: 600, want: "other"},
	}
	for _, test := range tests {
		assert.Equal(t, test.want, httpStatusClass(test.status))
	}
}

func scrapeRuntimeMetrics(t *testing.T, runtime *prometheusmetrics.Runtime) string {
	t.Helper()
	response := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	return response.Body.String()
}
