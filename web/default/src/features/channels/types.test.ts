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
import { channelSchema } from './types.ts'

describe('channel schema runtime health', () => {
  test('parses runtime health snapshots from channel list responses', () => {
    const channel = channelSchema.parse({
      id: 1,
      type: 1,
      key: '',
      status: 1,
      name: 'runtime-health-channel',
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
      runtime_health: {
        channel_id: 1,
        state: 'warming',
        reason: 'warming',
        inflight: 2,
        window_samples: 10,
        window_failures: 4,
        error_rate: 0.4,
        warmup_percent: 40,
      },
    })

    assert.equal(channel.runtime_health?.state, 'warming')
    assert.equal(channel.runtime_health?.warmup_percent, 40)
    assert.equal(channel.runtime_health?.inflight, 2)
  })
})
