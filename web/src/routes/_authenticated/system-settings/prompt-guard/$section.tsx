/*
Copyright (C) 2023-2026 QuantumNous
This program is free software: GNU AGPL v3+.
*/
import { createFileRoute, redirect } from '@tanstack/react-router'

import { PromptGuardSettingsPage } from '@/features/system-settings/prompt-guard'
import {
  PROMPT_GUARD_DEFAULT_SECTION,
  PROMPT_GUARD_SECTION_IDS,
} from '@/features/system-settings/prompt-guard/section-registry.tsx'

export const Route = createFileRoute(
  '/_authenticated/system-settings/prompt-guard/$section'
)({
  beforeLoad: ({ params }) => {
    const validSections = PROMPT_GUARD_SECTION_IDS as unknown as string[]
    if (!validSections.includes(params.section)) {
      throw redirect({
        to: '/system-settings/prompt-guard/$section',
        params: { section: PROMPT_GUARD_DEFAULT_SECTION },
      })
    }
  },
  component: PromptGuardSettingsPage,
})
