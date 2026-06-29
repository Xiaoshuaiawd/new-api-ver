package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

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

func TestChannelMultiplierMonitorIntervalUsesSystemSetting(t *testing.T) {
	setting := operation_setting.GetChannelMultiplierMonitorSetting()
	original := *setting
	t.Cleanup(func() {
		*setting = original
	})

	setting.IntervalMinutes = 7
	assert.Equal(t, 7, int(channelMultiplierMonitorInterval().Minutes()))

	setting.IntervalMinutes = 0
	assert.Equal(t, 2, int(channelMultiplierMonitorInterval().Minutes()))
}

func TestRefreshChannelMultiplierSnapshotStoresResult(t *testing.T) {
	ResetChannelMultiplierMonitorForTest()
	t.Cleanup(ResetChannelMultiplierMonitorForTest)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/login":
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":4718}}`))
		case "/api/token/":
			_, _ = w.Write([]byte(`{"success":true,"data":{"items":[{"id":8889,"key":"sk-current","group":{"name":"pro","ratio":0.07}}]}}`))
		case "/api/user/self":
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota":1000000}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	channel := &model.Channel{
		Id:   1201,
		Type: constant.ChannelTypeCustom,
		Key:  "sk-current",
		Name: "refresh-now",
		OtherSettings: channelMultiplierSettingsJSON(t, &dto.ChannelMultiplierMonitorConfig{
			Enabled:  true,
			Format:   dto.ChannelMultiplierProviderFormatNewAPI,
			BaseURL:  server.URL,
			Username: "alice",
			Password: "secret",
		}),
	}

	snapshot, err := RefreshChannelMultiplierSnapshot(context.Background(), channel)

	require.NoError(t, err)
	assert.Equal(t, ChannelMultiplierSnapshotHealthy, snapshot.State)
	assert.Equal(t, 0.07, snapshot.Multiplier)

	stored, ok := GetChannelMultiplierSnapshot(channel.Id)
	require.True(t, ok)
	assert.Equal(t, snapshot.Multiplier, stored.Multiplier)
	assert.Equal(t, snapshot.ObservedAt, stored.ObservedAt)
}

func TestRunChannelMultiplierMonitorOnceUpdatesChannelBalances(t *testing.T) {
	withChannelMultiplierMonitorTestDB(t)
	ResetChannelMultiplierMonitorForTest()
	t.Cleanup(ResetChannelMultiplierMonitorForTest)

	originalProbe := channelMultiplierProbe
	t.Cleanup(func() {
		channelMultiplierProbe = originalProbe
	})
	autoPrioritySetting := operation_setting.GetChannelAutoPrioritySetting()
	originalAutoPrioritySetting := *autoPrioritySetting
	*autoPrioritySetting = operation_setting.ChannelAutoPrioritySetting{Enabled: false, MinWeight: 20, MaxWeight: 100}
	t.Cleanup(func() {
		*autoPrioritySetting = originalAutoPrioritySetting
	})

	channelMultiplierProbe = func(ctx context.Context, channel *model.Channel) (ChannelMultiplierSnapshot, error) {
		return buildHealthyMultiplierSnapshot(
			channel,
			GetChannelMultiplierMonitorConfig(channel),
			0.5,
			float64(channel.Id)+0.25,
			"default",
			"token-1",
		), nil
	}

	seedChannelMultiplierMonitorChannel(t, 1401)
	seedChannelMultiplierMonitorChannel(t, 1402)

	runChannelMultiplierMonitorOnce()

	channelA := loadChannelMultiplierMonitorChannel(t, 1401)
	channelB := loadChannelMultiplierMonitorChannel(t, 1402)
	assert.Equal(t, 1401.25, channelA.Balance)
	assert.Greater(t, channelA.BalanceUpdatedTime, int64(0))
	assert.Equal(t, 1402.25, channelB.Balance)
	assert.Greater(t, channelB.BalanceUpdatedTime, int64(0))

	snapshot, ok := GetChannelMultiplierSnapshot(1401)
	require.True(t, ok)
	assert.Equal(t, ChannelMultiplierSnapshotHealthy, snapshot.State)
	assert.Equal(t, 1401.25, snapshot.Balance)
}

