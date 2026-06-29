package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestApplyChannelAutoPrioritySortsByMultiplierAndWeightsByHealth(t *testing.T) {
	withChannelAutoPriorityTestDB(t)
	withChannelHealthTestSettings(t)
	ResetChannelMultiplierMonitorForTest()
	t.Cleanup(ResetChannelMultiplierMonitorForTest)

	setting := operation_setting.GetChannelAutoPrioritySetting()
	originalSetting := *setting
	*setting = operation_setting.ChannelAutoPrioritySetting{Enabled: true, MinWeight: 20, MaxWeight: 100}
	t.Cleanup(func() {
		*setting = originalSetting
	})

	seedAutoPriorityChannel(t, 1001, "cheap", 9, 9)
	seedAutoPriorityChannel(t, 1002, "stable-middle", 9, 9)
	seedAutoPriorityChannel(t, 1003, "unstable-middle", 9, 9)
	seedAutoPriorityChannel(t, 1004, "expensive", 9, 9)
	seedAutoPriorityChannel(t, 1005, "no-snapshot", 77, 77)
	model.InitChannelCache()

	now := time.Now()
	setHealthyMultiplierSnapshot(1001, 0.2, now)
	setHealthyMultiplierSnapshot(1002, 0.5, now)
	setHealthyMultiplierSnapshot(1003, 0.5+0.0000004, now)
	setHealthyMultiplierSnapshot(1004, 1.0, now)

	for i := 0; i < 10; i++ {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: 1002})
		RecordFirstResponse(handle)
		RecordAttemptFinish(handle, ChannelAttemptResult{})
	}
	for i := 0; i < 5; i++ {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: 1003})
		RecordAttemptFinish(handle, ChannelAttemptResult{Error: channelHealthTestUpstreamError()})
	}

	summary, err := ApplyChannelAutoPriority(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 4, summary.UpdatedChannels)
	assert.Equal(t, 1, summary.SkippedChannels)

	cheap := loadAutoPriorityChannel(t, 1001)
	stableMiddle := loadAutoPriorityChannel(t, 1002)
	unstableMiddle := loadAutoPriorityChannel(t, 1003)
	expensive := loadAutoPriorityChannel(t, 1004)
	noSnapshot := loadAutoPriorityChannel(t, 1005)

	assert.Greater(t, cheap.GetPriority(), stableMiddle.GetPriority())
	assert.Equal(t, stableMiddle.GetPriority(), unstableMiddle.GetPriority())
	assert.Greater(t, stableMiddle.GetPriority(), expensive.GetPriority())
	assert.Greater(t, stableMiddle.GetWeight(), unstableMiddle.GetWeight())
	assert.Equal(t, int64(77), noSnapshot.GetPriority())
	assert.Equal(t, 77, noSnapshot.GetWeight())

	for _, channel := range []*model.Channel{cheap, stableMiddle, unstableMiddle, expensive} {
		var ability model.Ability
		require.NoError(t, model.DB.Where("channel_id = ? AND model = ?", channel.Id, "gpt-auto-priority").First(&ability).Error)
		require.NotNil(t, ability.Priority)
		assert.Equal(t, channel.GetPriority(), *ability.Priority)
		assert.Equal(t, uint(channel.GetWeight()), ability.Weight)
	}
}

func TestApplyChannelAutoPriorityUpdatesRoutePreference(t *testing.T) {
	withChannelAutoPriorityTestDB(t)
	withChannelHealthTestSettings(t)
	ResetChannelMultiplierMonitorForTest()
	t.Cleanup(ResetChannelMultiplierMonitorForTest)

	setting := operation_setting.GetChannelAutoPrioritySetting()
	originalSetting := *setting
	*setting = operation_setting.ChannelAutoPrioritySetting{Enabled: true, MinWeight: 20, MaxWeight: 100}
	t.Cleanup(func() {
		*setting = originalSetting
	})

	seedAutoPriorityChannel(t, 1101, "cheap", 1, 50)
	seedAutoPriorityChannel(t, 1102, "expensive", 100, 50)
	model.InitChannelCache()
	now := time.Now()
	setHealthyMultiplierSnapshot(1101, 0.2, now)
	setHealthyMultiplierSnapshot(1102, 1.0, now)

	_, err := ApplyChannelAutoPriority(context.Background())
	require.NoError(t, err)

	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		TokenGroup: "default",
		ModelName:  "gpt-auto-priority",
		Retry:      common.GetPointer(0),
	})

	require.NoError(t, err)
	require.Equal(t, "default", group)
	require.NotNil(t, channel)
	assert.Equal(t, 1101, channel.Id)
}

