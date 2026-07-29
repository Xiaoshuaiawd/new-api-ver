package feishualert

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	maxVisibleAlerts    = 10
	maxSummaryRunes     = 200
	maxDescriptionRunes = 500
	maxFeishuBodyBytes  = 20 * 1024
)

var visibleLabelNames = []struct {
	key  string
	name string
}{
	{key: "channel_id", name: "渠道 ID"},
	{key: "channel_type", name: "渠道类型"},
	{key: "relay_format", name: "Relay 格式"},
	{key: "instance", name: "实例"},
	{key: "database", name: "数据库"},
	{key: "platform", name: "平台"},
	{key: "device", name: "设备"},
	{key: "mountpoint", name: "挂载点"},
	{key: "collector", name: "采集器"},
	{key: "error_type", name: "错误类型"},
}

type WebhookMessage struct {
	Version           string            `json:"version"`
	Status            string            `json:"status"`
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []Alert           `json:"alerts"`
	TruncatedAlerts   int               `json:"truncatedAlerts"`
}

type Alert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

type CardMessage struct {
	MsgType string `json:"msg_type"`
	Card    Card   `json:"card"`
}

type Card struct {
	Schema string     `json:"schema"`
	Config CardConfig `json:"config"`
	Body   CardBody   `json:"body"`
	Header CardHeader `json:"header"`
}

type CardConfig struct {
	UpdateMulti bool `json:"update_multi"`
}

type CardBody struct {
	Direction string        `json:"direction"`
	Padding   string        `json:"padding"`
	Elements  []CardElement `json:"elements"`
}

type CardElement struct {
	Tag       string `json:"tag"`
	Content   string `json:"content"`
	TextAlign string `json:"text_align"`
	TextSize  string `json:"text_size"`
}

type CardHeader struct {
	Title    CardText `json:"title"`
	Template string   `json:"template"`
	Padding  string   `json:"padding"`
}

type CardText struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

type CardMeta struct {
	Severity   string
	Status     string
	AlertCount int
}

func BuildCard(message WebhookMessage, now time.Time, location *time.Location) (CardMessage, CardMeta, error) {
	if len(message.Alerts) == 0 {
		return CardMessage{}, CardMeta{}, fmt.Errorf("alertmanager message has no alerts")
	}
	if location == nil {
		location = time.UTC
	}

	severity := severityOf(message)
	status := statusOf(message)
	totalAlerts := len(message.Alerts) + message.TruncatedAlerts
	visibleAlerts := message.Alerts
	if len(visibleAlerts) > maxVisibleAlerts {
		visibleAlerts = visibleAlerts[:maxVisibleAlerts]
	}

	for descriptionLimit := maxDescriptionRunes; descriptionLimit >= 0; descriptionLimit -= 50 {
		card := buildCard(message, visibleAlerts, totalAlerts, severity, status, now, location, maxSummaryRunes, descriptionLimit)
		payload, err := common.Marshal(card)
		if err != nil {
			return CardMessage{}, CardMeta{}, fmt.Errorf("marshal Feishu card: %w", err)
		}
		if len(payload) < maxFeishuBodyBytes {
			return card, CardMeta{
				Severity:   severity,
				Status:     status,
				AlertCount: totalAlerts,
			}, nil
		}
	}
	for summaryLimit := maxSummaryRunes - 25; summaryLimit >= 25; summaryLimit -= 25 {
		card := buildCard(message, visibleAlerts, totalAlerts, severity, status, now, location, summaryLimit, 0)
		payload, err := common.Marshal(card)
		if err != nil {
			return CardMessage{}, CardMeta{}, fmt.Errorf("marshal Feishu card: %w", err)
		}
		if len(payload) < maxFeishuBodyBytes {
			return card, CardMeta{
				Severity:   severity,
				Status:     status,
				AlertCount: totalAlerts,
			}, nil
		}
	}
	return CardMessage{}, CardMeta{}, fmt.Errorf("Feishu card exceeds %d-byte request limit", maxFeishuBodyBytes)
}

