/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  applyChannelHealthPreset,
  CHANNEL_HEALTH_DEFAULT_VALUES,
  CHANNEL_HEALTH_PRESET_VALUES,
  CHANNEL_HEALTH_SETTING_FIELDS,
  CHANNEL_HEALTH_SETTING_KEYS,
  markChannelHealthPresetCustom,
} from './channel-health-settings.ts'

describe('channel health setting metadata', () => {
  test('lists every editable runtime health option with backend defaults', () => {
    assert.deepEqual(CHANNEL_HEALTH_SETTING_KEYS, [
      'channel_health_setting.enabled',
      'channel_health_setting.warmup_enabled',
      'channel_health_setting.preset',
      'channel_health_setting.model_level_enabled',
      'channel_health_setting.events_enabled',
      'channel_health_setting.alert_min_interval_seconds',
      'channel_health_setting.window_seconds',
      'channel_health_setting.min_samples',
      'channel_health_setting.min_failures',
      'channel_health_setting.error_rate_threshold',
      'channel_health_setting.consecutive_failure_threshold',
      'channel_health_setting.first_response_timeout_seconds',
      'channel_health_setting.stuck_inflight_threshold',
      'channel_health_setting.single_stuck_timeout_seconds',
      'channel_health_setting.probe_interval_seconds',
      'channel_health_setting.probe_timeout_seconds',
      'channel_health_setting.probe_successes_to_recover',
      'channel_health_setting.probe_backoff_max_seconds',
      'channel_health_setting.warmup_duration_seconds',
      'channel_health_setting.warmup_start_percent',
      'channel_health_setting.warmup_step_percent',
    ])

    assert.equal(CHANNEL_HEALTH_SETTING_FIELDS.length, 15)
    assert.deepEqual(CHANNEL_HEALTH_DEFAULT_VALUES, {
      'channel_health_setting.enabled': true,
      'channel_health_setting.warmup_enabled': true,
      'channel_health_setting.preset': 'balanced',
      'channel_health_setting.model_level_enabled': false,
      'channel_health_setting.events_enabled': true,
      'channel_health_setting.alert_min_interval_seconds': 60,
      'channel_health_setting.window_seconds': 180,
      'channel_health_setting.min_samples': 10,
      'channel_health_setting.min_failures': 5,
      'channel_health_setting.error_rate_threshold': 0.4,
      'channel_health_setting.consecutive_failure_threshold': 5,
      'channel_health_setting.first_response_timeout_seconds': 45,
      'channel_health_setting.stuck_inflight_threshold': 3,
      'channel_health_setting.single_stuck_timeout_seconds': 75,
      'channel_health_setting.probe_interval_seconds': 30,
      'channel_health_setting.probe_timeout_seconds': 30,
      'channel_health_setting.probe_successes_to_recover': 2,
      'channel_health_setting.probe_backoff_max_seconds': 300,
      'channel_health_setting.warmup_duration_seconds': 60,
      'channel_health_setting.warmup_start_percent': 10,
      'channel_health_setting.warmup_step_percent': 30,
    })
  })

  test('preset selection fills numeric values and manual edit marks custom', () => {
    const aggressive = applyChannelHealthPreset(
      CHANNEL_HEALTH_DEFAULT_VALUES,
      'aggressive'
    )

    assert.equal(aggressive['channel_health_setting.preset'], 'aggressive')
    assert.deepEqual(
      Object.fromEntries(
        Object.entries(aggressive).filter(([key]) =>
          key in CHANNEL_HEALTH_PRESET_VALUES.aggressive
        )
      ),
      CHANNEL_HEALTH_PRESET_VALUES.aggressive
    )

    const custom = markChannelHealthPresetCustom({
      ...aggressive,
      'channel_health_setting.window_seconds': 121,
    })
    assert.equal(custom['channel_health_setting.preset'], 'custom')
    assert.equal(custom['channel_health_setting.window_seconds'], 121)
  })
})
