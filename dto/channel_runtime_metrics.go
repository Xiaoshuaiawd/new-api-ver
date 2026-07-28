package dto

type ChannelRuntimeMetrics struct {
	Concurrency int `json:"concurrency"`
	RPM         int `json:"rpm"`
}
