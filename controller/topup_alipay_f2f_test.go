package controller

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type alipayF2FControllerAPIResponse struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data"`
}

type fakeAlipayF2FClient struct {
	precreateFn func(context.Context, *service.AlipayF2FPrecreateRequest) (*service.AlipayF2FPrecreateResponse, error)
	queryFn     func(context.Context, string) (*service.AlipayF2FTradeQueryResponse, error)
	verifyFn    func(map[string]string) (*service.AlipayF2FNotification, error)
}

func (f *fakeAlipayF2FClient) Precreate(ctx context.Context, req *service.AlipayF2FPrecreateRequest) (*service.AlipayF2FPrecreateResponse, error) {
	return f.precreateFn(ctx, req)
}

func (f *fakeAlipayF2FClient) Query(ctx context.Context, outTradeNo string) (*service.AlipayF2FTradeQueryResponse, error) {
	return f.queryFn(ctx, outTradeNo)
}

func (f *fakeAlipayF2FClient) Close(ctx context.Context, outTradeNo string) (*service.AlipayF2FTradeCloseResponse, error) {
	return &service.AlipayF2FTradeCloseResponse{
		Code:       "10000",
		OutTradeNo: outTradeNo,
	}, nil
}

func (f *fakeAlipayF2FClient) VerifyNotification(params map[string]string) (*service.AlipayF2FNotification, error) {
	return f.verifyFn(params)
}

func setupAlipayF2FControllerTestDB(t *testing.T) {
	t.Helper()

	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.TopUp{},
		&model.Log{},
		&model.SubscriptionPlan{},
		&model.SubscriptionOrder{},
		&model.UserSubscription{},
	))
}

func seedAlipayF2FControllerUser(t *testing.T, userID int) {
	t.Helper()

	require.NoError(t, model.DB.Create(&model.User{
		Id:       userID,
		Username: "alipay_f2f_user",
		Status:   common.UserStatusEnabled,
		Quota:    0,
	}).Error)
}

func decodeAlipayF2FControllerResponse(t *testing.T, recorder *httptest.ResponseRecorder) alipayF2FControllerAPIResponse {
	t.Helper()

	var response alipayF2FControllerAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestRequestAlipayF2FPayReturnsQRCodeAndCreatesTopUp(t *testing.T) {
	setupAlipayF2FControllerTestDB(t)
	seedAlipayF2FControllerUser(t, 1)

	originalFactory := alipayF2FClientFactory
	originalEnabled := setting.AlipayF2FEnabled
	originalAppID := setting.AlipayF2FAppID
	originalSellerID := setting.AlipayF2FSellerID
	originalPrivateKey := setting.AlipayF2FPrivateKey
	originalPublicKey := setting.AlipayF2FPublicKey
	originalMinTopUp := setting.AlipayF2FMinTopUp
	originalServerAddress := system_setting.ServerAddress
	t.Cleanup(func() {
		alipayF2FClientFactory = originalFactory
		setting.AlipayF2FEnabled = originalEnabled
		setting.AlipayF2FAppID = originalAppID
		setting.AlipayF2FSellerID = originalSellerID
		setting.AlipayF2FPrivateKey = originalPrivateKey
		setting.AlipayF2FPublicKey = originalPublicKey
		setting.AlipayF2FMinTopUp = originalMinTopUp
		system_setting.ServerAddress = originalServerAddress
	})

	setting.AlipayF2FEnabled = true
	setting.AlipayF2FAppID = "2026000000000000"
	setting.AlipayF2FSellerID = "2088000000000000"
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
					QRCode:     "https://qr.example.com/pay/trade-123",
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

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/user/alipay-f2f/pay", map[string]any{
		"amount":         10,
		"payment_method": model.PaymentMethodAlipayF2F,
	}, 1)

	RequestAlipayF2FPay(ctx)

	response := decodeAlipayF2FControllerResponse(t, recorder)
	require.True(t, response.Success)
	require.Equal(t, "https://qr.example.com/pay/trade-123", response.Data["qr_code"])

	var topUp model.TopUp
	require.NoError(t, model.DB.First(&topUp).Error)
	require.Equal(t, model.PaymentProviderAlipayF2F, topUp.PaymentProvider)
	require.Equal(t, common.TopUpStatusPending, topUp.Status)
}

func TestGetAlipayF2FTopUpStatusCompletesSuccessfulTrade(t *testing.T) {
	setupAlipayF2FControllerTestDB(t)
	seedAlipayF2FControllerUser(t, 2)

	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          2,
		Amount:          5,
		Money:           35,
		TradeNo:         "trade-success",
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
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/alipay-f2f/topup/trade-success/status", nil)
	ctx.Params = gin.Params{{Key: "tradeNo", Value: "trade-success"}}
	ctx.Set("id", 2)

	GetAlipayF2FTopUpStatus(ctx)

	response := decodeAlipayF2FControllerResponse(t, recorder)
	require.True(t, response.Success)
	require.Equal(t, common.TopUpStatusSuccess, response.Data["status"])

	topUp := model.GetTopUpByTradeNo("trade-success")
	require.NotNil(t, topUp)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)

	user, err := model.GetUserById(2, false)
	require.NoError(t, err)
	require.Equal(t, int(5*common.QuotaPerUnit), user.Quota)
}

