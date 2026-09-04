package delta

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// jsonText is the JSON form of a value or a batch — the comparison every
// tracker golden is made on, since the ops are the wire and Go's encoder
// sorts object keys on both sides.
func jsonText(t *testing.T, v any) string {
	t.Helper()
	data, err := marshalJSON(v)
	if err != nil {
		t.Fatalf("marshal %#v: %v", v, err)
	}
	return string(data)
}

// wantOps compares a flushed batch with the tuples upstream asserts, given as
// a JSON literal.
func wantOps(t *testing.T, got []Op, literal string) {
	t.Helper()
	if got == nil {
		t.Fatalf("flush returned a nil batch; an empty batch is [] on the wire")
	}
	want := jsonText(t, tree(t, literal))
	if g := jsonText(t, got); g != want {
		t.Errorf("ops\n got %s\nwant %s", g, want)
	}
}

// wantJSON compares two values by their JSON form.
func wantJSON(t *testing.T, got, want any) {
	t.Helper()
	if g, w := jsonText(t, got), jsonText(t, want); g != w {
		t.Errorf("\n got %s\nwant %s", g, w)
	}
}

// tracked builds a tracker over a JSON literal and drains the base batch —
// upstream's `track(...)` followed by `t.flush()`.
func tracked(t *testing.T, literal string) *Tracker[any] {
	t.Helper()
	tr := Track(tree(t, literal))
	if ops := tr.Flush(); !IsBase(ops) {
		t.Fatalf("first flush is not a base batch: %s", jsonText(t, ops))
	}
	return tr
}

// replay applies a batch to a fresh copy of the literal — upstream's
// apply(structuredClone(initial), ops) — and returns the replica.
func replay(t *testing.T, literal string, batch []Op) any {
	t.Helper()
	return applied(t, literal, batch)
}

// mustPanic runs fn and reports the panic value as an error, or fails.
func mustPanic(t *testing.T, fn func()) (err error) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected a panic")
		}
		e, ok := r.(error)
		if !ok {
			t.Fatalf("panic value %#v is not an error", r)
		}
		err = e
	}()
	fn()
	return nil
}

func concat(s State, key string, text string) {
	s.Set(key, s.Get(key).(string)+text)
}

// ─── tracker: intent ─────────────────────────────────────────────────────────

// delta.test.ts "tracker: intent" → "records an append, not a replacement".
func TestTrackRecordsAppendNotReplacement(t *testing.T) {
	tr := tracked(t, `{"s": "", "pad": "`+pad+`"}`)
	concat(tr.State(), "s", "ab")
	concat(tr.State(), "s", "cd")
	ops := tr.Flush()
	wantOps(t, ops, `[["a", ["s"], "abcd"]]`)
	wantTree(t, replay(t, `{"s": "", "pad": "`+pad+`"}`, ops), `{"s": "abcd", "pad": "`+pad+`"}`)
}

// delta.test.ts "tracker: intent" → "recovers truncate+append from a rolling
// window". The case every effect-recording library degrades to a whole-value
// set.
func TestTrackRollingWindow(t *testing.T) {
	tr := tracked(t, `{"s": "abcdefgh", "pad": "`+pad+`"}`)
	s := tr.State()
	s.Set("s", s.Get("s").(string)[3:]+"xyz")
	ops := tr.Flush()
	wantOps(t, ops, `[["t", ["s"], 3], ["a", ["s"], "xyz"]]`)
	wantTree(t, replay(t, `{"s": "abcdefgh", "pad": "`+pad+`"}`, ops), `{"s": "defghxyz", "pad": "`+pad+`"}`)
}

// The rolling window counts in UTF-16 code units on both sides: a "t" count
// is pi's unit, and the "a" text starts where the shared units end.
func TestTrackRollingWindowCountsUTF16(t *testing.T) {
	tr := tracked(t, `{"s": "😀😁abcdefgh"}`)
	s := tr.State()
	// Drop the first emoji (two code units) and grow the window.
	s.Set("s", "😁abcdefghé🧑")
	ops := tr.Flush()
	wantOps(t, ops, `[["t", ["s"], 2], ["a", ["s"], "é🧑"]]`)
	wantTree(t, replay(t, `{"s": "😀😁abcdefgh"}`, ops), `{"s": "😁abcdefghé🧑"}`)
}

// delta.test.ts "tracker: intent" → "caches nested proxies while replacing
// changed children". A cursor is addressed by path, so it follows a replaced
// child rather than pointing at the old one.
func TestTrackCursorFollowsReplacedChild(t *testing.T) {
	tr := Track(map[string]any{"child": map[string]any{"value": float64(1)}})
	first := tr.State().At("child")
	if got := first.Get("value"); got != float64(1) {
		t.Fatalf("got %v, want 1", got)
	}
	tr.State().Set("child", map[string]any{"value": float64(2)})
	if got := first.Get("value"); got != float64(2) {
		t.Errorf("a cursor read after the child was replaced got %v, want 2", got)
	}
}

// delta.test.ts "tracker: intent" → "records array intent as one splice".
func TestTrackArrayIntentIsOneSplice(t *testing.T) {
	tr := tracked(t, `{"xs": [1, 2], "pad": "`+pad+`"}`)
	tr.State().At("xs").Push(float64(3))
	wantOps(t, tr.Flush(), `[["p", ["xs"], 2, 0, [3]]]`)
}

// delta.test.ts "tracker: intent" → "normalises undefined to a delete" and
// "uses absence and delete for optional object properties". Go has no
// undefined; Delete is the one spelling of absence.
func TestTrackOptionalPropertiesUseAbsence(t *testing.T) {
	tr := tracked(t, `{"a": 1, "pad": "`+pad+`"}`)
	tr.State().Delete("a")
	wantOps(t, tr.Flush(), `[["d", ["a"]]]`)

	tr = Track(tree(t, `{"foo": 1}`))
	replica, err := Apply[any](nil, tr.Flush())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tr.State().Lookup("something"); ok {
		t.Fatal("something should be absent")
	}
	tr.State().Set("something", "enabled")
	if replica, err = Apply(replica, tr.Flush()); err != nil {
		t.Fatal(err)
	}
	wantTree(t, replica, `{"foo": 1, "something": "enabled"}`)
	tr.State().Delete("something")
	ops := tr.Flush()
	wantOps(t, ops, `[["d", ["something"]]]`)
	if replica, err = Apply(replica, ops); err != nil {
		t.Fatal(err)
	}
	wantTree(t, replica, `{"foo": 1}`)
}

// delta.test.ts "tracker: intent" → "round-trips interleaved writes".
func TestTrackRoundTripsInterleavedWrites(t *testing.T) {
	initial := `{"out": "` + strings.Repeat("x", 500) + `", "total": 0}`
	tr := tracked(t, initial)
	s := tr.State()
	for i := range 1000 {
		s.Set("out", s.Get("out").(string)[10:]+fmt.Sprintf("%010d", i))
		s.Set("total", s.Get("total").(float64)+10)
	}
	ops := tr.Flush()
	if len(ops) > 3 {
		t.Errorf("got %d ops, want at most 3: %s", len(ops), jsonText(t, ops))
	}
	wantJSON(t, replay(t, initial, ops), tr.Target())
}

