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

type modelMonitorModel struct {
	ModelName          string    `json:"model_name"`
	Description        string    `json:"description,omitempty"`
	Icon               string    `json:"icon,omitempty"`
	VendorName         string    `json:"vendor_name,omitempty"`
	VendorIcon         string    `json:"vendor_icon,omitempty"`
	RequestCount       int64     `json:"-"`
	SuccessRate        float64   `json:"success_rate"`
	AvgTtftMs          int64     `json:"avg_ttft_ms"`
	AvgLatencyMs       int64     `json:"avg_latency_ms"`
	AvgTps             float64   `json:"avg_tps"`
	RecentSuccessRates []float64 `json:"recent_success_rates,omitempty"`
	LastBucketTs       int64     `json:"last_bucket_ts"`
	Status             string    `json:"status"`
}

type modelMonitorGroup struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Ratio       float64             `json:"ratio"`
	Summary     modelMonitorSummary `json:"summary"`
	Models      []modelMonitorModel `json:"models"`
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

func modelMonitorStatus(summary perfmetrics.ModelGroupSummary) string {
	if summary.RequestCount == 0 {
		return "idle"
	}
	if summary.SuccessRate < 70 {
		return "critical"
	}
	if summary.SuccessRate < 90 {
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

func modelMonitorSummaryFor(models []modelMonitorModel) modelMonitorSummary {
	total := modelMonitorSummary{}
	var latencySum int64
	var ttftSum int64
	var tpsSum float64
	for _, item := range models {
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
				"groups":       []modelMonitorGroup{},
			},
		})
		return
	}

	summaries, err := perfmetrics.QueryModelGroupSummaryAll(hours, allowedGroups)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	statsByGroupModel := map[string]map[string]perfmetrics.ModelGroupSummary{}
	for _, summary := range summaries.Models {
		if _, ok := statsByGroupModel[summary.Group]; !ok {
			statsByGroupModel[summary.Group] = map[string]perfmetrics.ModelGroupSummary{}
		}
		statsByGroupModel[summary.Group][summary.ModelName] = summary
	}

	vendorByID := map[int]model.PricingVendor{}
	for _, vendor := range model.GetVendors() {
		vendorByID[vendor.ID] = vendor
	}

	modelsByGroup := map[string]map[string]modelMonitorModel{}
	allowedGroupSet := map[string]struct{}{}
	for _, group := range allowedGroups {
		allowedGroupSet[group] = struct{}{}
		modelsByGroup[group] = map[string]modelMonitorModel{}
	}

	for _, pricing := range filterPricingByUsableGroups(model.GetPricing(), usableGroups) {
		targetGroups := pricing.EnableGroup
		if common.StringsContains(targetGroups, "all") {
			targetGroups = allowedGroups
		}
		for _, group := range targetGroups {
			if _, ok := allowedGroupSet[group]; !ok {
				continue
			}
			vendor := vendorByID[pricing.VendorID]
			modelsByGroup[group][pricing.ModelName] = modelMonitorModel{
				ModelName:   pricing.ModelName,
				Description: pricing.Description,
				Icon:        pricing.Icon,
				VendorName:  vendor.Name,
				VendorIcon:  vendor.Icon,
				Status:      "idle",
			}
		}
	}

	groups := make([]modelMonitorGroup, 0, len(allowedGroups))
	for _, group := range allowedGroups {
		modelItems := make([]modelMonitorModel, 0, len(modelsByGroup[group]))
		for modelName, item := range modelsByGroup[group] {
			if summary, ok := statsByGroupModel[group][modelName]; ok {
				item.RequestCount = summary.RequestCount
				item.SuccessRate = summary.SuccessRate
				item.AvgTtftMs = summary.AvgTtftMs
				item.AvgLatencyMs = summary.AvgLatencyMs
				item.AvgTps = summary.AvgTps
				item.RecentSuccessRates = summary.RecentSuccessRates
				item.LastBucketTs = summary.LastBucketTs
				item.Status = modelMonitorStatus(summary)
			}
			modelItems = append(modelItems, item)
		}
		sort.Slice(modelItems, func(i, j int) bool {
			leftRank := modelMonitorStatusRank(modelItems[i].Status)
			rightRank := modelMonitorStatusRank(modelItems[j].Status)
			if leftRank != rightRank {
				return leftRank < rightRank
			}
			return modelItems[i].ModelName < modelItems[j].ModelName
		})
		groups = append(groups, modelMonitorGroup{
			Name:        group,
			Description: usableGroups[group],
			Ratio:       service.GetUserGroupRatio(user.Group, group),
			Summary:     modelMonitorSummaryFor(modelItems),
			Models:      modelItems,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"updated_at":   time.Now().Unix(),
			"window_hours": hours,
			"groups":       groups,
		},
	})
}
