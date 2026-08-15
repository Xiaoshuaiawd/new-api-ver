package error_model_redaction

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactMatchingModelNames(t *testing.T) {
	original := PatternsJSON()
	t.Cleanup(func() { require.NoError(t, UpdatePatternsJSON(original)) })
	require.NoError(t, UpdatePatternsJSON(`["^gpt.*", "^claude-.*"]`))

	tests := []struct {
		name       string
		message    string
		modelNames []string
		want       string
	}{
		{
			name:       "original model",
			message:    "status_code=400, unknown provider for model gpt-5.6-luna",
			modelNames: []string{"gpt-5.6-luna"},
			want:       "status_code=400, unknown provider for model ****",
		},
		{
			name:       "mapped model",
			message:    "upstream rejected claude-private-v2.",
			modelNames: []string{"public-model", "claude-private-v2"},
			want:       "upstream rejected ****.",
		},
		{
			name:       "nonmatching model",
			message:    "unknown provider for model gemini-2.5-pro",
			modelNames: []string{"gemini-2.5-pro"},
			want:       "unknown provider for model gemini-2.5-pro",
		},
		{
			name:       "longest model first",
			message:    "gpt-5.6-luna is mapped from gpt-5",
			modelNames: []string{"gpt-5", "gpt-5.6-luna"},
			want:       "**** is mapped from ****",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Redact(tt.message, tt.modelNames...))
		})
	}
}

func TestUpdatePatternsJSONRejectsInvalidRegexAtomically(t *testing.T) {
	original := PatternsJSON()
	t.Cleanup(func() { require.NoError(t, UpdatePatternsJSON(original)) })
	require.NoError(t, UpdatePatternsJSON(`["^gpt.*"]`))

	err := UpdatePatternsJSON(`["[invalid"]`)

	require.ErrorContains(t, err, "[invalid")
	assert.Equal(t, `["^gpt.*"]`, PatternsJSON())
	assert.Equal(t, "model ****", Redact("model gpt-5.6-luna", "gpt-5.6-luna"))
}

func TestValidatePatternsJSONRejectsNonArrayJSON(t *testing.T) {
	assert.Error(t, ValidatePatternsJSON(`{"pattern":"^gpt.*"}`))
}
