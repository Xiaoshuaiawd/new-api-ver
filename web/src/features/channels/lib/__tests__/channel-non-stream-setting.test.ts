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
import { describe, expect, test } from 'vitest'

import { channelSchema, type Channel } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  buildSettingJSON,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
} from '../channel-form'

function channelWithSetting(setting: string): Channel {
  return channelSchema.parse({
    id: 1,
    type: 1,
    key: '',
    status: 1,
    name: 'test',
    created_time: 0,
    test_time: 0,
    response_time: 0,
    balance: 0,
    balance_updated_time: 0,
    models: 'gpt-5',
    group: 'default',
    used_quota: 0,
    setting,
    settings: '{}',
  })
}

describe('channel disable non-stream setting', () => {
  test('defaults to off for new and legacy channels', () => {
    expect(CHANNEL_FORM_DEFAULT_VALUES.disable_non_stream).toBe(false)
    expect(
      transformChannelToFormDefaults(channelWithSetting('{}'))
        .disable_non_stream
    ).toBe(false)
  })

  test('hydrates enabled setting when editing a channel', () => {
    const values = transformChannelToFormDefaults(
      channelWithSetting('{"disable_non_stream":true}')
    )
    expect(values.disable_non_stream).toBe(true)
  })

  test('serializes enabled setting for create and update payloads', () => {
    const values = {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      disable_non_stream: true,
    }

    expect(JSON.parse(buildSettingJSON(values)).disable_non_stream).toBe(true)
    expect(
      JSON.parse(
        String(transformFormDataToCreatePayload(values).channel.setting)
      ).disable_non_stream
    ).toBe(true)
    expect(
      JSON.parse(String(transformFormDataToUpdatePayload(values, 1).setting))
        .disable_non_stream
    ).toBe(true)
  })
})
