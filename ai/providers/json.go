package providers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/sky-valley/pi/ai"
)

var validJSONEscapes = map[byte]bool{
	'"': true, '\\': true, '/': true, 'b': true, 'f': true,
	'n': true, 'r': true, 't': true, 'u': true,
}

func isHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// repairJSON escapes raw control characters inside strings and doubles
// backslashes before invalid escape characters (port of repairJson).
func repairJSON(s string) string {
	var b strings.Builder
	inString := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if !inString {
			b.WriteByte(ch)
			if ch == '"' {
				inString = true
			}
			continue
		}
		if ch == '"' {
			b.WriteByte(ch)
			inString = false
			continue
		}
		if ch == '\\' {
			if i+1 >= len(s) {
				b.WriteString("\\\\")
				continue
			}
			next := s[i+1]
			if next == 'u' && i+6 <= len(s) {
				hex := s[i+2 : i+6]
				if len(hex) == 4 && isHexDigit(hex[0]) && isHexDigit(hex[1]) && isHexDigit(hex[2]) && isHexDigit(hex[3]) {
					b.WriteString("\\u")
					b.WriteString(hex)
					i += 5
					continue
				}
			}
			if validJSONEscapes[next] {
				b.WriteByte('\\')
				b.WriteByte(next)
				i++
				continue
			}
			b.WriteString("\\\\")
			continue
		}
		if ch <= 0x1f {
			b.WriteString(escapeControlChar(ch))
		} else {
			b.WriteByte(ch)
		}
	}
	return b.String()
}

func escapeControlChar(ch byte) string {
	switch ch {
	case '\b':
		return "\\b"
	case '\f':
		return "\\f"
	case '\n':
		return "\\n"
	case '\r':
		return "\\r"
	case '\t':
		return "\\t"
	default:
		return fmt.Sprintf("\\u%04x", ch)
	}
}

// jsEscape renders s the way `JSON.stringify(s).slice(1, -1)` does in
// JavaScript: only `"`, `\` and control characters are escaped, and unpaired
// UTF-16 surrogates become \udXXX (ES2019 well-formed stringify). Go's
// encoding/json cannot be used here — it additionally escapes <, >, & and
// U+2028/U+2029.
//
// The lone-surrogate handling only reaches strings that did NOT arrive through
// encoding/json: Unmarshal replaces an unpaired surrogate escape with U+FFFD
// before this function ever sees it, and that loss is not recoverable here. It
// applies to values a caller built in Go (where a WTF-8 triple can survive), so
// the compensation is partial, not total.
func jsEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if hi, isSurrogate := decodeWTF8Surrogate(s[i:]); isSurrogate {
			// A well-formed pair is a single astral character in JS and is
			// emitted literally; a lone surrogate is escaped.
			if hi <= 0xDBFF {
				if lo, ok := decodeWTF8Surrogate(s[i+3:]); ok && lo >= 0xDC00 {
					b.WriteRune(utf16.DecodeRune(hi, lo))
					i += 6
					continue
				}
			}
			fmt.Fprintf(&b, "\\u%04x", hi)
			i += 3
			continue
		}
		switch ch := s[i]; {
		case ch == '"':
			b.WriteString(`\"`)
		case ch == '\\':
			b.WriteString(`\\`)
		case ch <= 0x1f:
			b.WriteString(escapeControlChar(ch))
		default:
			b.WriteByte(ch)
		}
		i++
	}
	return b.String()
}

// jsQuote renders s the way `JSON.stringify(s)` does, quotes included.
func jsQuote(s string) string { return `"` + jsEscape(s) + `"` }

