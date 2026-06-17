package service

import (
	"strings"

	"github.com/gin-gonic/gin"
)

const ginKeyChannelSelectionTrace = "channel_selection_trace"

type ChannelSelectionTraceStage string

const (
	ChannelSelectionTraceStageAffinity ChannelSelectionTraceStage = "affinity"
	ChannelSelectionTraceStageRuntime  ChannelSelectionTraceStage = "runtime"
	ChannelSelectionTraceStagePriority ChannelSelectionTraceStage = "priority"
	ChannelSelectionTraceStageProbe    ChannelSelectionTraceStage = "probe"
	ChannelSelectionTraceStageFinal    ChannelSelectionTraceStage = "final"
)

type ChannelSelectionTraceAction string

const (
	ChannelSelectionTraceActionHit      ChannelSelectionTraceAction = "hit"
	ChannelSelectionTraceActionMiss     ChannelSelectionTraceAction = "miss"
	ChannelSelectionTraceActionSkip     ChannelSelectionTraceAction = "skip"
	ChannelSelectionTraceActionClear    ChannelSelectionTraceAction = "clear"
	ChannelSelectionTraceActionFallback ChannelSelectionTraceAction = "fallback"
	ChannelSelectionTraceActionSelect   ChannelSelectionTraceAction = "select"
)

type ChannelSelectionTraceEvent struct {
	Stage       ChannelSelectionTraceStage
	Action      ChannelSelectionTraceAction
	Group       string
	Model       string
	ChannelID   int
	Priority    int64
	HealthState string
	Reason      string
	Probe       bool
}

func RecordChannelSelectionTrace(c *gin.Context, event ChannelSelectionTraceEvent) {
	if c == nil {
		return
	}
	if event.Stage == "" || event.Action == "" {
		return
	}

	events := getChannelSelectionTraceEvents(c)
	events = append(events, event)
	c.Set(ginKeyChannelSelectionTrace, events)
}

func getChannelSelectionTraceEvents(c *gin.Context) []ChannelSelectionTraceEvent {
	if c == nil {
		return nil
	}
	anyEvents, ok := c.Get(ginKeyChannelSelectionTrace)
	if !ok {
		return nil
	}
	events, ok := anyEvents.([]ChannelSelectionTraceEvent)
	if !ok {
		return nil
	}
	return events
}

func AppendChannelSelectionTraceAdminInfo(c *gin.Context, adminInfo map[string]interface{}) {
	if c == nil || adminInfo == nil {
		return
	}
	events := getChannelSelectionTraceEvents(c)
	if len(events) == 0 {
		return
	}
	adminInfo["channel_selection_trace"] = channelSelectionTraceEventsForLog(events)
}

func channelSelectionTraceEventsForLog(events []ChannelSelectionTraceEvent) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(events))
	for _, event := range events {
		item := map[string]interface{}{
			"stage":  string(event.Stage),
			"action": string(event.Action),
		}
		if group := strings.TrimSpace(event.Group); group != "" {
			item["group"] = group
		}
		if modelName := strings.TrimSpace(event.Model); modelName != "" {
			item["model"] = modelName
		}
		if event.ChannelID > 0 {
			item["channel_id"] = event.ChannelID
		}
		if event.Priority != 0 {
			item["priority"] = event.Priority
		}
		if healthState := strings.TrimSpace(event.HealthState); healthState != "" {
			item["health_state"] = healthState
		}
		if reason := strings.TrimSpace(event.Reason); reason != "" {
			item["reason"] = reason
		}
		if event.Probe {
			item["probe"] = true
		}
		out = append(out, item)
	}
	return out
}
