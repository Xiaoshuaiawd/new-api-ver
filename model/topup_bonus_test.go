package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetTopUpBonusSettingForTest(t *testing.T) {
	t.Helper()
	original := operation_setting.GetPaymentSetting().TopUpBonus
	t.Cleanup(func() {
		operation_setting.GetPaymentSetting().TopUpBonus = original
	})
	operation_setting.GetPaymentSetting().TopUpBonus = operation_setting.TopUpBonusSetting{}
}

func TestCalculateTopUpSettlementAppliesPercentBonusAboveThreshold(t *testing.T) {
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().TopUpBonus = operation_setting.TopUpBonusSetting{
		ActivityID:             "summer-2026",
		Enabled:                true,
		ActivityName:           "summer",
		MinAmount:              100,
		BonusPercent:           10,
		SingleBonusMaxAmount:   0,
		UserBonusMaxAmount:     0,
		TotalBonusBudgetAmount: 0,
	}

	settlement := CalculateTopUpSettlement(100, 0, 0)

	assert.Equal(t, 50000000, settlement.BaseQuota)
	assert.Equal(t, 5000000, settlement.BonusQuota)
	assert.Equal(t, 55000000, settlement.TotalQuota)
	assert.Equal(t, int64(10), settlement.BonusAmount)
	assert.Equal(t, "summer-2026", settlement.ActivityID)
	assert.Equal(t, "summer", settlement.ActivityName)
}

func TestCalculateTopUpSettlementDoesNotApplyBonusBelowThreshold(t *testing.T) {
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().TopUpBonus = operation_setting.TopUpBonusSetting{
		Enabled:      true,
		MinAmount:    100,
		BonusPercent: 10,
	}

	settlement := CalculateTopUpSettlement(99, 0, 0)

	assert.Equal(t, 49500000, settlement.BaseQuota)
	assert.Equal(t, 0, settlement.BonusQuota)
	assert.Equal(t, 49500000, settlement.TotalQuota)
	assert.Equal(t, int64(0), settlement.BonusAmount)
}

func TestCalculateTopUpSettlementCapsSingleBonusAmount(t *testing.T) {
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().TopUpBonus = operation_setting.TopUpBonusSetting{
		Enabled:              true,
		MinAmount:            100,
		BonusPercent:         20,
		SingleBonusMaxAmount: 15,
	}

	settlement := CalculateTopUpSettlement(100, 0, 0)

	assert.Equal(t, int64(15), settlement.BonusAmount)
	assert.Equal(t, 7500000, settlement.BonusQuota)
	assert.Equal(t, 57500000, settlement.TotalQuota)
}

func TestCalculateTopUpSettlementCapsUserAndTotalBonusBudget(t *testing.T) {
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().TopUpBonus = operation_setting.TopUpBonusSetting{
		Enabled:                true,
		MinAmount:              100,
		BonusPercent:           20,
		UserBonusMaxAmount:     25,
		TotalBonusBudgetAmount: 18,
	}

	settlement := CalculateTopUpSettlement(100, 10, 5)

	assert.Equal(t, int64(13), settlement.BonusAmount)
	assert.Equal(t, 6500000, settlement.BonusQuota)
	assert.Equal(t, 56500000, settlement.TotalQuota)
}

