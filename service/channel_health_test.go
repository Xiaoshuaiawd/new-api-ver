package service

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withChannelHealthTestSettings(t *testing.T) *operation_setting.ChannelHealthSetting {
	t.Helper()

	setting := operation_setting.GetChannelHealthSetting()
	original := *setting
	*setting = operation_setting.ChannelHealthSetting{
		Enabled:                     true,
		WindowSeconds:               180,
		MinSamples:                  10,
		MinFailures:                 5,
		DegradationThreshold:        0.10,
		ErrorRateThreshold:          0.40,
		ConsecutiveFailureThreshold: 5,
		FirstResponseTimeoutSeconds: 45,
		StuckInflightThreshold:      3,
		SingleStuckTimeoutSeconds:   75,
		ProbeIntervalSeconds:        30,
		ProbeTimeoutSeconds:         30,
		ProbeSuccessesToRecover:     2,
		ProbeBackoffMaxSeconds:      300,
		WarmupEnabled:               true,
		WarmupDurationSeconds:       60,
		WarmupStartPercent:          10,
		WarmupStepPercent:           30,
		Preset:                      operation_setting.ChannelHealthPresetBalanced,
		ModelLevelEnabled:           false,
		EventsEnabled:               true,
		AlertMinIntervalSeconds:     60,
		StuckDetectionEnabled:       true,
	}
	t.Cleanup(func() {
		channelHealthProbeWaitGroup.Wait()
		*setting = original
		ResetChannelHealthForTest()
	})
	ResetChannelHealthForTest()
	SetChannelHealthEventNotifyFuncForTest(func(ChannelHealthEvent) {})
	return setting
}

func TestChannelHealthOpensOnSlidingWindowErrorRate(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.ConsecutiveFailureThreshold = 100

	const channelID = 8801
	for i := 0; i < 5; i++ {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
		RecordAttemptFinish(handle, ChannelAttemptResult{Error: channelHealthTestUpstreamError()})
	}
	for i := 0; i < 5; i++ {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
		RecordFirstResponse(handle)
		RecordAttemptFinish(handle, ChannelAttemptResult{})
	}

	require.False(t, IsChannelAvailable(channelID))
	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateOpen, snapshot.State)
	require.Contains(t, snapshot.Reason, "error_rate")
}

func TestChannelHealthKeepsChannelAvailableBelowErrorThreshold(t *testing.T) {
	withChannelHealthTestSettings(t)

	const channelID = 8802
	for i := 0; i < 4; i++ {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
		RecordAttemptFinish(handle, ChannelAttemptResult{Error: channelHealthTestUpstreamError()})
	}
	for i := 0; i < 6; i++ {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
		RecordFirstResponse(handle)
		RecordAttemptFinish(handle, ChannelAttemptResult{})
	}

	require.True(t, IsChannelAvailable(channelID))
}

func TestChannelHealthDegradesTrafficWithoutReusingWarmupState(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.ConsecutiveFailureThreshold = 100

	const channelID = 8826
	for i := 0; i < 2; i++ {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
		RecordAttemptFinish(handle, ChannelAttemptResult{Error: channelHealthTestUpstreamError()})
	}
	for i := 0; i < 8; i++ {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
		RecordFirstResponse(handle)
		RecordAttemptFinish(handle, ChannelAttemptResult{})
	}

	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateHealthy, snapshot.State)
	require.Equal(t, ChannelHealthStateV2Degraded, snapshot.StateV2)
	require.Equal(t, channelHealthDegradedTrafficPercent, snapshot.TrafficPercent)
	require.True(t, snapshot.RuntimeAvailable)
	require.Equal(t, 30, adjustChannelHealthWeight(channelID, "", 100, 0))
	degradedEvents := GetChannelHealthEvents(ChannelHealthEventFilter{ChannelID: channelID, State: string(ChannelHealthStateV2Degraded)})
	require.Len(t, degradedEvents, 1)
	require.Equal(t, ChannelHealthEventTypeDegraded, degradedEvents[0].Type)
	require.Equal(t, string(ChannelHealthStateV2Degraded), degradedEvents[0].StateV2)

	for i := 0; i < 11; i++ {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
		RecordFirstResponse(handle)
		RecordAttemptFinish(handle, ChannelAttemptResult{})
	}

	snapshot, ok = GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateHealthy, snapshot.State)
	require.Equal(t, ChannelHealthStateV2Healthy, snapshot.StateV2)
	require.Equal(t, 100, snapshot.TrafficPercent)
	require.Empty(t, snapshot.Reason)
	// The discrete degraded state is gone, while the existing proportional
	// error-rate penalty remains until the failed samples age out of the window.
	require.Equal(t, 90, adjustChannelHealthWeight(channelID, "", 100, 0))
	restoredEvents := GetChannelHealthEvents(ChannelHealthEventFilter{ChannelID: channelID, Type: ChannelHealthEventTypeRestored})
	require.Len(t, restoredEvents, 1)
	require.Equal(t, string(ChannelHealthStateV2Healthy), restoredEvents[0].StateV2)
}

func TestChannelHealthOpensOnStuckInflightThreshold(t *testing.T) {
	withChannelHealthTestSettings(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8803
	for i := 0; i < 3; i++ {
		RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
	}

	now = now.Add(46 * time.Second)
	CheckChannelHealthStuckRequests()

	require.False(t, IsChannelAvailable(channelID))
	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateOpen, snapshot.State)
	require.Contains(t, snapshot.Reason, "stuck")
	require.Contains(t, snapshot.Reason, "inflight=3")
	require.Equal(t, 0, snapshot.Inflight)
}

func TestChannelHealthCancelsStuckInflightWhenOpened(t *testing.T) {
	withChannelHealthTestSettings(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8806
	cancelled := 0
	for i := 0; i < 3; i++ {
		RecordAttemptStart(ChannelAttemptMeta{
			ChannelID: channelID,
			Cancel: func() {
				cancelled++
			},
		})
	}

	now = now.Add(46 * time.Second)
	CheckChannelHealthStuckRequests()

	require.False(t, IsChannelAvailable(channelID))
	require.Equal(t, 3, cancelled)
}

func TestChannelHealthDoesNotReopenAfterCancelledStuckInflightRecovers(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.WarmupEnabled = false
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8815
	for i := 0; i < 3; i++ {
		RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
	}

	now = now.Add(46 * time.Second)
	CheckChannelHealthStuckRequests()
	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateOpen, snapshot.State)
	require.Equal(t, 0, snapshot.Inflight)

	RecordProbeResult(channelID, true, "")
	RecordProbeResult(channelID, true, "")
	require.True(t, IsChannelAvailable(channelID))

	now = now.Add(time.Second)
	CheckChannelHealthStuckRequests()

	snapshot, ok = GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateHealthy, snapshot.State)
	require.Equal(t, 0, snapshot.Inflight)
}

