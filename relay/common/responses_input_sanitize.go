package common

import (
	"encoding/json"
	"strings"

	projectcommon "github.com/QuantumNous/new-api/common"
)

var responsesInputIDPrefixes = map[string]string{
	"message":                 "msg",
	"function_call":           "fc",
	"function_call_output":    "fco",
	"custom_tool_call":        "ctc",
	"custom_tool_call_output": "ctco",
	"reasoning":               "rs",
	"agent_message":           "amsg",
}

// SanitizeResponsesInputItemIDs removes IDs that cannot be accepted by the
// Responses API for their input item type. Clients may replay local item_* IDs
// from a different provider; omitting those IDs lets the upstream assign one.
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
		itemType := strings.TrimSpace(projectcommon.Interface2String(item["type"]))
		prefix, knownType := responsesInputIDPrefixes[itemType]
		if !knownType {
			continue
		}
		id, hasID := item["id"].(string)
		if hasID && !strings.HasPrefix(id, prefix) {
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
