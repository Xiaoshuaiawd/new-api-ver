# Channel Disable Non-Stream Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a default-off channel switch that rejects non-stream Chat Completions and Responses requests with HTTP 400 before contacting upstream.

**Architecture:** Store `disable_non_stream` in the existing channel `setting` JSON through `dto.ChannelSettings`. After channel selection, the relay controller reads the selected channel settings from Gin context and applies one centralized policy check before dispatch, returning a skip-retry invalid-request error without invoking channel-error handling. The existing channel form owns parsing and serialization, while the existing extra-settings switch list exposes the option in both create and edit modes.

**Tech Stack:** Go 1.22+, Gin, relaykit DTOs, React 19, TypeScript, React Hook Form, Zod, Base UI Switch, i18next, Vitest, Bun

## Global Constraints

- The switch is channel-level, named `disable_non_stream`, and defaults to `false`.
- Only Chat Completions and Responses are restricted; all other relay modes remain unchanged.
- `stream: false` and omitted `stream` are non-stream; `stream: true` is allowed.
- Rejection returns HTTP 400 with the fixed message `当前号池正在遭受恶意毁号请求，暂停同步请求` in the existing OpenAI-compatible error envelope.
- Rejection must not retry another channel, contact upstream, or enter channel auto-disable/error processing.
- Use the existing channel `setting` JSON; do not add a database migration or dependency.
- `relaykit/` must remain independently buildable with `cd relaykit && GOWORK=off go build ./...`.
- Frontend text must be translated in all seven locale files and frontend commands use Bun.
- Preserve all protected project and author identity references.

---

## File Map

- `relaykit/dto/channel_settings.go` — defines the persisted channel setting field.
- `relaykit/dto/channel_settings_test.go` — protects JSON compatibility and default-off behavior.
- `controller/relay.go` — owns the selected-channel non-stream policy and relay-loop enforcement point.
- `controller/relay_non_stream_test.go` — tests the policy boundary independently of upstream providers.
- `web/src/features/channels/types.ts` — mirrors channel settings returned by the API.
- `web/src/features/channels/lib/channel-form.ts` — owns form schema, defaults, edit hydration, configured state input, and create/update serialization.
- `web/src/features/channels/lib/__tests__/channel-non-stream-setting.test.ts` — protects add/edit settings behavior.
- `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx` — renders and tracks the extra-settings switch.
- `web/src/i18n/locales/{en,zh,zh-TW,fr,ru,ja,vi}.json` — translates the label and description.

---

### Task 1: Persist the Channel Setting

**Files:**
- Modify: `relaykit/dto/channel_settings.go`
- Modify: `relaykit/dto/channel_settings_test.go`

**Interfaces:**
- Produces: `dto.ChannelSettings.DisableNonStream bool` serialized as `disable_non_stream`.
- Consumed by: controller policy in Task 2 and frontend/API JSON in Task 3.

- [ ] **Step 1: Write the failing JSON compatibility test**

Extend `TestChannelSettingsHTTPTransportJSONRoundTrip` or add a focused table test proving legacy JSON defaults off, explicit `true` round-trips, and false is omitted:

