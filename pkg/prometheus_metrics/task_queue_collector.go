package prometheusmetrics

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	TaskQueueSourceTasks       = "tasks"
	TaskQueueSourceMidjourneys = "midjourneys"
)

type TaskQueueRecord struct {
	Source   string
	Platform string
	Status   string
	Count    int64
}

type TaskQueueSource func() ([]TaskQueueRecord, error)

type taskQueueCollector struct {
	source          TaskQueueSource
	collectorErrors *prometheus.CounterVec
	logError        func(string)
	queueSize       *prometheus.Desc
	up              *prometheus.Desc

	logMu        sync.Mutex
	lastErrorLog time.Time
}

func newTaskQueueCollector(source TaskQueueSource, collectorErrors *prometheus.CounterVec, logError func(string)) prometheus.Collector {
	return &taskQueueCollector{
		source:          source,
		collectorErrors: collectorErrors,
		logError:        logError,
		queueSize: prometheus.NewDesc(
			"newapi_task_queue_size",
			"Current unfinished asynchronous tasks by normalized platform and queue state.",
			[]string{"platform", "state"},
			nil,
		),
		up: prometheus.NewDesc(
			"newapi_shared_collector_up",
			"Whether a shared database collector completed successfully.",
			[]string{"collector"},
			nil,
		),
	}
}

func (c *taskQueueCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.queueSize
	ch <- c.up
}

func (c *taskQueueCollector) Collect(ch chan<- prometheus.Metric) {
	records, err := c.source()
	if err != nil {
		if c.collectorErrors != nil {
			c.collectorErrors.WithLabelValues("task_queue").Inc()
		}
		c.logCollectionError(err)
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0, "task_queue")
		return
	}

	counts := make(map[string]map[string]int64, 4)
	for _, platform := range []string{"midjourney", "suno", "video", "other"} {
		counts[platform] = map[string]int64{"waiting": 0, "running": 0, "unknown": 0}
	}
	for _, record := range records {
		if record.Count <= 0 {
			continue
		}
		platform := normalizeTaskPlatform(record.Platform)
		if record.Source == TaskQueueSourceMidjourneys {
			platform = "midjourney"
		}
		state := normalizeTaskQueueState(record.Source, record.Status)
		counts[platform][state] += record.Count
	}

	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1, "task_queue")
	for _, platform := range []string{"midjourney", "other", "suno", "video"} {
		for _, state := range []string{"running", "unknown", "waiting"} {
			ch <- prometheus.MustNewConstMetric(c.queueSize, prometheus.GaugeValue, float64(counts[platform][state]), platform, state)
		}
	}
}

func normalizeTaskQueueState(source, status string) string {
	switch status {
	case "IN_PROGRESS":
		return "running"
	case "SUBMITTED", "QUEUED":
		return "waiting"
	case "NOT_START":
		if source == TaskQueueSourceTasks {
			return "waiting"
		}
	case "":
		if source == TaskQueueSourceMidjourneys {
			return "waiting"
		}
	}
	return "unknown"
}

func (c *taskQueueCollector) logCollectionError(err error) {
	if c.logError == nil {
		return
	}
	now := time.Now()
	c.logMu.Lock()
	if !c.lastErrorLog.IsZero() && now.Sub(c.lastErrorLog) < time.Minute {
		c.logMu.Unlock()
		return
	}
	c.lastErrorLog = now
	c.logMu.Unlock()
	c.logError(fmt.Sprintf("prometheus task queue collector error: %v", err))
}
