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
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'

const numericString = z.string().refine((value) => {
  const trimmed = value.trim()
  if (!trimmed) return true
  return !Number.isNaN(Number(trimmed)) && Number(trimmed) >= 0
}, 'Enter a non-negative number or leave empty')

const monitoringSchema = z.object({
  QuotaRemindThreshold: numericString,
  channel_alert_setting: z.object({
    enabled: z.boolean(),
    balance_alert_enabled: z.boolean(),
    multiplier_change_enabled: z.boolean(),
    balance_threshold: z.coerce.number().min(0),
    min_interval_seconds: z.coerce.number().int().min(1),
    feishu_enabled: z.boolean(),
    feishu_webhook_url: z.string(),
    feishu_secret: z.string(),
    dingtalk_enabled: z.boolean(),
    dingtalk_webhook_url: z.string(),
    dingtalk_secret: z.string(),
  }),
  perf_metrics_setting: z.object({
    enabled: z.boolean(),
    flush_interval: z.coerce.number().min(1),
    bucket_time: z.enum(['minute', '5min', 'hour']),
    retention_days: z.coerce.number().min(0),
  }),
})

type MonitoringFormInput = z.input<typeof monitoringSchema>
type MonitoringFormValues = z.output<typeof monitoringSchema>

type FlatMonitoringDefaults = {
  QuotaRemindThreshold: string
  'channel_alert_setting.enabled': boolean
  'channel_alert_setting.balance_alert_enabled': boolean
  'channel_alert_setting.multiplier_change_enabled': boolean
  'channel_alert_setting.balance_threshold': number
  'channel_alert_setting.min_interval_seconds': number
  'channel_alert_setting.feishu_enabled': boolean
  'channel_alert_setting.feishu_webhook_url': string
  'channel_alert_setting.feishu_secret': string
  'channel_alert_setting.dingtalk_enabled': boolean
  'channel_alert_setting.dingtalk_webhook_url': string
  'channel_alert_setting.dingtalk_secret': string
  'perf_metrics_setting.enabled': boolean
  'perf_metrics_setting.flush_interval': number
  'perf_metrics_setting.bucket_time': 'minute' | '5min' | 'hour'
  'perf_metrics_setting.retention_days': number
}

type MonitoringSettingsSectionProps = {
  defaultValues: FlatMonitoringDefaults
}

const buildFormDefaults = (
  defaults: MonitoringSettingsSectionProps['defaultValues']
): MonitoringFormInput => ({
  QuotaRemindThreshold: defaults.QuotaRemindThreshold ?? '',
  channel_alert_setting: {
    enabled: defaults['channel_alert_setting.enabled'],
    balance_alert_enabled:
      defaults['channel_alert_setting.balance_alert_enabled'],
    multiplier_change_enabled:
      defaults['channel_alert_setting.multiplier_change_enabled'],
    balance_threshold: defaults['channel_alert_setting.balance_threshold'],
    min_interval_seconds:
      defaults['channel_alert_setting.min_interval_seconds'],
    feishu_enabled: defaults['channel_alert_setting.feishu_enabled'],
    feishu_webhook_url: defaults['channel_alert_setting.feishu_webhook_url'],
    feishu_secret: defaults['channel_alert_setting.feishu_secret'],
    dingtalk_enabled: defaults['channel_alert_setting.dingtalk_enabled'],
    dingtalk_webhook_url:
      defaults['channel_alert_setting.dingtalk_webhook_url'],
    dingtalk_secret: defaults['channel_alert_setting.dingtalk_secret'],
  },
  perf_metrics_setting: {
    enabled: defaults['perf_metrics_setting.enabled'],
    flush_interval: defaults['perf_metrics_setting.flush_interval'],
    bucket_time: defaults['perf_metrics_setting.bucket_time'],
    retention_days: defaults['perf_metrics_setting.retention_days'],
  },
})

