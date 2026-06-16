package service

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
	"github.com/tidwall/gjson"
)

const (
	ginKeyCodexUserAgentRouteMeta = "codex_user_agent_route_meta"

	codexUserAgentFakeCacheNamespace = "new-api:codex_ua_fake_input_cache:v1"
)

type codexUserAgentRouteMeta struct {
	RuleName         string
	ChannelID        int
	SelectedGroup    string
	ModelName        string
	RequestPath      string
	FakeCacheEnabled bool
	FakeCacheTTL     int
	FakeCacheHit     bool
	FakeCacheTokens  int
}

var (
	codexUserAgentFakeCacheOnce  sync.Once
	codexUserAgentFakeCache      *cachex.HybridCache[string]
	codexUserAgentRoutingNowFunc = time.Now
	codexUserAgentRoutingRandInt = rand.Intn
)

func getCodexUserAgentFakeCache() *cachex.HybridCache[string] {
	codexUserAgentFakeCacheOnce.Do(func() {
		setting := operation_setting.GetCodexUserAgentRoutingSetting()
		capacity := 100_000
		defaultTTLSeconds := 300
		if setting != nil {
			if setting.MaxEntries > 0 {
				capacity = setting.MaxEntries
			}
			if setting.DefaultFakeCacheTTL > 0 {
				defaultTTLSeconds = setting.DefaultFakeCacheTTL
			}
		}

		codexUserAgentFakeCache = cachex.NewHybridCache[string](cachex.HybridCacheConfig[string]{
			Namespace:  cachex.Namespace(codexUserAgentFakeCacheNamespace),
			Redis:      common.RDB,
			RedisCodec: cachex.StringCodec{},
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			Memory: func() *hot.HotCache[string, string] {
				return hot.NewHotCache[string, string](hot.LRU, capacity).
					WithTTL(time.Duration(defaultTTLSeconds) * time.Second).
					WithJanitor().
					Build()
			},
		})
	})
	return codexUserAgentFakeCache
}

func SelectCodexUserAgentRoute(c *gin.Context, modelName string) (int, bool) {
	setting := operation_setting.GetCodexUserAgentRoutingSetting()
	if setting == nil || !setting.Enabled || c == nil || c.Request == nil {
		return 0, false
	}
	userAgent := c.Request.UserAgent()
	if strings.TrimSpace(userAgent) == "" {
		return 0, false
	}
	path := ""
	if c.Request.URL != nil {
		path = c.Request.URL.Path
	}

	for _, rule := range setting.Rules {
		if !matchAnyRegexCached(rule.UserAgentRegex, userAgent) {
			continue
		}
		if len(rule.ModelRegex) > 0 && !matchAnyRegexCached(rule.ModelRegex, modelName) {
			continue
		}
		if len(rule.PathRegex) > 0 && !matchAnyRegexCached(rule.PathRegex, path) {
			continue
		}
		channelID, ok := chooseCodexUserAgentRouteTarget(rule.Targets)
		if !ok {
			continue
		}
		ttl := rule.FakeCacheTTL
		if ttl <= 0 {
			ttl = setting.DefaultFakeCacheTTL
		}
		if ttl <= 0 {
			ttl = 300
		}
		c.Set(ginKeyCodexUserAgentRouteMeta, codexUserAgentRouteMeta{
			RuleName:         strings.TrimSpace(rule.Name),
			ChannelID:        channelID,
			ModelName:        modelName,
			RequestPath:      path,
			FakeCacheEnabled: rule.FakeCacheEnabled,
			FakeCacheTTL:     ttl,
		})
		return channelID, true
	}
	return 0, false
}

