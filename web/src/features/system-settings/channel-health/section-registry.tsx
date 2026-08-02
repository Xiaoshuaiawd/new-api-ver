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
import type { ChannelHealthSettings } from '../types'
import { createSectionRegistry } from '../utils/section-registry'
import { ChannelHealthSection } from './channel-health-section'

const CHANNEL_HEALTH_SECTIONS = [
  {
    id: 'health',
    titleKey: 'Channel Health',
    build: (settings: ChannelHealthSettings) => (
      <ChannelHealthSection
        defaultValues={{
          'channel_health_setting.enabled':
            settings['channel_health_setting.enabled'],
          'channel_health_setting.ttft_timeout_seconds':
            settings['channel_health_setting.ttft_timeout_seconds'],
          'channel_health_setting.window_size':
            settings['channel_health_setting.window_size'],
          'channel_health_setting.cooldown_after':
            settings['channel_health_setting.cooldown_after'],
          'channel_health_setting.cooldown_duration_minutes':
            settings['channel_health_setting.cooldown_duration_minutes'],
          'channel_health_setting.reference_ttft_ms':
            settings['channel_health_setting.reference_ttft_ms'],
          'channel_health_setting.warmup_threshold':
            settings['channel_health_setting.warmup_threshold'],
          'channel_health_setting.min_multiplier_pct':
            settings['channel_health_setting.min_multiplier_pct'],
        }}
      />
    ),
  },
] as const

export type ChannelHealthSectionId =
  (typeof CHANNEL_HEALTH_SECTIONS)[number]['id']

const channelHealthRegistry = createSectionRegistry<
  ChannelHealthSectionId,
  ChannelHealthSettings
>({
  sections: CHANNEL_HEALTH_SECTIONS,
  defaultSection: 'health',
  basePath: '/system-settings/channel-health',
  urlStyle: 'path',
})

export const CHANNEL_HEALTH_SECTION_IDS = channelHealthRegistry.sectionIds
export const CHANNEL_HEALTH_DEFAULT_SECTION =
  channelHealthRegistry.defaultSection
export const getChannelHealthSectionNavItems =
  channelHealthRegistry.getSectionNavItems
export const getChannelHealthSectionContent =
  channelHealthRegistry.getSectionContent
export const getChannelHealthSectionMeta = channelHealthRegistry.getSectionMeta
