package service

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/dto"
)

const channelRuntimeWindowSeconds int64 = 60

type channelRuntimeMetrics = dto.ChannelRuntimeMetrics

type channelRuntimeBucket struct {
	second int64
	count  int
}

type channelRuntimeState struct {
	concurrency int
	buckets     [channelRuntimeWindowSeconds]channelRuntimeBucket
}

type channelRuntimeTracker struct {
	mu       sync.Mutex
	now      func() time.Time
	channels map[int]*channelRuntimeState
}

type ChannelRuntimeAttempt struct {
	tracker   *channelRuntimeTracker
	channelID int
	done      sync.Once
}

var defaultChannelRuntimeTracker = newChannelRuntimeTracker(time.Now)

func newChannelRuntimeTracker(now func() time.Time) *channelRuntimeTracker {
	return &channelRuntimeTracker{
		now:      now,
		channels: make(map[int]*channelRuntimeState),
	}
}

func (tracker *channelRuntimeTracker) begin(channelID int) *ChannelRuntimeAttempt {
	attempt := &ChannelRuntimeAttempt{}
	if tracker == nil || channelID <= 0 {
		return attempt
	}

	second := tracker.now().Unix()
	tracker.mu.Lock()
	state := tracker.channels[channelID]
	if state == nil {
		state = &channelRuntimeState{}
		tracker.channels[channelID] = state
	}
	bucket := &state.buckets[second%channelRuntimeWindowSeconds]
	if bucket.second != second {
		bucket.second = second
		bucket.count = 0
	}
	bucket.count++
	state.concurrency++
	tracker.mu.Unlock()

	attempt.tracker = tracker
	attempt.channelID = channelID
	return attempt
}

func (tracker *channelRuntimeTracker) snapshot(channelIDs []int) map[int]dto.ChannelRuntimeMetrics {
	metrics := make(map[int]dto.ChannelRuntimeMetrics, len(channelIDs))
	if tracker == nil {
		return metrics
	}

	now := tracker.now().Unix()
	cutoff := now - channelRuntimeWindowSeconds
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	for _, channelID := range channelIDs {
		current := dto.ChannelRuntimeMetrics{}
		state := tracker.channels[channelID]
		if state != nil {
			current.Concurrency = state.concurrency
			for _, bucket := range state.buckets {
				if bucket.second > cutoff && bucket.second <= now {
					current.RPM += bucket.count
				}
			}
		}
		metrics[channelID] = current
	}
	return metrics
}

func (attempt *ChannelRuntimeAttempt) Done() {
	if attempt == nil || attempt.tracker == nil || attempt.channelID <= 0 {
		return
	}

	attempt.done.Do(func() {
		attempt.tracker.mu.Lock()
		defer attempt.tracker.mu.Unlock()
		state := attempt.tracker.channels[attempt.channelID]
		if state != nil && state.concurrency > 0 {
			state.concurrency--
		}
	})
}

func BeginChannelRuntimeAttempt(channelID int) *ChannelRuntimeAttempt {
	return defaultChannelRuntimeTracker.begin(channelID)
}

func GetChannelRuntimeMetrics(channelIDs []int) map[int]dto.ChannelRuntimeMetrics {
	return defaultChannelRuntimeTracker.snapshot(channelIDs)
}
