package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelRuntimeTrackerTracksConcurrentAttemptsAndRPM(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := newChannelRuntimeTracker(func() time.Time { return now })

	first := tracker.begin(7)
	second := tracker.begin(7)

	metrics := tracker.snapshot([]int{7})[7]
	require.Equal(t, 2, metrics.Concurrency)
	assert.Equal(t, 2, metrics.RPM)

	first.Done()
	first.Done()
	metrics = tracker.snapshot([]int{7})[7]
	require.Equal(t, 1, metrics.Concurrency)
	assert.Equal(t, 2, metrics.RPM)

	second.Done()
	metrics = tracker.snapshot([]int{7})[7]
	assert.Zero(t, metrics.Concurrency)
	assert.Equal(t, 2, metrics.RPM)
}

func TestChannelRuntimeTrackerKeepsOnlyTheLatestSixtySeconds(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := newChannelRuntimeTracker(func() time.Time { return now })

	tracker.begin(7).Done()
	now = now.Add(59 * time.Second)
	assert.Equal(t, 1, tracker.snapshot([]int{7})[7].RPM)

	now = now.Add(time.Second)
	assert.Zero(t, tracker.snapshot([]int{7})[7].RPM)
}

func TestChannelRuntimeTrackerSeparatesChannelsAndIgnoresInvalidIDs(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := newChannelRuntimeTracker(func() time.Time { return now })

	first := tracker.begin(7)
	second := tracker.begin(9)
	invalid := tracker.begin(0)

	metrics := tracker.snapshot([]int{0, 7, 9})
	assert.Equal(t, channelRuntimeMetrics{}, metrics[0])
	assert.Equal(t, channelRuntimeMetrics{Concurrency: 1, RPM: 1}, metrics[7])
	assert.Equal(t, channelRuntimeMetrics{Concurrency: 1, RPM: 1}, metrics[9])

	invalid.Done()
	first.Done()
	second.Done()
}
