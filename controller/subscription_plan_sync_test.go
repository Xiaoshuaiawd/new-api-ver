package controller

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupSubscriptionPlanSyncTestDB(t *testing.T) {
	t.Helper()

	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.SubscriptionPlan{},
		&model.UserSubscription{},
	))
}

func TestAdminUpdateSubscriptionPlanSyncsExistingUserSubscriptions(t *testing.T) {
	setupSubscriptionPlanSyncTestDB(t)
	confirmPaymentComplianceForTest(t)

	previousGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatio))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"Codex":1,"Codex-combo":1,"Codex-sale":1}`))

	now := time.Now().Unix()
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:              10,
		Title:           "Codex Plan",
		PriceAmount:     3.4,
		Currency:        "USD",
		DurationUnit:    model.SubscriptionDurationDay,
		DurationValue:   1,
		Enabled:         true,
		TotalAmount:     15000000,
		AvailableGroups: model.SubscriptionAvailableGroups{"Codex", "Codex-combo"},
	}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:              11,
		Title:           "Other Plan",
		PriceAmount:     3.4,
		Currency:        "USD",
		DurationUnit:    model.SubscriptionDurationDay,
		DurationValue:   1,
		Enabled:         true,
		TotalAmount:     15000000,
		AvailableGroups: model.SubscriptionAvailableGroups{"Codex"},
	}).Error)
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id:              101,
		UserId:          201,
		PlanId:          10,
		AmountTotal:     15000000,
		AmountUsed:      0,
		Status:          "active",
		StartTime:       now,
		EndTime:         now + 86400,
		AvailableGroups: model.SubscriptionAvailableGroups{"Codex", "Codex-combo"},
	}).Error)
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id:              102,
		UserId:          202,
		PlanId:          11,
		AmountTotal:     15000000,
		AmountUsed:      0,
		Status:          "active",
		StartTime:       now,
		EndTime:         now + 86400,
		AvailableGroups: model.SubscriptionAvailableGroups{"Codex"},
	}).Error)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/subscription/plan/10", map[string]any{
		"plan": map[string]any{
			"title":             "Codex Plan",
			"price_amount":      3.4,
			"currency":          "USD",
			"duration_unit":     model.SubscriptionDurationDay,
			"duration_value":    1,
			"enabled":           true,
			"total_amount":      15000000,
			"available_groups":  []string{"Codex-combo", "Codex-sale"},
			"allow_balance_pay": true,
		},
	}, 1)
	ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: "10"})

	AdminUpdateSubscriptionPlan(ctx)

	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	var updatedSub model.UserSubscription
	require.NoError(t, model.DB.First(&updatedSub, "id = ?", 101).Error)
	require.Equal(t, model.SubscriptionAvailableGroups{"Codex-combo", "Codex-sale"}, updatedSub.AvailableGroups)
	require.Equal(t, "Codex-combo", updatedSub.UpgradeGroup)

	hasCodex, err := model.HasActiveUserSubscriptionForGroup(201, "Codex")
	require.NoError(t, err)
	require.False(t, hasCodex)
	hasSale, err := model.HasActiveUserSubscriptionForGroup(201, "Codex-sale")
	require.NoError(t, err)
	require.True(t, hasSale)

	var untouchedSub model.UserSubscription
	require.NoError(t, model.DB.First(&untouchedSub, "id = ?", 102).Error)
	require.Equal(t, model.SubscriptionAvailableGroups{"Codex"}, untouchedSub.AvailableGroups)
	require.Equal(t, "Codex", untouchedSub.UpgradeGroup)
}
