package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

func TestChannelAffinitySettingExportsAndLoadsRecoveryStrategy(t *testing.T) {
	setting := GetChannelAffinitySetting()
	original := *setting
	t.Cleanup(func() {
		*setting = original
	})

	exported := config.GlobalConfig.ExportAllConfigs()
	require.Equal(t, ChannelAffinityRecoveryStrategyPriorityFirst, exported["channel_affinity_setting.recovery_strategy"])

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"channel_affinity_setting.recovery_strategy": ChannelAffinityRecoveryStrategyStableAffinity,
	}))

	require.Equal(t, ChannelAffinityRecoveryStrategyStableAffinity, setting.RecoveryStrategy)
}
