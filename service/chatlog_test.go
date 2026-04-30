package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestNormalizeNonStreamResponseOpenAI(t *testing.T) {
	raw := []byte(`{
		"id":"chatcmpl_1",
		"choices":[
			{
				"message":{
					"content":"最终回复",
					"tool_calls":[
						{
							"id":"call_1",
							"type":"function",
							"function":{"name":"search","arguments":"{\"q\":\"hello\"}"}
						}
					]
				}
			}
		]
	}`)

	normalized, finalText, toolCallsMerged, streamMerged, providerReqID, isTools := normalizeNonStreamResponse(raw)
	require.Equal(t, "最终回复", finalText)
	require.Equal(t, "chatcmpl_1", providerReqID)
	require.True(t, streamMerged)
	require.True(t, isTools)
	require.NotEmpty(t, normalized)
	require.NotEmpty(t, toolCallsMerged)

	var normalizedObj map[string]any
	require.NoError(t, common.Unmarshal([]byte(normalized), &normalizedObj))
	require.Equal(t, "assistant", normalizedObj["role"])
	require.Equal(t, "最终回复", normalizedObj["content"])

	var toolCalls []map[string]any
	require.NoError(t, common.Unmarshal([]byte(toolCallsMerged), &toolCalls))
	require.Len(t, toolCalls, 1)
}

func TestNormalizeNonStreamResponseResponsesAPI(t *testing.T) {
	raw := []byte(`{
		"id":"resp_123",
		"output":[
			{
				"type":"message",
				"role":"assistant",
				"content":[{"type":"output_text","text":"4"}]
			}
		]
	}`)

	normalized, finalText, toolCallsMerged, streamMerged, providerReqID, isTools := normalizeNonStreamResponse(raw)
	require.Equal(t, "4", finalText)
	require.Equal(t, "resp_123", providerReqID)
	require.True(t, streamMerged)
	require.False(t, isTools)
	require.NotEmpty(t, normalized)
	require.Empty(t, toolCallsMerged)
}

func TestNormalizeNonStreamResponseGeminiWithFunctionCall(t *testing.T) {
	raw := []byte(`{
		"id":"gem_1",
		"candidates":[
			{
				"content":{
					"parts":[
						{"text":"我来查一下"},
						{"functionCall":{"name":"search","args":{"q":"上海天气"}}}
					]
				}
			}
		]
	}`)

	normalized, finalText, toolCallsMerged, streamMerged, providerReqID, isTools := normalizeNonStreamResponse(raw)
	require.Equal(t, "我来查一下", finalText)
	require.Equal(t, "gem_1", providerReqID)
	require.True(t, streamMerged)
	require.True(t, isTools)
	require.NotEmpty(t, normalized)
	require.NotEmpty(t, toolCallsMerged)
}

func TestNormalizeStreamResponseOpenAIMergeTextAndTools(t *testing.T) {
	raw := []byte("" +
		"data: {\"id\":\"resp_1\",\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"search\",\"arguments\":\"{\\\"q\\\":\\\"\"}}]}}]}\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"test\\\"}\"}}]}}]}\n" +
		"data: [DONE]\n")

	normalized, finalText, toolCallsMerged, streamMerged, providerReqID, isTools := normalizeStreamResponse(raw)
	require.Equal(t, "你好", finalText)
	require.True(t, streamMerged)
	require.Equal(t, "resp_1", providerReqID)
	require.True(t, isTools)
	require.NotEmpty(t, normalized)
	require.NotEmpty(t, toolCallsMerged)

	var toolCalls []map[string]any
	require.NoError(t, common.Unmarshal([]byte(toolCallsMerged), &toolCalls))
	require.Len(t, toolCalls, 1)
	functionObj, ok := toolCalls[0]["function"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "search", functionObj["name"])
	require.Equal(t, "{\"q\":\"test\"}", functionObj["arguments"])
}

func TestNormalizeStreamResponseResponsesAPIMergeText(t *testing.T) {
	raw := []byte("" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_2\"}}\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"4\"}\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"2\"}\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_2\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"42\"}]}]}}\n")

	normalized, finalText, toolCallsMerged, streamMerged, providerReqID, isTools := normalizeStreamResponse(raw)
	require.Equal(t, "42", finalText)
	require.Equal(t, "resp_2", providerReqID)
	require.True(t, streamMerged)
	require.False(t, isTools)
	require.NotEmpty(t, normalized)
	require.Empty(t, toolCallsMerged)
}

