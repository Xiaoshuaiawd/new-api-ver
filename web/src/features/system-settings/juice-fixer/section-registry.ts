import type { TFunction } from 'i18next'
import { createElement } from 'react'

import { createSectionRegistry } from '../utils/section-registry'
import { JuiceFixerSection } from './juice-fixer-section'

const JUICE_FIXER_SECTIONS = [
  {
    id: 'juice-fixer',
    titleKey: 'Juice Value Fixer',
    build: () => createElement(JuiceFixerSection),
  },
] as const

export type JuiceFixerSectionId = (typeof JUICE_FIXER_SECTIONS)[number]['id']

const registry = createSectionRegistry<JuiceFixerSectionId, Record<string, never>>({
  sections: JUICE_FIXER_SECTIONS,
  defaultSection: 'juice-fixer',
  basePath: '/system-settings/juice-fixer',
  urlStyle: 'path',
})

export const JUICE_FIXER_SECTION_IDS = registry.sectionIds
export const JUICE_FIXER_DEFAULT_SECTION = registry.defaultSection
export const getJuiceFixerSectionNavItems = (t: TFunction) =>
  registry.getSectionNavItems(t)
export const getJuiceFixerSectionContent = registry.getSectionContent
export const getJuiceFixerSectionMeta = registry.getSectionMeta
