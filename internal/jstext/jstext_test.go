package jstext

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// whitespaceCapture is testdata/whitespace-node.json, written by
// testdata/capture-whitespace.mjs under node.
type whitespaceCapture struct {
	Node              string    `json:"node"`
	Trim              []rune    `json:"trim"`
	TrimStart         []rune    `json:"trimStart"`
	TrimEnd           []rune    `json:"trimEnd"`
	ParseFloatLeading []rune    `json:"parseFloatLeading"`
	Strings           []trimmed `json:"strings"`
}

type trimmed struct {
	In        string `json:"in"`
	Trim      string `json:"trim"`
	TrimStart string `json:"trimStart"`
	TrimEnd   string `json:"trimEnd"`
}

func loadCapture(t *testing.T) whitespaceCapture {
	t.Helper()
	data, err := os.ReadFile("testdata/whitespace-node.json")
	if err != nil {
		t.Fatal(err)
	}
	var c whitespaceCapture
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("testdata/whitespace-node.json: %v; rerun capture-whitespace.mjs", err)
	}
	if len(c.Trim) == 0 {
		t.Fatal("testdata/whitespace-node.json carries no trim set; rerun capture-whitespace.mjs")
	}
	return c
}

// Every Unicode scalar value is whitespace to IsWhitespace exactly when node's
// trim, trimStart, trimEnd and parseFloat treat it as whitespace.
func TestWhitespaceMatchesNode(t *testing.T) {
	c := loadCapture(t)
	for name, set := range map[string][]rune{
		"trim": c.Trim, "trimStart": c.TrimStart, "trimEnd": c.TrimEnd, "parseFloat": c.ParseFloatLeading,
	} {
		var got []rune
		for r := rune(0); r <= 0x10ffff; r++ {
			if r >= 0xd800 && r <= 0xdfff {
				continue
			}
			if IsWhitespace(r) {
				got = append(got, r)
			}
		}
		if !slices.Equal(got, set) {
			t.Errorf("IsWhitespace set = %U\nnode %s %s set = %U", got, c.Node, name, set)
		}
	}
}

func TestTrimsMatchNode(t *testing.T) {
	for _, s := range loadCapture(t).Strings {
		if got := Trim(s.In); got != s.Trim {
			t.Errorf("Trim(%+q) = %+q, node says %+q", s.In, got, s.Trim)
		}
		if got := TrimStart(s.In); got != s.TrimStart {
			t.Errorf("TrimStart(%+q) = %+q, node says %+q", s.In, got, s.TrimStart)
		}
		if got := TrimEnd(s.In); got != s.TrimEnd {
			t.Errorf("TrimEnd(%+q) = %+q, node says %+q", s.In, got, s.TrimEnd)
		}
	}
}
