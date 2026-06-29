package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

func TestChannelMultiplierMonitorSettingExportsAndLoadsRuntimeOptions(t *testing.T) {
	setting := GetChannelMultiplierMonitorSetting()
	original := *setting
	t.Cleanup(func() {
		*setting = original
	})

	exported := config.GlobalConfig.ExportAllConfigs()
	require.Equal(t, "2", exported["channel_multiplier_monitor_setting.interval_minutes"])

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"channel_multiplier_monitor_setting.interval_minutes": "7",
	}))

	require.Equal(t, 7, setting.IntervalMinutes)
}
