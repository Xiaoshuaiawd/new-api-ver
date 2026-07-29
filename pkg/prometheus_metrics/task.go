package prometheusmetrics

import (
	"fmt"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

type TaskTimestampUnit string

const (
	TaskTimestampSeconds      TaskTimestampUnit = "seconds"
	TaskTimestampMilliseconds TaskTimestampUnit = "milliseconds"
)

type taskMetrics struct {
	submissions *prometheus.CounterVec
	completions *prometheus.CounterVec
	duration    *prometheus.HistogramVec
	polls       *prometheus.CounterVec
}

func registerTaskMetrics(registry prometheus.Registerer) (*taskMetrics, error) {
	metrics := &taskMetrics{
		submissions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_task_submissions_total",
			Help: "Total asynchronous task submission outcomes after upstream response and local persistence.",
		}, []string{"platform", "result"}),
		completions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_task_completions_total",
			Help: "Total first successful CAS transitions into terminal asynchronous task states.",
		}, []string{"platform", "result"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "newapi_task_duration_seconds",
			Help:    "Asynchronous task duration observed at the first successful terminal CAS transition.",
			Buckets: []float64{30, 60, 300, 900, 1800, 3600, 7200, 14400},
		}, []string{"platform", "result"}),
		polls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_task_poll_total",
			Help: "Total asynchronous upstream task polling requests by final query result.",
		}, []string{"platform", "result"}),
	}
	collectors := []struct {
		name      string
		collector prometheus.Collector
	}{
		{name: "task submissions", collector: metrics.submissions},
		{name: "task completions", collector: metrics.completions},
		{name: "task duration", collector: metrics.duration},
		{name: "task polls", collector: metrics.polls},
	}
	for _, item := range collectors {
		if err := registry.Register(item.collector); err != nil {
			return nil, fmt.Errorf("register %s metric: %w", item.name, err)
		}
	}
	return metrics, nil
}

func RecordTaskSubmission(platform string, success bool) {
	runtime := defaultRuntime.Load()
	if runtime == nil || runtime.task == nil {
		return
	}
	runtime.task.submissions.WithLabelValues(normalizeTaskPlatform(platform), taskTerminalResult(success)).Inc()
}

func RecordTaskPoll(platform string, success bool) {
	runtime := defaultRuntime.Load()
	if runtime == nil || runtime.task == nil {
		return
	}
	result := "error"
	if success {
		result = "success"
	}
	runtime.task.polls.WithLabelValues(normalizeTaskPlatform(platform), result).Inc()
}

func RecordTaskCompletion(platform string, success bool, submitTime, finishTime int64, unit TaskTimestampUnit) {
	runtime := defaultRuntime.Load()
	if runtime == nil || runtime.task == nil {
		return
	}
	normalizedPlatform := normalizeTaskPlatform(platform)
	result := taskTerminalResult(success)
	runtime.task.completions.WithLabelValues(normalizedPlatform, result).Inc()

	if submitTime <= 0 || finishTime <= submitTime {
		return
	}
	delta := finishTime - submitTime
	var seconds float64
	switch unit {
	case TaskTimestampSeconds:
		seconds = float64(delta)
	case TaskTimestampMilliseconds:
		seconds = float64(delta) / 1000
	default:
		return
	}
	if seconds <= 0 {
		return
	}
	runtime.task.duration.WithLabelValues(normalizedPlatform, result).Observe(seconds)
}

func normalizeTaskPlatform(platform string) string {
	switch platform {
	case "mj", "midjourney":
		return "midjourney"
	case "suno":
		return "suno"
	case "video":
		return "video"
	}
	if _, err := strconv.Atoi(platform); err == nil {
		return "video"
	}
	return "other"
}

func taskTerminalResult(success bool) string {
	if success {
		return "success"
	}
	return "failure"
}
