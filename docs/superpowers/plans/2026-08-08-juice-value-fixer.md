# Juice Value Fixer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an admin-configured, centralized response postprocessor that replaces only Juice-number text in OpenAI Responses and Chat Completions APIs for streaming and non-streaming requests.

**Architecture:** Normalize incoming request text into a trigger context, resolve an exact `model + reasoning_effort` rule from a runtime option-backed configuration, and pass the selected replacement into shared OpenAI response handlers. Non-streaming handlers transform decoded response content before writing; streaming handlers transform only text deltas while preserving SSE structure, reasoning fields, usage, and metadata.

**Tech Stack:** Go 1.22, Gin, GORM option storage, `common.Marshal`/`common.Unmarshal`, React 19/TypeScript, i18next, Base UI/Tailwind.

## Global Constraints

- Use `common.Marshal`/`common.Unmarshal` for JSON operations in Go business code.
- Preserve SQLite, MySQL, and PostgreSQL compatibility by using the existing option storage; no schema migration.
- Use pointer/optional request fields as already defined; do not change request billing or usage fields.
- Preserve protected new-api and QuantumNous identifiers.
- User-facing UI text must use i18n keys in every supported locale.
- Runtime transformation failures must pass through the original upstream response.

---

### Task 1: Runtime Configuration and Matcher

**Files:**
- Create: `setting/juice_fixer_setting/juice_fixer_setting.go`
- Create: `setting/juice_fixer_setting/juice_fixer_setting_test.go`
- Create: `service/juice_fixer.go`
- Create: `service/juice_fixer_test.go`
- Modify: `model/option.go` (register/load `juice_fixer_setting`)

**Interfaces:**
- `juice_fixer_setting.StorageConfig`, `juice_fixer_setting.Rule`, `juice_fixer_setting.Default`, `LoadFromJSON`, `GetStorageJSON`, `GetPublic`, `Update`.
- `service.JuiceContext{Model, ReasoningEffort, Triggered}` and `service.NewJuiceContext(request any, model, effort string)`.
- `service.ResolveJuiceValue(ctx JuiceContext) (int, bool)`.
- `service.ReplaceJuiceNumber(text string, value int) (string, bool)`.

- [ ] **Step 1: Write failing tests** for default-disabled config, exact model/effort matching with no fallback, atomic validation boundaries, user/system-only triggers, case/spacing variants, numeric-only replacement, and sentence replacement.
- [ ] **Step 2: Run focused Go tests** with `go test ./service ./setting/juice_fixer_setting ./model`; expect failures for missing symbols.
- [ ] **Step 3: Implement option-backed config** with an RWMutex snapshot, non-negative bounded integer values, duplicate rule rejection, and `model.UpdateOption` loading in the existing option switch.
- [ ] **Step 4: Implement normalized trigger extraction** for `dto.GeneralOpenAIRequest` and `dto.OpenAIResponsesRequest`, scanning latest user text and system/instructions text with Unicode whitespace normalization and case-insensitive Juice patterns.
- [ ] **Step 5: Implement bounded Juice-number replacement** that targets a standalone number next to Juice wording, or the complete numeric-only answer, preserving punctuation and surrounding sentence text.
- [ ] **Step 6: Run focused tests** and commit `feat: add juice fixer matcher and settings`.

### Task 2: Admin API

**Files:**
- Create: `controller/juice_fixer.go`
- Create: `controller/juice_fixer_test.go`
- Modify: `router/api-router.go` (root-auth `/juice-fixer/config` routes)

**Interfaces:**
- `GET /api/juice-fixer/config` returns `juice_fixer_setting.PublicConfig`.
- `PUT /api/juice-fixer/config` accepts the public rule shape, validates the full payload, persists atomically, and returns the updated public config.

- [ ] **Step 1: Write failing controller tests** for GET, valid PUT, invalid value/rule, duplicate rule, and persistence failure behavior using Gin test contexts.
- [ ] **Step 2: Run `go test ./controller -run JuiceFixer`** and verify failures.
- [ ] **Step 3: Add controller validation and option persistence** following `controller/prompt_guard.go`; never expose internal mutex/storage details.
- [ ] **Step 4: Register root-auth routes** in the existing API router.
- [ ] **Step 5: Run controller/router tests** and commit `feat: add juice fixer admin api`.

### Task 3: Non-Streaming OpenAI Responses

