package controller

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

type modelMonitorSummary struct {
	RequestCount int64   `json:"-"`
	SuccessRate  float64 `json:"success_rate"`
	AvgTtftMs    int64   `json:"avg_ttft_ms"`
	AvgLatencyMs int64   `json:"avg_latency_ms"`
	AvgTps       float64 `json:"avg_tps"`
}

type modelMonitorGroup struct {
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Ratio        float64   `json:"ratio"`
	SuccessRate  float64   `json:"success_rate"`
	AvgTtftMs    int64     `json:"avg_ttft_ms"`
	AvgLatencyMs int64     `json:"avg_latency_ms"`
	AvgTps       float64   `json:"avg_tps"`
	RecentRates  []float64 `json:"recent_success_rates,omitempty"`
	LastBucketTs int64     `json:"last_bucket_ts"`
	Status       string    `json:"status"`
	RequestCount int64     `json:"-"`
}

func parseModelMonitorHours(raw string) int {
	hours := 1
	if raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			hours = parsed
		}
	}
	if hours < 1 {
		return 1
	}
	if hours > 24*30 {
		return 24 * 30
	}
	return hours
}

func modelMonitorStatus(requestCount int64, successRate float64) string {
	if requestCount == 0 {
		return "idle"
	}
	if successRate < 70 {
		return "critical"
	}
	if successRate < 90 {
		return "degraded"
	}
	return "healthy"
}

func modelMonitorStatusRank(status string) int {
	switch status {
	case "critical":
		return 0
	case "degraded":
		return 1
	case "healthy":
		return 2
	default:
		return 3
	}
}

func modelMonitorSummaryFor(groups []modelMonitorGroup) modelMonitorSummary {
	total := modelMonitorSummary{}
	var latencySum int64
	var ttftSum int64
	var tpsSum float64
	for _, item := range groups {
		if item.RequestCount <= 0 {
			continue
		}
		total.RequestCount += item.RequestCount
		total.SuccessRate += item.SuccessRate * float64(item.RequestCount)
		latencySum += item.AvgLatencyMs * item.RequestCount
		ttftSum += item.AvgTtftMs * item.RequestCount
		tpsSum += item.AvgTps * float64(item.RequestCount)
	}
	if total.RequestCount == 0 {
		return total
	}
	total.SuccessRate = total.SuccessRate / float64(total.RequestCount)
	total.AvgLatencyMs = latencySum / total.RequestCount
	total.AvgTtftMs = ttftSum / total.RequestCount
	total.AvgTps = tpsSum / float64(total.RequestCount)
	return total
}

func GetModelMonitorSelf(c *gin.Context) {
	hours := parseModelMonitorHours(c.Query("hours"))
	user, err := model.GetUserCache(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	usableGroups := service.GetUserUsableGroups(user.Group)
	activeRatios := ratio_setting.GetGroupRatioCopy()
	allowedGroups := make([]string, 0, len(activeRatios))
	for group := range activeRatios {
		if _, ok := usableGroups[group]; ok {
			allowedGroups = append(allowedGroups, group)
		}
	}
	sort.Strings(allowedGroups)
	if len(allowedGroups) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"updated_at":   time.Now().Unix(),
				"window_hours": hours,
				"summary":      modelMonitorSummary{},
				"groups":       []modelMonitorGroup{},
			},
		})
		return
	}

	summaries, err := perfmetrics.QueryGroupSummaryAll(hours, allowedGroups)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	statsByGroup := map[string]perfmetrics.GroupSummary{}
	for _, summary := range summaries.Groups {
		statsByGroup[summary.Group] = summary
	}

	groups := make([]modelMonitorGroup, 0, len(allowedGroups))
	for _, group := range allowedGroups {
		item := modelMonitorGroup{
			Name:        group,
			Description: usableGroups[group],
			Ratio:       service.GetUserGroupRatio(user.Group, group),
			Status:      "idle",
		}
		if summary, ok := statsByGroup[group]; ok {
			item.RequestCount = summary.RequestCount
			item.SuccessRate = summary.SuccessRate
			item.AvgTtftMs = summary.AvgTtftMs
			item.AvgLatencyMs = summary.AvgLatencyMs
			item.AvgTps = summary.AvgTps
			item.RecentRates = summary.RecentSuccessRates
			item.LastBucketTs = summary.LastBucketTs
			item.Status = modelMonitorStatus(summary.RequestCount, summary.SuccessRate)
		}
		groups = append(groups, item)
	}
	sort.Slice(groups, func(i, j int) bool {
		leftRank := modelMonitorStatusRank(groups[i].Status)
		rightRank := modelMonitorStatusRank(groups[j].Status)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return groups[i].Name < groups[j].Name
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"updated_at":   time.Now().Unix(),
			"window_hours": hours,
			"summary":      modelMonitorSummaryFor(groups),
			"groups":       groups,
		},
	})
}
