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
import { Handshake, ShieldCheck } from 'lucide-react'
import * as React from 'react'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import { safeJsonParseWithValidation } from '../utils/json-parser'
import { isObjectRecord } from '../utils/json-validators'

type ReferralFirstTopUpRewardConfig = {
  enabled: boolean
  activity_id: string
  activity_name: string
  start_time: number
  end_time: number
  min_paid_money: number
  threshold_operator: 'gte' | 'gt'
  first_topup_mode: 'strict_first' | 'first_qualified'
  invitee_reward_percent: number
  inviter_reward_percent: number
  inviter_settle_delay_days: number
  single_invitee_reward_max_quota: number
  single_inviter_reward_max_quota: number
  inviter_monthly_max_quota: number
  total_budget_quota: number
  stack_with_topup_bonus: boolean
  excluded_payment_providers: string[]
  excluded_user_groups: string[]
  auto_block_risky_rewards: boolean
  visible: boolean
}

type ReferralFirstTopUpRewardVisualEditorProps = {
  value: string
  onChange: (value: string) => void
}

const DEFAULT_REWARD_CONFIG: ReferralFirstTopUpRewardConfig = {
  enabled: false,
  activity_id: 'referral_first_topup_v1',
  activity_name: '邀请首充双向奖励',
  start_time: 0,
  end_time: 0,
  min_paid_money: 30,
  threshold_operator: 'gte',
  first_topup_mode: 'strict_first',
  invitee_reward_percent: 10,
  inviter_reward_percent: 10,
  inviter_settle_delay_days: 7,
  single_invitee_reward_max_quota: 0,
  single_inviter_reward_max_quota: 0,
  inviter_monthly_max_quota: 0,
  total_budget_quota: 0,
  stack_with_topup_bonus: true,
  excluded_payment_providers: [],
  excluded_user_groups: [],
  auto_block_risky_rewards: true,
  visible: true,
}

function toNumber(value: unknown, fallback = 0) {
  const next = Number(value)
  return Number.isFinite(next) ? next : fallback
}

function splitList(value: string) {
  return value
    .split(/[,，\n]+/g)
    .map((item) => item.trim())
    .filter(Boolean)
}

function joinList(value: string[]) {
  return value.join(', ')
}

function normalizeConfig(value: string): ReferralFirstTopUpRewardConfig {
  const parsed = safeJsonParseWithValidation<Record<string, unknown>>(value, {
    fallback: {},
    validator: isObjectRecord,
    silent: true,
  })

  const thresholdOperator =
    parsed.threshold_operator === 'gt' || parsed.threshold_operator === 'gte'
      ? parsed.threshold_operator
      : DEFAULT_REWARD_CONFIG.threshold_operator
  const firstTopupMode =
    parsed.first_topup_mode === 'first_qualified' ||
    parsed.first_topup_mode === 'strict_first'
      ? parsed.first_topup_mode
      : DEFAULT_REWARD_CONFIG.first_topup_mode

  return {
    enabled: parsed.enabled === true,
    activity_id:
      typeof parsed.activity_id === 'string'
        ? parsed.activity_id
        : DEFAULT_REWARD_CONFIG.activity_id,
    activity_name:
      typeof parsed.activity_name === 'string'
        ? parsed.activity_name
        : DEFAULT_REWARD_CONFIG.activity_name,
    start_time: toNumber(parsed.start_time),
    end_time: toNumber(parsed.end_time),
    min_paid_money: toNumber(
      parsed.min_paid_money,
      DEFAULT_REWARD_CONFIG.min_paid_money
    ),
    threshold_operator: thresholdOperator,
    first_topup_mode: firstTopupMode,
    invitee_reward_percent: toNumber(
      parsed.invitee_reward_percent,
      DEFAULT_REWARD_CONFIG.invitee_reward_percent
    ),
    inviter_reward_percent: toNumber(
      parsed.inviter_reward_percent,
      DEFAULT_REWARD_CONFIG.inviter_reward_percent
    ),
    inviter_settle_delay_days: toNumber(
      parsed.inviter_settle_delay_days,
      DEFAULT_REWARD_CONFIG.inviter_settle_delay_days
    ),
    single_invitee_reward_max_quota: toNumber(
      parsed.single_invitee_reward_max_quota
    ),
    single_inviter_reward_max_quota: toNumber(
      parsed.single_inviter_reward_max_quota
    ),
    inviter_monthly_max_quota: toNumber(parsed.inviter_monthly_max_quota),
    total_budget_quota: toNumber(parsed.total_budget_quota),
    stack_with_topup_bonus: parsed.stack_with_topup_bonus !== false,
    excluded_payment_providers: Array.isArray(parsed.excluded_payment_providers)
      ? parsed.excluded_payment_providers
          .map((item) => (typeof item === 'string' ? item.trim() : ''))
          .filter(Boolean)
      : [],
    excluded_user_groups: Array.isArray(parsed.excluded_user_groups)
      ? parsed.excluded_user_groups
          .map((item) => (typeof item === 'string' ? item.trim() : ''))
          .filter(Boolean)
      : [],
    auto_block_risky_rewards: parsed.auto_block_risky_rewards !== false,
    visible: parsed.visible !== false,
  }
}

