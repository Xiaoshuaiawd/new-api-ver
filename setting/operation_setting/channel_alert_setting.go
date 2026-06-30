package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

const ChannelAlertDefaultMinIntervalSeconds = 300

type ChannelAlertSetting struct {
	Enabled                 bool    `json:"enabled"`
	BalanceAlertEnabled     bool    `json:"balance_alert_enabled"`
	MultiplierChangeEnabled bool    `json:"multiplier_change_enabled"`
	BalanceThreshold        float64 `json:"balance_threshold"`
	MinIntervalSeconds      int     `json:"min_interval_seconds"`
	FeishuEnabled           bool    `json:"feishu_enabled"`
	FeishuWebhookURL        string  `json:"feishu_webhook_url"`
	FeishuSecret            string  `json:"feishu_secret"`
	DingTalkEnabled         bool    `json:"dingtalk_enabled"`
	DingTalkWebhookURL      string  `json:"dingtalk_webhook_url"`
	DingTalkSecret          string  `json:"dingtalk_secret"`
}

var channelAlertSetting = ChannelAlertSetting{
	Enabled:                 false,
	BalanceAlertEnabled:     true,
	MultiplierChangeEnabled: true,
	BalanceThreshold:        0,
	MinIntervalSeconds:      ChannelAlertDefaultMinIntervalSeconds,
}

func init() {
	config.GlobalConfig.Register("channel_alert_setting", &channelAlertSetting)
}

func GetChannelAlertSetting() *ChannelAlertSetting {
	return &channelAlertSetting
}
