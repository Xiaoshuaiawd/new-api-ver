package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func channelMultiplierSettingsJSON(t *testing.T, cfg *dto.ChannelMultiplierMonitorConfig) string {
	t.Helper()
	raw, err := common.Marshal(dto.ChannelOtherSettings{
		UpstreamKeyMultiplier:   cfg,
		DisableTaskPollingSleep: true,
	})
	require.NoError(t, err)
	return string(raw)
}

func TestRedactChannelMultiplierMonitorSettings(t *testing.T) {
	rawSettings, err := common.Marshal(map[string]any{
		"upstream_key_multiplier": map[string]any{
			"enabled":  true,
			"format":   dto.ChannelMultiplierProviderFormatSub2API,
			"base_url": "https://upstream.example.com",
			"username": "alice@example.com",
			"password": "secret",
		},
		"disable_task_polling_sleep": true,
		"unknown_vendor_setting":     "keep-me",
	})
	require.NoError(t, err)
	settings := string(rawSettings)

	redacted := RedactChannelMultiplierMonitorSettings(settings)

	var parsed dto.ChannelOtherSettings
	require.NoError(t, common.UnmarshalJsonStr(redacted, &parsed))
	require.NotNil(t, parsed.UpstreamKeyMultiplier)
	assert.Empty(t, parsed.UpstreamKeyMultiplier.Password)
	assert.Equal(t, "alice@example.com", parsed.UpstreamKeyMultiplier.Username)
	assert.True(t, parsed.DisableTaskPollingSleep)

	var rawParsed map[string]any
	require.NoError(t, common.UnmarshalJsonStr(redacted, &rawParsed))
	assert.Equal(t, "keep-me", rawParsed["unknown_vendor_setting"])
}

func TestMergeChannelMultiplierMonitorSecretPreservesBlankPassword(t *testing.T) {
	previous := channelMultiplierSettingsJSON(t, &dto.ChannelMultiplierMonitorConfig{
		Enabled:  true,
		Format:   dto.ChannelMultiplierProviderFormatNewAPI,
		BaseURL:  "https://upstream.example.com",
		Username: "alice",
		Password: "existing-secret",
	})
	rawNext, err := common.Marshal(map[string]any{
		"upstream_key_multiplier": map[string]any{
			"enabled":  true,
			"format":   dto.ChannelMultiplierProviderFormatNewAPI,
			"base_url": "https://upstream.example.com",
			"username": "alice",
			"password": "",
		},
		"unknown_vendor_setting": "keep-me",
	})
	require.NoError(t, err)
	next := string(rawNext)

	merged := MergeChannelMultiplierMonitorSecret(next, previous)

	var parsed dto.ChannelOtherSettings
	require.NoError(t, common.UnmarshalJsonStr(merged, &parsed))
	require.NotNil(t, parsed.UpstreamKeyMultiplier)
	assert.Equal(t, "existing-secret", parsed.UpstreamKeyMultiplier.Password)

	var rawParsed map[string]any
	require.NoError(t, common.UnmarshalJsonStr(merged, &rawParsed))
	assert.Equal(t, "keep-me", rawParsed["unknown_vendor_setting"])
}

func TestProbeSub2APIChannelMultiplierMatchesCurrentKey(t *testing.T) {
	var sawLogin bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			sawLogin = true
			assert.Equal(t, http.MethodPost, r.Method)
			_, _ = w.Write([]byte(`{"data":{"access_token":"token-123"}}`))
		case "/api/v1/keys":
			assert.Equal(t, "Bearer token-123", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"data":{"items":[{"id":1,"key":"sk-other","group":{"name":"Other","rate_multiplier":0.65}},{"id":581,"key":"sk-current","group":{"name":"Pro","rate_multiplier":0.08}}]}}`))
		case "/api/v1/auth/me":
			assert.Equal(t, "Bearer token-123", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"data":{"balance":26.5}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	channel := &model.Channel{
		Id:      1001,
		Type:    constant.ChannelTypeCustom,
		Key:     "sk-current",
		Name:    "sub2api",
		BaseURL: common.GetPointer("https://proxy.example.com"),
		OtherSettings: channelMultiplierSettingsJSON(t, &dto.ChannelMultiplierMonitorConfig{
			Enabled:  true,
			Format:   dto.ChannelMultiplierProviderFormatSub2API,
			BaseURL:  server.URL,
			Username: "alice@example.com",
			Password: "secret",
		}),
	}

	snapshot, err := probeChannelMultiplier(context.Background(), channel)

	require.NoError(t, err)
	assert.True(t, sawLogin)
	assert.Equal(t, 1001, snapshot.ChannelID)
	assert.Equal(t, ChannelMultiplierSnapshotHealthy, snapshot.State)
	assert.Equal(t, 0.08, snapshot.Multiplier)
	assert.Equal(t, 26.5, snapshot.Balance)
	assert.Equal(t, "Pro", snapshot.ObservedGroup)
	assert.Equal(t, "581", snapshot.ObservedTokenID)
}

func TestProbeNewAPIChannelMultiplierUsesGroupRatioFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/login":
			assert.Equal(t, http.MethodPost, r.Method)
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "session-123", Path: "/"})
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":4718}}`))
		case "/api/token/":
			assert.Equal(t, "4718", r.Header.Get("new-api-user"))
			assert.Contains(t, r.Header.Get("Cookie"), "session=session-123")
			_, _ = w.Write([]byte(`{"success":true,"data":{"items":[{"id":8889,"key":"lcuN**********OhAB","group":"gptproo","created_time":1781174251}]}}`))
		case "/api/user/self/groups":
			assert.Equal(t, "4718", r.Header.Get("new-api-user"))
			_, _ = w.Write([]byte(`{"success":true,"data":{"gptproo":{"desc":"pro","ratio":0.07}}}`))
		case "/api/user/self":
			assert.Equal(t, "4718", r.Header.Get("new-api-user"))
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota":1000000}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	channel := &model.Channel{
		Id:   1002,
		Type: constant.ChannelTypeCustom,
		Key:  "lcuNxxxxxxOhAB",
		Name: "new-api",
		OtherSettings: channelMultiplierSettingsJSON(t, &dto.ChannelMultiplierMonitorConfig{
			Enabled:  true,
			Format:   dto.ChannelMultiplierProviderFormatNewAPI,
			BaseURL:  server.URL,
			Username: "alice",
			Password: "secret",
		}),
	}

	snapshot, err := probeChannelMultiplier(context.Background(), channel)

	require.NoError(t, err)
	assert.Equal(t, 1002, snapshot.ChannelID)
	assert.Equal(t, ChannelMultiplierSnapshotHealthy, snapshot.State)
	assert.Equal(t, 0.07, snapshot.Multiplier)
	assert.Equal(t, 2.0, snapshot.Balance)
	assert.Equal(t, "gptproo", snapshot.ObservedGroup)
	assert.Equal(t, "8889", snapshot.ObservedTokenID)
}
