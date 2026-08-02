package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

// obsoleteChannelHealthOptionKeys are option keys written by the previous
// complex channel-health / auto-circuit-breaker implementation that was later
// reverted (commit "revert: remove channel health and monitoring features").
// The current simplified channel_health_setting only recognises a small set of
// keys, so these stale rows would otherwise be rejected on every option sync
// with "unknown channel health option" / type-mismatch log spam. They carry no
// runtime meaning anymore and are safe to delete.
var obsoleteChannelHealthOptionKeys = []string{
	"channel_health_setting.consecutive_failure_threshold",
	"channel_health_setting.error_rate_threshold",
	"channel_health_setting.first_response_timeout_seconds",
	"channel_health_setting.min_failures",
	"channel_health_setting.min_samples",
	"channel_health_setting.model_level_enabled",
	"channel_health_setting.performance_guard_enabled",
	"channel_health_setting.preset",
	"channel_health_setting.probe_interval_seconds",
	"channel_health_setting.probe_successes_to_recover",
	"channel_health_setting.single_stuck_timeout_seconds",
	"channel_health_setting.slow_first_response_seconds",
	"channel_health_setting.stuck_detection_enabled",
	"channel_health_setting.stuck_inflight_threshold",
	"channel_health_setting.warmup_duration_seconds",
	"channel_health_setting.warmup_step_percent",
	"channel_health_setting.window_seconds",
}

// cleanupObsoleteChannelHealthOptions removes stale option rows left over from
// the reverted channel-health feature. It is idempotent and safe to run on
// every boot. Runs before options are loaded so the stale keys never reach
// updateOptionMap. Works across SQLite/MySQL/PostgreSQL via GORM.
func cleanupObsoleteChannelHealthOptions() {
	result := DB.Where(commonKeyCol+" IN ?", obsoleteChannelHealthOptionKeys).Delete(&Option{})
	if result.Error != nil {
		common.SysError("failed to clean up obsolete channel health options: " + result.Error.Error())
		return
	}
	if result.RowsAffected > 0 {
		common.SysLog(fmt.Sprintf("cleaned up %d obsolete channel health option rows", result.RowsAffected))
	}
}
