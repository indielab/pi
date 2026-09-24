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

// TestRawArrayIndexAndCount pins which numbers address an array slot, and
// which counts an int holds.
func TestRawArrayIndexAndCount(t *testing.T) {
	for _, tc := range []struct {
		in    json.RawMessage
		index int
		ok    bool
	}{
		{rawJSON(`0`), 0, true},
		{rawJSON(`-0`), 0, true},
		{rawJSON(`2.0`), 2, true},
		{rawJSON(`1e1`), 10, true},
		{rawJSON(`4294967295`), 0, false},
		{rawJSON(`1.5`), 0, false},
		{rawJSON(`-1`), 0, false},
		{rawJSON(`"1"`), 0, false},
		{rawJSON(`null`), 0, false},
		{nil, 0, false},
	} {
		index, ok := rawArrayIndex(tc.in)
		if index != tc.index || ok != tc.ok {
			t.Errorf("rawArrayIndex(%s) = (%d, %v), want (%d, %v)", tc.in, index, ok, tc.index, tc.ok)
		}
	}
	// The largest array index, 2^32-2, is a slot only where an int holds it.
	if index, ok := rawArrayIndex(rawJSON(`4294967294`)); ok != (strconv.IntSize == 64) || (ok && uint64(index) != 4294967294) {
		t.Errorf("rawArrayIndex(4294967294) = (%d, %v) on a %d-bit int", index, ok, strconv.IntSize)
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
