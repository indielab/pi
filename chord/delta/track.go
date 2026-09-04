package delta

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// ─── Tracker ─────────────────────────────────────────────────────────────────
//
// Upstream tracks dirtiness ON MUTATION rather than diffing a snapshot at
// flush, for a measured reason: this state flushes per streamed token, and a
// 200 KB string growing 8 bytes per flush went from 845 µs to 42 µs. Its
// producer mutates a Proxy that records a dirty path per write; flush walks
// only the dirty paths, comparing the published baseline with the current
// value there.
//
// Go has no Proxy, and no way to intercept an assignment to a map or slice
// element. The mirror is a cursor: a State addresses one path in the tracked
// tree and offers the same mutations the Proxy traps — set, delete, and the
// array methods — each of which records the same dirty mark upstream's trap
// records. The tracked tree is the any-tree encoding/json produces (the form
// Apply works on), so no per-type code generation is needed: a producer's
// state is JSON on the wire, and JSON in memory keeps one diff and one walk
// for every state shape. The dirty tree, the flush walk and the baseline sync
// are upstream's, and the ops a flush emits are the ops pi's tracker emits for
// the same logical mutation.

// defaultMaxOverlapScan is how far back a string diff looks for a rolling
// window, in UTF-16 code units, when no option says otherwise.
const defaultMaxOverlapScan = 65_536

// Option configures a Tracker.
type Option struct{ apply func(*tracker) }

// WithMaxOverlapScan bounds how far back a string diff looks for a rolling
// window — the longest suffix of the previous value that is a prefix of the
// new one — in UTF-16 code units. Zero disables the search, so a moved window
// is published as a whole-value set. The default is 65536.
func WithMaxOverlapScan(units int) Option {
	return Option{func(t *tracker) { t.scan = units }}
}

// Tracker publishes changes to one JSON value as batches of ops. T is the
// root's Go type: map[string]any, []any, or any when it is not known.
//
// The first Flush is a base batch — one Replace carrying the complete value.
// Each later Flush is the ops that transform the previously published value
// into the current one, or an empty batch when nothing changed. Mutate the
// value through State; the object handed to Track, and every value later
// inserted, becomes tracker-owned.
type Tracker[T any] struct{ core *tracker }

type tracker struct {
	scan        int
	root        any
	pending     *dirtyNode
	hasPending  bool
	baseline    any
	hasBaseline bool
	forceBase   bool
	stamp       uint64 // orders sibling dirty nodes; a re-touched path moves last
}

// Track starts tracking root, which must be a JSON object (map[string]any)
// or array ([]any) — anything else panics, as a Proxy over a non-object
// would. The value is adopted, not copied: mutate it only through State.
func Track[T any](root T, opts ...Option) *Tracker[T] {
	core := &tracker{scan: defaultMaxOverlapScan, pending: newDirtyNode(), forceBase: true}
	core.adopt(root)
	for _, opt := range opts {
		opt.apply(core)
	}
	return &Tracker[T]{core: core}
}

// adopt takes v as the tracked root, refusing anything but a container.
func (t *tracker) adopt(v any) {
	switch c := v.(type) {
	case map[string]any:
		if c == nil {
			panic(errors.New("delta: tracked state is a nil map; track an empty map[string]any{} instead"))
		}
	case []any:
	default:
		panic(fmt.Errorf("delta: tracked state must be a JSON object (map[string]any) or array ([]any), got %s (decode the value with encoding/json, or build it from those two types)", describe(v)))
	}
	t.root = v
}

// State is a cursor at the root of the tracked value. Read and mutate the
// value through it.
func (t *Tracker[T]) State() State { return State{t: t.core} }

// Target is the untracked current value. Mutating it bypasses change
// tracking. A root array is re-headered by an append, so read Target again
// after mutating rather than keeping an earlier slice.
func (t *Tracker[T]) Target() T { return t.core.root.(T) }

// SetState replaces the whole tracked value. Ops recorded before it are
// discarded — they describe a value that no longer exists — and the next
// Flush is a base batch. Upstream spells this `tracker.state = next`.
func (t *Tracker[T]) SetState(next T) {
	t.core.adopt(next)
	t.core.clearPending()
	t.core.baseline, t.core.hasBaseline = nil, false
	t.core.forceBase = true
}

// Rebase makes the next Flush a complete base batch without changing the
// value. This is the checkpoint: recovery replays from the last base batch,
// so a producer must be able to bound that.
func (t *Tracker[T]) Rebase() {
	t.core.clearPending()
	t.core.forceBase = true
}

// Discard accepts pending mutations into the local baseline without
// publishing them. Replicas that exist never see them; use it only when they
// need not.
func (t *Tracker[T]) Discard() {
	t.core.baseline, t.core.hasBaseline = cloneJSON(t.core.root), true
	t.core.clearPending()
}

// Dirty reports whether a Flush would emit anything: a base batch is owed, or
// a path has been touched since the last one. A touched path whose value is
// back where it was still counts; Flush is what compares values.
func (t *Tracker[T]) Dirty() bool { return t.core.forceBase || t.core.hasPending }