func buildCard(
	message WebhookMessage,
	alerts []Alert,
	totalAlerts int,
	severity string,
	status string,
	now time.Time,
	location *time.Location,
	summaryLimit int,
	descriptionLimit int,
) CardMessage {
	alertName := alertNameOf(message, alerts[0])
	titlePrefix := "警告"
	template := "orange"
	if status == "resolved" {
		titlePrefix = "告警恢复"
		template = "green"
	} else if severity == "critical" {
		titlePrefix = "严重告警"
		template = "red"
	}
	title := titlePrefix + " · " + alertName
	if totalAlerts > 1 {
		title = fmt.Sprintf("%s · %d 条告警", titlePrefix, totalAlerts)
	}

	var content strings.Builder
	if status == "firing" && severity == "critical" {
		content.WriteString("<at id=all></at>\n")
	}
	content.WriteString("**状态：** ")
	if status == "resolved" {
		content.WriteString("已恢复\n")
	} else {
		content.WriteString("触发中\n")
	}
	content.WriteString("**级别：** ")
	if severity == "critical" {
		content.WriteString("严重\n")
	} else {
		content.WriteString("警告\n")
	}
	if cluster := sanitizeCardText(message.CommonLabels["cluster"]); cluster != "" {
		content.WriteString("**集群：** ")
		content.WriteString(cluster)
		content.WriteString("\n")
	}
	if job := sanitizeCardText(message.CommonLabels["job"]); job != "" {
		content.WriteString("**任务：** ")
		content.WriteString(job)
		content.WriteString("\n")
	}
	if receiver := sanitizeCardText(message.Receiver); receiver != "" {
		content.WriteString("**接收器：** ")
		content.WriteString(receiver)
		content.WriteString("\n")
	}
	content.WriteString("**告警数量：** ")
	content.WriteString(fmt.Sprintf("%d\n", totalAlerts))
	content.WriteString("**通知时间：** ")
	content.WriteString(now.In(location).Format("2006-01-02 15:04:05 MST"))

	for _, alert := range alerts {
		content.WriteString("\n\n**")
		content.WriteString(sanitizeCardText(alertNameOf(message, alert)))
		content.WriteString("**\n")
		if summary := truncateRunes(sanitizeCardText(alert.Annotations["summary"]), summaryLimit); summary != "" {
			content.WriteString("摘要：")
			content.WriteString(summary)
			content.WriteString("\n")
		}
		if description := truncateRunes(sanitizeCardText(alert.Annotations["description"]), descriptionLimit); description != "" {
			content.WriteString("说明：")
			content.WriteString(description)
			content.WriteString("\n")
		}
		if value := truncateRunes(sanitizeCardText(alert.Annotations["value"]), 100); value != "" {
			content.WriteString("当前值：")
			content.WriteString(value)
			content.WriteString("\n")
		}
		for _, label := range visibleLabelNames {
			if value := truncateRunes(sanitizeCardText(alert.Labels[label.key]), 200); value != "" {
				content.WriteString(label.name)
				content.WriteString("：")
				content.WriteString(value)
				content.WriteString("\n")
			}
		}
		if !alert.StartsAt.IsZero() {
			content.WriteString("开始时间：")
			content.WriteString(alert.StartsAt.In(location).Format("2006-01-02 15:04:05 MST"))
			content.WriteString("\n")
		}
		if status == "resolved" && !alert.EndsAt.IsZero() {
			content.WriteString("恢复时间：")
			content.WriteString(alert.EndsAt.In(location).Format("2006-01-02 15:04:05 MST"))
			content.WriteString("\n")
			if !alert.StartsAt.IsZero() && !alert.EndsAt.Before(alert.StartsAt) {
				content.WriteString("持续时长：")
				content.WriteString(alert.EndsAt.Sub(alert.StartsAt).Round(time.Second).String())
				content.WriteString("\n")
			}
		}
	}
	if hiddenAlerts := totalAlerts - len(alerts); hiddenAlerts > 0 {
		content.WriteString("\n另有 ")
		content.WriteString(fmt.Sprintf("%d", hiddenAlerts))
		content.WriteString(" 条告警未展开，请前往 Alertmanager 查看。")
	}

	return CardMessage{
		MsgType: "interactive",
		Card: Card{
			Schema: "2.0",
			Config: CardConfig{
				UpdateMulti: true,
			},
			Body: CardBody{
				Direction: "vertical",
				Padding:   "12px 12px 12px 12px",
				Elements: []CardElement{
					{
						Tag:       "markdown",
						Content:   content.String(),
						TextAlign: "left",
						TextSize:  "normal",
					},
				},
			},
			Header: CardHeader{
				Title: CardText{
					Tag:     "plain_text",
					Content: title,
				},
				Template: template,
				Padding:  "12px 12px 12px 12px",
			},
		},
	}
}

func severityOf(message WebhookMessage) string {
	severity := message.CommonLabels["severity"]
	if severity == "" && len(message.Alerts) > 0 {
		severity = message.Alerts[0].Labels["severity"]
	}
	if severity == "critical" {
		return "critical"
	}
	return "warning"
}

func statusOf(message WebhookMessage) string {
	status := message.Status
	if status == "" && len(message.Alerts) > 0 {
		status = message.Alerts[0].Status
	}
	if status == "resolved" {
		return "resolved"
	}
	return "firing"
}

func alertNameOf(message WebhookMessage, alert Alert) string {
	if alertName := alert.Labels["alertname"]; alertName != "" {
		return alertName
	}
	if alertName := message.CommonLabels["alertname"]; alertName != "" {
		return alertName
	}
	return "未命名告警"
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

func sanitizeCardText(value string) string {
	value = strings.ReplaceAll(value, "<", "＜")
	return strings.ReplaceAll(value, ">", "＞")
}
