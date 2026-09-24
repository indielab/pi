package providers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
)

// maxProviderErrorBodyChars caps the surfaced HTTP error body, matching pi's
// MAX_PROVIDER_ERROR_BODY_CHARS (error-body.ts). Upstream 6fbeba51 added this
// cap so a verbose proxy/gateway error body cannot dominate the surfaced
// message (the string can land in a recorded error turn's session JSON).
const maxProviderErrorBodyChars = 4000

// providerStoppedPrefix is the prefix pi puts in front of a provider's own
// terminal stop reason (anthropic-messages.ts, google-generative-ai.ts). This
// package reproduces pi's user-facing strings byte-exactly, capitalization
// included.
const providerStoppedPrefix = "Provider stopped with: "

// truncateErrorText ports pi's truncateErrorText (error-body.ts). JS measures
// with String.length / String.slice, i.e. UTF-16 code units, so the cap and the
// "[truncated N chars]" count are UTF-16-unit based, not byte- or rune-based.
// The suffix string is matched byte-exactly.
func truncateErrorText(text string, maxChars int) string {
	units := utf16.Encode([]rune(text))
	if len(units) <= maxChars {
		return text
	}
	head := string(utf16.Decode(units[:maxChars]))
	return fmt.Sprintf("%s... [truncated %d chars]", head, len(units)-maxChars)
}

// formatProviderError builds a concise error from an HTTP error response,
// extracting the provider's structured error message when present (OpenAI,
// Anthropic, and Google all nest it under "error": {"message": ...}).
//
// Architecture note (upstream 6fbeba51): pi's normalizeProviderError recovers
// the HTTP status and raw body from the JS provider SDKs' opaque error objects
// (.statusCode/.error/.body/$response/$metadata). The Go port issues raw HTTP
// requests and already holds resp.StatusCode and the raw body here, so the
// field-probing half is N/A — the #5763 "opaque, no body" bug cannot occur. Its
// other half is observable, though: for the openai SDK, a parsed `error` object
// REPLACES the SDK's message in what the user sees. formatResponsesHTTPError
// ports that composition against a captured oracle; this function keeps the
// port's own `<Label> API error <status>: <msg>` shape for the completions,
// google and anthropic call sites (docs/UPSTREAM.md D20 and K18) and applies
// 6fbeba51's 4000-char cap to the body-derived message.
func formatProviderError(label string, status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	var parsed struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Error.Message != "" {
		msg = parsed.Error.Message
		if parsed.Error.Code != "" {
			msg = fmt.Sprintf("%s (%s)", msg, parsed.Error.Code)
		}
	}
	msg = truncateErrorText(msg, maxProviderErrorBodyChars)
	return fmt.Errorf("%s API error %d: %s", label, status, msg)
}

// formatResponsesHTTPError ports the error message pi's OpenAI Responses
// provider surfaces for a non-2xx HTTP response — the openai-responses.ts catch
// (198-201): formatProviderError(normalizeProviderError(err), prefix), where err
// is the openai SDK's APIError and the prefix names the provider, "OpenAI" for
// provider "openai" and the id verbatim otherwise (upstream 0c7bb7c5c, #9298: a
// Grok 403 used to read as an OpenAI error).
//
// Composition, byte-exact to the captured oracle in testdata/httperror: the
// SDK message is `${status} ${msg}` (openaiSDKErrorMessage); when the body's
// `error` member is a non-empty JSON object, its JSON.stringify form, capped at
// maxProviderErrorBodyChars, replaces the message unless the message already
// contains it (error-body.ts messageCarriesBody). So
// `{"error":{"message":"blocked"}}` reads `xai API error (403): {"message":"blocked"}`
// while `{"error":"boom"}` reads `xai API error (403): 403 "boom"`.
func formatResponsesHTTPError(provider string, status int, body []byte) error {
	label := provider
	if provider == "openai" {
		label = "OpenAI"
	}
	message := openaiSDKErrorMessage(status, body)
	if errBody := responsesErrorBody(body); errBody != "" && !strings.Contains(message, errBody) {
		return fmt.Errorf("%s API error (%d): %s", label, status, errBody)
	}
	return fmt.Errorf("%s API error (%d): %s", label, status, message)
}

