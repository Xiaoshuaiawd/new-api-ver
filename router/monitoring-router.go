package router

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func SetMonitoringRouter(router *gin.Engine) {
	config := common.GetPrometheusConfig()
	if !config.Enabled {
		return
	}
	router.GET(config.Path, middleware.RouteTag("metrics"), common.PrometheusHandler())
}
