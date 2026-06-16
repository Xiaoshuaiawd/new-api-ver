package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func withCodexUARoutingSetting(t *testing.T, setting operation_setting.CodexUserAgentRoutingSetting) {
	t.Helper()
	ptr := operation_setting.GetCodexUserAgentRoutingSetting()
	old := *ptr
	*ptr = setting
	t.Cleanup(func() {
		*ptr = old
		ResetCodexUserAgentRoutingForTest()
	})
}

func newCodexUATestContext(t *testing.T, body string, ua string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", ua)
	return c
}

func TestSelectCodexUserAgentRouteMatchesRegexAndWeight(t *testing.T) {
	withCodexUARoutingSetting(t, operation_setting.CodexUserAgentRoutingSetting{
		Enabled:             true,
		DefaultFakeCacheTTL: 300,
		Rules: []operation_setting.CodexUserAgentRouteRule{
			{
				Name:           "codex desktop",
				UserAgentRegex: []string{`(?i)^Codex Desktop/`},
				ModelRegex:     []string{"^gpt-5"},
				PathRegex:      []string{"/v1/responses"},
				Targets: []operation_setting.CodexUserAgentRouteTarget{
					{ChannelID: 101, Weight: 0},
					{ChannelID: 202, Weight: 100},
				},
			},
		},
	})
	SetCodexUserAgentRoutingRandForTest(func(n int) int { return 0 })

	ctx := newCodexUATestContext(t, `{"model":"gpt-5-mini","input":"hi"}`, "Codex Desktop/0.140.0-alpha.2 (Windows 10.0.19045; x86_64) unknown (Codex Desktop; 26.609.41114)")
	channelID, found := SelectCodexUserAgentRoute(ctx, "gpt-5-mini")

	require.True(t, found)
	require.Equal(t, 202, channelID)
	require.True(t, IsCodexUserAgentRouteMatched(ctx))
}

func TestSelectCodexUserAgentRouteIgnoresNonMatchingUserAgent(t *testing.T) {
	withCodexUARoutingSetting(t, operation_setting.CodexUserAgentRoutingSetting{
		Enabled:             true,
		DefaultFakeCacheTTL: 300,
		Rules: []operation_setting.CodexUserAgentRouteRule{
			{
				Name:           "codex cli",
				UserAgentRegex: []string{`(?i)^codex_cli_rs/`},
				Targets: []operation_setting.CodexUserAgentRouteTarget{
					{ChannelID: 101, Weight: 100},
				},
			},
		},
	})

	ctx := newCodexUATestContext(t, `{"model":"gpt-5-mini","input":"hi"}`, "curl/8.0")
	channelID, found := SelectCodexUserAgentRoute(ctx, "gpt-5-mini")

	require.False(t, found)
	require.Zero(t, channelID)
	require.False(t, IsCodexUserAgentRouteMatched(ctx))
}

