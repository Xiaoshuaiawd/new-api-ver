package prometheusmetrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisDisabledExportsOnlyDisabledStateWithoutOperationSamples(t *testing.T) {
	runtime, err := NewRuntime(
		Config{Enabled: true, AllowPublic: true},
		"v-test",
		nil,
		nil,
		WithRedisClient(nil, false),
	)
	require.NoError(t, err)

	response := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	output := response.Body.String()

	assert.Contains(t, output, "newapi_redis_enabled 0")
	assert.NotContains(t, output, "newapi_redis_operations_total{")
	assert.NotContains(t, output, "newapi_redis_operation_duration_seconds_count{")
}

func TestRedisHookRecordsCommandsPipelineAndEnabledWithoutKeys(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })

	runtime, err := NewRuntime(
		Config{Enabled: true, AllowPublic: true},
		"v-test",
		nil,
		nil,
		WithRedisClient(redisClient, true),
	)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, redisClient.Set(ctx, "private:user:42", "value", 0).Err())
	_, err = redisClient.Get(ctx, "private:missing:42").Result()
	require.ErrorIs(t, err, redis.Nil)

	pipeline := redisClient.Pipeline()
	pipeline.Set(ctx, "private:pipeline:42", "value", 0)
	pipeline.Get(ctx, "private:pipeline:missing:42")
	_, err = pipeline.Exec(ctx)
	require.ErrorIs(t, err, redis.Nil)

	response := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	output := response.Body.String()

	assert.Contains(t, output, "newapi_redis_enabled 1")
	assert.Contains(t, output,
		`newapi_redis_operations_total{command="set",operation_type="command",result="success"} 1`,
	)
	assert.Contains(t, output,
		`newapi_redis_operations_total{command="get",operation_type="command",result="miss"} 1`,
	)
	assert.Contains(t, output,
		`newapi_redis_operations_total{command="pipeline",operation_type="pipeline",result="success"} 1`,
	)
	assert.Contains(t, output,
		`newapi_redis_operations_total{command="set",operation_type="pipeline_command",result="success"} 1`,
	)
	assert.Contains(t, output,
		`newapi_redis_operations_total{command="get",operation_type="pipeline_command",result="miss"} 1`,
	)
	assert.Contains(t, output,
		`newapi_redis_operation_duration_seconds_count{command="pipeline",operation_type="pipeline",result="success"} 1`,
	)
	assert.NotContains(t, output, "private:user:42")
	assert.NotContains(t, output, "private:missing:42")
	assert.NotContains(t, output, "private:pipeline:42")
}

func TestRecordRedisFailureAndDegradationNormalizesLabels(t *testing.T) {
	runtime, err := NewRuntime(Config{Enabled: true, AllowPublic: true}, "v-test", nil, nil)
	require.NoError(t, err)
	SetDefaultRuntime(runtime)
	t.Cleanup(func() { SetDefaultRuntime(nil) })

	RecordRedisRateLimitFailure("fixed_window")
	RecordRedisRateLimitFailure("raw limiter name")
	RecordRedisDegradation("rate_limit_fallback")
	RecordRedisDegradation("raw error text")
	RecordCacheLookup("redis", "hit")
	RecordCacheLookup("raw backend", "raw result")

	response := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	output := response.Body.String()

	assert.Contains(t, output,
		`newapi_redis_rate_limit_failures_total{limiter="fixed_window"} 1`,
	)
	assert.Contains(t, output,
		`newapi_redis_rate_limit_failures_total{limiter="other"} 1`,
	)
	assert.Contains(t, output,
		`newapi_redis_degradations_total{reason="rate_limit_fallback"} 1`,
	)
	assert.Contains(t, output,
		`newapi_redis_degradations_total{reason="other"} 1`,
	)
	assert.Contains(t, output,
		`newapi_cache_lookups_total{backend="redis",result="hit"} 1`,
	)
	assert.Contains(t, output,
		`newapi_cache_lookups_total{backend="other",result="error"} 1`,
	)
}

func TestRedisHookRecordsTimeoutAndConnectionErrorsWithoutKeys(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })

	runtime, err := NewRuntime(
		Config{Enabled: true, AllowPublic: true},
		"v-test",
		nil,
		nil,
		WithRedisClient(redisClient, true),
	)
	require.NoError(t, err)
	timeoutKey := "private:timeout:user:42"
	timeoutContext, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	err = redisClient.Get(timeoutContext, timeoutKey).Err()
	require.ErrorIs(t, err, context.DeadlineExceeded)

	redisServer.Close()

	privateKey := "private:error:user:42"
	err = redisClient.Get(context.Background(), privateKey).Err()
	require.Error(t, err)

	response := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	output := response.Body.String()

	assert.Contains(t, output,
		`newapi_redis_operations_total{command="get",operation_type="command",result="error"} 2`,
	)
	assert.Contains(t, output,
		`newapi_redis_operation_duration_seconds_count{command="get",operation_type="command",result="error"} 2`,
	)
	assert.False(t, strings.Contains(output, timeoutKey))
	assert.False(t, strings.Contains(output, privateKey))
}