// delta.test.ts "tracker: intent" → "deep-diffs reassigned objects".
func TestTrackDeepDiffsReassignedObjects(t *testing.T) {
	tr := tracked(t, `{"message": {"content": [{"text": "hello"}], "count": 0}}`)
	tr.State().Set("message", tree(t, `{"content": [{"text": "hello world"}], "count": 1}`))
	wantOps(t, tr.Flush(), `[
		["a", ["message", "content", 0, "text"], " world"],
		["s", ["message", "count"], 1]
	]`)
}

// delta.test.ts "tracker: intent" → "deep-diffs retained array edits combined
// with append in a replacement".
func TestTrackDeepDiffsRetainedEditsWithAppend(t *testing.T) {
	initial := `{"view": {"messages": [{"text": "a"}, {"text": "b"}]}}`
	tr := tracked(t, initial)
	tr.State().Set("view", tree(t, `{"messages": [{"text": "ax"}, {"text": "b"}, {"text": "c"}]}`))
	ops := tr.Flush()
	wantOps(t, ops, `[
		["a", ["view", "messages", 0, "text"], "x"],
		["p", ["view", "messages"], 2, 0, [{"text": "c"}]]
	]`)
	wantJSON(t, replay(t, initial, ops), tr.Target())
}

// delta.test.ts "tracker: intent" → "emits nothing when a replacement is
// deeply equal".
func TestTrackEmitsNothingForDeeplyEqualReplacement(t *testing.T) {
	tr := tracked(t, `{"value": {"nested": [1, {"text": "same"}]}}`)
	tr.State().Set("value", tree(t, `{"nested": [1, {"text": "same"}]}`))
	if !tr.Dirty() {
		t.Error("dirty should be true after a replacement")
	}
	wantOps(t, tr.Flush(), `[]`)
	if tr.Dirty() {
		t.Error("dirty should be false after a flush")
	}
}

// A Go producer writes ints where a decoded tree holds float64; the two are
// one JSON number.
func TestTrackNumbersOfAnyGoKindAreOneNumber(t *testing.T) {
	tr := tracked(t, `{"count": 1, "items": [2]}`)
	tr.State().Set("count", 1)
	tr.State().Set("items", []any{int64(2)})
	wantOps(t, tr.Flush(), `[]`)
	tr.State().Set("count", 2)
	wantOps(t, tr.Flush(), `[["s", ["count"], 2]]`)

	// And the other way round: a Go-built tree holding ints, written floats.
	tr = Track[any](map[string]any{"n": 1, "xs": []any{int32(2)}})
	tr.Flush()
	tr.State().Set("n", float64(1))
	tr.State().Set("xs", []any{float64(2)})
	wantOps(t, tr.Flush(), `[]`)
	tr.State().Set("n", uint8(3))
	wantOps(t, tr.Flush(), `[["s", ["n"], 3]]`)
}

// The baseline never shares a container with the tracked tree — whether the
// sync advanced it by cloning or the replay adopted a cloned payload — so a
// later mutation inside that container is still a change.
func TestTrackBaselineDoesNotAliasRootOrBatch(t *testing.T) {
	// Sync path: a pure set is synced by cloning.
	tr := tracked(t, `{"o": null}`)
	tr.State().Set("o", map[string]any{"a": float64(1)})
	wantOps(t, tr.Flush(), `[["s", ["o"], {"a": 1}]]`)
	tr.State().At("o").Set("a", float64(2))
	wantOps(t, tr.Flush(), `[["s", ["o", "a"], 2]]`)

	// Replay path: a structural array change declines the sync, so the batch
	// is replayed onto the baseline — from cloned payloads, or the consumer
	// that adopts the batch would be writing into the producer's baseline.
	tr = tracked(t, `{"xs": [1, 2], "o": null}`)
	tr.State().At("xs").Shift()
	tr.State().Set("o", map[string]any{"a": float64(1)})
	batch := tr.Flush()
	wantOps(t, batch, `[["p", ["xs"], 0, 1, []], ["s", ["o"], {"a": 1}]]`)
	replica, err := Apply(tree(t, `{"xs": [1, 2], "o": null}`), batch)
	if err != nil {
		t.Fatal(err)
	}
	replica.(map[string]any)["o"].(map[string]any)["a"] = float64(9)
	tr.State().At("o").Set("a", float64(9))
	wantOps(t, tr.Flush(), `[["s", ["o", "a"], 9]]`)
}

// delta.test.ts "tracker: intent" → "invalidates a pending child when its
// parent is overwritten".
func TestTrackInvalidatesPendingChildOnParentOverwrite(t *testing.T) {
	initial := `{"a": {"x": 1}}`
	tr := tracked(t, initial)
	tr.State().At("a").Set("b", float64(99))
	tr.State().Set("a", tree(t, `{"c": 2}`))
	wantJSON(t, replay(t, initial, tr.Flush()), tr.Target())
}

// ─── tracker: root ops ───────────────────────────────────────────────────────

// delta.test.ts "tracker: root ops" → "splices a value that is itself an
// array".
func TestTrackRootArraySplice(t *testing.T) {
	tr := tracked(t, `[1, 2, 3, "`+pad+`"]`)
	tr.State().Push(float64(4))
	ops := tr.Flush()
	wantOps(t, ops, `[["p", [], 4, 0, [4]]]`)
	wantTree(t, replay(t, `[1, 2, 3, "`+pad+`"]`, ops), `[1, 2, 3, "`+pad+`", 4]`)
}

// delta.test.ts "tracker: root ops" → "normalises a splice covering the whole
// root to a replacement".
func TestTrackRootSpliceAllIsReplacement(t *testing.T) {
	tr := tracked(t, `[1, 2, 3]`)
	tr.State().Splice(0, 3, float64(9))
	ops := tr.Flush()
	wantOps(t, ops, `[["r", [9]]]`)
	if !IsBase(ops) {
		t.Error("a root replacement is a base batch")
	}
}

// delta.test.ts "tracker: root ops" → "normalises a nested splice-all to a
// set, not a replacement".
func TestTrackNestedSpliceAllIsSet(t *testing.T) {
	tr := tracked(t, `{"xs": [1, 2, 3], "pad": "`+pad+`"}`)
	tr.State().At("xs").Splice(0, 3, float64(9))
	wantOps(t, tr.Flush(), `[["s", ["xs"], [9]]]`)
}

// delta.test.ts "tracker: root ops" → "matches splice with no arguments" and
// "normalises splice arguments to integers". Go's ints need no coercion; what
// is left is Array.prototype.splice's clamping: a count past the end, a
// negative start counted from the end.
func TestTrackSpliceClamps(t *testing.T) {
	initial := `{"xs": [1, 2, 3]}`
	tr := tracked(t, initial)
	xs := tr.State().At("xs")
	if removed := xs.Splice(0, 0); len(removed) != 0 {
		t.Errorf("splice(0, 0) removed %v", removed)
	}
	wantJSON(t, tr.Target(), tree(t, initial))
	wantOps(t, tr.Flush(), `[]`)

	xs.Splice(0, 0, float64(9))
	xs.Splice(1, 1)
	xs.Splice(-1, 1)
	xs.Splice(10, 5, float64(7))
	ops := tr.Flush()
	wantJSON(t, tr.Target(), tree(t, `{"xs": [9, 2, 7]}`))
	wantOps(t, ops, `[["s", ["xs", 0], 9], ["s", ["xs", 2], 7]]`)
	wantJSON(t, replay(t, initial, ops), tr.Target())
}

