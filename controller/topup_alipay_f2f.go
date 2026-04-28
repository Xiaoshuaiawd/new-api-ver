package controller

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type alipayF2FClient interface {
	Precreate(context.Context, *service.AlipayF2FPrecreateRequest) (*service.AlipayF2FPrecreateResponse, error)
	Query(context.Context, string) (*service.AlipayF2FTradeQueryResponse, error)
	Close(context.Context, string) (*service.AlipayF2FTradeCloseResponse, error)
	VerifyNotification(map[string]string) (*service.AlipayF2FNotification, error)
}

var alipayF2FClientFactory = func() (alipayF2FClient, error) {
	return service.NewConfiguredAlipayF2FClient()
}

type AlipayF2FPayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

func getAlipayF2FMinTopup() int64 {
	minTopup := setting.AlipayF2FMinTopUp
	if minTopup <= 0 {
		minTopup = 1
	}

	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dMinTopup := decimal.NewFromInt(int64(minTopup))
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		minTopup = int(dMinTopup.Mul(dQuotaPerUnit).IntPart())
	}

	return int64(minTopup)
}

func getAlipayF2FTimeoutExpress() string {
	timeoutMinutes := setting.AlipayF2FOrderTimeout
	if timeoutMinutes <= 0 {
		timeoutMinutes = 30
	}
	return fmt.Sprintf("%dm", timeoutMinutes)
}

func getAlipayF2FTimeoutMinutes() int {
	timeoutMinutes := setting.AlipayF2FOrderTimeout
	if timeoutMinutes <= 0 {
		return 30
	}
	return timeoutMinutes
}

func buildAlipayF2FSubject(amount int64) string {
	subjectPrefix := strings.TrimSpace(setting.AlipayF2FSubjectPrefix)
	if subjectPrefix == "" {
		subjectPrefix = "new-api"
	}
	return fmt.Sprintf("%s Recharge %d", subjectPrefix, amount)
}

func getAlipayF2FUserGroup(userID int) (string, error) {
	user, err := model.GetUserById(userID, false)
	if err != nil {
		return "", err
	}
	group := strings.TrimSpace(user.Group)
	if group == "" {
		group = "default"
	}
	return group, nil
}

func RequestAlipayF2FAmount(c *gin.Context) {
	var req AmountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	if req.Amount < getAlipayF2FMinTopup() {
		common.ApiErrorMsg(c, fmt.Sprintf("充值数量不能小于 %d", getAlipayF2FMinTopup()))
		return
	}

	id := c.GetInt("id")
	group, err := getAlipayF2FUserGroup(id)
	if err != nil {
		common.ApiErrorMsg(c, "获取用户分组失败")
		return
	}

	payMoney := getPayMoney(req.Amount, group)
	if payMoney <= 0.01 {
		common.ApiErrorMsg(c, "充值金额过低")
		return
	}

	common.ApiSuccess(c, strconv.FormatFloat(payMoney, 'f', 2, 64))
}

func RequestAlipayF2FPay(c *gin.Context) {
	if !isAlipayF2FTopUpEnabled() {
		common.ApiErrorMsg(c, "管理员未开启支付宝当面付充值")
		return
	}

	var req AlipayF2FPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	if req.PaymentMethod != model.PaymentMethodAlipayF2F {
		common.ApiErrorMsg(c, "不支持的支付方式")
		return
	}
	if req.Amount < getAlipayF2FMinTopup() {
		common.ApiErrorMsg(c, fmt.Sprintf("充值数量不能小于 %d", getAlipayF2FMinTopup()))
		return
	}

	id := c.GetInt("id")
	group, err := getAlipayF2FUserGroup(id)
	if err != nil {
		common.ApiErrorMsg(c, "获取用户分组失败")
		return
	}

	payMoney := getPayMoney(req.Amount, group)
	if payMoney <= 0.01 {
		common.ApiErrorMsg(c, "充值金额过低")
		return
	}

	client, err := alipayF2FClientFactory()
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝当面付 client 初始化失败 user_id=%d error=%q", id, err.Error()))
		common.ApiErrorMsg(c, "支付配置错误")
		return
	}

	tradeNo := fmt.Sprintf("ALP%d%d%s", id, time.Now().UnixMilli(), common.GetRandomString(6))
	notifyURL := strings.TrimRight(service.GetCallbackAddress(), "/") + "/api/alipay-f2f/notify"
	precreateResp, err := client.Precreate(c.Request.Context(), &service.AlipayF2FPrecreateRequest{
		OutTradeNo:     tradeNo,
		TotalAmount:    strconv.FormatFloat(payMoney, 'f', 2, 64),
		Subject:        buildAlipayF2FSubject(req.Amount),
		NotifyURL:      notifyURL,
		TimeoutExpress: getAlipayF2FTimeoutExpress(),
	})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝当面付 下单失败 user_id=%d trade_no=%s amount=%d error=%q", id, tradeNo, req.Amount, err.Error()))
		common.ApiErrorMsg(c, "拉起支付失败")
		return
	}
	if strings.TrimSpace(precreateResp.QRCode) == "" {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝当面付 下单未返回二维码 user_id=%d trade_no=%s amount=%d", id, tradeNo, req.Amount))
		common.ApiErrorMsg(c, "拉起支付失败")
		return
	}

	amount := req.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dAmount := decimal.NewFromInt(amount)
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		amount = dAmount.Div(dQuotaPerUnit).IntPart()
		if amount < 1 {
			amount = 1
		}
	}

	topUp := &model.TopUp{
		UserId:          id,
		Amount:          amount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodAlipayF2F,
		PaymentProvider: model.PaymentProviderAlipayF2F,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝当面付 创建充值订单失败 user_id=%d trade_no=%s amount=%d error=%q", id, tradeNo, req.Amount, err.Error()))
		common.ApiErrorMsg(c, "创建订单失败")
		return
	}

	common.ApiSuccess(c, gin.H{
		"trade_no":        tradeNo,
		"qr_code":         precreateResp.QRCode,
		"status":          common.TopUpStatusPending,
		"amount":          amount,
		"pay_money":       strconv.FormatFloat(payMoney, 'f', 2, 64),
		"timeout_express": getAlipayF2FTimeoutExpress(),
		"expires_in_sec":  getAlipayF2FTimeoutMinutes() * 60,
	})
}

