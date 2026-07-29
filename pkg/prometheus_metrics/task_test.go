package prometheusmetrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	clientmodel "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskMetricsNormalizePlatformsResultsAndTimestampUnits(t *testing.T) {
	runtime, err := NewRuntime(Config{Enabled: true, AllowPublic: true}, "v-test", nil, nil)
	require.NoError(t, err)
	SetDefaultRuntime(runtime)
	t.Cleanup(func() { SetDefaultRuntime(nil) })

	RecordTaskSubmission("suno", true)
	RecordTaskSubmission("mj", false)
	RecordTaskSubmission("15", true)
	RecordTaskSubmission("dynamic-provider", false)
	RecordTaskPoll("suno", true)
	RecordTaskPoll("15", false)
	RecordTaskCompletion("15", true, 100, 160, TaskTimestampSeconds)
	RecordTaskCompletion("mj", false, 1000, 3500, TaskTimestampMilliseconds)

	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.task.submissions.WithLabelValues("suno", "success")))
	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.task.submissions.WithLabelValues("midjourney", "failure")))
	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.task.submissions.WithLabelValues("video", "success")))
	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.task.submissions.WithLabelValues("other", "failure")))
	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.task.polls.WithLabelValues("suno", "success")))
	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.task.polls.WithLabelValues("video", "error")))
	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.task.completions.WithLabelValues("video", "success")))
	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.task.completions.WithLabelValues("midjourney", "failure")))
	assert.Equal(t, uint64(1), taskHistogramCount(t, runtime.task.duration.WithLabelValues("video", "success")))
	assert.Equal(t, uint64(1), taskHistogramCount(t, runtime.task.duration.WithLabelValues("midjourney", "failure")))
}

func TestTaskCompletionSkipsInvalidDurationButKeepsCompletion(t *testing.T) {
	runtime, err := NewRuntime(Config{Enabled: true, AllowPublic: true}, "v-test", nil, nil)
	require.NoError(t, err)
	SetDefaultRuntime(runtime)
	t.Cleanup(func() { SetDefaultRuntime(nil) })

	RecordTaskCompletion("suno", true, 0, 10, TaskTimestampSeconds)
	RecordTaskCompletion("suno", false, 20, 10, TaskTimestampSeconds)
	RecordTaskCompletion("suno", true, 10, 20, TaskTimestampUnit("unknown"))

	assert.Equal(t, float64(2), testutil.ToFloat64(runtime.task.completions.WithLabelValues("suno", "success")))
	assert.Equal(t, float64(1), testutil.ToFloat64(runtime.task.completions.WithLabelValues("suno", "failure")))
	assert.Zero(t, taskHistogramCount(t, runtime.task.duration.WithLabelValues("suno", "success")))
	assert.Zero(t, taskHistogramCount(t, runtime.task.duration.WithLabelValues("suno", "failure")))
}

func taskHistogramCount(t *testing.T, observer prometheus.Observer) uint64 {
	t.Helper()
	metric, ok := observer.(prometheus.Metric)
	require.True(t, ok)
	output := &clientmodel.Metric{}
	require.NoError(t, metric.Write(output))
	return output.GetHistogram().GetSampleCount()
}
