package prometheusmetrics

import (
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskQueueCollectorMergesSourcesAndExportsKnownZeros(t *testing.T) {
	errorsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_task_queue_errors_total"}, []string{"collector"})
	collector := newTaskQueueCollector(func() ([]TaskQueueRecord, error) {
		return []TaskQueueRecord{
			{Source: TaskQueueSourceTasks, Platform: "suno", Status: "SUBMITTED", Count: 2},
			{Source: TaskQueueSourceTasks, Platform: "15", Status: "IN_PROGRESS", Count: 3},
			{Source: TaskQueueSourceTasks, Platform: "dynamic", Status: "PAUSED", Count: 4},
			{Source: TaskQueueSourceMidjourneys, Status: "", Count: 5},
			{Source: TaskQueueSourceMidjourneys, Status: "IN_PROGRESS", Count: 6},
		}, nil
	}, errorsTotal, nil)
	registry := prometheus.NewRegistry()
	require.NoError(t, registry.Register(collector))

	expected := `
# HELP newapi_shared_collector_up Whether a shared database collector completed successfully.
# TYPE newapi_shared_collector_up gauge
newapi_shared_collector_up{collector="task_queue"} 1
# HELP newapi_task_queue_size Current unfinished asynchronous tasks by normalized platform and queue state.
# TYPE newapi_task_queue_size gauge
newapi_task_queue_size{platform="midjourney",state="running"} 6
newapi_task_queue_size{platform="midjourney",state="unknown"} 0
newapi_task_queue_size{platform="midjourney",state="waiting"} 5
newapi_task_queue_size{platform="other",state="running"} 0
newapi_task_queue_size{platform="other",state="unknown"} 4
newapi_task_queue_size{platform="other",state="waiting"} 0
newapi_task_queue_size{platform="suno",state="running"} 0
newapi_task_queue_size{platform="suno",state="unknown"} 0
newapi_task_queue_size{platform="suno",state="waiting"} 2
newapi_task_queue_size{platform="video",state="running"} 3
newapi_task_queue_size{platform="video",state="unknown"} 0
newapi_task_queue_size{platform="video",state="waiting"} 0
`
	require.NoError(t, testutil.GatherAndCompare(registry, strings.NewReader(expected), "newapi_task_queue_size", "newapi_shared_collector_up"))
}

func TestTaskQueueCollectorReportsFailureWithoutQueueSamples(t *testing.T) {
	errorsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_task_queue_failure_errors_total"}, []string{"collector"})
	collector := newTaskQueueCollector(func() ([]TaskQueueRecord, error) {
		return nil, errors.New("query failed")
	}, errorsTotal, nil)

	expected := `
# HELP newapi_shared_collector_up Whether a shared database collector completed successfully.
# TYPE newapi_shared_collector_up gauge
newapi_shared_collector_up{collector="task_queue"} 0
`
	require.NoError(t, testutil.CollectAndCompare(
		collector,
		strings.NewReader(expected),
		"newapi_task_queue_size",
		"newapi_shared_collector_up",
	))
	assert.Equal(t, float64(1), testutil.ToFloat64(errorsTotal.WithLabelValues("task_queue")))
}

func TestRuntimeRegistersTaskQueueCollectorOnlyOnMaster(t *testing.T) {
	previousMaster := common.IsMasterNode
	t.Cleanup(func() { common.IsMasterNode = previousMaster })

	t.Run("master", func(t *testing.T) {
		common.IsMasterNode = true
		calls := 0
		runtime, err := NewRuntime(
			Config{Enabled: true, AllowPublic: true},
			"v-test",
			nil,
			nil,
			WithTaskQueueSource(func() ([]TaskQueueRecord, error) {
				calls++
				return nil, nil
			}),
		)
		require.NoError(t, err)

		output := scrapeMetrics(t, runtime)
		assert.Equal(t, 1, calls)
		assert.Contains(t, output, "newapi_shared_collector_up{collector=\"task_queue\"} 1")
		assert.Contains(t, output, "newapi_task_queue_size{platform=\"suno\",state=\"waiting\"} 0")
	})

	t.Run("slave", func(t *testing.T) {
		common.IsMasterNode = false
		calls := 0
		runtime, err := NewRuntime(
			Config{Enabled: true, AllowPublic: true},
			"v-test",
			nil,
			nil,
			WithTaskQueueSource(func() ([]TaskQueueRecord, error) {
				calls++
				return nil, nil
			}),
		)
		require.NoError(t, err)

		output := scrapeMetrics(t, runtime)
		assert.Zero(t, calls)
		assert.NotContains(t, output, "newapi_task_queue_size")
		assert.NotContains(t, output, "collector=\"task_queue\"")
	})
}
