package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

type ChannelHealthSetting struct {
	Enabled                     bool    `json:"enabled"`
	WindowSeconds               int     `json:"window_seconds"`
	MinSamples                  int     `json:"min_samples"`
	MinFailures                 int     `json:"min_failures"`
	ErrorRateThreshold          float64 `json:"error_rate_threshold"`
	ConsecutiveFailureThreshold int     `json:"consecutive_failure_threshold"`
	FirstResponseTimeoutSeconds int     `json:"first_response_timeout_seconds"`
	StuckInflightThreshold      int     `json:"stuck_inflight_threshold"`
	SingleStuckTimeoutSeconds   int     `json:"single_stuck_timeout_seconds"`
	ProbeIntervalSeconds        int     `json:"probe_interval_seconds"`
	ProbeTimeoutSeconds         int     `json:"probe_timeout_seconds"`
	ProbeSuccessesToRecover     int     `json:"probe_successes_to_recover"`
	ProbeBackoffMaxSeconds      int     `json:"probe_backoff_max_seconds"`
	WarmupEnabled               bool    `json:"warmup_enabled"`
	WarmupDurationSeconds       int     `json:"warmup_duration_seconds"`
	WarmupStartPercent          int     `json:"warmup_start_percent"`
	WarmupStepPercent           int     `json:"warmup_step_percent"`
}

var channelHealthSetting = ChannelHealthSetting{
	Enabled:                     true,
	WindowSeconds:               180,
	MinSamples:                  10,
	MinFailures:                 5,
	ErrorRateThreshold:          0.40,
	ConsecutiveFailureThreshold: 5,
	FirstResponseTimeoutSeconds: 45,
	StuckInflightThreshold:      3,
	SingleStuckTimeoutSeconds:   75,
	ProbeIntervalSeconds:        30,
	ProbeTimeoutSeconds:         30,
	ProbeSuccessesToRecover:     2,
	ProbeBackoffMaxSeconds:      300,
	WarmupEnabled:               true,
	WarmupDurationSeconds:       60,
	WarmupStartPercent:          10,
	WarmupStepPercent:           30,
}

func init() {
	config.GlobalConfig.Register("channel_health_setting", &channelHealthSetting)
}

func GetChannelHealthSetting() *ChannelHealthSetting {
	return &channelHealthSetting
}
