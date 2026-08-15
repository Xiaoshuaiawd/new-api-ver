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
import { useEffect } from 'react'
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
import { Textarea } from '@/components/ui/textarea'

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const optionKey = 'ErrorModelRedactionPatterns'

function parsePatterns(value: string): string[] {
  try {
    const parsed: unknown = JSON.parse(value)
    return Array.isArray(parsed)
      ? parsed.filter((pattern): pattern is string => typeof pattern === 'string')
      : []
  } catch {
    return []
  }
}

function normalizePatterns(value: string): string[] {
  return [
    ...new Set(
      value
        .split('\n')
        .map((pattern) => pattern.trim())
        .filter(Boolean)
    ),
  ]
}

export function ErrorModelRedactionSection(props: { defaultValue: string }) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const schema = z.object({
    patterns: z.string().superRefine((value, context) => {
      for (const pattern of normalizePatterns(value)) {
        try {
          new RegExp(`^(?:${pattern})$`)
        } catch {
          context.addIssue({
            code: 'custom',
            message: t('Invalid regular expression: {{pattern}}', { pattern }),
          })
          return
        }
      }
    }),
  })
  type FormValues = z.infer<typeof schema>
  const defaultPatternText = parsePatterns(props.defaultValue).join('\n')
  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { patterns: defaultPatternText },
  })

  useEffect(() => {
    form.reset({ patterns: defaultPatternText })
  }, [defaultPatternText, form])

  const onSubmit = async (values: FormValues) => {
    await updateOption.mutateAsync({
      key: optionKey,
      value: JSON.stringify(normalizePatterns(values.patterns)),
    })
  }

  return (
    <SettingsSection title={t('Error Model Redaction')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save redaction rules'
          />
          <FormField
            control={form.control}
            name='patterns'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Model name regex patterns')}</FormLabel>
                <FormControl>
                  <Textarea rows={10} placeholder='^gpt.*' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Enter one regular expression per line. Matching original or mapped model names are replaced with **** in relay errors.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
