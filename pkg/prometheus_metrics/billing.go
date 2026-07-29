package prometheusmetrics

import (
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"

	"github.com/prometheus/client_golang/prometheus"
)

type billingMetrics struct {
	tokens                 *prometheus.CounterVec
	quotaCharged           *prometheus.CounterVec
	quotaRefunded          *prometheus.CounterVec
	actualQuotaCharged     *prometheus.CounterVec
	actualQuotaRefunded    *prometheus.CounterVec
	operations             *prometheus.CounterVec
	failures               *prometheus.CounterVec
	quotaSaturation        *prometheus.CounterVec
	subscriptionRejections *prometheus.CounterVec
}

type BillingLogEvent struct {
	EventType        string
	BillingSource    string
	Quota            int
	GroupRatio       float64
	PromptTokens     int
	CompletionTokens int
	CacheTokens      int
	CacheWriteTokens int
	SaturationKind   string
	Operation        string
}

type persistedBillingMetadata struct {
	BillingSource    string   `json:"billing_source"`
	GroupRatio       *float64 `json:"group_ratio"`
	CacheTokens      int      `json:"cache_tokens"`
	CacheWriteTokens int      `json:"cache_write_tokens"`
	ActualQuota      any      `json:"actual_quota"`
	AdminInfo        struct {
		QuotaSaturation struct {
			Kind string `json:"kind"`
		} `json:"quota_saturation"`
	} `json:"admin_info"`
}

func registerBillingMetrics(registry prometheus.Registerer) (*billingMetrics, error) {
	metrics := &billingMetrics{
		tokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_tokens_total",
			Help: "Total billable tokens recorded on persisted consume logs.",
		}, []string{"direction"}),
		quotaCharged: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_quota_charged_total",
			Help: "Total internal quota recorded by persisted consume logs.",
		}, []string{"billing_source"}),
		quotaRefunded: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_quota_refunded_total",
			Help: "Total internal quota recorded by persisted refund logs.",
		}, []string{"billing_source"}),
		actualQuotaCharged: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_actual_quota_charged_total",
			Help: "Total pre-group-ratio quota recorded by persisted consume logs.",
		}, []string{"billing_source"}),
		actualQuotaRefunded: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_actual_quota_refunded_total",
			Help: "Total pre-group-ratio quota recorded by persisted refund logs.",
		}, []string{"billing_source"}),
		operations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_billing_operations_total",
			Help: "Total billing lifecycle operations by source and result.",
		}, []string{"operation", "billing_source", "result"}),
		failures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_billing_failures_total",
			Help: "Total billing lifecycle failures by fixed reason.",
		}, []string{"operation", "billing_source", "reason"}),
		quotaSaturation: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_quota_saturation_total",
			Help: "Total audited quota saturation events on persisted billing logs.",
		}, []string{"kind", "operation"}),
		subscriptionRejections: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_subscription_rejections_total",
			Help: "Total requests rejected by subscription availability or quota checks.",
		}, []string{"reason"}),
	}

	collectors := []struct {
		name      string
		collector prometheus.Collector
	}{
		{name: "billing tokens", collector: metrics.tokens},
		{name: "quota charged", collector: metrics.quotaCharged},
		{name: "quota refunded", collector: metrics.quotaRefunded},
		{name: "actual quota charged", collector: metrics.actualQuotaCharged},
		{name: "actual quota refunded", collector: metrics.actualQuotaRefunded},
		{name: "billing operations", collector: metrics.operations},
		{name: "billing failures", collector: metrics.failures},
		{name: "quota saturation", collector: metrics.quotaSaturation},
		{name: "subscription rejections", collector: metrics.subscriptionRejections},
	}
	for _, item := range collectors {
		if err := registry.Register(item.collector); err != nil {
			return nil, fmt.Errorf("register %s metric: %w", item.name, err)
		}
	}
	return metrics, nil
}

func RecordBillingLogEvent(event BillingLogEvent) {
	runtime := defaultRuntime.Load()
	if runtime == nil || runtime.billing == nil {
		return
	}
	runtime.RecordBillingLogEvent(event)
}

func RecordPersistedBillingLog(eventType string, quota, promptTokens, completionTokens int, otherJSON string) {
	metadata := persistedBillingMetadata{}
	if otherJSON != "" {
		_ = common.UnmarshalJsonStr(otherJSON, &metadata)
	}
	groupRatio := 0.0
	if metadata.GroupRatio != nil {
		groupRatio = *metadata.GroupRatio
	}
	operation := "settle"
	if metadata.ActualQuota != nil {
		operation = "task_recalculate"
	}
	RecordBillingLogEvent(BillingLogEvent{
		EventType:        eventType,
		BillingSource:    metadata.BillingSource,
		Quota:            quota,
		GroupRatio:       groupRatio,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		CacheTokens:      metadata.CacheTokens,
		CacheWriteTokens: metadata.CacheWriteTokens,
		SaturationKind:   metadata.AdminInfo.QuotaSaturation.Kind,
		Operation:        operation,
	})
}

