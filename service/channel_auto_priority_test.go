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

func withChannelAutoPriorityTestDB(t *testing.T) {
	t.Helper()

	oldDB := model.DB
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	common.MemoryCacheEnabled = true
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))

	t.Cleanup(func() {
		model.DB = oldDB
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
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
