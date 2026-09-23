package jstext

import (
	"encoding/json"
	"math"
	"testing"
)

// Boolean(JSON.parse(text)) in node, for each text.
func TestTruthyMatchesNode(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"null", false},
		{"false", false},
		{"true", true},
		{"0", false},
		{"-0", false},
		{"0.0", false},
		{"1e-400", false}, // 0 once parsed
		{"1e400", true},   // Infinity
		{"-1e400", true},  // -Infinity
		{`""`, false},
		{`"0"`, true},
		{`"false"`, true},
		{"[]", true},
		{"{}", true},
		{"[0]", true},
	} {
		v, err := Parse([]byte(tc.text))
		if err != nil {
			t.Fatalf("Parse(%s): %v", tc.text, err)
		}
		if got := Truthy(v); got != tc.want {
			t.Errorf("Truthy(%s) = %v, node says %v", tc.text, got, tc.want)
		}
	}
}

func TestNumberSaturatesLikeJSONParse(t *testing.T) {
	for _, tc := range []struct {
		text string
		want float64
	}{
		{"1e400", math.Inf(1)},
		{"-1e400", math.Inf(-1)},
		{"1e-400", 0},
		{"2.5e-324", 5e-324},
		{"0.1", 0.1},
	} {
		if got := Number(json.Number(tc.text)); got != tc.want {
			t.Errorf("Number(%s) = %v, want %v", tc.text, got, tc.want)
		}
	}
	if got := Number(json.Number("x")); !math.IsNaN(got) {
		t.Errorf(`Number("x") = %v, want NaN`, got)
	}
}

func TestParseTakesOneValue(t *testing.T) {
	for _, text := range []string{"", "1 2", "{} x", "[1"} {
		if _, err := Parse([]byte(text)); err == nil {
			t.Errorf("Parse(%q) accepted it", text)
		}
	}
	if v, err := Parse([]byte(" 1e400 \n")); err != nil || v != json.Number("1e400") {
		t.Errorf(`Parse(" 1e400 \n") = %v, %v`, v, err)
	}
}
