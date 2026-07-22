package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	referralTestInviterID = 9101
	referralTestInviteeID = 9102
)

func resetReferralTestData(t *testing.T) {
	t.Helper()
	tables := []string{
		"referral_rewards",
		"referral_invites",
		"top_ups",
		"users",
		"logs",
	}
	for _, table := range tables {
		require.NoError(t, DB.Exec("DELETE FROM "+table).Error)
	}
	t.Cleanup(func() {
		for _, table := range tables {
			require.NoError(t, DB.Exec("DELETE FROM "+table).Error)
		}
	})
}

func useReferralRewardSetting(t *testing.T, delayDays int) {
	t.Helper()
	paymentSetting := operation_setting.GetPaymentSetting()
	original := *paymentSetting
	t.Cleanup(func() {
		*paymentSetting = original
	})

	cfg := operation_setting.DefaultReferralFirstTopUpRewardSetting()
	cfg.Enabled = true
	cfg.InviterSettleDelayDays = delayDays
	paymentSetting.ReferralFirstTopUpReward = cfg
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion =
		operation_setting.CurrentComplianceTermsVersion
}

func seedReferralRewardUsers(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Create(&User{
		Id:       referralTestInviterID,
		Username: "referral_inviter",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "TINV",
	}).Error)
	require.NoError(t, DB.Create(&User{
		Id:        referralTestInviteeID,
		Username:  "referral_invitee",
		Status:    common.UserStatusEnabled,
		Group:     "default",
		AffCode:   "TIVT",
		InviterId: referralTestInviterID,
	}).Error)
}

func createReferralTopUp(t *testing.T, tradeNo string, money float64, completeTime int64) TopUp {
	t.Helper()
	topUp := TopUp{
		UserId:          referralTestInviteeID,
		Amount:          100,
		Money:           money,
		TradeNo:         tradeNo,
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe,
		BaseQuota:       1000,
		CompleteTime:    completeTime,
		Status:          common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(&topUp).Error)
	return topUp
}

func applyReferralRewardForTest(t *testing.T, topUp *TopUp) {
	t.Helper()
	settlement := TopUpSettlement{
		BaseAmount: 100,
		BaseQuota:  1000,
		TotalQuota: 1000,
	}
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return applyReferralFirstTopUpRewardTx(tx, topUp, settlement)
	}))
}

func TestReferralFirstTopUpRewardIncludesThirtyAndIsIdempotent(t *testing.T) {
	resetReferralTestData(t)
	useReferralRewardSetting(t, 7)
	seedReferralRewardUsers(t)

	topUp := createReferralTopUp(t, "referral-thirty", 30, common.GetTimestamp())
	applyReferralRewardForTest(t, &topUp)
	applyReferralRewardForTest(t, &topUp)

	var rewards []ReferralReward
	require.NoError(t, DB.Order("reward_role asc").Find(&rewards).Error)
	require.Len(t, rewards, 2)

	byRole := map[string]ReferralReward{}
	for _, reward := range rewards {
		byRole[reward.RewardRole] = reward
	}
	assert.Equal(t, ReferralRewardStatusSettled, byRole[ReferralRewardRoleInvitee].Status)
	assert.Equal(t, 100, byRole[ReferralRewardRoleInvitee].RewardQuota)
	assert.Equal(t, 100, byRole[ReferralRewardRoleInvitee].SettledQuota)
	assert.Equal(t, ReferralRewardStatusPending, byRole[ReferralRewardRoleInviter].Status)
	assert.Equal(t, 100, byRole[ReferralRewardRoleInviter].RewardQuota)

	var invitee User
	require.NoError(t, DB.Select("quota").First(&invitee, referralTestInviteeID).Error)
	assert.Equal(t, 100, invitee.Quota)

	var inviter User
	require.NoError(t, DB.Select("aff_quota").First(&inviter, referralTestInviterID).Error)
	assert.Equal(t, 0, inviter.AffQuota)
}

