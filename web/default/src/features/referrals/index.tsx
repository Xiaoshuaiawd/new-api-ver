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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  BarChart3,
  ChevronLeft,
  ChevronRight,
  Gift,
  Info,
  RefreshCw,
  Shield,
  TrendingUp,
  Users,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatNumber, formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  approveReferralReward,
  blockInviterPendingRewards,
  blockReferralReward,
  cancelReferralReward,
  getReferralRewards,
  getReferralRiskRewards,
  getReferralStatsFunnel,
  getReferralStatsSummary,
  getReferralStatsTrend,
  getReferralTopInviters,
  reverseReferralReward,
} from './api'
import type {
  ReferralFunnelItem,
  ReferralReward,
  ReferralStatsParams,
  ReferralStatsSummary,
  ReferralTopInviter,
  ReferralTrendItem,
} from './types'

type RangeType =
  | 'today'
  | 'yesterday'
  | '7d'
  | '30d'
  | 'month'
  | 'last_month'
  | 'custom'
type BucketType = 'day' | 'week' | 'month'
type TopInviterSort = '' | 'settled_reward_desc' | 'roi_desc' | 'refund_rate_desc'
type RewardAction = 'approve' | 'block' | 'cancel' | 'reverse' | 'block_pending'

type FilterState = {
  rangeType: RangeType
  customStart: string
  customEnd: string
  activityId: string
  inviterKeyword: string
  inviteeKeyword: string
  paymentProvider: string
  status: string
  riskStatus: string
  userGroup: string
  refundOnly: boolean
}

const DEFAULT_FILTER: FilterState = {
  rangeType: '30d',
  customStart: '',
  customEnd: '',
  activityId: '',
  inviterKeyword: '',
  inviteeKeyword: '',
  paymentProvider: '',
  status: '',
  riskStatus: '',
  userGroup: '',
  refundOnly: false,
}
function filterToParams(filter: FilterState): Partial<ReferralStatsParams> {
  const now = Math.floor(Date.now() / 1000)
  let start_time: number | undefined
  let end_time: number | undefined
  switch (filter.rangeType) {
    case 'today': {
      const d = new Date()
      d.setHours(0, 0, 0, 0)
      start_time = Math.floor(d.getTime() / 1000)
      break
    }
    case 'yesterday': {
      const d = new Date()
      d.setHours(0, 0, 0, 0)
      end_time = Math.floor(d.getTime() / 1000) - 1
      start_time = end_time - 86399
      break
    }
    case '7d':
      start_time = now - 7 * 86400
      break
    case '30d':
      start_time = now - 30 * 86400
      break
    case 'month': {
      const d = new Date()
      start_time = Math.floor(
        new Date(d.getFullYear(), d.getMonth(), 1).getTime() / 1000
      )
      break
    }
    case 'last_month': {
      const d = new Date()
      const s = new Date(d.getFullYear(), d.getMonth() - 1, 1)
      const e = new Date(d.getFullYear(), d.getMonth(), 1)
      start_time = Math.floor(s.getTime() / 1000)
      end_time = Math.floor(e.getTime() / 1000) - 1
      break
    }
    case 'custom':
      if (filter.customStart)
        start_time = Math.floor(new Date(filter.customStart).getTime() / 1000)
      if (filter.customEnd)
        end_time = Math.floor(
          new Date(filter.customEnd + 'T23:59:59').getTime() / 1000
        )
      break
  }
  return {
    start_time,
    end_time,
    activity_id: filter.activityId || undefined,
    inviter_keyword: filter.inviterKeyword || undefined,
    invitee_keyword: filter.inviteeKeyword || undefined,
    payment_provider: filter.paymentProvider || undefined,
    status: filter.status || undefined,
    risk_status: filter.riskStatus || undefined,
    user_group: filter.userGroup || undefined,
    refund_only: filter.refundOnly || undefined,
  }
}
function formatRate(value: number) {
  if (!Number.isFinite(value)) return '0%'
  return `${formatNumber(value * 100)}%`
}

function formatMoney(value: number) {
  return `$${formatNumber(value)}`
}

function formatTs(ts: number) {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleString()
}

function formatDate(ts: number) {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleDateString()
}

function riskBadgeClass(status: string) {
  if (status === 'blocked' || status === 'review')
    return 'bg-red-500/10 text-red-600'
  if (status === 'approved' || status === 'normal')
    return 'bg-emerald-500/10 text-emerald-600'
  return 'bg-muted text-muted-foreground'
}

function statusBadgeVariant(status: string): 'default' | 'secondary' | 'destructive' {
  if (status === 'settled') return 'default'
  if (status === 'cancelled' || status === 'reversed') return 'destructive'
  return 'secondary'
}

