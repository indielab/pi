package providers

import (
	"bytes"
	"encoding/json"
)

// openai-completions `reasoning_details` — OpenRouter's structured reasoning
// replay channel (pi openai-completions.ts, upstream b7bb00b93).
//
// A detail is provider-opaque: we validate its shape, never its contents, and
// carry every member — including ones pi has no name for — so the sequence
// replays whole. The whole sequence is serialized into the thinking block's
// thinkingSignature, which is also the session-format slot — see
// ai.ThinkingContent.ThinkingSignature.
//
// "Replayed unmodified" is OpenRouter's requirement, but the bytes pi replays
// are not the provider's bytes: pi parses the detail out of the SSE line and
// the SDK re-serializes the resulting object, so `1.0` becomes `1`, `é`
// becomes `é` and a repeated key collapses. Details therefore go through
// jsStringify (JSON.stringify ∘ JSON.parse) rather than json.Compact, which
// would keep the provider's spelling and diverge from pi on the wire.

// jsonStringValue decodes raw as a JSON string. Unlike json.Unmarshal into a
// string it rejects `null`, which would otherwise decode to "" and make a null
// field indistinguishable from an empty one (pi's `typeof x === "string"`).
func jsonStringValue(raw json.RawMessage) (string, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(trimmed, &s) != nil {
		return "", false
	}
	return s, true
}

// isJSONString reports whether raw is a JSON string (pi `typeof x === "string"`).
func isJSONString(raw json.RawMessage) bool {
	_, ok := jsonStringValue(raw)
	return ok
}

// isJSONNull reports whether raw is the literal `null`.
func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// isJSONNumber reports whether raw is a JSON number (pi `typeof x === "number"`).
//
// The leading-byte screen is what rejects non-numbers: decoding into
// json.Number keeps the literal instead of converting it, and encoding/json
// accepts a QUOTED number there ("1"), which is a string to `typeof`. Keeping
// the literal is the point — a magnitude json.Parse saturates to ±Infinity is
// still `typeof "number"` in JS, so `1e400` must be accepted, where decoding
// into float64 would fail with "number out of range".
func isJSONNumber(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	if c := trimmed[0]; c != '-' && (c < '0' || c > '9') {
		return false
	}
	var n json.Number
	return json.Unmarshal(trimmed, &n) == nil
}

// reasoningDetailFields decodes a reasoning detail's members.
//
// Port of pi isReasoningDetailObject: only a JSON object qualifies. `null` and
// arrays are rejected explicitly — `typeof null === "object"` and
// `typeof [] === "object"` in JS, and json.Unmarshal of `null` into a map
// succeeds with a nil map, so neither is caught by decoding alone.
func reasoningDetailFields(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(trimmed, &fields) != nil {
		return nil, false
	}
	return fields, true
}

// hasValidCommonReasoningDetailFields checks the members every detail shape may
// carry (pi hasValidCommonReasoningDetailFields): `id` absent, null or string;
// `format` absent or string; `index` absent or number. Note the asymmetry — a
// null `id` is accepted but a null `format` or `index` is not.
func hasValidCommonReasoningDetailFields(fields map[string]json.RawMessage) bool {
	if id, ok := fields["id"]; ok && !isJSONNull(id) && !isJSONString(id) {
		return false
	}
	if format, ok := fields["format"]; ok && !isJSONString(format) {
		return false
	}
	if index, ok := fields["index"]; ok && !isJSONNumber(index) {
		return false
	}
	return true
}

// isOpenAIReasoningDetail reports whether raw is one of the three reasoning
// detail shapes pi accepts (openai-completions.ts isOpenAIReasoningDetail):
//
//	{"type":"reasoning.summary",   "summary": string}
//	{"type":"reasoning.encrypted", "data":    string}
//	{"type":"reasoning.text",      "text":    string, "signature"?: string|null}
//
// plus valid common fields. Any other type — and any non-object — is rejected.
func isOpenAIReasoningDetail(raw json.RawMessage) bool {
	fields, ok := reasoningDetailFields(raw)
	if !ok || !hasValidCommonReasoningDetailFields(fields) {
		return false
	}
	detailType, _ := jsonStringValue(fields["type"])
	switch detailType {
	case "reasoning.summary":
		return isJSONString(fields["summary"])
	case "reasoning.encrypted":
		return isJSONString(fields["data"])
	case "reasoning.text":
		if !isJSONString(fields["text"]) {
			return false
		}
		signature, ok := fields["signature"]
		return !ok || isJSONNull(signature) || isJSONString(signature)
	default:
		return false
	}
}