func TestReferralFirstTopUpRewardRejectsBelowThresholdAndStrictLaterTopUp(t *testing.T) {
	resetReferralTestData(t)
	useReferralRewardSetting(t, 7)
	seedReferralRewardUsers(t)

	now := common.GetTimestamp()
	firstLow := createReferralTopUp(t, "referral-below-threshold", 29.99, now)
	applyReferralRewardForTest(t, &firstLow)

	laterQualified := createReferralTopUp(t, "referral-later-qualified", 50, now+1)
	applyReferralRewardForTest(t, &laterQualified)

	var count int64
	require.NoError(t, DB.Model(&ReferralReward{}).Count(&count).Error)
	assert.Zero(t, count)

	var invitee User
	require.NoError(t, DB.Select("quota").First(&invitee, referralTestInviteeID).Error)
	assert.Equal(t, 0, invitee.Quota)
}

func TestReferralInviterRewardSettlesAfterDelayWindow(t *testing.T) {
	resetReferralTestData(t)
	useReferralRewardSetting(t, 0)
	seedReferralRewardUsers(t)

	topUp := createReferralTopUp(t, "referral-settle", 30, common.GetTimestamp())
	applyReferralRewardForTest(t, &topUp)

	settled, err := SettleDueReferralRewards(10)
	require.NoError(t, err)
	assert.Equal(t, 1, settled)

	var reward ReferralReward
	require.NoError(t, DB.Where("reward_role = ?", ReferralRewardRoleInviter).First(&reward).Error)
	assert.Equal(t, ReferralRewardStatusSettled, reward.Status)
	assert.Equal(t, 100, reward.SettledQuota)

	var inviter User
	require.NoError(t, DB.Select("aff_quota", "aff_history").First(&inviter, referralTestInviterID).Error)
	assert.Equal(t, 100, inviter.AffQuota)
	assert.Equal(t, 100, inviter.AffHistoryQuota)
}

func TestReferralRewardRefundReversesSettledAndCancelsPending(t *testing.T) {
	resetReferralTestData(t)
	useReferralRewardSetting(t, 7)
	seedReferralRewardUsers(t)

	topUp := createReferralTopUp(t, "referral-refund", 30, common.GetTimestamp())
	applyReferralRewardForTest(t, &topUp)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return reverseReferralRewardsForTopUpTx(tx, &topUp, 30)
	}))

	var invitee User
	require.NoError(t, DB.Select("quota").First(&invitee, referralTestInviteeID).Error)
	assert.Equal(t, 0, invitee.Quota)

	var rewards []ReferralReward
	require.NoError(t, DB.Find(&rewards).Error)
	require.Len(t, rewards, 2)
	byRole := map[string]ReferralReward{}
	for _, reward := range rewards {
		byRole[reward.RewardRole] = reward
	}
	assert.Equal(t, ReferralRewardStatusReversed, byRole[ReferralRewardRoleInvitee].Status)
	assert.Equal(t, 100, byRole[ReferralRewardRoleInvitee].ReversedQuota)
	assert.Equal(t, ReferralRewardStatusCancelled, byRole[ReferralRewardRoleInviter].Status)
	assert.Equal(t, 100, byRole[ReferralRewardRoleInviter].ReversedQuota)
}

