package controller

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
	"github.com/waffo-com/waffo-go/core"
)

func setupProviderRefundControllerTestDB(t *testing.T) {
	t.Helper()

	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))
}

func resetProviderRefundBonusSettingForTest(t *testing.T) {
	t.Helper()

	paymentSetting := operation_setting.GetPaymentSetting()
	original := paymentSetting.TopUpBonus
	t.Cleanup(func() {
		paymentSetting.TopUpBonus = original
	})
	paymentSetting.TopUpBonus = operation_setting.TopUpBonusSetting{}
}

func seedSuccessfulProviderTopUpForRefundTest(t *testing.T, userID int, tradeNo string, provider string) {
	t.Helper()

	require.NoError(t, model.DB.Create(&model.User{
		Id:       userID,
		Username: "provider_refund_user",
		Status:   common.UserStatusEnabled,
		Quota:    55000000,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          userID,
		Amount:          100,
		Money:           100,
		TradeNo:         tradeNo,
		PaymentMethod:   provider,
		PaymentProvider: provider,
		BaseQuota:       50000000,
		BonusAmount:     10,
		BonusQuota:      5000000,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      common.GetTimestamp() - 60,
		CompleteTime:    common.GetTimestamp() - 30,
	}).Error)
}

func TestWaffoRefundNotificationDeductsTopUpBonus(t *testing.T) {
	setupProviderRefundControllerTestDB(t)
	resetProviderRefundBonusSettingForTest(t)
	common.QuotaPerUnit = 500000
	seedSuccessfulProviderTopUpForRefundTest(t, 9301, "waffo-refund-order", model.PaymentProviderWaffo)

	err := processWaffoRefund(context.Background(), &core.RefundNotificationResult{
		OrigPaymentRequestID:  "waffo-refund-order",
		RefundAmount:          "50.00",
		RemainingRefundAmount: "50.00",
		RefundStatus:          core.RefundStatusPartiallyRefunded,
	}, "127.0.0.1")

	require.NoError(t, err)

	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", 9301).First(&user).Error)
	require.Equal(t, 27500000, user.Quota)

	topUp := model.GetTopUpByTradeNo("waffo-refund-order")
	require.NotNil(t, topUp)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, 50.0, topUp.RefundAmount)
	require.Equal(t, 27500000, topUp.RefundQuota)
}

func TestWaffoPancakeRefundSucceededDeductsTopUpBonus(t *testing.T) {
	setupProviderRefundControllerTestDB(t)
	resetProviderRefundBonusSettingForTest(t)
	common.QuotaPerUnit = 500000
	seedSuccessfulProviderTopUpForRefundTest(t, 9302, "pancake-refund-order", model.PaymentProviderWaffoPancake)

	err := processWaffoPancakeRefund(context.Background(), &service.WaffoPancakeWebhookEvent{
		EventType: "refund.succeeded",
		Data: service.WaffoPancakeWebhookData{
			OrderMerchantExternalID:        "pancake-refund-order",
			MerchantProvidedBuyerIdentity:  service.WaffoPancakeBuyerIdentityFromUserID(9302),
			RefundTicketMerchantExternalID: "refund-ticket-9302",
			RefundStatus:                   "succeeded",
			Amount:                         "50.00",
		},
	}, "127.0.0.1")

	require.NoError(t, err)

	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", 9302).First(&user).Error)
	require.Equal(t, 27500000, user.Quota)

	topUp := model.GetTopUpByTradeNo("pancake-refund-order")
	require.NotNil(t, topUp)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, 50.0, topUp.RefundAmount)
	require.Equal(t, 27500000, topUp.RefundQuota)
}
