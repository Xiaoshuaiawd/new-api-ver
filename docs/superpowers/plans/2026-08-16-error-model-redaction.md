# Error Model Redaction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add administrator-configurable model-name regular expressions that redact matching original or mapped model names from relay error responses and error content logs.

**Architecture:** A focused `error_model_redaction` settings package owns validated source patterns, compiled regex snapshots, and literal model-name replacement. The relay controller applies redaction only at downstream and logging boundaries while retaining the original `NewAPIError` for retry, auto-disable, and other internal decisions. A Security & Limits section edits the registered option as a JSON string array.

**Tech Stack:** Go 1.22+, Gin, GORM, testify, React 19, TypeScript, React Hook Form, Zod, i18next, Vitest, Testing Library, Bun.

## Global Constraints

- Preserve the error-log row's independent model field.
- Do not change retry, channel-disable, violation-fee, or error-classification behavior.
- Use `common.Marshal` and `common.UnmarshalJsonStr` for Go JSON operations.
- Support SQLite, MySQL, and PostgreSQL without dialect-specific persistence.
- Use Bun for frontend scripts and add all user-facing copy to every supported locale.
- Keep changes surgical and do not modify protected project or author identifiers.

---

### Task 1: Validated runtime configuration and redaction primitive

**Files:**
- Create: `setting/error_model_redaction/setting.go`
- Create: `setting/error_model_redaction/setting_test.go`
- Modify: `model/option.go`
- Test: `model/option_error_model_redaction_test.go`

**Interfaces:**
- Produces: `ValidatePatternsJSON(value string) error`
- Produces: `UpdatePatternsJSON(value string) error`
- Produces: `PatternsJSON() string`
- Produces: `Redact(message string, modelNames ...string) string`

- [ ] Write failing table tests proving valid multi-pattern matching, original/mapped model replacement, punctuation preservation, nonmatching no-op behavior, longest-first replacement, and atomic rejection of invalid expressions.
- [ ] Run `go test ./setting/error_model_redaction ./model -run 'ErrorModelRedaction|Redact'` and verify failures are caused by missing implementation.
- [ ] Implement an immutable compiled snapshot guarded by `sync.RWMutex`; trim blanks, deduplicate patterns, compile each as `^(?:<pattern>)$`, sort selected model names longest first, and replace matching literal names with `****`.
- [ ] Register the option default in `model.InitOptionMap`, validate before database persistence in `validateOptionValue`, and update runtime state from `updateOptionMap`.
- [ ] Re-run the focused tests and verify they pass.
- [ ] Commit with `feat: add error model redaction settings`.

### Task 2: Relay response and error-log boundary integration

**Files:**
- Modify: `controller/relay.go`
- Create or modify: `controller/relay_test.go`

**Interfaces:**
- Consumes: `error_model_redaction.Redact(message string, modelNames ...string) string`
- Produces: controller-local redacted error rendering without mutating the original `*types.NewAPIError`

- [ ] Write failing tests showing `gpt-5.6-luna` becomes `****` in downstream OpenAI error JSON and database error-log content, while the log's `ModelName` stays `gpt-5.6-luna` and the original error remains unchanged.
- [ ] Run the focused controller tests and verify the sensitive model is still exposed before implementation.
- [ ] Add a small boundary helper that derives original and mapped model names from relay/context state, copies the error value, replaces its display message only, and uses the copy for client serialization.
- [ ] Use redacted rendering in final relay logs, channel logs, and `RecordErrorLog`; retain the original error for `shouldRetry`, `ShouldDisableChannel`, `DisableChannel`, and violation-fee decisions.
- [ ] Re-run focused controller tests and verify they pass.
- [ ] Commit with `feat: redact model names from relay errors`.

### Task 3: Security settings editor

**Files:**
- Create: `web/src/features/system-settings/request-limits/error-model-redaction-section.tsx`
- Create: `web/src/features/system-settings/request-limits/__tests__/error-model-redaction-section.test.tsx`
- Modify: `web/src/features/system-settings/security/section-registry.tsx`
- Modify: `web/src/features/system-settings/security/index.tsx`
- Modify: `web/src/features/system-settings/types.ts`
- Modify: `web/src/i18n/locales/en.json`
- Modify: `web/src/i18n/locales/zh.json`
- Modify: `web/src/i18n/locales/zh-TW.json`
- Modify: `web/src/i18n/locales/fr.json`
- Modify: `web/src/i18n/locales/ru.json`
- Modify: `web/src/i18n/locales/ja.json`
- Modify: `web/src/i18n/locales/vi.json`

**Interfaces:**
- Consumes option key: `ErrorModelRedactionPatterns`
- Stores value: JSON string array such as `["^gpt.*", "^claude.*"]`

- [ ] Write failing component tests that load JSON patterns into one-rule-per-line text, edit multiple expressions, save normalized JSON, and expose backend invalid-regex feedback through the existing mutation path.
- [ ] Run the focused Vitest file and verify it fails because the section does not exist.
- [ ] Implement a React Hook Form section with a textarea, `^gpt.*` example, empty-line filtering, client-side JavaScript regex validation for fast feedback, and the existing `useUpdateOption` save flow.
- [ ] Register the new `error-model-redaction` Security & Limits route section and add the option to `SecuritySettings` defaults/types.
- [ ] Add all user-facing strings to seven locale files.
- [ ] Run focused tests, typecheck, and affected-file lint; verify all pass.
- [ ] Commit with `feat(web): configure error model redaction`.

### Task 4: End-to-end verification

**Files:**
- Verify all files changed by Tasks 1-3.

- [ ] Run `go test ./setting/error_model_redaction ./model ./controller ./service ./relay/...` and confirm zero failures.
- [ ] Run `cd web && npx --yes bun@1.3.14 run test` and confirm zero failures.
- [ ] Run `cd web && npx --yes bun@1.3.14 run typecheck` and confirm exit code 0.
- [ ] Run affected-file Oxlint and confirm exit code 0.
- [ ] Run `cd web && npx --yes bun@1.3.14 run build` and confirm exit code 0.
- [ ] Run `git diff --check`, inspect `git status --short --branch`, and review the final commit list.
