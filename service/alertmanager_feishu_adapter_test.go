package service

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestAlertmanagerFeishuAdapterRejectsUnauthorizedRequests(t *testing.T) {
	t.Parallel()

	var downstreamCalls atomic.Int32
	feishuServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer feishuServer.Close()

	adapter, err := NewAlertmanagerFeishuAdapter(AlertmanagerFeishuAdapterConfig{
		WebhookURL:  feishuServer.URL,
		BearerToken: "adapter-token",
	})
	require.NoError(t, err)

	payload := mustMarshalAlertmanagerWebhook(t, sampleAlertmanagerWebhookPayload())
	req := httptest.NewRequest(http.MethodPost, "/alertmanager/feishu", bytes.NewReader(payload))
	rec := httptest.NewRecorder()

	adapter.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, int32(0), downstreamCalls.Load())
}

func TestAlertmanagerFeishuAdapterAggregatesAlertsIntoOneFeishuMessage(t *testing.T) {
	t.Parallel()

	var received FeishuWebhookMessage
	feishuServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, common.Unmarshal(body, &received))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer feishuServer.Close()

	adapter, err := NewAlertmanagerFeishuAdapter(AlertmanagerFeishuAdapterConfig{
		WebhookURL:          feishuServer.URL,
		MessagePrefix:       "[监控告警]",
		MinInterval:         0,
		MaxAlertsPerMessage: 10,
	})
	require.NoError(t, err)

	payload := sampleAlertmanagerWebhookPayload()
	payload.Alerts = []AlertmanagerWebhookAlert{
		{
			Status: "firing",
			Labels: map[string]string{
				"alertname":    "NewAPIChannelHasErrors",
				"site":         "site-a:3000",
				"severity":     "critical",
				"channel_id":   "1001",
				"channel_name": "alpha",
			},
			Annotations: map[string]string{
				"summary":     "渠道 alpha 有报错",
				"description": "alpha 在最近 5 分钟出现连续错误",
			},
			StartsAt:    "2026-04-27T06:00:00Z",
			Fingerprint: "fp-alpha",
		},
		{
			Status: "firing",
			Labels: map[string]string{
				"alertname":    "NewAPIChannelHasErrors",
				"site":         "site-a:3000",
				"severity":     "critical",
				"channel_id":   "1002",
				"channel_name": "beta",
			},
			Annotations: map[string]string{
				"summary":     "渠道 beta 有报错",
				"description": "beta 在最近 5 分钟出现连续错误",
			},
			StartsAt:    "2026-04-27T06:01:00Z",
			Fingerprint: "fp-beta",
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/alertmanager/feishu", bytes.NewReader(mustMarshalAlertmanagerWebhook(t, payload)))
	rec := httptest.NewRecorder()

	adapter.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text", received.MsgType)
	require.Contains(t, received.Content.Text, "[监控告警]")
	require.Contains(t, received.Content.Text, "NewAPIChannelHasErrors")
	require.Contains(t, received.Content.Text, "site-a:3000")
	require.Contains(t, received.Content.Text, "alpha")
	require.Contains(t, received.Content.Text, "beta")
	require.Contains(t, received.Content.Text, "共 2 条")
}

func TestAlertmanagerFeishuAdapterSuppressesDuplicateNotificationsWithinCooldown(t *testing.T) {
	t.Parallel()

	var downstreamCalls atomic.Int32
	feishuServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer feishuServer.Close()

	adapter, err := NewAlertmanagerFeishuAdapter(AlertmanagerFeishuAdapterConfig{
		WebhookURL:          feishuServer.URL,
		MinInterval:         time.Minute,
		MaxAlertsPerMessage: 10,
	})
	require.NoError(t, err)

	baseTime := time.Date(2026, 4, 27, 14, 0, 0, 0, time.FixedZone("CST", 8*3600))
	adapter.now = func() time.Time {
		return baseTime
	}

	body := mustMarshalAlertmanagerWebhook(t, sampleAlertmanagerWebhookPayload())

	req := httptest.NewRequest(http.MethodPost, "/alertmanager/feishu", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	adapter.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	req = httptest.NewRequest(http.MethodPost, "/alertmanager/feishu", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	adapter.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Equal(t, int32(1), downstreamCalls.Load())
}

func sampleAlertmanagerWebhookPayload() AlertmanagerWebhookPayload {
	return AlertmanagerWebhookPayload{
		Receiver: "feishu-adapter",
		Status:   "firing",
		GroupLabels: map[string]string{
			"alertname": "NewAPIChannelHasErrors",
			"site":      "site-a:3000",
			"severity":  "critical",
		},
		CommonLabels: map[string]string{
			"alertname": "NewAPIChannelHasErrors",
			"site":      "site-a:3000",
			"severity":  "critical",
		},
		CommonAnnotations: map[string]string{
			"summary": "渠道有报错",
		},
		ExternalURL: "http://alertmanager:9093",
		Version:     "4",
		GroupKey:    "{}:{alertname=\"NewAPIChannelHasErrors\",site=\"site-a:3000\"}",
		Alerts: []AlertmanagerWebhookAlert{
			{
				Status: "firing",
				Labels: map[string]string{
					"alertname":    "NewAPIChannelHasErrors",
					"site":         "site-a:3000",
					"severity":     "critical",
					"channel_id":   "1001",
					"channel_name": "alpha",
				},
				Annotations: map[string]string{
					"summary":     "渠道 alpha 有报错",
					"description": "alpha 在最近 5 分钟出现连续错误",
				},
				StartsAt:    "2026-04-27T06:00:00Z",
				Fingerprint: "fp-alpha",
			},
		},
	}
}

func mustMarshalAlertmanagerWebhook(t *testing.T, payload AlertmanagerWebhookPayload) []byte {
	t.Helper()

	data, err := common.Marshal(payload)
	require.NoError(t, err)
	return data
}
