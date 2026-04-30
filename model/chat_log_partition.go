package model

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

const (
	chatLogSuccessPrefix = "chat_log_success_"
	chatLogFailPrefix    = "chat_log_fail_"
)

var (
	chatLogLoc              *time.Location
	chatLogLocOnce          sync.Once
	chatLogTableReady       sync.Map // map[string]struct{}
	chatLogTableEnsureLocks sync.Map // map[string]*sync.Mutex
)

// ChatLogRecord is the physical row stored in partition tables.
type ChatLogRecord struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`

	UserID string `gorm:"column:user_id"`

	CreatedAt   string `gorm:"column:created_at"`
	CreatedDate string `gorm:"column:created_date"`
	TimeZone    string `gorm:"column:time_zone"`

	ConversationID string `gorm:"column:conversation_id"`
	ModelName      string `gorm:"column:model_name"`
	MessageID      string `gorm:"column:message_id"`
	ChannelID      string `gorm:"column:channel_id"`

	PromptTokens     int `gorm:"column:prompt_tokens"`
	CompletionTokens int `gorm:"column:completion_tokens"`
	TotalTokens      int `gorm:"column:total_tokens"`

	IsStream     bool `gorm:"column:is_stream"`
	MessageRound int  `gorm:"column:message_round"`
	IsTools      bool `gorm:"column:is_tools"`

	Provider          string `gorm:"column:provider"`
	RequestID         string `gorm:"column:request_id"`
	ProviderRequestID string `gorm:"column:provider_request_id"`

	StatusCode   int    `gorm:"column:status_code"`
	ErrorCode    string `gorm:"column:error_code"`
	ErrorMessage string `gorm:"column:error_message"`
	LatencyMS    int    `gorm:"column:latency_ms"`

	RawRequest         string `gorm:"column:raw_request"`
	RawResponse        string `gorm:"column:raw_response"`
	MergedTimeline     string `gorm:"column:merged_timeline"`
	ToolTrace          string `gorm:"column:tool_trace"`
	StreamChunks       string `gorm:"column:stream_chunks"`
	NormalizedResponse string `gorm:"column:normalized_response"`
	FinalAnswerText    string `gorm:"column:final_answer_text"`
	FinalMergedJSON    string `gorm:"column:final_merged_json"`
	ToolCallsMerged    string `gorm:"column:tool_calls_merged"`
	StreamMerged       bool   `gorm:"column:stream_merged"`

	PayloadBytes  int64  `gorm:"column:payload_bytes"`
	PayloadSHA256 string `gorm:"column:payload_sha256"`
	StorageRef    string `gorm:"column:storage_ref"`
}

func getChatLogLocation() *time.Location {
	chatLogLocOnce.Do(func() {
		loc, err := time.LoadLocation(common.ChatLogTimeZone)
		if err != nil {
			common.SysError(fmt.Sprintf("invalid CHAT_LOG_TIMEZONE=%s, fallback to Asia/Shanghai: %v", common.ChatLogTimeZone, err))
			loc = time.FixedZone("CST", 8*3600)
		}
		chatLogLoc = loc
	})
	return chatLogLoc
}

func chatLogCurrentDBType() string {
	chatLogDB := getChatLogDB()
	if chatLogDB != nil {
		if chatLogDB != LOG_DB {
			return common.ChatLogSqlType
		}
		if chatLogDB == LOG_DB && os.Getenv("LOG_SQL_DSN") != "" && getChatLogDSNEnvName() == "" {
			return common.LogSqlType
		}
	}
	if common.UsingPostgreSQL {
		return common.DatabaseTypePostgreSQL
	}
	if common.UsingMySQL {
		return common.DatabaseTypeMySQL
	}
	return common.DatabaseTypeSQLite
}

func getChatLogDB() *gorm.DB {
	if CHAT_LOG_DB != nil {
		return CHAT_LOG_DB
	}
	return LOG_DB
}

func ChatLogTableName(success bool, ts time.Time) string {
	loc := getChatLogLocation()
	suffix := ts.In(loc).Format("20060102")
	if success {
		return chatLogSuccessPrefix + suffix
	}
	return chatLogFailPrefix + suffix
}

func chatLogDBTimeString(ts time.Time) string {
	return ts.In(getChatLogLocation()).Format("2006-01-02 15:04:05.000")
}

func chatLogDBDateString(ts time.Time) string {
	return ts.In(getChatLogLocation()).Format("2006-01-02")
}

func EnsureChatLogTablesByTime(ts time.Time) error {
	if err := ensureChatLogTableFast(ChatLogTableName(true, ts)); err != nil {
		return err
	}
	if err := ensureChatLogTableFast(ChatLogTableName(false, ts)); err != nil {
		return err
	}
	return nil
}

func EnsureChatLogTable(tableName string) error {
	chatLogDB := getChatLogDB()
	if chatLogDB == nil {
		return errors.New("chat log db is nil")
	}
	if !chatLogDB.Migrator().HasTable(tableName) {
		sql := chatLogCreateTableSQL(tableName)
		if err := chatLogDB.Exec(sql).Error; err != nil {
			return err
		}
	}
	if err := ensureChatLogColumns(tableName); err != nil {
		return err
	}
	return ensureChatLogIndexes(tableName)
}

func ensureChatLogTableFast(tableName string) error {
	if _, ok := chatLogTableReady.Load(tableName); ok {
		return nil
	}
	lockVal, _ := chatLogTableEnsureLocks.LoadOrStore(tableName, &sync.Mutex{})
	lock := lockVal.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	if _, ok := chatLogTableReady.Load(tableName); ok {
		return nil
	}
	if err := EnsureChatLogTable(tableName); err != nil {
		return err
	}
	chatLogTableReady.Store(tableName, struct{}{})
	return nil
}

func chatLogCreateTableSQL(tableName string) string {
	switch chatLogCurrentDBType() {
	case common.DatabaseTypePostgreSQL:
		return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS "%s" (
			id BIGSERIAL PRIMARY KEY,
			user_id VARCHAR(64) NOT NULL,
			created_at TIMESTAMP(3) NOT NULL,
			created_date DATE NOT NULL,
			time_zone VARCHAR(64) NOT NULL DEFAULT 'Asia/Shanghai',
			conversation_id VARCHAR(64) NOT NULL,
			model_name VARCHAR(128) NOT NULL,
			message_id VARCHAR(64) NOT NULL,
			channel_id VARCHAR(64) NOT NULL,
			prompt_tokens INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			is_stream BOOLEAN NOT NULL DEFAULT false,
			message_round INTEGER NOT NULL DEFAULT 0,
			is_tools BOOLEAN NOT NULL DEFAULT false,
			provider VARCHAR(32) NOT NULL,
			request_id VARCHAR(64) NOT NULL,
			provider_request_id VARCHAR(128),
			status_code INTEGER,
			error_code VARCHAR(64),
			error_message TEXT,
			latency_ms INTEGER,
			raw_request TEXT,
			raw_response TEXT,
			merged_timeline TEXT,
			tool_trace TEXT,
			stream_chunks TEXT,
			normalized_response TEXT,
			final_answer_text TEXT,
			final_merged_json TEXT,
			tool_calls_merged TEXT,
			stream_merged BOOLEAN NOT NULL DEFAULT false,
			payload_bytes BIGINT,
			payload_sha256 CHAR(64),
			storage_ref VARCHAR(512),
			CONSTRAINT %s UNIQUE (request_id)
		)`, tableName, tableName+"_uniq_request_id")
	case common.DatabaseTypeMySQL:
		return fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s` (\n"+
			"`id` BIGINT NOT NULL AUTO_INCREMENT,\n"+
			"`user_id` VARCHAR(64) NOT NULL,\n"+
			"`created_at` DATETIME(3) NOT NULL,\n"+
			"`created_date` DATE NOT NULL,\n"+
			"`time_zone` VARCHAR(64) NOT NULL DEFAULT 'Asia/Shanghai',\n"+
			"`conversation_id` VARCHAR(64) NOT NULL,\n"+
			"`model_name` VARCHAR(128) NOT NULL,\n"+
			"`message_id` VARCHAR(64) NOT NULL,\n"+
			"`channel_id` VARCHAR(64) NOT NULL,\n"+
			"`prompt_tokens` INT NOT NULL DEFAULT 0,\n"+
			"`completion_tokens` INT NOT NULL DEFAULT 0,\n"+
			"`total_tokens` INT NOT NULL DEFAULT 0,\n"+
			"`is_stream` TINYINT(1) NOT NULL DEFAULT 0,\n"+
			"`message_round` INT NOT NULL DEFAULT 0,\n"+
			"`is_tools` TINYINT(1) NOT NULL DEFAULT 0,\n"+
			"`provider` VARCHAR(32) NOT NULL,\n"+
			"`request_id` VARCHAR(64) NOT NULL,\n"+
			"`provider_request_id` VARCHAR(128) DEFAULT NULL,\n"+
			"`status_code` INT DEFAULT NULL,\n"+
			"`error_code` VARCHAR(64) DEFAULT NULL,\n"+
			"`error_message` TEXT,\n"+
			"`latency_ms` INT DEFAULT NULL,\n"+
			"`raw_request` LONGTEXT,\n"+
			"`raw_response` LONGTEXT,\n"+
			"`merged_timeline` LONGTEXT,\n"+
			"`tool_trace` LONGTEXT,\n"+
			"`stream_chunks` LONGTEXT,\n"+
			"`normalized_response` LONGTEXT,\n"+
			"`final_answer_text` LONGTEXT,\n"+
			"`final_merged_json` LONGTEXT,\n"+
			"`tool_calls_merged` LONGTEXT,\n"+
			"`stream_merged` TINYINT(1) NOT NULL DEFAULT 0,\n"+
			"`payload_bytes` BIGINT DEFAULT NULL,\n"+
			"`payload_sha256` CHAR(64) DEFAULT NULL,\n"+
			"`storage_ref` VARCHAR(512) DEFAULT NULL,\n"+
			"PRIMARY KEY (`id`),\n"+
			"UNIQUE KEY `uniq_request_id` (`request_id`)\n"+
			") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4", tableName)
	default:
		return fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s` (\n"+
			"`id` INTEGER PRIMARY KEY AUTOINCREMENT,\n"+
			"`user_id` TEXT NOT NULL,\n"+
			"`created_at` TEXT NOT NULL,\n"+
			"`created_date` TEXT NOT NULL,\n"+
			"`time_zone` TEXT NOT NULL DEFAULT 'Asia/Shanghai',\n"+
			"`conversation_id` TEXT NOT NULL,\n"+
			"`model_name` TEXT NOT NULL,\n"+
			"`message_id` TEXT NOT NULL,\n"+
			"`channel_id` TEXT NOT NULL,\n"+
			"`prompt_tokens` INTEGER NOT NULL DEFAULT 0,\n"+
			"`completion_tokens` INTEGER NOT NULL DEFAULT 0,\n"+
			"`total_tokens` INTEGER NOT NULL DEFAULT 0,\n"+
			"`is_stream` INTEGER NOT NULL DEFAULT 0,\n"+
			"`message_round` INTEGER NOT NULL DEFAULT 0,\n"+
			"`is_tools` INTEGER NOT NULL DEFAULT 0,\n"+
			"`provider` TEXT NOT NULL,\n"+
			"`request_id` TEXT NOT NULL UNIQUE,\n"+
			"`provider_request_id` TEXT,\n"+
			"`status_code` INTEGER,\n"+
			"`error_code` TEXT,\n"+
			"`error_message` TEXT,\n"+
			"`latency_ms` INTEGER,\n"+
			"`raw_request` TEXT,\n"+
			"`raw_response` TEXT,\n"+
			"`merged_timeline` TEXT,\n"+
			"`tool_trace` TEXT,\n"+
			"`stream_chunks` TEXT,\n"+
			"`normalized_response` TEXT,\n"+
			"`final_answer_text` TEXT,\n"+
			"`final_merged_json` TEXT,\n"+
			"`tool_calls_merged` TEXT,\n"+
			"`stream_merged` INTEGER NOT NULL DEFAULT 0,\n"+
			"`payload_bytes` INTEGER,\n"+
			"`payload_sha256` TEXT,\n"+
			"`storage_ref` TEXT\n"+
			")", tableName)
	}
}

