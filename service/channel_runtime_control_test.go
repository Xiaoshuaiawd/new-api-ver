package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestForceOpenChannelRuntimeDoesNotChangeDBStatus(t *testing.T) {
	withChannelHealthTestSettings(t)
	withChannelHealthSelectionDB(t)

	result, err := ForceOpenChannelRuntime(9101, "operator isolate", time.Minute)
	require.NoError(t, err)
	require.Equal(t, ChannelHealthStateOpen, result.Snapshot.State)
	require.Equal(t, "operator isolate", result.Snapshot.Reason)
	require.False(t, IsChannelAvailable(9101))

	channel, err := model.CacheGetChannel(9101)
	require.NoError(t, err)
	require.Equal(t, common.ChannelStatusEnabled, channel.Status)
}

func TestClearChannelRuntimeIsolationRestoresHealthyOnlyWhenDBEnabled(t *testing.T) {
	withChannelHealthTestSettings(t)
	withChannelHealthSelectionDB(t)

	OpenChannel(9101, "runtime isolate")
	result, err := ClearChannelRuntimeIsolation(9101)
	require.NoError(t, err)
	require.Equal(t, ChannelHealthStateHealthy, result.Snapshot.State)
	require.True(t, IsChannelAvailable(9101))

	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 9101).Update("status", common.ChannelStatusManuallyDisabled).Error)
	model.CacheUpdateChannelStatus(9101, common.ChannelStatusManuallyDisabled)
	OpenChannel(9101, "runtime isolate")
	_, err = ClearChannelRuntimeIsolation(9101)
	require.Error(t, err)
	require.False(t, IsChannelAvailable(9101))
}

func TestClearChannelRuntimeAffinityDeletesReverseIndexedKeys(t *testing.T) {
	withChannelHealthTestSettings(t)

	cache := getChannelAffinityCache()
	key := "runtime-control-affinity-key"
	require.NoError(t, cache.SetWithTTL(key, 9301, time.Minute))
	RecordChannelAffinityKeyForChannelForTest(9301, key, time.Minute)
	t.Cleanup(func() {
		_, _ = cache.DeleteMany([]string{key})
	})

	deleted := ClearChannelAffinityByChannelID(9301)
	require.Equal(t, 1, deleted)
	_, found, err := cache.Get(key)
	require.NoError(t, err)
	require.False(t, found)
}
