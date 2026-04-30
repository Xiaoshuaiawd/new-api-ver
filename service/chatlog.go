package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

var (
	chatLogStartOnce sync.Once
	chatLogLoc       *time.Location
	chatLogLocOnce   sync.Once
	chatLogLocalQ    chan *dto.ChatLogEvent
)

func getChatLogLocation() *time.Location {
	chatLogLocOnce.Do(func() {
		loc, err := time.LoadLocation(common.ChatLogTimeZone)
		if err != nil {
			common.SysError(fmt.Sprintf("invalid CHAT_LOG_TIMEZONE=%s, fallback to Asia/Shanghai: %v", common.ChatLogTimeZone, err))
			loc = time.FixedZone("CST", 8*3600)
		}
		chatLogLoc = loc
	})
	return chatLogLoc
}

func chatLogNow() time.Time {
	return time.Now().In(getChatLogLocation())
}

func StartChatLogPipeline() {
	chatLogStartOnce.Do(func() {
		if !common.ChatLogEnabled {
			return
		}
		chatLogLocalQ = make(chan *dto.ChatLogEvent, 2048)
		localWorkers := common.ChatLogLocalWorkers
		if localWorkers <= 0 {
			localWorkers = 1
		}
		for i := 0; i < localWorkers; i++ {
			go chatLogLocalConsumer()
		}
		go chatLogPartitionPrecreateLoop()

		consumerWorkers := 0
		if common.RedisEnabled {
			consumerWorkers = common.ChatLogConsumerWorkers
			if consumerWorkers <= 0 {
				consumerWorkers = 1
			}
			for i := 0; i < consumerWorkers; i++ {
				go chatLogRedisConsumerLoop(i)
			}
		}
		common.SysLog(fmt.Sprintf("chat log pipeline started: local_workers=%d redis_workers=%d stream_max_len=%d dlq_max_len=%d",
			localWorkers, consumerWorkers, common.ChatLogStreamMaxLen, common.ChatLogDLQMaxLen))
	})
}

func chatLogPartitionPrecreateLoop() {
	now := chatLogNow()
	if err := model.EnsureChatLogTablesByTime(now); err != nil {
		common.SysError("ensure chat log partition failed: " + err.Error())
	}
	if shouldPrecreateNextChatLogDay(now) {
		if err := model.EnsureChatLogTablesByTime(now.Add(24 * time.Hour)); err != nil {
			common.SysError("ensure next-day chat log partition failed: " + err.Error())
		}
	}

	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		n := chatLogNow()
		if err := model.EnsureChatLogTablesByTime(n); err != nil {
			common.SysError("ensure chat log partition failed: " + err.Error())
		}
		if shouldPrecreateNextChatLogDay(n) {
			if err := model.EnsureChatLogTablesByTime(n.Add(24 * time.Hour)); err != nil {
				common.SysError("ensure next-day chat log partition failed: " + err.Error())
			}
		}
	}
}

func shouldPrecreateNextChatLogDay(now time.Time) bool {
	return now.In(getChatLogLocation()).Hour() >= 23
}

func EmitChatLogSuccess(c *gin.Context, relayInfo *relaycommon.RelayInfo, promptTokens, completionTokens, totalTokens int) {
	if !common.ChatLogEnabled || c == nil {
		return
	}
	event := buildChatLogEvent(c, relayInfo)
	event.Success = true
	event.StatusCode = c.Writer.Status()
	if event.StatusCode == 0 {
		event.StatusCode = 200
	}
	event.PromptTokens = promptTokens
	event.CompletionTokens = completionTokens
	event.TotalTokens = totalTokens
	enqueueChatLogEvent(event)
}

func EmitChatLogFailure(c *gin.Context, relayInfo *relaycommon.RelayInfo, apiErr *types.NewAPIError) {
	if !common.ChatLogEnabled || c == nil || apiErr == nil {
		return
	}
	event := buildChatLogEvent(c, relayInfo)
	event.Success = false
	event.StatusCode = apiErr.StatusCode
	event.ErrorCode = string(apiErr.GetErrorCode())
	event.ErrorMessage = apiErr.MaskSensitiveErrorWithStatusCode()
	enqueueChatLogEvent(event)
}

