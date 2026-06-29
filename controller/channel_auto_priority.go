package controller

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func ApplyChannelAutoPriority(c *gin.Context) {
	summary, err := service.ApplyChannelAutoPriority(context.Background())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, summary)
}
