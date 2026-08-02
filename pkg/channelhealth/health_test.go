package channelhealth_test

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/channelhealth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTracker() *channelhealth.Tracker {
	return channelhealth.NewTracker(time.Now)
}

// noDataChannel should return full weight (no data = unknown = don't penalise)
func TestMultiplier_NoData(t *testing.T) {
	tr := newTracker()
	assert.Equal(t, 1.0, tr.Multiplier(42))
}

// warmup period: fewer than 10 samples → no penalty (use 4 failures to stay below cooldown threshold of 5)
func TestMultiplier_Warmup(t *testing.T) {
	tr := newTracker()
	for i := 0; i < 4; i++ {
		tr.Record(1, false, -1) // 4 failures, below cooldown threshold
	}
	assert.Equal(t, 1.0, tr.Multiplier(1), "should still be full weight during warmup")
}

// perfect success rate after warmup → multiplier = 1.0 (ttft factor also 1 when no ttft)
func TestMultiplier_AllSuccess(t *testing.T) {
	tr := newTracker()
	for i := 0; i < 50; i++ {
		tr.Record(1, true, -1)
	}
	m := tr.Multiplier(1)
	assert.Equal(t, 1.0, m)
}

// all failures after warmup → multiplier clamped to minMultiplier (0.05), not zero
func TestMultiplier_AllFailure_NoCooldown(t *testing.T) {
	tr := newTracker()
	// record 10 failures spread so streak never hits cooldownAfter (5)
	// do it in groups of 4 with a success in between
	for i := 0; i < 5; i++ {
		tr.Record(1, false, -1)
		tr.Record(1, false, -1)
		tr.Record(1, true, -1) // reset streak
		tr.Record(1, false, -1)
	}
	m := tr.Multiplier(1)
	assert.True(t, m >= 0.05 && m < 1.0, "expected degraded but non-zero multiplier, got %v", m)
}

// cooldown: 5 consecutive failures → multiplier becomes 0
func TestMultiplier_Cooldown(t *testing.T) {
	now := time.Now()
	tr := channelhealth.NewTracker(func() time.Time { return now })

	// warmup first with successes
	for i := 0; i < 10; i++ {
		tr.Record(1, true, -1)
	}
	// trigger cooldown with 5 consecutive failures
	for i := 0; i < 5; i++ {
		tr.Record(1, false, -1)
	}
	assert.Equal(t, 0.0, tr.Multiplier(1), "channel in cooldown should have 0 multiplier")
}

// after cooldown expires the channel recovers
func TestMultiplier_CooldownExpiry(t *testing.T) {
	now := time.Now()
	tr := channelhealth.NewTracker(func() time.Time { return now })

	for i := 0; i < 10; i++ {
		tr.Record(1, true, -1)
	}
	for i := 0; i < 5; i++ {
		tr.Record(1, false, -1)
	}
	require.Equal(t, 0.0, tr.Multiplier(1))

	// advance clock past 2-minute cooldown
	now = now.Add(3 * time.Minute)
	m := tr.Multiplier(1)
	assert.True(t, m > 0, "multiplier should recover after cooldown expires, got %v", m)
}

// slow TTFT degrades the multiplier
func TestMultiplier_SlowTTFT(t *testing.T) {
	tr := newTracker()
	for i := 0; i < 20; i++ {
		tr.Record(1, true, 8000) // 8 s TTFT, reference is 2 s
	}
	m := tr.Multiplier(1)
	assert.True(t, m < 1.0, "slow TTFT should lower the multiplier, got %v", m)
	assert.True(t, m >= 0.05, "multiplier should not fall below floor, got %v", m)
}

// fast TTFT leaves multiplier at 1.0
func TestMultiplier_FastTTFT(t *testing.T) {
	tr := newTracker()
	for i := 0; i < 20; i++ {
		tr.Record(1, true, 500) // 0.5 s TTFT
	}
	assert.Equal(t, 1.0, tr.Multiplier(1))
}

// Snapshot returns data consistent with recorded outcomes
func TestSnapshot(t *testing.T) {
	tr := newTracker()
	for i := 0; i < 20; i++ {
		tr.Record(7, true, 1000)
	}
	snap := tr.Snapshot()
	s, ok := snap[7]
	require.True(t, ok)
	assert.Equal(t, 20, s.SampleCount)
	assert.InDelta(t, 1.0, s.SuccessRate, 0.001)
	assert.False(t, s.InCooldown)
	assert.Equal(t, 1.0, s.Multiplier)
}

func TestConfigureClampsMinMultiplierWhenHealthIsDisabled(t *testing.T) {
	previous := channelhealth.GetConfig()
	t.Cleanup(func() { channelhealth.Configure(previous) })

	channelhealth.Configure(channelhealth.Config{
		Enabled:       false,
		MinMultiplier: 2, // >1 should be clamped to 1
		TTFTTimeout:   time.Second,
	})

	cfg := channelhealth.GetConfig()
	assert.Equal(t, 1.0, cfg.MinMultiplier, "MinMultiplier > 1 should be clamped to 1")
	// TTFTTimeout is independent of Enabled: disabling health tracking should
	// not clear the TTFT timeout so callers can still use it for retries.
	assert.Equal(t, time.Second, cfg.TTFTTimeout, "TTFTTimeout should not be cleared when Enabled=false")
}

func TestRecordDoesNotTrackWhenHealthIsDisabled(t *testing.T) {
	previous := channelhealth.GetConfig()
	t.Cleanup(func() { channelhealth.Configure(previous) })

	channelhealth.Configure(channelhealth.Config{Enabled: false})
	tracker := channelhealth.NewTracker(time.Now)
	tracker.Record(42, false, -1)

	assert.Empty(t, tracker.Snapshot())
}
