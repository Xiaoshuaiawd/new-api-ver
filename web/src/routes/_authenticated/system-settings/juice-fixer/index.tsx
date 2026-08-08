import { createFileRoute, redirect } from '@tanstack/react-router'

import { JUICE_FIXER_DEFAULT_SECTION } from '@/features/system-settings/juice-fixer/section-registry'

export const Route = createFileRoute('/_authenticated/system-settings/juice-fixer/')({
  beforeLoad: () => {
    throw redirect({
      to: '/system-settings/juice-fixer/$section',
      params: { section: JUICE_FIXER_DEFAULT_SECTION },
    })
  },
})
