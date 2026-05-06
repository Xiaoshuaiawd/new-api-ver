package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func seedSubscriptionAlipayF2FUserAndPlan(t *testing.T, userID int, planID int) {
	t.Helper()

	require.NoError(t, model.DB.Create(&model.User{
		Id:       userID,
		Username: "sub_alipay_f2f_user",
		Status:   common.UserStatusEnabled,
		Quota:    0,
	}).Error)

	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:            planID,
		Title:         "Pro Monthly",
		PriceAmount:   19.90,
		Currency:      "CNY",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   500000,
	}).Error)
}

func TestSubscriptionRequestAlipayF2FPayReturnsQRCodeAndCreatesOrder(t *testing.T) {
	setupAlipayF2FControllerTestDB(t)
	seedSubscriptionAlipayF2FUserAndPlan(t, 11, 21)

	originalFactory := alipayF2FClientFactory
	originalEnabled := setting.AlipayF2FEnabled
	originalAppID := setting.AlipayF2FAppID
	originalPrivateKey := setting.AlipayF2FPrivateKey
	originalPublicKey := setting.AlipayF2FPublicKey
	originalMinTopUp := setting.AlipayF2FMinTopUp
	originalServerAddress := system_setting.ServerAddress
	t.Cleanup(func() {
		alipayF2FClientFactory = originalFactory
		setting.AlipayF2FEnabled = originalEnabled
		setting.AlipayF2FAppID = originalAppID
		setting.AlipayF2FPrivateKey = originalPrivateKey
		setting.AlipayF2FPublicKey = originalPublicKey
		setting.AlipayF2FMinTopUp = originalMinTopUp
		system_setting.ServerAddress = originalServerAddress
	})

	setting.AlipayF2FEnabled = true
	setting.AlipayF2FAppID = "2026000000000000"
	setting.AlipayF2FPrivateKey = "merchant-private-key"
	setting.AlipayF2FPublicKey = "alipay-public-key"
	setting.AlipayF2FMinTopUp = 1
	system_setting.ServerAddress = "https://new-api.example.com"

	alipayF2FClientFactory = func() (alipayF2FClient, error) {
		return &fakeAlipayF2FClient{
			precreateFn: func(ctx context.Context, req *service.AlipayF2FPrecreateRequest) (*service.AlipayF2FPrecreateResponse, error) {
				return &service.AlipayF2FPrecreateResponse{
					Code:       "10000",
					OutTradeNo: req.OutTradeNo,
					QRCode:     "https://qr.example.com/subscription/trade-123",
				}, nil
			},
			queryFn: func(ctx context.Context, outTradeNo string) (*service.AlipayF2FTradeQueryResponse, error) {
				return &service.AlipayF2FTradeQueryResponse{Code: "10000", OutTradeNo: outTradeNo, TradeStatus: "WAIT_BUYER_PAY"}, nil
			},
			verifyFn: func(params map[string]string) (*service.AlipayF2FNotification, error) {
				return nil, nil
			},
		}, nil
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/subscription/alipay-f2f/pay", map[string]any{
		"plan_id":        21,
		"payment_method": model.PaymentMethodAlipayF2F,
	}, 11)

	SubscriptionRequestAlipayF2FPay(ctx)

	response := decodeAlipayF2FControllerResponse(t, recorder)
	require.True(t, response.Success)
	require.Equal(t, "https://qr.example.com/subscription/trade-123", response.Data["qr_code"])

	var order model.SubscriptionOrder
	require.NoError(t, model.DB.First(&order).Error)
	require.Equal(t, 11, order.UserId)
	require.Equal(t, 21, order.PlanId)
	require.Equal(t, model.PaymentProviderAlipayF2F, order.PaymentProvider)
	require.Equal(t, model.PaymentMethodAlipayF2F, order.PaymentMethod)
	require.Equal(t, common.TopUpStatusPending, order.Status)
}

func TestGetSubscriptionAlipayF2FStatusCompletesSuccessfulTrade(t *testing.T) {
	setupAlipayF2FControllerTestDB(t)
	seedSubscriptionAlipayF2FUserAndPlan(t, 12, 22)

	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId:          12,
		PlanId:          22,
		Money:           19.90,
		TradeNo:         "sub-alipay-success",
		PaymentMethod:   model.PaymentMethodAlipayF2F,
		PaymentProvider: model.PaymentProviderAlipayF2F,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}).Error)

	originalFactory := alipayF2FClientFactory
	t.Cleanup(func() {
		alipayF2FClientFactory = originalFactory
	})
	alipayF2FClientFactory = func() (alipayF2FClient, error) {
		return &fakeAlipayF2FClient{
			precreateFn: func(ctx context.Context, req *service.AlipayF2FPrecreateRequest) (*service.AlipayF2FPrecreateResponse, error) {
				return nil, nil
			},
			queryFn: func(ctx context.Context, outTradeNo string) (*service.AlipayF2FTradeQueryResponse, error) {
				return &service.AlipayF2FTradeQueryResponse{
					Code:        "10000",
					OutTradeNo:  outTradeNo,
					TradeStatus: "TRADE_SUCCESS",
				}, nil
			},
			verifyFn: func(params map[string]string) (*service.AlipayF2FNotification, error) {
				return nil, nil
			},
		}, nil
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/subscription/alipay-f2f/order/sub-alipay-success/status", nil)
	ctx.Params = gin.Params{{Key: "tradeNo", Value: "sub-alipay-success"}}
	ctx.Set("id", 12)

	GetSubscriptionAlipayF2FOrderStatus(ctx)

	response := decodeAlipayF2FControllerResponse(t, recorder)
	require.True(t, response.Success)
	require.Equal(t, common.TopUpStatusSuccess, response.Data["status"])

	order := model.GetSubscriptionOrderByTradeNo("sub-alipay-success")
	require.NotNil(t, order)
	require.Equal(t, common.TopUpStatusSuccess, order.Status)

	var subCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", 12).Count(&subCount).Error)
	require.Equal(t, int64(1), subCount)

	topup := model.GetTopUpByTradeNo("sub-alipay-success")
	require.NotNil(t, topup)
	require.Equal(t, common.TopUpStatusSuccess, topup.Status)
}
