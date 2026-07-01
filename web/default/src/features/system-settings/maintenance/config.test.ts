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
  parseHeaderNavModules,
  parseSidebarModulesAdmin,
} from './config.ts'

describe('maintenance navigation config', () => {
  test('defaults group monitor to an enabled header module', () => {
    const config = parseHeaderNavModules('')

    assert.equal(config.groupMonitor, true)
  })

  test('merges group monitor into legacy header navigation config', () => {
    const config = parseHeaderNavModules(
      JSON.stringify({
        home: false,
        pricing: { enabled: false, requireAuth: false },
      })
    )

    assert.equal(config.home, false)
    assert.equal(config.groupMonitor, true)
  })

  test('parses explicit group monitor header navigation settings', () => {
    const config = parseHeaderNavModules(
      JSON.stringify({
        groupMonitor: false,
      })
    )

    assert.equal(config.groupMonitor, false)
  })

  test('defaults group monitor sidebar module to enabled', () => {
    const config = parseSidebarModulesAdmin('')

    assert.equal(config.console.modelMonitor, true)
  })
})