// delta.test.ts "tracker: root ops" → "normalises length = 0 on the root".
func TestTrackRootLengthZero(t *testing.T) {
	tr := tracked(t, `[1, 2, 3]`)
	tr.State().SetLen(0)
	ops := tr.Flush()
	wantOps(t, ops, `[["r", []]]`)
	wantTree(t, replay(t, `[1, 2, 3]`, ops), `[]`)
}

// delta.test.ts "tracker: root ops" → "round-trips repeated nested splices".
func TestTrackRoundTripsRepeatedNestedSplices(t *testing.T) {
	initial := `{"xs": [1]}`
	tr := tracked(t, initial)
	for i := 2; i <= 100; i++ {
		tr.State().At("xs").Push(float64(i))
	}
	wantJSON(t, replay(t, initial, tr.Flush()), tr.Target())
}

// delta.test.ts "tracker: root ops" → the reindexing round trips: "does not
// fold a child path across a parent splice", "drops child ops when repeated
// parent splices collapse to a snapshot", "does not let a post-splice set
// dominate an earlier element append", "does not discard a nested append
// through a reindexed set", "preserves element operations across middle-array
// insertions".
func TestTrackReindexingRoundTrips(t *testing.T) {
	cases := []struct {
		name    string
		initial string
		mutate  func(xs State)
	}{
		{"child path across a parent splice", `{"xs": ["ab", "cd"]}`, func(xs State) {
			xs.Set(0, xs.Get(0).(string)+"x")
			xs.Shift()
			xs.Set(0, xs.Get(0).(string)+"y")
		}},
		{"repeated parent splices collapse to a snapshot", `{"xs": ["ab"]}`, func(xs State) {
			xs.Push("q")
			xs.Set(0, xs.Get(0).(string)+"cd")
			xs.Push("z")
		}},
		{"post-splice set after an element append", `{"xs": ["ab"]}`, func(xs State) {
			xs.Set(0, xs.Get(0).(string)+"x")
			xs.Unshift("q")
			xs.Set(0, "Z")
		}},
		{"nested append through a reindexed set", `{"xs": [{"k": "a"}]}`, func(xs State) {
			xs.At(0).Set("k", xs.At(0).Get("k").(string)+"x")
			xs.Unshift(float64(9))
			xs.Set(0, float64(7))
		}},
		{"element ops across middle insertions", `{"xs": ["a", "b"]}`, func(xs State) {
			xs.Set(1, xs.Get(1).(string)+"x")
			xs.Splice(1, 0, "inserted")
			xs.Set(1, "changed")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := tracked(t, tc.initial)
			tc.mutate(tr.State().At("xs"))
			wantJSON(t, replay(t, tc.initial, tr.Flush()), tr.Target())
		})
	}
}

// lcg is upstream's deterministic generator: (seed * 1664525 + 1013904223) >>> 0.
type lcg uint32

func (g *lcg) next() int {
	*g = *g*1664525 + 1013904223
	return int(*g)
}

// delta.test.ts "tracker: root ops" → "round-trips deterministic element
// mutations across reindexing splices".
func TestTrackDeterministicMutationsAcrossReindexing(t *testing.T) {
	seed := lcg(0x1234abcd)
	initial := `{"xs": [{"k": "a"}, {"k": "b"}, {"k": "c"}]}`
	for round := range 200 {
		tr := tracked(t, initial)
		xs := tr.State().At("xs")
		for step := range 30 {
			index := seed.next() % xs.Len()
			switch seed.next() % 5 {
			case 0:
				el := xs.At(index)
				el.Set("k", el.Get("k").(string)+string(rune('a'+seed.next()%26)))
			case 1:
				xs.Set(index, map[string]any{"k": fmt.Sprintf("set-%d-%d", round, step)})
			case 2:
				xs.Unshift(map[string]any{"k": fmt.Sprintf("head-%d-%d", round, step)})
			case 3:
				if xs.Len() > 1 {
					xs.Shift()
				}
			default:
				at := seed.next() % (xs.Len() + 1)
				remove := 0
				if xs.Len() > 1 && seed.next()%2 == 0 {
					remove = 1
				}
				xs.Splice(at, remove, map[string]any{"k": fmt.Sprintf("mid-%d-%d", round, step)})
			}
			if xs.Len() > 10 {
				xs.Shift()
			}
		}
		wantJSON(t, replay(t, initial, tr.Flush()), tr.Target())
	}
}

// delta.test.ts "tracker: root ops" → "keeps mutator chaining tracked".
func TestTrackSortThenPush(t *testing.T) {
	initial := `{"xs": [3, 1, 2]}`
	tr := tracked(t, initial)
	xs := tr.State().At("xs")
	xs.Sort(func(a, b any) int {
		return int(a.(float64) - b.(float64))
	})
	xs.Push(float64(4))
	ops := tr.Flush()
	wantJSON(t, tr.Target(), tree(t, `{"xs": [1, 2, 3, 4]}`))
	wantJSON(t, replay(t, initial, ops), tr.Target())
}

// delta.test.ts "tracker: root ops" → "combines retained element edits with
// one append".
func TestTrackCombinesRetainedEditsWithOneAppend(t *testing.T) {
	initial := `{"messages": [{"text": "a"}, {"text": "b"}], "flag": 0}`
	tr := tracked(t, initial)
	s := tr.State()
	messages := s.At("messages")
	concat(messages.At(0), "text", "x")
	s.Set("flag", float64(1))
	messages.Push(map[string]any{"text": "c"})
	concat(messages.At(1), "text", "y")
	concat(messages.At(2), "text", "z")
	ops := tr.Flush()
	splices := []Op{}
	for _, op := range ops {
		switch op := op.(type) {
		case Splice:
			splices = append(splices, op)
		case Set:
			if len(op.Path) == 1 && op.Path[0] == Key("messages") {
				t.Errorf("the array was replaced whole: %s", jsonText(t, op))
			}
		}
		if _, ok := op.(Replace); !ok {
			for _, seg := range pathOf(op) {
				if seg == Index(2) {
					t.Errorf("an op addressed the pushed element: %s", jsonText(t, op))
				}
			}
		}
	}
	wantOps(t, splices, `[["p", ["messages"], 2, 0, [{"text": "cz"}]]]`)
	wantJSON(t, replay(t, initial, ops), tr.Target())
	// The whole batch, as pi emits it: flag first, because messages was
	// re-touched after it and a re-touched path is emitted last.
	wantOps(t, ops, `[
		["s", ["flag"], 1],
		["a", ["messages", 0, "text"], "x"],
		["a", ["messages", 1, "text"], "y"],
		["p", ["messages"], 2, 0, [{"text": "cz"}]]
	]`)
}

