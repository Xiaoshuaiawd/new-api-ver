package service

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	prometheusmetrics "github.com/QuantumNous/new-api/pkg/prometheus_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	BillingSourceWallet       = "wallet"
	BillingSourceSubscription = "subscription"
)

// PreConsumeBilling 根据用户计费偏好创建 BillingSession 并执行预扣费。
// 会话存储在 relayInfo.Billing 上，供后续 Settle / Refund 使用。
func PreConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	if relayInfo != nil && relayInfo.QuotaClamp != nil {
		source := billingSourceForMetrics(relayInfo)
		prometheusmetrics.RecordBillingOperation("pre_consume", source, "error")
		prometheusmetrics.RecordBillingFailure("pre_consume", source, "quota_saturation")
		prometheusmetrics.RecordQuotaSaturation(string(relayInfo.QuotaClamp.Kind), "pre_consume")
		return types.NewErrorWithStatusCode(
			relayInfo.QuotaClamp,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if preConsumedQuota < 0 {
		source := billingSourceForMetrics(relayInfo)
		prometheusmetrics.RecordBillingOperation("pre_consume", source, "error")
		prometheusmetrics.RecordBillingFailure("pre_consume", source, "invalid_quota")
		return types.NewErrorWithStatusCode(
			fmt.Errorf("pre-consume quota cannot be negative: %d", preConsumedQuota),
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	session, apiErr := NewBillingSession(c, relayInfo, preConsumedQuota)
	if apiErr != nil {
		source := billingSourceForMetrics(relayInfo)
		prometheusmetrics.RecordBillingOperation("pre_consume", source, "error")
		prometheusmetrics.RecordBillingFailure("pre_consume", source, billingFailureReason(apiErr, source))
		return apiErr
	}
	relayInfo.Billing = session
	prometheusmetrics.RecordBillingOperation("pre_consume", session.funding.Source(), "success")
	return nil
}

func billingSourceForMetrics(relayInfo *relaycommon.RelayInfo) string {
	if relayInfo == nil {
		return "unknown"
	}
	if relayInfo.BillingSource != "" {
		return relayInfo.BillingSource
	}
	if source := relayInfo.PriceData.GroupRatioInfo.BillingSource; source != "" {
		return source
	}
	return BillingSourceWallet
}

func billingFailureReason(err error, source string) string {
	if err == nil {
		return "other"
	}
	var clamp *common.QuotaClamp
	if errors.As(err, &clamp) {
		return "quota_saturation"
	}
	if errors.Is(err, model.ErrSubscriptionQuotaInsufficient) || errors.Is(err, model.ErrNoActiveSubscription) {
		return "subscription_quota"
	}
	if apiErr, ok := err.(*types.NewAPIError); ok {
		switch apiErr.GetErrorCode() {
		case types.ErrorCodePreConsumeTokenQuotaFailed:
			return "token_quota"
		case types.ErrorCodeInsufficientUserQuota:
			if source == BillingSourceSubscription {
				return "subscription_quota"
			}
			return "user_quota"
		case types.ErrorCodeQueryDataError, types.ErrorCodeUpdateDataError:
			return "database"
		case types.ErrorCodeModelPriceError:
			return "invalid_quota"
		}
	}
	return "other"
}

func billingFundingFailureReason(err error, source string) string {
	reason := billingFailureReason(err, source)
	if reason == "other" {
		return "database"
	}
	return reason
}

// ---------------------------------------------------------------------------
// SettleBilling — 后结算辅助函数
// ---------------------------------------------------------------------------

// SettleBilling 执行计费结算。如果 RelayInfo 上有 BillingSession 则通过 session 结算，
// 否则回退到旧的 PostConsumeQuota 路径（兼容按次计费等场景）。
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	if relayInfo.Billing != nil {
		preConsumed := relayInfo.Billing.GetPreConsumedQuota()
		delta := actualQuota - preConsumed

		if delta > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后补扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else if delta < 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后返还扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(-delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费与实际消耗一致，无需调整：%s（按次计费）",
				logger.FormatQuota(actualQuota),
			))
		}

		if err := relayInfo.Billing.Settle(actualQuota); err != nil {
			return err
		}

		// 发送额度通知（订阅计费使用订阅剩余额度）
		if actualQuota != 0 {
			if relayInfo.BillingSource == BillingSourceSubscription {
				checkAndSendSubscriptionQuotaNotify(relayInfo)
			} else {
				checkAndSendQuotaNotify(relayInfo, actualQuota-preConsumed, preConsumed)
			}
		}
		return nil
	}

	// 回退：无 BillingSession 时使用旧路径
	quotaDelta := actualQuota - relayInfo.FinalPreConsumedQuota
	if quotaDelta != 0 {
		source := billingSourceForMetrics(relayInfo)
		err := PostConsumeQuota(relayInfo, quotaDelta, relayInfo.FinalPreConsumedQuota, true)
		if err != nil {
			prometheusmetrics.RecordBillingOperation("settle", source, "error")
			prometheusmetrics.RecordBillingFailure("settle", source, billingFailureReason(err, source))
			return err
		}
		prometheusmetrics.RecordBillingOperation("settle", source, "success")
	}
	return nil
}