// Flush publishes the changes since the previous flush as decoded ops with
// complete paths. The first flush, and the one after Rebase or SetState, is
// [Replace]. The batch is never nil: an unchanged value flushes [].
//
// The ops carry snapshots of the values they set, so a consumer's replica
// never aliases the producer.
func (t *Tracker[T]) Flush() []Op { return t.core.flush() }

func (t *tracker) flush() []Op {
	if t.forceBase {
		value := cloneJSON(t.root)
		t.baseline, t.hasBaseline = cloneJSON(t.root), true
		t.forceBase = false
		t.clearPending()
		return []Op{Replace{Value: value}}
	}
	if !t.hasPending || !t.hasBaseline {
		return []Op{}
	}
	d := differ{scan: t.scan, out: []Op{}}
	d.walkDirty(t.baseline, t.root, t.pending, nil)
	// Advance the baseline by SHARING references from root where that is
	// cheap and exact — scalars, strings, and array appends. Replaying the
	// ops rebuilds every touched string via slice + concat: two window-sized
	// allocations per flush. For anything the sync cannot express cheaply (a
	// non-append array change), it declines and the replay runs instead.
	if !t.syncBaseline() && len(d.out) > 0 {
		next, err := applyOps(t.baseline, cloneOps(d.out), false)
		if err != nil {
			// The batch was derived from this very baseline; a failure is a
			// tracker bug, not a producer mistake.
			panic(fmt.Errorf("delta: replaying a flushed batch onto the baseline failed: %w", err))
		}
		t.baseline = next
	}
	t.clearPending()
	return d.out
}

func (t *tracker) clearPending() {
	t.pending = newDirtyNode()
	t.hasPending = false
}

// ─── Dirty tree ──────────────────────────────────────────────────────────────

// arrayDirty is what an array node remembers of its structural changes.
type arrayDirty uint8

const (
	arrayClean   arrayDirty = iota
	arrayAppend             // the tail grew from start; older elements carry their own marks
	arrayDiff               // indices moved: compare positionally at flush
	arrayReplace            // replaced whole: one set, or nothing when deeply equal
)

// dirtyNode is one touched path. valueDirty means the value here was
// assigned or deleted and must be diffed whole; array is an array node's
// structural mark; children are the touched paths below it.
type dirtyNode struct {
	valueDirty bool
	array      arrayDirty
	start      int // arrayAppend: the length before the first append
	children   map[Seg]*dirtyNode
	touched    uint64 // upstream's Map order: a re-touched child moves last
}

func newDirtyNode() *dirtyNode { return &dirtyNode{children: map[Seg]*dirtyNode{}} }

// ordered is the children in touch order, which is the order their ops are
// emitted: upstream deletes and re-inserts a Map entry on every touch.
func (n *dirtyNode) ordered() []dirtyChild {
	out := make([]dirtyChild, 0, len(n.children))
	for seg, child := range n.children {
		out = append(out, dirtyChild{seg, child})
	}
	slices.SortFunc(out, func(a, b dirtyChild) int {
		return int(a.node.touched - b.node.touched) // stamps are distinct and increasing
	})
	return out
}

type dirtyChild struct {
	seg  Seg
	node *dirtyNode
}

// ensureNode finds or creates the node for path, moving every node along the
// way last among its siblings. It returns nil when an ancestor is already
// dirty whole — a value mark, or an array diff or replace — because the
// ancestor's diff covers this path.
func (t *tracker) ensureNode(path Path) *dirtyNode {
	t.hasPending = true
	node := t.pending
	for _, seg := range path {
		if node.valueDirty || node.array == arrayDiff || node.array == arrayReplace {
			return nil
		}
		child := node.children[seg]
		if child == nil {
			child = newDirtyNode()
			node.children[seg] = child
		}
		t.stamp++
		child.touched = t.stamp
		node = child
	}
	return node
}

func (t *tracker) findNode(path Path) *dirtyNode {
	node := t.pending
	for _, seg := range path {
		if node = node.children[seg]; node == nil {
			return nil
		}
	}
	return node
}

// markValue: the value at path was assigned or deleted. Whatever was known
// below it no longer matters.
func (t *tracker) markValue(path Path) {
	node := t.ensureNode(path)
	if node == nil {
		return
	}
	node.valueDirty = true
	node.array = arrayClean
	clear(node.children)
}

// markArrayAppend: the array at path grew from start. A later append keeps
// the first start; a stronger mark already there wins.
func (t *tracker) markArrayAppend(path Path, start int) {
	node := t.ensureNode(path)
	if node == nil || node.valueDirty || node.array == arrayDiff || node.array == arrayReplace {
		return
	}
	if node.array == arrayClean {
		node.array, node.start = arrayAppend, start
	}
}