func TestChannelHealthPrunesCancelledInflightAfterRetention(t *testing.T) {
	withChannelHealthTestSettings(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8818
	for i := 0; i < 3; i++ {
		RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
	}

	now = now.Add(46 * time.Second)
	CheckChannelHealthStuckRequests()

	shard := channelHealthShardFor(channelID)
	shard.Lock()
	state := shard.channels[channelHealthScopeKey(channelHealthScope{channelID: channelID})]
	require.Len(t, state.inflight, 3)
	shard.Unlock()

	now = now.Add(10 * time.Minute)
	CheckChannelHealthStuckRequests()

	shard.Lock()
	require.Len(t, state.inflight, 0)
	shard.Unlock()
}

func TestChannelHealthStuckProbeReleasesProbeInProgress(t *testing.T) {
	withChannelHealthTestSettings(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8817
	OpenChannel(channelID, "test open")
	now = now.Add(31 * time.Second)
	require.True(t, MarkChannelProbing(channelID))
	RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})

	now = now.Add(76 * time.Second)
	CheckChannelHealthStuckRequests()
	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateOpen, snapshot.State)
	require.False(t, snapshot.ProbeInProgress)

	now = now.Add(31 * time.Second)
	require.True(t, IsChannelProbeAvailable(channelID))
}

func TestRunDueChannelHealthProbesBypassesBackoffAfterMaxIsolation(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.MaxIsolationSeconds = 60
	withChannelHealthSelectionDB(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	OpenChannel(9101, "runtime isolate")
	shard := channelHealthShardFor(9101)
	shard.Lock()
	state := shard.channels[channelHealthScopeKey(channelHealthScope{channelID: 9101})]
	state.nextProbeAt = now.Add(time.Hour)
	shard.Unlock()

	probed := make(chan struct{}, 1)
	SetChannelHealthProbeFunc(func(ctx context.Context, channel *model.Channel, modelName string) error {
		probed <- struct{}{}
		return nil
	})

	now = now.Add(61 * time.Second)
	RunDueChannelHealthProbes()

	select {
	case <-probed:
	case <-time.After(time.Second):
		t.Fatal("expected overdue isolation to bypass probe backoff")
	}
}

func TestRunDueChannelHealthProbesUsesModelLevelScopeModel(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.ModelLevelEnabled = true
	withChannelHealthSelectionDB(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	OpenChannelForModel(9101, "gpt-health-test", "runtime isolate")
	now = now.Add(31 * time.Second)

	probedModel := make(chan string, 1)
	SetChannelHealthProbeFunc(func(ctx context.Context, channel *model.Channel, modelName string) error {
		probedModel <- modelName
		return nil
	})

	RunDueChannelHealthProbes()

	select {
	case modelName := <-probedModel:
		require.Equal(t, "gpt-health-test", modelName)
	case <-time.After(time.Second):
		t.Fatal("expected model-level probe to run")
	}
}

func TestProbePermanentFailureUsesMaxBackoff(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.ProbeIntervalSeconds = 30
	setting.ProbeBackoffMaxSeconds = 300
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8819
	OpenChannel(channelID, "runtime isolate")
	RecordProbeResult(channelID, false, "401 unauthorized invalid api key")

	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, now.Add(300*time.Second).Unix(), snapshot.NextProbeAt)
}

func TestChannelHealthRequiresTwoProbeSuccessesToRecover(t *testing.T) {
	withChannelHealthTestSettings(t)

	const channelID = 8804
	OpenChannel(channelID, "test open")
	require.False(t, IsChannelAvailable(channelID))

	RecordProbeResult(channelID, true, "")
	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateProbing, snapshot.State)

	RecordProbeResult(channelID, true, "")
	snapshot, ok = GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateWarming, snapshot.State)
	require.Equal(t, ChannelHealthStateV2Degraded, snapshot.StateV2)
	require.Equal(t, 10, snapshot.WarmupPercent)
	require.Equal(t, 10, snapshot.TrafficPercent)
}

func TestChannelHealthWarmupCompletesAfterDuration(t *testing.T) {
	withChannelHealthTestSettings(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8810
	OpenChannel(channelID, "test open")
	RecordProbeResult(channelID, true, "")
	RecordProbeResult(channelID, true, "")

	now = now.Add(61 * time.Second)

	require.True(t, IsChannelAvailable(channelID))
	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateHealthy, snapshot.State)
	require.Equal(t, ChannelHealthStateV2Healthy, snapshot.StateV2)
	require.Equal(t, 100, snapshot.WarmupPercent)
	require.Equal(t, 100, snapshot.TrafficPercent)
}

func TestChannelHealthWarmupRampsSnapshotPercent(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.FirstResponseTimeoutSeconds = 10
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8812
	OpenChannel(channelID, "test open")
	RecordProbeResult(channelID, true, "")
	RecordProbeResult(channelID, true, "")

	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateWarming, snapshot.State)
	require.Equal(t, 10, snapshot.WarmupPercent)

	now = now.Add(20 * time.Second)
	snapshot, ok = GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateWarming, snapshot.State)
	require.Equal(t, 40, snapshot.WarmupPercent)

	now = now.Add(20 * time.Second)
	snapshot, ok = GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateWarming, snapshot.State)
	require.Equal(t, 70, snapshot.WarmupPercent)

	handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
	now = now.Add(11 * time.Second)
	RecordFirstResponse(handle)
	RecordAttemptFinish(handle, ChannelAttemptResult{StatusCode: http.StatusOK})
	snapshot, ok = GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, 10, snapshot.WarmupPercent)

	now = now.Add(10 * time.Second)
	snapshot, ok = GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateHealthy, snapshot.State)
	require.Equal(t, 100, snapshot.WarmupPercent)
}

func TestChannelHealthAdjustedWeightReducesWarmingAndSlowChannels(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.FirstResponseTimeoutSeconds = 10
	// Slow-response weight halving keys off SlowFirstResponseSeconds (a slow but
	// working upstream is degraded, never isolated), so drive it at the same 10s
	// bar this test uses for the timeout knobs.
	setting.SlowFirstResponseSeconds = 10
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8820
	OpenChannel(channelID, "test open")
	RecordProbeResult(channelID, true, "")
	RecordProbeResult(channelID, true, "")

	require.Equal(t, 10, adjustChannelHealthWeight(channelID, "", 100, 0))

	now = now.Add(40 * time.Second)
	require.Equal(t, 70, adjustChannelHealthWeight(channelID, "", 100, 0))

	handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
	now = now.Add(11 * time.Second)
	RecordFirstResponse(handle)
	RecordAttemptFinish(handle, ChannelAttemptResult{StatusCode: http.StatusOK})

	require.Equal(t, 5, adjustChannelHealthWeight(channelID, "", 100, 0))
}

