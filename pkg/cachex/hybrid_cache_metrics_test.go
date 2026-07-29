package cachex

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHybridCacheReportsRedisAndMemoryLookupOutcomes(t *testing.T) {
	observed := make([]string, 0, 4)
	SetLookupObserver(func(backend, result string) {
		observed = append(observed, backend+":"+result)
	})
	t.Cleanup(func() { SetLookupObserver(nil) })

	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })
	redisCache := NewHybridCache[string](HybridCacheConfig[string]{
		Namespace:  "metrics-test",
		Redis:      redisClient,
		RedisCodec: StringCodec{},
	})
	require.NoError(t, redisClient.Set(context.Background(), "metrics-test:hit", "value", time.Minute).Err())

	value, found, err := redisCache.Get("hit")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "value", value)
	_, found, err = redisCache.Get("miss")
	require.NoError(t, err)
	assert.False(t, found)

	memoryCache := NewHybridCache[string](HybridCacheConfig[string]{Namespace: "memory-test"})
	require.NoError(t, memoryCache.SetWithTTL("hit", "value", time.Minute))
	_, found, err = memoryCache.Get("hit")
	require.NoError(t, err)
	assert.True(t, found)
	_, found, err = memoryCache.Get("miss")
	require.NoError(t, err)
	assert.False(t, found)

	assert.Equal(t, []string{"redis:hit", "redis:miss", "memory:hit", "memory:miss"}, observed)
}
