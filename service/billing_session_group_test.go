package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedSubscriptionPlanForBillingGroupTest(t *testing.T, id int, allowedGroup string) {
	t.Helper()
	plan := &model.SubscriptionPlan{
		Id:            id,
		Title:         "Group Plan",
		PriceAmount:   9.99,
		Currency:      "USD",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
		UpgradeGroup:  allowedGroup,
	}
	require.NoError(t, model.DB.Create(plan).Error)
}

func seedSubscriptionPlanWithAvailableGroupsForBillingGroupTest(t *testing.T, id int, availableGroups []string) {
	t.Helper()
	plan := &model.SubscriptionPlan{
		Id:              id,
		Title:           "Group Plan",
		PriceAmount:     9.99,
		Currency:        "USD",
		DurationUnit:    model.SubscriptionDurationMonth,
		DurationValue:   1,
		Enabled:         true,
		TotalAmount:     1000,
		AvailableGroups: availableGroups,
	}
	require.NoError(t, model.DB.Create(plan).Error)
}

func seedUserSubscriptionForBillingGroupTest(t *testing.T, id int, userId int, planId int, allowedGroup string) {
	t.Helper()
	sub := &model.UserSubscription{
		Id:           id,
		UserId:       userId,
		PlanId:       planId,
		AmountTotal:  1000,
		AmountUsed:   0,
		Status:       "active",
		StartTime:    time.Now().Unix(),
		EndTime:      time.Now().Add(30 * 24 * time.Hour).Unix(),
		UpgradeGroup: allowedGroup,
	}
	require.NoError(t, model.DB.Create(sub).Error)
}

func seedUserSubscriptionWithAvailableGroupsForBillingGroupTest(t *testing.T, id int, userId int, planId int, availableGroups []string) {
	t.Helper()
	sub := &model.UserSubscription{
		Id:              id,
		UserId:          userId,
		PlanId:          planId,
		AmountTotal:     1000,
		AmountUsed:      0,
		Status:          "active",
		StartTime:       time.Now().Unix(),
		EndTime:         time.Now().Add(30 * 24 * time.Hour).Unix(),
		AvailableGroups: availableGroups,
	}
	require.NoError(t, model.DB.Create(sub).Error)
}

func seedUnlimitedTokenForBillingGroupTest(t *testing.T, id int, userId int, key string, remainQuota int) {
	t.Helper()
	token := &model.Token{
		Id:             id,
		UserId:         userId,
		Key:            key,
		Name:           "billing_group_token",
		Status:         common.TokenStatusEnabled,
		RemainQuota:    remainQuota,
		UnlimitedQuota: true,
		UsedQuota:      0,
	}
	require.NoError(t, model.DB.Create(token).Error)
}

func makeBillingGroupRelayInfo(userId int, tokenId int, tokenKey string, usingGroup string, preference string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:          userId,
		TokenId:         tokenId,
		TokenKey:        tokenKey,
		UsingGroup:      usingGroup,
		OriginModelName: "test-model",
		RequestId:       "req-" + usingGroup + "-" + preference,
		IsPlayground:    true,
		UserSetting: dto.UserSetting{
			BillingPreference: preference,
		},
	}
}

func getWalletQuotaForBillingGroupTest(t *testing.T, userId int) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", userId).First(&user).Error)
	return user.Quota
}

func getSubscriptionUsedForBillingGroupTest(t *testing.T, subId int) int64 {
	t.Helper()
	var sub model.UserSubscription
	require.NoError(t, model.DB.Select("amount_used").Where("id = ?", subId).First(&sub).Error)
	return sub.AmountUsed
}

func getTokenRemainForBillingGroupTest(t *testing.T, tokenId int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota").Where("id = ?", tokenId).First(&token).Error)
	return token.RemainQuota
}

func TestNewBillingSession_UsesSubscriptionOnlyWhenGroupMatches(t *testing.T) {
	truncate(t)

	userId := 7101
	tokenId := 8101
	subId := 9101
	planId := 9201
	allowedGroup := "vip"
	quota := 100
	seedUser(t, userId, 1000)
	seedUnlimitedTokenForBillingGroupTest(t, tokenId, userId, "billing-group-match", 1000)
	seedSubscriptionPlanForBillingGroupTest(t, planId, allowedGroup)
	seedUserSubscriptionForBillingGroupTest(t, subId, userId, planId, allowedGroup)

	ctx := &gin.Context{}
	ctx.Set("token_quota", 1000)
	relayInfo := makeBillingGroupRelayInfo(userId, tokenId, "billing-group-match", allowedGroup, "subscription_first")

	session, apiErr := NewBillingSession(ctx, relayInfo, quota)

	require.Nil(t, apiErr)
	require.NotNil(t, session)
	assert.Equal(t, BillingSourceSubscription, relayInfo.BillingSource)
	assert.Equal(t, quota, relayInfo.FinalPreConsumedQuota)
	assert.Equal(t, subId, relayInfo.SubscriptionId)
	assert.Equal(t, 1000, getWalletQuotaForBillingGroupTest(t, userId))
	assert.Equal(t, int64(quota), getSubscriptionUsedForBillingGroupTest(t, subId))
	assert.Equal(t, 1000, getTokenRemainForBillingGroupTest(t, tokenId))
}

