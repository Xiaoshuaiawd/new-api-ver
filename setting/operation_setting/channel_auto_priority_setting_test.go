package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

func TestChannelAutoPrioritySettingExportsAndLoadsRuntimeOptions(t *testing.T) {
	setting := GetChannelAutoPrioritySetting()
	original := *setting
	t.Cleanup(func() {
		*setting = original
	})

	exported := config.GlobalConfig.ExportAllConfigs()
	require.Equal(t, "false", exported["channel_auto_priority_setting.enabled"])
	require.Equal(t, "20", exported["channel_auto_priority_setting.min_weight"])
	require.Equal(t, "100", exported["channel_auto_priority_setting.max_weight"])
	require.Equal(t, "false", exported["channel_auto_priority_setting.latency_guard_enabled"])
	require.Equal(t, "10", exported["channel_auto_priority_setting.latency_threshold_seconds"])
	require.Equal(t, "10", exported["channel_auto_priority_setting.latency_window_minutes"])
	require.Equal(t, "20", exported["channel_auto_priority_setting.latency_min_samples"])
	require.Equal(t, "0.3", exported["channel_auto_priority_setting.latency_slow_ratio_threshold"])
	require.Equal(t, "0.1", exported["channel_auto_priority_setting.latency_recovery_ratio_threshold"])
	require.Equal(t, "20", exported["channel_auto_priority_setting.latency_retained_weight_percent"])
	require.Equal(t, "1", exported["channel_auto_priority_setting.latency_priority_penalty"])

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"channel_auto_priority_setting.enabled":                          "true",
		"channel_auto_priority_setting.min_weight":                       "10",
		"channel_auto_priority_setting.max_weight":                       "120",
		"channel_auto_priority_setting.latency_guard_enabled":            "true",
		"channel_auto_priority_setting.latency_threshold_seconds":        "8",
		"channel_auto_priority_setting.latency_window_minutes":           "15",
		"channel_auto_priority_setting.latency_min_samples":              "30",
		"channel_auto_priority_setting.latency_slow_ratio_threshold":     "0.25",
		"channel_auto_priority_setting.latency_recovery_ratio_threshold": "0.05",
		"channel_auto_priority_setting.latency_retained_weight_percent":  "15",
		"channel_auto_priority_setting.latency_priority_penalty":         "2",
	}))

	require.True(t, setting.Enabled)
	require.Equal(t, 10, setting.MinWeight)
	require.Equal(t, 120, setting.MaxWeight)
	require.True(t, setting.LatencyGuardEnabled)
	require.Equal(t, 8, setting.LatencyThresholdSeconds)
	require.Equal(t, 15, setting.LatencyWindowMinutes)
	require.Equal(t, 30, setting.LatencyMinSamples)
	require.Equal(t, 0.25, setting.LatencySlowRatioThreshold)
	require.Equal(t, 0.05, setting.LatencyRecoveryRatioThreshold)
	require.Equal(t, 15, setting.LatencyRetainedWeightPercent)
	require.Equal(t, 2, setting.LatencyPriorityPenalty)
}
