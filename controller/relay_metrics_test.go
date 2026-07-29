package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	prometheusmetrics "github.com/QuantumNous/new-api/pkg/prometheus_metrics"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	settingconfig "github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var disableRelayMetricsPerfSampling sync.Once

func configureRelayMetricsFreeModel(t *testing.T, modelName string) {
	t.Helper()

	previousCountToken := constant.CountToken
	previousStreamingTimeout := constant.StreamingTimeout
	previousLogConsume := common.LogConsumeEnabled
	previousFreeModelPreConsume := operation_setting.GetQuotaSetting().EnableFreeModelPreConsume
	previousModelRatio := ratio_setting.ModelRatio2JSONString()
	constant.CountToken = false
	if constant.StreamingTimeout <= 0 {
		constant.StreamingTimeout = 30
	}
	common.LogConsumeEnabled = false
	operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = false
	disableRelayMetricsPerfSampling.Do(func() {
		perfMetricsConfig, ok := settingconfig.GlobalConfig.Get("perf_metrics_setting").(*perf_metrics_setting.PerfMetricsSetting)
		require.True(t, ok)
		perfMetricsConfig.Enabled = false
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"`+modelName+`":0}`))
	service.InitHttpClient()
	t.Cleanup(func() {
		constant.CountToken = previousCountToken
		constant.StreamingTimeout = previousStreamingTimeout
		common.LogConsumeEnabled = previousLogConsume
		operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = previousFreeModelPreConsume
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousModelRatio))
	})
}

type relayMetricsWriteRecorder struct {
	*httptest.ResponseRecorder
	written chan struct{}
	once    sync.Once
}

func (w *relayMetricsWriteRecorder) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.written) })
	return w.ResponseRecorder.Write(data)
}

func (w *relayMetricsWriteRecorder) WriteString(data string) (int, error) {
	w.once.Do(func() { close(w.written) })
	return w.ResponseRecorder.WriteString(data)
}

func newRelayMetricsRuntime(t *testing.T) *prometheusmetrics.Runtime {
	t.Helper()
	runtime, err := prometheusmetrics.NewRuntime(
		prometheusmetrics.Config{Enabled: true, AllowPublic: true},
		"v-test",
		nil,
		nil,
	)
	require.NoError(t, err)
	prometheusmetrics.SetDefaultRuntime(runtime)
	t.Cleanup(func() { prometheusmetrics.SetDefaultRuntime(nil) })
	return runtime
}

func scrapeRelayMetrics(t *testing.T, runtime *prometheusmetrics.Runtime) string {
	t.Helper()
	response := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	return response.Body.String()
}

func setupRelayMetricsChannelContext(t *testing.T, ctx *gin.Context, channel *model.Channel, modelName string) {
	t.Helper()
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "default")
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, channel, modelName))
}

func TestRelayMetricsOutcomeUsesFinalAPIError(t *testing.T) {
	apiErr := types.NewErrorWithStatusCode(
		errors.New("quota exhausted"),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusForbidden,
	)

	outcome := relayMetricsOutcome(nil, apiErr, nil)

	assert.False(t, outcome.Success)
	assert.Equal(t, prometheusmetrics.ErrorTypeInsufficientQuota, prometheusmetrics.ClassifyError(outcome.Error))
}

func TestRelayMetricsOutcomeRequiresNormalErrorFreeStreamEnd(t *testing.T) {
	startedAt := time.Now().Add(-2 * time.Second)
	tests := []struct {
		name          string
		status        *relaycommon.StreamStatus
		success       bool
		wantErrorType string
	}{
		{
			name: "normal stream",
			status: func() *relaycommon.StreamStatus {
				status := relaycommon.NewStreamStatus()
				status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				return status
			}(),
			success: true,
		},
		{
			name: "soft stream error",
			status: func() *relaycommon.StreamStatus {
				status := relaycommon.NewStreamStatus()
				status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				status.RecordError("upstream error event")
				return status
			}(),
			wantErrorType: prometheusmetrics.ErrorTypeInternal,
		},
		{
			name: "normal reason with end error",
			status: func() *relaycommon.StreamStatus {
				status := relaycommon.NewStreamStatus()
				status.SetEndReason(relaycommon.StreamEndReasonDone, context.DeadlineExceeded)
				return status
			}(),
			wantErrorType: prometheusmetrics.ErrorTypeTimeout,
		},
		{
			name: "stream timeout",
			status: func() *relaycommon.StreamStatus {
				status := relaycommon.NewStreamStatus()
				status.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
				return status
			}(),
			wantErrorType: prometheusmetrics.ErrorTypeTimeout,
		},
		{
			name: "client disconnect",
			status: func() *relaycommon.StreamStatus {
				status := relaycommon.NewStreamStatus()
				status.SetEndReason(relaycommon.StreamEndReasonClientGone, context.Canceled)
				return status
			}(),
			wantErrorType: prometheusmetrics.ErrorTypeClientCancelled,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				IsStream:          true,
				StartTime:         startedAt,
				FirstResponseTime: startedAt.Add(250 * time.Millisecond),
				StreamStatus:      test.status,
			}

			outcome := relayMetricsOutcome(nil, nil, info)

			assert.Equal(t, test.success, outcome.Success)
			if test.success {
				assert.Equal(t, 250*time.Millisecond, outcome.TTFT)
			} else {
				assert.Equal(t, test.wantErrorType, prometheusmetrics.ClassifyError(outcome.Error))
			}
		})
	}
}

func TestRelayMetricsOutcomeTreatsMissingStreamStatusAsHandlerResult(t *testing.T) {
	outcome := relayMetricsOutcome(nil, nil, &relaycommon.RelayInfo{IsStream: true})

	require.True(t, outcome.Success)
	assert.Zero(t, outcome.TTFT)
}

func TestRelayMetricsRecordsSuccessfulRequestAndChannelAttemptOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		channelID = 930_000_001
		modelName = "prometheus-success-model"
	)
	configureRelayMetricsFreeModel(t, modelName)
	setupModelListControllerTestDB(t)
	runtime := newRelayMetricsRuntime(t)

	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"chatcmpl-metrics","object":"chat.completion","created":1,"model":"prometheus-success-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`))
		require.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		strings.NewReader(`{"model":"`+modelName+`","messages":[{"role":"user","content":"hello"}]}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	setupRelayMetricsChannelContext(t, ctx, &model.Channel{
		Id:      channelID,
		Type:    constant.ChannelTypeOpenAI,
		Name:    "metrics-success",
		Key:     "sk-test",
		BaseURL: common.GetPointer(upstream.URL),
		AutoBan: common.GetPointer(0),
	}, modelName)

	Relay(ctx, types.RelayFormatOpenAI)

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, 1, upstreamCalls)
	metrics := scrapeRelayMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_relay_requests_total{error_type="none",relay_format="openai",result="success",stream="false"} 1`)
	assert.Contains(t, metrics, `newapi_relay_inflight{relay_format="openai",stream="false"} 0`)
	assert.Contains(t, metrics, `newapi_channel_attempts_total{channel_id="930000001",channel_type="1",error_type="none",result="success",stream="false"} 1`)
	assert.Contains(t, metrics, `newapi_channel_inflight{channel_id="930000001",channel_type="1"} 0`)
	assert.Equal(t, 1, service.GetChannelRuntimeMetrics([]int{channelID})[channelID].RPM)
}

