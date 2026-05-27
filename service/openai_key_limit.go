package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/go-redis/redis/v8"
)

type OpenAIUpstreamKeyLimitType string

const (
	OpenAIUpstreamKeyLimitRPM OpenAIUpstreamKeyLimitType = "rpm"
	OpenAIUpstreamKeyLimitTPM OpenAIUpstreamKeyLimitType = "tpm"
	OpenAIUpstreamKeyLimitRPD OpenAIUpstreamKeyLimitType = "rpd"
	OpenAIUpstreamKeyLimitTPD OpenAIUpstreamKeyLimitType = "tpd"
)

const (
	openAIUpstreamMinuteWindowSeconds = int64(60)
	openAIUpstreamDailyWindowSeconds  = int64(24 * 60 * 60)
)

type OpenAIUpstreamKeyLimitRequest struct {
	ChannelID       int
	Key             string
	EstimatedTokens int
	Config          setting.OpenAIUpstreamKeyLimitConfig
	Now             time.Time
}

type OpenAIUpstreamKeyLimitResult struct {
	Allowed       bool
	LimitType     OpenAIUpstreamKeyLimitType
	RetryAt       time.Time
	ReservationID string
	Estimated     int
}

type OpenAIUpstreamKeyLimitCommit struct {
	ReservationID string
	ActualTokens  int
	Now           time.Time
}

type OpenAIUpstreamKeyLimiter interface {
	Reserve(OpenAIUpstreamKeyLimitRequest) (OpenAIUpstreamKeyLimitResult, error)
	Commit(OpenAIUpstreamKeyLimitCommit) error
}

type openAIUpstreamEvent struct {
	id     string
	at     int64
	amount int
}

type memoryOpenAIUpstreamKeyLimiter struct {
	mu           sync.Mutex
	events       map[string][]openAIUpstreamEvent
	reservations map[string]openAIUpstreamReservation
	nextID       int64
}

type openAIUpstreamReservation struct {
	fingerprint string
	requestID   string
	tokenID     string
	estimated   int
}

func NewMemoryOpenAIUpstreamKeyLimiter(now time.Time) OpenAIUpstreamKeyLimiter {
	return &memoryOpenAIUpstreamKeyLimiter{
		events:       make(map[string][]openAIUpstreamEvent),
		reservations: make(map[string]openAIUpstreamReservation),
	}
}

func OpenAIUpstreamKeyFingerprint(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

func EstimateOpenAIUpstreamTotalTokens(promptTokens int, maxOutputTokens int) int {
	if promptTokens < 0 {
		promptTokens = 0
	}
	if maxOutputTokens < 0 {
		maxOutputTokens = 0
	}
	return promptTokens + maxOutputTokens
}

func ShouldApplyOpenAIUpstreamKeyLimit(channel *model.Channel) bool {
	if channel == nil || channel.Type != constant.ChannelTypeOpenAI {
		return false
	}
	baseURL := strings.TrimSpace(channel.GetBaseURL())
	if baseURL == "" || baseURL == constant.ChannelBaseURLs[constant.ChannelTypeOpenAI] {
		return true
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), "api.openai.com")
}

