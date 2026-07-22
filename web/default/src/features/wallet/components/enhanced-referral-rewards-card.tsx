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
import { Handshake, Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { toast } from 'sonner'

import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { IconBadge } from '@/components/ui/icon-badge'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { formatQuota, formatNumber } from '@/lib/format'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import { getSelfReferralSummary } from '../../referrals/api'
import type { UserWalletData } from '../types'

interface EnhancedReferralRewardsCardProps {
  user: UserWalletData | null
  affiliateLink: string
  onTransfer: () => void
  complianceConfirmed?: boolean
  loading?: boolean
}

export function EnhancedReferralRewardsCard({
  user,
  affiliateLink,
  onTransfer,
  complianceConfirmed = true,
  loading,
}: EnhancedReferralRewardsCardProps) {
  const { t } = useTranslation()

  const { data: referralSummaryData, isLoading: summaryLoading } = useQuery({
    queryKey: ['referral-self-summary'],
    queryFn: getSelfReferralSummary,
    staleTime: 30 * 1000,
  })

  const summary = referralSummaryData?.data
  const showLoading = loading || summaryLoading

  if (showLoading) {
    return (
      <Card data-card-hover='false'>
        <CardHeader className='pb-2'>
          <Skeleton className='h-5 w-40' />
          <Skeleton className='mt-1 h-4 w-64' />
        </CardHeader>
        <CardContent className='space-y-4'>
          <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
            {Array.from({ length: 4 }, (_, i) => `stat-${i}`).map((k) => (
              <Skeleton key={k} className='h-14 rounded-lg' />
            ))}
          </div>
          <Skeleton className='h-10 rounded-md' />
        </CardContent>
      </Card>
    )
  }

  const hasTransferableRewards = (user?.aff_quota ?? 0) > 0

  const statItems = [
    {
      label: t('Invites'),
      value: formatNumber(summary?.invite_count ?? user?.aff_count ?? 0),
    },
    {
      label: t('Qualified first top-ups'),
      value: formatNumber(summary?.qualified_first_topup_count ?? 0),
    },
    {
      label: t('Pending rewards'),
      value: formatQuota(summary?.pending_reward_quota ?? 0),
    },
    {
      label: t('Settled rewards'),
      value: formatQuota(summary?.settled_reward_quota ?? 0),
    },
  ]

  return (
    <Card data-card-hover='false'>
      <CardHeader className='pb-3'>
        <div className='flex items-center gap-2'>
          <IconBadge tone='chart-3'>
            <Handshake />
          </IconBadge>
          <div>
            <CardTitle className='text-base'>{t('Referral Program')}</CardTitle>
            <CardDescription className='text-xs'>
              {t('Invite friends to get first top-up rewards for both parties.')}
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent className='space-y-4'>
        {/* Stats grid */}
        <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
          {statItems.map(({ label, value }) => (
            <div
              key={label}
              className='bg-muted/30 rounded-lg p-2.5 text-center'
            >
              <div className='text-muted-foreground truncate text-[10px] font-medium tracking-wider uppercase'>
                {label}
              </div>
              <div className='mt-0.5 truncate text-sm font-semibold tabular-nums'>
                {value}
              </div>
            </div>
          ))}
        </div>

        {/* Invite link */}
        <div className='flex items-center gap-2'>
          <Input
            value={affiliateLink}
            readOnly
            className='border-muted bg-background/70 h-9 min-w-0 flex-1 font-mono text-xs'
            aria-label={t('Your Referral Link')}
          />
          <CopyButton
            value={affiliateLink}
            variant='outline'
            className='bg-background size-9 shrink-0'
            iconClassName='size-4'
            tooltip={t('Copy referral link')}
            aria-label={t('Copy referral link')}
          />
          {hasTransferableRewards && (
            <Button
              onClick={onTransfer}
              disabled={!complianceConfirmed}
              className='h-9 shrink-0 px-3'
              size='sm'
            >
              {t('Transfer to Balance')}
            </Button>
          )}
        </div>

        {/* Activity rule hint */}
        {summary?.invite_count !== undefined && (
          <div className='bg-muted/20 flex items-start gap-2 rounded-md px-3 py-2 text-xs'>
            <Info className='text-muted-foreground mt-0.5 size-3.5 shrink-0' />
            <span className='text-muted-foreground'>
              {t(
                'Invite friends to register and complete their first wallet top-up ≥ 30. Both you and your friend each receive 10% of the base top-up amount as a reward.'
              )}
            </span>
          </div>
        )}

        {/* Reversed rewards warning */}
        {(summary?.reversed_reward_quota ?? 0) > 0 && (
          <div className='flex items-center gap-2 text-xs'>
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger className='flex items-center gap-1.5 text-amber-600 dark:text-amber-400'>
                  <Info className='size-3' />
                  {t('Reversed')}: {formatQuota(summary!.reversed_reward_quota)}
                </TooltipTrigger>
                <TooltipContent>
                  {t('Rewards deducted due to refunds or risk review.')}
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>
        )}

        {!complianceConfirmed && (
          <p className='text-muted-foreground text-xs'>
            {t(
              'Referral reward transfer is disabled until the administrator confirms compliance terms.'
            )}
          </p>
        )}
      </CardContent>
    </Card>
  )
}
