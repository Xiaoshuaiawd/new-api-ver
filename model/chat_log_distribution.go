package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	chatLogStatusAll     = "all"
	chatLogStatusSuccess = "success"
	chatLogStatusFail    = "fail"
)

type ChatLogDistributionBucket struct {
	Key     string  `json:"key"`
	Label   string  `json:"label"`
	Count   int64   `json:"count"`
	Percent float64 `json:"percent"`
}

type ChatLogModelDistributionItem struct {
	ModelName string  `json:"model_name"`
	Count     int64   `json:"count"`
	Percent   float64 `json:"percent"`
}

type ChatLogDailyStatItem struct {
	Date                string  `json:"date"`
	TotalCount          int64   `json:"total_count"`
	SuccessCount        int64   `json:"success_count"`
	FailCount           int64   `json:"fail_count"`
	ToolsCount          int64   `json:"tools_count"`
	PromptTokensSum     int64   `json:"prompt_tokens_sum"`
	CompletionTokensSum int64   `json:"completion_tokens_sum"`
	TotalTokensSum      int64   `json:"total_tokens_sum"`
	AvgLatencyMS        float64 `json:"avg_latency_ms"`
	TopModelName        string  `json:"top_model_name"`
	TopModelCount       int64   `json:"top_model_count"`
}

type ChatLogDistributionOverview struct {
	TotalCount          int64   `json:"total_count"`
	SuccessCount        int64   `json:"success_count"`
	FailCount           int64   `json:"fail_count"`
	ToolsCount          int64   `json:"tools_count"`
	ToolsPercent        float64 `json:"tools_percent"`
	PromptTokensSum     int64   `json:"prompt_tokens_sum"`
	CompletionTokensSum int64   `json:"completion_tokens_sum"`
	TotalTokensSum      int64   `json:"total_tokens_sum"`
}

type ChatLogDBCompare struct {
	ChatLogCount        int64   `json:"chat_log_count"`
	ChatLogPromptTokens int64   `json:"chat_log_prompt_tokens"`
	ChatLogTotalTokens  int64   `json:"chat_log_total_tokens"`
	LogsCount           int64   `json:"logs_count"`
	LogsPromptTokens    int64   `json:"logs_prompt_tokens"`
	LogsTotalTokens     int64   `json:"logs_total_tokens"`
	CountDiff           int64   `json:"count_diff"`
	PromptTokensDiff    int64   `json:"prompt_tokens_diff"`
	TotalTokensDiff     int64   `json:"total_tokens_diff"`
	CountDiffPercent    float64 `json:"count_diff_percent"`
}

type ChatLogScanTableItem struct {
	Table  string `json:"table"`
	Date   string `json:"date"`
	Status string `json:"status"`
}

type ChatLogDistributionResult struct {
	TimeZone                 string                         `json:"time_zone"`
	Status                   string                         `json:"status"`
	StartTime                int64                          `json:"start_time"`
	EndTime                  int64                          `json:"end_time"`
	Overview                 ChatLogDistributionOverview    `json:"overview"`
	RoundDistribution        []ChatLogDistributionBucket    `json:"round_distribution"`
	PromptTokensDistribution []ChatLogDistributionBucket    `json:"prompt_tokens_distribution"`
	TotalTokensDistribution  []ChatLogDistributionBucket    `json:"total_tokens_distribution"`
	ModelDistribution        []ChatLogModelDistributionItem `json:"model_distribution"`
	DailyStats               []ChatLogDailyStatItem         `json:"daily_stats"`
	DBCompare                ChatLogDBCompare               `json:"db_compare"`
	ScanTables               []ChatLogScanTableItem         `json:"scan_tables"`
}

type chatLogSummaryRow struct {
	TotalCount          int64 `gorm:"column:total_count"`
	ToolsCount          int64 `gorm:"column:tools_count"`
	PromptTokensSum     int64 `gorm:"column:prompt_tokens_sum"`
	CompletionTokensSum int64 `gorm:"column:completion_tokens_sum"`
	TotalTokensSum      int64 `gorm:"column:total_tokens_sum"`
	LatencyMSSum        int64 `gorm:"column:latency_ms_sum"`
}

type chatLogRoundRow struct {
	MessageRound int   `gorm:"column:message_round"`
	Count        int64 `gorm:"column:count"`
}

type chatLogModelRow struct {
	ModelName string `gorm:"column:model_name"`
	Count     int64  `gorm:"column:count"`
}

