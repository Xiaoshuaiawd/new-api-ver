package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOpenAIUpstreamKeyLimiterAllowsThreeRequestsPerMinute(t *testing.T) {
	limiter := NewMemoryOpenAIUpstreamKeyLimiter(time.Unix(1000, 0))
	cfg := setting.OpenAIUpstreamKeyLimitConfig{
		RPM: 3,
		TPM: 50000,
		RPD: 50,
		TPD: 200000,
	}

	for i := 0; i < 3; i++ {
		res, err := limiter.Reserve(OpenAIUpstreamKeyLimitRequest{
			ChannelID:       1,
			Key:             "sk-test",
			EstimatedTokens: 100,
			Config:          cfg,
			Now:             time.Unix(1000+int64(i), 0),
		})
		require.NoError(t, err)
		require.True(t, res.Allowed)
		require.NotEmpty(t, res.ReservationID)
	}

	res, err := limiter.Reserve(OpenAIUpstreamKeyLimitRequest{
		ChannelID:       1,
		Key:             "sk-test",
		EstimatedTokens: 100,
		Config:          cfg,
		Now:             time.Unix(1003, 0),
	})
	require.NoError(t, err)
	require.False(t, res.Allowed)
	require.Equal(t, OpenAIUpstreamKeyLimitRPM, res.LimitType)
	require.Equal(t, int64(1060), res.RetryAt.Unix())
}

func TestOpenAIUpstreamKeyLimiterUsesTokenWindowsAndCorrection(t *testing.T) {
	limiter := NewMemoryOpenAIUpstreamKeyLimiter(time.Unix(2000, 0))
	cfg := setting.OpenAIUpstreamKeyLimitConfig{
		RPM: 10,
		TPM: 50000,
		RPD: 50,
		TPD: 200000,
	}

	first, err := limiter.Reserve(OpenAIUpstreamKeyLimitRequest{
		ChannelID:       1,
		Key:             "sk-test",
		EstimatedTokens: 49000,
		Config:          cfg,
		Now:             time.Unix(2000, 0),
	})
	require.NoError(t, err)
	require.True(t, first.Allowed)

	denied, err := limiter.Reserve(OpenAIUpstreamKeyLimitRequest{
		ChannelID:       1,
		Key:             "sk-test",
		EstimatedTokens: 1001,
		Config:          cfg,
		Now:             time.Unix(2001, 0),
	})
	require.NoError(t, err)
	require.False(t, denied.Allowed)
	require.Equal(t, OpenAIUpstreamKeyLimitTPM, denied.LimitType)

	err = limiter.Commit(OpenAIUpstreamKeyLimitCommit{
		ReservationID: first.ReservationID,
		ActualTokens:  1000,
		Now:           time.Unix(2002, 0),
	})
	require.NoError(t, err)

	allowed, err := limiter.Reserve(OpenAIUpstreamKeyLimitRequest{
		ChannelID:       1,
		Key:             "sk-test",
		EstimatedTokens: 49000,
		Config:          cfg,
		Now:             time.Unix(2003, 0),
	})
	require.NoError(t, err)
	require.True(t, allowed.Allowed)
}

func TestOpenAIUpstreamKeyLimiterCanReleaseFailedReservation(t *testing.T) {
	limiter := NewMemoryOpenAIUpstreamKeyLimiter(time.Unix(2500, 0))
	cfg := setting.OpenAIUpstreamKeyLimitConfig{
		RPM: 10,
		TPM: 50000,
		RPD: 50,
		TPD: 200000,
	}

	first, err := limiter.Reserve(OpenAIUpstreamKeyLimitRequest{
		ChannelID:       1,
		Key:             "sk-test",
		EstimatedTokens: 49000,
		Config:          cfg,
		Now:             time.Unix(2500, 0),
	})
	require.NoError(t, err)
	require.True(t, first.Allowed)

	err = limiter.Commit(OpenAIUpstreamKeyLimitCommit{
		ReservationID: first.ReservationID,
		ActualTokens:  0,
		Now:           time.Unix(2501, 0),
	})
	require.NoError(t, err)

	allowed, err := limiter.Reserve(OpenAIUpstreamKeyLimitRequest{
		ChannelID:       1,
		Key:             "sk-test",
		EstimatedTokens: 50000,
		Config:          cfg,
		Now:             time.Unix(2502, 0),
	})
	require.NoError(t, err)
	require.True(t, allowed.Allowed)
}

