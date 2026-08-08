package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransformChatStreamChunksPreservesReasoningAndReplacesSplitText(t *testing.T) {
	chunks := []string{
		`{"choices":[{"index":0,"delta":{"reasoning_content":"reasoning 12"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"The Juice number is 1"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"2."}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"total_tokens":9}}`,
	}

	transformed := service.TransformChatStreamChunks(chunks, 8)
	require.Len(t, transformed, len(chunks))
	assert.NotContains(t, transformed[1], `"content"`)
	assert.Contains(t, transformed[2], `"content":"The Juice number is 8."`)
	assert.Contains(t, transformed[0], `"reasoning_content":"reasoning 12"`)
	assert.Contains(t, transformed[3], `"total_tokens":9`)
}

func TestTransformResponsesStreamChunksPreservesCompletedEvent(t *testing.T) {
	chunks := []string{
		`{"type":"response.output_text.delta","delta":"Juice: 1"}`,
		`{"type":"response.output_text.delta","delta":"2"}`,
		`{"type":"response.completed","response":{"usage":{"total_tokens":4}}}`,
	}

	transformed := service.TransformResponsesStreamChunks(chunks, 8)
	assert.Contains(t, transformed[1], `"delta":"Juice: 8"`)
	assert.Contains(t, transformed[2], `"type":"response.completed"`)
	assert.Contains(t, transformed[2], `"total_tokens":4`)
}