func buildChatLogEvent(c *gin.Context, relayInfo *relaycommon.RelayInfo) *dto.ChatLogEvent {
	now := chatLogNow()
	requestID := c.GetString(common.RequestIdKey)
	if requestID == "" {
		requestID = fmt.Sprintf("chatlog_%d", now.UnixNano())
	}

	requestBody := getRawRequest(c)
	if len(requestBody) == 0 {
		requestBody = fallbackRawRequest(relayInfo)
	}
	responseBody := getRawResponse(c)

	reqObj := parseObject(requestBody)
	respObj := parseObject(responseBody)

	provider, modelName := detectProviderAndModel(c, relayInfo)
	conversationID, messageID, messageRound, reqTools := extractConversationMeta(reqObj, requestID)

	normalizedResp, finalText, toolCallsMerged, streamMerged, providerReqID, respTools := normalizeResponse(responseBody, isStream(c, relayInfo))
	if messageID == "" {
		messageID = providerReqID
	}
	if messageID == "" {
		messageID = requestID
	}
	if conversationID == "" {
		conversationID = requestID
	}
	if messageRound <= 0 {
		messageRound = 1
	}
	if strings.TrimSpace(modelName) == "" {
		modelName = "unknown"
	}
	if strings.TrimSpace(provider) == "" {
		provider = "unknown"
	}

	mergedTimeline := buildMergedTimeline(reqObj, finalText, toolCallsMerged)
	finalMergedJSON := buildFinalMergedJSON(requestBody, normalizedResp, finalText, toolCallsMerged)

	latencyMS := 0
	startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if !startTime.IsZero() {
		latencyMS = int(time.Since(startTime).Milliseconds())
		if latencyMS < 0 {
			latencyMS = 0
		}
	}

	payloadBytes, payloadSHA := calcPayloadDigest(requestBody, responseBody)
	userID := ""
	if uid := c.GetInt("id"); uid > 0 {
		userID = fmt.Sprintf("%d", uid)
	}
	if userID == "" {
		userID = "0"
	}
	channelID := ""
	if cid := c.GetInt("channel_id"); cid > 0 {
		channelID = fmt.Sprintf("%d", cid)
	}
	if channelID == "" {
		channelID = "0"
	}

	if len(requestBody) == 0 || len(responseBody) == 0 {
		path := ""
		if c.Request != nil && c.Request.URL != nil {
			path = c.Request.URL.Path
		}
		common.SysError(fmt.Sprintf("chat log payload missing: request_empty=%t response_empty=%t request_id=%s path=%s",
			len(requestBody) == 0, len(responseBody) == 0, requestID, path))
	}

	event := &dto.ChatLogEvent{
		EventID:        requestID,
		UserID:         userID,
		CreatedAt:      now,
		CreatedDate:    now.Format("2006-01-02"),
		TimeZone:       common.ChatLogTimeZone,
		ConversationID: conversationID,
		ModelName:      modelName,
		MessageID:      messageID,
		ChannelID:      channelID,
		IsStream:       isStream(c, relayInfo),
		MessageRound:   messageRound,
		IsTools:        reqTools || respTools,
		Provider:       provider,
		RequestID:      requestID,
		ProviderRequestID: firstNonEmpty(
			providerReqID,
			strVal(respObj["id"]),
		),
		LatencyMS:          latencyMS,
		RawRequest:         bytesToString(requestBody),
		RawResponse:        bytesToString(responseBody),
		StreamChunks:       streamChunksForEvent(isStream(c, relayInfo), responseBody),
		NormalizedResponse: normalizedResp,
		FinalAnswerText:    finalText,
		FinalMergedJSON:    finalMergedJSON,
		ToolCallsMerged:    toolCallsMerged,
		MergedTimeline:     mergedTimeline,
		StreamMerged:       streamMerged,
		PayloadBytes:       payloadBytes,
		PayloadSHA256:      payloadSHA,
	}
	return event
}

