package prometheusmetrics

import (
	"database/sql"

	"github.com/prometheus/client_golang/prometheus"
)

type databaseStatsSource struct {
	name  string
	stats func() sql.DBStats
}

type databaseCollector struct {
	sources []databaseStatsSource

	connections       *prometheus.Desc
	maxOpen           *prometheus.Desc
	waitTotal         *prometheus.Desc
	waitDuration      *prometheus.Desc
	maxIdleClosed     *prometheus.Desc
	maxIdleTimeClosed *prometheus.Desc
	maxLifetimeClosed *prometheus.Desc
}

func newDatabaseCollector(sources []databaseStatsSource) prometheus.Collector {
	return &databaseCollector{
		sources: sources,
		connections: prometheus.NewDesc(
			"newapi_db_connections",
			"Current database connections by state.",
			[]string{"database", "state"},
			nil,
		),
		maxOpen: prometheus.NewDesc(
			"newapi_db_max_open_connections",
			"Maximum open database connections. Zero means unlimited.",
			[]string{"database"},
			nil,
		),
		waitTotal: prometheus.NewDesc(
			"newapi_db_wait_total",
			"Total waits for a database connection.",
			[]string{"database"},
			nil,
		),
		waitDuration: prometheus.NewDesc(
			"newapi_db_wait_duration_seconds_total",
			"Total time blocked waiting for a database connection.",
			[]string{"database"},
			nil,
		),
		maxIdleClosed: prometheus.NewDesc(
			"newapi_db_max_idle_closed_total",
			"Total connections closed because of the idle connection limit.",
			[]string{"database"},
			nil,
		),
		maxIdleTimeClosed: prometheus.NewDesc(
			"newapi_db_max_idle_time_closed_total",
			"Total connections closed because of the idle time limit.",
			[]string{"database"},
			nil,
		),
		maxLifetimeClosed: prometheus.NewDesc(
			"newapi_db_max_lifetime_closed_total",
			"Total connections closed because of the lifetime limit.",
			[]string{"database"},
			nil,
		),
	}
}

func (c *databaseCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.connections
	ch <- c.maxOpen
	ch <- c.waitTotal
	ch <- c.waitDuration
	ch <- c.maxIdleClosed
	ch <- c.maxIdleTimeClosed
	ch <- c.maxLifetimeClosed
}

func (c *databaseCollector) Collect(ch chan<- prometheus.Metric) {
	for _, source := range c.sources {
		stats := source.stats()
		ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stats.OpenConnections), source.name, "open")
		ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stats.InUse), source.name, "in_use")
		ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stats.Idle), source.name, "idle")
		ch <- prometheus.MustNewConstMetric(c.maxOpen, prometheus.GaugeValue, float64(stats.MaxOpenConnections), source.name)
		ch <- prometheus.MustNewConstMetric(c.waitTotal, prometheus.CounterValue, float64(stats.WaitCount), source.name)
		ch <- prometheus.MustNewConstMetric(c.waitDuration, prometheus.CounterValue, stats.WaitDuration.Seconds(), source.name)
		ch <- prometheus.MustNewConstMetric(c.maxIdleClosed, prometheus.CounterValue, float64(stats.MaxIdleClosed), source.name)
		ch <- prometheus.MustNewConstMetric(c.maxIdleTimeClosed, prometheus.CounterValue, float64(stats.MaxIdleTimeClosed), source.name)
		ch <- prometheus.MustNewConstMetric(c.maxLifetimeClosed, prometheus.CounterValue, float64(stats.MaxLifetimeClosed), source.name)
	}
}
