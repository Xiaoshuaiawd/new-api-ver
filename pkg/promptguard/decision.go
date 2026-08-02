package promptguard

import "strings"

// applyDecisionMatrix converts a raw guard response into an actionable Decision
// following the rules in the design doc:
//
//	Safe                                                   → Allow
//	Controversial (no Jailbreak/PII/Suicide)               → Flag (allowed)
//	Controversial + Jailbreak|PII|Suicide&Self-Harm        → Block
//	Unsafe, hits ≥1 enabled scanner                        → Block
//	Unsafe, all hits are known but no enabled scanner      → Flag
//	Unsafe, no valid known categories                      → Block (conservative)
func applyDecisionMatrix(resp *guardResponse, enabledScanners []string) *Decision {
	enabledSet := make(map[string]struct{}, len(enabledScanners))
	for _, s := range enabledScanners {
		enabledSet[normalizeCategory(s)] = struct{}{}
	}

	validCats := validCategories(resp.Categories)

	switch resp.Safety {
	case "Safe":
		return &Decision{Kind: DecisionAllow, Categories: validCats}

	case "Controversial":
		// Always block on these specific categories regardless of admin toggle
		for _, cat := range validCats {
			norm := normalizeCategory(cat)
			if norm == "jailbreak" || norm == "pii" || norm == "suicide_and_self_harm" {
				return &Decision{Kind: DecisionBlock, ErrorCode: ErrorCodeBlocked, Categories: validCats}
			}
		}
		return &Decision{Kind: DecisionFlag, Categories: validCats}

	case "Unsafe":
		if len(validCats) == 0 {
			// No valid categories → conservative block
			return &Decision{Kind: DecisionBlock, ErrorCode: ErrorCodeBlocked}
		}
		for _, cat := range validCats {
			if _, ok := enabledSet[normalizeCategory(cat)]; ok {
				return &Decision{Kind: DecisionBlock, ErrorCode: ErrorCodeBlocked, Categories: validCats}
			}
		}
		// All hits are known categories but none are enabled → flag
		return &Decision{Kind: DecisionFlag, Categories: validCats}

	default:
		// Unknown safety value should have been caught during parsing, but be safe
		return &Decision{Kind: DecisionUnavailable, ErrorCode: ErrorCodeUnavailable}
	}
}

// validCategories filters the response categories to only known values.
func validCategories(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, cat := range raw {
		norm := normalizeCategory(cat)
		if !KnownScannerID(norm) {
			continue
		}
		if _, dup := seen[norm]; dup {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, cat)
	}
	return out
}

// normalizeCategory converts a human-readable category label to its scanner ID.
var categoryToID = map[string]string{
	"violent":                           "violent",
	"non-violent illegal acts":           "non_violent_illegal_acts",
	"non_violent_illegal_acts":           "non_violent_illegal_acts",
	"sexual content or sexual acts":     "sexual_content_or_sexual_acts",
	"sexual_content_or_sexual_acts":      "sexual_content_or_sexual_acts",
	"pii":                               "pii",
	"suicide & self-harm":               "suicide_and_self_harm",
	"suicide_and_self_harm":             "suicide_and_self_harm",
	"unethical acts":                    "unethical_acts",
	"unethical_acts":                    "unethical_acts",
	"politically sensitive topics":      "politically_sensitive_topics",
	"politically_sensitive_topics":      "politically_sensitive_topics",
	"copyright violation":               "copyright_violation",
	"copyright_violation":               "copyright_violation",
	"jailbreak":                         "jailbreak",
}

func normalizeCategory(cat string) string {
	key := strings.ToLower(strings.TrimSpace(cat))
	if id, ok := categoryToID[key]; ok {
		return id
	}
	// already a known ID?
	if KnownScannerID(key) {
		return key
	}
	return key
}
