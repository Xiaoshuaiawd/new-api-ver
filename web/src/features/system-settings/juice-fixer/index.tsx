import { SettingsPage } from '../components/settings-page'
import {
  JUICE_FIXER_DEFAULT_SECTION,
  getJuiceFixerSectionContent,
  getJuiceFixerSectionMeta,
} from './section-registry'

export function JuiceFixerSettingsPage() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/juice-fixer/$section'
      defaultSettings={{}}
      defaultSection={JUICE_FIXER_DEFAULT_SECTION}
      getSectionContent={(sectionId) => getJuiceFixerSectionContent(sectionId, {})}
      getSectionMeta={getJuiceFixerSectionMeta}
    />
  )
}