func TestApplyChannelAutoPrioritySkipsInvalidSnapshotsAndUnconfiguredChannels(t *testing.T) {
	withChannelAutoPriorityTestDB(t)
	withChannelHealthTestSettings(t)
	ResetChannelMultiplierMonitorForTest()
	t.Cleanup(ResetChannelMultiplierMonitorForTest)

	setting := operation_setting.GetChannelAutoPrioritySetting()
	originalSetting := *setting
	*setting = operation_setting.ChannelAutoPrioritySetting{Enabled: true, MinWeight: 20, MaxWeight: 100}
	t.Cleanup(func() {
		*setting = originalSetting
	})

	seedAutoPriorityChannel(t, 1201, "valid", 12, 12)
	seedAutoPriorityChannel(t, 1202, "error-snapshot", 55, 55)
	seedAutoPriorityChannelWithConfig(t, 1203, "unconfigured", 66, 66, nil)
	model.InitChannelCache()

	now := time.Now()
	setHealthyMultiplierSnapshot(1201, 0.2, now)
	SetChannelMultiplierSnapshotForTest(ChannelMultiplierSnapshot{
		ChannelID:  1202,
		Enabled:    true,
		State:      ChannelMultiplierSnapshotError,
		Multiplier: 0.1,
		ObservedAt: now.Unix(),
		ExpiresAt:  now.Add(5 * time.Minute).Unix(),
	})
	SetChannelMultiplierSnapshotForTest(ChannelMultiplierSnapshot{
		ChannelID:  1203,
		Enabled:    true,
		State:      ChannelMultiplierSnapshotHealthy,
		Multiplier: 0.01,
		ObservedAt: now.Unix(),
		ExpiresAt:  now.Add(5 * time.Minute).Unix(),
	})

	summary, err := ApplyChannelAutoPriority(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, summary.UpdatedChannels)
	assert.Equal(t, 1, summary.SkippedChannels)

	valid := loadAutoPriorityChannel(t, 1201)
	invalidSnapshot := loadAutoPriorityChannel(t, 1202)
	unconfigured := loadAutoPriorityChannel(t, 1203)

	assert.Equal(t, int64(1), valid.GetPriority())
	assert.Equal(t, 60, valid.GetWeight())
	assert.Equal(t, int64(55), invalidSnapshot.GetPriority())
	assert.Equal(t, 55, invalidSnapshot.GetWeight())
	assert.Equal(t, int64(66), unconfigured.GetPriority())
	assert.Equal(t, 66, unconfigured.GetWeight())

	assert.Equal(t, valid.GetPriority(), *loadAutoPriorityAbility(t, 1201).Priority)
	assert.Equal(t, uint(valid.GetWeight()), loadAutoPriorityAbility(t, 1201).Weight)
	assert.Equal(t, int64(55), *loadAutoPriorityAbility(t, 1202).Priority)
	assert.Equal(t, uint(55), loadAutoPriorityAbility(t, 1202).Weight)
	assert.Equal(t, int64(66), *loadAutoPriorityAbility(t, 1203).Priority)
	assert.Equal(t, uint(66), loadAutoPriorityAbility(t, 1203).Weight)
}

func TestChannelAutoPriorityLatencyStatsUseRecentFRTLogs(t *testing.T) {
	withChannelAutoPriorityTestDB(t)

	setting := operation_setting.ChannelAutoPrioritySetting{
		LatencyGuardEnabled:       true,
		LatencyThresholdSeconds:   10,
		LatencyWindowMinutes:      10,
		LatencyMinSamples:         2,
		LatencySlowRatioThreshold: 0.30,
	}
	now := time.Unix(1_700_000_000, 0)
	seedAutoPriorityConsumeLog(t, 1301, now.Add(-time.Minute), 12_000)
	seedAutoPriorityConsumeLog(t, 1301, now.Add(-2*time.Minute), 11_000)
	seedAutoPriorityConsumeLog(t, 1301, now.Add(-3*time.Minute), 5_000)
	seedAutoPriorityConsumeLog(t, 1301, now.Add(-4*time.Minute), 0)
	seedAutoPriorityConsumeLog(t, 1301, now.Add(-5*time.Minute), -1)
	seedAutoPriorityConsumeLogOther(t, 1301, now.Add(-6*time.Minute), `{"frt":"oops"}`)
	seedAutoPriorityConsumeLogOther(t, 1301, now.Add(-7*time.Minute), `not-json`)
	seedAutoPriorityConsumeLog(t, 1301, now.Add(-20*time.Minute), 20_000)
	seedAutoPriorityConsumeLog(t, 1302, now.Add(-time.Minute), 20_000)

	stats, err := loadChannelAutoPriorityLatencyStats(context.Background(), []int{1301}, setting, now)

	require.NoError(t, err)
	require.Contains(t, stats, 1301)
	assert.Equal(t, 3, stats[1301].Samples)
	assert.Equal(t, 2, stats[1301].SlowSamples)
	assert.InDelta(t, 2.0/3.0, stats[1301].SlowRatio, 0.0001)
}

