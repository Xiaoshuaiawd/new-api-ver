package prometheusmetrics

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
)

func TestClassifyErrorUsesFixedPrecedence(t *testing.T) {
	tests := []struct {
		name    string
		details ErrorDetails
		want    string
	}{
		{name: "success", details: ErrorDetails{}, want: ErrorTypeNone},
		{
			name:    "client cancellation wins over status",
			details: ErrorDetails{Err: fmt.Errorf("wrapped: %w", context.Canceled), StatusCode: http.StatusInternalServerError},
			want:    ErrorTypeClientCancelled,
		},
		{
			name:    "deadline wins over status",
			details: ErrorDetails{Err: fmt.Errorf("wrapped: %w", context.DeadlineExceeded), StatusCode: http.StatusInternalServerError},
			want:    ErrorTypeTimeout,
		},
		{
			name:    "channel response timeout code",
			details: ErrorDetails{ErrorCode: types.ErrorCodeChannelResponseTimeExceeded},
			want:    ErrorTypeTimeout,
		},
		{
			name:    "gateway timeout status",
			details: ErrorDetails{StatusCode: http.StatusGatewayTimeout},
			want:    ErrorTypeTimeout,
		},
		{
			name:    "rate limit",
			details: ErrorDetails{StatusCode: http.StatusTooManyRequests},
			want:    ErrorTypeRateLimit,
		},
		{
			name:    "quota code wins over generic forbidden status",
			details: ErrorDetails{ErrorCode: types.ErrorCodeInsufficientUserQuota, StatusCode: http.StatusForbidden},
			want:    ErrorTypeInsufficientQuota,
		},
		{
			name:    "pre-consume quota failure",
			details: ErrorDetails{ErrorCode: types.ErrorCodePreConsumeTokenQuotaFailed, StatusCode: http.StatusForbidden},
			want:    ErrorTypeInsufficientQuota,
		},
		{
			name:    "invalid channel key",
			details: ErrorDetails{ErrorCode: types.ErrorCodeChannelInvalidKey},
			want:    ErrorTypeAuthentication,
		},
		{
			name:    "authentication status",
			details: ErrorDetails{StatusCode: http.StatusUnauthorized},
			want:    ErrorTypeAuthentication,
		},
		{
			name:    "channel selection failed",
			details: ErrorDetails{ErrorCode: types.ErrorCodeGetChannelFailed},
			want:    ErrorTypeChannelUnavailable,
		},
		{
			name:    "channel has no key",
			details: ErrorDetails{ErrorCode: types.ErrorCodeChannelNoAvailableKey},
			want:    ErrorTypeChannelUnavailable,
		},
		{
			name:    "request conversion failed",
			details: ErrorDetails{ErrorCode: types.ErrorCodeConvertRequestFailed},
			want:    ErrorTypeInvalidRequest,
		},
		{
			name:    "unprocessable request",
			details: ErrorDetails{StatusCode: http.StatusUnprocessableEntity},
			want:    ErrorTypeInvalidRequest,
		},
		{
			name:    "transport code",
			details: ErrorDetails{ErrorCode: types.ErrorCodeDoRequestFailed},
			want:    ErrorTypeConnection,
		},
		{
			name:    "local relay initialization failure is internal",
			details: ErrorDetails{ErrorCode: types.ErrorCodeGenRelayInfoFailed, StatusCode: http.StatusInternalServerError},
			want:    ErrorTypeInternal,
		},
		{
			name: "network error",
			details: ErrorDetails{Err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: errors.New("connection refused"),
			}},
			want: ErrorTypeConnection,
		},
		{
			name:    "other upstream client status",
			details: ErrorDetails{StatusCode: http.StatusNotFound},
			want:    ErrorTypeUpstream4xx,
		},
		{
			name:    "upstream server status",
			details: ErrorDetails{StatusCode: http.StatusBadGateway},
			want:    ErrorTypeUpstream5xx,
		},
		{
			name:    "unknown error",
			details: ErrorDetails{Err: errors.New("unknown")},
			want:    ErrorTypeInternal,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, ClassifyError(test.details))
		})
	}
}

func TestClassifyNewAPIErrorReadsWrappedDetails(t *testing.T) {
	apiError := types.NewErrorWithStatusCode(
		context.DeadlineExceeded,
		types.ErrorCodeDoRequestFailed,
		http.StatusInternalServerError,
	)

	assert.Equal(t, ErrorTypeTimeout, ClassifyNewAPIError(apiError))
	assert.Equal(t, ErrorTypeNone, ClassifyNewAPIError(nil))
}

func TestClassifyErrorUsesProtocolTypeOnlyAsFallback(t *testing.T) {
	tests := []struct {
		name      string
		errorType types.ErrorType
		want      string
	}{
		{name: "local new-api error", errorType: types.ErrorTypeNewAPIError, want: ErrorTypeInternal},
		{name: "OpenAI upstream error", errorType: types.ErrorTypeOpenAIError, want: ErrorTypeUpstream5xx},
		{name: "Claude upstream error", errorType: types.ErrorTypeClaudeError, want: ErrorTypeUpstream5xx},
		{name: "Gemini upstream error", errorType: types.ErrorTypeGeminiError, want: ErrorTypeUpstream5xx},
		{name: "Midjourney upstream error", errorType: types.ErrorTypeMidjourneyError, want: ErrorTypeUpstream5xx},
		{name: "rerank upstream error", errorType: types.ErrorTypeRerankError, want: ErrorTypeUpstream5xx},
		{name: "generic upstream error", errorType: types.ErrorTypeUpstreamError, want: ErrorTypeUpstream5xx},
		{name: "unknown protocol type", errorType: types.ErrorType("dynamic-provider-error"), want: ErrorTypeInternal},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, ClassifyError(ErrorDetails{ErrorType: test.errorType}))
		})
	}

	assert.Equal(t, ErrorTypeAuthentication, ClassifyError(ErrorDetails{
		ErrorType:  types.ErrorTypeOpenAIError,
		StatusCode: http.StatusUnauthorized,
	}))
}

func TestClassifyErrorAlwaysReturnsDeclaredValue(t *testing.T) {
	declared := map[string]struct{}{
		ErrorTypeNone:               {},
		ErrorTypeClientCancelled:    {},
		ErrorTypeTimeout:            {},
		ErrorTypeConnection:         {},
		ErrorTypeRateLimit:          {},
		ErrorTypeAuthentication:     {},
		ErrorTypeInsufficientQuota:  {},
		ErrorTypeInvalidRequest:     {},
		ErrorTypeChannelUnavailable: {},
		ErrorTypeUpstream4xx:        {},
		ErrorTypeUpstream5xx:        {},
		ErrorTypeInternal:           {},
	}

	for status := 0; status <= 700; status++ {
		value := ClassifyError(ErrorDetails{Err: errors.New("test"), StatusCode: status})
		_, ok := declared[value]
		assert.True(t, ok, "status %d returned undeclared value %q", status, value)
	}
}
