package common

import (
	"encoding/json"

	projectcommon "github.com/QuantumNous/new-api/common"
)

// SanitizeResponsesInputItemIDs removes all item IDs from array-form Responses
// input. Replaying upstream IDs can also replay hidden dependencies between an
// assistant message and its reasoning item, which may not be present in input.
// The upstream assigns fresh IDs while call_id remains available for tool links.
func SanitizeResponsesInputItemIDs(raw []byte) ([]byte, bool, error) {
	if projectcommon.GetJsonType(raw) != "array" {
		return raw, false, nil
	}

	var items []map[string]any
	if err := projectcommon.Unmarshal(raw, &items); err != nil {
		return nil, false, err
	}

	changed := false
	for _, item := range items {
		if _, hasID := item["id"]; hasID {
			delete(item, "id")
			changed = true
		}
	}
	if !changed {
		return raw, false, nil
	}

	sanitized, err := projectcommon.Marshal(items)
	return sanitized, true, err
}

// SanitizeResponsesRequestBody applies input-ID normalization while retaining
// all other raw request fields for pass-through channels.
func SanitizeResponsesRequestBody(raw []byte) ([]byte, bool, error) {
	if projectcommon.GetJsonType(raw) != "object" {
		return raw, false, nil
	}

	var body map[string]json.RawMessage
	if err := projectcommon.Unmarshal(raw, &body); err != nil {
		return nil, false, err
	}
	input, found := body["input"]
	if !found {
		return raw, false, nil
	}
	sanitizedInput, changed, err := SanitizeResponsesInputItemIDs(input)
	if err != nil || !changed {
		return raw, false, err
	}
	body["input"] = sanitizedInput
	sanitizedBody, err := projectcommon.Marshal(body)
	return sanitizedBody, true, err
}