func TestReferralRewardFirstTopUpFifty(t *testing.T) {
	resetReferralTestData(t)
	useReferralRewardSetting(t, 7)
	seedReferralRewardUsers(t)

	topUp := createReferralTopUp(t, "referral-fifty", 50, common.GetTimestamp())
	applyReferralRewardForTest(t, &topUp)

	var rewards []ReferralReward
	require.NoError(t, DB.Order("reward_role asc").Find(&rewards).Error)
	require.Len(t, rewards, 2)

	byRole := map[string]ReferralReward{}
	for _, reward := range rewards {
		byRole[reward.RewardRole] = reward
	}

	assert.Equal(t, ReferralRewardStatusSettled, byRole[ReferralRewardRoleInvitee].Status)
	assert.Equal(t, 100, byRole[ReferralRewardRoleInvitee].RewardQuota)
	assert.Equal(t, 100, byRole[ReferralRewardRoleInvitee].SettledQuota)
	assert.Equal(t, ReferralRewardStatusPending, byRole[ReferralRewardRoleInviter].Status)
	assert.Equal(t, 100, byRole[ReferralRewardRoleInviter].RewardQuota)
	assert.Equal(t, 0, byRole[ReferralRewardRoleInviter].SettledQuota)

	var invitee User
	require.NoError(t, DB.Select("quota").First(&invitee, referralTestInviteeID).Error)
	assert.Equal(t, 100, invitee.Quota)

	var inviter User
	require.NoError(t, DB.Select("aff_quota").First(&inviter, referralTestInviterID).Error)
	assert.Equal(t, 0, inviter.AffQuota)
}

