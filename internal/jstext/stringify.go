package jstext

import (
	"bytes"
	"encoding/json"
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
//   - it writes negative zero as -0 where JSON.stringify writes 0.
//
// What remains is encoding/json's: map keys come out sorted where JS keeps
// insertion order (the length is the same, the text is not), and a Go string
// cannot hold a lone UTF-16 surrogate, which JS would write as an escape.
func Stringify(v any) (string, error) {
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