func TestCalculateTopUpSettlementBudgetsUseNetBonusAfterPartialRefund(t *testing.T) {
	truncateTables(t)
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().TopUpBonus = operation_setting.TopUpBonusSetting{
		Enabled:                true,
		MinAmount:              100,
		BonusPercent:           20,
		UserBonusMaxAmount:     25,
		TotalBonusBudgetAmount: 25,
	}

	insertUserForPaymentGuardTest(t, 911, 60000000)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          911,
		Amount:          100,
		Money:           100,
		TradeNo:         "budget-refunded-old",
		PaymentMethod:   PaymentMethodWaffo,
		PaymentProvider: PaymentProviderWaffo,
		BaseQuota:       50000000,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      common.GetTimestamp() - 120,
		CompleteTime:    common.GetTimestamp() - 100,
		BonusAmount:     20,
		BonusQuota:      10000000,
	}).Error)
	require.NoError(t, RefundTopUp("budget-refunded-old", PaymentProviderWaffo, 50, "127.0.0.1"))

	require.NoError(t, DB.Create(&TopUp{
		UserId:          911,
		Amount:          100,
		Money:           100,
		TradeNo:         "budget-refunded-next",
		PaymentMethod:   PaymentMethodWaffo,
		PaymentProvider: PaymentProviderWaffo,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}).Error)

	require.NoError(t, RechargeWaffo("budget-refunded-next", "127.0.0.1"))

	next := GetTopUpByTradeNo("budget-refunded-next")
	require.NotNil(t, next)
	assert.Equal(t, int64(15), next.BonusAmount)
	assert.Equal(t, 7500000, next.BonusQuota)
}

func TestCalculateTopUpSettlementScalesBonusQuotaFromPaidAmountAndBaseQuota(t *testing.T) {
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().TopUpBonus = operation_setting.TopUpBonusSetting{
		Enabled:      true,
		MinAmount:    100,
		BonusPercent: 10,
	}

	settlement := CalculateTopUpSettlementWithPaidAmount(100, 100, 1000, 0, 0)

	assert.Equal(t, int64(10), settlement.BonusAmount)
	assert.Equal(t, 100, settlement.BonusQuota)
	assert.Equal(t, 1100, settlement.TotalQuota)
}

func TestRechargeWaffoCreditsBonusAndPersistsBonusFields(t *testing.T) {
	truncateTables(t)
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().TopUpBonus = operation_setting.TopUpBonusSetting{
		Enabled:      true,
		ActivityID:   "bonus-2026",
		ActivityName: "recharge bonus",
		MinAmount:    100,
		BonusPercent: 10,
	}

	insertUserForPaymentGuardTest(t, 901, 0)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          901,
		Amount:          100,
		Money:           100,
		TradeNo:         "waffo-bonus",
		PaymentMethod:   PaymentMethodWaffo,
		PaymentProvider: PaymentProviderWaffo,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}).Error)

	err := RechargeWaffo("waffo-bonus", "127.0.0.1")

	require.NoError(t, err)
	assert.Equal(t, 55000000, getUserQuotaForPaymentGuardTest(t, 901))

	topUp := GetTopUpByTradeNo("waffo-bonus")
	require.NotNil(t, topUp)
	assert.Equal(t, int64(10), topUp.BonusAmount)
	assert.Equal(t, 5000000, topUp.BonusQuota)
	assert.Equal(t, "bonus-2026", topUp.BonusActivityID)
	assert.Equal(t, "recharge bonus", topUp.BonusActivityName)
}

func TestRechargeWaffoUsesActualPaidMoneyForBonusThreshold(t *testing.T) {
	truncateTables(t)
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().TopUpBonus = operation_setting.TopUpBonusSetting{
		Enabled:      true,
		MinAmount:    100,
		BonusPercent: 10,
	}

	insertUserForPaymentGuardTest(t, 904, 0)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          904,
		Amount:          100,
		Money:           80,
		TradeNo:         "waffo-actual-money-threshold",
		PaymentMethod:   PaymentMethodWaffo,
		PaymentProvider: PaymentProviderWaffo,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}).Error)

	err := RechargeWaffo("waffo-actual-money-threshold", "127.0.0.1")

	require.NoError(t, err)
	assert.Equal(t, 50000000, getUserQuotaForPaymentGuardTest(t, 904))

	topUp := GetTopUpByTradeNo("waffo-actual-money-threshold")
	require.NotNil(t, topUp)
	assert.Equal(t, int64(0), topUp.BonusAmount)
	assert.Equal(t, 0, topUp.BonusQuota)
}