func TestReferralRewardNoInviter(t *testing.T) {
	resetReferralTestData(t)
	useReferralRewardSetting(t, 7)

	noInviterUserID := 9103
	require.NoError(t, DB.Create(&User{
		Id:       noInviterUserID,
		Username: "no_inviter_user",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "NOINV",
	}).Error)

	topUp := TopUp{
		UserId:          noInviterUserID,
		Amount:          100,
		Money:           50,
		TradeNo:         "no-inviter-topup",
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe,
		BaseQuota:       1000,
		CompleteTime:    common.GetTimestamp(),
		Status:          common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(&topUp).Error)

	settlement := TopUpSettlement{
		BaseAmount: 100,
		BaseQuota:  1000,
		TotalQuota: 1000,
	}
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return applyReferralFirstTopUpRewardTx(tx, &topUp, settlement)
	}))

	var count int64
	require.NoError(t, DB.Model(&ReferralReward{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestReferralRewardDisabledInviter(t *testing.T) {
	resetReferralTestData(t)
	useReferralRewardSetting(t, 7)
	seedReferralRewardUsers(t)

	require.NoError(t, DB.Model(&User{}).Where("id = ?", referralTestInviterID).Update("status", common.UserStatusDisabled).Error)

	topUp := createReferralTopUp(t, "disabled-inviter", 50, common.GetTimestamp())
	applyReferralRewardForTest(t, &topUp)

	var rewards []ReferralReward
	require.NoError(t, DB.Order("reward_role asc").Find(&rewards).Error)
	require.Len(t, rewards, 2)

	for _, reward := range rewards {
		assert.Equal(t, ReferralRewardStatusCancelled, reward.Status)
		assert.Equal(t, ReferralRewardRiskRejected, reward.RiskStatus)
		assert.Equal(t, "invalid_inviter", reward.RiskReason)
		assert.Equal(t, 0, reward.RewardQuota)
	}

	var invitee User
	require.NoError(t, DB.Select("quota").First(&invitee, referralTestInviteeID).Error)
	assert.Equal(t, 0, invitee.Quota)
}

func TestReferralRewardZeroBaseQuota(t *testing.T) {
	resetReferralTestData(t)
	useReferralRewardSetting(t, 7)
	seedReferralRewardUsers(t)

	topUp := TopUp{
		UserId:          referralTestInviteeID,
		Amount:          100,
		Money:           50,
		TradeNo:         "zero-base-quota",
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe,
		BaseQuota:       0,
		CompleteTime:    common.GetTimestamp(),
		Status:          common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(&topUp).Error)

	settlement := TopUpSettlement{
		BaseAmount: 100,
		BaseQuota:  0,
		TotalQuota: 0,
	}
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return applyReferralFirstTopUpRewardTx(tx, &topUp, settlement)
	}))

	var count int64
	require.NoError(t, DB.Model(&ReferralReward{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestReferralPartialRefund(t *testing.T) {
	resetReferralTestData(t)
	useReferralRewardSetting(t, 0)
	seedReferralRewardUsers(t)

	topUp := createReferralTopUp(t, "partial-refund", 50, common.GetTimestamp())
	applyReferralRewardForTest(t, &topUp)

	settled, err := SettleDueReferralRewards(10)
	require.NoError(t, err)
	assert.Equal(t, 1, settled)

	var inviter User
	require.NoError(t, DB.Select("aff_quota").First(&inviter, referralTestInviterID).Error)
	assert.Equal(t, 100, inviter.AffQuota)

	partialRefundAmount := 25.0
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return reverseReferralRewardsForTopUpTx(tx, &topUp, partialRefundAmount)
	}))

	var rewards []ReferralReward
	require.NoError(t, DB.Order("reward_role asc").Find(&rewards).Error)
	require.Len(t, rewards, 2)

	byRole := map[string]ReferralReward{}
	for _, reward := range rewards {
		byRole[reward.RewardRole] = reward
	}

	expectedReversed := 50
	assert.Equal(t, ReferralRewardStatusPartialReversed, byRole[ReferralRewardRoleInvitee].Status)
	assert.Equal(t, expectedReversed, byRole[ReferralRewardRoleInvitee].ReversedQuota)
	assert.Equal(t, ReferralRewardStatusPartialReversed, byRole[ReferralRewardRoleInviter].Status)
	assert.Equal(t, expectedReversed, byRole[ReferralRewardRoleInviter].ReversedQuota)

	var invitee User
	require.NoError(t, DB.Select("quota").First(&invitee, referralTestInviteeID).Error)
	assert.Equal(t, 50, invitee.Quota)

	require.NoError(t, DB.Select("aff_quota").First(&inviter, referralTestInviterID).Error)
	assert.Equal(t, 50, inviter.AffQuota)
}

func TestReferralRewardConcurrentCallbackIdempotent(t *testing.T) {
	resetReferralTestData(t)
	useReferralRewardSetting(t, 7)
	seedReferralRewardUsers(t)

	topUp := createReferralTopUp(t, "concurrent-idempotent", 30, common.GetTimestamp())

	const goroutineCount = 5
	errChan := make(chan error, goroutineCount)
	for i := 0; i < goroutineCount; i++ {
		go func() {
			settlement := TopUpSettlement{
				BaseAmount: 100,
				BaseQuota:  1000,
				TotalQuota: 1000,
			}
			errChan <- DB.Transaction(func(tx *gorm.DB) error {
				return applyReferralFirstTopUpRewardTx(tx, &topUp, settlement)
			})
		}()
	}
	for i := 0; i < goroutineCount; i++ {
		<-errChan
	}

	var rewards []ReferralReward
	require.NoError(t, DB.Order("reward_role asc").Find(&rewards).Error)
	require.Len(t, rewards, 2, "concurrent callbacks must not duplicate rewards")

	var invitee User
	require.NoError(t, DB.Select("quota").First(&invitee, referralTestInviteeID).Error)
	assert.Equal(t, 100, invitee.Quota)
}

func TestReferralRewardBudgetExhausted(t *testing.T) {
	resetReferralTestData(t)
	useReferralRewardSetting(t, 7)
	seedReferralRewardUsers(t)

	paymentSetting := operation_setting.GetPaymentSetting()
	paymentSetting.ReferralFirstTopUpReward.TotalBudgetQuota = 200

	topUp1 := createReferralTopUp(t, "budget-first", 30, common.GetTimestamp())
	applyReferralRewardForTest(t, &topUp1)

	var rewards1 []ReferralReward
	require.NoError(t, DB.Where("top_up_id = ?", topUp1.Id).Find(&rewards1).Error)
	require.Len(t, rewards1, 2)

	secondInviteeID := referralTestInviteeID + 10
	require.NoError(t, DB.Create(&User{
		Id:        secondInviteeID,
		Username:  "referral_invitee2",
		Status:    common.UserStatusEnabled,
		Group:     "default",
		AffCode:   "TIVT2",
		InviterId: referralTestInviterID,
	}).Error)
	topUp2 := TopUp{
		UserId:          secondInviteeID,
		Amount:          100,
		Money:           50,
		TradeNo:         "budget-exhausted",
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe,
		BaseQuota:       1000,
		CompleteTime:    common.GetTimestamp(),
		Status:          common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(&topUp2).Error)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		settlement := TopUpSettlement{BaseAmount: 100, BaseQuota: 1000, TotalQuota: 1000}
		return applyReferralFirstTopUpRewardTx(tx, &topUp2, settlement)
	}))

	var rewards2 []ReferralReward
	require.NoError(t, DB.Where("top_up_id = ?", topUp2.Id).Find(&rewards2).Error)
	require.Len(t, rewards2, 2)
	for _, reward := range rewards2 {
		assert.Equal(t, ReferralRewardStatusCancelled, reward.Status)
		assert.Equal(t, "budget_exhausted", reward.RiskReason)
	}
}