func TestRelayMetricsClassifiesCancelledAndExpiredRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name           string
		channelID      int
		modelName      string
		requestContext func() (context.Context, context.CancelFunc)
		wantErrorType  string
	}{
		{
			name:      "client cancelled",
			channelID: 930_000_005,
			modelName: "prometheus-client-cancelled-model",
			requestContext: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, func() {}
			},
			wantErrorType: prometheusmetrics.ErrorTypeClientCancelled,
		},
		{
			name:      "deadline exceeded",
			channelID: 930_000_006,
			modelName: "prometheus-deadline-model",
			requestContext: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
			wantErrorType: prometheusmetrics.ErrorTypeTimeout,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configureRelayMetricsFreeModel(t, test.modelName)
			setupModelListControllerTestDB(t)
			runtime := newRelayMetricsRuntime(t)
			previousRetryTimes := common.RetryTimes
			common.RetryTimes = 0
			t.Cleanup(func() { common.RetryTimes = previousRetryTimes })

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("cancelled or expired request must not reach the upstream")
				w.WriteHeader(http.StatusInternalServerError)
			}))
			t.Cleanup(upstream.Close)

			requestContext, cancel := test.requestContext()
			t.Cleanup(cancel)
			request := httptest.NewRequest(
				http.MethodPost,
				"/v1/chat/completions",
				strings.NewReader(`{"model":"`+test.modelName+`","messages":[{"role":"user","content":"hello"}]}`),
			).WithContext(requestContext)
			request.Header.Set("Content-Type", "application/json")

			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = request
			setupRelayMetricsChannelContext(t, ctx, &model.Channel{
				Id:      test.channelID,
				Type:    constant.ChannelTypeOpenAI,
				Name:    "metrics-" + test.name,
				Key:     "sk-test",
				BaseURL: common.GetPointer(upstream.URL),
				AutoBan: common.GetPointer(0),
			}, test.modelName)

			Relay(ctx, types.RelayFormatOpenAI)

			metrics := scrapeRelayMetrics(t, runtime)
			assert.Contains(t, metrics, `newapi_relay_requests_total{error_type="`+test.wantErrorType+`",relay_format="openai",result="failure",stream="false"} 1`)
			assert.Contains(t, metrics, `newapi_relay_inflight{relay_format="openai",stream="false"} 0`)
			assert.Contains(t, metrics, `newapi_channel_attempts_total{channel_id="`+fmt.Sprintf("%d", test.channelID)+`",channel_type="1",error_type="`+test.wantErrorType+`",result="failure",stream="false"} 1`)
			assert.Contains(t, metrics, `newapi_channel_inflight{channel_id="`+fmt.Sprintf("%d", test.channelID)+`",channel_type="1"} 0`)
			assert.Zero(t, service.GetChannelRuntimeMetrics([]int{test.channelID})[test.channelID].RPM)
		})
	}
}

