package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	defaultAlertmanagerFeishuRequestTimeout = 10 * time.Second
	defaultAlertmanagerFeishuMaxAlerts      = 10
	maxAlertmanagerWebhookBodyBytes         = 2 << 20
	maxAlertDescriptionLength               = 200
)

type AlertmanagerFeishuAdapterConfig struct {
	WebhookURL          string
	BearerToken         string
	MessagePrefix       string
	MinInterval         time.Duration
	MaxAlertsPerMessage int
	RequestTimeout      time.Duration
}

type AlertmanagerWebhookPayload struct {
	Receiver          string                     `json:"receiver"`
	Status            string                     `json:"status"`
	Alerts            []AlertmanagerWebhookAlert `json:"alerts"`
	GroupLabels       map[string]string          `json:"groupLabels"`
	CommonLabels      map[string]string          `json:"commonLabels"`
	CommonAnnotations map[string]string          `json:"commonAnnotations"`
	ExternalURL       string                     `json:"externalURL"`
	Version           string                     `json:"version"`
	GroupKey          string                     `json:"groupKey"`
	TruncatedAlerts   int                        `json:"truncatedAlerts"`
}

type AlertmanagerWebhookAlert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     string            `json:"startsAt"`
	EndsAt       string            `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

type FeishuWebhookMessage struct {
	MsgType string                   `json:"msg_type"`
	Content FeishuWebhookTextContent `json:"content"`
}

type FeishuWebhookTextContent struct {
	Text string `json:"text"`
}

type feishuWebhookResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

type AlertmanagerFeishuAdapter struct {
	config   AlertmanagerFeishuAdapterConfig
	client   *http.Client
	now      func() time.Time
	mu       sync.Mutex
	lastSent map[string]time.Time
}

func NewAlertmanagerFeishuAdapter(config AlertmanagerFeishuAdapterConfig) (*AlertmanagerFeishuAdapter, error) {
	config.WebhookURL = strings.TrimSpace(config.WebhookURL)
	config.BearerToken = strings.TrimSpace(config.BearerToken)
	config.MessagePrefix = strings.TrimSpace(config.MessagePrefix)
	if config.WebhookURL == "" {
		return nil, fmt.Errorf("webhook url is required")
	}
	if config.MaxAlertsPerMessage <= 0 {
		config.MaxAlertsPerMessage = defaultAlertmanagerFeishuMaxAlerts
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = defaultAlertmanagerFeishuRequestTimeout
	}
	if config.MinInterval < 0 {
		config.MinInterval = 0
	}

	return &AlertmanagerFeishuAdapter{
		config: config,
		client: &http.Client{
			Timeout: config.RequestTimeout,
		},
		now:      time.Now,
		lastSent: make(map[string]time.Time),
	}, nil
}

func (a *AlertmanagerFeishuAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAlertmanagerWebhookBodyBytes))
	if err != nil {
		http.Error(w, "read request body failed", http.StatusBadRequest)
		return
	}

	var payload AlertmanagerWebhookPayload
	if err := common.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid alertmanager webhook payload", http.StatusBadRequest)
		return
	}
	if len(payload.Alerts) == 0 {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("empty alert list"))
		return
	}

	message, suppressionKey := a.buildMessage(payload)
	if a.shouldSuppress(suppressionKey) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("suppressed by cooldown"))
		return
	}

	if err := a.sendTextMessage(message); err != nil {
		common.SysError(fmt.Sprintf("alertmanager feishu adapter send failed: %v", err))
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	a.markSent(suppressionKey)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (a *AlertmanagerFeishuAdapter) authorized(r *http.Request) bool {
	if a.config.BearerToken == "" {
		return true
	}
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	expected := "Bearer " + a.config.BearerToken
	return authHeader == expected
}

func (a *AlertmanagerFeishuAdapter) buildMessage(payload AlertmanagerWebhookPayload) (string, string) {
	alertname := firstNonEmpty(
		payload.CommonLabels["alertname"],
		payload.GroupLabels["alertname"],
		firstAlertLabel(payload.Alerts, "alertname"),
	)
	site := firstNonEmpty(
		payload.CommonLabels["site"],
		payload.GroupLabels["site"],
		firstAlertLabel(payload.Alerts, "site"),
	)
	severity := firstNonEmpty(
		payload.CommonLabels["severity"],
		payload.GroupLabels["severity"],
		firstAlertLabel(payload.Alerts, "severity"),
	)
	status := firstNonEmpty(payload.Status, firstAlertStatus(payload.Alerts), "firing")

	alerts := append([]AlertmanagerWebhookAlert(nil), payload.Alerts...)
	sort.Slice(alerts, func(i, j int) bool {
		leftChannel := firstNonEmpty(alerts[i].Labels["channel_name"], alerts[i].Labels["channel_id"], alerts[i].Fingerprint)
		rightChannel := firstNonEmpty(alerts[j].Labels["channel_name"], alerts[j].Labels["channel_id"], alerts[j].Fingerprint)
		if leftChannel == rightChannel {
			return alerts[i].Fingerprint < alerts[j].Fingerprint
		}
		return leftChannel < rightChannel
	})

	lines := make([]string, 0, 8+len(alerts))
	if a.config.MessagePrefix != "" {
		lines = append(lines, a.config.MessagePrefix)
	}
	lines = append(lines,
		fmt.Sprintf("状态: %s", status),
		fmt.Sprintf("告警: %s", firstNonEmpty(alertname, "unknown")),
		fmt.Sprintf("级别: %s", firstNonEmpty(severity, "unknown")),
		fmt.Sprintf("站点: %s", firstNonEmpty(site, "unknown")),
		fmt.Sprintf("数量: 共 %d 条", len(alerts)),
	)
	if summary := strings.TrimSpace(payload.CommonAnnotations["summary"]); summary != "" {
		lines = append(lines, fmt.Sprintf("摘要: %s", summary))
	}

	limit := a.config.MaxAlertsPerMessage
	if limit > len(alerts) {
		limit = len(alerts)
	}
	for i := 0; i < limit; i++ {
		lines = append(lines, formatAlertLine(i+1, alerts[i]))
	}
	hiddenAlerts := len(alerts) - limit
	if hiddenAlerts > 0 {
		lines = append(lines, fmt.Sprintf("其余 %d 条渠道告警已省略，请到 Grafana 查看明细。", hiddenAlerts))
	}
	if payload.TruncatedAlerts > 0 {
		lines = append(lines, fmt.Sprintf("Alertmanager 还有 %d 条被截断未包含在本次回调里。", payload.TruncatedAlerts))
	}

	suppressionKey := payload.GroupKey
	if strings.TrimSpace(suppressionKey) == "" {
		suppressionKey = strings.Join([]string{status, alertname, severity, site}, "|")
	}

	return strings.Join(lines, "\n"), suppressionKey
}

func formatAlertLine(index int, alert AlertmanagerWebhookAlert) string {
	channelName := strings.TrimSpace(alert.Labels["channel_name"])
	channelID := strings.TrimSpace(alert.Labels["channel_id"])
	summary := strings.TrimSpace(alert.Annotations["summary"])
	description := truncateText(strings.TrimSpace(alert.Annotations["description"]), maxAlertDescriptionLength)

	parts := []string{fmt.Sprintf("%d.", index)}
	if channelName != "" {
		parts = append(parts, fmt.Sprintf("渠道=%s", channelName))
	}
	if channelID != "" {
		parts = append(parts, fmt.Sprintf("ID=%s", channelID))
	}
	if summary != "" {
		parts = append(parts, fmt.Sprintf("摘要=%s", summary))
	}
	if description != "" {
		parts = append(parts, fmt.Sprintf("详情=%s", description))
	}
	if len(parts) == 1 {
		parts = append(parts, fmt.Sprintf("指纹=%s", alert.Fingerprint))
	}
	return strings.Join(parts, " | ")
}

func (a *AlertmanagerFeishuAdapter) shouldSuppress(key string) bool {
	if a.config.MinInterval <= 0 {
		return false
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	lastSentAt, ok := a.lastSent[key]
	if !ok {
		return false
	}
	return a.now().Sub(lastSentAt) < a.config.MinInterval
}

func (a *AlertmanagerFeishuAdapter) markSent(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastSent[key] = a.now()
}

func (a *AlertmanagerFeishuAdapter) sendTextMessage(text string) error {
	payloadBytes, err := common.Marshal(FeishuWebhookMessage{
		MsgType: "text",
		Content: FeishuWebhookTextContent{
			Text: truncateText(text, 4000),
		},
	})
	if err != nil {
		return fmt.Errorf("marshal feishu payload failed: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, a.config.WebhookURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("build feishu request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("request feishu webhook failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if readErr != nil {
		return fmt.Errorf("read feishu response failed: %w", readErr)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("feishu webhook responded with status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	if len(bytes.TrimSpace(respBody)) == 0 {
		return nil
	}

	var feishuResp feishuWebhookResponse
	if err := common.Unmarshal(respBody, &feishuResp); err == nil && feishuResp.Code != 0 {
		return fmt.Errorf("feishu webhook rejected request code=%d msg=%s", feishuResp.Code, feishuResp.Msg)
	}

	return nil
}

func firstAlertLabel(alerts []AlertmanagerWebhookAlert, key string) string {
	for _, alert := range alerts {
		if value := strings.TrimSpace(alert.Labels[key]); value != "" {
			return value
		}
	}
	return ""
}

func firstAlertStatus(alerts []AlertmanagerWebhookAlert) string {
	for _, alert := range alerts {
		if status := strings.TrimSpace(alert.Status); status != "" {
			return status
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func truncateText(text string, maxLen int) string {
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}
