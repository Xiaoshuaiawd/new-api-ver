package router

import (
	"net/http"

	prometheusmetrics "github.com/QuantumNous/new-api/pkg/prometheus_metrics"

	"github.com/gin-gonic/gin"
)

func SetMetricsRouter(router *gin.Engine, runtime *prometheusmetrics.Runtime) {
	if runtime == nil || !runtime.Enabled || runtime.Handler == nil {
		return
	}
	router.GET("/metrics", func(c *gin.Context) {
		if !runtime.Authorize(c.ClientIP(), c.GetHeader("Authorization")) {
			c.Data(http.StatusForbidden, "text/plain; charset=utf-8", []byte("forbidden\n"))
			return
		}
		runtime.Handler.ServeHTTP(c.Writer, c.Request)
	})
}
