package promptguard

import (
	"strings"
	"unicode/utf8"
)

// ExtractSnapshot builds the text to evaluate from a request's combined text.
// When latestTurnOnly is true, only the last non-empty user message (plus the
// preceding assistant/tool turn) is sent — never the full history.
//
// The input text is already combined by dto.Request.GetTokenCountMeta().CombineText,
// so we receive a flat string of the joined messages. For latest-turn-only mode
// we re-derive from the raw messages slice passed by the caller.
//
// combineText: the already-joined full text (used when latestTurnOnly=false)
// userMessages / assistantMessages: alternating turns (used when latestTurnOnly=true)
// inputLimit: maximum Unicode characters to include in ScanText
func BuildSnapshot(
	combineText string,
	latestUserTurn string,
	latestAssistantTurn string,
	latestTurnOnly bool,
	inputLimit int,
	requestID string,
	tokenGroup string,
	modelName string,
) Snapshot {
	if inputLimit <= 0 {
		inputLimit = DefaultInputLimit
	}

	var raw string
	if latestTurnOnly {
		// include previous assistant output first (context), then the new user input
		if latestAssistantTurn != "" {
			raw = latestAssistantTurn + "\n" + latestUserTurn
		} else {
			raw = latestUserTurn
		}
	} else {
		raw = combineText
	}

	raw = strings.TrimSpace(raw)
	originalLen := utf8.RuneCountInString(raw)

	scanText := truncateRunes(raw, inputLimit)

	return Snapshot{
		ScanText:     scanText,
		PromptLength: originalLen,
		RequestID:    requestID,
		TokenGroup:   tokenGroup,
		ModelName:    modelName,
	}
}

// truncateRunes truncates s to at most maxRunes Unicode code points.
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count >= maxRunes {
			return s[:i]
		}
		count++
	}
	return s
}
