package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClearLogBodyDetailsRemovesBodiesAndKeepsOtherDetails(t *testing.T) {
	truncateTables(t)

	withBodies := map[string]interface{}{
		"log_detail": map[string]interface{}{
			"request_body":             `{"model":"gpt","messages":[]}`,
			"request_body_truncated":   true,
			"request_body_size":        float64(123456),
			"response_body":            `{"error":"upstream failed"}`,
			"response_body_truncated":  true,
			"response_body_size":       float64(234567),
			"upstream_debug_reference": "keep-me",
		},
		"error_code": "bad_gateway",
	}
	withoutBodies := map[string]interface{}{
		"log_detail": map[string]interface{}{
			"upstream_debug_reference": "already-small",
		},
	}
	require.NoError(t, LOG_DB.Create(&Log{Other: common.MapToJsonStr(withBodies)}).Error)
	require.NoError(t, LOG_DB.Create(&Log{Other: common.MapToJsonStr(withoutBodies)}).Error)
	require.NoError(t, LOG_DB.Create(&Log{Other: "not-json"}).Error)

	count, err := ClearLogBodyDetails(context.Background(), 10)

	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	var logs []Log
	require.NoError(t, LOG_DB.Order("id asc").Find(&logs).Error)
	require.Len(t, logs, 3)

	updated, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.Equal(t, "bad_gateway", updated["error_code"])
	detail, ok := updated["log_detail"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "keep-me", detail["upstream_debug_reference"])
	assert.NotContains(t, detail, "request_body")
	assert.NotContains(t, detail, "request_body_truncated")
	assert.NotContains(t, detail, "request_body_size")
	assert.NotContains(t, detail, "response_body")
	assert.NotContains(t, detail, "response_body_truncated")
	assert.NotContains(t, detail, "response_body_size")

	unchanged, err := common.StrToMap(logs[1].Other)
	require.NoError(t, err)
	assert.Equal(t, withoutBodies, unchanged)
	assert.Equal(t, "not-json", logs[2].Other)
}
