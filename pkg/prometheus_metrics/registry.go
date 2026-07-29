package prometheusmetrics

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/cachex"

	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	GitCommit = ""
	BuildTime = ""
)

type Runtime struct {
	Enabled bool
	Handler http.Handler

	config          Config
	registry        *prometheus.Registry
	collectorErrors *prometheus.CounterVec
	http            *httpMetrics
	rateLimit       *rateLimitMetrics
	redis           *redisMetrics
	billing         *billingMetrics
	task            *taskMetrics
	channel         *channelMetrics
	relay           *relayMetrics
}

type buildInfo struct {
	Version   string
	Commit    string
	BuildTime string
}

type runtimeOptions struct {
	channelStateSource ChannelStateSource
	taskQueueSource    TaskQueueSource
	redisClient        *redis.Client
	redisEnabled       bool
}

type RuntimeOption func(*runtimeOptions)

func WithChannelStateSource(source ChannelStateSource) RuntimeOption {
	return func(options *runtimeOptions) {
		options.channelStateSource = source
	}
}

func WithTaskQueueSource(source TaskQueueSource) RuntimeOption {
	return func(options *runtimeOptions) {
		options.taskQueueSource = source
	}
}

func WithRedisClient(client *redis.Client, enabled bool) RuntimeOption {
	return func(options *runtimeOptions) {
		options.redisClient = client
		options.redisEnabled = enabled
	}
}

var defaultRuntime atomic.Pointer[Runtime]

func SetDefaultRuntime(runtime *Runtime) {
	defaultRuntime.Store(runtime)
}

func NewRuntime(cfg Config, version string, mainDB, logDB *sql.DB, runtimeOpts ...RuntimeOption) (*Runtime, error) {
	if !cfg.Enabled {
		return &Runtime{}, nil
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	options := runtimeOptions{}
	for _, applyOption := range runtimeOpts {
		if applyOption != nil {
			applyOption(&options)
		}
	}

	registry := prometheus.NewRegistry()
	if err := registry.Register(collectors.NewGoCollector()); err != nil {
		return nil, fmt.Errorf("register Go collector: %w", err)
	}
	if err := registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})); err != nil {
		return nil, fmt.Errorf("register process collector: %w", err)
	}

	info := resolveBuildInfo(version, readVCSSettings)
	buildMetric := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "newapi_build_info",
		Help: "Build information for this new-api process.",
	}, []string{"version", "commit", "build_time"})
	buildMetric.WithLabelValues(info.Version, info.Commit, info.BuildTime).Set(1)
	if err := registry.Register(buildMetric); err != nil {
		return nil, fmt.Errorf("register build collector: %w", err)
	}
	httpMetrics, err := registerHTTPMetrics(registry)
	if err != nil {
		return nil, err
	}
	rateLimitMetrics, err := registerRateLimitMetrics(registry)
	if err != nil {
		return nil, err
	}
	redisMetrics, err := registerRedisMetrics(registry, options.redisEnabled && options.redisClient != nil)
	if err != nil {
		return nil, err
	}
	billingMetrics, err := registerBillingMetrics(registry)
	if err != nil {
		return nil, err
	}
	taskMetrics, err := registerTaskMetrics(registry)
	if err != nil {
		return nil, err
	}
	channelMetrics, err := registerChannelMetrics(registry, !cfg.DisableChannelHistogram)
	if err != nil {
		return nil, err
	}
	relayMetrics, err := registerRelayMetrics(registry)
	if err != nil {
		return nil, err
	}

	sources := make([]databaseStatsSource, 0, 2)
	if mainDB != nil {
		sources = append(sources, databaseStatsSource{name: "main", stats: mainDB.Stats})
	}
	if logDB != nil && logDB != mainDB {
		sources = append(sources, databaseStatsSource{name: "log", stats: logDB.Stats})
	}
	if len(sources) > 0 {
		if err := registry.Register(newDatabaseCollector(sources)); err != nil {
			return nil, fmt.Errorf("register database collector: %w", err)
		}
	}

	collectorErrors := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "newapi_collector_errors_total",
		Help: "Total Prometheus collection and gather errors.",
	}, []string{"collector"})
	collectorErrors.WithLabelValues("gather").Add(0)
	if err := registry.Register(collectorErrors); err != nil {
		return nil, fmt.Errorf("register collector error metric: %w", err)
	}
	if common.IsMasterNode && options.channelStateSource != nil {
		collectorErrors.WithLabelValues("channel_state").Add(0)
		if err := registry.Register(newChannelStateCollector(
			options.channelStateSource,
			collectorErrors,
			time.Now,
			common.SysError,
		)); err != nil {
			return nil, fmt.Errorf("register channel state collector: %w", err)
		}
	}
	if common.IsMasterNode && options.taskQueueSource != nil {
		collectorErrors.WithLabelValues("task_queue").Add(0)
		if err := registry.Register(newTaskQueueCollector(options.taskQueueSource, collectorErrors, common.SysError)); err != nil {
			return nil, fmt.Errorf("register task queue collector: %w", err)
		}
	}

	errorLogger := log.New(prometheusErrorWriter{collectorErrors: collectorErrors}, "", 0)
	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
		ErrorLog:      errorLogger,
	})
	runtime := &Runtime{
		Enabled:         true,
		Handler:         handler,
		config:          cfg,
		registry:        registry,
		collectorErrors: collectorErrors,
		http:            httpMetrics,
		rateLimit:       rateLimitMetrics,
		redis:           redisMetrics,
		billing:         billingMetrics,
		task:            taskMetrics,
		channel:         channelMetrics,
		relay:           relayMetrics,
	}
	common.SetCacheLookupObserver(RecordCacheLookup)
	cachex.SetLookupObserver(RecordCacheLookup)
	if options.redisEnabled && options.redisClient != nil {
		options.redisClient.AddHook(newRedisMetricsHook(redisMetrics, time.Now))
	}
	return runtime, nil
}

