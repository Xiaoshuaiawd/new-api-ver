import { Plus, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { SettingsPageActionsPortal } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import {
  type JuiceFixerRule,
  getJuiceFixerConfig,
  updateJuiceFixerConfig,
} from './api'

const emptyRule = (): JuiceFixerRule => ({
  model: '',
  reasoning_effort: '',
  value: 0,
})

type EditableRule = JuiceFixerRule & { id: string }

const toEditableRule = (rule: JuiceFixerRule): EditableRule => ({
  ...rule,
  id: crypto.randomUUID(),
})

export function JuiceFixerSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['juice-fixer-config'],
    queryFn: getJuiceFixerConfig,
  })
  const [enabled, setEnabled] = useState(false)
  const [rules, setRules] = useState<EditableRule[]>([])

  useEffect(() => {
    if (!data) return
    setEnabled(data.enabled)
    setRules((data.rules ?? []).map(toEditableRule))
  }, [data])

  const saveMutation = useMutation({
    mutationFn: updateJuiceFixerConfig,
    onSuccess: (config) => {
      setEnabled(config.enabled)
      setRules((config.rules ?? []).map(toEditableRule))
      queryClient.setQueryData(['juice-fixer-config'], config)
      toast.success(t('Juice Value Fixer settings saved'))
    },
    onError: (error: Error) => toast.error(error.message),
  })

  const updateRule = (index: number, field: keyof JuiceFixerRule, value: string) => {
    setRules((current) => current.map((rule, i) => (
      i === index
        ? { ...rule, [field]: field === 'value' ? Number(value) : value }
        : rule
    )))
  }

  const save = () => {
    const normalizedRules = rules.map((rule) => ({
      model: rule.model.trim(),
      reasoning_effort: rule.reasoning_effort.trim(),
      value: Math.max(0, Math.trunc(rule.value)),
    }))
    if (normalizedRules.some((rule) => !rule.model)) {
      toast.error(t('Model is required for every Juice rule'))
      return
    }
    saveMutation.mutate({ enabled, rules: normalizedRules })
  }

  if (isLoading) return null

  return (
    <SettingsSection title={t('Juice Value Fixer')}>
      <SettingsPageActionsPortal>
        <Button onClick={save} disabled={saveMutation.isPending}>
          {saveMutation.isPending ? t('Saving...') : t('Save')}
        </Button>
      </SettingsPageActionsPortal>

      <div className='flex items-center justify-between gap-4 border-b pb-4'>
        <div className='space-y-1'>
          <Label>{t('Enable Juice Value Fixer')}</Label>
          <p className='text-muted-foreground text-sm'>
            {t('Replace Juice numbers in matching upstream answers with the configured value.')}
          </p>
        </div>
        <Switch checked={enabled} onCheckedChange={setEnabled} />
      </div>

      <div className='space-y-3'>
        <div className='flex items-center justify-between gap-3'>
          <div>
            <h4 className='font-medium'>{t('Model rules')}</h4>
            <p className='text-muted-foreground text-sm'>
              {t('Rules require an exact model and reasoning effort match. Unmatched requests pass through unchanged.')}
            </p>
          </div>
          <Button type='button' variant='outline' onClick={() => setRules((current) => [...current, toEditableRule(emptyRule())])}>
            <Plus />
            {t('Add rule')}
          </Button>
        </div>

        {rules.length === 0 ? (
          <p className='text-muted-foreground rounded-lg border border-dashed p-4 text-sm'>
            {t('No Juice rules configured')}
          </p>
        ) : (
          <div className='space-y-2'>
            {rules.map((rule, index) => (
              <div className='grid gap-2 rounded-lg border p-3 md:grid-cols-[1.4fr_1fr_120px_auto]' key={rule.id}>
                <Input
                  aria-label={t('Model')}
                  placeholder={t('Model')}
                  value={rule.model}
                  onChange={(event) => updateRule(index, 'model', event.target.value)}
                />
                <Input
                  aria-label={t('Reasoning effort')}
                  placeholder={t('Reasoning effort')}
                  value={rule.reasoning_effort}
                  onChange={(event) => updateRule(index, 'reasoning_effort', event.target.value)}
                />
                <Input
                  aria-label={t('Juice value')}
                  type='number'
                  min={0}
                  max={2147483647}
                  step={1}
                  value={rule.value}
                  onChange={(event) => updateRule(index, 'value', event.target.value)}
                />
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  title={t('Remove rule')}
                  aria-label={t('Remove rule')}
                  onClick={() => setRules((current) => current.filter((_, i) => i !== index))}
                >
                  <Trash2 />
                </Button>
              </div>
            ))}
          </div>
        )}
      </div>
    </SettingsSection>
  )
}