// responsesErrorBody ports the body half of pi's normalizeProviderError for the
// openai SDK's APIError (error-body.ts pickBodyText/extractBody): the parsed
// body's `error` member when it is a non-empty plain object, stringified the way
// JSON.stringify does and capped at maxProviderErrorBodyChars. Every other
// shape — a string, array, empty or null `error`, no `error` key, a non-object
// or non-JSON body — yields "" (pi: undefined), and the SDK message stands.
func responsesErrorBody(body []byte) string {
	var top map[string]json.RawMessage
	if json.Unmarshal(stripBOM(body), &top) != nil {
		return ""
	}
	raw, ok := top["error"]
	if !ok {
		return ""
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil || len(members) == 0 {
		return ""
	}
	text, ok := jsStringify(raw)
	if !ok {
		return ""
	}
	return truncateErrorText(text, maxProviderErrorBodyChars)
}

// openaiSDKErrorMessage replicates openai SDK APIError.makeMessage plus the
// client's body handling: the body is parsed as JSON and, when the parsed value
// is truthy, `error` is its `error` member (so only an object body can carry
// one); the message is error.message (JSON.stringify'd when not a string), else
// JSON.stringify(error) when error is truthy, else the raw body text — which
// the client also passes through when the body was not JSON or parsed to a
// falsy value (`errJSON ? undefined : errText`). Nothing here is capped — pi's
// cap applies to the body half only (see responsesErrorBody). Stringification
// goes through jsStringify so key order and escaping are JavaScript's, not
// encoding/json's.
func openaiSDKErrorMessage(status int, body []byte) string {
	body = stripBOM(body)
	var msg string
	if !json.Valid(body) || !rawTruthy(body) {
		msg = string(body)
	} else {
		var top map[string]json.RawMessage
		if json.Unmarshal(body, &top) == nil {
			msg = openaiSDKErrorDetail(top["error"])
		}
	}
	if msg == "" {
		return fmt.Sprintf("%d status code (no body)", status)
	}
	return fmt.Sprintf("%d %s", status, msg)
}

// openaiSDKErrorDetail is the text openai SDK APIError.makeMessage builds from
// its `error` argument, given as JSON (nil when absent): error.message when it
// is a truthy string, JSON.stringify(error.message) when it is truthy
// otherwise, else JSON.stringify(error) — or "" when error is falsy, where
// makeMessage falls back to its status and message arguments.
func openaiSDKErrorDetail(errorValue json.RawMessage) string {
	if !rawTruthy(errorValue) {
		return ""
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(errorValue, &members) == nil {
		if m, has := members["message"]; has && rawTruthy(m) {
			var str string
			if json.Unmarshal(m, &str) == nil {
				return str
			}
			if j, ok := jsStringify(m); ok {
				return j
			}
		}
	}
	j, _ := jsStringify(errorValue)
	return j
}

// stripBOM is the one piece of fetch's text() decode that the SDK-level
// functions above see: one leading UTF-8 BOM is dropped before the client ever
// parses, on the terminal path and on the retry fail-fast quote alike. The rest
// of that decode — invalid UTF-8 becoming U+FFFD per maximal subpart — is not
// ported; the raw bytes pass through (docs/UPSTREAM.md K18).
func stripBOM(body []byte) []byte {
	return bytes.TrimPrefix(body, []byte("\xEF\xBB\xBF"))
}

// rawTruthy reports JavaScript truthiness for a well-formed JSON value without
// decoding it: null, false, 0 and "" are falsy; everything else — objects and
// arrays even when empty, and any non-zero number including the ones JSON.parse
// overflows to Infinity, which encoding/json cannot decode — is truthy.
func rawTruthy(raw json.RawMessage) bool {
	v := bytes.TrimSpace(raw)
	if len(v) == 0 {
		return false
	}
	switch v[0] {
	case 'n', 'f':
		return false
	case 't', '{', '[':
		return true
	case '"':
		return len(v) > 2
	}
	f, err := strconv.ParseFloat(string(v), 64)
	if err != nil {
		return errors.Is(err, strconv.ErrRange) // overflow: JS Infinity, truthy
	}
	return f != 0
}

// anthropicSDKErrorMessage replicates the Anthropic SDK's APIError message for
// a non-2xx response. Both SDKs share a byte-identical makeMessage; they differ
// in APIError.generate, which decides what makeMessage receives as `error`:
// openai passes `errorResponse['error']` (so the message is the nested
// error.message), while anthropic passes the WHOLE parsed body. For a
// conformant anthropic body — {"type":"error","error":{...}} — the body has no
// top-level `message`, so makeMessage falls through to JSON.stringify(body) and
// the result is `${status} {"type":"error",...}`, not the nested message.
func anthropicSDKErrorMessage(status int, body []byte) string {
	errText := string(body)
	var errJSON any
	// safeJSON: JSON.parse in a try, with no blank check of its own.
	jsonOK := json.Unmarshal(body, &errJSON) == nil

	var msg string
	switch {
	case !jsonOK:
		msg = errText
	default:
		if obj, ok := errJSON.(map[string]any); ok {
			if m, has := obj["message"]; has && jsTruthy(m) {
				if s, ok := m.(string); ok {
					msg = s
				} else if j, err := json.Marshal(m); err == nil {
					msg = string(j)
				}
			}
		}
		// `error ? JSON.stringify(error) : message` — the whole body, with JS
		// key ordering and escaping rather than encoding/json's.
		if msg == "" && jsTruthy(errJSON) {
			if s, ok := jsStringify(body); ok {
				msg = s
			}
		}
	}
	if msg == "" {
		return fmt.Sprintf("%d status code (no body)", status)
	}
	msg = truncateErrorText(msg, maxProviderErrorBodyChars)
	return fmt.Sprintf("%d %s", status, msg)
}

// jsTruthy reports JavaScript truthiness for a JSON-decoded value.
func jsTruthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0
	default:
		return true // objects and arrays are always truthy
	}
}