func GetAlipayF2FTopUpStatus(c *gin.Context) {
	tradeNo := strings.TrimSpace(c.Param("tradeNo"))
	if tradeNo == "" {
		common.ApiErrorMsg(c, "缺少订单号")
		return
	}

	userID := c.GetInt("id")
	topUp := model.GetTopUpByTradeNo(tradeNo)
	if topUp == nil || topUp.UserId != userID {
		common.ApiErrorMsg(c, "充值订单不存在")
		return
	}
	if topUp.PaymentProvider != model.PaymentProviderAlipayF2F {
		common.ApiErrorMsg(c, "订单支付方式不匹配")
		return
	}

	tradeStatus := ""
	if topUp.Status == common.TopUpStatusPending {
		client, err := alipayF2FClientFactory()
		if err != nil {
			common.ApiErrorMsg(c, "支付配置错误")
			return
		}

		queryResp, err := client.Query(c.Request.Context(), tradeNo)
		if err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝当面付 查询订单失败 user_id=%d trade_no=%s error=%q", userID, tradeNo, err.Error()))
			common.ApiSuccess(c, gin.H{
				"trade_no":     tradeNo,
				"status":       topUp.Status,
				"trade_status": tradeStatus,
			})
			return
		}
		tradeStatus = queryResp.TradeStatus

		switch queryResp.TradeStatus {
		case "TRADE_SUCCESS", "TRADE_FINISHED":
			LockOrder(tradeNo)
			err = model.RechargeAlipayF2F(tradeNo, c.ClientIP())
			UnlockOrder(tradeNo)
			if err != nil && !errors.Is(err, model.ErrTopUpStatusInvalid) {
				common.ApiErrorMsg(c, "充值入账失败，请稍后重试")
				return
			}
		case "TRADE_CLOSED":
			err = model.UpdatePendingTopUpStatus(tradeNo, model.PaymentProviderAlipayF2F, common.TopUpStatusExpired)
			if err != nil && !errors.Is(err, model.ErrTopUpStatusInvalid) && !errors.Is(err, model.ErrTopUpNotFound) {
				common.ApiErrorMsg(c, "更新订单状态失败")
				return
			}
		}

		topUp = model.GetTopUpByTradeNo(tradeNo)
	}

	common.ApiSuccess(c, gin.H{
		"trade_no":     tradeNo,
		"status":       topUp.Status,
		"trade_status": tradeStatus,
	})
}

func AlipayF2FNotify(c *gin.Context) {
	if !isAlipayF2FWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝当面付 webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	if err := c.Request.ParseForm(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝当面付 webhook 表单解析失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	params := make(map[string]string, len(c.Request.PostForm))
	for key := range c.Request.PostForm {
		params[key] = c.Request.PostForm.Get(key)
	}

	client, err := alipayF2FClientFactory()
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝当面付 client 初始化失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	notification, err := client.VerifyNotification(params)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝当面付 webhook 验签失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	switch notification.TradeStatus {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		LockOrder(notification.OutTradeNo)
		defer UnlockOrder(notification.OutTradeNo)

		if err := model.RechargeAlipayF2F(notification.OutTradeNo, c.ClientIP()); err != nil && !errors.Is(err, model.ErrTopUpStatusInvalid) {
			logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝当面付 入账失败 trade_no=%s client_ip=%s error=%q", notification.OutTradeNo, c.ClientIP(), err.Error()))
			_, _ = c.Writer.Write([]byte("fail"))
			return
		}
	case "TRADE_CLOSED":
		if err := model.UpdatePendingTopUpStatus(notification.OutTradeNo, model.PaymentProviderAlipayF2F, common.TopUpStatusExpired); err != nil &&
			!errors.Is(err, model.ErrTopUpStatusInvalid) &&
			!errors.Is(err, model.ErrTopUpNotFound) {
			logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝当面付 更新关闭订单状态失败 trade_no=%s client_ip=%s error=%q", notification.OutTradeNo, c.ClientIP(), err.Error()))
			_, _ = c.Writer.Write([]byte("fail"))
			return
		}
	}

	_, _ = c.Writer.Write([]byte("success"))
}
