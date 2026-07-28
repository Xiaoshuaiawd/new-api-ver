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
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import {
  formatLocalCurrencyAmount,
  formatQuotaWithCurrency,
} from '@/lib/currency'
import { formatLogQuota } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import { getLogStats, getUserLogStats } from '../api'
import { DEFAULT_LOG_STATS } from '../constants'
import { buildApiParams } from '../lib/utils'
import type { LogStatistics } from '../types'
import { useLogsViewScope, useUsageLogsContext } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

function StatBadge(props: {
  label: string
  value: string | number
  accent: string
}) {
  return (
    <span className='border-border/60 bg-muted/25 inline-flex h-7 items-center gap-2 rounded-md border px-2.5 text-xs shadow-xs'>
      <span className={cn('h-3.5 w-0.5 rounded-full', props.accent)} />
      <span className='text-muted-foreground'>{props.label}</span>
      <span className='text-foreground/85 font-mono font-semibold tabular-nums'>
        {props.value}
      </span>
    </span>
  )
}

export function CommonLogsStatsView(props: {
  stats: LogStatistics
  isRoot: boolean
  sensitiveVisible: boolean
}) {
  const { t } = useTranslation()

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <StatBadge
        label={t('Usage')}
        value={
          props.sensitiveVisible ? formatLogQuota(props.stats.quota) : '••••'
        }
        accent='bg-sky-500/70'
      />
      <StatBadge
        label={t('RPM')}
        value={props.stats.rpm}
        accent='bg-rose-500/65'
      />
      <StatBadge
        label={t('TPM')}
        value={props.stats.tpm}
        accent='bg-slate-400/70'
      />
      {props.isRoot ? (
        <StatBadge
          label={t('Period Revenue')}
          value={formatLocalCurrencyAmount(props.stats.today_revenue ?? 0, {
            digitsLarge: 2,
            digitsSmall: 2,
            abbreviate: false,
          })}
          accent='bg-emerald-500/70'
        />
      ) : null}
      {props.isRoot ? (
        <StatBadge
          label={t('Actual Quota Consumption')}
          value={formatQuotaWithCurrency(props.stats.actual_quota ?? 0, {
            digitsLarge: 2,
            digitsSmall: 2,
            abbreviate: false,
          })}
          accent='bg-amber-500/70'
        />
      ) : null}
    </div>
  )
}

export function CommonLogsStatsSkeleton(props: { isRoot: boolean }) {
  return (
    <div className='flex items-center gap-2'>
      <Skeleton className='h-7 w-[150px] rounded-md' />
      <Skeleton className='h-7 w-[100px] rounded-md' />
      <Skeleton className='h-7 w-[120px] rounded-md' />
      {props.isRoot ? <Skeleton className='h-7 w-[130px] rounded-md' /> : null}
      {props.isRoot ? <Skeleton className='h-7 w-[160px] rounded-md' /> : null}
    </div>
  )
}

export function CommonLogsStats() {
  const userRole = useAuthStore((state) => state.auth.user?.role ?? 0)
  const isRoot = userRole >= ROLE.SUPER_ADMIN
  const { isAdminView: isAdmin } = useLogsViewScope()
  const canViewRootStats = isRoot && isAdmin
  const searchParams = route.useSearch()
  const { sensitiveVisible } = useUsageLogsContext()

  const { data: stats, isLoading } = useQuery({
    queryKey: ['usage-logs-stats', isAdmin, searchParams],
    queryFn: async () => {
      const params = buildApiParams({
        page: 1,
        pageSize: 1,
        searchParams,
        columnFilters: [],
        isAdmin,
      })

      const result = isAdmin
        ? await getLogStats(params)
        : await getUserLogStats(params)

      return result.success
        ? result.data || DEFAULT_LOG_STATS
        : DEFAULT_LOG_STATS
    },
    placeholderData: (previousData) => previousData,
  })

  if (isLoading) {
    return <CommonLogsStatsSkeleton isRoot={canViewRootStats} />
  }

  return (
    <CommonLogsStatsView
      stats={stats ?? DEFAULT_LOG_STATS}
      isRoot={canViewRootStats}
      sensitiveVisible={sensitiveVisible}
    />
  )
}
