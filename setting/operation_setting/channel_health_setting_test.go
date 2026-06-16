package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

func TestChannelHealthSettingExportsAndLoadsRuntimeOptions(t *testing.T) {
	setting := GetChannelHealthSetting()
	original := *setting
	t.Cleanup(func() {
		*setting = original
	})

	exported := config.GlobalConfig.ExportAllConfigs()
	require.Equal(t, "true", exported["channel_health_setting.enabled"])
	require.Equal(t, "180", exported["channel_health_setting.window_seconds"])
	require.Equal(t, "10", exported["channel_health_setting.min_samples"])
	require.Equal(t, "5", exported["channel_health_setting.min_failures"])
	require.Equal(t, "0.4", exported["channel_health_setting.error_rate_threshold"])
	require.Equal(t, "5", exported["channel_health_setting.consecutive_failure_threshold"])
	require.Equal(t, "45", exported["channel_health_setting.first_response_timeout_seconds"])
	require.Equal(t, "3", exported["channel_health_setting.stuck_inflight_threshold"])
	require.Equal(t, "75", exported["channel_health_setting.single_stuck_timeout_seconds"])
	require.Equal(t, "30", exported["channel_health_setting.probe_interval_seconds"])
	require.Equal(t, "30", exported["channel_health_setting.probe_timeout_seconds"])
	require.Equal(t, "2", exported["channel_health_setting.probe_successes_to_recover"])
	require.Equal(t, "300", exported["channel_health_setting.probe_backoff_max_seconds"])
	require.Equal(t, "true", exported["channel_health_setting.warmup_enabled"])
	require.Equal(t, "60", exported["channel_health_setting.warmup_duration_seconds"])
	require.Equal(t, "10", exported["channel_health_setting.warmup_start_percent"])
	require.Equal(t, "30", exported["channel_health_setting.warmup_step_percent"])

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"channel_health_setting.enabled":                        "false",
		"channel_health_setting.window_seconds":                 "240",
		"channel_health_setting.min_samples":                    "12",
		"channel_health_setting.min_failures":                   "6",
		"channel_health_setting.error_rate_threshold":           "0.25",
		"channel_health_setting.consecutive_failure_threshold":  "4",
		"channel_health_setting.first_response_timeout_seconds": "50",
		"channel_health_setting.stuck_inflight_threshold":       "2",
		"channel_health_setting.single_stuck_timeout_seconds":   "80",
		"channel_health_setting.probe_interval_seconds":         "40",
		"channel_health_setting.probe_timeout_seconds":          "20",
		"channel_health_setting.probe_successes_to_recover":     "3",
		"channel_health_setting.probe_backoff_max_seconds":      "180",
		"channel_health_setting.warmup_enabled":                 "true",
		"channel_health_setting.warmup_duration_seconds":        "120",
		"channel_health_setting.warmup_start_percent":           "15",
		"channel_health_setting.warmup_step_percent":            "25",
	}))

	require.False(t, setting.Enabled)
	require.Equal(t, 240, setting.WindowSeconds)
	require.Equal(t, 12, setting.MinSamples)
	require.Equal(t, 6, setting.MinFailures)
	require.Equal(t, 0.25, setting.ErrorRateThreshold)
	require.Equal(t, 4, setting.ConsecutiveFailureThreshold)
	require.Equal(t, 50, setting.FirstResponseTimeoutSeconds)
	require.Equal(t, 2, setting.StuckInflightThreshold)
	require.Equal(t, 80, setting.SingleStuckTimeoutSeconds)
	require.Equal(t, 40, setting.ProbeIntervalSeconds)
	require.Equal(t, 20, setting.ProbeTimeoutSeconds)
	require.Equal(t, 3, setting.ProbeSuccessesToRecover)
	require.Equal(t, 180, setting.ProbeBackoffMaxSeconds)
	require.True(t, setting.WarmupEnabled)
	require.Equal(t, 120, setting.WarmupDurationSeconds)
	require.Equal(t, 15, setting.WarmupStartPercent)
	require.Equal(t, 25, setting.WarmupStepPercent)
}
