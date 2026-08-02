/*
Copyright (C) 2023-2026 QuantumNous
This program is free software: GNU AGPL v3+.
For commercial licensing: support@quantumnous.com
*/
import { api } from '@/lib/api'

export type PromptGuardEndpoint = {
  id: string
  name: string
  base_url: string
  model: string
  format: string
  has_token: boolean
  timeout_ms: number
  input_limit: number
  enabled: boolean
}

export type PromptGuardConfig = {
  enabled: boolean
  blocking_enabled: boolean
  latest_turn_only: boolean
  store_pass_events: boolean
  scanners: string[]
  all_groups: boolean
  group_names: string[]
  endpoints: PromptGuardEndpoint[]
  config_version: number
}

export type UpdatePromptGuardEndpoint = {
  id: string
  name: string
  base_url: string
  model: string
  format: string
  token?: string
  clear_token?: boolean
  timeout_ms: number
  input_limit: number
  enabled: boolean
}

export type UpdatePromptGuardConfig = {
  expected_version: number
  enabled: boolean
  blocking_enabled: boolean
  latest_turn_only: boolean
  store_pass_events: boolean
  scanners: string[]
  all_groups: boolean
  group_names: string[]
  endpoints: UpdatePromptGuardEndpoint[]
}

export async function getPromptGuardConfig(): Promise<PromptGuardConfig> {
  const res = await api.get('/api/prompt-guard/config')
  if (!res.data.success) throw new Error(res.data.message)
  return res.data.data
}

export async function updatePromptGuardConfig(
  req: UpdatePromptGuardConfig
): Promise<PromptGuardConfig> {
  const res = await api.put('/api/prompt-guard/config', req)
  if (!res.data.success) throw new Error(res.data.message)
  return res.data.data
}

export async function probePromptGuardEndpoint(params: {
  base_url: string
  model: string
  format?: string
  token?: string
  timeout_ms?: number
}): Promise<{ decision: string; latency_ms: number }> {
  const res = await api.post('/api/prompt-guard/probe', params)
  if (!res.data.success) throw new Error(res.data.message)
  return res.data.data
}
