package providers

import (
	"encoding/json"
	"strconv"
	"testing"
)

// rawJSON spells a member's JSON text; nil is an absent member (undefined).
func rawJSON(s string) json.RawMessage { return json.RawMessage(s) }

// TestRawToStringMatchesNode pins String(value) for the values a wire event can
// hold. want is node's String(JSON.parse(in)); nil is undefined.
func TestRawToStringMatchesNode(t *testing.T) {
	for _, tc := range []struct {
		in   json.RawMessage
		want string
	}{
		{nil, "undefined"},
		{rawJSON(`5`), "5"},
		{rawJSON(`5.0`), "5"},
		{rawJSON(`-0`), "0"},
		{rawJSON(`1e400`), "Infinity"},
		{rawJSON(`1e21`), "1e+21"},
		{rawJSON(`0.1`), "0.1"},
		{rawJSON(`"0"`), "0"},
		{rawJSON(`null`), "null"},
		{rawJSON(`true`), "true"},
		{rawJSON(`false`), "false"},
		{rawJSON(`[1,[2,null]]`), "1,2,"},
		{rawJSON(`[]`), ""},
		{rawJSON(`{}`), "[object Object]"},
	} {
		got, err := rawToString(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("rawToString(%s) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, in := range []string{`{"toString":1}`, `[{"toString":1}]`} {
		if _, err := rawToString(rawJSON(in)); err == nil || err.Error() != "Cannot convert object to primitive value" {
			t.Errorf("rawToString(%s) error = %v, want V8's TypeError", in, err)
		}
	}
}

// TestRawStrictEqualMatchesNode pins === between values of two parsed events.
// want is node's JSON.parse(a) === JSON.parse(b); nil is undefined.
func TestRawStrictEqualMatchesNode(t *testing.T) {
	for _, tc := range []struct {
		a, b json.RawMessage
		want bool
	}{
		{rawJSON(`0`), rawJSON(`-0`), true},
		{rawJSON(`1`), rawJSON(`1.0`), true},
		{rawJSON(`1e400`), rawJSON(`1e400`), true},
		{rawJSON(`"0"`), rawJSON(`0`), false},
		{rawJSON(`"a"`), rawJSON(`"a"`), true},
		{rawJSON(`"\u0030"`), rawJSON(`"0"`), true},
		{rawJSON(`null`), rawJSON(`null`), true},
		{rawJSON(`true`), rawJSON(`true`), true},
		{rawJSON(`true`), rawJSON(`false`), false},
		{rawJSON(`{}`), rawJSON(`{}`), false},
		{rawJSON(`[]`), rawJSON(`[]`), false},
		{nil, nil, true},
		{nil, rawJSON(`null`), false},
		{rawJSON(`null`), nil, false},
	} {
		if got := rawStrictEqual(tc.a, tc.b); got != tc.want {
			t.Errorf("rawStrictEqual(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestRawPropertyKeyAndCount pins the property key a value names and the
// array slot it addresses, and which counts an int holds. Each key and slot
// is node's: `const a = []; a[JSON.parse(in)] = 1`, then Object.keys(a) and
// whether a.length grew.
func TestRawPropertyKeyAndCount(t *testing.T) {
	for _, tc := range []struct {
		in     json.RawMessage
		key    string
		slot   int
		isSlot bool
	}{
		{rawJSON(`0`), "0", 0, true},
		{rawJSON(`-0`), "0", 0, true},
		{rawJSON(`2.0`), "2", 2, true},
		{rawJSON(`1e1`), "10", 10, true},
		{rawJSON(`"1"`), "1", 1, true},
		{rawJSON(`[1]`), "1", 1, true},
		{rawJSON(`[[2]]`), "2", 2, true},
		{rawJSON(`4294967295`), "4294967295", 0, false},
		{rawJSON(`1.5`), "1.5", 0, false},
		{rawJSON(`-1`), "-1", 0, false},
		{rawJSON(`"01"`), "01", 0, false},
		{rawJSON(`"1.0"`), "1.0", 0, false},
		{rawJSON(`[]`), "", 0, false},
		{rawJSON(`null`), "null", 0, false},
		{rawJSON(`true`), "true", 0, false},
		{rawJSON(`{}`), "[object Object]", 0, false},
		{rawJSON(`1e21`), "1e+21", 0, false},
		{rawJSON(`1e400`), "Infinity", 0, false},
		{nil, "undefined", 0, false},
	} {
		key, slot, isSlot, err := rawPropertyKey(tc.in)
		if err != nil || key != tc.key || slot != tc.slot || isSlot != tc.isSlot {
			t.Errorf("rawPropertyKey(%s) = (%q, %d, %v, %v), want (%q, %d, %v)", tc.in, key, slot, isSlot, err, tc.key, tc.slot, tc.isSlot)
		}
	}
	// ToPropertyKey throws V8's TypeError for a value with no string form.
	if _, _, _, err := rawPropertyKey(rawJSON(`{"toString":1}`)); err == nil || err.Error() != "Cannot convert object to primitive value" {
		t.Errorf("rawPropertyKey({\"toString\":1}) error = %v, want V8's TypeError", err)
	}
	// The largest array index, 2^32-2, is a slot only where an int holds it.
	if _, slot, isSlot, _ := rawPropertyKey(rawJSON(`4294967294`)); isSlot != (strconv.IntSize == 64) || (isSlot && uint64(slot) != 4294967294) {
		t.Errorf("rawPropertyKey(4294967294) = (%d, %v) on a %d-bit int", slot, isSlot, strconv.IntSize)
	}
	for _, tc := range []struct {
		in    json.RawMessage
		count int
		ok    bool
	}{
		{rawJSON(`10.0`), 10, true},
		{rawJSON(`5e0`), 5, true},
		{rawJSON(`-3`), -3, true},
		{rawJSON(`2.9`), 2, true},
		{rawJSON(`1e400`), 0, false},
		{rawJSON(`"10"`), 0, false},
		{rawJSON(`true`), 0, false},
		{nil, 0, false},
	} {
		count, ok := rawCount(tc.in)
		if count != tc.count || ok != tc.ok {
			t.Errorf("rawCount(%s) = (%d, %v), want (%d, %v)", tc.in, count, ok, tc.count, tc.ok)
		}
	}
}
