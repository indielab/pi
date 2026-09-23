package delta

import (
	"encoding/json"
	"errors"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"
	"weak"
)

// The upstream assertions ops goldens cannot carry: object identity (what is
// shared between revisions and what is copied), aliasing of the caller's
// values, the lifecycle errors as Go sentinels, and what a settled change
// retains. testdata/upstream_delta.json pins the ops, values and error text
// of the same cases against pi.

func mustBegin[T any](t *testing.T, tr *Tracker[T]) *Change[T] {
	t.Helper()
	c, err := tr.BeginChange()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func mustPrepare[T any](t *testing.T, c *Change[T]) *Prepared[T] {
	t.Helper()
	p, err := c.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func obj(v any) map[string]any { return v.(map[string]any) }

// delta.test.ts "keeps a draft alive across await and adopts only a prepared
// change": the committed value is a detached copy, and nothing reaches it
// until the prepared change is adopted.
func TestTrackerAdoptsOnlyAPreparedChange(t *testing.T) {
	input := map[string]any{"count": 1, "nested": map[string]any{"text": "a"}, "values": []any{1}}
	tr, err := Track(input)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, tr.Value(), input)
	if same(tr.Value(), input) || same(tr.Value()["nested"], input["nested"]) {
		t.Error("Track adopted the caller's containers; it must copy them")
	}
	if n := tr.Value()["count"]; n != 1.0 {
		t.Errorf("count = %#v, want float64(1): revisions hold JavaScript numbers", n)
	}

	c := mustBegin(t, tr)
	must(t, c.State().Set("count", 2))
	done := make(chan struct{})
	go func() { runtime.Gosched(); close(done) }()
	<-done // a draft outlives any amount of work between writes
	concat(t, c.State().At("nested"), "text", "b")
	_, err = c.State().At("values").Push(2)
	must(t, err)
	wantJSON(t, tr.Value(), input)

	p := mustPrepare(t, c)
	if !same(p.Base(), tr.Value()) {
		t.Error("Base is not the committed revision")
	}
	wantJSON(t, p.Value(), tree(t, `{"count": 2, "nested": {"text": "ab"}, "values": [1, 2]}`))
	wantJSON(t, tr.Value(), input)
	if err := mustPanic(t, func() { c.State().Get("count") }); !errors.Is(err, ErrDraftRevoked) {
		t.Errorf("read after Prepare: panic %v, want ErrDraftRevoked", err)
	}
	must(t, tr.Adopt(p))
	if !same(tr.Value(), p.Value()) {
		t.Error("Adopt did not commit the prepared revision itself")
	}
}

// delta.test.ts "normalizes a deep no-op to exact previous identity" and
// "copies replacement input and applies the same no-op normalization".
func TestTrackerNoOpKeepsIdentity(t *testing.T) {
	tr := mustTrack(t, `{"value": {"nested": [1, 2]}}`)
	c := mustBegin(t, tr)
	must(t, c.State().Set("value", map[string]any{"nested": []any{1, 2}}))
	p := mustPrepare(t, c)
	if !same(p.Value(), p.Base()) || len(p.Ops()) != 0 {
		t.Errorf("deep no-op: value is base %v, ops %s", same(p.Value(), p.Base()), jsonText(t, p.Ops()))
	}
	base := tr.Value()
	must(t, tr.Adopt(p))
	if !same(tr.Value(), base) {
		t.Error("adopting a no-op replaced the committed revision")
	}

	tr2 := mustTrack(t, `{"nested": {"value": 1}}`)
	replacement := map[string]any{"nested": map[string]any{"value": 2}}
	p2, err := tr2.PrepareReplace(replacement)
	if err != nil {
		t.Fatal(err)
	}
	obj(replacement["nested"])["value"] = 9
	if v := obj(obj(p2.Value())["nested"])["value"]; v != 2.0 {
		t.Errorf("prepared value saw the caller's later write: %v", v)
	}
	must(t, tr2.Adopt(p2))
	noop, err := tr2.PrepareReplace(tree(t, `{"nested": {"value": 2}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !same(noop.Value(), tr2.Value()) || len(noop.Ops()) != 0 {
		t.Error("an equal replacement is not the committed revision itself")
	}
}

// delta.test.ts "rejects foreign, stale, repeated, and overlapping changes",
// "invalidates a prepared result when its change is aborted", "makes
// competing same-base preparations stale after adopting a no-op": each
// refusal is its sentinel, and a refusal changes nothing.
func TestTrackerLifecycleErrors(t *testing.T) {
	first := mustTrack(t, `{"value": 0}`)
	second := mustTrack(t, `{"value": 0}`)
	c := mustBegin(t, first)
	if _, err := first.BeginChange(); !errors.Is(err, ErrChangeActive) {
		t.Errorf("second BeginChange: %v", err)
	}
	if _, err := first.PrepareReplace(tree(t, `{"value": 5}`)); !errors.Is(err, ErrChangeActive) {
		t.Errorf("PrepareReplace during a change: %v", err)
	}
	must(t, c.State().Set("value", 1))
	p := mustPrepare(t, c)
	if _, err := c.Prepare(); !errors.Is(err, ErrChangeSettled) {
		t.Errorf("second Prepare: %v", err)
	}
	if err := second.Adopt(p); !errors.Is(err, ErrForeignPrepared) {
		t.Errorf("foreign Adopt: %v", err)
	}
	if err := second.Adopt(nil); !errors.Is(err, ErrForeignPrepared) {
		t.Errorf("nil Adopt: %v", err)
	}
	must(t, first.Adopt(p))
	if err := first.Adopt(p); !errors.Is(err, ErrPreparedUsed) {
		t.Errorf("repeated Adopt: %v", err)
	}

	stale, err := first.PrepareReplace(tree(t, `{"value": 2}`))
	must(t, err)
	winner, err := first.PrepareReplace(tree(t, `{"value": 3}`))
	must(t, err)
	must(t, first.Adopt(winner))
	for range 2 {
		if err := first.Adopt(stale); !errors.Is(err, ErrStalePrepared) {
			t.Errorf("stale Adopt: %v", err)
		}
	}
	wantJSON(t, first.Value(), tree(t, `{"value": 3}`))

	waiting, err := first.PrepareReplace(tree(t, `{"value": 4}`))
	must(t, err)
	open := mustBegin(t, first)
	if err := first.Adopt(waiting); !errors.Is(err, ErrAdoptDuringChange) {
		t.Errorf("Adopt during a change: %v", err)
	}
	open.Abort()
	must(t, first.Adopt(waiting))

	aborted := mustBegin(t, first)
	must(t, aborted.State().Set("value", 9))
	pa := mustPrepare(t, aborted)
	aborted.Abort()
	aborted.Abort()
	if err := first.Adopt(pa); !errors.Is(err, ErrPreparedAborted) {
		t.Errorf("Adopt after Abort: %v", err)
	}

	noopTracker := mustTrack(t, `{"value": {"count": 1}}`)
	a, err := noopTracker.PrepareReplace(tree(t, `{"value": {"count": 1}}`))
	must(t, err)
	b, err := noopTracker.PrepareReplace(tree(t, `{"value": {"count": 1}}`))
	must(t, err)
	must(t, noopTracker.Adopt(a))
	if err := noopTracker.Adopt(b); !errors.Is(err, ErrStalePrepared) {
		t.Errorf("competing no-op: %v, want stale", err)
	}
}

// noPanic runs fn and fails the test, rather than crashing it, if fn panics.
func noPanic(t *testing.T, what string, fn func() error) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", what, r)
			err = nil
		}
	}()
	return fn()
}

// Go only: the zero values of the exported types. A zero Tracker holds no
// revision and a zero Prepared was prepared by no tracker, so each call
// reports how to get a real one instead of panicking — the zero Prepared as
// upstream's adopt reports any object that is not a PreparedImpl ("belongs to
// a different tracker", tracker.ts:135-136).
func TestTrackerZeroValues(t *testing.T) {
	var zero Tracker[any]
	if err := noPanic(t, "BeginChange on a zero Tracker", func() error { _, err := zero.BeginChange(); return err }); !errors.Is(err, ErrZeroTracker) {
		t.Errorf("BeginChange on a zero Tracker: %v, want ErrZeroTracker", err)
	}
	if err := noPanic(t, "PrepareReplace on a zero Tracker", func() error {
		_, err := zero.PrepareReplace(map[string]any{})
		return err
	}); !errors.Is(err, ErrZeroTracker) {
		t.Errorf("PrepareReplace on a zero Tracker: %v, want ErrZeroTracker", err)
	}
	tr := mustTrack(t, `{"value": 0}`)
	p, err := tr.PrepareReplace(tree(t, `{"value": 1}`))
	must(t, err)
	if err := noPanic(t, "Adopt on a zero Tracker", func() error { return zero.Adopt(p) }); !errors.Is(err, ErrZeroTracker) {
		t.Errorf("Adopt on a zero Tracker: %v, want ErrZeroTracker", err)
	}
	if err := noPanic(t, "Adopt of a zero Prepared", func() error { return tr.Adopt(&Prepared[any]{}) }); !errors.Is(err, ErrForeignPrepared) {
		t.Errorf("Adopt of a zero Prepared: %v, want ErrForeignPrepared", err)
	}
	must(t, tr.Adopt(p)) // the refusals changed nothing

	var change Change[any]
	if err := noPanic(t, "Prepare of a zero Change", func() error { _, err := change.Prepare(); return err }); err == nil || !strings.Contains(err.Error(), "BeginChange") {
		t.Errorf("Prepare of a zero Change: %v, want an error naming BeginChange", err)
	}
	noPanic(t, "Abort of a zero Change", func() error { change.Abort(); return nil })
	if err := noPanic(t, "Set through a zero Change's State", func() error { return change.State().Set("x", 1) }); !errors.Is(err, errNilDraft) {
		t.Errorf("Set through a zero Change's State: %v, want errNilDraft", err)
	}
	var draft Draft
	if err := noPanic(t, "Set on a zero Draft", func() error { return draft.Set("x", 1) }); !errors.Is(err, errNilDraft) {
		t.Errorf("Set on a zero Draft: %v, want errNilDraft", err)
	}
	noPanic(t, "reading a zero Draft", func() error {
		if draft.Len() != 0 || draft.Keys() != nil || draft.Has("x") {
			t.Error("a zero Draft reads as non-empty")
		}
		return nil
	})
}

// delta.test.ts "aborts and revokes without changing the committed value" and
// "releases the tracker and revokes drafts when preparation fails".
func TestTrackerAbortAndFailedPrepareRelease(t *testing.T) {
	tr := mustTrack(t, `{"child": {"value": 1}, "values": [1, 2]}`)
	c := mustBegin(t, tr)
	child := c.State().At("child")
	must(t, child.Set("value", 2))
	c.Abort()
	wantJSON(t, tr.Value(), tree(t, `{"child": {"value": 1}, "values": [1, 2]}`))
	if err := mustPanic(t, func() { child.Get("value") }); !errors.Is(err, ErrDraftRevoked) {
		t.Errorf("read after Abort: %v", err)
	}
	if err := child.Set("value", 3); !errors.Is(err, ErrDraftRevoked) {
		t.Errorf("write after Abort: %v", err)
	}
	c.Abort()
	if _, err := c.Prepare(); !errors.Is(err, ErrChangeSettled) {
		t.Errorf("Prepare after Abort: %v", err)
	}

	c = mustBegin(t, tr)
	must(t, c.State().At("values").SetLen(4))
	_, err := c.Prepare()
	var ve *ValueError
	if !errors.As(err, &ve) || !strings.Contains(err.Error(), "dense") {
		t.Fatalf("Prepare over holes: %v, want the dense-array ValueError", err)
	}
	if err := mustPanic(t, func() { c.State().Len() }); !errors.Is(err, ErrDraftRevoked) {
		t.Errorf("draft after a failed Prepare: %v", err)
	}
	c.Abort()
	if _, err := c.Prepare(); !errors.Is(err, ErrChangeSettled) {
		t.Errorf("Prepare after a failed Prepare: %v", err)
	}
	mustBegin(t, tr).Abort() // the tracker was released
}

// delta.test.ts "keeps large prepareReplace array edits narrow": the
// replacement is imported (so shares nothing with the revision) and still
// diffs to one leaf op. testdata pins the op itself.
func TestTrackerPrepareReplaceIsNarrowAndReplays(t *testing.T) {
	rows := make([]any, 10_000)
	for i := range rows {
		rows[i] = map[string]any{"value": i, "stable": map[string]any{"value": i}}
	}
	tr, err := Track(map[string]any{"rows": rows})
	must(t, err)
	replacement := append([]any(nil), tr.Value()["rows"].([]any)...)
	replacement[5_000] = map[string]any{"value": -1, "stable": obj(replacement[5_000])["stable"]}
	p, err := tr.PrepareReplace(map[string]any{"rows": replacement})
	must(t, err)
	wantOps(t, p.Ops(), `[["s", ["rows", 5000, "value"], -1]]`)
	replica, err := ApplyImmutable(p.Base(), p.Ops())
	must(t, err)
	wantJSON(t, replica, p.Value())
}

// delta-clone.test.ts "deeply detaches and freezes the imported revision" and
// "preserves null prototypes and expands aliases"; state-value.test.ts
// "imports detached frozen JSON and expands aliases".
func TestTrackerImportDetachesAndExpandsAliases(t *testing.T) {
	shared := map[string]any{"nested": []any{map[string]any{"n": 1}}}
	input := map[string]any{
		"point": map[string]any{"x": 3, "y": 7, "pressure": 0.1},
		"rows":  []any{map[string]any{"values": []any{0, false, nil, "text", map[string]any{"n": 1}}}},
		"left":  shared,
		"right": shared,
	}
	tr, err := Track(input)
	must(t, err)
	v := tr.Value()
	if same(v["point"], input["point"]) || same(v["rows"], input["rows"]) {
		t.Error("imported containers are the caller's")
	}
	if same(v["left"], v["right"]) || same(obj(v["left"])["nested"], obj(v["right"])["nested"]) {
		t.Error("an alias in the input is still one container in the revision")
	}
	obj(input["point"])["x"] = 99
	if x := obj(v["point"])["x"]; x != 3.0 {
		t.Errorf("revision saw the caller's later write: %v", x)
	}
}

// Numbers of every Go kind are one JavaScript number, and a typed nil
// container is an empty one — the tracker's representation, whatever Go
// handed it. Anything else with no JSON form is refused with pi's text.
func TestTrackerImportRepresentation(t *testing.T) {
	type myInt int
	type myFloat float64
	tr, err := Track(map[string]any{
		"int": 1, "int64": int64(2), "uint8": uint8(3), "float32": float32(0.5), "named": myInt(4),
		"namedFloat": myFloat(6.5), "number": json.Number("5e2"), "nilMap": map[string]any(nil), "nilSlice": []any(nil),
	})
	must(t, err)
	wantJSON(t, tr.Value(), tree(t, `{"int":1,"int64":2,"uint8":3,"float32":0.5,"named":4,"namedFloat":6.5,"number":500,"nilMap":{},"nilSlice":[]}`))
	for k, v := range tr.Value() {
		switch v.(type) {
		case float64, map[string]any, []any:
		default:
			t.Errorf("%s imported as %T", k, v)
		}
	}
	if obj := tr.Value()["nilMap"].(map[string]any); obj == nil {
		t.Error("a nil map imported as nil")
	}

	cases := []struct {
		value any
		text  string
	}{
		{math.NaN(), "Replicated state values must be strict JSON"},
		{math.Inf(-1), "Replicated state values must be strict JSON"},
		{json.Number("1e400"), "Replicated state values must be strict JSON"},
		// Every numeric kind is checked, not only float64: pi's assertPrimitive
		// is Number.isFinite on the one number type.
		{float32(math.NaN()), "Replicated state values must be strict JSON"},
		{float32(math.Inf(1)), "Replicated state values must be strict JSON"},
		{float32(math.Inf(-1)), "Replicated state values must be strict JSON"},
		{json.Number("NaN"), "Replicated state values must be strict JSON"},
		{json.Number("Inf"), "Replicated state values must be strict JSON"},
		{json.Number("-Infinity"), "Replicated state values must be strict JSON"},
		{func() {}, "Replicated state values must be strict JSON"},
		{complex(1, 2), "Replicated state values must be strict JSON"},
		{make(chan int), "Replicated state values must be strict JSON"},
		{time.Unix(0, 0), "Replicated state containers must be plain objects or arrays"},
		{&struct{}{}, "Replicated state containers must be plain objects or arrays"},
		{map[string]string{"a": "b"}, "Replicated state containers must be plain objects or arrays"},
		{[]string{"a"}, "Replicated state containers must be plain objects or arrays"},
		{[]byte("a"), "Replicated state containers must be plain objects or arrays"},
	}
	for _, tc := range cases {
		_, err := Track(map[string]any{"v": tc.value})
		var ve *ValueError
		if !errors.As(err, &ve) || ve.Message != tc.text {
			t.Errorf("Track(%T %v): %v, want %q", tc.value, tc.value, err, tc.text)
		}
	}
	// The same refusals through PrepareReplace and a draft write, which says
	// so in draft.ts's words; the committed revision is untouched.
	tr, err = Track(map[string]any{"v": 1, "xs": []any{}})
	must(t, err)
	for _, bad := range []any{float32(math.NaN()), float32(math.Inf(1)), json.Number("NaN")} {
		var ve *ValueError
		if _, err := tr.PrepareReplace(map[string]any{"v": bad}); !errors.As(err, &ve) || ve.Message != "Replicated state values must be strict JSON" {
			t.Errorf("PrepareReplace(%T %v): %v", bad, bad, err)
		}
		c := mustBegin(t, tr)
		if err := c.State().Set("v", bad); !errors.As(err, &ve) || ve.Message != "Assigned values must be strict JSON values" {
			t.Errorf("Set(%T %v): %v", bad, bad, err)
		}
		if _, err := c.State().At("xs").Push(bad); !errors.As(err, &ve) || ve.Message != "Assigned values must be strict JSON values" {
			t.Errorf("Push(%T %v): %v", bad, bad, err)
		}
		c.Abort()
	}
	wantJSON(t, tr.Value(), tree(t, `{"v": 1, "xs": []}`))
	// The error names the number, whatever kind it came as.
	if _, err := Track(map[string]any{"v": float32(math.Inf(-1))}); err == nil || !strings.Contains(err.Error(), "got -Inf") {
		t.Errorf("float32 -Inf: %v, want the error to say it got -Inf", err)
	}
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	if _, err := Track(cyclic); err == nil || !strings.Contains(err.Error(), "Replicated state cannot contain cycles") {
		t.Errorf("cycle: %v", err)
	}
	loop := make([]any, 1)
	loop[0] = loop
	if _, err := Track(map[string]any{"v": loop}); err == nil || !strings.Contains(err.Error(), "cycles") {
		t.Errorf("array cycle: %v", err)
	}
	for _, root := range []any{nil, 1.0, "x", true} {
		if _, err := Track(root); err == nil || !strings.Contains(err.Error(), "JSON object") {
			t.Errorf("Track(%#v): %v, want a root error", root, err)
		}
	}
}

// delta-clone.test.ts "copies assigned and inserted values immediately" and
// state.test.ts "copies assigned values by value": one Go map assigned twice
// becomes two values, and neither sees the caller's later writes.
func TestDraftCopiesAssignedValues(t *testing.T) {
	tr := mustTrack(t, `{"rows": [], "left": null, "right": null}`)
	assigned := map[string]any{"nested": map[string]any{"value": 1}}
	c := mustBegin(t, tr)
	_, err := c.State().At("rows").Push(assigned, assigned)
	must(t, err)
	must(t, c.State().Set("left", assigned))
	must(t, c.State().Set("right", assigned))
	obj(assigned["nested"])["value"] = 9
	if v, _ := c.State().At("rows").At(0).At("nested").Get("value"); v != 1.0 {
		t.Errorf("draft saw the caller's write: %v", v)
	}
	must(t, c.State().At("left").At("nested").Set("value", 2))
	p := mustPrepare(t, c)
	v := p.Value().(map[string]any)
	rows := v["rows"].([]any)
	if same(rows[0], rows[1]) || same(v["left"], v["right"]) {
		t.Error("repeated placements share one container")
	}
	wantJSON(t, v, tree(t, `{"rows": [{"nested": {"value": 1}}, {"nested": {"value": 1}}], "left": {"nested": {"value": 2}}, "right": {"nested": {"value": 1}}}`))
	replica, err := ApplyImmutable(p.Base(), p.Ops())
	must(t, err)
	wantJSON(t, replica, v)
}

// state-draft.test.ts "copies only changed branches" and state-value.test.ts
// "commits transaction copies in place while sharing unchanged branches".
func TestDraftCopiesOnlyChangedBranches(t *testing.T) {
	tr := mustTrack(t, `{"changed": {"count": 1, "sibling": {"value": "kept"}}, "untouched": {"value": 2}}`)
	base := tr.Value().(map[string]any)
	c := mustBegin(t, tr)
	first := c.State().At("changed")
	if c.State().At("changed") != first {
		t.Error("two reads of one member are two drafts")
	}
	must(t, first.Set("count", 3))
	p := mustPrepare(t, c)
	v := p.Value().(map[string]any)
	switch {
	case same(v, base):
		t.Error("the root was not copied")
	case same(v["changed"], base["changed"]):
		t.Error("the changed branch was not copied")
	case !same(obj(v["changed"])["sibling"], obj(base["changed"])["sibling"]):
		t.Error("an unchanged sibling was copied")
	case !same(v["untouched"], base["untouched"]):
		t.Error("an untouched branch was copied")
	}
}

// A copy that ends up equal to its base is dropped, so a branch written and
// restored keeps its identity even when the change has other effects.
func TestDraftRestoredBranchKeepsIdentity(t *testing.T) {
	tr := mustTrack(t, `{"a": 1, "o": {"x": 1}, "v": [1, 2]}`)
	base := tr.Value().(map[string]any)
	c := mustBegin(t, tr)
	must(t, c.State().Set("a", 2))
	must(t, c.State().At("o").Set("x", 5))
	must(t, c.State().At("o").Set("x", 1))
	_, err := c.State().At("v").Push(3)
	must(t, err)
	_, _, err = c.State().At("v").Pop()
	must(t, err)
	p := mustPrepare(t, c)
	v := p.Value().(map[string]any)
	if !same(v["o"], base["o"]) || !same(v["v"], base["v"]) {
		t.Error("a written-and-restored branch lost its identity")
	}
	wantOps(t, p.Ops(), `[["s", ["a"], 2]]`)
}

// Two empty arrays are two arrays. A Go slice's identity is its backing array,
// and two make([]any, 0) share one zero-size address — so the tracker gives
// every array it owns a slot of its own, or writes to one empty array would
// land in the other.
func TestDraftEmptyArraysAreDistinct(t *testing.T) {
	tr := mustTrack(t, `{"a": [], "b": [], "c": {"d": [], "e": []}}`)
	c := mustBegin(t, tr)
	if c.State().At("a") == c.State().At("b") {
		t.Fatal("two empty arrays share one draft")
	}
	_, err := c.State().At("a").Push(1)
	must(t, err)
	_, err = c.State().At("c").At("e").Push(2)
	must(t, err)
	must(t, c.State().Set("f", []any{}))
	must(t, c.State().Set("g", []any{}))
	_, err = c.State().At("g").Push(3)
	must(t, err)
	p := mustPrepare(t, c)
	wantJSON(t, p.Value(), tree(t, `{"a": [1], "b": [], "c": {"d": [], "e": [2]}, "f": [], "g": [3]}`))
	must(t, tr.Adopt(p))
	c = mustBegin(t, tr)
	_, err = c.State().At("f").Push(4)
	must(t, err)
	wantJSON(t, mustPrepare(t, c).Value(), tree(t, `{"a": [1], "b": [], "c": {"d": [], "e": [2]}, "f": [4], "g": [3]}`))
}

// state.test.ts "expands reused owned subtrees into independent placements":
// a replacement that reuses one of the revision's own subtrees twice is
// imported as two.
func TestTrackerReusedSubtreesBecomeIndependent(t *testing.T) {
	tr := mustTrack(t, `{"left": {"value": 1}, "right": {"value": 2}}`)
	left := obj(tr.Value())["left"]
	p, err := tr.PrepareReplace(map[string]any{"left": left, "right": left})
	must(t, err)
	must(t, tr.Adopt(p))
	v := obj(tr.Value())
	if same(v["left"], v["right"]) || same(v["left"], left) {
		t.Error("a reused subtree is shared")
	}
	c := mustBegin(t, tr)
	must(t, c.State().At("left").Set("value", 9))
	wantJSON(t, mustPrepare(t, c).Value(), tree(t, `{"left": {"value": 9}, "right": {"value": 1}}`))
}

// A nil draft — what At returns for a member that is absent or a scalar —
// reads as empty, and every write to it reports the missing link.
func TestDraftNilAndKindErrors(t *testing.T) {
	tr := mustTrack(t, `{"n": 1, "o": {}, "v": []}`)
	c := mustBegin(t, tr)
	for _, key := range []string{"missing", "n"} {
		d := c.State().At(key)
		if d != nil {
			t.Fatalf("At(%q) = %v, want nil", key, d)
		}
		if d.Len() != 0 || d.Keys() != nil || d.IsArray() || d.Has("x") {
			t.Errorf("nil draft reads as non-empty")
		}
		if err := d.Set("x", 1); !errors.Is(err, errNilDraft) {
			t.Errorf("Set on nil draft: %v", err)
		}
		if _, err := d.Push(1); !errors.Is(err, errNilDraft) {
			t.Errorf("Push on nil draft: %v", err)
		}
	}
	o := c.State().At("o")
	for name, call := range map[string]func() error{
		"Push":       func() error { _, err := o.Push(1); return err },
		"Pop":        func() error { _, _, err := o.Pop(); return err },
		"Shift":      func() error { _, _, err := o.Shift(); return err },
		"Unshift":    func() error { _, err := o.Unshift(1); return err },
		"Splice":     func() error { _, err := o.Splice(0, 0); return err },
		"Sort":       func() error { return o.Sort(nil) },
		"Reverse":    func() error { return o.Reverse() },
		"Fill":       func() error { return o.Fill(1, 0, 0) },
		"CopyWithin": func() error { return o.CopyWithin(0, 0, 0) },
		"SetLen":     func() error { return o.SetLen(0) },
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), name+" on an object draft") {
			t.Errorf("%s on an object: %v", name, err)
		}
	}
	if err := c.State().Set(true, 1); !errors.Is(err, ErrInvalidOp) {
		t.Errorf("Set with a bool key: %v", err)
	}
	c.Abort()
	wantJSON(t, tr.Value(), tree(t, `{"n": 1, "o": {}, "v": []}`))
}

// ─── Retention ───────────────────────────────────────────────────────────────
//
// delta-retention.test.ts: what a settled change, an unadopted prepared
// change and a revoked draft may still hold.

func collected[T any](t *testing.T, w weak.Pointer[T], what string) {
	t.Helper()
	for range 30 {
		runtime.GC()
		if w.Value() == nil {
			return
		}
	}
	t.Errorf("%s is still retained", what)
}

func payloadTracker(t *testing.T) *Tracker[map[string]any] {
	tr, err := Track(map[string]any{"payload": nil})
	must(t, err)
	return tr
}

func bigPayload() map[string]any {
	rows := make([]any, 50_000)
	for i := range rows {
		rows[i] = map[string]any{"value": i}
	}
	return map[string]any{"rows": rows}
}

//go:noinline
func abortedPayload(t *testing.T, tr *Tracker[map[string]any]) (weak.Pointer[Draft], weak.Pointer[any]) {
	c := mustBegin(t, tr)
	must(t, c.State().Set("payload", bigPayload()))
	rows := c.State().At("payload").At("rows")
	backing := rows.s.base.([]any)
	c.Abort()
	return weak.Make(rows), weak.Make(&backing[0])
}

//go:noinline
func unadoptedPrepared(t *testing.T, tr *Tracker[map[string]any]) weak.Pointer[any] {
	c := mustBegin(t, tr)
	must(t, c.State().Set("payload", bigPayload()))
	p := mustPrepare(t, c)
	rows := obj(p.Value()["payload"])["rows"].([]any)
	return weak.Make(&rows[0])
}

//go:noinline
func draftHandles(t *testing.T, tr *Tracker[map[string]any]) weak.Pointer[Draft] {
	c := mustBegin(t, tr)
	must(t, c.State().Set("payload", map[string]any{"rows": []any{map[string]any{"value": 1}}}))
	row := c.State().At("payload").At("rows").At(0)
	c.Abort()
	return weak.Make(row)
}

var retainedHandles []*Draft

// A draft handle retained past its change retains nothing of the change: the
// Go form of upstream's RELEASED_BASE, which every released state points at.
//
//go:noinline
func retainedHandle(t *testing.T, tr *Tracker[map[string]any]) weak.Pointer[any] {
	c := mustBegin(t, tr)
	must(t, c.State().Set("payload", bigPayload()))
	payload := c.State().At("payload")
	rows := payload.At("rows")
	backing := rows.s.base.([]any)
	c.Abort()
	retainedHandles = append(retainedHandles, payload, rows)
	return weak.Make(&backing[0])
}

var retainedSettled []any

//go:noinline
func settledLifecycle(t *testing.T) (weak.Pointer[Tracker[map[string]any]], weak.Pointer[any]) {
	tr := payloadTracker(t)
	c := mustBegin(t, tr)
	must(t, c.State().Set("payload", map[string]any{"rows": []any{map[string]any{"value": 1}}}))
	retained := mustPrepare(t, c)
	must(t, tr.Adopt(retained))
	retainedSettled = append(retainedSettled, c, retained)

	future, err := tr.PrepareReplace(map[string]any{"payload": bigPayload()})
	must(t, err)
	must(t, tr.Adopt(future))
	rows := obj(future.Value()["payload"])["rows"].([]any)
	return weak.Make(tr), weak.Make(&rows[0])
}

func TestTrackerRetention(t *testing.T) {
	t.Run("aborted-payload", func(t *testing.T) {
		tr := payloadTracker(t)
		handle, backing := abortedPayload(t, tr)
		collected(t, handle, "an aborted change's draft")
		collected(t, backing, "an aborted change's payload")
		wantJSON(t, tr.Value(), tree(t, `{"payload": null}`))
	})
	t.Run("unadopted-prepared", func(t *testing.T) {
		tr := payloadTracker(t)
		collected(t, unadoptedPrepared(t, tr), "an unadopted prepared payload")
		wantJSON(t, tr.Value(), tree(t, `{"payload": null}`))
	})
	t.Run("retained-handle", func(t *testing.T) {
		tr := payloadTracker(t)
		collected(t, retainedHandle(t, tr), "the payload behind a retained, revoked draft")
	})
	t.Run("draft-handles", func(t *testing.T) {
		tr := payloadTracker(t)
		collected(t, draftHandles(t, tr), "an aborted change's member draft")
		wantJSON(t, tr.Value(), tree(t, `{"payload": null}`))
	})
	t.Run("settled-lifecycle", func(t *testing.T) {
		tracker, future := settledLifecycle(t)
		collected(t, tracker, "a tracker whose settled change and prepared change are retained")
		collected(t, future, "a dropped tracker's committed payload")
	})
}
