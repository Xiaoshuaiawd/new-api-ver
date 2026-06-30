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
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  Clock3,
  Gauge,
  RefreshCw,
  Search,
  Timer,
  type LucideIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { GroupBadge } from '@/components/group-badge'
import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import { Skeleton } from '@/components/ui/skeleton'
import {
  formatLatency,
  formatThroughput,
  formatUptimePct,
  getSuccessRateTextClass,
} from '@/features/performance-metrics/lib/format'
import { getModelMonitor } from './api'
import type {
  ModelMonitorGroup,
  ModelMonitorStatus,
  ModelMonitorSummary,
} from './types'

const REFRESH_INTERVAL_MS = 15_000
const WINDOW_OPTIONS = [1, 6, 24] as const

function formatCount(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0'
  return new Intl.NumberFormat().format(value)
}

function formatLastSample(timestamp: number): string {
  if (!Number.isFinite(timestamp) || timestamp <= 0) return '—'
  return dayjs.unix(timestamp).fromNow()
}

function emptySummary(): ModelMonitorSummary {
  return {
    success_rate: 0,
    avg_ttft_ms: 0,
    avg_latency_ms: 0,
    avg_tps: 0,
  }
}

function statusLabel(status: ModelMonitorStatus, t: (key: string) => string) {
  switch (status) {
    case 'healthy':
      return t('Healthy')
    case 'degraded':
      return t('Degraded')
    case 'critical':
      return t('Critical')
    default:
      return t('Idle')
  }
}

function statusClassName(status: ModelMonitorStatus) {
  switch (status) {
    case 'healthy':
      return 'border-emerald-500/20 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
    case 'degraded':
      return 'border-amber-500/20 bg-amber-500/10 text-amber-600 dark:text-amber-400'
    case 'critical':
      return 'border-red-500/20 bg-red-500/10 text-red-600 dark:text-red-400'
    default:
      return 'border-border bg-muted text-muted-foreground'
  }
}

function statusDotClassName(status: ModelMonitorStatus) {
  switch (status) {
    case 'healthy':
      return 'bg-emerald-500'
    case 'degraded':
      return 'bg-amber-500'
    case 'critical':
      return 'bg-red-500'
    default:
      return 'bg-muted-foreground/35'
  }
}

function filterGroups(groups: ModelMonitorGroup[], search: string) {
  const keyword = search.trim().toLowerCase()
  if (!keyword) return groups

  return groups.filter((group) => {
    return (
      group.name.toLowerCase().includes(keyword) ||
      group.description.toLowerCase().includes(keyword)
    )
  })
}

function SummaryMetric(props: {
  icon: LucideIcon
  label: string
  value: string
  valueClassName?: string
}) {
  const Icon = props.icon
  return (
    <div className='flex min-w-[8.5rem] items-center gap-2 rounded-lg border px-3 py-2'>
      <Icon className='text-muted-foreground size-4 shrink-0' />
      <div className='min-w-0'>
        <div className='text-muted-foreground text-xs'>{props.label}</div>
        <div
          className={cn(
            'font-mono text-sm font-semibold tabular-nums',
            props.valueClassName
          )}
        >
          {props.value}
        </div>
      </div>
    </div>
  )
}

function RecentBars(props: {
  rates?: number[]
  status: ModelMonitorStatus
  label: string
}) {
  const rates = props.rates?.filter(Number.isFinite).slice(-60) ?? []
  const bars = [...Array(Math.max(0, 60 - rates.length)).fill(null), ...rates]
  return (
    <div
      className='flex h-6 min-w-[18rem] items-end gap-1'
      aria-label={props.label}
      role='img'
    >
      {bars.map((rate, index) => (
        <span
          key={`${index}-${rate ?? 'empty'}`}
          className={cn(
            'h-5 flex-1 rounded-sm',
            rate == null && 'bg-muted-foreground/15',
            rate != null && rate >= 90 && 'bg-emerald-500',
            rate != null && rate < 90 && rate >= 70 && 'bg-amber-500',
            rate != null && rate < 70 && 'bg-red-500',
            props.status === 'idle' && 'bg-muted-foreground/15'
          )}
        />
      ))}
    </div>
  )
}

