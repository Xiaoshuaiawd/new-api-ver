package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func insertChatLogTestRow(t *testing.T, table string, row map[string]any) {
	t.Helper()
	require.NoError(t, LOG_DB.Table(table).Create(row).Error)
}

func resetChatLogTables(t *testing.T, tables ...string) {
	t.Helper()
	for _, table := range tables {
		require.NoError(t, EnsureChatLogTable(table))
		require.NoError(t, LOG_DB.Exec("DELETE FROM "+table).Error)
	}
	require.NoError(t, LOG_DB.Exec("DELETE FROM logs").Error)
}

func TestGetChatLogDistributionAll(t *testing.T) {
	common.ChatLogTimeZone = "Asia/Shanghai"
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)

	day1 := time.Date(2026, 4, 14, 0, 0, 0, 0, loc)
	day2 := day1.AddDate(0, 0, 1)

	succDay1 := ChatLogTableName(true, day1)
	failDay1 := ChatLogTableName(false, day1)
	succDay2 := ChatLogTableName(true, day2)
	failDay2 := ChatLogTableName(false, day2)

	resetChatLogTables(t, succDay1, failDay1, succDay2, failDay2)

	insertChatLogTestRow(t, succDay1, map[string]any{
		"user_id":             "1",
		"created_at":          day1.Add(2 * time.Hour),
		"created_date":        "2026-04-14",
		"time_zone":           "Asia/Shanghai",
		"conversation_id":     "conv_1",
		"model_name":          "gpt-5.3-codex",
		"message_id":          "msg_1",
		"channel_id":          "1",
		"prompt_tokens":       50,
		"completion_tokens":   100,
		"total_tokens":        150,
		"is_stream":           false,
		"message_round":       1,
		"is_tools":            false,
		"provider":            "openai",
		"request_id":          "req_dist_1",
		"provider_request_id": "provider_1",
		"status_code":         200,
		"latency_ms":          200,
	})
	insertChatLogTestRow(t, succDay1, map[string]any{
		"user_id":             "1",
		"created_at":          day1.Add(3 * time.Hour),
		"created_date":        "2026-04-14",
		"time_zone":           "Asia/Shanghai",
		"conversation_id":     "conv_2",
		"model_name":          "gpt-5.3-codex",
		"message_id":          "msg_2",
		"channel_id":          "1",
		"prompt_tokens":       600,
		"completion_tokens":   300,
		"total_tokens":        900,
		"is_stream":           true,
		"message_round":       2,
		"is_tools":            true,
		"provider":            "openai",
		"request_id":          "req_dist_2",
		"provider_request_id": "provider_2",
		"status_code":         200,
		"latency_ms":          400,
	})
	insertChatLogTestRow(t, failDay1, map[string]any{
		"user_id":             "2",
		"created_at":          day1.Add(4 * time.Hour),
		"created_date":        "2026-04-14",
		"time_zone":           "Asia/Shanghai",
		"conversation_id":     "conv_3",
		"model_name":          "claude-3-7-sonnet",
		"message_id":          "msg_3",
		"channel_id":          "2",
		"prompt_tokens":       10,
		"completion_tokens":   0,
		"total_tokens":        10,
		"is_stream":           true,
		"message_round":       10,
		"is_tools":            false,
		"provider":            "claude",
		"request_id":          "req_dist_3",
		"provider_request_id": "provider_3",
		"status_code":         500,
		"latency_ms":          50,
	})
	insertChatLogTestRow(t, succDay2, map[string]any{
		"user_id":             "3",
		"created_at":          day2.Add(1 * time.Hour),
		"created_date":        "2026-04-15",
		"time_zone":           "Asia/Shanghai",
		"conversation_id":     "conv_4",
		"model_name":          "gemini-2.5-pro",
		"message_id":          "msg_4",
		"channel_id":          "3",
		"prompt_tokens":       2500,
		"completion_tokens":   500,
		"total_tokens":        3000,
		"is_stream":           false,
		"message_round":       20,
		"is_tools":            false,
		"provider":            "gemini",
		"request_id":          "req_dist_4",
		"provider_request_id": "provider_4",
		"status_code":         200,
		"latency_ms":          300,
	})

	require.NoError(t, LOG_DB.Table("logs").Create(&Log{
		UserId:           1,
		Username:         "u1",
		CreatedAt:        day1.Add(2 * time.Hour).Unix(),
		Type:             LogTypeConsume,
		ModelName:        "gpt-5.3-codex",
		PromptTokens:     50,
		CompletionTokens: 100,
	}).Error)
	require.NoError(t, LOG_DB.Table("logs").Create(&Log{
		UserId:           2,
		Username:         "u2",
		CreatedAt:        day1.Add(4 * time.Hour).Unix(),
		Type:             LogTypeError,
		ModelName:        "claude-3-7-sonnet",
		PromptTokens:     10,
		CompletionTokens: 0,
	}).Error)

	res, err := GetChatLogDistribution(day1, day2.Add(12*time.Hour), "all")
	require.NoError(t, err)
	require.NotNil(t, res)

	require.Equal(t, int64(4), res.Overview.TotalCount)
	require.Equal(t, int64(3), res.Overview.SuccessCount)
	require.Equal(t, int64(1), res.Overview.FailCount)
	require.Equal(t, int64(1), res.Overview.ToolsCount)

	require.Equal(t, int64(1), findBucketCount(res.RoundDistribution, "1"))
	require.Equal(t, int64(1), findBucketCount(res.RoundDistribution, "2"))
	require.Equal(t, int64(1), findBucketCount(res.RoundDistribution, "10"))
	require.Equal(t, int64(1), findBucketCount(res.RoundDistribution, "20"))

	require.Equal(t, int64(2), findModelCount(res.ModelDistribution, "gpt-5.3-codex"))
	require.Equal(t, int64(1), findModelCount(res.ModelDistribution, "claude-3-7-sonnet"))
	require.Equal(t, int64(1), findModelCount(res.ModelDistribution, "gemini-2.5-pro"))

	require.Len(t, res.DailyStats, 2)
	require.Equal(t, "gpt-5.3-codex", res.DailyStats[0].TopModelName)

	require.Equal(t, int64(2), res.DBCompare.LogsCount)
	require.Equal(t, int64(60), res.DBCompare.LogsPromptTokens)
	require.Equal(t, int64(160), res.DBCompare.LogsTotalTokens)
}