func TestNewBillingSession_FallsBackToWalletWhenSubscriptionGroupDoesNotMatch(t *testing.T) {
	truncate(t)

	userId := 7102
	tokenId := 8102
	subId := 9102
	planId := 9202
	quota := 100
	seedUser(t, userId, 1000)
	seedUnlimitedTokenForBillingGroupTest(t, tokenId, userId, "billing-group-mismatch", 1000)
	seedSubscriptionPlanForBillingGroupTest(t, planId, "vip")
	seedUserSubscriptionForBillingGroupTest(t, subId, userId, planId, "vip")

	ctx := &gin.Context{}
	ctx.Set("token_quota", 1000)
	relayInfo := makeBillingGroupRelayInfo(userId, tokenId, "billing-group-mismatch", "default", "subscription_first")

	session, apiErr := NewBillingSession(ctx, relayInfo, quota)

	require.Nil(t, apiErr)
	require.NotNil(t, session)
	assert.Equal(t, BillingSourceWallet, relayInfo.BillingSource)
	assert.Equal(t, quota, relayInfo.FinalPreConsumedQuota)
	assert.Equal(t, 0, relayInfo.SubscriptionId)
	assert.Equal(t, 900, getWalletQuotaForBillingGroupTest(t, userId))
	assert.Equal(t, int64(0), getSubscriptionUsedForBillingGroupTest(t, subId))
	assert.Equal(t, 1000, getTokenRemainForBillingGroupTest(t, tokenId))
}

func TestNewBillingSession_SubscriptionWithoutAvailableGroupUsesWallet(t *testing.T) {
	truncate(t)

	userId := 7104
	tokenId := 8104
	subId := 9104
	planId := 9204
	quota := 100
	seedUser(t, userId, 1000)
	seedUnlimitedTokenForBillingGroupTest(t, tokenId, userId, "billing-group-empty", 1000)
	seedSubscriptionPlanForBillingGroupTest(t, planId, "")
	seedUserSubscriptionForBillingGroupTest(t, subId, userId, planId, "")

	ctx := &gin.Context{}
	ctx.Set("token_quota", 1000)
	relayInfo := makeBillingGroupRelayInfo(userId, tokenId, "billing-group-empty", "default", "subscription_first")

	session, apiErr := NewBillingSession(ctx, relayInfo, quota)

	require.Nil(t, apiErr)
	require.NotNil(t, session)
	assert.Equal(t, BillingSourceWallet, relayInfo.BillingSource)
	assert.Equal(t, quota, relayInfo.FinalPreConsumedQuota)
	assert.Equal(t, 0, relayInfo.SubscriptionId)
	assert.Equal(t, 900, getWalletQuotaForBillingGroupTest(t, userId))
	assert.Equal(t, int64(0), getSubscriptionUsedForBillingGroupTest(t, subId))
	assert.Equal(t, 1000, getTokenRemainForBillingGroupTest(t, tokenId))
}

func TestNewBillingSession_SubscriptionOnlyUsesWalletWhenGroupDoesNotMatch(t *testing.T) {
	truncate(t)

	userId := 7103
	tokenId := 8103
	subId := 9103
	planId := 9203
	quota := 100
	seedUser(t, userId, 1000)
	seedUnlimitedTokenForBillingGroupTest(t, tokenId, userId, "billing-group-only-mismatch", 1000)
	seedSubscriptionPlanForBillingGroupTest(t, planId, "vip")
	seedUserSubscriptionForBillingGroupTest(t, subId, userId, planId, "vip")

	ctx := &gin.Context{}
	ctx.Set("token_quota", 1000)
	relayInfo := makeBillingGroupRelayInfo(userId, tokenId, "billing-group-only-mismatch", "default", "subscription_only")

	session, apiErr := NewBillingSession(ctx, relayInfo, quota)

	require.Nil(t, apiErr)
	require.NotNil(t, session)
	assert.Equal(t, BillingSourceWallet, relayInfo.BillingSource)
	assert.Equal(t, quota, relayInfo.FinalPreConsumedQuota)
	assert.Equal(t, 0, relayInfo.SubscriptionId)
	assert.Equal(t, 900, getWalletQuotaForBillingGroupTest(t, userId))
	assert.Equal(t, int64(0), getSubscriptionUsedForBillingGroupTest(t, subId))
	assert.Equal(t, 1000, getTokenRemainForBillingGroupTest(t, tokenId))
}

