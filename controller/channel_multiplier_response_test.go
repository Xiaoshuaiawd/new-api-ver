package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithChannelRuntimeHealthAddsMultiplierSnapshotAndRedactsPassword(t *testing.T) {
	service.ResetChannelMultiplierMonitorForTest()
	t.Cleanup(service.ResetChannelMultiplierMonitorForTest)

	settings, err := common.Marshal(dto.ChannelOtherSettings{
		UpstreamKeyMultiplier: &dto.ChannelMultiplierMonitorConfig{
			Enabled:  true,
			Format:   dto.ChannelMultiplierProviderFormatSub2API,
			BaseURL:  "https://upstream.example.com",
			Username: "alice@example.com",
			Password: "secret",
		},
	})
	require.NoError(t, err)

	service.SetChannelMultiplierSnapshotForTest(service.ChannelMultiplierSnapshot{
		ChannelID:       9201,
		Enabled:         true,
		Format:          dto.ChannelMultiplierProviderFormatSub2API,
		State:           service.ChannelMultiplierSnapshotHealthy,
		Multiplier:      0.08,
		Balance:         26.5,
		ObservedGroup:   "Pro",
		ObservedTokenID: "581",
		ObservedAt:      time.Now().Unix(),
	})

	item := withChannelRuntimeHealth(&model.Channel{
		Id:            9201,
		Name:          "monitored",
		OtherSettings: string(settings),
	})

	require.NotNil(t, item.Channel)
	assert.Equal(t, 0.08, item.UpstreamMultiplier.Multiplier)
	assert.Equal(t, service.ChannelMultiplierSnapshotHealthy, item.UpstreamMultiplier.State)

	var redacted dto.ChannelOtherSettings
	require.NoError(t, common.UnmarshalJsonStr(item.Channel.OtherSettings, &redacted))
	require.NotNil(t, redacted.UpstreamKeyMultiplier)
	assert.Empty(t, redacted.UpstreamKeyMultiplier.Password)
}
