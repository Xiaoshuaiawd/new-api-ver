import { useQuery } from '@tanstack/react-query'
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
import { VChart } from '@visactor/react-vchart'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import { toIntlLocale } from '@/i18n/languages'
import { api } from '@/lib/api'
import { formatNumber } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'
import { VCHART_OPTION } from '@/lib/vchart'

type Bucket = { key: string; label: string; count: number; percent: number }
type ModelItem = { model_name: string; count: number; percent: number }
type DailyStat = {
  date: string
  total_count: number
  success_count: number
  fail_count: number
  tools_count: number
  prompt_tokens_sum: number
  completion_tokens_sum: number
  total_tokens_sum: number
  avg_latency_ms: number
  top_model_name: string
  top_model_count: number
}
type Distribution = {
  overview: {
    total_count: number
    success_count: number
    fail_count: number
    tools_count: number
    tools_percent: number
    prompt_tokens_sum: number
    completion_tokens_sum: number
    total_tokens_sum: number
  }
  round_distribution: Bucket[]
  prompt_tokens_distribution: Bucket[]
  total_tokens_distribution: Bucket[]
  model_distribution: ModelItem[]
  daily_stats: DailyStat[]
  db_compare: {
    count_diff: number
    count_diff_percent: number
    prompt_tokens_diff: number
    total_tokens_diff: number
  }
  scan_tables: { table: string; date: string; status: string }[]
}
type Status = 'all' | 'success' | 'fail'
type Filters = { start: Date; end: Date; status: Status }

function todayFilters(): Filters {
  const end = new Date()
  const start = new Date(end)
  start.setHours(0, 0, 0, 0)
  return { start, end, status: 'all' }
}

async function getDistribution(filters: Filters): Promise<Distribution> {
  const response = await api.get<{
    success: boolean
    data: Distribution
    message?: string
  }>('/api/admin/data-distribution', {
    params: {
      start_timestamp: Math.floor(filters.start.getTime() / 1000),
      end_timestamp: Math.floor(filters.end.getTime() / 1000),
      status: filters.status,
    },
  })
  return requireServerSuccess(response.data).data
}

function DistributionChart({
  title,
  values,
}: {
  title: string
  values: { label: string; count: number }[]
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent className='h-72'>
        <VChart
          spec={{
            type: 'pie',
            data: [
              {
                id: 'pie',
                values: values.map((item) => ({
                  type: item.label,
                  value: item.count,
                })),
              },
            ],
            categoryField: 'type',
            valueField: 'value',
            legends: { visible: true, orient: 'bottom' },
            tooltip: { visible: true },
            label: { visible: true },
            background: 'transparent',
          }}
          option={VCHART_OPTION}
        />
      </CardContent>
    </Card>
  )
}

