package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestModelPriceHelperTieredUsesPreloadedRequestInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"tiered-test-model":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"tiered-test-model":"param(\"stream\") == true ? tier(\"stream\", p * 3) : tier(\"base\", p * 2)"}`,
	}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/channel/test/1", nil)
	req.Body = nil
	req.ContentLength = 0
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	ctx.Set("group", "default")

	info := &relaycommon.RelayInfo{
		OriginModelName: "tiered-test-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		RequestHeaders:  map[string]string{"Content-Type": "application/json"},
		BillingRequestInput: &billingexpr.RequestInput{
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"stream":true}`),
		},
	}

	priceData, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Equal(t, 1500, priceData.QuotaToPreConsume)
	require.NotNil(t, info.TieredBillingSnapshot)
	require.Equal(t, "stream", info.TieredBillingSnapshot.EstimatedTier)
	require.Equal(t, billing_setting.BillingModeTieredExpr, info.TieredBillingSnapshot.BillingMode)
	require.Equal(t, common.QuotaPerUnit, info.TieredBillingSnapshot.QuotaPerUnit)
}

func setupPriceHelperSubscriptionDB(t *testing.T) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	previousDB := model.DB
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	t.Cleanup(func() {
		model.DB = previousDB
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		_ = sqlDB.Close()
	})

	model.DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false

	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.SubscriptionPlan{},
		&model.UserSubscription{},
		&model.SubscriptionPreConsumeRecord{},
	))
}

func seedPriceHelperSubscription(t *testing.T, userId int, usingGroup string) {
	t.Helper()

	require.NoError(t, model.DB.Create(&model.User{
		Id:       userId,
		Username: "price_helper_user",
		Quota:    100000,
		Status:   common.UserStatusEnabled,
	}).Error)

	plan := &model.SubscriptionPlan{
		Id:              6101,
		Title:           "Price Helper Plan",
		PriceAmount:     9.99,
		Currency:        "USD",
		DurationUnit:    model.SubscriptionDurationMonth,
		DurationValue:   1,
		Enabled:         true,
		TotalAmount:     100000,
		AvailableGroups: []string{usingGroup},
	}
	require.NoError(t, model.DB.Create(plan).Error)

	sub := &model.UserSubscription{
		Id:              6201,
		UserId:          userId,
		PlanId:          plan.Id,
		AmountTotal:     100000,
		AmountUsed:      0,
		Status:          "active",
		StartTime:       time.Now().Add(-time.Hour).Unix(),
		EndTime:         time.Now().Add(24 * time.Hour).Unix(),
		AvailableGroups: []string{usingGroup},
	}
	require.NoError(t, model.DB.Create(sub).Error)
}

func TestModelPriceHelperUsesSubscriptionGroupRatioWhenGroupHasSubscription(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPriceHelperSubscriptionDB(t)

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"group_ratio_setting.group_ratio":              `{"vip":0.13}`,
		"group_ratio_setting.subscription_group_ratio": `{"vip":0.7}`,
	}))

	previousModelRatio := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousModelRatio))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"price-helper-model":2}`))

	userId := 6111
	seedPriceHelperSubscription(t, userId, "vip")

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{
		UserId:          userId,
		OriginModelName: "price-helper-model",
		UserGroup:       "default",
		UsingGroup:      "vip",
	}

	priceData, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})

	require.NoError(t, err)
	require.Equal(t, 0.7, priceData.GroupRatioInfo.GroupRatio)
	require.Equal(t, "subscription", priceData.GroupRatioInfo.BillingSource)
	require.Equal(t, 1400, priceData.QuotaToPreConsume)
}

func TestHandleGroupRatioKeepsResolvedSubscriptionSource(t *testing.T) {
	gin.SetMode(gin.TestMode)

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"group_ratio_setting.group_ratio":              `{"vip":0.13}`,
		"group_ratio_setting.subscription_group_ratio": `{"vip":0.7}`,
	}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{
		OriginModelName: "price-helper-model",
		UserGroup:       "default",
		UsingGroup:      "vip",
		BillingSource:   "subscription",
	}

	groupRatioInfo := HandleGroupRatio(ctx, info)

	require.Equal(t, 0.7, groupRatioInfo.GroupRatio)
	require.Equal(t, "subscription", groupRatioInfo.BillingSource)
}