// markArrayDiff: indices moved. Element marks below are dropped; flush
// compares positionally.
func (t *tracker) markArrayDiff(path Path) {
	node := t.ensureNode(path)
	if node == nil || node.valueDirty || node.array == arrayReplace {
		return
	}
	node.array = arrayDiff
	clear(node.children)
}

// markArrayReplace: every element was removed. Flush sets the array whole
// unless it is deeply equal to the published one.
func (t *tracker) markArrayReplace(path Path) {
	node := t.ensureNode(path)
	if node == nil || node.valueDirty {
		return
	}
	node.array = arrayReplace
	clear(node.children)
}

// appendStart is the start of the array at path's pending append, if it has
// one.
func (t *tracker) appendStart(path Path) (int, bool) {
	if node := t.findNode(path); node != nil && node.array == arrayAppend {
		return node.start, true
	}
	return 0, false
}

// ─── State ───────────────────────────────────────────────────────────────────

// State is a cursor into a tracked value: the tracker plus a path. It is the
// Go form of upstream's proxy — reads resolve the path on each call, so a
// cursor follows a replaced child rather than pointing at the old one, and
// after an array operation that moves indices a cursor at an index addresses
// whatever now sits there.
//
// Segments are given as any: a string is an object key, an int (or any
// integral Go number) an array index, and a Key or Index is taken as is. On
// an array a numeric string is the index it spells, as JavaScript coerces it.
//
// Values inserted through a State are adopted: a map becomes part of the
// tracked tree and may be read through a retained reference, but must not be
// mutated outside the tracker or inserted at a second live path. A slice is
// adopted by its header — the tracker's appends do not reach the caller's
// variable. A number of any Go kind is one JSON number. A State itself is
// not a value: assigning one to its own slot is a no-op, anywhere else a
// panic.
//
// Misuse panics rather than returning an error, as an out-of-range index
// does: these are the producer's own programming errors, not data. The panic
// value is an error — *UnsafePathError for a reserved key, a non-index
// segment on an array or a write past the one-slot append window;
// *PathError when the cursor's path does not resolve to a container; an
// ErrInvalidOp-wrapped error for a segment that is neither a key nor an
// index; and a plain error for a delete on an array, a negative length, or a
// State inserted as a value.
type State struct {
	t    *tracker
	path Path
	// blocked is the reserved key this cursor was reached through, if any: the
	// subtree can be read and serialised but not mutated through that key.
	blocked Seg
}

// Path is the cursor's path from the root; empty at the root.
func (s State) Path() Path { return slices.Clone(s.path) }

// At is the cursor for a child. It does not resolve the path: a child that
// does not exist reads as absent and refuses to be written to.
func (s State) At(seg any) State {
	container, _ := resolveValue(s.t.root, s.path)
	key := normSeg(mustSeg(seg), container)
	blocked := s.blocked
	if k, ok := key.(Key); ok && blocked == nil && ReservedSegments[string(k)] {
		blocked = k
	}
	return State{t: s.t, path: childPath(s.path, key), blocked: blocked}
}

// Value is the current value at the cursor, or nil when the path does not
// resolve. A container is returned as is — the tracked tree itself, to be
// read and not mutated.
func (s State) Value() any {
	v, _ := resolveValue(s.t.root, s.path)
	return v
}

// Get is the member's value, or nil when absent. Lookup tells the two apart.
func (s State) Get(seg any) any {
	v, _ := s.Lookup(seg)
	return v
}

// Lookup is the member's value and whether it is present.
func (s State) Lookup(seg any) (any, bool) {
	container, ok := resolveValue(s.t.root, s.path)
	if !ok {
		return nil, false
	}
	v := ownValue(container, normSeg(mustSeg(seg), container))
	if v == missing {
		return nil, false
	}
	return v, true
}

// Len is an array's length or an object's member count; 0 for anything else.
func (s State) Len() int {
	switch c := s.Value().(type) {
	case []any:
		return len(c)
	case map[string]any:
		return len(c)
	}
	return 0
}

// Set assigns a member: an object property, or an array element at an
// existing index or exactly one past the end. Assigning a member the value it
// already has (a scalar by value, a map by identity) records nothing. nil is
// JSON null; absence is spelled with Delete.
func (s State) Set(seg any, value any) {
	s.mutate(func(container any) any {
		key := s.segFor(seg, container)
		at := childPath(s.path, key)
		if cursor, ok := value.(State); ok {
			if cursor.t == s.t && slices.Equal(cursor.path, at) {
				return container // the child assigned back to its own slot
			}
			panic(errAliasedCursor)
		}
		switch c := container.(type) {
		case []any:
			i, ok := key.(Index)
			if !ok || int(i) > len(c) {
				// A gap would make a sparse array, which no JSON replica can hold.
				panic(&UnsafePathError{Segment: key})
			}
			if int(i) == len(c) {
				s.t.markArrayAppend(s.path, len(c))
				return append(c, value)
			}
			if same(c[i], value) {
				return c
			}
			if start, ok := s.t.appendStart(s.path); !ok || int(i) < start {
				s.t.markValue(at)
			}
			c[i] = value
			return c
		case map[string]any:
			k := propertyKey(key)
			if previous, ok := c[k]; ok && same(previous, value) {
				return c
			}
			s.t.markValue(at)
			c[k] = value
			return c
		}
		panic(&PathError{Ref: s.path})
	})
}