func TestFetchChannelMultiplierAccountBalanceUsesSub2APIAccount(t *testing.T) {
	var sawKeysEndpoint bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			assert.Equal(t, http.MethodPost, r.Method)
			_, _ = w.Write([]byte(`{"data":{"access_token":"token-123"}}`))
		case "/api/v1/auth/me":
			assert.Equal(t, "Bearer token-123", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"data":{"balance":26.54674067}}`))
		case "/api/v1/keys":
			sawKeysEndpoint = true
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	channel := &model.Channel{
		Id:      1301,
		Type:    constant.ChannelTypeCustom,
		Key:     "sk-current",
		Name:    "sub2api-account-balance",
		BaseURL: common.GetPointer(server.URL),
		OtherSettings: channelMultiplierSettingsJSON(t, &dto.ChannelMultiplierMonitorConfig{
			Enabled:  false,
			Format:   dto.ChannelMultiplierProviderFormatSub2API,
			BaseURL:  server.URL,
			Username: "alice@example.com",
			Password: "secret",
		}),
	}

	balance, configured, err := FetchChannelMultiplierAccountBalance(context.Background(), channel)

	require.NoError(t, err)
	assert.True(t, configured)
	assert.False(t, sawKeysEndpoint)
	assert.Equal(t, 26.54674067, balance)
}

func TestFetchChannelMultiplierAccountBalanceUsesNewAPIAccount(t *testing.T) {
	var sawTokenEndpoint bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/login":
			assert.Equal(t, http.MethodPost, r.Method)
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "session-123", Path: "/"})
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":4718}}`))
		case "/api/user/self":
			assert.Equal(t, "4718", r.Header.Get("new-api-user"))
			assert.Contains(t, r.Header.Get("Cookie"), "session=session-123")
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota":6749186}}`))
		case "/api/token/":
			sawTokenEndpoint = true
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	channel := &model.Channel{
		Id:      1302,
		Type:    constant.ChannelTypeCustom,
		Key:     "sk-current",
		Name:    "new-api-account-balance",
		BaseURL: common.GetPointer(server.URL),
		OtherSettings: channelMultiplierSettingsJSON(t, &dto.ChannelMultiplierMonitorConfig{
			Enabled:  false,
			Format:   dto.ChannelMultiplierProviderFormatNewAPI,
			BaseURL:  server.URL,
			Username: "alice",
			Password: "secret",
		}),
	}

	balance, configured, err := FetchChannelMultiplierAccountBalance(context.Background(), channel)

	require.NoError(t, err)
	assert.True(t, configured)
	assert.False(t, sawTokenEndpoint)
	assert.Equal(t, 6749186.0/500000.0, balance)
}

func withChannelMultiplierMonitorTestDB(t *testing.T) {
	t.Helper()

	oldDB := model.DB
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.Channel{}))

	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})
}

func seedChannelMultiplierMonitorChannel(t *testing.T, id int) {
	t.Helper()

	channel := &model.Channel{
		Id:     id,
		Type:   constant.ChannelTypeCustom,
		Key:    "sk-current",
		Status: common.ChannelStatusEnabled,
		Name:   "monitor-balance",
		OtherSettings: channelMultiplierSettingsJSON(t, &dto.ChannelMultiplierMonitorConfig{
			Enabled:  true,
			Format:   dto.ChannelMultiplierProviderFormatNewAPI,
			BaseURL:  "https://upstream.example.com",
			Username: "alice",
			Password: "secret",
		}),
	}
	require.NoError(t, model.DB.Create(channel).Error)
}

func loadChannelMultiplierMonitorChannel(t *testing.T, id int) model.Channel {
	t.Helper()

	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, id).Error)
	return channel
}

func TestProbeChannelMultipliersRunsWithBoundedConcurrency(t *testing.T) {
	ResetChannelMultiplierMonitorForTest()
	t.Cleanup(ResetChannelMultiplierMonitorForTest)

	originalProbe := channelMultiplierProbe
	t.Cleanup(func() {
		channelMultiplierProbe = originalProbe
	})

	var active int32
	var maxActive int32
	var total int32
	started := make(chan int, 6)
	release := make(chan struct{})
	channelMultiplierProbe = func(ctx context.Context, channel *model.Channel) (ChannelMultiplierSnapshot, error) {
		current := atomic.AddInt32(&active, 1)
		for {
			previous := atomic.LoadInt32(&maxActive)
			if current <= previous || atomic.CompareAndSwapInt32(&maxActive, previous, current) {
				break
			}
		}
		started <- channel.Id
		<-release
		atomic.AddInt32(&active, -1)
		atomic.AddInt32(&total, 1)
		return buildHealthyMultiplierSnapshot(channel, GetChannelMultiplierMonitorConfig(channel), 0.5, 1, "default", "1"), nil
	}

	channels := make([]*model.Channel, 0, 6)
	for i := 1; i <= 6; i++ {
		channels = append(channels, &model.Channel{
			Id:   i,
			Type: constant.ChannelTypeCustom,
			Key:  "sk-current",
			Name: "concurrent",
			OtherSettings: channelMultiplierSettingsJSON(t, &dto.ChannelMultiplierMonitorConfig{
				Enabled:  true,
				Format:   dto.ChannelMultiplierProviderFormatNewAPI,
				BaseURL:  "https://upstream.example.com",
				Username: "alice",
				Password: "secret",
			}),
		})
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		probeChannelMultipliers(context.Background(), channels, 3)
	}()

	for i := 0; i < 3; i++ {
		<-started
	}
	assert.Equal(t, int32(3), atomic.LoadInt32(&maxActive))
	select {
	case id := <-started:
		t.Fatalf("probe %d started before a worker slot was released", id)
	default:
	}

	close(release)
	wg.Wait()
	assert.Equal(t, int32(6), atomic.LoadInt32(&total))
	assert.Equal(t, int32(3), atomic.LoadInt32(&maxActive))
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