type chatLogTokenBucketRow struct {
	Prompt0          int64 `gorm:"column:prompt_0"`
	Prompt1To100     int64 `gorm:"column:prompt_1_100"`
	Prompt101To500   int64 `gorm:"column:prompt_101_500"`
	Prompt501To1000  int64 `gorm:"column:prompt_501_1000"`
	Prompt1001To2000 int64 `gorm:"column:prompt_1001_2000"`
	Prompt2001To4000 int64 `gorm:"column:prompt_2001_4000"`
	Prompt4001Plus   int64 `gorm:"column:prompt_4001_plus"`

	Total0          int64 `gorm:"column:total_0"`
	Total1To200     int64 `gorm:"column:total_1_200"`
	Total201To1000  int64 `gorm:"column:total_201_1000"`
	Total1001To2000 int64 `gorm:"column:total_1001_2000"`
	Total2001To4000 int64 `gorm:"column:total_2001_4000"`
	Total4001To8000 int64 `gorm:"column:total_4001_8000"`
	Total8001Plus   int64 `gorm:"column:total_8001_plus"`
}

type chatLogLegacySummaryRow struct {
	Count           int64 `gorm:"column:count"`
	PromptTokensSum int64 `gorm:"column:prompt_tokens_sum"`
	TotalTokensSum  int64 `gorm:"column:total_tokens_sum"`
}

type chatLogDailyAccumulator struct {
	ChatLogDailyStatItem
	latencyMSSum int64
	modelCounts  map[string]int64
}