```go
func TestChannelSettingsDisableNonStreamJSONRoundTrip(t *testing.T) {
	var legacy ChannelSettings
	require.NoError(t, json.Unmarshal([]byte(`{"proxy":""}`), &legacy))
	assert.False(t, legacy.DisableNonStream)

	explicit := ChannelSettings{DisableNonStream: true}
	encoded, err := json.Marshal(explicit)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"disable_non_stream":true`)

	var decoded ChannelSettings
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.True(t, decoded.DisableNonStream)

	encoded, err = json.Marshal(ChannelSettings{})
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "disable_non_stream")
}
```

- [ ] **Step 2: Run the relaykit DTO test and verify RED**

Run:

```bash
cd relaykit && GOWORK=off go test ./dto -run TestChannelSettingsDisableNonStreamJSONRoundTrip -count=1
```

Expected: compilation fails because `ChannelSettings.DisableNonStream` does not exist.

- [ ] **Step 3: Add the minimal DTO field**

Add to `ChannelSettings`:

```go
DisableNonStream bool `json:"disable_non_stream,omitempty"`
```

Keep it in `relaykit/dto`; do not introduce a root-module dependency.

- [ ] **Step 4: Run the DTO test and independent relaykit build**

Run:

```bash
cd relaykit && GOWORK=off go test ./dto -run TestChannelSettingsDisableNonStreamJSONRoundTrip -count=1
cd relaykit && GOWORK=off go build ./...
```

Expected: both commands exit 0.

- [ ] **Step 5: Commit the setting contract**

```bash
git add relaykit/dto/channel_settings.go relaykit/dto/channel_settings_test.go
git commit -m "feat: add channel non-stream setting"
```

---

### Task 2: Reject Selected-Channel Non-Stream Requests

**Files:**
- Create: `controller/relay_non_stream_test.go`
- Modify: `controller/relay.go`

**Interfaces:**
- Consumes: `dto.ChannelSettings.DisableNonStream` from `constant.ContextKeyChannelSetting`.
- Produces: `rejectDisabledNonStreamRequest(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError`.
- Contract: returns `nil` unless the selected channel has the switch enabled, relay mode is Chat Completions or Responses, and `info.IsStream` is false.

- [ ] **Step 1: Write failing policy tests**

Create a deterministic table test using real Gin context values:

```go
package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRejectDisabledNonStreamRequest(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		relayMode  int
		isStream   bool
		wantReject bool
	}{
		{name: "chat non-stream", enabled: true, relayMode: relayconstant.RelayModeChatCompletions, wantReject: true},
		{name: "responses non-stream", enabled: true, relayMode: relayconstant.RelayModeResponses, wantReject: true},
		{name: "chat stream", enabled: true, relayMode: relayconstant.RelayModeChatCompletions, isStream: true},
		{name: "responses stream", enabled: true, relayMode: relayconstant.RelayModeResponses, isStream: true},
		{name: "default off", relayMode: relayconstant.RelayModeChatCompletions},
		{name: "completions excluded", enabled: true, relayMode: relayconstant.RelayModeCompletions},
		{name: "responses compact excluded", enabled: true, relayMode: relayconstant.RelayModeResponsesCompact},
		{name: "embedding excluded", enabled: true, relayMode: relayconstant.RelayModeEmbeddings},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{
				DisableNonStream: tt.enabled,
			})
			info := &relaycommon.RelayInfo{RelayMode: tt.relayMode, IsStream: tt.isStream}

			err := rejectDisabledNonStreamRequest(c, info)
			if !tt.wantReject {
				assert.Nil(t, err)
				return
			}

			require.NotNil(t, err)
			assert.Equal(t, http.StatusBadRequest, err.StatusCode)
			assert.Equal(t, types.ErrorCodeInvalidRequest, err.GetErrorCode())
			assert.Equal(t, "当前号池正在遭受恶意毁号请求，暂停同步请求", err.Error())
			assert.True(t, types.IsSkipRetryError(err))
		})
	}
}
```

This covers omitted `stream` because `RelayInfo.IsStream` defaults to `false`, matching the request DTO behavior.

- [ ] **Step 2: Run the controller test and verify RED**

Run:

```bash
go test ./controller -run TestRejectDisabledNonStreamRequest -count=1
```

Expected: compilation fails because `rejectDisabledNonStreamRequest` does not exist.

- [ ] **Step 3: Implement the minimal policy function**

Add a stable domain helper near `relayHandler` in `controller/relay.go`:

```go
const disabledNonStreamMessage = "当前号池正在遭受恶意毁号请求，暂停同步请求"

func rejectDisabledNonStreamRequest(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if c == nil || info == nil || info.IsStream {
		return nil
	}
	if info.RelayMode != relayconstant.RelayModeChatCompletions &&
		info.RelayMode != relayconstant.RelayModeResponses {
		return nil
	}
	settings, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting)
	if !ok || !settings.DisableNonStream {
		return nil
	}
	return types.NewErrorWithStatusCode(
		errors.New(disabledNonStreamMessage),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)
}
```

No new error code is needed because this is a client request policy rejection.

- [ ] **Step 4: Wire the check before relay dispatch and channel-error processing**

Immediately after successful `getChannel(...)` and before `addUsedChannel(...)`, add:

```go
if policyErr := rejectDisabledNonStreamRequest(c, relayInfo); policyErr != nil {
	newAPIError = policyErr
	break
}
```

Do not call `processChannelError` for this branch. The existing deferred cleanup will refund pre-consumption, and the loop exits without evaluating retry.

- [ ] **Step 5: Run policy and controller regression tests**

Run:

```bash
go test ./controller -run 'TestRejectDisabledNonStreamRequest|TestRedactedRelayError|TestProcessChannelError' -count=1
go test ./controller ./relay/... ./service
```

Expected: all selected packages pass.

- [ ] **Step 6: Commit the enforcement boundary**

```bash
git add controller/relay.go controller/relay_non_stream_test.go
git commit -m "feat: reject channel non-stream requests"
```

---

### Task 3: Add Channel Form Parsing and Serialization

**Files:**
- Modify: `web/src/features/channels/types.ts`
- Modify: `web/src/features/channels/lib/channel-form.ts`
- Create: `web/src/features/channels/lib/__tests__/channel-non-stream-setting.test.ts`

**Interfaces:**
- Consumes: API setting key `disable_non_stream`.
- Produces: `ChannelFormValues.disable_non_stream?: boolean`, default `false`, and `setting` JSON containing the form value.
- Consumed by: switch UI in Task 4.

- [ ] **Step 1: Write failing form contract tests**

Create the test file with project copyright header and these behaviors:

```ts
import { describe, expect, test } from 'vitest'

