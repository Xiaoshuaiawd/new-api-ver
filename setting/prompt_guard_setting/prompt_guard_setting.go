// Package prompt_guard_setting holds the runtime variables for Prompt Guard,
// loaded from the option table via model/option.go updateOptionMap.
package prompt_guard_setting

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/pkg/promptguard"
)

var mu sync.RWMutex

// StorageConfig is the JSON structure persisted in the option table.
type StorageConfig struct {
	Enabled         bool              `json:"enabled"`
	BlockingEnabled bool              `json:"blocking_enabled"`
	LatestTurnOnly  bool              `json:"latest_turn_only"`
	StorePassEvents bool              `json:"store_pass_events"`
	Scanners        []string          `json:"scanners"`
	AllGroups       bool              `json:"all_groups"`
	GroupNames      []string          `json:"group_names"`
	Endpoints       []StorageEndpoint `json:"endpoints"`
	SystemPrompt    string            `json:"system_prompt"`
	MaxConcurrency  int               `json:"max_concurrency"`
	ConfigVersion   int64             `json:"config_version"`
}

type StorageEndpoint struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	Format      string `json:"format"` // "qwen3guard" (default) or "json"
	TokenCipher string `json:"token_cipher,omitempty"`
	TimeoutMS   int    `json:"timeout_ms"`
	InputLimit  int    `json:"input_limit"`
	Enabled     bool   `json:"enabled"`
}

var current StorageConfig

// Default returns the default storage config (disabled).
func Default() StorageConfig {
	return StorageConfig{
		Enabled:         false,
		BlockingEnabled: false,
		LatestTurnOnly:  true,
		StorePassEvents: false,
		Scanners:        append([]string(nil), promptguard.AllScannerIDs...),
		AllGroups:       false,
		GroupNames:      []string{},
		Endpoints:       []StorageEndpoint{},
		ConfigVersion:   1,
	}
}

// LoadFromJSON parses raw JSON into the in-memory config and applies it.
func LoadFromJSON(raw string, decrypt func(string) (string, error)) {
	mu.Lock()
	defer mu.Unlock()

	cfg := Default()
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	current = cfg
	applyLocked(decrypt)
}

// GetActive returns the current runtime config snapshot.
func GetActive(decrypt func(string) (string, error)) promptguard.Config {
	mu.RLock()
	defer mu.RUnlock()
	return buildActive(current, decrypt)
}

// GetStorageJSON serialises the current config to JSON for persistence.
func GetStorageJSON() (string, error) {
	mu.RLock()
	defer mu.RUnlock()
	b, err := json.Marshal(current)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Update atomically replaces the stored config and rebuilds the active snapshot.
func Update(cfg StorageConfig, decrypt func(string) (string, error)) {
	mu.Lock()
	defer mu.Unlock()
	current = cfg
	applyLocked(decrypt)
}

// GetPublic returns the config as safe-to-return JSON for the admin API
// (token ciphertexts replaced by has_token boolean).
func GetPublic() PublicConfig {
	mu.RLock()
	defer mu.RUnlock()
	return toPublic(current)
}

// applyLocked rebuilds the runtime snapshot. Caller must hold mu.
func applyLocked(decrypt func(string) (string, error)) {
	// Invalidate cached HTTP clients on config change.
	promptguard.InvalidateAllClients()
}

func buildActive(cfg StorageConfig, decrypt func(string) (string, error)) promptguard.Config {
	eps := make([]promptguard.Endpoint, 0, len(cfg.Endpoints))
	for _, ep := range cfg.Endpoints {
		// Tokens are stored as plaintext (same pattern as SMTPToken/StripeApiSecret),
		// so when no decryptor is provided the stored value IS the token. Only run
		// decrypt when a decryptor is supplied and the value is ciphertext.
		token := ep.TokenCipher
		if decrypt != nil && ep.TokenCipher != "" {
			if pt, err := decrypt(ep.TokenCipher); err == nil {
				token = pt
			}
		}
		timeoutMS := ep.TimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = promptguard.DefaultTimeoutMS
		}
		inputLimit := ep.InputLimit
		if inputLimit <= 0 {
			inputLimit = promptguard.DefaultInputLimit
		}
		format := ep.Format
		if format != promptguard.FormatJSON {
			format = promptguard.FormatQwen3Guard
		}
		eps = append(eps, promptguard.Endpoint{
			ID:         ep.ID,
			Name:       ep.Name,
			BaseURL:    ep.BaseURL,
			Model:      ep.Model,
			Token:      token,
			Format:     format,
			TimeoutMS:  timeoutMS,
			InputLimit: inputLimit,
			Enabled:    ep.Enabled,
		})
	}
	scanners := cfg.Scanners
	if len(scanners) == 0 {
		scanners = append([]string(nil), promptguard.AllScannerIDs...)
	}
	maxConcurrency := cfg.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = promptguard.DefaultMaxConcurrency
	}
	return promptguard.Config{
		Enabled:         cfg.Enabled,
		BlockingEnabled: cfg.BlockingEnabled,
		LatestTurnOnly:  cfg.LatestTurnOnly,
		StorePassEvents: cfg.StorePassEvents,
		Scanners:        scanners,
		AllGroups:       cfg.AllGroups,
		GroupNames:      cfg.GroupNames,
		Endpoints:       eps,
		SystemPrompt:    cfg.SystemPrompt,
		MaxConcurrency:  maxConcurrency,
		ConfigVersion:   cfg.ConfigVersion,
	}
}