func TestApplyCodexFakeInputCacheMarksSecondRequestCachedAndExpires(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	withCodexUARoutingSetting(t, operation_setting.CodexUserAgentRoutingSetting{
		Enabled:             true,
		DefaultFakeCacheTTL: 300,
		MaxEntries:          10,
		Rules: []operation_setting.CodexUserAgentRouteRule{
			{
				Name:             "codex cli",
				UserAgentRegex:   []string{`(?i)^codex_cli_rs/`},
				Targets:          []operation_setting.CodexUserAgentRouteTarget{{ChannelID: 101, Weight: 100}},
				FakeCacheEnabled: true,
			},
		},
	})
	SetCodexUserAgentRoutingRandForTest(func(n int) int { return 0 })
	SetCodexUserAgentRoutingNowForTest(func() time.Time { return now })

	body := `{"model":"gpt-5-mini","prompt_cache_key":"session-1","input":"turn one"}`
	ctx1 := newCodexUATestContext(t, body, "codex_cli_rs/0.73.0 (Mac OS 15.3.0; arm64) Apple_Terminal/455")
	channelID, found := SelectCodexUserAgentRoute(ctx1, "gpt-5-mini")
	require.True(t, found)
	require.Equal(t, 101, channelID)
	common.SetContextKey(ctx1, constant.ContextKeyChannelId, channelID)
	usage1 := &dto.Usage{PromptTokens: 123, CompletionTokens: 5, TotalTokens: 128}
	hit1 := ApplyCodexFakeInputCache(ctx1, usage1)
	require.False(t, hit1)
	require.Zero(t, usage1.PromptTokensDetails.CachedTokens)

	ctx2 := newCodexUATestContext(t, `{"model":"gpt-5-mini","prompt_cache_key":"session-1","input":"turn two"}`, "codex_cli_rs/0.73.0 (Mac OS 15.3.0; arm64) Apple_Terminal/455")
	channelID, found = SelectCodexUserAgentRoute(ctx2, "gpt-5-mini")
	require.True(t, found)
	common.SetContextKey(ctx2, constant.ContextKeyChannelId, channelID)
	usage2 := &dto.Usage{PromptTokens: 120, CompletionTokens: 3, TotalTokens: 123}
	hit2 := ApplyCodexFakeInputCache(ctx2, usage2)
	require.True(t, hit2)
	require.Equal(t, 120, usage2.PromptTokensDetails.CachedTokens)
	require.NotNil(t, usage2.InputTokensDetails)
	require.Equal(t, 120, usage2.InputTokensDetails.CachedTokens)

	now = now.Add(301 * time.Second)
	ctx3 := newCodexUATestContext(t, `{"model":"gpt-5-mini","prompt_cache_key":"session-1","input":"turn three"}`, "codex_cli_rs/0.73.0 (Mac OS 15.3.0; arm64) Apple_Terminal/455")
	channelID, found = SelectCodexUserAgentRoute(ctx3, "gpt-5-mini")
	require.True(t, found)
	common.SetContextKey(ctx3, constant.ContextKeyChannelId, channelID)
	usage3 := &dto.Usage{PromptTokens: 119, CompletionTokens: 2, TotalTokens: 121}
	hit3 := ApplyCodexFakeInputCache(ctx3, usage3)
	require.False(t, hit3)
	require.Zero(t, usage3.PromptTokensDetails.CachedTokens)
}

func TestApplyCodexFakeInputCacheUsesResponsesInputTokens(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	withCodexUARoutingSetting(t, operation_setting.CodexUserAgentRoutingSetting{
		Enabled:             true,
		DefaultFakeCacheTTL: 300,
		Rules: []operation_setting.CodexUserAgentRouteRule{
			{
				Name:             "codex responses",
				UserAgentRegex:   []string{`(?i)^codex_cli_rs/`},
				Targets:          []operation_setting.CodexUserAgentRouteTarget{{ChannelID: 101, Weight: 100}},
				FakeCacheEnabled: true,
			},
		},
	})
	SetCodexUserAgentRoutingRandForTest(func(n int) int { return 0 })
	SetCodexUserAgentRoutingNowForTest(func() time.Time { return now })

	ctx1 := newCodexUATestContext(t, `{"model":"gpt-5-mini","prompt_cache_key":"responses-1"}`, "codex_cli_rs/0.73.0")
	channelID, found := SelectCodexUserAgentRoute(ctx1, "gpt-5-mini")
	require.True(t, found)
	common.SetContextKey(ctx1, constant.ContextKeyChannelId, channelID)
	require.False(t, ApplyCodexFakeInputCache(ctx1, &dto.Usage{InputTokens: 88, OutputTokens: 3, TotalTokens: 91}))

	ctx2 := newCodexUATestContext(t, `{"model":"gpt-5-mini","prompt_cache_key":"responses-1"}`, "codex_cli_rs/0.73.0")
	channelID, found = SelectCodexUserAgentRoute(ctx2, "gpt-5-mini")
	require.True(t, found)
	common.SetContextKey(ctx2, constant.ContextKeyChannelId, channelID)
	usage := &dto.Usage{InputTokens: 88, OutputTokens: 3, TotalTokens: 91}
	require.True(t, ApplyCodexFakeInputCache(ctx2, usage))
	require.Equal(t, 88, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 88, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)

	adminInfo := map[string]interface{}{}
	AppendCodexUserAgentRouteAdminInfo(ctx2, adminInfo)
	routeInfo, ok := adminInfo["codex_user_agent_route"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, true, routeInfo["fake_cache_hit"])
	require.Equal(t, 88, routeInfo["fake_cache_tokens"])
}
