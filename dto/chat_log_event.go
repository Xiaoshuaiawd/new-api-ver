package dto

import "time"

// ChatLogEvent is the unified async event for chat invocation storage.
type ChatLogEvent struct {
	EventID string `json:"event_id"`

	UserID string `json:"user_id"`

	CreatedAt   time.Time `json:"created_at"`
	CreatedDate string    `json:"created_date"`
	TimeZone    string    `json:"time_zone"`

	ConversationID string `json:"conversation_id"`
	ModelName      string `json:"model_name"`
	MessageID      string `json:"message_id"`
	ChannelID      string `json:"channel_id"`

	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	IsStream     bool `json:"is_stream"`
	MessageRound int  `json:"message_round"`
	IsTools      bool `json:"is_tools"`

	Provider          string `json:"provider"`
	RequestID         string `json:"request_id"`
	ProviderRequestID string `json:"provider_request_id"`

	StatusCode   int    `json:"status_code"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
	LatencyMS    int    `json:"latency_ms"`

	RawRequest         string `json:"raw_request"`
	RawResponse        string `json:"raw_response"`
	MergedTimeline     string `json:"merged_timeline"`
	ToolTrace          string `json:"tool_trace"`
	StreamChunks       string `json:"stream_chunks"`
	NormalizedResponse string `json:"normalized_response"`
	FinalAnswerText    string `json:"final_answer_text"`
	FinalMergedJSON    string `json:"final_merged_json"`
	ToolCallsMerged    string `json:"tool_calls_merged"`
	StreamMerged       bool   `json:"stream_merged"`

	PayloadBytes  int64  `json:"payload_bytes"`
	PayloadSHA256 string `json:"payload_sha256"`
	StorageRef    string `json:"storage_ref"`

	Success bool `json:"success"`
	Retry   int  `json:"retry"`
}