function timestampToInput(value: number) {
  if (!value) return ''
  const date = new Date(value * 1000)
  if (Number.isNaN(date.getTime())) return ''
  const offsetMs = date.getTimezoneOffset() * 60 * 1000
  return new Date(date.getTime() - offsetMs).toISOString().slice(0, 16)
}

function inputToTimestamp(value: string) {
  if (!value) return 0
  const timestamp = Math.floor(new Date(value).getTime() / 1000)
  return Number.isFinite(timestamp) ? timestamp : 0
}

export function ReferralFirstTopUpRewardVisualEditor({
  value,
  onChange,
}: ReferralFirstTopUpRewardVisualEditorProps) {
  const { t } = useTranslation()
  const config = React.useMemo(() => normalizeConfig(value), [value])

  const update = <K extends keyof ReferralFirstTopUpRewardConfig>(
    key: K,
    nextValue: ReferralFirstTopUpRewardConfig[K]
  ) => {
    onChange(
      JSON.stringify(
        {
          ...config,
          [key]: nextValue,
        },
        null,
        2
      )
    )
  }

  const previewInvitee = (() => {
    const raw = Math.floor((100 * config.invitee_reward_percent) / 100)
    return config.single_invitee_reward_max_quota > 0
      ? Math.min(raw, config.single_invitee_reward_max_quota)
      : raw
  })()
  const previewInviter = (() => {
    const raw = Math.floor((100 * config.inviter_reward_percent) / 100)
    return config.single_inviter_reward_max_quota > 0
      ? Math.min(raw, config.single_inviter_reward_max_quota)
      : raw
  })()

  return (
    <div className='space-y-5 rounded-md border p-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
        <div className='min-w-0 space-y-1'>
          <div className='flex items-center gap-2 font-medium'>
            <Handshake className='h-4 w-4' />
            <span>{t('Referral first top-up rewards')}</span>
          </div>
          <p className='text-muted-foreground text-sm'>
            {t(
              'When the invitee makes the first qualified wallet top-up, the invitee gets rewarded immediately and the inviter gets rewarded after the delay window.'
            )}
          </p>
        </div>
        <div className='flex items-center gap-2'>
          <span className='text-muted-foreground text-sm'>
            {config.enabled ? t('Enabled') : t('Disabled')}
          </span>
          <Switch
            checked={config.enabled}
            onCheckedChange={(checked) => update('enabled', checked)}
          />
        </div>
      </div>

      <div className='grid gap-4 md:grid-cols-2'>
        <div className='space-y-2'>
          <Label>{t('Activity ID')}</Label>
          <Input
            value={config.activity_id}
            onChange={(event) => update('activity_id', event.target.value)}
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Activity Name')}</Label>
          <Input
            value={config.activity_name}
            onChange={(event) => update('activity_name', event.target.value)}
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Start time')}</Label>
          <Input
            type='datetime-local'
            value={timestampToInput(config.start_time)}
            onChange={(event) =>
              update('start_time', inputToTimestamp(event.target.value))
            }
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('End time')}</Label>
          <Input
            type='datetime-local'
            value={timestampToInput(config.end_time)}
            onChange={(event) =>
              update('end_time', inputToTimestamp(event.target.value))
            }
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Minimum paid amount')}</Label>
          <Input
            type='number'
            min={0}
            step='0.01'
            value={config.min_paid_money}
            onChange={(event) =>
              update('min_paid_money', toNumber(event.target.value))
            }
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Threshold operator')}</Label>
          <Select
            value={config.threshold_operator}
            onValueChange={(value) =>
              update('threshold_operator', value as 'gte' | 'gt')
            }
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='gte'>{t('Greater than or equal')}</SelectItem>
              <SelectItem value='gt'>{t('Greater than')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className='space-y-2'>
          <Label>{t('First top-up mode')}</Label>
          <Select
            value={config.first_topup_mode}
            onValueChange={(value) =>
              update(
                'first_topup_mode',
                value as 'strict_first' | 'first_qualified'
              )
            }
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='strict_first'>
                {t('Strict first top-up')}
              </SelectItem>
              <SelectItem value='first_qualified'>
                {t('First qualified top-up')}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className='space-y-2'>
          <Label>{t('Invitee reward percentage')}</Label>
          <Input
            type='number'
            min={0}
            step='0.01'
            value={config.invitee_reward_percent}
            onChange={(event) =>
              update('invitee_reward_percent', toNumber(event.target.value))
            }
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Inviter reward percentage')}</Label>
          <Input
            type='number'
            min={0}
            step='0.01'
            value={config.inviter_reward_percent}
            onChange={(event) =>
              update('inviter_reward_percent', toNumber(event.target.value))
            }
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Inviter settle delay days')}</Label>
          <Input
            type='number'
            min={0}
            step='1'
            value={config.inviter_settle_delay_days}
            onChange={(event) =>
              update('inviter_settle_delay_days', toNumber(event.target.value))
            }
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Single invitee reward cap')}</Label>
          <Input
            type='number'
            min={0}
            step='1'
            value={config.single_invitee_reward_max_quota}
            onChange={(event) =>
              update(
                'single_invitee_reward_max_quota',
                toNumber(event.target.value)
              )
            }
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Single inviter reward cap')}</Label>
          <Input
            type='number'
            min={0}
            step='1'
            value={config.single_inviter_reward_max_quota}
            onChange={(event) =>
              update(
                'single_inviter_reward_max_quota',
                toNumber(event.target.value)
              )
            }
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Inviter monthly reward cap')}</Label>
          <Input
            type='number'
            min={0}
            step='1'
            value={config.inviter_monthly_max_quota}
            onChange={(event) =>
              update('inviter_monthly_max_quota', toNumber(event.target.value))
            }
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Total activity budget')}</Label>
          <Input
            type='number'
            min={0}
            step='1'
            value={config.total_budget_quota}
            onChange={(event) =>
              update('total_budget_quota', toNumber(event.target.value))
            }
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Excluded payment providers')}</Label>
          <Textarea
            rows={3}
            value={joinList(config.excluded_payment_providers)}
            onChange={(event) =>
              update(
                'excluded_payment_providers',
                splitList(event.target.value)
              )
            }
            placeholder='stripe, waffo'
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Excluded user groups')}</Label>
          <Textarea
            rows={3}
            value={joinList(config.excluded_user_groups)}
            onChange={(event) =>
              update('excluded_user_groups', splitList(event.target.value))
            }
            placeholder='default, trial'
          />
        </div>
        <div className='grid gap-3 sm:grid-cols-2 md:col-span-2'>
          <label className='flex items-center justify-between gap-3 rounded-md border px-3 py-2'>
            <span className='text-sm'>{t('Stack with recharge bonus')}</span>
            <Switch
              checked={config.stack_with_topup_bonus}
              onCheckedChange={(checked) =>
                update('stack_with_topup_bonus', checked)
              }
            />
          </label>
          <label className='flex items-center justify-between gap-3 rounded-md border px-3 py-2'>
            <span className='text-sm'>{t('Auto block risky rewards')}</span>
            <Switch
              checked={config.auto_block_risky_rewards}
              onCheckedChange={(checked) =>
                update('auto_block_risky_rewards', checked)
              }
            />
          </label>
          <label className='flex items-center justify-between gap-3 rounded-md border px-3 py-2'>
            <span className='text-sm'>{t('Show to users')}</span>
            <Switch
              checked={config.visible}
              onCheckedChange={(checked) => update('visible', checked)}
            />
          </label>
        </div>
      </div>

      <div className='bg-muted/40 space-y-1 rounded-md px-3 py-2 text-sm'>
        <div className='flex items-center gap-2 font-medium'>
          <ShieldCheck className='h-4 w-4' />
          <span>{t('Reward preview')}</span>
        </div>
        <div className='text-muted-foreground'>
          {t(
            'Invitee receives {{invitee}} and inviter receives {{inviter}} for a 100 quota first top-up.',
            {
              invitee: previewInvitee,
              inviter: previewInviter,
            }
          )}
        </div>
      </div>
    </div>
  )
}
