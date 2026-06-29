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
export const CHANNEL_HEALTH_NUMBER_FIELD_KEYS = [
  'window_seconds',
  'min_samples',
  'min_failures',
  'error_rate_threshold',
  'consecutive_failure_threshold',
  'first_response_timeout_seconds',
  'stuck_inflight_threshold',
  'single_stuck_timeout_seconds',
  'probe_interval_seconds',
  'probe_timeout_seconds',
  'probe_successes_to_recover',
  'probe_backoff_max_seconds',
  'warmup_duration_seconds',
  'warmup_start_percent',
  'warmup_step_percent',
] as const

export type ChannelHealthNumberFieldKey =
  (typeof CHANNEL_HEALTH_NUMBER_FIELD_KEYS)[number]

export type ChannelHealthSettingKey =
  | 'channel_health_setting.enabled'
  | 'channel_health_setting.warmup_enabled'
  | 'channel_health_setting.preset'
  | 'channel_health_setting.model_level_enabled'
  | 'channel_health_setting.events_enabled'
  | `channel_health_setting.${ChannelHealthNumberFieldKey}`
  | 'channel_health_setting.alert_min_interval_seconds'

export type ChannelHealthSettings = {
  'channel_health_setting.enabled': boolean
  'channel_health_setting.warmup_enabled': boolean
  'channel_health_setting.preset': ChannelHealthPreset
  'channel_health_setting.model_level_enabled': boolean
  'channel_health_setting.events_enabled': boolean
  'channel_health_setting.alert_min_interval_seconds': number
} & Record<`channel_health_setting.${ChannelHealthNumberFieldKey}`, number>

export const CHANNEL_MULTIPLIER_MONITOR_SETTING_KEY =
  'channel_multiplier_monitor_setting.interval_minutes' as const

export type ChannelMultiplierMonitorSettings = {
  [CHANNEL_MULTIPLIER_MONITOR_SETTING_KEY]: number
}

export const CHANNEL_AUTO_PRIORITY_SETTING_KEYS = [
  'channel_auto_priority_setting.enabled',
  'channel_auto_priority_setting.min_weight',
  'channel_auto_priority_setting.max_weight',
  'channel_auto_priority_setting.latency_guard_enabled',
  'channel_auto_priority_setting.latency_threshold_seconds',
  'channel_auto_priority_setting.latency_window_minutes',
  'channel_auto_priority_setting.latency_min_samples',
  'channel_auto_priority_setting.latency_slow_ratio_threshold',
  'channel_auto_priority_setting.latency_recovery_ratio_threshold',
  'channel_auto_priority_setting.latency_retained_weight_percent',
  'channel_auto_priority_setting.latency_priority_penalty',
] as const

export type ChannelAutoPrioritySettingKey =
  (typeof CHANNEL_AUTO_PRIORITY_SETTING_KEYS)[number]

export type ChannelAutoPrioritySettings = {
  'channel_auto_priority_setting.enabled': boolean
  'channel_auto_priority_setting.min_weight': number
  'channel_auto_priority_setting.max_weight': number
  'channel_auto_priority_setting.latency_guard_enabled': boolean
  'channel_auto_priority_setting.latency_threshold_seconds': number
  'channel_auto_priority_setting.latency_window_minutes': number
  'channel_auto_priority_setting.latency_min_samples': number
  'channel_auto_priority_setting.latency_slow_ratio_threshold': number
  'channel_auto_priority_setting.latency_recovery_ratio_threshold': number
  'channel_auto_priority_setting.latency_retained_weight_percent': number
  'channel_auto_priority_setting.latency_priority_penalty': number
}

export type ChannelHealthPanelSettings = ChannelHealthSettings &
  ChannelMultiplierMonitorSettings &
  ChannelAutoPrioritySettings

export const CHANNEL_HEALTH_PRESETS = [
  'conservative',
  'balanced',
  'aggressive',
  'custom',
] as const

export type ChannelHealthPreset = (typeof CHANNEL_HEALTH_PRESETS)[number]

export type ChannelHealthFieldGroup = 'errors' | 'stuck' | 'probe' | 'warmup'

export type ChannelHealthNumberField = {
  key: ChannelHealthNumberFieldKey
  optionKey: `channel_health_setting.${ChannelHealthNumberFieldKey}`
  labelKey: string
  descriptionKey: string
  min: number
  max?: number
  step: number
  group: ChannelHealthFieldGroup
}

