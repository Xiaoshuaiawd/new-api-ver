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

func TestGetChannelSkipsRuntimeUnavailableChannel(t *testing.T) {
	oldDB := DB
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldSQLite := common.UsingSQLite
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	common.MemoryCacheEnabled = false
	common.UsingSQLite = true
	initCol()
	t.Cleanup(func() {
		DB = oldDB
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.UsingSQLite = oldSQLite
		SetChannelRuntimeStateFunc(nil)
		initCol()
	})
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))

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
	oldDB := DB
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldSQLite := common.UsingSQLite
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	common.MemoryCacheEnabled = false
	common.UsingSQLite = true
	initCol()
	t.Cleanup(func() {
		DB = oldDB
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.UsingSQLite = oldSQLite
		SetChannelRuntimeStateFunc(nil)
		initCol()
	})
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))

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
	oldDB := DB
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldSQLite := common.UsingSQLite
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	common.MemoryCacheEnabled = false
	common.UsingSQLite = true
	initCol()
	t.Cleanup(func() {
		DB = oldDB
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.UsingSQLite = oldSQLite
		SetChannelRuntimeStateFunc(nil)
		initCol()
	})
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))

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
