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
import type { TopUpBonusConfig } from '../types.ts'

export type TopUpBonusPreview = {
  eligible: boolean
  enabled: boolean
  paidAmount: number
  baseCreditAmount: number
  bonusAmount: number
  bonusCreditAmount: number
  totalAmount: number
  remainingAmount: number
  minAmount: number
  bonusPercent: number
  activityName: string
}

function getActiveTopUpBonus(config?: TopUpBonusConfig): TopUpBonusConfig | null {
  if (!config?.enabled || config.visible === false) return null
  const nowSeconds = Math.floor(Date.now() / 1000)
  if (config.start_time && nowSeconds < config.start_time) return null
  if (config.end_time && nowSeconds > config.end_time) return null
  const minAmount = Number(config.min_amount) || 0
  const bonusPercent = Number(config.bonus_percent) || 0
  if (minAmount <= 0 || bonusPercent <= 0) return null
  return config
}

export function calculateTopUpBonusPreview(
  paidAmount: number,
  baseCreditAmount: number,
  config?: TopUpBonusConfig
): TopUpBonusPreview | null {
  const active = getActiveTopUpBonus(config)
  if (!active) return null

  const minAmount = Number(active.min_amount) || 0
  const bonusPercent = Number(active.bonus_percent) || 0
  const safePaidAmount = Number.isFinite(paidAmount) ? paidAmount : 0
  const safeBaseCreditAmount = Number.isFinite(baseCreditAmount)
    ? baseCreditAmount
    : 0
  const roundedPaidAmount = Math.floor(safePaidAmount)
  const remainingAmount = Math.max(minAmount - roundedPaidAmount, 0)
  const eligible = roundedPaidAmount >= minAmount
  let bonusAmount = 0
  if (eligible) {
    bonusAmount = Math.floor((roundedPaidAmount * bonusPercent) / 100)
    const singleCap = Number(active.single_bonus_max_amount) || 0
    if (singleCap > 0) {
      bonusAmount = Math.min(bonusAmount, singleCap)
    }
  }
  const bonusCreditAmount =
    eligible && roundedPaidAmount > 0
      ? Math.floor((bonusAmount * safeBaseCreditAmount) / roundedPaidAmount)
      : 0

  return {
    eligible,
    enabled: true,
    paidAmount: roundedPaidAmount,
    baseCreditAmount: safeBaseCreditAmount,
    bonusAmount,
    bonusCreditAmount,
    totalAmount: safeBaseCreditAmount + bonusCreditAmount,
    remainingAmount,
    minAmount,
    bonusPercent,
    activityName: active.activity_name || '',
  }
}
