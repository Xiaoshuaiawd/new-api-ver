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

export type ReferralRewardStatus =
  | 'pending'
  | 'settled'
  | 'cancelled'
  | 'reversed'
  | 'partial_reversed'

export type ReferralRewardRiskStatus =
  | 'normal'
  | 'review'
  | 'blocked'
  | 'approved'
  | 'rejected'

export type ReferralRewardRole = 'invitee' | 'inviter'

export type ReferralReward = {
  id: number
  activity_id: string
  activity_name: string
  reward_role: ReferralRewardRole
  inviter_id: number
  invitee_id: number
  topup_id: number
  trade_no: string
  payment_provider: string
  payment_account_hash?: string
  paid_money: number
  base_quota: number
  reward_percent: number
  reward_quota: number
  settled_quota: number
  reversed_quota: number
  owed_quota?: number
  refund_amount: number
  status: ReferralRewardStatus
  risk_status: ReferralRewardRiskStatus
  risk_reason: string
  risk_snapshot?: string
  settle_at: number
  settled_at: number
  cancelled_at: number
  reversed_at: number
  created_at: number
  updated_at: number
}

export type ReferralStatsSummary = {
  invite_registered_count: number
  first_topup_count: number
  qualified_first_topup_count: number
  qualified_first_topup_net_money: number
  invitee_settled_reward_quota: number
  inviter_settled_reward_quota: number
  pending_reward_quota: number
  reversed_reward_quota: number
  refund_money: number
  refund_rate: number
  conversion_rate: number
  reward_cost_rate: number
  roi: number
  blocked_reward_count: number
}

export type ReferralFunnelItem = {
  stage: string
  count: number
  rate: number
  prior_rate: number
}

export type ReferralTrendItem = {
  bucket: string
  net_money: number
  reward_cost_quota: number
  qualified_first_topup_count: number
  refund_money: number
}

export type ReferralTopInviter = {
  inviter_id: number
  inviter_username: string
  invite_registered_count: number
  qualified_first_topup_count: number
  first_topup_net_money: number
  inviter_settled_reward_quota: number
  pending_reward_quota: number
  invitee_reward_quota: number
  refund_money: number
  refund_rate: number
  roi: number
  risk_status: ReferralRewardRiskStatus
}

export type ReferralPageResponse<T> = {
  success: boolean
  message: string
  data: {
    page: number
    page_size: number
    total: number
    items: T[]
  }
}

export type ApiResponse<T> = {
  success: boolean
  message: string
  data: T
}

export type ReferralStatsParams = {
  start_time?: number
  end_time?: number
  activity_id?: string
  inviter_id?: number
  invitee_id?: number
  inviter_keyword?: string
  invitee_keyword?: string
  payment_provider?: string
  status?: string
  risk_status?: string
  user_group?: string
  refund_only?: boolean
  bucket?: 'day' | 'week' | 'month'
  sort?: string
  limit?: number
  p?: number
  page_size?: number
}

export type ReferralRiskSnapshot = {
  flags?: string[]
  same_ip_24h_register_count?: number
  same_device_7d_bind_count?: number
  same_payment_account_invitee_count?: number
  inviter_24h_reward_quota?: number
  inviter_30d_refund_rate?: number
  register_to_topup_seconds?: number
  payment_account_hash?: string
  owed_quota?: number
}

export type ReferralSummary = {
  aff_code: string
  invite_link: string
  invite_count: number
  qualified_first_topup_count: number
  pending_reward_quota: number
  settled_reward_quota: number
  reversed_reward_quota: number
  aff_quota: number
  aff_history_quota: number
  inviter_id: number
}
