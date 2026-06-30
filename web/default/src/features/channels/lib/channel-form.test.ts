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

import type { Channel } from '../types.ts'
import {
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
} from './channel-form.ts'

function makeChannel(overrides: Partial<Channel> = {}): Channel {
  return {
    id: 1,
    type: 8,
    key: '',
    status: 1,
    name: 'test-channel',
    created_time: 0,
    test_time: 0,
    response_time: 0,
    other: '',
    balance: 0,
    balance_updated_time: 0,
    models: 'gpt-test',
    group: 'default',
    used_quota: 0,
    other_info: '',
    remark: '',
    max_input_tokens: 0,
    settings: '{}',
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: 'random',
    },
    ...overrides,
  } as Channel
}

describe('channel multiplier monitor form settings', () => {
  test('keeps redacted monitor password blank in update settings', () => {
    const channel = makeChannel({
      name: 'monitored-channel',
      settings: JSON.stringify({
        upstream_key_multiplier: {
          enabled: true,
          format: 'new-api',
          base_url: 'https://upstream.example.com',
          username: 'alice',
          password: '',
        },
      }),
    })

    const defaults = transformChannelToFormDefaults(channel)
    const payload = transformFormDataToUpdatePayload(defaults, channel.id)
    const settings = JSON.parse(String(payload.settings))

    assert.equal(defaults.upstream_key_multiplier_enabled, true)
    assert.equal(defaults.upstream_key_multiplier_format, 'new-api')
    assert.equal(
      settings.upstream_key_multiplier.base_url,
      'https://upstream.example.com'
    )
    assert.equal(settings.upstream_key_multiplier.password, '')
  })
})

describe('channel image input support form settings', () => {
  test('defaults existing channels to image input support', () => {
    const defaults = transformChannelToFormDefaults(makeChannel())

    assert.equal(defaults.supports_image_input, true)
  })

  test('stores explicit false when image input support is disabled', () => {
    const defaults = transformChannelToFormDefaults(makeChannel())
    const payload = transformFormDataToCreatePayload({
      ...defaults,
      supports_image_input: false,
    })
    const settings = JSON.parse(String(payload.channel.settings))

    assert.equal(settings.supports_image_input, false)
  })

  test('omits image input support setting when enabled', () => {
    const defaults = transformChannelToFormDefaults(
      makeChannel({
        settings: JSON.stringify({ supports_image_input: false }),
      })
    )
    const payload = transformFormDataToUpdatePayload(
      {
        ...defaults,
        supports_image_input: true,
      },
      1
    )
    const settings = JSON.parse(String(payload.settings))

    assert.equal('supports_image_input' in settings, false)
  })
})