func TestChannelHealthWarmupRepeatedFailuresReopenChannel(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.MinSamples = 2
	setting.MinFailures = 2
	setting.ErrorRateThreshold = 0.5
	setting.ConsecutiveFailureThreshold = 10

	const channelID = 8811
	OpenChannel(channelID, "test open")
	RecordProbeResult(channelID, true, "")
	RecordProbeResult(channelID, true, "")

	handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
	RecordAttemptFinish(handle, ChannelAttemptResult{Error: channelHealthTestUpstreamError()})
	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateWarming, snapshot.State)

	handle = RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
	RecordAttemptFinish(handle, ChannelAttemptResult{Error: channelHealthTestUpstreamError()})

	require.False(t, IsChannelAvailable(channelID))
	snapshot, ok = GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateOpen, snapshot.State)
	require.Contains(t, snapshot.Reason, "warmup unhealthy")
}

func TestChannelHealthHalfOpenAttemptSuccessCountsTowardRecovery(t *testing.T) {
	withChannelHealthTestSettings(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8809
	OpenChannel(channelID, "test open")
	now = now.Add(31 * time.Second)
	require.True(t, MarkChannelProbing(channelID))
	handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
	RecordFirstResponse(handle)
	RecordAttemptFinish(handle, ChannelAttemptResult{StatusCode: http.StatusOK})

	require.False(t, IsChannelAvailable(channelID))
	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateProbing, snapshot.State)
	require.Equal(t, 1, snapshot.ProbeSuccesses)
	require.False(t, snapshot.ProbeInProgress)

	now = now.Add(31 * time.Second)
	require.True(t, MarkChannelProbing(channelID))
	handle = RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
	RecordFirstResponse(handle)
	RecordAttemptFinish(handle, ChannelAttemptResult{StatusCode: http.StatusOK})

	snapshot, ok = GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateWarming, snapshot.State)
	require.Equal(t, 10, snapshot.WarmupPercent)

	now = now.Add(61 * time.Second)
	require.True(t, IsChannelAvailable(channelID))
	snapshot, ok = GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateHealthy, snapshot.State)
}

func TestChannelHealthAvailabilityReadsIsolationCache(t *testing.T) {
	withChannelHealthTestSettings(t)

	const channelID = 8805
	OpenChannel(channelID, "cached isolate")

	// Drop the in-memory state so availability must be resolved from the
	// isolation cache. Clear every shard to stay independent of the hash.
	for i := range channelHealthShards {
		channelHealthShards[i].Lock()
		channelHealthShards[i].channels = make(map[string]*channelHealthStateData)
		channelHealthShards[i].Unlock()
	}

	require.False(t, IsChannelAvailable(channelID))
}

func TestChannelHealthAvailabilityHonorsIsolationCacheOverLocalHealthyState(t *testing.T) {
	withChannelHealthTestSettings(t)

	const channelID = 8808
	handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
	RecordFirstResponse(handle)
	RecordAttemptFinish(handle, ChannelAttemptResult{StatusCode: http.StatusOK})
	require.True(t, IsChannelAvailable(channelID))

	err := getChannelHealthIsolationCache().SetWithTTL(channelHealthCacheKey(channelHealthScope{channelID: channelID}), ChannelHealthSnapshot{
		ChannelID: channelID,
		State:     ChannelHealthStateOpen,
		Reason:    "remote isolate",
	}, time.Minute)
	require.NoError(t, err)

	require.False(t, IsChannelAvailable(channelID))
	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateV2Unavailable, snapshot.StateV2)
	require.Equal(t, 0, snapshot.TrafficPercent)
}

func TestChannelHealthStaleIsolationSnapshotCanBeClaimedForRecoveryProbe(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.MaxIsolationSeconds = 60
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8821
	require.NoError(t, getChannelHealthIsolationCache().SetWithTTL(channelHealthCacheKey(channelHealthScope{channelID: channelID}), ChannelHealthSnapshot{
		ChannelID:   channelID,
		State:       ChannelHealthStateOpen,
		Reason:      "remote stale isolate",
		OpenedAt:    now.Add(-2 * time.Minute).Unix(),
		NextProbeAt: now.Add(time.Hour).Unix(),
	}, time.Minute))

	require.True(t, IsChannelProbeAvailable(channelID))
	require.True(t, MarkChannelProbing(channelID))
	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateProbing, snapshot.State)
	require.True(t, snapshot.ProbeInProgress)
}

func TestChannelHealthProbeRecoveryRequiresHealthyLatencyWindow(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.FirstResponseTimeoutSeconds = 10
	setting.MinSamples = 2
	setting.MinFailures = 1
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8822
	OpenChannel(channelID, "test open")

	for i := 0; i < 2; i++ {
		now = now.Add(31 * time.Second)
		require.True(t, MarkChannelProbing(channelID))
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
		now = now.Add(11 * time.Second)
		RecordFirstResponse(handle)
		RecordAttemptFinish(handle, ChannelAttemptResult{StatusCode: http.StatusOK})
	}

	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateProbing, snapshot.State)
	require.Contains(t, snapshot.Reason, "first_response")

	now = now.Add(181 * time.Second)
	RecordProbeResult(channelID, true, "")
	RecordProbeResult(channelID, true, "")

	snapshot, ok = GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateWarming, snapshot.State)
}

func TestChannelHealthWarmupThrottlesSingleFailureInsteadOfReopeningImmediately(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.MinSamples = 4
	setting.MinFailures = 2
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8823
	OpenChannel(channelID, "test open")
	RecordProbeResult(channelID, true, "")
	RecordProbeResult(channelID, true, "")

	now = now.Add(20 * time.Second)
	handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
	RecordAttemptFinish(handle, ChannelAttemptResult{Error: channelHealthTestUpstreamError()})

	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateWarming, snapshot.State)
	require.Equal(t, setting.WarmupStartPercent, snapshot.WarmupPercent)
	require.Equal(t, setting.WarmupStartPercent, snapshot.WarmupThrottlePercent)
}

func TestRunDueChannelHealthProbesSkipsManualDisabledChannel(t *testing.T) {
	withChannelHealthTestSettings(t)
	withChannelHealthSelectionDB(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	OpenChannel(9101, "runtime isolate")
	now = now.Add(31 * time.Second)
	called := false
	SetChannelHealthProbeFunc(func(ctx context.Context, channel *model.Channel, modelName string) error {
		called = true
		return nil
	})
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 9101).Update("status", common.ChannelStatusManuallyDisabled).Error)
	model.CacheUpdateChannelStatus(9101, common.ChannelStatusManuallyDisabled)

	RunDueChannelHealthProbes()
	time.Sleep(20 * time.Millisecond)

	require.False(t, called)
	require.False(t, IsChannelAvailable(9101))
}

