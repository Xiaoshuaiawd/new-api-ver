package controller

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTTFTTimeoutErrorClassifiesRetrySafety(t *testing.T) {
	for _, tt := range []struct {
		name      string
		endReason relaycommon.StreamEndReason
		retrySafe bool
		wantError bool
		wantRetry bool
	}{
		{
			name:      "safe timeout retries",
			endReason: relaycommon.StreamEndReasonTTFTTimeout,
			retrySafe: true,
			wantError: true,
			wantRetry: true,
		},
		{
			name:      "unsafe timeout remains a failure without retry",
			endReason: relaycommon.StreamEndReasonTTFTTimeout,
			retrySafe: false,
			wantError: true,
			wantRetry: false,
		},
		{
			name:      "normal stream is not a timeout",
			endReason: relaycommon.StreamEndReasonDone,
			wantError: false,
			wantRetry: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				StreamStatus:  &relaycommon.StreamStatus{EndReason: tt.endReason},
				TTFTRetrySafe: tt.retrySafe,
			}

			err, retry := ttftTimeoutError(info)
			assert.Equal(t, tt.wantRetry, retry)
			if tt.wantError {
				require.NotNil(t, err)
				assert.True(t, types.IsChannelError(err))
			} else {
				assert.Nil(t, err)
			}
		})
	}
}
