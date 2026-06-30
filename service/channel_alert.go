package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/bytedance/gopkg/util/gopool"
)

const (
	ChannelAlertEventTypeBalanceLow        = "channel_balance_low"
	ChannelAlertEventTypeMultiplierChanged = "channel_multiplier_changed"
)

type ChannelAlertEvent struct {
	Type               string  `json:"type"`
	ChannelID          int     `json:"channel_id"`
	ChannelName        string  `json:"channel_name,omitempty"`
	PreviousBalance    float64 `json:"previous_balance,omitempty"`
	CurrentBalance     float64 `json:"current_balance,omitempty"`
	BalanceThreshold   float64 `json:"balance_threshold,omitempty"`
	PreviousMultiplier float64 `json:"previous_multiplier,omitempty"`
	CurrentMultiplier  float64 `json:"current_multiplier,omitempty"`
	ObservedGroup      string  `json:"observed_group,omitempty"`
	ObservedTokenID    string  `json:"observed_token_id,omitempty"`
	OccurredAt         int64   `json:"occurred_at"`
}

var channelAlertState = struct {
	sync.Mutex
	lastAlertAt map[string]time.Time
	notifyFunc  func(event ChannelAlertEvent)
	now         func() time.Time
}{
	lastAlertAt: map[string]time.Time{},
	now:         time.Now,
}

func ResetChannelAlertStateForTest() {
	channelAlertState.Lock()
	defer channelAlertState.Unlock()

	channelAlertState.lastAlertAt = map[string]time.Time{}
	channelAlertState.notifyFunc = nil
	channelAlertState.now = time.Now
}

func SetChannelAlertNotifyFuncForTest(fn func(event ChannelAlertEvent)) {
	channelAlertState.Lock()
	defer channelAlertState.Unlock()
	channelAlertState.notifyFunc = fn
}

func SetChannelAlertNowFuncForTest(fn func() time.Time) {
	channelAlertState.Lock()
	defer channelAlertState.Unlock()
	if fn == nil {
		channelAlertState.now = time.Now
		return
	}
	channelAlertState.now = fn
}

func UpdateChannelBalanceWithAlert(channel *model.Channel, balance float64) {
	if channel == nil {
		return
	}
	previousBalance := channel.Balance
	channel.UpdateBalance(balance)
	channel.Balance = balance
	RecordChannelBalanceChange(channel, previousBalance, balance)
}

func RecordChannelBalanceChange(channel *model.Channel, previousBalance float64, currentBalance float64) {
	if channel == nil {
		return
	}
	setting := operation_setting.GetChannelAlertSetting()
	if !setting.Enabled || !setting.BalanceAlertEnabled || setting.BalanceThreshold <= 0 {
		return
	}
	if previousBalance < setting.BalanceThreshold || currentBalance >= setting.BalanceThreshold {
		return
	}

	emitChannelAlert(setting, ChannelAlertEvent{
		Type:             ChannelAlertEventTypeBalanceLow,
		ChannelID:        channel.Id,
		ChannelName:      channel.Name,
		PreviousBalance:  previousBalance,
		CurrentBalance:   currentBalance,
		BalanceThreshold: setting.BalanceThreshold,
	})
}

func RecordChannelMultiplierChange(channel *model.Channel, previous ChannelMultiplierSnapshot, current ChannelMultiplierSnapshot) {
	if channel == nil {
		return
	}
	setting := operation_setting.GetChannelAlertSetting()
	if !setting.Enabled || !setting.MultiplierChangeEnabled {
		return
	}
	if previous.State != ChannelMultiplierSnapshotHealthy || current.State != ChannelMultiplierSnapshotHealthy {
		return
	}
	if !multiplierChanged(previous.Multiplier, current.Multiplier) {
		return
	}

	emitChannelAlert(setting, ChannelAlertEvent{
		Type:               ChannelAlertEventTypeMultiplierChanged,
		ChannelID:          channel.Id,
		ChannelName:        channel.Name,
		PreviousMultiplier: previous.Multiplier,
		CurrentMultiplier:  current.Multiplier,
		ObservedGroup:      current.ObservedGroup,
		ObservedTokenID:    current.ObservedTokenID,
	})
}

func multiplierChanged(previous float64, current float64) bool {
	if normalizeMultiplier(previous) == 0 || normalizeMultiplier(current) == 0 {
		return false
	}
	return math.Abs(previous-current) > 1e-9
}

