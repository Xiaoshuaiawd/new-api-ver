package promptguard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// scanRequest is the body sent to the guard service.
//
// qwen3guard is a specialised safety model: it takes the raw user content as a
// single user message (no system prompt) and returns its own fixed line-based
// format ("Safety: ...\nCategories: ...") rather than JSON. We must not inject a
// system prompt asking for JSON — the model ignores it and returns native output.
type scanRequest struct {
	Model       string        `json:"model"`
	Messages    []scanMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	Seed        int           `json:"seed"`
}

type scanMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// scanResponse is a minimal subset of the OpenAI chat completion response.
type scanResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// callGuardEndpoint calls a single guard endpoint and returns the raw response.
// It returns a retryable error for network / 5xx / 429 failures and a
// non-retryable error for invalid JSON / unexpected content.
func callGuardEndpoint(ctx context.Context, client *http.Client, ep Endpoint, text string) (*guardResponse, error) {
	model := ep.Model
	if model == "" {
		model = "default"
	}

	body := scanRequest{
		Model: model,
		Messages: []scanMessage{
			{Role: "user", Content: text},
		},
		Stream:      false,
		Temperature: 0,
		MaxTokens:   64,
		Seed:        42,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: false, Cause: err}
	}

	baseURL := strings.TrimRight(ep.BaseURL, "/")
	url := baseURL + "/v1/chat/completions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: false, Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")
	if ep.Token != "" {
		req.Header.Set("Authorization", "Bearer "+ep.Token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: true, Timeout: isTimeoutError(err), Cause: err}
	}
	defer resp.Body.Close()

	// 429 / 5xx are retryable
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: true}
	}
	// 4xx other than 429 are non-retryable (misconfiguration)
	if resp.StatusCode >= 400 {
		return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: false, Cause: fmt.Errorf("guard returned %d", resp.StatusCode)}
	}

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: true, Cause: err}
	}

	return parseGuardResponse(rawBody)
}

// parseGuardResponse extracts the OpenAI envelope then parses qwen3guard's
// native line-based format:
//
//	Safety: Safe|Controversial|Unsafe
//	Categories: Violent, Jailbreak, ...
//
// Any other/unknown safety value, missing Categories line, duplicate fields or
// truncated output is treated as an invalid response.
func parseGuardResponse(raw []byte) (*guardResponse, error) {
	var chatResp scanResponse
	if err := json.Unmarshal(raw, &chatResp); err != nil {
		return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: false, Cause: fmt.Errorf("parse guard chat response: %w", err)}
	}
	if len(chatResp.Choices) == 0 {
		return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: false, Cause: fmt.Errorf("empty choices in guard response")}
	}
	choice := chatResp.Choices[0]
	if choice.FinishReason != "" && choice.FinishReason != "stop" {
		return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: false, Cause: fmt.Errorf("unexpected finish_reason: %s", choice.FinishReason)}
	}

	return parseQwen3GuardContent(choice.Message.Content)
}

// parseQwen3GuardContent parses the model's line-based output into guardResponse.
func parseQwen3GuardContent(content string) (*guardResponse, error) {
	var safety string
	var categoryLine string
	categoriesSeen := false

	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "safety:"):
			if safety != "" {
				return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: false, Cause: fmt.Errorf("duplicate safety line")}
			}
			safety = strings.TrimSpace(line[len("safety:"):])
		case strings.HasPrefix(lower, "categories:"):
			if categoriesSeen {
				return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: false, Cause: fmt.Errorf("duplicate categories line")}
			}
			categoriesSeen = true
			categoryLine = strings.TrimSpace(line[len("categories:"):])
		default:
			// Auxiliary fields (e.g. Refusal) are ignored.
		}
	}

	switch strings.ToLower(safety) {
	case "safe":
		safety = "Safe"
	case "controversial":
		safety = "Controversial"
	case "unsafe":
		safety = "Unsafe"
	default:
		return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: false, Cause: fmt.Errorf("unknown safety value: %q", safety)}
	}
	if !categoriesSeen {
		return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: false, Cause: fmt.Errorf("missing categories line")}
	}

	cats := make([]string, 0)
	for _, raw := range strings.Split(categoryLine, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.EqualFold(raw, "none") || strings.EqualFold(raw, "n/a") {
			continue
		}
		cats = append(cats, raw)
	}

	return &guardResponse{Safety: safety, Categories: cats}, nil
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if netErr, ok := err.(interface{ Timeout() bool }); ok {
		return netErr.Timeout()
	}
	return false
}

// newHTTPClient builds an http.Client suitable for guard calls.
func newHTTPClient(timeoutMS int) *http.Client {
	if timeoutMS <= 0 {
		timeoutMS = DefaultTimeoutMS
	}
	return &http.Client{
		Timeout: time.Duration(timeoutMS)*time.Millisecond + 2*time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 8,
		},
	}
}

// GuardError is the error type returned by guard operations.
type GuardError struct {
	Code      string
	Retryable bool
	Timeout   bool
	Cause     error
}

func (e *GuardError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("prompt_guard [%s]: %v", e.Code, e.Cause)
	}
	return fmt.Sprintf("prompt_guard [%s]", e.Code)
}

func (e *GuardError) Unwrap() error { return e.Cause }
