package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupLogStatRevenueTestDB(t *testing.T) {
	t.Helper()

	db := openTokenControllerTestDB(t)
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	require.NoError(t, db.AutoMigrate(
		&model.Log{},
		&model.TopUp{},
		&model.SubscriptionOrder{},
	))
}

func seedLogStatRevenue(t *testing.T) {
	t.Helper()

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	tenDaysAgo := todayStart - 10*24*60*60
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId:       1,
		PlanId:       1,
		Money:        32,
		TradeNo:      "sub-stat",
		Status:       common.TopUpStatusSuccess,
		CreateTime:   todayStart + 60,
		CompleteTime: todayStart + 120,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:       1,
		Money:        32,
		TradeNo:      "sub-stat",
		Status:       common.TopUpStatusSuccess,
		CreateTime:   todayStart + 60,
		CompleteTime: todayStart + 120,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:       2,
		Money:        10,
		TradeNo:      "wallet-stat",
		Status:       common.TopUpStatusSuccess,
		CreateTime:   todayStart + 180,
		CompleteTime: todayStart + 240,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:       3,
		Money:        7,
		TradeNo:      "wallet-ten-days-ago",
		Status:       common.TopUpStatusSuccess,
		CreateTime:   tenDaysAgo,
		CompleteTime: tenDaysAgo,
	}).Error)
}

func seedLogStatRevenueBalanceSubscription(t *testing.T) {
	t.Helper()

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId:          4,
		PlanId:          2,
		Money:           3.4,
		TradeNo:         "sub-balance-stat",
		PaymentMethod:   model.PaymentMethodBalance,
		PaymentProvider: model.PaymentProviderBalance,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      todayStart + 300,
		CompleteTime:    todayStart + 360,
	}).Error)
}

func callGetLogsStatWithRole(t *testing.T, role int, target string) tokenAPIResponse {
	t.Helper()

	if target == "" {
		target = "/api/log/stat"
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	ctx.Set("role", role)

	GetLogsStat(ctx)

	return decodeAPIResponse(t, recorder)
}

func TestGetLogsStatIncludesTodayRevenueForRootOnly(t *testing.T) {
	setupLogStatRevenueTestDB(t)
	seedLogStatRevenue(t)

	rootResponse := callGetLogsStatWithRole(t, common.RoleRootUser, "")
	require.True(t, rootResponse.Success, rootResponse.Message)
	var rootData map[string]any
	require.NoError(t, common.Unmarshal(rootResponse.Data, &rootData))
	require.InDelta(t, 42.0, rootData["today_revenue"].(float64), 0.001)

	adminResponse := callGetLogsStatWithRole(t, common.RoleAdminUser, "")
	require.True(t, adminResponse.Success, adminResponse.Message)
	var adminData map[string]any
	require.NoError(t, common.Unmarshal(adminResponse.Data, &adminData))
	_, exists := adminData["today_revenue"]
	require.False(t, exists)
}

func TestGetLogsStatRevenueFollowsSelectedTimeRange(t *testing.T) {
	setupLogStatRevenueTestDB(t)
	seedLogStatRevenue(t)

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	rangeStart := todayStart - 20*24*60*60
	rangeEnd := todayStart + 24*60*60
	target := "/api/log/stat?start_timestamp=" + strconv.FormatInt(rangeStart, 10) +
		"&end_timestamp=" + strconv.FormatInt(rangeEnd, 10)

	response := callGetLogsStatWithRole(t, common.RoleRootUser, target)

	require.True(t, response.Success, response.Message)
	var data map[string]any
	require.NoError(t, common.Unmarshal(response.Data, &data))
	require.InDelta(t, 49.0, data["today_revenue"].(float64), 0.001)
}

func TestGetLogsStatRevenueExcludesBalanceSubscriptionOrders(t *testing.T) {
	setupLogStatRevenueTestDB(t)
	seedLogStatRevenue(t)
	seedLogStatRevenueBalanceSubscription(t)

	response := callGetLogsStatWithRole(t, common.RoleRootUser, "")

	require.True(t, response.Success, response.Message)
	var data map[string]any
	require.NoError(t, common.Unmarshal(response.Data, &data))
	require.InDelta(t, 42.0, data["today_revenue"].(float64), 0.001)
}

func createActualQuotaLog(t *testing.T, log model.Log) {
	t.Helper()

	require.NoError(t, model.LOG_DB.Create(&log).Error)
}

func TestGetLogsStatActualQuotaUsesLoggedRatioAndFallbacksForRootOnly(t *testing.T) {
	setupLogStatRevenueTestDB(t)

	createdAt := time.Now().Unix() - 60
	baseLog := model.Log{
		CreatedAt: createdAt,
		Type:      model.LogTypeConsume,
		Username:  "alice",
		TokenName: "token-a",
		ModelName: "gpt-4",
		ChannelId: 7,
		Group:     "premium",
	}
	testCases := []struct {
		name  string
		quota int
		other string
	}{
		{name: "ratio one", quota: 100, other: `{"group_ratio":1}`},
		{name: "fractional ratio", quota: 230, other: `{"group_ratio":0.23}`},
		{name: "missing ratio", quota: 50, other: `{}`},
		{name: "zero ratio", quota: 60, other: `{"group_ratio":0}`},
		{name: "negative ratio", quota: 70, other: `{"group_ratio":-2}`},
		{name: "malformed json", quota: 80, other: `not-json`},
		{name: "non-numeric ratio", quota: 90, other: `{"group_ratio":"abc"}`},
		{name: "numeric-prefix string", quota: 110, other: `{"group_ratio":"0.23junk"}`},
		{name: "malformed json with ratio fragment", quota: 100, other: `not-json "group_ratio":0.23`},
		{name: "nested ratio", quota: 120, other: `{"nested":{"group_ratio":0.23}}`},
		{name: "ratio causing overflow", quota: 130, other: `{"group_ratio":5e-324}`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			log := baseLog
			log.Quota = testCase.quota
			log.Other = testCase.other
			createActualQuotaLog(t, log)
		})
	}

	refundLog := baseLog
	refundLog.Type = model.LogTypeRefund
	refundLog.Quota = 900
	refundLog.Other = `{"group_ratio":0.1}`
	createActualQuotaLog(t, refundLog)

	rootResponse := callGetLogsStatWithRole(t, common.RoleRootUser, "")
	require.True(t, rootResponse.Success, rootResponse.Message)
	var rootData map[string]any
	require.NoError(t, common.Unmarshal(rootResponse.Data, &rootData))
	actualQuota, exists := rootData["actual_quota"]
	require.True(t, exists)
	require.InDelta(t, 1910.0, actualQuota.(float64), 0.001)

	adminResponse := callGetLogsStatWithRole(t, common.RoleAdminUser, "")
	require.True(t, adminResponse.Success, adminResponse.Message)
	var adminData map[string]any
	require.NoError(t, common.Unmarshal(adminResponse.Data, &adminData))
	_, exists = adminData["actual_quota"]
	require.False(t, exists)
}

