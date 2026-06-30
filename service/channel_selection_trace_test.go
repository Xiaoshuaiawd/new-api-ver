package service

import (
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSelectionTraceRecordsAffinityHit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)

	RecordChannelSelectionTrace(ctx, ChannelSelectionTraceEvent{
		Stage:     ChannelSelectionTraceStageAffinity,
		Action:    ChannelSelectionTraceActionHit,
		Group:     "default",
		Model:     "gpt-trace",
		ChannelID: 9101,
		Reason:    "rule-a",
	})

	adminInfo := map[string]interface{}{}
	AppendChannelSelectionTraceAdminInfo(ctx, adminInfo)

	trace, ok := adminInfo["channel_selection_trace"].([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, trace, 1)
	require.Equal(t, string(ChannelSelectionTraceStageAffinity), trace[0]["stage"])
	require.Equal(t, string(ChannelSelectionTraceActionHit), trace[0]["action"])
	require.Equal(t, 9101, trace[0]["channel_id"])
}

func TestChannelSelectionTraceRecordsRuntimeOpenSkip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)

	RecordChannelSelectionTrace(ctx, ChannelSelectionTraceEvent{
		Stage:       ChannelSelectionTraceStageRuntime,
		Action:      ChannelSelectionTraceActionSkip,
		Group:       "default",
		Model:       "gpt-trace",
		ChannelID:   9101,
		Priority:    10,
		HealthState: string(ChannelHealthStateOpen),
		Reason:      "runtime open",
	})

	adminInfo := map[string]interface{}{}
	AppendChannelSelectionTraceAdminInfo(ctx, adminInfo)

	trace, ok := adminInfo["channel_selection_trace"].([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, trace, 1)
	require.Equal(t, string(ChannelSelectionTraceStageRuntime), trace[0]["stage"])
	require.Equal(t, string(ChannelSelectionTraceActionSkip), trace[0]["action"])
	require.Equal(t, string(ChannelHealthStateOpen), trace[0]["health_state"])
	require.Equal(t, "runtime open", trace[0]["reason"])
}

func TestChannelSelectionTraceDoesNotLeakBetweenConcurrentSelections(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withChannelHealthTestSettings(t)
	withChannelHealthSelectionDB(t)

	var calls atomic.Int32
	firstRuntimeReached := make(chan struct{})
	secondRuntimeReached := make(chan struct{})
	releaseFirst := make(chan struct{})
	releaseSecond := make(chan struct{})

	model.SetChannelRuntimeStateFunc(func(channelID int, modelName string, mode model.ChannelRuntimeStateMode) (bool, int) {
		if channelID != 9101 || mode != model.ChannelRuntimeStateNormal {
			return true, 0
		}
		switch calls.Add(1) {
		case 1:
			close(firstRuntimeReached)
			<-releaseFirst
		case 2:
			close(secondRuntimeReached)
			<-releaseSecond
		}
		return false, 0
	})
	t.Cleanup(func() {
		model.SetChannelRuntimeStateFunc(func(channelID int, modelName string, mode model.ChannelRuntimeStateMode) (bool, int) {
			return IsChannelAvailable(channelID), GetChannelInflight(channelID)
		})
	})

	ctxA, _ := gin.CreateTestContext(nil)
	ctxB, _ := gin.CreateTestContext(nil)
	errA := make(chan error, 1)
	errB := make(chan error, 1)

	go func() {
		_, _, err := CacheGetRandomSatisfiedChannel(&RetryParam{
			Ctx:        ctxA,
			TokenGroup: "default",
			ModelName:  "gpt-health-test",
		})
		errA <- err
	}()
	<-firstRuntimeReached

	go func() {
		_, _, err := CacheGetRandomSatisfiedChannel(&RetryParam{
			Ctx:        ctxB,
			TokenGroup: "default",
			ModelName:  "gpt-health-test",
		})
		errB <- err
	}()
	<-secondRuntimeReached

	close(releaseFirst)
	require.NoError(t, <-errA)
	close(releaseSecond)
	require.NoError(t, <-errB)

	eventsA := getChannelSelectionTraceEvents(ctxA)
	require.NotEmpty(t, eventsA)
	require.Equal(t, 9101, eventsA[0].ChannelID)
	require.Equal(t, ChannelSelectionTraceStageRuntime, eventsA[0].Stage)

	eventsB := getChannelSelectionTraceEvents(ctxB)
	require.NotEmpty(t, eventsB)
	require.Equal(t, 9101, eventsB[0].ChannelID)
	require.Equal(t, ChannelSelectionTraceStageRuntime, eventsB[0].Stage)
}

func TestCacheGetRandomSatisfiedChannelExcludesFailedChannelsDuringRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withChannelHealthSelectionDB(t)
	pFallback := int64(0)
	weight := uint(100)
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:       9103,
		Type:     constant.ChannelTypeOpenAI,
		Key:      "sk-fallback",
		Status:   common.ChannelStatusEnabled,
		Name:     "fallback-priority",
		Priority: &pFallback,
		Weight:   &weight,
		Models:   "gpt-health-test",
		Group:    "default",
	}).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-health-test",
		ChannelId: 9103,
		Enabled:   true,
		Priority:  &pFallback,
		Weight:    weight,
	}).Error)
	model.InitChannelCache()

	param := &RetryParam{
		TokenGroup:         "default",
		ModelName:          "gpt-health-test",
		Retry:              common.GetPointer(0),
		ExcludedChannelIDs: map[int]struct{}{9101: {}},
	}

	channel, group, err := CacheGetRandomSatisfiedChannel(param)

	require.NoError(t, err)
	require.Equal(t, "default", group)
	require.NotNil(t, channel)
	assert.Equal(t, 9102, channel.Id)

	param.ExcludedChannelIDs[9102] = struct{}{}
	channel, _, err = CacheGetRandomSatisfiedChannel(param)

	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 9103, channel.Id)
}

func TestCacheGetRandomSatisfiedChannelSkipsChannelsWithoutImageInputSupport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withChannelHealthSelectionDB(t)

	supportsImageInput := false
	settings, err := common.Marshal(dto.ChannelOtherSettings{
		SupportsImageInput: &supportsImageInput,
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 9101).Update("settings", string(settings)).Error)
	model.InitChannelCache()

	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		TokenGroup:               "default",
		ModelName:                "gpt-health-test",
		Retry:                    common.GetPointer(0),
		RequireImageInputSupport: true,
		ExcludedChannelIDs:       map[int]struct{}{},
	})

	require.NoError(t, err)
	require.Equal(t, "default", group)
	require.NotNil(t, channel)
	assert.Equal(t, 9102, channel.Id)

	channel, _, err = CacheGetRandomSatisfiedChannel(&RetryParam{
		TokenGroup:               "default",
		ModelName:                "gpt-health-test",
		Retry:                    common.GetPointer(0),
		RequireImageInputSupport: false,
		ExcludedChannelIDs:       map[int]struct{}{},
	})

	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 9101, channel.Id)
}