// jsStringify renders a JSON document the way JavaScript's
// `JSON.stringify(JSON.parse(raw))` does. Go's encoding/json cannot be used:
// marshalling a decoded map sorts its keys, whereas JS reproduces the parsed
// object's own-property order, and Go escapes strings differently (see
// jsEscape). Returns ok=false when raw is not a single well-formed JSON value.
//
// One JS behavior survives only partially: an unpaired UTF-16 surrogate escape
// is replaced with U+FFFD by encoding/json before jsEscape can see it, where JS
// keeps the code unit and re-emits `\udXXX`. Not recoverable at this layer —
// see jsEscape.
func jsStringify(raw []byte) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var b strings.Builder
	if err := jsStringifyValue(dec, &b); err != nil {
		return "", false
	}
	// Reject trailing content, which JSON.parse would also reject.
	if _, err := dec.Token(); err != io.EOF {
		return "", false
	}
	return b.String(), true
}

func jsStringifyValue(dec *json.Decoder, b *strings.Builder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	return jsStringifyToken(dec, b, tok)
}

func jsStringifyToken(dec *json.Decoder, b *strings.Builder, tok json.Token) error {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			if err := jsStringifyObjectBody(dec, b); err != nil {
				return err
			}
		case '[':
			b.WriteByte('[')
			for i := 0; dec.More(); i++ {
				if i > 0 {
					b.WriteByte(',')
				}
				if err := jsStringifyValue(dec, b); err != nil {
					return err
				}
			}
			b.WriteByte(']')
		default:
			return fmt.Errorf("unexpected delimiter %q", t)
		}
		// Consume the matching closing delimiter.
		if _, err := dec.Token(); err != nil {
			return err
		}
	case string:
		b.WriteString(jsQuote(t))
	case json.Number:
		b.WriteString(jsNumber(t.String()))
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case nil:
		b.WriteString("null")
	default:
		return fmt.Errorf("unexpected token %T", tok)
	}
	return nil
}

// jsStringifyObjectBody renders the members of an object whose `{` has already
// been read, reproducing the two things JSON.parse does to an object literal
// that a streaming re-emit does not.
//
//   - A repeated key is ONE property: JSON.parse creates it at its first
//     occurrence and later occurrences only overwrite the value, so the last
//     value is kept in the first position.
//   - Own-property order is not insertion order. OrdinaryOwnPropertyKeys lists
//     array-index keys first, ascending numerically, and only then the rest in
//     creation order — so `{"type":"x","0":"y"}` re-serializes as
//     `{"0":"y","type":"x"}`.
//
// Members are rendered into a buffer before being emitted because both rules
// can move a member that has already been read.
func jsStringifyObjectBody(dec *json.Decoder, b *strings.Builder) error {
	type member struct {
		key     string
		index   uint32 // array-index value of key; meaningful when isIndex
		value   string
		isIndex bool
	}
	var members []member
	positions := map[string]int{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok {
			return fmt.Errorf("object key is not a string")
		}
		var value strings.Builder
		if err := jsStringifyValue(dec, &value); err != nil {
			return err
		}
		if at, seen := positions[name]; seen {
			members[at].value = value.String()
			continue
		}
		positions[name] = len(members)
		index, isIndex := jsArrayIndexKey(name)
		members = append(members, member{key: name, index: index, value: value.String(), isIndex: isIndex})
	}

	order := make([]int, 0, len(members))
	for i, m := range members {
		if m.isIndex {
			order = append(order, i)
		}
	}
	sort.Slice(order, func(x, y int) bool { return members[order[x]].index < members[order[y]].index })
	for i, m := range members {
		if !m.isIndex {
			order = append(order, i)
		}
	}

	b.WriteByte('{')
	for i, at := range order {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(jsQuote(members[at].key))
		b.WriteByte(':')
		b.WriteString(members[at].value)
	}
	b.WriteByte('}')
	return nil
}

