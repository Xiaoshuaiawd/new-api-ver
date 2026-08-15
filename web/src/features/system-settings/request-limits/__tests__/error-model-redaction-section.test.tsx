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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import i18next from 'i18next'
import { beforeAll, beforeEach, describe, expect, test, vi } from 'vitest'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { ErrorModelRedactionSection } from '../error-model-redaction-section'

const mutateAsync = vi.fn()

vi.mock('../../hooks/use-update-option', () => ({
  useUpdateOption: () => ({ mutateAsync, isPending: false }),
}))

function renderSection(defaultValue: string) {
  const queryClient = new QueryClient()
  const actionsContainer = document.createElement('div')
  document.body.append(actionsContainer)

  return render(
    <QueryClientProvider client={queryClient}>
      <SettingsPageProvider actionsContainer={actionsContainer}>
        <ErrorModelRedactionSection defaultValue={defaultValue} />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
}

describe('error model redaction settings', () => {
  beforeAll(() => {
    i18next.addResourceBundle('en', 'translation', {
      'Error Model Redaction': 'Error Model Redaction',
      'Model name regex patterns': 'Model name regex patterns',
      'Save redaction rules': 'Save redaction rules',
      'Invalid regular expression: {{pattern}}':
        'Invalid regular expression: {{pattern}}',
    })
  })

  beforeEach(() => {
    mutateAsync.mockReset()
    mutateAsync.mockResolvedValue({ success: true })
  })

  test('loads JSON patterns and saves non-empty lines as normalized JSON', async () => {
    renderSection('["^gpt.*","^claude.*"]')
    const input = screen.getByRole('textbox', {
      name: 'Model name regex patterns',
    })

    expect(input).toHaveValue('^gpt.*\n^claude.*')
    fireEvent.change(input, {
      target: { value: '^gpt.*\n\n  ^gemini.*  ' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save redaction rules' }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith({
        key: 'ErrorModelRedactionPatterns',
        value: '["^gpt.*","^gemini.*"]',
      })
    })
  })

  test('shows invalid regex feedback and does not save', async () => {
    renderSection('[]')
    const input = screen.getByRole('textbox', {
      name: 'Model name regex patterns',
    })

    fireEvent.change(input, { target: { value: '[invalid' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save redaction rules' }))

    expect(
      await screen.findByText('Invalid regular expression: [invalid')
    ).toBeInTheDocument()
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(mutateAsync).not.toHaveBeenCalled()
  })
})
