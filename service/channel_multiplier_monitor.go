package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/hot"
)

const (
	channelMultiplierMonitorConcurrency = 4
	channelMultiplierMonitorTimeout     = 15 * time.Second
	channelMultiplierSnapshotTTL        = 5 * time.Minute
	channelMultiplierCacheTTL           = 15 * time.Minute
	channelMultiplierCacheNamespace     = "new-api:channel_multiplier:v1"
)

type ChannelMultiplierSnapshotState string

const (
	ChannelMultiplierSnapshotHealthy ChannelMultiplierSnapshotState = "healthy"
	ChannelMultiplierSnapshotStale   ChannelMultiplierSnapshotState = "stale"
	ChannelMultiplierSnapshotError   ChannelMultiplierSnapshotState = "error"
	ChannelMultiplierSnapshotEmpty   ChannelMultiplierSnapshotState = "empty"
)

type ChannelMultiplierSnapshot struct {
	ChannelID       int                                 `json:"channel_id"`
	Enabled         bool                                `json:"enabled"`
	Format          dto.ChannelMultiplierProviderFormat `json:"format,omitempty"`
	BaseURL         string                              `json:"base_url,omitempty"`
	State           ChannelMultiplierSnapshotState      `json:"state"`
	Multiplier      float64                             `json:"multiplier"`
	Balance         float64                             `json:"balance"`
	Username        string                              `json:"username,omitempty"`
	ObservedGroup   string                              `json:"observed_group,omitempty"`
	ObservedTokenID string                              `json:"observed_token_id,omitempty"`
	Reason          string                              `json:"reason,omitempty"`
	ObservedAt      int64                               `json:"observed_at"`
	ExpiresAt       int64                               `json:"expires_at,omitempty"`
}

type channelMultiplierAuth struct {
	accessToken string
	userID      string
}

type channelMultiplierMonitorClient struct {
	baseURL string
	client  *http.Client
}

type sub2APIKeyItem struct {
	ID    int    `json:"id"`
	Key   string `json:"key"`
	Group struct {
		Name           string  `json:"name"`
		RateMultiplier float64 `json:"rate_multiplier"`
	} `json:"group"`
}

type newAPITokenItem struct {
	ID          int    `json:"id"`
	Key         string `json:"key"`
	CreatedTime int64  `json:"created_time"`
	Group       any    `json:"group"`
}

var channelMultiplierMonitor = struct {
	sync.Mutex
	once      sync.Once
	running   atomic.Bool
	cacheOnce sync.Once
	cache     *cachex.HybridCache[ChannelMultiplierSnapshot]
	now       func() time.Time
}{
	now: time.Now,
}

var channelMultiplierProbe = probeChannelMultiplier

func StartChannelMultiplierMonitorWorker() {
	channelMultiplierMonitor.once.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			common.SysLog(fmt.Sprintf("channel multiplier monitor worker started: interval=%s", channelMultiplierMonitorInterval()))
			runChannelMultiplierMonitorOnce()
			for {
				time.Sleep(channelMultiplierMonitorInterval())
				runChannelMultiplierMonitorOnce()
			}
		})
	})
}

func channelMultiplierMonitorInterval() time.Duration {
	minutes := operation_setting.GetChannelMultiplierMonitorSetting().IntervalMinutes
	if minutes < 1 {
		minutes = operation_setting.ChannelMultiplierMonitorDefaultIntervalMinutes
	}
	return time.Duration(minutes) * time.Minute
}

func ResetChannelMultiplierMonitorForTest() {
	channelMultiplierMonitor.Lock()
	defer channelMultiplierMonitor.Unlock()

	channelMultiplierMonitor.once = sync.Once{}
	channelMultiplierMonitor.running.Store(false)
	channelMultiplierMonitor.cacheOnce = sync.Once{}
	channelMultiplierMonitor.cache = nil
	channelMultiplierMonitor.now = time.Now
}

func SetChannelMultiplierSnapshotForTest(snapshot ChannelMultiplierSnapshot) {
	setChannelMultiplierSnapshot(snapshot)
}

