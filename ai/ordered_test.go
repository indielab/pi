package ai

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDecodeOrderedValue(t *testing.T) {
	cases := []struct {
		name string
		in   string
		// wantJSON is JSON.stringify of pi's JSON.parse(in): key order is the
		// wire's, which a map[string]any would lose.
		wantJSON string
		want     any
	}{
		{name: "object keeps wire order", in: `{"type":"a","id":1,"choices":[]}`, wantJSON: `{"type":"a","id":1,"choices":[]}`},
		{name: "nested objects keep order", in: `{"z":{"b":1,"a":[{"d":true,"c":null}]},"y":"s"}`, wantJSON: `{"z":{"b":1,"a":[{"d":true,"c":null}]},"y":"s"}`},
		{name: "repeated key keeps first slot, last value", in: `{"a":1,"b":2,"a":3}`, wantJSON: `{"a":3,"b":2}`},
		{name: "array", in: `[3,{"b":1,"a":2}]`, wantJSON: `[3,{"b":1,"a":2}]`},
		{name: "null", in: `null`, want: nil},
		{name: "number", in: ` 5 `, want: float64(5)},
		{name: "string", in: `"x"`, want: "x"},
		{name: "bool", in: `false`, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodeOrderedValue([]byte(tc.in))
			if err != nil {
				t.Fatalf("DecodeOrderedValue(%s): %v", tc.in, err)
			}
			if tc.wantJSON != "" {
				b, err := json.Marshal(got)
				if err != nil {
					t.Fatal(err)
				}
				if string(b) != tc.wantJSON {
					t.Fatalf("marshal = %s, want %s", b, tc.wantJSON)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestDecodeOrderedValueObjectIsOrdered(t *testing.T) {
	got, err := DecodeOrderedValue([]byte(`{"b":1,"a":{"d":2,"c":3}}`))
	if err != nil {
		t.Fatal(err)
	}
	obj, ok := got.(OrderedObject)
	if !ok {
		t.Fatalf("top-level object decoded as %T, want OrderedObject", got)
	}
	if _, ok := obj[1].Value.(OrderedObject); !ok {
		t.Fatalf("nested object decoded as %T, want OrderedObject", obj[1].Value)
	}
}

func TestDecodeOrderedValueRejectsIncompleteOrTrailing(t *testing.T) {
	for _, in := range []string{``, `{"a":1`, `{"a":1} {}`, `1 2`, `nope`} {
		if v, err := DecodeOrderedValue([]byte(in)); err == nil {
			t.Errorf("DecodeOrderedValue(%q) = %#v, want an error", in, v)
		}
	}
}
