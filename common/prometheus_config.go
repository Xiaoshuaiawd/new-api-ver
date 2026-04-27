package common

import (
	"strings"
	"sync/atomic"
)

const defaultPrometheusPath = "/metrics"

type PrometheusConfig struct {
	Enabled     bool   `json:"enabled"`
	Path        string `json:"path"`
	BearerToken string `json:"-"`
}

var prometheusConfig atomic.Value

func init() {
	prometheusConfig.Store(normalizePrometheusConfig(PrometheusConfig{}))
}

func normalizePrometheusConfig(config PrometheusConfig) PrometheusConfig {
	config.Path = strings.TrimSpace(config.Path)
	if config.Path == "" {
		config.Path = defaultPrometheusPath
	}
	if !strings.HasPrefix(config.Path, "/") {
		config.Path = "/" + config.Path
	}
	config.BearerToken = strings.TrimSpace(config.BearerToken)
	return config
}

func GetPrometheusConfig() PrometheusConfig {
	return prometheusConfig.Load().(PrometheusConfig)
}

func SetPrometheusConfig(config PrometheusConfig) {
	prometheusConfig.Store(normalizePrometheusConfig(config))
}
