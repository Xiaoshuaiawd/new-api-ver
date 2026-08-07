package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldRetryOverloadErrorWithSpecificChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("specific_channel_id", "32")
	err := types.WithOpenAIError(types.OpenAIError{
		Type:    "service_unavailable_error",
		Code:    "server_is_overloaded",
		Message: "Our servers are currently overloaded. Please try again later.",
	}, 500)

	require.True(t, shouldRetry(c, err, 10))
}