func GetChatLogDistribution(startTime, endTime time.Time, status string) (*ChatLogDistributionResult, error) {
	chatLogDB := getChatLogDB()
	if chatLogDB == nil {
		return nil, errors.New("chat log db is nil")
	}
	normalizedStatus := normalizeChatLogStatus(status)
	if normalizedStatus == "" {
		return nil, fmt.Errorf("invalid status: %s", status)
	}

	loc := getChatLogLocation()
	start := startTime.In(loc)
	end := endTime.In(loc)
	if end.Before(start) {
		start, end = end, start
	}

	result := &ChatLogDistributionResult{
		TimeZone:  "Asia/Shanghai",
		Status:    normalizedStatus,
		StartTime: start.Unix(),
		EndTime:   end.Unix(),
	}

	dayStarts := buildChatLogDayStarts(start, end, loc)
	dailyMap := make(map[string]*chatLogDailyAccumulator, len(dayStarts))
	for _, dayStart := range dayStarts {
		date := dayStart.Format("2006-01-02")
		dailyMap[date] = &chatLogDailyAccumulator{
			ChatLogDailyStatItem: ChatLogDailyStatItem{Date: date},
			modelCounts:          make(map[string]int64),
		}
	}

	roundBuckets := map[string]int64{
		"1":     0,
		"2":     0,
		"3":     0,
		"4":     0,
		"5":     0,
		"10":    0,
		"15":    0,
		"20":    0,
		"other": 0,
	}
	promptBuckets := map[string]int64{
		"0":         0,
		"1-100":     0,
		"101-500":   0,
		"501-1000":  0,
		"1001-2000": 0,
		"2001-4000": 0,
		"4001+":     0,
	}
	totalBuckets := map[string]int64{
		"0":         0,
		"1-200":     0,
		"201-1000":  0,
		"1001-2000": 0,
		"2001-4000": 0,
		"4001-8000": 0,
		"8001+":     0,
	}
	modelCountMap := make(map[string]int64)
	scanTables := make([]ChatLogScanTableItem, 0, len(dayStarts)*2)

	for _, dayStart := range dayStarts {
		dateKey := dayStart.Format("2006-01-02")
		dailyAcc := dailyMap[dateKey]
		for _, success := range statusesByFilter(normalizedStatus) {
			tableName := ChatLogTableName(success, dayStart)
			hasTable := chatLogDB.Migrator().HasTable(tableName)
			if !hasTable {
				scanTables = append(scanTables, ChatLogScanTableItem{
					Table:  tableName,
					Date:   dateKey,
					Status: "missing",
				})
				continue
			}
			scanTables = append(scanTables, ChatLogScanTableItem{
				Table:  tableName,
				Date:   dateKey,
				Status: "ok",
			})

			where := chatLogDB.Table(tableName).Where("created_at >= ? AND created_at <= ?", start, end)

			var summary chatLogSummaryRow
			if err := where.
				Select(`
COUNT(*) AS total_count,
COALESCE(SUM(CASE WHEN is_tools THEN 1 ELSE 0 END), 0) AS tools_count,
COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens_sum,
COALESCE(SUM(completion_tokens), 0) AS completion_tokens_sum,
COALESCE(SUM(total_tokens), 0) AS total_tokens_sum,
COALESCE(SUM(latency_ms), 0) AS latency_ms_sum`).
				Scan(&summary).Error; err != nil {
				return nil, err
			}

			result.Overview.TotalCount += summary.TotalCount
			if success {
				result.Overview.SuccessCount += summary.TotalCount
				dailyAcc.SuccessCount += summary.TotalCount
			} else {
				result.Overview.FailCount += summary.TotalCount
				dailyAcc.FailCount += summary.TotalCount
			}
			result.Overview.ToolsCount += summary.ToolsCount
			result.Overview.PromptTokensSum += summary.PromptTokensSum
			result.Overview.CompletionTokensSum += summary.CompletionTokensSum
			result.Overview.TotalTokensSum += summary.TotalTokensSum

			dailyAcc.TotalCount += summary.TotalCount
			dailyAcc.ToolsCount += summary.ToolsCount
			dailyAcc.PromptTokensSum += summary.PromptTokensSum
			dailyAcc.CompletionTokensSum += summary.CompletionTokensSum
			dailyAcc.TotalTokensSum += summary.TotalTokensSum
			dailyAcc.latencyMSSum += summary.LatencyMSSum

			var roundRows []chatLogRoundRow
			if err := where.
				Select("message_round, COUNT(*) AS count").
				Group("message_round").
				Scan(&roundRows).Error; err != nil {
				return nil, err
			}
			for _, roundRow := range roundRows {
				roundBuckets[roundBucketKey(roundRow.MessageRound)] += roundRow.Count
			}

			var modelRows []chatLogModelRow
			if err := where.
				Select("model_name, COUNT(*) AS count").
				Group("model_name").
				Scan(&modelRows).Error; err != nil {
				return nil, err
			}
			for _, modelRow := range modelRows {
				name := strings.TrimSpace(modelRow.ModelName)
				if name == "" {
					name = "unknown"
				}
				modelCountMap[name] += modelRow.Count
				dailyAcc.modelCounts[name] += modelRow.Count
			}

			var tokenBucketRow chatLogTokenBucketRow
			if err := where.
				Select(`
COALESCE(SUM(CASE WHEN prompt_tokens = 0 THEN 1 ELSE 0 END), 0) AS prompt_0,
COALESCE(SUM(CASE WHEN prompt_tokens BETWEEN 1 AND 100 THEN 1 ELSE 0 END), 0) AS prompt_1_100,
COALESCE(SUM(CASE WHEN prompt_tokens BETWEEN 101 AND 500 THEN 1 ELSE 0 END), 0) AS prompt_101_500,
COALESCE(SUM(CASE WHEN prompt_tokens BETWEEN 501 AND 1000 THEN 1 ELSE 0 END), 0) AS prompt_501_1000,
COALESCE(SUM(CASE WHEN prompt_tokens BETWEEN 1001 AND 2000 THEN 1 ELSE 0 END), 0) AS prompt_1001_2000,
COALESCE(SUM(CASE WHEN prompt_tokens BETWEEN 2001 AND 4000 THEN 1 ELSE 0 END), 0) AS prompt_2001_4000,
COALESCE(SUM(CASE WHEN prompt_tokens >= 4001 THEN 1 ELSE 0 END), 0) AS prompt_4001_plus,
COALESCE(SUM(CASE WHEN total_tokens = 0 THEN 1 ELSE 0 END), 0) AS total_0,
COALESCE(SUM(CASE WHEN total_tokens BETWEEN 1 AND 200 THEN 1 ELSE 0 END), 0) AS total_1_200,
COALESCE(SUM(CASE WHEN total_tokens BETWEEN 201 AND 1000 THEN 1 ELSE 0 END), 0) AS total_201_1000,
COALESCE(SUM(CASE WHEN total_tokens BETWEEN 1001 AND 2000 THEN 1 ELSE 0 END), 0) AS total_1001_2000,
COALESCE(SUM(CASE WHEN total_tokens BETWEEN 2001 AND 4000 THEN 1 ELSE 0 END), 0) AS total_2001_4000,
COALESCE(SUM(CASE WHEN total_tokens BETWEEN 4001 AND 8000 THEN 1 ELSE 0 END), 0) AS total_4001_8000,
COALESCE(SUM(CASE WHEN total_tokens >= 8001 THEN 1 ELSE 0 END), 0) AS total_8001_plus`).
				Scan(&tokenBucketRow).Error; err != nil {
				return nil, err
			}

			promptBuckets["0"] += tokenBucketRow.Prompt0
			promptBuckets["1-100"] += tokenBucketRow.Prompt1To100
			promptBuckets["101-500"] += tokenBucketRow.Prompt101To500
			promptBuckets["501-1000"] += tokenBucketRow.Prompt501To1000
			promptBuckets["1001-2000"] += tokenBucketRow.Prompt1001To2000
			promptBuckets["2001-4000"] += tokenBucketRow.Prompt2001To4000
			promptBuckets["4001+"] += tokenBucketRow.Prompt4001Plus

			totalBuckets["0"] += tokenBucketRow.Total0
			totalBuckets["1-200"] += tokenBucketRow.Total1To200
			totalBuckets["201-1000"] += tokenBucketRow.Total201To1000
			totalBuckets["1001-2000"] += tokenBucketRow.Total1001To2000
			totalBuckets["2001-4000"] += tokenBucketRow.Total2001To4000
			totalBuckets["4001-8000"] += tokenBucketRow.Total4001To8000
			totalBuckets["8001+"] += tokenBucketRow.Total8001Plus
		}
	}

	result.Overview.ToolsPercent = calcPercent(result.Overview.ToolsCount, result.Overview.TotalCount)
	result.RoundDistribution = buildRoundBuckets(roundBuckets, result.Overview.TotalCount)
	result.PromptTokensDistribution = buildPromptBuckets(promptBuckets, result.Overview.TotalCount)
	result.TotalTokensDistribution = buildTotalBuckets(totalBuckets, result.Overview.TotalCount)
	result.ModelDistribution = buildModelDistribution(modelCountMap, result.Overview.TotalCount)
	result.DailyStats = buildDailyStats(dayStarts, dailyMap)
	result.DBCompare = buildChatLogDBCompare(start, end, normalizedStatus, result.Overview)
	result.ScanTables = scanTables

	return result, nil
}

func normalizeChatLogStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", chatLogStatusAll:
		return chatLogStatusAll
	case chatLogStatusSuccess:
		return chatLogStatusSuccess
	case chatLogStatusFail:
		return chatLogStatusFail
	default:
		return ""
	}
}

func statusesByFilter(status string) []bool {
	switch status {
	case chatLogStatusSuccess:
		return []bool{true}
	case chatLogStatusFail:
		return []bool{false}
	default:
		return []bool{true, false}
	}
}

func buildChatLogDayStarts(start, end time.Time, loc *time.Location) []time.Time {
	dayStart := time.Date(start.In(loc).Year(), start.In(loc).Month(), start.In(loc).Day(), 0, 0, 0, 0, loc)
	dayEnd := time.Date(end.In(loc).Year(), end.In(loc).Month(), end.In(loc).Day(), 0, 0, 0, 0, loc)
	days := make([]time.Time, 0, int(dayEnd.Sub(dayStart).Hours()/24)+1)
	for cur := dayStart; !cur.After(dayEnd); cur = cur.AddDate(0, 0, 1) {
		days = append(days, cur)
	}
	return days
}

func roundBucketKey(round int) string {
	switch round {
	case 1:
		return "1"
	case 2:
		return "2"
	case 3:
		return "3"
	case 4:
		return "4"
	case 5:
		return "5"
	case 10:
		return "10"
	case 15:
		return "15"
	case 20:
		return "20"
	default:
		return "other"
	}
}

func buildRoundBuckets(counts map[string]int64, total int64) []ChatLogDistributionBucket {
	order := []struct {
		Key   string
		Label string
	}{
		{Key: "1", Label: "1"},
		{Key: "2", Label: "2"},
		{Key: "3", Label: "3"},
		{Key: "4", Label: "4"},
		{Key: "5", Label: "5"},
		{Key: "10", Label: "10"},
		{Key: "15", Label: "15"},
		{Key: "20", Label: "20"},
		{Key: "other", Label: "other"},
	}
	items := make([]ChatLogDistributionBucket, 0, len(order))
	for _, it := range order {
		count := counts[it.Key]
		items = append(items, ChatLogDistributionBucket{
			Key:     it.Key,
			Label:   it.Label,
			Count:   count,
			Percent: calcPercent(count, total),
		})
	}
	return items
}

func buildPromptBuckets(counts map[string]int64, total int64) []ChatLogDistributionBucket {
	order := []struct {
		Key   string
		Label string
	}{
		{Key: "0", Label: "0"},
		{Key: "1-100", Label: "1-100"},
		{Key: "101-500", Label: "101-500"},
		{Key: "501-1000", Label: "501-1000"},
		{Key: "1001-2000", Label: "1001-2000"},
		{Key: "2001-4000", Label: "2001-4000"},
		{Key: "4001+", Label: "4001+"},
	}
	items := make([]ChatLogDistributionBucket, 0, len(order))
	for _, it := range order {
		count := counts[it.Key]
		items = append(items, ChatLogDistributionBucket{
			Key:     it.Key,
			Label:   it.Label,
			Count:   count,
			Percent: calcPercent(count, total),
		})
	}
	return items
}