func TestRunDueChannelHealthProbesSkipsAutoDisabledChannel(t *testing.T) {
	withChannelHealthTestSettings(t)
	withChannelHealthSelectionDB(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	OpenChannel(9101, "runtime isolate")
	now = now.Add(31 * time.Second)
	called := false
	SetChannelHealthProbeFunc(func(ctx context.Context, channel *model.Channel, modelName string) error {
		called = true
		return nil
	})
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 9101).Update("status", common.ChannelStatusAutoDisabled).Error)
	model.CacheUpdateChannelStatus(9101, common.ChannelStatusAutoDisabled)

	RunDueChannelHealthProbes()
	time.Sleep(20 * time.Millisecond)

	require.False(t, called)
	require.False(t, IsChannelAvailable(9101))
}

func channelHealthTestUpstreamError() *types.NewAPIError {
	return types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
}

func TestClassifyChannelHealthFailureIgnoresClientErrors(t *testing.T) {
	withChannelHealthTestSettings(t)

	err := types.NewOpenAIError(errors.New("bad request"), types.ErrorCodeInvalidRequest, http.StatusBadRequest)
	sample, failed := classifyChannelAttemptResult(ChannelAttemptResult{Error: err})
	require.False(t, sample, "client 4xx must not enter the health window")
	require.False(t, failed)
}

func TestClassifyChannelHealthGatewayErrorsAreFailures(t *testing.T) {
	withChannelHealthTestSettings(t)

	for _, code := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		err := types.NewOpenAIError(errors.New("upstream unusable"), types.ErrorCodeDoRequestFailed, code)
		sample, failed := classifyChannelAttemptResult(ChannelAttemptResult{Error: err})
		require.Truef(t, sample, "gateway %d must be sampled", code)
		require.Truef(t, failed, "gateway %d must count as a failure", code)
	}
}

// Channel/model-mapped errors used to force isolation. Under the gateway-only
// contract they are no longer failures — they are not gateway errors and are
// skip-retry, so they are neither sampled nor failed and never isolate a channel.
func TestClassifyChannelHealthChannelErrorNoLongerFails(t *testing.T) {
	withChannelHealthTestSettings(t)

	err := types.NewError(errors.New("model mapping failed"), types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	sample, failed := classifyChannelAttemptResult(ChannelAttemptResult{Error: err})
	require.False(t, failed, "channel errors must no longer trigger isolation")
	require.False(t, sample)
}

// 429 rate limiting and 500 internal errors reach upstream, so they are sampled
// (to give the gateway error rate an honest denominator) but never count as
// failures — they must not isolate the channel.
func TestClassifyChannelHealthRateLimitAndServerErrorSampledNotFailed(t *testing.T) {
	withChannelHealthTestSettings(t)

	for _, code := range []int{http.StatusTooManyRequests, http.StatusInternalServerError} {
		err := types.NewOpenAIError(errors.New("transient"), types.ErrorCodeDoRequestFailed, code)
		sample, failed := classifyChannelAttemptResult(ChannelAttemptResult{Error: err})
		require.Truef(t, sample, "status %d should be sampled", code)
		require.Falsef(t, failed, "status %d must not count as a failure", code)
	}
}

func TestRecordAttemptFinishDoesNotSampleIgnoredClientErrors(t *testing.T) {
	withChannelHealthTestSettings(t)

	const channelID = 8807
	handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
	err := types.NewOpenAIError(errors.New("bad request"), types.ErrorCodeInvalidRequest, http.StatusBadRequest)

	RecordAttemptFinish(handle, ChannelAttemptResult{Error: err})

	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, 0, snapshot.WindowSamples)
	require.Equal(t, 0, snapshot.WindowFailures)
	require.True(t, IsChannelAvailable(channelID))
}

func TestChannelHealthImmediatelyIsolatesUnauthorizedChannel(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.MinSamples = 100
	setting.MinFailures = 100
	setting.ConsecutiveFailureThreshold = 100

	const channelID = 8834
	handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
	unauthorized := types.NewOpenAIError(errors.New("invalid API key"), "invalid_api_key", http.StatusUnauthorized)
	RecordAttemptFinish(handle, ChannelAttemptResult{Error: unauthorized})

	require.False(t, IsChannelAvailable(channelID))
	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateOpen, snapshot.State)
	require.Contains(t, snapshot.Reason, "status_code=401")
	require.Equal(t, 0, snapshot.WindowSamples, "terminal account errors must not pollute gateway error-rate samples")
}

func TestChannelHealthImmediatelyIsolatesExhaustedBalance(t *testing.T) {
	tests := []struct {
		name       string
		channelID  int
		statusCode int
		errorCode  types.ErrorCode
		message    string
	}{
		{name: "payment required", channelID: 8835, statusCode: http.StatusPaymentRequired, errorCode: types.ErrorCodeDoRequestFailed, message: "payment required"},
		{name: "insufficient quota code", channelID: 8836, statusCode: http.StatusTooManyRequests, errorCode: "insufficient_quota", message: "quota exhausted"},
		{name: "english balance message", channelID: 8837, statusCode: http.StatusForbidden, errorCode: types.ErrorCodeDoRequestFailed, message: "credit balance is too low"},
		{name: "chinese balance message", channelID: 8838, statusCode: http.StatusBadRequest, errorCode: types.ErrorCodeDoRequestFailed, message: "账户余额不足"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withChannelHealthTestSettings(t)
			handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: tt.channelID})
			err := types.NewOpenAIError(errors.New(tt.message), tt.errorCode, tt.statusCode)

			RecordAttemptFinish(handle, ChannelAttemptResult{Error: err})

			require.False(t, IsChannelAvailable(tt.channelID))
			snapshot, ok := GetChannelHealthSnapshot(tt.channelID)
			require.True(t, ok)
			require.Equal(t, ChannelHealthStateOpen, snapshot.State)
			require.Contains(t, snapshot.Reason, "terminal upstream error")
			require.Equal(t, 0, snapshot.WindowSamples)
		})
	}
}

