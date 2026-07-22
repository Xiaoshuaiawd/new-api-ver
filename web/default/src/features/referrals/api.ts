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
import { api } from '@/lib/api'

import type {
  ApiResponse,
  ReferralFunnelItem,
  ReferralPageResponse,
  ReferralReward,
  ReferralSummary,
  ReferralStatsParams,
  ReferralStatsSummary,
  ReferralTopInviter,
  ReferralTrendItem,
} from './types'

function buildQuery(params: ReferralStatsParams = {}) {
  const query = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return
    query.set(key, String(value))
  })
  return query.toString()
}

export async function getReferralStatsSummary(
  params: ReferralStatsParams = {}
): Promise<ApiResponse<ReferralStatsSummary>> {
  const res = await api.get(
    `/api/admin/referral/stats/summary?${buildQuery(params)}`
  )
  return res.data
}

export async function getReferralStatsFunnel(
  params: ReferralStatsParams = {}
): Promise<ApiResponse<ReferralFunnelItem[]>> {
  const res = await api.get(
    `/api/admin/referral/stats/funnel?${buildQuery(params)}`
  )
  return res.data
}

export async function getReferralStatsTrend(
  params: ReferralStatsParams = {}
): Promise<ApiResponse<ReferralTrendItem[]>> {
  const res = await api.get(
    `/api/admin/referral/stats/trend?${buildQuery(params)}`
  )
  return res.data
}

export async function getReferralTopInviters(
  params: ReferralStatsParams = {}
): Promise<ApiResponse<ReferralTopInviter[]>> {
  const res = await api.get(
    `/api/admin/referral/stats/top-inviters?${buildQuery(params)}`
  )
  return res.data
}

export async function getReferralRewards(
  params: ReferralStatsParams = {}
): Promise<ReferralPageResponse<ReferralReward>> {
  const res = await api.get(`/api/admin/referral/rewards?${buildQuery(params)}`)
  return res.data
}

export async function getReferralRiskRewards(
  params: ReferralStatsParams = {}
): Promise<ReferralPageResponse<ReferralReward>> {
  const res = await api.get(
    `/api/admin/referral/risk-rewards?${buildQuery(params)}`
  )
  return res.data
}

export async function approveReferralReward(id: number, reason = '') {
  const res = await api.post(`/api/admin/referral/rewards/${id}/approve`, {
    reason,
  })
  return res.data
}

export async function blockReferralReward(id: number, reason = '') {
  const res = await api.post(`/api/admin/referral/rewards/${id}/block`, {
    reason,
  })
  return res.data
}

export async function cancelReferralReward(id: number, reason = '') {
  const res = await api.post(`/api/admin/referral/rewards/${id}/cancel`, {
    reason,
  })
  return res.data
}

export async function reverseReferralReward(id: number, reason = '') {
  const res = await api.post(`/api/admin/referral/rewards/${id}/reverse`, {
    reason,
  })
  return res.data
}

export async function blockInviterPendingRewards(id: number, reason = '') {
  const res = await api.post(
    `/api/admin/referral/inviters/${id}/block-pending`,
    { reason }
  )
  return res.data
}

export async function getSelfReferralSummary(): Promise<
  ApiResponse<ReferralSummary>
> {
  const res = await api.get('/api/user/referral/summary')
  return res.data
}

export async function getSelfReferralRewards(
  params: ReferralStatsParams = {}
): Promise<ReferralPageResponse<ReferralReward>> {
  const res = await api.get(`/api/user/referral/rewards?${buildQuery(params)}`)
  return res.data
}
