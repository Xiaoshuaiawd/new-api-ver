package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestWithChannelsRuntimeHealthAddsHealthyAndOpenSnapshots(t *testing.T) {
	setting := operation_setting.GetChannelHealthSetting()
	original := *setting
	*setting = operation_setting.ChannelHealthSetting{
		Enabled:                true,
		WindowSeconds:          180,
		ProbeIntervalSeconds:   30,
		ProbeTimeoutSeconds:    30,
		ProbeBackoffMaxSeconds: 300,
		WarmupEnabled:          true,
		WarmupDurationSeconds:  60,
		WarmupStartPercent:     10,
		WarmupStepPercent:      30,
	}
	t.Cleanup(func() {
		*setting = original
		service.ResetChannelHealthForTest()
	})
	service.ResetChannelHealthForTest()

	service.OpenChannel(9202, "runtime isolate")
	items := withChannelsRuntimeHealth([]*model.Channel{
		{
			Id:     9201,
			Type:   constant.ChannelTypeOpenAI,
			Status: common.ChannelStatusEnabled,
			Name:   "healthy-channel",
		},
		{
			Id:     9202,
			Type:   constant.ChannelTypeOpenAI,
			Status: common.ChannelStatusEnabled,
			Name:   "open-channel",
		},
	})

	require.Len(t, items, 2)
	require.Equal(t, 9201, items[0].RuntimeHealth.ChannelID)
	require.Equal(t, service.ChannelHealthStateHealthy, items[0].RuntimeHealth.State)
	require.Equal(t, 100, items[0].RuntimeHealth.WarmupPercent)
	require.Equal(t, 9202, items[1].RuntimeHealth.ChannelID)
	require.Equal(t, service.ChannelHealthStateOpen, items[1].RuntimeHealth.State)
	require.Equal(t, "runtime isolate", items[1].RuntimeHealth.Reason)
}
