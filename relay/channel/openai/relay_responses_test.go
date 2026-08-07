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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesHandlerRestoresMappedModelName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.4",
			IsModelMapped:     true,
		},
	}
	body := `{"id":"resp_1","object":"response","model":"gpt-5.4-2026-03-17","output":[],"tools":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2},"provider_sequence":9007199254740993}`
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
	assert.Equal(t, "gpt-5.5", payload.Model)
	assert.Contains(t, recorder.Body.String(), `"provider_sequence":9007199254740993`)
	assert.NotContains(t, recorder.Body.String(), "gpt-5.4")
}

func TestOaiResponsesHandlerDetectsOverloadErrorWithHTTP200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	body := `{"id":"resp_1","status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}

	_, apiErr := OaiResponsesHandler(ctx, info, resp)
	require.NotNil(t, apiErr)
	assert.Contains(t, apiErr.Error(), "overloaded")
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
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.4",
			IsModelMapped:     true,
		},
	}
	stream := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","model":"gpt-5.4-2026-03-17","output":[],"tools":[],"usage":null}}`,
		`data: {"type":"response.output_text.delta","delta":"1"}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","model":"gpt-5.4-2026-03-17","output":[],"tools":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
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
	assert.Equal(t, 2, usage.TotalTokens)
	assert.Equal(t, 2, strings.Count(recorder.Body.String(), `"model":"gpt-5.5"`))
	assert.NotContains(t, recorder.Body.String(), "gpt-5.4")
}

func TestRestoreResponsesModelLeavesUnmappedPayloadUnchanged(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","provider_sequence":9007199254740993}`)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.4",
		},
	}

	assert.Equal(t, body, restoreResponsesModelInBody(body, info))
}
