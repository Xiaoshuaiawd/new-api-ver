package perfmetrics

import (
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearPerfMetricsHotBuckets() {
	hotBuckets.Range(func(key, _ any) bool {
		hotBuckets.Delete(key)
		return true
	})
}

func TestRecordRelaySampleUsesFinalSuccess(t *testing.T) {
	clearPerfMetricsHotBuckets()
	t.Cleanup(clearPerfMetricsHotBuckets)

	tests := []struct {
		name      string
		hasStatus bool
		endReason relaycommon.StreamEndReason
		wantCount int64
	}{
		{
			name:      "normal stream remains successful",
			hasStatus: true,
			endReason: relaycommon.StreamEndReasonDone,
			wantCount: 1,
		},
		{
			name:      "abnormal stream overrides handler success",
			hasStatus: true,
			endReason: relaycommon.StreamEndReasonTimeout,
			wantCount: 0,
		},
		{
			name:      "missing stream status keeps compatibility success",
			wantCount: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearPerfMetricsHotBuckets()
			var status *relaycommon.StreamStatus
			if test.hasStatus {
				status = relaycommon.NewStreamStatus()
				status.SetEndReason(test.endReason, nil)
			}
			info := &relaycommon.RelayInfo{
				OriginModelName: "perf-final-success-model",
				UsingGroup:      "default",
				IsStream:        true,
				StartTime:       time.Now().Add(-time.Second),
				StreamStatus:    status,
			}

			RecordRelaySample(info, true, 0)

			var snapshot counters
			bucketCount := 0
			hotBuckets.Range(func(_, value any) bool {
				bucketCount++
				snapshot = value.(*atomicBucket).snapshot()
				return true
			})
			require.Equal(t, 1, bucketCount)
			assert.Equal(t, int64(1), snapshot.requestCount)
			assert.Equal(t, test.wantCount, snapshot.successCount)
		})
	}
}