func TestChannelHealthDoesNotMisclassifyBalanceOrLocalQuotaErrors(t *testing.T) {
	tests := []struct {
		name      string
		channelID int
		err       *types.NewAPIError
	}{
		{
			name:      "unrelated balance wording",
			channelID: 8839,
			err:       types.NewOpenAIError(errors.New("request has an invalid load balance option"), types.ErrorCodeInvalidRequest, http.StatusBadRequest),
		},
		{
			name:      "local user quota",
			channelID: 8840,
			err:       types.NewErrorWithStatusCode(errors.New("用户额度不足"), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden),
		},
		{
			name:      "local pre-consume quota",
			channelID: 8841,
			err:       types.NewErrorWithStatusCode(errors.New("预扣费失败：余额不足"), types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withChannelHealthTestSettings(t)
			handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: tt.channelID})

			RecordAttemptFinish(handle, ChannelAttemptResult{Error: tt.err})

			require.True(t, IsChannelAvailable(tt.channelID))
			snapshot, ok := GetChannelHealthSnapshot(tt.channelID)
			require.True(t, ok)
			require.Equal(t, ChannelHealthStateHealthy, snapshot.State)
		})
	}
}

// 503 (service unavailable, e.g. "no available upstream account") reaching the
// error-rate threshold isolates the channel — requirement 1.
func TestChannelHealthIsolatesOnGatewayServiceUnavailable(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.ConsecutiveFailureThreshold = 100

	const channelID = 8830
	unavailable := types.NewOpenAIError(errors.New("no available upstream account"), types.ErrorCodeDoRequestFailed, http.StatusServiceUnavailable)
	for i := 0; i < 5; i++ {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
		RecordAttemptFinish(handle, ChannelAttemptResult{Error: unavailable})
	}
	for i := 0; i < 5; i++ {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
		RecordFirstResponse(handle)
		RecordAttemptFinish(handle, ChannelAttemptResult{})
	}

	require.False(t, IsChannelAvailable(channelID))
	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateOpen, snapshot.State)
}

// Consecutive 504 gateway timeouts mean the upstream is unusable — requirement 3.
// 504 is always-skip-retry, so this also guards that gateway matching runs before
// the sampling gate.
func TestChannelHealthIsolatesOnConsecutiveGatewayTimeouts(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.ConsecutiveFailureThreshold = 5

	const channelID = 8831
	timeout := types.NewOpenAIError(errors.New("gateway timeout"), types.ErrorCodeDoRequestFailed, http.StatusGatewayTimeout)
	for i := 0; i < 5; i++ {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
		RecordAttemptFinish(handle, ChannelAttemptResult{Error: timeout})
	}

	require.False(t, IsChannelAvailable(channelID))
	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateOpen, snapshot.State)
	require.Contains(t, snapshot.Reason, "consecutive")
}

// A flood of 429 rate limits (or 500s) must never isolate: they are not gateway
// errors, only rate limiting / transient server errors on a reachable upstream.
func TestChannelHealthDoesNotIsolateOnRateLimitFlood(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.ConsecutiveFailureThreshold = 5

	const channelID = 8832
	rateLimited := types.NewOpenAIError(errors.New("rate limited"), types.ErrorCodeDoRequestFailed, http.StatusTooManyRequests)
	for i := 0; i < 30; i++ {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
		RecordAttemptFinish(handle, ChannelAttemptResult{Error: rateLimited})
	}

	require.True(t, IsChannelAvailable(channelID), "429 rate limiting must not isolate a channel")
	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateHealthy, snapshot.State)
}

// A slow-but-working upstream (first response >= SlowFirstResponseSeconds) is
// degraded via weight, never isolated — requirement 2.
func TestChannelHealthSlowFirstResponseDegradesWeightWithoutIsolation(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.SlowFirstResponseSeconds = 18
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8833
	for i := 0; i < 10; i++ {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
		now = now.Add(20 * time.Second) // first byte after 20s (>= 18s bar)
		RecordFirstResponse(handle)
		RecordAttemptFinish(handle, ChannelAttemptResult{StatusCode: http.StatusOK})
	}

	require.True(t, IsChannelAvailable(channelID), "slow first response must degrade, not isolate")
	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Equal(t, ChannelHealthStateHealthy, snapshot.State)
	// Weight halved for the slow-but-healthy channel.
	require.Equal(t, 50, adjustChannelHealthWeight(channelID, "", 100, 0))
}

func TestChannelHealthSlowRatioDegradesWeightWhenAverageIsFast(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.SlowFirstResponseSeconds = 18
	setting.FirstResponseTimeoutSeconds = 45
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8834
	for i := 0; i < 10; i++ {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
		if i < 2 {
			now = now.Add(20 * time.Second)
		} else {
			now = now.Add(time.Second)
		}
		RecordFirstResponse(handle)
		RecordAttemptFinish(handle, ChannelAttemptResult{StatusCode: http.StatusOK})
		now = now.Add(time.Second)
	}

	snapshot, ok := GetChannelHealthSnapshot(channelID)
	require.True(t, ok)
	require.Less(t, snapshot.AverageFirstResponseMs, float64(setting.SlowFirstResponseSeconds*1_000))
	require.InDelta(t, 0.2, snapshot.SlowFirstResponseRatio, 0.001)
	require.Equal(t, 50, adjustChannelHealthWeight(channelID, "", 100, 0))
}

func TestChannelHealthSlowWeightUsesHysteresisOnRecovery(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.WindowSeconds = 1
	setting.SlowFirstResponseSeconds = 18
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8835
	recordSuccess := func(firstResponse time.Duration) {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID})
		now = now.Add(firstResponse)
		RecordFirstResponse(handle)
		RecordAttemptFinish(handle, ChannelAttemptResult{StatusCode: http.StatusOK})
	}

	recordSuccess(20 * time.Second)
	require.Equal(t, 50, adjustChannelHealthWeight(channelID, "", 100, 0))

	now = now.Add(2 * time.Second)
	recordSuccess(16 * time.Second)
	require.Equal(t, 50, adjustChannelHealthWeight(channelID, "", 100, 0))

	now = now.Add(2 * time.Second)
	recordSuccess(13 * time.Second)
	require.Equal(t, 80, adjustChannelHealthWeight(channelID, "", 100, 0))
}

func TestCacheGetRandomSatisfiedChannelSkipsRuntimeOpenChannel(t *testing.T) {
	withChannelHealthTestSettings(t)
	withChannelHealthSelectionDB(t)

	OpenChannel(9101, "runtime isolate")

	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		TokenGroup: "default",
		ModelName:  "gpt-health-test",
		Retry:      common.GetPointer(0),
	})

	require.NoError(t, err)
	require.Equal(t, "default", group)
	require.NotNil(t, channel)
	require.Equal(t, 9102, channel.Id)
}

