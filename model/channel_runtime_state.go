package model

import "sync"

type ChannelRuntimeStateMode int

const (
	ChannelRuntimeStateNormal ChannelRuntimeStateMode = iota
	ChannelRuntimeStateProbe
	ChannelRuntimeStateClaimProbe
)

type ChannelRuntimeStateFunc func(channelID int, modelName string, mode ChannelRuntimeStateMode) (available bool, inflight int)

type ChannelSelectionTraceEvent struct {
	Stage       string
	Action      string
	Group       string
	Model       string
	ChannelID   int
	Priority    int64
	HealthState string
	Reason      string
	Probe       bool
}

type ChannelSelectionTraceFunc func(event ChannelSelectionTraceEvent)
type ChannelRuntimeHealthStateFunc func(channelID int) string

var channelRuntimeState = struct {
	sync.RWMutex
	fn ChannelRuntimeStateFunc
}{}

var channelRuntimeHealthState = struct {
	sync.RWMutex
	fn ChannelRuntimeHealthStateFunc
}{}

func SetChannelRuntimeStateFunc(fn ChannelRuntimeStateFunc) {
	channelRuntimeState.Lock()
	defer channelRuntimeState.Unlock()
	channelRuntimeState.fn = fn
}

func getChannelRuntimeState(channelID int, modelName string) (bool, int) {
	return getChannelRuntimeStateForMode(channelID, modelName, ChannelRuntimeStateNormal)
}

func getChannelProbeRuntimeState(channelID int, modelName string) (bool, int) {
	return getChannelRuntimeStateForMode(channelID, modelName, ChannelRuntimeStateProbe)
}

func claimChannelProbeRuntimeState(channelID int, modelName string) bool {
	available, _ := getChannelRuntimeStateForMode(channelID, modelName, ChannelRuntimeStateClaimProbe)
	return available
}

func getChannelRuntimeStateForMode(channelID int, modelName string, mode ChannelRuntimeStateMode) (bool, int) {
	channelRuntimeState.RLock()
	fn := channelRuntimeState.fn
	channelRuntimeState.RUnlock()
	if fn == nil {
		return true, 0
	}
	available, inflight := fn(channelID, modelName, mode)
	if inflight < 0 {
		inflight = 0
	}
	return available, inflight
}

func recordChannelSelectionTrace(fn ChannelSelectionTraceFunc, event ChannelSelectionTraceEvent) {
	if fn != nil {
		fn(event)
	}
}

func SetChannelRuntimeHealthStateFunc(fn ChannelRuntimeHealthStateFunc) {
	channelRuntimeHealthState.Lock()
	defer channelRuntimeHealthState.Unlock()
	channelRuntimeHealthState.fn = fn
}

func runtimeTraceHealthState(channelID int) string {
	channelRuntimeHealthState.RLock()
	fn := channelRuntimeHealthState.fn
	channelRuntimeHealthState.RUnlock()
	if fn == nil {
		return ""
	}
	return fn(channelID)
}
