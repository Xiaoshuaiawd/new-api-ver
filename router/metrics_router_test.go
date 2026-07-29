package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	prometheusmetrics "github.com/QuantumNous/new-api/pkg/prometheus_metrics"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetMetricsRouterDoesNotRegisterWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	SetMetricsRouter(engine, &prometheusmetrics.Runtime{})

	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Equal(t, http.StatusNotFound, response.Code)
}

func TestMetricsRouterRequiresConfiguredBearerToken(t *testing.T) {
	t.Setenv("PROMETHEUS_ENABLED", "true")
	t.Setenv("PROMETHEUS_BEARER_TOKEN", "metrics-secret")
	runtime := newMetricsRouterTestRuntime(t)
	engine := gin.New()
	SetMetricsRouter(engine, runtime)

	t.Run("rejects missing token without leaking policy", func(t *testing.T) {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))

		assert.Equal(t, http.StatusForbidden, response.Code)
		assert.Equal(t, "forbidden\n", response.Body.String())
	})

	t.Run("accepts valid token", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		request.Header.Set("Authorization", "Bearer metrics-secret")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Contains(t, response.Body.String(), "newapi_build_info")
	})
}

func TestMetricsRouterUsesGinTrustedClientIPForAllowlist(t *testing.T) {
	t.Setenv("PROMETHEUS_ENABLED", "true")
	t.Setenv("PROMETHEUS_ALLOWED_IPS", "10.0.0.0/8")
	runtime := newMetricsRouterTestRuntime(t)

	t.Run("trusted proxy address is accepted", func(t *testing.T) {
		engine := gin.New()
		require.NoError(t, engine.SetTrustedProxies([]string{"127.0.0.1"}))
		SetMetricsRouter(engine, runtime)

		request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		request.RemoteAddr = "127.0.0.1:12345"
		request.Header.Set("X-Forwarded-For", "10.23.4.5")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
	})

	t.Run("untrusted proxy header is ignored", func(t *testing.T) {
		engine := gin.New()
		require.NoError(t, engine.SetTrustedProxies([]string{"192.0.2.0/24"}))
		SetMetricsRouter(engine, runtime)

		request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		request.RemoteAddr = "127.0.0.1:12345"
		request.Header.Set("X-Forwarded-For", "10.23.4.5")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)

		assert.Equal(t, http.StatusForbidden, response.Code)
	})
}

func TestSetRouterRegistersMetricsBeforeRelayGlobalMiddleware(t *testing.T) {
	t.Setenv("PROMETHEUS_ENABLED", "true")
	t.Setenv("PROMETHEUS_ALLOW_PUBLIC", "true")
	t.Setenv("FRONTEND_BASE_URL", "https://frontend.example.com")
	runtime := newMetricsRouterTestRuntime(t)
	runtime.Handler = http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(writer, middleware.GetStats().ActiveConnections)
	})

	previousMaster := common.IsMasterNode
	common.IsMasterNode = false
	t.Cleanup(func() { common.IsMasterNode = previousMaster })

	engine := gin.New()
	SetRouter(engine, WebAssets{}, runtime)

	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "0", response.Body.String(), "relay StatsMiddleware must not wrap /metrics")
}

func newMetricsRouterTestRuntime(t *testing.T) *prometheusmetrics.Runtime {
	t.Helper()
	cfg, err := prometheusmetrics.LoadConfig()
	require.NoError(t, err)
	runtime, err := prometheusmetrics.NewRuntime(cfg, "v-test", nil, nil)
	require.NoError(t, err)
	return runtime
}
