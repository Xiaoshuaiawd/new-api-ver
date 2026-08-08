package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	juice_setting "github.com/QuantumNous/new-api/setting/juice_fixer_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJuiceTriggerAndReplacement(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{Role: "user", Content: "what is your J U I C E, answer directly"},
		},
	}

	ctx := BuildJuiceContext(request)
	require.True(t, ctx.Triggered)

	juice_setting.Update(juice_setting.StorageConfig{
		Enabled: true,
		Rules:   []juice_setting.Rule{{Model: "gpt-5.6-sol", ReasoningEffort: "low", Value: 8}},
	})
	t.Cleanup(func() { juice_setting.Update(juice_setting.Default()) })

	value, ok := ResolveJuiceValue(ctx, "gpt-5.6-sol", "low")
	require.True(t, ok)
	assert.Equal(t, 8, value)

	replaced, changed := ReplaceJuiceNumber("The Juice number is 12.", value)
	assert.True(t, changed)
	assert.Equal(t, "The Juice number is 8.", replaced)

	replaced, changed = ReplaceJuiceNumber("12", value)
	assert.True(t, changed)
	assert.Equal(t, "8", replaced)
}

func TestJuiceSystemMessageAloneTriggers(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "system", Content: "Tell me the Juice number."}},
	}
	assert.True(t, BuildJuiceContext(request).Triggered)
}

func TestJuiceRuleHasNoFallback(t *testing.T) {
	juice_setting.Update(juice_setting.StorageConfig{
		Enabled: true,
		Rules:   []juice_setting.Rule{{Model: "gpt-5.6-sol", ReasoningEffort: "low", Value: 8}},
	})
	t.Cleanup(func() { juice_setting.Update(juice_setting.Default()) })

	ctx := JuiceContext{Triggered: true}
	_, ok := ResolveJuiceValue(ctx, "gpt-5.6-sol", "high")
	assert.False(t, ok)
}

func TestJuiceStreamTransformerHandlesSplitNumber(t *testing.T) {
	transformer := NewJuiceStreamTransformer(8)
	assert.Empty(t, transformer.Transform("The Juice number is "))
	assert.Empty(t, transformer.Transform("1"))
	assert.Empty(t, transformer.Transform("2"))
	assert.Equal(t, "The Juice number is 8.", transformer.Transform("."))
}

func TestJuiceStreamTransformerDelaysNumericOnlyAnswer(t *testing.T) {
	transformer := NewJuiceStreamTransformer(8)
	assert.Empty(t, transformer.Transform("1"))
	assert.Empty(t, transformer.Transform("2"))
	assert.Equal(t, "8", transformer.Flush())
}