// Delete removes an object property. Deleting one that is absent still marks
// the path, as `delete` does. An array element cannot be deleted — that would
// leave a hole; use Splice.
func (s State) Delete(seg any) {
	s.mutate(func(container any) any {
		key := s.segFor(seg, container)
		switch c := container.(type) {
		case []any:
			if _, ok := key.(Index); !ok {
				panic(&UnsafePathError{Segment: key})
			}
			panic(errSparseDelete)
		case map[string]any:
			s.t.markValue(childPath(s.path, key))
			delete(c, propertyKey(key))
			return c
		}
		panic(&PathError{Ref: s.path})
	})
}

// Push appends to an array and returns the new length. Pushes before one
// flush publish as one tail splice.
func (s State) Push(items ...any) (length int) {
	adoptItems(items)
	s.array("Push", func(xs []any) []any {
		if len(items) > 0 {
			s.t.markArrayAppend(s.path, len(xs))
		}
		xs = append(xs, items...)
		length = len(xs)
		return xs
	})
	return length
}

// Unshift inserts at the front and returns the new length.
func (s State) Unshift(items ...any) (length int) {
	adoptItems(items)
	s.array("Unshift", func(xs []any) []any {
		if len(items) > 0 {
			s.t.markArrayDiff(s.path)
		}
		xs = slices.Insert(xs, 0, items...)
		length = len(xs)
		return xs
	})
	return length
}

// Pop removes and returns the last element; false when the array is empty.
// Popping an element pushed since the last flush cancels the push.
func (s State) Pop() (value any, ok bool) {
	s.array("Pop", func(xs []any) []any {
		if len(xs) == 0 {
			return xs
		}
		last := len(xs) - 1
		if start, has := s.t.appendStart(s.path); !has || last < start {
			s.t.markArrayDiff(s.path)
		}
		value, ok = xs[last], true
		xs[last] = nil
		return xs[:last]
	})
	return value, ok
}

// Shift removes and returns the first element; false when the array is empty.
func (s State) Shift() (value any, ok bool) {
	s.array("Shift", func(xs []any) []any {
		if len(xs) == 0 {
			return xs
		}
		s.t.markArrayDiff(s.path)
		value, ok = xs[0], true
		return slices.Delete(xs, 0, 1)
	})
	return value, ok
}

// Splice is Array.prototype.splice: remove `remove` elements at index and
// insert items there, returning the removed elements. A negative index counts
// from the end; an index past the end appends; a remove count past the end
// is clamped. A splice covering the whole array publishes as a set (or, on
// the root, a Replace); one at the pending append's tail folds into it.
func (s State) Splice(index, remove int, items ...any) (removed []any) {
	adoptItems(items)
	s.array("Splice", func(xs []any) []any {
		before := len(xs)
		index, remove = spliceRange(before, index, remove)
		if remove > 0 || len(items) > 0 {
			start, has := s.t.appendStart(s.path)
			switch {
			case index == 0 && remove == before:
				s.t.markArrayReplace(s.path)
			case has && index >= start:
				// The final append payload includes all tail edits.
			case index == before && remove == 0:
				s.t.markArrayAppend(s.path, before)
			default:
				s.t.markArrayDiff(s.path)
			}
		}
		removed = slices.Clone(xs[index : index+remove])
		return splice(xs, index, remove, items)
	})
	return removed
}

// spliceRange is Array.prototype.splice's normalisation of its first two
// arguments against the array's length.
func spliceRange(length, index, remove int) (int, int) {
	if index < 0 {
		index = max(0, length+index)
	} else {
		index = min(index, length)
	}
	return index, max(0, min(remove, length-index))
}

// Sort sorts the array in place, stably, by cmp. JavaScript's default order
// — by string form, in UTF-16 code units — is a footgun on numbers, so there
// is no default; pass the order you mean.
func (s State) Sort(cmp func(a, b any) int) {
	s.array("Sort", func(xs []any) []any {
		s.t.markArrayDiff(s.path)
		slices.SortStableFunc(xs, cmp)
		return xs
	})
}

// Reverse reverses the array in place.
func (s State) Reverse() {
	s.array("Reverse", func(xs []any) []any {
		s.t.markArrayDiff(s.path)
		slices.Reverse(xs)
		return xs
	})
}

// Fill is Array.prototype.fill: it sets the elements in [start, end) to
// value, with negative bounds counted from the end and both clamped. A map
// filled into several slots would live at several paths; fill scalars.
func (s State) Fill(value any, start, end int) {
	adoptItems([]any{value})
	s.array("Fill", func(xs []any) []any {
		s.t.markArrayDiff(s.path)
		from, to := relativeIndex(start, len(xs)), relativeIndex(end, len(xs))
		for i := from; i < to; i++ {
			xs[i] = value
		}
		return xs
	})
}