func TestOpenAIUpstreamKeyLimiterCanCorrectZeroTokenEstimateUpward(t *testing.T) {
	limiter := NewMemoryOpenAIUpstreamKeyLimiter(time.Unix(2700, 0))
	cfg := setting.OpenAIUpstreamKeyLimitConfig{
		RPM: 10,
		TPM: 50000,
		RPD: 50,
		TPD: 200000,
	}

	first, err := limiter.Reserve(OpenAIUpstreamKeyLimitRequest{
		ChannelID:       1,
		Key:             "sk-test",
		EstimatedTokens: 0,
		Config:          cfg,
		Now:             time.Unix(2700, 0),
	})
	require.NoError(t, err)
	require.True(t, first.Allowed)

	err = limiter.Commit(OpenAIUpstreamKeyLimitCommit{
		ReservationID: first.ReservationID,
		ActualTokens:  50000,
		Now:           time.Unix(2701, 0),
	})
	require.NoError(t, err)

	denied, err := limiter.Reserve(OpenAIUpstreamKeyLimitRequest{
		ChannelID:       1,
		Key:             "sk-test",
		EstimatedTokens: 1,
		Config:          cfg,
		Now:             time.Unix(2702, 0),
	})
	require.NoError(t, err)
	require.False(t, denied.Allowed)
	require.Equal(t, OpenAIUpstreamKeyLimitTPM, denied.LimitType)
}

func TestOpenAIUpstreamKeyLimiterUsesRollingDailyWindows(t *testing.T) {
	limiter := NewMemoryOpenAIUpstreamKeyLimiter(time.Unix(3000, 0))
	cfg := setting.OpenAIUpstreamKeyLimitConfig{
		RPM: 100,
		TPM: 50000,
		RPD: 50,
		TPD: 200000,
	}

	for i := 0; i < 50; i++ {
		res, err := limiter.Reserve(OpenAIUpstreamKeyLimitRequest{
			ChannelID:       1,
			Key:             "sk-test",
			EstimatedTokens: 100,
			Config:          cfg,
			Now:             time.Unix(3000+int64(i*120), 0),
		})
		require.NoError(t, err)
		require.True(t, res.Allowed)
	}

	denied, err := limiter.Reserve(OpenAIUpstreamKeyLimitRequest{
		ChannelID:       1,
		Key:             "sk-test",
		EstimatedTokens: 100,
		Config:          cfg,
		Now:             time.Unix(3000+int64(50*120), 0),
	})
	require.NoError(t, err)
	require.False(t, denied.Allowed)
	require.Equal(t, OpenAIUpstreamKeyLimitRPD, denied.LimitType)
	require.Equal(t, int64(3000+24*60*60), denied.RetryAt.Unix())
}

func TestOpenAIUpstreamKeyLimiterUsesRollingDailyTokenWindows(t *testing.T) {
	limiter := NewMemoryOpenAIUpstreamKeyLimiter(time.Unix(3200, 0))
	cfg := setting.OpenAIUpstreamKeyLimitConfig{
		RPM: 100,
		TPM: 300000,
		RPD: 100,
		TPD: 200000,
	}

	first, err := limiter.Reserve(OpenAIUpstreamKeyLimitRequest{
		ChannelID:       1,
		Key:             "sk-test",
		EstimatedTokens: 200000,
		Config:          cfg,
		Now:             time.Unix(3200, 0),
	})
	require.NoError(t, err)
	require.True(t, first.Allowed)

	denied, err := limiter.Reserve(OpenAIUpstreamKeyLimitRequest{
		ChannelID:       1,
		Key:             "sk-test",
		EstimatedTokens: 1,
		Config:          cfg,
		Now:             time.Unix(3201, 0),
	})
	require.NoError(t, err)
	require.False(t, denied.Allowed)
	require.Equal(t, OpenAIUpstreamKeyLimitTPD, denied.LimitType)
	require.Equal(t, int64(3200+24*60*60), denied.RetryAt.Unix())
}

func TestOpenAIUpstreamKeyLimitOnlyAppliesToOfficialOpenAI(t *testing.T) {
	require.False(t, ShouldApplyOpenAIUpstreamKeyLimit(nil))

	official := &model.Channel{Type: constant.ChannelTypeOpenAI}
	require.True(t, ShouldApplyOpenAIUpstreamKeyLimit(official))

	defaultBaseURL := constant.ChannelBaseURLs[constant.ChannelTypeOpenAI]
	official.BaseURL = &defaultBaseURL
	require.True(t, ShouldApplyOpenAIUpstreamKeyLimit(official))

	customBaseURL := "https://example.com/v1"
	official.BaseURL = &customBaseURL
	require.False(t, ShouldApplyOpenAIUpstreamKeyLimit(official))

	azure := &model.Channel{Type: constant.ChannelTypeAzure}
	require.False(t, ShouldApplyOpenAIUpstreamKeyLimit(azure))
}