function GroupMetric(props: {
  icon: LucideIcon
  label: string
  value: string
  valueClassName?: string
}) {
  const Icon = props.icon
  return (
    <div className='bg-muted/45 min-w-0 rounded-md px-3 py-2'>
      <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
        <Icon className='size-3.5 shrink-0' aria-hidden='true' />
        <span className='truncate'>{props.label}</span>
      </div>
      <div
        className={cn(
          'mt-1 truncate font-mono text-sm font-semibold tabular-nums',
          props.valueClassName
        )}
      >
        {props.value}
      </div>
    </div>
  )
}

function GroupCard(props: { group: ModelMonitorGroup }) {
  const { t } = useTranslation()
  const group = props.group

  return (
    <article className='bg-card rounded-lg border p-3 sm:p-4'>
      <div className='flex min-w-0 items-start justify-between gap-3'>
        <div className='min-w-0'>
          <div className='flex min-w-0 items-center gap-2'>
            <span
              className={cn(
                'size-2.5 shrink-0 rounded-full',
                statusDotClassName(group.status)
              )}
              aria-hidden='true'
            />
            <GroupBadge group={group.name} ratio={group.ratio} />
          </div>
          <div className='text-muted-foreground mt-1 truncate text-xs'>
            {group.description}
          </div>
        </div>
        <Badge
          variant='outline'
          className={cn('h-6 shrink-0', statusClassName(group.status))}
        >
          {statusLabel(group.status, t)}
        </Badge>
      </div>

      <div className='mt-3 grid gap-2 sm:grid-cols-2 xl:grid-cols-4'>
        <GroupMetric
          icon={Timer}
          label={t('TTFT')}
          value={formatLatency(group.avg_ttft_ms)}
        />
        <GroupMetric
          icon={Clock3}
          label={t('Latency')}
          value={formatLatency(group.avg_latency_ms)}
        />
        <GroupMetric
          icon={Activity}
          label={t('Success rate')}
          value={
            group.status !== 'idle' ? formatUptimePct(group.success_rate) : '—'
          }
          valueClassName={getSuccessRateTextClass(group.success_rate)}
        />
        <GroupMetric
          icon={Gauge}
          label={t('Throughput')}
          value={formatThroughput(group.avg_tps)}
        />
      </div>

      <div className='mt-4 space-y-2'>
        <div className='flex items-center justify-between gap-3 text-xs'>
          <span className='text-muted-foreground'>{t('Recent 60 checks')}</span>
          <span className='text-muted-foreground font-mono'>
            {t('Last sample')}: {formatLastSample(group.last_bucket_ts)}
          </span>
        </div>
        <RecentBars
          rates={group.recent_success_rates}
          status={group.status}
          label={t('Recent 60 checks')}
        />
      </div>
    </article>
  )
}

function ModelMonitorSkeleton() {
  return (
    <div className='grid gap-3 xl:grid-cols-2'>
      {Array.from({ length: 4 }).map((_, index) => (
        <div key={index} className='rounded-lg border p-4'>
          <Skeleton className='h-6 w-48' />
          <Skeleton className='mt-2 h-3 w-64 max-w-full' />
          <div className='mt-4 grid gap-2 sm:grid-cols-2 xl:grid-cols-4'>
            <Skeleton className='h-14' />
            <Skeleton className='h-14' />
            <Skeleton className='h-14' />
            <Skeleton className='h-14' />
          </div>
          <Skeleton className='mt-4 h-6 w-full' />
        </div>
      ))}
    </div>
  )
}

