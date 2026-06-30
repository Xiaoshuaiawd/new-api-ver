package service

import (
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
)

func RequestRequiresImageInputSupport(request dto.Request) bool {
	if request == nil {
		return false
	}
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		return openAIRequestRequiresImageInputSupport(req)
	case *dto.OpenAIResponsesRequest:
		return responsesRequestRequiresImageInputSupport(req)
	case *dto.ClaudeRequest:
		return claudeRequestRequiresImageInputSupport(req)
	case *dto.GeminiChatRequest:
		return geminiRequestRequiresImageInputSupport(req)
	case *dto.ImageRequest:
		return imageRequestRequiresImageInputSupport(req)
	default:
		return TokenMetaRequiresImageInputSupport(request.GetTokenCountMeta())
	}
}

func TokenMetaRequiresImageInputSupport(meta *types.TokenCountMeta) bool {
	if meta == nil {
		return false
	}
	for _, file := range meta.Files {
		if file != nil && file.FileType == types.FileTypeImage {
			return true
		}
	}
	return false
}

func openAIRequestRequiresImageInputSupport(request *dto.GeneralOpenAIRequest) bool {
	if request == nil {
		return false
	}
	for _, message := range request.Messages {
		for _, content := range openAIMessageContent(message) {
			if content.Type == dto.ContentTypeImageURL && content.ToFileSource() != nil {
				return true
			}
		}
	}
	return false
}

func openAIMessageContent(message dto.Message) []dto.MediaContent {
	if mediaContent, ok := message.Content.([]dto.MediaContent); ok {
		return mediaContent
	}
	return message.ParseContent()
}

func responsesRequestRequiresImageInputSupport(request *dto.OpenAIResponsesRequest) bool {
	if request == nil {
		return false
	}
	for _, input := range request.ParseInput() {
		if input.Type == "input_image" && strings.TrimSpace(input.ImageUrl) != "" {
			return true
		}
	}
	return false
}

func claudeRequestRequiresImageInputSupport(request *dto.ClaudeRequest) bool {
	if request == nil {
		return false
	}
	for _, message := range request.Messages {
		if message.IsStringContent() {
			continue
		}
		contents, _ := message.ParseContent()
		for _, content := range contents {
			if content.Type == "image" && content.ToFileSource() != nil {
				return true
			}
		}
	}
	return false
}

func geminiRequestRequiresImageInputSupport(request *dto.GeminiChatRequest) bool {
	if request == nil {
		return false
	}
	for _, content := range request.Contents {
		for _, part := range content.Parts {
			if part.InlineData != nil &&
				strings.HasPrefix(part.InlineData.MimeType, "image/") &&
				part.InlineData.ToFileSource() != nil {
				return true
			}
			if part.FileData != nil &&
				strings.HasPrefix(part.FileData.MimeType, "image/") &&
				strings.TrimSpace(part.FileData.FileUri) != "" {
				return true
			}
		}
	}
	return false
}

func imageRequestRequiresImageInputSupport(request *dto.ImageRequest) bool {
	if request == nil {
		return false
	}
	return rawJSONHasValue(request.Images) || rawJSONHasValue(request.Image) || rawJSONHasValue(request.Mask)
}

func rawJSONHasValue(raw []byte) bool {
	value := strings.TrimSpace(string(raw))
	return value != "" && value != "null" && value != `""` && value != "[]"
}
