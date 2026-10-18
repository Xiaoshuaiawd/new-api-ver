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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import { expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { DataDistribution } from '..'

vi.mock('@visactor/react-vchart', () => ({
  VChart: () => <div role='img' aria-label='Distribution chart' />,
}))

it('loads chat log distribution and displays summary, model, and daily data', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        overview: {
          total_count: 7,
          success_count: 6,
          fail_count: 1,
          tools_count: 2,
          tools_percent: 28.57,
          prompt_tokens_sum: 120,
          completion_tokens_sum: 30,
          total_tokens_sum: 150,
        },
        round_distribution: [],
        prompt_tokens_distribution: [],
        total_tokens_distribution: [],
        model_distribution: [
          { model_name: 'gpt-test', count: 7, percent: 100 },
        ],
        daily_stats: [
          {
            date: '2026-10-18',
            total_count: 7,
            success_count: 6,
            fail_count: 1,
            tools_count: 2,
            prompt_tokens_sum: 120,
            completion_tokens_sum: 30,
            total_tokens_sum: 150,
            avg_latency_ms: 45,
            top_model_name: 'gpt-test',
            top_model_count: 7,
          },
        ],
        db_compare: {
          count_diff: 0,
          count_diff_percent: 0,
          prompt_tokens_diff: 0,
          total_tokens_diff: 0,
        },
        scan_tables: [
          {
            table: 'chat_log_success_20261018',
            date: '2026-10-18',
            status: 'ok',
          },
        ],
      },
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <DataDistribution />
    </QueryClientProvider>
  )

  await waitFor(() =>
    expect(get).toHaveBeenCalledWith(
      '/api/admin/data-distribution',
      expect.objectContaining({
        params: expect.objectContaining({ status: 'all' }),
      })
    )
  )
  expect(await screen.findByText('Total stored records')).toBeInTheDocument()
  expect(screen.getAllByText('gpt-test').length).toBeGreaterThan(0)
  expect(screen.getByText('chat_log_success_20261018')).toBeInTheDocument()
  get.mockRestore()
})
