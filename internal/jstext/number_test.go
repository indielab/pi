package jstext

import (
	"math"
	"testing"
)

func TestNumberToString(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{1000000, "1000000"},
		{0.0000001, "1e-7"},
		{1e21, "1e+21"},
		{1e20, "100000000000000000000"},
		{1e-6, "0.000001"},
		{1e-7, "1e-7"},
		{123.456, "123.456"},
		{-0.5, "-0.5"},
		{0, "0"},
		{math.Copysign(0, -1), "0"},
		{42, "42"},
		{1.5e22, "1.5e+22"},
		{1234567890123456789, "1234567890123456800"},
		{math.Inf(1), "Infinity"},
		{math.Inf(-1), "-Infinity"},
		{math.NaN(), "NaN"},
	}
	for _, c := range cases {
		if got := NumberToString(c.in); got != c.want {
			t.Errorf("NumberToString(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// String(JSON.parse(text)) in node, for each text; throws marks the texts
// whose String() throws "TypeError: Cannot convert object to primitive value".
func TestToStringMatchesNode(t *testing.T) {
	for _, tc := range []struct {
		text, want string
		throws     bool
	}{
		{text: "null", want: "null"},
		{text: "true", want: "true"},
		{text: "false", want: "false"},
		{text: "5", want: "5"},
		{text: "5.0", want: "5"},
		{text: "-0", want: "0"},
		{text: "1e21", want: "1e+21"},
		{text: "0.1", want: "0.1"},
		{text: "1e400", want: "Infinity"},
		{text: "-1e400", want: "-Infinity"},
		{text: `"x"`, want: "x"},
		{text: "[]", want: ""},
		{text: "[null]", want: ""},
		{text: `[null,"x",["y",2]]`, want: ",x,y,2"},
		{text: "[[],[[]]]", want: ","},
		{text: "{}", want: "[object Object]"},
		{text: `{"x":1}`, want: "[object Object]"},
		{text: "[{}]", want: "[object Object]"},
		{text: `{"valueOf":1}`, want: "[object Object]"},
		{text: `{"a":{"toString":1}}`, want: "[object Object]"},
		{text: `{"__proto__":{"toString":1}}`, want: "[object Object]"},
		{text: `{"toString":1}`, throws: true},
		{text: `{"toString":1,"valueOf":2}`, throws: true},
		{text: `[{"toString":"x"}]`, throws: true},
		{text: `[1,{"toString":1}]`, throws: true},
		{text: `[[{"toString":null}]]`, throws: true},
	} {
		v, err := Parse([]byte(tc.text))
		if err != nil {
			t.Fatalf("Parse(%s): %v", tc.text, err)
		}
		got, ok := ToString(v)
		switch {
		case tc.throws && ok:
			t.Errorf("ToString(%s) = %q, but node's String() throws", tc.text, got)
		case !tc.throws && !ok:
			t.Errorf("ToString(%s) has no string form, node says %q", tc.text, tc.want)
		case got != tc.want:
			t.Errorf("ToString(%s) = %q, node says %q", tc.text, got, tc.want)
		}
	}
}
