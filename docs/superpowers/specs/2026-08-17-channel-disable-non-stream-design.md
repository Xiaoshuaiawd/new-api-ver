# Channel Disable Non-Stream Design

## Goal

Add a channel-level **Disable Non-Stream** switch. When enabled, the selected channel accepts streaming Chat Completions and Responses requests but rejects synchronous requests before they reach the upstream provider.

The switch is available when administrators create or edit a channel and is disabled by default.

## Scope

- Add a channel setting named `disable_non_stream`.
- Expose the setting in both channel creation and channel editing.
- Apply the restriction only to:
  - Chat Completions;
  - Responses.
- Treat `stream: false` and an omitted `stream` field as non-stream requests.
- Return HTTP 400 with the existing OpenAI-compatible error envelope and this fixed message:

```text
当前号池正在遭受恶意毁号请求，暂停同步请求
```

## Non-goals

- Do not restrict Completions, Claude Messages, Gemini GenerateContent, Embeddings, Rerank, images, audio, realtime, Responses Compact, Alpha Search, or asynchronous task APIs.
- Do not automatically reroute a rejected request to another channel.
- Do not disable the channel or count the rejection as an upstream channel failure.
- Do not alter streaming requests.
- Do not add a database column or migration.

## Backend Design

### Channel Configuration

Add `DisableNonStream bool` to the existing `dto.ChannelSettings` structure and serialize it as `disable_non_stream` with `omitempty`. The setting remains inside the channel's existing `setting` JSON, so old channels and empty settings continue to behave as `false` without schema changes.

The existing channel validation, save, cache, and distributor context paths already transport `ChannelSettings`; no parallel configuration mechanism is needed.

### Enforcement Point

Enforce the policy after a channel has been selected and its settings have been loaded into the request context, but before the request is sent to the upstream provider.

This placement is intentional:

1. The request has already been parsed, so the effective client `stream` value and relay mode are known.
2. The selected channel's `disable_non_stream` value is available.
3. Returning at this boundary prevents upstream traffic.
4. The error is marked skip-retry, preventing fallback to a different channel.
5. The rejection is not passed to channel-error processing, so it cannot trigger automatic channel disabling.

The check applies only when both conditions hold:

- relay mode is Chat Completions or Responses;
- `RelayInfo.IsStream` is false.

Responses Compact is explicitly excluded even though it shares parts of the Responses implementation.

### Error Contract

Create an invalid-request error with HTTP 400 and skip-retry semantics. The controller's existing deferred response handling renders it through the OpenAI-compatible error envelope and appends the request ID using the normal project behavior.

The message body is exactly:

```text
当前号池正在遭受恶意毁号请求，暂停同步请求
```

The rejection must happen before relay/channel error processing. It therefore does not represent an upstream failure and must not affect automatic channel-disable logic.

### Billing Behavior

The current relay flow performs channel selection after pre-consumption. A rejected request therefore exits through the existing deferred refund path. No upstream request is made and no actual consumption is recorded.

## Frontend Design

Add a `disable_non_stream` boolean to the channel form schema, defaults, parsing, dirty-state/configured-state calculation, and payload serialization.

Render a switch in the existing **Channel Extra Settings** group for both add and edit modes:

- Label: **Disable Non-Stream**
- Description: explain that enabling the option rejects non-stream Chat Completions and Responses requests for this channel.
- Default: off

The control follows the existing permission lock, submitting state, `FormField`, and `Switch` patterns. No new UI dependency or layout pattern is introduced.

All user-facing text is added to every supported locale.

## Data Flow

1. Administrator creates or edits a channel and toggles **Disable Non-Stream**.
2. The frontend serializes `disable_non_stream` into the existing channel `setting` JSON.
3. The backend persists and caches the channel through the existing channel workflow.
4. A downstream Chat Completions or Responses request is parsed and its stream state is recorded in `RelayInfo.IsStream`.
5. The distributor selects a channel and places its `ChannelSettings` in the request context.
6. Before relay dispatch, the policy check rejects a non-stream request when the selected channel has the switch enabled.
7. The controller returns HTTP 400, skips retry and upstream traffic, and the existing billing cleanup refunds any pre-consumed quota.

## Tests

Backend regression tests cover:

- enabled channel + non-stream Chat Completions is rejected;
- enabled channel + non-stream Responses is rejected;
- enabled channel + streaming Chat Completions is allowed;
- enabled channel + streaming Responses is allowed;
- omitted `stream` is treated as non-stream;
- disabled/default setting does not reject;
- unrelated relay modes are not rejected;
- rejection is HTTP 400 and skip-retry;
- fixed downstream message is preserved;
- channel settings JSON round-trips the new field.

Frontend tests cover:

- new-channel default is off;
- existing channel settings populate the switch;
- create and update payloads serialize the value;
- legacy settings without the field remain off.

Verification includes affected Go tests, affected frontend tests, i18n synchronization, TypeScript type checking, affected-file lint, production build, and `git diff --check`.
