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
import * as React from 'react'
import { Gift } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { safeJsonParseWithValidation } from '../utils/json-parser'
import { isObjectRecord } from '../utils/json-validators'

type TopUpBonusConfig = {
  enabled: boolean
  activity_id: string
  activity_name: string
  start_time: number
  end_time: number
  min_amount: number
  bonus_percent: number
  single_bonus_max_amount: number
  user_bonus_max_amount: number
  total_bonus_budget_amount: number
  first_topup_only: boolean
  visible: boolean
}

type TopUpBonusVisualEditorProps = {
  value: string
  onChange: (value: string) => void
}

const DEFAULT_TOPUP_BONUS: TopUpBonusConfig = {
  enabled: false,
  activity_id: '',
  activity_name: '',
  start_time: 0,
  end_time: 0,
  min_amount: 100,
  bonus_percent: 10,
  single_bonus_max_amount: 0,
  user_bonus_max_amount: 0,
  total_bonus_budget_amount: 0,
  first_topup_only: false,
  visible: true,
}

function toNumber(value: unknown, fallback = 0) {
  const next = Number(value)
  return Number.isFinite(next) ? next : fallback
}

function normalizeConfig(value: string): TopUpBonusConfig {
  const parsed = safeJsonParseWithValidation<Record<string, unknown>>(value, {
    fallback: {},
    validator: isObjectRecord,
    silent: true,
  })

  return {
    enabled: parsed.enabled === true,
    activity_id:
      typeof parsed.activity_id === 'string' ? parsed.activity_id : '',
    activity_name:
      typeof parsed.activity_name === 'string' ? parsed.activity_name : '',
    start_time: toNumber(parsed.start_time),
    end_time: toNumber(parsed.end_time),
    min_amount: toNumber(
      parsed.min_amount,
      DEFAULT_TOPUP_BONUS.min_amount
    ),
    bonus_percent: toNumber(
      parsed.bonus_percent,
      DEFAULT_TOPUP_BONUS.bonus_percent
    ),
    single_bonus_max_amount: toNumber(parsed.single_bonus_max_amount),
    user_bonus_max_amount: toNumber(parsed.user_bonus_max_amount),
    total_bonus_budget_amount: toNumber(parsed.total_bonus_budget_amount),
    first_topup_only: parsed.first_topup_only === true,
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

export function TopUpBonusVisualEditor({
  value,
  onChange,
}: TopUpBonusVisualEditorProps) {
  const { t } = useTranslation()
  const config = React.useMemo(() => normalizeConfig(value), [value])

  const update = <K extends keyof TopUpBonusConfig>(
    key: K,
    nextValue: TopUpBonusConfig[K]
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

  const previewBonus =
    config.min_amount > 0 && config.bonus_percent > 0
      ? Math.floor((config.min_amount * config.bonus_percent) / 100)
      : 0
  const cappedPreviewBonus =
    config.single_bonus_max_amount > 0
      ? Math.min(previewBonus, config.single_bonus_max_amount)
      : previewBonus

  return (
    <div className='space-y-5 rounded-md border p-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
        <div className='min-w-0 space-y-1'>
          <div className='flex items-center gap-2 font-medium'>
            <Gift className='h-4 w-4' />
            <span>{t('Recharge bonus activity')}</span>
          </div>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Users receive an extra percentage after a successful payment reaches the minimum amount.'
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
            placeholder='618-2026'
            onChange={(event) => update('activity_id', event.target.value)}
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Activity Name')}</Label>
          <Input
            value={config.activity_name}
            placeholder={t('618 Recharge Bonus')}
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
          <Label>{t('Minimum recharge amount')}</Label>
          <Input
            type='number'
            min={0}
            step='1'
            value={config.min_amount}
            onChange={(event) => update('min_amount', toNumber(event.target.value))}
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Bonus percentage')}</Label>
          <Input
            type='number'
            min={0}
            step='0.01'
            value={config.bonus_percent}
            onChange={(event) =>
              update('bonus_percent', toNumber(event.target.value))
            }
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Single bonus cap')}</Label>
          <Input
            type='number'
            min={0}
            step='1'
            value={config.single_bonus_max_amount}
            onChange={(event) =>
              update('single_bonus_max_amount', toNumber(event.target.value))
            }
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Per-user bonus cap')}</Label>
          <Input
            type='number'
            min={0}
            step='1'
            value={config.user_bonus_max_amount}
            onChange={(event) =>
              update('user_bonus_max_amount', toNumber(event.target.value))
            }
          />
        </div>
        <div className='space-y-2'>
          <Label>{t('Total bonus budget')}</Label>
          <Input
            type='number'
            min={0}
            step='1'
            value={config.total_bonus_budget_amount}
            onChange={(event) =>
              update('total_bonus_budget_amount', toNumber(event.target.value))
            }
          />
        </div>
        <div className='grid gap-3 sm:grid-cols-2'>
          <label className='flex items-center justify-between gap-3 rounded-md border px-3 py-2'>
            <span className='text-sm'>{t('First top-up only')}</span>
            <Switch
              checked={config.first_topup_only}
              onCheckedChange={(checked) => update('first_topup_only', checked)}
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

      <div className='bg-muted/40 rounded-md px-3 py-2 text-sm'>
        {t(
          'Preview: recharge {{amount}} and get {{bonus}} bonus credit.',
          {
            amount: config.min_amount || 0,
            bonus: cappedPreviewBonus,
          }
        )}
      </div>
    </div>
  )
}