func TestCacheGetRandomSatisfiedChannelSkipsSingleRuntimeOpenChannel(t *testing.T) {
	withChannelHealthTestSettings(t)
	withChannelHealthSelectionDB(t)
	require.NoError(t, model.DB.Where("id = ?", 9102).Delete(&model.Channel{}).Error)
	require.NoError(t, model.DB.Where("channel_id = ?", 9102).Delete(&model.Ability{}).Error)
	model.InitChannelCache()

	OpenChannel(9101, "runtime isolate")

	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		TokenGroup: "default",
		ModelName:  "gpt-health-test",
		Retry:      common.GetPointer(0),
	})

	require.Error(t, err)
	require.Equal(t, "default", group)
	require.Nil(t, channel)
}

func TestCacheGetRandomSatisfiedChannelUsesDueProbingChannelWhenAllHealthyUnavailable(t *testing.T) {
	withChannelHealthTestSettings(t)
	withChannelHealthSelectionDB(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	OpenChannel(9101, "runtime isolate")
	OpenChannel(9102, "runtime isolate")
	now = now.Add(31 * time.Second)
	RecordProbeResult(9101, true, "")
	now = now.Add(31 * time.Second)

	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		TokenGroup: "default",
		ModelName:  "gpt-health-test",
		Retry:      common.GetPointer(0),
	})

	require.NoError(t, err)
	require.Equal(t, "default", group)
	require.NotNil(t, channel)
	require.Equal(t, 9101, channel.Id)
	snapshot, ok := GetChannelHealthSnapshot(9101)
	require.True(t, ok)
	require.True(t, snapshot.ProbeInProgress)
}

func TestCacheGetRandomSatisfiedChannelPrefersHealthyOverDueProbingChannel(t *testing.T) {
	withChannelHealthTestSettings(t)
	withChannelHealthSelectionDB(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	OpenChannel(9101, "runtime isolate")
	now = now.Add(31 * time.Second)
	RecordProbeResult(9101, true, "")
	now = now.Add(31 * time.Second)

	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		TokenGroup: "default",
		ModelName:  "gpt-health-test",
		Retry:      common.GetPointer(0),
	})

	require.NoError(t, err)
	require.Equal(t, "default", group)
	require.NotNil(t, channel)
	require.Equal(t, 9102, channel.Id)
	snapshot, ok := GetChannelHealthSnapshot(9101)
	require.True(t, ok)
	require.False(t, snapshot.ProbeInProgress)
}

func TestChannelHealthModelLevelIsolationDoesNotBlockOtherModels(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.ModelLevelEnabled = true
	withChannelHealthSelectionDB(t)
	addChannelHealthSelectionModel(t, "gpt-health-other")

	OpenChannelForModel(9101, "gpt-health-test", "model specific isolate")

	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		TokenGroup: "default",
		ModelName:  "gpt-health-test",
		Retry:      common.GetPointer(0),
	})
	require.NoError(t, err)
	require.Equal(t, "default", group)
	require.NotNil(t, channel)
	require.Equal(t, 9102, channel.Id)

	channel, group, err = CacheGetRandomSatisfiedChannel(&RetryParam{
		TokenGroup: "default",
		ModelName:  "gpt-health-other",
		Retry:      common.GetPointer(0),
	})
	require.NoError(t, err)
	require.Equal(t, "default", group)
	require.NotNil(t, channel)
	require.Equal(t, 9101, channel.Id)
}

func TestChannelHealthChannelLevelModeKeepsCurrentBehavior(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.ModelLevelEnabled = false
	withChannelHealthSelectionDB(t)
	addChannelHealthSelectionModel(t, "gpt-health-other")

	OpenChannelForModel(9101, "gpt-health-test", "channel isolate")

	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		TokenGroup: "default",
		ModelName:  "gpt-health-other",
		Retry:      common.GetPointer(0),
	})
	require.NoError(t, err)
	require.Equal(t, "default", group)
	require.NotNil(t, channel)
	require.Equal(t, 9102, channel.Id)
}

func TestChannelHealthEventsEmitOncePerTransitionAndSummarize(t *testing.T) {
	withChannelHealthTestSettings(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	OpenChannel(8813, "first isolate")
	CheckChannelHealthStuckRequests()
	OpenChannel(8813, "second isolate should not duplicate")
	events := GetChannelHealthEvents(ChannelHealthEventFilter{})
	require.Len(t, events, 1)
	require.Equal(t, ChannelHealthEventTypeOpened, events[0].Type)
	require.Equal(t, 8813, events[0].ChannelID)

	RecordProbeResult(8813, true, "")
	RecordProbeResult(8813, true, "")
	events = GetChannelHealthEvents(ChannelHealthEventFilter{})
	require.Len(t, events, 2)
	require.Equal(t, ChannelHealthEventTypeWarming, events[1].Type)

	now = now.Add(61 * time.Second)
	require.True(t, IsChannelAvailable(8813))
	events = GetChannelHealthEvents(ChannelHealthEventFilter{})
	require.Len(t, events, 3)
	require.Equal(t, ChannelHealthEventTypeRecovered, events[2].Type)

	report := GetChannelHealthReport(ChannelHealthEventFilter{})
	require.Equal(t, 1, report.IsolationCount)
	require.Equal(t, 1, report.RecoveryCount)
	require.Len(t, report.TopFailingChannels, 1)
	require.Equal(t, 8813, report.TopFailingChannels[0].ChannelID)
}

func TestChannelHealthEventsIncludeSnapshotAndStateTimeline(t *testing.T) {
	setting := withChannelHealthTestSettings(t)
	setting.MinSamples = 2
	setting.MinFailures = 2
	setting.ErrorRateThreshold = 0.5
	setting.FirstResponseTimeoutSeconds = 10
	setting.ModelLevelEnabled = true
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	const channelID = 8824
	for _, latency := range []time.Duration{100 * time.Millisecond, 300 * time.Millisecond} {
		handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: channelID, ModelName: "gpt-p2", Group: "vip"})
		now = now.Add(latency)
		RecordFirstResponse(handle)
		RecordAttemptFinish(handle, ChannelAttemptResult{Error: channelHealthTestUpstreamError()})
		now = now.Add(time.Second)
	}

	now = now.Add(31 * time.Second)
	require.True(t, MarkChannelProbingForModel(channelID, "gpt-p2"))
	now = now.Add(181 * time.Second)
	RecordProbeResultForModel(channelID, "gpt-p2", true, "")
	RecordProbeResultForModel(channelID, "gpt-p2", true, "")
	now = now.Add(61 * time.Second)
	require.True(t, IsChannelAvailableForModel(channelID, "gpt-p2"))

	events := GetChannelHealthEvents(ChannelHealthEventFilter{ChannelID: channelID, ModelName: "gpt-p2", Limit: 10})
	require.Len(t, events, 4)
	require.Equal(t, ChannelHealthEventTypeOpened, events[0].Type)
	require.Equal(t, 2, events[0].Snapshot.WindowSamples)
	require.Equal(t, 2, events[0].Snapshot.WindowFailures)
	require.Equal(t, 200.0, events[0].Snapshot.AverageFirstResponseMs)
	require.Equal(t, 300.0, events[0].Snapshot.P95FirstResponseMs)
	require.Equal(t, 30, events[0].Snapshot.ProbeBackoffSeconds)
	require.Equal(t, ChannelHealthEventTypeProbing, events[1].Type)
	require.Equal(t, ChannelHealthEventTypeWarming, events[2].Type)
	require.Equal(t, ChannelHealthEventTypeRecovered, events[3].Type)
	require.Equal(t, string(ChannelHealthStateHealthy), events[3].State)
}