func TestReferralRewardInviterMonthlyLimit(t *testing.T) {
	resetReferralTestData(t)
	useReferralRewardSetting(t, 7)
	seedReferralRewardUsers(t)

	paymentSetting := operation_setting.GetPaymentSetting()
	paymentSetting.ReferralFirstTopUpReward.InviterMonthlyMaxQuota = 200

	topUp1 := createReferralTopUp(t, "monthly-first", 30, common.GetTimestamp())
	applyReferralRewardForTest(t, &topUp1)

	var inviterReward1 ReferralReward
	require.NoError(t, DB.Where("top_up_id = ? AND reward_role = ?", topUp1.Id, ReferralRewardRoleInviter).First(&inviterReward1).Error)
	assert.Equal(t, 100, inviterReward1.RewardQuota)

	secondInviteeID := referralTestInviteeID + 20
	require.NoError(t, DB.Create(&User{
		Id:        secondInviteeID,
		Username:  "referral_invitee_monthly",
		Status:    common.UserStatusEnabled,
		Group:     "default",
		AffCode:   "MNTH",
		InviterId: referralTestInviterID,
	}).Error)
	topUp2 := TopUp{
		UserId:          secondInviteeID,
		Amount:          200,
		Money:           50,
		TradeNo:         "monthly-second",
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe,
		BaseQuota:       2000,
		CompleteTime:    common.GetTimestamp(),
		Status:          common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(&topUp2).Error)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		settlement := TopUpSettlement{BaseAmount: 200, BaseQuota: 2000, TotalQuota: 2000}
		return applyReferralFirstTopUpRewardTx(tx, &topUp2, settlement)
	}))

	var inviterReward2 ReferralReward
	require.NoError(t, DB.Where("top_up_id = ? AND reward_role = ?", topUp2.Id, ReferralRewardRoleInviter).First(&inviterReward2).Error)
	assert.Equal(t, 100, inviterReward2.RewardQuota, "inviter reward capped at monthly remainder: 200 - 100 = 100")
}

func TestReferralRewardInviterSettledRefund(t *testing.T) {
	resetReferralTestData(t)
	useReferralRewardSetting(t, 0)
	seedReferralRewardUsers(t)

	topUp := createReferralTopUp(t, "inviter-settled-refund", 30, common.GetTimestamp())
	applyReferralRewardForTest(t, &topUp)

	settled, err := SettleDueReferralRewards(10)
	require.NoError(t, err)
	assert.Equal(t, 1, settled)

	var inviter User
	require.NoError(t, DB.Select("aff_quota", "quota").First(&inviter, referralTestInviterID).Error)
	assert.Equal(t, 100, inviter.AffQuota)
	originalQuota := inviter.Quota

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return reverseReferralRewardsForTopUpTx(tx, &topUp, 30)
	}))

	require.NoError(t, DB.Select("aff_quota", "quota").First(&inviter, referralTestInviterID).Error)
	assert.Equal(t, 0, inviter.AffQuota, "refund should drain AffQuota first")
	assert.Equal(t, originalQuota, inviter.Quota, "regular Quota must not be touched when AffQuota covers the reversal")
}

