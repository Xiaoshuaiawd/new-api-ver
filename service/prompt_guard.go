package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/promptguard"
	prompt_guard_setting "github.com/QuantumNous/new-api/setting/prompt_guard_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// CheckPromptGuard runs the Prompt Guard check before channel selection,
// pre-consume billing and upstream dispatch. It MUST be called after
// request parsing and before any account/quota side effects.
//
// Returns nil when the request may proceed, or a *types.NewAPIError with:
//   - HTTP 403 + code "prompt_guard_blocked"   on block decision
//   - HTTP 503 + code "prompt_guard_unavailable" on fail-closed unavailability
//
// The function never touches users.status, api_keys.status, quota, auto-ban
// or any violation counter.
func CheckPromptGuard(c *gin.Context, tokenGroup, requestID, modelName string, request dto.Request) *types.NewAPIError {
	cfg := prompt_guard_setting.GetActive(nil)

	if !cfg.Enabled {
		return nil
	}

	if !cfg.IncludesGroup(tokenGroup) {
		return nil
	}

	// Pick input limit from first enabled endpoint
	inputLimit := promptguard.DefaultInputLimit
	eps := cfg.EnabledEndpoints()
	if len(eps) > 0 && eps[0].InputLimit > 0 {
		inputLimit = eps[0].InputLimit
	}

	latestUser, latestAssistant, combineText := extractPromptText(request)
	snap := promptguard.BuildSnapshot(
		combineText,
		latestUser,
		latestAssistant,
		cfg.LatestTurnOnly,
		inputLimit,
		requestID,
		tokenGroup,
		modelName,
	)

	if strings.TrimSpace(snap.ScanText) == "" {
		return nil
	}

	// Total budget must accommodate the configured per-node timeout (so raising
	// it in the UI actually takes effect) plus a margin for queueing when the
	// concurrency limit is momentarily saturated. Derive it from the largest
	// enabled node timeout rather than a fixed cap.
	nodeTimeoutMS := promptguard.DefaultTimeoutMS
	for _, ep := range eps {
		if ep.TimeoutMS > nodeTimeoutMS {
			nodeTimeoutMS = ep.TimeoutMS
		}
	}
	const queueWaitMarginMS = 3000
	totalMS := nodeTimeoutMS + queueWaitMarginMS
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(totalMS)*time.Millisecond)
	defer cancel()

	decision, err := promptguard.Evaluate(ctx, cfg, snap)
	if err != nil {
		logger.LogWarn(c, "prompt_guard unavailable: "+err.Error())
		return types.NewErrorWithStatusCode(
			fmt.Errorf("prompt guard service temporarily unavailable"),
			types.ErrorCode("prompt_guard_unavailable"),
			http.StatusServiceUnavailable,
			types.ErrOptionWithSkipRetry(),
		)
	}

	switch decision.Kind {
	case promptguard.DecisionBlock:
		logger.LogWarn(c, "prompt_guard blocked request")
		return types.NewErrorWithStatusCode(
			fmt.Errorf("Request blocked by input safety policy."),
			types.ErrorCode("prompt_guard_blocked"),
			http.StatusForbidden,
			types.ErrOptionWithSkipRetry(),
		)
	case promptguard.DecisionUnavailable:
		logger.LogWarn(c, "prompt_guard unavailable (decision)")
		return types.NewErrorWithStatusCode(
			fmt.Errorf("prompt guard service temporarily unavailable"),
			types.ErrorCode("prompt_guard_unavailable"),
			http.StatusServiceUnavailable,
			types.ErrOptionWithSkipRetry(),
		)
	default:
		return nil
	}
}

// extractPromptText returns (latestUserTurn, latestAssistantTurn, fullCombineText).
func extractPromptText(request dto.Request) (string, string, string) {
	if request == nil {
		return "", "", ""
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		return extractOpenAITurns(r)
	case *dto.ClaudeRequest:
		return extractClaudeTurns(r)
	case *dto.GeminiChatRequest:
		return extractGeminiTurns(r)
	default:
		meta := request.GetTokenCountMeta()
		if meta != nil {
			return meta.CombineText, "", meta.CombineText
		}
		return "", "", ""
	}
}

func extractOpenAITurns(r *dto.GeneralOpenAIRequest) (latestUser, latestAssistant, combine string) {
	var sb strings.Builder
	latestUserIdx := -1
	latestAssistantIdx := -1

	for i, msg := range r.Messages {
		text := openAIMessageText(msg)
		if text == "" {
			continue
		}
		sb.WriteString(text)
		sb.WriteString("\n")
		switch strings.ToLower(msg.Role) {
		case "user":
			latestUserIdx = i
		case "assistant":
			latestAssistantIdx = i
		}
	}
	combine = sb.String()
	if latestUserIdx >= 0 {
		latestUser = openAIMessageText(r.Messages[latestUserIdx])
	}
	// Only include assistant turn if it preceded the latest user turn
	if latestAssistantIdx >= 0 && latestAssistantIdx < latestUserIdx {
		latestAssistant = openAIMessageText(r.Messages[latestAssistantIdx])
	}
	return
}

func openAIMessageText(msg dto.Message) string {
	if s, ok := msg.Content.(string); ok {
		return s
	}
	var sb strings.Builder
	for _, part := range msg.ParseContent() {
		if part.Type == "text" {
			sb.WriteString(part.Text)
		}
	}
	return sb.String()
}

func extractClaudeTurns(r *dto.ClaudeRequest) (latestUser, latestAssistant, combine string) {
	var sb strings.Builder
	var lastAssistant string
	for _, msg := range r.Messages {
		text := msg.GetStringContent()
		if text == "" {
			continue
		}
		sb.WriteString(text)
		sb.WriteString("\n")
		switch strings.ToLower(msg.Role) {
		case "user":
			latestAssistant = lastAssistant
			latestUser = text
		case "assistant":
			lastAssistant = text
		}
	}
	combine = sb.String()
	return
}

func extractGeminiTurns(r *dto.GeminiChatRequest) (latestUser, latestAssistant, combine string) {
	var sb strings.Builder
	var lastModel string
	for _, part := range r.Contents {
		text := geminiContentText(part)
		if text == "" {
			continue
		}
		sb.WriteString(text)
		sb.WriteString("\n")
		switch strings.ToLower(part.Role) {
		case "user":
			latestAssistant = lastModel
			latestUser = text
		case "model":
			lastModel = text
		}
	}
	combine = sb.String()
	return
}

func geminiContentText(content dto.GeminiChatContent) string {
	var sb strings.Builder
	for _, p := range content.Parts {
		if p.Text != "" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}
