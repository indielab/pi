// Package jsonvalue is chord's strict-JSON rules — upstream's src/json.ts,
// copyJson and isJsonValue — shared by package chord, which exports them, and
// chord/delta, whose drafts copy every value placed into them with them. It
// is a package of its own because chord imports delta.
//
// A value is strict JSON in its Go spelling: nil, a bool, a finite number of
// any Go numeric kind or a json.Number, a string, a slice or array of anything
// but bytes (a []byte is a binary blob, the way a Uint8Array is upstream, and
// encoding/json writes it as a base64 string), or a map with a string-kind
// key. A struct is a class instance, not a plain object, and pointers, funcs,
// channels and complex numbers have no JSON form. No container may reach
// itself.
package jsonvalue

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
)

// Upstream's copyJson TypeError texts.
const (
	NonFinite = "Value contains a non-finite number and is not strict JSON"
	Cycle     = "Value contains cycles and is not strict JSON"
	Plain     = "Value must contain strict JSON plain objects or arrays"
	// NonJSON is a template: upstream fills in typeof — "function" for a
	// func, which is the one Go kind JavaScript has — and the port its Go
	// type for the kinds JavaScript has no typeof for.
	NonJSON = "Value contains a non-JSON %s; expected strict JSON"
)

// Error reports a value that is not strict JSON. Message is upstream's
// TypeError text; Value the offending Go value, nil for a cycle.
type Error struct {
	Message string
	Value   any
}

func (e *Error) Error() string {
	switch e.Message {
	case Cycle:
		return e.Message + " (a container reaches itself; break the cycle before handing the value over)"
	case NonFinite:
		f, _ := number(reflect.ValueOf(e.Value))
		got := "NaN"
		if !math.IsNaN(f) {
			got = fmt.Sprint(f)
		}
		return fmt.Sprintf("%s: got %s (a JSON number is finite)", e.Message, got)
	}
	return fmt.Sprintf("%s: got %T (build values from nil, bool, string, finite numbers, slices and string-keyed maps, as encoding/json decodes them)", e.Message, e.Value)
}

// Hook lets a caller copy a value the rules do not know: handled reports that
// it did. chord/delta copies a draft with it, as it holds its content now —
// what upstream's copy reads through the draft's traps.
type Hook func(v any) (copied any, handled bool, err error)

// Copy is copyJson: a detached, alias-free copy of v in the representation a
// decoded tree has — nil, bool, float64, string, []any and map[string]any —
// with every container reachable twice copied twice, or an *Error. Every
// array it allocates has a backing array of its own (capacity at least one),
// so that each copy is a container of its own identity. hook, which may be
// nil, sees every value before the rules do.
func Copy(v any, hook Hook) (any, error) {
	w := walker{hook: hook, copy: true}
	return w.walk(v)
}

// Is is isJsonValue: whether v is strict JSON, without copying or normalizing
// it.
func Is(v any) bool {
	w := walker{}
	_, err := w.walk(v)
	return err == nil
}

// identity is a container on the current path: a map's header or a slice's
// backing array together with its length (a shorter view of the same array is
// a different array, and a descendant equal to an ancestor in both is a
// cycle).
type identity struct {
	ptr uintptr
	len int
}

type walker struct {
	hook      Hook
	copy      bool
	ancestors map[identity]bool
}

func (w *walker) walk(v any) (any, error) {
	if w.hook != nil {
		if c, ok, err := w.hook(v); ok || err != nil {
			return c, err
		}
	}
	switch x := v.(type) {
	case nil, bool, string:
		return x, nil
	case float64:
		return finite(x, v)
	case json.Number:
		// Float64 reads "1e400" as +Inf with an error, and a non-number as 0
		// with one.
		f, err := x.Float64()
		if err != nil && !math.IsInf(f, 0) {
			f = math.NaN()
		}
		return finite(f, f)
	case []any:
		return w.container(reflect.ValueOf(x))
	case map[string]any:
		return w.container(reflect.ValueOf(x))
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Bool:
		return rv.Bool(), nil
	case reflect.String:
		return rv.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		f, _ := number(rv)
		return finite(f, v)
	case reflect.Slice, reflect.Array:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return nil, &Error{Message: Plain, Value: v}
		}
		return w.container(rv)
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, &Error{Message: Plain, Value: v}
		}
		return w.container(rv)
	case reflect.Struct, reflect.Pointer, reflect.Interface:
		return nil, &Error{Message: Plain, Value: v}
	case reflect.Func:
		return nil, &Error{Message: fmt.Sprintf(NonJSON, "function"), Value: v}
	}
	return nil, &Error{Message: fmt.Sprintf(NonJSON, rv.Type()), Value: v}
}

func number(rv reflect.Value) (float64, bool) {
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return math.NaN(), false
}

func finite(f float64, v any) (any, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, &Error{Message: NonFinite, Value: v}
	}
	return f, nil
}

// container walks a slice, array or string-keyed map, refusing one that is
// its own ancestor. []any and map[string]any, which every decoded tree is
// made of, skip reflection.
func (w *walker) container(rv reflect.Value) (any, error) {
	if rv.Kind() != reflect.Array {
		if ptr := rv.Pointer(); ptr != 0 {
			id := identity{ptr: ptr, len: -1}
			if rv.Kind() == reflect.Slice {
				id.len = rv.Len()
			}
			if w.ancestors[id] {
				return nil, &Error{Message: Cycle}
			}
			if w.ancestors == nil {
				w.ancestors = map[identity]bool{}
			}
			w.ancestors[id] = true
			defer delete(w.ancestors, id)
		}
	}
	switch x := rv.Interface().(type) {
	case []any:
		return w.elements(len(x), func(i int) any { return x[i] })
	case map[string]any:
		return w.members(slices.Sorted(maps.Keys(x)), func(k string) any { return x[k] })
	}
	if rv.Kind() == reflect.Map {
		keys := make(map[string]reflect.Value, rv.Len())
		for _, k := range rv.MapKeys() {
			keys[k.String()] = k
		}
		return w.members(slices.Sorted(maps.Keys(keys)), func(k string) any { return rv.MapIndex(keys[k]).Interface() })
	}
	return w.elements(rv.Len(), func(i int) any { return rv.Index(i).Interface() })
}

func (w *walker) elements(n int, at func(int) any) (any, error) {
	var out []any
	if w.copy {
		out = make([]any, n, max(n, 1))
	}
	for i := range n {
		c, err := w.walk(at(i))
		if err != nil {
			return nil, err
		}
		if w.copy {
			out[i] = c
		}
	}
	if !w.copy {
		return nil, nil
	}
	return out, nil
}

// members walks an object's members in key order, so that of several bad
// members the one reported is always the same.
func (w *walker) members(keys []string, at func(string) any) (any, error) {
	var out map[string]any
	if w.copy {
		out = make(map[string]any, len(keys))
	}
	for _, k := range keys {
		c, err := w.walk(at(k))
		if err != nil {
			return nil, err
		}
		if w.copy {
			out[k] = c
		}
	}
	if !w.copy {
		return nil, nil
	}
	return out, nil
}
