package prometheusmetrics

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/prometheus/client_golang/prometheus"
)

const channelStateErrorLogInterval = time.Minute

type ChannelState struct {
	ID     int
	Name   string
	Type   int
	Status int
}

type ChannelStateSource func() ([]ChannelState, error)

type channelStateCollector struct {
	source          ChannelStateSource
	collectorErrors *prometheus.CounterVec
	now             func() time.Time
	logError        func(string)

	enabled *prometheus.Desc
	info    *prometheus.Desc
	up      *prometheus.Desc

	logMu        sync.Mutex
	lastErrorLog time.Time
}

func newChannelStateCollector(
	source ChannelStateSource,
	collectorErrors *prometheus.CounterVec,
	now func() time.Time,
	logError func(string),
) prometheus.Collector {
	return &channelStateCollector{
		source:          source,
		collectorErrors: collectorErrors,
		now:             now,
		logError:        logError,
		enabled: prometheus.NewDesc(
			"newapi_channel_enabled",
			"Whether the channel is enabled in the shared database.",
			[]string{"channel_id", "channel_type"},
			nil,
		),
		info: prometheus.NewDesc(
			"newapi_channel_info",
			"Static channel metadata used to map a channel ID to its display name.",
			[]string{"channel_id", "channel_name", "channel_type"},
			nil,
		),
		up: prometheus.NewDesc(
			"newapi_shared_collector_up",
			"Whether a shared database collector completed successfully.",
			nil,
			prometheus.Labels{"collector": "channel_state"},
		),
	}
}

func (c *channelStateCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.enabled
	ch <- c.info
	ch <- c.up
}

func (c *channelStateCollector) Collect(ch chan<- prometheus.Metric) {
	states, err := c.source()
	if err != nil {
		if c.collectorErrors != nil {
			c.collectorErrors.WithLabelValues("channel_state").Inc()
		}
		c.logCollectionError(err)
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
		return
	}

	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)
	for _, state := range states {
		enabled := 0.0
		if state.Status == common.ChannelStatusEnabled {
			enabled = 1
		}
		ch <- prometheus.MustNewConstMetric(
			c.enabled,
			prometheus.GaugeValue,
			enabled,
			strconv.Itoa(state.ID),
			strconv.Itoa(state.Type),
		)
		ch <- prometheus.MustNewConstMetric(
			c.info,
			prometheus.GaugeValue,
			1,
			strconv.Itoa(state.ID),
			state.Name,
			strconv.Itoa(state.Type),
		)
	}
}

func (c *channelStateCollector) logCollectionError(err error) {
	if c.logError == nil {
		return
	}
	now := c.now()
	c.logMu.Lock()
	if !c.lastErrorLog.IsZero() && now.Sub(c.lastErrorLog) < channelStateErrorLogInterval {
		c.logMu.Unlock()
		return
	}
	c.lastErrorLog = now
	c.logMu.Unlock()
	c.logError(fmt.Sprintf("prometheus channel state collector error: %v", err))
}