func buildTotalBuckets(counts map[string]int64, total int64) []ChatLogDistributionBucket {
	order := []struct {
		Key   string
		Label string
	}{
		{Key: "0", Label: "0"},
		{Key: "1-200", Label: "1-200"},
		{Key: "201-1000", Label: "201-1000"},
		{Key: "1001-2000", Label: "1001-2000"},
		{Key: "2001-4000", Label: "2001-4000"},
		{Key: "4001-8000", Label: "4001-8000"},
		{Key: "8001+", Label: "8001+"},
	}
	items := make([]ChatLogDistributionBucket, 0, len(order))
	for _, it := range order {
		count := counts[it.Key]
		items = append(items, ChatLogDistributionBucket{
			Key:     it.Key,
			Label:   it.Label,
			Count:   count,
			Percent: calcPercent(count, total),
		})
	}
	return items
}

func buildModelDistribution(modelCounts map[string]int64, total int64) []ChatLogModelDistributionItem {
	items := make([]ChatLogModelDistributionItem, 0, len(modelCounts))
	for modelName, count := range modelCounts {
		items = append(items, ChatLogModelDistributionItem{
			ModelName: modelName,
			Count:     count,
			Percent:   calcPercent(count, total),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].ModelName < items[j].ModelName
		}
		return items[i].Count > items[j].Count
	})
	return items
}

func buildDailyStats(dayStarts []time.Time, dailyMap map[string]*chatLogDailyAccumulator) []ChatLogDailyStatItem {
	items := make([]ChatLogDailyStatItem, 0, len(dayStarts))
	for _, dayStart := range dayStarts {
		dateKey := dayStart.Format("2006-01-02")
		acc, ok := dailyMap[dateKey]
		if !ok {
			items = append(items, ChatLogDailyStatItem{Date: dateKey})
			continue
		}
		if acc.TotalCount > 0 {
			acc.AvgLatencyMS = float64(acc.latencyMSSum) / float64(acc.TotalCount)
		}
		for modelName, cnt := range acc.modelCounts {
			if cnt > acc.TopModelCount || (cnt == acc.TopModelCount && modelName < acc.TopModelName) {
				acc.TopModelName = modelName
				acc.TopModelCount = cnt
			}
		}
		items = append(items, acc.ChatLogDailyStatItem)
	}
	return items
}

func buildChatLogDBCompare(start, end time.Time, status string, overview ChatLogDistributionOverview) ChatLogDBCompare {
	compare := ChatLogDBCompare{
		ChatLogCount:        overview.TotalCount,
		ChatLogPromptTokens: overview.PromptTokensSum,
		ChatLogTotalTokens:  overview.TotalTokensSum,
	}
	compare.CountDiff = compare.ChatLogCount
	compare.PromptTokensDiff = compare.ChatLogPromptTokens
	compare.TotalTokensDiff = compare.ChatLogTotalTokens
	if LOG_DB == nil || !LOG_DB.Migrator().HasTable("logs") {
		return compare
	}

	var summary chatLogLegacySummaryRow
	query := LOG_DB.Table("logs").Select(`
COUNT(*) AS count,
COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens_sum,
COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS total_tokens_sum`).
		Where("created_at >= ? AND created_at <= ?", start.Unix(), end.Unix())

	switch status {
	case chatLogStatusSuccess:
		query = query.Where("type = ?", LogTypeConsume)
	case chatLogStatusFail:
		query = query.Where("type = ?", LogTypeError)
	default:
		query = query.Where("type IN ?", []int{LogTypeConsume, LogTypeError})
	}
	if err := query.Scan(&summary).Error; err != nil {
		return compare
	}

	compare.LogsCount = summary.Count
	compare.LogsPromptTokens = summary.PromptTokensSum
	compare.LogsTotalTokens = summary.TotalTokensSum
	compare.CountDiff = compare.ChatLogCount - compare.LogsCount
	compare.PromptTokensDiff = compare.ChatLogPromptTokens - compare.LogsPromptTokens
	compare.TotalTokensDiff = compare.ChatLogTotalTokens - compare.LogsTotalTokens
	compare.CountDiffPercent = calcPercent(compare.CountDiff, compare.LogsCount)
	return compare
}

func calcPercent(part, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}
