package model

import "sync"

type ChannelRuntimeStateMode int

const (
	ChannelRuntimeStateNormal ChannelRuntimeStateMode = iota
	ChannelRuntimeStateProbe
	ChannelRuntimeStateClaimProbe
)

type ChannelRuntimeStateFunc func(channelID int, mode ChannelRuntimeStateMode) (available bool, inflight int)

var channelRuntimeState = struct {
	sync.RWMutex
	fn ChannelRuntimeStateFunc
}{}

func SetChannelRuntimeStateFunc(fn ChannelRuntimeStateFunc) {
	channelRuntimeState.Lock()
	defer channelRuntimeState.Unlock()
	channelRuntimeState.fn = fn
}

func getChannelRuntimeState(channelID int) (bool, int) {
	return getChannelRuntimeStateForMode(channelID, ChannelRuntimeStateNormal)
}

func getChannelProbeRuntimeState(channelID int) (bool, int) {
	return getChannelRuntimeStateForMode(channelID, ChannelRuntimeStateProbe)
}

func claimChannelProbeRuntimeState(channelID int) bool {
	available, _ := getChannelRuntimeStateForMode(channelID, ChannelRuntimeStateClaimProbe)
	return available
}

func getChannelRuntimeStateForMode(channelID int, mode ChannelRuntimeStateMode) (bool, int) {
	channelRuntimeState.RLock()
	fn := channelRuntimeState.fn
	channelRuntimeState.RUnlock()
	if fn == nil {
		return true, 0
	}
	available, inflight := fn(channelID, mode)
	if inflight < 0 {
		inflight = 0
	}
	return available, inflight
}