type StatCardProps = {
  title: string
  value: string
  description?: string
  tone?: 'default' | 'success' | 'warning' | 'danger'
}

function StatCard({ title, value, description, tone = 'default' }: StatCardProps) {
  return (
    <Card
      className={cn(
        'min-h-32',
        tone !== 'default' && 'ring-1',
        tone === 'success' && 'ring-emerald-500/30',
        tone === 'warning' && 'ring-amber-500/30',
        tone === 'danger' && 'ring-red-500/30'
      )}
    >
      <CardHeader className='pb-0'>
        <CardDescription>{title}</CardDescription>
        <CardTitle className='text-2xl'>{value}</CardTitle>
      </CardHeader>
      {description && (
        <CardContent className='text-muted-foreground text-xs'>
          {description}
        </CardContent>
      )}
    </Card>
  )
}
function SummaryCards({ summary }: { summary: ReferralStatsSummary }) {
  const { t } = useTranslation()
  const sentReward =
    summary.invitee_settled_reward_quota + summary.inviter_settled_reward_quota
  return (
    <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-4 2xl:grid-cols-6'>
      <StatCard
        title={t('Referral registrations')}
        value={formatNumber(summary.invite_registered_count)}
        description={t('Users registered with an invite relationship')}
      />
      <StatCard
        title={t('Qualified first top-ups')}
        value={formatNumber(summary.qualified_first_topup_count)}
        description={t('First wallet top-ups with paid amount at least 30')}
        tone='success'
      />
      <StatCard
        title={t('Referral top-up revenue')}
        value={formatMoney(summary.qualified_first_topup_net_money)}
        description={t('Net paid amount after refunds')}
        tone='success'
      />
      <div>
        <Card className='min-h-32 ring-1 ring-amber-500/30'>
          <CardHeader className='pb-0'>
            <CardDescription className='flex items-center gap-1'>
              {t('Rewards sent')}
              <Tooltip>
                <TooltipTrigger>
                  <Info className='size-3 cursor-pointer' />
                </TooltipTrigger>
                <TooltipContent className='max-w-56 text-xs'>
                  {t(
                    'Sum of settled invitee and inviter rewards. Excludes pending rewards and reversed portions.'
                  )}
                </TooltipContent>
              </Tooltip>
            </CardDescription>
            <CardTitle className='text-2xl'>{formatQuota(sentReward)}</CardTitle>
          </CardHeader>
        </Card>
      </div>
      <StatCard
        title={t('Pending rewards')}
        value={formatQuota(summary.pending_reward_quota)}
        description={t('Inviter rewards waiting for risk-window settlement')}
        tone='warning'
      />
      <StatCard
        title={t('Reversed rewards')}
        value={formatQuota(summary.reversed_reward_quota)}
        description={t('Rewards cancelled or deducted after refunds or risk review')}
        tone='danger'
      />
      <StatCard
        title={t('First top-up conversion')}
        value={formatRate(summary.conversion_rate)}
      />
      <StatCard
        title={t('Reward cost rate')}
        value={formatRate(summary.reward_cost_rate)}
      />
      <StatCard title='ROI' value={formatNumber(summary.roi)} />
      <StatCard
        title={t('Refund amount')}
        value={formatMoney(summary.refund_money)}
        tone={summary.refund_money > 0 ? 'danger' : 'default'}
      />
      <StatCard
        title={t('Refund rate')}
        value={formatRate(summary.refund_rate)}
        tone={summary.refund_rate > 0.1 ? 'danger' : 'default'}
      />
      <StatCard
        title={t('Risk holds')}
        value={formatNumber(summary.blocked_reward_count)}
        description={t('Rewards in review or blocked status')}
        tone={summary.blocked_reward_count > 0 ? 'danger' : 'default'}
      />
    </div>
  )
}
const RANGE_OPTIONS: Array<{ value: RangeType; labelKey: string }> = [
  { value: 'today', labelKey: 'Today' },
  { value: 'yesterday', labelKey: 'Yesterday' },
  { value: '7d', labelKey: 'Last 7 days' },
  { value: '30d', labelKey: 'Last 30 days' },
  { value: 'month', labelKey: 'This month' },
  { value: 'last_month', labelKey: 'Last month' },
  { value: 'custom', labelKey: 'Custom' },
]

const STATUS_OPTIONS = [
  'pending',
  'settled',
  'cancelled',
  'reversed',
  'partial_reversed',
]

const RISK_STATUS_OPTIONS = ['normal', 'review', 'blocked', 'approved', 'rejected']