func (r *Runtime) RecordBillingLogEvent(event BillingLogEvent) {
	if r == nil || r.billing == nil {
		return
	}

	if kind, ok := normalizeQuotaSaturationKind(event.SaturationKind); ok {
		r.billing.quotaSaturation.WithLabelValues(kind, normalizeBillingOperation(event.Operation)).Inc()
	}

	if event.EventType == "consume" {
		promptTokens := nonNegativeTokenCount(event.PromptTokens)
		cacheTokens := nonNegativeTokenCount(event.CacheTokens) + nonNegativeTokenCount(event.CacheWriteTokens)
		var inputTokens uint64
		if promptTokens > cacheTokens {
			inputTokens = promptTokens - cacheTokens
		}
		recordTokenCount(r.billing.tokens, "input", inputTokens)
		recordTokenCount(r.billing.tokens, "output", nonNegativeTokenCount(event.CompletionTokens))
		recordTokenCount(r.billing.tokens, "cache", cacheTokens)
	}

	if event.Quota <= 0 {
		return
	}
	source := normalizeBillingSource(event.BillingSource)
	quota := float64(event.Quota)
	actualQuota := quotaBeforeGroupRatio(quota, event.GroupRatio)
	switch event.EventType {
	case "consume":
		r.billing.quotaCharged.WithLabelValues(source).Add(quota)
		r.billing.actualQuotaCharged.WithLabelValues(source).Add(actualQuota)
	case "refund":
		r.billing.quotaRefunded.WithLabelValues(source).Add(quota)
		r.billing.actualQuotaRefunded.WithLabelValues(source).Add(actualQuota)
	}
}

func RecordBillingOperation(operation, billingSource, result string) {
	runtime := defaultRuntime.Load()
	if runtime == nil || runtime.billing == nil {
		return
	}
	runtime.billing.operations.WithLabelValues(
		normalizeBillingOperation(operation),
		normalizeBillingSource(billingSource),
		normalizeBillingResult(result),
	).Inc()
}

func RecordBillingFailure(operation, billingSource, reason string) {
	runtime := defaultRuntime.Load()
	if runtime == nil || runtime.billing == nil {
		return
	}
	runtime.billing.failures.WithLabelValues(
		normalizeBillingOperation(operation),
		normalizeBillingSource(billingSource),
		normalizeBillingFailureReason(reason),
	).Inc()
}

func RecordSubscriptionRejection(reason string) {
	runtime := defaultRuntime.Load()
	if runtime == nil || runtime.billing == nil {
		return
	}
	runtime.billing.subscriptionRejections.WithLabelValues(normalizeSubscriptionRejectionReason(reason)).Inc()
}

func RecordQuotaSaturation(kind, operation string) {
	runtime := defaultRuntime.Load()
	if runtime == nil || runtime.billing == nil {
		return
	}
	normalizedKind, ok := normalizeQuotaSaturationKind(kind)
	if !ok {
		return
	}
	runtime.billing.quotaSaturation.WithLabelValues(
		normalizedKind,
		normalizeBillingOperation(operation),
	).Inc()
}

func normalizeBillingSource(source string) string {
	switch source {
	case "", "wallet":
		return "wallet"
	case "subscription":
		return source
	default:
		return "unknown"
	}
}

func normalizeBillingOperation(operation string) string {
	switch operation {
	case "pre_consume", "settle", "refund", "task_recalculate":
		return operation
	default:
		return "other"
	}
}

func normalizeBillingResult(result string) string {
	if result == "success" {
		return result
	}
	return "error"
}

func normalizeBillingFailureReason(reason string) string {
	switch reason {
	case "invalid_quota", "quota_saturation", "token_quota", "user_quota", "subscription_quota", "database":
		return reason
	default:
		return "other"
	}
}

func normalizeSubscriptionRejectionReason(reason string) string {
	switch reason {
	case "insufficient_quota", "no_available_subscription", "expired":
		return reason
	default:
		return "other"
	}
}

func normalizeQuotaSaturationKind(kind string) (string, bool) {
	switch kind {
	case "overflow", "underflow", "nan":
		return kind, true
	default:
		return "", false
	}
}

func quotaBeforeGroupRatio(quota, groupRatio float64) float64 {
	if groupRatio <= 0 || math.IsNaN(groupRatio) || math.IsInf(groupRatio, 0) {
		groupRatio = 1
	}
	actualQuota := quota / groupRatio
	if actualQuota <= 0 || math.IsNaN(actualQuota) || math.IsInf(actualQuota, 0) {
		return quota
	}
	return actualQuota
}

func recordTokenCount(counter *prometheus.CounterVec, direction string, tokens uint64) {
	if counter == nil || tokens <= 0 {
		return
	}
	counter.WithLabelValues(direction).Add(float64(tokens))
}

func nonNegativeTokenCount(value int) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}