func TestGetChatLogDistributionStatusFilter(t *testing.T) {
	common.ChatLogTimeZone = "Asia/Shanghai"
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	day := time.Date(2026, 4, 16, 0, 0, 0, 0, loc)
	succ := ChatLogTableName(true, day)
	fail := ChatLogTableName(false, day)
	resetChatLogTables(t, succ, fail)

	insertChatLogTestRow(t, succ, map[string]any{
		"user_id":             "1",
		"created_at":          day.Add(time.Hour),
		"created_date":        "2026-04-16",
		"time_zone":           "Asia/Shanghai",
		"conversation_id":     "conv_s",
		"model_name":          "gpt",
		"message_id":          "msg_s",
		"channel_id":          "1",
		"prompt_tokens":       1,
		"completion_tokens":   1,
		"total_tokens":        2,
		"is_stream":           false,
		"message_round":       1,
		"is_tools":            false,
		"provider":            "openai",
		"request_id":          "req_dist_sf_1",
		"provider_request_id": "provider_sf_1",
		"status_code":         200,
		"latency_ms":          10,
	})
	insertChatLogTestRow(t, fail, map[string]any{
		"user_id":             "1",
		"created_at":          day.Add(2 * time.Hour),
		"created_date":        "2026-04-16",
		"time_zone":           "Asia/Shanghai",
		"conversation_id":     "conv_f",
		"model_name":          "claude",
		"message_id":          "msg_f",
		"channel_id":          "1",
		"prompt_tokens":       1,
		"completion_tokens":   0,
		"total_tokens":        1,
		"is_stream":           true,
		"message_round":       1,
		"is_tools":            false,
		"provider":            "claude",
		"request_id":          "req_dist_sf_2",
		"provider_request_id": "provider_sf_2",
		"status_code":         500,
		"latency_ms":          10,
	})

	resSuccess, err := GetChatLogDistribution(day, day.Add(23*time.Hour), "success")
	require.NoError(t, err)
	require.Equal(t, int64(1), resSuccess.Overview.TotalCount)
	require.Equal(t, int64(1), resSuccess.Overview.SuccessCount)
	require.Equal(t, int64(0), resSuccess.Overview.FailCount)

	resFail, err := GetChatLogDistribution(day, day.Add(23*time.Hour), "fail")
	require.NoError(t, err)
	require.Equal(t, int64(1), resFail.Overview.TotalCount)
	require.Equal(t, int64(0), resFail.Overview.SuccessCount)
	require.Equal(t, int64(1), resFail.Overview.FailCount)
}

func findBucketCount(buckets []ChatLogDistributionBucket, key string) int64 {
	for _, bucket := range buckets {
		if bucket.Key == key {
			return bucket.Count
		}
	}
	return 0
}

func findModelCount(items []ChatLogModelDistributionItem, modelName string) int64 {
	for _, item := range items {
		if item.ModelName == modelName {
			return item.Count
		}
	}
	return 0
}