func TestNormalizeStreamResponseClaudeRaw(t *testing.T) {
	raw := []byte("" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"你好\"}}\n" +
		"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"tool_1\",\"name\":\"search\",\"input\":{\"q\":\"杭州\"}}}\n" +
		"data: {\"type\":\"message_stop\"}\n")

	_, finalText, toolCallsMerged, streamMerged, _, isTools := normalizeStreamResponse(raw)
	require.Equal(t, "你好", finalText)
	require.True(t, streamMerged)
	require.True(t, isTools)
	require.NotEmpty(t, toolCallsMerged)
}

func TestNormalizeStreamResponseGeminiRawWithFunctionCall(t *testing.T) {
	raw := []byte("" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"先调用工具\"},{\"functionCall\":{\"name\":\"web_search\",\"args\":{\"query\":\"2+2\"}}}]}}]}\n" +
		"data: [DONE]\n")

	normalized, finalText, toolCallsMerged, streamMerged, _, isTools := normalizeStreamResponse(raw)
	require.Equal(t, "先调用工具", finalText)
	require.True(t, streamMerged)
	require.True(t, isTools)
	require.NotEmpty(t, normalized)
	require.NotEmpty(t, toolCallsMerged)

	var toolCalls []map[string]any
	require.NoError(t, common.Unmarshal([]byte(toolCallsMerged), &toolCalls))
	require.Len(t, toolCalls, 1)
	functionObj, ok := toolCalls[0]["function"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "web_search", functionObj["name"])
	require.Equal(t, "{\"query\":\"2+2\"}", functionObj["arguments"])
}

func TestNormalizeStreamResponseResponsesAPIFunctionCall(t *testing.T) {
	raw := []byte("" +
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"item_1\",\"call_id\":\"call_9\",\"name\":\"search\",\"arguments\":\"{\\\"q\\\":\\\"grok\\\"}\"}}\n" +
		"data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"item_id\":\"call_9\",\"delta\":\"\"}\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_fn\"}}\n")

	_, _, toolCallsMerged, streamMerged, providerReqID, isTools := normalizeStreamResponse(raw)
	require.Equal(t, "resp_fn", providerReqID)
	require.True(t, streamMerged)
	require.True(t, isTools)
	require.NotEmpty(t, toolCallsMerged)
}

func TestExtractConversationMetaRoundAndTools(t *testing.T) {
	reqObj := map[string]any{
		"conversation_id": "conv_1",
		"messages": []any{
			map[string]any{"role": "system", "content": "s"},
			map[string]any{"role": "user", "content": "u1"},
			map[string]any{"role": "assistant", "content": "a1"},
			map[string]any{"role": "user", "content": "u2"},
		},
		"tools": []any{
			map[string]any{"type": "function"},
		},
	}

	conversationID, messageID, messageRound, isTools := extractConversationMeta(reqObj, "req_1")
	require.Equal(t, "conv_1", conversationID)
	require.Equal(t, "req_1", messageID)
	require.Equal(t, 2, messageRound)
	require.True(t, isTools)
}

func TestExtractConversationMetaFromResponsesInput(t *testing.T) {
	reqObj := map[string]any{
		"input": []any{
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "1+1"}}},
			map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "2"}}},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "2+2"}}},
		},
	}

	_, messageID, messageRound, isTools := extractConversationMeta(reqObj, "req_responses_1")
	require.Equal(t, "req_responses_1", messageID)
	require.Equal(t, 2, messageRound)
	require.False(t, isTools)
}

func TestCalcPayloadDigestDeterministic(t *testing.T) {
	rawReq := []byte(`{"a":1}`)
	rawResp := []byte(`{"b":2}`)
	bytes1, hash1 := calcPayloadDigest(rawReq, rawResp)
	bytes2, hash2 := calcPayloadDigest(rawReq, rawResp)
	require.Equal(t, int64(len(rawReq)+len(rawResp)), bytes1)
	require.Equal(t, bytes1, bytes2)
	require.Equal(t, hash1, hash2)
	require.Len(t, hash1, 64)
}

func TestBuildChatLogEventFallbackFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`))

	common.SetContextKey(ctx, constant.ContextKeyChatLogRawRequest, []byte(`{"messages":[{"role":"user","content":"hi"}]}`))
	common.SetContextKey(ctx, constant.ContextKeyChatLogRawResponse, []byte(`{"choices":[{"message":{"content":"ok"}}]}`))

	event := buildChatLogEvent(ctx, nil)
	require.Equal(t, "0", event.UserID)
	require.Equal(t, "0", event.ChannelID)
	require.Equal(t, "unknown", event.ModelName)
	require.Equal(t, "openai", event.Provider)
	require.NotEmpty(t, event.ConversationID)
	require.NotEmpty(t, event.MessageID)
	require.Equal(t, 1, event.MessageRound)
	require.Equal(t, common.ChatLogTimeZone, event.TimeZone)
}

func TestBuildChatLogEventFallbackRawRequestFromRelayInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewBufferString(""))

	common.SetContextKey(ctx, constant.ContextKeyChatLogRawResponse, []byte(`{"id":"resp_1","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))

	relayInfo := &relaycommon.RelayInfo{
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.3-codex",
			Input: json.RawMessage(`[{"role":"user","content":[{"type":"input_text","text":"1+1"}]}]`),
		},
	}

	event := buildChatLogEvent(ctx, relayInfo)
	require.NotEmpty(t, event.RawRequest)
	require.Contains(t, event.RawRequest, `"model":"gpt-5.3-codex"`)
	require.NotEmpty(t, event.FinalMergedJSON)
	var merged map[string]any
	require.NoError(t, common.Unmarshal([]byte(event.FinalMergedJSON), &merged))
	require.Contains(t, merged, "request")
	require.Contains(t, merged, "response")
}

func TestBuildFinalMergedJSON(t *testing.T) {
	rawReq := []byte(`{"model":"gpt-5.3-codex","stream":true,"input":[{"role":"user","content":[{"type":"input_text","text":"2+2=?"}]}]}`)
	normalizedResp := `{"role":"assistant","content":"4","tool_calls":[]}`

	out := buildFinalMergedJSON(rawReq, normalizedResp, "4", "")
	require.NotEmpty(t, out)

	var merged map[string]any
	require.NoError(t, common.Unmarshal([]byte(out), &merged))
	reqObj, ok := merged["request"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "gpt-5.3-codex", reqObj["model"])

	respObj, ok := merged["response"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "assistant", respObj["role"])
	require.Equal(t, "4", respObj["content"])
}

func TestShouldPrecreateNextChatLogDayOnlyNearShanghaiMidnight(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)

	require.False(t, shouldPrecreateNextChatLogDay(time.Date(2026, 4, 15, 8, 0, 0, 0, loc)))
	require.False(t, shouldPrecreateNextChatLogDay(time.Date(2026, 4, 15, 22, 59, 59, 0, loc)))
	require.True(t, shouldPrecreateNextChatLogDay(time.Date(2026, 4, 15, 23, 0, 0, 0, loc)))
}

func TestHandleChatLogUpsertFailureDoesNotRepublishMainStream(t *testing.T) {
	event := &dto.ChatLogEvent{
		RequestID: "req_retry_no_republish",
		Retry:     0,
	}

	ack, dlq, reason := decideChatLogFailureAction(
		event,
		1,
		errors.New("db down"),
	)

	require.False(t, ack)
	require.False(t, dlq)
	require.Empty(t, reason)
}

func TestHandleChatLogUpsertFailureDLQAfterMaxRetry(t *testing.T) {
	oldMaxRetry := common.ChatLogMaxRetry
	common.ChatLogMaxRetry = 1
	defer func() { common.ChatLogMaxRetry = oldMaxRetry }()

	event := &dto.ChatLogEvent{
		RequestID: "req_retry_dlq",
		Retry:     0,
	}

	ack, dlq, reason := decideChatLogFailureAction(
		event,
		2,
		errors.New("db down"),
	)

	require.True(t, ack)
	require.True(t, dlq)
	require.Contains(t, reason, "upsert_failed")
}

func TestChatLogRetryDelayCaps(t *testing.T) {
	require.Equal(t, time.Second, chatLogRetryDelay(1))
	require.Equal(t, 30*time.Second, chatLogRetryDelay(100))
}

func TestHandleChatLogRedisMessageNilRedisNoPanic(t *testing.T) {
	ctx := context.Background()
	handleChatLogRedisMessage(ctx, "stream", "group", redis.XMessage{ID: "1-0"})
}
