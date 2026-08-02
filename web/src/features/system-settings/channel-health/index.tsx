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
import { SettingsPage } from '../components/settings-page'
import type { ChannelHealthSettings } from '../types'
import {
  CHANNEL_HEALTH_DEFAULT_SECTION,
  getChannelHealthSectionContent,
  getChannelHealthSectionMeta,
} from './section-registry.tsx'

const defaultChannelHealthSettings: ChannelHealthSettings = {
  'channel_health_setting.enabled': true,
  'channel_health_setting.ttft_timeout_seconds': 0,
  'channel_health_setting.window_size': 50,
  'channel_health_setting.cooldown_after': 5,
  'channel_health_setting.cooldown_duration_minutes': 2,
  'channel_health_setting.reference_ttft_ms': 2000,
  'channel_health_setting.warmup_threshold': 10,
  'channel_health_setting.min_multiplier_pct': 5,
}

export function ChannelHealthSettingsPage() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/channel-health/$section'
      defaultSettings={defaultChannelHealthSettings}
      defaultSection={CHANNEL_HEALTH_DEFAULT_SECTION}
      getSectionContent={getChannelHealthSectionContent}
      getSectionMeta={getChannelHealthSectionMeta}
    />
  )
}
