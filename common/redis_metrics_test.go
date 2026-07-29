package common

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type redisMetricsFixture struct {
	Name string
}

func TestRedisCacheHelpersReportHitMissAndError(t *testing.T) {
	observed := make([]string, 0, 5)
	SetCacheLookupObserver(func(backend, result string) {
		observed = append(observed, backend+":"+result)
	})
	t.Cleanup(func() { SetCacheLookupObserver(nil) })

	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	previousRDB := RDB
	RDB = redisClient
	t.Cleanup(func() {
		RDB = previousRDB
		require.NoError(t, redisClient.Close())
	})

	require.NoError(t, redisClient.Set(redisClient.Context(), "cache:hit", "value", 0).Err())
	_, err := RedisGet("cache:hit")
	require.NoError(t, err)
	_, err = RedisGet("cache:miss")
	require.ErrorIs(t, err, redis.Nil)

	require.NoError(t, redisClient.HSet(redisClient.Context(), "hash:hit", "Name", "value").Err())
	var fixture redisMetricsFixture
	require.NoError(t, RedisHGetObj("hash:hit", &fixture))
	assert.Equal(t, "value", fixture.Name)
	require.Error(t, RedisHGetObj("hash:miss", &redisMetricsFixture{}))

	redisServer.Close()
	_, err = RedisGet("cache:error")
	require.Error(t, err)

	assert.Equal(t, []string{"redis:hit", "redis:miss", "redis:hit", "redis:miss", "redis:error"}, observed)
}
