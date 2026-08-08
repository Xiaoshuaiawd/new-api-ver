# Juice Value Fixer Design

## Goal

Add an administrator-controlled response postprocessor that replaces the numeric Juice value in selected OpenAI-compatible responses. It must cover `/v1/responses` and `/v1/chat/completions`, in both streaming and non-streaming modes.

The feature changes only the answer text containing the Juice number. It does not change reasoning content, `usage`, token counts, response metadata, billing, or upstream requests.

## Triggering

The request is eligible when either of these is true:

1. The latest user message matches a Juice-number query.
2. Any system message matches a Juice-related query or instruction.

Matching is case-insensitive and whitespace/punctuation tolerant. A bounded keyword/regular-expression matcher recognizes the English and Chinese variants supplied in the initial requirements, including `juice`, `J U I C E`, `果汁值`, requests for a number, and attempts to expose the hidden system Juice value. It does not require both message types to match.

The matcher receives normalized text from the request DTO rather than scanning serialized JSON, so it can handle chat messages and Responses `input` items consistently. Tool messages and assistant history are not triggers unless they are represented as system content by the normalized request.

## Configuration

Persist one JSON configuration object in the existing option storage mechanism, following the Prompt Guard configuration pattern. The configuration contains an enabled flag and a list/map of rules:

```json
{
  "enabled": true,
  "rules": [
    {"model": "gpt-5.6-sol", "reasoning_effort": "low", "value": 8}
  ]
}
```

`value` is a non-negative integer within the response text range accepted by the API. Rule lookup uses exact model and exact `reasoning_effort`; there is no fallback. When disabled, unmatched, invalid, or absent, responses pass through unchanged. Invalid configuration updates are rejected atomically and the previous valid configuration remains active.

Expose administrator-only endpoints:

```text
GET /api/juice-fixer/config
PUT /api/juice-fixer/config
```

The public response omits no functional fields because the configuration contains no secrets. The admin UI adds a System Settings section for enablement and CRUD of model/effort/value rules, with i18n strings in all supported locales.

## Response Pipeline

Implement a reusable `service`/`relay/helper` postprocessor with three responsibilities:

1. Build a `JuiceMatchContext` from the normalized request and resolved model/effort.
2. Decide whether a configured rule applies.
3. Replace only the Juice number in response text.

The postprocessor is called from the common OpenAI relay response path, after the upstream payload has been decoded and before it is written to the downstream client. This keeps provider adapters unchanged and gives both requested endpoints identical behavior.

### Non-streaming

Decode the JSON response with the project `common.Unmarshal` wrapper. For Chat Completions, process text in `choices[*].message.content`; for Responses, process text in output message content parts. Preserve all other JSON fields and re-marshal with `common.Marshal`. If decoding or content extraction fails, pass through the original body.

### Streaming

Apply the postprocessor in the shared SSE scanner. Preserve SSE event names, comments, ordering, and protocol headers. For each eligible text field, buffer only the text needed to identify and replace a Juice number; at stream completion flush the transformed text as the same delta/output events. Reasoning fields and non-text events are emitted byte-for-byte unchanged. If the stream ends without a replaceable number, emit the buffered text unchanged.

For a sentence such as `The Juice number is 12.`, replace only `12` and retain the sentence. For a numeric-only answer, replace the complete numeric token. Numeric matching is bounded to standalone integer/decimal tokens adjacent to Juice wording; unrelated numbers are not modified.

## Failure Handling and Observability

The feature is best-effort and must never turn a successful upstream response into an API error. Configuration validation errors return a normal admin API validation error. Runtime matcher, decoder, or replacement errors log at debug level and return the original response. No request or response content is logged.

## Testing

Add deterministic tests for:

- English, Chinese, casing, spacing, punctuation, XML, and system/user trigger variants.
- User-only and system-only activation, plus non-triggering normal prompts.
- Exact model + effort matching and no-fallback behavior.
- Sentence and numeric-only replacement while preserving surrounding text.
- Chat Completions and Responses non-streaming payloads.
- Chat Completions and Responses SSE streams, including reasoning events and usage preservation.
- Invalid/atomic configuration updates and disabled/pass-through behavior.

