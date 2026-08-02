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
import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useMemo, useRef } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type { ChannelHealthSettings } from '../types'
import { safeNumberFieldProps } from '../utils/numeric-field'

/**
 * react-hook-form 7 treats dotted names as nested paths, so we model the form
 * with a proper nested object and flatten back to server key format on save.
 */
const healthSchema = z.object({
  channel_health_setting: z.object({
    enabled: z.boolean(),
    ttft_timeout_seconds: z.coerce.number().int().min(0).max(3600),
    window_size: z.coerce.number().int().min(10).max(1000),
    cooldown_after: z.coerce.number().int().min(1).max(1000),
    cooldown_duration_minutes: z.coerce.number().int().min(1).max(1440),
    reference_ttft_ms: z.coerce.number().int().min(100).max(600000),
    warmup_threshold: z.coerce.number().int().min(1).max(1000),
    min_multiplier_pct: z.coerce.number().int().min(1).max(99),
  }),
})

type HealthFormInput = z.input<typeof healthSchema>
type HealthFormValues = z.output<typeof healthSchema>

type FlatDefaults = ChannelHealthSettings

const buildFormDefaults = (d: FlatDefaults): HealthFormInput => ({
  channel_health_setting: {
    enabled: d['channel_health_setting.enabled'],
    ttft_timeout_seconds: d['channel_health_setting.ttft_timeout_seconds'],
    window_size: d['channel_health_setting.window_size'],
    cooldown_after: d['channel_health_setting.cooldown_after'],
    cooldown_duration_minutes:
      d['channel_health_setting.cooldown_duration_minutes'],
    reference_ttft_ms: d['channel_health_setting.reference_ttft_ms'],
    warmup_threshold: d['channel_health_setting.warmup_threshold'],
    min_multiplier_pct: d['channel_health_setting.min_multiplier_pct'],
  },
})

const normalizeFormValues = (values: HealthFormValues): FlatDefaults => ({
  'channel_health_setting.enabled': values.channel_health_setting.enabled,
  'channel_health_setting.ttft_timeout_seconds':
    values.channel_health_setting.ttft_timeout_seconds,
  'channel_health_setting.window_size':
    values.channel_health_setting.window_size,
  'channel_health_setting.cooldown_after':
    values.channel_health_setting.cooldown_after,
  'channel_health_setting.cooldown_duration_minutes':
    values.channel_health_setting.cooldown_duration_minutes,
  'channel_health_setting.reference_ttft_ms':
    values.channel_health_setting.reference_ttft_ms,
  'channel_health_setting.warmup_threshold':
    values.channel_health_setting.warmup_threshold,
  'channel_health_setting.min_multiplier_pct':
    values.channel_health_setting.min_multiplier_pct,
})

interface Props {
  defaultValues: FlatDefaults
}

export function ChannelHealthSection({ defaultValues }: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo(
    () => buildFormDefaults(defaultValues),
    [defaultValues]
  )

  const form = useForm<HealthFormInput, unknown, HealthFormValues>({
    resolver: zodResolver(healthSchema),
    defaultValues: formDefaults,
  })

  const baselineRef = useRef<FlatDefaults>(defaultValues)
  const baselineSerializedRef = useRef<string>(JSON.stringify(defaultValues))

  useEffect(() => {
    const serialized = JSON.stringify(defaultValues)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = defaultValues
    baselineSerializedRef.current = serialized
    form.reset(buildFormDefaults(defaultValues))
  }, [defaultValues, form])

  const onSubmit = async (values: HealthFormValues) => {
    const normalized = normalizeFormValues(values)
    const changedKeys = (
      Object.keys(normalized) as Array<keyof FlatDefaults>
    ).filter(
      (key) => String(normalized[key]) !== String(baselineRef.current[key])
    )
    if (changedKeys.length === 0) return

    for (const key of changedKeys) {
      await updateOption.mutateAsync({ key, value: normalized[key] })
    }

    baselineRef.current = normalized
    baselineSerializedRef.current = JSON.stringify(normalized)
    form.reset(buildFormDefaults(normalized))
  }

  const enabled = form.watch('channel_health_setting.enabled')

  return (
    <SettingsSection title={t('Channel Health')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          {/* Master switch */}
          <FormField
            control={form.control}
            name='channel_health_setting.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable Channel Health Tracking')}</FormLabel>
                  <FormDescription>
                    {t(
                      'When enabled, channel success rate and first-token latency are tracked in real time and used to dynamically adjust channel weights. Channels with poor performance are automatically deprioritised.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <Separator />

          {/* TTFT timeout */}
          <div>
            <h4 className='font-medium'>{t('First-Token Timeout')}</h4>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t(
                'If no first token is received within this deadline, the current attempt is cancelled and retried on another channel. Set to 0 to disable. Recommended: 10–15 s for chat models, 30–60 s for reasoning models.'
              )}
            </p>
          </div>

          <div className='max-w-xs'>
            <FormField
              control={form.control}
              name='channel_health_setting.ttft_timeout_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('TTFT Timeout (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={3600}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!enabled}
                    />
                  </FormControl>
                  <FormDescription>{t('0 = disabled')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <Separator />

          {/* Health scoring */}
          <div>
            <h4 className='font-medium'>{t('Health Scoring')}</h4>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t(
                'Parameters that control how the health multiplier is computed from recent request outcomes.'
              )}
            </p>
          </div>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='channel_health_setting.window_size'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Window Size')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={10}
                      max={1000}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!enabled}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Number of recent requests used to compute success rate (10–1000)'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='channel_health_setting.reference_ttft_ms'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Reference TTFT (ms)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={100}
                      max={600000}
                      step={100}
                      {...safeNumberFieldProps(field)}
                      disabled={!enabled}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Channels faster than this value get a TTFT factor of 1.0'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='channel_health_setting.warmup_threshold'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Warmup Threshold')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={1000}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!enabled}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Minimum samples required before penalising a channel')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='max-w-xs'>
            <FormField
              control={form.control}
              name='channel_health_setting.min_multiplier_pct'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Minimum Multiplier (%)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={99}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!enabled}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Floor multiplier (1–99 %) for degraded channels outside cooldown, ensuring they still receive a trickle of traffic and can recover'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <Separator />

          {/* Cooldown */}
          <div>
            <h4 className='font-medium'>{t('Cooldown')}</h4>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t(
                'When a channel fails consecutively, it enters a cooldown period during which its effective weight drops to 0. It recovers automatically once the cooldown expires.'
              )}
            </p>
          </div>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='channel_health_setting.cooldown_after'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Cooldown After (failures)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={1000}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!enabled}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Number of consecutive failures that trigger a cooldown'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='channel_health_setting.cooldown_duration_minutes'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Cooldown Duration (minutes)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={1440}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!enabled}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'How long a channel stays in cooldown before automatically recovering'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
