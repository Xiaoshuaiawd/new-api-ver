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
  AlertTriangle,
  CheckCircle2,
  Clock,
  RefreshCw,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatDateTimeObject } from '@/lib/time'
import { formatLatency } from '@/features/performance-metrics/lib/format'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { getChannelRuntimeHealthReport } from '../api'

const healthReportQueryKey = ['channels', 'runtime-health-report'] as const
const ALL_FILTER_VALUE = 'all'

function eventBadgeVariant(type: string) {
  if (type === 'opened' || type === 'probe_failed') return 'destructive'
  if (type === 'recovered') return 'secondary'
  return 'outline'
}

function optionalFilter(value: string) {
  const trimmed = value.trim()
  return trimmed === '' || trimmed === ALL_FILTER_VALUE ? undefined : trimmed
}

function optionalChannelID(value: string) {
  const trimmed = value.trim()
  if (trimmed === '') return undefined
  const parsed = Number(trimmed)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : undefined
}

export function ChannelHealthReportPanel() {
  const { t } = useTranslation()
  const [channelID, setChannelID] = useState('')
  const [model, setModel] = useState('')
  const [group, setGroup] = useState('')
  const [eventType, setEventType] = useState(ALL_FILTER_VALUE)
  const [state, setState] = useState(ALL_FILTER_VALUE)

  const queryParams = useMemo(
    () => ({
      channel_id: optionalChannelID(channelID),
      model: optionalFilter(model),
      group: optionalFilter(group),
      type: optionalFilter(eventType),
      state: optionalFilter(state),
      limit: 50,
    }),
    [channelID, eventType, group, model, state]
  )

  const { data, isFetching, refetch } = useQuery({
    queryKey: [...healthReportQueryKey, queryParams],
    queryFn: () => getChannelRuntimeHealthReport(queryParams),
    refetchInterval: 30_000,
  })

  const report = data?.data
  const events = report?.events ?? []
  const topFailing = report?.top_failing_channels ?? []

  return (
    <Card size='sm' className='rounded-lg'>
      <CardHeader>
        <CardTitle className='flex items-center gap-2'>
          <Activity className='size-4' />
          {t('Channel health overview')}
        </CardTitle>
        <CardDescription>
          {t('Recent runtime isolation, recovery, and probe activity.')}
        </CardDescription>
        <CardAction>
          <Button
            variant='ghost'
            size='icon-sm'
            onClick={() => refetch()}
            disabled={isFetching}
            title={t('Refresh')}
          >
            <RefreshCw
              className={isFetching ? 'size-4 animate-spin' : 'size-4'}
            />
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className='grid gap-4'>
        <div className='grid gap-2 sm:grid-cols-2 lg:grid-cols-5'>
          <Input
            type='number'
            min={1}
            value={channelID}
            onChange={(event) => setChannelID(event.target.value)}
            placeholder={t('Channel ID')}
            aria-label={t('Channel ID')}
          />
          <Input
            value={model}
            onChange={(event) => setModel(event.target.value)}
            placeholder={t('Model')}
            aria-label={t('Model')}
          />
          <Input
            value={group}
            onChange={(event) => setGroup(event.target.value)}
            placeholder={t('Group')}
            aria-label={t('Group')}
          />
          <Select
            value={eventType}
            onValueChange={(value) => setEventType(value ?? ALL_FILTER_VALUE)}
          >
            <SelectTrigger className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value={ALL_FILTER_VALUE}>{t('All')}</SelectItem>
                <SelectItem value='opened'>{t('opened')}</SelectItem>
                <SelectItem value='recovered'>{t('recovered')}</SelectItem>
                <SelectItem value='probe_failed'>{t('probe_failed')}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
          <Select
            value={state}
            onValueChange={(value) => setState(value ?? ALL_FILTER_VALUE)}
          >
            <SelectTrigger className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value={ALL_FILTER_VALUE}>{t('All')}</SelectItem>
                <SelectItem value='healthy'>healthy</SelectItem>
                <SelectItem value='open'>open</SelectItem>
                <SelectItem value='probing'>probing</SelectItem>
                <SelectItem value='warming'>warming</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>

        <div className='grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(280px,420px)]'>
          <div className='grid gap-3 sm:grid-cols-4'>
            <div className='rounded-md border p-3'>
              <div className='text-muted-foreground flex items-center gap-2 text-xs'>
                <AlertTriangle className='size-3.5' />
                {t('Isolations')}
              </div>
              <div className='mt-1 text-xl font-semibold'>
                {report?.isolation_count ?? 0}
              </div>
            </div>
            <div className='rounded-md border p-3'>
              <div className='text-muted-foreground flex items-center gap-2 text-xs'>
                <CheckCircle2 className='size-3.5' />
                {t('Recoveries')}
              </div>
              <div className='mt-1 text-xl font-semibold'>
                {report?.recovery_count ?? 0}
              </div>
            </div>
            <div className='rounded-md border p-3'>
              <div className='text-muted-foreground flex items-center gap-2 text-xs'>
                <AlertTriangle className='size-3.5' />
                {t('Probe failures')}
              </div>
              <div className='mt-1 text-xl font-semibold'>
                {report?.probe_failure_count ?? 0}
              </div>
            </div>
            <div className='rounded-md border p-3'>
              <div className='text-muted-foreground flex items-center gap-2 text-xs'>
                <Clock className='size-3.5' />
                {t('Average first response')}
              </div>
              <div className='mt-1 text-xl font-semibold'>
                {formatLatency(report?.average_first_response_ms ?? 0)}
              </div>
            </div>
            <div className='rounded-md border p-3 sm:col-span-4'>
              <div className='text-muted-foreground text-xs'>
                {t('Top failing channels')}
              </div>
              <div className='mt-2 flex flex-wrap gap-2'>
                {topFailing.length > 0 ? (
                  topFailing.slice(0, 5).map((item) => (
                    <Badge
                      key={`${item.channel_id}:${item.model_name ?? ''}:${item.group ?? ''}`}
                      variant='outline'
                    >
                      #{item.channel_id}
                      {item.model_name ? ` / ${item.model_name}` : ''}:{' '}
                      {item.count}
                      {item.group ? ` (${item.group})` : ''}
                    </Badge>
                  ))
                ) : (
                  <span className='text-muted-foreground text-sm'>
                    {t('No recent failures')}
                  </span>
                )}
              </div>
            </div>
          </div>

          <div className='min-h-0 rounded-md border'>
            <div className='border-b px-3 py-2 text-sm font-medium'>
              {t('Health event timeline')}
            </div>
            <div className='max-h-[420px] divide-y overflow-y-auto'>
              {events.length > 0 ? (
                events.map((event) => (
                  <div
                    key={`${event.occurred_at}:${event.channel_id}:${event.type}:${event.model_name ?? ''}:${event.group ?? ''}`}
                    className='grid gap-1 px-3 py-2 text-sm'
                  >
                    <div className='flex items-center justify-between gap-2'>
                      <div className='flex min-w-0 items-center gap-2'>
                        <Badge variant={eventBadgeVariant(event.type)}>
                          {t(event.type)}
                        </Badge>
                        <span className='truncate'>
                          #{event.channel_id}
                          {event.model_name ? ` / ${event.model_name}` : ''}
                          {event.group ? ` / ${event.group}` : ''}
                        </span>
                      </div>
                      <span className='text-muted-foreground shrink-0 text-xs'>
                        {event.occurred_at > 0
                          ? formatDateTimeObject(
                              new Date(event.occurred_at * 1000)
                            )
                          : ''}
                      </span>
                    </div>
                    {event.reason ? (
                      <div className='text-muted-foreground truncate text-xs'>
                        {event.reason}
                      </div>
                    ) : null}
                  </div>
                ))
              ) : (
                <div className='text-muted-foreground px-3 py-6 text-center text-sm'>
                  {t('No recent health events')}
                </div>
              )}
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
