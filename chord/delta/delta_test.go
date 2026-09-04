package delta

import (
	"fmt"
	"testing"
)

// The delta.test.ts cases that run a tracker, the codec and an applier
// together — a producer and its replica with a boundary between them.
// track_test.go holds each of these "less the codec"; here the stream crosses
// the wire.

// delta.test.ts "apply and fan-out" → "folds a whole stream without a
// base-batch branch". Apply handles "r" by replacing and tolerates a nil
// target, so a consumer needs no IsBase check and no clone of its own — and
// the codec sits in the middle without a branch either.
func TestFoldsAWholeStreamThroughTheCodec(t *testing.T) {
	tr := Track(map[string]any{"x": float64(0), "l": []any{}})
	var enc Encoder
	var dec Decoder
	var replica any
	send := func() {
		t.Helper()
		ops, err := dec.Decode(enc.Encode(tr.Flush()))
		if err != nil {
			t.Fatal(err)
		}
		if replica, err = Apply(replica, ops); err != nil {
			t.Fatal(err)
		}
	}
	tr.State().Set("x", float64(100))
	tr.State().At("l").Push("xyz")
	send()
	tr.State().Set("x", float64(101))
	send()
	wantJSON(t, replica, tr.Target())
	wantJSON(t, replica, tree(t, `{"x": 101, "l": ["xyz"]}`))
}

// delta.test.ts "property: flush-time tracking" → "converges across mixed
// nested writes, replacements, and array mutations", with the codec in the
// loop as upstream runs it: one encoder/decoder pair per stream, the replica
// checked at random flushes and at the end.
func TestPropertyFlushTimeTrackingConvergesThroughTheCodec(t *testing.T) {
	seed := lcg(0x5eed1234)
	initial := `{"rows": [{"text": "a", "count": 0}, {"text": "b", "count": 0}], "meta": {"revision": 0}}`
	for round := range 100 {
		tr := Track(tree(t, initial))
		var enc Encoder
		var dec Decoder
		var replica any
		sync := func(step int) {
			t.Helper()
			ops, err := dec.Decode(enc.Encode(tr.Flush()))
			if err != nil {
				t.Fatalf("round %d step %d: decode: %v", round, step, err)
			}
			if replica, err = Apply(replica, ops); err != nil {
				t.Fatalf("round %d step %d: apply: %v", round, step, err)
			}
			wantJSON(t, replica, tr.Target())
		}
		sync(-1)
		for step := range 60 {
			rows := tr.State().At("rows")
			switch seed.next() % 10 {
			case 0:
				if rows.Len() > 0 {
					row := rows.At(seed.next() % rows.Len())
					concat(row, "text", string(rune('a'+seed.next()%26)))
				}
			case 1:
				if rows.Len() > 0 {
					row := rows.At(seed.next() % rows.Len())
					row.Set("count", row.Get("count").(float64)+1)
				}
			case 2:
				rows.Push(map[string]any{"text": fmt.Sprintf("tail-%d-%d", round, step), "count": float64(step)})
			case 3:
				if rows.Len() > 0 {
					rows.Pop()
				}
			case 4:
				rows.Unshift(map[string]any{"text": fmt.Sprintf("head-%d-%d", round, step), "count": float64(step)})
			case 5:
				if rows.Len() > 0 {
					rows.Shift()
				}
			case 6:
				index := seed.next() % (rows.Len() + 1)
				remove := 0
				if rows.Len() > 0 && seed.next()%2 == 0 {
					remove = 1
				}
				rows.Splice(index, remove, map[string]any{"text": fmt.Sprintf("mid-%d-%d", round, step), "count": float64(step)})
			case 7:
				replacement := cloneJSON(rows.Value()).([]any)
				if len(replacement) > 0 {
					first := replacement[0].(map[string]any)
					first["text"] = first["text"].(string) + "r"
				}
				if seed.next()%2 == 0 {
					replacement = append(replacement, map[string]any{"text": "replacement-tail", "count": float64(step)})
				}
				tr.State().Set("rows", replacement)
			case 8:
				meta := tr.State().At("meta")
				tr.State().Set("meta", map[string]any{"revision": meta.Get("revision").(float64) + 1})
			default:
				if rows.Len() > 1 {
					rows.Reverse()
				}
			}
			if n := tr.State().At("rows").Len(); n > 12 {
				tr.State().At("rows").Splice(0, n-12)
			}
			if seed.next()%5 == 0 {
				sync(step)
			}
		}
		sync(60)
		if t.Failed() {
			t.FailNow()
		}
	}
}
