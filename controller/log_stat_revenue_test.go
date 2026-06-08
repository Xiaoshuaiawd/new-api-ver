package controller

import (
	"net/http"
	"net/http/httptest"
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
}

func callGetLogsStatWithRole(t *testing.T, role int) tokenAPIResponse {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/log/stat", nil)
	ctx.Set("role", role)

	GetLogsStat(ctx)

	return decodeAPIResponse(t, recorder)
}

func TestGetLogsStatIncludesTodayRevenueForRootOnly(t *testing.T) {
	setupLogStatRevenueTestDB(t)
	seedLogStatRevenue(t)

	rootResponse := callGetLogsStatWithRole(t, common.RoleRootUser)
	require.True(t, rootResponse.Success, rootResponse.Message)
	var rootData map[string]any
	require.NoError(t, common.Unmarshal(rootResponse.Data, &rootData))
	require.InDelta(t, 42.0, rootData["today_revenue"].(float64), 0.001)

	adminResponse := callGetLogsStatWithRole(t, common.RoleAdminUser)
	require.True(t, adminResponse.Success, adminResponse.Message)
	var adminData map[string]any
	require.NoError(t, common.Unmarshal(adminResponse.Data, &adminData))
	_, exists := adminData["today_revenue"]
	require.False(t, exists)
}
