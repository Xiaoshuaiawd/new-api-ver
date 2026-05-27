package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestUpdateChannelStatusUntilStoresMultiKeyDisabledUntil(t *testing.T) {
	channel := &Channel{
		Id:     101,
		Key:    "sk-a\nsk-b",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}

	handlerMultiKeyUpdate(channel, "sk-a", common.ChannelStatusAutoDisabled, "rpm limit", 12345)

	require.Equal(t, common.ChannelStatusAutoDisabled, channel.ChannelInfo.MultiKeyStatusList[0])
	require.Equal(t, int64(12345), channel.ChannelInfo.MultiKeyDisabledUntil[0])
	require.Equal(t, "rpm limit", channel.ChannelInfo.MultiKeyDisabledReason[0])
	require.Equal(t, common.ChannelStatusEnabled, channel.Status)
}

func TestRestoreExpiredAutoDisabledKeysDoesNotRestoreManualDisabledKeys(t *testing.T) {
	channel := &Channel{
		Id:     102,
		Key:    "sk-a\nsk-b",
		Status: common.ChannelStatusAutoDisabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey: true,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
				1: common.ChannelStatusManuallyDisabled,
			},
			MultiKeyDisabledUntil: map[int]int64{
				0: 100,
				1: 100,
			},
			MultiKeyDisabledReason: map[int]string{
				0: "rpm limit",
				1: "manual",
			},
			MultiKeyDisabledTime: map[int]int64{
				0: 1,
				1: 1,
			},
		},
	}

	changed := channel.restoreExpiredAutoDisabledKeysLocked(101)

	require.True(t, changed)
	require.NotContains(t, channel.ChannelInfo.MultiKeyStatusList, 0)
	require.Equal(t, common.ChannelStatusManuallyDisabled, channel.ChannelInfo.MultiKeyStatusList[1])
	require.Contains(t, channel.ChannelInfo.MultiKeyDisabledUntil, 1)
	require.Equal(t, common.ChannelStatusEnabled, channel.Status)
}
