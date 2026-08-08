package openai

import (
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func transformChatResponse(c *gin.Context, info *relaycommon.RelayInfo, response *dto.OpenAITextResponse) bool {
	value, ok := service.ResolveJuiceValue(service.GetJuiceContext(c), info.OriginModelName, info.ReasoningEffort)
	if !ok {
		return false
	}
	changed := false
	for i := range response.Choices {
		message := &response.Choices[i].Message
		if message.IsStringContent() {
			text, replaced := service.ReplaceJuiceNumber(message.StringContent(), value)
			if replaced {
				message.SetStringContent(text)
				changed = true
			}
			continue
		}
		parts := message.ParseContent()
		partChanged := false
		for j := range parts {
			if parts[j].Type != dto.ContentTypeText {
				continue
			}
			text, replaced := service.ReplaceJuiceNumber(parts[j].Text, value)
			if replaced {
				parts[j].Text = text
				partChanged = true
			}
		}
		if partChanged {
			message.SetMediaContent(parts)
			changed = true
		}
	}
	return changed
}

func transformResponsesResponse(c *gin.Context, info *relaycommon.RelayInfo, response *dto.OpenAIResponsesResponse) bool {
	value, ok := service.ResolveJuiceValue(service.GetJuiceContext(c), info.OriginModelName, info.ReasoningEffort)
	if !ok {
		return false
	}
	changed := false
	for i := range response.Output {
		for j := range response.Output[i].Content {
			content := &response.Output[i].Content[j]
			if content.Type != "output_text" && content.Type != "text" {
				continue
			}
			text, replaced := service.ReplaceJuiceNumber(content.Text, value)
			if replaced {
				content.Text = text
				changed = true
			}
		}
	}
	return changed
}

func transformChatStreamResponse(c *gin.Context, info *relaycommon.RelayInfo, response *dto.ChatCompletionsStreamResponse) bool {
	value, ok := service.ResolveJuiceValue(service.GetJuiceContext(c), info.OriginModelName, info.ReasoningEffort)
	if !ok {
		return false
	}
	changed := false
	for i := range response.Choices {
		content := response.Choices[i].Delta.GetContentString()
		if content == "" {
			continue
		}
		text, replaced := service.ReplaceJuiceNumber(content, value)
		if replaced {
			response.Choices[i].Delta.SetContentString(text)
			changed = true
		}
	}
	return changed
}

func transformResponsesStreamResponse(c *gin.Context, info *relaycommon.RelayInfo, response *dto.ResponsesStreamResponse) bool {
	value, ok := service.ResolveJuiceValue(service.GetJuiceContext(c), info.OriginModelName, info.ReasoningEffort)
	if !ok || response == nil {
		return false
	}
	if response.Type != "response.output_text.delta" {
		return false
	}
	text, replaced := service.ReplaceJuiceNumber(response.Delta, value)
	if replaced {
		response.Delta = text
		return true
	}
	return false
}
