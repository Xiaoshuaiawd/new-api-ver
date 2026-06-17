package controller

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
)

func setupStripeRefundControllerTestDB(t *testing.T) {
	t.Helper()

	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))
}

func resetStripeRefundBonusSettingForTest(t *testing.T) {
	t.Helper()

	paymentSetting := operation_setting.GetPaymentSetting()
	original := paymentSetting.TopUpBonus
	t.Cleanup(func() {
		paymentSetting.TopUpBonus = original
	})
	paymentSetting.TopUpBonus = operation_setting.TopUpBonusSetting{}
}

func TestStripeChargeRefundedDeductsTopUpBonusByPaymentIntent(t *testing.T) {
	setupStripeRefundControllerTestDB(t)
	resetStripeRefundBonusSettingForTest(t)
	common.QuotaPerUnit = 500000

	require.NoError(t, model.DB.Create(&model.User{
		Id:       9201,
		Username: "stripe_refund_user",
		Status:   common.UserStatusEnabled,
		Quota:    55000000,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          9201,
		Amount:          100,
		Money:           100,
		TradeNo:         "stripe-refund-pi-order",
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		PaymentIntentID: "pi_refund_123",
		BaseQuota:       50000000,
		BonusAmount:     10,
		BonusQuota:      5000000,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      common.GetTimestamp() - 60,
		CompleteTime:    common.GetTimestamp() - 30,
	}).Error)

	event := stripe.Event{
		Type: stripe.EventTypeChargeRefunded,
		Data: &stripe.EventData{
			Object: map[string]interface{}{
				"payment_intent":  "pi_refund_123",
				"amount_refunded": 5000,
				"currency":        "usd",
			},
		},
	}

	handleStripeChargeRefunded(context.Background(), event, "127.0.0.1")

	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", 9201).First(&user).Error)
	require.Equal(t, 27500000, user.Quota)

	topUp := model.GetTopUpByTradeNo("stripe-refund-pi-order")
	require.NotNil(t, topUp)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, 50.0, topUp.RefundAmount)
	require.Equal(t, 27500000, topUp.RefundQuota)
}

func TestStripeChargeRefundedUsesCumulativeRefundAmountIdempotently(t *testing.T) {
	setupStripeRefundControllerTestDB(t)
	resetStripeRefundBonusSettingForTest(t)
	common.QuotaPerUnit = 500000

	require.NoError(t, model.DB.Create(&model.User{
		Id:       9203,
		Username: "stripe_refund_idempotent_user",
		Status:   common.UserStatusEnabled,
		Quota:    55000000,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          9203,
		Amount:          100,
		Money:           100,
		TradeNo:         "stripe-refund-idempotent",
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		PaymentIntentID: "pi_refund_repeat",
		BaseQuota:       50000000,
		BonusAmount:     10,
		BonusQuota:      5000000,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      common.GetTimestamp() - 60,
		CompleteTime:    common.GetTimestamp() - 30,
	}).Error)

	event := stripe.Event{
		Type: stripe.EventTypeChargeRefunded,
		Data: &stripe.EventData{
			Object: map[string]interface{}{
				"payment_intent":  "pi_refund_repeat",
				"amount_refunded": 5000,
				"currency":        "usd",
			},
		},
	}

	handleStripeChargeRefunded(context.Background(), event, "127.0.0.1")
	handleStripeChargeRefunded(context.Background(), event, "127.0.0.1")

	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", 9203).First(&user).Error)
	require.Equal(t, 27500000, user.Quota)

	topUp := model.GetTopUpByTradeNo("stripe-refund-idempotent")
	require.NotNil(t, topUp)
	require.Equal(t, 50.0, topUp.RefundAmount)
	require.Equal(t, 27500000, topUp.RefundQuota)
}

func TestStripeFulfillOrderPersistsPaymentIntentForRefundLookup(t *testing.T) {
	setupStripeRefundControllerTestDB(t)
	resetStripeRefundBonusSettingForTest(t)
	common.QuotaPerUnit = 500000

	require.NoError(t, model.DB.Create(&model.User{
		Id:       9202,
		Username: "stripe_pi_user",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          9202,
		Amount:          100,
		Money:           100,
		TradeNo:         "stripe-pi-order",
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}).Error)

	event := stripe.Event{
		Type: stripe.EventTypeCheckoutSessionCompleted,
		Data: &stripe.EventData{
			Object: map[string]interface{}{
				"amount_total":   10000,
				"currency":       "usd",
				"payment_intent": "pi_complete_123",
			},
		},
	}

	fulfillOrder(context.Background(), event, "stripe-pi-order", "cus_123", "127.0.0.1")

	topUp := model.GetTopUpByTradeNo("stripe-pi-order")
	require.NotNil(t, topUp)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, "pi_complete_123", topUp.PaymentIntentID)
}