func TestRelayMetricsRecordsClientCancelledStreamAsFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		channelID = 930_000_007
		modelName = "prometheus-stream-cancelled-model"
	)
	configureRelayMetricsFreeModel(t, modelName)
	setupModelListControllerTestDB(t)
	runtime := newRelayMetricsRuntime(t)
	previousRetryTimes := common.RetryTimes
	common.RetryTimes = 0
	t.Cleanup(func() { common.RetryTimes = previousRetryTimes })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-cancelled\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"first\"},\"finish_reason\":null}]}\n\n", modelName)
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-cancelled\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"second\"},\"finish_reason\":null}]}\n\n", modelName)
		flusher.Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(upstream.Close)

	requestContext, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	response := &relayMetricsWriteRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		written:          make(chan struct{}),
	}
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		strings.NewReader(`{"model":"`+modelName+`","stream":true,"messages":[{"role":"user","content":"hello"}]}`),
	).WithContext(requestContext)
	ctx.Request.Header.Set("Content-Type", "application/json")
	setupRelayMetricsChannelContext(t, ctx, &model.Channel{
		Id:      channelID,
		Type:    constant.ChannelTypeOpenAI,
		Name:    "metrics-stream-cancelled",
		Key:     "sk-test",
		BaseURL: common.GetPointer(upstream.URL),
		AutoBan: common.GetPointer(0),
	}, modelName)

	relayDone := make(chan struct{})
	go func() {
		Relay(ctx, types.RelayFormatOpenAI)
		close(relayDone)
	}()

	select {
	case <-response.written:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the first downstream stream write")
	}
	cancel()
	select {
	case <-relayDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Relay did not return after client cancellation")
	}

	metrics := scrapeRelayMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_relay_requests_total{error_type="client_cancelled",relay_format="openai",result="failure",stream="true"} 1`)
	assert.Contains(t, metrics, `newapi_relay_inflight{relay_format="openai",stream="true"} 0`)
	assert.Contains(t, metrics, `newapi_channel_attempts_total{channel_id="930000007",channel_type="1",error_type="client_cancelled",result="failure",stream="true"} 1`)
	assert.Contains(t, metrics, `newapi_channel_inflight{channel_id="930000007",channel_type="1"} 0`)
	assert.Contains(t, metrics, `newapi_stream_duration_seconds_count{relay_format="openai",result="failure"} 1`)
	assert.NotContains(t, metrics, `newapi_stream_ttft_seconds_count{relay_format="openai"}`)
	assert.Zero(t, service.GetChannelRuntimeMetrics([]int{channelID})[channelID].RPM)
}

func TestRelayMetricsTreatsCleanStreamEOFAsSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		channelID = 930_000_008
		modelName = "prometheus-stream-eof-model"
	)
	configureRelayMetricsFreeModel(t, modelName)
	setupModelListControllerTestDB(t)
	runtime := newRelayMetricsRuntime(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, err := fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-eof\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"complete\"},\"finish_reason\":\"stop\"}]}\n\n", modelName)
		require.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		strings.NewReader(`{"model":"`+modelName+`","stream":true,"messages":[{"role":"user","content":"hello"}]}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	setupRelayMetricsChannelContext(t, ctx, &model.Channel{
		Id:      channelID,
		Type:    constant.ChannelTypeOpenAI,
		Name:    "metrics-stream-eof",
		Key:     "sk-test",
		BaseURL: common.GetPointer(upstream.URL),
		AutoBan: common.GetPointer(0),
	}, modelName)

	Relay(ctx, types.RelayFormatOpenAI)

	require.Equal(t, http.StatusOK, response.Code)
	metrics := scrapeRelayMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_relay_requests_total{error_type="none",relay_format="openai",result="success",stream="true"} 1`)
	assert.Contains(t, metrics, `newapi_relay_inflight{relay_format="openai",stream="true"} 0`)
	assert.Contains(t, metrics, `newapi_channel_attempts_total{channel_id="930000008",channel_type="1",error_type="none",result="success",stream="true"} 1`)
	assert.Contains(t, metrics, `newapi_channel_inflight{channel_id="930000008",channel_type="1"} 0`)
	assert.Contains(t, metrics, `newapi_stream_duration_seconds_count{relay_format="openai",result="success"} 1`)
	assert.Contains(t, metrics, `newapi_stream_ttft_seconds_count{relay_format="openai"} 1`)
	assert.Equal(t, 1, service.GetChannelRuntimeMetrics([]int{channelID})[channelID].RPM)
}

func TestRelayMetricsRetryRecordsTwoChannelAttemptsAndOneFinalSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		firstChannelID  = 930_000_002
		secondChannelID = 930_000_003
		modelName       = "prometheus-retry-model"
	)
	configureRelayMetricsFreeModel(t, modelName)
	db := setupModelListControllerTestDB(t)
	runtime := newRelayMetricsRuntime(t)

	previousRetryTimes := common.RetryTimes
	previousMemoryCache := common.MemoryCacheEnabled
	previousErrorLog := constant.ErrorLogEnabled
	common.RetryTimes = 1
	common.MemoryCacheEnabled = false
	constant.ErrorLogEnabled = false
	t.Cleanup(func() {
		common.RetryTimes = previousRetryTimes
		common.MemoryCacheEnabled = previousMemoryCache
		constant.ErrorLogEnabled = previousErrorLog
	})

	firstCalls := 0
	firstUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstCalls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte(`{"error":{"message":"temporary upstream failure","type":"server_error"}}`))
		require.NoError(t, err)
	}))
	t.Cleanup(firstUpstream.Close)

	secondCalls := 0
	secondUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondCalls++
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"chatcmpl-retry","object":"chat.completion","created":1,"model":"prometheus-retry-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`))
		require.NoError(t, err)
	}))
	t.Cleanup(secondUpstream.Close)

	priority := int64(0)
	secondChannel := &model.Channel{
		Id:       secondChannelID,
		Type:     constant.ChannelTypeOpenAI,
		Status:   common.ChannelStatusEnabled,
		Name:     "metrics-retry-success",
		Key:      "sk-second",
		BaseURL:  common.GetPointer(secondUpstream.URL),
		Models:   modelName,
		Group:    "default",
		Priority: &priority,
		AutoBan:  common.GetPointer(0),
	}
	require.NoError(t, db.Create(secondChannel).Error)
	require.NoError(t, secondChannel.AddAbilities(db))

	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		strings.NewReader(`{"model":"`+modelName+`","messages":[{"role":"user","content":"retry"}]}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	setupRelayMetricsChannelContext(t, ctx, &model.Channel{
		Id:      firstChannelID,
		Type:    constant.ChannelTypeOpenAI,
		Name:    "metrics-retry-failure",
		Key:     "sk-first",
		BaseURL: common.GetPointer(firstUpstream.URL),
		AutoBan: common.GetPointer(0),
	}, modelName)

	Relay(ctx, types.RelayFormatOpenAI)

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, 1, firstCalls)
	assert.Equal(t, 1, secondCalls)
	metrics := scrapeRelayMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_relay_requests_total{error_type="none",relay_format="openai",result="success",stream="false"} 1`)
	assert.Contains(t, metrics, `newapi_channel_attempts_total{channel_id="930000002",channel_type="1",error_type="upstream_5xx",result="failure",stream="false"} 1`)
	assert.Contains(t, metrics, `newapi_channel_attempts_total{channel_id="930000003",channel_type="1",error_type="none",result="success",stream="false"} 1`)
	assert.Contains(t, metrics, `newapi_channel_retries_total{channel_id="930000003",channel_type="1",reason="upstream_5xx"} 1`)
	assert.Contains(t, metrics, `newapi_channel_inflight{channel_id="930000002",channel_type="1"} 0`)
	assert.Contains(t, metrics, `newapi_channel_inflight{channel_id="930000003",channel_type="1"} 0`)
	assert.Zero(t, service.GetChannelRuntimeMetrics([]int{firstChannelID})[firstChannelID].RPM)
	assert.Equal(t, 1, service.GetChannelRuntimeMetrics([]int{secondChannelID})[secondChannelID].RPM)
}