func TestReferralRewardDuplicateRefundCallback(t *testing.T) {
	resetReferralTestData(t)
	useReferralRewardSetting(t, 7)
	seedReferralRewardUsers(t)

	topUp := createReferralTopUp(t, "dup-refund", 30, common.GetTimestamp())
	applyReferralRewardForTest(t, &topUp)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return reverseReferralRewardsForTopUpTx(tx, &topUp, 30)
	}))

	var after1 []ReferralReward
	require.NoError(t, DB.Where("top_up_id = ?", topUp.Id).Order("reward_role asc").Find(&after1).Error)
	require.Len(t, after1, 2)
	inviteeReversed1 := after1[0].ReversedQuota
	inviterReversed1 := after1[1].ReversedQuota

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return reverseReferralRewardsForTopUpTx(tx, &topUp, 30)
	}))

	var after2 []ReferralReward
	require.NoError(t, DB.Where("top_up_id = ?", topUp.Id).Order("reward_role asc").Find(&after2).Error)
	require.Len(t, after2, 2)
	assert.Equal(t, inviteeReversed1, after2[0].ReversedQuota, "invitee reversed_quota must not grow on duplicate refund")
	assert.Equal(t, inviterReversed1, after2[1].ReversedQuota, "inviter reversed_quota must not grow on duplicate refund")
}

func TestReferralRewardCumulativeRefund(t *testing.T) {
	resetReferralTestData(t)
	useReferralRewardSetting(t, 0)
	seedReferralRewardUsers(t)

	topUp := createReferralTopUp(t, "cumulative-refund", 50, common.GetTimestamp())
	applyReferralRewardForTest(t, &topUp)

	settled, err := SettleDueReferralRewards(10)
	require.NoError(t, err)
	assert.Equal(t, 1, settled)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return reverseReferralRewardsForTopUpTx(tx, &topUp, 20)
	}))

	var after1 []ReferralReward
	require.NoError(t, DB.Where("top_up_id = ?", topUp.Id).Order("reward_role asc").Find(&after1).Error)
	require.Len(t, after1, 2)
	assert.Equal(t, 40, after1[0].ReversedQuota, "first refund: 20/50 * 100 = 40")
	assert.Equal(t, 40, after1[1].ReversedQuota)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return reverseReferralRewardsForTopUpTx(tx, &topUp, 10)
	}))

	var after2 []ReferralReward
	require.NoError(t, DB.Where("top_up_id = ?", topUp.Id).Order("reward_role asc").Find(&after2).Error)
	require.Len(t, after2, 2)
	assert.Equal(t, 60, after2[0].ReversedQuota, "second refund adds 10/50 * 100 = 20, total 60")
	assert.Equal(t, 60, after2[1].ReversedQuota)
}

func TestReferralRewardInsufficientBalance(t *testing.T) {
	resetReferralTestData(t)
	useReferralRewardSetting(t, 0)
	seedReferralRewardUsers(t)

	topUp := createReferralTopUp(t, "insufficient-balance", 30, common.GetTimestamp())
	applyReferralRewardForTest(t, &topUp)

	settled, err := SettleDueReferralRewards(10)
	require.NoError(t, err)
	assert.Equal(t, 1, settled)

	require.NoError(t, DB.Model(&User{}).Where("id = ?", referralTestInviteeID).Update("quota", 0).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", referralTestInviterID).Updates(map[string]interface{}{
		"quota":     0,
		"aff_quota": 0,
	}).Error)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return reverseReferralRewardsForTopUpTx(tx, &topUp, 30)
	}))

	var rewards []ReferralReward
	require.NoError(t, DB.Where("top_up_id = ?", topUp.Id).Find(&rewards).Error)
	require.Len(t, rewards, 2)
	for _, reward := range rewards {
		assert.Greater(t, reward.OwedQuota, 0, "owed_quota should be recorded for %s", reward.RewardRole)
		assert.Equal(t, ReferralRewardRiskBlocked, reward.RiskStatus, "risk_status should be blocked for %s", reward.RewardRole)
	}
}
