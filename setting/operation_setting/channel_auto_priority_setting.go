package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

const (
	ChannelAutoPriorityDefaultMinWeight = 20
	ChannelAutoPriorityDefaultMaxWeight = 100
)

type ChannelAutoPrioritySetting struct {
	Enabled   bool `json:"enabled"`
	MinWeight int  `json:"min_weight"`
	MaxWeight int  `json:"max_weight"`
}

var channelAutoPrioritySetting = ChannelAutoPrioritySetting{
	Enabled:   false,
	MinWeight: ChannelAutoPriorityDefaultMinWeight,
	MaxWeight: ChannelAutoPriorityDefaultMaxWeight,
}

func init() {
	config.GlobalConfig.Register("channel_auto_priority_setting", &channelAutoPrioritySetting)
}

func GetChannelAutoPrioritySetting() *ChannelAutoPrioritySetting {
	return &channelAutoPrioritySetting
}
