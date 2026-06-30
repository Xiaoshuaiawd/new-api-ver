package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withChannelAlertTestSettings(t *testing.T) *operation_setting.ChannelAlertSetting {
	t.Helper()

	setting := operation_setting.GetChannelAlertSetting()
	original := *setting
	*setting = operation_setting.ChannelAlertSetting{
		Enabled:                 true,
		BalanceAlertEnabled:     true,
		MultiplierChangeEnabled: true,
		BalanceThreshold:        10,
		MinIntervalSeconds:      60,
	}
	ResetChannelAlertStateForTest()
	t.Cleanup(func() {
		*setting = original
		ResetChannelAlertStateForTest()
	})
	return setting
}

func captureChannelAlertEvents(t *testing.T) *[]ChannelAlertEvent {
	t.Helper()

	events := make([]ChannelAlertEvent, 0, 4)
	SetChannelAlertNotifyFuncForTest(func(event ChannelAlertEvent) {
		events = append(events, event)
	})
	return &events
}

func marshalChannelAlertTestJSON(t *testing.T, value any) string {
	t.Helper()

	data, err := common.Marshal(value)
	require.NoError(t, err)
	return string(data)
}

func TestChannelBalanceAlertRequiresEnabledThresholdCrossing(t *testing.T) {
	setting := withChannelAlertTestSettings(t)
	events := captureChannelAlertEvents(t)

	channel := &model.Channel{Id: 101, Name: "low-balance"}

	RecordChannelBalanceChange(channel, 25, 12)
	RecordChannelBalanceChange(channel, 9, 8)
	setting.Enabled = false
	RecordChannelBalanceChange(channel, 25, 5)
	setting.Enabled = true
	setting.BalanceThreshold = 0
	RecordChannelBalanceChange(channel, 25, 5)

	require.Empty(t, *events)
}

func TestChannelBalanceAlertEmitsOnlyWhenBalanceFallsBelowThreshold(t *testing.T) {
	withChannelAlertTestSettings(t)
	events := captureChannelAlertEvents(t)

	channel := &model.Channel{Id: 102, Name: "paid-upstream"}

	RecordChannelBalanceChange(channel, 10, 9.99)

	require.Len(t, *events, 1)
	event := (*events)[0]
	assert.Equal(t, ChannelAlertEventTypeBalanceLow, event.Type)
	assert.Equal(t, 102, event.ChannelID)
	assert.Equal(t, "paid-upstream", event.ChannelName)
	assert.Equal(t, 10.0, event.PreviousBalance)
	assert.Equal(t, 9.99, event.CurrentBalance)
	assert.Equal(t, 10.0, event.BalanceThreshold)
}

func TestChannelMultiplierAlertEmitsWhenHealthyMultiplierChanges(t *testing.T) {
	withChannelAlertTestSettings(t)
	events := captureChannelAlertEvents(t)

	channel := &model.Channel{Id: 103, Name: "multiplier-upstream"}
	previous := ChannelMultiplierSnapshot{
		ChannelID:  103,
		State:      ChannelMultiplierSnapshotHealthy,
		Multiplier: 0.7,
	}
	next := ChannelMultiplierSnapshot{
		ChannelID:       103,
		State:           ChannelMultiplierSnapshotHealthy,
		Multiplier:      0.9,
		ObservedGroup:   "premium",
		ObservedTokenID: "token-1",
	}

	RecordChannelMultiplierChange(channel, previous, next)

	require.Len(t, *events, 1)
	event := (*events)[0]
	assert.Equal(t, ChannelAlertEventTypeMultiplierChanged, event.Type)
	assert.Equal(t, 103, event.ChannelID)
	assert.Equal(t, 0.7, event.PreviousMultiplier)
	assert.Equal(t, 0.9, event.CurrentMultiplier)
	assert.Equal(t, "premium", event.ObservedGroup)
	assert.Equal(t, "token-1", event.ObservedTokenID)
}

