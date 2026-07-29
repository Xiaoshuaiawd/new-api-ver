package prometheusmetrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordRateLimitRejectionNormalizesLabels(t *testing.T) {
	runtime, err := NewRuntime(Config{Enabled: true, AllowPublic: true}, "v-test", nil, nil)
	require.NoError(t, err)
	SetDefaultRuntime(runtime)
	t.Cleanup(func() { SetDefaultRuntime(nil) })

	RecordRateLimitRejection("user", "successful_request_count")
	RecordRateLimitRejection("user-42", "raw backend error")

	assert.Equal(t, float64(1), testutil.ToFloat64(
		runtime.rateLimit.rejections.WithLabelValues("user", "successful_request_count"),
	))
	assert.Equal(t, float64(1), testutil.ToFloat64(
		runtime.rateLimit.rejections.WithLabelValues("global", "other"),
	))
}
