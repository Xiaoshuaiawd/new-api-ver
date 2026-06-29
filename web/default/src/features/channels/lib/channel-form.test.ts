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
  transformFormDataToUpdatePayload,
} from './channel-form.ts'

describe('channel multiplier monitor form settings', () => {
  test('keeps redacted monitor password blank in update settings', () => {
    const channel = {
      id: 1,
      type: 8,
      key: '',
      status: 1,
      name: 'monitored-channel',
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
      settings: JSON.stringify({
        upstream_key_multiplier: {
          enabled: true,
          format: 'new-api',
          base_url: 'https://upstream.example.com',
          username: 'alice',
          password: '',
        },
      }),
      channel_info: {
        is_multi_key: false,
        multi_key_size: 0,
        multi_key_polling_index: 0,
        multi_key_mode: 'random',
      },
    } as Channel

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
