package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMidjourneyPollingRecordsSuccessfulUpstreamQuery(t *testing.T) {
	service.InitHttpClient()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Midjourney{}))
	runtime := newRelayMetricsRuntime(t)
	submitTime := time.Now().Add(-time.Minute).UnixMilli()
	finishTime := submitTime + int64(time.Minute/time.Millisecond)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/mj/task/list-by-condition", r.URL.Path)
		body, err := common.Marshal([]dto.MidjourneyDto{{
			MjId:       "mj-poll-success",
			Status:     string(model.TaskStatusSuccess),
			Progress:   "100%",
			SubmitTime: submitTime,
			FinishTime: finishTime,
		}})
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, err = w.Write(body)
		require.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	const channelID = 940_000_001
	require.NoError(t, db.Create(&model.Channel{
		Id:      channelID,
		Type:    constant.ChannelTypeMidjourney,
		Name:    "mj-poll-success",
		Key:     "sk-mj-test",
		Status:  common.ChannelStatusEnabled,
		BaseURL: common.GetPointer(upstream.URL),
	}).Error)
	require.NoError(t, db.Create(&model.Midjourney{
		MjId:       "mj-poll-success",
		ChannelId:  channelID,
		Status:     string(model.TaskStatusSubmitted),
		Progress:   "10%",
		SubmitTime: submitTime,
	}).Error)

	runMidjourneyTaskUpdateOnce(t.Context(), nil)

	metrics := scrapeRelayMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_task_poll_total{platform="midjourney",result="success"} 1`)
	assert.Contains(t, metrics, `newapi_task_completions_total{platform="midjourney",result="success"} 1`)
	assert.Contains(t, metrics, `newapi_task_duration_seconds_count{platform="midjourney",result="success"} 1`)
	assert.NotContains(t, metrics, `newapi_task_poll_total{platform="midjourney",result="error"}`)
}

func TestMidjourneyPollingRecordsHTTPError(t *testing.T) {
	service.InitHttpClient()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Midjourney{}))
	runtime := newRelayMetricsRuntime(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(upstream.Close)

	const channelID = 940_000_002
	require.NoError(t, db.Create(&model.Channel{
		Id:      channelID,
		Type:    constant.ChannelTypeMidjourney,
		Name:    "mj-poll-error",
		Key:     "sk-mj-test",
		Status:  common.ChannelStatusEnabled,
		BaseURL: common.GetPointer(upstream.URL),
	}).Error)
	require.NoError(t, db.Create(&model.Midjourney{
		MjId:       "mj-poll-error",
		ChannelId:  channelID,
		Status:     string(model.TaskStatusSubmitted),
		Progress:   "10%",
		SubmitTime: time.Now().Add(-time.Minute).UnixMilli(),
	}).Error)

	runMidjourneyTaskUpdateOnce(t.Context(), nil)

	metrics := scrapeRelayMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_task_poll_total{platform="midjourney",result="error"} 1`)
	assert.NotContains(t, metrics, `newapi_task_poll_total{platform="midjourney",result="success"}`)
}

func TestMidjourneyPollingDoesNotRecordPollForEmptyQueue(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Midjourney{}))
	runtime := newRelayMetricsRuntime(t)

	runMidjourneyTaskUpdateOnce(t.Context(), nil)

	assert.NotContains(t, scrapeRelayMetrics(t, runtime), `newapi_task_poll_total{platform="midjourney"`)
}

func TestMidjourneyNotifyAndPollingRaceRecordsCompletionOnce(t *testing.T) {
	service.InitHttpClient()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Midjourney{}))
	runtime := newRelayMetricsRuntime(t)
	submitTime := time.Now().Add(-time.Minute).UnixMilli()
	pollStarted := make(chan struct{})
	releasePoll := make(chan struct{})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(pollStarted)
		<-releasePoll
		body, err := common.Marshal([]dto.MidjourneyDto{{
			MjId:       "mj-notify-poll-race",
			Status:     string(model.TaskStatusSuccess),
			Progress:   "100%",
			SubmitTime: submitTime,
			FinishTime: submitTime + int64(time.Minute/time.Millisecond),
		}})
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, err = w.Write(body)
		require.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	const channelID = 940_000_004
	require.NoError(t, db.Create(&model.Channel{
		Id:      channelID,
		Type:    constant.ChannelTypeMidjourney,
		Name:    "mj-notify-poll-race",
		Key:     "sk-mj-test",
		Status:  common.ChannelStatusEnabled,
		BaseURL: common.GetPointer(upstream.URL),
	}).Error)
	require.NoError(t, db.Create(&model.Midjourney{
		MjId:       "mj-notify-poll-race",
		ChannelId:  channelID,
		Status:     string(model.TaskStatusSubmitted),
		Progress:   "10%",
		SubmitTime: submitTime,
	}).Error)

	pollDone := make(chan struct{})
	go func() {
		defer close(pollDone)
		runMidjourneyTaskUpdateOnce(t.Context(), nil)
	}()
	<-pollStarted

	notifyPayload, err := common.Marshal(dto.MidjourneyDto{
		MjId:       "mj-notify-poll-race",
		Status:     string(model.TaskStatusSuccess),
		Progress:   "100%",
		SubmitTime: submitTime,
		FinishTime: submitTime + int64(30*time.Second/time.Millisecond),
	})
	require.NoError(t, err)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mj/notify", bytes.NewReader(notifyPayload))
	ctx.Request.Header.Set("Content-Type", "application/json")
	assert.Nil(t, relay.RelayMidjourneyNotify(ctx))
	close(releasePoll)
	<-pollDone

	metrics := scrapeRelayMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_task_poll_total{platform="midjourney",result="success"} 1`)
	assert.Contains(t, metrics, `newapi_task_completions_total{platform="midjourney",result="success"} 1`)
	assert.Contains(t, metrics, `newapi_task_duration_seconds_count{platform="midjourney",result="success"} 1`)
}
