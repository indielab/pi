package providers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/sky-valley/pi/internal/jstext"
)

// Reading a parsed wire event the way pi's JavaScript reads it.
//
// pi's anthropic and pi-messages adapters read the properties of a JSON.parse
// value directly, so a field of an unexpected JSON type is just a value: it
// converts, compares unequal or reads as undefined, and never fails the event
// the way a typed encoding/json decode fails it. These helpers keep each
// member as its raw JSON text — nil when absent, which is JavaScript's
// undefined — and make the reads pi's code makes: property lookup by exact
// name (encoding/json matches struct fields case-insensitively), the TypeError
// V8 throws for a property of undefined or null, truthiness (rawTruthy),
// strict equality and String() conversion.
//
// A value pi stores as-is into a field Go types as string or int (an id, a
// stop reason, a token count) is kept when Go can hold it — a string, a finite
// number — and reads as absent otherwise.

// rawObject is a JSON object's members by exact name, each as its raw JSON
// text; a repeated name keeps its last value, as the object JSON.parse builds
// does.
type rawObject map[string]json.RawMessage

// decodeRawObject decodes data, a JSON object. Its error is the syntax error
// JSON.parse throws when data is not JSON at all.
func decodeRawObject(data []byte) (rawObject, error) {
	var o rawObject
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, err
	}
	return o, nil
}

// rawRead returns the members of the value raw holds, for the property reads
// pi's code makes on it, the first of which reads key: like `raw.key`, it
// throws V8's TypeError when raw is undefined or null. Any other value that is
// not an object has none of the properties pi reads, so each read of the
// empty result is undefined.
func rawRead(raw json.RawMessage, key string) (rawObject, error) {
	if raw == nil {
		return nil, fmt.Errorf("Cannot read properties of undefined (reading '%s')", key)
	}
	if jsonValueKind(raw) == 'n' {
		return nil, fmt.Errorf("Cannot read properties of null (reading '%s')", key)
	}
	return rawOptional(raw), nil
}

// rawOptional is optional chaining, `raw?.key`: the members of an object, and
// none for any other value, undefined and null included.
func rawOptional(raw json.RawMessage) rawObject {
	if jsonValueKind(raw) != '{' {
		return nil
	}
	o, err := decodeRawObject(raw)
	if err != nil {
		return nil // unreachable: raw is a member of a decoded document
	}
	return o
}

// rawNullish reports whether raw is undefined or null: what `?? x` replaces
// and `!= null` rules out.
func rawNullish(raw json.RawMessage) bool {
	return raw == nil || jsonValueKind(raw) == 'n'
}

// rawString is the string raw holds, when it holds one.
func rawString(raw json.RawMessage) (string, bool) {
	if jsonValueKind(raw) != '"' {
		return "", false
	}
	// raw is a decoded document's member, so a string with no escape is its
	// text between the quotes — as long as that is valid UTF-8, which the
	// decode below would otherwise repair.
	if text := bytes.TrimSpace(raw); bytes.IndexByte(text, '\\') < 0 && utf8.Valid(text) {
		return string(text[1 : len(text)-1]), true
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// rawNumber is the number raw holds, when it holds one, as JSON.parse
// reads it: ±Inf past float64's range, 0 below its smallest subnormal.
func rawNumber(raw json.RawMessage) (float64, bool) {
	switch c := jsonValueKind(raw); {
	case c == '-', c >= '0' && c <= '9':
		return jstext.Number(json.Number(bytes.TrimSpace(raw))), true
	}
	return 0, false
}

// rawCount reads a count pi stores as-is into a field Go holds as an int: a
// number, truncated toward zero, so the 10.0 a non-JavaScript server writes
// reads as 10. ok is false for anything an int cannot hold — a value that is
// not a number, an infinity, or a number past the int range.
func rawCount(raw json.RawMessage) (int, bool) {
	f, ok := rawNumber(raw)
	if !ok || !(-float64(math.MaxInt) < f && f < float64(math.MaxInt)) {
		return 0, false
	}
	return int(f), true
}

// rawArrayIndex reads raw as `array[raw]` reads it when that addresses a slot:
// a number that is an array index, a whole number in [0, 2^32-1) (-0 and 1.0
// included). ok is false for any other value, which in JavaScript names an
// ordinary property of the array, not a slot.
func rawArrayIndex(raw json.RawMessage) (int, bool) {
	f, ok := rawNumber(raw)
	if !ok || f != math.Trunc(f) || f < 0 || f >= math.MaxUint32 || f >= float64(math.MaxInt) {
		return 0, false
	}
	return int(f), true
}

// rawToString is String(value), which `+` and a template literal apply: the
// text of a string, "undefined" for undefined, and jstext.ToString of any
// other value ("null", "5", "1,2", "[object Object]"). Its error is the
// TypeError V8 throws for an object with its own "toString" member, which
// JSON cannot make callable, leaving the object no string form.
func rawToString(raw json.RawMessage) (string, error) {
	if raw == nil {
		return "undefined", nil
	}
	if s, ok := rawString(raw); ok {
		return s, nil
	}
	v, err := jstext.Parse(raw)
	if err != nil {
		return "", err // unreachable: raw is a member of a decoded document
	}
	s, ok := jstext.ToString(v)
	if !ok {
		return "", errors.New("Cannot convert object to primitive value")
	}
	return s, nil
}

// rawStrictEqual is `a === b` for values from two separately parsed events:
// undefined and null each equal only themselves, booleans, numbers (by value,
// so 0 === -0 and 1 === 1.0) and strings (by content) compare, and an object
// or array equals nothing, as two parses never share one.
func rawStrictEqual(a, b json.RawMessage) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ka, kb := jsonValueKind(a), jsonValueKind(b)
	if x, ok := rawNumber(a); ok {
		y, ok := rawNumber(b)
		return ok && x == y
	}
	switch ka {
	case '"':
		x, _ := rawString(a)
		y, ok := rawString(b)
		return ok && x == y
	case 'n', 't', 'f':
		return ka == kb
	}
	return false
}
