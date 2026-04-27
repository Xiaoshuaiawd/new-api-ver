package common

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestPrometheusMetricsTrackChannelAttemptLifecycle(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	metrics, err := NewPrometheusMetrics(reg)
	require.NoError(t, err)

	labels := ChannelMetricLabels{
		ChannelID:   "101",
		ChannelName: "alpha",
		ChannelType: "1",
		RequestKind: "relay",
	}

	attempt := metrics.BeginChannelAttemptWithModel(labels, "gpt-4o-mini")
	require.Equal(t, float64(1), testutil.ToFloat64(
		metrics.channelInflight.WithLabelValues("101", "alpha", "1", "relay"),
	))

	time.Sleep(time.Millisecond)
	attempt.Done(ChannelAttemptResult{
		Result:     ChannelRequestResultSuccess,
		StatusCode: 200,
	})

	require.Equal(t, float64(0), testutil.ToFloat64(
		metrics.channelInflight.WithLabelValues("101", "alpha", "1", "relay"),
	))
	require.Equal(t, float64(1), testutil.ToFloat64(
		metrics.channelRequests.WithLabelValues("101", "alpha", "1", "relay", ChannelRequestResultSuccess),
	))
	require.Equal(t, float64(1), testutil.ToFloat64(
		metrics.channelModelRequests.WithLabelValues("101", "alpha", "1", "relay", "gpt-4o-mini", ChannelRequestResultSuccess),
	))
	require.Equal(t, uint64(1), histogramSampleCount(t, metrics.channelAttemptDuration,
		"channel_id", "101",
		"channel_name", "alpha",
		"channel_type", "1",
		"request_kind", "relay",
		"result", ChannelRequestResultSuccess,
	))
	require.Equal(t, float64(1), gaugeMetricValue(t, metrics.channelSuccessRPMLast1m,
		"channel_id", "101",
		"channel_name", "alpha",
		"channel_type", "1",
		"request_kind", "relay",
	))
	require.Equal(t, float64(1), gaugeMetricValue(t, metrics.channelRequestsLast1m,
		"channel_id", "101",
		"channel_name", "alpha",
		"channel_type", "1",
		"request_kind", "relay",
	))
}

func TestPrometheusMetricsTrackErrorsAndSystemRejections(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	metrics, err := NewPrometheusMetrics(reg)
	require.NoError(t, err)

	labels := ChannelMetricLabels{
		ChannelID:   "202",
		ChannelName: "beta",
		ChannelType: "9",
		RequestKind: "task",
	}

	attempt := metrics.BeginChannelAttemptWithModel(labels, "claude-3-5-sonnet")
	attempt.Done(ChannelAttemptResult{
		Result:     ChannelRequestResultError,
		StatusCode: 504,
		ErrorCode:  "upstream_timeout",
	})

	metrics.ObserveSystemPerformanceRejection("cpu")

	require.Equal(t, float64(1), testutil.ToFloat64(
		metrics.channelRequests.WithLabelValues("202", "beta", "9", "task", ChannelRequestResultError),
	))
	require.Equal(t, float64(1), testutil.ToFloat64(
		metrics.channelModelRequests.WithLabelValues("202", "beta", "9", "task", "claude-3-5-sonnet", ChannelRequestResultError),
	))
	require.Equal(t, float64(1), testutil.ToFloat64(
		metrics.channelErrors.WithLabelValues("202", "beta", "9", "task", "upstream_timeout", "504"),
	))
	require.Equal(t, float64(1), testutil.ToFloat64(
		metrics.systemPerformanceRejections.WithLabelValues("cpu"),
	))
	require.Equal(t, float64(1), gaugeMetricValue(t, metrics.channelRequestsLast1m,
		"channel_id", "202",
		"channel_name", "beta",
		"channel_type", "9",
		"request_kind", "task",
	))
}

func TestPrometheusMetricsChannelSuccessRPMUsesExactSlidingWindow(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	metrics, err := NewPrometheusMetrics(reg)
	require.NoError(t, err)

	baseTime := time.Unix(1_700_000_000, 0)
	metrics.now = func() time.Time {
		return baseTime
	}

	labels := ChannelMetricLabels{
		ChannelID:   "303",
		ChannelName: "gamma",
		ChannelType: "7",
		RequestKind: "relay",
	}

	for range 100 {
		attempt := metrics.BeginChannelAttemptWithModel(labels, "gemini-2.5-flash")
		attempt.Done(ChannelAttemptResult{
			Result:     ChannelRequestResultSuccess,
			StatusCode: 200,
		})
	}

	require.Equal(t, float64(100), gaugeMetricValue(t, metrics.channelSuccessRPMLast1m,
		"channel_id", "303",
		"channel_name", "gamma",
		"channel_type", "7",
		"request_kind", "relay",
	))
	require.Equal(t, float64(100), gaugeMetricValue(t, metrics.channelRequestsLast1m,
		"channel_id", "303",
		"channel_name", "gamma",
		"channel_type", "7",
		"request_kind", "relay",
	))

	baseTime = baseTime.Add(61 * time.Second)

	require.Equal(t, float64(0), gaugeMetricValue(t, metrics.channelSuccessRPMLast1m,
		"channel_id", "303",
		"channel_name", "gamma",
		"channel_type", "7",
		"request_kind", "relay",
	))
	require.Equal(t, float64(0), gaugeMetricValue(t, metrics.channelRequestsLast1m,
		"channel_id", "303",
		"channel_name", "gamma",
		"channel_type", "7",
		"request_kind", "relay",
	))
}

