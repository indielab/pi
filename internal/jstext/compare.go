package jstext

import "unicode/utf8"

// CompareUTF16 orders two strings by UTF-16 code unit, as JavaScript's < and
// Array.prototype.sort's default order do. It differs from Go's byte order
// only between an astral rune and a BMP rune above the surrogate range
// (U+E000..U+FFFF), which UTF-8 puts first and UTF-16 last.
func CompareUTF16(a, b string) int {
	for a != "" && b != "" {
		ra, na := utf8.DecodeRuneInString(a)
		rb, nb := utf8.DecodeRuneInString(b)
		if ra != rb {
			ua, ub := ra, rb
			if ua >= 0x10000 {
				ua = 0xD800 + (ua-0x10000)>>10
			}
			if ub >= 0x10000 {
				ub = 0xD800 + (ub-0x10000)>>10
			}
			if ua != ub {
				return int(ua) - int(ub)
			}
			// Same high surrogate: the low surrogates order as the runes do.
			return int(ra) - int(rb)
		}
		a, b = a[na:], b[nb:]
	}
	return len(a) - len(b)
}