// ---- Public (admin-facing) types -------------------------------------------

type PublicConfig struct {
	Enabled         bool             `json:"enabled"`
	BlockingEnabled bool             `json:"blocking_enabled"`
	LatestTurnOnly  bool             `json:"latest_turn_only"`
	StorePassEvents bool             `json:"store_pass_events"`
	Scanners        []string         `json:"scanners"`
	AllGroups       bool             `json:"all_groups"`
	GroupNames      []string         `json:"group_names"`
	Endpoints       []PublicEndpoint `json:"endpoints"`
	SystemPrompt    string           `json:"system_prompt"`
	MaxConcurrency  int              `json:"max_concurrency"`
	ConfigVersion   int64            `json:"config_version"`
}

type PublicEndpoint struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	BaseURL    string `json:"base_url"`
	Model      string `json:"model"`
	Format     string `json:"format"`
	HasToken   bool   `json:"has_token"`
	TimeoutMS  int    `json:"timeout_ms"`
	InputLimit int    `json:"input_limit"`
	Enabled    bool   `json:"enabled"`
}

func toPublic(cfg StorageConfig) PublicConfig {
	eps := make([]PublicEndpoint, 0, len(cfg.Endpoints))
	for _, ep := range cfg.Endpoints {
		format := ep.Format
		if format != promptguard.FormatJSON {
			format = promptguard.FormatQwen3Guard
		}
		eps = append(eps, PublicEndpoint{
			ID:         ep.ID,
			Name:       ep.Name,
			BaseURL:    ep.BaseURL,
			Model:      ep.Model,
			Format:     format,
			HasToken:   ep.TokenCipher != "",
			TimeoutMS:  ep.TimeoutMS,
			InputLimit: ep.InputLimit,
			Enabled:    ep.Enabled,
		})
	}
	groups := cfg.GroupNames
	if groups == nil {
		groups = []string{}
	}
	scanners := cfg.Scanners
	if scanners == nil {
		scanners = []string{}
	}
	return PublicConfig{
		Enabled:         cfg.Enabled,
		BlockingEnabled: cfg.BlockingEnabled,
		LatestTurnOnly:  cfg.LatestTurnOnly,
		StorePassEvents: cfg.StorePassEvents,
		Scanners:        scanners,
		AllGroups:       cfg.AllGroups,
		GroupNames:      groups,
		Endpoints:       eps,
		SystemPrompt:    cfg.SystemPrompt,
		MaxConcurrency:  cfg.MaxConcurrency,
		ConfigVersion:   cfg.ConfigVersion,
	}
}

// UpdateRequest is the body sent by the admin frontend.
type UpdateRequest struct {
	ExpectedVersion int64            `json:"expected_version"`
	Enabled         bool             `json:"enabled"`
	BlockingEnabled bool             `json:"blocking_enabled"`
	LatestTurnOnly  bool             `json:"latest_turn_only"`
	StorePassEvents bool             `json:"store_pass_events"`
	Scanners        []string         `json:"scanners"`
	AllGroups       bool             `json:"all_groups"`
	GroupNames      []string         `json:"group_names"`
	Endpoints       []UpdateEndpoint `json:"endpoints"`
	SystemPrompt    string           `json:"system_prompt"`
	MaxConcurrency  int              `json:"max_concurrency"`
}

type UpdateEndpoint struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	BaseURL    string `json:"base_url"`
	Model      string `json:"model"`
	Format     string `json:"format"`
	Token      string `json:"token,omitempty"`
	ClearToken bool   `json:"clear_token"`
	TimeoutMS  int    `json:"timeout_ms"`
	InputLimit int    `json:"input_limit"`
	Enabled    bool   `json:"enabled"`
}