func getChannelMultiplierCache() *cachex.HybridCache[ChannelMultiplierSnapshot] {
	channelMultiplierMonitor.cacheOnce.Do(func() {
		channelMultiplierMonitor.cache = cachex.NewHybridCache[ChannelMultiplierSnapshot](cachex.HybridCacheConfig[ChannelMultiplierSnapshot]{
			Namespace:  cachex.Namespace(channelMultiplierCacheNamespace),
			Redis:      common.RDB,
			RedisCodec: cachex.JSONCodec[ChannelMultiplierSnapshot]{},
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			Memory: func() *hot.HotCache[string, ChannelMultiplierSnapshot] {
				return hot.NewHotCache[string, ChannelMultiplierSnapshot](hot.LRU, 10_000).
					WithTTL(channelMultiplierCacheTTL).
					WithJanitor().
					Build()
			},
		})
	})
	return channelMultiplierMonitor.cache
}

func channelMultiplierNow() time.Time {
	channelMultiplierMonitor.Lock()
	defer channelMultiplierMonitor.Unlock()
	return channelMultiplierMonitor.now()
}

func runChannelMultiplierMonitorOnce() {
	if !channelMultiplierMonitor.running.CompareAndSwap(false, true) {
		return
	}
	defer channelMultiplierMonitor.running.Store(false)

	ctx := context.Background()
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("channel multiplier monitor: load channels failed: %v", err))
		return
	}

	probeChannelMultipliers(ctx, channels, channelMultiplierMonitorConcurrency)
	if summary, applied, err := ApplyChannelAutoPriorityIfEnabled(ctx); applied {
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("channel auto priority: apply failed: %v", err))
		} else if summary.UpdatedChannels > 0 {
			common.SysLog(fmt.Sprintf("channel auto priority applied: updated=%d skipped=%d", summary.UpdatedChannels, summary.SkippedChannels))
		}
	}
}

func probeChannelMultipliers(ctx context.Context, channels []*model.Channel, concurrency int) {
	candidates := make([]*model.Channel, 0, len(channels))
	for _, channel := range channels {
		if channel == nil || !shouldMonitorChannelMultiplier(channel) {
			continue
		}
		candidates = append(candidates, channel)
	}
	if len(candidates) == 0 {
		return
	}
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(candidates) {
		concurrency = len(candidates)
	}

	jobs := make(chan *model.Channel)
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			for channel := range jobs {
				_, _ = refreshChannelMultiplierSnapshot(ctx, channel, false)
			}
		}()
	}
	for _, channel := range candidates {
		jobs <- channel
	}
	close(jobs)
	wg.Wait()
}

func shouldMonitorChannelMultiplier(channel *model.Channel) bool {
	cfg := GetChannelMultiplierMonitorConfig(channel)
	return cfg.Enabled &&
		cfg.Format != "" &&
		strings.TrimSpace(channelMultiplierBaseURL(channel, cfg)) != "" &&
		strings.TrimSpace(cfg.Username) != "" &&
		strings.TrimSpace(cfg.Password) != "" &&
		firstChannelKey(channel) != ""
}

func RefreshChannelMultiplierSnapshot(ctx context.Context, channel *model.Channel) (ChannelMultiplierSnapshot, error) {
	return refreshChannelMultiplierSnapshot(ctx, channel, true)
}

func RefreshChannelMultiplierSnapshotAsync(channelID int) {
	if channelID <= 0 {
		return
	}
	gopool.Go(func() {
		channel, err := model.GetChannelById(channelID, true)
		if err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("channel multiplier monitor: async load failed: channel_id=%d, err=%v", channelID, err))
			return
		}
		_, _ = refreshChannelMultiplierSnapshot(context.Background(), channel, false)
	})
}

