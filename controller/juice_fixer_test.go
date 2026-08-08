package controller

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateJuiceFixerConfigRejectsInvalidRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("PUT", "/api/juice-fixer/config", strings.NewReader(`{"enabled":true,"rules":[{"model":"","reasoning_effort":"low","value":8}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	UpdateJuiceFixerConfig(c)

	require.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "model is required")
}