func ensureChatLogColumns(tableName string) error {
	chatLogDB := getChatLogDB()
	if chatLogDB == nil {
		return errors.New("chat log db is nil")
	}
	columns := chatLogColumnDefinitions(tableName)
	silentDB := chatLogDB.Session(&gorm.Session{Logger: chatLogDB.Logger.LogMode(gormlogger.Silent)})
	for _, column := range columns {
		if strings.TrimSpace(column.Statement) == "" || strings.TrimSpace(column.Name) == "" {
			continue
		}
		// Avoid Migrator.HasColumn(tableName, ...) because some GORM versions may panic with string table names.
		// We execute in silent logger mode and treat duplicate-column errors as ignorable for idempotency.
		if err := silentDB.Exec(column.Statement).Error; err != nil {
			if isIgnorableColumnError(err) {
				continue
			}
			return err
		}
	}
	return nil
}

type chatLogColumnDefinition struct {
	Name      string
	Statement string
}

func chatLogColumnDefinitions(tableName string) []chatLogColumnDefinition {
	switch chatLogCurrentDBType() {
	case common.DatabaseTypeMySQL:
		return []chatLogColumnDefinition{
			{
				Name:      "final_merged_json",
				Statement: fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN `final_merged_json` LONGTEXT", tableName),
			},
		}
	case common.DatabaseTypePostgreSQL:
		return []chatLogColumnDefinition{
			{
				Name:      "final_merged_json",
				Statement: fmt.Sprintf("ALTER TABLE \"%s\" ADD COLUMN \"final_merged_json\" TEXT", tableName),
			},
		}
	default:
		return []chatLogColumnDefinition{
			{
				Name:      "final_merged_json",
				Statement: fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN `final_merged_json` TEXT", tableName),
			},
		}
	}
}