const normalizeDefaults = (
  defaults: MonitoringSettingsSectionProps['defaultValues']
): FlatMonitoringDefaults => ({
  QuotaRemindThreshold: (defaults.QuotaRemindThreshold ?? '').trim(),
  'channel_alert_setting.enabled':
    defaults['channel_alert_setting.enabled'] ?? false,
  'channel_alert_setting.balance_alert_enabled':
    defaults['channel_alert_setting.balance_alert_enabled'] ?? true,
  'channel_alert_setting.multiplier_change_enabled':
    defaults['channel_alert_setting.multiplier_change_enabled'] ?? true,
  'channel_alert_setting.balance_threshold':
    defaults['channel_alert_setting.balance_threshold'] ?? 0,
  'channel_alert_setting.min_interval_seconds':
    defaults['channel_alert_setting.min_interval_seconds'] ?? 300,
  'channel_alert_setting.feishu_enabled':
    defaults['channel_alert_setting.feishu_enabled'] ?? false,
  'channel_alert_setting.feishu_webhook_url': (
    defaults['channel_alert_setting.feishu_webhook_url'] ?? ''
  ).trim(),
  'channel_alert_setting.feishu_secret': (
    defaults['channel_alert_setting.feishu_secret'] ?? ''
  ).trim(),
  'channel_alert_setting.dingtalk_enabled':
    defaults['channel_alert_setting.dingtalk_enabled'] ?? false,
  'channel_alert_setting.dingtalk_webhook_url': (
    defaults['channel_alert_setting.dingtalk_webhook_url'] ?? ''
  ).trim(),
  'channel_alert_setting.dingtalk_secret': (
    defaults['channel_alert_setting.dingtalk_secret'] ?? ''
  ).trim(),
  'perf_metrics_setting.enabled': defaults['perf_metrics_setting.enabled'],
  'perf_metrics_setting.flush_interval':
    defaults['perf_metrics_setting.flush_interval'],
  'perf_metrics_setting.bucket_time': defaults['perf_metrics_setting.bucket_time'],
  'perf_metrics_setting.retention_days':
    defaults['perf_metrics_setting.retention_days'],
})

const normalizeFormValues = (
  values: MonitoringFormValues
): FlatMonitoringDefaults => ({
  QuotaRemindThreshold: values.QuotaRemindThreshold.trim(),
  'channel_alert_setting.enabled': values.channel_alert_setting.enabled,
  'channel_alert_setting.balance_alert_enabled':
    values.channel_alert_setting.balance_alert_enabled,
  'channel_alert_setting.multiplier_change_enabled':
    values.channel_alert_setting.multiplier_change_enabled,
  'channel_alert_setting.balance_threshold':
    values.channel_alert_setting.balance_threshold,
  'channel_alert_setting.min_interval_seconds':
    values.channel_alert_setting.min_interval_seconds,
  'channel_alert_setting.feishu_enabled':
    values.channel_alert_setting.feishu_enabled,
  'channel_alert_setting.feishu_webhook_url':
    values.channel_alert_setting.feishu_webhook_url.trim(),
  'channel_alert_setting.feishu_secret':
    values.channel_alert_setting.feishu_secret.trim(),
  'channel_alert_setting.dingtalk_enabled':
    values.channel_alert_setting.dingtalk_enabled,
  'channel_alert_setting.dingtalk_webhook_url':
    values.channel_alert_setting.dingtalk_webhook_url.trim(),
  'channel_alert_setting.dingtalk_secret':
    values.channel_alert_setting.dingtalk_secret.trim(),
  'perf_metrics_setting.enabled': values.perf_metrics_setting.enabled,
  'perf_metrics_setting.flush_interval':
    values.perf_metrics_setting.flush_interval,
  'perf_metrics_setting.bucket_time': values.perf_metrics_setting.bucket_time,
  'perf_metrics_setting.retention_days':
    values.perf_metrics_setting.retention_days,
})

