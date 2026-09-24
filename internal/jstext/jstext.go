// Package jstext holds the JavaScript text semantics the port reproduces
// wherever pi relies on them: the string trims and their whitespace set, the
// text JSON.stringify writes (Stringify), the UTF-16 code-unit order of <
// and Array.prototype.sort (CompareUTF16), how a value JSON.parse returns
// converts (Parse, Number, NumberToString, ToString, Truthy), and the text a
// TextDecoder makes of bytes that are not UTF-8 (DecodeUTF8).
package jstext

import "strings"

// IsWhitespace reports whether r is removed by String.prototype.trim: TAB, VT,
// FF, SP, NBSP, ZWNBSP (U+FEFF) and the other Space_Separator code points, plus
// the line terminators LF, CR, LS and PS. It is also the StrWhiteSpaceChar set
// Number() and Number.parseFloat skip.
//
// ECMAScript trims WhiteSpace ∪ LineTerminator (ECMA-262 TrimString), which is
// not Go's unicode.IsSpace: JavaScript strips U+FEFF and keeps U+0085, Go does
// the reverse. strings.TrimSpace therefore accepts as blank content pi sends,
// and sends content pi drops. The set is a table rather than a Unicode category
// lookup so it cannot drift with Go's Unicode version;
// testdata/whitespace-node.json is node's own answer for every scalar value and
// pins it.
func IsWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680',
		'\u2000', '\u2001', '\u2002', '\u2003', '\u2004', '\u2005', '\u2006',
		'\u2007', '\u2008', '\u2009', '\u200a', '\u2028', '\u2029', '\u202f',
		'\u205f', '\u3000', '\ufeff':
		return true
	}
	return false
}

// Trim is String.prototype.trim.
func Trim(s string) string { return strings.TrimFunc(s, IsWhitespace) }

// TrimStart is String.prototype.trimStart.
func TrimStart(s string) string { return strings.TrimLeftFunc(s, IsWhitespace) }

// TrimEnd is String.prototype.trimEnd.
func TrimEnd(s string) string { return strings.TrimRightFunc(s, IsWhitespace) }
