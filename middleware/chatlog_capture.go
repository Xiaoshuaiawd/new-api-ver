package middleware

import (
	"bytes"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

type chatLogCaptureWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *chatLogCaptureWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if n > 0 {
		_, _ = w.body.Write(data[:n])
	}
	return n, err
}

func (w *chatLogCaptureWriter) WriteString(s string) (int, error) {
	n, err := w.ResponseWriter.WriteString(s)
	if n > 0 {
		_, _ = w.body.WriteString(s[:n])
	}
	return n, err
}

func (w *chatLogCaptureWriter) Bytes() []byte {
	return w.body.Bytes()
}

// ChatLogCapture stores request/response payload in context for async chat log producer.
func ChatLogCapture() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.ChatLogEnabled || c == nil || c.Request == nil {
			c.Next()
			return
		}

		if storage, err := common.GetBodyStorage(c); err == nil && storage != nil {
			if body, bodyErr := storage.Bytes(); bodyErr == nil {
				copied := make([]byte, len(body))
				copy(copied, body)
				common.SetContextKey(c, constant.ContextKeyChatLogRawRequest, copied)
			}
		}

		captureWriter := &chatLogCaptureWriter{ResponseWriter: c.Writer}
		c.Writer = captureWriter
		common.SetContextKey(c, constant.ContextKeyChatLogWriter, &captureWriter.body)

		c.Next()

		resp := captureWriter.Bytes()
		if len(resp) > 0 {
			copied := make([]byte, len(resp))
			copy(copied, resp)
			common.SetContextKey(c, constant.ContextKeyChatLogRawResponse, copied)
		}
	}
}
