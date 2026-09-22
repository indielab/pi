package jstext

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// TestStringifyMatchesNode compares Stringify with node's JSON.stringify for
// testdata/stringify-node.json, captured by testdata/capture-stringify.mjs.
// The cases cover the escapes where encoding/json and JSON.stringify disagree
// (<, >, &, U+2028, U+2029), an escaped backslash right before "u2028", and
// the escapes they already share.
func TestStringifyMatchesNode(t *testing.T) {
	data, err := os.ReadFile("testdata/stringify-node.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input any    `json:"input"`
		JSON  string `json:"json"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases in testdata/stringify-node.json")
	}
	for _, c := range cases {
		got, err := Stringify(c.Input)
		if err != nil {
			t.Fatalf("Stringify(%v): %v", c.Input, err)
		}
		if got != c.JSON {
			t.Errorf("Stringify = %q, node = %q", got, c.JSON)
		}
	}
}

// htmlMarshaler returns text json.Marshal has already HTML-escaped, the way a
// MarshalJSON method that delegates to json.Marshal does.
type htmlMarshaler struct{ S string }

func (h htmlMarshaler) MarshalJSON() ([]byte, error) { return json.Marshal(h.S) }

func TestStringifyUndoesEscapesFromMarshalJSON(t *testing.T) {
	got, err := Stringify(map[string]any{"v": htmlMarshaler{"a<b>&c\u2028"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\"v\":\"a<b>&c\u2028\"}"; got != want {
		t.Fatalf("Stringify = %q, want %q", got, want)
	}
}

// JSON.stringify writes negative zero as 0 (it cannot reach the node-captured
// cases above: node's own oracle file already reads 0). Only a number is
// rewritten; "-0" inside a string, -0.5 and -1 are not.
func TestStringifyNegativeZero(t *testing.T) {
	negZero := math.Copysign(0, -1)
	got, err := Stringify(map[string]any{"a": []any{negZero, 0.0, -0.5, -1.0, negZero}, "s": "-0 x", "z": negZero})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"a":[0,0,-0.5,-1,0],"s":"-0 x","z":0}`; got != want {
		t.Fatalf("Stringify = %s, want %s", got, want)
	}
}