func TestRelayTaskMetricsRecordsSuccessfulSubmissionAndChannelAttemptOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		channelID = 930_000_004
		modelName = "prometheus-task-model"
	)
	configureRelayMetricsFreeModel(t, modelName)
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	runtime := newRelayMetricsRuntime(t)

	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		assert.Equal(t, "/v1/videos", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"upstream-task-1","object":"video","model":"prometheus-task-model","status":"queued","progress":0,"created_at":1}`))
		require.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/videos",
		strings.NewReader(`{"model":"`+modelName+`","prompt":"make a short clip","seconds":"4","size":"720x1280"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	setupRelayMetricsChannelContext(t, ctx, &model.Channel{
		Id:      channelID,
		Type:    constant.ChannelTypeOpenAI,
		Name:    "metrics-task-success",
		Key:     "sk-task",
		BaseURL: common.GetPointer(upstream.URL),
		AutoBan: common.GetPointer(0),
	}, modelName)

	RelayTask(ctx)

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, 1, upstreamCalls)
	var taskCount int64
	require.NoError(t, db.Model(&model.Task{}).Count(&taskCount).Error)
	assert.Equal(t, int64(1), taskCount)
	metrics := scrapeRelayMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_task_submissions_total{platform="video",result="success"} 1`)
	assert.Contains(t, metrics, `newapi_relay_requests_total{error_type="none",relay_format="task",result="success",stream="false"} 1`)
	assert.Contains(t, metrics, `newapi_relay_inflight{relay_format="task",stream="false"} 0`)
	assert.Contains(t, metrics, `newapi_channel_attempts_total{channel_id="930000004",channel_type="1",error_type="none",result="success",stream="false"} 1`)
	assert.Contains(t, metrics, `newapi_channel_inflight{channel_id="930000004",channel_type="1"} 0`)
	assert.Equal(t, 1, service.GetChannelRuntimeMetrics([]int{channelID})[channelID].RPM)
}

func TestRelayTaskMetricsRecordsLocalInsertFailureAsSubmissionFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		channelID = 930_000_014
		modelName = "prometheus-task-insert-failure-model"
	)
	configureRelayMetricsFreeModel(t, modelName)
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:fail_task_insert", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "tasks" {
			tx.AddError(errors.New("forced task insert failure"))
		}
	}))
	t.Cleanup(func() { db.Callback().Create().Remove("test:fail_task_insert") })
	runtime := newRelayMetricsRuntime(t)

	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"upstream-task-insert-failure","object":"video","model":"prometheus-task-insert-failure-model","status":"queued","progress":0,"created_at":1}`))
		require.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/videos",
		strings.NewReader(`{"model":"`+modelName+`","prompt":"make a short clip","seconds":"4","size":"720x1280"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	setupRelayMetricsChannelContext(t, ctx, &model.Channel{
		Id:      channelID,
		Type:    constant.ChannelTypeOpenAI,
		Name:    "metrics-task-insert-failure",
		Key:     "sk-task",
		BaseURL: common.GetPointer(upstream.URL),
		AutoBan: common.GetPointer(0),
	}, modelName)

	RelayTask(ctx)

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, 1, upstreamCalls)
	var taskCount int64
	require.NoError(t, db.Model(&model.Task{}).Count(&taskCount).Error)
	assert.Zero(t, taskCount)
	metrics := scrapeRelayMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_task_submissions_total{platform="video",result="failure"} 1`)
	assert.NotContains(t, metrics, `newapi_task_submissions_total{platform="video",result="success"}`)
	assert.Contains(t, metrics, `newapi_relay_requests_total{error_type="none",relay_format="task",result="success",stream="false"} 1`)
}

