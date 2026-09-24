package jstext

import (
	"bytes"
	"encoding/json"
	"math"
	"slices"
	"strings"
)

// Stringify returns v's JSON as JSON.stringify writes it, for the places pi
// measures or embeds that text. Two encoding/json habits are undone:
//
//   - it writes <, >, &, U+2028 and U+2029 as \u003c, \u003e, \u0026, \u2028
//     and \u2029 where JSON.stringify writes the characters themselves.
//     SetEscapeHTML alone is not enough — a MarshalJSON method that calls
//     json.Marshal hands the encoder text that is already escaped — so the
//     escapes are written back as characters. JSON.stringify never emits any
//     of the five, so this cannot misfire;
//   - it writes negative zero as -0 where JSON.stringify writes 0;
//   - it refuses a number that is not finite, which JSON.stringify writes as
//     null (a float64 directly in v or in its []any and map[string]any; a
//     type with its own MarshalJSON writes itself).
//
// What remains is encoding/json's: map keys come out sorted where JS keeps
// insertion order (the length is the same, the text is not), and a Go string
// cannot hold a lone UTF-16 surrogate, which JS would write as an escape.
func Stringify(v any) (string, error) {
	if hasNonFinite(v) {
		v = nonFiniteAsNull(v)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	s := strings.TrimSuffix(buf.String(), "\n")
	if !strings.Contains(s, `\u`) && !strings.Contains(s, "-0") {
		return s, nil
	}
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inString && c == '\\' && i+1 < len(s):
			// Step over whole escapes, so an escaped backslash followed by
			// "u2028" stays two characters of text.
			if s[i+1] == 'u' && i+5 < len(s) {
				if r, ok := unescaped[s[i+2:i+6]]; ok {
					b.WriteRune(r)
					i += 5
					continue
				}
			}
			b.WriteByte(c)
			b.WriteByte(s[i+1])
			i++
		case c == '"':
			inString = !inString
			b.WriteByte(c)
		case !inString && c == '-' && isNegativeZero(s[i+1:]):
			// JSON.stringify(-0) is "0": drop the sign.
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}

// hasNonFinite reports whether v holds a float64 that is not finite, directly
// or in its []any and map[string]any.
func hasNonFinite(v any) bool {
	switch t := v.(type) {
	case float64:
		return math.IsInf(t, 0) || math.IsNaN(t)
	case []any:
		return slices.ContainsFunc(t, hasNonFinite)
	case map[string]any:
		for _, e := range t {
			if hasNonFinite(e) {
				return true
			}
		}
	}
	return false
}

// nonFiniteAsNull is a copy of v with every float64 that is not finite, in it
// or in its []any and map[string]any, replaced by nil.
func nonFiniteAsNull(v any) any {
	switch t := v.(type) {
	case float64:
		if math.IsInf(t, 0) || math.IsNaN(t) {
			return nil
		}
	case []any:
		if t == nil {
			return t
		}
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = nonFiniteAsNull(e)
		}
		return out
	case map[string]any:
		if t == nil {
			return t
		}
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = nonFiniteAsNull(e)
		}
		return out
	}
	return v
}

// isNegativeZero reports whether rest, the text after a '-' outside a string,
// is the number 0 itself, not 0.5 or 0e-3.
func isNegativeZero(rest string) bool {
	if rest == "" || rest[0] != '0' {
		return false
	}
	return len(rest) == 1 || (rest[1] != '.' && rest[1] != 'e' && rest[1] != 'E')
}

// unescaped maps the \uXXXX escapes encoding/json writes and JSON.stringify
// does not to the characters JSON.stringify writes instead.
var unescaped = map[string]rune{"003c": '<', "003e": '>', "0026": '&', "2028": '\u2028', "2029": '\u2029'}
