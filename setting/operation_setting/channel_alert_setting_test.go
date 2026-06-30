package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

func TestChannelAlertSettingExportsAndLoadsRuntimeOptions(t *testing.T) {
	setting := GetChannelAlertSetting()
	original := *setting
	t.Cleanup(func() {
		*setting = original
	})

	exported := config.GlobalConfig.ExportAllConfigs()
	require.Equal(t, "false", exported["channel_alert_setting.enabled"])
	require.Equal(t, "true", exported["channel_alert_setting.balance_alert_enabled"])
	require.Equal(t, "true", exported["channel_alert_setting.multiplier_change_enabled"])
	require.Equal(t, "0", exported["channel_alert_setting.balance_threshold"])
	require.Equal(t, "300", exported["channel_alert_setting.min_interval_seconds"])
	require.Equal(t, "false", exported["channel_alert_setting.feishu_enabled"])
	require.Equal(t, "", exported["channel_alert_setting.feishu_webhook_url"])
	require.Equal(t, "", exported["channel_alert_setting.feishu_secret"])
	require.Equal(t, "false", exported["channel_alert_setting.dingtalk_enabled"])
	require.Equal(t, "", exported["channel_alert_setting.dingtalk_webhook_url"])
	require.Equal(t, "", exported["channel_alert_setting.dingtalk_secret"])

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"channel_alert_setting.enabled":                   "true",
		"channel_alert_setting.balance_alert_enabled":     "false",
		"channel_alert_setting.multiplier_change_enabled": "false",
		"channel_alert_setting.balance_threshold":         "12.5",
		"channel_alert_setting.min_interval_seconds":      "60",
		"channel_alert_setting.feishu_enabled":            "true",
		"channel_alert_setting.feishu_webhook_url":        "https://open.feishu.cn/open-apis/bot/v2/hook/test",
		"channel_alert_setting.feishu_secret":             "feishu-secret",
		"channel_alert_setting.dingtalk_enabled":          "true",
		"channel_alert_setting.dingtalk_webhook_url":      "https://oapi.dingtalk.com/robot/send?access_token=test",
		"channel_alert_setting.dingtalk_secret":           "dingtalk-secret",
	}))

	require.True(t, setting.Enabled)
	require.False(t, setting.BalanceAlertEnabled)
	require.False(t, setting.MultiplierChangeEnabled)
	require.Equal(t, 12.5, setting.BalanceThreshold)
	require.Equal(t, 60, setting.MinIntervalSeconds)
	require.True(t, setting.FeishuEnabled)
	require.Equal(t, "https://open.feishu.cn/open-apis/bot/v2/hook/test", setting.FeishuWebhookURL)
	require.Equal(t, "feishu-secret", setting.FeishuSecret)
	require.True(t, setting.DingTalkEnabled)
	require.Equal(t, "https://oapi.dingtalk.com/robot/send?access_token=test", setting.DingTalkWebhookURL)
	require.Equal(t, "dingtalk-secret", setting.DingTalkSecret)
}
