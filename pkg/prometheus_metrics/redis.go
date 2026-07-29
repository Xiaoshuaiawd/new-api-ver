package prometheusmetrics

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
)

var redisDurationBuckets = []float64{
	0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5,
}

type redisMetrics struct {
	enabled           prometheus.Gauge
	operations        *prometheus.CounterVec
	duration          *prometheus.HistogramVec
	cacheLookups      *prometheus.CounterVec
	rateLimitFailures *prometheus.CounterVec
	degradations      *prometheus.CounterVec
}

func registerRedisMetrics(registry prometheus.Registerer, enabled bool) (*redisMetrics, error) {
	metrics := &redisMetrics{
		enabled: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "newapi_redis_enabled",
			Help: "Whether this new-api process has an enabled Redis client.",
		}),
		operations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_redis_operations_total",
			Help: "Total Redis operations observed by the go-redis hook.",
		}, []string{"command", "operation_type", "result"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "newapi_redis_operation_duration_seconds",
			Help:    "Redis command and aggregate pipeline duration in seconds.",
			Buckets: redisDurationBuckets,
		}, []string{"command", "operation_type", "result"}),
		cacheLookups: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_cache_lookups_total",
			Help: "Total application cache lookups by backend and outcome.",
		}, []string{"backend", "result"}),
		rateLimitFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_redis_rate_limit_failures_total",
			Help: "Total Redis-backed rate limiter checks that failed.",
		}, []string{"limiter"}),
		degradations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_redis_degradations_total",
			Help: "Total operations that fell back after a Redis failure.",
		}, []string{"reason"}),
	}
	if enabled {
		metrics.enabled.Set(1)
	}
	for name, collector := range map[string]prometheus.Collector{
		"Redis enabled":             metrics.enabled,
		"Redis operations":          metrics.operations,
		"Redis duration":            metrics.duration,
		"Cache lookups":             metrics.cacheLookups,
		"Redis rate limit failures": metrics.rateLimitFailures,
		"Redis degradations":        metrics.degradations,
	} {
		if err := registry.Register(collector); err != nil {
			return nil, fmt.Errorf("register %s metric: %w", name, err)
		}
	}
	return metrics, nil
}

func RecordCacheLookup(backend, result string) {
	runtime := defaultRuntime.Load()
	if runtime == nil || runtime.redis == nil {
		return
	}
	runtime.redis.cacheLookups.WithLabelValues(
		normalizeCacheBackend(backend),
		normalizeCacheLookupResult(result),
	).Inc()
}

func normalizeCacheBackend(backend string) string {
	switch backend {
	case "redis", "memory":
		return backend
	default:
		return "other"
	}
}

func normalizeCacheLookupResult(result string) string {
	switch result {
	case "hit", "miss", "error":
		return result
	default:
		return "error"
	}
}

func RecordRedisRateLimitFailure(limiter string) {
	runtime := defaultRuntime.Load()
	if runtime == nil || runtime.redis == nil {
		return
	}
	runtime.redis.rateLimitFailures.WithLabelValues(normalizeRedisRateLimiter(limiter)).Inc()
}

func RecordRedisDegradation(reason string) {
	runtime := defaultRuntime.Load()
	if runtime == nil || runtime.redis == nil {
		return
	}
	runtime.redis.degradations.WithLabelValues(normalizeRedisDegradationReason(reason)).Inc()
}

func normalizeRedisRateLimiter(limiter string) string {
	switch limiter {
	case "fixed_window", "model_success", "model_total":
		return limiter
	default:
		return "other"
	}
}

func normalizeRedisDegradationReason(reason string) string {
	switch reason {
	case "rate_limit_fallback", "cache_fallback":
		return reason
	default:
		return "other"
	}
}

type redisCommandStartedKey struct{}
type redisPipelineStartedKey struct{}

type redisMetricsHook struct {
	metrics *redisMetrics
	now     func() time.Time
}

func newRedisMetricsHook(metrics *redisMetrics, now func() time.Time) redis.Hook {
	return &redisMetricsHook{metrics: metrics, now: now}
}

func (h *redisMetricsHook) BeforeProcess(ctx context.Context, _ redis.Cmder) (context.Context, error) {
	return context.WithValue(ctx, redisCommandStartedKey{}, h.now()), nil
}

func (h *redisMetricsHook) AfterProcess(ctx context.Context, command redis.Cmder) error {
	startedAt, _ := ctx.Value(redisCommandStartedKey{}).(time.Time)
	h.observeCommand(command, "command", startedAt, true)
	return nil
}

func (h *redisMetricsHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return context.WithValue(ctx, redisPipelineStartedKey{}, h.now()), nil
}

func (h *redisMetricsHook) AfterProcessPipeline(ctx context.Context, commands []redis.Cmder) error {
	startedAt, _ := ctx.Value(redisPipelineStartedKey{}).(time.Time)
	pipelineResult := "success"
	for _, command := range commands {
		result := redisOperationResult(command.Err())
		if result == "error" {
			pipelineResult = "error"
		}
		h.observeCommand(command, "pipeline_command", time.Time{}, false)
	}
	h.observe("pipeline", "pipeline", pipelineResult, startedAt, true)
	return nil
}

func (h *redisMetricsHook) observeCommand(command redis.Cmder, operationType string, startedAt time.Time, observeDuration bool) {
	if command == nil {
		return
	}
	h.observe(
		normalizeRedisCommand(command.Name()),
		operationType,
		redisOperationResult(command.Err()),
		startedAt,
		observeDuration,
	)
}

func (h *redisMetricsHook) observe(command, operationType, result string, startedAt time.Time, observeDuration bool) {
	if h == nil || h.metrics == nil {
		return
	}
	h.metrics.operations.WithLabelValues(command, operationType, result).Inc()
	if observeDuration && !startedAt.IsZero() {
		h.metrics.duration.WithLabelValues(command, operationType, result).Observe(h.now().Sub(startedAt).Seconds())
	}
}

func redisOperationResult(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, redis.Nil):
		return "miss"
	default:
		return "error"
	}
}

func normalizeRedisCommand(command string) string {
	command = strings.ToLower(strings.TrimSpace(command))
	switch command {
	case "append", "decr", "decrby", "del", "eval", "evalsha", "exists", "expire", "get", "getset",
		"hdel", "hexists", "hget", "hgetall", "hincrby", "hincrbyfloat", "hset", "incr", "incrby",
		"lindex", "llen", "lpop", "lpush", "lrange", "lrem", "ltrim", "mget", "mset", "ping",
		"publish", "rpop", "rpush", "sadd", "scard", "scan", "set", "setnx", "smembers", "srem", "ttl", "unlink", "zadd",
		"zcard", "zrange", "zrem", "zremrangebyscore", "zscore":
		return command
	default:
		return "other"
	}
}
