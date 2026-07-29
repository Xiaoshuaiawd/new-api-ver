package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	prometheusmetrics "github.com/QuantumNous/new-api/pkg/prometheus_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type billingMetricsFunding struct {
	source    string
	settleErr error
	refundErr error
}

func (f *billingMetricsFunding) Source() string       { return f.source }
func (f *billingMetricsFunding) PreConsume(int) error { return nil }
func (f *billingMetricsFunding) Settle(int) error     { return f.settleErr }
func (f *billingMetricsFunding) Refund() error        { return f.refundErr }

func useBillingMetricsRuntime(t *testing.T) *prometheusmetrics.Runtime {
	t.Helper()
	runtime, err := prometheusmetrics.NewRuntime(
		prometheusmetrics.Config{Enabled: true, AllowPublic: true},
		"v-test",
		nil,
		nil,
	)
	require.NoError(t, err)
	prometheusmetrics.SetDefaultRuntime(runtime)
	t.Cleanup(func() { prometheusmetrics.SetDefaultRuntime(nil) })
	return runtime
}

func billingMetricsOutput(runtime *prometheusmetrics.Runtime) string {
	response := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return response.Body.String()
}

func TestPersistedBillingLogsRecordQuotaTokensRefundAndSaturation(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	runtime := useBillingMetricsRuntime(t)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	model.RecordConsumeLog(ctx, 1, model.RecordConsumeLogParams{
		PromptTokens:     1000,
		CompletionTokens: 200,
		Quota:            100,
		Other: map[string]interface{}{
			"billing_source":     BillingSourceSubscription,
			"group_ratio":        0.25,
			"cache_tokens":       200,
			"cache_write_tokens": 100,
			"admin_info": map[string]interface{}{
				"quota_saturation": map[string]interface{}{
					"kind": "overflow",
				},
			},
		},
	})
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:  1,
		LogType: model.LogTypeRefund,
		Quota:   50,
		Other: map[string]interface{}{
			"billing_source": BillingSourceWallet,
			"group_ratio":    0.5,
		},
	})

	output := billingMetricsOutput(runtime)

	assert.Contains(t, output, `newapi_quota_charged_total{billing_source="subscription"} 100`)
	assert.Contains(t, output, `newapi_actual_quota_charged_total{billing_source="subscription"} 400`)
	assert.Contains(t, output, `newapi_quota_refunded_total{billing_source="wallet"} 50`)
	assert.Contains(t, output, `newapi_actual_quota_refunded_total{billing_source="wallet"} 100`)
	assert.Contains(t, output, `newapi_tokens_total{direction="input"} 700`)
	assert.Contains(t, output, `newapi_tokens_total{direction="output"} 200`)
	assert.Contains(t, output, `newapi_tokens_total{direction="cache"} 300`)
	assert.Contains(t, output, `newapi_quota_saturation_total{kind="overflow",operation="settle"} 1`)
}

func TestDisabledConsumeLoggingDoesNotRecordBillingEvent(t *testing.T) {
	truncate(t)
	runtime := useBillingMetricsRuntime(t)

	previous := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = previous })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	model.RecordConsumeLog(ctx, 1, model.RecordConsumeLogParams{Quota: 100})

	assert.NotContains(t, billingMetricsOutput(runtime), "newapi_quota_charged_total{")
}

func TestConsumeLogPersistenceFailureDoesNotRecordBillingEvent(t *testing.T) {
	runtime := useBillingMetricsRuntime(t)
	previousLogDB := model.LOG_DB
	brokenLogDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.LOG_DB = brokenLogDB
	t.Cleanup(func() { model.LOG_DB = previousLogDB })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	model.RecordConsumeLog(ctx, 1, model.RecordConsumeLogParams{Quota: 100})

	assert.NotContains(t, billingMetricsOutput(runtime), "newapi_quota_charged_total{")
}

func TestPreConsumeBillingRecordsValidationFailuresAndSaturationOnce(t *testing.T) {
	runtime := useBillingMetricsRuntime(t)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		BillingSource: BillingSourceSubscription,
		QuotaClamp: &common.QuotaClamp{
			Kind: common.QuotaClampOverflow,
		},
	}

	require.NotNil(t, PreConsumeBilling(ctx, common.MaxQuota, relayInfo))
	relayInfo.QuotaClamp = nil
	relayInfo.BillingSource = BillingSourceWallet
	require.NotNil(t, PreConsumeBilling(ctx, -1, relayInfo))

	output := billingMetricsOutput(runtime)
	assert.Contains(t, output, `newapi_billing_operations_total{billing_source="subscription",operation="pre_consume",result="error"} 1`)
	assert.Contains(t, output, `newapi_billing_failures_total{billing_source="subscription",operation="pre_consume",reason="quota_saturation"} 1`)
	assert.Contains(t, output, `newapi_quota_saturation_total{kind="overflow",operation="pre_consume"} 1`)
	assert.Contains(t, output, `newapi_billing_failures_total{billing_source="wallet",operation="pre_consume",reason="invalid_quota"} 1`)
}

func TestPreConsumeBillingRecordsWalletSuccess(t *testing.T) {
	truncate(t)
	runtime := useBillingMetricsRuntime(t)
	const userID, tokenID = 7501, 8501
	seedUser(t, userID, 1000)
	seedUnlimitedTokenForBillingGroupTest(t, tokenID, userID, "billing-metrics-wallet-success", 1000)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("token_quota", 1000)
	relayInfo := makeBillingGroupRelayInfo(userID, tokenID, "billing-metrics-wallet-success", "default", "wallet_only")

	require.Nil(t, PreConsumeBilling(ctx, 100, relayInfo))

	assert.Contains(t, billingMetricsOutput(runtime), `newapi_billing_operations_total{billing_source="wallet",operation="pre_consume",result="success"} 1`)
}