func fallbackRawRequest(relayInfo *relaycommon.RelayInfo) []byte {
	if relayInfo == nil || relayInfo.Request == nil {
		return nil
	}
	b, err := common.Marshal(relayInfo.Request)
	if err != nil || len(b) == 0 {
		return nil
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}

func buildFinalMergedJSON(rawRequest []byte, normalizedResponse, finalAnswerText, toolCallsMerged string) string {
	var requestPayload any
	if len(rawRequest) > 0 {
		if err := common.Unmarshal(rawRequest, &requestPayload); err != nil {
			requestPayload = bytesToString(rawRequest)
		}
	}
	if requestPayload == nil {
		requestPayload = map[string]any{}
	}

	responsePayload := map[string]any{
		"role":    "assistant",
		"content": finalAnswerText,
	}
	if normalizedResponse != "" {
		var normalizedObj map[string]any
		if err := common.Unmarshal([]byte(normalizedResponse), &normalizedObj); err == nil && len(normalizedObj) > 0 {
			responsePayload = normalizedObj
		}
	}
	if toolCallsMerged != "" {
		if _, exists := responsePayload["tool_calls"]; !exists {
			var tc any
			if err := common.Unmarshal([]byte(toolCallsMerged), &tc); err == nil {
				responsePayload["tool_calls"] = tc
			}
		}
	}

	merged := map[string]any{
		"request":  requestPayload,
		"response": responsePayload,
	}
	b, err := common.Marshal(merged)
	if err != nil {
		return ""
	}
	return string(b)
}

func getRawRequest(c *gin.Context) []byte {
	if v, ok := common.GetContextKey(c, constant.ContextKeyChatLogRawRequest); ok {
		if body, ok := v.([]byte); ok {
			cp := make([]byte, len(body))
			copy(cp, body)
			return cp
		}
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage == nil {
		return nil
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil
	}
	cp := make([]byte, len(body))
	copy(cp, body)
	return cp
}

func getRawResponse(c *gin.Context) []byte {
	if v, ok := common.GetContextKey(c, constant.ContextKeyChatLogRawResponse); ok {
		if body, ok := v.([]byte); ok {
			cp := make([]byte, len(body))
			copy(cp, body)
			return cp
		}
	}
	if v, ok := common.GetContextKey(c, constant.ContextKeyChatLogWriter); ok {
		if buf, ok := v.(*bytes.Buffer); ok {
			body := buf.Bytes()
			cp := make([]byte, len(body))
			copy(cp, body)
			return cp
		}
	}
	return nil
}

func detectProviderAndModel(c *gin.Context, relayInfo *relaycommon.RelayInfo) (string, string) {
	modelName := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)
	provider := "openai"

	if relayInfo != nil {
		if relayInfo.OriginModelName != "" {
			modelName = relayInfo.OriginModelName
		}
		if relayInfo.ChannelMeta != nil && relayInfo.ChannelMeta.ChannelType == constant.ChannelTypeXai {
			provider = "grok"
		} else {
			switch relayInfo.GetFinalRequestRelayFormat() {
			case types.RelayFormatClaude:
				provider = "claude"
			case types.RelayFormatGemini:
				provider = "gemini"
			default:
				provider = "openai"
			}
		}
	}

	path := c.Request.URL.Path
	if strings.HasPrefix(path, "/v1/messages") {
		provider = "claude"
	} else if strings.HasPrefix(path, "/v1beta/models") || strings.HasPrefix(path, "/v1/models/") {
		if c.GetHeader("x-goog-api-key") != "" || c.Query("key") != "" {
			provider = "gemini"
		}
	}
	if strings.Contains(strings.ToLower(modelName), "grok") {
		provider = "grok"
	}
	return provider, modelName
}

func isStream(c *gin.Context, relayInfo *relaycommon.RelayInfo) bool {
	if relayInfo != nil {
		return relayInfo.IsStream
	}
	return common.GetContextKeyBool(c, constant.ContextKeyIsStream)
}

func extractConversationMeta(reqObj map[string]any, requestID string) (conversationID, messageID string, messageRound int, isTools bool) {
	conversationID = firstNonEmpty(
		strVal(reqObj["conversation_id"]),
		strVal(reqObj["conversationId"]),
	)
	messageID = firstNonEmpty(
		strVal(reqObj["message_id"]),
		strVal(reqObj["messageId"]),
		strVal(reqObj["id"]),
	)
	messageRound = intVal(reqObj["message_round"])
	if messageRound <= 0 {
		if messages, ok := reqObj["messages"].([]any); ok {
			for _, msg := range messages {
				if m, ok := msg.(map[string]any); ok && strVal(m["role"]) == "user" {
					messageRound++
				}
			}
		} else if inputs, ok := reqObj["input"].([]any); ok {
			for _, msg := range inputs {
				if m, ok := msg.(map[string]any); ok && strVal(m["role"]) == "user" {
					messageRound++
				}
			}
		}
	}
	if tools, ok := reqObj["tools"].([]any); ok && len(tools) > 0 {
		isTools = true
	}
	if messageID == "" {
		messageID = requestID
	}
	return
}

func parseObject(data []byte) map[string]any {
	if len(data) == 0 {
		return map[string]any{}
	}
	var obj map[string]any
	if err := common.Unmarshal(data, &obj); err != nil {
		return map[string]any{}
	}
	return obj
}

func normalizeResponse(raw []byte, stream bool) (normalizedResponse, finalText, toolCallsMerged string, streamMerged bool, providerReqID string, isTools bool) {
	if len(raw) == 0 {
		return "", "", "", false, "", false
	}
	if stream {
		return normalizeStreamResponse(raw)
	}
	return normalizeNonStreamResponse(raw)
}

func normalizeNonStreamResponse(raw []byte) (normalizedResponse, finalText, toolCallsMerged string, streamMerged bool, providerReqID string, isTools bool) {
	obj := parseObject(raw)
	providerReqID = strVal(obj["id"])

	var toolCalls []any

	if choices, ok := obj["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if message, ok := choice["message"].(map[string]any); ok {
				finalText = extractContent(message["content"])
				if tc, ok := message["tool_calls"].([]any); ok && len(tc) > 0 {
					toolCalls = tc
				}
				if len(toolCalls) == 0 {
					if fc, ok := message["function_call"].(map[string]any); ok {
						toolCalls = append(toolCalls, map[string]any{
							"id":   strVal(fc["id"]),
							"type": "function",
							"function": map[string]any{
								"name":      strVal(fc["name"]),
								"arguments": strVal(fc["arguments"]),
							},
						})
					}
				}
			}
			if finalText == "" {
				finalText = extractContent(choice["text"])
			}
		}
	}

	if finalText == "" {
		if content, ok := obj["content"].([]any); ok {
			finalText = extractClaudeContent(content)
			for _, block := range content {
				if blk, ok := block.(map[string]any); ok && strVal(blk["type"]) == "tool_use" {
					isTools = true
				}
			}
		}
	}

	if candidates, ok := obj["candidates"].([]any); ok && len(candidates) > 0 {
		candidateText, candidateToolCalls, candidateHasTools := extractGeminiCandidateOutput(candidates)
		if finalText == "" {
			finalText = candidateText
		}
		if len(toolCalls) == 0 && len(candidateToolCalls) > 0 {
			toolCalls = candidateToolCalls
		}
		if candidateHasTools {
			isTools = true
		}
	}

	if output, ok := obj["output"].([]any); ok {
		outputText, outputToolCalls, outputHasTools := extractResponsesOutput(output)
		if finalText == "" {
			finalText = outputText
		}
		if len(toolCalls) == 0 && len(outputToolCalls) > 0 {
			toolCalls = outputToolCalls
		}
		if outputHasTools {
			isTools = true
		}
	}

	if len(toolCalls) > 0 {
		isTools = true
		if b, err := common.Marshal(toolCalls); err == nil {
			toolCallsMerged = string(b)
		}
	}

	normalized := map[string]any{
		"role":    "assistant",
		"content": finalText,
	}
	if len(toolCalls) > 0 {
		normalized["tool_calls"] = toolCalls
	}
	if b, err := common.Marshal(normalized); err == nil {
		normalizedResponse = string(b)
	}
	return normalizedResponse, finalText, toolCallsMerged, true, providerReqID, isTools
}

