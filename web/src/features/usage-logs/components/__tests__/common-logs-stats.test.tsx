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
import assert from 'node:assert/strict'
import { after, describe, test } from 'node:test'

import { Window } from 'happy-dom'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        Usage: 'Usage',
        RPM: 'RPM',
        TPM: 'TPM',
        'Period Revenue': 'Period Revenue',
        'Actual Quota Consumption': 'Actual Quota Consumption',
      },
    },
  },
})

const { CommonLogsStatsView, CommonLogsStatsSkeleton } =
  await import('../common-logs-stats')

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

type RenderedStats = {
  container: HTMLDivElement
  root: ReturnType<typeof createRoot>
}

async function renderStats(isRoot: boolean): Promise<RenderedStats> {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <CommonLogsStatsView
          stats={{
            quota: 5_000_000,
            rpm: 2,
            tpm: 300,
            today_revenue: 12.34,
            actual_quota: 21_739_130.43478261,
          }}
          isRoot={isRoot}
          sensitiveVisible
        />
      </I18nextProvider>
    )
  })

  return { container, root }
}

async function unmountStats(rendered: RenderedStats) {
  await act(async () => rendered.root.unmount())
  rendered.container.remove()
}

describe('common logs stats', () => {
  after(() => {
    domWindow.close()
  })

  test('shows actual quota after period revenue for root users', async () => {
    const rendered = await renderStats(true)
    const text = rendered.container.textContent ?? ''

    const revenueIndex = text.indexOf('Period Revenue')
    const actualQuotaIndex = text.indexOf('Actual Quota Consumption')
    assert.notEqual(revenueIndex, -1)
    assert.ok(actualQuotaIndex > revenueIndex)
    assert.equal(text.includes('$43.48'), true)

    await unmountStats(rendered)
  })

  test('hides revenue and actual quota from non-root users', async () => {
    const rendered = await renderStats(false)
    const text = rendered.container.textContent ?? ''

    assert.equal(text.includes('Period Revenue'), false)
    assert.equal(text.includes('Actual Quota Consumption'), false)

    await unmountStats(rendered)
  })

  test('reserves a second root-only skeleton for actual quota', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<CommonLogsStatsSkeleton isRoot />)
    })
    assert.equal(container.querySelectorAll('[data-slot="skeleton"]').length, 5)

    await act(async () => {
      root.render(<CommonLogsStatsSkeleton isRoot={false} />)
    })
    assert.equal(container.querySelectorAll('[data-slot="skeleton"]').length, 3)

    await act(async () => root.unmount())
    container.remove()
  })
})
