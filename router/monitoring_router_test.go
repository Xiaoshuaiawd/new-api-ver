package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMetricsRouteRespectsConfigAndBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldConfig := common.GetPrometheusConfig()
	defer common.SetPrometheusConfig(oldConfig)

	common.InitPrometheusMetrics()

	t.Run("disabled", func(t *testing.T) {
		common.SetPrometheusConfig(common.PrometheusConfig{
			Enabled: false,
		})

		engine := gin.New()
		SetMonitoringRouter(engine)

		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		require.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("enabled with bearer token", func(t *testing.T) {
		common.SetPrometheusConfig(common.PrometheusConfig{
			Enabled:     true,
			Path:        "/metrics",
			BearerToken: "secret-token",
		})

		engine := gin.New()
		SetMonitoringRouter(engine)

		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)

		req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.Header.Set("Authorization", "Bearer secret-token")
		rec = httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.True(t, strings.Contains(rec.Body.String(), "go_gc_duration_seconds") ||
			strings.Contains(rec.Body.String(), "newapi_http_requests_total"))
	})
}