import { channelSchema, type Channel } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  buildSettingJSON,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
} from '../channel-form'

function channelWithSetting(setting: string): Channel {
  return channelSchema.parse({
    id: 1,
    type: 1,
    key: '',
    status: 1,
    name: 'test',
    created_time: 0,
    test_time: 0,
    response_time: 0,
    balance: 0,
    balance_updated_time: 0,
    models: 'gpt-5',
    group: 'default',
    used_quota: 0,
    setting,
    settings: '{}',
  })
}

describe('channel disable non-stream setting', () => {
  test('defaults to off for new and legacy channels', () => {
    expect(CHANNEL_FORM_DEFAULT_VALUES.disable_non_stream).toBe(false)
    expect(transformChannelToFormDefaults(channelWithSetting('{}')).disable_non_stream).toBe(false)
  })

  test('hydrates enabled setting when editing a channel', () => {
    const values = transformChannelToFormDefaults(
      channelWithSetting('{"disable_non_stream":true}')
    )
    expect(values.disable_non_stream).toBe(true)
  })

  test('serializes enabled setting for create and update payloads', () => {
    const values = { ...CHANNEL_FORM_DEFAULT_VALUES, disable_non_stream: true }
    expect(JSON.parse(buildSettingJSON(values)).disable_non_stream).toBe(true)
    expect(
      JSON.parse(String(transformFormDataToCreatePayload(values).channel.setting))
        .disable_non_stream
    ).toBe(true)
    expect(
      JSON.parse(String(transformFormDataToUpdatePayload(values, 1).setting))
        .disable_non_stream
    ).toBe(true)
  })
})
```

- [ ] **Step 2: Run the focused Vitest and verify RED**

Run from `web/`:

```bash
bun run test -- src/features/channels/lib/__tests__/channel-non-stream-setting.test.ts
```

Expected: TypeScript/test failure because `disable_non_stream` is absent from the form and settings types.

- [ ] **Step 3: Add the setting to types, schema, defaults, hydration, and serialization**

In `ChannelSettings` add:

```ts
disable_non_stream?: boolean
```

In `channelFormSchema` add:

```ts
disable_non_stream: z.boolean().optional(),
```

In `CHANNEL_FORM_DEFAULT_VALUES` add:

```ts
disable_non_stream: false,
```

In `transformChannelToFormDefaults`, initialize and hydrate it with strict boolean semantics:

```ts
disable_non_stream: false,
```

and:

```ts
disable_non_stream: parsed.disable_non_stream === true,
```

In `buildSettingJSON` add:

```ts
disable_non_stream: formData.disable_non_stream === true,
```

Keep explicit `false` consistent with the existing form serializer; the Go DTO accepts both omitted and false as disabled.

- [ ] **Step 4: Run the focused test and typecheck**

Run from `web/`:

```bash
bun run test -- src/features/channels/lib/__tests__/channel-non-stream-setting.test.ts
bun run typecheck
```

Expected: both commands exit 0.

- [ ] **Step 5: Commit the form contract**

```bash
git add web/src/features/channels/types.ts web/src/features/channels/lib/channel-form.ts web/src/features/channels/lib/__tests__/channel-non-stream-setting.test.ts
git commit -m "feat(web): serialize channel non-stream setting"
```

---

### Task 4: Expose the Switch in Channel Create and Edit

**Files:**
- Modify: `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx`
- Modify: `web/src/i18n/locales/en.json`
- Modify: `web/src/i18n/locales/zh.json`
- Modify: `web/src/i18n/locales/zh-TW.json`
- Modify: `web/src/i18n/locales/fr.json`
- Modify: `web/src/i18n/locales/ru.json`
- Modify: `web/src/i18n/locales/ja.json`
- Modify: `web/src/i18n/locales/vi.json`

**Interfaces:**
- Consumes: `ChannelFormValues.disable_non_stream` from Task 3.
- Produces: an accessible `FormField` switch shown in the shared create/edit drawer.

- [ ] **Step 1: Add field tracking and configured-state behavior**

Add `disable_non_stream` to `SENSITIVE_FORM_FIELDS`, watch it alongside the other extra settings, and include it in both `hasAdvancedSettingsValues(...)` and `extraSettingsConfigured`:

```ts
const currentDisableNonStream = form.watch('disable_non_stream')
```

and Boolean expressions containing:

```ts
values.disable_non_stream ||
```

```ts
currentDisableNonStream ||
```

This preserves permission locking and makes the advanced section visibly configured when enabled.

- [ ] **Step 2: Render the switch in Channel Extra Settings**

Add after **Pass Through Body** and before task-polling settings:

```tsx
<FormField
  control={form.control}
  name='disable_non_stream'
  render={({ field }) => (
    <FormItem className='flex items-center justify-between px-4 py-3'>
      <div className='space-y-0.5'>
        <FormLabel>{t('Disable Non-Stream')}</FormLabel>
        <FormDescription>
          {t(
            'Reject non-stream Chat Completions and Responses requests for this channel'
          )}
        </FormDescription>
      </div>
      <FormControl>
        <Switch
          checked={field.value}
          onCheckedChange={field.onChange}
        />
      </FormControl>
    </FormItem>
  )}