func TestChannelMultiplierAlertIgnoresMissingOrUnchangedHealthySnapshot(t *testing.T) {
	setting := withChannelAlertTestSettings(t)
	events := captureChannelAlertEvents(t)

	channel := &model.Channel{Id: 104, Name: "unchanged"}
	previous := ChannelMultiplierSnapshot{
		ChannelID:  104,
		State:      ChannelMultiplierSnapshotHealthy,
		Multiplier: 1.2,
	}

	RecordChannelMultiplierChange(channel, ChannelMultiplierSnapshot{}, previous)
	RecordChannelMultiplierChange(channel, previous, previous)
	setting.MultiplierChangeEnabled = false
	RecordChannelMultiplierChange(channel, previous, ChannelMultiplierSnapshot{
		ChannelID:  104,
		State:      ChannelMultiplierSnapshotHealthy,
		Multiplier: 1.4,
	})

	require.Empty(t, *events)
}

func TestChannelAlertCooldownSuppressesRepeatedEvents(t *testing.T) {
	withChannelAlertTestSettings(t)
	events := captureChannelAlertEvents(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelAlertNowFuncForTest(func() time.Time { return now })

	channel := &model.Channel{Id: 105, Name: "cooldown"}

	RecordChannelBalanceChange(channel, 20, 9)
	RecordChannelBalanceChange(channel, 20, 8)
	now = now.Add(61 * time.Second)
	RecordChannelBalanceChange(channel, 20, 7)

	require.Len(t, *events, 2)
	assert.Equal(t, 9.0, (*events)[0].CurrentBalance)
	assert.Equal(t, 7.0, (*events)[1].CurrentBalance)
}

func TestSendChannelAlertNotificationsPostsFeishuAndDingTalkPayloads(t *testing.T) {
	setting := withChannelAlertTestSettings(t)
	SetChannelAlertNowFuncForTest(func() time.Time {
		return time.Unix(1_700_000_000, 0)
	})

	fetchSetting := system_setting.GetFetchSetting()
	originalFetchSetting := *fetchSetting
	*fetchSetting = system_setting.FetchSetting{
		EnableSSRFProtection: false,
		AllowPrivateIp:       true,
	}
	t.Cleanup(func() {
		*fetchSetting = originalFetchSetting
	})

	type capturedRequest struct {
		path  string
		query string
		body  []byte
	}
	requests := make(chan capturedRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		requests <- capturedRequest{
			path:  r.URL.Path,
			query: r.URL.RawQuery,
			body:  body,
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	setting.FeishuEnabled = true
	setting.FeishuWebhookURL = server.URL + "/feishu"
	setting.FeishuSecret = "feishu-secret"
	setting.DingTalkEnabled = true
	setting.DingTalkWebhookURL = server.URL + "/dingtalk?access_token=test"
	setting.DingTalkSecret = "dingtalk-secret"

	err := sendChannelAlertNotifications(ChannelAlertEvent{
		Type:             ChannelAlertEventTypeBalanceLow,
		ChannelID:        106,
		ChannelName:      "paid-upstream",
		PreviousBalance:  12,
		CurrentBalance:   8,
		BalanceThreshold: 10,
		OccurredAt:       1_700_000_000,
	})

	require.NoError(t, err)

	var feishuBody map[string]any
	var dingTalkBody map[string]any
	for i := 0; i < 2; i++ {
		req := <-requests
		switch req.path {
		case "/feishu":
			require.NoError(t, common.Unmarshal(req.body, &feishuBody))
			assert.Equal(t, "1700000000", feishuBody["timestamp"])
			assert.NotEmpty(t, feishuBody["sign"])
		case "/dingtalk":
			parsedQuery, err := url.ParseQuery(req.query)
			require.NoError(t, err)
			require.NoError(t, common.Unmarshal(req.body, &dingTalkBody))
			assert.Equal(t, "1700000000000", parsedQuery.Get("timestamp"))
			assert.NotEmpty(t, parsedQuery.Get("sign"))
		default:
			t.Fatalf("unexpected request path: %s", req.path)
		}
	}

	assert.Equal(t, "interactive", feishuBody["msg_type"])
	feishuCard, ok := feishuBody["card"].(map[string]any)
	require.True(t, ok)
	feishuHeader, ok := feishuCard["header"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "red", feishuHeader["template"])
	feishuTitle, ok := feishuHeader["title"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "渠道余额不足", feishuTitle["content"])
	feishuElements, ok := feishuCard["elements"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, feishuElements)
	feishuElementsJSON := marshalChannelAlertTestJSON(t, feishuElements)
	assert.Contains(t, feishuElementsJSON, "paid-upstream")
	assert.Contains(t, feishuElementsJSON, "当前余额")
	assert.Contains(t, feishuElementsJSON, "8")

	assert.Equal(t, "markdown", dingTalkBody["msgtype"])
	dingTalkMarkdown, ok := dingTalkBody["markdown"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "渠道余额不足", dingTalkMarkdown["title"])
	assert.Contains(t, dingTalkMarkdown["text"], "paid-upstream")
	assert.Contains(t, dingTalkMarkdown["text"], "当前余额")
	assert.Contains(t, dingTalkMarkdown["text"], "8")
}

func TestChannelAlertCardContentForMultiplierChange(t *testing.T) {
	SetChannelAlertNowFuncForTest(func() time.Time {
		return time.Unix(1_700_000_000, 0)
	})
	t.Cleanup(ResetChannelAlertStateForTest)

	feishuPayload := buildFeishuChannelAlertPayload(ChannelAlertEvent{
		Type:               ChannelAlertEventTypeMultiplierChanged,
		ChannelID:          6,
		ChannelName:        "junche-pro",
		PreviousMultiplier: 0.1,
		CurrentMultiplier:  0.06,
		ObservedGroup:      "codex-plus",
		ObservedTokenID:    "162",
		OccurredAt:         1_700_000_000,
	})

	assert.Equal(t, "interactive", feishuPayload.MsgType)
	assert.Equal(t, "orange", feishuPayload.Card.Header.Template)
	assert.Equal(t, "渠道倍率变化", feishuPayload.Card.Header.Title.Content)
	payloadJSON := marshalChannelAlertTestJSON(t, feishuPayload)
	assert.Contains(t, payloadJSON, "#6 junche-pro")
	assert.Contains(t, payloadJSON, "原倍率")
	assert.Contains(t, payloadJSON, "0.1")
	assert.Contains(t, payloadJSON, "新倍率")
	assert.Contains(t, payloadJSON, "0.06")
	assert.Contains(t, payloadJSON, "变化")
	assert.Contains(t, payloadJSON, "-40%")
	assert.Contains(t, payloadJSON, "codex-plus")
	assert.Contains(t, payloadJSON, "162")

	dingTalkPayload := buildDingTalkChannelAlertPayload(ChannelAlertEvent{
		Type:               ChannelAlertEventTypeMultiplierChanged,
		ChannelID:          6,
		ChannelName:        "junche-pro",
		PreviousMultiplier: 0.1,
		CurrentMultiplier:  0.06,
		ObservedGroup:      "codex-plus",
		ObservedTokenID:    "162",
		OccurredAt:         1_700_000_000,
	})

	assert.Equal(t, "markdown", dingTalkPayload.MsgType)
	assert.Equal(t, "渠道倍率变化", dingTalkPayload.Markdown.Title)
	assert.Contains(t, dingTalkPayload.Markdown.Text, "#6 junche-pro")
	assert.Contains(t, dingTalkPayload.Markdown.Text, "原倍率")
	assert.Contains(t, dingTalkPayload.Markdown.Text, "0.1")
	assert.Contains(t, dingTalkPayload.Markdown.Text, "新倍率")
	assert.Contains(t, dingTalkPayload.Markdown.Text, "0.06")
	assert.Contains(t, dingTalkPayload.Markdown.Text, "变化")
	assert.Contains(t, dingTalkPayload.Markdown.Text, "-40%")
}
