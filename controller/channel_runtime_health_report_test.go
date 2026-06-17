package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetChannelRuntimeHealthReportReturnsFilteredReport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setting := operation_setting.GetChannelHealthSetting()
	original := *setting
	*setting = operation_setting.ChannelHealthSetting{
		Enabled:                 true,
		WindowSeconds:           180,
		ProbeIntervalSeconds:    30,
		ProbeTimeoutSeconds:     30,
		ProbeBackoffMaxSeconds:  300,
		WarmupEnabled:           true,
		WarmupDurationSeconds:   60,
		WarmupStartPercent:      10,
		WarmupStepPercent:       30,
		EventsEnabled:           true,
		ModelLevelEnabled:       true,
		AlertMinIntervalSeconds: 60,
	}
	t.Cleanup(func() {
		*setting = original
		service.ResetChannelHealthForTest()
	})
	service.ResetChannelHealthForTest()
	service.SetChannelHealthEventNotifyFuncForTest(func(event service.ChannelHealthEvent) {})
	now := time.Unix(1_700_000_000, 0)
	service.SetChannelHealthNowFuncForTest(func() time.Time { return now })
	service.OpenChannelForModel(9901, "gpt-report", "report isolate")
	service.OpenChannelForModel(9902, "gpt-other", "other isolate")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/api/channel/runtime_health_report?channel_id=9901&model=gpt-report&type=opened&limit=10", nil)
	c.Request = req

	GetChannelRuntimeHealthReport(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Success bool                        `json:"success"`
		Data    service.ChannelHealthReport `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	require.Equal(t, 1, resp.Data.IsolationCount)
	require.Len(t, resp.Data.Events, 1)
	require.Equal(t, 9901, resp.Data.Events[0].ChannelID)
	require.Equal(t, "gpt-report", resp.Data.Events[0].ModelName)
}

func TestGetChannelRuntimeHealthReportFiltersByGroupAndState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setting := operation_setting.GetChannelHealthSetting()
	original := *setting
	*setting = operation_setting.ChannelHealthSetting{
		Enabled:                     true,
		WindowSeconds:               180,
		MinSamples:                  10,
		MinFailures:                 5,
		ErrorRateThreshold:          0.40,
		ConsecutiveFailureThreshold: 5,
		ProbeIntervalSeconds:        30,
		ProbeTimeoutSeconds:         30,
		ProbeBackoffMaxSeconds:      300,
		EventsEnabled:               true,
		AlertMinIntervalSeconds:     60,
	}
	t.Cleanup(func() {
		*setting = original
		service.ResetChannelHealthForTest()
	})
	service.ResetChannelHealthForTest()
	service.SetChannelHealthEventNotifyFuncForTest(func(event service.ChannelHealthEvent) {})
	now := time.Unix(1_700_000_000, 0)
	service.SetChannelHealthNowFuncForTest(func() time.Time { return now })
	for i := 0; i < 5; i++ {
		handle := service.RecordAttemptStart(service.ChannelAttemptMeta{
			ChannelID: 9903,
			ModelName: "gpt-report",
			Group:     "vip",
		})
		service.RecordAttemptFinish(handle, service.ChannelAttemptResult{Error: serviceTestUpstreamError()})
	}
	service.OpenChannelForModel(9904, "gpt-report", "default isolate")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/api/channel/runtime_health_report?group=vip&state=open&limit=10", nil)
	c.Request = req

	GetChannelRuntimeHealthReport(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Success bool                        `json:"success"`
		Data    service.ChannelHealthReport `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	require.Equal(t, 1, resp.Data.IsolationCount)
	require.Len(t, resp.Data.Events, 1)
	require.Equal(t, 9903, resp.Data.Events[0].ChannelID)
	require.Equal(t, "vip", resp.Data.Events[0].Group)
	require.Equal(t, "open", resp.Data.Events[0].State)
}

func serviceTestUpstreamError() *types.NewAPIError {
	return types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
}