func normalizeStreamResponse(raw []byte) (normalizedResponse, finalText, toolCallsMerged string, streamMerged bool, providerReqID string, isTools bool) {
	var contentBuilder strings.Builder
	type toolState struct {
		ID   string
		Name string
		Args string
	}
	toolMap := map[int]*toolState{}
	toolIndexes := make([]int, 0)
	toolIndexSet := map[int]struct{}{}
	dynamicToolIndexByKey := map[string]int{}
	nextDynamicToolIndex := 2000000
	getToolState := func(idx int) *toolState {
		ts, found := toolMap[idx]
		if !found {
			ts = &toolState{}
			toolMap[idx] = ts
		}
		if _, exists := toolIndexSet[idx]; !exists {
			toolIndexSet[idx] = struct{}{}
			toolIndexes = append(toolIndexes, idx)
		}
		return ts
	}
	hasDone := false

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			hasDone = true
			continue
		}

		var obj map[string]any
		if err := common.Unmarshal([]byte(payload), &obj); err != nil {
			continue
		}
		if providerReqID == "" {
			providerReqID = strVal(obj["id"])
		}

		if choices, ok := obj["choices"].([]any); ok && len(choices) > 0 {
			if c0, ok := choices[0].(map[string]any); ok {
				if delta, ok := c0["delta"].(map[string]any); ok {
					if txt := extractContent(delta["content"]); txt != "" {
						contentBuilder.WriteString(txt)
					}
					if tcs, ok := delta["tool_calls"].([]any); ok {
						for _, item := range tcs {
							tm, ok := item.(map[string]any)
							if !ok {
								continue
							}
							idx := intVal(tm["index"])
							ts := getToolState(idx)
							if id := strVal(tm["id"]); id != "" {
								ts.ID = id
							}
							if fn, ok := tm["function"].(map[string]any); ok {
								if name := strVal(fn["name"]); name != "" {
									ts.Name = name
								}
								if args := strVal(fn["arguments"]); args != "" {
									ts.Args += args
								}
							}
						}
					}
					if fc, ok := delta["function_call"].(map[string]any); ok {
						ts := getToolState(0)
						if name := strVal(fc["name"]); name != "" {
							ts.Name = name
						}
						if args := strVal(fc["arguments"]); args != "" {
							ts.Args += args
						}
					}
				}
			}
		}

		if typ := strVal(obj["type"]); typ != "" {
			switch typ {
			case "response.created":
				if response, ok := obj["response"].(map[string]any); ok {
					if id := strVal(response["id"]); id != "" {
						providerReqID = id
					}
				}
			case "response.output_text.delta":
				if txt := strVal(obj["delta"]); txt != "" {
					contentBuilder.WriteString(txt)
				}
			case "response.output_text.done":
				if contentBuilder.Len() == 0 {
					if txt := strVal(obj["text"]); txt != "" {
						contentBuilder.WriteString(txt)
					}
				}
			case "response.output_item.added", "response.output_item.done":
				if item, ok := obj["item"].(map[string]any); ok {
					itemType := strVal(item["type"])
					idx := intVal(obj["output_index"])
					switch itemType {
					case "function_call":
						ts := getToolState(idx)
						if id := firstNonEmpty(strVal(item["call_id"]), strVal(item["id"])); id != "" {
							ts.ID = id
						}
						if name := strVal(item["name"]); name != "" {
							ts.Name = name
						}
						if args := strVal(item["arguments"]); args != "" {
							ts.Args = args
						}
					case "message":
						if contentBuilder.Len() == 0 {
							if content, ok := item["content"].([]any); ok {
								parts := make([]string, 0, len(content))
								for _, p := range content {
									if pm, ok := p.(map[string]any); ok {
										if txt := strVal(pm["text"]); txt != "" {
											parts = append(parts, txt)
										}
									}
								}
								if len(parts) > 0 {
									contentBuilder.WriteString(strings.Join(parts, ""))
								}
							}
						}
					}
				}
			case "response.function_call_arguments.delta":
				idx := intVal(obj["output_index"])
				ts := getToolState(idx)
				if id := strVal(obj["item_id"]); id != "" && ts.ID == "" {
					ts.ID = id
				}
				if delta := strVal(obj["delta"]); delta != "" {
					ts.Args += delta
				}
			case "response.function_call_arguments.done":
				idx := intVal(obj["output_index"])
				ts := getToolState(idx)
				if args := strVal(obj["arguments"]); args != "" && ts.Args == "" {
					ts.Args = args
				}
			case "response.completed":
				if response, ok := obj["response"].(map[string]any); ok {
					if id := strVal(response["id"]); id != "" {
						providerReqID = id
					}
					if output, ok := response["output"].([]any); ok {
						outputText, outputToolCalls, outputHasTools := extractResponsesOutput(output)
						if contentBuilder.Len() == 0 && outputText != "" {
							contentBuilder.WriteString(outputText)
						}
						if outputHasTools {
							isTools = true
						}
						for i, tc := range outputToolCalls {
							tcm, ok := tc.(map[string]any)
							if !ok {
								continue
							}
							idx := 1000000 + i
							ts := getToolState(idx)
							if id := strVal(tcm["id"]); id != "" {
								ts.ID = id
							}
							if fn, ok := tcm["function"].(map[string]any); ok {
								if name := strVal(fn["name"]); name != "" {
									ts.Name = name
								}
								if args := strVal(fn["arguments"]); args != "" {
									ts.Args = args
								}
							}
						}
					}
				}
				hasDone = true
			case "content_block_start":
				if block, ok := obj["content_block"].(map[string]any); ok && strVal(block["type"]) == "tool_use" {
					idx := intVal(obj["index"])
					ts := getToolState(idx)
					ts.ID = strVal(block["id"])
					ts.Name = strVal(block["name"])
					if input := block["input"]; input != nil {
						if b, err := common.Marshal(input); err == nil {
							ts.Args = string(b)
						}
					}
				}
			case "content_block_delta":
				if delta, ok := obj["delta"].(map[string]any); ok {
					if txt := strVal(delta["text"]); txt != "" {
						contentBuilder.WriteString(txt)
					}
					if p := strVal(delta["partial_json"]); p != "" {
						idx := intVal(obj["index"])
						ts := getToolState(idx)
						ts.Args += p
					}
				}
			case "message_stop":
				hasDone = true
			}
		}

		if candidates, ok := obj["candidates"].([]any); ok && len(candidates) > 0 {
			candidateText, candidateToolCalls, candidateHasTools := extractGeminiCandidateOutput(candidates)
			if candidateText != "" {
				contentBuilder.WriteString(candidateText)
			}
			if candidateHasTools {
				isTools = true
			}
			for _, tc := range candidateToolCalls {
				tcm, ok := tc.(map[string]any)
				if !ok {
					continue
				}
				callID := firstNonEmpty(strVal(tcm["id"]), strVal(tcm["call_id"]))
				var functionName, functionArgs string
				if fn, ok := tcm["function"].(map[string]any); ok {
					functionName = strVal(fn["name"])
					functionArgs = strVal(fn["arguments"])
				}
				key := firstNonEmpty(callID, functionName)
				idx, exists := dynamicToolIndexByKey[key]
				if !exists {
					idx = nextDynamicToolIndex
					nextDynamicToolIndex++
					dynamicToolIndexByKey[key] = idx
				}
				ts := getToolState(idx)
				if callID != "" {
					ts.ID = callID
				}
				if functionName != "" {
					ts.Name = functionName
				}
				if functionArgs != "" {
					if len(functionArgs) >= len(ts.Args) {
						ts.Args = functionArgs
					}
				}
			}
		}
	}

	sort.Ints(toolIndexes)
	toolCalls := make([]map[string]any, 0, len(toolIndexes))
	for _, idx := range toolIndexes {
		ts := toolMap[idx]
		if ts == nil {
			continue
		}
		isTools = true
		toolCalls = append(toolCalls, map[string]any{
			"id":   ts.ID,
			"type": "function",
			"function": map[string]any{
				"name":      ts.Name,
				"arguments": ts.Args,
			},
		})
	}
	if len(toolCalls) > 0 {
		if b, err := common.Marshal(toolCalls); err == nil {
			toolCallsMerged = string(b)
		}
	}

	finalText = contentBuilder.String()
	normalized := map[string]any{
		"role":    "assistant",
		"content": finalText,
	}
	if len(toolCalls) > 0 {
		normalized["tool_calls"] = toolCalls
	}
	if b, err := common.Marshal(normalized); err == nil {
		normalizedResponse = string(b)
	}
	streamMerged = hasDone || finalText != "" || len(toolCalls) > 0
	return normalizedResponse, finalText, toolCallsMerged, streamMerged, providerReqID, isTools
}