func TestApplyChannelAutoPriorityDegradesAndRecoversSlowFirstResponseChannel(t *testing.T) {
	withChannelAutoPriorityTestDB(t)
	withChannelHealthTestSettings(t)
	ResetChannelMultiplierMonitorForTest()
	resetChannelAutoPriorityLatencyGuardForTest()
	t.Cleanup(ResetChannelMultiplierMonitorForTest)

	setting := operation_setting.GetChannelAutoPrioritySetting()
	originalSetting := *setting
	*setting = operation_setting.ChannelAutoPrioritySetting{
		Enabled:                       true,
		MinWeight:                     20,
		MaxWeight:                     100,
		LatencyGuardEnabled:           true,
		LatencyThresholdSeconds:       10,
		LatencyWindowMinutes:          10,
		LatencyMinSamples:             4,
		LatencySlowRatioThreshold:     0.50,
		LatencyRecoveryRatioThreshold: 0.20,
		LatencyRetainedWeightPercent:  20,
		LatencyPriorityPenalty:        1,
	}
	t.Cleanup(func() {
		*setting = originalSetting
		resetChannelAutoPriorityLatencyGuardForTest()
	})

	seedAutoPriorityChannel(t, 1301, "cheap-slow", 9, 9)
	seedAutoPriorityChannel(t, 1302, "expensive-stable", 9, 9)
	model.InitChannelCache()

	now := time.Now()
	setHealthyMultiplierSnapshot(1301, 0.2, now)
	setHealthyMultiplierSnapshot(1302, 1.0, now)
	for _, frtMs := range []float64{12_000, 13_000, 11_000, 3_000} {
		seedAutoPriorityConsumeLog(t, 1301, now.Add(-time.Minute), frtMs)
	}
	for _, frtMs := range []float64{2_000, 2_500, 3_000, 3_500} {
		seedAutoPriorityConsumeLog(t, 1302, now.Add(-time.Minute), frtMs)
	}

	_, err := ApplyChannelAutoPriority(context.Background())
	require.NoError(t, err)

	degradedCheap := loadAutoPriorityChannel(t, 1301)
	stableExpensive := loadAutoPriorityChannel(t, 1302)
	assert.Equal(t, stableExpensive.GetPriority(), degradedCheap.GetPriority())
	assert.Equal(t, 20, degradedCheap.GetWeight())
	assert.Equal(t, uint(degradedCheap.GetWeight()), loadAutoPriorityAbility(t, 1301).Weight)

	require.NoError(t, model.LOG_DB.Where("channel_id = ?", 1301).Delete(&model.Log{}).Error)
	for _, frtMs := range []float64{2_000, 2_500, 3_000, 3_500} {
		seedAutoPriorityConsumeLog(t, 1301, time.Now().Add(-time.Minute), frtMs)
	}

	_, err = ApplyChannelAutoPriority(context.Background())
	require.NoError(t, err)

	recoveredCheap := loadAutoPriorityChannel(t, 1301)
	assert.Greater(t, recoveredCheap.GetPriority(), loadAutoPriorityChannel(t, 1302).GetPriority())
	assert.Greater(t, recoveredCheap.GetWeight(), degradedCheap.GetWeight())
	assert.Equal(t, recoveredCheap.GetPriority(), *loadAutoPriorityAbility(t, 1301).Priority)
}