func chooseCodexUserAgentRouteTarget(targets []operation_setting.CodexUserAgentRouteTarget) (int, bool) {
	total := 0
	for _, target := range targets {
		if target.ChannelID <= 0 || target.Weight <= 0 {
			continue
		}
		total += target.Weight
	}
	if total <= 0 {
		return 0, false
	}
	pick := codexUserAgentRoutingRandInt(total)
	for _, target := range targets {
		if target.ChannelID <= 0 || target.Weight <= 0 {
			continue
		}
		pick -= target.Weight
		if pick < 0 {
			return target.ChannelID, true
		}
	}
	return 0, false
}

func IsCodexUserAgentRouteMatched(c *gin.Context) bool {
	_, ok := getCodexUserAgentRouteMeta(c)
	return ok
}

func MarkCodexUserAgentRouteUsed(c *gin.Context, selectedGroup string, channelID int) {
	if c == nil || channelID <= 0 {
		return
	}
	meta, ok := getCodexUserAgentRouteMeta(c)
	if !ok || meta.ChannelID != channelID {
		return
	}
	meta.SelectedGroup = selectedGroup
	c.Set(ginKeyCodexUserAgentRouteMeta, meta)
}

func ClearCodexUserAgentRoute(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set(ginKeyCodexUserAgentRouteMeta, nil)
}

func AppendCodexUserAgentRouteAdminInfo(c *gin.Context, adminInfo map[string]interface{}) {
	if c == nil || adminInfo == nil {
		return
	}
	meta, ok := getCodexUserAgentRouteMeta(c)
	if !ok {
		return
	}
	adminInfo["codex_user_agent_route"] = map[string]interface{}{
		"rule_name":          meta.RuleName,
		"channel_id":         meta.ChannelID,
		"selected_group":     meta.SelectedGroup,
		"model":              meta.ModelName,
		"request_path":       meta.RequestPath,
		"fake_cache_enabled": meta.FakeCacheEnabled,
		"fake_cache_ttl":     meta.FakeCacheTTL,
		"fake_cache_hit":     meta.FakeCacheHit,
		"fake_cache_tokens":  meta.FakeCacheTokens,
	}
}

func ApplyCodexFakeInputCache(c *gin.Context, usage *dto.Usage) bool {
	if c == nil || usage == nil {
		return false
	}
	meta, ok := getCodexUserAgentRouteMeta(c)
	if !ok || !meta.FakeCacheEnabled {
		return false
	}
	if currentChannelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId); currentChannelID > 0 && currentChannelID != meta.ChannelID {
		return false
	}

	normalizeCodexUsageTokenFields(usage)
	promptTokens := usagePromptTokens(usage)
	if promptTokens <= 0 {
		return false
	}
	cacheKey := buildCodexFakeInputCacheKey(c, meta)
	if cacheKey == "" {
		return false
	}

	now := codexUserAgentRoutingNowFunc()
	ttl := meta.FakeCacheTTL
	if ttl <= 0 {
		ttl = 300
	}
	expiresAt := now.Add(time.Duration(ttl) * time.Second).Unix()
	cache := getCodexUserAgentFakeCache()
	rawExpiresAt, found, err := cache.Get(cacheKey)
	if err != nil {
		common.SysError(fmt.Sprintf("codex ua fake input cache get failed: err=%v", err))
		found = false
	}

	hit := false
	if found {
		if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(rawExpiresAt), 10, 64); parseErr == nil && parsed > now.Unix() {
			hit = true
		}
	}

	if err := cache.SetWithTTL(cacheKey, strconv.FormatInt(expiresAt, 10), time.Duration(ttl)*time.Second); err != nil {
		common.SysError(fmt.Sprintf("codex ua fake input cache set failed: err=%v", err))
	}

	if !hit {
		return false
	}
	applyCodexCachedTokens(usage, promptTokens)
	meta.FakeCacheHit = true
	meta.FakeCacheTokens = promptTokens
	c.Set(ginKeyCodexUserAgentRouteMeta, meta)
	return true
}