func TestPrometheusMetricsTrackLastChannelErrorDetails(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	metrics, err := NewPrometheusMetrics(reg)
	require.NoError(t, err)

	baseTime := time.Unix(1_700_000_100, 0)
	metrics.now = func() time.Time {
		return baseTime
	}

	labels := ChannelMetricLabels{
		ChannelID:   "404",
		ChannelName: "delta",
		ChannelType: "3",
		RequestKind: "relay",
	}

	metrics.ObserveChannelLastError(labels, ChannelErrorDetail{
		ModelName:    "gpt-4o",
		ErrorType:    "upstream_error",
		ErrorCode:    "bad_response_status_code",
		StatusCode:   429,
		ErrorMessage: "quota exceeded\nplease retry later",
	})

	require.Equal(t, float64(baseTime.Unix()), gaugeMetricValue(t, metrics.channelLastErrorTimestamp,
		"channel_id", "404",
		"channel_name", "delta",
		"channel_type", "3",
		"request_kind", "relay",
		"model_name", "gpt-4o",
		"error_type", "upstream_error",
		"error_code", "bad_response_status_code",
		"status_code", "429",
		"error_message", "quota exceeded please retry later",
	))
}

func TestPrometheusMetricsTrackChannelFirstResponseDuration(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	metrics, err := NewPrometheusMetrics(reg)
	require.NoError(t, err)

	labels := ChannelMetricLabels{
		ChannelID:   "505",
		ChannelName: "epsilon",
		ChannelType: "2",
		RequestKind: "relay",
	}

	metrics.ObserveChannelFirstResponseDuration(labels, 1500*time.Millisecond)

	require.Equal(t, uint64(1), histogramSampleCount(t, metrics.channelFirstResponseDuration,
		"channel_id", "505",
		"channel_name", "epsilon",
		"channel_type", "2",
		"request_kind", "relay",
	))
	require.InDelta(t, 1.5, histogramSampleSum(t, metrics.channelFirstResponseDuration,
		"channel_id", "505",
		"channel_name", "epsilon",
		"channel_type", "2",
		"request_kind", "relay",
	), 0.000001)
}

func histogramSampleCount(t *testing.T, collector prometheus.Collector, labels ...string) uint64 {
	t.Helper()

	metricCh := make(chan prometheus.Metric, 16)
	go func() {
		defer close(metricCh)
		collector.Collect(metricCh)
	}()

	want := make(map[string]string, len(labels)/2)
	for i := 0; i < len(labels); i += 2 {
		want[labels[i]] = labels[i+1]
	}

	for metric := range metricCh {
		desc := &dto.Metric{}
		require.NoError(t, metric.Write(desc))
		if !matchLabels(desc.GetLabel(), want) {
			continue
		}
		if desc.GetHistogram() != nil {
			return desc.GetHistogram().GetSampleCount()
		}
	}

	t.Fatalf("histogram with labels %+v not found", want)
	return 0
}

func histogramSampleSum(t *testing.T, collector prometheus.Collector, labels ...string) float64 {
	t.Helper()

	metricCh := make(chan prometheus.Metric, 16)
	go func() {
		defer close(metricCh)
		collector.Collect(metricCh)
	}()

	want := make(map[string]string, len(labels)/2)
	for i := 0; i < len(labels); i += 2 {
		want[labels[i]] = labels[i+1]
	}

	for metric := range metricCh {
		desc := &dto.Metric{}
		require.NoError(t, metric.Write(desc))
		if !matchLabels(desc.GetLabel(), want) {
			continue
		}
		if desc.GetHistogram() != nil {
			return desc.GetHistogram().GetSampleSum()
		}
	}

	t.Fatalf("histogram with labels %+v not found", want)
	return 0
}

func gaugeMetricValue(t *testing.T, collector prometheus.Collector, labels ...string) float64 {
	t.Helper()

	metricCh := make(chan prometheus.Metric, 16)
	go func() {
		defer close(metricCh)
		collector.Collect(metricCh)
	}()

	want := make(map[string]string, len(labels)/2)
	for i := 0; i < len(labels); i += 2 {
		want[labels[i]] = labels[i+1]
	}

	for metric := range metricCh {
		desc := &dto.Metric{}
		require.NoError(t, metric.Write(desc))
		if !matchLabels(desc.GetLabel(), want) {
			continue
		}
		if desc.GetGauge() != nil {
			return desc.GetGauge().GetValue()
		}
	}

	t.Fatalf("gauge with labels %+v not found", want)
	return 0
}

func matchLabels(pairs []*dto.LabelPair, want map[string]string) bool {
	if len(pairs) != len(want) {
		return false
	}
	for _, pair := range pairs {
		value, ok := want[pair.GetName()]
		if !ok || value != pair.GetValue() {
			return false
		}
	}
	return true
}