// parseOpenAIReasoningDetails reads the reasoning-detail sequence serialized in
// a thinking block's signature (pi parseOpenAIReasoningDetails). It returns nil
// unless the signature is a JSON array that is non-empty and whose every entry
// is a valid detail — so an ordinary opaque signature (a reasoning field name,
// a Responses item id, a base64 blob) is simply not a sequence.
func parseOpenAIReasoningDetails(signature string) []json.RawMessage {
	if signature == "" {
		return nil
	}
	var details []json.RawMessage
	// `null` decodes into a slice without error but is not an array to
	// Array.isArray; the length check below covers it along with `[]`.
	if json.Unmarshal([]byte(signature), &details) != nil || len(details) == 0 {
		return nil
	}
	for i, detail := range details {
		if !isOpenAIReasoningDetail(detail) {
			return nil
		}
		details[i] = stringifyReasoningDetail(detail)
	}
	return details
}

// parseLegacyEncryptedReasoningDetail reads the pre-b7bb00b93 session shape: a
// tool call's thoughtSignature holding ONE encrypted detail (pi
// parseLegacyEncryptedReasoningDetail). Beyond being a valid detail it must be
// `reasoning.encrypted` with a non-empty id AND non-empty data, so the loose
// "anything that parses" signatures older code stored are not replayed.
func parseLegacyEncryptedReasoningDetail(signature string) (json.RawMessage, bool) {
	if signature == "" {
		return nil, false
	}
	raw := json.RawMessage(signature)
	if !isOpenAIReasoningDetail(raw) {
		return nil, false
	}
	fields, _ := reasoningDetailFields(raw)
	if detailType, _ := jsonStringValue(fields["type"]); detailType != "reasoning.encrypted" {
		return nil, false
	}
	if id, ok := jsonStringValue(fields["id"]); !ok || id == "" {
		return nil, false
	}
	if data, _ := jsonStringValue(fields["data"]); data == "" {
		return nil, false
	}
	return stringifyReasoningDetail(raw), true
}

// marshalOpenAIReasoningDetails serializes a detail sequence back into a
// thinking signature. Entries are already JSON.stringify-shaped, and
// stringifying an array is its elements joined by commas, so a plain join IS
// pi's JSON.stringify of the array it just parsed and pushed onto.
func marshalOpenAIReasoningDetails(details []json.RawMessage) string {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, detail := range details {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(detail)
	}
	buf.WriteByte(']')
	return buf.String()
}

// stringifyReasoningDetail renders a detail the way pi's SDK renders the object
// it parsed out of the SSE line: `JSON.stringify(JSON.parse(detail))`. Unknown
// members and their nesting survive whole, which is the property OpenRouter
// needs; what does NOT survive is the provider's own spelling of a value pi
// never had either — redundant number forms, `\uXXXX` escapes of characters
// JSON.stringify emits literally, and a repeated key.
//
// Invalid JSON is returned unchanged; every caller here has already validated
// its input, so the fallback never fires in practice.
func stringifyReasoningDetail(raw json.RawMessage) json.RawMessage {
	rendered, ok := jsStringify(raw)
	if !ok {
		return raw
	}
	return json.RawMessage(rendered)
}

// isOpenAICompletionsReasoningField reports whether a thinking signature names
// a raw reasoning field on the assistant message (pi
// isOpenAICompletionsReasoningField). Signatures outside this set are replay
// data — an OpenAI Responses item id, an anthropic blob, a serialized detail
// sequence — and must never be used as a request key.
func isOpenAICompletionsReasoningField(field string) bool {
	switch field {
	case "reasoning", "reasoning_content", "reasoning_text":
		return true
	default:
		return false
	}
}