func TestGetLogsStatActualQuotaFollowsLogFilters(t *testing.T) {
	setupLogStatRevenueTestDB(t)

	createdAt := time.Now().Unix() - 60
	targetLog := model.Log{
		CreatedAt: createdAt,
		Type:      model.LogTypeConsume,
		Username:  "alice",
		TokenName: "token-a",
		ModelName: "gpt-4",
		Quota:     200,
		ChannelId: 7,
		Group:     "premium",
		Other:     `{"group_ratio":0.25}`,
	}
	createActualQuotaLog(t, targetLog)

	distractors := []model.Log{
		{CreatedAt: createdAt - 100, Type: model.LogTypeConsume, Username: "alice", TokenName: "token-a", ModelName: "gpt-4", Quota: 100, ChannelId: 7, Group: "premium", Other: `{"group_ratio":0.5}`},
		{CreatedAt: createdAt + 100, Type: model.LogTypeConsume, Username: "alice", TokenName: "token-a", ModelName: "gpt-4", Quota: 100, ChannelId: 7, Group: "premium", Other: `{"group_ratio":0.5}`},
		{CreatedAt: createdAt, Type: model.LogTypeConsume, Username: "bob", TokenName: "token-a", ModelName: "gpt-4", Quota: 100, ChannelId: 7, Group: "premium", Other: `{"group_ratio":0.5}`},
		{CreatedAt: createdAt, Type: model.LogTypeConsume, Username: "alice", TokenName: "token-b", ModelName: "gpt-4", Quota: 100, ChannelId: 7, Group: "premium", Other: `{"group_ratio":0.5}`},
		{CreatedAt: createdAt, Type: model.LogTypeConsume, Username: "alice", TokenName: "token-a", ModelName: "gpt-3.5", Quota: 100, ChannelId: 7, Group: "premium", Other: `{"group_ratio":0.5}`},
		{CreatedAt: createdAt, Type: model.LogTypeConsume, Username: "alice", TokenName: "token-a", ModelName: "gpt-4", Quota: 100, ChannelId: 8, Group: "premium", Other: `{"group_ratio":0.5}`},
		{CreatedAt: createdAt, Type: model.LogTypeConsume, Username: "alice", TokenName: "token-a", ModelName: "gpt-4", Quota: 100, ChannelId: 7, Group: "basic", Other: `{"group_ratio":0.5}`},
	}
	for _, log := range distractors {
		createActualQuotaLog(t, log)
	}

	target := "/api/log/stat?start_timestamp=" + strconv.FormatInt(createdAt-1, 10) +
		"&end_timestamp=" + strconv.FormatInt(createdAt+1, 10) +
		"&username=alice&token_name=token-a&model_name=gpt-4&channel=7&group=premium"
	response := callGetLogsStatWithRole(t, common.RoleRootUser, target)

	require.True(t, response.Success, response.Message)
	var data map[string]any
	require.NoError(t, common.Unmarshal(response.Data, &data))
	actualQuota, exists := data["actual_quota"]
	require.True(t, exists)
	require.InDelta(t, 800.0, actualQuota.(float64), 0.001)
}
