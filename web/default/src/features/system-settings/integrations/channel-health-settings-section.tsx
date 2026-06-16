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
import { useEffect, useMemo, useRef } from 'react'
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
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'
import {
  CHANNEL_HEALTH_SETTING_FIELDS,
  CHANNEL_HEALTH_SETTING_KEYS,
  type ChannelHealthFieldGroup,
  type ChannelHealthSettings,
} from './channel-health-settings'

const channelHealthSchema = z.object({
  channel_health_setting: z.object({
    enabled: z.boolean(),
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
  defaultValues: ChannelHealthSettings
}

function buildFormDefaults(
  defaults: ChannelHealthSettings
): ChannelHealthFormInput {
  return {
    channel_health_setting: {
      enabled: defaults['channel_health_setting.enabled'],
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
  }
}

function normalizeFormValues(
  values: ChannelHealthFormValues
): ChannelHealthSettings {
  const flattened = {
    'channel_health_setting.enabled': values.channel_health_setting.enabled,
    'channel_health_setting.warmup_enabled':
      values.channel_health_setting.warmup_enabled,
  } as Partial<ChannelHealthSettings>

  for (const field of CHANNEL_HEALTH_SETTING_FIELDS) {
    flattened[field.optionKey] = values.channel_health_setting[field.key]
  }

  return flattened as ChannelHealthSettings
}

export function ChannelHealthSettingsSection({
  defaultValues,
}: ChannelHealthSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const baselineRef = useRef<ChannelHealthSettings>(defaultValues)

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

  const onSubmit = async (values: ChannelHealthFormValues) => {
    const normalized = normalizeFormValues(values)
    const updates = CHANNEL_HEALTH_SETTING_KEYS.filter(
      (key) => normalized[key] !== baselineRef.current[key]
    )

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
  }

  return (
    <SettingsSection title={t('Channel Health Guard')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
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
                                {...safeNumberFieldProps(field)}
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