// CopyWithin is Array.prototype.copyWithin: it copies the elements in
// [start, end) to target, within the array, with the same bounds rules as
// Fill. Reference semantics apply, as they do in JavaScript.
func (s State) CopyWithin(target, start, end int) {
	s.array("CopyWithin", func(xs []any) []any {
		s.t.markArrayDiff(s.path)
		to := relativeIndex(target, len(xs))
		from, final := relativeIndex(start, len(xs)), relativeIndex(end, len(xs))
		if count := min(final-from, len(xs)-to); count > 0 {
			copy(xs[to:to+count], xs[from:from+count])
		}
		return xs
	})
}

// relativeIndex is JavaScript's relative-index rule: negative counts from the
// end, and both directions clamp to [0, length].
func relativeIndex(i, length int) int {
	if i < 0 {
		return max(0, length+i)
	}
	return min(i, length)
}

// SetLen is `array.length = n`: it truncates, or grows with explicit nulls.
// A negative length panics, as the RangeError does.
func (s State) SetLen(n int) {
	s.array("SetLen", func(xs []any) []any {
		before := len(xs)
		switch {
		case n < 0:
			panic(fmt.Errorf("delta: invalid array length %d", n))
		case n < before:
			if n == 0 {
				s.t.markArrayReplace(s.path)
			} else if start, has := s.t.appendStart(s.path); !has || n < start {
				s.t.markArrayDiff(s.path)
			}
			clear(xs[n:])
			return xs[:n]
		case n > before:
			s.t.markArrayAppend(s.path, before)
			return append(xs, make([]any, n-before)...)
		}
		return xs
	})
}

var (
	errSparseDelete  = errors.New("delta: delete would create a sparse array; use Splice instead")
	errAliasedCursor = errors.New("delta: a State cursor is not a value; one value cannot live at two paths, so insert a copy of its Value() instead")
)

// mutate resolves the cursor's path, hands fn the container there, and
// stores what fn returns in its place — an append re-headers a slice, so the
// write back is what keeps a root or nested array attached.
func (s State) mutate(fn func(container any) any) {
	if s.blocked != nil {
		panic(&UnsafePathError{Segment: s.blocked})
	}
	root, err := walk(s.t.root, s.path, false, func(node any) (any, error) { return fn(node), nil })
	if err != nil {
		panic(err)
	}
	s.t.root = root
}

// array is mutate for the array methods, which have no meaning elsewhere.
func (s State) array(verb string, fn func(xs []any) []any) {
	s.mutate(func(container any) any {
		xs, ok := container.([]any)
		if !ok {
			panic(fmt.Errorf("delta: %s on %s, which is %s, not an array", verb, s.path, describe(container)))
		}
		return fn(xs)
	})
}

// segFor is a mutation's segment: normalised for the container, and refused
// when reserved — upstream's guard. A reserved key can be read through a
// blocked cursor but never written through.
func (s State) segFor(raw any, container any) Seg {
	key := normSeg(mustSeg(raw), container)
	if k, ok := key.(Key); ok && ReservedSegments[string(k)] {
		panic(&UnsafePathError{Segment: k})
	}
	return key
}

// mustSeg reads a segment given as any: a Seg, a string, or an integral Go
// number, as parsePath accepts them.
func mustSeg(v any) Seg {
	seg, err := parseSeg(v)
	if err != nil {
		panic(err)
	}
	return seg
}

// normSeg is upstream's norm: on an array a canonical numeric string is the
// index it spells. Everywhere else the segment is what it was — an object's
// key stays a string however it spells, and an Index on an object is the
// property it coerces to when written.
func normSeg(seg Seg, container any) Seg {
	if k, ok := seg.(Key); ok {
		if _, isArray := container.([]any); isArray {
			if i, ok := canonicalIndex(string(k)); ok {
				return Index(i)
			}
		}
	}
	return seg
}

// canonicalIndex reports whether s is an array index as JavaScript spells
// one: the decimal form of an integer in [0, 2^32-2], without sign or
// leading zeros.
func canonicalIndex(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 1<<32-2 || strconv.Itoa(n) != s {
		return 0, false
	}
	return n, true
}

// adoptItems refuses a State among the values inserted into the tree.
func adoptItems(items []any) {
	for _, item := range items {
		if _, ok := item.(State); ok {
			panic(errAliasedCursor)
		}
	}
}

// childPath is path plus one segment, in fresh storage: the path an op keeps.
func childPath(path Path, seg Seg) Path {
	return append(slices.Clip(path), seg)
}

// resolveValue reads the value at path — own members only, an index where the
// node is an array — reporting whether the path resolves.
func resolveValue(root any, path Path) (any, bool) {
	node := root
	for _, seg := range path {
		if node = ownValue(node, seg); node == missing {
			return nil, false
		}
	}
	return node, true
}