// pathOf is the path an op carries; nil for a Replace.
func pathOf(op Op) Path {
	switch op := op.(type) {
	case Set:
		return op.Path
	case Delete:
		return op.Path
	case Append:
		return op.Path
	case Truncate:
		return op.Path
	case Splice:
		return op.Path
	}
	return nil
}

// delta.test.ts "tracker: root ops" → "collapses many pushes into one append".
func TestTrackCollapsesManyPushes(t *testing.T) {
	tr := tracked(t, `{"xs": [1]}`)
	for value := 2; value <= 100; value++ {
		tr.State().At("xs").Push(float64(value))
	}
	items := make([]string, 0, 99)
	for value := 2; value <= 100; value++ {
		items = append(items, fmt.Sprint(value))
	}
	wantOps(t, tr.Flush(), `[["p", ["xs"], 1, 0, [`+strings.Join(items, ",")+`]]]`)
}

// delta.test.ts "tracker: root ops" → "keeps direct tail writes and length
// growth in append mode".
func TestTrackTailWritesAndGrowthStayAppend(t *testing.T) {
	tr := tracked(t, `{"xs": [1]}`)
	xs := tr.State().At("xs")
	xs.Set(1, map[string]any{"value": float64(2)})
	xs.At(1).Set("value", float64(3))
	xs.SetLen(4)
	wantOps(t, tr.Flush(), `[["p", ["xs"], 1, 0, [{"value": 3}, null, null]]]`)
}

// delta.test.ts "tracker: root ops" → "keeps tail-only splices in append
// mode".
func TestTrackTailOnlySplicesStayAppend(t *testing.T) {
	tr := tracked(t, `{"xs": [{"value": 1}]}`)
	xs := tr.State().At("xs")
	xs.Push(map[string]any{"value": float64(2)}, map[string]any{"value": float64(3)})
	xs.Splice(1, 1, map[string]any{"value": float64(4)})
	xs.At(2).Set("value", float64(5))
	wantOps(t, tr.Flush(), `[["p", ["xs"], 1, 0, [{"value": 4}, {"value": 5}]]]`)
}

// delta.test.ts "tracker: root ops" → "forgets append-tail mutations that
// cancel out".
func TestTrackForgetsCancelledAppends(t *testing.T) {
	tr := tracked(t, `{"xs": [1]}`)
	xs := tr.State().At("xs")
	xs.Push(float64(2))
	xs.Push(float64(3))
	if v, ok := xs.Pop(); !ok || v != float64(3) {
		t.Errorf("pop got %v, %v", v, ok)
	}
	xs.Pop()
	wantOps(t, tr.Flush(), `[]`)
}

// delta.test.ts "tracker: root ops" → "accepts large append argument lists
// without spreading them internally".
func TestTrackLargeAppend(t *testing.T) {
	tr := tracked(t, `{"xs": []}`)
	items := make([]any, 100_000)
	if got := tr.State().At("xs").Push(items...); got != len(items) {
		t.Fatalf("push returned %d, want %d", got, len(items))
	}
	ops := tr.Flush()
	if len(ops) != 1 {
		t.Fatalf("got %d ops, want 1", len(ops))
	}
	sp, ok := ops[0].(Splice)
	if !ok || sp.Index != 0 || sp.Remove != 0 || len(sp.Items) != len(items) || sp.Path.String() != `["xs"]` {
		t.Errorf("got %#v", ops[0])
	}
}

// delta.test.ts "tracker: root ops" → "grows arrays with explicit null
// values".
func TestTrackGrowsWithNulls(t *testing.T) {
	initial := `{"xs": [1]}`
	tr := tracked(t, initial)
	tr.State().At("xs").SetLen(4)
	wantJSON(t, tr.Target(), tree(t, `{"xs": [1, null, null, null]}`))
	wantJSON(t, replay(t, initial, tr.Flush()), tr.Target())
}

