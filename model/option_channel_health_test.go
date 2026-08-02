package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateOptionValueRejectsInvalidChannelHealthSettings(t *testing.T) {
	require.Error(t, validateOptionValue("channel_health_setting.unknown", "1"))

	invalid := map[string]string{
		"channel_health_setting.enabled":                   "enabled",
		"channel_health_setting.ttft_timeout_seconds":      "3601",
		"channel_health_setting.window_size":               "9",
		"channel_health_setting.cooldown_after":            "0",
		"channel_health_setting.cooldown_duration_minutes": "0",
		"channel_health_setting.reference_ttft_ms":         "99",
		"channel_health_setting.warmup_threshold":          "0",
		"channel_health_setting.min_multiplier_pct":        "100",
	}

	for key, value := range invalid {
		assert.Error(t, validateOptionValue(key, value), "%s=%s must be rejected", key, value)
	}

	valid := map[string]string{
		"channel_health_setting.enabled":                   "true",
		"channel_health_setting.ttft_timeout_seconds":      "3600",
		"channel_health_setting.window_size":               "1000",
		"channel_health_setting.cooldown_after":            "1",
		"channel_health_setting.cooldown_duration_minutes": "1",
		"channel_health_setting.reference_ttft_ms":         "100",
		"channel_health_setting.warmup_threshold":          "1",
		"channel_health_setting.min_multiplier_pct":        "99",
	}

	for key, value := range valid {
		require.NoError(t, validateOptionValue(key, value), "%s=%s must be accepted", key, value)
	}
}
