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
		// wantJSON is JSON.stringify of pi's JSON.parse(in), as node writes
		// it: key order is the wire's, which a map[string]any would lose,
		// except that array-index keys come first, ascending.
		wantJSON string
		want     any
	}{
		{name: "object keeps wire order", in: `{"type":"a","id":1,"choices":[]}`, wantJSON: `{"type":"a","id":1,"choices":[]}`},
		{name: "nested objects keep order", in: `{"z":{"b":1,"a":[{"d":true,"c":null}]},"y":"s"}`, wantJSON: `{"z":{"b":1,"a":[{"d":true,"c":null}]},"y":"s"}`},
		{name: "repeated key keeps first slot, last value", in: `{"a":1,"b":2,"a":3}`, wantJSON: `{"a":3,"b":2}`},
		{name: "array", in: `[3,{"b":1,"a":2}]`, wantJSON: `[3,{"b":1,"a":2}]`},
		{
			name:     "array-index keys first, ascending",
			in:       `{"b":1,"2":"x","1":"y","-1":0,"01":2,"4294967295":3,"4294967294":4,"id":"k"}`,
			wantJSON: `{"1":"y","2":"x","4294967294":4,"b":1,"-1":0,"01":2,"4294967295":3,"id":"k"}`,
		},
		{name: "repeated array-index key keeps its place", in: `{"a":1,"3":1,"a":2,"0":0,"3":"again"}`, wantJSON: `{"0":0,"3":"again","a":2}`},
		{
			name:     "only canonical indexes are hoisted, at any depth",
			in:       `{"z":{"10":1,"9":2,"x":3},"1.0":1,"1e1":2,"00":3,"0":4," 1":5,"+1":6}`,
			wantJSON: `{"0":4,"z":{"9":2,"10":1,"x":3},"1.0":1,"1e1":2,"00":3," 1":5,"+1":6}`,
		},
		{name: "index keys inside arrays", in: `[{"2":1,"1":2},{"4294967296":1,"4294967294":2}]`, wantJSON: `[{"1":2,"2":1},{"4294967294":2,"4294967296":1}]`},
		{name: "empty key is not an index", in: `{"":1,"0":2}`, wantJSON: `{"0":2,"":1}`},
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

// DecodeOrderedObject's twin lists the keys as JSON.parse's object does, and
// its map holds the same members.
func TestDecodeOrderedObjectOrdersKeysAsJSONParse(t *testing.T) {
	plain, ordered, err := DecodeOrderedObject([]byte(`{"b":1,"2":"x","1":{"4":0,"3":0}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(ordered)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"1":{"3":0,"4":0},"2":"x","b":1}`; string(b) != want {
		t.Fatalf("ordered = %s, want %s (node's JSON.stringify(JSON.parse(...)))", b, want)
	}
	if !reflect.DeepEqual(plain, ordered.Plain()) {
		t.Fatalf("map %v does not hold the ordered twin's members %v", plain, ordered.Plain())
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
