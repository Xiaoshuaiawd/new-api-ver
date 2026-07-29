package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	prometheusmetrics "github.com/QuantumNous/new-api/pkg/prometheus_metrics"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelRedisRateLimitUsesUTCRegardlessOfLocalTimezone(t *testing.T) {
	redisServer, redisClient := useRateLimitMiniRedis(t)
	previousLocation := time.Local
	time.Local = time.FixedZone("test-utc-plus-eight", 8*60*60)
	t.Cleanup(func() { time.Local = previousLocation })

	ctx := context.Background()
	recordKey := "rateLimit:model-utc-record"
	recordRedisRequest(ctx, redisClient, recordKey, 2)
	recorded, err := redisClient.LIndex(ctx, recordKey, 0).Result()
	require.NoError(t, err)
	recordedAt, err := time.Parse(modelRateLimitTimeFormat, recorded)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().UTC(), recordedAt, 2*time.Second)

	checkKey := "rateLimit:model-utc-check"
	withinWindow := time.Now().UTC().Add(-30 * time.Second).Format(modelRateLimitTimeFormat)
	_, err = redisServer.Push(checkKey, withinWindow, withinWindow)
	require.NoError(t, err)
	allowed, err := checkRedisRateLimit(ctx, redisClient, checkKey, 2, 60)
	require.NoError(t, err)
	assert.False(t, allowed, "an existing UTC timestamp inside the window must remain limited on a non-UTC host")
}

func TestModelRateLimitRejectionMetricsDistinguishTotalAndSuccessfulLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })

	runtime, err := prometheusmetrics.NewRuntime(
		prometheusmetrics.Config{Enabled: true, AllowPublic: true},
		"v-test",
		nil,
		nil,
	)
	require.NoError(t, err)
	prometheusmetrics.SetDefaultRuntime(runtime)
	t.Cleanup(func() { prometheusmetrics.SetDefaultRuntime(nil) })

	totalRouter := gin.New()
	totalRouter.GET(
		"/total",
		func(c *gin.Context) { c.Set("id", 91001) },
		memoryRateLimitHandler(60, 1, 100),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(totalRouter, "/total", "192.0.2.81:12345").Code)
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(totalRouter, "/total", "192.0.2.81:12345").Code)

	successRouter := gin.New()
	successRouter.GET(
		"/success",
		func(c *gin.Context) { c.Set("id", 91002) },
		memoryRateLimitHandler(60, 0, 1),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(successRouter, "/success", "192.0.2.82:12345").Code)
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(successRouter, "/success", "192.0.2.82:12345").Code)

	response := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Contains(t, response.Body.String(),
		`newapi_rate_limit_rejections_total{reason="total_request_count",scope="user"} 1`,
	)
	assert.Contains(t, response.Body.String(),
		`newapi_rate_limit_rejections_total{reason="successful_request_count",scope="user"} 1`,
	)
}
