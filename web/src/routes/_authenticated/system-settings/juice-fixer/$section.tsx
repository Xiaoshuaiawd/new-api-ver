import { createFileRoute, redirect } from '@tanstack/react-router'

import { JuiceFixerSettingsPage } from '@/features/system-settings/juice-fixer'
import {
  JUICE_FIXER_DEFAULT_SECTION,
  JUICE_FIXER_SECTION_IDS,
} from '@/features/system-settings/juice-fixer/section-registry'

export const Route = createFileRoute('/_authenticated/system-settings/juice-fixer/$section')({
  beforeLoad: ({ params }) => {
    const validSections = JUICE_FIXER_SECTION_IDS as unknown as string[]
    if (!validSections.includes(params.section)) {
      throw redirect({
        to: '/system-settings/juice-fixer/$section',
        params: { section: JUICE_FIXER_DEFAULT_SECTION },
      })
    }
  },
  component: JuiceFixerSettingsPage,
})
