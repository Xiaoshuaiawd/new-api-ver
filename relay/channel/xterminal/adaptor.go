package xterminal

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

// Adaptor relays OpenAI chat completions to XTerminal. Credentials are not
// stored on the channel: every request borrows a token from the channel's
// in-memory pool and a rejected token is retired immediately.
type Adaptor struct {
	openai.Adaptor
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	baseURL := strings.TrimSuffix(strings.TrimSpace(info.ChannelBaseUrl), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	requestPath := info.RequestURLPath
	if requestPath == "" {
		requestPath = ChatCompletionsPath
	}
	return baseURL + requestPath, nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	// The minted token is a bearer credential; the upstream rejects requests
	// that carry no Authorization header.
	req.Set("Authorization", "Bearer "+info.ApiKey)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	return request, nil
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return nil, errors.New("endpoint not supported")
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("endpoint not supported")
}

// DoRequest replays the buffered request body with pooled tokens until the
// upstream accepts it. A token that the upstream rejects is dropped from the
// pool and replaced, so a dead token costs one retry instead of failing the
// client request.
func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	body, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, fmt.Errorf("read request body failed: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= maxTokenAttempts; attempt++ {
		token, err := TakeToken(info.ChannelId)
		if err != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, err
		}
		info.ApiKey = token

		resp, err := channel.DoApiRequest(a, c, info, bytes.NewReader(body))
		if err != nil {
			DiscardToken(info.ChannelId, token)
			lastErr = err
			logger.LogWarn(c, "xterminal: upstream request failed (attempt %d/%d): %s", attempt, maxTokenAttempts, err.Error())
			continue
		}

		if !tokenRejected(resp.StatusCode) {
			return resp, nil
		}
		DiscardToken(info.ChannelId, token)
		if attempt == maxTokenAttempts {
			// Hand the upstream reply back so the client sees the real error
			// instead of a synthetic one.
			return resp, nil
		}
		logger.LogWarn(c, "xterminal: token rejected with status %d, retrying with a new token (attempt %d/%d)", resp.StatusCode, attempt, maxTokenAttempts)
		drainAndClose(resp.Body)
	}

	return nil, lastErr
}

// tokenRejected reports whether an upstream status means the borrowed token is
// no longer usable. The upstream answers a dead token with 401/403; other
// failures (a 502 for an unsupported model, a 400 for a malformed request) say
// nothing about the token, so they are returned without burning another one.
func tokenRejected(statusCode int) bool {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	}
	return false
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1<<16))
	_ = body.Close()
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
