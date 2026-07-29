package prometheusmetrics

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestNewRuntimeUsesIsolatedRegistryWithRuntimeProcessAndBuildMetrics(t *testing.T) {
	originalCommit, originalBuildTime := GitCommit, BuildTime
	GitCommit, BuildTime = "abc123", "2026-07-28T12:00:00Z"
	t.Cleanup(func() {
		GitCommit, BuildTime = originalCommit, originalBuildTime
	})

	defaultOnly := prometheus.NewGauge(prometheus.GaugeOpts{Name: "default_registry_only_metric"})
	require.NoError(t, prometheus.Register(defaultOnly))
	t.Cleanup(func() { prometheus.Unregister(defaultOnly) })

	runtime, err := NewRuntime(Config{Enabled: true, AllowPublic: true}, "v-test", nil, nil)
	require.NoError(t, err)
	require.True(t, runtime.Enabled)
	require.NotNil(t, runtime.Handler)

	response := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	body, err := io.ReadAll(response.Result().Body)
	require.NoError(t, err)
	output := string(body)

	assert.Contains(t, output, "go_goroutines")
	assert.Contains(t, output, "process_start_time_seconds")
	assert.Contains(t, output, "newapi_build_info{build_time=\"2026-07-28T12:00:00Z\",commit=\"abc123\",version=\"v-test\"} 1")
	assert.NotContains(t, output, "default_registry_only_metric")
}

func TestNewRuntimeCanBeConstructedMoreThanOnce(t *testing.T) {
	cfg := Config{Enabled: true, AllowPublic: true}
	first, err := NewRuntime(cfg, "v-test", nil, nil)
	require.NoError(t, err)
	second, err := NewRuntime(cfg, "v-test", nil, nil)
	require.NoError(t, err)

	assert.NotSame(t, first.registry, second.registry)
}

func TestNewRuntimeDisabledDoesNotCreateMetricsHandler(t *testing.T) {
	runtime, err := NewRuntime(Config{}, "v-test", nil, nil)
	require.NoError(t, err)

	assert.False(t, runtime.Enabled)
	assert.Nil(t, runtime.Handler)
	assert.Nil(t, runtime.registry)
}

func TestNewRuntimeDeduplicatesSharedMainAndLogDatabase(t *testing.T) {
	db := openMetricsTestDatabase(t)
	runtime, err := NewRuntime(Config{Enabled: true, AllowPublic: true}, "v-test", db, db)
	require.NoError(t, err)

	response := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)

	output := response.Body.String()
	assert.Contains(t, output, "newapi_db_connections{database=\"main\",state=\"open\"}")
	assert.NotContains(t, output, "database=\"log\"")
}

func TestResolveBuildInfoFallsBackToUnknown(t *testing.T) {
	originalCommit, originalBuildTime := GitCommit, BuildTime
	GitCommit, BuildTime = "", ""
	t.Cleanup(func() {
		GitCommit, BuildTime = originalCommit, originalBuildTime
	})

	info := resolveBuildInfo("v-test", func() (map[string]string, bool) {
		return nil, false
	})

	assert.Equal(t, buildInfo{Version: "v-test", Commit: "unknown", BuildTime: "unknown"}, info)
}

func TestResolveBuildInfoReadsVCSSettings(t *testing.T) {
	originalCommit, originalBuildTime := GitCommit, BuildTime
	GitCommit, BuildTime = "", ""
	t.Cleanup(func() {
		GitCommit, BuildTime = originalCommit, originalBuildTime
	})

	info := resolveBuildInfo("v-test", func() (map[string]string, bool) {
		return map[string]string{
			"vcs.revision": "revision-from-build-info",
			"vcs.time":     "2026-07-28T11:00:00Z",
		}, true
	})

	assert.Equal(t, buildInfo{
		Version:   "v-test",
		Commit:    "revision-from-build-info",
		BuildTime: "2026-07-28T11:00:00Z",
	}, info)
}

func TestMetricsHandlerUsesPrometheusContentType(t *testing.T) {
	runtime, err := NewRuntime(Config{Enabled: true, AllowPublic: true}, "v-test", nil, nil)
	require.NoError(t, err)

	response := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	assert.Contains(t, response.Header().Get("Content-Type"), "text/plain")
}

func TestMetricsHandlerCountsGatherErrors(t *testing.T) {
	runtime, err := NewRuntime(Config{Enabled: true, AllowPublic: true}, "v-test", nil, nil)
	require.NoError(t, err)
	runtime.registry.MustRegister(failingCollector{
		desc: prometheus.NewDesc("broken_test_metric", "A collector that always fails.", nil, nil),
	})

	response := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	assert.GreaterOrEqual(t, readCounterValue(t, runtime, "newapi_collector_errors_total", "gather"), float64(1))
}

func openMetricsTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db, err := gormDB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

type failingCollector struct {
	desc *prometheus.Desc
}

func (c failingCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

func (c failingCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.NewInvalidMetric(c.desc, errors.New("expected gather failure"))
}

func readCounterValue(t *testing.T, runtime *Runtime, metricName, collector string) float64 {
	t.Helper()
	families, err := runtime.registry.Gather()
	require.Error(t, err)
	for _, family := range families {
		if family.GetName() != metricName {
			continue
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "collector" && label.GetValue() == collector {
					return metric.GetCounter().GetValue()
				}
			}
		}
	}
	t.Fatalf("metric %s with collector=%s not found", metricName, collector)
	return 0
}
