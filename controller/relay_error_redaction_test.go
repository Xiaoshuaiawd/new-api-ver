package controller

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/error_model_redaction"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactedRelayErrorHidesOriginalAndMappedModelsWithoutMutation(t *testing.T) {
	originalPatterns := error_model_redaction.PatternsJSON()
	t.Cleanup(func() { require.NoError(t, error_model_redaction.UpdatePatternsJSON(originalPatterns)) })
	require.NoError(t, error_model_redaction.UpdatePatternsJSON(`["^gpt.*"]`))

	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message: "unknown provider for model gpt-5.6-luna mapped to gpt-private-v2",
		Type:    "invalid_request_error",
		Code:    "unknown_provider",
	}, http.StatusBadRequest)
	relayInfo := &common.RelayInfo{
		OriginModelName: "gpt-5.6-luna",
		ChannelMeta: &common.ChannelMeta{
			UpstreamModelName: "gpt-private-v2",
		},
	}

	redacted := redactRelayError(apiErr, relayInfo)

	assert.Equal(t, "unknown provider for model **** mapped to ****", redacted.Error())
	assert.Equal(t, "unknown provider for model **** mapped to ****", redacted.ToOpenAIError().Message)
	assert.Equal(t, "unknown provider for model gpt-5.6-luna mapped to gpt-private-v2", apiErr.Error())
	assert.Equal(t, "unknown provider for model gpt-5.6-luna mapped to gpt-private-v2", apiErr.ToOpenAIError().Message)
}

func TestRedactedRelayErrorTextPreservesStatusCode(t *testing.T) {
	originalPatterns := error_model_redaction.PatternsJSON()
	t.Cleanup(func() { require.NoError(t, error_model_redaction.UpdatePatternsJSON(originalPatterns)) })
	require.NoError(t, error_model_redaction.UpdatePatternsJSON(`["^gpt.*"]`))

	apiErr := types.NewErrorWithStatusCode(
		errors.New("unknown provider for model gpt-5.6-luna"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadRequest,
	)
	c, _ := gin.CreateTestContext(nil)
	c.Set("original_model", "gpt-5.6-luna")

	assert.Equal(
		t,
		"status_code=400, unknown provider for model ****",
		redactRelayErrorText(c, nil, apiErr.MaskSensitiveErrorWithStatusCode()),
	)
}