func TestRechargeStripeUsesWebhookPaidMoneyForBonusThreshold(t *testing.T) {
	truncateTables(t)
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().TopUpBonus = operation_setting.TopUpBonusSetting{
		Enabled:      true,
		MinAmount:    100,
		BonusPercent: 10,
	}

	insertUserForPaymentGuardTest(t, 908, 0)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          908,
		Amount:          100,
		Money:           80,
		TradeNo:         "stripe-webhook-paid-money",
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}).Error)

	err := RechargeStripe("stripe-webhook-paid-money", "cus_123", 120, "127.0.0.1")

	require.NoError(t, err)
	assert.Equal(t, 66000000, getUserQuotaForPaymentGuardTest(t, 908))

	topUp := GetTopUpByTradeNo("stripe-webhook-paid-money")
	require.NotNil(t, topUp)
	assert.Equal(t, 120.0, topUp.Money)
	assert.Equal(t, 60000000, topUp.BaseQuota)
	assert.Equal(t, int64(12), topUp.BonusAmount)
	assert.Equal(t, 6000000, topUp.BonusQuota)
}

func TestRechargeWaffoSkipsBonusWhenFirstTopUpOnlyAndUserAlreadyPaid(t *testing.T) {
	truncateTables(t)
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().TopUpBonus = operation_setting.TopUpBonusSetting{
		Enabled:        true,
		MinAmount:      100,
		BonusPercent:   10,
		FirstTopUpOnly: true,
	}

	insertUserForPaymentGuardTest(t, 902, 0)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          902,
		Amount:          100,
		Money:           100,
		TradeNo:         "waffo-old",
		PaymentMethod:   PaymentMethodWaffo,
		PaymentProvider: PaymentProviderWaffo,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      common.GetTimestamp() - 60,
		CompleteTime:    common.GetTimestamp() - 30,
	}).Error)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          902,
		Amount:          100,
		Money:           100,
		TradeNo:         "waffo-first-only",
		PaymentMethod:   PaymentMethodWaffo,
		PaymentProvider: PaymentProviderWaffo,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}).Error)

	err := RechargeWaffo("waffo-first-only", "127.0.0.1")

	require.NoError(t, err)
	assert.Equal(t, 50000000, getUserQuotaForPaymentGuardTest(t, 902))

	topUp := GetTopUpByTradeNo("waffo-first-only")
	require.NotNil(t, topUp)
	assert.Equal(t, int64(0), topUp.BonusAmount)
	assert.Equal(t, 0, topUp.BonusQuota)
}

func TestManualCompleteTopUpAlreadySuccessfulIsIdempotent(t *testing.T) {
	truncateTables(t)
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000

	insertUserForPaymentGuardTest(t, 903, 50000000)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          903,
		Amount:          100,
		Money:           100,
		TradeNo:         "manual-complete-done",
		PaymentMethod:   PaymentMethodWaffo,
		PaymentProvider: PaymentProviderWaffo,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      common.GetTimestamp() - 60,
		CompleteTime:    common.GetTimestamp() - 30,
		BonusAmount:     10,
		BonusQuota:      5000000,
	}).Error)

	require.NoError(t, ManualCompleteTopUp("manual-complete-done", "127.0.0.1"))

	assert.Equal(t, 50000000, getUserQuotaForPaymentGuardTest(t, 903))

	var logCount int64
	require.NoError(t, LOG_DB.Model(&Log{}).
		Where("user_id = ? AND content LIKE ?", 903, "%管理员补单成功%").
		Count(&logCount).Error)
	assert.Equal(t, int64(0), logCount)
}

