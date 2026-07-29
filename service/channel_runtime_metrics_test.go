package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	prometheusmetrics "github.com/QuantumNous/new-api/pkg/prometheus_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelRuntimeTrackerCountsOnlySuccessfulAttemptsInRPM(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := newChannelRuntimeTracker(func() time.Time { return now })

	first := tracker.begin(7)
	second := tracker.begin(7)

	metrics := tracker.snapshot([]int{7})[7]
	require.Equal(t, 2, metrics.Concurrency)
	assert.Zero(t, metrics.RPM)

	first.Done(true)
	first.Done(false)
	metrics = tracker.snapshot([]int{7})[7]
	require.Equal(t, 1, metrics.Concurrency)
	assert.Equal(t, 1, metrics.RPM)

	second.Done(false)
	metrics = tracker.snapshot([]int{7})[7]
	assert.Zero(t, metrics.Concurrency)
	assert.Equal(t, 1, metrics.RPM)
}

func TestChannelRuntimeTrackerKeepsOnlyTheLatestSixtySeconds(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := newChannelRuntimeTracker(func() time.Time { return now })

	tracker.begin(7).Done(true)
	now = now.Add(59 * time.Second)
	assert.Equal(t, 1, tracker.snapshot([]int{7})[7].RPM)

	now = now.Add(time.Second)
	assert.Zero(t, tracker.snapshot([]int{7})[7].RPM)
}

func TestChannelRuntimeTrackerSeparatesChannelsAndIgnoresInvalidIDs(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := newChannelRuntimeTracker(func() time.Time { return now })

	first := tracker.begin(7)
	second := tracker.begin(9)
	invalid := tracker.begin(0)

	first.Done(true)
	second.Done(false)
	metrics := tracker.snapshot([]int{0, 7, 9})
	assert.Equal(t, channelRuntimeMetrics{}, metrics[0])
	assert.Equal(t, channelRuntimeMetrics{RPM: 1}, metrics[7])
	assert.Equal(t, channelRuntimeMetrics{}, metrics[9])

	invalid.Done(true)
}

func TestChannelRuntimeTrackerRemovesExpiredInactiveChannels(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := newChannelRuntimeTracker(func() time.Time { return now })

	tracker.begin(7).Done(true)
	require.Contains(t, tracker.channels, 7)

	now = now.Add(time.Duration(channelRuntimeWindowSeconds) * time.Second)
	metrics := tracker.snapshot([]int{7})[7]

	assert.Zero(t, metrics.Concurrency)
	assert.Zero(t, metrics.RPM)
	assert.NotContains(t, tracker.channels, 7)
}

func TestChannelRuntimeTrackerKeepsInflightChannelsDuringCleanup(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := newChannelRuntimeTracker(func() time.Time { return now })
	active := tracker.begin(7)

	now = now.Add(2 * time.Duration(channelRuntimeWindowSeconds) * time.Second)
	metrics := tracker.snapshot([]int{7})[7]

	assert.Equal(t, 1, metrics.Concurrency)
	require.Contains(t, tracker.channels, 7)

	active.Done(false)
	now = now.Add(time.Duration(channelRuntimeWindowSeconds) * time.Second)
	tracker.snapshot([]int{7})
	assert.NotContains(t, tracker.channels, 7)
}

func TestChannelRuntimeTrackerReturnsConcurrencyWhenOperationPanics(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := newChannelRuntimeTracker(func() time.Time { return now })

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		tracker.track(7, func() bool {
			metrics := tracker.snapshot([]int{7})[7]
			assert.Equal(t, 1, metrics.Concurrency)
			panic("relay panic")
		})
	}()

	require.Equal(t, "relay panic", recovered)
	metrics := tracker.snapshot([]int{7})[7]
	assert.Zero(t, metrics.Concurrency)
	assert.Zero(t, metrics.RPM)
}

func TestTrackChannelAttemptUpdatesRuntimeAndPrometheusFromOneLifecycle(t *testing.T) {
	runtime, err := prometheusmetrics.NewRuntime(
		prometheusmetrics.Config{Enabled: true, AllowPublic: true},
		"v-test",
		nil,
		nil,
	)
	require.NoError(t, err)
	prometheusmetrics.SetDefaultRuntime(runtime)
	t.Cleanup(func() { prometheusmetrics.SetDefaultRuntime(nil) })

	const channelID = 920_000_001
	TrackChannelAttempt(ChannelAttemptMeta{
		ChannelID:   channelID,
		ChannelType: 8,
		Stream:      true,
		RetryIndex:  1,
		RetryReason: prometheusmetrics.ErrorTypeTimeout,
	}, func() ChannelAttemptOutcome {
		assert.Equal(t, 1, GetChannelRuntimeMetrics([]int{channelID})[channelID].Concurrency)
		return ChannelAttemptOutcome{Error: prometheusmetrics.ErrorDetails{StatusCode: http.StatusTooManyRequests}}
	})

	assert.Equal(t, channelRuntimeMetrics{}, GetChannelRuntimeMetrics([]int{channelID})[channelID])
	output := scrapePrometheusRuntime(t, runtime)
	assert.Contains(t, output, "newapi_channel_attempts_total{channel_id=\"920000001\",channel_type=\"8\",error_type=\"rate_limit\",result=\"failure\",stream=\"true\"} 1")
	assert.Contains(t, output, "newapi_channel_retries_total{channel_id=\"920000001\",channel_type=\"8\",reason=\"timeout\"} 1")
	assert.Contains(t, output, "newapi_channel_inflight{channel_id=\"920000001\",channel_type=\"8\"} 0")
}

