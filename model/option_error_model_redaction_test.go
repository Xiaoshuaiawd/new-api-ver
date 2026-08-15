package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateOptionValueValidatesErrorModelRedactionPatterns(t *testing.T) {
	require.NoError(t, validateOptionValue("ErrorModelRedactionPatterns", `["^gpt.*", "^claude.*"]`))
	assert.Error(t, validateOptionValue("ErrorModelRedactionPatterns", `["[invalid"]`))
}
