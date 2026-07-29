package relay

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	prometheusmetrics "github.com/QuantumNous/new-api/pkg/prometheus_metrics"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoTrackedMidjourneyHttpRequestRecordsBusinessSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	tests := []struct {
		name    string
		code    int
		channel int
		wantRPM int
	}{
		{name: "successful response", code: 1, channel: 910_000_001, wantRPM: 1},
		{name: "failed response", code: 23, channel: 910_000_002, wantRPM: 0},
	}

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

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"code":%d,"description":"test"}`, test.code)
			}))
			defer server.Close()

			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodGet, "/mj/test", nil)
			ctx.Set("channel_id", test.channel)
			ctx.Set("channel_type", 17)

			response, _, err := doTrackedMidjourneyHttpRequest(ctx, time.Second, server.URL, func(response *dto.MidjourneyResponseWithStatusCode) bool {
				return response.Response.Code == 1
			})

			require.NoError(t, err)
			require.NotNil(t, response)
			upstreamSucceeded, recorded := MidjourneyUpstreamSucceeded(ctx)
			require.True(t, recorded)
			assert.Equal(t, test.wantRPM == 1, upstreamSucceeded)
			metrics := service.GetChannelRuntimeMetrics([]int{test.channel})[test.channel]
			assert.Zero(t, metrics.Concurrency)
			assert.Equal(t, test.wantRPM, metrics.RPM)
			prometheusOutput := scrapeMidjourneyPrometheusMetrics(t, runtime)
			if test.wantRPM == 1 {
				assert.Contains(t, prometheusOutput, fmt.Sprintf(
					"newapi_channel_attempts_total{channel_id=\"%d\",channel_type=\"17\",error_type=\"none\",result=\"success\",stream=\"false\"} 1",
					test.channel,
				))
			} else {
				assert.Contains(t, prometheusOutput, fmt.Sprintf(
					"newapi_channel_attempts_total{channel_id=\"%d\",channel_type=\"17\",error_type=\"internal\",result=\"failure\",stream=\"false\"} 1",
					test.channel,
				))
			}
		})
	}
}

func TestMidjourneyBusinessSuccessBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		response  *dto.MidjourneyResponseWithStatusCode
		succeeded func(*dto.MidjourneyResponseWithStatusCode) bool
		want      bool
	}{
		{name: "submit code 1", response: &dto.MidjourneyResponseWithStatusCode{Response: dto.MidjourneyResponse{Code: 1}}, succeeded: midjourneySubmitSucceeded, want: true},
		{name: "submit code 21", response: &dto.MidjourneyResponseWithStatusCode{Response: dto.MidjourneyResponse{Code: 21}}, succeeded: midjourneySubmitSucceeded, want: true},
		{name: "submit code 22", response: &dto.MidjourneyResponseWithStatusCode{Response: dto.MidjourneyResponse{Code: 22}}, succeeded: midjourneySubmitSucceeded, want: true},
		{name: "submit code 23", response: &dto.MidjourneyResponseWithStatusCode{Response: dto.MidjourneyResponse{Code: 23}}, succeeded: midjourneySubmitSucceeded, want: false},
		{name: "swap face 200 and code 1", response: &dto.MidjourneyResponseWithStatusCode{StatusCode: http.StatusOK, Response: dto.MidjourneyResponse{Code: 1}}, succeeded: midjourneySwapFaceSucceeded, want: true},
		{name: "swap face non 200", response: &dto.MidjourneyResponseWithStatusCode{StatusCode: http.StatusBadGateway, Response: dto.MidjourneyResponse{Code: 1}}, succeeded: midjourneySwapFaceSucceeded, want: false},
		{name: "swap face failed business code", response: &dto.MidjourneyResponseWithStatusCode{StatusCode: http.StatusOK, Response: dto.MidjourneyResponse{Code: 23}}, succeeded: midjourneySwapFaceSucceeded, want: false},
		{name: "image seed lower 2xx", response: &dto.MidjourneyResponseWithStatusCode{StatusCode: http.StatusOK}, succeeded: midjourneyImageSeedSucceeded, want: true},
		{name: "image seed upper 2xx", response: &dto.MidjourneyResponseWithStatusCode{StatusCode: http.StatusMultipleChoices - 1}, succeeded: midjourneyImageSeedSucceeded, want: true},
		{name: "image seed below 2xx", response: &dto.MidjourneyResponseWithStatusCode{StatusCode: http.StatusContinue}, succeeded: midjourneyImageSeedSucceeded, want: false},
		{name: "image seed 3xx", response: &dto.MidjourneyResponseWithStatusCode{StatusCode: http.StatusMultipleChoices}, succeeded: midjourneyImageSeedSucceeded, want: false},
		{name: "nil response", response: nil, succeeded: midjourneySubmitSucceeded, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, test.succeeded(test.response))
		})
	}
}

func TestRelayMidjourneyNotifyRecordsFirstTerminalCASOnce(t *testing.T) {
	db, runtime := setupRealtimeTaskMetricsTest(t)
	require.NoError(t, db.AutoMigrate(&model.Midjourney{}))
	submitTime := time.Now().Add(-time.Minute).UnixMilli()
	require.NoError(t, db.Create(&model.Midjourney{
		MjId:       "mj-notify-terminal",
		Status:     string(model.TaskStatusSubmitted),
		Progress:   "10%",
		SubmitTime: submitTime,
	}).Error)

	payload, err := common.Marshal(dto.MidjourneyDto{
		MjId:       "mj-notify-terminal",
		Status:     string(model.TaskStatusSuccess),
		Progress:   "100%",
		SubmitTime: submitTime,
	})
	require.NoError(t, err)

	for range 2 {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/mj/notify", bytes.NewReader(payload))
		ctx.Request.Header.Set("Content-Type", "application/json")
		assert.Nil(t, RelayMidjourneyNotify(ctx))
	}

	var reloaded model.Midjourney
	require.NoError(t, db.Where("mj_id = ?", "mj-notify-terminal").First(&reloaded).Error)
	assert.Equal(t, string(model.TaskStatusSuccess), reloaded.Status)
	assert.Positive(t, reloaded.FinishTime)
	metrics := scrapeMidjourneyPrometheusMetrics(t, runtime)
	assert.Contains(t, metrics, `newapi_task_completions_total{platform="midjourney",result="success"} 1`)
	assert.Contains(t, metrics, `newapi_task_duration_seconds_count{platform="midjourney",result="success"} 1`)
}

func scrapeMidjourneyPrometheusMetrics(t *testing.T, runtime *prometheusmetrics.Runtime) string {
	t.Helper()
	response := httptest.NewRecorder()
	runtime.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	return response.Body.String()
}