// Structural changes to published elements, each as pi emits it: a pop or a
// shrink is a removing splice, a shift or unshift a head splice, and a shrink
// below a pending append's start keeps the retained edits.
func TestTrackStructuralOpsOnPublishedElements(t *testing.T) {
	cases := []struct {
		name    string
		initial string
		mutate  func(xs State)
		want    string
	}{
		{"pop", `{"xs": [1, 2, 3]}`, func(xs State) { xs.Pop() }, `[["p", ["xs"], 2, 1, []]]`},
		{"shift", `{"xs": [1, 2, 3]}`, func(xs State) { xs.Shift() }, `[["p", ["xs"], 0, 1, []]]`},
		{"unshift", `{"xs": [1, 2, 3]}`, func(xs State) { xs.Unshift(float64(0)) }, `[["p", ["xs"], 0, 0, [0]]]`},
		{"shrink", `{"xs": [1, 2, 3]}`, func(xs State) { xs.SetLen(2) }, `[["p", ["xs"], 2, 1, []]]`},
		{"push then shrink", `{"xs": [1, 2, 3]}`, func(xs State) { xs.Push(float64(4)); xs.SetLen(2) }, `[["p", ["xs"], 2, 1, []]]`},
		{"edit, push, shrink below start", `{"xs": [{"k": 1}, {"k": 2}]}`, func(xs State) {
			xs.At(0).Set("k", float64(5))
			xs.Push(map[string]any{"k": float64(3)})
			xs.SetLen(1)
		}, `[["s", ["xs", 0, "k"], 5], ["p", ["xs"], 1, 1, []]]`},
		{"grow", `{"xs": [1, 2, 3]}`, func(xs State) { xs.SetLen(5) }, `[["p", ["xs"], 3, 0, [null, null]]]`},
		{"same length", `{"xs": [1]}`, func(xs State) { xs.SetLen(1) }, `[]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := tracked(t, tc.initial)
			tc.mutate(tr.State().At("xs"))
			ops := tr.Flush()
			wantOps(t, ops, tc.want)
			wantJSON(t, replay(t, tc.initial, ops), tr.Target())
		})
	}
}

// A set carries a snapshot: the consumer's replica never aliases the
// producer's tree, whichever way the value got there.
func TestTrackSetPayloadIsASnapshot(t *testing.T) {
	tr := tracked(t, `{"item": null, "o": {"a": 1}}`)
	tr.State().Set("item", map[string]any{"value": float64(1)})
	tr.State().Set("o", map[string]any{"a": float64(2), "b": []any{float64(1)}})
	replica, err := Apply(tree(t, `{"item": null, "o": {"a": 1}}`), tr.Flush())
	if err != nil {
		t.Fatal(err)
	}
	r := replica.(map[string]any)
	r["item"].(map[string]any)["value"] = float64(9)
	r["o"].(map[string]any)["b"].([]any)[0] = float64(9)
	if got := tr.State().At("item").Get("value"); got != float64(1) {
		t.Errorf("the consumer's write reached the producer's item: %v", got)
	}
	if got := tr.State().At("o").At("b").Get(0); got != float64(1) {
		t.Errorf("the consumer's write reached the producer's array: %v", got)
	}
}

// ─── replacing the whole value ───────────────────────────────────────────────

// delta.test.ts "replacing the whole value" → "assigning to state emits a base
// batch and keeps tracking".
func TestTrackSetStateEmitsBaseAndKeepsTracking(t *testing.T) {
	tr := tracked(t, `{"p": 1, "q": "`+pad+`"}`)
	tr.State().Set("p", float64(2))
	tr.SetState(tree(t, `{"r": 9, "s": "new"}`))
	ops := tr.Flush()
	wantOps(t, ops, `[["r", {"r": 9, "s": "new"}]]`)
	if !IsBase(ops) {
		t.Error("not a base batch")
	}
	wantJSON(t, replay(t, `{}`, ops), tr.Target())

	tr.State().Set("r", float64(10)) // the new value is tracked
	wantOps(t, tr.Flush(), `[["s", ["r"], 10]]`)
}

// delta.test.ts "replacing the whole value" → "assigning the tracked root to
// state rebases without poisoning the tracker".
func TestTrackSetStateWithOwnRootRebases(t *testing.T) {
	tr := tracked(t, `{"value": 1}`)
	tr.SetState(tr.Target())
	wantOps(t, tr.Flush(), `[["r", {"value": 1}]]`)
	tr.State().Set("value", float64(2))
	wantOps(t, tr.Flush(), `[["s", ["value"], 2]]`)
}

// delta.test.ts "replacing the whole value" → "allows a tracked child
// self-assignment as a no-op".
func TestTrackChildSelfAssignmentIsNoOp(t *testing.T) {
	tr := tracked(t, `{"child": {"value": 1}}`)
	child := tr.State().Get("child")
	tr.State().Set("child", child)
	if tr.Dirty() {
		t.Error("assigning a child back to its own slot marked the tracker dirty")
	}
	wantOps(t, tr.Flush(), `[]`)
}

// delta.test.ts "replacing the whole value" → "assigning to state discards
// ops recorded before it": they describe a value that no longer exists.
func TestTrackSetStateDiscardsEarlierOps(t *testing.T) {
	tr := tracked(t, `{"p": 1, "q": "`+pad+`"}`)
	tr.State().Set("p", float64(2))
	tr.SetState(tree(t, `{"z": 1}`))
	wantOps(t, tr.Flush(), `[["r", {"z": 1}]]`)
}

// delta.test.ts "replacing the whole value" → "a partial rewrite keeps its
// ops".
func TestTrackPartialRewriteKeepsOps(t *testing.T) {
	tr := tracked(t, `{"a": "`+strings.Repeat("x", 200)+`", "b": "`+strings.Repeat("y", 200)+`"}`)
	tr.State().Set("a", "p")
	ops := tr.Flush()
	if IsBase(ops) {
		t.Error("a partial rewrite is not a base batch")
	}
	wantOps(t, ops, `[["s", ["a"], "p"]]`)
}

// delta.test.ts "replacing the whole value" → "rebase() forces a base batch
// without changing the value" and "rebase() applies once, not to every later
// flush". This is the checkpoint: recovery replays from the last base batch,
// so a producer must be able to bound that.
func TestTrackRebase(t *testing.T) {
	tr := tracked(t, `{"p": 1, "pad": "`+pad+`"}`)
	tr.Rebase()
	ops := tr.Flush()
	if !IsBase(ops) {
		t.Error("rebase did not force a base batch")
	}
	wantJSON(t, replay(t, `{}`, ops), tr.Target())

	tr.Rebase()
	tr.Flush()
	tr.State().Set("p", float64(2))
	wantOps(t, tr.Flush(), `[["s", ["p"], 2]]`)
}

// ─── the first flush ─────────────────────────────────────────────────────────

// delta.test.ts "the first flush" → "is always a base batch": a consumer
// starts with nothing, so a stream opening with deltas has nothing to apply
// them to. It carries mutations made before it.
func TestTrackFirstFlushIsBase(t *testing.T) {
	tr := Track(tree(t, `{"x": 0}`))
	tr.State().Set("x", float64(100))
	ops := tr.Flush()
	if !IsBase(ops) {
		t.Error("first flush is not a base batch")
	}
	wantOps(t, ops, `[["r", {"x": 100}]]`)
}

// delta.test.ts "the first flush" → "is followed by deltas".
func TestTrackFirstFlushIsFollowedByDeltas(t *testing.T) {
	tr := tracked(t, `{"x": 0, "pad": "`+pad+`"}`)
	tr.State().Set("x", float64(1))
	wantOps(t, tr.Flush(), `[["s", ["x"], 1]]`)
}

// delta.test.ts "the first flush" → "emits nothing when untouched": the base
// batch is owed to a consumer, and there is no consumer until someone flushes.
func TestTrackUntouchedEmitsNothingAfterBase(t *testing.T) {
	tr := Track(tree(t, `{"x": 0}`))
	if !tr.Dirty() {
		t.Error("a fresh tracker owes its base batch")
	}
	wantOps(t, tr.Flush(), `[["r", {"x": 0}]]`)
	if tr.Dirty() {
		t.Error("dirty after the base batch")
	}
	wantOps(t, tr.Flush(), `[]`)
}

// delta.test.ts "the first flush" → "discard accepts pending changes into the
// local baseline".
func TestTrackDiscard(t *testing.T) {
	tr := tracked(t, `{"x": 0, "y": 0}`)
	tr.State().Set("x", float64(1))
	if !tr.Dirty() {
		t.Error("not dirty after a write")
	}
	tr.Discard()
	if tr.Dirty() {
		t.Error("dirty after discard")
	}
	wantOps(t, tr.Flush(), `[]`)
	tr.State().Set("y", float64(1))
	wantOps(t, tr.Flush(), `[["s", ["y"], 1]]`)
}

// ─── apply and fan-out ───────────────────────────────────────────────────────

// delta.test.ts "apply and fan-out" → "does not alias the producer".
func TestTrackDoesNotAliasProducer(t *testing.T) {
	tr := Track(tree(t, `{"x": 0}`))
	replica, err := Apply[map[string]any](nil, tr.Flush())
	if err != nil {
		t.Fatal(err)
	}
	tr.State().Set("x", float64(999))
	tr.Flush()
	if replica["x"] != float64(0) {
		t.Errorf("replica saw the producer's write: %v", replica["x"])
	}
}

// delta.test.ts "apply and fan-out" → "adopts assigned values without cloning
// them": a map inserted into state is tracker-owned, and a mutation through
// the tracker is visible through the caller's retained reference.
func TestTrackAdoptsAssignedValues(t *testing.T) {
	tr := Track(map[string]any{})
	replica, err := Apply[map[string]any](nil, tr.Flush())
	if err != nil {
		t.Fatal(err)
	}
	supplied := map[string]any{"value": float64(0)}
	tr.State().Set("item", supplied)
	tr.State().At("item").Set("value", float64(1))
	if supplied["value"] != float64(1) {
		t.Errorf("the inserted map was copied rather than adopted: %v", supplied)
	}
	replica, err = Apply(replica, tr.Flush())
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, replica, tr.Target())
}

// delta.test.ts "apply and fan-out" → "does not alias pushed values with an
// in-process consumer".
func TestTrackDoesNotAliasPushedValues(t *testing.T) {
	tr := Track(map[string]any{"xs": []any{}})
	replica, err := Apply[map[string]any](nil, tr.Flush())
	if err != nil {
		t.Fatal(err)
	}
	tr.State().At("xs").Push(map[string]any{"value": float64(1)})
	next, err := Apply(replica, tr.Flush())
	if err != nil {
		t.Fatal(err)
	}
	next["xs"].([]any)[0].(map[string]any)["value"] = float64(2)
	if got := tr.State().At("xs").At(0).Get("value"); got != float64(1) {
		t.Errorf("the consumer's write reached the producer: %v", got)
	}
}

// delta.test.ts "apply and fan-out" → "folds a whole stream without a
// base-batch branch", less the codec (S11): Apply handles "r" by replacing
// and tolerates a nil target, so a consumer needs no IsBase check.
func TestTrackFoldsWholeStream(t *testing.T) {
	tr := Track(map[string]any{"x": float64(0), "l": []any{}})
	var replica any
	send := func() {
		var err error
		if replica, err = Apply(replica, tr.Flush()); err != nil {
			t.Fatal(err)
		}
	}
	tr.State().Set("x", float64(100))
	tr.State().At("l").Push("xyz")
	send()
	tr.State().Set("x", float64(101))
	send()
	wantJSON(t, replica, tr.Target())
}

// ─── flush-time minimization ─────────────────────────────────────────────────

// Mutations only mark dirty paths. Flush compares the last published baseline
// with the current value, so repeated writes never accumulate pending ops.
func runMinimization(t *testing.T, mutate func(s State)) []Op {
	t.Helper()
	tr := tracked(t, `{"a": {"b": 1}, "x": 1, "y": 2}`)
	mutate(tr.State())
	return tr.Flush()
}

// delta.test.ts "flush-time minimization" → the six tabulated cases.
func TestTrackFlushTimeMinimization(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(s State)
		want   string
	}{
		{"collapses repeated writes to one field", func(s State) {
			s.Set("x", float64(1))
			s.Set("x", float64(2))
			s.Set("x", float64(3))
		}, `[["s", ["x"], 3]]`},
		// The later write moves x behind y: a re-touched path is emitted last.
		{"drops a write superseded by a later, non-adjacent one", func(s State) {
			s.Set("x", float64(10))
			s.Set("y", float64(20))
			s.Set("x", float64(30))
		}, `[["s", ["y"], 20], ["s", ["x"], 30]]`},
		{"drops a child write when the parent is replaced after it", func(s State) {
			s.At("a").Set("b", float64(99))
			s.Set("a", map[string]any{"c": float64(5)})
		}, `[["s", ["a", "c"], 5], ["d", ["a", "b"]]]`},
		{"folds a child write into its pending parent replacement", func(s State) {
			s.Set("a", map[string]any{"b": float64(1)})
			s.At("a").Set("b", float64(7))
		}, `[["s", ["a", "b"], 7]]`},
		{"collapses set-then-delete to the delete", func(s State) {
			s.Set("x", float64(5))
			s.Delete("x")
		}, `[["d", ["x"]]]`},
		// Delta replication preserves JSON values, not object insertion order.
		{"collapses delete-then-set to the final value", func(s State) {
			s.Delete("x")
			s.Set("x", float64(5))
		}, `[["s", ["x"], 5]]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantOps(t, runMinimization(t, tc.mutate), tc.want)
		})
	}
}