/>
```

The shared drawer is used for both create and edit, so no duplicate control is needed.

- [ ] **Step 3: Add translations to all locale files**

Use these flat English keys and localized values:

| Locale | `Disable Non-Stream` | `Reject non-stream Chat Completions and Responses requests for this channel` |
|---|---|---|
| en | Disable Non-Stream | Reject non-stream Chat Completions and Responses requests for this channel |
| zh | 禁用非流 | 拒绝此渠道的非流式 Chat Completions 和 Responses 请求 |
| zh-TW | 停用非串流 | 拒絕此渠道的非串流 Chat Completions 和 Responses 請求 |
| fr | Désactiver le mode non-streaming | Rejeter les requêtes Chat Completions et Responses non-streaming pour ce canal |
| ru | Запретить нестриминговые запросы | Отклонять нестриминговые запросы Chat Completions и Responses для этого канала |
| ja | 非ストリーミングを無効化 | このチャネルの非ストリーミング Chat Completions および Responses リクエストを拒否します |
| vi | Tắt yêu cầu không phát trực tiếp | Từ chối yêu cầu Chat Completions và Responses không phát trực tiếp cho kênh này |

Use the project i18n editing workflow and keep JSON formatting stable.

- [ ] **Step 4: Synchronize i18n and run affected frontend checks**

Run from `web/`:

```bash
bun run i18n:sync
bun run test -- src/features/channels/lib/__tests__/channel-non-stream-setting.test.ts
bun run typecheck
bunx oxlint -c .oxlintrc.json \
  src/features/channels/types.ts \
  src/features/channels/lib/channel-form.ts \
  src/features/channels/lib/__tests__/channel-non-stream-setting.test.ts \
  src/features/channels/components/drawers/channel-mutate-drawer.tsx
```

Expected: i18n reports zero missing, extra, and untranslated keys for all locales; tests, typecheck, and affected-file lint exit 0.

- [ ] **Step 5: Commit the channel UI**

```bash
git add web/src/features/channels/components/drawers/channel-mutate-drawer.tsx web/src/i18n/locales/*.json
git commit -m "feat(web): configure channel non-stream restriction"
```

---

### Task 5: Full Verification and Handoff

**Files:**
- Verify only; modify production files only if a test exposes a defect within this feature's scope.

**Interfaces:**
- Verifies all contracts produced by Tasks 1–4.

- [ ] **Step 1: Run backend and relaykit verification**

```bash
go test ./controller ./model ./middleware ./service ./relay/...
cd relaykit && GOWORK=off go test ./...
cd relaykit && GOWORK=off go build ./...
```

Expected: all commands exit 0.

- [ ] **Step 2: Run complete frontend verification**

From `web/`:

```bash
bun run test
bun run typecheck
bun run build
```

Expected: all commands exit 0 with zero failed tests.

- [ ] **Step 3: Verify formatting and repository state**

```bash
git diff --check
git status --short --branch
git log -8 --oneline
```

Expected: no whitespace errors and no uncommitted feature files.

- [ ] **Step 4: Review the requirement checklist**

Confirm from code and tests:

- default-off channel setting persists without migration;
- create and edit use the same switch;
- only Chat Completions and Responses are blocked;
- omitted/false stream is blocked and true is allowed;
- response is HTTP 400 with the fixed message;
- the branch exits before upstream dispatch, retry, and channel-error processing;
- relaykit remains independently buildable;
- all seven locales are complete.

- [ ] **Step 5: Present branch integration options**

Use `superpowers:finishing-a-development-branch` and offer local merge, push/PR, keep-as-is, or discard. Do not push or create a PR without explicit user choice.