**Files:**
- Modify: `relay/channel/openai/relay_responses.go`
- Modify: `relay/channel/openai/relay-openai.go`
- Create: `relay/channel/openai/juice_fixer_response_test.go`

**Interfaces:**
- Shared helper receives `*relaycommon.RelayInfo` plus the incoming request trigger context and returns the transformed response body without changing usage structures.

- [ ] **Step 1: Add failing tests** for Chat Completions `choices[*].message.content`, Responses output text content parts, no-trigger pass-through, and reasoning/usage preservation.
- [ ] **Step 2: Run `go test ./relay/channel/openai -run JuiceFixer`** and verify failures.
- [ ] **Step 3: Apply `service.ReplaceJuiceNumber` after successful upstream decode and before `IOCopyBytesGracefully`/client JSON output.** Keep the original bytes on decode or transform errors.
- [ ] **Step 4: Run focused OpenAI tests** and commit `feat: transform juice values in non-stream responses`.

### Task 4: Streaming OpenAI Responses

**Files:**
- Modify: `relay/channel/openai/relay-openai.go`
- Modify: `relay/channel/openai/relay_responses.go`
- Create: `relay/channel/openai/juice_fixer_stream_test.go`

**Interfaces:**
- A per-request `service.JuiceStreamTransformer` buffers only eligible output text and exposes `TransformChatChunk`/`TransformResponsesChunk` plus `Flush`.

- [ ] **Step 1: Add failing SSE tests** where the Juice number is split across deltas, where reasoning deltas contain numbers, and where usage/final events must remain unchanged.
- [ ] **Step 2: Run the focused stream tests** and verify failures.
- [ ] **Step 3: Integrate the transformer in `OaiStreamHandler` and `OaiResponsesStreamHandler`** before `sendStreamData`/`sendResponsesStreamData`; preserve event framing and existing usage extraction.
- [ ] **Step 4: Ensure stream-end flush emits transformed pending text exactly once** and pass-through is used when no replacement is found.
- [ ] **Step 5: Run focused stream tests** and commit `feat: transform juice values in streaming responses`.

### Task 5: Admin UI and i18n

**Files:**
- Create: `web/src/features/system-settings/juice-fixer/api.ts`
- Create: `web/src/features/system-settings/juice-fixer/juice-fixer-section.tsx`
- Create: `web/src/features/system-settings/juice-fixer/section-registry.tsx`
- Create: `web/src/features/system-settings/juice-fixer/index.tsx`
- Modify: `web/src/components/layout/config/system-settings.config.ts`
- Create: `web/src/routes/_authenticated/system-settings/juice-fixer/index.tsx`
- Create: `web/src/routes/_authenticated/system-settings/juice-fixer/$section.tsx`
- Modify: `web/src/i18n/locales/{en,zh,zh-TW,fr,ru,ja,vi}.json`

**Interfaces:**
- Admin page edits `enabled` and a table of `{model, reasoning_effort, value}` rules through the Task 2 API.

- [ ] **Step 1: Add typed API functions and a component-level validation test fixture** for load, add/edit/remove rule, validation, save, and disabled state.
- [ ] **Step 2: Run `bun run typecheck` from `web/` and verify the new API/component symbols fail type checking before implementation.
- [ ] **Step 3: Implement the settings section using existing settings-page, Base UI, and i18n patterns.** Use numeric input bounds and explicit save/loading/error states.
- [ ] **Step 4: Register the system-settings navigation and route, then add locale keys for all user-facing strings. Run `bun run i18n:sync` from `web/` to propagate missing locale keys.
- [ ] **Step 5: Run `bun run build:check` and `bun run lint`; commit `feat: add juice fixer admin settings`.

### Task 6: End-to-End Verification

**Files:**
- Modify: `relay/channel/openai/juice_fixer_response_test.go` and `relay/channel/openai/juice_fixer_stream_test.go` only if integration gaps are found.

- [ ] **Step 1: Run backend formatting and tests:** `gofmt -w` on changed Go files, then `go test ./...`.
- [ ] **Step 2: Run frontend checks:** `cd web && bun run build:check && bun run lint && bun run i18n:sync`.
- [ ] **Step 3: Inspect `git diff --check` and verify no protected identifiers changed.
- [ ] **Step 4: Commit any verification-only fixes as `test: verify juice fixer integration`.
