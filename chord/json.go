package chord

import (
	"math"
	"reflect"
)

// Value is a strict JSON value: nil, bool, a finite number, a string, an
// array of Values, or a string-keyed object of Values. It is an alias so any
// decoded tree already is one; [IsValue] is the check that a given value
// keeps the contract.
type Value = any

// maxValueDepth is the nesting upstream's isJsonValue refuses beyond, root at
// depth 0. It is also what terminates a cycle: a self-referencing map or
// slice walks down until the cap, so no visited set is needed — and none would
// be sound, since reflect.Value.Pointer cannot tell two slices sharing a
// backing array apart.
const maxValueDepth = 512

// IsValue reports whether value is finite strict JSON with plain containers
// and no cycles, without normalizing it.
//
// Every Go numeric kind is a number, as every JS number is; only a finite
// float passes. A slice or array of anything but bytes is an array — a []byte
// is a binary blob, the way a Uint8Array is upstream, and encoding/json would
// write it as a base64 string rather than an array. A map with a string-kind
// key is an object. A struct is a class instance rather than a plain object,
// and pointers, funcs, channels and complex numbers have no JSON form.
func IsValue(value any) bool {
	return checkValue(value, 0)
}

func checkValue(value any, depth int) bool {
	if depth > maxValueDepth {
		return false
	}
	switch v := value.(type) {
	case nil, bool, string:
		return true
	case float64:
		return !math.IsInf(v, 0) && !math.IsNaN(v)
	case []any:
		for _, item := range v {
			if !checkValue(item, depth+1) {
				return false
			}
		}
		return true
	case map[string]any:
		for _, item := range v {
			if !checkValue(item, depth+1) {
				return false
			}
		}
		return true
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.String, reflect.Bool:
		return true
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		return !math.IsInf(f, 0) && !math.IsNaN(f)
	case reflect.Slice, reflect.Array:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return false
		}
		for i := range rv.Len() {
			if !checkValue(rv.Index(i).Interface(), depth+1) {
				return false
			}
		}
		return true
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return false
		}
		for iter := rv.MapRange(); iter.Next(); {
			if !checkValue(iter.Value().Interface(), depth+1) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
