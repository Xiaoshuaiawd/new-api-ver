package volcengine

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	prometheusmetrics "github.com/QuantumNous/new-api/pkg/prometheus_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleTTSWebSocketResponseRecordsFirstResponseByte(t *testing.T) {
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
			t.Errorf("read volcengine request: %v", readErr)
			return
		}

		message, messageErr := NewMessage(MsgTypeAudioOnlyServer, MsgTypeFlagNegativeSeq)
		if messageErr != nil {
			t.Errorf("create volcengine response: %v", messageErr)
			return
		}
		message.Sequence = -1
		message.Payload = []byte("audio")
		payload, marshalErr := message.Marshal()
		if marshalErr != nil {
			t.Errorf("marshal volcengine response: %v", marshalErr)
			return
		}
		if writeErr := conn.WriteMessage(websocket.BinaryMessage, payload); writeErr != nil {
			t.Errorf("write volcengine response: %v", writeErr)
		}
	}))
	t.Cleanup(upstream.Close)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   51,
			ChannelType: 52,
			ApiKey:      "app-id|access-token",
		},
	}

	usage, newAPIError := handleTTSWebSocketResponse(
		c,
		"ws"+strings.TrimPrefix(upstream.URL, "http"),
		VolcengineTTSRequest{},
		info,
		"mp3",
	)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)

	metrics := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, metrics.Code)
	assert.Contains(t, metrics.Body.String(), "newapi_channel_first_byte_seconds_count{channel_id=\"51\",channel_type=\"52\"} 1")
}
