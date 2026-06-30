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
  ChevronsDownUp,
  ChevronsUpDown,
  ChevronDown,
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
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
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
  ModelMonitorModel,
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

function weightedSummary(groups: ModelMonitorGroup[]): ModelMonitorSummary {
  const summary: ModelMonitorSummary = {
    success_rate: 0,
    avg_ttft_ms: 0,
    avg_latency_ms: 0,
    avg_tps: 0,
  }
  let activeModels = 0

  for (const group of groups) {
    for (const model of group.models) {
      if (model.status === 'idle') continue
      activeModels += 1
      summary.success_rate += model.success_rate
      summary.avg_ttft_ms += model.avg_ttft_ms
      summary.avg_latency_ms += model.avg_latency_ms
      summary.avg_tps += model.avg_tps
    }
  }

  if (activeModels <= 0) return summary
  summary.success_rate = summary.success_rate / activeModels
  summary.avg_ttft_ms = Math.round(summary.avg_ttft_ms / activeModels)
  summary.avg_latency_ms = Math.round(summary.avg_latency_ms / activeModels)
  summary.avg_tps = summary.avg_tps / activeModels
  return summary
}

function filterGroups(groups: ModelMonitorGroup[], search: string) {
  const keyword = search.trim().toLowerCase()
  if (!keyword) return groups

  return groups
    .map((group) => {
      const groupMatched =
        group.name.toLowerCase().includes(keyword) ||
        group.description.toLowerCase().includes(keyword)
      const models = groupMatched
        ? group.models
        : group.models.filter((model) => {
            return (
              model.model_name.toLowerCase().includes(keyword) ||
              (model.vendor_name ?? '').toLowerCase().includes(keyword)
            )
          })
      return { ...group, models }
    })
    .filter((group) => group.models.length > 0)
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

function ModelMetric(props: {
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

function ModelCard(props: { model: ModelMonitorModel }) {
  const { t } = useTranslation()
  const model = props.model

  return (
    <article className='bg-card rounded-lg border p-3 sm:p-4'>
      <div className='flex min-w-0 items-start justify-between gap-3'>
        <div className='min-w-0'>
          <div className='flex min-w-0 items-center gap-2'>
            <span
              className={cn(
                'size-2.5 shrink-0 rounded-full',
                statusDotClassName(model.status)
              )}
              aria-hidden='true'
            />
            <h3 className='min-w-0 truncate font-mono text-sm font-semibold'>
              {model.model_name}
            </h3>
          </div>
          {model.vendor_name != null && model.vendor_name !== '' && (
            <div className='text-muted-foreground mt-1 truncate text-xs'>
              {model.vendor_name}
            </div>
          )}
        </div>
        <Badge
          variant='outline'
          className={cn('h-6 shrink-0', statusClassName(model.status))}
        >
          {statusLabel(model.status, t)}
        </Badge>
      </div>

      <div className='mt-3 grid gap-2 sm:grid-cols-2 xl:grid-cols-4'>
        <ModelMetric
          icon={Timer}
          label={t('TTFT')}
          value={formatLatency(model.avg_ttft_ms)}
        />
        <ModelMetric
          icon={Clock3}
          label={t('Latency')}
          value={formatLatency(model.avg_latency_ms)}
        />
        <ModelMetric
          icon={Activity}
          label={t('Success rate')}
          value={
            model.status !== 'idle' ? formatUptimePct(model.success_rate) : '—'
          }
          valueClassName={getSuccessRateTextClass(model.success_rate)}
        />
        <ModelMetric
          icon={Gauge}
          label={t('Throughput')}
          value={formatThroughput(model.avg_tps)}
        />
      </div>

      <div className='mt-4 space-y-2'>
        <div className='flex items-center justify-between gap-3 text-xs'>
          <span className='text-muted-foreground'>{t('Recent 60 checks')}</span>
          <span className='text-muted-foreground font-mono'>
            {t('Last sample')}: {formatLastSample(model.last_bucket_ts)}
          </span>
        </div>
        <RecentBars
          rates={model.recent_success_rates}
          status={model.status}
          label={t('Recent 60 checks')}
        />
      </div>
    </article>
  )
}

function ModelRows(props: { models: ModelMonitorModel[] }) {
  return (
    <div className='grid gap-3 p-3 xl:grid-cols-2'>
      {props.models.map((model) => (
        <ModelCard key={model.model_name} model={model} />
      ))}
    </div>
  )
}

function GroupSection(props: {
  group: ModelMonitorGroup
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const summary = props.group.summary

  return (
    <Collapsible
      open={props.open}
      onOpenChange={props.onOpenChange}
      className='bg-card overflow-hidden rounded-lg border'
    >
      <CollapsibleTrigger className='hover:bg-muted/40 flex w-full cursor-pointer items-center gap-3 px-3 py-3 text-left transition-colors sm:px-4'>
        <ChevronDown
          className={cn(
            'text-muted-foreground size-4 shrink-0 transition-transform',
            !props.open && '-rotate-90'
          )}
          aria-hidden='true'
        />
        <div className='min-w-0 flex-1'>
          <div className='flex min-w-0 flex-wrap items-center gap-2'>
            <GroupBadge group={props.group.name} ratio={props.group.ratio} />
            <span className='text-muted-foreground truncate text-xs'>
              {props.group.description}
            </span>
          </div>
          <div className='text-muted-foreground mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs'>
            <span>
              {t('Models')}: {props.group.models.length}
            </span>
            <span>
              {t('Success rate')}: {formatUptimePct(summary.success_rate)}
            </span>
          </div>
        </div>
        <div className='hidden shrink-0 grid-cols-3 gap-2 lg:grid'>
          <CompactMetric
            label={t('TTFT')}
            value={formatLatency(summary.avg_ttft_ms)}
          />
          <CompactMetric
            label={t('Latency')}
            value={formatLatency(summary.avg_latency_ms)}
          />
          <CompactMetric
            label={t('TPS')}
            value={formatThroughput(summary.avg_tps)}
          />
        </div>
      </CollapsibleTrigger>
      <CollapsibleContent className='border-t'>
        {props.group.models.length > 0 ? (
          <ModelRows models={props.group.models} />
        ) : (
          <div className='text-muted-foreground px-4 py-8 text-center text-sm'>
            {t('No models in this group')}
          </div>
        )}
      </CollapsibleContent>
    </Collapsible>
  )
}

function CompactMetric(props: { label: string; value: string }) {
  return (
    <div className='min-w-[6rem] rounded-md border px-2 py-1.5 text-right'>
      <div className='text-muted-foreground text-[11px]'>{props.label}</div>
      <div className='font-mono text-xs font-semibold tabular-nums'>
        {props.value}
      </div>
    </div>
  )
}

function ModelMonitorSkeleton() {
  return (
    <div className='space-y-3'>
      {Array.from({ length: 3 }).map((_, index) => (
        <div key={index} className='rounded-lg border'>
          <div className='flex items-center gap-3 px-4 py-3'>
            <Skeleton className='size-4' />
            <div className='flex-1 space-y-2'>
              <Skeleton className='h-5 w-40' />
              <Skeleton className='h-3 w-72 max-w-full' />
            </div>
            <Skeleton className='hidden h-10 w-72 lg:block' />
          </div>
          <div className='border-t p-3'>
            <Skeleton className='h-36 w-full' />
          </div>
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
  const [closedGroups, setClosedGroups] = useState<Set<string>>(() => new Set())

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
  const summary = useMemo(() => weightedSummary(groups), [groups])
  const totalModels = useMemo(
    () => groups.reduce((sum, group) => sum + group.models.length, 0),
    [groups]
  )

  const setGroupOpen = (group: string, open: boolean) => {
    setClosedGroups((current) => {
      const next = new Set(current)
      if (open) next.delete(group)
      else next.add(group)
      return next
    })
  }

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
            <div className='grid gap-2 sm:grid-cols-2 lg:grid-cols-4'>
              <SummaryMetric
                icon={Activity}
                label={t('Groups')}
                value={formatCount(groups.length)}
              />
              <SummaryMetric
                icon={Gauge}
                label={t('Models')}
                value={formatCount(totalModels)}
              />
              <SummaryMetric
                icon={Timer}
                label={t('Success rate')}
                value={formatUptimePct(summary.success_rate)}
                valueClassName={getSuccessRateTextClass(summary.success_rate)}
              />
            </div>
            <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-end'>
              <InputGroup className='w-full sm:w-[18rem]'>
                <InputGroupAddon>
                  <Search className='size-4' aria-hidden='true' />
                </InputGroupAddon>
                <InputGroupInput
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder={t('Search models')}
                />
              </InputGroup>
              <ButtonGroup>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => setClosedGroups(new Set<string>())}
                >
                  <ChevronsUpDown className='size-3.5' />
                  {t('Expand all')}
                </Button>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() =>
                    setClosedGroups(
                      new Set(filteredGroups.map((group) => group.name))
                    )
                  }
                >
                  <ChevronsDownUp className='size-3.5' />
                  {t('Collapse all')}
                </Button>
              </ButtonGroup>
            </div>
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
                search ? t('No matching models') : t('No model monitor data')
              }
            />
          ) : (
            <div className='space-y-3'>
              {filteredGroups.map((group) => (
                <GroupSection
                  key={group.name}
                  group={group}
                  open={!closedGroups.has(group.name)}
                  onOpenChange={(open) => setGroupOpen(group.name, open)}
                />
              ))}
            </div>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