func TestOpenAIUpstreamKeyLimitConfigParsesDefaultsAndUpdates(t *testing.T) {
	oldConfig := setting.OpenAIUpstreamKeyLimitConfigValue
	t.Cleanup(func() {
		setting.OpenAIUpstreamKeyLimitConfigValue = oldConfig
	})

	require.NoError(t, setting.UpdateOpenAIUpstreamKeyLimitConfigByJSONString(`{"rpm":4,"tpm":60000,"rpd":60,"tpd":300000,"daily_window":"rolling_24h"}`))
	require.Equal(t, 4, setting.OpenAIUpstreamKeyLimitConfigValue.RPM)
	require.Equal(t, 60000, setting.OpenAIUpstreamKeyLimitConfigValue.TPM)
	require.Equal(t, "rolling_24h", setting.OpenAIUpstreamKeyLimitConfigValue.DailyWindow)

	defaultJSON := setting.OpenAIUpstreamKeyLimitConfig2JSONString()
	require.Contains(t, defaultJSON, `"rpm"`)
	require.NotContains(t, defaultJSON, "sk-test")
}

func TestOpenAIUpstreamKeyLimiterIgnoresDisabledConfig(t *testing.T) {
	limiter := NewMemoryOpenAIUpstreamKeyLimiter(time.Unix(4000, 0))
	cfg := setting.OpenAIUpstreamKeyLimitConfig{}

	for i := 0; i < 10; i++ {
		res, err := limiter.Reserve(OpenAIUpstreamKeyLimitRequest{
			ChannelID:       1,
			Key:             "sk-test",
			EstimatedTokens: 1_000_000,
			Config:          cfg,
			Now:             time.Unix(4000, 0),
		})
		require.NoError(t, err)
		require.True(t, res.Allowed)
	}
}

func TestOpenAIUpstreamKeyLimitFingerprintsDoNotExposeRawKeys(t *testing.T) {
	fingerprint := OpenAIUpstreamKeyFingerprint("sk-secret-value")
	require.NotContains(t, fingerprint, "sk-secret-value")
	require.NotContains(t, fingerprint, "secret")
	require.NotEmpty(t, fingerprint)
	require.Equal(t, fingerprint, OpenAIUpstreamKeyFingerprint("sk-secret-value"))
	require.NotEqual(t, fingerprint, OpenAIUpstreamKeyFingerprint("sk-other-value"))
}

func TestOpenAIUpstreamKeyLimitReservationTotalUsesEstimateAndMaxOutput(t *testing.T) {
	require.Equal(t, 120, EstimateOpenAIUpstreamTotalTokens(100, 20))
	require.Equal(t, 100, EstimateOpenAIUpstreamTotalTokens(100, 0))
	require.Equal(t, 0, EstimateOpenAIUpstreamTotalTokens(-1, -1))
}

func TestOpenAIUpstreamKeyLimitUsesCommonJSONWrapper(t *testing.T) {
	data, err := common.Marshal(setting.DefaultOpenAIUpstreamKeyLimitConfig())
	require.NoError(t, err)
	require.Contains(t, string(data), "rolling_24h")
}

func TestRestoreExpiredOpenAIUpstreamKeyLimitsRestoresEnabledMultiKeyChannel(t *testing.T) {
	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldSQLite := common.UsingSQLite
	oldMySQL := common.UsingMySQL
	oldPostgreSQL := common.UsingPostgreSQL
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldSQLite
		common.UsingMySQL = oldMySQL
		common.UsingPostgreSQL = oldPostgreSQL
	})

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	db, err := gorm.Open(sqlite.Open("file:openai_key_limit_restore_enabled_multikey?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))

	baseURL := constant.ChannelBaseURLs[constant.ChannelTypeOpenAI]
	channel := &model.Channel{
		Id:      901,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "sk-a\nsk-b",
		Status:  common.ChannelStatusEnabled,
		Name:    "official-multi",
		BaseURL: &baseURL,
		Group:   "default",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
			},
			MultiKeyDisabledUntil: map[int]int64{
				0: 100,
			},
			MultiKeyDisabledReason: map[int]string{
				0: "rpm limit",
			},
			MultiKeyDisabledTime: map[int]int64{
				0: 1,
			},
		},
	}
	require.NoError(t, db.Create(channel).Error)

	RestoreExpiredOpenAIUpstreamKeyLimits(time.Unix(101, 0))

	var restored model.Channel
	require.NoError(t, db.First(&restored, 901).Error)
	require.NotContains(t, restored.ChannelInfo.MultiKeyStatusList, 0)
	require.NotContains(t, restored.ChannelInfo.MultiKeyDisabledUntil, 0)
	require.Equal(t, common.ChannelStatusEnabled, restored.Status)
}