func extractResponsesOutput(output []any) (string, []any, bool) {
	if len(output) == 0 {
		return "", nil, false
	}
	textParts := make([]string, 0, len(output))
	toolCalls := make([]any, 0)
	isTools := false

	for _, item := range output {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType := strVal(m["type"])
		switch itemType {
		case "message":
			if content, ok := m["content"].([]any); ok {
				for _, block := range content {
					if blk, ok := block.(map[string]any); ok {
						blockType := strVal(blk["type"])
						if blockType == "output_text" || blockType == "text" {
							if txt := strVal(blk["text"]); txt != "" {
								textParts = append(textParts, txt)
							}
						}
					}
				}
			}
		case "function_call":
			isTools = true
			toolCalls = append(toolCalls, map[string]any{
				"id":   firstNonEmpty(strVal(m["call_id"]), strVal(m["id"])),
				"type": "function",
				"function": map[string]any{
					"name":      strVal(m["name"]),
					"arguments": strVal(m["arguments"]),
				},
			})
		default:
			if strings.Contains(itemType, "tool") || strings.Contains(itemType, "function") {
				isTools = true
			}
		}
	}

	return strings.Join(textParts, ""), toolCalls, isTools
}

func buildMergedTimeline(reqObj map[string]any, finalText string, toolCallsMerged string) string {
	timeline := make([]map[string]any, 0)
	seq := 1
	msgs, ok := reqObj["messages"].([]any)
	if !ok || len(msgs) == 0 {
		msgs, _ = reqObj["input"].([]any)
	}
	for _, item := range msgs {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		entry := map[string]any{"seq": seq}
		if role := strVal(msg["role"]); role != "" {
			entry["role"] = role
		}
		if content := msg["content"]; content != nil {
			entry["content"] = content
		}
		if tc := msg["tool_calls"]; tc != nil {
			entry["tool_calls"] = tc
		}
		timeline = append(timeline, entry)
		seq++
	}
	assistant := map[string]any{"seq": seq, "role": "assistant"}
	if finalText != "" {
		assistant["content"] = finalText
	}
	if toolCallsMerged != "" {
		var tc any
		if err := common.Unmarshal([]byte(toolCallsMerged), &tc); err == nil {
			assistant["tool_calls"] = tc
		}
	}
	timeline = append(timeline, assistant)
	b, err := common.Marshal(timeline)
	if err != nil {
		return ""
	}
	return string(b)
}

