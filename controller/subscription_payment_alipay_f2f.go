package controller

import (
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
	"github.com/gin-gonic/gin"
)

type SubscriptionAlipayF2FPayRequest struct {
	PlanId        int    `json:"plan_id"`
	PaymentMethod string `json:"payment_method"`
}

func buildSubscriptionAlipayF2FSubject(planTitle string) string {
	subjectPrefix := strings.TrimSpace(setting.AlipayF2FSubjectPrefix)
	if subjectPrefix == "" {
		subjectPrefix = "new-api"
	}
	title := strings.TrimSpace(planTitle)
	if title == "" {
		title = "Subscription"
	}
	return fmt.Sprintf("%s Subscription %s", subjectPrefix, title)
}

func SubscriptionRequestAlipayF2FPay(c *gin.Context) {
	if !isAlipayF2FTopUpEnabled() {
		common.ApiErrorMsg(c, "管理员未开启支付宝当面付支付")
		return
	}

	var req SubscriptionAlipayF2FPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if req.PaymentMethod != model.PaymentMethodAlipayF2F {
		common.ApiErrorMsg(c, "不支持的支付方式")
		return
	}

	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorMsg(c, "套餐未启用")
		return
	}
	if plan.PriceAmount < 0.01 {
		common.ApiErrorMsg(c, "套餐金额过低")
		return
	}

	userId := c.GetInt("id")
	if plan.MaxPurchasePerUser > 0 {
		count, err := model.CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			common.ApiErrorMsg(c, "已达到该套餐购买上限")
			return
		}
	}

	client, err := alipayF2FClientFactory()
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝当面付订阅 client 初始化失败 user_id=%d error=%q", userId, err.Error()))
		common.ApiErrorMsg(c, "支付配置错误")
		return
	}

	tradeNo := fmt.Sprintf("SUBALP%d%d%s", userId, time.Now().UnixMilli(), common.GetRandomString(6))
	notifyURL := strings.TrimRight(service.GetCallbackAddress(), "/") + "/api/subscription/alipay-f2f/notify"
	precreateResp, err := client.Precreate(c.Request.Context(), &service.AlipayF2FPrecreateRequest{
		OutTradeNo:     tradeNo,
		TotalAmount:    strconv.FormatFloat(plan.PriceAmount, 'f', 2, 64),
		Subject:        buildSubscriptionAlipayF2FSubject(plan.Title),
		NotifyURL:      notifyURL,
		TimeoutExpress: getAlipayF2FTimeoutExpress(),
	})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝当面付订阅下单失败 user_id=%d trade_no=%s plan_id=%d error=%q", userId, tradeNo, plan.Id, err.Error()))
		common.ApiErrorMsg(c, buildAlipayF2FErrorMessage(c, "拉起支付失败", err))
		return
	}
	if strings.TrimSpace(precreateResp.QRCode) == "" {
		common.ApiErrorMsg(c, "拉起支付失败")
		return
	}

	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodAlipayF2F,
		PaymentProvider: model.PaymentProviderAlipayF2F,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		common.ApiErrorMsg(c, "创建订单失败")
		return
	}

	common.ApiSuccess(c, gin.H{
		"trade_no":        tradeNo,
		"qr_code":         precreateResp.QRCode,
		"status":          common.TopUpStatusPending,
		"pay_money":       strconv.FormatFloat(plan.PriceAmount, 'f', 2, 64),
		"timeout_express": getAlipayF2FTimeoutExpress(),
		"expires_in_sec":  getAlipayF2FTimeoutMinutes() * 60,
	})
}

func GetSubscriptionAlipayF2FOrderStatus(c *gin.Context) {
	tradeNo := strings.TrimSpace(c.Param("tradeNo"))
	if tradeNo == "" {
		common.ApiErrorMsg(c, "缺少订单号")
		return
	}

	userID := c.GetInt("id")
	order := model.GetSubscriptionOrderByTradeNo(tradeNo)
	if order == nil || order.UserId != userID {
		common.ApiErrorMsg(c, "订阅订单不存在")
		return
	}
	if order.PaymentProvider != model.PaymentProviderAlipayF2F {
		common.ApiErrorMsg(c, "订单支付方式不匹配")
		return
	}

	tradeStatus := ""
	if order.Status == common.TopUpStatusPending {
		client, err := alipayF2FClientFactory()
		if err != nil {
			common.ApiErrorMsg(c, "支付配置错误")
			return
		}

		queryResp, err := client.Query(c.Request.Context(), tradeNo)
		if err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝当面付订阅查询失败 user_id=%d trade_no=%s error=%q", userID, tradeNo, err.Error()))
			common.ApiSuccess(c, gin.H{
				"trade_no":     tradeNo,
				"status":       order.Status,
				"trade_status": tradeStatus,
			})
			return
		}
		tradeStatus = queryResp.TradeStatus

		switch queryResp.TradeStatus {
		case "TRADE_SUCCESS", "TRADE_FINISHED":
			LockOrder(tradeNo)
			err = model.CompleteSubscriptionOrder(tradeNo, common.GetJsonString(queryResp), model.PaymentProviderAlipayF2F, model.PaymentMethodAlipayF2F)
			UnlockOrder(tradeNo)
			if err != nil && !errors.Is(err, model.ErrSubscriptionOrderStatusInvalid) {
				common.ApiErrorMsg(c, "订阅开通失败，请稍后重试")
				return
			}
		case "TRADE_CLOSED":
			err = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderAlipayF2F)
			if err != nil && !errors.Is(err, model.ErrSubscriptionOrderStatusInvalid) && !errors.Is(err, model.ErrSubscriptionOrderNotFound) {
				common.ApiErrorMsg(c, "更新订单状态失败")
				return
			}
		}

		order = model.GetSubscriptionOrderByTradeNo(tradeNo)
	}

	common.ApiSuccess(c, gin.H{
		"trade_no":     tradeNo,
		"status":       order.Status,
		"trade_status": tradeStatus,
	})
}

func SubscriptionAlipayF2FNotify(c *gin.Context) {
	if !isAlipayF2FWebhookEnabled() {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	if err := c.Request.ParseForm(); err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	params := make(map[string]string, len(c.Request.PostForm))
	for key := range c.Request.PostForm {
		params[key] = c.Request.PostForm.Get(key)
	}

	client, err := alipayF2FClientFactory()
	if err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	notification, err := client.VerifyNotification(params)
	if err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	switch notification.TradeStatus {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		LockOrder(notification.OutTradeNo)
		defer UnlockOrder(notification.OutTradeNo)

		if err := model.CompleteSubscriptionOrder(notification.OutTradeNo, common.GetJsonString(notification), model.PaymentProviderAlipayF2F, model.PaymentMethodAlipayF2F); err != nil &&
			!errors.Is(err, model.ErrSubscriptionOrderStatusInvalid) {
			_, _ = c.Writer.Write([]byte("fail"))
			return
		}
	case "TRADE_CLOSED":
		if err := model.ExpireSubscriptionOrder(notification.OutTradeNo, model.PaymentProviderAlipayF2F); err != nil &&
			!errors.Is(err, model.ErrSubscriptionOrderStatusInvalid) &&
			!errors.Is(err, model.ErrSubscriptionOrderNotFound) {
			_, _ = c.Writer.Write([]byte("fail"))
			return
		}
	}

	_, _ = c.Writer.Write([]byte("success"))
}
