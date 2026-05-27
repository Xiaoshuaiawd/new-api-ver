package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesHandlerRestoresMappedModelName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5-mini",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.4-mini",
			IsModelMapped:     true,
		},
	}
	body := `{"id":"resp_1","object":"response","model":"gpt-5.4-mini-2026-03-17","output":[],"tools":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, apiErr := OaiResponsesHandler(ctx, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	var payload dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, "gpt-5-mini", payload.Model)
	require.NotContains(t, recorder.Body.String(), "gpt-5.4-mini")
}

func TestOaiResponsesStreamHandlerRestoresMappedModelName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5-mini",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.4-mini",
			IsModelMapped:     true,
		},
	}
	stream := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","model":"gpt-5.4-mini-2026-03-17","output":[],"tools":[],"usage":null}}`,
		`data: {"type":"response.output_text.delta","delta":"1"}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","model":"gpt-5.4-mini-2026-03-17","output":[],"tools":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(stream)),
	}

	usage, apiErr := OaiResponsesStreamHandler(ctx, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 2, usage.TotalTokens)
	require.Contains(t, recorder.Body.String(), `"model":"gpt-5-mini"`)
	require.NotContains(t, recorder.Body.String(), "gpt-5.4-mini")
}
