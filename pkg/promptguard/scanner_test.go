package promptguard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// JSON format (general models like gpt-4o-mini) and tolerant fallback.
func TestParseGuardContentJSON(t *testing.T) {
	cases := []struct {
		name       string
		content    string
		wantSafety string
		wantCats   []string
		wantErr    bool
	}{
		{
			name:       "plain json",
			content:    `{"safety":"Unsafe","categories":["Jailbreak"]}`,
			wantSafety: "Unsafe",
			wantCats:   []string{"Jailbreak"},
		},
		{
			name:       "json wrapped in markdown fence",
			content:    "```json\n{\"safety\":\"Safe\",\"categories\":[]}\n```",
			wantSafety: "Safe",
			wantCats:   []string{},
		},
		{
			name:       "json with null categories",
			content:    `{"safety":"Safe"}`,
			wantSafety: "Safe",
			wantCats:   []string{},
		},
		{
			name:       "fallback: line format still parses via unified parser",
			content:    "Safety: Unsafe\nCategories: PII",
			wantSafety: "Unsafe",
			wantCats:   []string{"PII"},
		},
		{
			name:    "garbage is invalid",
			content: "I cannot classify this.",
			wantErr: true,
		},
		{
			name:    "json with unknown safety is invalid",
			content: `{"safety":"Dangerous","categories":[]}`,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := parseGuardContent(tc.content)
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

// callGuardEndpoint should use a custom system prompt when provided, and fall
// back to the built-in default when empty. This test drives it against a mock
// server and asserts the outgoing system message.
func TestCallGuardEndpointSystemPrompt(t *testing.T) {
	cases := []struct {
		name         string
		customPrompt string
		wantSystem   string
	}{
		{name: "custom prompt used", customPrompt: "MY CUSTOM CLASSIFIER", wantSystem: "MY CUSTOM CLASSIFIER"},
		{name: "empty falls back to default", customPrompt: "", wantSystem: DefaultJSONSystemPrompt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotSystem string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body scanRequest
				_ = json.NewDecoder(r.Body).Decode(&body)
				for _, m := range body.Messages {
					if m.Role == "system" {
						gotSystem = m.Content
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"safety\":\"Safe\",\"categories\":[]}"},"finish_reason":"stop"}]}`))
			}))
			defer srv.Close()

			ep := Endpoint{ID: "t", BaseURL: srv.URL, Model: "gpt-4o-mini", Format: FormatJSON, TimeoutMS: 2000, Enabled: true}
			resp, err := callGuardEndpoint(context.Background(), srv.Client(), ep, "hello", tc.customPrompt)
			require.NoError(t, err)
			assert.Equal(t, "Safe", resp.Safety)
			assert.Equal(t, tc.wantSystem, gotSystem)
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