func TestAlipayF2FNotifyRejectsInvalidSignature(t *testing.T) {
	setupAlipayF2FControllerTestDB(t)

	originalFactory := alipayF2FClientFactory
	originalEnabled := setting.AlipayF2FEnabled
	originalAppID := setting.AlipayF2FAppID
	originalSellerID := setting.AlipayF2FSellerID
	originalPrivateKey := setting.AlipayF2FPrivateKey
	originalPublicKey := setting.AlipayF2FPublicKey
	t.Cleanup(func() {
		alipayF2FClientFactory = originalFactory
		setting.AlipayF2FEnabled = originalEnabled
		setting.AlipayF2FAppID = originalAppID
		setting.AlipayF2FSellerID = originalSellerID
		setting.AlipayF2FPrivateKey = originalPrivateKey
		setting.AlipayF2FPublicKey = originalPublicKey
	})
	setting.AlipayF2FEnabled = true
	setting.AlipayF2FAppID = "2026000000000000"
	setting.AlipayF2FSellerID = "2088000000000000"
	setting.AlipayF2FPrivateKey = "merchant-private-key"
	setting.AlipayF2FPublicKey = "alipay-public-key"
	alipayF2FClientFactory = func() (alipayF2FClient, error) {
		return &fakeAlipayF2FClient{
			precreateFn: func(ctx context.Context, req *service.AlipayF2FPrecreateRequest) (*service.AlipayF2FPrecreateResponse, error) {
				return nil, nil
			},
			queryFn: func(ctx context.Context, outTradeNo string) (*service.AlipayF2FTradeQueryResponse, error) {
				return nil, nil
			},
			verifyFn: func(params map[string]string) (*service.AlipayF2FNotification, error) {
				return nil, context.DeadlineExceeded
			},
		}, nil
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/alipay-f2f/notify", bytes.NewBufferString("out_trade_no=trade-1"))
	ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	AlipayF2FNotify(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "fail", recorder.Body.String())
}

func TestRequestAlipayF2FAmountHonorsProviderMinimum(t *testing.T) {
	setupAlipayF2FControllerTestDB(t)
	seedAlipayF2FControllerUser(t, 3)

	originalMinTopUp := setting.AlipayF2FMinTopUp
	originalPrice := operation_setting.Price
	t.Cleanup(func() {
		setting.AlipayF2FMinTopUp = originalMinTopUp
		operation_setting.Price = originalPrice
	})

	setting.AlipayF2FMinTopUp = 3
	operation_setting.Price = 7

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/user/alipay-f2f/amount", map[string]any{
		"amount": 2,
	}, 3)
	RequestAlipayF2FAmount(ctx)
	response := decodeAlipayF2FControllerResponse(t, recorder)
	require.False(t, response.Success)
}

func TestGetTopUpInfoUsesConfiguredAlipayF2FDisplayName(t *testing.T) {
	originalEnabled := setting.AlipayF2FEnabled
	originalAppID := setting.AlipayF2FAppID
	originalPrivateKey := setting.AlipayF2FPrivateKey
	originalPublicKey := setting.AlipayF2FPublicKey
	originalMinTopUp := setting.AlipayF2FMinTopUp
	originalDisplayName := setting.AlipayF2FDisplayName
	originalPayMethods := operation_setting.PayMethods
	t.Cleanup(func() {
		setting.AlipayF2FEnabled = originalEnabled
		setting.AlipayF2FAppID = originalAppID
		setting.AlipayF2FPrivateKey = originalPrivateKey
		setting.AlipayF2FPublicKey = originalPublicKey
		setting.AlipayF2FMinTopUp = originalMinTopUp
		setting.AlipayF2FDisplayName = originalDisplayName
		operation_setting.PayMethods = originalPayMethods
	})

	setting.AlipayF2FEnabled = true
	setting.AlipayF2FAppID = "2026000000000000"
	setting.AlipayF2FPrivateKey = "merchant-private-key"
	setting.AlipayF2FPublicKey = "alipay-public-key"
	setting.AlipayF2FMinTopUp = 1
	setting.AlipayF2FDisplayName = "扫码支付宝"
	operation_setting.PayMethods = []map[string]string{}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/topup/info", nil)

	GetTopUpInfo(ctx)

	response := decodeAlipayF2FControllerResponse(t, recorder)
	require.True(t, response.Success)

	rawMethods, ok := response.Data["pay_methods"].([]interface{})
	require.True(t, ok)
	require.Len(t, rawMethods, 1)

	method, ok := rawMethods[0].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, model.PaymentMethodAlipayF2F, method["type"])
	require.Equal(t, "扫码支付宝", method["name"])
}