func ensureChatLogIndexes(tableName string) error {
	chatLogDB := getChatLogDB()
	if chatLogDB == nil {
		return errors.New("chat log db is nil")
	}
	stmts := chatLogIndexStatements(tableName)
	for _, stmt := range stmts {
		if err := chatLogDB.Exec(stmt).Error; err != nil {
			if isIgnorableIndexError(err) {
				continue
			}
			return err
		}
	}
	return nil
}

func chatLogIndexStatements(tableName string) []string {
	switch chatLogCurrentDBType() {
	case common.DatabaseTypeMySQL:
		return []string{
			fmt.Sprintf("ALTER TABLE `%s` ADD INDEX `idx_user_time` (`user_id`, `created_at`)", tableName),
			fmt.Sprintf("ALTER TABLE `%s` ADD INDEX `idx_conv_round` (`conversation_id`, `message_round`)", tableName),
			fmt.Sprintf("ALTER TABLE `%s` ADD INDEX `idx_message_id` (`message_id`)", tableName),
			fmt.Sprintf("ALTER TABLE `%s` ADD INDEX `idx_channel_time` (`channel_id`, `created_at`)", tableName),
			fmt.Sprintf("ALTER TABLE `%s` ADD INDEX `idx_model_time` (`model_name`, `created_at`)", tableName),
		}
	case common.DatabaseTypePostgreSQL:
		return []string{
			fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_user_time ON \"%s\" (user_id, created_at)", tableName),
			fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_conv_round ON \"%s\" (conversation_id, message_round)", tableName),
			fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_message_id ON \"%s\" (message_id)", tableName),
			fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_channel_time ON \"%s\" (channel_id, created_at)", tableName),
			fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_model_time ON \"%s\" (model_name, created_at)", tableName),
		}
	default:
		return []string{
			fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_user_time ON `%s` (user_id, created_at)", tableName),
			fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_conv_round ON `%s` (conversation_id, message_round)", tableName),
			fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_message_id ON `%s` (message_id)", tableName),
			fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_channel_time ON `%s` (channel_id, created_at)", tableName),
			fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_model_time ON `%s` (model_name, created_at)", tableName),
		}
	}
}