// delta.test.ts "flush-time minimization" → "derives an append after
// delete-then-recreate".
func TestTrackAppendAfterDeleteThenRecreate(t *testing.T) {
	tr := tracked(t, `{"x": "", "y": 1}`)
	s := tr.State()
	s.Delete("x")
	s.Set("x", "ab")
	concat(s, "x", "cd")
	wantOps(t, tr.Flush(), `[["a", ["x"], "abcd"]]`)
}

// delta.test.ts "flush-time minimization" → "is linear in the number of ops".
// The naive formulation compares every op against every dominator, which is
// quadratic and degrades on exactly the wide flush this pass cleans up.
func TestTrackFlushIsLinear(t *testing.T) {
	wide := func(n int) time.Duration {
		root := make(map[string]any, n)
		for i := range n {
			root[fmt.Sprintf("f%d", i)] = float64(i)
		}
		tr := Track(root)
		tr.Flush()
		started := time.Now()
		for i := range n {
			tr.State().Set(fmt.Sprintf("f%d", i), float64(i+1))
		}
		tr.Flush()
		return time.Since(started)
	}
	wide(200) // warm
	small := max(wide(250), 100*time.Microsecond)
	large := wide(2500)
	if ratio := float64(large) / float64(small); ratio >= 40 {
		t.Errorf("2500 fields took %v, 250 took %v: ratio %.1f, linear would be ~10", large, small, ratio)
	}
}

// delta.test.ts "flush-time minimization" → "collapses a pathological
// redundant producer".
func TestTrackCollapsesRedundantProducer(t *testing.T) {
	initial := `{"a": {"b": 1}, "x": 1}`
	tr := tracked(t, initial)
	s := tr.State()
	for i := range 2000 {
		s.Set("x", float64(i))
		s.At("a").Set("b", float64(i))
	}
	s.Set("a", map[string]any{"done": true})
	ops := tr.Flush()
	if len(ops) != 3 {
		t.Errorf("got %d ops, want 3: %s", len(ops), jsonText(t, ops))
	}
	wantJSON(t, replay(t, initial, ops), tr.Target())
}

// ─── flush ───────────────────────────────────────────────────────────────────

// delta.test.ts "flush" → "drops everything before a replacement".
func TestTrackFlushDropsEverythingBeforeReplacement(t *testing.T) {
	tr := tracked(t, `[1, 2]`)
	tr.State().Push(float64(3))
	tr.State().Splice(0, 3, float64(7))
	wantOps(t, tr.Flush(), `[["r", [7]]]`)
}

// delta.test.ts "flush" → "keeps the prefix when the replacement is nested":
// "s" on a subtree is not a root replacement, so earlier ops stay live.
func TestTrackFlushKeepsPrefixForNestedReplacement(t *testing.T) {
	tr := tracked(t, `{"a": 1, "xs": [1, 2, 3], "pad": "`+pad+`"}`)
	tr.State().Set("a", float64(2))
	tr.State().At("xs").Splice(0, 3, float64(9))
	wantOps(t, tr.Flush(), `[["s", ["a"], 2], ["s", ["xs"], [9]]]`)
}