func TestNewBillingSession_UsesSubscriptionWhenGroupMatchesRegardlessOfWalletPreference(t *testing.T) {
	for _, preference := range []string{"wallet_first", "wallet_only"} {
		t.Run(preference, func(t *testing.T) {
			truncate(t)

			userId := 7201
			tokenId := 8201
			subId := 9301
			planId := 9401
			quota := 100
			allowedGroup := "vip"
			tokenKey := "billing-group-wallet-pref-" + preference
			seedUser(t, userId, 1000)
			seedUnlimitedTokenForBillingGroupTest(t, tokenId, userId, tokenKey, 1000)
			seedSubscriptionPlanForBillingGroupTest(t, planId, allowedGroup)
			seedUserSubscriptionForBillingGroupTest(t, subId, userId, planId, allowedGroup)

			ctx := &gin.Context{}
			ctx.Set("token_quota", 1000)
			relayInfo := makeBillingGroupRelayInfo(userId, tokenId, tokenKey, allowedGroup, preference)

			session, apiErr := NewBillingSession(ctx, relayInfo, quota)

			require.Nil(t, apiErr)
			require.NotNil(t, session)
			assert.Equal(t, BillingSourceSubscription, relayInfo.BillingSource)
			assert.Equal(t, quota, relayInfo.FinalPreConsumedQuota)
			assert.Equal(t, subId, relayInfo.SubscriptionId)
			assert.Equal(t, 1000, getWalletQuotaForBillingGroupTest(t, userId))
			assert.Equal(t, int64(quota), getSubscriptionUsedForBillingGroupTest(t, subId))
			assert.Equal(t, 1000, getTokenRemainForBillingGroupTest(t, tokenId))
		})
	}
}

func TestNewBillingSession_PreservesSubscriptionPricedQuotaWhenGroupMatches(t *testing.T) {
	truncate(t)

	userId := 7204
	tokenId := 8204
	subId := 9306
	planId := 9406
	subscriptionPricedQuota := 140
	allowedGroup := "vip"
	tokenKey := "billing-group-subscription-priced-quota"
	seedUser(t, userId, 1000)
	seedUnlimitedTokenForBillingGroupTest(t, tokenId, userId, tokenKey, 1000)
	seedSubscriptionPlanForBillingGroupTest(t, planId, allowedGroup)
	seedUserSubscriptionForBillingGroupTest(t, subId, userId, planId, allowedGroup)

	ctx := &gin.Context{}
	ctx.Set("token_quota", 1000)
	relayInfo := makeBillingGroupRelayInfo(userId, tokenId, tokenKey, allowedGroup, "wallet_first")

	session, apiErr := NewBillingSession(ctx, relayInfo, subscriptionPricedQuota)

	require.Nil(t, apiErr)
	require.NotNil(t, session)
	assert.Equal(t, BillingSourceSubscription, relayInfo.BillingSource)
	assert.Equal(t, subscriptionPricedQuota, relayInfo.FinalPreConsumedQuota)
	assert.Equal(t, int64(subscriptionPricedQuota), getSubscriptionUsedForBillingGroupTest(t, subId))
	assert.Equal(t, 1000, getWalletQuotaForBillingGroupTest(t, userId))
}

func TestNewBillingSession_UsesSubscriptionWhenAnyAvailableGroupMatches(t *testing.T) {
	truncate(t)

	userId := 7301
	tokenId := 8301
	subId := 9304
	planId := 9404
	quota := 100
	tokenKey := "billing-group-multi-match"
	seedUser(t, userId, 1000)
	seedUnlimitedTokenForBillingGroupTest(t, tokenId, userId, tokenKey, 1000)
	seedSubscriptionPlanWithAvailableGroupsForBillingGroupTest(t, planId, []string{"vip", "svip"})
	seedUserSubscriptionWithAvailableGroupsForBillingGroupTest(t, subId, userId, planId, []string{"vip", "svip"})

	ctx := &gin.Context{}
	ctx.Set("token_quota", 1000)
	relayInfo := makeBillingGroupRelayInfo(userId, tokenId, tokenKey, "svip", "subscription_first")

	session, apiErr := NewBillingSession(ctx, relayInfo, quota)

	require.Nil(t, apiErr)
	require.NotNil(t, session)
	assert.Equal(t, BillingSourceSubscription, relayInfo.BillingSource)
	assert.Equal(t, quota, relayInfo.FinalPreConsumedQuota)
	assert.Equal(t, subId, relayInfo.SubscriptionId)
	assert.Equal(t, 1000, getWalletQuotaForBillingGroupTest(t, userId))
	assert.Equal(t, int64(quota), getSubscriptionUsedForBillingGroupTest(t, subId))
	assert.Equal(t, 1000, getTokenRemainForBillingGroupTest(t, tokenId))
}

