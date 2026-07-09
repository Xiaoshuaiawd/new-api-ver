package service

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const ginKeyChannelSelectionTrace = "channel_selection_trace"
const channelSelectionTraceSummaryMaxItems = 2000

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

type ChannelSelectionTraceSummary struct {
	ChannelID          int    `json:"channel_id,omitempty"`
	Group              string `json:"group,omitempty"`
	Model              string `json:"model,omitempty"`
	Priority           int64  `json:"priority,omitempty"`
	Selected           int    `json:"selected"`
	Skipped            int    `json:"skipped"`
	RuntimeUnavailable int    `json:"runtime_unavailable"`
	HealthDegraded     int    `json:"health_degraded"`
	PriorityFallbacks  int    `json:"priority_fallbacks"`
	ProbeFallbacks     int    `json:"probe_fallbacks"`
	LastHealthState    string `json:"last_health_state,omitempty"`
	LastReason         string `json:"last_reason,omitempty"`
	LastSeenAt         int64  `json:"last_seen_at"`
}

var channelSelectionTraceSummary = struct {
	sync.Mutex
	items map[string]ChannelSelectionTraceSummary
}{
	items: make(map[string]ChannelSelectionTraceSummary),
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
	recordChannelSelectionTraceSummary(event, time.Now())
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

func ResetChannelSelectionTraceSummaryForTest() {
	channelSelectionTraceSummary.Lock()
	defer channelSelectionTraceSummary.Unlock()
	channelSelectionTraceSummary.items = make(map[string]ChannelSelectionTraceSummary)
}

func recordChannelSelectionTraceSummary(event ChannelSelectionTraceEvent, now time.Time) {
	key := channelSelectionSummaryKey(event)
	channelSelectionTraceSummary.Lock()
	defer channelSelectionTraceSummary.Unlock()
	if channelSelectionTraceSummary.items == nil {
		channelSelectionTraceSummary.items = make(map[string]ChannelSelectionTraceSummary)
	}
	item := channelSelectionTraceSummary.items[key]
	item.ChannelID = event.ChannelID
	item.Group = strings.TrimSpace(event.Group)
	item.Model = strings.TrimSpace(event.Model)
	item.Priority = event.Priority
	item.LastHealthState = strings.TrimSpace(event.HealthState)
	if reason := strings.TrimSpace(event.Reason); reason != "" {
		item.LastReason = reason
	}
	item.LastSeenAt = now.Unix()
	switch event.Action {
	case ChannelSelectionTraceActionSelect, ChannelSelectionTraceActionHit:
		if event.Stage == ChannelSelectionTraceStageFinal || event.ChannelID > 0 {
			item.Selected++
		}
	case ChannelSelectionTraceActionSkip:
		item.Skipped++
		if event.Stage == ChannelSelectionTraceStageRuntime {
			item.RuntimeUnavailable++
		}
	}
	if event.Stage == ChannelSelectionTraceStageRuntime && event.Action == ChannelSelectionTraceActionSkip {
		healthState := strings.TrimSpace(event.HealthState)
		if healthState != "" && healthState != string(ChannelHealthStateHealthy) {
			item.HealthDegraded++
		}
	}
	if event.Stage == ChannelSelectionTraceStagePriority && event.Action == ChannelSelectionTraceActionFallback {
		item.PriorityFallbacks++
	}
	if event.Stage == ChannelSelectionTraceStageProbe && event.Action == ChannelSelectionTraceActionFallback {
		item.ProbeFallbacks++
	}
	channelSelectionTraceSummary.items[key] = item
	pruneChannelSelectionTraceSummaryLocked()
}

func channelSelectionSummaryKey(event ChannelSelectionTraceEvent) string {
	return strings.Join([]string{
		strings.TrimSpace(event.Group),
		strings.TrimSpace(event.Model),
		formatSelectionSummaryInt(event.ChannelID),
		formatSelectionSummaryInt64(event.Priority),
	}, "\x00")
}

func pruneChannelSelectionTraceSummaryLocked() {
	if len(channelSelectionTraceSummary.items) <= channelSelectionTraceSummaryMaxItems {
		return
	}
	type keyedSummary struct {
		key  string
		item ChannelSelectionTraceSummary
	}
	items := make([]keyedSummary, 0, len(channelSelectionTraceSummary.items))
	for key, item := range channelSelectionTraceSummary.items {
		items = append(items, keyedSummary{key: key, item: item})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].item.LastSeenAt < items[j].item.LastSeenAt
	})
	for len(channelSelectionTraceSummary.items) > channelSelectionTraceSummaryMaxItems && len(items) > 0 {
		delete(channelSelectionTraceSummary.items, items[0].key)
		items = items[1:]
	}
}

func formatSelectionSummaryInt(value int) string {
	if value == 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func formatSelectionSummaryInt64(value int64) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}

func GetChannelSelectionTraceSummary(filter ChannelHealthEventFilter) []ChannelSelectionTraceSummary {
	channelSelectionTraceSummary.Lock()
	defer channelSelectionTraceSummary.Unlock()
	summary := make([]ChannelSelectionTraceSummary, 0, len(channelSelectionTraceSummary.items))
	for _, item := range channelSelectionTraceSummary.items {
		if filter.ChannelID > 0 && item.ChannelID != filter.ChannelID {
			continue
		}
		if filter.ModelName != "" && item.Model != filter.ModelName {
			continue
		}
		if filter.Group != "" && item.Group != filter.Group {
			continue
		}
		summary = append(summary, item)
	}
	sort.Slice(summary, func(i, j int) bool {
		if summary[i].LastSeenAt == summary[j].LastSeenAt {
			if summary[i].ChannelID == summary[j].ChannelID {
				return summary[i].Priority > summary[j].Priority
			}
			return summary[i].ChannelID < summary[j].ChannelID
		}
		return summary[i].LastSeenAt > summary[j].LastSeenAt
	})
	if filter.Limit > 0 && len(summary) > filter.Limit {
		return summary[:filter.Limit]
	}
	return summary
}
