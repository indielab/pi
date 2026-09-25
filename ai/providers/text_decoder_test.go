package providers

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// TestUTF8StreamDecoderMatchesNode replays each decode node's TextDecoder made
// of a byte stream split into reads (testdata/text-decoder/capture.mts): the
// text of every read, a byte-order mark dropped only before the stream's first
// character, a sequence a read leaves incomplete carried into the next, and
// the bytes still held at the end dropped, as a decoder never flushed drops
// them. Where node's UTF-8 fast path drops a second byte-order mark (the
// capture's nodeQuirk rows), the reads must join to the Encoding Standard's
// text instead.
func TestUTF8StreamDecoderMatchesNode(t *testing.T) {
	data, err := os.ReadFile("testdata/text-decoder/text-decoder-node.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Rows []struct {
			Input        []byte   `json:"input"`
			Cuts         []int    `json:"cuts"`
			Reads        []string `json:"reads"`
			NodeQuirk    bool     `json:"nodeQuirk"`
			StandardJoin string   `json:"standardJoin"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	if len(capture.Rows) < 100 {
		t.Fatalf("only %d rows in the capture", len(capture.Rows))
	}
	quirks := 0
	for _, row := range capture.Rows {
		var d utf8StreamDecoder
		var reads []string
		at := 0
		for _, cut := range append(slices.Clone(row.Cuts), len(row.Input)) {
			reads = append(reads, d.decode(row.Input[at:cut]))
			at = cut
		}
		if row.NodeQuirk {
			quirks++
			if joined := strings.Join(reads, ""); joined != row.StandardJoin {
				t.Errorf("% x cut at %v: %q, the Encoding Standard %q", row.Input, row.Cuts, joined, row.StandardJoin)
			}
			continue
		}
		if !slices.Equal(reads, row.Reads) {
			t.Errorf("% x cut at %v: %q, node %q", row.Input, row.Cuts, reads, row.Reads)
		}
	}
	if quirks == 0 {
		t.Fatal("the capture tags no nodeQuirk rows; the split doubled byte-order mark should be one")
	}
}
