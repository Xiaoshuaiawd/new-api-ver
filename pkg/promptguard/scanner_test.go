package promptguard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// qwen3guard returns a line-based format, not JSON. These tests lock that contract.
func TestParseQwen3GuardContent(t *testing.T) {
	cases := []struct {
		name       string
		content    string
		wantSafety string
		wantCats   []string
		wantErr    bool
	}{
		{
			name:       "safe no categories",
			content:    "Safety: Safe\nCategories: None",
			wantSafety: "Safe",
			wantCats:   []string{},
		},
		{
			name:       "unsafe with categories",
			content:    "Safety: Unsafe\nCategories: Violent, Jailbreak",
			wantSafety: "Unsafe",
			wantCats:   []string{"Violent", "Jailbreak"},
		},
		{
			name:       "controversial",
			content:    "Safety: Controversial\nCategories: PII",
			wantSafety: "Controversial",
			wantCats:   []string{"PII"},
		},
		{
			name:       "lowercase and extra whitespace",
			content:    "safety:  unsafe \ncategories:  jailbreak ",
			wantSafety: "Unsafe",
			wantCats:   []string{"jailbreak"},
		},
		{
			name:       "ignores auxiliary fields",
			content:    "Safety: Safe\nRefusal: No\nCategories: None",
			wantSafety: "Safe",
			wantCats:   []string{},
		},
		{
			name:    "missing categories line is invalid",
			content: "Safety: Safe",
			wantErr: true,
		},
		{
			name:    "unknown safety value is invalid",
			content: "Safety: Maybe\nCategories: None",
			wantErr: true,
		},
		{
			name:    "duplicate safety line is invalid",
			content: "Safety: Safe\nSafety: Unsafe\nCategories: None",
			wantErr: true,
		},
		{
			name:    "empty content is invalid",
			content: "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := parseQwen3GuardContent(tc.content)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantSafety, resp.Safety)
			assert.Equal(t, tc.wantCats, resp.Categories)
		})
	}
}

// Verify the decision matrix wiring end-to-end from a parsed response.
func TestDecisionMatrixFromParsedResponse(t *testing.T) {
	// Unsafe + enabled category → block
	resp, err := parseQwen3GuardContent("Safety: Unsafe\nCategories: Jailbreak")
	require.NoError(t, err)
	d := applyDecisionMatrix(resp, []string{"jailbreak"})
	assert.Equal(t, DecisionBlock, d.Kind)

	// Unsafe but category not enabled → flag
	d = applyDecisionMatrix(resp, []string{"violent"})
	assert.Equal(t, DecisionFlag, d.Kind)

	// Controversial + jailbreak → always block regardless of enabled set
	resp, err = parseQwen3GuardContent("Safety: Controversial\nCategories: Jailbreak")
	require.NoError(t, err)
	d = applyDecisionMatrix(resp, []string{})
	assert.Equal(t, DecisionBlock, d.Kind)

	// Safe → allow
	resp, err = parseQwen3GuardContent("Safety: Safe\nCategories: None")
	require.NoError(t, err)
	d = applyDecisionMatrix(resp, AllScannerIDs)
	assert.Equal(t, DecisionAllow, d.Kind)
}
