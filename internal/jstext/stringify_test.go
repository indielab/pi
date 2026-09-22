package jstext

import (
	"encoding/json"
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
