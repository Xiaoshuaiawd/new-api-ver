# Error Model Redaction Design

## Goal

Allow administrators to configure multiple regular expressions that identify model names whose values must be hidden in relay error text. Matching model names are replaced with `****` in downstream error responses, persisted error-log content, and relay/channel application logs.

For example, with the rule `^gpt.*`:

```text
status_code=400, unknown provider for model gpt-5.6-luna
```

becomes:

```text
status_code=400, unknown provider for model ****
```

## Scope

- Add an administrator-editable setting under **System Settings → Security & Limits**.
- Accept multiple Go regular expressions, one per entry.
- Validate every expression before persisting the setting.
- Evaluate both the client-requested model name and the mapped upstream model name.
- Replace exact occurrences of matching model names in error text with `****`.
- Apply redaction to:
  - errors returned to downstream clients;
  - persisted error-log content;
  - relay and channel application-log messages.
- Preserve the independent model field on error-log records so administrators can continue filtering logs by the actual model.

## Non-goals

- Do not redact model names in successful responses or usage logs.
- Do not alter retry, channel-disable, violation-fee, or error-classification behavior.
- Do not use configured expressions to scan arbitrary error text directly.
- Do not change the stored model field on an error-log row.

## Backend Design

### Configuration

Introduce a registered setting containing a list of regular-expression strings. The runtime representation keeps both the source strings and their compiled expressions. Configuration updates are atomic: all expressions must compile before the active configuration changes.

An empty rule list disables redaction. Duplicate expressions may be normalized away, and blank entries are ignored.

### Redaction

The redaction function receives:

- the error text;
- the original model name;
- the mapped upstream model name.

For each non-empty distinct model name, it checks whether the complete model name matches any configured expression. A match is accepted only when the expression matches the full model name, even if the administrator omitted anchors. Matching model names are then replaced literally in the error text with `****`.

This separates selection from replacement: expressions decide which known model names are sensitive, while literal replacement avoids consuming punctuation or unrelated error details.

### Relay Integration

Keep the original `NewAPIError` untouched throughout retries and internal decisions. At output and logging boundaries, create or derive a redacted view of its message:

1. Channel processing continues using the original error for retry and automatic-disable decisions.
2. Relay/channel application logs render redacted text.
3. Database error-log content receives redacted text while its `ModelName` column remains unchanged.
4. The final downstream error payload uses the redacted message before adding the request ID.

This preserves internal behavior while preventing disclosure through externally visible or persisted error text.

## Frontend Design

Add an **Error Model Redaction** section under **Security & Limits**. It contains a multiline rule editor with one regular expression per line, explanatory text, an example such as `^gpt.*`, and the existing settings save/error feedback patterns.

All user-facing text is added to every supported locale. The interface must remain keyboard accessible and must clearly report invalid expressions returned by the backend.

## Error Handling

- Invalid regular expressions are rejected with HTTP 400 and identify the invalid entry.
- Failed saves do not change the active runtime rules.
- Empty model names and empty error strings are safe no-ops.
- Repeated or overlapping model names are replaced deterministically, longest first, so a shorter name cannot partially mask a longer one.

## Tests

Backend tests cover:

- multiple valid expressions;
- invalid-expression rejection without changing active configuration;
- original and mapped model-name matching;
- literal replacement with punctuation preserved;
- no-op behavior for nonmatching models;
- downstream error text and persisted error-log content redaction;
- preservation of the error-log model field;
- original error retention for internal retry/disable decisions.

Frontend tests cover rule parsing, rendering, editing, saving, and invalid-rule feedback where the existing settings test infrastructure supports those behaviors.

Verification includes affected Go tests, frontend tests, type checking, affected-file lint, production build, and `git diff --check`.
