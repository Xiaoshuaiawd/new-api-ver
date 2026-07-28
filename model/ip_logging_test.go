package model

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnforceRecordIpLoggingForAllUsers(t *testing.T) {
	setupUserUpdateTestState(t)

	users := []User{
		{Id: 10, Username: "ip-disabled", Password: "password", AffCode: "ip10", Setting: `{"language":"zh","record_ip_log":false}`},
		{Id: 11, Username: "ip-missing", Password: "password", AffCode: "ip11", Setting: `{"language":"en"}`},
		{Id: 12, Username: "ip-enabled", Password: "password", AffCode: "ip12", Setting: `{"language":"fr","record_ip_log":true}`},
		{Id: 13, Username: "ip-invalid", Password: "password", AffCode: "ip13", Setting: `invalid-json`},
	}
	require.NoError(t, DB.Create(&users).Error)

	require.NoError(t, enforceRecordIpLoggingForAllUsers())

	var stored []User
	require.NoError(t, DB.Where("id IN ?", []int{10, 11, 12, 13}).Order("id").Find(&stored).Error)
	require.Len(t, stored, 4)

	expectedLanguages := []string{"zh", "en", "fr"}
	for i := range expectedLanguages {
		var settings map[string]interface{}
		require.NoError(t, common.Unmarshal([]byte(stored[i].Setting), &settings))
		assert.Equal(t, true, settings["record_ip_log"])
		assert.Equal(t, expectedLanguages[i], settings["language"])
	}
	assert.Equal(t, "invalid-json", stored[3].Setting)
}

func TestUserSettingsAlwaysEnableIpLogging(t *testing.T) {
	setupUserUpdateTestState(t)

	user := User{
		Id:       19,
		Username: "ip-setting-user",
		Password: "password",
	}
	user.SetSetting(dto.UserSetting{Language: "zh", RecordIpLog: false})
	require.NoError(t, DB.Create(&user).Error)

	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, "zh", stored.GetSetting().Language)
	assert.True(t, stored.GetSetting().RecordIpLog)
}

func TestUsageAndErrorLogsAlwaysRecordClientIP(t *testing.T) {
	setupUserUpdateTestState(t)

	user := User{
		Id:       20,
		Username: "ip-log-user",
		Password: "password",
		Setting:  `{"record_ip_log":false}`,
	}
	require.NoError(t, DB.Create(&user).Error)

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	context.Request.RemoteAddr = "203.0.113.42:12345"
	context.Set("username", user.Username)

	RecordConsumeLog(context, user.Id, RecordConsumeLogParams{ModelName: "test-model"})
	RecordErrorLog(context, user.Id, 0, "test-model", "test-token", "upstream error", 0, 1, false, "default", nil)

	var logs []Log
	require.NoError(t, LOG_DB.Where("user_id = ?", user.Id).Order("id").Find(&logs).Error)
	require.Len(t, logs, 2)
	assert.Equal(t, LogTypeConsume, logs[0].Type)
	assert.Equal(t, LogTypeError, logs[1].Type)
	assert.Equal(t, "203.0.113.42", logs[0].Ip)
	assert.Equal(t, "203.0.113.42", logs[1].Ip)
}
