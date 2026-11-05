package xterminal

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// tokenRequestTimeout bounds a single /user/offline call. Token minting is a
// cheap request, so a slow upstream is treated as a failure instead of
// stalling a relay request indefinitely.
const tokenRequestTimeout = 15 * time.Second

var tokenHTTPClient = &http.Client{Timeout: tokenRequestTimeout}

// tokenEndpoint is the minting URL. It is swappable so tests can serve a stub
// while background refills are still running.
var (
	tokenEndpointMu sync.RWMutex
	tokenEndpoint   = offlineTokenURL
)

func currentTokenEndpoint() string {
	tokenEndpointMu.RLock()
	defer tokenEndpointMu.RUnlock()
	return tokenEndpoint
}

func setTokenEndpoint(url string) {
	tokenEndpointMu.Lock()
	defer tokenEndpointMu.Unlock()
	tokenEndpoint = url
}

// channelTokenPool keeps the minted tokens of one channel. Tokens are opaque
// bearer credentials: they are used round-robin and thrown away the moment the
// upstream rejects one, then replaced by a freshly minted token.
type channelTokenPool struct {
	mu        sync.Mutex
	tokens    []string
	next      int
	refilling bool
}

func (pool *channelTokenPool) size() int {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	return len(pool.tokens)
}

// take returns the next token in rotation, or false when the pool is empty.
func (pool *channelTokenPool) take() (string, bool) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if len(pool.tokens) == 0 {
		return "", false
	}
	if pool.next >= len(pool.tokens) {
		pool.next = 0
	}
	token := pool.tokens[pool.next]
	pool.next = (pool.next + 1) % len(pool.tokens)
	return token, true
}

func (pool *channelTokenPool) add(token string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.tokens = append(pool.tokens, token)
}

// remove drops a rejected token and keeps the rotation cursor consistent.
func (pool *channelTokenPool) remove(token string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	for i, existing := range pool.tokens {
		if existing != token {
			continue
		}
		pool.tokens = append(pool.tokens[:i], pool.tokens[i+1:]...)
		if pool.next > i {
			pool.next--
		}
		return
	}
}

type tokenPools struct {
	mu    sync.Mutex
	pools map[int]*channelTokenPool
}

var pools = &tokenPools{pools: make(map[int]*channelTokenPool)}

func (p *tokenPools) forChannel(channelID int) *channelTokenPool {
	p.mu.Lock()
	defer p.mu.Unlock()
	pool, ok := p.pools[channelID]
	if !ok {
		pool = &channelTokenPool{}
		p.pools[channelID] = pool
	}
	return pool
}

// TakeToken returns a token for the channel, minting one when the pool is
// empty. The pool is topped up to tokenPoolSize in the background so that a
// relay request rarely pays the minting latency.
func TakeToken(channelID int) (string, error) {
	pool := pools.forChannel(channelID)

	token, ok := pool.take()
	if !ok {
		minted, err := mintToken()
		if err != nil {
			return "", err
		}
		pool.add(minted)
		token = minted
	}
	pool.refillAsync()
	return token, nil
}

// DiscardToken retires a token the upstream rejected and schedules a
// replacement so the pool converges back to tokenPoolSize.
func DiscardToken(channelID int, token string) {
	if token == "" {
		return
	}
	pool := pools.forChannel(channelID)
	pool.remove(token)
	pool.refillAsync()
}

// ForgetPool releases the tokens of a deleted channel.
func ForgetPool(channelID int) {
	pools.mu.Lock()
	defer pools.mu.Unlock()
	delete(pools.pools, channelID)
}

// refillAsync mints missing tokens without blocking the relay path. Only one
// refill runs per channel at a time.
func (pool *channelTokenPool) refillAsync() {
	pool.mu.Lock()
	if pool.refilling || len(pool.tokens) >= tokenPoolSize {
		pool.mu.Unlock()
		return
	}
	pool.refilling = true
	pool.mu.Unlock()

	go func() {
		defer func() {
			pool.mu.Lock()
			pool.refilling = false
			pool.mu.Unlock()
		}()
		for pool.size() < tokenPoolSize {
			token, err := mintToken()
			if err != nil {
				common.SysError(common.LogText("xterminal: mint token failed: %s", err.Error()))
				return
			}
			pool.add(token)
		}
	}()
}

// mintToken exchanges a freshly generated userId for a bearer token.
func mintToken() (string, error) {
	payload, err := common.Marshal(map[string]string{"userId": randomUserID()})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, currentTokenEndpoint(), bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", offlineUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("sec-ch-ua", `"Not(A:Brand";v="8", "Chromium";v="144"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"macOS"`)

	resp, err := tokenHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request token failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read token response failed: %w", err)
	}
	// The endpoint answers 201 Created on success, so any 2xx counts.
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("token endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := common.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode token response failed: %w", err)
	}
	token := strings.TrimSpace(result.Token)
	if token == "" {
		return "", fmt.Errorf("token response carried no token: %s", strings.TrimSpace(string(body)))
	}
	return token, nil
}

// randomUserID returns a 24 character hexadecimal identifier, matching the
// ObjectID shaped userId the desktop client sends.
func randomUserID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%024x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
