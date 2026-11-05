package xterminal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubTokenServer points token minting at a local endpoint that hands out a
// distinct token per call.
func stubTokenServer(t *testing.T, minted *atomic.Int64) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			UserID string `json:"userId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload.UserID) != 24 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token": fmt.Sprintf("token-%d", minted.Add(1)),
		})
	}))
	t.Cleanup(server.Close)

	setTokenEndpoint(server.URL)
	t.Cleanup(func() { setTokenEndpoint(offlineTokenURL) })
}

func snapshotTokens(channelID int) []string {
	pool := pools.forChannel(channelID)
	pool.mu.Lock()
	defer pool.mu.Unlock()
	return append([]string(nil), pool.tokens...)
}

func TestMintTokenUsesTwentyFourCharacterUserID(t *testing.T) {
	var minted atomic.Int64
	stubTokenServer(t, &minted)

	token, err := mintToken()
	require.NoError(t, err)
	assert.Equal(t, "token-1", token)
	assert.Len(t, randomUserID(), 24)
}

func TestMintTokenRejectsResponseWithoutToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":"denied"}`))
	}))
	t.Cleanup(server.Close)
	setTokenEndpoint(server.URL)
	t.Cleanup(func() { setTokenEndpoint(offlineTokenURL) })

	_, err := mintToken()
	require.Error(t, err)
}

func TestTokenPoolRotatesAcrossFiveTokensAndRefillsAfterDiscard(t *testing.T) {
	var minted atomic.Int64
	stubTokenServer(t, &minted)

	const channelID = 9001
	first, err := TakeToken(channelID)
	require.NoError(t, err)
	require.NotEmpty(t, first)

	require.Eventually(t, func() bool {
		return len(snapshotTokens(channelID)) == tokenPoolSize
	}, 2*time.Second, 5*time.Millisecond)

	seen := make(map[string]struct{})
	for range tokenPoolSize * 2 {
		token, err := TakeToken(channelID)
		require.NoError(t, err)
		seen[token] = struct{}{}
	}
	assert.Len(t, seen, tokenPoolSize, "rotation must cycle through the whole pool")

	DiscardToken(channelID, first)
	assert.NotContains(t, snapshotTokens(channelID), first)

	require.Eventually(t, func() bool {
		return len(snapshotTokens(channelID)) == tokenPoolSize
	}, 2*time.Second, 5*time.Millisecond)
	assert.NotContains(t, snapshotTokens(channelID), first)
}

func TestTokenRejectedOnlyForCredentialOrThrottlingFailures(t *testing.T) {
	assert.True(t, tokenRejected(http.StatusUnauthorized))
	assert.True(t, tokenRejected(http.StatusForbidden))
	assert.True(t, tokenRejected(http.StatusTooManyRequests))
	// An upstream outage or an unsupported model says nothing about the token.
	assert.False(t, tokenRejected(http.StatusInternalServerError))
	assert.False(t, tokenRejected(http.StatusBadGateway))
	assert.False(t, tokenRejected(http.StatusBadRequest))
	assert.False(t, tokenRejected(http.StatusNotFound))
	assert.False(t, tokenRejected(http.StatusOK))
}

func TestGetRequestURLFallsBackToBuiltInBase(t *testing.T) {
	adaptor := &Adaptor{}

	url, err := adaptor.GetRequestURL(&relaycommon.RelayInfo{
		ChannelMeta:    &relaycommon.ChannelMeta{},
		RequestURLPath: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Equal(t, DefaultBaseURL+"/v1/chat/completions", url)

	url, err = adaptor.GetRequestURL(&relaycommon.RelayInfo{
		ChannelMeta:    &relaycommon.ChannelMeta{ChannelBaseUrl: "https://proxy.example/chatv2/"},
		RequestURLPath: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://proxy.example/chatv2/v1/chat/completions", url)
}

func TestDoRequestRetriesWithFreshTokenAfterRejection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var minted atomic.Int64
	stubTokenServer(t, &minted)

	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"token expired"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","choices":[]}`))
	}))
	t.Cleanup(upstream.Close)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:      9101,
			ChannelType:    constant.ChannelTypeXTerminal,
			ChannelBaseUrl: upstream.URL,
		},
		RequestURLPath: "/v1/chat/completions",
	}
	adaptor := &Adaptor{}
	adaptor.Init(info)

	rejected, err := TakeToken(info.ChannelId)
	require.NoError(t, err)

	respAny, err := adaptor.DoRequest(c, info, strings.NewReader(`{"model":"gpt-4o-mini","messages":[]}`))
	require.NoError(t, err)
	resp, ok := respAny.(*http.Response)
	require.True(t, ok)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.EqualValues(t, 2, calls.Load())
	assert.NotContains(t, snapshotTokens(info.ChannelId), rejected)
	assert.Contains(t, snapshotTokens(info.ChannelId), info.ApiKey)
	assert.NotEqual(t, rejected, info.ApiKey)
}

func TestDoRequestReturnsUpstreamErrorAfterExhaustingTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var minted atomic.Int64
	stubTokenServer(t, &minted)

	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"forbidden"}}`))
	}))
	t.Cleanup(upstream.Close)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:      9102,
			ChannelType:    constant.ChannelTypeXTerminal,
			ChannelBaseUrl: upstream.URL,
		},
		RequestURLPath: "/v1/chat/completions",
	}
	adaptor := &Adaptor{}
	adaptor.Init(info)

	respAny, err := adaptor.DoRequest(c, info, strings.NewReader(`{"model":"gpt-4o-mini"}`))
	require.NoError(t, err)
	resp, ok := respAny.(*http.Response)
	require.True(t, ok)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.EqualValues(t, maxTokenAttempts, calls.Load())
}

func TestDiscardTokenIgnoresUnknownToken(t *testing.T) {
	var minted atomic.Int64
	stubTokenServer(t, &minted)

	const channelID = 9002
	token, err := TakeToken(channelID)
	require.NoError(t, err)

	DiscardToken(channelID, "not-a-pooled-token")
	assert.True(t, slices.Contains(snapshotTokens(channelID), token))

	// Let the background refill finish while the stub server is still up.
	require.Eventually(t, func() bool {
		return len(snapshotTokens(channelID)) == tokenPoolSize
	}, 2*time.Second, 5*time.Millisecond)
}
