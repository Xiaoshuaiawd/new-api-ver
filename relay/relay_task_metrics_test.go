package relay

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	prometheusmetrics "github.com/QuantumNous/new-api/pkg/prometheus_metrics"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/service"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupRealtimeTaskMetricsTest(t *testing.T) (*gorm.DB, *prometheusmetrics.Runtime) {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Task{}))
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	service.InitHttpClient()

	runtime, err := prometheusmetrics.NewRuntime(
		prometheusmetrics.Config{Enabled: true, AllowPublic: true},
		"v-test",
		nil,
		nil,
	)
	require.NoError(t, err)
	prometheusmetrics.SetDefaultRuntime(runtime)
	t.Cleanup(func() {
		prometheusmetrics.SetDefaultRuntime(nil)
		common.RedisEnabled = previousRedisEnabled
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db, runtime
}

func scrapeRealtimeTaskMetrics(t *testing.T, runtime *prometheusmetrics.Runtime) string {
	t.Helper()
	response := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	return response.Body.String()
}

func TestRealtimeTaskFetchRecordsFirstTerminalCASOnce(t *testing.T) {
	db, runtime := setupRealtimeTaskMetricsTest(t)
	operationName := "models/veo-3.0-generate-001/operations/operation-1"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"name":"` + operationName + `","done":true}`))
		require.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	const channelID = 950_000_001
	require.NoError(t, db.Create(&model.Channel{
		Id:      channelID,
		Type:    constant.ChannelTypeGemini,
		Name:    "realtime-task-success",
		Key:     "gemini-key",
		Status:  common.ChannelStatusEnabled,
		BaseURL: common.GetPointer(upstream.URL),
	}).Error)
	task := &model.Task{
		TaskID:     "public-realtime-success",
		Platform:   constant.TaskPlatform(fmt.Sprintf("%d", constant.ChannelTypeGemini)),
		ChannelId:  channelID,
		Status:     model.TaskStatusSubmitted,
		Progress:   "10%",
		SubmitTime: time.Now().Add(-time.Minute).Unix(),
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: taskcommon.EncodeLocalTaskID(operationName),
		},
	}
	require.NoError(t, db.Create(task).Error)
	var firstFetch model.Task
	var staleSecondFetch model.Task
	require.NoError(t, db.First(&firstFetch, task.ID).Error)
	require.NoError(t, db.First(&staleSecondFetch, task.ID).Error)

	response := tryRealtimeFetch(&firstFetch, false)
	staleResponse := tryRealtimeFetch(&staleSecondFetch, false)

	require.NotEmpty(t, response)
	require.NotEmpty(t, staleResponse)
	metrics := scrapeRealtimeTaskMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_task_poll_total{platform="video",result="success"} 2`)
	assert.Contains(t, metrics, `newapi_task_completions_total{platform="video",result="success"} 1`)
	assert.Contains(t, metrics, `newapi_task_duration_seconds_count{platform="video",result="success"} 1`)
	assert.NotContains(t, metrics, `newapi_task_poll_total{platform="video",result="error"}`)
}

func TestRealtimeTaskFetchRecordsParseError(t *testing.T) {
	db, runtime := setupRealtimeTaskMetricsTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"invalid"`))
		require.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	const channelID = 950_000_002
	require.NoError(t, db.Create(&model.Channel{
		Id:      channelID,
		Type:    constant.ChannelTypeGemini,
		Name:    "realtime-task-error",
		Key:     "gemini-key",
		Status:  common.ChannelStatusEnabled,
		BaseURL: common.GetPointer(upstream.URL),
	}).Error)
	task := &model.Task{
		TaskID:    "public-realtime-error",
		Platform:  constant.TaskPlatform(fmt.Sprintf("%d", constant.ChannelTypeGemini)),
		ChannelId: channelID,
		Status:    model.TaskStatusSubmitted,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: taskcommon.EncodeLocalTaskID("models/veo-3.0-generate-001/operations/operation-2"),
		},
	}
	require.NoError(t, db.Create(task).Error)

	response := tryRealtimeFetch(task, false)

	assert.Empty(t, response)
	metrics := scrapeRealtimeTaskMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_task_poll_total{platform="video",result="error"} 1`)
	assert.NotContains(t, metrics, `newapi_task_poll_total{platform="video",result="success"}`)
}

func TestRealtimeTaskFetchDoesNotRecordUnsupportedChannel(t *testing.T) {
	db, runtime := setupRealtimeTaskMetricsTest(t)

	const channelID = 950_000_003
	require.NoError(t, db.Create(&model.Channel{
		Id:     channelID,
		Type:   constant.ChannelTypeKling,
		Name:   "realtime-task-unsupported",
		Key:    "kling-key",
		Status: common.ChannelStatusEnabled,
	}).Error)
	task := &model.Task{TaskID: "public-realtime-unsupported", ChannelId: channelID}

	assert.Empty(t, tryRealtimeFetch(task, false))
	assert.NotContains(t, scrapeRealtimeTaskMetrics(t, runtime), "newapi_task_poll_total")
}
