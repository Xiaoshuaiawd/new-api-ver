package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDistributeRoutesCodexUserAgentToConfiguredChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldDB := model.DB
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldUsingSQLite := common.UsingSQLite
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB = db
	common.MemoryCacheEnabled = true
	common.UsingSQLite = true
	t.Cleanup(func() {
		model.DB = oldDB
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.UsingSQLite = oldUsingSQLite
		service.ResetCodexUserAgentRoutingForTest()
	})

	priority := int64(0)
	weight := uint(100)
	channels := []model.Channel{
		{
			Id:       1,
			Type:     constant.ChannelTypeOpenAI,
			Key:      "sk-normal",
			Status:   common.ChannelStatusEnabled,
			Name:     "normal",
			Weight:   &weight,
			Priority: &priority,
			Models:   "gpt-5-mini",
			Group:    "default",
		},
		{
			Id:       2,
			Type:     constant.ChannelTypeOpenAI,
			Key:      "sk-codex",
			Status:   common.ChannelStatusEnabled,
			Name:     "codex",
			Weight:   &weight,
			Priority: &priority,
			Models:   "gpt-5-mini",
			Group:    "default",
		},
	}
	require.NoError(t, db.Create(&channels).Error)
	for i := range channels {
		require.NoError(t, channels[i].AddAbilities(nil))
	}
	model.InitChannelCache()

	setting := operation_setting.GetCodexUserAgentRoutingSetting()
	oldSetting := *setting
	*setting = operation_setting.CodexUserAgentRoutingSetting{
		Enabled:             true,
		DefaultFakeCacheTTL: 300,
		Rules: []operation_setting.CodexUserAgentRouteRule{
			{
				Name:           "codex desktop",
				UserAgentRegex: []string{`(?i)^Codex Desktop/`},
				ModelRegex:     []string{"^gpt-5-mini$"},
				PathRegex:      []string{"/v1/responses"},
				Targets: []operation_setting.CodexUserAgentRouteTarget{
					{ChannelID: 2, Weight: 100},
				},
			},
		},
	}
	t.Cleanup(func() {
		*setting = oldSetting
	})
	service.SetCodexUserAgentRoutingRandForTest(func(n int) int { return 0 })

	router := gin.New()
	router.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	})
	router.Use(Distribute())
	router.POST("/v1/responses", func(c *gin.Context) {
		require.Equal(t, 2, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
		require.Equal(t, "sk-codex", common.GetContextKeyString(c, constant.ContextKeyChannelKey))
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5-mini","input":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Codex Desktop/0.140.0-alpha.2 (Windows 10.0.19045; x86_64) unknown (Codex Desktop; 26.609.41114)")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}
