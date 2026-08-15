package error_model_redaction

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const OptionKey = "ErrorModelRedactionPatterns"

type compiledPattern struct {
	source string
	regex  *regexp.Regexp
}

var state = struct {
	sync.RWMutex
	patterns []compiledPattern
}{}

func parsePatternsJSON(value string) ([]compiledPattern, error) {
	var sources []string
	if err := common.UnmarshalJsonStr(value, &sources); err != nil {
		return nil, fmt.Errorf("error model redaction patterns must be a JSON string array: %w", err)
	}

	seen := make(map[string]struct{}, len(sources))
	patterns := make([]compiledPattern, 0, len(sources))
	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		if _, exists := seen[source]; exists {
			continue
		}
		compiled, err := regexp.Compile("^(?:" + source + ")$")
		if err != nil {
			return nil, fmt.Errorf("invalid error model redaction pattern %q: %w", source, err)
		}
		seen[source] = struct{}{}
		patterns = append(patterns, compiledPattern{source: source, regex: compiled})
	}
	return patterns, nil
}

func ValidatePatternsJSON(value string) error {
	_, err := parsePatternsJSON(value)
	return err
}

func UpdatePatternsJSON(value string) error {
	patterns, err := parsePatternsJSON(value)
	if err != nil {
		return err
	}
	state.Lock()
	state.patterns = patterns
	state.Unlock()
	return nil
}

func PatternsJSON() string {
	state.RLock()
	sources := make([]string, len(state.patterns))
	for i, pattern := range state.patterns {
		sources[i] = pattern.source
	}
	state.RUnlock()

	data, err := common.Marshal(sources)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func Redact(message string, modelNames ...string) string {
	if message == "" || len(modelNames) == 0 {
		return message
	}

	state.RLock()
	patterns := append([]compiledPattern(nil), state.patterns...)
	state.RUnlock()
	if len(patterns) == 0 {
		return message
	}

	seen := make(map[string]struct{}, len(modelNames))
	matchedModels := make([]string, 0, len(modelNames))
	for _, modelName := range modelNames {
		if modelName == "" {
			continue
		}
		if _, exists := seen[modelName]; exists {
			continue
		}
		seen[modelName] = struct{}{}
		for _, pattern := range patterns {
			if pattern.regex.MatchString(modelName) {
				matchedModels = append(matchedModels, modelName)
				break
			}
		}
	}

	sort.Slice(matchedModels, func(i, j int) bool {
		return len(matchedModels[i]) > len(matchedModels[j])
	})
	for _, modelName := range matchedModels {
		message = strings.ReplaceAll(message, modelName, "****")
	}
	return message
}
