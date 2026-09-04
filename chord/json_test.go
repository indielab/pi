package chord

import (
	"math"
	"testing"
)

// Port of packages/chord/test/json.test.ts at 64eeb82a4.

// TestIsValueChecksStrictJSONWithoutNormalizingIt is upstream's single case,
// one assertion per line of it. Go has no undefined, so the "omitted"
// property carries the nearest thing — a value of a non-JSON kind — and the
// Uint8Array case is a []byte, which encoding/json would base64 rather than
// write as an array.
func TestIsValueChecksStrictJSONWithoutNormalizingIt(t *testing.T) {
	if !IsValue(map[string]any{"nested": []any{1, true, nil}}) {
		t.Error("nested plain JSON rejected")
	}
	if IsValue(map[string]any{"omitted": func() {}}) {
		t.Error("object holding a non-JSON member accepted")
	}
	if IsValue([]byte{1}) {
		t.Error("byte string accepted as an array")
	}
	if IsValue(math.Inf(1)) {
		t.Error("+Inf accepted")
	}
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	if IsValue(cyclic) {
		t.Error("cyclic object accepted")
	}
}

// TestIsValueScalars pins the Go spelling of upstream's scalar rules: every
// numeric kind is a number, only finite floats pass, and Go-only kinds are not
// JSON.
func TestIsValueScalars(t *testing.T) {
	type named string
	accept := []any{nil, true, "s", named("n"), 0, int8(1), uint64(2), uintptr(3), float32(1.5), 2.5}
	for _, v := range accept {
		if !IsValue(v) {
			t.Errorf("IsValue(%#v) = false, want true", v)
		}
	}
	reject := []any{math.NaN(), math.Inf(-1), float32(math.Inf(1)), struct{}{}, new(int), make(chan int), complex(1, 0)}
	for _, v := range reject {
		if IsValue(v) {
			t.Errorf("IsValue(%#v) = true, want false", v)
		}
	}
}

// TestIsValueContainers: arrays and objects are checked by kind, so typed
// slices and string-keyed maps count, but a non-string key does not (upstream
// rejects symbol keys), and a struct is a class instance, not a plain object.
func TestIsValueContainers(t *testing.T) {
	type key string
	accept := []any{[]any{}, []string{"a"}, [2]int{1, 2}, map[string]any{}, map[key]int{"k": 1}, map[string][]any{"a": {nil}}}
	for _, v := range accept {
		if !IsValue(v) {
			t.Errorf("IsValue(%#v) = false, want true", v)
		}
	}
	reject := []any{map[int]any{1: "a"}, []any{math.NaN()}, map[string]any{"a": struct{}{}}, struct{ A int }{1}, []struct{}{{}}}
	for _, v := range reject {
		if IsValue(v) {
			t.Errorf("IsValue(%#v) = true, want false", v)
		}
	}
}

// TestIsValueDepthCap: upstream refuses a value whose depth exceeds 512, root
// at 0, so 513 nested containers pass and 514 do not. This is also what makes
// a cycle terminate without tracking visited pointers. Every container kind
// counts a level, so the cap is pinned through plain []any and
// map[string]any as well as through the typed slices and maps that walk
// by reflection.
func TestIsValueDepthCap(t *testing.T) {
	type list []any
	type object map[string]any
	nest := func(n int, wrap func(any) any) any {
		var v any = wrap(nil)
		for range n - 1 {
			v = wrap(v)
		}
		return v
	}
	for name, wrap := range map[string]func(any) any{
		"[]any": func(v any) any {
			if v == nil {
				return []any{}
			}
			return []any{v}
		},
		"map[string]any": func(v any) any {
			if v == nil {
				return map[string]any{}
			}
			return map[string]any{"k": v}
		},
		"typed slice": func(v any) any {
			if v == nil {
				return list{}
			}
			return list{v}
		},
		"typed map": func(v any) any {
			if v == nil {
				return object{}
			}
			return object{"k": v}
		},
	} {
		if !IsValue(nest(513, wrap)) {
			t.Errorf("%s: 513 nested rejected", name)
		}
		if IsValue(nest(514, wrap)) {
			t.Errorf("%s: 514 nested accepted", name)
		}
	}
}
