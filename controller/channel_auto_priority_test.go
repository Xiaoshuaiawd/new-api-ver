package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestApplyChannelAutoPriorityHandlerReturnsSummary(t *testing.T) {
	setupChannelAutoPriorityControllerTestDB(t)
	service.ResetChannelMultiplierMonitorForTest()
	t.Cleanup(service.ResetChannelMultiplierMonitorForTest)

	setting := operation_setting.GetChannelAutoPrioritySetting()
	originalSetting := *setting
	*setting = operation_setting.ChannelAutoPrioritySetting{Enabled: false, MinWeight: 20, MaxWeight: 100}
	t.Cleanup(func() {
		*setting = originalSetting
	})

	priority := int64(1)
	weight := uint(1)
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:       1201,
		Type:     constant.ChannelTypeCustom,
		Key:      "sk-auto",
		Status:   common.ChannelStatusEnabled,
		Name:     "manual-apply",
		Priority: &priority,
		Weight:   &weight,
		Models:   "gpt-auto-priority",
		Group:    "default",
		OtherSettings: channelBillingSettingsJSON(t, &dto.ChannelMultiplierMonitorConfig{
			Enabled:  true,
			Format:   dto.ChannelMultiplierProviderFormatSub2API,
			BaseURL:  "https://upstream.example.com",
			Username: "alice@example.com",
			Password: "secret",
		}),
	}).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-auto-priority",
		ChannelId: 1201,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)
	now := time.Now()
	service.SetChannelMultiplierSnapshotForTest(service.ChannelMultiplierSnapshot{
		ChannelID:  1201,
		Enabled:    true,
		State:      service.ChannelMultiplierSnapshotHealthy,
		Multiplier: 0.2,
		ObservedAt: now.Unix(),
		ExpiresAt:  now.Add(5 * time.Minute).Unix(),
	})

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/auto_priority/apply", nil)

	ApplyChannelAutoPriority(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			UpdatedChannels int `json:"updated_channels"`
			SkippedChannels int `json:"skipped_channels"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, 1, response.Data.UpdatedChannels)
	assert.Equal(t, 0, response.Data.SkippedChannels)
}

func setupChannelAutoPriorityControllerTestDB(t *testing.T) {
	t.Helper()

	oldDB := model.DB
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		model.DB = oldDB
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		model.InitChannelCache()
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
}