func (r *Runtime) Authorize(clientIP, authorization string) bool {
	return r != nil && r.config.Authorize(clientIP, authorization)
}

func (c Config) validate() error {
	if !c.Enabled {
		return nil
	}
	if !c.AllowPublic && c.bearerToken == "" && len(c.allowedPrefixes) == 0 {
		return fmt.Errorf("%s=true requires a bearer token, an IP allowlist, or explicit public access", envEnabled)
	}
	return nil
}

func resolveBuildInfo(version string, readSettings func() (map[string]string, bool)) buildInfo {
	info := buildInfo{
		Version:   strings.TrimSpace(version),
		Commit:    strings.TrimSpace(GitCommit),
		BuildTime: strings.TrimSpace(BuildTime),
	}
	if info.Version == "" {
		info.Version = "unknown"
	}
	if settings, ok := readSettings(); ok {
		if info.Commit == "" {
			info.Commit = strings.TrimSpace(settings["vcs.revision"])
		}
		if info.BuildTime == "" {
			info.BuildTime = strings.TrimSpace(settings["vcs.time"])
		}
	}
	if info.Commit == "" {
		info.Commit = "unknown"
	}
	if info.BuildTime == "" {
		info.BuildTime = "unknown"
	}
	return info
}

func readVCSSettings() (map[string]string, bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return nil, false
	}
	settings := make(map[string]string, len(bi.Settings))
	for _, setting := range bi.Settings {
		settings[setting.Key] = setting.Value
	}
	return settings, true
}

type prometheusErrorWriter struct {
	collectorErrors *prometheus.CounterVec
}

func (w prometheusErrorWriter) Write(data []byte) (int, error) {
	if w.collectorErrors != nil {
		w.collectorErrors.WithLabelValues("gather").Inc()
	}
	message := strings.TrimSpace(string(data))
	if message != "" {
		common.SysError("prometheus gather error: " + message)
	}
	return len(data), nil
}
