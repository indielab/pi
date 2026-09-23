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

// String(JSON.parse(text)) in node, for each text.
func TestToStringMatchesNode(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"null", "null"},
		{"true", "true"},
		{"false", "false"},
		{"5", "5"},
		{"5.0", "5"},
		{"-0", "0"},
		{"1e21", "1e+21"},
		{"0.1", "0.1"},
		{"1e400", "Infinity"},
		{"-1e400", "-Infinity"},
		{`"x"`, "x"},
		{"[]", ""},
		{"[null]", ""},
		{`[null,"x",["y",2]]`, ",x,y,2"},
		{"[[],[[]]]", ","},
		{"{}", "[object Object]"},
		{`{"x":1}`, "[object Object]"},
		{"[{}]", "[object Object]"},
	} {
		v, err := Parse([]byte(tc.text))
		if err != nil {
			t.Fatalf("Parse(%s): %v", tc.text, err)
		}
		if got := ToString(v); got != tc.want {
			t.Errorf("ToString(%s) = %q, node says %q", tc.text, got, tc.want)
		}
	}
}
