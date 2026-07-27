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

import { PAYMENT_TYPES } from '../constants'
import {
  dispatchSelectedPayment,
  isAlipayF2FPayment,
  isStripePayment,
  isWaffoPayment,
  isWaffoPancakePayment,
} from './payment'
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

describe('payment type classification', () => {
  test('keeps dedicated payment methods on their own flows', () => {
    assert.equal(isAlipayF2FPayment(PAYMENT_TYPES.ALIPAY_F2F), true)
    assert.equal(isAlipayF2FPayment(PAYMENT_TYPES.WAFFO), false)
    assert.equal(isWaffoPayment(PAYMENT_TYPES.WAFFO), true)
    assert.equal(isWaffoPayment(PAYMENT_TYPES.WAFFO_PANCAKE), false)
    assert.equal(isWaffoPancakePayment(PAYMENT_TYPES.WAFFO_PANCAKE), true)
    assert.equal(isWaffoPancakePayment(PAYMENT_TYPES.WAFFO), false)
    assert.equal(isStripePayment(PAYMENT_TYPES.STRIPE), true)
  })
})

describe('payment dispatch', () => {
  test('keeps the selected Waffo method index through confirmation', async () => {
    const calls: string[] = []
    const success = await dispatchSelectedPayment(
      { name: 'Waffo Card', type: PAYMENT_TYPES.WAFFO },
      120,
      3,
      {
        regular: async () => {
          calls.push('regular')
          return false
        },
        alipayF2F: async () => {
          calls.push('alipay')
          return false
        },
        waffo: async (amount, index) => {
          calls.push(`waffo:${amount}:${index}`)
          return true
        },
        waffoPancake: async () => {
          calls.push('pancake')
          return false
        },
      }
    )

    assert.equal(success, true)
    assert.deepEqual(calls, ['waffo:120:3'])
  })

  test('does not create a Waffo order without a selected method index', async () => {
    let called = false
    const success = await dispatchSelectedPayment(
      { name: 'Waffo Card', type: PAYMENT_TYPES.WAFFO },
      120,
      null,
      {
        regular: async () => false,
        alipayF2F: async () => false,
        waffo: async () => {
          called = true
          return true
        },
        waffoPancake: async () => false,
      }
    )

    assert.equal(success, false)
    assert.equal(called, false)
  })

  test('routes Alipay Face-to-Face through its QR processor', async () => {
    const calls: string[] = []
    const success = await dispatchSelectedPayment(
      { name: 'Alipay', type: PAYMENT_TYPES.ALIPAY_F2F },
      88,
      null,
      {
        regular: async () => false,
        alipayF2F: async (amount) => {
          calls.push(`alipay:${amount}`)
          return true
        },
        waffo: async () => false,
        waffoPancake: async () => false,
      }
    )

    assert.equal(success, true)
    assert.deepEqual(calls, ['alipay:88'])
  })
})
