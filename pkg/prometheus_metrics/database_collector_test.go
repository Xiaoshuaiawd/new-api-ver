package prometheusmetrics

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestDatabaseCollectorExportsPoolStatsForMainAndLog(t *testing.T) {
	collector := newDatabaseCollector([]databaseStatsSource{
		{
			name: "main",
			stats: func() sql.DBStats {
				return sql.DBStats{
					MaxOpenConnections: 20,
					OpenConnections:    7,
					InUse:              3,
					Idle:               4,
					WaitCount:          5,
					WaitDuration:       1500 * time.Millisecond,
					MaxIdleClosed:      6,
					MaxIdleTimeClosed:  7,
					MaxLifetimeClosed:  8,
				}
			},
		},
		{
			name: "log",
			stats: func() sql.DBStats {
				return sql.DBStats{MaxOpenConnections: 10, OpenConnections: 2, InUse: 1, Idle: 1}
			},
		},
	})
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	expected := strings.Join([]string{
		"# HELP newapi_db_connections Current database connections by state.",
		"# TYPE newapi_db_connections gauge",
		"newapi_db_connections{database=\"log\",state=\"idle\"} 1",
		"newapi_db_connections{database=\"log\",state=\"in_use\"} 1",
		"newapi_db_connections{database=\"log\",state=\"open\"} 2",
		"newapi_db_connections{database=\"main\",state=\"idle\"} 4",
		"newapi_db_connections{database=\"main\",state=\"in_use\"} 3",
		"newapi_db_connections{database=\"main\",state=\"open\"} 7",
		"# HELP newapi_db_max_idle_closed_total Total connections closed because of the idle connection limit.",
		"# TYPE newapi_db_max_idle_closed_total counter",
		"newapi_db_max_idle_closed_total{database=\"log\"} 0",
		"newapi_db_max_idle_closed_total{database=\"main\"} 6",
		"# HELP newapi_db_max_idle_time_closed_total Total connections closed because of the idle time limit.",
		"# TYPE newapi_db_max_idle_time_closed_total counter",
		"newapi_db_max_idle_time_closed_total{database=\"log\"} 0",
		"newapi_db_max_idle_time_closed_total{database=\"main\"} 7",
		"# HELP newapi_db_max_lifetime_closed_total Total connections closed because of the lifetime limit.",
		"# TYPE newapi_db_max_lifetime_closed_total counter",
		"newapi_db_max_lifetime_closed_total{database=\"log\"} 0",
		"newapi_db_max_lifetime_closed_total{database=\"main\"} 8",
		"# HELP newapi_db_max_open_connections Maximum open database connections. Zero means unlimited.",
		"# TYPE newapi_db_max_open_connections gauge",
		"newapi_db_max_open_connections{database=\"log\"} 10",
		"newapi_db_max_open_connections{database=\"main\"} 20",
		"# HELP newapi_db_wait_duration_seconds_total Total time blocked waiting for a database connection.",
		"# TYPE newapi_db_wait_duration_seconds_total counter",
		"newapi_db_wait_duration_seconds_total{database=\"log\"} 0",
		"newapi_db_wait_duration_seconds_total{database=\"main\"} 1.5",
		"# HELP newapi_db_wait_total Total waits for a database connection.",
		"# TYPE newapi_db_wait_total counter",
		"newapi_db_wait_total{database=\"log\"} 0",
		"newapi_db_wait_total{database=\"main\"} 5",
		"",
	}, "\n")
	require.NoError(t, testutil.GatherAndCompare(
		registry,
		strings.NewReader(expected),
		"newapi_db_connections",
		"newapi_db_max_open_connections",
		"newapi_db_wait_total",
		"newapi_db_wait_duration_seconds_total",
		"newapi_db_max_idle_closed_total",
		"newapi_db_max_idle_time_closed_total",
		"newapi_db_max_lifetime_closed_total",
	))
}
