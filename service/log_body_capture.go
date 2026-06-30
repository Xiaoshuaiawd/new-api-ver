package service

import (
	"io"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const (
	ginKeyLogResponseBody = "log_response_body"
	maxLogBodyDetailBytes = 64 * 1024
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

	detail := map[string]interface{}{}
	if requestBody, size, truncated, ok := getLogRequestBody(c); ok {
		detail["request_body"] = requestBody
		if truncated {
			detail["request_body_truncated"] = true
			detail["request_body_size"] = size
		}
	}
	if responseBody, size, truncated, ok := getLogResponseBody(c); ok {
		detail["response_body"] = responseBody
		if truncated {
			detail["response_body_truncated"] = true
			detail["response_body_size"] = size
		}
	}
	if len(detail) == 0 {
		return
	}
	other["log_detail"] = detail
}

func getLogRequestBody(c *gin.Context) (string, int, bool, bool) {
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage == nil {
		return "", 0, false, false
	}
	data, err := storage.Bytes()
	if err != nil {
		return "", 0, false, false
	}
	_, _ = storage.Seek(0, io.SeekStart)
	return truncateLogBody(data)
}

func getLogResponseBody(c *gin.Context) (string, int, bool, bool) {
	value, ok := c.Get(ginKeyLogResponseBody)
	if !ok || value == nil {
		return "", 0, false, false
	}
	switch body := value.(type) {
	case string:
		return truncateLogBody([]byte(body))
	case []byte:
		return truncateLogBody(body)
	default:
		return "", 0, false, false
	}
}

func truncateLogBody(data []byte) (string, int, bool, bool) {
	size := len(data)
	if len(data) > maxLogBodyDetailBytes {
		data = data[:maxLogBodyDetailBytes]
		return string(data), size, true, true
	}
	return string(data), size, false, true
}
