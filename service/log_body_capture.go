package service

import (
	"io"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const (
	ginKeyLogResponseBody = "log_response_body"
)

func SetLogResponseBody(c *gin.Context, body []byte) {
	if c == nil {
		return
	}
	if existing, ok := c.Get(ginKeyLogResponseBody); ok && existing != nil {
		return
	}
	SetFinalLogResponseBody(c, body)
}

func SetFinalLogResponseBody(c *gin.Context, body []byte) {
	if c == nil {
		return
	}
	c.Set(ginKeyLogResponseBody, string(body))
}

func SetLogResponseBodyFromContext(ctx any, body []byte) {
	ginCtx, ok := ctx.(*gin.Context)
	if !ok {
		return
	}
	SetLogResponseBody(ginCtx, body)
}

func AppendLogBodyDetails(c *gin.Context, other map[string]interface{}) {
	if c == nil || other == nil {
		return
	}
	if !common.LogBodyCaptureEnabled {
		return
	}

	detail := map[string]interface{}{}
	if requestBody, ok := getLogRequestBody(c); ok {
		detail["request_body"] = requestBody
	}
	if responseBody, ok := getLogResponseBody(c); ok {
		detail["response_body"] = responseBody
	}
	if len(detail) == 0 {
		return
	}
	other["log_detail"] = detail
}

func appendLogBodyDetailsForZeroTokens(c *gin.Context, other map[string]interface{}, totalTokens int) {
	if totalTokens != 0 {
		return
	}
	AppendLogBodyDetails(c, other)
}

func getLogRequestBody(c *gin.Context) (string, bool) {
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage == nil {
		return "", false
	}
	data, err := storage.Bytes()
	if err != nil {
		return "", false
	}
	_, _ = storage.Seek(0, io.SeekStart)
	return string(data), true
}

func getLogResponseBody(c *gin.Context) (string, bool) {
	value, ok := c.Get(ginKeyLogResponseBody)
	if !ok || value == nil {
		return "", false
	}
	switch body := value.(type) {
	case string:
		return body, true
	case []byte:
		return string(body), true
	default:
		return "", false
	}
}
