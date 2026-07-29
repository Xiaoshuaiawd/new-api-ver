package common

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoFinalSuccess(t *testing.T) {
	tests := []struct {
		name           string
		handlerSuccess bool
		nilInfo        bool
		isStream       bool
		hasStatus      bool
		endReason      StreamEndReason
		endError       error
		softError      bool
		want           bool
	}{
		{name: "handler failure", handlerSuccess: false, want: false},
		{name: "nil relay info keeps handler success", handlerSuccess: true, nilInfo: true, want: true},
		{name: "non stream keeps handler success", handlerSuccess: true, want: true},
		{name: "stream without status keeps handler success", handlerSuccess: true, isStream: true, want: true},
		{name: "done", handlerSuccess: true, isStream: true, hasStatus: true, endReason: StreamEndReasonDone, want: true},
		{name: "clean eof", handlerSuccess: true, isStream: true, hasStatus: true, endReason: StreamEndReasonEOF, want: true},
		{name: "handler stop", handlerSuccess: true, isStream: true, hasStatus: true, endReason: StreamEndReasonHandlerStop, want: true},
		{name: "timeout", handlerSuccess: true, isStream: true, hasStatus: true, endReason: StreamEndReasonTimeout, want: false},
		{name: "client gone", handlerSuccess: true, isStream: true, hasStatus: true, endReason: StreamEndReasonClientGone, want: false},
		{name: "scanner error", handlerSuccess: true, isStream: true, hasStatus: true, endReason: StreamEndReasonScannerErr, want: false},
		{name: "panic", handlerSuccess: true, isStream: true, hasStatus: true, endReason: StreamEndReasonPanic, want: false},
		{name: "ping failure", handlerSuccess: true, isStream: true, hasStatus: true, endReason: StreamEndReasonPingFail, want: false},
		{name: "empty reason", handlerSuccess: true, isStream: true, hasStatus: true, endReason: StreamEndReasonNone, want: false},
		{name: "unknown reason", handlerSuccess: true, isStream: true, hasStatus: true, endReason: StreamEndReason("future_reason"), want: false},
		{name: "normal reason with end error", handlerSuccess: true, isStream: true, hasStatus: true, endReason: StreamEndReasonDone, endError: errors.New("write failed"), want: false},
		{name: "normal reason with soft error", handlerSuccess: true, isStream: true, hasStatus: true, endReason: StreamEndReasonEOF, softError: true, want: false},
		{name: "handler failure overrides normal stream", handlerSuccess: false, isStream: true, hasStatus: true, endReason: StreamEndReasonDone, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var info *RelayInfo
			if !test.nilInfo {
				info = &RelayInfo{IsStream: test.isStream}
				if test.hasStatus {
					info.StreamStatus = NewStreamStatus()
					info.StreamStatus.SetEndReason(test.endReason, test.endError)
					if test.softError {
						info.StreamStatus.RecordError("upstream error event")
					}
				}
			}

			assert.Equal(t, test.want, info.FinalSuccess(test.handlerSuccess))
		})
	}
}

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