func TestRefundTopUpDeductsBonusProportionally(t *testing.T) {
	truncateTables(t)
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000

	insertUserForPaymentGuardTest(t, 905, 55000000)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          905,
		Amount:          100,
		Money:           100,
		TradeNo:         "refund-bonus-half",
		PaymentMethod:   PaymentMethodWaffo,
		PaymentProvider: PaymentProviderWaffo,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      common.GetTimestamp() - 60,
		CompleteTime:    common.GetTimestamp() - 30,
		BonusAmount:     10,
		BonusQuota:      5000000,
	}).Error)

	require.NoError(t, RefundTopUp("refund-bonus-half", PaymentProviderWaffo, 50, "127.0.0.1"))

	assert.Equal(t, 27500000, getUserQuotaForPaymentGuardTest(t, 905))
	topUp := GetTopUpByTradeNo("refund-bonus-half")
	require.NotNil(t, topUp)
	assert.Equal(t, common.TopUpStatusSuccess, topUp.Status)
}

func TestRefundTopUpAccumulatesPartialRefunds(t *testing.T) {
	truncateTables(t)
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000

	insertUserForPaymentGuardTest(t, 909, 55000000)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          909,
		Amount:          100,
		Money:           100,
		TradeNo:         "refund-bonus-repeat",
		PaymentMethod:   PaymentMethodWaffo,
		PaymentProvider: PaymentProviderWaffo,
		BaseQuota:       50000000,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      common.GetTimestamp() - 60,
		CompleteTime:    common.GetTimestamp() - 30,
		BonusAmount:     10,
		BonusQuota:      5000000,
	}).Error)

	require.NoError(t, RefundTopUp("refund-bonus-repeat", PaymentProviderWaffo, 50, "127.0.0.1"))
	require.NoError(t, RefundTopUp("refund-bonus-repeat", PaymentProviderWaffo, 50, "127.0.0.1"))

	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, 909))
	topUp := GetTopUpByTradeNo("refund-bonus-repeat")
	require.NotNil(t, topUp)
	assert.Equal(t, 100.0, topUp.RefundAmount)
	assert.Equal(t, 55000000, topUp.RefundQuota)
	assert.Equal(t, common.TopUpStatusRefunded, topUp.Status)
}

func TestRefundTopUpByPaymentIntentCumulativeIsIdempotent(t *testing.T) {
	truncateTables(t)
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000

	insertUserForPaymentGuardTest(t, 910, 55000000)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          910,
		Amount:          100,
		Money:           100,
		TradeNo:         "refund-cumulative",
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe,
		PaymentIntentID: "pi_cumulative",
		BaseQuota:       50000000,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      common.GetTimestamp() - 60,
		CompleteTime:    common.GetTimestamp() - 30,
		BonusAmount:     10,
		BonusQuota:      5000000,
	}).Error)

	require.NoError(t, RefundTopUpByPaymentIntentCumulative("pi_cumulative", PaymentProviderStripe, 50, "127.0.0.1"))
	require.NoError(t, RefundTopUpByPaymentIntentCumulative("pi_cumulative", PaymentProviderStripe, 50, "127.0.0.1"))
	require.NoError(t, RefundTopUpByPaymentIntentCumulative("pi_cumulative", PaymentProviderStripe, 100, "127.0.0.1"))

	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, 910))
	topUp := GetTopUpByTradeNo("refund-cumulative")
	require.NotNil(t, topUp)
	assert.Equal(t, 100.0, topUp.RefundAmount)
	assert.Equal(t, 55000000, topUp.RefundQuota)
	assert.Equal(t, common.TopUpStatusRefunded, topUp.Status)
}

func TestRefundTopUpWithReferenceIsIdempotent(t *testing.T) {
	truncateTables(t)
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000

	insertUserForPaymentGuardTest(t, 912, 55000000)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          912,
		Amount:          100,
		Money:           100,
		TradeNo:         "refund-reference-repeat",
		PaymentMethod:   PaymentMethodWaffo,
		PaymentProvider: PaymentProviderWaffo,
		BaseQuota:       50000000,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      common.GetTimestamp() - 60,
		CompleteTime:    common.GetTimestamp() - 30,
		BonusAmount:     10,
		BonusQuota:      5000000,
	}).Error)

	require.NoError(t, RefundTopUpWithReference("refund-reference-repeat", PaymentProviderWaffo, 50, "refund-event-1", "127.0.0.1"))
	require.NoError(t, RefundTopUpWithReference("refund-reference-repeat", PaymentProviderWaffo, 50, "refund-event-1", "127.0.0.1"))

	assert.Equal(t, 27500000, getUserQuotaForPaymentGuardTest(t, 912))
	topUp := GetTopUpByTradeNo("refund-reference-repeat")
	require.NotNil(t, topUp)
	assert.Equal(t, 50.0, topUp.RefundAmount)
	assert.Equal(t, 27500000, topUp.RefundQuota)
}

