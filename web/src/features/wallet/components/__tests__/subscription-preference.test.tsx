/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or (at your
option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from 'node:assert/strict'
import { after, afterEach, before, describe, test } from 'node:test'

import { Window } from 'happy-dom'

import { api } from '@/lib/api'

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
  'MouseEvent',
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

Object.defineProperty(domWindow, 'matchMedia', {
  configurable: true,
  value: () => ({
    matches: false,
    media: '',
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent() {
      return true
    },
  }),
})

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { SubscriptionPlansCard } = await import('../subscription-plans-card')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'My Subscriptions': 'My Subscriptions',
        'No Active': 'No Active',
        expired: 'expired',
        'Subscription First': 'Subscription First',
        'Wallet First': 'Wallet First',
        'Subscription Only': 'Subscription Only',
        'Wallet Only': 'Wallet Only',
        'Subscription Plans': 'Subscription Plans',
        'Subscribe to a plan for model access':
          'Subscribe to a plan for model access',
        Subscription: 'Subscription',
        Expired: 'Expired',
        'Expired at': 'Expired at',
        'Total Quota': 'Total Quota',
        Remaining: 'Remaining',
        'Raw Quota': 'Raw Quota',
        Used: 'Used',
        'Preference saved as {{pref}}, but no active subscription. Wallet will be used automatically.':
          'Preference saved as {{pref}}, but no active subscription. Wallet will be used automatically.',
      },
    },
  },
})

const originalGet = api.get
const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}

type RenderedCard = {
  container: HTMLDivElement
  root: ReturnType<typeof createRoot>
}

let renderedCard: RenderedCard | null = null

function mockSubscriptionPreference(preference: string) {
  api.get = (async (url: string) => {
    if (url === '/api/subscription/plans') {
      return {
        data: {
          success: true,
          data: [
            {
              plan: {
                id: 1,
                title: 'Starter',
                price_amount: 10,
                currency: 'USD',
                duration_unit: 'month',
                duration_value: 1,
                quota_reset_period: 'never',
                enabled: true,
                sort_order: 0,
                allow_balance_pay: true,
                allow_wallet_overflow: true,
                max_purchase_per_user: 0,
                total_amount: 100,
              },
            },
          ],
        },
      }
    }
    if (url === '/api/subscription/self') {
      return {
        data: {
          success: true,
          data: {
            billing_preference: preference,
            subscriptions: [],
            all_subscriptions: [],
          },
        },
      }
    }
    throw new Error(`unexpected request: ${url}`)
  }) as typeof api.get
}

async function renderCard(preference: string): Promise<RenderedCard> {
  mockSubscriptionPreference(preference)
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <SubscriptionPlansCard topupInfo={null} />
      </I18nextProvider>
    )
    await Promise.resolve()
    await Promise.resolve()
  })

  renderedCard = { container, root }
  return renderedCard
}

async function unmountCard(rendered: RenderedCard) {
  await act(async () => rendered.root.unmount())
  rendered.container.remove()
}

before(() => {
  reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true
})

afterEach(async () => {
  if (renderedCard) {
    await unmountCard(renderedCard)
    renderedCard = null
  }
  api.get = originalGet
  document.body.replaceChildren()
})

after(() => {
  delete reactTestGlobals.IS_REACT_ACT_ENVIRONMENT
  domWindow.close()
})

describe('subscription billing preference without active subscription', () => {
  test('keeps subscription-only visible as the selected preference', async () => {
    const rendered = await renderCard('subscription_only')

    const trigger = rendered.container.querySelector(
      '[data-slot="select-trigger"]'
    )
    assert.ok(trigger)
    assert.match(trigger.textContent || '', /Subscription Only/)
    assert.doesNotMatch(trigger.textContent || '', /Wallet First/)
  })

  test('shows the wallet fallback notice only for subscription-first', async () => {
    const rendered = await renderCard('subscription_first')

    assert.match(
      rendered.container.textContent || '',
      /Wallet will be used automatically/
    )
  })
})
