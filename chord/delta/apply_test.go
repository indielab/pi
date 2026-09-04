package delta

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// ops parses a JSON array of decoded tuples through ParseOp — the batch a
// consumer holds after decode — so a test reads the way its upstream
// counterpart does.
func ops(t *testing.T, literal string) []Op {
	t.Helper()
	raw, ok := tree(t, literal).([]any)
	if !ok {
		t.Fatalf("bad ops literal %s: not an array", literal)
	}
	out := make([]Op, 0, len(raw))
	for _, v := range raw {
		op, err := ParseOp(v)
		if err != nil {
			t.Fatalf("bad ops literal %s: %v", literal, err)
		}
		out = append(out, op)
	}
	return out
}

// applied runs Apply on a JSON literal target and fails on error.
func applied(t *testing.T, target string, batch []Op) any {
	t.Helper()
	got, err := Apply(tree(t, target), batch)
	if err != nil {
		t.Fatalf("Apply(%s): %v", target, err)
	}
	return got
}

func wantTree(t *testing.T, got any, literal string) {
	t.Helper()
	if want := tree(t, literal); !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

// pad is upstream's PAD: see "adaptive emission" — a delta must be able to win.
var pad = strings.Repeat("p", 400)

// delta.test.ts "tracker: intent" → the apply halves of "records an append,
// not a replacement" and "recovers truncate+append from a rolling window".
// The ops are the ones upstream asserts its tracker flushes.
func TestApplyAppendAndRollingWindow(t *testing.T) {
	got := applied(t, `{"s": "", "pad": "`+pad+`"}`, ops(t, `[["a", ["s"], "abcd"]]`))
	wantTree(t, got, `{"s": "abcd", "pad": "`+pad+`"}`)

	got = applied(t, `{"s": "abcdefgh", "pad": "`+pad+`"}`, ops(t, `[["t", ["s"], 3], ["a", ["s"], "xyz"]]`))
	wantTree(t, got, `{"s": "defghxyz", "pad": "`+pad+`"}`)
}

// delta.test.ts "tracker: root ops" → the apply halves of "splices a value
// that is itself an array" and "normalises length = 0 on the root". A root
// splice re-headers the slice; Apply returns the value for the same reason it
// does for "r".
func TestApplyRootOps(t *testing.T) {
	got := applied(t, `[1, 2, 3, "`+pad+`"]`, ops(t, `[["p", [], 4, 0, [4]]]`))
	wantTree(t, got, `[1, 2, 3, "`+pad+`", 4]`)

	got = applied(t, `[1, 2, 3]`, ops(t, `[["r", []]]`))
	wantTree(t, got, `[]`)
}

// delta.test.ts "immutable operation application" → "does not mutate a
// replacement payload targeted by a later operation".
func TestApplyImmutableDoesNotMutateReplacementPayload(t *testing.T) {
	replacement := map[string]any{"nested": map[string]any{"value": float64(1)}}
	next, err := ApplyImmutable[map[string]any](nil, []Op{
		Replace{Value: replacement},
		Set{Path: Path{Key("nested"), Key("value")}, Value: float64(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := replacement["nested"].(map[string]any)["value"]; got != float64(1) {
		t.Errorf("replacement.nested.value = %v, want 1 (payload mutated)", got)
	}
	if got := next["nested"].(map[string]any)["value"]; got != float64(2) {
		t.Errorf("next.nested.value = %v, want 2", got)
	}
}

// README: applyImmutable copies only containers along changed paths and
// shares unchanged subtrees; it does not mutate either complete input.
func TestApplyImmutableSharesUnchangedSubtrees(t *testing.T) {
	sibling := map[string]any{"keep": true}
	xs := []any{float64(1), float64(2)}
	before := map[string]any{"a": map[string]any{"xs": xs}, "sibling": sibling}

	after, err := ApplyImmutable(before, ops(t, `[["p", ["a", "xs"], 2, 0, [3]], ["s", ["a", "flag"], true]]`))
	if err != nil {
		t.Fatal(err)
	}
	wantTree(t, after, `{"a": {"xs": [1, 2, 3], "flag": true}, "sibling": {"keep": true}}`)
	wantTree(t, before, `{"a": {"xs": [1, 2]}, "sibling": {"keep": true}}`)
	if len(xs) != 2 {
		t.Errorf("previous slice re-headered: %v", xs)
	}
	if reflect.ValueOf(after["sibling"]).Pointer() != reflect.ValueOf(sibling).Pointer() {
		t.Error("unchanged sibling was copied, not shared")
	}
	if reflect.ValueOf(after["a"]).Pointer() == reflect.ValueOf(before["a"]).Pointer() {
		t.Error("container on the changed path was shared, not copied")
	}
}

// A set into an existing element and a delete both write INTO the leaf
// container rather than re-headering it, so only a real copy keeps the previous
// value intact. Pinned against pi: applyImmutable({xs:[1,2]}, [["s",["xs",0],9]])
// leaves before as {xs:[1,2]} and returns {xs:[9,2]}.
func TestApplyImmutableLeavesPreviousContainersUntouched(t *testing.T) {
	xs := []any{float64(1), float64(2)}
	before := map[string]any{"xs": xs}
	after, err := ApplyImmutable(before, ops(t, `[["s", ["xs", 0], 9]]`))
	if err != nil {
		t.Fatal(err)
	}
	wantTree(t, after, `{"xs": [9, 2]}`)
	wantTree(t, before, `{"xs": [1, 2]}`)
	if xs[0] != float64(1) {
		t.Errorf("previous slice written through: %v", xs)
	}

	xs = []any{float64(1), float64(2), float64(3)}
	before = map[string]any{"xs": xs}
	after, err = ApplyImmutable(before, ops(t, `[["d", ["xs", 1]]]`))
	if err != nil {
		t.Fatal(err)
	}
	wantTree(t, after, `{"xs": [1, 3]}`)
	wantTree(t, before, `{"xs": [1, 2, 3]}`)
	if xs[1] != float64(2) {
		t.Errorf("previous slice written through by delete: %v", xs)
	}

	// The same for an object leaf under an array.
	inner := map[string]any{"a": float64(1)}
	before = map[string]any{"xs": []any{inner}}
	after, err = ApplyImmutable(before, ops(t, `[["s", ["xs", 0, "a"], 2]]`))
	if err != nil {
		t.Fatal(err)
	}
	wantTree(t, after, `{"xs": [{"a": 2}]}`)
	wantTree(t, before, `{"xs": [{"a": 1}]}`)
}

// delta.test.ts "apply and fan-out" → "adopts an `r` payload rather than
// copying it". The consumer owns the batch it was handed; copying every base
// batch defensively doubles the memory of the one op carrying the whole value.
// The rule that follows: do not hand one batch to two in-process consumers.
// This test pins the aliasing so nobody "fixes" it by adding a clone back.
func TestApplyAdoptsReplacePayload(t *testing.T) {
	batch := []Op{Replace{Value: map[string]any{"n": float64(0)}}}
	a, err := Apply[map[string]any](nil, batch)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Apply[map[string]any](nil, batch)
	if err != nil {
		t.Fatal(err)
	}
	if a == nil || b == nil {
		t.Fatalf("r payload not adopted: a = %v, b = %v", a, b)
	}
	a["n"] = float64(1)
	if b["n"] != float64(1) {
		t.Errorf("b.n = %v, want 1 (r payload was copied)", b["n"])
	}
}

// delta.test.ts "apply and fan-out" → "folds a whole stream without a
// base-batch branch". Apply handles "r" by replacing and tolerates a zero
// target, so a consumer needs no IsBase check and no clone of its own. The
// batches are the ones upstream's tracker flushes for the same writes.
func TestApplyFoldsStreamWithoutBaseBranch(t *testing.T) {
	var replica any
	var err error
	send := func(literal string) {
		t.Helper()
		if replica, err = Apply(replica, ops(t, literal)); err != nil {
			t.Fatal(err)
		}
	}
	send(`[["r", {"x": 100, "l": ["xyz"]}]]`)
	send(`[["s", ["x"], 101]]`)
	wantTree(t, replica, `{"x": 101, "l": ["xyz"]}`)

	// The same with a typed replica: the zero map is upstream's undefined.
	var typed map[string]any
	typed, err = Apply(typed, ops(t, `[["r", {"x": 100, "l": ["xyz"]}]]`))
	if err != nil {
		t.Fatal(err)
	}
	typed, err = Apply(typed, ops(t, `[["s", ["x"], 101], ["p", ["l"], 1, 0, ["abc"]]]`))
	if err != nil {
		t.Fatal(err)
	}
	wantTree(t, typed, `{"x": 101, "l": ["xyz", "abc"]}`)
}

// Go only: a typed replica gets its own type back, and a stream whose base
// carries a different shape is an error rather than a panic.
func TestApplyTypedReplica(t *testing.T) {
	_, err := Apply(map[string]any{}, []Op{Replace{Value: []any{float64(1)}}})
	if err == nil {
		t.Fatal("array base into a map replica: want error, got nil")
	}
	if !strings.Contains(err.Error(), "[]interface {}") || !strings.Contains(err.Error(), "map[string]interface {}") {
		t.Errorf("error should name both types, got %q", err)
	}

	// A write into a zero replica is upstream's apply(undefined, [s]): an
	// unresolvable root, not a nil-map panic.
	var pe *PathError
	if _, err := Apply[map[string]any](nil, ops(t, `[["s", ["a"], 1]]`)); !errors.As(err, &pe) {
		t.Errorf("set into zero replica: got %v, want *PathError", err)
	} else if pe.Ref.(Path).String() != "[]" {
		t.Errorf("PathError ref = %s, want [] (the root)", pe.Ref.(Path))
	}
	if _, err := Apply[any](nil, ops(t, `[["d", ["a"]]]`)); !errors.As(err, &pe) {
		t.Errorf("delete in nil replica: got %v, want *PathError", err)
	}
}

// delta.test.ts "flush" → "rejects a constructor walk". ({}).constructor
// .constructor is Function — the classic escape ladder. ParseOp refuses the
// tuple (op_test.go); a typed op that carries the path anyway is refused by
// Apply itself, before anything is walked, and the target is untouched.
func TestApplyRejectsConstructorWalk(t *testing.T) {
	target := map[string]any{}
	_, err := Apply(target, []Op{Set{Path: Path{Key("constructor"), Key("prototype"), Key("gadget")}, Value: true}})
	var unsafe *UnsafePathError
	if !errors.As(err, &unsafe) {
		t.Fatalf("got %v, want *UnsafePathError", err)
	}
	if len(target) != 0 {
		t.Errorf("target written despite the refusal: %v", target)
	}
	_, err = ApplyImmutable(target, []Op{Delete{Path: Path{Key("__proto__")}}})
	if !errors.As(err, &unsafe) {
		t.Fatalf("immutable: got %v, want *UnsafePathError", err)
	}
}

// delta.test.ts "flush" → "allows a reserved name as a VALUE key". Reserved
// as segments, not as values: a value is written whole and never walked.
func TestApplyAllowsReservedNameAsValueKey(t *testing.T) {
	got := applied(t, `{}`, ops(t, `[["s", ["a"], {"__proto__": {"z": 1}}]]`))
	wantTree(t, got, `{"a": {"__proto__": {"z": 1}}}`)

	// "clones and reads reserved value keys": the key is an own key of the
	// replica's value.
	got = applied(t, `null`, ops(t, `[["r", {"value": {"__proto__": {"z": 1}}}]]`))
	root, _ := got.(map[string]any)
	value, _ := root["value"].(map[string]any)
	if _, ok := value["__proto__"]; !ok {
		t.Errorf("__proto__ is not an own key of the value: %#v", got)
	}
}

// delta.test.ts "safety: array indices". An index may address an existing
// element or append exactly one past the end. Not an arbitrary cap: a sparse
// array does not survive a JSON round trip, so arr[7] = x on a length-3 array
// already produces unreplicable state.
func TestApplyArrayIndexSafety(t *testing.T) {
	var unsafe *UnsafePathError
	var pe *PathError

	// "writes an existing index"
	wantTree(t, applied(t, `{"xs": [1, 2, 3]}`, ops(t, `[["s", ["xs", 1], 9]]`)), `{"xs": [1, 9, 3]}`)
	// "appends one past the end"
	wantTree(t, applied(t, `{"xs": [1, 2, 3]}`, ops(t, `[["s", ["xs", 3], 9]]`)), `{"xs": [1, 2, 3, 9]}`)
	// "rejects a gap"
	if _, err := Apply(tree(t, `{"xs": [1, 2, 3]}`), ops(t, `[["s", ["xs", 5], 9]]`)); !errors.As(err, &unsafe) {
		t.Errorf("gap: got %v, want *UnsafePathError", err)
	} else if unsafe.Segment != Index(5) {
		t.Errorf("gap: Segment = %#v, want Index(5)", unsafe.Segment)
	}
	// ...including a gap of exactly one, the tight edge of the append window.
	if _, err := Apply(tree(t, `{"xs": [1, 2, 3]}`), ops(t, `[["s", ["xs", 4], 9]]`)); !errors.As(err, &unsafe) {
		t.Errorf("gap of one: got %v, want *UnsafePathError", err)
	}
	// "rejects a huge index": would otherwise allocate 4.29 billion entries from one op.
	if _, err := Apply(tree(t, `{"xs": []}`), ops(t, `[["s", ["xs", 4294967290], 1]]`)); !errors.As(err, &unsafe) {
		t.Errorf("huge index: got %v, want *UnsafePathError", err)
	}
	// "rejects string-spelled array indices at the consumer"
	if _, err := Apply(tree(t, `{"xs": [1, 2, 3]}`), ops(t, `[["s", ["xs", "7"], 9]]`)); !errors.As(err, &unsafe) {
		t.Errorf(`s ["xs","7"]: got %v, want *UnsafePathError`, err)
	} else if unsafe.Segment != Key("7") {
		t.Errorf(`s ["xs","7"]: Segment = %#v, want Key("7")`, unsafe.Segment)
	}
	if _, err := Apply(tree(t, `{"xs": ["a"]}`), ops(t, `[["a", ["xs", "0"], "b"]]`)); !errors.As(err, &unsafe) {
		t.Errorf(`a ["xs","0"]: got %v, want *UnsafePathError`, err)
	}
	// A string-spelled index on the way down, not just at the leaf.
	if _, err := Apply(tree(t, `{"xs": [{"a": 1}]}`), ops(t, `[["s", ["xs", "0", "a"], 2]]`)); !errors.As(err, &unsafe) {
		t.Errorf(`walk through ["xs","0"]: got %v, want *UnsafePathError`, err)
	}
	// Descending THROUGH an index equal to the length is unresolvable, not a
	// panic: pi's apply({xs:[1]}, [["s",["xs",1,"a"],2]]) → PathError ["xs",1].
	if _, err := Apply(tree(t, `{"xs": [1]}`), ops(t, `[["s", ["xs", 1, "a"], 2]]`)); !errors.As(err, &pe) {
		t.Errorf(`walk through ["xs",1] at the end: got %v, want *PathError`, err)
	} else if got := refString(pe.Ref); got != `["xs",1]` {
		t.Errorf(`walk through ["xs",1] at the end: ref = %s, want ["xs",1]`, got)
	}
	// "allows explicit growth with nulls"
	wantTree(t, applied(t, `{"xs": [1]}`, ops(t, `[["p", ["xs"], 1, 0, [null, null, 9]]]`)), `{"xs": [1, null, null, 9]}`)
	// "rejects deleting one past an array's end"
	if _, err := Apply(tree(t, `{"xs": [1]}`), ops(t, `[["d", ["xs", 1]]]`)); !errors.As(err, &pe) {
		t.Errorf("delete one past end: got %v, want *PathError", err)
	}
	// ...and a delete further out is the index rule, not a resolution failure.
	if _, err := Apply(tree(t, `{"xs": [1]}`), ops(t, `[["d", ["xs", 5]]]`)); !errors.As(err, &unsafe) {
		t.Errorf("delete past a gap: got %v, want *UnsafePathError", err)
	}
	// "applies large splice payloads without spreading them at once"
	items := make([]any, 300_000)
	got, err := Apply(map[string]any{"xs": []any{}}, []Op{Splice{Path: Path{Key("xs")}, Items: items}})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got["xs"].([]any)); n != len(items) {
		t.Errorf("large splice: len = %d, want %d", n, len(items))
	}
}

// delta.test.ts "safety: op structure". Shape is the type's job in Go — an
// unknown verb, a string path or non-array items cannot be spelled as an Op
// and are ParseOp's to refuse (op_test.go) — so what is left for the applier
// is the nil element, the constraints Validate carries, and the value checks.
func TestApplyOpStructureSafety(t *testing.T) {
	var pe *PathError

	// "rejects a non-tuple op": the one shape an []Op can still hold.
	if _, err := Apply(tree(t, `{"a": 1}`), []Op{nil}); !errors.Is(err, ErrInvalidOp) {
		t.Errorf("nil op: got %v, want ErrInvalidOp", err)
	}
	// "rejects append to a missing or non-string value"
	if _, err := Apply(tree(t, `{"a": 1}`), ops(t, `[["a", ["missing"], "x"]]`)); !errors.As(err, &pe) {
		t.Errorf("append to missing: got %v, want *PathError", err)
	}
	if _, err := Apply(tree(t, `{"a": 1}`), ops(t, `[["a", ["a"], "x"]]`)); !errors.As(err, &pe) {
		t.Errorf("append to number: got %v, want *PathError", err)
	}
	// Appending one past an array's end reads undefined, which is not a string.
	if _, err := Apply(tree(t, `{"xs": ["a"]}`), ops(t, `[["a", ["xs", 1], "x"]]`)); !errors.As(err, &pe) {
		t.Errorf("append one past end: got %v, want *PathError", err)
	}
	// "rejects negative truncation": unspellable through ParseOp, so typed.
	if _, err := Apply(tree(t, `{"a": "abc"}`), []Op{Truncate{Path: Path{Key("a")}, Count: -1}}); !errors.Is(err, ErrInvalidOp) {
		t.Errorf("negative truncation: got %v, want ErrInvalidOp", err)
	}
	if _, err := Apply(tree(t, `{"a": "abc"}`), []Op{Set{Value: 1}}); !errors.Is(err, ErrInvalidOp) {
		t.Errorf("root set: got %v, want ErrInvalidOp", err)
	}
	// "clamps a splice remove past the end, as Array.prototype.splice does":
	// deterministic and identical on both sides — not a hole.
	wantTree(t, applied(t, `{"xs": [1, 2]}`, ops(t, `[["p", ["xs"], 0, 1000000000, []]]`)), `{"xs": []}`)
	// splice clamps the index the same way.
	wantTree(t, applied(t, `{"xs": [1, 2]}`, ops(t, `[["p", ["xs"], 5, 0, [9]]]`)), `{"xs": [1, 2, 9]}`)
}

// The path a PathError carries is the one upstream's resolve was handed: the
// parent path for s/d/a/t resolution failures, the op's full path when the
// value at it is the wrong kind. Pinned against pi's messages.
func TestApplyPathErrorsCarryTheResolvedPath(t *testing.T) {
	cases := []struct {
		name, target, batch, wantRef string
	}{
		{"set under missing parent", `{"a": 1}`, `[["s", ["b", "c"], 1]]`, `["b"]`},
		{"set under scalar parent", `{"a": 1}`, `[["s", ["a", "c"], 1]]`, `["a"]`},
		{"splice a non-array", `{"a": 1}`, `[["p", ["a"], 0, 0, []]]`, `["a"]`},
		{"append to a non-string", `{"a": 1}`, `[["a", ["a"], "x"]]`, `["a"]`},
		{"truncate a non-string", `{"a": 1}`, `[["t", ["a"], 1]]`, `["a"]`},
		{"append one past an array's end", `{"xs": ["a"]}`, `[["a", ["xs", 1], "x"]]`, `["xs",1]`},
		{"delete one past an array's end", `{"xs": [1]}`, `[["d", ["xs", 1]]]`, `["xs",1]`},
		{"splice the root when it is not an array", `"str"`, `[["p", [], 0, 0, []]]`, `[]`},
		{"set into a scalar root", `5`, `[["s", ["a"], 1]]`, `[]`},
	}
	for _, tc := range cases {
		for _, fn := range []struct {
			name string
			f    func(any, []Op) (any, error)
		}{{"Apply", Apply[any]}, {"ApplyImmutable", ApplyImmutable[any]}} {
			_, err := fn.f(tree(t, tc.target), ops(t, tc.batch))
			var pe *PathError
			if !errors.As(err, &pe) {
				t.Errorf("%s/%s: got %v, want *PathError", fn.name, tc.name, err)
				continue
			}
			if got := refString(pe.Ref); got != tc.wantRef {
				t.Errorf("%s/%s: PathError ref = %s, want %s", fn.name, tc.name, got, tc.wantRef)
			}
		}
	}
}

// Object keys are strings on both sides: an Index segment under an object is
// the property spelled as a number, the way JavaScript coerces it. Pinned
// against pi.
func TestApplyNumericSegmentOnObject(t *testing.T) {
	wantTree(t, applied(t, `{"o": {}}`, ops(t, `[["s", ["o", 3], 1]]`)), `{"o": {"3": 1}}`)
	wantTree(t, applied(t, `{"o": {"3": 1, "x": 2}}`, ops(t, `[["d", ["o", 3]]]`)), `{"o": {"x": 2}}`)
	// Deleting a missing key is a no-op, as `delete` is.
	wantTree(t, applied(t, `{"o": {}}`, ops(t, `[["d", ["o", "nope"]]]`)), `{"o": {}}`)
}

// "t" counts UTF-16 code units, the wire's unit, not bytes or runes; a Go
// replica that counted bytes would drift from a pi replica on the first
// non-ASCII window. Values pinned against pi except the split surrogate: pi
// holds the lone low surrogate "\ude00ab", which is not a Go string; U+FFFD is
// what encoding/json makes of that value once it crosses the wire, so a Go
// replica holds the same thing a Go decoder would read from a pi replica.
func TestApplyTruncateCountsUTF16CodeUnits(t *testing.T) {
	cases := []struct {
		in    string
		count int
		want  string
	}{
		{"héllo", 2, "llo"},
		{"😀ab", 2, "ab"},
		{"😀ab", 1, "�ab"},
		{"abc", 100, ""},
		{"abc", 0, "abc"},
		{"日本語", 1, "本語"},
		{"a😀b😀", 3, "b😀"},
		// Cuts that land after multi-byte runes, where a byte count would
		// stop short.
		{"日本語", 2, "語"},
		{"ééx", 2, "x"},
		{"😀😀x", 3, "�x"},
		{"😀😀x", 4, "x"},
		{"é😀é", 2, "�é"},
		{"é😀é", 3, "é"},
	}
	for _, tc := range cases {
		got, err := Apply(map[string]any{"a": tc.in}, []Op{Truncate{Path: Path{Key("a")}, Count: tc.count}})
		if err != nil {
			t.Fatalf("t %q %d: %v", tc.in, tc.count, err)
		}
		if got["a"] != tc.want {
			t.Errorf("t %q %d = %q, want %q", tc.in, tc.count, got["a"], tc.want)
		}
	}
}

// Apply is not transactional: ops before the failing one have already changed
// the replica (README, "Tracker lifecycle"). The error terminates the stream.
func TestApplyIsNotTransactional(t *testing.T) {
	target := map[string]any{"a": float64(1)}
	_, err := Apply(target, ops(t, `[["s", ["a"], 2], ["s", ["missing", "x"], 1]]`))
	if err == nil {
		t.Fatal("want error")
	}
	if target["a"] != float64(2) {
		t.Errorf("first op rolled back: a = %v, want 2", target["a"])
	}
}