// same is `previous === value`: scalars by value, a map by identity, a slice
// by identity of its header. Assigning a member what it already holds is not
// a change.
func same(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case map[string]any:
		y, ok := b.(map[string]any)
		return ok && reflect.ValueOf(x).Pointer() == reflect.ValueOf(y).Pointer()
	case []any:
		y, ok := b.([]any)
		return ok && len(x) == len(y) && len(x) > 0 && &x[0] == &y[0]
	}
	if fa, ok := number(a); ok {
		fb, ok := number(b)
		return ok && fa == fb
	}
	return false
}

// ─── Flush: dirty walk and diff ──────────────────────────────────────────────

// missingValue is upstream's MISSING: a member that is not there, distinct
// from one that is null.
type missingValue struct{}

var missing any = missingValue{}

// ownValue is the member seg of value, or missing: a key on an object (an
// Index spells the property it coerces to), an index within an array. A
// key on an array, or any member of a scalar, is missing.
func ownValue(value any, seg Seg) any {
	switch c := value.(type) {
	case map[string]any:
		if v, ok := c[propertyKey(seg)]; ok {
			return v
		}
	case []any:
		if i, ok := seg.(Index); ok && i >= 0 && int(i) < len(c) {
			return c[i]
		}
	}
	return missing
}

func isContainer(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}

// differ accumulates one flush's ops.
type differ struct {
	scan int
	out  []Op
}

// emitSet publishes the value at path whole, as a Replace at the root.
func (d *differ) emitSet(path Path, value any) {
	snapshot := cloneJSON(value)
	if len(path) == 0 {
		d.out = append(d.out, Replace{Value: snapshot})
		return
	}
	d.out = append(d.out, Set{Path: path, Value: snapshot})
}

func (d *differ) emitDelete(path Path) {
	if len(path) == 0 {
		panic(errors.New("delta: the tracked root cannot be deleted"))
	}
	d.out = append(d.out, Delete{Path: path})
}

// diffString publishes a string change as the smallest of an append, a
// truncate-and-append (a rolling window), or a set. Counts are UTF-16 code
// units throughout, because overlap answers in them and a "t" carries them;
// the append text is sliced at the shared units, which never split a rune.
func (d *differ) diffString(before, after string, path Path) {
	if before == after {
		return
	}
	if len(path) == 0 {
		d.emitSet(path, after)
		return
	}
	// A byte prefix is a code-unit prefix: before is whole, so it ends on a
	// rune boundary. Upstream flattens with slice+compare rather than
	// startsWith for the same memcmp this is.
	if len(after) > len(before) && strings.HasPrefix(after, before) {
		d.out = append(d.out, Append{Path: path, Text: after[len(before):]})
		return
	}
	shared := overlap(before, after, d.scan)
	if shared == 0 {
		d.out = append(d.out, Set{Path: path, Value: after})
		return
	}
	d.out = append(d.out, Truncate{Path: path, Count: utf16Len(before) - shared})
	if rest := after[unitsOffset(after, shared):]; rest != "" {
		d.out = append(d.out, Append{Path: path, Text: rest})
	}
}

// unitsOffset is the byte offset in s after its first units UTF-16 code
// units — s.slice(units), for a cut that lands on a rune boundary.
func unitsOffset(s string, units int) int {
	i := 0
	for units > 0 && i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		units -= utf16.RuneLen(r)
		i += size
	}
	return i
}

// diffValue publishes whatever turns before into after at path: nothing for
// equal scalars, a string, array or object diff for matching kinds, and a
// whole set (or delete) otherwise.
func (d *differ) diffValue(before, after any, path Path) {
	if before == missing {
		if after != missing {
			d.emitSet(path, after)
		}
		return
	}
	if after == missing {
		d.emitDelete(path)
		return
	}
	if same(before, after) {
		return
	}
	switch b := before.(type) {
	case string:
		if a, ok := after.(string); ok {
			d.diffString(b, a, path)
			return
		}
	case []any:
		if a, ok := after.([]any); ok {
			d.diffArray(b, a, path)
			return
		}
	case map[string]any:
		if a, ok := after.(map[string]any); ok {
			d.diffObject(b, a, path)
			return
		}
	}
	d.emitSet(path, after)
}

// diffObject sets each member of after that differs and deletes each member
// of before that is gone. An object holding a reserved key is set whole:
// its members are never addressed by path.
//
// Members are visited in a deterministic order — integer-like keys first,
// ascending, then the rest by UTF-16 code unit — where upstream visits
// insertion order, which a Go map does not have. Either order yields the
// same replica.
func (d *differ) diffObject(before, after map[string]any, path Path) {
	for key := range before {
		if ReservedSegments[key] {
			d.emitSet(path, after)
			return
		}
	}
	for key := range after {
		if ReservedSegments[key] {
			d.emitSet(path, after)
			return
		}
	}
	for _, key := range keyOrder(after) {
		previous, ok := before[key]
		if !ok {
			previous = missing
		}
		d.diffValue(previous, after[key], childPath(path, Key(key)))
	}
	for _, key := range keyOrder(before) {
		if _, ok := after[key]; !ok {
			d.emitDelete(childPath(path, Key(key)))
		}
	}
}

