package xunfei

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	prometheusmetrics "github.com/QuantumNous/new-api/pkg/prometheus_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXunfeiMakeRequestRecordsWebSocketFirstResponseByte(t *testing.T) {
	runtime, err := prometheusmetrics.NewRuntime(prometheusmetrics.Config{
		Enabled:     true,
		AllowPublic: true,
	}, "v-test", nil, nil)
	require.NoError(t, err)
	prometheusmetrics.SetDefaultRuntime(runtime)
	t.Cleanup(func() { prometheusmetrics.SetDefaultRuntime(nil) })

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, upgradeErr := upgrader.Upgrade(w, r, nil)
		if upgradeErr != nil {
			t.Errorf("upgrade websocket: %v", upgradeErr)
			return
		}
		defer conn.Close()
		if _, _, readErr := conn.ReadMessage(); readErr != nil {
			t.Errorf("read xunfei request: %v", readErr)
			return
		}

		response := XunfeiChatResponse{}
		response.Payload.Choices.Status = 2
		payload, marshalErr := common.Marshal(response)
		if marshalErr != nil {
			t.Errorf("marshal xunfei response: %v", marshalErr)
			return
		}
		if writeErr := conn.WriteMessage(websocket.TextMessage, payload); writeErr != nil {
			t.Errorf("write xunfei response: %v", writeErr)
		}
	}))
	t.Cleanup(upstream.Close)

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 41, ChannelType: 42}}
	dataChan, stopChan, err := xunfeiMakeRequest(
		context.Background(),
		info,
		dto.GeneralOpenAIRequest{},
		"general",
		"ws"+strings.TrimPrefix(upstream.URL, "http"),
		"app-id",
	)
	require.NoError(t, err)
	<-dataChan
	<-stopChan

	recorder := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "newapi_channel_first_byte_seconds_count{channel_id=\"41\",channel_type=\"42\"} 1")
}
