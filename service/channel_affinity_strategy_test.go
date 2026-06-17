package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestChannelAffinityRecoveryStrategyPriorityFirstKeepsExistingBehavior(t *testing.T) {
	withChannelHealthTestSettings(t)
	withChannelHealthSelectionDB(t)
	withChannelAffinityRecoveryStrategyForTest(t, operation_setting.ChannelAffinityRecoveryStrategyPriorityFirst)

	require.True(t, IsChannelAffinityPriorityStale("default", "gpt-health-test", 9102))
}

func TestChannelAffinityRecoveryStrategyStableKeepsHealthyAffinity(t *testing.T) {
	withChannelHealthTestSettings(t)
	withChannelHealthSelectionDB(t)
	withChannelAffinityRecoveryStrategyForTest(t, operation_setting.ChannelAffinityRecoveryStrategyStableAffinity)

	require.False(t, IsChannelAffinityPriorityStale("default", "gpt-health-test", 9102))
}

func TestChannelAffinityRecoveryStrategyStrictOnlyMovesWhenUnusable(t *testing.T) {
	withChannelHealthTestSettings(t)
	withChannelHealthSelectionDB(t)
	withChannelAffinityRecoveryStrategyForTest(t, operation_setting.ChannelAffinityRecoveryStrategyStrictAffinity)

	require.False(t, IsChannelAffinityPriorityStale("default", "gpt-health-test", 9102))
	OpenChannel(9102, "runtime isolate")
	require.True(t, IsChannelAffinityPriorityStale("default", "gpt-health-test", 9102))
}

func withChannelAffinityRecoveryStrategyForTest(t *testing.T, strategy string) {
	t.Helper()

	setting := operation_setting.GetChannelAffinitySetting()
	original := *setting
	setting.Enabled = true
	setting.RecoveryStrategy = strategy
	t.Cleanup(func() {
		*setting = original
	})
}