// diffArray diffs equal lengths positionally; otherwise it finds the common
// prefix and suffix and, when they account for the shorter array, publishes
// the middle as one splice (or a whole set when nothing is kept). Structural
// movement combined with retained-index edits has no unique alignment:
// preserve the retained index deltas and express only the tail length change
// structurally. It may be broader than the producer's intent, but never
// degrades those edits to a whole-array replacement.
func (d *differ) diffArray(before, after []any, path Path) {
	if len(before) == len(after) {
		for i := range after {
			d.diffValue(before[i], after[i], childPath(path, Index(i)))
		}
		return
	}

	prefix := 0
	for prefix < len(before) && prefix < len(after) && jsonEqual(before[prefix], after[prefix]) {
		prefix++
	}
	suffix := 0
	for suffix < len(before)-prefix && suffix < len(after)-prefix &&
		jsonEqual(before[len(before)-1-suffix], after[len(after)-1-suffix]) {
		suffix++
	}
	shorter := min(len(before), len(after))
	if prefix+suffix == shorter {
		remove := len(before) - prefix - suffix
		items := after[prefix : len(after)-suffix]
		if prefix == 0 && remove == len(before) {
			d.emitSet(path, after)
		} else {
			d.out = append(d.out, Splice{Path: path, Index: prefix, Remove: remove, Items: cloneItems(items)})
		}
		return
	}

	for i := range shorter {
		d.diffValue(before[i], after[i], childPath(path, Index(i)))
	}
	switch {
	case len(after) > len(before):
		d.out = append(d.out, Splice{Path: path, Index: len(before), Items: cloneItems(after[len(before):])})
	case len(before) > len(after):
		if len(after) == 0 {
			d.emitSet(path, after)
		} else {
			d.out = append(d.out, Splice{Path: path, Index: len(after), Remove: len(before) - len(after), Items: []any{}})
		}
	}
}

// walkDirty descends the dirty tree, diffing at each marked node: a dirty
// value whole; an array replace as one set unless equal; an array diff, or
// an append whose premise no longer holds, positionally; an append as its
// older elements' own marks followed by one tail splice; and an unmarked
// container by its children.
func (d *differ) walkDirty(before, after any, node *dirtyNode, path Path) {
	if node.valueDirty {
		d.diffValue(before, after, path)
		return
	}
	if node.array != arrayClean {
		if node.array == arrayReplace {
			if !jsonEqual(before, after) {
				d.emitSet(path, after)
			}
			return
		}
		b, bok := before.([]any)
		a, aok := after.([]any)
		if !bok || !aok || node.array == arrayDiff {
			d.diffValue(before, after, path)
			return
		}
		start := node.start
		if len(b) != start || len(a) < start {
			d.diffValue(before, after, path)
			return
		}
		for _, child := range node.ordered() {
			i, ok := child.seg.(Index)
			if !ok || int(i) >= start {
				continue
			}
			d.walkChild(b, a, child, path)
		}
		if items := a[start:]; len(items) > 0 {
			d.out = append(d.out, Splice{Path: path, Index: start, Items: cloneItems(items)})
		}
		return
	}
	for _, child := range node.ordered() {
		d.walkChild(before, after, child, path)
	}
}

// walkChild diffs one touched member: whole when it appeared, vanished, was
// assigned, or is not a container on both sides; by its own marks otherwise.
func (d *differ) walkChild(before, after any, child dirtyChild, path Path) {
	previous := ownValue(before, child.seg)
	current := ownValue(after, child.seg)
	at := childPath(path, child.seg)
	if previous == missing || current == missing || child.node.valueDirty ||
		!isContainer(previous) || !isContainer(current) {
		d.diffValue(previous, current, at)
		return
	}
	d.walkDirty(previous, current, child.node, at)
}

// ─── Baseline sync ───────────────────────────────────────────────────────────

// syncBaseline brings the baseline up to root along the dirty paths by
// sharing references. It returns false — having changed nothing — if any
// dirty node is an array change other than a pure append, so the caller can
// replay ops instead: cloning a whole array there is O(n) per flush, replay
// is O(changes).
//
// Strings are immutable, so root's value is shared outright. Maps and slices
// are cloned because root keeps mutating them.
func (t *tracker) syncBaseline() bool {
	if !canSync(t.pending) {
		return false
	}
	t.baseline = syncInto(t.baseline, t.root, t.pending)
	return true
}

func canSync(node *dirtyNode) bool {
	if node.array != arrayClean && node.array != arrayAppend {
		return false
	}
	for _, child := range node.children {
		if !canSync(child) {
			return false
		}
	}
	return true
}

