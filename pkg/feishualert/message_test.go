package feishualert

import (
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCardCriticalFiring(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	now := time.Date(2026, time.July, 29, 15, 30, 0, 0, time.UTC)
	message := WebhookMessage{
		Version:  "4",
		Status:   "firing",
		Receiver: "new-api-webhook-critical",
		CommonLabels: map[string]string{
			"alertname":  "NewAPIChannelNoSuccess",
			"severity":   "critical",
			"cluster":    "default",
			"job":        "new-api",
			"channel_id": "12",
			"token":      "secret-value",
		},
		Alerts: []Alert{
			{
				Status: "firing",
				Labels: map[string]string{
					"alertname":  "NewAPIChannelNoSuccess",
					"severity":   "critical",
					"channel_id": "12",
					"token":      "secret-value",
				},
				Annotations: map[string]string{
					"summary":     "渠道连续请求无成功",
					"description": "渠道 12 在最近 5 分钟没有成功请求。",
				},
				StartsAt: now.Add(-5 * time.Minute),
			},
		},
	}

	card, meta, err := BuildCard(message, now, shanghai)
	require.NoError(t, err)
	assert.Equal(t, "critical", meta.Severity)
	assert.Equal(t, "firing", meta.Status)
	assert.Equal(t, 1, meta.AlertCount)
	assert.Equal(t, "interactive", card.MsgType)
	assert.Equal(t, "2.0", card.Card.Schema)
	assert.Equal(t, "red", card.Card.Header.Template)
	require.Len(t, card.Card.Body.Elements, 1)
	content := card.Card.Body.Elements[0].Content
	assert.Contains(t, content, "<at id=all></at>")
	assert.Contains(t, content, "渠道 ID：12")
	assert.Contains(t, content, "渠道连续请求无成功")
	assert.NotContains(t, content, "secret-value")
}

func TestBuildCardStatusPresentation(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	now := time.Date(2026, time.July, 29, 15, 30, 0, 0, time.UTC)

	tests := []struct {
		name       string
		status     string
		severity   string
		template   string
		title      string
		mentionAll bool
	}{
		{name: "warning firing", status: "firing", severity: "warning", template: "orange", title: "警告 · TestAlert"},
		{name: "critical firing", status: "firing", severity: "critical", template: "red", title: "严重告警 · TestAlert", mentionAll: true},
		{name: "critical resolved", status: "resolved", severity: "critical", template: "green", title: "告警恢复 · TestAlert"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message := WebhookMessage{
				Version: "4",
				Status:  test.status,
				CommonLabels: map[string]string{
					"alertname": "TestAlert",
					"severity":  test.severity,
				},
				Alerts: []Alert{
					{
						Status: test.status,
						Labels: map[string]string{
							"alertname": "TestAlert",
							"severity":  test.severity,
						},
						Annotations: map[string]string{"summary": "测试告警"},
						StartsAt:    now.Add(-10 * time.Minute),
						EndsAt:      now,
					},
				},
			}

			card, meta, err := BuildCard(message, now, shanghai)
			require.NoError(t, err)
			assert.Equal(t, test.template, card.Card.Header.Template)
			assert.Equal(t, test.title, card.Card.Header.Title.Content)
			assert.Equal(t, test.status, meta.Status)
			assert.Equal(t, test.severity, meta.Severity)
			content := card.Card.Body.Elements[0].Content
			assert.Equal(t, test.mentionAll, strings.Contains(content, "<at id=all></at>"))
			if test.status == "resolved" {
				assert.Contains(t, content, "**状态：** 已恢复")
				assert.Contains(t, content, "持续时长：10m0s")
			}
		})
	}
}

func TestBuildCardDefaultsMissingSeverityToWarning(t *testing.T) {
	message := WebhookMessage{
		Version: "4",
		Status:  "firing",
		Alerts: []Alert{
			{
				Status:      "firing",
				Labels:      map[string]string{"alertname": "MissingSeverity"},
				Annotations: map[string]string{"summary": "缺少级别"},
			},
		},
	}

	card, meta, err := BuildCard(message, time.Now(), time.UTC)
	require.NoError(t, err)
	assert.Equal(t, "warning", meta.Severity)
	assert.Equal(t, "orange", card.Card.Header.Template)
	assert.NotContains(t, card.Card.Body.Elements[0].Content, "<at id=all></at>")
}

func TestBuildCardTruncatesAlertGroupAndUnsafeContent(t *testing.T) {
	alerts := make([]Alert, 0, 12)
	for index := 0; index < 12; index++ {
		alerts = append(alerts, Alert{
			Status: "firing",
			Labels: map[string]string{
				"alertname":  "GroupedAlert",
				"severity":   "warning",
				"channel_id": string(rune('A' + index)),
				"api_key":    "must-not-leak",
			},
			Annotations: map[string]string{
				"summary":     "普通告警 <at id=all></at> " + strings.Repeat("摘要", 300),
				"description": strings.Repeat("详情", 700),
			},
		})
	}
	message := WebhookMessage{
		Version:      "4",
		Status:       "firing",
		Receiver:     "new-api-webhook-warning",
		CommonLabels: map[string]string{"severity": "warning", "cluster": "default", "job": "new-api"},
		Alerts:       alerts,
	}

	card, meta, err := BuildCard(message, time.Now(), time.UTC)
	require.NoError(t, err)
	assert.Equal(t, 12, meta.AlertCount)
	assert.Equal(t, "警告 · 12 条告警", card.Card.Header.Title.Content)
	content := card.Card.Body.Elements[0].Content
	assert.Equal(t, 10, strings.Count(content, "**GroupedAlert**"))
	assert.Contains(t, content, "另有 2 条告警未展开")
	assert.NotContains(t, content, "<at id=all></at>")
	assert.NotContains(t, content, "must-not-leak")
	payload, err := common.Marshal(card)
	require.NoError(t, err)
	assert.Less(t, len(payload), 20*1024)
}

func TestBuildCardRejectsEmptyAlerts(t *testing.T) {
	_, _, err := BuildCard(WebhookMessage{Version: "4", Status: "firing"}, time.Now(), time.UTC)
	require.Error(t, err)
}
