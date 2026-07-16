package common

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoBeginRelayAttemptResetsAttemptState(t *testing.T) {
	startedAt := time.Unix(1_700_000_000, 0)
	info := &RelayInfo{StartTime: startedAt, isFirstResponse: true}

	info.BeginRelayAttempt(startedAt)
	info.SetFirstResponseTime()
	info.ReceivedResponseCount = 3
	info.MarkDownstreamSemanticStarted()
	info.MarkAttemptSucceeded()

	info.BeginRelayAttempt(startedAt.Add(time.Second))

	require.True(t, info.isFirstResponse)
	require.Equal(t, 0, info.ReceivedResponseCount)
	require.False(t, info.HasDownstreamSemanticStarted())
	require.False(t, info.IsAttemptSuccessful())
	_, ok := info.FirstResponseDuration()
	require.False(t, ok)
}

func TestRelayInfoKeepsCommittedDownstreamAcrossRetries(t *testing.T) {
	info := &RelayInfo{}
	info.BeginRelayAttempt(time.Now())
	info.MarkDownstreamAckSent()

	info.BeginRelayAttempt(time.Now())

	require.True(t, info.HasDownstreamResponseCommitted())
	require.False(t, info.HasDownstreamSemanticStarted())
}

func TestRelayInfoAttemptTimingMetricsAreAttemptScoped(t *testing.T) {
	info := &RelayInfo{}
	info.BeginRelayAttempt(time.Now().Add(-time.Second))
	info.MarkUpstreamHeadersReceived()
	info.SetFirstResponseTime()
	info.MarkDownstreamAckSent()
	info.MarkDownstreamSemanticStarted()

	metrics := info.AttemptTimingMetrics()
	require.GreaterOrEqual(t, metrics["gateway_processing_ms"], int64(1_000))
	require.Contains(t, metrics, "upstream_headers_ms")
	require.Contains(t, metrics, "upstream_first_event_ms")
	require.Contains(t, metrics, "downstream_first_flush_ms")
	require.Contains(t, metrics, "downstream_first_semantic_chunk_ms")
}