func (l *memoryOpenAIUpstreamKeyLimiter) Reserve(req OpenAIUpstreamKeyLimitRequest) (OpenAIUpstreamKeyLimitResult, error) {
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	fingerprint := OpenAIUpstreamKeyFingerprint(req.Key)
	if fingerprint == "" {
		return OpenAIUpstreamKeyLimitResult{}, errors.New("empty key fingerprint")
	}
	if req.EstimatedTokens < 0 {
		req.EstimatedTokens = 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.cleanup(fingerprint, req.Now.Unix())
	checks := []struct {
		limitType OpenAIUpstreamKeyLimitType
		key       string
		limit     int
		window    int64
		requested int
	}{
		{OpenAIUpstreamKeyLimitRPM, l.windowKey(fingerprint, "rpm"), req.Config.RPM, openAIUpstreamMinuteWindowSeconds, 1},
		{OpenAIUpstreamKeyLimitTPM, l.windowKey(fingerprint, "tpm"), req.Config.TPM, openAIUpstreamMinuteWindowSeconds, req.EstimatedTokens},
		{OpenAIUpstreamKeyLimitRPD, l.windowKey(fingerprint, "rpd"), req.Config.RPD, openAIUpstreamDailyWindowSeconds, 1},
		{OpenAIUpstreamKeyLimitTPD, l.windowKey(fingerprint, "tpd"), req.Config.TPD, openAIUpstreamDailyWindowSeconds, req.EstimatedTokens},
	}

	for _, check := range checks {
		if check.limit <= 0 || check.requested <= 0 {
			continue
		}
		used := l.sum(check.key, req.Now.Unix()-check.window)
		if used+check.requested > check.limit {
			retryAt := l.retryAt(check.key, req.Now, check.window)
			return OpenAIUpstreamKeyLimitResult{
				Allowed:   false,
				LimitType: check.limitType,
				RetryAt:   retryAt,
			}, nil
		}
	}

	l.nextID++
	reservationID := fmt.Sprintf("%s:%d", fingerprint[:16], l.nextID)
	requestEventID := reservationID + ":req"
	tokenEventID := reservationID + ":tok"
	nowUnix := req.Now.Unix()

	l.append(l.windowKey(fingerprint, "rpm"), openAIUpstreamEvent{id: requestEventID, at: nowUnix, amount: 1})
	l.append(l.windowKey(fingerprint, "rpd"), openAIUpstreamEvent{id: requestEventID, at: nowUnix, amount: 1})
	l.append(l.windowKey(fingerprint, "tpm"), openAIUpstreamEvent{id: tokenEventID, at: nowUnix, amount: req.EstimatedTokens})
	l.append(l.windowKey(fingerprint, "tpd"), openAIUpstreamEvent{id: tokenEventID, at: nowUnix, amount: req.EstimatedTokens})
	l.reservations[reservationID] = openAIUpstreamReservation{
		fingerprint: fingerprint,
		requestID:   requestEventID,
		tokenID:     tokenEventID,
		estimated:   req.EstimatedTokens,
	}

	return OpenAIUpstreamKeyLimitResult{
		Allowed:       true,
		ReservationID: reservationID,
		Estimated:     req.EstimatedTokens,
	}, nil
}

func (l *memoryOpenAIUpstreamKeyLimiter) Commit(commit OpenAIUpstreamKeyLimitCommit) error {
	if commit.ReservationID == "" {
		return nil
	}
	if commit.ActualTokens < 0 {
		commit.ActualTokens = 0
	}
	if commit.Now.IsZero() {
		commit.Now = time.Now()
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	reservation, ok := l.reservations[commit.ReservationID]
	if !ok {
		return nil
	}
	l.updateAmount(l.windowKey(reservation.fingerprint, "tpm"), reservation.tokenID, commit.ActualTokens)
	l.updateAmount(l.windowKey(reservation.fingerprint, "tpd"), reservation.tokenID, commit.ActualTokens)
	delete(l.reservations, commit.ReservationID)
	return nil
}

func (l *memoryOpenAIUpstreamKeyLimiter) windowKey(fingerprint string, metric string) string {
	return "openai_upstream_key_limit:" + metric + ":" + fingerprint
}

func (l *memoryOpenAIUpstreamKeyLimiter) cleanup(fingerprint string, now int64) {
	for _, metric := range []string{"rpm", "tpm"} {
		key := l.windowKey(fingerprint, metric)
		l.events[key] = filterOpenAIUpstreamEvents(l.events[key], now-openAIUpstreamMinuteWindowSeconds)
	}
	for _, metric := range []string{"rpd", "tpd"} {
		key := l.windowKey(fingerprint, metric)
		l.events[key] = filterOpenAIUpstreamEvents(l.events[key], now-openAIUpstreamDailyWindowSeconds)
	}
}

func filterOpenAIUpstreamEvents(events []openAIUpstreamEvent, after int64) []openAIUpstreamEvent {
	kept := events[:0]
	for _, event := range events {
		if event.at > after {
			kept = append(kept, event)
		}
	}
	return kept
}

func (l *memoryOpenAIUpstreamKeyLimiter) append(key string, event openAIUpstreamEvent) {
	l.events[key] = append(l.events[key], event)
}

func (l *memoryOpenAIUpstreamKeyLimiter) sum(key string, after int64) int {
	total := 0
	for _, event := range l.events[key] {
		if event.at > after {
			total += event.amount
		}
	}
	return total
}

func (l *memoryOpenAIUpstreamKeyLimiter) retryAt(key string, now time.Time, window int64) time.Time {
	oldest := int64(math.MaxInt64)
	after := now.Unix() - window
	for _, event := range l.events[key] {
		if event.at > after && event.at < oldest {
			oldest = event.at
		}
	}
	if oldest == int64(math.MaxInt64) {
		return now.Add(time.Duration(window) * time.Second)
	}
	return time.Unix(oldest+window, 0)
}

func (l *memoryOpenAIUpstreamKeyLimiter) updateAmount(key string, id string, amount int) {
	for i := range l.events[key] {
		if l.events[key][i].id == id {
			l.events[key][i].amount = amount
			return
		}
	}
}

type redisOpenAIUpstreamKeyLimiter struct {
	client *redis.Client
}

func NewRedisOpenAIUpstreamKeyLimiter(client *redis.Client) OpenAIUpstreamKeyLimiter {
	return &redisOpenAIUpstreamKeyLimiter{client: client}
}

func (l *redisOpenAIUpstreamKeyLimiter) Reserve(req OpenAIUpstreamKeyLimitRequest) (OpenAIUpstreamKeyLimitResult, error) {
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	if req.EstimatedTokens < 0 {
		req.EstimatedTokens = 0
	}
	fingerprint := OpenAIUpstreamKeyFingerprint(req.Key)
	now := req.Now.Unix()
	prefix := "openai_upstream_key_limit:"
	reservationID := fmt.Sprintf("%s:%d", fingerprint[:16], req.Now.UnixNano())
	result, err := l.client.Eval(context.Background(), openAIUpstreamReserveLua, []string{
		prefix + "rpm:" + fingerprint,
		prefix + "tpm:" + fingerprint,
		prefix + "rpd:" + fingerprint,
		prefix + "tpd:" + fingerprint,
		prefix + "reservation:" + reservationID,
	}, now, req.Config.RPM, req.Config.TPM, req.Config.RPD, req.Config.TPD, req.EstimatedTokens, reservationID).Result()
	if err != nil {
		return OpenAIUpstreamKeyLimitResult{}, err
	}
	values, ok := result.([]interface{})
	if !ok || len(values) < 4 {
		return OpenAIUpstreamKeyLimitResult{}, fmt.Errorf("unexpected redis limiter result: %v", result)
	}
	allowed, _ := strconv.Atoi(fmt.Sprintf("%v", values[0]))
	if allowed == 1 {
		return OpenAIUpstreamKeyLimitResult{
			Allowed:       true,
			ReservationID: reservationID,
			Estimated:     req.EstimatedTokens,
		}, nil
	}
	retryUnix, _ := strconv.ParseInt(fmt.Sprintf("%v", values[2]), 10, 64)
	return OpenAIUpstreamKeyLimitResult{
		Allowed:   false,
		LimitType: OpenAIUpstreamKeyLimitType(fmt.Sprintf("%v", values[1])),
		RetryAt:   time.Unix(retryUnix, 0),
	}, nil
}

func (l *redisOpenAIUpstreamKeyLimiter) Commit(commit OpenAIUpstreamKeyLimitCommit) error {
	if commit.ReservationID == "" {
		return nil
	}
	if commit.ActualTokens < 0 {
		commit.ActualTokens = 0
	}
	prefix := "openai_upstream_key_limit:"
	_, err := l.client.Eval(context.Background(), openAIUpstreamCommitLua, []string{
		prefix + "reservation:" + commit.ReservationID,
	}, commit.ActualTokens).Result()
	return err
}

var defaultOpenAIUpstreamKeyLimiter OpenAIUpstreamKeyLimiter = NewMemoryOpenAIUpstreamKeyLimiter(time.Now())

func getOpenAIUpstreamKeyLimiter() OpenAIUpstreamKeyLimiter {
	if common.RedisEnabled && common.RDB != nil {
		return NewRedisOpenAIUpstreamKeyLimiter(common.RDB)
	}
	return defaultOpenAIUpstreamKeyLimiter
}

func GetOpenAIUpstreamKeyLimiterForTest() OpenAIUpstreamKeyLimiter {
	return defaultOpenAIUpstreamKeyLimiter
}

func SetOpenAIUpstreamKeyLimiterForTest(limiter OpenAIUpstreamKeyLimiter) {
	if limiter == nil {
		return
	}
	defaultOpenAIUpstreamKeyLimiter = limiter
}

func ReserveOpenAIUpstreamKeyLimit(channel *model.Channel, key string, estimatedTokens int) (OpenAIUpstreamKeyLimitResult, *types.NewAPIError) {
	if !setting.OpenAIUpstreamKeyLimitEnabled || !ShouldApplyOpenAIUpstreamKeyLimit(channel) {
		return OpenAIUpstreamKeyLimitResult{Allowed: true}, nil
	}
	result, err := getOpenAIUpstreamKeyLimiter().Reserve(OpenAIUpstreamKeyLimitRequest{
		ChannelID:       channel.Id,
		Key:             key,
		EstimatedTokens: estimatedTokens,
		Config:          setting.OpenAIUpstreamKeyLimitConfigValue,
		Now:             time.Now(),
	})
	if err != nil {
		return result, types.NewError(err, types.ErrorCodeOpenAIUpstreamKeyRateLimited, types.ErrOptionWithSkipRetry())
	}
	if result.Allowed {
		return result, nil
	}
	return result, types.NewErrorWithStatusCode(
		fmt.Errorf("OpenAI upstream key reached %s limit, retry after %s", result.LimitType, result.RetryAt.Format(time.RFC3339)),
		types.ErrorCodeOpenAIUpstreamKeyRateLimited,
		429,
		types.ErrOptionWithNoRecordErrorLog(),
	)
}

func CommitOpenAIUpstreamKeyLimitReservation(reservationID string, actualTokens int) error {
	return getOpenAIUpstreamKeyLimiter().Commit(OpenAIUpstreamKeyLimitCommit{
		ReservationID: reservationID,
		ActualTokens:  actualTokens,
		Now:           time.Now(),
	})
}

func StartOpenAIUpstreamKeyLimitRestoreTask() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			RestoreExpiredOpenAIUpstreamKeyLimits(time.Now())
		}
	}()
}