func TestApplyChannelAutoPriorityLatencyGuardChangesRoutePreference(t *testing.T) {
	withChannelAutoPriorityTestDB(t)
	withChannelHealthTestSettings(t)
	ResetChannelMultiplierMonitorForTest()
	resetChannelAutoPriorityLatencyGuardForTest()
	t.Cleanup(ResetChannelMultiplierMonitorForTest)

	setting := operation_setting.GetChannelAutoPrioritySetting()
	originalSetting := *setting
	*setting = operation_setting.ChannelAutoPrioritySetting{
		Enabled:                       true,
		MinWeight:                     20,
		MaxWeight:                     100,
		LatencyGuardEnabled:           true,
		LatencyThresholdSeconds:       10,
		LatencyWindowMinutes:          10,
		LatencyMinSamples:             4,
		LatencySlowRatioThreshold:     0.50,
		LatencyRecoveryRatioThreshold: 0.20,
		LatencyRetainedWeightPercent:  20,
		LatencyPriorityPenalty:        2,
	}
	t.Cleanup(func() {
		*setting = originalSetting
		resetChannelAutoPriorityLatencyGuardForTest()
	})

	seedAutoPriorityChannel(t, 1311, "cheap-slow", 1, 50)
	seedAutoPriorityChannel(t, 1312, "middle-stable", 1, 50)
	seedAutoPriorityChannel(t, 1313, "expensive-stable", 1, 50)
	model.InitChannelCache()

	now := time.Now()
	setHealthyMultiplierSnapshot(1311, 0.2, now)
	setHealthyMultiplierSnapshot(1312, 0.5, now)
	setHealthyMultiplierSnapshot(1313, 1.0, now)
	for _, frtMs := range []float64{12_000, 13_000, 11_000, 14_000} {
		seedAutoPriorityConsumeLog(t, 1311, now.Add(-time.Minute), frtMs)
	}

	_, err := ApplyChannelAutoPriority(context.Background())
	require.NoError(t, err)

	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		TokenGroup: "default",
		ModelName:  "gpt-auto-priority",
		Retry:      common.GetPointer(0),
	})

	require.NoError(t, err)
	require.Equal(t, "default", group)
	require.NotNil(t, channel)
	assert.Equal(t, 1312, channel.Id)
	assert.Greater(t, loadAutoPriorityChannel(t, 1312).GetPriority(), loadAutoPriorityChannel(t, 1311).GetPriority())
}

func withChannelAutoPriorityTestDB(t *testing.T) {
	t.Helper()

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldMainDatabaseType := common.MainDatabaseType()
	oldLogDatabaseType := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB = db
	model.LOG_DB = db
	common.MemoryCacheEnabled = true
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Log{}))

	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.SetDatabaseTypes(oldMainDatabaseType, oldLogDatabaseType)
		_ = sqlDB.Close()
		model.InitChannelCache()
	})
}

func seedAutoPriorityChannel(t *testing.T, id int, name string, priority int64, weight uint) {
	t.Helper()

	seedAutoPriorityChannelWithConfig(t, id, name, priority, weight, &dto.ChannelMultiplierMonitorConfig{
		Enabled:  true,
		Format:   dto.ChannelMultiplierProviderFormatSub2API,
		BaseURL:  "https://upstream.example.com",
		Username: "alice@example.com",
		Password: "secret",
	})
}

func seedAutoPriorityChannelWithConfig(t *testing.T, id int, name string, priority int64, weight uint, cfg *dto.ChannelMultiplierMonitorConfig) {
	t.Helper()

	otherSettings := "{}"
	if cfg != nil {
		otherSettings = channelMultiplierSettingsJSON(t, cfg)
	}
	channel := &model.Channel{
		Id:            id,
		Type:          constant.ChannelTypeCustom,
		Key:           "sk-auto",
		Status:        common.ChannelStatusEnabled,
		Name:          name,
		Priority:      &priority,
		Weight:        &weight,
		Models:        "gpt-auto-priority",
		Group:         "default",
		OtherSettings: otherSettings,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-auto-priority",
		ChannelId: id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)
}

func setHealthyMultiplierSnapshot(channelID int, multiplier float64, now time.Time) {
	SetChannelMultiplierSnapshotForTest(ChannelMultiplierSnapshot{
		ChannelID:  channelID,
		Enabled:    true,
		State:      ChannelMultiplierSnapshotHealthy,
		Multiplier: multiplier,
		ObservedAt: now.Unix(),
		ExpiresAt:  now.Add(5 * time.Minute).Unix(),
	})
}

func loadAutoPriorityChannel(t *testing.T, id int) *model.Channel {
	t.Helper()

	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, id).Error)
	return &channel
}

func loadAutoPriorityAbility(t *testing.T, channelID int) model.Ability {
	t.Helper()

	var ability model.Ability
	require.NoError(t, model.DB.Where("channel_id = ? AND model = ?", channelID, "gpt-auto-priority").First(&ability).Error)
	return ability
}

func seedAutoPriorityConsumeLog(t *testing.T, channelID int, createdAt time.Time, firstResponseMs float64) {
	t.Helper()

	other := map[string]any{}
	if firstResponseMs >= 0 {
		other["frt"] = firstResponseMs
	}
	seedAutoPriorityConsumeLogOther(t, channelID, createdAt, common.MapToJsonStr(other))
}

func seedAutoPriorityConsumeLogOther(t *testing.T, channelID int, createdAt time.Time, other string) {
	t.Helper()

	require.NoError(t, model.LOG_DB.Create(&model.Log{
		CreatedAt: createdAt.Unix(),
		Type:      model.LogTypeConsume,
		ChannelId: channelID,
		Other:     other,
	}).Error)
}
