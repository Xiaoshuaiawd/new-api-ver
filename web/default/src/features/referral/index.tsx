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
import {
  CheckCircle2,
  Copy,
  Gift,
  Handshake,
  Info,
  RefreshCw,
  Share2,
  TrendingUp,
  Users,
  XCircle,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { formatNumber, formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  getSelfReferralRewards,
  getSelfReferralSummary,
} from '../referrals/api'
import type { ReferralReward, ReferralSummary } from '../referrals/types'

type StatCardProps = {
  title: string
  value: string
  description?: string
  icon: React.ReactNode
  tone?: 'default' | 'success' | 'warning' | 'info'
}

function StatCard({ title, value, description, icon, tone = 'default' }: StatCardProps) {
  return (
    <Card
      className={cn(
        'min-h-28',
        tone !== 'default' && 'ring-1',
        tone === 'success' && 'ring-emerald-500/30',
        tone === 'warning' && 'ring-amber-500/30',
        tone === 'info' && 'ring-blue-500/30'
      )}
    >
      <CardHeader className='pb-2'>
        <div className='flex items-center justify-between'>
          <CardDescription className='text-xs font-medium uppercase tracking-wider'>
            {title}
          </CardDescription>
          <div className='text-muted-foreground'>{icon}</div>
        </div>
        <CardTitle className='text-2xl tabular-nums'>{value}</CardTitle>
      </CardHeader>
      {description && (
        <CardContent className='text-muted-foreground pb-4 pt-0 text-xs'>
          {description}
        </CardContent>
      )}
    </Card>
  )
}

function ReferralLinkSection({ summary }: { summary: ReferralSummary }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)

  const handleCopy = () => {
    navigator.clipboard.writeText(summary.invite_link)
    setCopied(true)
    toast.success(t('Referral link copied!'))
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <Card>
      <CardHeader>
        <div className='flex items-center gap-2'>
          <Share2 className='size-4' />
          <CardTitle className='text-base'>{t('Your Referral Link')}</CardTitle>
        </div>
        <CardDescription>
          {t('Share this link with friends to earn rewards')}
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-3'>
        <div className='flex gap-2'>
          <Input
            value={summary.invite_link}
            readOnly
            className='font-mono text-xs'
          />
          <Button
            onClick={handleCopy}
            variant='outline'
            className='shrink-0 gap-2'
          >
            {copied ? (
              <CheckCircle2 className='size-4' />
            ) : (
              <Copy className='size-4' />
            )}
            {copied ? t('Copied!') : t('Copy')}
          </Button>
        </div>
        <div className='bg-muted/50 flex items-start gap-2 rounded-md p-3 text-xs'>
          <Info className='text-muted-foreground mt-0.5 size-4 shrink-0' />
          <div className='text-muted-foreground'>
            <div className='font-medium'>{t('Referral first top-up rewards')}</div>
            <div className='mt-1'>
              {t(
                'When your friend completes their first wallet top-up ≥ 30, both of you receive 10% of the base top-up amount as a reward. Your reward settles after 7 days.'
              )}
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function RewardsTable({ rewards }: { rewards: ReferralReward[] }) {
  const { t } = useTranslation()

  if (rewards.length === 0) {
    return (
      <div className='text-muted-foreground py-12 text-center text-sm'>
        {t('No reward records yet')}
      </div>
    )
  }

  const getRiskBadgeClass = (status: string) => {
    if (status === 'blocked' || status === 'review') {
      return 'bg-red-500/10 text-red-600'
    }
    if (status === 'approved' || status === 'normal') {
      return 'bg-emerald-500/10 text-emerald-600'
    }
    return 'bg-muted text-muted-foreground'
  }

  return (
    <div className='overflow-x-auto'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Reward ID')}</TableHead>
            <TableHead>{t('Role')}</TableHead>
            <TableHead>{t('Paid amount')}</TableHead>
            <TableHead>{t('Reward')}</TableHead>
            <TableHead>{t('Status')}</TableHead>
            <TableHead>{t('Risk status')}</TableHead>
            <TableHead>{t('Created at')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rewards.map((reward) => (
            <TableRow key={reward.id}>
              <TableCell className='font-mono text-xs'>#{reward.id}</TableCell>
              <TableCell>
                <Badge variant='secondary'>{t(reward.reward_role)}</Badge>
              </TableCell>
              <TableCell className='tabular-nums'>
                ${formatNumber(reward.paid_money)}
              </TableCell>
              <TableCell>
                <div>
                  <div className='font-medium'>
                    {formatQuota(reward.reward_quota)}
                  </div>
                  {reward.reversed_quota > 0 && (
                    <div className='text-muted-foreground text-xs'>
                      {t('Reversed')}: {formatQuota(reward.reversed_quota)}
                    </div>
                  )}
                </div>
              </TableCell>
              <TableCell>
                <Badge variant='secondary'>{t(reward.status)}</Badge>
              </TableCell>
              <TableCell>
                <Badge className={getRiskBadgeClass(reward.risk_status)}>
                  {t(reward.risk_status)}
                </Badge>
              </TableCell>
              <TableCell className='text-muted-foreground text-xs'>
                {new Date(reward.created_at * 1000).toLocaleDateString()}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

export function UserReferralPage() {
  const { t } = useTranslation()

  const summaryQuery = useQuery({
    queryKey: ['referral-self-summary'],
    queryFn: getSelfReferralSummary,
  })

  const rewardsQuery = useQuery({
    queryKey: ['referral-self-rewards'],
    queryFn: () =>
      getSelfReferralRewards({ p: 1, page_size: 50 }),
  })

  const inviteeRewardsQuery = useQuery({
    queryKey: ['referral-self-invitee-rewards'],
    queryFn: () =>
      getSelfReferralRewards({ p: 1, page_size: 50 }),
  })

  const refresh = () => {
    void summaryQuery.refetch()
    void rewardsQuery.refetch()
    void inviteeRewardsQuery.refetch()
  }

  const summary = summaryQuery.data?.data
  const isLoading = summaryQuery.isLoading
  const isError = summaryQuery.isError

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <div className='flex items-center gap-2'>
          <Handshake className='size-5' />
          {t('Referral Program')}
        </div>
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          onClick={refresh}
          disabled={isLoading}
          className='gap-2'
        >
          <RefreshCw className={cn('size-4', isLoading && 'animate-spin')} />
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-6'>
          {isError ? (
            <ErrorState title={t('Loading failed')} />
          ) : isLoading ? (
            <>
              <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-4'>
                {Array.from({ length: 4 }, (_, i) => `skeleton-${i}`).map((key) => (
                  <Skeleton key={key} className='h-32' />
                ))}
              </div>
              <Skeleton className='h-48' />
            </>
          ) : summary ? (
            <>
              {/* Stats Cards */}
              <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-4'>
                <StatCard
                  title={t('Total Invites')}
                  value={formatNumber(summary.invite_count)}
                  description={t('Users registered with your referral code')}
                  icon={<Users className='size-4' />}
                  tone='info'
                />
                <StatCard
                  title={t('Qualified first top-ups')}
                  value={formatNumber(summary.qualified_first_topup_count)}
                  description={t('Friends who completed first top-up ≥ 30')}
                  icon={<CheckCircle2 className='size-4' />}
                  tone='success'
                />
                <StatCard
                  title={t('Pending rewards')}
                  value={formatQuota(summary.pending_reward_quota)}
                  description={t('Waiting for settlement after risk window')}
                  icon={<Gift className='size-4' />}
                  tone='warning'
                />
                <StatCard
                  title={t('Settled rewards')}
                  value={formatQuota(summary.settled_reward_quota)}
                  description={t('Total rewards credited to your account')}
                  icon={<TrendingUp className='size-4' />}
                  tone='success'
                />
              </div>

              {/* Additional info */}
              {summary.reversed_reward_quota > 0 && (
                <Alert className='border-amber-200 bg-amber-50 text-amber-950 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-100'>
                  <XCircle className='size-4' />
                  <AlertDescription>
                    {t('Reversed rewards')}: {formatQuota(summary.reversed_reward_quota)}
                    {' — '}
                    {t('Rewards deducted due to refunds or risk review.')}
                  </AlertDescription>
                </Alert>
              )}

              {/* Referral Link */}
              <ReferralLinkSection summary={summary} />

              {/* Rewards History */}
              <Card>
                <CardHeader>
                  <CardTitle className='flex items-center gap-2'>
                    <Gift className='size-4' />
                    {t('Reward History')}
                  </CardTitle>
                  <CardDescription>
                    {t('Your referral reward records and status')}
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <Tabs defaultValue='inviter'>
                    <TabsList>
                      <TabsTrigger value='inviter'>
                        {t('As Inviter')}
                        {rewardsQuery.data?.data.items.length ? (
                          <Badge
                            variant='secondary'
                            className='ml-2 rounded-full'
                          >
                            {rewardsQuery.data.data.items.length}
                          </Badge>
                        ) : null}
                      </TabsTrigger>
                      <TabsTrigger value='invitee'>
                        {t('As Invitee')}
                        {inviteeRewardsQuery.data?.data.items.length ? (
                          <Badge
                            variant='secondary'
                            className='ml-2 rounded-full'
                          >
                            {inviteeRewardsQuery.data.data.items.length}
                          </Badge>
                        ) : null}
                      </TabsTrigger>
                    </TabsList>
                    <TabsContent value='inviter' className='mt-4'>
                      {rewardsQuery.isLoading ? (
                        <div className='space-y-2'>
                          {[1, 2, 3].map((i) => (
                            <Skeleton key={i} className='h-12' />
                          ))}
                        </div>
                      ) : (
                        <RewardsTable
                          rewards={rewardsQuery.data?.data.items || []}
                        />
                      )}
                    </TabsContent>
                    <TabsContent value='invitee' className='mt-4'>
                      {inviteeRewardsQuery.isLoading ? (
                        <div className='space-y-2'>
                          {[1, 2, 3].map((i) => (
                            <Skeleton key={i} className='h-12' />
                          ))}
                        </div>
                      ) : (
                        <RewardsTable
                          rewards={inviteeRewardsQuery.data?.data.items || []}
                        />
                      )}
                    </TabsContent>
                  </Tabs>
                </CardContent>
              </Card>
            </>
          ) : null}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