func TestRelayTaskMetricsRecordsUpstreamFailureOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		channelID = 930_000_015
		modelName = "prometheus-task-upstream-failure-model"
	)
	configureRelayMetricsFreeModel(t, modelName)
	setupModelListControllerTestDB(t)
	runtime := newRelayMetricsRuntime(t)
	previousRetryTimes := common.RetryTimes
	common.RetryTimes = 0
	t.Cleanup(func() { common.RetryTimes = previousRetryTimes })

	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte(`{"error":{"message":"upstream failed","type":"server_error"}}`))
		require.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/videos",
		strings.NewReader(`{"model":"`+modelName+`","prompt":"make a short clip","seconds":"4","size":"720x1280"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	setupRelayMetricsChannelContext(t, ctx, &model.Channel{
		Id:      channelID,
		Type:    constant.ChannelTypeOpenAI,
		Name:    "metrics-task-upstream-failure",
		Key:     "sk-task",
		BaseURL: common.GetPointer(upstream.URL),
		AutoBan: common.GetPointer(0),
	}, modelName)

	RelayTask(ctx)

	assert.Equal(t, 1, upstreamCalls)
	metrics := scrapeRelayMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_task_submissions_total{platform="video",result="failure"} 1`)
	assert.NotContains(t, metrics, `newapi_task_submissions_total{platform="video",result="success"}`)
}

