package prometheusmetrics

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
)

func TestRelayAttemptRecordsFinalRequestInflightDurationAndTTFT(t *testing.T) {
	runtime := activateMetricsTestRuntime(t)
	attempt := BeginRelayAttempt(types.RelayFormatOpenAI)
	attempt.SetStream(true)

	active := scrapeMetrics(t, runtime)
	assert.Contains(t, active, "newapi_relay_inflight{relay_format=\"openai\",stream=\"true\"} 1")

	attempt.Done(RelayOutcome{
		Success: true,
		TTFT:    250 * time.Millisecond,
	})
	attempt.Done(RelayOutcome{Success: false, Error: ErrorDetails{Err: errors.New("duplicate")}})

	finished := scrapeMetrics(t, runtime)
	assert.Contains(t, finished, "newapi_relay_inflight{relay_format=\"openai\",stream=\"true\"} 0")
	assert.Contains(t, finished, "newapi_relay_requests_total{error_type=\"none\",relay_format=\"openai\",result=\"success\",stream=\"true\"} 1")
	assert.Contains(t, finished, "newapi_relay_duration_seconds_count{relay_format=\"openai\",result=\"success\",stream=\"true\"} 1")
	assert.Contains(t, finished, "newapi_stream_ttft_seconds_count{relay_format=\"openai\"} 1")
	assert.Contains(t, finished, "newapi_stream_duration_seconds_count{relay_format=\"openai\",result=\"success\"} 1")
}

func TestRelayAttemptDefaultsUnknownFormatsAndUnsetStream(t *testing.T) {
	runtime := activateMetricsTestRuntime(t)
	attempt := BeginRelayAttempt(types.RelayFormat("user-supplied-format"))
	attempt.Done(RelayOutcome{Success: false})

	metrics := scrapeMetrics(t, runtime)
	assert.Contains(t, metrics, "newapi_relay_requests_total{error_type=\"internal\",relay_format=\"other\",result=\"failure\",stream=\"false\"} 1")
	assert.Contains(t, metrics, "newapi_relay_inflight{relay_format=\"other\",stream=\"false\"} 0")
}
