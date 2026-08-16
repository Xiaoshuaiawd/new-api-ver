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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useForm } from 'react-hook-form'
import { describe, expect, test } from 'vitest'

import { Form } from '@/components/ui/form'

import {
  CHANNEL_FORM_DEFAULT_VALUES,
  type ChannelFormValues,
} from '../../../../lib/channel-form'
import { ChannelDisableNonStreamField } from '../channel-disable-non-stream-field'

function FieldHarness(props: { enabled?: boolean }) {
  const form = useForm<ChannelFormValues>({
    defaultValues: {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      disable_non_stream: props.enabled === true,
    },
  })

  return (
    <Form {...form}>
      <ChannelDisableNonStreamField control={form.control} />
    </Form>
  )
}

describe('channel disable non-stream field', () => {
  test('starts off and lets administrators enable it', async () => {
    const user = userEvent.setup()
    render(<FieldHarness />)
    const control = screen.getByRole('switch', {
      name: 'Disable Non-Stream',
    })

    expect(control).not.toBeChecked()
    await user.click(control)
    expect(control).toBeChecked()
  })

  test('shows the saved enabled state when editing', () => {
    render(<FieldHarness enabled />)

    expect(
      screen.getByRole('switch', { name: 'Disable Non-Stream' })
    ).toBeChecked()
  })
})
