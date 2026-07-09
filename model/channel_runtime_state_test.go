package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetChannelRetainsDueProbeTrafficBeforeLowerPriorityNormalChannel(t *testing.T) {
	withRuntimeStateSelectionDB(t, false)
	insertRuntimeStateCandidate(t, 9601, "gpt-runtime-retained-probe", 10)
	insertRuntimeStateCandidate(t, 9602, "gpt-runtime-retained-probe", 1)
	claimed := 0
	SetChannelRuntimeStateFunc(func(channelID int, modelName string, mode ChannelRuntimeStateMode) (bool, int) {
		switch mode {
		case ChannelRuntimeStateNormal:
			return channelID == 9602, 0
		case ChannelRuntimeStateProbe:
			return channelID == 9601, 0
		case ChannelRuntimeStateClaimProbe:
			if channelID == 9601 {
				claimed++
				return true, 0
			}
			return false, 0
		default:
			return false, 0
		}
	})

	hits := map[int]int{}
	for i := 0; i < 12; i++ {
		channel, err := GetChannel("default", "gpt-runtime-retained-probe", 0, "")
		require.NoError(t, err)
		require.NotNil(t, channel)
		hits[channel.Id]++
	}

	require.Greater(t, hits[9601], 0)
	require.Greater(t, hits[9602], hits[9601])
	require.Equal(t, hits[9601], claimed)
}

func TestRuntimeProbeCandidatesExcludeNormalAvailableChannels(t *testing.T) {
	withRuntimeStateSelectionDB(t, true)
	insertRuntimeStateCandidate(t, 9651, "gpt-runtime-probe-dedupe", 10)
	insertRuntimeStateCandidate(t, 9652, "gpt-runtime-probe-dedupe", 10)
	InitChannelCache()
	SetChannelRuntimeStateFunc(func(channelID int, modelName string, mode ChannelRuntimeStateMode) (bool, int) {
		return true, 0
	})

	normalCandidates, err := collectRuntimeCandidates("default", "gpt-runtime-probe-dedupe", group2model2channels["default"]["gpt-runtime-probe-dedupe"], 10, false, nil)
	require.NoError(t, err)
	probeCandidates, err := collectRuntimeCandidates("default", "gpt-runtime-probe-dedupe", group2model2channels["default"]["gpt-runtime-probe-dedupe"], 10, true, nil)
	require.NoError(t, err)

	require.Len(t, normalCandidates, 2)
	require.Empty(t, filterRuntimeProbeCandidates(probeCandidates, normalCandidates))
}

func TestChannelSelectionUsesSmoothWeightsForDatabaseAndCachePaths(t *testing.T) {
	for _, memoryCacheEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory_cache_%t", memoryCacheEnabled), func(t *testing.T) {
			withRuntimeStateSelectionDB(t, memoryCacheEnabled)
			insertRuntimeStateCandidate(t, 9701, "gpt-runtime-smooth", 10)
			insertRuntimeStateCandidate(t, 9702, "gpt-runtime-smooth", 10)
			if memoryCacheEnabled {
				InitChannelCache()
			}
			SetChannelRuntimeStateFunc(func(channelID int, modelName string, mode ChannelRuntimeStateMode) (bool, int) {
				return true, 0
			})

			first, err := GetRandomSatisfiedChannel("default", "gpt-runtime-smooth", 0, "")
			require.NoError(t, err)
			require.NotNil(t, first)
			second, err := GetRandomSatisfiedChannel("default", "gpt-runtime-smooth", 0, "")
			require.NoError(t, err)
			require.NotNil(t, second)

			require.NotEqual(t, first.Id, second.Id)
		})
	}
}

func TestGetChannelSkipsRuntimeUnavailableChannel(t *testing.T) {
	withRuntimeStateSelectionDB(t, false)

	insertRuntimeStateCandidate(t, 9301, "gpt-runtime-state", 10)
	insertRuntimeStateCandidate(t, 9302, "gpt-runtime-state", 1)
	SetChannelRuntimeStateFunc(func(channelID int, modelName string, mode ChannelRuntimeStateMode) (bool, int) {
		return channelID != 9301, 0
	})

	channel, err := GetChannel("default", "gpt-runtime-state", 0, "")

	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, 9302, channel.Id)
}

