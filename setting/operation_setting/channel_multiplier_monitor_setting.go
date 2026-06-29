package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

const ChannelMultiplierMonitorDefaultIntervalMinutes = 2

type ChannelMultiplierMonitorSetting struct {
	IntervalMinutes int `json:"interval_minutes"`
}

var channelMultiplierMonitorSetting = ChannelMultiplierMonitorSetting{
	IntervalMinutes: ChannelMultiplierMonitorDefaultIntervalMinutes,
}

func init() {
	config.GlobalConfig.Register("channel_multiplier_monitor_setting", &channelMultiplierMonitorSetting)
}

func GetChannelMultiplierMonitorSetting() *ChannelMultiplierMonitorSetting {
	return &channelMultiplierMonitorSetting
}
