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
import type { Control } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'

import type { ChannelFormValues } from '../../../lib/channel-form'

type ChannelDisableNonStreamFieldProps = {
  control: Control<ChannelFormValues>
}

export function ChannelDisableNonStreamField(
  props: ChannelDisableNonStreamFieldProps
) {
  const { t } = useTranslation()

  return (
    <FormField
      control={props.control}
      name='disable_non_stream'
      render={({ field }) => (
        <FormItem className='flex items-center justify-between px-4 py-3'>
          <div className='space-y-0.5'>
            <FormLabel>{t('Disable Non-Stream')}</FormLabel>
            <FormDescription>
              {t(
                'Reject non-stream Chat Completions and Responses requests for this channel'
              )}
            </FormDescription>
          </div>
          <FormControl>
            <Switch checked={field.value} onCheckedChange={field.onChange} />
          </FormControl>
        </FormItem>
      )}
    />
  )
}