func extractContent(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case []any:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			if m, ok := item.(map[string]any); ok {
				if txt := strVal(m["text"]); txt != "" {
					parts = append(parts, txt)
				}
			}
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}

func extractClaudeContent(content []any) string {
	parts := make([]string, 0, len(content))
	for _, block := range content {
		m, ok := block.(map[string]any)
		if !ok {
			continue
		}
		if strVal(m["type"]) == "text" {
			if txt := strVal(m["text"]); txt != "" {
				parts = append(parts, txt)
			}
		}
	}
	return strings.Join(parts, "")
}

func extractGeminiParts(parts []any) string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if p, ok := part.(map[string]any); ok {
			if txt := strVal(p["text"]); txt != "" {
				texts = append(texts, txt)
			}
		}
	}
	return strings.Join(texts, "")
}

func extractGeminiCandidateOutput(candidates []any) (text string, toolCalls []any, isTools bool) {
	if len(candidates) == 0 {
		return "", nil, false
	}
	candidate, ok := candidates[0].(map[string]any)
	if !ok {
		return "", nil, false
	}
	content, ok := candidate["content"].(map[string]any)
	if !ok {
		return "", nil, false
	}
	parts, ok := content["parts"].([]any)
	if !ok {
		return "", nil, false
	}
	return extractGeminiPartsAndToolCalls(parts)
}