export function DataDistribution() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [draft, setDraft] = useState<Filters>(todayFilters)
  const [filters, setFilters] = useState<Filters>(todayFilters)
  const distribution = useQuery({
    queryKey: [
      'data-distribution',
      filters.start.getTime(),
      filters.end.getTime(),
      filters.status,
    ],
    queryFn: () => getDistribution(filters),
  })
  const data = distribution.data
  const number = (value: number | undefined) => formatNumber(value ?? 0, locale)
  const percent = (value: number | undefined) => `${(value ?? 0).toFixed(2)}%`
  const summary = data
    ? [
        [t('Total stored records'), number(data.overview.total_count)],
        [t('Successful records'), number(data.overview.success_count)],
        [t('Failed records'), number(data.overview.fail_count)],
        [
          t('Tool calls'),
          `${number(data.overview.tools_count)} (${percent(data.overview.tools_percent)})`,
        ],
        [t('Prompt tokens'), number(data.overview.prompt_tokens_sum)],
        [t('Total tokens'), number(data.overview.total_tokens_sum)],
      ]
    : []

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Data Distribution')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <Card>
            <CardContent className='flex flex-wrap items-end gap-3'>
              <div className='min-w-60 flex-1 space-y-1'>
                <div className='text-muted-foreground text-sm'>
                  {t('Date Range')}
                </div>
                <CompactDateTimeRangePicker
                  start={draft.start}
                  end={draft.end}
                  onChange={({ start, end }) => {
                    if (start && end) setDraft({ ...draft, start, end })
                  }}
                />
              </div>
              <div className='min-w-36 space-y-1'>
                <div className='text-muted-foreground text-sm'>
                  {t('Status')}
                </div>
                <Select
                  value={draft.status}
                  onValueChange={(status) =>
                    setDraft({ ...draft, status: status as Status })
                  }
                >
                  <SelectTrigger aria-label={t('Status')}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='all'>{t('All')}</SelectItem>
                    <SelectItem value='success'>{t('Success')}</SelectItem>
                    <SelectItem value='fail'>{t('Failed records')}</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <Button
                disabled={distribution.isFetching || draft.end < draft.start}
                onClick={() => setFilters({ ...draft })}
              >
                {t('Search')}
              </Button>
              <Button
                variant='outline'
                onClick={() => {
                  const initial = todayFilters()
                  setDraft(initial)
                  setFilters(initial)
                }}
              >
                {t('Reset')}
              </Button>
            </CardContent>
          </Card>

          {distribution.isLoading && <p role='status'>{t('Loading...')}</p>}
          {distribution.isError && (
            <p role='alert' className='text-destructive'>
              {t('Failed to load data distribution.')}
            </p>
          )}
          {data && (
            <>
              <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-3'>
                {summary.map(([title, value]) => (
                  <Card key={title} size='sm'>
                    <CardContent>
                      <div className='text-muted-foreground text-sm'>
                        {title}
                      </div>
                      <div className='mt-1 text-xl font-semibold tabular-nums'>
                        {value}
                      </div>
                    </CardContent>
                  </Card>
                ))}
              </div>
              <Card>
                <CardHeader>
                  <CardTitle>{t('Log database comparison')}</CardTitle>
                </CardHeader>
                <CardContent className='grid gap-3 sm:grid-cols-3'>
                  <div>
                    <div className='text-muted-foreground'>
                      {t('Request count difference')}
                    </div>
                    <div className='font-semibold tabular-nums'>
                      {number(data.db_compare.count_diff)} (
                      {percent(data.db_compare.count_diff_percent)})
                    </div>
                  </div>
                  <div>
                    <div className='text-muted-foreground'>
                      {t('Prompt token difference')}
                    </div>
                    <div className='font-semibold tabular-nums'>
                      {number(data.db_compare.prompt_tokens_diff)}
                    </div>
                  </div>
                  <div>
                    <div className='text-muted-foreground'>
                      {t('Total token difference')}
                    </div>
                    <div className='font-semibold tabular-nums'>
                      {number(data.db_compare.total_tokens_diff)}
                    </div>
                  </div>
                </CardContent>
              </Card>
              <div className='grid gap-3 lg:grid-cols-2'>
                <DistributionChart
                  title={t('Round distribution')}
                  values={data.round_distribution.map((item) => ({
                    label: item.label,
                    count: item.count,
                  }))}
                />
                <DistributionChart
                  title={t('Model call distribution')}
                  values={data.model_distribution.map((item) => ({
                    label: item.model_name,
                    count: item.count,
                  }))}
                />
                <DistributionChart
                  title={t('Prompt token distribution')}
                  values={data.prompt_tokens_distribution.map((item) => ({
                    label: item.label,
                    count: item.count,
                  }))}
                />
                <DistributionChart
                  title={t('Total token distribution')}
                  values={data.total_tokens_distribution.map((item) => ({
                    label: item.label,
                    count: item.count,
                  }))}
                />
              </div>
              <Card>
                <CardHeader>
                  <CardTitle>{t('Daily statistics')}</CardTitle>
                </CardHeader>
                <CardContent>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        {[
                          t('Day'),
                          t('Total'),
                          t('Success'),
                          t('Failed records'),
                          t('Tool calls'),
                          t('Prompt tokens'),
                          t('Completion tokens'),
                          t('Total tokens'),
                          t('Average latency (ms)'),
                          t('Top model'),
                        ].map((name) => (
                          <TableHead key={name}>{name}</TableHead>
                        ))}
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {data.daily_stats.map((item) => (
                        <TableRow key={item.date}>
                          <TableCell>{item.date}</TableCell>
                          <TableCell>{number(item.total_count)}</TableCell>
                          <TableCell>{number(item.success_count)}</TableCell>
                          <TableCell>{number(item.fail_count)}</TableCell>
                          <TableCell>{number(item.tools_count)}</TableCell>
                          <TableCell>
                            {number(item.prompt_tokens_sum)}
                          </TableCell>
                          <TableCell>
                            {number(item.completion_tokens_sum)}
                          </TableCell>
                          <TableCell>{number(item.total_tokens_sum)}</TableCell>
                          <TableCell>{number(item.avg_latency_ms)}</TableCell>
                          <TableCell>
                            {item.top_model_name
                              ? `${item.top_model_name} (${number(item.top_model_count)})`
                              : '-'}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </CardContent>
              </Card>
              <Card>
                <CardHeader>
                  <CardTitle>{t('Model distribution details')}</CardTitle>
                </CardHeader>
                <CardContent>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('Model')}</TableHead>
                        <TableHead>{t('Count')}</TableHead>
                        <TableHead>{t('Share')}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {data.model_distribution.map((item) => (
                        <TableRow key={item.model_name}>
                          <TableCell>{item.model_name}</TableCell>
                          <TableCell>{number(item.count)}</TableCell>
                          <TableCell>{percent(item.percent)}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </CardContent>
              </Card>
              <Card>
                <CardHeader>
                  <CardTitle>{t('Partition scan status')}</CardTitle>
                </CardHeader>
                <CardContent className='flex flex-wrap gap-2'>
                  {data.scan_tables.map((item) => (
                    <Badge
                      key={`${item.table}-${item.date}`}
                      variant={item.status === 'ok' ? 'secondary' : 'outline'}
                    >
                      {item.table}
                    </Badge>
                  ))}
                </CardContent>
              </Card>
            </>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
