/*
Copyright (C) 2023-2026 QuantumNous
This program is free software: GNU AGPL v3+.
*/
import type { SecuritySettings } from '../types'
import { createSectionRegistry } from '../utils/section-registry'
import { PromptGuardSection } from './prompt-guard-section'

// PromptGuard has its own dedicated settings page separate from the Security
// section because it uses a custom API (not the generic option PUT).
// We register it as a standalone nav group with a single section.

const PROMPT_GUARD_SECTIONS = [
  {
    id: 'prompt-guard',
    titleKey: 'Prompt Guard',
    build: (_settings: SecuritySettings) => <PromptGuardSection />,
  },
] as const

export type PromptGuardSectionId = (typeof PROMPT_GUARD_SECTIONS)[number]['id']

const promptGuardRegistry = createSectionRegistry<
  PromptGuardSectionId,
  SecuritySettings
>({
  sections: PROMPT_GUARD_SECTIONS,
  defaultSection: 'prompt-guard',
  basePath: '/system-settings/prompt-guard',
  urlStyle: 'path',
})

export const PROMPT_GUARD_SECTION_IDS = promptGuardRegistry.sectionIds
export const PROMPT_GUARD_DEFAULT_SECTION = promptGuardRegistry.defaultSection
export const getPromptGuardSectionNavItems =
  promptGuardRegistry.getSectionNavItems
export const getPromptGuardSectionContent =
  promptGuardRegistry.getSectionContent
export const getPromptGuardSectionMeta = promptGuardRegistry.getSectionMeta