// jsArrayIndexKey reports whether name is an array index in the ECMAScript
// sense — `ToString(ToUint32(name)) === name` and the value is not 2^32-1 — and
// returns that value. Those are the keys OrdinaryOwnPropertyKeys hoists to the
// front, so "0" and "12" qualify while "01", "-1", "1.0" and "4294967295" are
// ordinary string keys.
func jsArrayIndexKey(name string) (uint32, bool) {
	if name == "" || len(name) > 10 {
		return 0, false
	}
	if name[0] == '0' && len(name) > 1 {
		return 0, false // canonical form has no leading zeros
	}
	for i := 0; i < len(name); i++ {
		if name[i] < '0' || name[i] > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseUint(name, 10, 64)
	if err != nil || value >= math.MaxUint32 {
		return 0, false
	}
	return uint32(value), true
}

// jsNumber renders a JSON number the way JavaScript's Number-to-String
// conversion does, so `1.0` becomes "1" and `1e2` becomes "100". JS switches to
// exponential notation only outside [1e-7, 1e21); Go's %g switches on a
// different threshold, so the ranges are selected explicitly. Values that do not
// parse as a float at all are passed through unchanged.
func jsNumber(src string) string {
	f, err := strconv.ParseFloat(src, 64)
	if err != nil {
		// Out of float64 range is not a syntax error: JSON.parse yields
		// ±Infinity (or a signed zero), and JSON.stringify writes Infinity as
		// `null`. ParseFloat reports the same saturated value alongside
		// ErrRange, so only a genuinely malformed literal passes through.
		if !errors.Is(err, strconv.ErrRange) {
			return src
		}
		if math.IsInf(f, 0) {
			return "null"
		}
	}
	// JS String(-0) is "0", where Go's FormatFloat keeps the sign.
	if f == 0 {
		return "0"
	}
	// JS uses fixed notation on [1e-6, 1e21) and exponential outside it:
	// String(1e-6) is "0.000001" but String(9.9e-7) is "9.9e-7".
	if abs := math.Abs(f); abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	// Go pads the exponent to two digits ("1e-08"); JS does not ("1e-8").
	out := strconv.FormatFloat(f, 'e', -1, 64)
	if i := strings.IndexByte(out, 'e'); i >= 0 {
		mant, exp := out[:i], out[i+1:]
		sign := ""
		if len(exp) > 0 && (exp[0] == '+' || exp[0] == '-') {
			sign, exp = string(exp[0]), exp[1:]
		}
		exp = strings.TrimLeft(exp, "0")
		if exp == "" {
			exp = "0"
		}
		out = mant + "e" + sign + exp
	}
	return out
}

// orEmptyArguments returns a tool call's arguments for a request body, in the
// model's original key order, standing in an empty object for absent arguments
// (pi's `toolCall.arguments ?? {}`).
func orEmptyArguments(tc ai.ToolCall) any {
	if args := tc.OrderedArguments(); args != nil {
		return args
	}
	return map[string]any{}
}

// parseJSONWithRepair parses JSON, retrying once with repairs on failure.
func parseJSONWithRepair(s string, out any) error {
	if err := json.Unmarshal([]byte(s), out); err == nil {
		return nil
	}
	repaired := repairJSON(s)
	if repaired != s {
		return json.Unmarshal([]byte(repaired), out)
	}
	return json.Unmarshal([]byte(s), out) // return original error
}

// parseStreamingJSON parses potentially-incomplete JSON from streaming tool-call
// deltas, always returning a map (empty on total failure). Port of
// parseStreamingJson. The second return is the same object with the model's key
// order kept (nil when nothing parsed): pi's JS object preserves it for free,
// and the order is replayed into later requests.
func parseStreamingJSON(partial string) (map[string]any, ai.OrderedObject) {
	if strings.TrimSpace(partial) == "" {
		return map[string]any{}, nil
	}
	if out, order, err := ai.DecodeOrderedObject([]byte(partial)); err == nil {
		return out, order
	}
	if repaired := repairJSON(partial); repaired != partial {
		if out, order, err := ai.DecodeOrderedObject([]byte(repaired)); err == nil {
			return out, order
		}
	}
	if completed, ok := completePartialJSON(partial); ok {
		if out, order, err := ai.DecodeOrderedObject([]byte(completed)); err == nil {
			return out, order
		}
	}
	if completed, ok := completePartialJSON(repairJSON(partial)); ok {
		if out, order, err := ai.DecodeOrderedObject([]byte(completed)); err == nil {
			return out, order
		}
	}
	return map[string]any{}, nil
}

// completePartialJSON closes open strings, arrays, and objects in a truncated
// JSON document so it can parse. It approximates the partial-json library for
// the streaming tool-argument case.
func completePartialJSON(s string) (string, bool) {
	var stack []byte
	inString := false
	escaped := false
	stringStart := -1
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
			stringStart = i
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	// partial-json trims the whole input first (jsonString.trim()), even when
	// it ends inside an open string.
	completed := strings.TrimRight(s, " \t\r\n")
	if inString && isDanglingObjectKey(s, stack, stringStart) {
		// An open string in object-KEY position can't be completed into a
		// member; partial-json drops the incomplete key entirely.
		completed = strings.TrimRight(s[:stringStart], " \t\r\n")
		completed = strings.TrimRight(completed, ",")
		completed = trimDanglingColon(completed)
	} else if inString {
		// A trailing comma inside an open string is string CONTENT, not a
		// dangling token — close the string without stripping it.
		completed += "\""
	} else {
		// Drop a dangling token that can't be completed (trailing comma, colon,
		// or a key with no value). Strip trailing comma.
		completed = strings.TrimRight(completed, ",")
		// Trim a dangling "key": with no value or trailing colon.
		completed = trimDanglingColon(completed)
	}

	for i := len(stack) - 1; i >= 0; i-- {
		completed += string(stack[i])
	}
	if completed == "" {
		return "", false
	}
	return completed, true
}

// isDanglingObjectKey reports whether the open string starting at stringStart
// sits in object-key position (directly after '{' or ',' inside an object).
func isDanglingObjectKey(s string, stack []byte, stringStart int) bool {
	if stringStart < 0 || len(stack) == 0 || stack[len(stack)-1] != '}' {
		return false
	}
	for j := stringStart - 1; j >= 0; j-- {
		switch s[j] {
		case ' ', '\t', '\r', '\n':
			continue
		case '{', ',':
			return true
		default:
			return false
		}
	}
	return false
}

func trimDanglingColon(s string) string {
	t := strings.TrimRight(s, " \t\r\n")
	if strings.HasSuffix(t, ":") {
		// remove "key": with the key, back to the previous { , or [
		idx := strings.LastIndexAny(t, "{[,")
		if idx >= 0 {
			return t[:idx+1]
		}
	}
	return s
}

// sanitizeSurrogates removes unpaired UTF-16 surrogate code units (port of
// sanitizeSurrogates): pi replaces them with "" — they are DELETED, not
// substituted with U+FFFD. In Go strings, surrogate code units can only occur
// as WTF-8 byte triples (0xED 0xA0..0xBF 0x80..0xBF); a high+low pair is the
// JS-valid case and decodes to the astral character, while a lone surrogate
// is dropped. Valid UTF-8 (including real U+FFFD characters) passes through.
func sanitizeSurrogates(s string) string {
	if !containsWTF8Surrogate(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		hi, ok := decodeWTF8Surrogate(s[i:])
		if !ok {
			b.WriteByte(s[i])
			i++
			continue
		}
		if hi <= 0xDBFF { // high surrogate: pair it with a following low surrogate
			if lo, ok2 := decodeWTF8Surrogate(s[i+3:]); ok2 && lo >= 0xDC00 {
				// Properly paired surrogates form a valid astral character in
				// JS and are preserved.
				b.WriteRune(utf16.DecodeRune(hi, lo))
				i += 6
				continue
			}
		}
		// Unpaired surrogate: deleted (pi replaces with "").
		i += 3
	}
	return b.String()
}

// decodeWTF8Surrogate decodes a UTF-16 surrogate code unit encoded as a WTF-8
// byte triple at the start of s.
func decodeWTF8Surrogate(s string) (rune, bool) {
	if len(s) >= 3 && s[0] == 0xED && s[1] >= 0xA0 && s[1] <= 0xBF && s[2] >= 0x80 && s[2] <= 0xBF {
		return 0xD000 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), true
	}
	return 0, false
}

func containsWTF8Surrogate(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF {
			return true
		}
	}
	return false
}