func refreshChannelMultiplierSnapshot(ctx context.Context, channel *model.Channel, requireConfigured bool) (ChannelMultiplierSnapshot, error) {
	if channel == nil {
		return ChannelMultiplierSnapshot{State: ChannelMultiplierSnapshotEmpty}, errors.New("channel is required")
	}
	if !shouldMonitorChannelMultiplier(channel) {
		snapshot := GetChannelMultiplierSnapshotForDisplay(channel)
		if requireConfigured {
			return snapshot, errors.New("channel multiplier monitor is not enabled or is missing base URL, account, password, or key")
		}
		return snapshot, nil
	}

	snapshot, err := channelMultiplierProbe(ctx, channel)
	if err != nil {
		cfg := GetChannelMultiplierMonitorConfig(channel)
		now := channelMultiplierNow()
		snapshot = ChannelMultiplierSnapshot{
			ChannelID:  channel.Id,
			Enabled:    true,
			Format:     cfg.Format,
			BaseURL:    channelMultiplierBaseURL(channel, cfg),
			State:      ChannelMultiplierSnapshotError,
			Username:   cfg.Username,
			Reason:     err.Error(),
			ObservedAt: now.Unix(),
			ExpiresAt:  now.Add(channelMultiplierSnapshotTTL).Unix(),
		}
		logger.LogWarn(ctx, fmt.Sprintf("channel multiplier monitor: probe failed: channel_id=%d, err=%v", channel.Id, err))
	}
	setChannelMultiplierSnapshot(snapshot)
	updateChannelBalanceFromMultiplierSnapshot(channel, snapshot)
	return snapshot, err
}

func updateChannelBalanceFromMultiplierSnapshot(channel *model.Channel, snapshot ChannelMultiplierSnapshot) {
	if channel == nil || snapshot.State != ChannelMultiplierSnapshotHealthy {
		return
	}
	channel.UpdateBalance(snapshot.Balance)
}

func GetChannelMultiplierMonitorConfig(channel *model.Channel) dto.ChannelMultiplierMonitorConfig {
	if channel == nil {
		return dto.ChannelMultiplierMonitorConfig{}
	}
	settings := channel.GetOtherSettings()
	if settings.UpstreamKeyMultiplier == nil {
		return dto.ChannelMultiplierMonitorConfig{}
	}
	cfg := *settings.UpstreamKeyMultiplier
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.Username = strings.TrimSpace(cfg.Username)
	return cfg
}

func RedactChannelMultiplierMonitorSettings(settingsJSON string) string {
	if strings.TrimSpace(settingsJSON) == "" {
		return settingsJSON
	}
	var settings map[string]any
	if err := common.UnmarshalJsonStr(settingsJSON, &settings); err != nil {
		return settingsJSON
	}
	monitor, ok := settings["upstream_key_multiplier"].(map[string]any)
	if !ok {
		return settingsJSON
	}
	monitor["password"] = ""
	raw, err := common.Marshal(settings)
	if err != nil {
		return settingsJSON
	}
	return string(raw)
}

func MergeChannelMultiplierMonitorSecret(nextSettingsJSON string, previousSettingsJSON string) string {
	var nextSettings map[string]any
	if err := common.UnmarshalJsonStr(nextSettingsJSON, &nextSettings); err != nil {
		return nextSettingsJSON
	}
	nextMonitor, ok := nextSettings["upstream_key_multiplier"].(map[string]any)
	if !ok {
		return nextSettingsJSON
	}
	if password, _ := nextMonitor["password"].(string); strings.TrimSpace(password) != "" {
		return nextSettingsJSON
	}

	var previousSettings map[string]any
	if err := common.UnmarshalJsonStr(previousSettingsJSON, &previousSettings); err != nil {
		return nextSettingsJSON
	}
	previousMonitor, ok := previousSettings["upstream_key_multiplier"].(map[string]any)
	if !ok {
		return nextSettingsJSON
	}
	previousPassword, _ := previousMonitor["password"].(string)
	if strings.TrimSpace(previousPassword) == "" {
		return nextSettingsJSON
	}

	nextMonitor["password"] = previousPassword
	raw, err := common.Marshal(nextSettings)
	if err != nil {
		return nextSettingsJSON
	}
	return string(raw)
}

func GetChannelMultiplierSnapshot(channelID int) (ChannelMultiplierSnapshot, bool) {
	snapshot, found, err := getChannelMultiplierCache().Get(strconv.Itoa(channelID))
	if err != nil {
		common.SysError(fmt.Sprintf("channel multiplier cache get failed: channel_id=%d, err=%v", channelID, err))
		return ChannelMultiplierSnapshot{}, false
	}
	return snapshot, found
}

