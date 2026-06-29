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
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type FormEvent,
} from 'react'
import { RefreshCw } from 'lucide-react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { applyChannelAutoPriority } from '../api'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'
import {
  CHANNEL_AUTO_PRIORITY_SETTING_KEYS,
  applyChannelHealthPreset,
  CHANNEL_HEALTH_PRESETS,
  CHANNEL_HEALTH_SETTING_FIELDS,
  CHANNEL_HEALTH_SETTING_KEYS,
  CHANNEL_MULTIPLIER_MONITOR_SETTING_KEY,
  markChannelHealthPresetCustom,
  type ChannelHealthPreset,
  type ChannelHealthFieldGroup,
  type ChannelHealthPanelSettings,
} from './channel-health-settings'

const channelHealthSchema = z.object({
  channel_health_setting: z.object({
    enabled: z.boolean(),
    preset: z.enum(CHANNEL_HEALTH_PRESETS),
    model_level_enabled: z.boolean(),
    events_enabled: z.boolean(),
    alert_min_interval_seconds: z.coerce.number().int().min(1),
    window_seconds: z.coerce.number().int().min(1),
    min_samples: z.coerce.number().int().min(1),
    min_failures: z.coerce.number().int().min(1),
    error_rate_threshold: z.coerce.number().min(0).max(1),
    consecutive_failure_threshold: z.coerce.number().int().min(1),
    first_response_timeout_seconds: z.coerce.number().int().min(1),
    stuck_inflight_threshold: z.coerce.number().int().min(1),
    single_stuck_timeout_seconds: z.coerce.number().int().min(1),
    probe_interval_seconds: z.coerce.number().int().min(1),
    probe_timeout_seconds: z.coerce.number().int().min(1),
    probe_successes_to_recover: z.coerce.number().int().min(1),
    probe_backoff_max_seconds: z.coerce.number().int().min(1),
    warmup_enabled: z.boolean(),
    warmup_duration_seconds: z.coerce.number().int().min(1),
    warmup_start_percent: z.coerce.number().int().min(1).max(100),
    warmup_step_percent: z.coerce.number().int().min(1).max(100),
  }),
  channel_multiplier_monitor_setting: z.object({
    interval_minutes: z.coerce.number().int().min(1),
  }),
  channel_auto_priority_setting: z
    .object({
      enabled: z.boolean(),
      min_weight: z.coerce.number().int().min(1),
      max_weight: z.coerce.number().int().min(1),
      latency_guard_enabled: z.boolean(),
      latency_threshold_seconds: z.coerce.number().int().min(1),
      latency_window_minutes: z.coerce.number().int().min(1),
      latency_min_samples: z.coerce.number().int().min(1),
      latency_slow_ratio_threshold: z.coerce.number().min(0).max(1),
      latency_recovery_ratio_threshold: z.coerce.number().min(0).max(1),
      latency_retained_weight_percent: z.coerce.number().int().min(1).max(100),
      latency_priority_penalty: z.coerce.number().int().min(0),
    })
    .refine((value) => value.max_weight >= value.min_weight, {
      path: ['max_weight'],
      message:
        'Maximum adaptive weight must be greater than or equal to minimum adaptive weight',
    })
    .refine(
      (value) =>
        value.latency_recovery_ratio_threshold <=
        value.latency_slow_ratio_threshold,
      {
        path: ['latency_recovery_ratio_threshold'],
        message:
          'Recovery slow-first-response ratio must be less than or equal to trigger ratio',
      }
    ),
})

type ChannelHealthFormValues = z.output<typeof channelHealthSchema>
type ChannelHealthFormInput = z.input<typeof channelHealthSchema>

const FIELD_GROUPS: Array<{
  id: ChannelHealthFieldGroup
  titleKey: string
}> = [
  { id: 'errors', titleKey: 'Error circuit breaker' },
  { id: 'stuck', titleKey: 'Stuck request circuit breaker' },
  { id: 'probe', titleKey: 'Recovery probing' },
  { id: 'warmup', titleKey: 'Recovery warm-up' },
]