func extractGeminiPartsAndToolCalls(parts []any) (text string, toolCalls []any, isTools bool) {
	if len(parts) == 0 {
		return "", nil, false
	}
	textParts := make([]string, 0, len(parts))
	toolCalls = make([]any, 0)
	for _, part := range parts {
		p, ok := part.(map[string]any)
		if !ok {
			continue
		}
		if txt := strVal(p["text"]); txt != "" {
			textParts = append(textParts, txt)
		}

		var functionCall map[string]any
		if fc, ok := p["functionCall"].(map[string]any); ok {
			functionCall = fc
		} else if fc, ok := p["function_call"].(map[string]any); ok {
			functionCall = fc
		}
		if functionCall == nil {
			if _, ok := p["functionResponse"]; ok {
				isTools = true
			}
			continue
		}

		isTools = true
		name := firstNonEmpty(
			strVal(functionCall["name"]),
			strVal(functionCall["functionName"]),
		)
		callID := firstNonEmpty(
			strVal(functionCall["id"]),
			strVal(functionCall["call_id"]),
			strVal(functionCall["callId"]),
		)
		args := valueToJSONString(functionCall["args"])
		if args == "" {
			args = valueToJSONString(functionCall["arguments"])
		}

		toolCalls = append(toolCalls, map[string]any{
			"id":      callID,
			"call_id": callID,
			"type":    "function",
			"function": map[string]any{
				"name":      name,
				"arguments": args,
			},
		})
	}
	return strings.Join(textParts, ""), toolCalls, isTools
}

func valueToJSONString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := common.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func streamChunksForEvent(stream bool, rawResponse []byte) string {
	if !stream {
		return ""
	}
	return bytesToString(rawResponse)
}

func calcPayloadDigest(rawRequest, rawResponse []byte) (int64, string) {
	totalBytes := int64(len(rawRequest) + len(rawResponse))
	h := sha256.New()
	_, _ = h.Write(rawRequest)
	_, _ = h.Write(rawResponse)
	return totalBytes, hex.EncodeToString(h.Sum(nil))
}

func strVal(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	default:
		return ""
	}
}

func intVal(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int8:
		return int(x)
	case int16:
		return int(x)
	case int32:
		return int(x)
	case int64:
		return int(x)
	case uint:
		return int(x)
	case uint8:
		return int(x)
	case uint16:
		return int(x)
	case uint32:
		return int(x)
	case uint64:
		return int(x)
	case float64:
		return int(x)
	case float32:
		return int(x)
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func bytesToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return string(b)
}

func enqueueChatLogEvent(event *dto.ChatLogEvent) {
	if event == nil {
		return
	}
	if common.RedisEnabled {
		if err := publishChatLogEventToRedis(event); err == nil {
			return
		}
	}
	select {
	case chatLogLocalQ <- event:
	default:
		common.SysError("chat log local queue is full, dropping event: " + event.RequestID)
	}
}

func publishChatLogEventToRedis(event *dto.ChatLogEvent) error {
	if common.RDB == nil {
		return fmt.Errorf("redis client is nil")
	}
	payload, err := common.Marshal(event)
	if err != nil {
		return err
	}
	xaddArgs := &redis.XAddArgs{
		Stream: common.ChatLogStreamKey,
		Values: map[string]interface{}{"event": string(payload)},
	}
	if common.ChatLogStreamMaxLen > 0 {
		xaddArgs.MaxLen = common.ChatLogStreamMaxLen
		xaddArgs.Approx = true
	}
	_, err = common.RDB.XAdd(context.Background(), xaddArgs).Result()
	if err != nil {
		common.SysError("publish chat log event to redis failed: " + err.Error())
	}
	return err
}

func chatLogLocalConsumer() {
	for event := range chatLogLocalQ {
		if event == nil {
			continue
		}
		if err := model.UpsertChatLogEvent(event); err != nil {
			event.Retry++
			if event.Retry <= common.ChatLogMaxRetry {
				time.AfterFunc(time.Duration(event.Retry)*time.Second, func() {
					enqueueChatLogEvent(event)
				})
				continue
			}
			common.SysError("chat log local consumer failed after retries: " + err.Error())
		}
	}
}

func chatLogRedisConsumerLoop(workerIndex int) {
	if common.RDB == nil {
		return
	}
	ctx := context.Background()
	group := common.ChatLogConsumerGroup
	stream := common.ChatLogStreamKey
	consumer := fmt.Sprintf("chat_log_consumer_%d_%d", workerIndex, time.Now().UnixNano())

	err := common.RDB.XGroupCreateMkStream(ctx, stream, group, common.ChatLogConsumerStartID).Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		common.SysError("create chat log consumer group failed: " + err.Error())
		return
	}

	for {
		claimPendingChatLogMessages(ctx, stream, group, consumer)

		result, readErr := common.RDB.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    group,
			Consumer: consumer,
			Streams:  []string{stream, ">"},
			Count:    int64(common.ChatLogMaxBatch),
			Block:    5 * time.Second,
		}).Result()
		if readErr != nil {
			if readErr == redis.Nil {
				continue
			}
			common.SysError("chat log XReadGroup failed: " + readErr.Error())
			time.Sleep(time.Second)
			continue
		}
		for _, xstream := range result {
			for _, msg := range xstream.Messages {
				handleChatLogRedisMessage(ctx, stream, group, msg)
			}
		}
	}
}