// delta.test.ts "flush" → "clones and reads reserved value keys without
// invoking prototype setters". Reserved as segments, not as values: the value
// is carried whole, can be read, and cannot be mutated through that key.
func TestTrackReservedValueKeys(t *testing.T) {
	tr := Track(tree(t, `{"value": {"__proto__": {"z": 1}}}`))
	out, err := Apply[map[string]any](nil, tr.Flush())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out["value"].(map[string]any)["__proto__"]; !ok {
		t.Error("the reserved value key did not survive the base batch")
	}
	wantJSON(t, tr.Target(), tree(t, `{"value": {"__proto__": {"z": 1}}}`))
	if got := tr.State().At("value").At("__proto__").Get("z"); got != float64(1) {
		t.Errorf("reading through a reserved key got %v, want 1", got)
	}
	err = mustPanic(t, func() { tr.State().At("value").At("__proto__").Set("z", float64(2)) })
	var unsafe *UnsafePathError
	if !errors.As(err, &unsafe) || unsafe.Segment != Key("__proto__") {
		t.Errorf("got %v, want an UnsafePathError for __proto__", err)
	}
	err = mustPanic(t, func() { tr.State().At("value").Set("__proto__", float64(2)) })
	if !errors.As(err, &unsafe) {
		t.Errorf("got %v, want an UnsafePathError", err)
	}
	// A nested replacement that carries a reserved key is diffed no further
	// than the object holding it: the whole object is set.
	tr.Flush()
	tr.State().Set("value", tree(t, `{"__proto__": {"z": 2}}`))
	wantOps(t, tr.Flush(), `[["s", ["value"], {"__proto__": {"z": 2}}]]`)
}

// ─── safety: array indices ───────────────────────────────────────────────────

// delta.test.ts "safety: array indices" → "rejects sparse writes at the
// producer", "rejects array deletes at the producer". (Go has no undefined
// element to reject.)
func TestTrackRejectsSparseWritesAndArrayDeletes(t *testing.T) {
	tr := tracked(t, `{"xs": [1, 2, 3]}`)
	xs := tr.State().At("xs")
	err := mustPanic(t, func() { xs.Set(5, float64(9)) })
	var unsafe *UnsafePathError
	if !errors.As(err, &unsafe) || unsafe.Segment != Index(5) {
		t.Errorf("sparse write: got %v, want UnsafePathError{5}", err)
	}
	err = mustPanic(t, func() { xs.Delete(1) })
	if !strings.Contains(err.Error(), "sparse") {
		t.Errorf("array delete: got %v", err)
	}
	err = mustPanic(t, func() { xs.Set("k", float64(1)) })
	if !errors.As(err, &unsafe) || unsafe.Segment != Key("k") {
		t.Errorf("key on array: got %v, want UnsafePathError{k}", err)
	}
	err = mustPanic(t, func() { xs.SetLen(-1) })
	if err == nil {
		t.Error("negative length accepted")
	}
	wantJSON(t, tr.Target(), tree(t, `{"xs": [1, 2, 3]}`))
	wantOps(t, tr.Flush(), `[]`)
	// A numeric-string key on an array is the index it spells, as JS coerces it.
	xs.Set("0", float64(7))
	wantOps(t, tr.Flush(), `[["s", ["xs", 0], 7]]`)
}

// ─── tracker proxy boundaries ────────────────────────────────────────────────

// delta.test.ts "tracker proxy boundaries" → "accepts a value that merely
// looks like an op".
func TestTrackAcceptsValueThatLooksLikeAnOp(t *testing.T) {
	tr := tracked(t, `{"pad": "`+pad+`"}`)
	tr.State().Set("x", tree(t, `["r", {"evil": true}]`))
	wantOps(t, tr.Flush(), `[["s", ["x"], ["r", {"evil": true}]]]`)
}

// A cursor is not a value. Assigning one to its own slot is a no-op; anywhere
// else it would put one value at two live paths, which the tracker refuses.
func TestTrackCursorIsNotAValue(t *testing.T) {
	tr := tracked(t, `{"a": {"v": 1}, "b": 2}`)
	child := tr.State().At("a")
	tr.State().Set("a", child)
	if tr.Dirty() {
		t.Error("self-assignment of a cursor marked dirty")
	}
	if err := mustPanic(t, func() { tr.State().Set("b", child) }); err == nil {
		t.Error("a cursor was inserted as a value")
	}
	tr.State().Set("xs", []any{})
	tr.Flush()
	xs := tr.State().At("xs")
	for name, insert := range map[string]func(){
		"push":   func() { xs.Push(child) },
		"splice": func() { xs.Splice(0, 0, float64(1), child) },
		"fill":   func() { xs.Push(nil); xs.Fill(child, 0, 1) },
	} {
		if err := mustPanic(t, insert); err == nil {
			t.Errorf("%s inserted a cursor as an item", name)
		}
	}
	tr.State().Set("xs", []any{})
	tr.Flush()
	wantOps(t, tr.Flush(), `[]`)
}

// Track refuses a root that is not a container; the tracked value is a JSON
// object or array, as upstream's `T extends object` says.
func TestTrackRefusesScalarRoot(t *testing.T) {
	if err := mustPanic(t, func() { Track[any]("text") }); err == nil {
		t.Error("a string root was accepted")
	}
	tr := Track[any](map[string]any{})
	if err := mustPanic(t, func() { tr.SetState(float64(1)) }); err == nil {
		t.Error("a number root was accepted")
	}
}

// The remaining array mutators: shift, reverse, fill and copyWithin mark a
// diff and round-trip.
func TestTrackOtherArrayMutators(t *testing.T) {
	initial := `{"xs": [1, 2, 3, 4, 5]}`
	tr := tracked(t, initial)
	xs := tr.State().At("xs")
	if v, ok := xs.Shift(); !ok || v != float64(1) {
		t.Errorf("shift got %v, %v", v, ok)
	}
	xs.Reverse()
	wantJSON(t, xs.Value(), tree(t, `[5, 4, 3, 2]`))
	xs.Fill(float64(0), 1, 3)
	wantJSON(t, xs.Value(), tree(t, `[5, 0, 0, 2]`))
	xs.CopyWithin(0, 2, 4)
	wantJSON(t, xs.Value(), tree(t, `[0, 2, 0, 2]`))
	xs.Fill(float64(9), -1, 4)
	wantJSON(t, xs.Value(), tree(t, `[0, 2, 0, 9]`))
	ops := tr.Flush()
	wantOps(t, ops, `[["s", ["xs", 0], 0], ["s", ["xs", 2], 0], ["s", ["xs", 3], 9], ["p", ["xs"], 4, 1, []]]`)
	wantJSON(t, replay(t, initial, ops), tr.Target())

	// A structural change is not an append: same length, positional diff.
	xs.Reverse()
	wantOps(t, tr.Flush(), `[["s", ["xs", 0], 9], ["s", ["xs", 1], 0], ["s", ["xs", 2], 2], ["s", ["xs", 3], 0]]`)
	// An empty shift or pop marks nothing.
	tr.State().Set("xs", []any{})
	tr.Flush()
	if _, ok := xs.Pop(); ok {
		t.Error("pop on an empty array succeeded")
	}
	if _, ok := xs.Shift(); ok {
		t.Error("shift on an empty array succeeded")
	}
	if tr.Dirty() {
		t.Error("an empty pop or shift marked dirty")
	}
}

