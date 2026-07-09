package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
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
	}
	t.Cleanup(func() {
		*setting = original
		ResetChannelHealthForTest()
	})
	ResetChannelHealthForTest()
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

	channelHealth.Lock()
	state := channelHealth.channels[channelHealthScopeKey(channelHealthScope{channelID: channelID})]
	require.Len(t, state.inflight, 3)
	channelHealth.Unlock()

	now = now.Add(10 * time.Minute)
	CheckChannelHealthStuckRequests()

	channelHealth.Lock()
	require.Len(t, state.inflight, 0)
	channelHealth.Unlock()
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
	channelHealth.Lock()
	state := channelHealth.channels[channelHealthScopeKey(channelHealthScope{channelID: 9101})]
	state.nextProbeAt = now.Add(time.Hour)
	channelHealth.Unlock()

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
	require.Equal(t, 10, snapshot.WarmupPercent)
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
	require.Equal(t, 100, snapshot.WarmupPercent)
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

	channelHealth.Lock()
	channelHealth.channels = make(map[string]*channelHealthStateData)
	channelHealth.Unlock()

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
	require.False(t, ShouldRecordChannelHealthFailure((*gin.Context)(nil), err))
}

func TestClassifyChannelHealthFailureCountsChannelErrorsEvenWhenSkipRetry(t *testing.T) {
	withChannelHealthTestSettings(t)

	err := types.NewError(errors.New("model mapping failed"), types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())

	require.True(t, ShouldRecordChannelHealthFailure((*gin.Context)(nil), err))
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
	model.DB = db
	common.MemoryCacheEnabled = true
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))

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
