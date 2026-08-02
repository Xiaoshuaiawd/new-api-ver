/*
Copyright (C) 2023-2026 QuantumNous
This program is free software: GNU AGPL v3+.
*/
import { SettingsPage } from '../components/settings-page'
import type { SecuritySettings } from '../types'
import {
  PROMPT_GUARD_DEFAULT_SECTION,
  getPromptGuardSectionContent,
  getPromptGuardSectionMeta,
} from './section-registry.tsx'

const defaultPromptGuardSettings: SecuritySettings = {
  ModelRequestRateLimitEnabled: false,
  ModelRequestRateLimitCount: 0,
  ModelRequestRateLimitSuccessCount: 1000,
  ModelRequestRateLimitDurationMinutes: 1,
  ModelRequestRateLimitGroup: '',
  CheckSensitiveEnabled: false,
  CheckSensitiveOnPromptEnabled: false,
  SensitiveWords: '',
  'fetch_setting.enable_ssrf_protection': true,
  'fetch_setting.allow_private_ip': false,
  'fetch_setting.domain_filter_mode': false,
  'fetch_setting.ip_filter_mode': false,
  'fetch_setting.domain_list': [],
  'fetch_setting.ip_list': [],
  'fetch_setting.allowed_ports': [],
  'fetch_setting.apply_ip_filter_for_domain': false,
  'token_setting.max_user_tokens': 1000,
}

export function PromptGuardSettingsPage() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/prompt-guard/$section'
      defaultSettings={defaultPromptGuardSettings}
      defaultSection={PROMPT_GUARD_DEFAULT_SECTION}
      getSectionContent={getPromptGuardSectionContent}
      getSectionMeta={getPromptGuardSectionMeta}
    />
  )
}