func GetChannelMultiplierSnapshotForDisplay(channel *model.Channel) ChannelMultiplierSnapshot {
	if channel == nil {
		return ChannelMultiplierSnapshot{State: ChannelMultiplierSnapshotEmpty}
	}
	cfg := GetChannelMultiplierMonitorConfig(channel)
	if !cfg.Enabled {
		return ChannelMultiplierSnapshot{
			ChannelID: channel.Id,
			Enabled:   false,
			State:     ChannelMultiplierSnapshotEmpty,
		}
	}

	now := channelMultiplierNow()
	if snapshot, ok := GetChannelMultiplierSnapshot(channel.Id); ok {
		if snapshot.ExpiresAt > 0 && snapshot.ExpiresAt < now.Unix() && snapshot.State == ChannelMultiplierSnapshotHealthy {
			snapshot.State = ChannelMultiplierSnapshotStale
			snapshot.Reason = "snapshot is stale"
		}
		return snapshot
	}
	return ChannelMultiplierSnapshot{
		ChannelID: channel.Id,
		Enabled:   true,
		Format:    cfg.Format,
		BaseURL:   channelMultiplierBaseURL(channel, cfg),
		State:     ChannelMultiplierSnapshotEmpty,
		Username:  cfg.Username,
		Reason:    "waiting for first probe",
	}
}

func setChannelMultiplierSnapshot(snapshot ChannelMultiplierSnapshot) {
	if snapshot.ChannelID <= 0 {
		return
	}
	if snapshot.ExpiresAt == 0 {
		snapshot.ExpiresAt = channelMultiplierNow().Add(channelMultiplierSnapshotTTL).Unix()
	}
	if err := getChannelMultiplierCache().SetWithTTL(strconv.Itoa(snapshot.ChannelID), snapshot, channelMultiplierCacheTTL); err != nil {
		common.SysError(fmt.Sprintf("channel multiplier cache set failed: channel_id=%d, err=%v", snapshot.ChannelID, err))
	}
}

func probeChannelMultiplier(ctx context.Context, channel *model.Channel) (ChannelMultiplierSnapshot, error) {
	cfg := GetChannelMultiplierMonitorConfig(channel)
	client, auth, err := loginChannelMultiplierProvider(ctx, channel, cfg)
	if err != nil {
		return ChannelMultiplierSnapshot{}, err
	}

	switch cfg.Format {
	case dto.ChannelMultiplierProviderFormatSub2API:
		return fetchSub2APIMultiplier(ctx, client, auth, channel, cfg)
	case dto.ChannelMultiplierProviderFormatNewAPI:
		return fetchNewAPIMultiplier(ctx, client, auth, channel, cfg)
	default:
		return ChannelMultiplierSnapshot{}, fmt.Errorf("unsupported monitor format: %s", cfg.Format)
	}
}

func FetchChannelMultiplierAccountBalance(ctx context.Context, channel *model.Channel) (float64, bool, error) {
	if channel == nil {
		return 0, false, nil
	}
	cfg := GetChannelMultiplierMonitorConfig(channel)
	if cfg.Format == "" ||
		strings.TrimSpace(channelMultiplierBaseURL(channel, cfg)) == "" ||
		strings.TrimSpace(cfg.Username) == "" ||
		strings.TrimSpace(cfg.Password) == "" {
		return 0, false, nil
	}

	client, auth, err := loginChannelMultiplierProvider(ctx, channel, cfg)
	if err != nil {
		return 0, true, err
	}
	switch cfg.Format {
	case dto.ChannelMultiplierProviderFormatSub2API:
		balance, err := fetchSub2APIBalance(ctx, client, auth)
		return balance, true, err
	case dto.ChannelMultiplierProviderFormatNewAPI:
		balance, err := fetchNewAPIBalance(ctx, client, auth)
		return balance, true, err
	default:
		return 0, true, fmt.Errorf("unsupported monitor format: %s", cfg.Format)
	}
}