export const CHANNEL_HEALTH_SETTING_FIELDS = [
  {
    key: 'window_seconds',
    optionKey: 'channel_health_setting.window_seconds',
    labelKey: 'Sliding window (seconds)',
    descriptionKey: 'Recent request window used to calculate failure rate',
    min: 1,
    step: 1,
    group: 'errors',
  },
  {
    key: 'min_samples',
    optionKey: 'channel_health_setting.min_samples',
    labelKey: 'Minimum samples',
    descriptionKey: 'Minimum attempts before error-rate isolation can trigger',
    min: 1,
    step: 1,
    group: 'errors',
  },
  {
    key: 'min_failures',
    optionKey: 'channel_health_setting.min_failures',
    labelKey: 'Minimum failures',
    descriptionKey:
      'Minimum failed attempts before error-rate isolation can trigger',
    min: 1,
    step: 1,
    group: 'errors',
  },
  {
    key: 'error_rate_threshold',
    optionKey: 'channel_health_setting.error_rate_threshold',
    labelKey: 'Error rate threshold',
    descriptionKey: 'Failure ratio from 0 to 1 required to isolate a channel',
    min: 0,
    max: 1,
    step: 0.01,
    group: 'errors',
  },
  {
    key: 'consecutive_failure_threshold',
    optionKey: 'channel_health_setting.consecutive_failure_threshold',
    labelKey: 'Consecutive failures',
    descriptionKey: 'Failures in a row required for low-traffic isolation',
    min: 1,
    step: 1,
    group: 'errors',
  },
  {
    key: 'first_response_timeout_seconds',
    optionKey: 'channel_health_setting.first_response_timeout_seconds',
    labelKey: 'First response timeout (seconds)',
    descriptionKey:
      'Mark a request stuck when no upstream first response arrives in time',
    min: 1,
    step: 1,
    group: 'stuck',
  },
  {
    key: 'stuck_inflight_threshold',
    optionKey: 'channel_health_setting.stuck_inflight_threshold',
    labelKey: 'Stuck inflight threshold',
    descriptionKey: 'Open the channel when this many stuck requests accumulate',
    min: 1,
    step: 1,
    group: 'stuck',
  },
  {
    key: 'single_stuck_timeout_seconds',
    optionKey: 'channel_health_setting.single_stuck_timeout_seconds',
    labelKey: 'Single stuck timeout (seconds)',
    descriptionKey: 'Open the channel when one request stays stuck this long',
    min: 1,
    step: 1,
    group: 'stuck',
  },
  {
    key: 'probe_interval_seconds',
    optionKey: 'channel_health_setting.probe_interval_seconds',
    labelKey: 'Probe interval (seconds)',
    descriptionKey: 'Wait time before the next recovery probe',
    min: 1,
    step: 1,
    group: 'probe',
  },
  {
    key: 'probe_timeout_seconds',
    optionKey: 'channel_health_setting.probe_timeout_seconds',
    labelKey: 'Probe timeout (seconds)',
    descriptionKey: 'Maximum duration allowed for a recovery probe',
    min: 1,
    step: 1,
    group: 'probe',
  },
  {
    key: 'probe_successes_to_recover',
    optionKey: 'channel_health_setting.probe_successes_to_recover',
    labelKey: 'Probe successes to recover',
    descriptionKey:
      'Consecutive successful probes required to restore the channel',
    min: 1,
    step: 1,
    group: 'probe',
  },
  {
    key: 'probe_backoff_max_seconds',
    optionKey: 'channel_health_setting.probe_backoff_max_seconds',
    labelKey: 'Probe backoff max (seconds)',
    descriptionKey: 'Maximum probe backoff after repeated failures',
    min: 1,
    step: 1,
    group: 'probe',
  },
  {
    key: 'warmup_duration_seconds',
    optionKey: 'channel_health_setting.warmup_duration_seconds',
    labelKey: 'Warm-up duration (seconds)',
    descriptionKey: 'Time window used to gradually restore recovered traffic',
    min: 1,
    step: 1,
    group: 'warmup',
  },
  {
    key: 'warmup_start_percent',
    optionKey: 'channel_health_setting.warmup_start_percent',
    labelKey: 'Warm-up start percent',
    descriptionKey: 'Initial percentage of new selections allowed during warm-up',
    min: 1,
    max: 100,
    step: 1,
    group: 'warmup',
  },
  {
    key: 'warmup_step_percent',
    optionKey: 'channel_health_setting.warmup_step_percent',
    labelKey: 'Warm-up step percent',
    descriptionKey: 'Traffic percentage added at each warm-up step',
    min: 1,
    max: 100,
    step: 1,
    group: 'warmup',
  },
] as const satisfies readonly ChannelHealthNumberField[]

export const CHANNEL_HEALTH_SETTING_KEYS = [
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
] as const satisfies readonly ChannelHealthSettingKey[]