type ChannelHealthSettingsSectionProps = {
  defaultValues: ChannelHealthPanelSettings
}

function buildFormDefaults(
  defaults: ChannelHealthPanelSettings
): ChannelHealthFormInput {
  return {
    channel_health_setting: {
      enabled: defaults['channel_health_setting.enabled'],
      preset: defaults['channel_health_setting.preset'],
      model_level_enabled:
        defaults['channel_health_setting.model_level_enabled'],
      events_enabled: defaults['channel_health_setting.events_enabled'],
      alert_min_interval_seconds:
        defaults['channel_health_setting.alert_min_interval_seconds'],
      window_seconds: defaults['channel_health_setting.window_seconds'],
      min_samples: defaults['channel_health_setting.min_samples'],
      min_failures: defaults['channel_health_setting.min_failures'],
      error_rate_threshold:
        defaults['channel_health_setting.error_rate_threshold'],
      consecutive_failure_threshold:
        defaults['channel_health_setting.consecutive_failure_threshold'],
      first_response_timeout_seconds:
        defaults['channel_health_setting.first_response_timeout_seconds'],
      stuck_inflight_threshold:
        defaults['channel_health_setting.stuck_inflight_threshold'],
      single_stuck_timeout_seconds:
        defaults['channel_health_setting.single_stuck_timeout_seconds'],
      probe_interval_seconds:
        defaults['channel_health_setting.probe_interval_seconds'],
      probe_timeout_seconds:
        defaults['channel_health_setting.probe_timeout_seconds'],
      probe_successes_to_recover:
        defaults['channel_health_setting.probe_successes_to_recover'],
      probe_backoff_max_seconds:
        defaults['channel_health_setting.probe_backoff_max_seconds'],
      warmup_enabled: defaults['channel_health_setting.warmup_enabled'],
      warmup_duration_seconds:
        defaults['channel_health_setting.warmup_duration_seconds'],
      warmup_start_percent:
        defaults['channel_health_setting.warmup_start_percent'],
      warmup_step_percent:
        defaults['channel_health_setting.warmup_step_percent'],
    },
    channel_multiplier_monitor_setting: {
      interval_minutes: defaults[CHANNEL_MULTIPLIER_MONITOR_SETTING_KEY],
    },
    channel_auto_priority_setting: {
      enabled: defaults['channel_auto_priority_setting.enabled'],
      min_weight: defaults['channel_auto_priority_setting.min_weight'],
      max_weight: defaults['channel_auto_priority_setting.max_weight'],
      latency_guard_enabled:
        defaults['channel_auto_priority_setting.latency_guard_enabled'],
      latency_threshold_seconds:
        defaults['channel_auto_priority_setting.latency_threshold_seconds'],
      latency_window_minutes:
        defaults['channel_auto_priority_setting.latency_window_minutes'],
      latency_min_samples:
        defaults['channel_auto_priority_setting.latency_min_samples'],
      latency_slow_ratio_threshold:
        defaults['channel_auto_priority_setting.latency_slow_ratio_threshold'],
      latency_recovery_ratio_threshold:
        defaults[
          'channel_auto_priority_setting.latency_recovery_ratio_threshold'
        ],
      latency_retained_weight_percent:
        defaults['channel_auto_priority_setting.latency_retained_weight_percent'],
      latency_priority_penalty:
        defaults['channel_auto_priority_setting.latency_priority_penalty'],
    },
  }
}