func emitChannelAlert(setting *operation_setting.ChannelAlertSetting, event ChannelAlertEvent) {
	if setting == nil {
		return
	}
	channelAlertState.Lock()
	now := channelAlertState.now()
	alertKey := fmt.Sprintf("%s:%d", event.Type, event.ChannelID)
	minInterval := time.Duration(setting.MinIntervalSeconds) * time.Second
	if minInterval <= 0 {
		minInterval = time.Duration(operation_setting.ChannelAlertDefaultMinIntervalSeconds) * time.Second
	}
	if last, ok := channelAlertState.lastAlertAt[alertKey]; ok && now.Sub(last) < minInterval {
		channelAlertState.Unlock()
		return
	}
	channelAlertState.lastAlertAt[alertKey] = now
	notify := channelAlertState.notifyFunc
	channelAlertState.Unlock()

	if event.OccurredAt == 0 {
		event.OccurredAt = now.Unix()
	}
	if notify != nil {
		notify(event)
		return
	}

	gopool.Go(func() {
		if err := sendChannelAlertNotifications(event); err != nil {
			common.SysLog(fmt.Sprintf("failed to send channel alert notification: %s", err.Error()))
		}
	})
}

func sendChannelAlertNotifications(event ChannelAlertEvent) error {
	setting := operation_setting.GetChannelAlertSetting()
	if !setting.Enabled {
		return nil
	}

	var firstErr error
	if setting.FeishuEnabled && strings.TrimSpace(setting.FeishuWebhookURL) != "" {
		if err := sendFeishuBotCard(setting.FeishuWebhookURL, setting.FeishuSecret, event); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if setting.DingTalkEnabled && strings.TrimSpace(setting.DingTalkWebhookURL) != "" {
		if err := sendDingTalkBotMarkdown(setting.DingTalkWebhookURL, setting.DingTalkSecret, event); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func channelAlertText(event ChannelAlertEvent) string {
	channel := formatChannelAlertChannel(event.ChannelID, event.ChannelName)
	switch event.Type {
	case ChannelAlertEventTypeBalanceLow:
		return fmt.Sprintf(
			"Channel balance is below threshold\nChannel: %s\nBalance: %s -> %s\nThreshold: %s",
			channel,
			formatChannelAlertFloat(event.PreviousBalance),
			formatChannelAlertFloat(event.CurrentBalance),
			formatChannelAlertFloat(event.BalanceThreshold),
		)
	case ChannelAlertEventTypeMultiplierChanged:
		parts := []string{
			"Channel upstream multiplier changed",
			"Channel: " + channel,
			fmt.Sprintf("Multiplier: %s -> %s", formatChannelAlertFloat(event.PreviousMultiplier), formatChannelAlertFloat(event.CurrentMultiplier)),
		}
		if event.ObservedGroup != "" {
			parts = append(parts, "Group: "+event.ObservedGroup)
		}
		if event.ObservedTokenID != "" {
			parts = append(parts, "Token: "+event.ObservedTokenID)
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprintf("Channel alert\nChannel: %s", channel)
	}
}

func formatChannelAlertChannel(channelID int, channelName string) string {
	if strings.TrimSpace(channelName) == "" {
		return fmt.Sprintf("#%d", channelID)
	}
	return fmt.Sprintf("#%d %s", channelID, strings.TrimSpace(channelName))
}

func formatChannelAlertFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

type channelAlertCardContent struct {
	Title       string
	Template    string
	AccentColor string
	Channel     string
	Summary     string
	Rows        []channelAlertCardRow
	OccurredAt  int64
}

type channelAlertCardRow struct {
	Label string
	Value string
}

type feishuBotCardPayload struct {
	Timestamp string        `json:"timestamp,omitempty"`
	Sign      string        `json:"sign,omitempty"`
	MsgType   string        `json:"msg_type"`
	Card      feishuBotCard `json:"card"`
}

type feishuBotCard struct {
	Config   feishuBotCardConfig    `json:"config"`
	Header   feishuBotCardHeader    `json:"header"`
	Elements []feishuBotCardElement `json:"elements"`
}

type feishuBotCardConfig struct {
	WideScreenMode bool `json:"wide_screen_mode"`
}

type feishuBotCardHeader struct {
	Template string         `json:"template"`
	Title    feishuCardText `json:"title"`
}

type feishuBotCardElement struct {
	Tag      string            `json:"tag"`
	Text     *feishuCardText   `json:"text,omitempty"`
	Fields   []feishuCardField `json:"fields,omitempty"`
	Elements []feishuCardText  `json:"elements,omitempty"`
}

type feishuCardField struct {
	IsShort bool           `json:"is_short"`
	Text    feishuCardText `json:"text"`
}

type feishuCardText struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

func sendFeishuBotCard(webhookURL string, secret string, event ChannelAlertEvent) error {
	payload := buildFeishuChannelAlertPayload(event)
	if strings.TrimSpace(secret) != "" {
		timestamp := strconv.FormatInt(channelAlertNow().Unix(), 10)
		payload.Timestamp = timestamp
		payload.Sign = signFeishuBot(timestamp, strings.TrimSpace(secret))
	}
	return postChannelAlertJSON(webhookURL, payload)
}

func buildFeishuChannelAlertPayload(event ChannelAlertEvent) feishuBotCardPayload {
	content := buildChannelAlertCardContent(event)
	mainContent := fmt.Sprintf(
		"**%s**\n<font color=\"%s\">%s</font>",
		content.Channel,
		content.AccentColor,
		content.Summary,
	)
	fields := make([]feishuCardField, 0, len(content.Rows))
	for _, row := range content.Rows {
		fields = append(fields, feishuCardField{
			IsShort: true,
			Text: feishuCardText{
				Tag:     "lark_md",
				Content: fmt.Sprintf("**%s**\n%s", row.Label, row.Value),
			},
		})
	}

	return feishuBotCardPayload{
		MsgType: "interactive",
		Card: feishuBotCard{
			Config: feishuBotCardConfig{
				WideScreenMode: true,
			},
			Header: feishuBotCardHeader{
				Template: content.Template,
				Title: feishuCardText{
					Tag:     "plain_text",
					Content: content.Title,
				},
			},
			Elements: []feishuBotCardElement{
				{
					Tag: "div",
					Text: &feishuCardText{
						Tag:     "lark_md",
						Content: mainContent,
					},
				},
				{
					Tag:    "div",
					Fields: fields,
				},
				{Tag: "hr"},
				{
					Tag: "note",
					Elements: []feishuCardText{
						{
							Tag:     "plain_text",
							Content: "触发时间 " + formatChannelAlertTime(content.OccurredAt),
						},
					},
				},
			},
		},
	}
}

type dingTalkBotMarkdownPayload struct {
	MsgType  string `json:"msgtype"`
	Markdown struct {
		Title string `json:"title"`
		Text  string `json:"text"`
	} `json:"markdown"`
}

func sendDingTalkBotMarkdown(webhookURL string, secret string, event ChannelAlertEvent) error {
	targetURL := webhookURL
	if strings.TrimSpace(secret) != "" {
		timestamp := strconv.FormatInt(channelAlertNow().UnixMilli(), 10)
		signedURL, err := appendDingTalkSignature(webhookURL, timestamp, strings.TrimSpace(secret))
		if err != nil {
			return err
		}
		targetURL = signedURL
	}

	payload := buildDingTalkChannelAlertPayload(event)
	return postChannelAlertJSON(targetURL, payload)
}

func buildDingTalkChannelAlertPayload(event ChannelAlertEvent) dingTalkBotMarkdownPayload {
	content := buildChannelAlertCardContent(event)
	lines := []string{
		"### " + content.Title,
		"",
		"> **" + content.Channel + "**",
		"",
		content.Summary,
		"",
	}
	for _, row := range content.Rows {
		lines = append(lines, fmt.Sprintf("**%s：** `%s`", row.Label, row.Value), "")
	}
	lines = append(lines, "---", fmt.Sprintf("###### 触发时间：%s", formatChannelAlertTime(content.OccurredAt)))

	payload := dingTalkBotMarkdownPayload{MsgType: "markdown"}
	payload.Markdown.Title = content.Title
	payload.Markdown.Text = strings.Join(lines, "\n")
	return payload
}

func buildChannelAlertCardContent(event ChannelAlertEvent) channelAlertCardContent {
	channel := formatChannelAlertChannel(event.ChannelID, event.ChannelName)
	occurredAt := event.OccurredAt
	if occurredAt == 0 {
		occurredAt = channelAlertNow().Unix()
	}

	switch event.Type {
	case ChannelAlertEventTypeBalanceLow:
		current := formatChannelAlertFloat(event.CurrentBalance)
		threshold := formatChannelAlertFloat(event.BalanceThreshold)
		shortfall := math.Max(event.BalanceThreshold-event.CurrentBalance, 0)
		return channelAlertCardContent{
			Title:       "渠道余额不足",
			Template:    "red",
			AccentColor: "red",
			Channel:     channel,
			Summary:     fmt.Sprintf("当前余额 %s 已低于告警阈值 %s，请及时处理。", current, threshold),
			Rows: []channelAlertCardRow{
				{Label: "当前余额", Value: current},
				{Label: "原余额", Value: formatChannelAlertFloat(event.PreviousBalance)},
				{Label: "告警阈值", Value: threshold},
				{Label: "需补足", Value: formatChannelAlertFloat(shortfall)},
			},
			OccurredAt: occurredAt,
		}
	case ChannelAlertEventTypeMultiplierChanged:
		previous := formatChannelAlertFloat(event.PreviousMultiplier)
		current := formatChannelAlertFloat(event.CurrentMultiplier)
		change := formatChannelAlertPercentChange(event.PreviousMultiplier, event.CurrentMultiplier)
		rows := []channelAlertCardRow{
			{Label: "原倍率", Value: previous},
			{Label: "新倍率", Value: current},
			{Label: "变化", Value: change},
		}
		if event.ObservedGroup != "" {
			rows = append(rows, channelAlertCardRow{Label: "分组", Value: event.ObservedGroup})
		}
		if event.ObservedTokenID != "" {
			rows = append(rows, channelAlertCardRow{Label: "Token", Value: event.ObservedTokenID})
		}
		return channelAlertCardContent{
			Title:       "渠道倍率变化",
			Template:    "orange",
			AccentColor: "orange",
			Channel:     channel,
			Summary:     fmt.Sprintf("倍率已从 %s 调整为 %s，变化 %s。", previous, current, change),
			Rows:        rows,
			OccurredAt:  occurredAt,
		}
	default:
		return channelAlertCardContent{
			Title:       "渠道告警",
			Template:    "blue",
			AccentColor: "blue",
			Channel:     channel,
			Summary:     "检测到渠道健康事件，请查看系统状态。",
			OccurredAt:  occurredAt,
		}
	}
}

func formatChannelAlertPercentChange(previous float64, current float64) string {
	if math.Abs(previous) <= 1e-9 {
		return "N/A"
	}
	change := (current - previous) / previous * 100
	if math.Abs(change) < 0.01 {
		return "0%"
	}
	formatted := strconv.FormatFloat(change, 'f', 2, 64)
	formatted = strings.TrimRight(strings.TrimRight(formatted, "0"), ".")
	return formatted + "%"
}

func formatChannelAlertTime(timestamp int64) string {
	if timestamp <= 0 {
		return "-"
	}
	return time.Unix(timestamp, 0).Format("2006-01-02 15:04:05")
}

func channelAlertNow() time.Time {
	channelAlertState.Lock()
	defer channelAlertState.Unlock()
	return channelAlertState.now()
}

func signFeishuBot(timestamp string, secret string) string {
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func appendDingTalkSignature(webhookURL string, timestamp string, secret string) (string, error) {
	parsed, err := url.Parse(webhookURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("timestamp", timestamp)
	query.Set("sign", signDingTalkBot(timestamp, secret))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func signDingTalkBot(timestamp string, secret string) string {
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func postChannelAlertJSON(webhookURL string, payload any) error {
	payloadBytes, err := common.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal channel alert payload: %v", err)
	}

	if system_setting.EnableWorker() {
		workerReq := &WorkerRequest{
			URL:    webhookURL,
			Key:    system_setting.WorkerValidKey,
			Method: http.MethodPost,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: payloadBytes,
		}
		resp, err := DoWorkerRequest(workerReq)
		if err != nil {
			return fmt.Errorf("failed to send channel alert through worker: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("channel alert webhook failed with status code: %d", resp.StatusCode)
		}
		return nil
	}

	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(webhookURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return fmt.Errorf("request reject: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create channel alert request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send channel alert request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("channel alert webhook failed with status code: %d", resp.StatusCode)
	}
	return nil
}