func loginChannelMultiplierProvider(ctx context.Context, channel *model.Channel, cfg dto.ChannelMultiplierMonitorConfig) (*channelMultiplierMonitorClient, *channelMultiplierAuth, error) {
	baseURL := channelMultiplierBaseURL(channel, cfg)
	if baseURL == "" {
		return nil, nil, errors.New("base_url is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, nil, fmt.Errorf("invalid base_url: %s", baseURL)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, nil, err
	}
	httpClient, err := GetHttpClientWithProxy("")
	if err != nil {
		return nil, nil, err
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	client := *httpClient
	client.Timeout = channelMultiplierMonitorTimeout
	client.Jar = jar

	auth, err := loginChannelMultiplierSession(ctx, &client, baseURL, cfg)
	if err != nil {
		return nil, nil, err
	}
	return &channelMultiplierMonitorClient{
		baseURL: baseURL,
		client:  &client,
	}, auth, nil
}

func loginChannelMultiplierSession(ctx context.Context, client *http.Client, baseURL string, cfg dto.ChannelMultiplierMonitorConfig) (*channelMultiplierAuth, error) {
	switch cfg.Format {
	case dto.ChannelMultiplierProviderFormatSub2API:
		return loginSub2API(ctx, client, baseURL, cfg)
	case dto.ChannelMultiplierProviderFormatNewAPI:
		return loginNewAPI(ctx, client, baseURL, cfg)
	default:
		return nil, fmt.Errorf("unsupported monitor format: %s", cfg.Format)
	}
}

func loginSub2API(ctx context.Context, client *http.Client, baseURL string, cfg dto.ChannelMultiplierMonitorConfig) (*channelMultiplierAuth, error) {
	reqBody := map[string]string{
		"email":    cfg.Username,
		"password": cfg.Password,
	}
	resBody, err := doJSONRequest(ctx, client, http.MethodPost, baseURL+"/api/v1/auth/login", nil, reqBody)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
		AccessToken string `json:"access_token"`
	}
	if err := common.Unmarshal(resBody, &parsed); err != nil {
		return nil, err
	}
	token := strings.TrimSpace(parsed.Data.AccessToken)
	if token == "" {
		token = strings.TrimSpace(parsed.AccessToken)
	}
	if token == "" {
		return nil, errors.New("sub2api login returned empty access_token")
	}
	return &channelMultiplierAuth{accessToken: token}, nil
}

func loginNewAPI(ctx context.Context, client *http.Client, baseURL string, cfg dto.ChannelMultiplierMonitorConfig) (*channelMultiplierAuth, error) {
	reqBody := map[string]string{
		"username": cfg.Username,
		"password": cfg.Password,
	}
	resBody, err := doJSONRequest(ctx, client, http.MethodPost, baseURL+"/api/user/login", nil, reqBody)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := common.Unmarshal(resBody, &parsed); err != nil {
		return nil, err
	}
	if parsed.Data.ID <= 0 {
		return nil, errors.New("new-api login returned invalid user id")
	}
	return &channelMultiplierAuth{userID: strconv.Itoa(parsed.Data.ID)}, nil
}

func fetchSub2APIMultiplier(ctx context.Context, client *channelMultiplierMonitorClient, auth *channelMultiplierAuth, channel *model.Channel, cfg dto.ChannelMultiplierMonitorConfig) (ChannelMultiplierSnapshot, error) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+auth.accessToken)

	tokenBody, err := doJSONRequest(ctx, client.client, http.MethodGet, client.baseURL+"/api/v1/keys?page=1&page_size=10&sort_by=created_at&sort_order=desc&timezone=Asia%2FShanghai", headers, nil)
	if err != nil {
		return ChannelMultiplierSnapshot{}, err
	}
	var parsed struct {
		Data struct {
			Items []sub2APIKeyItem `json:"items"`
		} `json:"data"`
	}
	if err := common.Unmarshal(tokenBody, &parsed); err != nil {
		return ChannelMultiplierSnapshot{}, err
	}
	if len(parsed.Data.Items) == 0 {
		return buildEmptyMultiplierSnapshot(channel, cfg, "no keys returned"), nil
	}
	item := selectSub2APIKeyItem(parsed.Data.Items, firstChannelKey(channel))
	balance, err := fetchSub2APIBalance(ctx, client, auth)
	if err != nil {
		return ChannelMultiplierSnapshot{}, err
	}
	return buildHealthyMultiplierSnapshot(channel, cfg, item.Group.RateMultiplier, balance, item.Group.Name, strconv.Itoa(item.ID)), nil
}