function normalizeFormValues(
  values: ChannelHealthFormValues
): ChannelHealthPanelSettings {
  const flattened = {
    'channel_health_setting.enabled': values.channel_health_setting.enabled,
    'channel_health_setting.preset': values.channel_health_setting.preset,
    'channel_health_setting.model_level_enabled':
      values.channel_health_setting.model_level_enabled,
    'channel_health_setting.events_enabled':
      values.channel_health_setting.events_enabled,
    'channel_health_setting.alert_min_interval_seconds':
      values.channel_health_setting.alert_min_interval_seconds,
    'channel_health_setting.warmup_enabled':
      values.channel_health_setting.warmup_enabled,
    [CHANNEL_MULTIPLIER_MONITOR_SETTING_KEY]:
      values.channel_multiplier_monitor_setting.interval_minutes,
    'channel_auto_priority_setting.enabled':
      values.channel_auto_priority_setting.enabled,
    'channel_auto_priority_setting.min_weight':
      values.channel_auto_priority_setting.min_weight,
    'channel_auto_priority_setting.max_weight':
      values.channel_auto_priority_setting.max_weight,
    'channel_auto_priority_setting.latency_guard_enabled':
      values.channel_auto_priority_setting.latency_guard_enabled,
    'channel_auto_priority_setting.latency_threshold_seconds':
      values.channel_auto_priority_setting.latency_threshold_seconds,
    'channel_auto_priority_setting.latency_window_minutes':
      values.channel_auto_priority_setting.latency_window_minutes,
    'channel_auto_priority_setting.latency_min_samples':
      values.channel_auto_priority_setting.latency_min_samples,
    'channel_auto_priority_setting.latency_slow_ratio_threshold':
      values.channel_auto_priority_setting.latency_slow_ratio_threshold,
    'channel_auto_priority_setting.latency_recovery_ratio_threshold':
      values.channel_auto_priority_setting.latency_recovery_ratio_threshold,
    'channel_auto_priority_setting.latency_retained_weight_percent':
      values.channel_auto_priority_setting.latency_retained_weight_percent,
    'channel_auto_priority_setting.latency_priority_penalty':
      values.channel_auto_priority_setting.latency_priority_penalty,
  } as Partial<ChannelHealthPanelSettings>

  for (const field of CHANNEL_HEALTH_SETTING_FIELDS) {
    flattened[field.optionKey] = values.channel_health_setting[field.key]
  }

  return flattened as ChannelHealthPanelSettings
}

function formInputToSettings(
  values: ChannelHealthFormInput
): ChannelHealthPanelSettings {
  return normalizeFormValues(channelHealthSchema.parse(values))
}

