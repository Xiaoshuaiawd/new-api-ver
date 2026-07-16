package cachex

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/samber/hot"
)

const (
	defaultRedisOpTimeout   = 2 * time.Second
	defaultRedisScanTimeout = 30 * time.Second
	defaultRedisDelTimeout  = 10 * time.Second
)

type HybridCacheConfig[V any] struct {
	Namespace Namespace

	// Redis is used when RedisEnabled returns true (or RedisEnabled is nil) and Redis is not nil.
	Redis        *redis.Client
	RedisCodec   ValueCodec[V]
	RedisEnabled func() bool

	// Memory builds a hot cache used when Redis is disabled. Keys stored in memory are fully namespaced.
	Memory func() *hot.HotCache[string, V]

	// L1TTL and L1MissTTL enable a bounded-lifetime local cache while Redis is
	// active. They are opt-in so callers that require every read to observe
	// Redis retain their current behavior.
	L1TTL      time.Duration
	L1MissTTL  time.Duration
	L1Capacity int
}

type l1Entry[V any] struct {
	value     V
	found     bool
	expiresAt time.Time
}

// HybridCache is a small helper that uses Redis when enabled, otherwise falls back to in-memory hot cache.
type HybridCache[V any] struct {
	ns Namespace

	redis        *redis.Client
	redisCodec   ValueCodec[V]
	redisEnabled func() bool

	memOnce sync.Once
	memInit func() *hot.HotCache[string, V]
	mem     *hot.HotCache[string, V]

	l1Mu       sync.Mutex
	l1         map[string]l1Entry[V]
	l1TTL      time.Duration
	l1MissTTL  time.Duration
	l1Capacity int
}

func NewHybridCache[V any](cfg HybridCacheConfig[V]) *HybridCache[V] {
	return &HybridCache[V]{
		ns:           cfg.Namespace,
		redis:        cfg.Redis,
		redisCodec:   cfg.RedisCodec,
		redisEnabled: cfg.RedisEnabled,
		memInit:      cfg.Memory,
		l1:           make(map[string]l1Entry[V]),
		l1TTL:        cfg.L1TTL,
		l1MissTTL:    cfg.L1MissTTL,
		l1Capacity:   cfg.L1Capacity,
	}
}

func (c *HybridCache[V]) FullKey(key string) string {
	return c.ns.FullKey(key)
}

func (c *HybridCache[V]) redisOn() bool {
	if c.redis == nil || c.redisCodec == nil {
		return false
	}
	if c.redisEnabled == nil {
		return true
	}
	return c.redisEnabled()
}

func (c *HybridCache[V]) memCache() *hot.HotCache[string, V] {
	c.memOnce.Do(func() {
		if c.memInit == nil {
			c.mem = hot.NewHotCache[string, V](hot.LRU, 1).Build()
			return
		}
		c.mem = c.memInit()
	})
	return c.mem
}

func (c *HybridCache[V]) Get(key string) (value V, found bool, err error) {
	full := c.ns.FullKey(key)
	if full == "" {
		var zero V
		return zero, false, nil
	}

	if c.redisOn() {
		if value, found, ok := c.getL1(full); ok {
			return value, found, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), defaultRedisOpTimeout)
		defer cancel()

		raw, e := c.redis.Get(ctx, full).Result()
		if e == nil {
			v, decErr := c.redisCodec.Decode(raw)
			if decErr != nil {
				var zero V
				return zero, false, decErr
			}
			c.setL1(full, v, true, c.l1TTL)
			return v, true, nil
		}
		if errors.Is(e, redis.Nil) {
			var zero V
			c.setL1(full, zero, false, c.l1MissTTL)
			return zero, false, nil
		}
		var zero V
		return zero, false, e
	}

	return c.memCache().Get(full)
}

func (c *HybridCache[V]) SetWithTTL(key string, v V, ttl time.Duration) error {
	full := c.ns.FullKey(key)
	if full == "" {
		return nil
	}

	if c.redisOn() {
		raw, err := c.redisCodec.Encode(v)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), defaultRedisOpTimeout)
		defer cancel()
		if err := c.redis.Set(ctx, full, raw, ttl).Err(); err != nil {
			return err
		}
		localTTL := c.l1TTL
		if localTTL > ttl && ttl > 0 {
			localTTL = ttl
		}
		c.setL1(full, v, true, localTTL)
		return nil
	}

	c.memCache().SetWithTTL(full, v, ttl)
	return nil
}

// Keys returns keys with valid values. In Redis, it returns all matching keys.
func (c *HybridCache[V]) Keys() ([]string, error) {
	if c.redisOn() {
		return c.scanKeys(c.ns.MatchPattern())
	}
	return c.memCache().Keys(), nil
}

func (c *HybridCache[V]) scanKeys(match string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultRedisScanTimeout)
	defer cancel()

	var cursor uint64
	keys := make([]string, 0, 1024)
	for {
		k, next, err := c.redis.Scan(ctx, cursor, match, 1000).Result()
		if err != nil {
			return keys, err
		}
		keys = append(keys, k...)
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return keys, nil
}

