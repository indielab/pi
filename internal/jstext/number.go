package jstext

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// NumberToString formats a float64 the way JavaScript's String(number) /
// Number.prototype.toString does.
//
// JS uses the shortest round-trippable decimal, switching to exponential
// notation only when the decimal exponent is >= 21 or <= -7. This differs from
// Go's FormatFloat('g'), which would render 1000000 as "1e+06" and 1e-7 as
// "1e-07"; JS renders them "1000000" and "1e-7".
func NumberToString(f float64) string {
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}
	if math.IsNaN(f) {
		return "NaN"
	}
	if f == 0 {
		// Negative zero stringifies to "0" in JS.
		return "0"
	}

	neg := math.Signbit(f)
	abs := math.Abs(f)

	// Shortest round-trippable significand and base-10 exponent.
	// 'e' format gives "d.dddde±dd"; parse mantissa digits + exponent k where
	// value = digits * 10^(k - (len(digits)-1)).
	mant := strconv.FormatFloat(abs, 'e', -1, 64)
	eIdx := strings.IndexByte(mant, 'e')
	digitsPart := mant[:eIdx]
	exp10, _ := strconv.Atoi(mant[eIdx+1:])

	// Strip the decimal point to get the bare significant digits.
	digits := strings.Replace(digitsPart, ".", "", 1)
	// n = exponent such that value = digits-as-integer-with-implied-point.
	// Per ECMA-262 Number::toString: let k = number of digits, and s = the
	// integer formed by the digits; n is the position of the decimal point.
	k := len(digits)
	n := exp10 + 1 // point sits after the first 'n' digits when n in (0,k]

	var out string
	switch {
	case k <= n && n <= 21:
		// Integer with trailing zeros: digits followed by (n-k) zeros.
		out = digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		// Decimal point inside the digit string.
		out = digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		// 0.00...digits
		out = "0." + strings.Repeat("0", -n) + digits
	default:
		// Exponential notation. Mantissa is first digit, optional ".rest",
		// exponent is (n-1) with explicit sign and no leading zero padding.
		e := n - 1
		var sign string
		if e >= 0 {
			sign = "+"
		} else {
			sign = "-"
			e = -e
		}
		if k == 1 {
			out = digits + "e" + sign + strconv.Itoa(e)
		} else {
			out = digits[:1] + "." + digits[1:] + "e" + sign + strconv.Itoa(e)
		}
	}

	if neg {
		return "-" + out
	}
	return out
}

// ToString is JavaScript's String(value) for a value Parse returns: "null",
// "true" or "false", a number as NumberToString writes it, a string as is, an
// array as Array.prototype.join(",") writes it (null elements as ""), and an
// object as "[object Object]".
func ToString(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(x)
	case json.Number:
		return NumberToString(Number(x))
	case string:
		return x
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if e != nil {
				parts[i] = ToString(e)
			}
		}
		return strings.Join(parts, ",")
	}
	return "[object Object]"
}
