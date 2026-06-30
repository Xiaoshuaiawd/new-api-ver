package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
)

func TestRequestRequiresImageInputSupportDetectsOpenAIImageContent(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{
				Role: "user",
				Content: []dto.MediaContent{
					{Type: dto.ContentTypeText, Text: "what is this?"},
					{
						Type:     dto.ContentTypeImageURL,
						ImageUrl: map[string]any{"url": "https://example.test/cat.png"},
					},
				},
			},
		},
	}

	assert.True(t, RequestRequiresImageInputSupport(request))
}

func TestRequestRequiresImageInputSupportIgnoresTextOnlyRequests(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{Role: "user", Content: "hello"},
		},
	}

	assert.False(t, RequestRequiresImageInputSupport(request))
}

func TestRequestRequiresImageInputSupportDetectsClaudeImageContent(t *testing.T) {
	request := &dto.ClaudeRequest{
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []dto.ClaudeMediaMessage{
					{
						Type: "image",
						Source: &dto.ClaudeMessageSource{
							Type:      "base64",
							MediaType: "image/png",
							Data:      "abc",
						},
					},
				},
			},
		},
	}

	assert.True(t, RequestRequiresImageInputSupport(request))
}

func TestTokenMetaRequiresImageInputSupportDetectsAnyImageFile(t *testing.T) {
	meta := &types.TokenCountMeta{
		Files: []*types.FileMeta{
			{FileType: types.FileTypeAudio},
			{FileType: types.FileTypeImage},
		},
	}

	assert.True(t, TokenMetaRequiresImageInputSupport(meta))
}

func TestRequestRequiresImageInputSupportIgnoresImageGenerationWithoutInputImage(t *testing.T) {
	request := &dto.ImageRequest{
		Model:  "gpt-image-1",
		Prompt: "draw a house",
	}

	assert.False(t, RequestRequiresImageInputSupport(request))
}

func TestRequestRequiresImageInputSupportDetectsImageEditInput(t *testing.T) {
	request := &dto.ImageRequest{
		Model:  "gpt-image-1",
		Prompt: "make it brighter",
		Image:  []byte(`"data:image/png;base64,abc"`),
	}

	assert.True(t, RequestRequiresImageInputSupport(request))
}