export function ChannelHealthSettingsSection({
  defaultValues,
}: ChannelHealthSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [isApplyingAutoPriority, setIsApplyingAutoPriority] = useState(false)
  const baselineRef = useRef<ChannelHealthPanelSettings>(defaultValues)

  const formDefaults = useMemo(
    () => buildFormDefaults(defaultValues),
    [defaultValues]
  )

  const form = useForm<
    ChannelHealthFormInput,
    unknown,
    ChannelHealthFormValues
  >({
    resolver: zodResolver(channelHealthSchema),
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  useEffect(() => {
    baselineRef.current = defaultValues
  }, [defaultValues])

  const applyPresetToForm = (preset: ChannelHealthPreset) => {
    const current = formInputToSettings(form.getValues())
    const next = applyChannelHealthPreset(current, preset)
    form.reset(buildFormDefaults(next), { keepDirty: true })
  }

  const markPresetCustom = () => {
    const current = formInputToSettings(form.getValues())
    const next = markChannelHealthPresetCustom(current)
    if (
      next['channel_health_setting.preset'] !==
      current['channel_health_setting.preset']
    ) {
      form.setValue('channel_health_setting.preset', 'custom', {
        shouldDirty: true,
        shouldValidate: true,
      })
    }
  }

  const onSubmit = useCallback(
    async (values: ChannelHealthFormValues) => {
      const normalized = normalizeFormValues(values)
      const updates = [
        ...CHANNEL_HEALTH_SETTING_KEYS,
        CHANNEL_MULTIPLIER_MONITOR_SETTING_KEY,
        ...CHANNEL_AUTO_PRIORITY_SETTING_KEYS,
      ].filter((key) => normalized[key] !== baselineRef.current[key])

      if (updates.length === 0) {
        toast.info(t('No changes to save'))
        return
      }

      for (const key of updates) {
        await updateOption.mutateAsync({
          key,
          value: normalized[key],
        })
      }

      baselineRef.current = normalized
    },
    [t, updateOption]
  )

  const handleFormSubmit = useCallback(
    (event: FormEvent<HTMLFormElement>) => {
      void form.handleSubmit(onSubmit)(event)
    },
    [form, onSubmit]
  )

  const handleSave = useCallback(() => {
    void form.handleSubmit(onSubmit)()
  }, [form, onSubmit])

  const handleApplyAutoPriority = useCallback(async () => {
    setIsApplyingAutoPriority(true)
    try {
      const response = await applyChannelAutoPriority()
      if (response.success) {
        toast.success(
          t('Auto priority applied: {{updated}} updated, {{skipped}} skipped', {
            updated: response.data?.updated_channels ?? 0,
            skipped: response.data?.skipped_channels ?? 0,
          })
        )
      } else {
        toast.error(response.message || t('Auto priority apply failed'))
      }
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('Auto priority apply failed')
      )
    } finally {
      setIsApplyingAutoPriority(false)
    }
  }, [t])

  return (
    <SettingsSection title={t('Channel Health Guard')}>
      <Form {...form}>
        <SettingsForm onSubmit={handleFormSubmit}>
          <SettingsPageFormActions
            onSave={handleSave}
            isSaving={updateOption.isPending}
            saveLabel='Save channel health settings'
          />

          <FormField
            control={form.control}
            name='channel_health_setting.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Runtime health guard')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Temporarily isolates unhealthy channels without changing manual channel status.'
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

          <FormField
            control={form.control}
            name='channel_health_setting.preset'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Health threshold preset')}</FormLabel>
                <Select
                  value={field.value}
                  onValueChange={(value) => {
                    if (value) applyPresetToForm(value as ChannelHealthPreset)
                  }}
                >
                  <FormControl>
                    <SelectTrigger className='w-full'>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectItem value='conservative'>
                        {t('Conservative')}
                      </SelectItem>
                      <SelectItem value='balanced'>{t('Balanced')}</SelectItem>
                      <SelectItem value='aggressive'>
                        {t('Aggressive')}
                      </SelectItem>
                      <SelectItem value='custom'>{t('Custom')}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormDescription>
                  {t(
                    'Presets fill all numeric thresholds. Editing any numeric value switches the preset to custom.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='channel_health_setting.model_level_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Model-level isolation')}</FormLabel>
                  <FormDescription>
                    {t(
                      'When enabled, health isolation applies to a channel and model pair instead of the whole channel.'
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

          <FormField
            control={form.control}
            name='channel_health_setting.events_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Health events and alerts')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Record runtime isolation, recovery, and probe failure events for operators.'
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

          <FormField
            control={form.control}
            name='channel_health_setting.warmup_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Recovery warm-up')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Gradually restores traffic after probes confirm a channel recovered.'
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

          <div data-settings-form-span='full' className='space-y-7'>
            <section className='space-y-4'>
              <h3 className='text-sm font-medium'>
                {t('Upstream key multiplier monitor')}
              </h3>
              <div className='grid gap-6 md:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='channel_multiplier_monitor_setting.interval_minutes'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Multiplier probe interval (minutes)')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          step={1}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'How often the background worker logs in to upstream consoles to refresh key multipliers.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
            </section>

            <section className='space-y-4'>
              <h3 className='text-sm font-medium'>
                {t('Automatic channel priority')}
              </h3>
              <FormField
                control={form.control}
                name='channel_auto_priority_setting.enabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>
                        {t('Sort channels by upstream multiplier')}
                      </FormLabel>
                      <FormDescription>
                        {t(
                          'Lower multipliers get higher priority. Channels with the same multiplier receive adaptive weights from runtime health.'
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
              <div className='grid gap-6 md:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='channel_auto_priority_setting.min_weight'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Minimum adaptive weight')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          step={1}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Lowest weight assigned to eligible channels.')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='channel_auto_priority_setting.max_weight'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Maximum adaptive weight')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          step={1}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Highest weight assigned to stable channels.')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
              <FormField
                control={form.control}
                name='channel_auto_priority_setting.latency_guard_enabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Slow first response guard')}</FormLabel>
                      <FormDescription>
                        {t(
                          'Reduce priority and retain a small traffic share when recent first responses are too slow.'
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
              <div className='grid gap-6 md:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='channel_auto_priority_setting.latency_threshold_seconds'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Slow first response threshold (seconds)')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          step={1}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Requests above this first response time count as slow.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='channel_auto_priority_setting.latency_window_minutes'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Latency window (minutes)')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          step={1}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Recent log window used to calculate slow first responses.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='channel_auto_priority_setting.latency_min_samples'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Latency minimum samples')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          step={1}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Minimum valid first response samples before applying latency protection.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='channel_auto_priority_setting.latency_slow_ratio_threshold'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Slow ratio trigger')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          max={1}
                          step={0.01}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Slow first response ratio from 0 to 1 required to reduce traffic.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='channel_auto_priority_setting.latency_recovery_ratio_threshold'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Slow ratio recovery')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          max={1}
                          step={0.01}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Degraded channels recover when the slow ratio falls to this value.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='channel_auto_priority_setting.latency_retained_weight_percent'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Retained traffic weight percent')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          max={100}
                          step={1}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Weight percentage kept for slow channels during latency protection.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='channel_auto_priority_setting.latency_priority_penalty'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Latency priority penalty')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          step={1}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Priority levels removed from slow channels while protection is active.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
              <Button
                type='button'
                variant='outline'
                disabled={isApplyingAutoPriority}
                onClick={handleApplyAutoPriority}
              >
                <RefreshCw
                  className={isApplyingAutoPriority ? 'animate-spin' : ''}
                  aria-hidden='true'
                />
                {isApplyingAutoPriority
                  ? t('Applying...')
                  : t('Apply auto priority now')}
              </Button>
            </section>

            <section className='space-y-4'>
              <h3 className='text-sm font-medium'>{t('Health alerts')}</h3>
              <div className='grid gap-6 md:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='channel_health_setting.alert_min_interval_seconds'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Alert minimum interval (seconds)')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          step={1}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Minimum time between repeated alerts for the same channel health event.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
            </section>

            {FIELD_GROUPS.map((group) => {
              const fields = CHANNEL_HEALTH_SETTING_FIELDS.filter(
                (field) => field.group === group.id
              )

              return (
                <section key={group.id} className='space-y-4'>
                  <h3 className='text-sm font-medium'>{t(group.titleKey)}</h3>
                  <div className='grid gap-6 md:grid-cols-2'>
                    {fields.map((fieldConfig) => (
                      <FormField
                        key={fieldConfig.optionKey}
                        control={form.control}
                        name={`channel_health_setting.${fieldConfig.key}`}
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel>{t(fieldConfig.labelKey)}</FormLabel>
                            <FormControl>
                              <Input
                                type='number'
                                min={fieldConfig.min}
                                max={
                                  'max' in fieldConfig
                                    ? fieldConfig.max
                                    : undefined
                                }
                                step={fieldConfig.step}
                                {...(() => {
                                  const props = safeNumberFieldProps(field)
                                  return {
                                    ...props,
                                    onChange: (
                                      event: ChangeEvent<HTMLInputElement>
                                    ) => {
                                      props.onChange(event)
                                      markPresetCustom()
                                    },
                                  }
                                })()}
                              />
                            </FormControl>
                            <FormDescription>
                              {t(fieldConfig.descriptionKey)}
                            </FormDescription>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                    ))}
                  </div>
                </section>
              )
            })}
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
