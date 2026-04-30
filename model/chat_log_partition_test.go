package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestChatLogTableNameShanghaiBoundary(t *testing.T) {
	common.ChatLogTimeZone = "Asia/Shanghai"

	utcBeforeDayChange := time.Date(2026, 4, 13, 15, 59, 59, 0, time.UTC) // 2026-04-13 23:59:59 +08
	utcAfterDayChange := utcBeforeDayChange.Add(time.Second)              // 2026-04-14 00:00:00 +08

	require.Equal(t, "chat_log_success_20260413", ChatLogTableName(true, utcBeforeDayChange))
	require.Equal(t, "chat_log_fail_20260414", ChatLogTableName(false, utcAfterDayChange))
}

func TestChatLogTimestampStringUsesShanghaiWallTime(t *testing.T) {
	common.ChatLogTimeZone = "Asia/Shanghai"

	utcTime := time.Date(2026, 4, 13, 15, 0, 0, 123456000, time.UTC) // 2026-04-13 23:00:00.123 +08

	require.Equal(t, "2026-04-13 23:00:00.123", chatLogDBTimeString(utcTime))
	require.Equal(t, "2026-04-13", chatLogDBDateString(utcTime))
}

func TestUpsertChatLogEventIdempotent(t *testing.T) {
	common.ChatLogTimeZone = "Asia/Shanghai"

	createdAt := time.Date(2026, 4, 13, 15, 0, 0, 0, time.UTC) // 2026-04-13 23:00:00 +08
	requestID := "req_chatlog_upsert_1"
	event := &dto.ChatLogEvent{
		EventID:            "evt_1",
		UserID:             "1",
		CreatedAt:          createdAt,
		ConversationID:     "conv_1",
		ModelName:          "gpt-4.1",
		MessageID:          "msg_1",
		ChannelID:          "10",
		PromptTokens:       11,
		CompletionTokens:   22,
		TotalTokens:        33,
		IsStream:           false,
		MessageRound:       1,
		IsTools:            false,
		Provider:           "openai",
		RequestID:          requestID,
		ProviderRequestID:  "provider_req_1",
		StatusCode:         200,
		RawRequest:         `{"messages":[{"role":"user","content":"hi"}]}`,
		RawResponse:        `{"choices":[{"message":{"content":"hello"}}]}`,
		NormalizedResponse: `{"role":"assistant","content":"hello"}`,
		FinalAnswerText:    "hello",
		FinalMergedJSON:    `{"request":{"messages":[{"role":"user","content":"hi"}]},"response":{"role":"assistant","content":"hello"}}`,
		StreamMerged:       true,
		PayloadBytes:       128,
		PayloadSHA256:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Success:            true,
	}

	require.NoError(t, UpsertChatLogEvent(event))

	event.FinalAnswerText = "hello-updated"
	event.CompletionTokens = 30
	event.TotalTokens = 41
	require.NoError(t, UpsertChatLogEvent(event))

	tableName := ChatLogTableName(true, createdAt)
	var count int64
	require.NoError(t, LOG_DB.Table(tableName).Where("request_id = ?", requestID).Count(&count).Error)
	require.Equal(t, int64(1), count)

	var row struct {
		FinalAnswerText  string `gorm:"column:final_answer_text"`
		FinalMergedJSON  string `gorm:"column:final_merged_json"`
		CompletionTokens int    `gorm:"column:completion_tokens"`
		TotalTokens      int    `gorm:"column:total_tokens"`
		CreatedAt        string `gorm:"column:created_at"`
		CreatedDate      string `gorm:"column:created_date"`
		TimeZone         string `gorm:"column:time_zone"`
	}
	require.NoError(t, LOG_DB.Table(tableName).
		Select("final_answer_text, final_merged_json, completion_tokens, total_tokens, created_at, created_date, time_zone").
		Where("request_id = ?", requestID).
		Take(&row).Error)
	require.Equal(t, "hello-updated", row.FinalAnswerText)
	require.NotEmpty(t, row.FinalMergedJSON)
	require.Equal(t, 30, row.CompletionTokens)
	require.Equal(t, 41, row.TotalTokens)
	require.Contains(t, row.CreatedAt, "2026-04-13 23:00:00")
	require.Equal(t, "2026-04-13", row.CreatedDate)
	require.Equal(t, "Asia/Shanghai", row.TimeZone)
}