func (c *HybridCache[V]) Purge() error {
	c.clearL1()
	if c.redisOn() {
		keys, err := c.scanKeys(c.ns.MatchPattern())
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			return nil
		}
		_, err = c.DeleteMany(keys)
		return err
	}

	c.memCache().Purge()
	return nil
}

func (c *HybridCache[V]) DeleteByPrefix(prefix string) (int, error) {
	fullPrefix := c.ns.FullKey(prefix)
	if fullPrefix == "" {
		return 0, nil
	}
	if !strings.HasSuffix(fullPrefix, ":") {
		fullPrefix += ":"
	}

	if c.redisOn() {
		match := fullPrefix + "*"
		keys, err := c.scanKeys(match)
		if err != nil {
			return 0, err
		}
		if len(keys) == 0 {
			return 0, nil
		}

		res, err := c.DeleteMany(keys)
		if err != nil {
			return 0, err
		}
		deleted := 0
		for _, ok := range res {
			if ok {
				deleted++
			}
		}
		return deleted, nil
	}

	// In memory, we filter keys and bulk delete.
	allKeys := c.memCache().Keys()
	keys := make([]string, 0, 128)
	for _, k := range allKeys {
		if strings.HasPrefix(k, fullPrefix) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return 0, nil
	}
	res, _ := c.DeleteMany(keys)
	deleted := 0
	for _, ok := range res {
		if ok {
			deleted++
		}
	}
	return deleted, nil
}

// DeleteMany accepts either fully namespaced keys or raw keys and deletes them.
// It returns a map keyed by fully namespaced keys.
func (c *HybridCache[V]) DeleteMany(keys []string) (map[string]bool, error) {
	res := make(map[string]bool, len(keys))
	if len(keys) == 0 {
		return res, nil
	}

	fullKeys := make([]string, 0, len(keys))
	for _, k := range keys {
		k = c.ns.FullKey(k)
		if k == "" {
			continue
		}
		fullKeys = append(fullKeys, k)
	}
	if len(fullKeys) == 0 {
		return res, nil
	}
	c.deleteL1(fullKeys)

	if c.redisOn() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultRedisDelTimeout)
		defer cancel()

		pipe := c.redis.Pipeline()
		cmds := make([]*redis.IntCmd, 0, len(fullKeys))
		for _, k := range fullKeys {
			// UNLINK is non-blocking vs DEL for large key batches.
			cmds = append(cmds, pipe.Unlink(ctx, k))
		}
		_, err := pipe.Exec(ctx)
		if err != nil && !errors.Is(err, redis.Nil) {
			return res, err
		}
		for i, cmd := range cmds {
			deleted := cmd != nil && cmd.Err() == nil && cmd.Val() > 0
			res[fullKeys[i]] = deleted
		}
		return res, nil
	}

	return c.memCache().DeleteMany(fullKeys), nil
}

// InvalidateLocal clears only this process's L1 entry. It is used by callers
// that distribute cache invalidations through their own pub/sub channel.
func (c *HybridCache[V]) InvalidateLocal(key string) {
	full := c.ns.FullKey(key)
	if full == "" {
		return
	}
	c.deleteL1([]string{full})
}

func (c *HybridCache[V]) getL1(full string) (value V, found bool, ok bool) {
	if c.l1TTL <= 0 && c.l1MissTTL <= 0 {
		return value, false, false
	}
	now := time.Now()
	c.l1Mu.Lock()
	defer c.l1Mu.Unlock()
	entry, ok := c.l1[full]
	if !ok || !now.Before(entry.expiresAt) {
		if ok {
			delete(c.l1, full)
		}
		return value, false, false
	}
	return entry.value, entry.found, true
}

func (c *HybridCache[V]) setL1(full string, value V, found bool, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.l1Mu.Lock()
	if c.l1Capacity > 0 && len(c.l1) >= c.l1Capacity {
		now := time.Now()
		for key, entry := range c.l1 {
			if !now.Before(entry.expiresAt) {
				delete(c.l1, key)
			}
		}
		if len(c.l1) >= c.l1Capacity {
			for key := range c.l1 {
				delete(c.l1, key)
				break
			}
		}
	}
	c.l1[full] = l1Entry[V]{value: value, found: found, expiresAt: time.Now().Add(ttl)}
	c.l1Mu.Unlock()
}

func (c *HybridCache[V]) clearL1() {
	if c.l1TTL <= 0 && c.l1MissTTL <= 0 {
		return
	}
	c.l1Mu.Lock()
	clear(c.l1)
	c.l1Mu.Unlock()
}

func (c *HybridCache[V]) deleteL1(fullKeys []string) {
	if c.l1TTL <= 0 && c.l1MissTTL <= 0 {
		return
	}
	c.l1Mu.Lock()
	for _, key := range fullKeys {
		delete(c.l1, key)
	}
	c.l1Mu.Unlock()
}

func (c *HybridCache[V]) Capacity() (mainCacheCapacity int, missingCacheCapacity int) {
	if c.redisOn() {
		return 0, 0
	}
	return c.memCache().Capacity()
}

func (c *HybridCache[V]) Algorithm() (mainCacheAlgorithm string, missingCacheAlgorithm string) {
	if c.redisOn() {
		return "redis", ""
	}
	return c.memCache().Algorithm()
}
