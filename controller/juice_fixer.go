package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	juice_setting "github.com/QuantumNous/new-api/setting/juice_fixer_setting"
	"github.com/gin-gonic/gin"
)

func GetJuiceFixerConfig(c *gin.Context) {
	common.ApiSuccess(c, juice_setting.GetPublic())
}

func UpdateJuiceFixerConfig(c *gin.Context) {
	var request juice_setting.StorageConfig
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorMsg(c, "参数错误: "+err.Error())
		return
	}
	config, err := juice_setting.Normalize(request)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	payload, err := common.Marshal(config)
	if err != nil {
		common.ApiErrorMsg(c, "序列化配置失败")
		return
	}
	if err := model.UpdateOption("juice_fixer_setting", string(payload)); err != nil {
		logger.LogError(c, "failed to save juice_fixer_setting: "+err.Error())
		common.ApiErrorMsg(c, "保存配置失败")
		return
	}
	common.ApiSuccess(c, juice_setting.GetPublic())
}
