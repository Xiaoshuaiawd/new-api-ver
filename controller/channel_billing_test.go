package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func channelBillingSettingsJSON(t *testing.T, cfg *dto.ChannelMultiplierMonitorConfig) string {
	t.Helper()
	raw, err := common.Marshal(dto.ChannelOtherSettings{
		UpstreamKeyMultiplier: cfg,
	})
	require.NoError(t, err)
	return string(raw)
}

func setupChannelBillingTestDB(t *testing.T) {
	t.Helper()
	oldDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
}

func TestUpdateChannelBalanceUsesConfiguredSub2APIAccountBalance(t *testing.T) {
	setupChannelBillingTestDB(t)

	var sawKeyBillingEndpoint bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			_, _ = w.Write([]byte(`{"data":{"access_token":"token-123"}}`))
		case "/api/v1/auth/me":
			_, _ = w.Write([]byte(`{"data":{"balance":26.54674067}}`))
		case "/v1/dashboard/billing/subscription", "/v1/dashboard/billing/usage":
			sawKeyBillingEndpoint = true
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	channel := &model.Channel{
		Id:      7301,
		Type:    constant.ChannelTypeCustom,
		Key:     "sk-channel-key",
		Name:    "account-balance",
		BaseURL: common.GetPointer(server.URL),
		OtherSettings: channelBillingSettingsJSON(t, &dto.ChannelMultiplierMonitorConfig{
			Enabled:  false,
			Format:   dto.ChannelMultiplierProviderFormatSub2API,
			BaseURL:  server.URL,
			Username: "alice@example.com",
			Password: "secret",
		}),
	}
	require.NoError(t, model.DB.Create(channel).Error)

	balance, err := updateChannelBalance(channel)

	require.NoError(t, err)
	assert.False(t, sawKeyBillingEndpoint)
	assert.Equal(t, 26.54674067, balance)

	var stored model.Channel
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	assert.Equal(t, 26.54674067, stored.Balance)
}
