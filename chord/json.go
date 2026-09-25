package chord

import (
	"fmt"

	"github.com/sky-valley/pi/chord/internal/jsonvalue"
)

// Value is a strict JSON value: nil, bool, a finite number, a string, an
// array of Values, or a string-keyed object of Values. It is an alias so any
// decoded tree already is one; [IsValue] is the check that a given value
// keeps the contract, and [CopyJSON] the copy that makes one owned.
type Value = any

// ValueError reports a value that is not strict JSON: Message is upstream's
// TypeError text, Value the offending Go value (nil for a cycle). A draft
// refuses a placement with the same type (delta.ValueError).
type ValueError = jsonvalue.Error

// IsValue reports whether value is finite strict JSON with plain containers
// and no cycles, without normalizing it.
//
// Every Go numeric kind is a number, as every JS number is, and so is a
// json.Number; only a finite one passes. A slice or array of anything but
// bytes is an array — a []byte is a binary blob, the way a Uint8Array is
// upstream, and encoding/json would write it as a base64 string rather than
// an array. A map with a string-kind key is an object. A struct is a class
// instance rather than a plain object, and pointers, funcs, channels and
// complex numbers have no JSON form. There is no depth limit: a cycle is
// refused as a cycle.
func IsValue(value any) bool {
	return jsonvalue.Is(value)
}

// CopyJSON is upstream's copyJson: a detached, alias-free copy of value in the
// representation a decoded tree has — nil, bool, float64, string, []any and
// map[string]any — so that the caller owns every container of it, and a
// container reachable twice in value is two containers in the copy. It
// accepts what IsValue accepts and refuses anything else with a *ValueError.
//
// Upstream's omitUndefinedProperties option has no Go form: a Go value holds
// no undefined. Nor is a *delta.Draft a value here — pi's copy reads a draft
// through its traps; place a draft in another draft, which copies what it
// holds now.
func CopyJSON(value any) (Value, error) {
	v, err := jsonvalue.Copy(value, nil)
	if err != nil {
		return nil, fmt.Errorf("chord: %w", err)
	}
	return v, nil
}
