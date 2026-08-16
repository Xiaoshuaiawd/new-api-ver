package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRejectDisabledNonStreamRequest(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		relayMode  int
		isStream   bool
		wantReject bool
	}{
		{name: "chat non-stream", enabled: true, relayMode: relayconstant.RelayModeChatCompletions, wantReject: true},
		{name: "responses non-stream", enabled: true, relayMode: relayconstant.RelayModeResponses, wantReject: true},
		{name: "chat stream", enabled: true, relayMode: relayconstant.RelayModeChatCompletions, isStream: true},
		{name: "responses stream", enabled: true, relayMode: relayconstant.RelayModeResponses, isStream: true},
		{name: "default off", relayMode: relayconstant.RelayModeChatCompletions},
		{name: "completions excluded", enabled: true, relayMode: relayconstant.RelayModeCompletions},
		{name: "responses compact excluded", enabled: true, relayMode: relayconstant.RelayModeResponsesCompact},
		{name: "embedding excluded", enabled: true, relayMode: relayconstant.RelayModeEmbeddings},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{
				DisableNonStream: tt.enabled,
			})
			info := &relaycommon.RelayInfo{RelayMode: tt.relayMode, IsStream: tt.isStream}

			err := rejectDisabledNonStreamRequest(c, info)
			if !tt.wantReject {
				assert.Nil(t, err)
				return
			}

			require.NotNil(t, err)
			assert.Equal(t, http.StatusBadRequest, err.StatusCode)
			assert.Equal(t, types.ErrorCodeInvalidRequest, err.GetErrorCode())
			assert.Equal(t, "当前号池正在遭受恶意毁号请求，暂停同步请求", err.Error())
			assert.True(t, types.IsSkipRetryError(err))
		})
	}
}