func TestRelayRecordsInvalidRequestOnceAndReturnsInflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtime, err := prometheusmetrics.NewRuntime(
		prometheusmetrics.Config{Enabled: true, AllowPublic: true},
		"v-test",
		nil,
		nil,
	)
	require.NoError(t, err)
	prometheusmetrics.SetDefaultRuntime(runtime)
	t.Cleanup(func() { prometheusmetrics.SetDefaultRuntime(nil) })

	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-test","messages":[`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	Relay(ctx, types.RelayFormatOpenAI)

	metricsResponse := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, metricsResponse.Code)
	metrics := metricsResponse.Body.String()
	assert.Contains(t, metrics, "newapi_relay_inflight{relay_format=\"openai\",stream=\"false\"} 0")
	assert.Contains(t, metrics, "newapi_relay_requests_total{error_type=\"invalid_request\",relay_format=\"openai\",result=\"failure\",stream=\"false\"} 1")
	assert.Contains(t, metrics, "newapi_relay_duration_seconds_count{relay_format=\"openai\",result=\"failure\",stream=\"false\"} 1")
}

func TestRelayTaskRecordsEarlyValidationFailureOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtime, err := prometheusmetrics.NewRuntime(
		prometheusmetrics.Config{Enabled: true, AllowPublic: true},
		"v-test",
		nil,
		nil,
	)
	require.NoError(t, err)
	prometheusmetrics.SetDefaultRuntime(runtime)
	t.Cleanup(func() { prometheusmetrics.SetDefaultRuntime(nil) })

	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/missing/remix", nil)

	RelayTask(ctx)

	metricsResponse := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, metricsResponse.Code)
	metrics := metricsResponse.Body.String()
	assert.Contains(t, metrics, `newapi_task_submissions_total{platform="other",result="failure"} 1`)
	assert.Contains(t, metrics, "newapi_relay_inflight{relay_format=\"task\",stream=\"false\"} 0")
	assert.Contains(t, metrics, "newapi_relay_requests_total{error_type=\"invalid_request\",relay_format=\"task\",result=\"failure\",stream=\"false\"} 1")
	assert.Contains(t, metrics, "newapi_relay_duration_seconds_count{relay_format=\"task\",result=\"failure\",stream=\"false\"} 1")
	assert.NotContains(t, metrics, "newapi_channel_attempts_total")
}

