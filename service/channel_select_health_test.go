package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/channelhealth"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createChannelSelectionTestChannel(t *testing.T, id int, group string, modelName string) {
	t.Helper()

	priority := int64(1)
	weight := uint(100)
	channel := &model.Channel{
		Id:       id,
		Key:      fmt.Sprintf("channel-selection-key-%d", id),
		Status:   common.ChannelStatusEnabled,
		Name:     fmt.Sprintf("channel-selection-%d", id),
		Priority: &priority,
		Weight:   &weight,
		Models:   modelName,
		Group:    group,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
}

func TestCacheGetRandomSatisfiedChannelDoesNotCrossGroupWithoutTokenPermission(t *testing.T) {
	const (
		firstChannelID  = 981101
		secondChannelID = 981102
		firstGroup      = "channel-selection-first"
		secondGroup     = "channel-selection-second"
		modelName       = "channel-selection-cross-group-model"
	)

	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}, &model.Ability{}))
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldAutoGroups := setting.AutoGroups2JsonString()
	oldUsableGroups := setting.UserUsableGroups2JSONString()
	common.MemoryCacheEnabled = true
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(fmt.Sprintf(`["%s","%s"]`, firstGroup, secondGroup)))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(fmt.Sprintf(`{"%s":"first","%s":"second"}`, firstGroup, secondGroup)))
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id IN ?", []int{firstChannelID, secondChannelID}).Delete(&model.Ability{}).Error)
		require.NoError(t, model.DB.Where("id IN ?", []int{firstChannelID, secondChannelID}).Delete(&model.Channel{}).Error)
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(oldAutoGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldUsableGroups))
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		model.InitChannelCache()
	})

	createChannelSelectionTestChannel(t, firstChannelID, firstGroup, modelName)
	createChannelSelectionTestChannel(t, secondChannelID, secondGroup, modelName)
	model.InitChannelCache()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyUserGroup, "channel-selection-user")
	common.SetContextKey(c, constant.ContextKeyTokenCrossGroupRetry, false)
	c.Set("use_channel", []string{fmt.Sprintf("%d", firstChannelID)})

	channel, _, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:        c,
		TokenGroup: "auto",
		ModelName:  modelName,
		Retry:      common.GetPointer(1),
	})

	require.NoError(t, err)
	assert.Nil(t, channel)
}

func TestCacheGetRandomSatisfiedChannelDoesNotReuseChannelInAnotherAutoGroup(t *testing.T) {
	const (
		channelID   = 981105
		firstGroup  = "channel-selection-shared-first"
		secondGroup = "channel-selection-shared-second"
		modelName   = "channel-selection-shared-model"
	)

	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}, &model.Ability{}))
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldAutoGroups := setting.AutoGroups2JsonString()
	oldUsableGroups := setting.UserUsableGroups2JSONString()
	common.MemoryCacheEnabled = true
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(fmt.Sprintf(`[%q,%q]`, firstGroup, secondGroup)))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(fmt.Sprintf(`{"%s":"first","%s":"second"}`, firstGroup, secondGroup)))
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id = ?", channelID).Delete(&model.Ability{}).Error)
		require.NoError(t, model.DB.Where("id = ?", channelID).Delete(&model.Channel{}).Error)
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(oldAutoGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldUsableGroups))
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		model.InitChannelCache()
	})

	createChannelSelectionTestChannel(t, channelID, firstGroup+","+secondGroup, modelName)
	model.InitChannelCache()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyUserGroup, "channel-selection-user")
	common.SetContextKey(c, constant.ContextKeyTokenCrossGroupRetry, true)
	c.Set("use_channel", []string{fmt.Sprintf("%d", channelID)})

	channel, _, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:        c,
		TokenGroup: "auto",
		ModelName:  modelName,
		Retry:      common.GetPointer(1),
	})

	require.NoError(t, err)
	assert.Nil(t, channel)
}

func TestCacheGetRandomSatisfiedChannelSkipsUsedChannelWithoutMemoryCache(t *testing.T) {
	const (
		channelID = 981103
		group     = "channel-selection-no-cache"
		modelName = "channel-selection-no-cache-model"
	)

	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}, &model.Ability{}))
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id = ?", channelID).Delete(&model.Ability{}).Error)
		require.NoError(t, model.DB.Where("id = ?", channelID).Delete(&model.Channel{}).Error)
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		model.InitChannelCache()
	})

	createChannelSelectionTestChannel(t, channelID, group, modelName)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("use_channel", []string{fmt.Sprintf("%d", channelID)})

	channel, _, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:        c,
		TokenGroup: group,
		ModelName:  modelName,
		Retry:      common.GetPointer(1),
	})

	require.NoError(t, err)
	assert.Nil(t, channel)
}

func TestCacheGetRandomSatisfiedChannelSkipsCooldownWithoutMemoryCache(t *testing.T) {
	const (
		channelID = 981104
		group     = "channel-selection-cooldown"
		modelName = "channel-selection-cooldown-model"
	)

	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}, &model.Ability{}))
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldConfig := channelhealth.GetConfig()
	common.MemoryCacheEnabled = false
	channelhealth.Configure(channelhealth.Config{
		Enabled:          true,
		CooldownAfter:    1,
		CooldownDuration: time.Minute,
	})
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id = ?", channelID).Delete(&model.Ability{}).Error)
		require.NoError(t, model.DB.Where("id = ?", channelID).Delete(&model.Channel{}).Error)
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		channelhealth.Configure(oldConfig)
		model.InitChannelCache()
	})

	createChannelSelectionTestChannel(t, channelID, group, modelName)
	channelhealth.Record(channelID, false, -1)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	channel, _, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:        c,
		TokenGroup: group,
		ModelName:  modelName,
		Retry:      common.GetPointer(0),
	})

	require.NoError(t, err)
	assert.Nil(t, channel)
}
