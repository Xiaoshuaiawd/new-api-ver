package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type channelRuntimeActionRequest struct {
	Action          string `json:"action"`
	Reason          string `json:"reason"`
	DurationSeconds int    `json:"duration_seconds"`
}

func ChannelRuntimeAction(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid channel id"})
		return
	}
	channel, err := model.CacheGetChannel(channelID)
	if err != nil || channel == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "channel not found"})
		return
	}

	var req channelRuntimeActionRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request"})
		return
	}

	action := strings.TrimSpace(req.Action)
	var result interface{}
	switch action {
	case "isolate":
		duration := time.Duration(req.DurationSeconds) * time.Second
		result, err = service.ForceOpenChannelRuntime(channelID, req.Reason, duration)
	case "probe_now":
		result, err = service.ForceChannelRuntimeProbeNow(channelID)
	case "clear_isolation":
		result, err = service.ClearChannelRuntimeIsolation(channelID)
	case "clear_affinity":
		deleted := service.ClearChannelAffinityByChannelID(channelID)
		result = gin.H{"channel_id": channelID, "affinity_deleted": deleted}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "unsupported action"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	recordManageAudit(c, "channel.runtime_action", map[string]interface{}{
		"id":               channelID,
		"name":             channel.Name,
		"action":           action,
		"duration_seconds": req.DurationSeconds,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result,
	})
}