type FilterBarProps = {
  filter: FilterState
  onChange: (patch: Partial<FilterState>) => void
}

function FilterBar({ filter, onChange }: FilterBarProps) {
  const { t } = useTranslation()
  return (
    <Card>
      <CardContent className='pt-4'>
        <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4'>
          <div className='space-y-1'>
            <Label className='text-xs'>{t('Time range')}</Label>
            <Select
              value={filter.rangeType}
              onValueChange={(v) => onChange({ rangeType: v as RangeType })}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {RANGE_OPTIONS.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {t(o.labelKey)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {filter.rangeType === 'custom' && (
            <>
              <div className='space-y-1'>
                <Label className='text-xs'>{t('Start date')}</Label>
                <Input
                  type='date'
                  value={filter.customStart}
                  onChange={(e) => onChange({ customStart: e.target.value })}
                />
              </div>
              <div className='space-y-1'>
                <Label className='text-xs'>{t('End date')}</Label>
                <Input
                  type='date'
                  value={filter.customEnd}
                  onChange={(e) => onChange({ customEnd: e.target.value })}
                />
              </div>
            </>
          )}
          <div className='space-y-1'>
            <Label className='text-xs'>{t('Activity ID')}</Label>
            <Input
              placeholder={t('Filter by activity ID')}
              value={filter.activityId}
              onChange={(e) => onChange({ activityId: e.target.value })}
            />
          </div>
          <div className='space-y-1'>
            <Label className='text-xs'>{t('Inviter ID / username')}</Label>
            <Input
              placeholder={t('Search inviter')}
              value={filter.inviterKeyword}
              onChange={(e) => onChange({ inviterKeyword: e.target.value })}
            />
          </div>
          <div className='space-y-1'>
            <Label className='text-xs'>{t('Invitee ID / username')}</Label>
            <Input
              placeholder={t('Search invitee')}
              value={filter.inviteeKeyword}
              onChange={(e) => onChange({ inviteeKeyword: e.target.value })}
            />
          </div>
          <div className='space-y-1'>
            <Label className='text-xs'>{t('Payment provider')}</Label>
            <Input
              placeholder={t('e.g. alipay, stripe')}
              value={filter.paymentProvider}
              onChange={(e) => onChange({ paymentProvider: e.target.value })}
            />
          </div>
          <div className='space-y-1'>
            <Label className='text-xs'>{t('Reward status')}</Label>
            <Select
              value={filter.status || 'all'}
              onValueChange={(v) => onChange({ status: v === 'all' ? undefined : (v || undefined) })}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='all'>{t('All statuses')}</SelectItem>
                {STATUS_OPTIONS.map((s) => (
                  <SelectItem key={s} value={s}>
                    {t(s)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className='space-y-1'>
            <Label className='text-xs'>{t('Risk status')}</Label>
            <Select
              value={filter.riskStatus || 'all'}
              onValueChange={(v) => onChange({ riskStatus: v === 'all' ? undefined : (v || undefined) })}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='all'>{t('All risk statuses')}</SelectItem>
                {RISK_STATUS_OPTIONS.map((s) => (
                  <SelectItem key={s} value={s}>
                    {t(s)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className='space-y-1'>
            <Label className='text-xs'>{t('User group')}</Label>
            <Input
              placeholder={t('Search user group')}
              value={filter.userGroup}
              onChange={(e) => onChange({ userGroup: e.target.value })}
            />
          </div>
          <div className='flex items-end pb-1'>
            <div className='flex items-center gap-2'>
              <Checkbox
                id='refund-only'
                checked={filter.refundOnly}
                onCheckedChange={(checked) =>
                  onChange({ refundOnly: checked === true })
                }
              />
              <Label htmlFor='refund-only' className='cursor-pointer text-sm'>
                {t('Refund only')}
              </Label>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
function Funnel({ items }: { items: ReferralFunnelItem[] }) {
  const { t } = useTranslation()
  const max = Math.max(...items.map((item) => item.count), 1)
  return (
    <Card>
      <CardHeader>
        <CardTitle className='flex items-center gap-2'>
          <BarChart3 className='size-4' />
          {t('Referral funnel')}
        </CardTitle>
        <CardDescription>
          {t('Registration to qualified first top-up and settlement')}
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-3'>
        {items.map((item) => (
          <div key={item.stage} className='space-y-1'>
            <div className='flex items-center justify-between text-sm'>
              <span>{t(item.stage)}</span>
              <span className='font-medium'>{formatNumber(item.count)}</span>
            </div>
            <div className='bg-muted h-2 overflow-hidden rounded-full'>
              <div
                className='bg-primary h-full rounded-full'
                style={{ width: `${Math.max((item.count / max) * 100, 2)}%` }}
              />
            </div>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

const BUCKET_OPTIONS: Array<{ value: BucketType; labelKey: string }> = [
  { value: 'day', labelKey: 'Day' },
  { value: 'week', labelKey: 'Week' },
  { value: 'month', labelKey: 'Month' },
]

const TREND_SERIES = [
  { key: 'net_money', labelKey: 'Net revenue', color: 'bg-emerald-500', fmt: formatMoney },
  { key: 'refund_money', labelKey: 'Refund amount', color: 'bg-red-400', fmt: formatMoney },
  { key: 'qualified_first_topup_count', labelKey: 'Qualified first top-ups', color: 'bg-blue-400', fmt: formatNumber },
] as const
function Trend({ items, params }: { items: ReferralTrendItem[]; params: Partial<ReferralStatsParams> }) {
  const { t } = useTranslation()
  const [bucket, setBucket] = useState<BucketType>('day')

  const trendQuery = useQuery({
    queryKey: ['referral-stats-trend-bucket', params, bucket],
    queryFn: () => getReferralStatsTrend({ ...params, bucket }),
  })

  const trendItems = trendQuery.data?.data || items
  const visibleItems = trendItems.slice(-14)
  const maxValues = TREND_SERIES.map((s) =>
    Math.max(...visibleItems.map((item) => (item as any)[s.key] || 0), 1)
  )

  return (
    <Card>
      <CardHeader>
        <div className='flex items-start justify-between'>
          <div>
            <CardTitle className='flex items-center gap-2'>
              <TrendingUp className='size-4' />
              {t('Referral trend')}
            </CardTitle>
            <CardDescription>
              {t('Daily net revenue, refund amount, and qualified first top-ups')}
            </CardDescription>
          </div>
          <Select value={bucket} onValueChange={(v) => setBucket(v as BucketType)}>
            <SelectTrigger className='w-24'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {BUCKET_OPTIONS.map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {t(o.labelKey)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </CardHeader>
      <CardContent className='space-y-3'>
        {visibleItems.length === 0 ? (
          <div className='text-muted-foreground py-8 text-center text-sm'>
            {t('No referral data')}
          </div>
        ) : (
          visibleItems.map((item) => (
            <div key={item.bucket} className='space-y-1'>
              <div className='text-muted-foreground text-xs'>{item.bucket}</div>
              {TREND_SERIES.map((series, idx) => {
                const val = (item as any)[series.key] || 0
                const pct = Math.max((val / maxValues[idx]) * 100, 1)
                return (
                  <div key={series.key} className='grid grid-cols-[1fr_auto] items-center gap-2 text-xs'>
                    <div className='bg-muted h-1.5 overflow-hidden rounded-full'>
                      <div className={cn('h-full rounded-full', series.color)} style={{ width: `${pct}%` }} />
                    </div>
                    <span className='font-medium tabular-nums'>{series.fmt(val)}</span>
                  </div>
                )
              })}
            </div>
          ))
        )}
      </CardContent>
    </Card>
  )
}
function Paginator({
  page,
  pageSize,
  total,
  onChange,
}: {
  page: number
  pageSize: number
  total: number
  onChange: (page: number) => void
}) {
  const { t } = useTranslation()
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  return (
    <div className='flex items-center justify-between pt-2 text-sm'>
      <span className='text-muted-foreground'>
        {t('Page {{page}} of {{total}}', { page, total: totalPages })}
        {' · '}
        {formatNumber(total)} {t('records')}
      </span>
      <div className='flex items-center gap-1'>
        <Button
          variant='outline'
          size='icon'
          className='size-8'
          disabled={page <= 1}
          onClick={() => onChange(page - 1)}
        >
          <ChevronLeft className='size-4' />
        </Button>
        <Button
          variant='outline'
          size='icon'
          className='size-8'
          disabled={page >= totalPages}
          onClick={() => onChange(page + 1)}
        >
          <ChevronRight className='size-4' />
        </Button>
      </div>
    </div>
  )
}

const TOP_INVITER_SORT_OPTIONS: Array<{ value: TopInviterSort; labelKey: string }> = [
  { value: '', labelKey: 'Default (net revenue)' },
  { value: 'settled_reward_desc', labelKey: 'Settled reward desc' },
  { value: 'roi_desc', labelKey: 'ROI desc' },
  { value: 'refund_rate_desc', labelKey: 'Refund rate desc' },
]
function TopInvitersTable({
  params,
}: {
  params: Partial<ReferralStatsParams>
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [sort, setSort] = useState<TopInviterSort>('')
  const [blockTarget, setBlockTarget] = useState<ReferralTopInviter | null>(null)
  const [blockReason, setBlockReason] = useState('')

  const query = useQuery({
    queryKey: ['referral-top-inviters', params, sort],
    queryFn: () =>
      getReferralTopInviters({ ...params, limit: 20, sort: sort || undefined }),
  })

  const blockMutation = useMutation({
    mutationFn: () =>
      blockInviterPendingRewards(blockTarget!.inviter_id, blockReason),
    onSuccess: () => {
      setBlockTarget(null)
      setBlockReason('')
      void queryClient.invalidateQueries({ queryKey: ['referral-top-inviters'] })
    },
  })

  const items = query.data?.data || []

  return (
    <>
      <Card>
        <CardHeader>
          <div className='flex items-start justify-between gap-2 flex-wrap'>
            <CardTitle className='flex items-center gap-2'>
              <Users className='size-4' />
              {t('Top inviters')}
            </CardTitle>
            <Select value={sort || 'default'} onValueChange={(v) => setSort(v === 'default' ? '' : (v as TopInviterSort))}>
              <SelectTrigger className='w-44'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {TOP_INVITER_SORT_OPTIONS.map((o) => (
                  <SelectItem key={o.value || 'default'} value={o.value || 'default'}>
                    {t(o.labelKey)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </CardHeader>
        <CardContent>
          <div className='overflow-x-auto'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Inviter')}</TableHead>
                  <TableHead>{t('Referral registrations')}</TableHead>
                  <TableHead>{t('Qualified first top-ups')}</TableHead>
                  <TableHead>{t('Net revenue')}</TableHead>
                  <TableHead>{t('Settled rewards')}</TableHead>
                  <TableHead>{t('Invitee rewards')}</TableHead>
                  <TableHead>{t('Pending rewards')}</TableHead>
                  <TableHead>{t('Refund amount')}</TableHead>
                  <TableHead>{t('Refund rate')}</TableHead>
                  <TableHead>ROI</TableHead>
                  <TableHead>{t('Risk status')}</TableHead>
                  <TableHead>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={12} className='text-muted-foreground h-24 text-center'>
                      {t('No referral data')}
                    </TableCell>
                  </TableRow>
                ) : (
                  items.map((item) => (
                    <TableRow key={item.inviter_id}>
                      <TableCell>
                        <div className='font-medium'>#{item.inviter_id}</div>
                        <div className='text-muted-foreground text-xs'>
                          {item.inviter_username || '-'}
                        </div>
                      </TableCell>
                      <TableCell>{formatNumber(item.invite_registered_count)}</TableCell>
                      <TableCell>{formatNumber(item.qualified_first_topup_count)}</TableCell>
                      <TableCell>{formatMoney(item.first_topup_net_money)}</TableCell>
                      <TableCell>{formatQuota(item.inviter_settled_reward_quota)}</TableCell>
                      <TableCell>{formatQuota(item.invitee_reward_quota)}</TableCell>
                      <TableCell>{formatQuota(item.pending_reward_quota)}</TableCell>
                      <TableCell>{formatMoney(item.refund_money)}</TableCell>
                      <TableCell>{formatRate(item.refund_rate)}</TableCell>
                      <TableCell>{formatNumber(item.roi)}</TableCell>
                      <TableCell>
                        <Badge className={riskBadgeClass(item.risk_status)}>
                          {t(item.risk_status)}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <Button
                          variant='outline'
                          size='sm'
                          disabled={item.pending_reward_quota === 0}
                          onClick={() => setBlockTarget(item)}
                        >
                          <Shield className='size-3 mr-1' />
                          {t('Block pending')}
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>
      <Dialog open={!!blockTarget} onOpenChange={() => setBlockTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t('Block pending rewards for inviter')} #{blockTarget?.inviter_id}
            </DialogTitle>
          </DialogHeader>
          <div className='space-y-3'>
            <div className='text-muted-foreground text-sm'>
              {t('This will freeze {{amount}} pending rewards.', {
                amount: formatQuota(blockTarget?.pending_reward_quota || 0),
              })}
            </div>
            <div className='space-y-1'>
              <Label>{t('Reason')}</Label>
              <Textarea
                value={blockReason}
                onChange={(e) => setBlockReason(e.target.value)}
                placeholder={t('Enter reason for blocking')}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setBlockTarget(null)}>
              {t('Cancel')}
            </Button>
            <Button
              onClick={() => blockMutation.mutate()}
              disabled={blockMutation.isPending || !blockReason.trim()}
            >
              {t('Confirm block')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
type ActionDialogProps = {
  open: boolean
  action: RewardAction
  reward: ReferralReward | null
  onClose: () => void
  onSuccess: () => void
}

const ACTION_LABELS: Record<RewardAction, string> = {
  approve: 'Approve',
  block: 'Block',
  cancel: 'Cancel',
  reverse: 'Reverse',
  block_pending: 'Block pending',
}

function ActionDialog({ open, action, reward, onClose, onSuccess }: ActionDialogProps) {
  const { t } = useTranslation()
  const [reason, setReason] = useState('')

  const mutationFn = async () => {
    if (!reward) return
    if (action === 'approve') return approveReferralReward(reward.id, reason)
    if (action === 'block') return blockReferralReward(reward.id, reason)
    if (action === 'cancel') return cancelReferralReward(reward.id, reason)
    if (action === 'reverse') return reverseReferralReward(reward.id, reason)
  }

  const mutation = useMutation({
    mutationFn,
    onSuccess: () => {
      setReason('')
      onSuccess()
    },
  })

  const handleClose = () => {
    setReason('')
    mutation.reset()
    onClose()
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {t(ACTION_LABELS[action])} {t('Reward')} #{reward?.id}
          </DialogTitle>
        </DialogHeader>
        <div className='space-y-3'>
          <div className='text-muted-foreground text-sm'>
            {t('Reward')}: {formatQuota(reward?.reward_quota || 0)} ·{' '}
            {t('Status')}: {t(reward?.status || '')}
          </div>
          <div className='space-y-1'>
            <Label>{t('Reason')}</Label>
            <Textarea
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder={t('Enter reason (optional)')}
            />
          </div>
          {mutation.isError && (
            <div className='text-destructive text-xs'>
              {String((mutation.error as Error)?.message || t('Operation failed'))}
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={handleClose}>{t('Cancel')}</Button>
          <Button
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
            variant={action === 'reverse' || action === 'cancel' ? 'destructive' : 'default'}
          >
            {t(ACTION_LABELS[action])}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
function RewardsTableContent({
  params,
  isRiskQueue,
}: {
  params: Partial<ReferralStatsParams>
  isRiskQueue?: boolean
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [actionDialog, setActionDialog] = useState<{
    action: RewardAction
    reward: ReferralReward
  } | null>(null)

  const pageSize = 15
  const queryKey = isRiskQueue
    ? ['referral-risk-rewards', params, page]
    : ['referral-rewards', params, page]
  const queryFn = isRiskQueue
    ? () => getReferralRiskRewards({ ...params, p: page, page_size: pageSize })
    : () => getReferralRewards({ ...params, p: page, page_size: pageSize })

  const query = useQuery({ queryKey, queryFn })
  const items = query.data?.data.items || []
  const total = query.data?.data.total || 0

  const openAction = (action: RewardAction, reward: ReferralReward) => {
    setActionDialog({ action, reward })
  }

  const onActionSuccess = () => {
    setActionDialog(null)
    void queryClient.invalidateQueries({ queryKey: isRiskQueue
      ? ['referral-risk-rewards']
      : ['referral-rewards'] })
  }

  const desktopTable = (
    <div className='hidden overflow-x-auto sm:block'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Reward ID')}</TableHead>
            <TableHead>{t('Activity ID')}</TableHead>
            <TableHead>{t('Role')}</TableHead>
            <TableHead>{t('Inviter')}</TableHead>
            <TableHead>{t('Invitee')}</TableHead>
            <TableHead>{t('Trade No')}</TableHead>
            <TableHead>{t('Paid')}</TableHead>
            <TableHead>{t('Base quota')}</TableHead>
            <TableHead>{t('Reward %')}</TableHead>
            <TableHead>{t('Reward')}</TableHead>
            <TableHead>{t('Settled')}</TableHead>
            <TableHead>{t('Status')}</TableHead>
            <TableHead>{t('Risk')}</TableHead>
            <TableHead>{t('Settle at')}</TableHead>
            <TableHead>{t('Created')}</TableHead>
            <TableHead>{t('Actions')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.length === 0 ? (
            <TableRow>
              <TableCell colSpan={16} className='text-muted-foreground h-24 text-center'>
                {t('No referral rewards')}
              </TableCell>
            </TableRow>
          ) : (
            items.map((item) => (
              <TableRow key={item.id}>
                <TableCell>#{item.id}</TableCell>
                <TableCell className='max-w-24 truncate text-xs'>{item.activity_id || '-'}</TableCell>
                <TableCell>{t(item.reward_role)}</TableCell>
                <TableCell>
                  <div className='font-medium'>#{item.inviter_id}</div>
                </TableCell>
                <TableCell>#{item.invitee_id}</TableCell>
                <TableCell className='max-w-32 truncate text-xs'>{item.trade_no}</TableCell>
                <TableCell>{formatMoney(item.paid_money)}</TableCell>
                <TableCell>{formatQuota(item.base_quota)}</TableCell>
                <TableCell>{formatRate(item.reward_percent / 100)}</TableCell>
                <TableCell>
                  <div>{formatQuota(item.reward_quota)}</div>
                  {item.reversed_quota > 0 && (
                    <div className='text-muted-foreground text-xs'>
                      -{formatQuota(item.reversed_quota)}
                    </div>
                  )}
                </TableCell>
                <TableCell>{formatQuota(item.settled_quota)}</TableCell>
                <TableCell>
                  <Badge variant={statusBadgeVariant(item.status)}>{t(item.status)}</Badge>
                </TableCell>
                <TableCell>
                  <Badge className={riskBadgeClass(item.risk_status)}>{t(item.risk_status)}</Badge>
                </TableCell>
                <TableCell className='whitespace-nowrap text-xs'>{formatDate(item.settle_at)}</TableCell>
                <TableCell className='whitespace-nowrap text-xs'>{formatTs(item.created_at)}</TableCell>
                <TableCell>
                  <div className='flex gap-1 flex-wrap'>
                    {item.risk_status !== 'approved' && (
                      <Button variant='outline' size='sm' onClick={() => openAction('approve', item)}>{t('Approve')}</Button>
                    )}
                    {item.risk_status !== 'blocked' && item.status === 'pending' && (
                      <Button variant='outline' size='sm' onClick={() => openAction('block', item)}>{t('Block')}</Button>
                    )}
                    {item.status === 'pending' && (
                      <Button variant='outline' size='sm' onClick={() => openAction('cancel', item)}>{t('Cancel')}</Button>
                    )}
                    <Button variant='outline' size='sm' onClick={() => openAction('reverse', item)}>{t('Reverse')}</Button>
                  </div>
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  )
  const mobileCards = (
    <div className='space-y-3 sm:hidden'>
      {items.length === 0 ? (
        <div className='text-muted-foreground py-8 text-center text-sm'>
          {t('No referral rewards')}
        </div>
      ) : (
        items.map((item) => (
          <Card key={item.id}>
            <CardContent className='space-y-2 pt-4'>
              <div className='flex items-start justify-between gap-2'>
                <div className='font-medium'>
                  {t('Reward')} #{item.id}
                </div>
                <div className='flex gap-1'>
                  <Badge variant={statusBadgeVariant(item.status)} className='text-xs'>
                    {t(item.status)}
                  </Badge>
                  <Badge className={cn(riskBadgeClass(item.risk_status), 'text-xs')}>
                    {t(item.risk_status)}
                  </Badge>
                </div>
              </div>
              <Separator />
              <div className='grid grid-cols-2 gap-2 text-xs'>
                <div>
                  <span className='text-muted-foreground'>{t('Activity ID')}: </span>
                  <span className='font-medium'>{item.activity_id || '-'}</span>
                </div>
                <div>
                  <span className='text-muted-foreground'>{t('Role')}: </span>
                  <span className='font-medium'>{t(item.reward_role)}</span>
                </div>
                <div>
                  <span className='text-muted-foreground'>{t('Inviter')}: </span>
                  <span className='font-medium'>#{item.inviter_id}</span>
                </div>
                <div>
                  <span className='text-muted-foreground'>{t('Invitee')}: </span>
                  <span className='font-medium'>#{item.invitee_id}</span>
                </div>
                <div>
                  <span className='text-muted-foreground'>{t('Paid')}: </span>
                  <span className='font-medium'>{formatMoney(item.paid_money)}</span>
                </div>
                <div>
                  <span className='text-muted-foreground'>{t('Reward %')}: </span>
                  <span className='font-medium'>{formatRate(item.reward_percent / 100)}</span>
                </div>
                <div>
                  <span className='text-muted-foreground'>{t('Reward')}: </span>
                  <span className='font-medium'>{formatQuota(item.reward_quota)}</span>
                </div>
                <div>
                  <span className='text-muted-foreground'>{t('Settled')}: </span>
                  <span className='font-medium'>{formatQuota(item.settled_quota)}</span>
                </div>
                <div className='col-span-2'>
                  <span className='text-muted-foreground'>{t('Settle at')}: </span>
                  <span className='font-medium'>{formatDate(item.settle_at)}</span>
                </div>
              </div>
              <div className='flex gap-1 flex-wrap pt-1'>
                {item.risk_status !== 'approved' && (
                  <Button variant='outline' size='sm' onClick={() => openAction('approve', item)}>
                    {t('Approve')}
                  </Button>
                )}
                {item.risk_status !== 'blocked' && item.status === 'pending' && (
                  <Button variant='outline' size='sm' onClick={() => openAction('block', item)}>
                    {t('Block')}
                  </Button>
                )}
                {item.status === 'pending' && (
                  <Button variant='outline' size='sm' onClick={() => openAction('cancel', item)}>
                    {t('Cancel')}
                  </Button>
                )}
                <Button variant='outline' size='sm' onClick={() => openAction('reverse', item)}>
                  {t('Reverse')}
                </Button>
              </div>
            </CardContent>
          </Card>
        ))
      )}
    </div>
  )
  return (
    <>
      {desktopTable}
      {mobileCards}
      {total > pageSize && (
        <Paginator page={page} pageSize={pageSize} total={total} onChange={setPage} />
      )}
      <ActionDialog
        open={!!actionDialog}
        action={actionDialog?.action || 'approve'}
        reward={actionDialog?.reward || null}
        onClose={() => setActionDialog(null)}
        onSuccess={onActionSuccess}
      />
    </>
  )
}

export function Referrals() {
  const { t } = useTranslation()
  const [filter, setFilter] = useState<FilterState>(DEFAULT_FILTER)
  const params = useMemo(() => filterToParams(filter), [filter])

  const summaryQuery = useQuery({
    queryKey: ['referral-stats-summary', params],
    queryFn: () => getReferralStatsSummary(params),
  })
  const funnelQuery = useQuery({
    queryKey: ['referral-stats-funnel', params],
    queryFn: () => getReferralStatsFunnel(params),
  })
  const trendQuery = useQuery({
    queryKey: ['referral-stats-trend', params],
    queryFn: () => getReferralStatsTrend(params),
  })

  const isLoading = summaryQuery.isLoading || funnelQuery.isLoading || trendQuery.isLoading
  const isError = summaryQuery.isError || funnelQuery.isError || trendQuery.isError

  const updateFilter = (patch: Partial<FilterState>) => {
    setFilter((prev) => ({ ...prev, ...patch }))
  }

  const refresh = () => {
    void summaryQuery.refetch()
    void funnelQuery.refetch()
    void trendQuery.refetch()
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Referral Stats')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button variant='outline' onClick={refresh} disabled={isLoading}>
          <RefreshCw className={cn('size-4', isLoading && 'animate-spin')} />
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          {isError ? (
            <ErrorState title={t('Loading failed')} />
          ) : (
            <>
              <div className='bg-muted/50 flex items-start gap-3 rounded-lg p-4 text-sm'>
                <Gift className='text-primary mt-0.5 size-4 shrink-0' />
                <div>
                  <div className='font-medium'>
                    {t('Referral first top-up rewards')}
                  </div>
                  <div className='text-muted-foreground'>
                    {t(
                      'Invitees with first wallet top-up paid amount at least 30 receive 10%, and inviters receive 10% after settlement.'
                    )}
                  </div>
                </div>
              </div>
              <FilterBar filter={filter} onChange={updateFilter} />
              {summaryQuery.data?.data && (
                <SummaryCards summary={summaryQuery.data.data} />
              )}
              <div className='grid gap-4 xl:grid-cols-2'>
                <Funnel items={funnelQuery.data?.data || []} />
                <Trend items={trendQuery.data?.data || []} params={params} />
              </div>
              <TopInvitersTable params={params} />
              <Card>
                <CardHeader>
                  <CardTitle className='flex items-center gap-2'>
                    <AlertTriangle className='size-4' />
                    {t('Referral rewards')}
                  </CardTitle>
                  <CardDescription>
                    {t('Latest referral reward ledger records and risk status')}
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <Tabs defaultValue='rewards'>
                    <TabsList>
                      <TabsTrigger value='rewards'>
                        {t('Reward ledger')}
                      </TabsTrigger>
                      <TabsTrigger value='risk'>
                        <Shield className='size-3 mr-1' />
                        {t('Risk queue')}
                      </TabsTrigger>
                    </TabsList>
                    <TabsContent value='rewards' className='mt-4'>
                      <RewardsTableContent params={params} />
                    </TabsContent>
                    <TabsContent value='risk' className='mt-4'>
                      <RewardsTableContent params={params} isRiskQueue />
                    </TabsContent>
                  </Tabs>
                </CardContent>
              </Card>
            </>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

