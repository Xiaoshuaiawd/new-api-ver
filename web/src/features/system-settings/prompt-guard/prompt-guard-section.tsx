/*
Copyright (C) 2023-2026 QuantumNous
This program is free software: GNU AGPL v3+.
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2, Zap } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import { safeNumberFieldProps } from '../utils/numeric-field'
import {
  type PromptGuardConfig,
  type UpdatePromptGuardEndpoint,
  getPromptGuardConfig,
  probePromptGuardEndpoint,
  updatePromptGuardConfig,
} from './api'
import { useForm } from 'react-hook-form'

const ALL_SCANNERS = [
  { id: 'jailbreak', label: 'Jailbreak' },
  { id: 'violent', label: 'Violent' },
  { id: 'non_violent_illegal_acts', label: 'Non-violent Illegal Acts' },
  { id: 'sexual_content_or_sexual_acts', label: 'Sexual Content or Sexual Acts' },
  { id: 'pii', label: 'PII' },
  { id: 'suicide_and_self_harm', label: 'Suicide & Self-Harm' },
  { id: 'unethical_acts', label: 'Unethical Acts' },
  { id: 'politically_sensitive_topics', label: 'Politically Sensitive Topics' },
  { id: 'copyright_violation', label: 'Copyright Violation' },
]

type FormEndpoint = UpdatePromptGuardEndpoint & { newToken?: string }

type FormValues = {
  enabled: boolean
  blocking_enabled: boolean
  latest_turn_only: boolean
  store_pass_events: boolean
  scanners: string[]
  all_groups: boolean
  group_names_raw: string
  endpoints: FormEndpoint[]
}

function buildFormValues(cfg: PromptGuardConfig): FormValues {
  return {
    enabled: cfg.enabled,
    blocking_enabled: cfg.blocking_enabled,
    latest_turn_only: cfg.latest_turn_only,
    store_pass_events: cfg.store_pass_events,
    scanners: cfg.scanners ?? ALL_SCANNERS.map((s) => s.id),
    all_groups: cfg.all_groups,
    group_names_raw: (cfg.group_names ?? []).join(','),
    endpoints: (cfg.endpoints ?? []).map((ep) => ({
      id: ep.id,
      name: ep.name,
      base_url: ep.base_url,
      model: ep.model,
      format: ep.format || 'qwen3guard',
      timeout_ms: ep.timeout_ms || 4000,
      input_limit: ep.input_limit || 16000,
      enabled: ep.enabled,
      newToken: '',
      clear_token: false,
    })),
  }
}

export function PromptGuardSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [probing, setProbing] = useState<Record<number, boolean>>({})

  const { data: cfg, isLoading } = useQuery({
    queryKey: ['prompt-guard-config'],
    queryFn: getPromptGuardConfig,
  })

  const saveMutation = useMutation({
    mutationFn: updatePromptGuardConfig,
    onSuccess: () => {
      toast.success(t('Prompt Guard settings saved'))
      queryClient.invalidateQueries({ queryKey: ['prompt-guard-config'] })
    },
    onError: (e: Error) => toast.error(e.message),
  })

  const form = useForm<FormValues>({
    values: cfg ? buildFormValues(cfg) : buildFormValues({
      enabled: false, blocking_enabled: false, latest_turn_only: true,
      store_pass_events: false, scanners: ALL_SCANNERS.map((s) => s.id),
      all_groups: false, group_names: [], endpoints: [], config_version: 1,
    }),
  })

  const enabled = form.watch('enabled')
  const allGroups = form.watch('all_groups')
  const endpoints = form.watch('endpoints')
  const scanners = form.watch('scanners')

  const onSubmit = (values: FormValues) => {
    const groupNames = values.group_names_raw
      .split(',')
      .map((g) => g.trim())
      .filter(Boolean)

    saveMutation.mutate({
      expected_version: cfg?.config_version ?? 1,
      enabled: values.enabled,
      blocking_enabled: values.blocking_enabled,
      latest_turn_only: values.latest_turn_only,
      store_pass_events: values.store_pass_events,
      scanners: values.scanners,
      all_groups: values.all_groups,
      group_names: groupNames,
      endpoints: values.endpoints.map((ep) => ({
        id: ep.id,
        name: ep.name,
        base_url: ep.base_url,
        model: ep.model,
        format: ep.format,
        token: ep.newToken || undefined,
        clear_token: ep.clear_token,
        timeout_ms: ep.timeout_ms,
        input_limit: ep.input_limit,
        enabled: ep.enabled,
      })),
    })
  }

  const addEndpoint = () => {
    const current = form.getValues('endpoints')
    form.setValue('endpoints', [
      ...current,
      {
        id: `ep-${Date.now()}`,
        name: '',
        base_url: '',
        model: '',
        format: 'qwen3guard',
        timeout_ms: 4000,
        input_limit: 16000,
        enabled: true,
        newToken: '',
        clear_token: false,
      },
    ])
  }

  const removeEndpoint = (idx: number) => {
    const current = form.getValues('endpoints')
    form.setValue('endpoints', current.filter((_, i) => i !== idx))
  }

  const probe = async (idx: number) => {
    const ep = form.getValues(`endpoints.${idx}`)
    setProbing((p) => ({ ...p, [idx]: true }))
    try {
      const result = await probePromptGuardEndpoint({
        base_url: ep.base_url,
        model: ep.model,
        format: ep.format,
        token: ep.newToken || undefined,
        timeout_ms: ep.timeout_ms,
      })
      toast.success(t('Probe succeeded: decision={{decision}}, {{ms}}ms', { decision: result.decision, ms: result.latency_ms }))
    } catch (e: any) {
      toast.error(t('Probe failed: {{msg}}', { msg: e.message }))
    } finally {
      setProbing((p) => ({ ...p, [idx]: false }))
    }
  }

  const toggleScanner = (id: string) => {
    const current = form.getValues('scanners')
    if (current.includes(id)) {
      form.setValue('scanners', current.filter((s) => s !== id))
    } else {
      form.setValue('scanners', [...current, id])
    }
  }

  if (isLoading) return null

  return (
    <SettingsSection title={t('Prompt Guard')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={saveMutation.isPending}
          />

          {/* Master switch */}
          <FormField control={form.control} name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable Prompt Guard')}</FormLabel>
                  <FormDescription>{t('Run safety classification before dispatching to upstream channels. Defaults to off.')}</FormDescription>
                </SettingsSwitchContent>
                <FormControl><Switch checked={field.value} onCheckedChange={field.onChange} /></FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField control={form.control} name='blocking_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Synchronous Blocking')}</FormLabel>
                  <FormDescription>{t('Block requests synchronously (403). When off, only logs flagged requests.')}</FormDescription>
                </SettingsSwitchContent>
                <FormControl><Switch checked={field.value} onCheckedChange={field.onChange} disabled={!enabled} /></FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField control={form.control} name='latest_turn_only'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Latest Turn Only')}</FormLabel>
                  <FormDescription>{t('Only send the latest user message (+ previous assistant reply) to the guard — not the full history.')}</FormDescription>
                </SettingsSwitchContent>
                <FormControl><Switch checked={field.value} onCheckedChange={field.onChange} disabled={!enabled} /></FormControl>
              </SettingsSwitchItem>
            )}
          />

          <Separator />

          {/* Risk categories */}
          <div>
            <h4 className='font-medium'>{t('Risk Categories')}</h4>
            <p className='text-muted-foreground mt-1 text-xs'>{t('Unsafe requests matching any enabled category will be blocked.')}</p>
          </div>
          <div className='grid grid-cols-2 gap-2 md:grid-cols-3'>
            {ALL_SCANNERS.map((s) => (
              <label key={s.id} className='flex items-center gap-2 cursor-pointer'>
                <Checkbox
                  checked={scanners.includes(s.id)}
                  onCheckedChange={() => toggleScanner(s.id)}
                  disabled={!enabled}
                />
                <span className='text-sm'>{s.label}</span>
              </label>
            ))}
          </div>

          <Separator />

          {/* Group scope */}
          <div>
            <h4 className='font-medium'>{t('Scope')}</h4>
            <p className='text-muted-foreground mt-1 text-xs'>{t('Select which token groups are covered by Prompt Guard.')}</p>
          </div>
          <FormField control={form.control} name='all_groups'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('All Groups')}</FormLabel>
                </SettingsSwitchContent>
                <FormControl><Switch checked={field.value} onCheckedChange={field.onChange} disabled={!enabled} /></FormControl>
              </SettingsSwitchItem>
            )}
          />
          {!allGroups && (
            <FormField control={form.control} name='group_names_raw'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Group Names (comma-separated)')}</FormLabel>
                  <FormControl><Input placeholder='default,premium' {...field} disabled={!enabled} /></FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          )}

          <Separator />

          {/* Guard nodes */}
          <div className='flex items-center justify-between'>
            <div>
              <h4 className='font-medium'>{t('Guard Nodes')}</h4>
              <p className='text-muted-foreground mt-1 text-xs'>{t('OpenAI-compatible endpoints that run the safety classifier. Tried in order on failure.')}</p>
            </div>
            <Button type='button' variant='outline' size='sm' onClick={addEndpoint} disabled={!enabled}>
              <Plus className='mr-1 h-4 w-4' />{t('Add Node')}
            </Button>
          </div>

          {endpoints.map((_, idx) => (
            <div key={idx} className='rounded-lg border p-4 space-y-3'>
              <div className='flex items-center justify-between'>
                <span className='text-sm font-medium'>{t('Node {{n}}', { n: idx + 1 })}</span>
                <div className='flex gap-2'>
                  <Button type='button' variant='outline' size='sm' onClick={() => probe(idx)} disabled={probing[idx]}>
                    <Zap className='mr-1 h-4 w-4' />{probing[idx] ? t('Testing…') : t('Test')}
                  </Button>
                  <Button type='button' variant='ghost' size='sm' onClick={() => removeEndpoint(idx)}>
                    <Trash2 className='h-4 w-4 text-destructive' />
                  </Button>
                </div>
              </div>
              <div className='grid grid-cols-1 gap-3 md:grid-cols-2'>
                <FormField control={form.control} name={`endpoints.${idx}.name`}
                  render={({ field }) => (
                    <FormItem><FormLabel>{t('Name')}</FormLabel><FormControl><Input {...field} disabled={!enabled} /></FormControl><FormMessage /></FormItem>
                  )}
                />
                <FormField control={form.control} name={`endpoints.${idx}.base_url`}
                  render={({ field }) => (
                    <FormItem><FormLabel>{t('Base URL')}</FormLabel><FormControl><Input placeholder='http://guard:8080' {...field} disabled={!enabled} /></FormControl><FormMessage /></FormItem>
                  )}
                />
                <FormField control={form.control} name={`endpoints.${idx}.model`}
                  render={({ field }) => (
                    <FormItem><FormLabel>{t('Model')}</FormLabel><FormControl><Input placeholder='sileader/qwen3guard:0.6b' {...field} disabled={!enabled} /></FormControl><FormMessage /></FormItem>
                  )}
                />
                <FormField control={form.control} name={`endpoints.${idx}.format`}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Response Format')}</FormLabel>
                      <FormControl>
                        <select
                          className='border-input bg-background flex h-9 w-full rounded-md border px-3 py-1 text-sm'
                          value={field.value}
                          onChange={(e) => field.onChange(e.target.value)}
                          disabled={!enabled}
                        >
                          <option value='qwen3guard'>{t('qwen3guard (native line format)')}</option>
                          <option value='json'>{t('General model (forced JSON)')}</option>
                        </select>
                      </FormControl>
                      <FormDescription>
                        {t('Use "General model" for gpt-4o-mini, qwen-turbo, etc. — forces JSON output for max reliability.')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField control={form.control} name={`endpoints.${idx}.newToken`}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('API Token')}</FormLabel>
                      <FormControl><Input type='password' placeholder={t('Leave empty to keep existing')} {...field} disabled={!enabled} /></FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField control={form.control} name={`endpoints.${idx}.timeout_ms`}
                  render={({ field }) => (
                    <FormItem><FormLabel>{t('Timeout (ms)')}</FormLabel><FormControl><Input type='number' min={500} step={500} {...safeNumberFieldProps(field)} disabled={!enabled} /></FormControl><FormMessage /></FormItem>
                  )}
                />
                <FormField control={form.control} name={`endpoints.${idx}.input_limit`}
                  render={({ field }) => (
                    <FormItem><FormLabel>{t('Input Limit (chars)')}</FormLabel><FormControl><Input type='number' min={128} step={1000} {...safeNumberFieldProps(field)} disabled={!enabled} /></FormControl><FormMessage /></FormItem>
                  )}
                />
              </div>
              <FormField control={form.control} name={`endpoints.${idx}.enabled`}
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent><FormLabel>{t('Enabled')}</FormLabel></SettingsSwitchContent>
                    <FormControl><Switch checked={field.value} onCheckedChange={field.onChange} disabled={!enabled} /></FormControl>
                  </SettingsSwitchItem>
                )}
              />
            </div>
          ))}
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