func isIgnorableIndexError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "duplicate key name") ||
		strings.Contains(errStr, "already exists") ||
		strings.Contains(errStr, "relation") && strings.Contains(errStr, "already exists")
}

func isIgnorableColumnError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "duplicate column") ||
		strings.Contains(errStr, "duplicate column name") ||
		(strings.Contains(errStr, "column") && strings.Contains(errStr, "already exists"))
}

func UpsertChatLogEvent(event *dto.ChatLogEvent) error {
	if event == nil {
		return errors.New("chat log event is nil")
	}
	if event.RequestID == "" {
		return errors.New("chat log request_id is empty")
	}
	loc := getChatLogLocation()
	createdAt := event.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().In(loc)
	} else {
		createdAt = createdAt.In(loc)
	}
	createdAtStr := chatLogDBTimeString(createdAt)
	createdDate := chatLogDBDateString(createdAt)
	if event.TimeZone == "" {
		event.TimeZone = common.ChatLogTimeZone
	}
	tableName := ChatLogTableName(event.Success, createdAt)
	if err := ensureChatLogTableFast(tableName); err != nil {
		return err
	}

	rec := &ChatLogRecord{
		UserID:             event.UserID,
		CreatedAt:          createdAtStr,
		CreatedDate:        createdDate,
		TimeZone:           event.TimeZone,
		ConversationID:     event.ConversationID,
		ModelName:          event.ModelName,
		MessageID:          event.MessageID,
		ChannelID:          event.ChannelID,
		PromptTokens:       event.PromptTokens,
		CompletionTokens:   event.CompletionTokens,
		TotalTokens:        event.TotalTokens,
		IsStream:           event.IsStream,
		MessageRound:       event.MessageRound,
		IsTools:            event.IsTools,
		Provider:           event.Provider,
		RequestID:          event.RequestID,
		ProviderRequestID:  event.ProviderRequestID,
		StatusCode:         event.StatusCode,
		ErrorCode:          event.ErrorCode,
		ErrorMessage:       event.ErrorMessage,
		LatencyMS:          event.LatencyMS,
		RawRequest:         event.RawRequest,
		RawResponse:        event.RawResponse,
		MergedTimeline:     event.MergedTimeline,
		ToolTrace:          event.ToolTrace,
		StreamChunks:       event.StreamChunks,
		NormalizedResponse: event.NormalizedResponse,
		FinalAnswerText:    event.FinalAnswerText,
		FinalMergedJSON:    event.FinalMergedJSON,
		ToolCallsMerged:    event.ToolCallsMerged,
		StreamMerged:       event.StreamMerged,
		PayloadBytes:       event.PayloadBytes,
		PayloadSHA256:      event.PayloadSHA256,
		StorageRef:         event.StorageRef,
	}

	updateCols := []string{
		"user_id", "created_at", "created_date", "time_zone",
		"conversation_id", "model_name", "message_id", "channel_id",
		"prompt_tokens", "completion_tokens", "total_tokens",
		"is_stream", "message_round", "is_tools",
		"provider", "provider_request_id", "status_code",
		"error_code", "error_message", "latency_ms",
		"raw_request", "raw_response", "merged_timeline", "tool_trace",
		"stream_chunks", "normalized_response", "final_answer_text",
		"final_merged_json", "tool_calls_merged", "stream_merged",
		"payload_bytes", "payload_sha256", "storage_ref",
	}

	chatLogDB := getChatLogDB()
	if chatLogDB == nil {
		return errors.New("chat log db is nil")
	}

	return chatLogDB.Table(tableName).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "request_id"}},
			DoUpdates: clause.AssignmentColumns(updateCols),
		}).
		Create(rec).Error
}