func claimPendingChatLogMessages(ctx context.Context, stream, group, consumer string) {
	start := "0-0"
	for {
		msgs, nextStart, err := common.RDB.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   stream,
			Group:    group,
			Consumer: consumer,
			MinIdle:  60 * time.Second,
			Start:    start,
			Count:    int64(common.ChatLogMaxBatch),
		}).Result()
		if err != nil {
			if err != redis.Nil {
				common.SysError("chat log XAutoClaim failed: " + err.Error())
			}
			return
		}
		if len(msgs) == 0 {
			return
		}
		for _, msg := range msgs {
			deliveryCount := getChatLogPendingDeliveryCount(ctx, stream, group, msg.ID)
			handleChatLogRedisMessageWithDeliveryCount(ctx, stream, group, msg, deliveryCount)
		}
		if nextStart == "0-0" || nextStart == start {
			return
		}
		start = nextStart
		if len(msgs) < common.ChatLogMaxBatch {
			return
		}
	}
}

func handleChatLogRedisMessage(ctx context.Context, stream, group string, msg redis.XMessage) {
	handleChatLogRedisMessageWithDeliveryCount(ctx, stream, group, msg, 1)
}

func handleChatLogRedisMessageWithDeliveryCount(ctx context.Context, stream, group string, msg redis.XMessage, deliveryCount int64) {
	if common.RDB == nil {
		return
	}
	ackAndDel := func() {
		_, _ = common.RDB.XAck(ctx, stream, group, msg.ID).Result()
		_, _ = common.RDB.XDel(ctx, stream, msg.ID).Result()
	}

	event, err := decodeChatLogEvent(msg.Values)
	if err != nil {
		publishChatLogDLQ(msg.Values, "decode_event_failed: "+err.Error())
		ackAndDel()
		return
	}

	if upsertErr := model.UpsertChatLogEvent(event); upsertErr != nil {
		ack, dlq, reason := decideChatLogFailureAction(event, deliveryCount, upsertErr)
		if dlq {
			publishChatLogDLQ(msg.Values, reason)
		}
		if ack {
			ackAndDel()
			return
		}
		common.SysError(fmt.Sprintf("chat log upsert failed, keep pending for retry: request_id=%s delivery_count=%d error=%s",
			event.RequestID, deliveryCount, upsertErr.Error()))
		return
	}

	ackAndDel()
}

func decideChatLogFailureAction(event *dto.ChatLogEvent, deliveryCount int64, upsertErr error) (ack bool, dlq bool, reason string) {
	if upsertErr == nil {
		return true, false, ""
	}
	maxRetry := common.ChatLogMaxRetry
	if maxRetry <= 0 {
		maxRetry = 1
	}
	if deliveryCount <= int64(maxRetry) {
		return false, false, ""
	}
	requestID := ""
	if event != nil {
		requestID = event.RequestID
	}
	return true, true, fmt.Sprintf("upsert_failed after %d deliveries request_id=%s: %s", deliveryCount, requestID, upsertErr.Error())
}

func getChatLogPendingDeliveryCount(ctx context.Context, stream, group, id string) int64 {
	if common.RDB == nil || strings.TrimSpace(id) == "" {
		return 1
	}
	rows, err := common.RDB.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: stream,
		Group:  group,
		Start:  id,
		End:    id,
		Count:  1,
	}).Result()
	if err != nil || len(rows) == 0 {
		if err != nil && err != redis.Nil {
			common.SysError("chat log XPENDING failed: " + err.Error())
		}
		return 1
	}
	if rows[0].RetryCount <= 0 {
		return 1
	}
	return rows[0].RetryCount
}

func chatLogRetryDelay(deliveryCount int64) time.Duration {
	if deliveryCount <= 0 {
		deliveryCount = 1
	}
	delay := time.Duration(deliveryCount) * time.Second
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

func decodeChatLogEvent(values map[string]interface{}) (*dto.ChatLogEvent, error) {
	raw, ok := values["event"]
	if !ok {
		return nil, fmt.Errorf("missing event field")
	}
	var eventStr string
	switch v := raw.(type) {
	case string:
		eventStr = v
	case []byte:
		eventStr = string(v)
	default:
		return nil, fmt.Errorf("invalid event field type: %T", raw)
	}
	event := &dto.ChatLogEvent{}
	if err := common.UnmarshalJsonStr(eventStr, event); err != nil {
		return nil, err
	}
	return event, nil
}

func publishChatLogDLQ(values map[string]interface{}, reason string) {
	if common.RDB == nil {
		common.SysError("chat log DLQ drop (redis unavailable): " + reason)
		return
	}
	dlqValues := map[string]interface{}{
		"reason":       reason,
		"failed_at":    chatLogNow().Format(time.RFC3339Nano),
		"origin_event": "",
	}
	if raw, ok := values["event"]; ok {
		dlqValues["origin_event"] = raw
	}
	if _, err := common.RDB.XAdd(context.Background(), &redis.XAddArgs{
		Stream: common.ChatLogDLQKey,
		Values: dlqValues,
		MaxLen: common.ChatLogDLQMaxLen,
		Approx: common.ChatLogDLQMaxLen > 0,
	}).Result(); err != nil {
		common.SysError("chat log publish DLQ failed: " + err.Error())
	}
}
