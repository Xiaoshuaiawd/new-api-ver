package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	common2 "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/error_model_redaction"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

	redacted := redactRelayError(nil, apiErr, relayInfo)

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

func TestProcessChannelErrorRedactsLogContentButPreservesModelField(t *testing.T) {
	originalPatterns := error_model_redaction.PatternsJSON()
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousErrorLogEnabled := constant.ErrorLogEnabled
	previousRedisEnabled := common2.RedisEnabled
	t.Cleanup(func() {
		require.NoError(t, error_model_redaction.UpdatePatternsJSON(originalPatterns))
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		constant.ErrorLogEnabled = previousErrorLogEnabled
		common2.RedisEnabled = previousRedisEnabled
	})
	require.NoError(t, error_model_redaction.UpdatePatternsJSON(`["^gpt.*"]`))
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	model.DB = db
	model.LOG_DB = db
	constant.ErrorLogEnabled = true
	common2.RedisEnabled = false

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("original_model", "gpt-5.6-luna")
	c.Set("channel_id", 9)
	c.Set("id", 7)
	apiErr := types.NewErrorWithStatusCode(
		errors.New("unknown provider for model gpt-5.6-luna"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadRequest,
	)
	relayInfo := &common.RelayInfo{OriginModelName: "gpt-5.6-luna"}

	processChannelError(c, *types.NewChannelError(9, 1, "test", false, "", false), apiErr, relayInfo)

	var stored model.Log
	require.NoError(t, db.First(&stored).Error)
	assert.Equal(t, "gpt-5.6-luna", stored.ModelName)
	assert.Equal(t, "status_code=400, unknown provider for model ****", stored.Content)
}
