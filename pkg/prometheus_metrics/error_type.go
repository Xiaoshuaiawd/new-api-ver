package prometheusmetrics

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/QuantumNous/new-api/types"
)

const (
	ErrorTypeNone               = "none"
	ErrorTypeClientCancelled    = "client_cancelled"
	ErrorTypeTimeout            = "timeout"
	ErrorTypeConnection         = "connection"
	ErrorTypeRateLimit          = "rate_limit"
	ErrorTypeAuthentication     = "authentication"
	ErrorTypeInsufficientQuota  = "insufficient_quota"
	ErrorTypeInvalidRequest     = "invalid_request"
	ErrorTypeChannelUnavailable = "channel_unavailable"
	ErrorTypeUpstream4xx        = "upstream_4xx"
	ErrorTypeUpstream5xx        = "upstream_5xx"
	ErrorTypeInternal           = "internal"
)

type ErrorDetails struct {
	Err        error
	ErrorType  types.ErrorType
	ErrorCode  types.ErrorCode
	StatusCode int
}

func ClassifyNewAPIError(err *types.NewAPIError) string {
	if err == nil {
		return ErrorTypeNone
	}
	return ClassifyError(ErrorDetails{
		Err:        err,
		ErrorType:  err.GetErrorType(),
		ErrorCode:  err.GetErrorCode(),
		StatusCode: err.StatusCode,
	})
}

func ClassifyError(details ErrorDetails) string {
	if errors.Is(details.Err, context.Canceled) {
		return ErrorTypeClientCancelled
	}

	var networkError net.Error
	isNetworkError := errors.As(details.Err, &networkError)
	if errors.Is(details.Err, context.DeadlineExceeded) ||
		details.ErrorCode == types.ErrorCodeChannelResponseTimeExceeded ||
		details.StatusCode == http.StatusRequestTimeout ||
		details.StatusCode == http.StatusGatewayTimeout ||
		details.StatusCode == 524 ||
		(isNetworkError && networkError.Timeout()) {
		return ErrorTypeTimeout
	}

	switch details.ErrorCode {
	case types.ErrorCodeInsufficientUserQuota, types.ErrorCodePreConsumeTokenQuotaFailed:
		return ErrorTypeInsufficientQuota
	}

	if details.StatusCode == http.StatusTooManyRequests {
		return ErrorTypeRateLimit
	}

	if details.ErrorCode == types.ErrorCodeChannelInvalidKey ||
		details.ErrorCode == types.ErrorCodeAccessDenied ||
		details.StatusCode == http.StatusUnauthorized ||
		details.StatusCode == http.StatusForbidden {
		return ErrorTypeAuthentication
	}

	switch details.ErrorCode {
	case types.ErrorCodeGetChannelFailed, types.ErrorCodeChannelNoAvailableKey:
		return ErrorTypeChannelUnavailable
	}

	switch details.ErrorCode {
	case types.ErrorCodeInvalidRequest,
		types.ErrorCodeReadRequestBodyFailed,
		types.ErrorCodeConvertRequestFailed,
		types.ErrorCodeBadRequestBody,
		types.ErrorCodeChannelParamOverrideInvalid,
		types.ErrorCodeChannelHeaderOverrideInvalid:
		return ErrorTypeInvalidRequest
	}
	if details.StatusCode == http.StatusBadRequest ||
		details.StatusCode == http.StatusUnprocessableEntity {
		return ErrorTypeInvalidRequest
	}

	switch details.ErrorCode {
	case types.ErrorCodeDoRequestFailed,
		types.ErrorCodeReadResponseBodyFailed,
		types.ErrorCodeChannelAwsClientError,
		types.ErrorCodeAwsInvokeError:
		return ErrorTypeConnection
	}
	if isNetworkError {
		return ErrorTypeConnection
	}

	switch details.ErrorCode {
	case types.ErrorCodeCountTokenFailed,
		types.ErrorCodeModelPriceError,
		types.ErrorCodeJsonMarshalFailed,
		types.ErrorCodeGenRelayInfoFailed,
		types.ErrorCodeQueryDataError,
		types.ErrorCodeUpdateDataError:
		return ErrorTypeInternal
	}

	if details.StatusCode >= 400 && details.StatusCode < 500 {
		return ErrorTypeUpstream4xx
	}
	if details.StatusCode >= 500 && details.StatusCode < 600 {
		return ErrorTypeUpstream5xx
	}
	switch details.ErrorType {
	case types.ErrorTypeOpenAIError,
		types.ErrorTypeClaudeError,
		types.ErrorTypeGeminiError,
		types.ErrorTypeMidjourneyError,
		types.ErrorTypeRerankError,
		types.ErrorTypeUpstreamError:
		return ErrorTypeUpstream5xx
	case types.ErrorTypeNewAPIError:
		return ErrorTypeInternal
	case "":
		// No protocol/source information is available.
	default:
		return ErrorTypeInternal
	}
	if details.Err == nil && details.ErrorCode == "" {
		return ErrorTypeNone
	}
	return ErrorTypeInternal
}

func outcomeLabels(success bool, details ErrorDetails) (string, string) {
	if success {
		return "success", ErrorTypeNone
	}
	errorType := ClassifyError(details)
	if errorType == ErrorTypeNone {
		errorType = ErrorTypeInternal
	}
	return "failure", errorType
}

func normalizeErrorType(value string) string {
	switch value {
	case ErrorTypeClientCancelled,
		ErrorTypeTimeout,
		ErrorTypeConnection,
		ErrorTypeRateLimit,
		ErrorTypeAuthentication,
		ErrorTypeInsufficientQuota,
		ErrorTypeInvalidRequest,
		ErrorTypeChannelUnavailable,
		ErrorTypeUpstream4xx,
		ErrorTypeUpstream5xx,
		ErrorTypeInternal:
		return value
	default:
		return ErrorTypeInternal
	}
}
