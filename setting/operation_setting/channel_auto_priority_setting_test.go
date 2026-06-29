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

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"channel_auto_priority_setting.enabled":    "true",
		"channel_auto_priority_setting.min_weight": "10",
		"channel_auto_priority_setting.max_weight": "120",
	}))

	require.True(t, setting.Enabled)
	require.Equal(t, 10, setting.MinWeight)
	require.Equal(t, 120, setting.MaxWeight)
}
