// Package juice_fixer_setting stores the administrator-controlled Juice response rules.
package juice_fixer_setting

import (
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const MaxValue = int(^uint32(0) >> 1)

type Rule struct {
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
	Value           int    `json:"value"`
}

type StorageConfig struct {
	Enabled bool   `json:"enabled"`
	Rules   []Rule `json:"rules"`
}

type PublicConfig = StorageConfig

var (
	mu      sync.RWMutex
	current = Default()
)

func Default() StorageConfig {
	return StorageConfig{Enabled: false, Rules: []Rule{}}
}

func Validate(cfg StorageConfig) error {
	seen := make(map[string]struct{}, len(cfg.Rules))
	for i, rule := range cfg.Rules {
		model := strings.TrimSpace(rule.Model)
		if model == "" {
			return fmt.Errorf("rules[%d].model is required", i)
		}
		if rule.Value < 0 || rule.Value > MaxValue {
			return fmt.Errorf("rules[%d].value must be between 0 and %d", i, MaxValue)
		}
		key := model + "\x00" + strings.TrimSpace(rule.ReasoningEffort)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate Juice rule for model %q and reasoning_effort %q", model, strings.TrimSpace(rule.ReasoningEffort))
		}
		seen[key] = struct{}{}
	}
	return nil
}

func Normalize(cfg StorageConfig) (StorageConfig, error) {
	if err := Validate(cfg); err != nil {
		return StorageConfig{}, err
	}
	normalized := StorageConfig{Enabled: cfg.Enabled, Rules: make([]Rule, len(cfg.Rules))}
	for i, rule := range cfg.Rules {
		normalized.Rules[i] = Rule{
			Model:           strings.TrimSpace(rule.Model),
			ReasoningEffort: strings.TrimSpace(rule.ReasoningEffort),
			Value:           rule.Value,
		}
	}
	return normalized, nil
}

func LoadFromJSON(raw string) {
	cfg := Default()
	if strings.TrimSpace(raw) != "" {
		if err := common.Unmarshal([]byte(raw), &cfg); err != nil {
			return
		}
	}
	normalized, err := Normalize(cfg)
	if err != nil {
		return
	}
	mu.Lock()
	current = normalized
	mu.Unlock()
}

func GetPublic() PublicConfig {
	mu.RLock()
	defer mu.RUnlock()
	return clone(current)
}

func GetStorageJSON() (string, error) {
	mu.RLock()
	defer mu.RUnlock()
	b, err := common.Marshal(current)
	return string(b), err
}

func Update(cfg StorageConfig) error {
	normalized, err := Normalize(cfg)
	if err != nil {
		return err
	}
	mu.Lock()
	current = normalized
	mu.Unlock()
	return nil
}

func Find(model, reasoningEffort string) (int, bool) {
	mu.RLock()
	defer mu.RUnlock()
	if !current.Enabled {
		return 0, false
	}
	model = strings.TrimSpace(model)
	reasoningEffort = strings.TrimSpace(reasoningEffort)
	for _, rule := range current.Rules {
		if rule.Model == model && rule.ReasoningEffort == reasoningEffort {
			return rule.Value, true
		}
	}
	return 0, false
}

func clone(cfg StorageConfig) StorageConfig {
	copyCfg := StorageConfig{Enabled: cfg.Enabled, Rules: make([]Rule, len(cfg.Rules))}
	copy(copyCfg.Rules, cfg.Rules)
	return copyCfg
}
