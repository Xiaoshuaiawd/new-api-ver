package prompt_guard_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/promptguard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression: with plaintext token storage (decrypt == nil), the stored token
// MUST reach the runtime config. Previously buildActive left it empty when no
// decryptor was passed, so live relay requests called upstream with an empty
// token and got 401 → prompt_guard_unavailable, even though the probe (which
// uses the freshly typed token) worked.
func TestBuildActivePlaintextTokenReachesRuntime(t *testing.T) {
	cfg := StorageConfig{
		Enabled: true,
		Endpoints: []StorageEndpoint{
			{
				ID:          "n1",
				Name:        "node1",
				BaseURL:     "https://api.deepseek.com",
				Model:       "deepseek-v4-flash",
				Format:      promptguard.FormatJSON,
				TokenCipher: "sk-plaintext-token",
				Enabled:     true,
			},
		},
	}

	active := buildActive(cfg, nil)
	require.Len(t, active.Endpoints, 1)
	assert.Equal(t, "sk-plaintext-token", active.Endpoints[0].Token,
		"plaintext token must be carried to runtime config when decrypt is nil")
	assert.Equal(t, promptguard.FormatJSON, active.Endpoints[0].Format)
}

// When a decryptor is supplied it should be used to turn ciphertext into the token.
func TestBuildActiveWithDecryptor(t *testing.T) {
	cfg := StorageConfig{
		Endpoints: []StorageEndpoint{
			{ID: "n1", TokenCipher: "CIPHER", Enabled: true},
		},
	}
	active := buildActive(cfg, func(s string) (string, error) {
		if s == "CIPHER" {
			return "decrypted-token", nil
		}
		return "", assert.AnError
	})
	require.Len(t, active.Endpoints, 1)
	assert.Equal(t, "decrypted-token", active.Endpoints[0].Token)
}