func getCodexUserAgentRouteMeta(c *gin.Context) (codexUserAgentRouteMeta, bool) {
	if c == nil {
		return codexUserAgentRouteMeta{}, false
	}
	anyMeta, ok := c.Get(ginKeyCodexUserAgentRouteMeta)
	if !ok {
		return codexUserAgentRouteMeta{}, false
	}
	meta, ok := anyMeta.(codexUserAgentRouteMeta)
	if !ok || meta.ChannelID <= 0 {
		return codexUserAgentRouteMeta{}, false
	}
	return meta, true
}

func buildCodexFakeInputCacheKey(c *gin.Context, meta codexUserAgentRouteMeta) string {
	fingerprint := codexFakeInputFingerprint(c)
	if fingerprint == "" {
		return ""
	}
	parts := []string{
		strings.TrimSpace(meta.RuleName),
		strconv.Itoa(meta.ChannelID),
		strings.TrimSpace(meta.SelectedGroup),
		strings.TrimSpace(meta.ModelName),
		strconv.Itoa(common.GetContextKeyInt(c, constant.ContextKeyTokenId)),
		fingerprint,
	}
	return strings.Join(parts, ":")
}

func codexFakeInputFingerprint(c *gin.Context) string {
	if c == nil {
		return ""
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return ""
	}
	body, err := storage.Bytes()
	if err != nil || len(body) == 0 {
		return ""
	}
	if promptCacheKey := strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String()); promptCacheKey != "" {
		return "pck:" + common.Sha1([]byte(promptCacheKey))
	}
	return "body:" + common.Sha1(body)
}

func applyCodexCachedTokens(usage *dto.Usage, promptTokens int) {
	if promptTokens <= 0 {
		return
	}
	normalizeCodexUsageTokenFields(usage)
	if usage.PromptTokensDetails.CachedTokens < promptTokens {
		usage.PromptTokensDetails.CachedTokens = promptTokens
	}
	if usage.InputTokensDetails == nil {
		usage.InputTokensDetails = &dto.InputTokenDetails{}
	}
	if usage.InputTokensDetails.CachedTokens < promptTokens {
		usage.InputTokensDetails.CachedTokens = promptTokens
	}
	if usage.InputTokens == 0 {
		usage.InputTokens = promptTokens
	}
}

func normalizeCodexUsageTokenFields(usage *dto.Usage) {
	if usage == nil {
		return
	}
	if usage.PromptTokens == 0 && usage.InputTokens > 0 {
		usage.PromptTokens = usage.InputTokens
	}
	if usage.CompletionTokens == 0 && usage.OutputTokens > 0 {
		usage.CompletionTokens = usage.OutputTokens
	}
	if usage.InputTokens == 0 && usage.PromptTokens > 0 {
		usage.InputTokens = usage.PromptTokens
	}
	if usage.OutputTokens == 0 && usage.CompletionTokens > 0 {
		usage.OutputTokens = usage.CompletionTokens
	}
	if usage.TotalTokens == 0 {
		if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		} else if usage.InputTokens > 0 || usage.OutputTokens > 0 {
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
		}
	}
}

func SetCodexUserAgentRoutingNowForTest(fn func() time.Time) {
	if fn == nil {
		codexUserAgentRoutingNowFunc = time.Now
		return
	}
	codexUserAgentRoutingNowFunc = fn
}

func SetCodexUserAgentRoutingRandForTest(fn func(int) int) {
	if fn == nil {
		codexUserAgentRoutingRandInt = rand.Intn
		return
	}
	codexUserAgentRoutingRandInt = fn
}

func ResetCodexUserAgentRoutingForTest() {
	SetCodexUserAgentRoutingNowForTest(nil)
	SetCodexUserAgentRoutingRandForTest(nil)
	if codexUserAgentFakeCache != nil {
		_ = codexUserAgentFakeCache.Purge()
	}
	codexUserAgentFakeCache = nil
	codexUserAgentFakeCacheOnce = sync.Once{}
}