func TestNewBillingSession_FallsBackToWalletWhenNoAvailableGroupsMatch(t *testing.T) {
	truncate(t)

	userId := 7302
	tokenId := 8302
	subId := 9305
	planId := 9405
	quota := 100
	tokenKey := "billing-group-multi-mismatch"
	seedUser(t, userId, 1000)
	seedUnlimitedTokenForBillingGroupTest(t, tokenId, userId, tokenKey, 1000)
	seedSubscriptionPlanWithAvailableGroupsForBillingGroupTest(t, planId, []string{"vip", "svip"})
	seedUserSubscriptionWithAvailableGroupsForBillingGroupTest(t, subId, userId, planId, []string{"vip", "svip"})

	ctx := &gin.Context{}
	ctx.Set("token_quota", 1000)
	relayInfo := makeBillingGroupRelayInfo(userId, tokenId, tokenKey, "default", "subscription_first")

	session, apiErr := NewBillingSession(ctx, relayInfo, quota)

	require.Nil(t, apiErr)
	require.NotNil(t, session)
	assert.Equal(t, BillingSourceWallet, relayInfo.BillingSource)
	assert.Equal(t, quota, relayInfo.FinalPreConsumedQuota)
	assert.Equal(t, 0, relayInfo.SubscriptionId)
	assert.Equal(t, 900, getWalletQuotaForBillingGroupTest(t, userId))
	assert.Equal(t, int64(0), getSubscriptionUsedForBillingGroupTest(t, subId))
	assert.Equal(t, 1000, getTokenRemainForBillingGroupTest(t, tokenId))
}

func TestNewBillingSession_GroupMatchDoesNotFallbackToWalletWhenSubscriptionInsufficient(t *testing.T) {
	for _, preference := range []string{"subscription_first", "wallet_first"} {
		t.Run(preference, func(t *testing.T) {
			truncate(t)

			userId := 7203
			tokenId := 8203
			subId := 9303
			planId := 9403
			quota := 100
			allowedGroup := "vip"
			tokenKey := "billing-group-sub-insufficient-" + preference
			seedUser(t, userId, 1000)
			seedUnlimitedTokenForBillingGroupTest(t, tokenId, userId, tokenKey, 1000)
			seedSubscriptionPlanForBillingGroupTest(t, planId, allowedGroup)
			seedUserSubscriptionForBillingGroupTest(t, subId, userId, planId, allowedGroup)
			require.NoError(t, model.DB.Model(&model.UserSubscription{}).
				Where("id = ?", subId).
				Updates(map[string]interface{}{
					"amount_total": 50,
					"amount_used":  0,
				}).Error)

			ctx := &gin.Context{}
			ctx.Set("token_quota", 1000)
			relayInfo := makeBillingGroupRelayInfo(userId, tokenId, tokenKey, allowedGroup, preference)

			session, apiErr := NewBillingSession(ctx, relayInfo, quota)

			require.Nil(t, session)
			require.NotNil(t, apiErr)
			assert.Contains(t, apiErr.Error(), "订阅额度")
			assert.Equal(t, 1000, getWalletQuotaForBillingGroupTest(t, userId))
			assert.Equal(t, int64(0), getSubscriptionUsedForBillingGroupTest(t, subId))
			assert.Equal(t, 1000, getTokenRemainForBillingGroupTest(t, tokenId))
		})
	}
}

func TestNewBillingSession_GroupMismatchDoesNotFallbackToSubscriptionWhenWalletInsufficient(t *testing.T) {
	truncate(t)

	userId := 7202
	tokenId := 8202
	subId := 9302
	planId := 9402
	quota := 100
	seedUser(t, userId, 50)
	seedUnlimitedTokenForBillingGroupTest(t, tokenId, userId, "billing-group-wallet-insufficient", 1000)
	seedSubscriptionPlanForBillingGroupTest(t, planId, "vip")
	seedUserSubscriptionForBillingGroupTest(t, subId, userId, planId, "vip")

	ctx := &gin.Context{}
	ctx.Set("token_quota", 1000)
	relayInfo := makeBillingGroupRelayInfo(userId, tokenId, "billing-group-wallet-insufficient", "default", "wallet_first")

	session, apiErr := NewBillingSession(ctx, relayInfo, quota)

	require.Nil(t, session)
	require.NotNil(t, apiErr)
	assert.Contains(t, apiErr.Error(), "用户剩余额度")
	assert.NotContains(t, apiErr.Error(), "订阅额度")
	assert.Equal(t, 50, getWalletQuotaForBillingGroupTest(t, userId))
	assert.Equal(t, int64(0), getSubscriptionUsedForBillingGroupTest(t, subId))
	assert.Equal(t, 1000, getTokenRemainForBillingGroupTest(t, tokenId))
}