export const CHANNEL_HEALTH_DEFAULT_VALUES = {
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
} as const satisfies ChannelHealthSettings

export const CHANNEL_MULTIPLIER_MONITOR_DEFAULT_VALUES = {
  [CHANNEL_MULTIPLIER_MONITOR_SETTING_KEY]: 2,
} as const satisfies ChannelMultiplierMonitorSettings

export const CHANNEL_AUTO_PRIORITY_DEFAULT_VALUES = {
  'channel_auto_priority_setting.enabled': false,
  'channel_auto_priority_setting.min_weight': 20,
  'channel_auto_priority_setting.max_weight': 100,
  'channel_auto_priority_setting.latency_guard_enabled': false,
  'channel_auto_priority_setting.latency_threshold_seconds': 10,
  'channel_auto_priority_setting.latency_window_minutes': 10,
  'channel_auto_priority_setting.latency_min_samples': 20,
  'channel_auto_priority_setting.latency_slow_ratio_threshold': 0.3,
  'channel_auto_priority_setting.latency_recovery_ratio_threshold': 0.1,
  'channel_auto_priority_setting.latency_retained_weight_percent': 20,
  'channel_auto_priority_setting.latency_priority_penalty': 1,
} as const satisfies ChannelAutoPrioritySettings

export const CHANNEL_HEALTH_PRESET_VALUES = {
  conservative: {
    'channel_health_setting.window_seconds': 300,
    'channel_health_setting.min_samples': 20,
    'channel_health_setting.min_failures': 8,
    'channel_health_setting.error_rate_threshold': 0.6,
    'channel_health_setting.consecutive_failure_threshold': 8,
    'channel_health_setting.first_response_timeout_seconds': 60,
    'channel_health_setting.stuck_inflight_threshold': 5,
    'channel_health_setting.single_stuck_timeout_seconds': 120,
    'channel_health_setting.probe_interval_seconds': 60,
    'channel_health_setting.probe_timeout_seconds': 30,
    'channel_health_setting.probe_successes_to_recover': 3,
    'channel_health_setting.probe_backoff_max_seconds': 300,
    'channel_health_setting.warmup_duration_seconds': 120,
    'channel_health_setting.warmup_start_percent': 10,
    'channel_health_setting.warmup_step_percent': 20,
  },
  balanced: {
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
  },
  aggressive: {
    'channel_health_setting.window_seconds': 120,
    'channel_health_setting.min_samples': 6,
    'channel_health_setting.min_failures': 3,
    'channel_health_setting.error_rate_threshold': 0.3,
    'channel_health_setting.consecutive_failure_threshold': 3,
    'channel_health_setting.first_response_timeout_seconds': 30,
    'channel_health_setting.stuck_inflight_threshold': 2,
    'channel_health_setting.single_stuck_timeout_seconds': 60,
    'channel_health_setting.probe_interval_seconds': 20,
    'channel_health_setting.probe_timeout_seconds': 20,
    'channel_health_setting.probe_successes_to_recover': 2,
    'channel_health_setting.probe_backoff_max_seconds': 180,
    'channel_health_setting.warmup_duration_seconds': 45,
    'channel_health_setting.warmup_start_percent': 15,
    'channel_health_setting.warmup_step_percent': 35,
  },
} as const satisfies Record<
  Exclude<ChannelHealthPreset, 'custom'>,
  Record<`channel_health_setting.${ChannelHealthNumberFieldKey}`, number>
>

export function applyChannelHealthPreset<T extends ChannelHealthSettings>(
  current: T,
  preset: ChannelHealthPreset
): T {
  if (preset === 'custom') {
    return { ...current, 'channel_health_setting.preset': 'custom' }
  }
  return {
    ...current,
    ...CHANNEL_HEALTH_PRESET_VALUES[preset],
    'channel_health_setting.preset': preset,
  }
}

export function markChannelHealthPresetCustom<T extends ChannelHealthSettings>(
  current: T
): T {
  if (current['channel_health_setting.preset'] === 'custom') return current
  return { ...current, 'channel_health_setting.preset': 'custom' }
}

export function pickChannelHealthSettings(
  settings: Partial<Record<ChannelHealthSettingKey, boolean | number | string>>
): ChannelHealthSettings {
  const picked = {} as ChannelHealthSettings
  for (const key of CHANNEL_HEALTH_SETTING_KEYS) {
    const fallback = CHANNEL_HEALTH_DEFAULT_VALUES[key]
    const value = settings[key]
    ;(picked as Record<ChannelHealthSettingKey, boolean | number | string>)[key] =
      value ?? fallback
  }
  return picked
}