func fetchSub2APIBalance(ctx context.Context, client *channelMultiplierMonitorClient, auth *channelMultiplierAuth) (float64, error) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+auth.accessToken)
	body, err := doJSONRequest(ctx, client.client, http.MethodGet, client.baseURL+"/api/v1/auth/me?timezone=Asia%2FShanghai", headers, nil)
	if err != nil {
		return 0, err
	}
	var parsed struct {
		Data struct {
			Balance float64 `json:"balance"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &parsed); err != nil {
		return 0, err
	}
	return parsed.Data.Balance, nil
}

func selectSub2APIKeyItem(items []sub2APIKeyItem, channelKey string) sub2APIKeyItem {
	for _, item := range items {
		if tokenKeyMatchesChannelKey(item.Key, channelKey) {
			return item
		}
	}
	return items[0]
}

func fetchNewAPIMultiplier(ctx context.Context, client *channelMultiplierMonitorClient, auth *channelMultiplierAuth, channel *model.Channel, cfg dto.ChannelMultiplierMonitorConfig) (ChannelMultiplierSnapshot, error) {
	headers := newAPIAuthHeaders(auth)
	body, err := doJSONRequest(ctx, client.client, http.MethodGet, client.baseURL+"/api/token/?p=1&size=10", headers, nil)
	if err != nil {
		return ChannelMultiplierSnapshot{}, err
	}
	var parsed struct {
		Data struct {
			Items []newAPITokenItem `json:"items"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &parsed); err != nil {
		return ChannelMultiplierSnapshot{}, err
	}
	if len(parsed.Data.Items) == 0 {
		return buildEmptyMultiplierSnapshot(channel, cfg, "no tokens returned"), nil
	}

	item := selectNewAPITokenItem(parsed.Data.Items, firstChannelKey(channel))
	groupName, multiplier := parseNewAPIGroup(item.Group)
	if normalizeMultiplier(multiplier) == 0 && groupName != "" {
		ratios, err := fetchNewAPIGroupRatios(ctx, client, auth)
		if err != nil {
			return ChannelMultiplierSnapshot{}, err
		}
		multiplier = ratios[groupName]
	}
	if normalizeMultiplier(multiplier) == 0 {
		return ChannelMultiplierSnapshot{}, fmt.Errorf("new-api token group %q has no multiplier", groupName)
	}

	balance, err := fetchNewAPIBalance(ctx, client, auth)
	if err != nil {
		return ChannelMultiplierSnapshot{}, err
	}
	return buildHealthyMultiplierSnapshot(channel, cfg, multiplier, balance, groupName, strconv.Itoa(item.ID)), nil
}