func RestoreExpiredOpenAIUpstreamKeyLimits(now time.Time) {
	if model.DB == nil {
		return
	}
	var channels []model.Channel
	if err := model.DB.Where("type = ?", constant.ChannelTypeOpenAI).Find(&channels).Error; err != nil {
		common.SysLog("failed to query OpenAI upstream key limit restore channels: " + err.Error())
		return
	}
	nowUnix := now.Unix()
	changed := false
	for i := range channels {
		channel := &channels[i]
		if !ShouldApplyOpenAIUpstreamKeyLimit(channel) {
			continue
		}
		if channel.ChannelInfo.IsMultiKey {
			lock := model.GetChannelPollingLock(channel.Id)
			lock.Lock()
			restored := restoreExpiredOpenAIMultiKeys(channel, nowUnix)
			lock.Unlock()
			changed = changed || restored
			continue
		}
		info := channel.GetOtherInfo()
		until, ok := openAIDisabledUntilFromOtherInfo(info)
		if ok && until <= nowUnix {
			model.UpdateChannelStatus(channel.Id, "", common.ChannelStatusEnabled, "")
			changed = true
		}
	}
	if changed {
		model.InitChannelCache()
	}
}

func restoreExpiredOpenAIMultiKeys(channel *model.Channel, nowUnix int64) bool {
	if channel.ChannelInfo.MultiKeyStatusList == nil || channel.ChannelInfo.MultiKeyDisabledUntil == nil {
		return false
	}
	changed := false
	for idx, status := range channel.ChannelInfo.MultiKeyStatusList {
		if status != common.ChannelStatusAutoDisabled {
			continue
		}
		until := channel.ChannelInfo.MultiKeyDisabledUntil[idx]
		if until > 0 && until <= nowUnix {
			delete(channel.ChannelInfo.MultiKeyStatusList, idx)
			if channel.ChannelInfo.MultiKeyDisabledReason != nil {
				delete(channel.ChannelInfo.MultiKeyDisabledReason, idx)
			}
			if channel.ChannelInfo.MultiKeyDisabledTime != nil {
				delete(channel.ChannelInfo.MultiKeyDisabledTime, idx)
			}
			delete(channel.ChannelInfo.MultiKeyDisabledUntil, idx)
			changed = true
		}
	}
	if !changed {
		return false
	}
	beforeStatus := channel.Status
	channel.Status = common.ChannelStatusEnabled
	if err := channel.SaveWithoutKey(); err != nil {
		common.SysLog(fmt.Sprintf("failed to restore OpenAI upstream key limit channel: channel_id=%d, error=%v", channel.Id, err))
		return false
	}
	if beforeStatus != common.ChannelStatusEnabled {
		if err := model.UpdateAbilityStatus(channel.Id, true); err != nil {
			common.SysLog(fmt.Sprintf("failed to restore OpenAI upstream key limit ability status: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return true
}

func openAIDisabledUntilFromOtherInfo(info map[string]interface{}) (int64, bool) {
	value, ok := info["disabled_until"]
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	case string:
		parsed, err := strconv.ParseInt(v, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

var openAIUpstreamReserveLua = `
local now = tonumber(ARGV[1])
local rpm = tonumber(ARGV[2])
local tpm = tonumber(ARGV[3])
local rpd = tonumber(ARGV[4])
local tpd = tonumber(ARGV[5])
local requested_tokens = tonumber(ARGV[6])
local reservation_id = ARGV[7]
local minute_start = now - 60
local day_start = now - 86400

redis.call("ZREMRANGEBYSCORE", KEYS[1], "-inf", minute_start)
redis.call("ZREMRANGEBYSCORE", KEYS[2], "-inf", minute_start)
redis.call("ZREMRANGEBYSCORE", KEYS[3], "-inf", day_start)
redis.call("ZREMRANGEBYSCORE", KEYS[4], "-inf", day_start)

local function retry_at(key, window)
  local oldest = redis.call("ZRANGE", key, 0, 0, "WITHSCORES")
  if oldest[2] then return tonumber(oldest[2]) + window end
  return now + window
end

if rpm > 0 and redis.call("ZCARD", KEYS[1]) + 1 > rpm then
  return {0, "rpm", retry_at(KEYS[1], 60), ""}
end

local tpm_used = 0
local tpm_items = redis.call("ZRANGE", KEYS[2], 0, -1)
for _, item in ipairs(tpm_items) do
  local sep = string.find(item, ":")
  tpm_used = tpm_used + tonumber(string.sub(item, 1, sep - 1))
end
if tpm > 0 and tpm_used + requested_tokens > tpm then
  return {0, "tpm", retry_at(KEYS[2], 60), ""}
end

if rpd > 0 and redis.call("ZCARD", KEYS[3]) + 1 > rpd then
  return {0, "rpd", retry_at(KEYS[3], 86400), ""}
end

local tpd_used = 0
local tpd_items = redis.call("ZRANGE", KEYS[4], 0, -1)
for _, item in ipairs(tpd_items) do
  local sep = string.find(item, ":")
  tpd_used = tpd_used + tonumber(string.sub(item, 1, sep - 1))
end
if tpd > 0 and tpd_used + requested_tokens > tpd then
  return {0, "tpd", retry_at(KEYS[4], 86400), ""}
end

local request_member = "1:" .. reservation_id .. ":req"
local token_member = requested_tokens .. ":" .. reservation_id .. ":tok"
redis.call("ZADD", KEYS[1], now, request_member)
redis.call("ZADD", KEYS[3], now, request_member)
redis.call("ZADD", KEYS[2], now, token_member)
redis.call("ZADD", KEYS[4], now, token_member)
redis.call("HMSET", KEYS[5], "tpm_key", KEYS[2], "tpd_key", KEYS[4], "token_member", token_member)
redis.call("EXPIRE", KEYS[5], 90000)
redis.call("EXPIRE", KEYS[1], 90000)
redis.call("EXPIRE", KEYS[2], 90000)
redis.call("EXPIRE", KEYS[3], 90000)
redis.call("EXPIRE", KEYS[4], 90000)
return {1, "", 0, reservation_id}
`

var openAIUpstreamCommitLua = `
local reservation = redis.call("HGETALL", KEYS[1])
if #reservation == 0 then return 0 end
local data = {}
for i = 1, #reservation, 2 do data[reservation[i]] = reservation[i+1] end
local actual = tonumber(ARGV[1])
local old_member = data["token_member"]
if old_member and old_member ~= "" then
  local score = redis.call("ZSCORE", data["tpm_key"], old_member)
  if score then
    redis.call("ZREM", data["tpm_key"], old_member)
    redis.call("ZADD", data["tpm_key"], score, string.gsub(old_member, "^%d+:", actual .. ":"))
  end
  local day_score = redis.call("ZSCORE", data["tpd_key"], old_member)
  if day_score then
    redis.call("ZREM", data["tpd_key"], old_member)
    redis.call("ZADD", data["tpd_key"], day_score, string.gsub(old_member, "^%d+:", actual .. ":"))
  end
end
redis.call("DEL", KEYS[1])
return 1
`
