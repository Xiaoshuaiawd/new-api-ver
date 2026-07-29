package prometheusmetrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelAttemptRecordsRetryInflightFailureAndDurationOnce(t *testing.T) {
	runtime := activateMetricsTestRuntime(t)
	attempt := BeginChannelAttempt(ChannelAttemptMeta{
		ChannelID:   7,
		ChannelType: 1,
		Stream:      true,
		RetryIndex:  1,
		RetryReason: ErrorTypeTimeout,
	})

	active := scrapeMetrics(t, runtime)
	assert.Contains(t, active, "newapi_channel_inflight{channel_id=\"7\",channel_type=\"1\"} 1")
	assert.Contains(t, active, "newapi_channel_retries_total{channel_id=\"7\",channel_type=\"1\",reason=\"timeout\"} 1")

	attempt.Done(ChannelAttemptOutcome{Success: false, Error: ErrorDetails{StatusCode: http.StatusTooManyRequests}})
	attempt.Done(ChannelAttemptOutcome{Success: true})

	finished := scrapeMetrics(t, runtime)
	assert.Contains(t, finished, "newapi_channel_inflight{channel_id=\"7\",channel_type=\"1\"} 0")
	assert.Contains(t, finished, "newapi_channel_attempts_total{channel_id=\"7\",channel_type=\"1\",error_type=\"rate_limit\",result=\"failure\",stream=\"true\"} 1")
	assert.Contains(t, finished, "newapi_channel_duration_seconds_count{channel_id=\"7\",channel_type=\"1\",result=\"failure\",stream=\"true\"} 1")
}

func TestChannelAttemptSuccessAlwaysUsesNoneErrorType(t *testing.T) {
	runtime := activateMetricsTestRuntime(t)
	attempt := BeginChannelAttempt(ChannelAttemptMeta{ChannelID: 8, ChannelType: 2})
	attempt.Done(ChannelAttemptOutcome{Success: true, Error: ErrorDetails{StatusCode: http.StatusInternalServerError}})

	metrics := scrapeMetrics(t, runtime)
	assert.Contains(t, metrics, "newapi_channel_attempts_total{channel_id=\"8\",channel_type=\"2\",error_type=\"none\",result=\"success\",stream=\"false\"} 1")
}

func TestChannelAttemptFailureWithoutDetailsUsesInternal(t *testing.T) {
	runtime := activateMetricsTestRuntime(t)
	attempt := BeginChannelAttempt(ChannelAttemptMeta{ChannelID: 9, ChannelType: 3})
	attempt.Done(ChannelAttemptOutcome{})

	metrics := scrapeMetrics(t, runtime)
	assert.Contains(t, metrics, "newapi_channel_attempts_total{channel_id=\"9\",channel_type=\"3\",error_type=\"internal\",result=\"failure\",stream=\"false\"} 1")
}

func TestChannelAttemptWithoutActiveRuntimeIsNoop(t *testing.T) {
	SetDefaultRuntime(nil)
	attempt := BeginChannelAttempt(ChannelAttemptMeta{ChannelID: 7, ChannelType: 1})
	require.NotNil(t, attempt)
	assert.NotPanics(t, func() { attempt.Done(ChannelAttemptOutcome{}) })
}

func TestChannelDurationHistogramCanBeDisabled(t *testing.T) {
	runtime, err := NewRuntime(Config{
		Enabled:                 true,
		AllowPublic:             true,
		DisableChannelHistogram: true,
	}, "v-test", nil, nil)
	require.NoError(t, err)
	SetDefaultRuntime(runtime)
	t.Cleanup(func() { SetDefaultRuntime(nil) })

	attempt := BeginChannelAttempt(ChannelAttemptMeta{ChannelID: 10, ChannelType: 4})
	attempt.Done(ChannelAttemptOutcome{Success: true})

	metrics := scrapeMetrics(t, runtime)
	assert.Contains(t, metrics, "newapi_channel_attempts_total{channel_id=\"10\",channel_type=\"4\",error_type=\"none\",result=\"success\",stream=\"false\"} 1")
	assert.NotContains(t, metrics, "newapi_channel_duration_seconds")
	assert.NotContains(t, metrics, "newapi_channel_first_byte_seconds")
	assert.NotContains(t, metrics, "newapi_channel_ttft_seconds")
}

func TestObserveChannelFirstByteRecordsOnlyValidSamples(t *testing.T) {
	runtime := activateMetricsTestRuntime(t)

	ObserveChannelFirstByte(12, 5, 250*time.Millisecond)
	ObserveChannelFirstByte(0, 5, 250*time.Millisecond)
	ObserveChannelFirstByte(12, 5, 0)

	metrics := scrapeMetrics(t, runtime)
	assert.Contains(t, metrics, "newapi_channel_first_byte_seconds_count{channel_id=\"12\",channel_type=\"5\"} 1")
}

func TestChannelAttemptRecordsSuccessfulStreamTTFT(t *testing.T) {
	runtime := activateMetricsTestRuntime(t)
	attempt := BeginChannelAttempt(ChannelAttemptMeta{ChannelID: 12, ChannelType: 5, Stream: true})
	attempt.Done(ChannelAttemptOutcome{Success: true, TTFT: 750 * time.Millisecond})

	metrics := scrapeMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_channel_ttft_seconds_count{channel_id="12",channel_type="5"} 1`)
}

func TestChannelAttemptDoesNotRecordTTFTForFailureOrNonStream(t *testing.T) {
	runtime := activateMetricsTestRuntime(t)

	failed := BeginChannelAttempt(ChannelAttemptMeta{ChannelID: 12, ChannelType: 5, Stream: true})
	failed.Done(ChannelAttemptOutcome{TTFT: 250 * time.Millisecond})
	nonStream := BeginChannelAttempt(ChannelAttemptMeta{ChannelID: 12, ChannelType: 5})
	nonStream.Done(ChannelAttemptOutcome{Success: true, TTFT: 250 * time.Millisecond})

	metrics := scrapeMetrics(t, runtime)
	assert.NotContains(t, metrics, `newapi_channel_ttft_seconds_count{channel_id="12",channel_type="5"}`)
}

func TestRecordChannelTokensUsesFixedTokenTypesAndIgnoresInvalidValues(t *testing.T) {
	runtime := activateMetricsTestRuntime(t)
	RecordChannelTokens(12, 5, ChannelTokenUsage{Input: 100, Output: 40, CacheRead: 25})
	RecordChannelTokens(0, 5, ChannelTokenUsage{Input: 999})
	RecordChannelTokens(12, 5, ChannelTokenUsage{Input: -1, Output: -1, CacheRead: -1})

	metrics := scrapeMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_channel_tokens_total{channel_id="12",channel_type="5",token_type="input"} 100`)
	assert.Contains(t, metrics, `newapi_channel_tokens_total{channel_id="12",channel_type="5",token_type="output"} 40`)
	assert.Contains(t, metrics, `newapi_channel_tokens_total{channel_id="12",channel_type="5",token_type="cache_read"} 25`)
	assert.NotContains(t, metrics, ` 999`)
}

func activateMetricsTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	runtime, err := NewRuntime(Config{Enabled: true, AllowPublic: true}, "v-test", nil, nil)
	require.NoError(t, err)
	SetDefaultRuntime(runtime)
	t.Cleanup(func() { SetDefaultRuntime(nil) })
	return runtime
}

func scrapeMetrics(t *testing.T, runtime *Runtime) string {
	t.Helper()
	response := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	return response.Body.String()
}
