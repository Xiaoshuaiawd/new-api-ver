package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetChannelRuntimeHealthReport(c *gin.Context) {
	filter := service.ChannelHealthEventFilter{
		ModelName: strings.TrimSpace(c.Query("model")),
		Group:     strings.TrimSpace(c.Query("group")),
		Type:      strings.TrimSpace(c.Query("type")),
		State:     strings.TrimSpace(c.Query("state")),
	}
	if channelIDText := strings.TrimSpace(c.Query("channel_id")); channelIDText != "" {
		channelID, err := strconv.Atoi(channelIDText)
		if err != nil || channelID < 0 {
			common.ApiErrorMsg(c, "invalid channel_id")
			return
		}
		filter.ChannelID = channelID
	}
	if limitText := strings.TrimSpace(c.Query("limit")); limitText != "" {
		limit, err := strconv.Atoi(limitText)
		if err != nil || limit < 0 {
			common.ApiErrorMsg(c, "invalid limit")
			return
		}
		filter.Limit = limit
	}
	common.ApiSuccess(c, service.GetChannelHealthReport(filter))
}
