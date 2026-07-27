package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestChannelAffinityYieldsToHigherPriorityChannelForRequestPath(t *testing.T) {
	const (
		group         = "affinity-priority-test-group"
		modelName     = "gpt-affinity-priority-test"
		highChannelID = 9301
		lowChannelID  = 9302
	)

	require.NoError(t, model.DB.AutoMigrate(&model.Ability{}))
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id IN ?", []int{highChannelID, lowChannelID}).Delete(&model.Ability{}).Error)
		require.NoError(t, model.DB.Where("id IN ?", []int{highChannelID, lowChannelID}).Delete(&model.Channel{}).Error)
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		model.InitChannelCache()
	})

	highPriority := int64(10)
	lowPriority := int64(1)
	weight := uint(100)
	highChannel := &model.Channel{
		Id:       highChannelID,
		Type:     constant.ChannelTypeAdvancedCustom,
		Key:      "sk-high-priority",
		Status:   common.ChannelStatusEnabled,
		Name:     "high-priority",
		Priority: &highPriority,
		Weight:   &weight,
		Models:   modelName,
		Group:    group,
	}
	highChannel.SetOtherSettings(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{
			Routes: []dto.AdvancedCustomRoute{
				{
					IncomingPath: "/v1/responses",
					UpstreamPath: "/v1/responses",
					Models:       []string{modelName},
				},
			},
		},
	})
	lowChannel := &model.Channel{
		Id:       lowChannelID,
		Type:     constant.ChannelTypeOpenAI,
		Key:      "sk-low-priority",
		Status:   common.ChannelStatusEnabled,
		Name:     "low-priority",
		Priority: &lowPriority,
		Weight:   &weight,
		Models:   modelName,
		Group:    group,
	}

	require.NoError(t, model.DB.Create(highChannel).Error)
	require.NoError(t, model.DB.Create(lowChannel).Error)
	require.NoError(t, highChannel.AddAbilities(nil))
	require.NoError(t, lowChannel.AddAbilities(nil))
	model.InitChannelCache()

	require.True(t, IsChannelAffinityPriorityStale(group, modelName, "/v1/responses", lowChannelID))
	require.False(t, IsChannelAffinityPriorityStale(group, modelName, "/v1/messages", lowChannelID))
	require.False(t, IsChannelAffinityPriorityStale(group, modelName, "/v1/responses", highChannelID))
}
