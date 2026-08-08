import { api } from '@/lib/api'

export type JuiceFixerRule = {
  model: string
  reasoning_effort: string
  value: number
}

export type JuiceFixerConfig = {
  enabled: boolean
  rules: JuiceFixerRule[]
}

export async function getJuiceFixerConfig(): Promise<JuiceFixerConfig> {
  const response = await api.get('/api/juice-fixer/config')
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data
}

export async function updateJuiceFixerConfig(
  config: JuiceFixerConfig
): Promise<JuiceFixerConfig> {
  const response = await api.put('/api/juice-fixer/config', config)
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data
}
