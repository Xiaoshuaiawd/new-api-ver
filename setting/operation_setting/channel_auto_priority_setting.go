package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

const (
	ChannelAutoPriorityDefaultMinWeight                     = 20
	ChannelAutoPriorityDefaultMaxWeight                     = 100
	ChannelAutoPriorityDefaultLatencyThresholdSeconds       = 10
	ChannelAutoPriorityDefaultLatencyWindowMinutes          = 10
	ChannelAutoPriorityDefaultLatencyMinSamples             = 20
	ChannelAutoPriorityDefaultLatencySlowRatioThreshold     = 0.30
	ChannelAutoPriorityDefaultLatencyRecoveryRatioThreshold = 0.10
	ChannelAutoPriorityDefaultLatencyRetainedWeightPercent  = 20
	ChannelAutoPriorityDefaultLatencyPriorityPenalty        = 1
)

type ChannelAutoPrioritySetting struct {
	Enabled                       bool    `json:"enabled"`
	MinWeight                     int     `json:"min_weight"`
	MaxWeight                     int     `json:"max_weight"`
	LatencyGuardEnabled           bool    `json:"latency_guard_enabled"`
	LatencyThresholdSeconds       int     `json:"latency_threshold_seconds"`
	LatencyWindowMinutes          int     `json:"latency_window_minutes"`
	LatencyMinSamples             int     `json:"latency_min_samples"`
	LatencySlowRatioThreshold     float64 `json:"latency_slow_ratio_threshold"`
	LatencyRecoveryRatioThreshold float64 `json:"latency_recovery_ratio_threshold"`
	LatencyRetainedWeightPercent  int     `json:"latency_retained_weight_percent"`
	LatencyPriorityPenalty        int     `json:"latency_priority_penalty"`
}

var channelAutoPrioritySetting = ChannelAutoPrioritySetting{
	Enabled:                       false,
	MinWeight:                     ChannelAutoPriorityDefaultMinWeight,
	MaxWeight:                     ChannelAutoPriorityDefaultMaxWeight,
	LatencyGuardEnabled:           false,
	LatencyThresholdSeconds:       ChannelAutoPriorityDefaultLatencyThresholdSeconds,
	LatencyWindowMinutes:          ChannelAutoPriorityDefaultLatencyWindowMinutes,
	LatencyMinSamples:             ChannelAutoPriorityDefaultLatencyMinSamples,
	LatencySlowRatioThreshold:     ChannelAutoPriorityDefaultLatencySlowRatioThreshold,
	LatencyRecoveryRatioThreshold: ChannelAutoPriorityDefaultLatencyRecoveryRatioThreshold,
	LatencyRetainedWeightPercent:  ChannelAutoPriorityDefaultLatencyRetainedWeightPercent,
	LatencyPriorityPenalty:        ChannelAutoPriorityDefaultLatencyPriorityPenalty,
}

func init() {
	config.GlobalConfig.Register("channel_auto_priority_setting", &channelAutoPrioritySetting)
}

func GetChannelAutoPrioritySetting() *ChannelAutoPrioritySetting {
	return &channelAutoPrioritySetting
}
