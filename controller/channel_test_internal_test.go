package controller

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.QuotaPerUnit,
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier: "base",
	})

	require.Equal(t, "tiered_expr", other["billing_mode"])
	require.Equal(t, "base", other["matched_tier"])
	require.NotEmpty(t, other["expr_b64"])
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := resolveChannelTestUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}

func TestGetChannelAppliesOpenAIKeyLimitToInitiallyDistributedChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 1)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(ctx, constant.ContextKeyChannelName, "official")
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, constant.ChannelBaseURLs[constant.ChannelTypeOpenAI])
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "sk-context")
	common.SetContextKey(ctx, constant.ContextKeyChannelAutoBan, true)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldSQLite := common.UsingSQLite
	oldMySQL := common.UsingMySQL
	oldPostgreSQL := common.UsingPostgreSQL
	oldEnabled := setting.OpenAIUpstreamKeyLimitEnabled
	oldConfig := setting.OpenAIUpstreamKeyLimitConfigValue
	oldRedis := common.RedisEnabled
	oldLimiter := service.GetOpenAIUpstreamKeyLimiterForTest()
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldSQLite
		common.UsingMySQL = oldMySQL
		common.UsingPostgreSQL = oldPostgreSQL
		setting.OpenAIUpstreamKeyLimitEnabled = oldEnabled
		setting.OpenAIUpstreamKeyLimitConfigValue = oldConfig
		common.RedisEnabled = oldRedis
		service.SetOpenAIUpstreamKeyLimiterForTest(oldLimiter)
	})

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	db, err := gorm.Open(sqlite.Open("file:openai_key_limit_initial_channel?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	baseURL := constant.ChannelBaseURLs[constant.ChannelTypeOpenAI]
	autoBan := 1
	require.NoError(t, db.Create(&model.Channel{
		Id:       1,
		Type:     constant.ChannelTypeOpenAI,
		Key:      "sk-context",
		Status:   common.ChannelStatusEnabled,
		Name:     "official",
		BaseURL:  &baseURL,
		Group:    "default",
		AutoBan:  &autoBan,
		Models:   "gpt-4o-mini",
		Priority: common.GetPointer[int64](0),
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-4o-mini",
		ChannelId: 1,
		Enabled:   true,
	}).Error)

	setting.OpenAIUpstreamKeyLimitEnabled = true
	setting.OpenAIUpstreamKeyLimitConfigValue = setting.OpenAIUpstreamKeyLimitConfig{
		RPM:         3,
		TPM:         50000,
		RPD:         50,
		TPD:         200000,
		DailyWindow: "rolling_24h",
	}
	common.RedisEnabled = false
	service.SetOpenAIUpstreamKeyLimiterForTest(service.NewMemoryOpenAIUpstreamKeyLimiter(time.Unix(1000, 0)))

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-4o-mini",
		TokenGroup:      "default",
		Request:         &dto.GeneralOpenAIRequest{},
	}
	info.SetEstimatePromptTokens(1)
	retryParam := &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-4o-mini",
		Retry:      common.GetPointer(0),
	}

	for i := 0; i < 3; i++ {
		channel, err := getChannel(ctx, info, retryParam)
		require.Nil(t, err)
		require.Equal(t, 1, channel.Id)
	}

	_, apiErr := getChannel(ctx, info, retryParam)
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeOpenAIUpstreamKeyRateLimited, apiErr.GetErrorCode())
}

func TestGetChannelPreservesInitiallyDistributedMultiKeyContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 7)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(ctx, constant.ContextKeyChannelName, "official-multi")
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, constant.ChannelBaseURLs[constant.ChannelTypeOpenAI])
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "sk-a")
	common.SetContextKey(ctx, constant.ContextKeyChannelAutoBan, true)
	common.SetContextKey(ctx, constant.ContextKeyChannelIsMultiKey, true)
	common.SetContextKey(ctx, constant.ContextKeyChannelMultiKeyIndex, 0)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")

	oldEnabled := setting.OpenAIUpstreamKeyLimitEnabled
	oldConfig := setting.OpenAIUpstreamKeyLimitConfigValue
	oldRedis := common.RedisEnabled
	oldLimiter := service.GetOpenAIUpstreamKeyLimiterForTest()
	t.Cleanup(func() {
		setting.OpenAIUpstreamKeyLimitEnabled = oldEnabled
		setting.OpenAIUpstreamKeyLimitConfigValue = oldConfig
		common.RedisEnabled = oldRedis
		service.SetOpenAIUpstreamKeyLimiterForTest(oldLimiter)
	})

	setting.OpenAIUpstreamKeyLimitEnabled = true
	setting.OpenAIUpstreamKeyLimitConfigValue = setting.DefaultOpenAIUpstreamKeyLimitConfig()
	common.RedisEnabled = false
	service.SetOpenAIUpstreamKeyLimiterForTest(service.NewMemoryOpenAIUpstreamKeyLimiter(time.Unix(2000, 0)))

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-4o-mini",
		TokenGroup:      "default",
		Request:         &dto.GeneralOpenAIRequest{},
	}
	info.SetEstimatePromptTokens(1)
	retryParam := &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-4o-mini",
		Retry:      common.GetPointer(0),
	}

	channel, apiErr := getChannel(ctx, info, retryParam)

	require.Nil(t, apiErr)
	require.Equal(t, 7, channel.Id)
	require.True(t, channel.ChannelInfo.IsMultiKey)
	require.Equal(t, "sk-a", common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))
	require.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyChannelIsMultiKey))
	require.Equal(t, 0, common.GetContextKeyInt(ctx, constant.ContextKeyChannelMultiKeyIndex))
}