// syncInto advances baseline to root below node and returns it — a slice
// grown by an append is re-headered, so the parent must store the result.
func syncInto(baseline, root any, node *dirtyNode) any {
	if node.array == arrayAppend {
		b, bok := baseline.([]any)
		r, rok := root.([]any)
		if bok && rok {
			for seg, child := range node.children {
				if i, ok := seg.(Index); ok && int(i) < node.start {
					b = syncChild(b, r, seg, child).([]any)
				}
			}
			for _, item := range r[node.start:] {
				b = append(b, cloneJSON(item))
			}
			return b
		}
	}
	for seg, child := range node.children {
		baseline = syncChild(baseline, root, seg, child)
	}
	return baseline
}

// syncChild advances one member of parent to root's and returns the parent:
// removed when it is gone, replaced by a clone when it was assigned or is
// not a container of the same kind on both sides, and synced below
// otherwise.
func syncChild(parent, root any, seg Seg, child *dirtyNode) any {
	current := ownValue(root, seg)
	previous := ownValue(parent, seg)
	if current == missing {
		return removeMember(parent, seg)
	}
	if child.valueDirty || !isContainer(current) || !isContainer(previous) || isArray(current) != isArray(previous) {
		return setMember(parent, seg, cloneJSON(current))
	}
	return setMember(parent, seg, syncInto(previous, current, child))
}

func isArray(v any) bool {
	_, ok := v.([]any)
	return ok
}

// setMember is parent[seg] = value on a baseline container, growing a slice
// by one when the index is its length.
func setMember(parent any, seg Seg, value any) any {
	switch p := parent.(type) {
	case map[string]any:
		p[propertyKey(seg)] = value
		return p
	case []any:
		if i, ok := seg.(Index); ok {
			if int(i) < len(p) {
				p[i] = value
				return p
			}
			return append(p, value)
		}
	}
	return parent
}

// removeMember is `delete parent[seg]`, or a one-element splice on a slice.
func removeMember(parent any, seg Seg) any {
	switch p := parent.(type) {
	case map[string]any:
		delete(p, propertyKey(seg))
		return p
	case []any:
		if i, ok := seg.(Index); ok && int(i) < len(p) {
			return slices.Delete(p, int(i), int(i)+1)
		}
	}
	return parent
}

// cloneOps copies the payloads of a batch about to be replayed onto the
// baseline, so the baseline never aliases the batch handed to the consumer.
func cloneOps(ops []Op) []Op {
	out := make([]Op, len(ops))
	for i, op := range ops {
		switch op := op.(type) {
		case Replace:
			out[i] = Replace{Value: cloneJSON(op.Value)}
		case Set:
			out[i] = Set{Path: op.Path, Value: cloneJSON(op.Value)}
		case Splice:
			out[i] = Splice{Path: op.Path, Index: op.Index, Remove: op.Remove, Items: cloneItems(op.Items)}
		default:
			out[i] = op
		}
	}
	return out
}

// ─── JSON values ─────────────────────────────────────────────────────────────

// cloneJSON deep-copies the containers of a JSON tree; scalars, being
// values, are returned as they are.
func cloneJSON(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, c := range x {
			m[k] = cloneJSON(c)
		}
		return m
	case []any:
		return cloneItems(x)
	}
	return v
}

// cloneItems is cloneJSON for a splice payload, never nil.
func cloneItems(items []any) []any {
	out := make([]any, len(items))
	for i, item := range items {
		out[i] = cloneJSON(item)
	}
	return out
}

// jsonEqual is deep equality of two JSON values: numbers of any Go kind
// compare by value, containers member by member.
func jsonEqual(a, b any) bool {
	switch x := a.(type) {
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !jsonEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !jsonEqual(v, w) {
				return false
			}
		}
		return true
	}
	return same(a, b)
}

// number reads any Go numeric kind as the one JSON number it is.
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}

// keyOrder is the order an object's members are diffed in: integer-like keys
// first, ascending — JavaScript's own rule for them — then the rest by UTF-16
// code unit, JavaScript's string order.
func keyOrder(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		ia, oka := canonicalIndex(a)
		ib, okb := canonicalIndex(b)
		switch {
		case oka && okb:
			return ia - ib
		case oka:
			return -1
		case okb:
			return 1
		}
		return compareUTF16(a, b)
	})
	return keys
}

// compareUTF16 orders two strings by UTF-16 code unit, as JavaScript's <
// does. It differs from Go's byte order only between an astral rune and a
// BMP rune above the surrogate range, which UTF-8 puts first and UTF-16 last.
func compareUTF16(a, b string) int {
	for a != "" && b != "" {
		ra, na := utf8.DecodeRuneInString(a)
		rb, nb := utf8.DecodeRuneInString(b)
		if ra != rb {
			ua, ub := ra, rb
			if ua >= 0x10000 {
				ua = 0xD800 + (ua-0x10000)>>10
			}
			if ub >= 0x10000 {
				ub = 0xD800 + (ub-0x10000)>>10
			}
			if ua != ub {
				return int(ua) - int(ub)
			}
			// Same high surrogate: the low surrogates order as the runes do.
			return int(ra) - int(rb)
		}
		a, b = a[na:], b[nb:]
	}
	return len(a) - len(b)
}
