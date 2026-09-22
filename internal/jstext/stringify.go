package jstext

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Stringify returns v's JSON the way JSON.stringify writes it, for the places
// pi measures or embeds that text. encoding/json writes <, >, &, U+2028 and
// U+2029 as \u003c, \u003e, \u0026, \u2028 and \u2029, where JSON.stringify
// writes the characters themselves; that changes the text's length, so those
// five escapes are written back as characters. SetEscapeHTML alone is not
// enough: a MarshalJSON method that calls json.Marshal hands the encoder text
// that is already escaped. JSON.stringify never emits any of the five, so
// undoing them is safe. Key order and number formatting are encoding/json's.
func Stringify(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	s := strings.TrimSuffix(buf.String(), "\n")
	if !strings.Contains(s, `\u`) {
		return s, nil
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		// Every backslash in encoder output opens an escape, so stepping over
		// whole escapes keeps an escaped backslash followed by "u2028" intact.
		if s[i+1] == 'u' && i+5 < len(s) {
			if r, ok := unescaped[s[i+2:i+6]]; ok {
				b.WriteRune(r)
				i += 5
				continue
			}
		}
		b.WriteByte(s[i])
		b.WriteByte(s[i+1])
		i++
	}
	return b.String(), nil
}

// unescaped maps the \uXXXX escapes encoding/json writes and JSON.stringify
// does not to the characters JSON.stringify writes instead.
var unescaped = map[string]rune{"003c": '<', "003e": '>', "0026": '&', "2028": '\u2028', "2029": '\u2029'}