export function MonitoringSettingsSection({
  defaultValues,
}: MonitoringSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const baselineRef = useRef<FlatMonitoringDefaults>(
    normalizeDefaults(defaultValues)
  )
  const baselineSerializedRef = useRef<string>(
    JSON.stringify(normalizeDefaults(defaultValues))
  )

  const formDefaults = useMemo(
    () => buildFormDefaults(defaultValues),
    [defaultValues]
  )

  const form = useForm<MonitoringFormInput, unknown, MonitoringFormValues>({
    resolver: zodResolver(monitoringSchema),
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  useEffect(() => {
    const normalized = normalizeDefaults(defaultValues)
    const serialized = JSON.stringify(normalized)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = normalized
    baselineSerializedRef.current = serialized
  }, [defaultValues])

  const channelAlertsEnabled = form.watch('channel_alert_setting.enabled')
  const balanceAlertsEnabled = form.watch(
    'channel_alert_setting.balance_alert_enabled'
  )
  const feishuAlertsEnabled = form.watch(
    'channel_alert_setting.feishu_enabled'
  )
  const dingTalkAlertsEnabled = form.watch(
    'channel_alert_setting.dingtalk_enabled'
  )
  const perfMetricsEnabled = form.watch('perf_metrics_setting.enabled')

  const onSubmit = async (values: MonitoringFormValues) => {
    const normalized = normalizeFormValues(values)
    const updates = (
      Object.keys(normalized) as Array<keyof FlatMonitoringDefaults>
    ).filter((key) => normalized[key] !== baselineRef.current[key])

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
    baselineSerializedRef.current = JSON.stringify(normalized)
  }

  return (
    <SettingsSection title={t('Monitoring & Alerts')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />
          <FormField
            control={form.control}
            name='QuotaRemindThreshold'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Quota reminder (tokens)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    step={1}
                    value={field.value}
                    onChange={(event) => field.onChange(event.target.value)}
                  />
                </FormControl>
                <FormDescription>
                  {t('Send email alerts when a user falls below this quota')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div>
            <h4 className='font-medium'>
              {t('Channel balance and multiplier alerts')}
            </h4>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t(
                'Send robot alerts when an upstream balance crosses the threshold or a monitored upstream multiplier changes.'
              )}
            </p>
          </div>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='channel_alert_setting.enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable channel alerts')}</FormLabel>
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
              name='channel_alert_setting.balance_alert_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Balance threshold alerts')}</FormLabel>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={!channelAlertsEnabled}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <FormField
              control={form.control}
              name='channel_alert_setting.multiplier_change_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Multiplier change alerts')}</FormLabel>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={!channelAlertsEnabled}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
          </div>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='channel_alert_setting.balance_threshold'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Channel balance threshold')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step='0.01'
                      {...safeNumberFieldProps(field)}
                      disabled={!channelAlertsEnabled || !balanceAlertsEnabled}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Alert once when a refreshed channel balance falls below this value.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='channel_alert_setting.min_interval_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Alert cooldown (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!channelAlertsEnabled}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Suppress duplicate alerts for the same channel event.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='channel_alert_setting.feishu_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Feishu alerts')}</FormLabel>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={!channelAlertsEnabled}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <FormField
              control={form.control}
              name='channel_alert_setting.dingtalk_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('DingTalk alerts')}</FormLabel>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={!channelAlertsEnabled}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
          </div>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='channel_alert_setting.feishu_webhook_url'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Feishu webhook URL')}</FormLabel>
                  <FormControl>
                    <Input
                      value={field.value}
                      onChange={field.onChange}
                      disabled={!channelAlertsEnabled || !feishuAlertsEnabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='channel_alert_setting.feishu_secret'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Feishu signing secret')}</FormLabel>
                  <FormControl>
                    <Input
                      type='password'
                      value={field.value}
                      onChange={field.onChange}
                      disabled={!channelAlertsEnabled || !feishuAlertsEnabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='channel_alert_setting.dingtalk_webhook_url'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('DingTalk webhook URL')}</FormLabel>
                  <FormControl>
                    <Input
                      value={field.value}
                      onChange={field.onChange}
                      disabled={!channelAlertsEnabled || !dingTalkAlertsEnabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='channel_alert_setting.dingtalk_secret'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('DingTalk signing secret')}</FormLabel>
                  <FormControl>
                    <Input
                      type='password'
                      value={field.value}
                      onChange={field.onChange}
                      disabled={!channelAlertsEnabled || !dingTalkAlertsEnabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div>
            <h4 className='font-medium'>{t('Model performance metrics')}</h4>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t(
                'Collect relay latency and success-rate metrics for the model square.'
              )}
            </p>
          </div>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-4'>
            <FormField
              control={form.control}
              name='perf_metrics_setting.enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>
                      {t('Enable model performance metrics')}
                    </FormLabel>
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
              name='perf_metrics_setting.flush_interval'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Flush interval (minutes)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!perfMetricsEnabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='perf_metrics_setting.bucket_time'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Aggregation bucket')}</FormLabel>
                  <Select
                    items={[
                      { value: 'minute', label: t('1 minute') },
                      { value: '5min', label: t('5 minutes') },
                      { value: 'hour', label: t('1 hour') },
                    ]}
                    value={field.value}
                    onValueChange={field.onChange}
                    disabled={!perfMetricsEnabled}
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        <SelectItem value='minute'>{t('1 minute')}</SelectItem>
                        <SelectItem value='5min'>{t('5 minutes')}</SelectItem>
                        <SelectItem value='hour'>{t('1 hour')}</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='perf_metrics_setting.retention_days'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Retention days')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!perfMetricsEnabled}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('0 means data is kept permanently')}
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