func TestTrackChannelAttemptExposesFirstTokenObserver(t *testing.T) {
	runtime, err := prometheusmetrics.NewRuntime(
		prometheusmetrics.Config{Enabled: true, AllowPublic: true},
		"v-test",
		nil,
		nil,
	)
	require.NoError(t, err)
	prometheusmetrics.SetDefaultRuntime(runtime)
	t.Cleanup(func() { prometheusmetrics.SetDefaultRuntime(nil) })

	called := false
	TrackChannelAttemptWithFirstTokenObserver(ChannelAttemptMeta{
		ChannelID:   920_000_005,
		ChannelType: 8,
		Stream:      true,
	}, func(markFirstToken func()) ChannelAttemptOutcome {
		require.NotNil(t, markFirstToken)
		markFirstToken()
		called = true
		return ChannelAttemptOutcome{Success: true}
	})

	assert.True(t, called)
}

func TestRecordChannelTokenMetricsUsesNormalizedUsageFields(t *testing.T) {
	runtime, err := prometheusmetrics.NewRuntime(
		prometheusmetrics.Config{Enabled: true, AllowPublic: true},
		"v-test",
		nil,
		nil,
	)
	require.NoError(t, err)
	prometheusmetrics.SetDefaultRuntime(runtime)
	t.Cleanup(func() { prometheusmetrics.SetDefaultRuntime(nil) })

	recordChannelTokenMetrics(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelId:   920_000_003,
		ChannelType: 6,
	}}, &dto.Usage{
		InputTokens:  120,
		OutputTokens: 30,
		InputTokensDetails: &dto.InputTokenDetails{
			CachedTokens: 45,
		},
	})

	output := scrapePrometheusRuntime(t, runtime)
	assert.Contains(t, output, `newapi_channel_tokens_total{channel_id="920000003",channel_type="6",token_type="input"} 120`)
	assert.Contains(t, output, `newapi_channel_tokens_total{channel_id="920000003",channel_type="6",token_type="output"} 30`)
	assert.Contains(t, output, `newapi_channel_tokens_total{channel_id="920000003",channel_type="6",token_type="cache_read"} 45`)
}

func TestRecordChannelTokenMetricsFallsBackToPromptUsage(t *testing.T) {
	runtime, err := prometheusmetrics.NewRuntime(
		prometheusmetrics.Config{Enabled: true, AllowPublic: true},
		"v-test",
		nil,
		nil,
	)
	require.NoError(t, err)
	prometheusmetrics.SetDefaultRuntime(runtime)
	t.Cleanup(func() { prometheusmetrics.SetDefaultRuntime(nil) })

	recordChannelTokenMetrics(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelId:   920_000_004,
		ChannelType: 7,
	}}, &dto.Usage{
		PromptTokens:         80,
		CompletionTokens:     20,
		PromptCacheHitTokens: 15,
	})

	output := scrapePrometheusRuntime(t, runtime)
	assert.Contains(t, output, `newapi_channel_tokens_total{channel_id="920000004",channel_type="7",token_type="input"} 80`)
	assert.Contains(t, output, `newapi_channel_tokens_total{channel_id="920000004",channel_type="7",token_type="output"} 20`)
	assert.Contains(t, output, `newapi_channel_tokens_total{channel_id="920000004",channel_type="7",token_type="cache_read"} 15`)
}

func TestTrackChannelAttemptReturnsBothInflightCountersAfterPanic(t *testing.T) {
	runtime, err := prometheusmetrics.NewRuntime(
		prometheusmetrics.Config{Enabled: true, AllowPublic: true},
		"v-test",
		nil,
		nil,
	)
	require.NoError(t, err)
	prometheusmetrics.SetDefaultRuntime(runtime)
	t.Cleanup(func() { prometheusmetrics.SetDefaultRuntime(nil) })

	const channelID = 920_000_002
	assert.PanicsWithValue(t, "relay panic", func() {
		TrackChannelAttempt(ChannelAttemptMeta{
			ChannelID:   channelID,
			ChannelType: 9,
		}, func() ChannelAttemptOutcome {
			panic("relay panic")
		})
	})

	assert.Zero(t, GetChannelRuntimeMetrics([]int{channelID})[channelID].Concurrency)
	output := scrapePrometheusRuntime(t, runtime)
	assert.Contains(t, output, "newapi_channel_inflight{channel_id=\"920000002\",channel_type=\"9\"} 0")
	assert.Contains(t, output, "newapi_channel_attempts_total{channel_id=\"920000002\",channel_type=\"9\",error_type=\"internal\",result=\"failure\",stream=\"false\"} 1")
}

func scrapePrometheusRuntime(t *testing.T, runtime *prometheusmetrics.Runtime) string {
	t.Helper()
	response := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	return response.Body.String()
}