func TestPreConsumeBillingClassifiesFinalSubscriptionRejections(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		reason string
		seed   func(t *testing.T, userID, tokenID int)
		quota  int
	}{
		{
			name:   "no available subscription",
			reason: "no_available_subscription",
			quota:  100,
		},
		{
			name:   "expired subscription",
			reason: "expired",
			quota:  100,
			seed: func(t *testing.T, userID, _ int) {
				const subscriptionID, planID = 9502, 9602
				seedSubscriptionPlanForBillingGroupTest(t, planID, "default")
				seedUserSubscriptionForBillingGroupTest(t, subscriptionID, userID, planID, "default")
				require.NoError(t, model.DB.Model(&model.UserSubscription{}).
					Where("id = ?", subscriptionID).
					Update("end_time", time.Now().Add(-time.Hour).Unix()).Error)
			},
		},
		{
			name:   "insufficient subscription quota",
			reason: "insufficient_quota",
			quota:  100,
			seed: func(t *testing.T, userID, _ int) {
				const subscriptionID, planID = 9503, 9603
				seedSubscriptionPlanForBillingGroupTest(t, planID, "default")
				seedUserSubscriptionForBillingGroupTest(t, subscriptionID, userID, planID, "default")
				require.NoError(t, model.DB.Model(&model.UserSubscription{}).
					Where("id = ?", subscriptionID).
					Updates(map[string]interface{}{"amount_total": 50, "amount_used": 0}).Error)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			truncate(t)
			runtime := useBillingMetricsRuntime(t)
			userID := 7502
			tokenID := 8502
			seedUser(t, userID, 1000)
			seedUnlimitedTokenForBillingGroupTest(t, tokenID, userID, "billing-metrics-subscription-rejection", 1000)
			if testCase.seed != nil {
				testCase.seed(t, userID, tokenID)
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Set("token_quota", 1000)
			relayInfo := makeBillingGroupRelayInfo(userID, tokenID, "billing-metrics-subscription-rejection", "default", "subscription_only")

			require.NotNil(t, PreConsumeBilling(ctx, testCase.quota, relayInfo))

			output := billingMetricsOutput(runtime)
			assert.Contains(t, output, `newapi_subscription_rejections_total{reason="`+testCase.reason+`"} 1`)
			assert.Contains(t, output, `newapi_billing_operations_total{billing_source="subscription",operation="pre_consume",result="error"} 1`)
			assert.Contains(t, output, `newapi_billing_failures_total{billing_source="subscription",operation="pre_consume",reason="subscription_quota"} 1`)
		})
	}
}

func TestBillingSessionSettleRecordsFinalResultWithoutCountingIdempotentReturn(t *testing.T) {
	runtime := useBillingMetricsRuntime(t)
	session := &BillingSession{
		relayInfo:        &relaycommon.RelayInfo{IsPlayground: true},
		funding:          &billingMetricsFunding{source: BillingSourceWallet},
		preConsumedQuota: 10,
	}

	require.NoError(t, session.Settle(20))
	require.NoError(t, session.Settle(20))

	output := billingMetricsOutput(runtime)
	assert.Contains(t, output, `newapi_billing_operations_total{billing_source="wallet",operation="settle",result="success"} 1`)
}

func TestBillingSessionSettleRecordsFundingFailure(t *testing.T) {
	runtime := useBillingMetricsRuntime(t)
	session := &BillingSession{
		relayInfo:        &relaycommon.RelayInfo{IsPlayground: true},
		funding:          &billingMetricsFunding{source: BillingSourceSubscription, settleErr: errors.New("database unavailable")},
		preConsumedQuota: 10,
	}

	require.Error(t, session.Settle(20))

	output := billingMetricsOutput(runtime)
	assert.Contains(t, output, `newapi_billing_operations_total{billing_source="subscription",operation="settle",result="error"} 1`)
	assert.Contains(t, output, `newapi_billing_failures_total{billing_source="subscription",operation="settle",reason="database"} 1`)
}

func TestBillingSessionRefundRecordsOnlyTheFirstAsyncResult(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		refundErr  error
		result     string
		wantReason string
	}{
		{name: "success", result: "success"},
		{name: "failure", refundErr: errors.New("database unavailable"), result: "error", wantReason: "database"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			runtime := useBillingMetricsRuntime(t)
			session := &BillingSession{
				relayInfo:     &relaycommon.RelayInfo{IsPlayground: true},
				funding:       &billingMetricsFunding{source: BillingSourceWallet, refundErr: testCase.refundErr},
				tokenConsumed: 1,
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

			session.Refund(ctx)
			session.Refund(ctx)

			require.Eventually(t, func() bool {
				return strings.Contains(
					billingMetricsOutput(runtime),
					`newapi_billing_operations_total{billing_source="wallet",operation="refund",result="`+testCase.result+`"} 1`,
				)
			}, time.Second, 10*time.Millisecond)
			output := billingMetricsOutput(runtime)
			if testCase.wantReason != "" {
				assert.Contains(t, output, `newapi_billing_failures_total{billing_source="wallet",operation="refund",reason="`+testCase.wantReason+`"} 1`)
			}
		})
	}
}
