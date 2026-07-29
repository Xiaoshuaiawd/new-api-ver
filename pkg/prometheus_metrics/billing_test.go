package prometheusmetrics

import (
	"math"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordBillingLogEventSeparatesChargeTokensAndActualQuota(t *testing.T) {
	runtime, err := NewRuntime(Config{Enabled: true, AllowPublic: true}, "v-test", nil, nil)
	require.NoError(t, err)
	SetDefaultRuntime(runtime)
	t.Cleanup(func() { SetDefaultRuntime(nil) })

	RecordBillingLogEvent(BillingLogEvent{
		EventType:        "consume",
		BillingSource:    "",
		Quota:            100,
		GroupRatio:       0.25,
		PromptTokens:     1000,
		CompletionTokens: 200,
		CacheTokens:      200,
		CacheWriteTokens: 100,
		SaturationKind:   "overflow",
		Operation:        "settle",
	})

	assert.Equal(t, float64(100), testutil.ToFloat64(runtime.billing.quotaCharged.WithLabelValues("wallet")))
	assert.Equal(t, float64(400), testutil.ToFloat64(runtime.billing.actualQuotaCharged.WithLabelValues("wallet")))
	assert.Equal(t, float64(700), testutil.ToFloat64(runtime.billing.tokens.WithLabelValues("input")))
	assert.Equal(t, float64(200), testutil.ToFloat64(runtime.billing.tokens.WithLabelValues("output")))
	assert.Equal(t, float64(300), testutil.ToFloat64(runtime.billing.tokens.WithLabelValues("cache")))
	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.billing.quotaSaturation.WithLabelValues("overflow", "settle")))
}

func TestRecordBillingLogEventSeparatesRefundAndSanitizesValues(t *testing.T) {
	runtime, err := NewRuntime(Config{Enabled: true, AllowPublic: true}, "v-test", nil, nil)
	require.NoError(t, err)
	SetDefaultRuntime(runtime)
	t.Cleanup(func() { SetDefaultRuntime(nil) })

	RecordBillingLogEvent(BillingLogEvent{
		EventType:     "refund",
		BillingSource: "subscription",
		Quota:         50,
		GroupRatio:    0.5,
	})
	RecordBillingLogEvent(BillingLogEvent{
		EventType:     "consume",
		BillingSource: "unexpected-source",
		Quota:         25,
		GroupRatio:    math.NaN(),
		PromptTokens:  -10,
		CacheTokens:   50,
	})
	RecordBillingLogEvent(BillingLogEvent{EventType: "consume", Quota: -1, GroupRatio: 1})

	assert.Equal(t, float64(50), testutil.ToFloat64(runtime.billing.quotaRefunded.WithLabelValues("subscription")))
	assert.Equal(t, float64(100), testutil.ToFloat64(runtime.billing.actualQuotaRefunded.WithLabelValues("subscription")))
	assert.Equal(t, float64(25), testutil.ToFloat64(runtime.billing.quotaCharged.WithLabelValues("unknown")))
	assert.Equal(t, float64(25), testutil.ToFloat64(runtime.billing.actualQuotaCharged.WithLabelValues("unknown")))
	assert.Equal(t, float64(50), testutil.ToFloat64(runtime.billing.tokens.WithLabelValues("cache")))
	assert.Zero(t, testutil.ToFloat64(runtime.billing.tokens.WithLabelValues("input")))
}

func TestRecordBillingLogEventDoesNotOverflowTokenSubtraction(t *testing.T) {
	runtime, err := NewRuntime(Config{Enabled: true, AllowPublic: true}, "v-test", nil, nil)
	require.NoError(t, err)
	SetDefaultRuntime(runtime)
	t.Cleanup(func() { SetDefaultRuntime(nil) })

	RecordBillingLogEvent(BillingLogEvent{
		EventType:    "consume",
		PromptTokens: math.MaxInt,
		CacheTokens:  -1,
	})

	assert.Equal(t, float64(math.MaxInt), testutil.ToFloat64(runtime.billing.tokens.WithLabelValues("input")))
	assert.Zero(t, testutil.ToFloat64(runtime.billing.tokens.WithLabelValues("cache")))
}

func TestBillingLifecycleMetricsNormalizeFixedLabels(t *testing.T) {
	runtime, err := NewRuntime(Config{Enabled: true, AllowPublic: true}, "v-test", nil, nil)
	require.NoError(t, err)
	SetDefaultRuntime(runtime)
	t.Cleanup(func() { SetDefaultRuntime(nil) })

	RecordBillingOperation("pre_consume", "wallet", "success")
	RecordBillingOperation("raw operation", "raw source", "raw result")
	RecordBillingFailure("settle", "subscription", "database")
	RecordBillingFailure("raw operation", "raw source", "raw reason")
	RecordSubscriptionRejection("expired")
	RecordSubscriptionRejection("raw reason")

	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.billing.operations.WithLabelValues("pre_consume", "wallet", "success")))
	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.billing.operations.WithLabelValues("other", "unknown", "error")))
	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.billing.failures.WithLabelValues("settle", "subscription", "database")))
	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.billing.failures.WithLabelValues("other", "unknown", "other")))
	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.billing.subscriptionRejections.WithLabelValues("expired")))
	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.billing.subscriptionRejections.WithLabelValues("other")))
}

func TestRecordPersistedBillingLogUsesFrozenMetadataAndSafeDefaults(t *testing.T) {
	runtime, err := NewRuntime(Config{Enabled: true, AllowPublic: true}, "v-test", nil, nil)
	require.NoError(t, err)
	SetDefaultRuntime(runtime)
	t.Cleanup(func() { SetDefaultRuntime(nil) })

	RecordPersistedBillingLog("consume", 20, 10, 5, `{"billing_source":"subscription","group_ratio":0.25,"actual_quota":80,"admin_info":{"quota_saturation":{"kind":"overflow"}}}`)
	RecordPersistedBillingLog("consume", 10, 0, 0, `{malformed`)

	assert.Equal(t, float64(20), testutil.ToFloat64(runtime.billing.quotaCharged.WithLabelValues("subscription")))
	assert.Equal(t, float64(80), testutil.ToFloat64(runtime.billing.actualQuotaCharged.WithLabelValues("subscription")))
	assert.Equal(t, float64(10), testutil.ToFloat64(runtime.billing.quotaCharged.WithLabelValues("wallet")))
	assert.Equal(t, float64(10), testutil.ToFloat64(runtime.billing.actualQuotaCharged.WithLabelValues("wallet")))
	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.billing.quotaSaturation.WithLabelValues("overflow", "task_recalculate")))
}

func TestRecordQuotaSaturationRejectsUnknownKinds(t *testing.T) {
	runtime, err := NewRuntime(Config{Enabled: true, AllowPublic: true}, "v-test", nil, nil)
	require.NoError(t, err)
	SetDefaultRuntime(runtime)
	t.Cleanup(func() { SetDefaultRuntime(nil) })

	RecordQuotaSaturation("overflow", "pre_consume")
	RecordQuotaSaturation("unbounded", "pre_consume")

	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.billing.quotaSaturation.WithLabelValues("overflow", "pre_consume")))
}
