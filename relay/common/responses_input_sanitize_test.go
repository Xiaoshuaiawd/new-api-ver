package common

import (
	"testing"

	projectcommon "github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeResponsesInputItemIDsDropsIncompatibleIDs(t *testing.T) {
	raw := []byte(`[
		{"type":"message","id":"item_6bf22f97126db4c57e0df4e0","role":"assistant","content":[]},
		{"type":"custom_tool_call","id":"item_72437f65f49630ddbdfba485","call_id":"call_1","name":"exec","input":"{}"},
		{"type":"message","id":"msg_123","role":"user","content":"keep"},
		{"type":"function_call","id":"fc_123","call_id":"call_2","name":"lookup","arguments":"{}"}
	]`)

	sanitized, changed, err := SanitizeResponsesInputItemIDs(raw)
	require.NoError(t, err)
	assert.True(t, changed)

	var items []map[string]any
	require.NoError(t, projectcommon.Unmarshal(sanitized, &items))
	assert.NotContains(t, items[0], "id")
	assert.NotContains(t, items[1], "id")
	assert.Equal(t, "msg_123", items[2]["id"])
	assert.Equal(t, "fc_123", items[3]["id"])
}

func TestSanitizeResponsesRequestBodyPreservesValidItems(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[{"type":"message","id":"msg_123","role":"user","content":"hello"}],
		"client_metadata":{"source":"codex"}
	}`)

	sanitized, changed, err := SanitizeResponsesRequestBody(raw)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, raw, sanitized)
}

func TestSanitizeResponsesRequestBodyUpdatesOnlyInput(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[{"type":"message","id":"item_1","role":"assistant","content":[]}],
		"client_metadata":{"source":"codex"}
	}`)

	sanitized, changed, err := SanitizeResponsesRequestBody(raw)
	require.NoError(t, err)
	assert.True(t, changed)

	var body map[string]any
	require.NoError(t, projectcommon.Unmarshal(sanitized, &body))
	items := body["input"].([]any)
	assert.NotContains(t, items[0].(map[string]any), "id")
	assert.Equal(t, "codex", body["client_metadata"].(map[string]any)["source"])
}