func TestRelayMidjourneyMetricsUsesTrackedBusinessOutcome(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtime := newRelayMetricsRuntime(t)

	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/midjourney-metrics-test", strings.NewReader(`{"ids":[]}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("relay_mode", relayconstant.RelayModeMidjourneyTaskFetchByCondition)
	relay.RecordMidjourneyUpstreamSuccess(ctx, false)

	RelayMidjourney(ctx)

	require.Equal(t, http.StatusOK, response.Code)
	metrics := scrapeRelayMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_relay_requests_total{error_type="internal",relay_format="mj_proxy",result="failure",stream="false"} 1`)
	assert.Contains(t, metrics, `newapi_relay_inflight{relay_format="mj_proxy",stream="false"} 0`)
	assert.NotContains(t, metrics, `newapi_relay_requests_total{error_type="none",relay_format="mj_proxy",result="success",stream="false"}`)
	assert.NotContains(t, metrics, `newapi_task_submissions_total{platform="midjourney"`)
}

func TestRelayMidjourneyMetricsRecordsSuccessfulSubmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		channelID = 930_000_016
		userID    = 930_000_017
		modelName = "mj_imagine"
	)
	configureRelayMetricsFreeModel(t, modelName)
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Midjourney{}))
	require.NoError(t, db.Create(&model.User{Id: userID, Username: "mj-metrics-user", Quota: 100_000, Status: common.UserStatusEnabled}).Error)
	runtime := newRelayMetricsRuntime(t)

	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		assert.Equal(t, "/mj/submit/imagine", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"code":1,"description":"success","result":"mj-task-metrics-1"}`))
		require.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/mj/submit/imagine",
		strings.NewReader(`{"prompt":"draw a monitoring dashboard"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("relay_mode", relayconstant.RelayModeMidjourneyImagine)
	common.SetContextKey(ctx, constant.ContextKeyUserId, userID)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 1)
	ctx.Set("token_name", "mj-metrics-token")
	setupRelayMetricsChannelContext(t, ctx, &model.Channel{
		Id:      channelID,
		Type:    constant.ChannelTypeMidjourney,
		Name:    "metrics-midjourney-success",
		Key:     "sk-mj-test",
		BaseURL: common.GetPointer(upstream.URL),
		AutoBan: common.GetPointer(0),
	}, modelName)

	RelayMidjourney(ctx)

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, 1, upstreamCalls)
	var taskCount int64
	require.NoError(t, db.Model(&model.Midjourney{}).Count(&taskCount).Error)
	assert.Equal(t, int64(1), taskCount)
	metrics := scrapeRelayMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_task_submissions_total{platform="midjourney",result="success"} 1`)
	assert.NotContains(t, metrics, `newapi_task_submissions_total{platform="midjourney",result="failure"}`)
}

