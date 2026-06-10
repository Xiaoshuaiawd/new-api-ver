package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedRedemptionUser(t *testing.T, userId int, quota int) {
	t.Helper()
	require.NoError(t, DB.Create(&User{
		Id:       userId,
		Username: "redemption_user",
		Status:   common.UserStatusEnabled,
		Quota:    quota,
	}).Error)
}

func TestRedeemQuotaCodeKeepsLegacyBehavior(t *testing.T) {
	truncateTables(t)

	seedRedemptionUser(t, 501, 100)
	require.NoError(t, DB.Create(&Redemption{
		Name:        "legacy quota",
		Key:         "legacy-quota-key",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       250,
		CreatedTime: common.GetTimestamp(),
	}).Error)

	result, err := Redeem("legacy-quota-key", 501)
	require.NoError(t, err)
	assert.Equal(t, RedemptionRewardTypeQuota, result.Type)
	assert.Equal(t, 250, result.Quota)

	var user User
	require.NoError(t, DB.Select("quota").Where("id = ?", 501).First(&user).Error)
	assert.Equal(t, 350, user.Quota)

	var redemption Redemption
	require.NoError(t, DB.Where("key = ?", "legacy-quota-key").First(&redemption).Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, redemption.Status)
	assert.Equal(t, 501, redemption.UsedUserId)
}

func TestRedeemSubscriptionCodeCreatesUserSubscriptionFromPlan(t *testing.T) {
	truncateTables(t)

	seedRedemptionUser(t, 502, 100)
	plan := &SubscriptionPlan{
		Id:                      701,
		Title:                   "Codex Day",
		PriceAmount:             32,
		Currency:                "CNY",
		DurationUnit:            SubscriptionDurationDay,
		DurationValue:           1,
		Enabled:                 true,
		TotalAmount:             200000,
		QuotaResetPeriod:        SubscriptionResetDaily,
		AvailableGroups:         SubscriptionAvailableGroups{"Codex", "Codex-combo"},
		MaxPurchasePerUser:      1,
		QuotaResetCustomSeconds: 0,
	}
	require.NoError(t, DB.Create(plan).Error)
	require.NoError(t, DB.Create(&Redemption{
		Name:        "subscription code",
		Key:         "subscription-key",
		Status:      common.RedemptionCodeStatusEnabled,
		RewardType:  RedemptionRewardTypeSubscription,
		PlanId:      plan.Id,
		CreatedTime: common.GetTimestamp(),
	}).Error)

	result, err := Redeem("subscription-key", 502)
	require.NoError(t, err)
	assert.Equal(t, RedemptionRewardTypeSubscription, result.Type)
	assert.Equal(t, plan.Id, result.PlanId)
	assert.Equal(t, plan.Title, result.PlanTitle)
	assert.Equal(t, 0, result.Quota)

	var user User
	require.NoError(t, DB.Select("quota").Where("id = ?", 502).First(&user).Error)
	assert.Equal(t, 100, user.Quota)

	var sub UserSubscription
	require.NoError(t, DB.Where("user_id = ? AND plan_id = ?", 502, plan.Id).First(&sub).Error)
	assert.Equal(t, "active", sub.Status)
	assert.Equal(t, "redemption", sub.Source)
	assert.Equal(t, plan.TotalAmount, sub.AmountTotal)
	assert.Equal(t, SubscriptionAvailableGroups{"Codex", "Codex-combo"}, sub.AvailableGroups)
	assert.Equal(t, "Codex", sub.UpgradeGroup)
	assert.Greater(t, sub.EndTime, sub.StartTime)
	assert.Greater(t, sub.NextResetTime, int64(0))
}

func TestRedeemSubscriptionCodeRejectsInvalidPlan(t *testing.T) {
	truncateTables(t)

	seedRedemptionUser(t, 503, 100)
	require.NoError(t, DB.Create(&Redemption{
		Name:        "bad subscription code",
		Key:         "bad-subscription-key",
		Status:      common.RedemptionCodeStatusEnabled,
		RewardType:  RedemptionRewardTypeSubscription,
		PlanId:      9999,
		CreatedTime: common.GetTimestamp(),
	}).Error)

	_, err := Redeem("bad-subscription-key", 503)
	require.Error(t, err)

	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", 503).Count(&count).Error)
	assert.Zero(t, count)

	var redemption Redemption
	require.NoError(t, DB.Where("key = ?", "bad-subscription-key").First(&redemption).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, redemption.Status)
}

func TestSearchRedemptionsFindsByCode(t *testing.T) {
	truncateTables(t)

	require.NoError(t, DB.Create(&Redemption{
		Name:        "search by code",
		Key:         "CODE-SEARCH-123456",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       100,
		CreatedTime: common.GetTimestamp(),
	}).Error)
	require.NoError(t, DB.Create(&Redemption{
		Name:        "other code",
		Key:         "OTHER-CODE-654321",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       100,
		CreatedTime: common.GetTimestamp(),
	}).Error)

	redemptions, total, err := SearchRedemptions("CODE-SEARCH", 0, 10)

	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, redemptions, 1)
	assert.Equal(t, "CODE-SEARCH-123456", redemptions[0].Key)
}
