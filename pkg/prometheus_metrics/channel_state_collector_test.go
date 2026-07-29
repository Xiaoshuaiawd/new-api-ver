package prometheusmetrics

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelStateCollectorExportsEnabledStateAndHealth(t *testing.T) {
	collectorErrors := newCollectorErrorCounter(t)
	collector := newChannelStateCollector(
		func() ([]ChannelState, error) {
			return []ChannelState{
				{ID: 7, Type: 1, Status: common.ChannelStatusEnabled},
				{ID: 9, Type: 14, Status: common.ChannelStatusManuallyDisabled},
			}, nil
		},
		collectorErrors,
		time.Now,
		func(string) {},
	)
	registry := prometheus.NewRegistry()
	registry.MustRegister(collectorErrors, collector)

	expected := strings.NewReader(`# HELP newapi_channel_enabled Whether the channel is enabled in the shared database.
# TYPE newapi_channel_enabled gauge
newapi_channel_enabled{channel_id="7",channel_type="1"} 1
newapi_channel_enabled{channel_id="9",channel_type="14"} 0
# HELP newapi_shared_collector_up Whether a shared database collector completed successfully.
# TYPE newapi_shared_collector_up gauge
newapi_shared_collector_up{collector="channel_state"} 1
`)
	require.NoError(t, testutil.GatherAndCompare(
		registry,
		expected,
		"newapi_channel_enabled",
		"newapi_shared_collector_up",
	))
}

func TestChannelStateCollectorTreatsEmptyChannelTableAsHealthy(t *testing.T) {
	collectorErrors := newCollectorErrorCounter(t)
	collector := newChannelStateCollector(
		func() ([]ChannelState, error) { return nil, nil },
		collectorErrors,
		time.Now,
		func(string) {},
	)
	registry := prometheus.NewRegistry()
	registry.MustRegister(collectorErrors, collector)

	expected := strings.NewReader(`# HELP newapi_shared_collector_up Whether a shared database collector completed successfully.
# TYPE newapi_shared_collector_up gauge
newapi_shared_collector_up{collector="channel_state"} 1
`)
	require.NoError(t, testutil.GatherAndCompare(
		registry,
		expected,
		"newapi_channel_enabled",
		"newapi_shared_collector_up",
	))
}

func TestChannelStateCollectorReportsQueryFailuresAndRateLimitsLogs(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	logs := make([]string, 0, 2)
	collectorErrors := newCollectorErrorCounter(t)
	collector := newChannelStateCollector(
		func() ([]ChannelState, error) { return nil, errors.New("database unavailable") },
		collectorErrors,
		func() time.Time { return now },
		func(message string) { logs = append(logs, message) },
	)
	registry := prometheus.NewRegistry()
	registry.MustRegister(collectorErrors, collector)

	for range 2 {
		_, err := registry.Gather()
		require.NoError(t, err)
	}
	assert.Len(t, logs, 1)
	assert.Contains(t, logs[0], "database unavailable")
	assert.Equal(t, float64(2), testutil.ToFloat64(collectorErrors.WithLabelValues("channel_state")))

	now = now.Add(channelStateErrorLogInterval)
	metrics, err := registry.Gather()
	require.NoError(t, err)
	assert.Len(t, logs, 2)
	assert.Equal(t, float64(3), testutil.ToFloat64(collectorErrors.WithLabelValues("channel_state")))
	assertMetricGaugeValue(t, metrics, "newapi_shared_collector_up", map[string]string{"collector": "channel_state"}, 0)
}

func TestRuntimeRegistersChannelStateCollectorOnlyOnMaster(t *testing.T) {
	previousMaster := common.IsMasterNode
	t.Cleanup(func() { common.IsMasterNode = previousMaster })

	t.Run("master", func(t *testing.T) {
		common.IsMasterNode = true
		runtime, err := NewRuntime(
			Config{Enabled: true, AllowPublic: true},
			"v-test",
			nil,
			nil,
			WithChannelStateSource(func() ([]ChannelState, error) {
				return []ChannelState{{ID: 11, Type: 2, Status: common.ChannelStatusEnabled}}, nil
			}),
		)
		require.NoError(t, err)

		output := scrapeMetrics(t, runtime)
		assert.Contains(t, output, "newapi_channel_enabled{channel_id=\"11\",channel_type=\"2\"} 1")
		assert.Contains(t, output, "newapi_shared_collector_up{collector=\"channel_state\"} 1")
	})

	t.Run("slave", func(t *testing.T) {
		common.IsMasterNode = false
		calls := 0
		runtime, err := NewRuntime(
			Config{Enabled: true, AllowPublic: true},
			"v-test",
			nil,
			nil,
			WithChannelStateSource(func() ([]ChannelState, error) {
				calls++
				return []ChannelState{{ID: 12, Type: 3, Status: common.ChannelStatusEnabled}}, nil
			}),
		)
		require.NoError(t, err)

		output := scrapeMetrics(t, runtime)
		assert.Zero(t, calls)
		assert.NotContains(t, output, "newapi_channel_enabled")
		assert.NotContains(t, output, "newapi_shared_collector_up")
	})
}

func newCollectorErrorCounter(t *testing.T) *prometheus.CounterVec {
	t.Helper()
	return prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "newapi_collector_errors_total",
		Help: "Total Prometheus collection and gather errors.",
	}, []string{"collector"})
}

func assertMetricGaugeValue(
	t *testing.T,
	families []*dto.MetricFamily,
	metricName string,
	wantLabels map[string]string,
	wantValue float64,
) {
	t.Helper()
	for _, family := range families {
		if family.GetName() != metricName {
			continue
		}
		for _, metric := range family.Metric {
			labels := make(map[string]string, len(metric.Label))
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			if assert.ObjectsAreEqual(wantLabels, labels) {
				assert.Equal(t, wantValue, metric.GetGauge().GetValue())
				return
			}
		}
	}
	t.Fatalf("metric %s with labels %v not found", metricName, wantLabels)
}