func TestChannelHealthTimelineEventsDoNotNotify(t *testing.T) {
	withChannelHealthTestSettings(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })
	notifications := make(chan ChannelHealthEvent, 4)
	SetChannelHealthEventNotifyFuncForTest(func(event ChannelHealthEvent) {
		notifications <- event
	})

	const channelID = 8825
	OpenChannel(channelID, "runtime isolate")
	require.Equal(t, ChannelHealthEventTypeOpened, waitChannelHealthNotificationForTest(t, notifications).Type)

	now = now.Add(31 * time.Second)
	require.True(t, MarkChannelProbing(channelID))
	requireNoChannelHealthNotificationForTest(t, notifications)

	now = now.Add(181 * time.Second)
	RecordProbeResult(channelID, true, "")
	RecordProbeResult(channelID, true, "")
	requireNoChannelHealthNotificationForTest(t, notifications)

	now = now.Add(61 * time.Second)
	require.True(t, IsChannelAvailable(channelID))
	require.Equal(t, ChannelHealthEventTypeRecovered, waitChannelHealthNotificationForTest(t, notifications).Type)
}

func waitChannelHealthNotificationForTest(t *testing.T, notifications <-chan ChannelHealthEvent) ChannelHealthEvent {
	t.Helper()
	select {
	case event := <-notifications:
		return event
	case <-time.After(time.Second):
		t.Fatal("expected channel health notification")
	}
	return ChannelHealthEvent{}
}

func requireNoChannelHealthNotificationForTest(t *testing.T, notifications <-chan ChannelHealthEvent) {
	t.Helper()
	select {
	case event := <-notifications:
		t.Fatalf("unexpected channel health notification: %s", event.Type)
	default:
	}
}

func TestChannelHealthReportIncludesAverageFirstResponseLatency(t *testing.T) {
	withChannelHealthTestSettings(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	handle := RecordAttemptStart(ChannelAttemptMeta{ChannelID: 8814})
	now = now.Add(100 * time.Millisecond)
	RecordFirstResponse(handle)
	RecordAttemptFinish(handle, ChannelAttemptResult{StatusCode: http.StatusOK})

	now = now.Add(time.Second)
	handle = RecordAttemptStart(ChannelAttemptMeta{ChannelID: 8814})
	now = now.Add(300 * time.Millisecond)
	RecordFirstResponse(handle)
	RecordAttemptFinish(handle, ChannelAttemptResult{StatusCode: http.StatusOK})

	report := GetChannelHealthReport(ChannelHealthEventFilter{})
	require.Equal(t, 200.0, report.AverageFirstResponseMs)

	snapshot, ok := GetChannelHealthSnapshot(8814)
	require.True(t, ok)
	require.Equal(t, 200.0, snapshot.AverageFirstResponseMs)
}

func TestChannelHealthEventsFilterByGroupAndState(t *testing.T) {
	withChannelHealthTestSettings(t)
	now := time.Unix(1_700_000_000, 0)
	SetChannelHealthNowFuncForTest(func() time.Time { return now })

	for i := 0; i < 5; i++ {
		handle := RecordAttemptStart(ChannelAttemptMeta{
			ChannelID: 8815,
			ModelName: "gpt-filter",
			Group:     "vip",
		})
		RecordAttemptFinish(handle, ChannelAttemptResult{Error: channelHealthTestUpstreamError()})
	}

	OpenChannelForModel(8816, "gpt-filter", "default isolate")

	events := GetChannelHealthEvents(ChannelHealthEventFilter{
		Group: "vip",
		State: string(ChannelHealthStateOpen),
	})
	require.Len(t, events, 1)
	require.Equal(t, 8815, events[0].ChannelID)
	require.Equal(t, "vip", events[0].Group)
	require.Equal(t, string(ChannelHealthStateOpen), events[0].State)
}

func TestClearChannelAffinityByChannelIDDeletesReverseIndexedKeys(t *testing.T) {
	withChannelHealthTestSettings(t)

	keyOne := "health-affinity:one"
	keyTwo := "health-affinity:two"
	cache := getChannelAffinityCache()
	require.NoError(t, cache.SetWithTTL(keyOne, 9201, time.Minute))
	require.NoError(t, cache.SetWithTTL(keyTwo, 9201, time.Minute))
	RecordChannelAffinityKeyForChannelForTest(9201, keyOne, time.Minute)
	RecordChannelAffinityKeyForChannelForTest(9201, keyTwo, time.Minute)
	t.Cleanup(func() {
		_, _ = cache.DeleteMany([]string{keyOne, keyTwo})
	})

	deleted := ClearChannelAffinityByChannelID(9201)

	require.Equal(t, 2, deleted)
	_, found, err := cache.Get(keyOne)
	require.NoError(t, err)
	require.False(t, found)
	_, found, err = cache.Get(keyTwo)
	require.NoError(t, err)
	require.False(t, found)
}

func TestChannelAffinityShouldYieldToRecoveredHigherPriorityChannel(t *testing.T) {
	withChannelHealthTestSettings(t)
	withChannelHealthSelectionDB(t)

	require.True(t, IsChannelAffinityPriorityStale("default", "gpt-health-test", 9102))
	require.False(t, IsChannelAffinityPriorityStale("default", "gpt-health-test", 9101))
}

func TestChannelAffinityKeepsLowerPriorityWhenHigherPriorityIsOpen(t *testing.T) {
	withChannelHealthTestSettings(t)
	withChannelHealthSelectionDB(t)

	OpenChannel(9101, "runtime isolate")

	require.False(t, IsChannelAffinityPriorityStale("default", "gpt-health-test", 9102))
}

func withChannelHealthSelectionDB(t *testing.T) {
	t.Helper()

	oldDB := model.DB
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	common.MemoryCacheEnabled = true
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB = db

	pHigh := int64(10)
	pLow := int64(1)
	weight := uint(100)
	require.NoError(t, db.Create(&model.Channel{
		Id:       9101,
		Type:     constant.ChannelTypeOpenAI,
		Key:      "sk-high",
		Status:   common.ChannelStatusEnabled,
		Name:     "high-priority",
		Priority: &pHigh,
		Weight:   &weight,
		Models:   "gpt-health-test",
		Group:    "default",
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:       9102,
		Type:     constant.ChannelTypeOpenAI,
		Key:      "sk-low",
		Status:   common.ChannelStatusEnabled,
		Name:     "low-priority",
		Priority: &pLow,
		Weight:   &weight,
		Models:   "gpt-health-test",
		Group:    "default",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-health-test", ChannelId: 9101, Enabled: true, Priority: &pHigh, Weight: weight}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-health-test", ChannelId: 9102, Enabled: true, Priority: &pLow, Weight: weight}).Error)
	model.InitChannelCache()

	t.Cleanup(func() {
		channelHealthProbeWaitGroup.Wait()
		model.DB = oldDB
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		model.InitChannelCache()
	})
}

func addChannelHealthSelectionModel(t *testing.T, modelName string) {
	t.Helper()

	pHigh := int64(10)
	pLow := int64(1)
	weight := uint(100)
	for _, channelID := range []int{9101, 9102} {
		priority := &pHigh
		if channelID == 9102 {
			priority = &pLow
		}
		require.NoError(t, model.DB.Create(&model.Ability{
			Group:     "default",
			Model:     modelName,
			ChannelId: channelID,
			Enabled:   true,
			Priority:  priority,
			Weight:    weight,
		}).Error)
	}
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id IN ?", []int{9101, 9102}).Update("models", "gpt-health-test,"+modelName).Error)
	model.InitChannelCache()
}

// waitForChannelHealthFlushIdle blocks until every shard has drained its flush
// queue, so a test can assert on the isolation cache without racing an
// in-flight drainer.
func waitForChannelHealthFlushIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		idle := true
		for i := range channelHealthShards {
			shard := &channelHealthShards[i]
			shard.Lock()
			busy := shard.flushing || len(shard.flushQueue) > 0 || len(shard.pending) > 0
			shard.Unlock()
			if busy {
				idle = false
				break
			}
		}
		if idle {
			return
		}
		require.False(t, time.Now().After(deadline), "flush queue did not drain in time")
		time.Sleep(time.Millisecond)
	}
}

