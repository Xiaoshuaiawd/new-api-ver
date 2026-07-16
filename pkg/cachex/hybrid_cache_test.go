package cachex

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHybridCacheL1CachesPositiveAndNegativeEntries(t *testing.T) {
	cache := NewHybridCache(HybridCacheConfig[int]{
		L1TTL:      time.Second,
		L1MissTTL:  time.Second,
		L1Capacity: 2,
	})

	cache.setL1("present", 42, true, time.Second)
	cache.setL1("missing", 0, false, time.Second)

	value, found, ok := cache.getL1("present")
	require.True(t, ok)
	require.True(t, found)
	require.Equal(t, 42, value)

	_, found, ok = cache.getL1("missing")
	require.True(t, ok)
	require.False(t, found)
}

func TestHybridCacheL1EvictsAtCapacity(t *testing.T) {
	cache := NewHybridCache(HybridCacheConfig[int]{
		L1TTL:      time.Second,
		L1Capacity: 1,
	})

	cache.setL1("first", 1, true, time.Second)
	cache.setL1("second", 2, true, time.Second)

	_, _, firstFound := cache.getL1("first")
	value, found, secondFound := cache.getL1("second")
	require.False(t, firstFound)
	require.True(t, secondFound)
	require.True(t, found)
	require.Equal(t, 2, value)
}
