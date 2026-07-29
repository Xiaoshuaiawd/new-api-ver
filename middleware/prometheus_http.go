package middleware

import (
	"time"

	prometheusmetrics "github.com/QuantumNous/new-api/pkg/prometheus_metrics"

	"github.com/gin-gonic/gin"
)

func PrometheusHTTP(runtime *prometheusmetrics.Runtime) gin.HandlerFunc {
	return func(c *gin.Context) {
		if runtime == nil || !runtime.Enabled {
			c.Next()
			return
		}

		startedAt := time.Now()
		c.Next()

		routeTag := c.GetString(RouteTagKey)
		if routeTag != "api" && routeTag != "old_api" && routeTag != "relay" {
			return
		}
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		runtime.ObserveHTTPRequest(
			route,
			c.Request.Method,
			httpStatusClass(c.Writer.Status()),
			time.Since(startedAt),
		)
	}
}

func httpStatusClass(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "2xx"
	case status >= 300 && status < 400:
		return "3xx"
	case status >= 400 && status < 500:
		return "4xx"
	case status >= 500 && status < 600:
		return "5xx"
	default:
		return "other"
	}
}