func fetchNewAPIBalance(ctx context.Context, client *channelMultiplierMonitorClient, auth *channelMultiplierAuth) (float64, error) {
	body, err := doJSONRequest(ctx, client.client, http.MethodGet, client.baseURL+"/api/user/self", newAPIAuthHeaders(auth), nil)
	if err != nil {
		return 0, err
	}
	var parsed struct {
		Data struct {
			Quota float64 `json:"quota"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &parsed); err != nil {
		return 0, err
	}
	return parsed.Data.Quota / 500000.0, nil
}

func fetchNewAPIGroupRatios(ctx context.Context, client *channelMultiplierMonitorClient, auth *channelMultiplierAuth) (map[string]float64, error) {
	body, err := doJSONRequest(ctx, client.client, http.MethodGet, client.baseURL+"/api/user/self/groups", newAPIAuthHeaders(auth), nil)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data map[string]struct {
			Ratio float64 `json:"ratio"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	ratios := make(map[string]float64, len(parsed.Data))
	for group, value := range parsed.Data {
		ratios[group] = value.Ratio
	}
	return ratios, nil
}

func newAPIAuthHeaders(auth *channelMultiplierAuth) http.Header {
	headers := http.Header{}
	if auth != nil && auth.userID != "" {
		headers.Set("new-api-user", auth.userID)
	}
	return headers
}

func selectNewAPITokenItem(items []newAPITokenItem, channelKey string) newAPITokenItem {
	for _, item := range items {
		if tokenKeyMatchesChannelKey(item.Key, channelKey) {
			return item
		}
	}
	return items[0]
}

func parseNewAPIGroup(group any) (string, float64) {
	switch value := group.(type) {
	case string:
		return strings.TrimSpace(value), 0
	case map[string]any:
		name := firstString(value["name"], value["group"], value["id"])
		multiplier := firstFloat(value["rate_multiplier"], value["ratio"])
		return name, multiplier
	default:
		return "", 0
	}
}

func buildHealthyMultiplierSnapshot(channel *model.Channel, cfg dto.ChannelMultiplierMonitorConfig, multiplier float64, balance float64, groupName string, tokenID string) ChannelMultiplierSnapshot {
	now := channelMultiplierNow()
	return ChannelMultiplierSnapshot{
		ChannelID:       channel.Id,
		Enabled:         true,
		Format:          cfg.Format,
		BaseURL:         channelMultiplierBaseURL(channel, cfg),
		State:           ChannelMultiplierSnapshotHealthy,
		Multiplier:      normalizeMultiplier(multiplier),
		Balance:         balance,
		Username:        cfg.Username,
		ObservedGroup:   groupName,
		ObservedTokenID: tokenID,
		ObservedAt:      now.Unix(),
		ExpiresAt:       now.Add(channelMultiplierSnapshotTTL).Unix(),
	}
}

func buildEmptyMultiplierSnapshot(channel *model.Channel, cfg dto.ChannelMultiplierMonitorConfig, reason string) ChannelMultiplierSnapshot {
	now := channelMultiplierNow()
	return ChannelMultiplierSnapshot{
		ChannelID:  channel.Id,
		Enabled:    true,
		Format:     cfg.Format,
		BaseURL:    channelMultiplierBaseURL(channel, cfg),
		State:      ChannelMultiplierSnapshotEmpty,
		Reason:     reason,
		Username:   cfg.Username,
		ObservedAt: now.Unix(),
		ExpiresAt:  now.Add(channelMultiplierSnapshotTTL).Unix(),
	}
}

func normalizeMultiplier(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0
	}
	return value
}

func firstChannelKey(channel *model.Channel) string {
	if channel == nil {
		return ""
	}
	for _, key := range channel.GetKeys() {
		key = strings.TrimSpace(key)
		if unquoted, err := strconv.Unquote(key); err == nil {
			key = strings.TrimSpace(unquoted)
		}
		if key != "" {
			return key
		}
	}
	return ""
}

func tokenKeyMatchesChannelKey(tokenKey string, channelKey string) bool {
	tokenKey = strings.TrimSpace(tokenKey)
	channelKey = strings.TrimSpace(channelKey)
	if tokenKey == "" || channelKey == "" {
		return false
	}
	if tokenKey == channelKey {
		return true
	}
	if !strings.Contains(tokenKey, "*") {
		return false
	}
	firstMask := strings.Index(tokenKey, "*")
	lastMask := strings.LastIndex(tokenKey, "*")
	prefix := tokenKey[:firstMask]
	suffix := tokenKey[lastMask+1:]
	if len(channelKey) < len(prefix)+len(suffix) {
		return false
	}
	return strings.HasPrefix(channelKey, prefix) && strings.HasSuffix(channelKey, suffix)
}

func channelMultiplierBaseURL(channel *model.Channel, cfg dto.ChannelMultiplierMonitorConfig) string {
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	}
	if channel == nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/")
}

func firstString(values ...any) string {
	for _, value := range values {
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		case float64:
			if v > 0 {
				return strconv.FormatInt(int64(v), 10)
			}
		case int:
			if v > 0 {
				return strconv.Itoa(v)
			}
		}
	}
	return ""
}

func firstFloat(values ...any) float64 {
	for _, value := range values {
		switch v := value.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}

func doJSONRequest(ctx context.Context, client *http.Client, method string, targetURL string, headers http.Header, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		encoded, err := common.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, targetURL, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream responded %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}