func TestMidjourneyTaskSubmissionModeClassification(t *testing.T) {
	tests := []struct {
		name string
		mode int
		want bool
	}{
		{name: "unknown defaults to submit", mode: relayconstant.RelayModeUnknown, want: true},
		{name: "imagine", mode: relayconstant.RelayModeMidjourneyImagine, want: true},
		{name: "swap face", mode: relayconstant.RelayModeSwapFace, want: true},
		{name: "notify", mode: relayconstant.RelayModeMidjourneyNotify},
		{name: "fetch", mode: relayconstant.RelayModeMidjourneyTaskFetch},
		{name: "fetch by condition", mode: relayconstant.RelayModeMidjourneyTaskFetchByCondition},
		{name: "image seed", mode: relayconstant.RelayModeMidjourneyTaskImageSeed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, isMidjourneyTaskSubmissionMode(test.mode))
		})
	}
}

func TestRelayControllersRecordPanicAndReturnInflightBeforeRethrowing(t *testing.T) {
	tests := []struct {
		name        string
		relayFormat string
		invoke      func(*gin.Context)
	}{
		{
			name:        "ordinary Relay",
			relayFormat: "openai",
			invoke: func(ctx *gin.Context) {
				Relay(ctx, types.RelayFormatOpenAI)
			},
		},
		{name: "task Relay", relayFormat: "task", invoke: RelayTask},
		{name: "Midjourney Relay", relayFormat: "mj_proxy", invoke: RelayMidjourney},
	}

	gin.SetMode(gin.TestMode)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, err := prometheusmetrics.NewRuntime(
				prometheusmetrics.Config{Enabled: true, AllowPublic: true},
				"v-test",
				nil,
				nil,
			)
			require.NoError(t, err)
			prometheusmetrics.SetDefaultRuntime(runtime)
			t.Cleanup(func() { prometheusmetrics.SetDefaultRuntime(nil) })

			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			assert.Panics(t, func() { test.invoke(ctx) })

			metricsResponse := httptest.NewRecorder()
			runtime.Handler.ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			require.Equal(t, http.StatusOK, metricsResponse.Code)
			metrics := metricsResponse.Body.String()
			assert.Contains(t, metrics, "newapi_relay_inflight{relay_format=\""+test.relayFormat+"\",stream=\"false\"} 0")
			assert.Contains(t, metrics, "newapi_relay_requests_total{error_type=\"internal\",relay_format=\""+test.relayFormat+"\",result=\"failure\",stream=\"false\"} 1")
			assert.Contains(t, metrics, "newapi_relay_duration_seconds_count{relay_format=\""+test.relayFormat+"\",result=\"failure\",stream=\"false\"} 1")
			if test.relayFormat == "task" {
				assert.Contains(t, metrics, `newapi_task_submissions_total{platform="other",result="failure"} 1`)
			}
			if test.relayFormat == "mj_proxy" {
				assert.Contains(t, metrics, `newapi_task_submissions_total{platform="midjourney",result="failure"} 1`)
			}
		})
	}
}