export function ModelMonitor() {
  const { t } = useTranslation()
  const [windowHours, setWindowHours] =
    useState<(typeof WINDOW_OPTIONS)[number]>(1)
  const [search, setSearch] = useState('')

  const query = useQuery({
    queryKey: ['model-monitor', windowHours],
    queryFn: () => getModelMonitor(windowHours),
    refetchInterval: REFRESH_INTERVAL_MS,
    staleTime: 10_000,
    retry: false,
  })

  const groups = query.data?.data.groups ?? []
  const filteredGroups = useMemo(
    () => filterGroups(groups, search),
    [groups, search]
  )
  const summary = query.data?.data.summary ?? emptySummary()
  const statusCounts = useMemo(
    () =>
      groups.reduce(
        (counts, group) => {
          counts[group.status] += 1
          return counts
        },
        { healthy: 0, degraded: 0, critical: 0, idle: 0 }
      ),
    [groups]
  )

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Group Monitor')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <ButtonGroup>
          {WINDOW_OPTIONS.map((hours) => (
            <Button
              key={hours}
              variant={windowHours === hours ? 'secondary' : 'outline'}
              size='sm'
              onClick={() => setWindowHours(hours)}
            >
              {hours === 1 ? t('1h') : hours === 6 ? t('6h') : t('24h')}
            </Button>
          ))}
        </ButtonGroup>
        <Button
          variant='outline'
          size='sm'
          onClick={() => void query.refetch()}
          disabled={query.isFetching}
        >
          <RefreshCw
            className={cn('size-3.5', query.isFetching && 'animate-spin')}
          />
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-3'>
          <div className='flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between'>
            <div className='grid gap-2 sm:grid-cols-2 xl:grid-cols-5'>
              <SummaryMetric
                icon={Activity}
                label={t('Groups')}
                value={formatCount(groups.length)}
              />
              <SummaryMetric
                icon={Gauge}
                label={t('Healthy groups')}
                value={formatCount(statusCounts.healthy)}
                valueClassName='text-emerald-600 dark:text-emerald-400'
              />
              <SummaryMetric
                icon={Timer}
                label={t('Degraded groups')}
                value={formatCount(statusCounts.degraded)}
                valueClassName='text-amber-600 dark:text-amber-400'
              />
              <SummaryMetric
                icon={Clock3}
                label={t('Critical groups')}
                value={formatCount(statusCounts.critical)}
                valueClassName='text-red-600 dark:text-red-400'
              />
              <SummaryMetric
                icon={Activity}
                label={t('Idle groups')}
                value={formatCount(statusCounts.idle)}
                valueClassName='text-muted-foreground'
              />
            </div>
            <InputGroup className='w-full sm:w-[18rem]'>
              <InputGroupAddon>
                <Search className='size-4' aria-hidden='true' />
              </InputGroupAddon>
              <InputGroupInput
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder={t('Search groups...')}
              />
            </InputGroup>
          </div>

          <div className='grid gap-2 sm:grid-cols-2 lg:grid-cols-4'>
            <SummaryMetric
              icon={Timer}
              label={t('Success rate')}
              value={formatUptimePct(summary.success_rate)}
              valueClassName={getSuccessRateTextClass(summary.success_rate)}
            />
            <SummaryMetric
              icon={Clock3}
              label={t('TTFT')}
              value={formatLatency(summary.avg_ttft_ms)}
            />
            <SummaryMetric
              icon={Activity}
              label={t('Latency')}
              value={formatLatency(summary.avg_latency_ms)}
            />
            <SummaryMetric
              icon={Gauge}
              label={t('Throughput')}
              value={formatThroughput(summary.avg_tps)}
            />
          </div>

          {query.isLoading ? (
            <ModelMonitorSkeleton />
          ) : query.isError ? (
            <ErrorState
              title={t('Failed to load model monitor')}
              onRetry={() => void query.refetch()}
            />
          ) : filteredGroups.length === 0 ? (
            <EmptyState
              bordered
              icon={Activity}
              title={
                search ? t('No matching groups') : t('No group monitor data')
              }
            />
          ) : (
            <div className='grid gap-3 xl:grid-cols-2'>
              {filteredGroups.map((group) => (
                <GroupCard key={group.name} group={group} />
              ))}
            </div>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
