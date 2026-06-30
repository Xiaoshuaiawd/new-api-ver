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
export type ModelMonitorStatus = 'healthy' | 'degraded' | 'critical' | 'idle'

export type ModelMonitorSummary = {
  success_rate: number
  avg_ttft_ms: number
  avg_latency_ms: number
  avg_tps: number
}

export type ModelMonitorGroup = ModelMonitorSummary & {
  name: string
  description: string
  ratio: number
  recent_success_rates?: number[]
  last_bucket_ts: number
  status: ModelMonitorStatus
}

export type ModelMonitorData = {
  updated_at: number
  window_hours: number
  summary: ModelMonitorSummary
  groups: ModelMonitorGroup[]
}

export type ModelMonitorResponse = {
  success: boolean
  message?: string
  data: ModelMonitorData
}
