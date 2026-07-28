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

import { aggregateChannelsByTag, type TagRow } from '../lib'
import { channelSchema, type Channel } from '../types'

describe('channel runtime metrics', () => {
  test('keeps concurrency and RPM from the channel API response', () => {
    const runtimeSchema = channelSchema.pick({ runtime_metrics: true })

    const channel = runtimeSchema.parse({
      runtime_metrics: { concurrency: 3, rpm: 120 },
    })

    assert.deepEqual(channel.runtime_metrics, { concurrency: 3, rpm: 120 })
  })

  test('sums concurrency and RPM for tag aggregate rows', () => {
    const channels = [
      {
        id: 1,
        tag: 'shared',
        used_quota: 0,
        response_time: 0,
        priority: 0,
        weight: 0,
        group: 'default',
        status: 1,
        runtime_metrics: { concurrency: 2, rpm: 30 },
      },
      {
        id: 2,
        tag: 'shared',
        used_quota: 0,
        response_time: 0,
        priority: 0,
        weight: 0,
        group: 'default',
        status: 1,
        runtime_metrics: { concurrency: 4, rpm: 70 },
      },
    ] as Channel[]

    const [row] = aggregateChannelsByTag(channels) as TagRow[]

    assert.deepEqual(row.runtime_metrics, { concurrency: 6, rpm: 100 })
  })
})
