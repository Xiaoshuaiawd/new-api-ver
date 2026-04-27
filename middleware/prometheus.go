package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func Prometheus() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestMetrics := common.GetPrometheusConfig()
		if !requestMetrics.Enabled {
			c.Next()
			return
		}

		path := ""
		if c.Request != nil && c.Request.URL != nil {
			path = c.Request.URL.Path
		}
		routeTag := common.MetricsRouteTag(path, c.GetString(RouteTagKey))
		observer := common.GetPrometheusHTTPObserver(routeTag, c.Request.Method)
		c.Next()
		observer.Done(c.Writer.Status())
	}
}
