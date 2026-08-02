/*
Copyright (C) 2023-2026 QuantumNous
This program is free software: GNU AGPL v3+.
*/
import { createFileRoute, redirect } from '@tanstack/react-router'

import { PROMPT_GUARD_DEFAULT_SECTION } from '@/features/system-settings/prompt-guard/section-registry.tsx'

export const Route = createFileRoute(
  '/_authenticated/system-settings/prompt-guard/'
)({
  beforeLoad: () => {
    throw redirect({
      to: '/system-settings/prompt-guard/$section',
      params: { section: PROMPT_GUARD_DEFAULT_SECTION },
    })
  },
})
