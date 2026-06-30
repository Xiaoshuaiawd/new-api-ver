package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendLogBodyDetailsCapturesRequestAndResponseBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt","prompt":"hi"}`))
	SetLogResponseBody(ctx, []byte(`{"error":{"message":"upstream failed"}}`))

	other := map[string]interface{}{}
	AppendLogBodyDetails(ctx, other)

	details, ok := other["log_detail"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, `{"model":"gpt","prompt":"hi"}`, details["request_body"])
	assert.Equal(t, `{"error":{"message":"upstream failed"}}`, details["response_body"])
	assert.NotContains(t, details, "request_body_truncated")
	assert.NotContains(t, details, "response_body_truncated")

	storage, err := common.GetBodyStorage(ctx)
	require.NoError(t, err)
	_, err = storage.Seek(0, 0)
	require.NoError(t, err)
	replayed, err := storage.Bytes()
	require.NoError(t, err)
	assert.Equal(t, `{"model":"gpt","prompt":"hi"}`, string(replayed))
}

func TestAppendLogBodyDetailsTruncatesLargeBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	requestBody := strings.Repeat("a", maxLogBodyDetailBytes+128)
	responseBody := strings.Repeat("b", maxLogBodyDetailBytes+64)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(requestBody))
	ctx.Set(ginKeyLogResponseBody, responseBody)

	other := map[string]interface{}{}
	AppendLogBodyDetails(ctx, other)

	details, ok := other["log_detail"].(map[string]interface{})
	require.True(t, ok)
	assert.Len(t, details["request_body"], maxLogBodyDetailBytes)
	assert.Len(t, details["response_body"], maxLogBodyDetailBytes)
	assert.Equal(t, true, details["request_body_truncated"])
	assert.Equal(t, len(requestBody), details["request_body_size"])
	assert.Equal(t, true, details["response_body_truncated"])
	assert.Equal(t, len(responseBody), details["response_body_size"])
}

func TestAppendLogBodyDetailsCapturesEmptyBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(""))
	SetLogResponseBody(ctx, []byte{})

	other := map[string]interface{}{}
	AppendLogBodyDetails(ctx, other)

	details, ok := other["log_detail"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, details, "request_body")
	assert.Contains(t, details, "response_body")
	assert.Equal(t, "", details["request_body"])
	assert.Equal(t, "", details["response_body"])
	assert.NotContains(t, details, "request_body_truncated")
	assert.NotContains(t, details, "response_body_truncated")
}

func TestIOCopyBytesGracefullyCapturesEmptyResponseBodyForLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt"}`))
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
	}

	IOCopyBytesGracefully(ctx, resp, []byte{})

	other := map[string]interface{}{}
	AppendLogBodyDetails(ctx, other)

	details, ok := other["log_detail"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, details, "response_body")
	assert.Equal(t, "", details["response_body"])
}

func TestIOCopyBytesGracefullyOverwritesEarlierErrorResponseBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt"}`))
	SetLogResponseBody(ctx, []byte(`{"error":"first attempt failed"}`))
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
	}

	IOCopyBytesGracefully(ctx, resp, []byte(`{"id":"ok"}`))

	other := map[string]interface{}{}
	AppendLogBodyDetails(ctx, other)

	details, ok := other["log_detail"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, `{"id":"ok"}`, details["response_body"])
}
