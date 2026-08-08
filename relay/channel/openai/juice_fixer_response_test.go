package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	juice_setting "github.com/QuantumNous/new-api/setting/juice_fixer_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func juiceTestContext(t *testing.T) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	service.SetJuiceContext(c, service.JuiceContext{Triggered: true})
	juice_setting.Update(juice_setting.StorageConfig{
		Enabled: true,
		Rules:   []juice_setting.Rule{{Model: "gpt-5.6-sol", ReasoningEffort: "low", Value: 8}},
	})
	t.Cleanup(func() { juice_setting.Update(juice_setting.Default()) })
	return c, &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol", ReasoningEffort: "low"}
}

func TestTransformChatResponseOnlyChangesJuiceText(t *testing.T) {
	c, info := juiceTestContext(t)
	response := &dto.OpenAITextResponse{
		Choices: []dto.OpenAITextResponseChoice{{
			Message: dto.Message{Content: "The Juice number is 12."},
		}},
		Usage: dto.Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7},
	}

	require.True(t, transformChatResponse(c, info, response))
	assert.Equal(t, "The Juice number is 8.", response.Choices[0].Message.StringContent())
	assert.Equal(t, 7, response.Usage.TotalTokens)
}

func TestTransformResponsesResponseLeavesReasoningAndUsage(t *testing.T) {
	c, info := juiceTestContext(t)
	response := &dto.OpenAIResponsesResponse{
		Output: []dto.ResponsesOutput{
			{Type: "reasoning", Content: []dto.ResponsesOutputContent{{Type: "summary_text", Text: "reasoning 12"}}},
			{Type: "message", Content: []dto.ResponsesOutputContent{{Type: "output_text", Text: "Juice: 12"}}},
		},
		Usage: &dto.Usage{InputTokens: 2, OutputTokens: 5, TotalTokens: 7},
	}

	require.True(t, transformResponsesResponse(c, info, response))
	assert.Equal(t, "reasoning 12", response.Output[0].Content[0].Text)
	assert.Equal(t, "Juice: 8", response.Output[1].Content[0].Text)
	assert.Equal(t, 7, response.Usage.TotalTokens)
}
