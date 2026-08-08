package juice_fixer_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRejectsInvalidRules(t *testing.T) {
	tests := []StorageConfig{
		{Enabled: true, Rules: []Rule{{Model: "", ReasoningEffort: "low", Value: 8}}},
		{Enabled: true, Rules: []Rule{{Model: "gpt", ReasoningEffort: "low", Value: -1}}},
		{Enabled: true, Rules: []Rule{{Model: "gpt", ReasoningEffort: "low", Value: 8}, {Model: "gpt", ReasoningEffort: "low", Value: 9}}},
	}
	for _, cfg := range tests {
		assert.Error(t, Validate(cfg))
	}
}

func TestNormalizeCopiesAndTrimsRules(t *testing.T) {
	cfg, err := Normalize(StorageConfig{Enabled: true, Rules: []Rule{{Model: " gpt ", ReasoningEffort: " low ", Value: 8}}})
	require.NoError(t, err)
	assert.Equal(t, "gpt", cfg.Rules[0].Model)
	assert.Equal(t, "low", cfg.Rules[0].ReasoningEffort)
}