// A cursor to a missing path reads as absent and refuses to write.
func TestTrackMissingPath(t *testing.T) {
	tr := tracked(t, `{"a": {"v": 1}}`)
	ghost := tr.State().At("missing").At("deeper")
	if ghost.Value() != nil {
		t.Errorf("a missing path read %v", ghost.Value())
	}
	if _, ok := ghost.Lookup("x"); ok {
		t.Error("a missing path had a member")
	}
	err := mustPanic(t, func() { ghost.Set("x", float64(1)) })
	var pathErr *PathError
	if !errors.As(err, &pathErr) {
		t.Errorf("got %v, want a PathError", err)
	}
	err = mustPanic(t, func() { tr.State().At("a").At("v").Set("x", float64(1)) })
	if !errors.As(err, &pathErr) {
		t.Errorf("writing under a scalar got %v, want a PathError", err)
	}
	if err := mustPanic(t, func() { tr.State().Set(true, float64(1)) }); err == nil {
		t.Error("a bool segment was accepted")
	}
	wantOps(t, tr.Flush(), `[]`)
}

// An object's diff order is deterministic where pi's is insertion order:
// integer-like keys first ascending, then the rest by UTF-16 code unit. A key
// on an object is always a string segment, however it spells.
func TestTrackObjectDiffOrder(t *testing.T) {
	tr := tracked(t, `{"o": {}}`)
	tr.State().Set("o", map[string]any{
		"b": float64(1), "a": float64(2), "10": float64(3), "9": float64(4), "\uffff": float64(5), "😀": float64(6),
		"!": float64(7), "01": float64(8),
	})
	// "!" sorts below "0" by code unit but after every integer-like key; "01"
	// is not canonical, so it is a string key.
	wantOps(t, tr.Flush(), `[
		["s", ["o", "9"], 4], ["s", ["o", "10"], 3],
		["s", ["o", "!"], 7], ["s", ["o", "01"], 8],
		["s", ["o", "a"], 2], ["s", ["o", "b"], 1],
		["s", ["o", "😀"], 6], ["s", ["o", "\uffff"], 5]
	]`)
}

// ─── property tests ──────────────────────────────────────────────────────────

// delta.test.ts "property: flush-time tracking" → "converges across mixed
// nested writes, replacements, and array mutations", less the codec (S11).
func TestTrackPropertyConverges(t *testing.T) {
	seed := lcg(0x5eed1234)
	initial := `{"rows": [{"text": "a", "count": 0}, {"text": "b", "count": 0}], "meta": {"revision": 0}}`
	for round := range 100 {
		tr := Track(tree(t, initial))
		replica, err := Apply[any](nil, tr.Flush())
		if err != nil {
			t.Fatal(err)
		}
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
				if replica, err = Apply(replica, tr.Flush()); err != nil {
					t.Fatalf("round %d step %d: %v", round, step, err)
				}
				wantJSON(t, replica, tr.Target())
			}
		}
		if replica, err = Apply(replica, tr.Flush()); err != nil {
			t.Fatal(err)
		}
		wantJSON(t, replica, tr.Target())
	}
}

// delta.test.ts "property: random round-trip" → "a replica always matches the
// producer". 3000 random sequences found the root-splice hole on the first
// run, before any hand-written case did.
func TestTrackPropertyRandomRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(0x5eed))
	var rnd func(d int) any
	rnd = func(d int) any {
		r := rng.Float64()
		switch {
		case d > 2 || r < 0.3:
			return float64(rng.Intn(5))
		case r < 0.45:
			return []any{"x", "y", nil, true}[rng.Intn(4)]
		case r < 0.7:
			xs := make([]any, rng.Intn(4))
			for i := range xs {
				xs[i] = rnd(d + 1)
			}
			return xs
		}
		o := map[string]any{}
		for _, k := range []string{"a", "b", "c"} {
			if rng.Float64() < 0.6 {
				o[k] = rnd(d + 1)
			}
		}
		return o
	}
	checked := 0
	for range 3000 {
		base := rnd(0)
		next := rnd(0)
		var tr *Tracker[any]
		switch b := base.(type) {
		case []any:
			n, ok := next.([]any)
			if !ok {
				continue
			}
			tr = Track(cloneJSON(b))
			tr.Flush()
			tr.State().Splice(0, tr.State().Len(), n...)
		case map[string]any:
			n, ok := next.(map[string]any)
			if !ok {
				continue
			}
			tr = Track(cloneJSON(b))
			tr.Flush()
			for k := range b {
				if _, ok := n[k]; !ok {
					tr.State().Delete(k)
				}
			}
			for k, v := range n {
				tr.State().Set(k, v)
			}
		default:
			continue
		}
		got, err := Apply(cloneJSON(base), tr.Flush())
		if err != nil {
			t.Fatalf("base %s next %s: %v", jsonText(t, base), jsonText(t, next), err)
		}
		if g, w := jsonText(t, got), jsonText(t, tr.Target()); g != w {
			t.Fatalf("base %s next %s:\n got %s\nwant %s", jsonText(t, base), jsonText(t, next), g, w)
		}
		checked++
	}
	// Most random pairs are shape-incompatible and skipped; this only guards
	// against the loop silently checking nothing.
	if checked <= 200 {
		t.Errorf("checked only %d pairs", checked)
	}
}

// The tracker's options: a disabled overlap scan turns a rolling window into
// a whole-value set, as overlap(…, 0) is 0.
func TestTrackWithMaxOverlapScan(t *testing.T) {
	tr := Track(tree(t, `{"s": "abcdefgh"}`), WithMaxOverlapScan(0))
	tr.Flush()
	tr.State().Set("s", "defghxyz")
	wantOps(t, tr.Flush(), `[["s", ["s"], "defghxyz"]]`)

	tr = Track(tree(t, `{"s": "abcdefgh"}`), WithMaxOverlapScan(4))
	tr.Flush()
	tr.State().Set("s", "ghXYZ")
	wantOps(t, tr.Flush(), `[["t", ["s"], 6], ["a", ["s"], "XYZ"]]`)
}

// The baseline is advanced by sharing, not by replay, for scalars, strings
// and appends; the next flush must still see the true previous value. A long
// stream of window moves and appends exercises both paths.
func TestTrackBaselineStaysExactAcrossFlushes(t *testing.T) {
	initial := `{"out": "", "log": [], "n": {"v": 0}}`
	tr := tracked(t, initial)
	replica := tree(t, initial)
	s := tr.State()
	for i := range 300 {
		switch i % 4 {
		case 0:
			concat(s, "out", fmt.Sprintf("chunk%03d\n", i))
		case 1:
			out := s.Get("out").(string)
			if len(out) > 40 {
				s.Set("out", out[20:]+"|")
			}
		case 2:
			s.At("log").Push(map[string]any{"i": float64(i)})
			if s.At("log").Len() > 5 {
				s.At("log").Shift()
			}
		default:
			s.At("n").Set("v", float64(i))
			if i%8 == 3 {
				s.At("log").At(0).Set("touched", true)
			}
		}
		ops := tr.Flush()
		if i == 299 {
			wantOps(t, ops, `[["s", ["n", "v"], 299], ["s", ["log", 0, "touched"], true]]`)
		}
		var err error
		if replica, err = Apply(replica, ops); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		wantJSON(t, replica, tr.Target())
	}
	wantJSON(t, replica, tree(t, `{"out": "chunk288\n|chunk292\nchunk296\n|",
		"log": [{"i": 282, "touched": true}, {"i": 286}, {"i": 290}, {"i": 294}, {"i": 298}], "n": {"v": 299}}`))
}
