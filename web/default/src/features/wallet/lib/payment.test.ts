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
import { calculateTopUpBonusPreview } from './topup-bonus.ts'

describe('topup bonus preview', () => {
  test('calculates estimated credit when amount reaches threshold', () => {
    const preview = calculateTopUpBonusPreview(100, 100, {
      enabled: true,
      visible: true,
      min_amount: 100,
      bonus_percent: 10,
    })

    assert.equal(preview?.eligible, true)
    assert.equal(preview?.bonusAmount, 10)
    assert.equal(preview?.bonusCreditAmount, 10)
    assert.equal(preview?.totalAmount, 110)
  })

  test('shows remaining amount below threshold', () => {
    const preview = calculateTopUpBonusPreview(80, 100, {
      enabled: true,
      visible: true,
      min_amount: 100,
      bonus_percent: 10,
    })

    assert.equal(preview?.eligible, false)
    assert.equal(preview?.remainingAmount, 20)
    assert.equal(preview?.bonusAmount, 0)
  })

  test('caps single bonus amount', () => {
    const preview = calculateTopUpBonusPreview(1000, 1000, {
      enabled: true,
      visible: true,
      min_amount: 100,
      bonus_percent: 20,
      single_bonus_max_amount: 50,
    })

    assert.equal(preview?.bonusAmount, 50)
    assert.equal(preview?.totalAmount, 1050)
  })

  test('uses paid amount for threshold and scales credited bonus from base credit', () => {
    const preview = calculateTopUpBonusPreview(100, 1000, {
      enabled: true,
      visible: true,
      min_amount: 100,
      bonus_percent: 10,
    })

    assert.equal(preview?.bonusAmount, 10)
    assert.equal(preview?.bonusCreditAmount, 100)
    assert.equal(preview?.totalAmount, 1100)
  })
})