func TestRefundTopUpByRemainingRefundAmountUsesCumulativeAmount(t *testing.T) {
	truncateTables(t)
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000

	insertUserForPaymentGuardTest(t, 913, 55000000)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          913,
		Amount:          100,
		Money:           100,
		TradeNo:         "refund-remaining",
		PaymentMethod:   PaymentMethodWaffo,
		PaymentProvider: PaymentProviderWaffo,
		BaseQuota:       50000000,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      common.GetTimestamp() - 60,
		CompleteTime:    common.GetTimestamp() - 30,
		BonusAmount:     10,
		BonusQuota:      5000000,
	}).Error)

	require.NoError(t, RefundTopUpByRemainingRefundAmountWithReference("refund-remaining", PaymentProviderWaffo, 50, "refund-event-1", "127.0.0.1"))
	require.NoError(t, RefundTopUpByRemainingRefundAmountWithReference("refund-remaining", PaymentProviderWaffo, 50, "refund-event-1", "127.0.0.1"))
	require.NoError(t, RefundTopUpByRemainingRefundAmountWithReference("refund-remaining", PaymentProviderWaffo, 0, "refund-event-2", "127.0.0.1"))

	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, 913))
	topUp := GetTopUpByTradeNo("refund-remaining")
	require.NotNil(t, topUp)
	assert.Equal(t, 100.0, topUp.RefundAmount)
	assert.Equal(t, 55000000, topUp.RefundQuota)
	assert.Equal(t, common.TopUpStatusRefunded, topUp.Status)
}

func TestRefundTopUpFullRefundMarksOrderRefunded(t *testing.T) {
	truncateTables(t)
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000

	insertUserForPaymentGuardTest(t, 906, 55000000)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          906,
		Amount:          100,
		Money:           100,
		TradeNo:         "refund-bonus-full",
		PaymentMethod:   PaymentMethodWaffo,
		PaymentProvider: PaymentProviderWaffo,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      common.GetTimestamp() - 60,
		CompleteTime:    common.GetTimestamp() - 30,
		BonusAmount:     10,
		BonusQuota:      5000000,
	}).Error)

	require.NoError(t, RefundTopUp("refund-bonus-full", PaymentProviderWaffo, 100, "127.0.0.1"))

	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, 906))
	topUp := GetTopUpByTradeNo("refund-bonus-full")
	require.NotNil(t, topUp)
	assert.Equal(t, common.TopUpStatusRefunded, topUp.Status)
}

func TestRefundTopUpFailsWhenQuotaInsufficient(t *testing.T) {
	truncateTables(t)
	resetTopUpBonusSettingForTest(t)
	common.QuotaPerUnit = 500000

	insertUserForPaymentGuardTest(t, 907, 1000)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          907,
		Amount:          100,
		Money:           100,
		TradeNo:         "refund-insufficient",
		PaymentMethod:   PaymentMethodWaffo,
		PaymentProvider: PaymentProviderWaffo,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      common.GetTimestamp() - 60,
		CompleteTime:    common.GetTimestamp() - 30,
		BonusAmount:     10,
		BonusQuota:      5000000,
	}).Error)

	err := RefundTopUp("refund-insufficient", PaymentProviderWaffo, 50, "127.0.0.1")

	require.Error(t, err)
	assert.Equal(t, 1000, getUserQuotaForPaymentGuardTest(t, 907))
}