// TestChannelHealthFlushQueuePreservesScopeOrder pins the P1 invariant that the
// single-drainer FIFO restored: when a stale Set(Open) is enqueued before a
// newer Delete for the same scope, the last write to reach the isolation cache
// must be the Delete, so a recovered channel is never re-isolated by a
// reordered flush. It drives the drainer directly with two ordered batches
// (the shape two goroutines produce when they transition one channel under the
// shard lock) instead of relying on Redis timing.
func TestChannelHealthFlushQueuePreservesScopeOrder(t *testing.T) {
	withChannelHealthTestSettings(t)

	const channelID = 8899
	scope := channelHealthScope{channelID: channelID}
	shard := channelHealthShardFor(channelID)
	setting := defaultChannelHealthSetting()
	openSnapshot := ChannelHealthSnapshot{ChannelID: channelID, State: ChannelHealthStateOpen}

	// Older transition (Open) enqueued first, newer transition (Delete/Healthy)
	// second — the drainer must apply them in that order and leave the cache
	// empty.
	shard.Lock()
	shard.queueIsolationPersist(scope, openSnapshot, channelHealthIsolationTTL(setting))
	shard.flushQueue = append(shard.flushQueue, shard.pending)
	shard.pending = nil
	shard.queueIsolationDelete(scope)
	shard.flushQueue = append(shard.flushQueue, shard.pending)
	shard.pending = nil
	shard.flushing = true
	shard.Unlock()
	shard.drainFlushQueue()

	_, found, err := getChannelHealthIsolationCache().Get(channelHealthCacheKey(scope))
	require.NoError(t, err)
	require.False(t, found, "Delete enqueued after Open must win; recovered channel stayed isolated")

	// Reverse order: Delete first, then Open. Open is the last transition, so it
	// must persist.
	shard.Lock()
	shard.queueIsolationDelete(scope)
	shard.flushQueue = append(shard.flushQueue, shard.pending)
	shard.pending = nil
	shard.queueIsolationPersist(scope, openSnapshot, channelHealthIsolationTTL(setting))
	shard.flushQueue = append(shard.flushQueue, shard.pending)
	shard.pending = nil
	shard.flushing = true
	shard.Unlock()
	shard.drainFlushQueue()

	stored, found, err := getChannelHealthIsolationCache().Get(channelHealthCacheKey(scope))
	require.NoError(t, err)
	require.True(t, found, "Open enqueued last must persist")
	require.Equal(t, ChannelHealthStateOpen, stored.State)
}

// recoverChannelHealthToHealthyForTest drives the Healthy transition the probe
// recovery path performs (mark healthy in memory + queue the isolation delete),
// so a test can race it against OpenChannel on the same channel.
func recoverChannelHealthToHealthyForTest(channelID int) {
	scope := channelHealthScope{channelID: channelID}
	shard := channelHealthShardFor(channelID)
	shard.Lock()
	defer shard.unlockAndFlush()
	state := getOrCreateChannelHealthLocked(shard, scope)
	markChannelHealthyLocked(state)
	shard.queueIsolationDelete(scope)
}

// TestChannelHealthConcurrentTransitionsConvergeCacheAndMemory hammers one
// channel with interleaved Open and recovery transitions from many goroutines
// (the exact concurrency that reordered flushes under the old design) and then
// asserts, after a final deterministic recovery, that the isolation cache
// agrees with the in-memory state. Run under -race it also guards the shard's
// pending/flushQueue bookkeeping.
func TestChannelHealthConcurrentTransitionsConvergeCacheAndMemory(t *testing.T) {
	withChannelHealthTestSettings(t)

	const channelID = 8901
	scope := channelHealthScope{channelID: channelID}

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			OpenChannel(channelID, "concurrent isolate")
		}()
		go func() {
			defer wg.Done()
			recoverChannelHealthToHealthyForTest(channelID)
		}()
	}
	wg.Wait()

	// Settle the churn, then apply a single known-last transition so the
	// expected end state is deterministic regardless of who won the race.
	waitForChannelHealthFlushIdle(t)
	recoverChannelHealthToHealthyForTest(channelID)
	waitForChannelHealthFlushIdle(t)

	shard := channelHealthShardFor(channelID)
	shard.Lock()
	memState := shard.channels[channelHealthScopeKey(scope)].state
	shard.Unlock()
	require.Equal(t, ChannelHealthStateHealthy, memState)

	_, found, err := getChannelHealthIsolationCache().Get(channelHealthCacheKey(scope))
	require.NoError(t, err)
	require.False(t, found, "cache must match the final Healthy memory state, not a stale Open")
}