func TestGetChannelUsesDueProbeChannelWhenAllNormalUnavailable(t *testing.T) {
	withRuntimeStateSelectionDB(t, false)

	insertRuntimeStateCandidate(t, 9401, "gpt-runtime-probe", 10)
	insertRuntimeStateCandidate(t, 9402, "gpt-runtime-probe", 1)
	claimed := false
	SetChannelRuntimeStateFunc(func(channelID int, modelName string, mode ChannelRuntimeStateMode) (bool, int) {
		switch mode {
		case ChannelRuntimeStateNormal:
			return false, 0
		case ChannelRuntimeStateProbe:
			return channelID == 9401, 0
		case ChannelRuntimeStateClaimProbe:
			if channelID == 9401 && !claimed {
				claimed = true
				return true, 0
			}
			return false, 0
		default:
			return false, 0
		}
	})

	channel, err := GetChannel("default", "gpt-runtime-probe", 0, "")

	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, 9401, channel.Id)
	require.True(t, claimed)
}

func TestGetChannelPassesModelNameToRuntimeState(t *testing.T) {
	withRuntimeStateSelectionDB(t, false)

	insertRuntimeStateCandidate(t, 9501, "gpt-runtime-model-a", 10)
	insertRuntimeStateCandidate(t, 9502, "gpt-runtime-model-a", 1)
	insertRuntimeStateAbility(t, 9501, "gpt-runtime-model-b", 10)
	insertRuntimeStateAbility(t, 9502, "gpt-runtime-model-b", 1)
	seenModels := make(map[string]bool)
	SetChannelRuntimeStateFunc(func(channelID int, modelName string, mode ChannelRuntimeStateMode) (bool, int) {
		seenModels[modelName] = true
		return !(channelID == 9501 && modelName == "gpt-runtime-model-a"), 0
	})

	channelA, err := GetChannel("default", "gpt-runtime-model-a", 0, "")
	require.NoError(t, err)
	require.NotNil(t, channelA)
	require.Equal(t, 9502, channelA.Id)

	channelB, err := GetChannel("default", "gpt-runtime-model-b", 0, "")
	require.NoError(t, err)
	require.NotNil(t, channelB)
	require.Equal(t, 9501, channelB.Id)
	require.True(t, seenModels["gpt-runtime-model-a"])
	require.True(t, seenModels["gpt-runtime-model-b"])
}

func withRuntimeStateSelectionDB(t *testing.T, memoryCacheEnabled bool) {
	t.Helper()

	oldDB := DB
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldMainDatabaseType := common.MainDatabaseType()
	oldLogDatabaseType := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	common.MemoryCacheEnabled = memoryCacheEnabled
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	initCol()
	t.Cleanup(func() {
		DB = oldDB
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.SetDatabaseTypes(oldMainDatabaseType, oldLogDatabaseType)
		SetChannelRuntimeStateFunc(nil)
		SetChannelRuntimeWeightFunc(nil)
		ResetChannelRuntimeSelectionStateForTest()
		initCol()
		InitChannelCache()
	})
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))
	ResetChannelRuntimeSelectionStateForTest()
}

func insertRuntimeStateCandidate(t *testing.T, channelID int, modelName string, priorityValue int64) {
	t.Helper()
	weight := uint(100)
	require.NoError(t, DB.Create(&Channel{
		Id:       channelID,
		Type:     constant.ChannelTypeOpenAI,
		Key:      fmt.Sprintf("sk-%d", channelID),
		Status:   common.ChannelStatusEnabled,
		Name:     fmt.Sprintf("channel-%d", channelID),
		Priority: &priorityValue,
		Weight:   &weight,
		Models:   modelName,
		Group:    "default",
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     modelName,
		ChannelId: channelID,
		Enabled:   true,
		Priority:  &priorityValue,
		Weight:    weight,
	}).Error)
}

func insertRuntimeStateAbility(t *testing.T, channelID int, modelName string, priorityValue int64) {
	t.Helper()
	weight := uint(100)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     modelName,
		ChannelId: channelID,
		Enabled:   true,
		Priority:  &priorityValue,
		Weight:    weight,
	}).Error)
}
