package delta

import (
	"encoding/json"
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
// Upstream records ops ON MUTATION and coalesces them, rather than diffing a
// published baseline at flush. Its producer mutates a Proxy whose traps append
// to a pending operation log; flush replays that log. Keeping no baseline is
// what makes the streaming case cheap — this state flushes per streamed token,
// and a baseline has to be advanced on every one of them.
//
// Go has no Proxy, and no way to intercept an assignment to a map or slice
// element. The mirror is a cursor: a State addresses one position in the
// tracked tree and offers the same mutations the Proxy traps — set, delete and
// the array methods — each of which records the op upstream's trap records.
// The tracked tree is the any-tree encoding/json produces (the form Apply works
// on), so no per-type code generation is needed: a producer's state is JSON on
// the wire, and JSON in memory keeps one log and one walk for every state
// shape.
//
// A flush guarantees convergence, not a minimal or canonical batch. Mutations
// that cancel out can still publish, and a long mutation window collapses to a
// complete base batch so the log and its path trie stay bounded.

// defaultMaxOverlapScan is how far back a string diff looks for a rolling
// window, in UTF-16 code units, when no option says otherwise.
const defaultMaxOverlapScan = 65_536

// maxPendingLog bounds the pending log and its path trie. Past it the window
// collapses to one complete base batch: the log stops growing, at the price of
// a whole-value snapshot and an extra recovery point.
const maxPendingLog = 4_096

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
// into the current one, or an empty batch when nothing was mutated. Mutate the
// value through State; the object handed to Track, and every value later
// inserted, becomes tracker-owned.
type Tracker[T any] struct{ core *tracker }

type tracker struct {
	scan int
	root any

	// The pending operation log. log is flush order with tombstones; trie
	// indexes it by path so a later op can supersede or fold into an earlier
	// one.
	log           []*slot
	trie          *logNode
	nextOrder     int
	tombstones    int
	liveSlots     int
	nodeCount     int
	lastAddedSlot *slot
	hasPending    bool
	forceBase     bool

	// Where the cursors point. rootCell is the root position; shape is bumped
	// by every structural mutation and invalidates the cells' cached paths.
	// positions holds the known positions of each tracked object, and aliased
	// says whether anything has more than one — until it does, a write has
	// exactly one path and the lookup is skipped.
	rootCell        *cell
	shape           uint64
	positions       map[uintptr]*positionSet
	positionsPruned int
	aliased         bool
}

// Track starts tracking root, which must be a JSON object (map[string]any)
// or array ([]any) — anything else panics, as a Proxy over a non-object
// would. The value is adopted, not copied: mutate it only through State.
func Track[T any](root T, opts ...Option) *Tracker[T] {
	core := &tracker{scan: defaultMaxOverlapScan, forceBase: true}
	core.clearPending()
	core.adopt(root)
	for _, opt := range opts {
		opt.apply(core)
	}
	return &Tracker[T]{core: core}
}

// adopt takes v as the tracked root, refusing anything but a container, and
// starts a fresh position graph over it: the cursors into the old tree address
// a value that is gone.
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
	t.shape++
	t.positions, t.positionsPruned, t.aliased = nil, 0, false
	t.rootCell = &cell{target: v}
	t.register(v, t.rootCell)
}

// State is a cursor at the root of the tracked value. Read and mutate the
// value through it.
func (t *Tracker[T]) State() State { return State{t: t.core, c: t.core.rootCell} }

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
	t.core.forceBase = true
}

// Rebase makes the next Flush a complete base batch without changing the
// value. This is the checkpoint: recovery replays from the last base batch,
// so a producer must be able to bound that.
func (t *Tracker[T]) Rebase() {
	t.core.clearPending()
	t.core.forceBase = true
}

// Discard drops the pending ops without publishing them. The value keeps its
// mutations; replicas that exist never learn of them, so use it only when they
// need not.
func (t *Tracker[T]) Discard() { t.core.clearPending() }

// Dirty reports whether a Flush would emit anything: a base batch is owed, or
// something was mutated since the last one. It is conservative — mutations
// that cancel each other out still count.
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
		t.forceBase = false
		t.clearPending()
		return []Op{Replace{Value: value}}
	}
	if !t.hasPending {
		return []Op{}
	}
	d := differ{scan: t.scan, out: []Op{}}
	for _, s := range t.log {
		if s == nil || s.dead {
			continue
		}
		if s.str != nil {
			// An anchored string slot holds the value the path had at the
			// first write in this window and the value it holds now; the pair
			// diffs once, here, into the same append or truncate+append a
			// baseline would have produced.
			d.diffValue(s.str.anchor, s.str.value, opPath(s.op))
			continue
		}
		d.out = append(d.out, s.op)
	}
	t.clearPending()
	return d.out
}

// ─── Pending operation log ───────────────────────────────────────────────────

// strAnchor is an anchored string slot: the value at the path when this window
// first wrote it, and the value it holds now.
type strAnchor struct{ anchor, value string }

// slot is one recorded op. order is the record order, which decides dominance
// between a path and its ancestors; index is the slot's place in the log, so
// killing it can tombstone that place in O(1).
type slot struct {
	op    Op
	dead  bool
	order int
	index int
	str   *strAnchor
}

// logNode is one path in the pending log's trie. retiredKids are generations
// detached by a splice: their ops stay live and ordered, but nothing folds
// into them any more, because the splice renumbered what they address.
type logNode struct {
	slots       []*slot
	kids        map[Seg]*logNode
	retiredKids []map[Seg]*logNode
	lastOrder   int // -1 until something is recorded here
}

func newLogNode() *logNode { return &logNode{lastOrder: -1} }

func (t *tracker) clearPending() {
	t.log = nil
	t.trie = newLogNode()
	t.nextOrder = 0
	t.tombstones = 0
	t.liveSlots = 0
	t.nodeCount = 1
	t.lastAddedSlot = nil
	t.hasPending = false
}

// logNodeAt is the trie node for path, created along the way.
func (t *tracker) logNodeAt(path Path) *logNode {
	at := t.trie
	for _, seg := range path {
		if at.kids == nil {
			at.kids = map[Seg]*logNode{}
		}
		next, ok := at.kids[seg]
		if !ok {
			next = newLogNode()
			at.kids[seg] = next
			t.nodeCount++
		}
		at = next
	}
	return at
}

// findLogNode is logNodeAt without creating anything; nil when the path has
// never been recorded.
func (t *tracker) findLogNode(path Path) *logNode {
	at := t.trie
	for _, seg := range path {
		next, ok := at.kids[seg]
		if !ok {
			return nil
		}
		at = next
	}
	return at
}

// compactLog drops tombstones once they dominate the log.
func (t *tracker) compactLog() {
	if t.tombstones < 1_024 || t.tombstones*2 < len(t.log) {
		return
	}
	compacted := make([]*slot, 0, t.liveSlots)
	for _, s := range t.log {
		if s == nil {
			continue
		}
		s.index = len(compacted)
		compacted = append(compacted, s)
	}
	t.log = compacted
	t.tombstones = 0
}

func (t *tracker) killSlot(s *slot) {
	if s.dead {
		return
	}
	s.dead = true
	t.liveSlots--
	if s.index < len(t.log) && t.log[s.index] == s {
		t.log[s.index] = nil
		t.tombstones++
	}
}

// liveSlot is the newest op recorded at this node, or nil.
func liveSlot(at *logNode) *slot {
	for len(at.slots) > 0 && at.slots[len(at.slots)-1].dead {
		at.slots = at.slots[:len(at.slots)-1]
	}
	if len(at.slots) == 0 {
		at.slots = nil
		return nil
	}
	return at.slots[len(at.slots)-1]
}

// killHere drops every op recorded at this node, keeping its children.
func (t *tracker) killHere(at *logNode) {
	if at.slots == nil {
		return
	}
	for _, s := range at.slots {
		t.killSlot(s)
	}
	at.slots = nil
}

func (t *tracker) addSlot(at *logNode, s *slot) {
	t.compactLog()
	s.order = t.nextOrder
	t.nextOrder++
	s.index = len(t.log)
	at.lastOrder = s.order
	at.slots = append(at.slots, s)
	t.log = append(t.log, s)
	t.liveSlots++
	t.lastAddedSlot = s
}

// collapsePending bounds a long mutation window. Without it the log and the
// retired path generations grow for as long as the producer mutates without
// flushing. Past the bound the window becomes one complete snapshot: metadata
// stops accumulating, at the cost of a full payload and an extra recovery
// point. Payload bytes and peak allocation stay workload-dependent.
func (t *tracker) collapsePending() {
	if t.forceBase || (t.liveSlots <= maxPendingLog && t.nodeCount <= maxPendingLog) {
		return
	}
	value := cloneJSON(t.root)
	t.clearPending()
	t.hasPending = true
	t.addSlot(t.trie, &slot{op: Replace{Value: value}})
}

// killSubtree drops every op at and below this node, retired generations
// included: a replacement here dominates all of them.
func (t *tracker) killSubtree(at *logNode) {
	t.killHere(at)
	for _, child := range at.kids {
		t.killSubtree(child)
	}
	at.kids = nil
	for _, generation := range at.retiredKids {
		for _, child := range generation {
			t.killSubtree(child)
		}
	}
	at.retiredKids = nil
}

// retireKids detaches this node's children. A splice keeps their ops — they
// were recorded against the indices of an earlier generation and must still
// apply in order — but nothing may fold into them afterwards.
func retireKids(at *logNode) {
	if at.kids == nil {
		return
	}
	at.retiredKids = append(at.retiredKids, at.kids)
	at.kids = nil
}

// foldSite is where a later write belongs INSIDE an already-recorded payload.
// item is the index within a splice's inserted items, or -1 when the payload
// is a set or a replacement.
type foldSite struct {
	slot *slot
	rest Path
	item int
}

// foldTarget is the deepest live ancestor op carrying a payload a later write
// can be folded into. Set and Replace carry the whole subtree; a Splice
// carries the items it inserted, so a write to one of those indices belongs
// inside the payload rather than after it.
func (t *tracker) foldTarget(path Path) (foldSite, bool) {
	at := t.trie
	var found *slot
	foundDepth, foundItem := 0, -1
	ancestorMax := -1
	for depth := 0; depth < len(path); depth++ {
		if found != nil && at.lastOrder > found.order {
			found = nil
		}
		// A fold is sound only if nothing has been recorded at this path or
		// above it since: a later op there (a splice on the same array, a
		// replacement of an ancestor) would have to apply after this write,
		// not before it. Writes to other branches are irrelevant, which is why
		// this is not "the most recent op".
		if s := liveSlot(at); s != nil && s.order >= ancestorMax && at.lastOrder == s.order {
			switch op := s.op.(type) {
			case Set, Replace:
				found, foundDepth, foundItem = s, depth, -1
			case Splice:
				if i, ok := path[depth].(Index); ok && int(i) >= op.Index && int(i) < op.Index+len(op.Items) {
					found, foundDepth, foundItem = s, depth+1, int(i)-op.Index
				}
			}
		}
		if at.lastOrder > ancestorMax {
			ancestorMax = at.lastOrder
		}
		next, ok := at.kids[path[depth]]
		if !ok {
			break
		}
		at = next
	}
	if found == nil {
		return foldSite{}, false
	}
	return foldSite{slot: found, rest: path[foundDepth:], item: foundItem}, true
}

// payload is the container a fold writes into: a Replace's or Set's value, or
// one of a Splice's inserted items.
func (site foldSite) payload() any {
	if site.item >= 0 {
		return site.slot.op.(Splice).Items[site.item]
	}
	if r, ok := site.slot.op.(Replace); ok {
		return r.Value
	}
	return site.slot.op.(Set).Value
}

// setItem replaces one of a splice's inserted items, for a fold whose rest is
// empty — the write lands on the item itself.
func (site foldSite) setItem(value any) { site.slot.op.(Splice).Items[site.item] = value }

// foldInto applies op to container at rest, reporting whether it could.
// Failure is not an error: the caller records the op instead.
func foldInto(container any, rest Path, op Op) bool {
	if len(rest) == 0 {
		return false
	}
	target := container
	for _, seg := range rest[:len(rest)-1] {
		if !isContainer(target) {
			return false
		}
		target = ownValue(target, seg)
	}
	if !isContainer(target) {
		return false
	}
	key := rest[len(rest)-1]
	switch op := op.(type) {
	case Set:
		// A Go map has no prototype, so writing __proto__ is an ordinary
		// member write — but the payload crosses to replicas that do, and
		// nothing may address it by path there.
		if k, ok := key.(Key); ok && string(k) == "__proto__" {
			return false
		}
		return writeMember(target, key, cloneJSON(op.Value))
	case Delete:
		return deleteMember(target, key)
	case Append:
		s, ok := ownValue(target, key).(string)
		if !ok {
			return false
		}
		return writeMember(target, key, s+op.Text)
	case Truncate:
		s, ok := ownValue(target, key).(string)
		if !ok {
			return false
		}
		return writeMember(target, key, truncateUTF16(s, op.Count))
	case Splice:
		xs, ok := ownValue(target, key).([]any)
		if !ok {
			return false
		}
		return writeMember(target, key, splice(xs, op.Index, op.Remove, cloneItems(op.Items)))
	}
	return false
}

// writeMember is parent[key] = value inside a pending payload. A slice index
// past the end is refused rather than grown: the holder cannot re-header its
// parent, and the caller records the op instead.
func writeMember(parent any, key Seg, value any) bool {
	switch p := parent.(type) {
	case map[string]any:
		p[propertyKey(key)] = value
		return true
	case []any:
		if i, ok := key.(Index); ok && i >= 0 && int(i) < len(p) {
			p[i] = value
			return true
		}
	}
	return false
}

// deleteMember is `delete parent[key]` inside a pending payload. An array
// element is refused: removing one would renumber the payload, and no op the
// tracker records deletes through an array.
func deleteMember(parent any, key Seg) bool {
	if p, ok := parent.(map[string]any); ok {
		delete(p, propertyKey(key))
		return true
	}
	return false
}

// recordString records a write of one string at path, anchored to the value
// the path held when this window first wrote it. Every later write updates the
// anchored value, and flush diffs anchor → final once. That yields the same
// truncate/append pair a baseline diff would, without keeping a baseline for
// the whole document.
func (t *tracker) recordString(path Path, previous, value string) {
	if t.forceBase {
		return
	}
	t.hasPending = true
	// A string inside a pending payload belongs in that payload, as for any
	// other write.
	if site, ok := t.foldTarget(path); ok {
		if site.item >= 0 && len(site.rest) == 0 {
			site.setItem(value)
			return
		}
		if foldInto(site.payload(), site.rest, Set{Path: path, Value: value}) {
			return
		}
	}
	at := t.logNodeAt(path)
	live := liveSlot(at)
	if live != nil && live.str != nil {
		live.str.value = value
		return
	}
	if live != nil {
		// A pending set or delete at this path already replaced the value;
		// keep that op and carry the new value in it rather than anchoring to
		// a value the replica will never hold.
		switch op := live.op.(type) {
		case Replace:
			live.op = Replace{Value: value}
			return
		case Set:
			live.op = Set{Path: op.Path, Value: value}
			return
		case Delete:
			t.killHere(at)
			t.addSlot(at, &slot{op: Set{Path: path, Value: value}})
			return
		}
		// A truncate/append pair from an earlier string diff: both must go.
		t.killHere(at)
	}
	t.killSubtree(at)
	t.addSlot(at, &slot{op: Set{Path: path, Value: value}, str: &strAnchor{anchor: previous, value: value}})
}

// record adds op to the pending log, superseding, folding or coalescing it
// with what is already recorded where that is sound.
func (t *tracker) record(op Op) {
	if t.forceBase {
		return
	}
	t.hasPending = true
	path := opPath(op)
	existing := t.findLogNode(path)
	var anchored *slot
	if existing != nil {
		anchored = liveSlot(existing)
	}
	if anchored != nil && anchored.str != nil {
		switch op := op.(type) {
		case Append:
			anchored.str.value += op.Text
			return
		case Truncate:
			anchored.str.value = truncateUTF16(anchored.str.value, op.Count)
			return
		case Set:
			if s, ok := op.Value.(string); ok {
				anchored.str.value = s
				return
			}
		}
		t.killSubtree(existing)
	} else if existing != nil && replaces(op) {
		// A replacement absorbed into an ancestor payload must still
		// invalidate ops already recorded at and below its destination.
		t.killSubtree(existing)
	}

	if len(path) > 0 {
		if site, ok := t.foldTarget(path); ok {
			if site.item >= 0 && len(site.rest) == 0 {
				if s, ok := op.(Set); ok {
					site.setItem(cloneJSON(s.Value))
					return
				}
			} else if foldInto(site.payload(), site.rest, op) {
				return
			}
		}
	}

	at := t.logNodeAt(path)
	if live := liveSlot(at); live != nil {
		if t.coalesce(live, op) {
			return
		}
		if replaces(op) {
			t.killHere(at)
		}
	}
	if replaces(op) {
		// Replacements dominate all earlier descendants, including
		// generations detached by array splices.
		t.killSubtree(at)
	} else if _, ok := op.(Splice); ok {
		// Splices preserve earlier writes but form a barrier for later
		// folding.
		retireKids(at)
	}
	t.addSlot(at, &slot{op: op})
}

// replaces reports whether op supersedes everything recorded below its path.
func replaces(op Op) bool {
	switch op.(type) {
	case Set, Delete, Replace:
		return true
	}
	return false
}

// coalesce folds op into the newest op at the same path where that is exactly
// equivalent, reporting whether it did. The splice rewrites are sound only for
// adjacent recorded ops; a tombstoned op in between remains a barrier through
// lastAddedSlot.
func (t *tracker) coalesce(live *slot, op Op) bool {
	switch op := op.(type) {
	case Append:
		switch prev := live.op.(type) {
		case Append:
			live.op = Append{Path: prev.Path, Text: prev.Text + op.Text}
			return true
		case Replace:
			if s, ok := prev.Value.(string); ok {
				live.op = Replace{Value: s + op.Text}
				return true
			}
		case Set:
			if s, ok := prev.Value.(string); ok {
				live.op = Set{Path: prev.Path, Value: s + op.Text}
				return true
			}
		}
	case Truncate:
		switch prev := live.op.(type) {
		case Replace:
			if s, ok := prev.Value.(string); ok {
				live.op = Replace{Value: truncateUTF16(s, op.Count)}
				return true
			}
		case Set:
			if s, ok := prev.Value.(string); ok {
				live.op = Set{Path: prev.Path, Value: truncateUTF16(s, op.Count)}
				return true
			}
		}
	case Splice:
		prev, ok := live.op.(Splice)
		if !ok || t.lastAddedSlot != live || prev.Remove != 0 {
			return false
		}
		items := prev.Items
		// An adjacent tail insert extends the pending one.
		if op.Remove == 0 && prev.Index+len(items) == op.Index {
			live.op = Splice{Path: prev.Path, Index: prev.Index, Items: append(slices.Clip(items), op.Items...)}
			return true
		}
		// A splice inside the pending insert edits its payload.
		if op.Index >= prev.Index && op.Index+op.Remove <= prev.Index+len(items) {
			t.replaceItems(live, prev, splice(items, op.Index-prev.Index, op.Remove, op.Items))
			return true
		}
		// A removal off the pending insert's tail shortens it.
		if op.Remove > 0 && len(op.Items) == 0 && len(items) > 0 {
			if from := op.Index - prev.Index; from >= 0 && from+op.Remove == len(items) {
				t.replaceItems(live, prev, items[:from])
				return true
			}
		}
	}
	return false
}

// replaceItems rewrites a pending insert's payload, dropping the op when the
// rewrite leaves it inserting nothing and removing nothing.
func (t *tracker) replaceItems(live *slot, prev Splice, items []any) {
	live.op = Splice{Path: prev.Path, Index: prev.Index, Items: items}
	if len(items) == 0 {
		t.killSlot(live)
	}
}

// diffInto records what turns before into after at path. It is the local diff
// used when a whole container is assigned: it keeps op quality without a
// baseline by comparing the outgoing value with the incoming one at the moment
// of the write. String leaves go through recordString so every string path
// keeps a single anchored slot; otherwise a later write to the same string
// would have to supersede ops whose starting value it no longer knows.
func (t *tracker) diffInto(before, after any, at Path) {
	if t.forceBase {
		return
	}
	t.hasPending = true
	if same(before, after) {
		return
	}
	if b, ok := before.(string); ok {
		if a, ok := after.(string); ok {
			t.recordString(at, b, a)
			return
		}
	}
	if b, ok := before.([]any); ok {
		if a, ok := after.([]any); ok && len(b) == len(a) {
			for i := range a {
				t.diffInto(b[i], a[i], childPath(at, Index(i)))
			}
			return
		}
	}
	if b, ok := before.(map[string]any); ok {
		if a, ok := after.(map[string]any); ok {
			if hasReservedKey(b) || hasReservedKey(a) {
				t.record(Set{Path: at, Value: cloneJSON(after)})
				return
			}
			for _, key := range keyOrder(a) {
				child := childPath(at, Key(key))
				if previous, ok := b[key]; ok {
					t.diffInto(previous, a[key], child)
				} else {
					t.record(Set{Path: child, Value: cloneJSON(a[key])})
				}
			}
			for _, key := range keyOrder(b) {
				if _, ok := a[key]; !ok {
					t.record(Delete{Path: childPath(at, Key(key))})
				}
			}
			return
		}
	}
	// Arrays of differing length, and everything else: chord's own diff.
	d := differ{scan: t.scan}
	d.diffValue(before, after, at)
	for _, op := range d.out {
		t.record(op)
	}
}

func hasReservedKey(m map[string]any) bool {
	for key := range m {
		if ReservedSegments[key] {
			return true
		}
	}
	return false
}

// ─── State ───────────────────────────────────────────────────────────────────

// State is a cursor into a tracked value: the tracker plus one position in
// its tree. It is the Go form of upstream's proxy. A cursor holds a cell — a
// parent link plus a segment — and walks its path at use time, so:
//
//   - it follows a replaced child rather than pointing at the old one;
//   - it stays correct across an array operation that renumbers it, so a
//     cursor taken at index 2 addresses index 3 after an Unshift;
//   - once the element it addresses leaves the document, a write through it
//     mutates that value and publishes nothing, as a plain Go write would.
//
// Segments are given as any: a string is an object key, an int (or any
// integral Go number) an array index, and a Key or Index is taken as is. On
// an array a numeric string is the index it spells, and on an object an index
// is the key it spells, as JavaScript coerces them.
//
// Values inserted through a State are adopted: a map becomes part of the
// tracked tree and may be read through a retained reference, but must not be
// mutated outside the tracker. One map MAY occupy several positions, and each
// live position is published — but only positions the tracker has seen, which
// means a cursor was taken at one of them before the other was assigned. A
// slice is adopted by its header, so it cannot occupy two positions (D65).
// A number of any Go kind is one JSON number. A State itself is not a value:
// assigning one to its own slot is a no-op, anywhere else a panic.
//
// A Tracker and its cursors are not safe for concurrent use: a cursor caches
// its path, and a mutation renumbers cells.
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
	t *tracker
	c *cell
	// blocked is the reserved key this cursor was reached through, if any: the
	// subtree can be read and serialised but not mutated through that key.
	blocked Seg
}

// path is the cursor's current position, walked from its cell.
func (s State) path() Path { return s.t.pathOf(s.c) }

// Path is the cursor's path from the root; empty at the root. It is the
// cursor's position NOW, which a structural mutation of an array above it
// changes. A cursor taken below a container that occupies several positions
// reports the first of them, which is the position its ops address.
func (s State) Path() Path { return slices.Clone(s.path()) }

// container is the value at the cursor and whether there is one. A cursor
// whose position has left the document still names the container it was taken
// at — that value is readable and mutable, it is simply no longer published.
func (s State) container() (any, bool) {
	if s.c.detached() {
		return s.c.target, s.c.target != nil
	}
	return resolveValue(s.t.root, s.path())
}

// At is the cursor for a child. It does not require the child to exist: one
// that does not reads as absent and refuses to be written to.
func (s State) At(seg any) State {
	container, _ := s.container()
	key := normSeg(mustSeg(seg), container)
	blocked := s.blocked
	if k, ok := key.(Key); ok && blocked == nil && ReservedSegments[string(k)] {
		blocked = k
	}
	value := ownValue(container, key)
	if value == missing {
		value = nil
	}
	// A member hangs off the container's primary position, not off the cursor
	// it was reached through: upstream shares one wrapper — and one child
	// cache — across every position of an object, so the members of an
	// aliased object are addressed through its first live position.
	parent := s.c
	if _, primary := s.t.positionsAt(s.c); primary != nil {
		parent = primary
	}
	_, inArray := container.([]any)
	return State{t: s.t, c: s.t.childCell(parent, key, value, inArray, blocked != nil), blocked: blocked}
}

// Value is the current value at the cursor, or nil when the path does not
// resolve. A container is returned as is — the tracked tree itself, to be
// read and not mutated. A cursor whose element has left the document still
// reads that element.
func (s State) Value() any {
	v, _ := s.container()
	return v
}

// Get is the member's value, or nil when absent. Lookup tells the two apart.
func (s State) Get(seg any) any {
	v, _ := s.Lookup(seg)
	return v
}

// Lookup is the member's value and whether it is present.
func (s State) Lookup(seg any) (any, bool) {
	container, ok := s.container()
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
// already has (a scalar by value, a container by identity) records nothing.
// nil is JSON null; absence is spelled with Delete.
//
// A whole-container assignment is diffed against the outgoing value at the
// moment of the write, so a producer that rebuilds its partial each frame
// still publishes appends rather than replacements.
func (s State) Set(seg any, value any) {
	s.mutate(func(container any) any {
		key := s.segFor(seg, container)
		if cursor, ok := value.(State); ok {
			if cursor.t == s.t && slices.Equal(cursor.path(), childPath(s.path(), key)) {
				return container // the child assigned back to its own slot
			}
			panic(errAliasedCursor)
		}
		// The assigned container may already live elsewhere in the document.
		// This becomes another of its positions, so later writes through it
		// emit an op for each.
		if set := s.t.positionsOf(value); set != nil {
			if _, primary := s.t.positionsAt(s.c); primary != nil {
				s.t.alias(set, &cell{parent: primary, seg: key, target: value})
			}
		}
		switch c := container.(type) {
		case []any:
			i, ok := key.(Index)
			if !ok || int(i) > len(c) {
				// A gap would make a sparse array, which no JSON replica can hold.
				panic(&UnsafePathError{Segment: key})
			}
			if int(i) == len(c) {
				s.emit(Splice{Path: slices.Clone(s.writePath()), Items: []any{cloneJSON(value)}, Index: len(c)})
				return append(c, value)
			}
			if same(c[i], value) {
				return c
			}
			s.assign(key, c[i], value)
			c[i] = value
			return c
		case map[string]any:
			k := propertyKey(key)
			previous, has := c[k]
			if has && same(previous, value) {
				return c
			}
			if !has {
				previous = missing
			}
			s.assign(key, previous, value)
			c[k] = value
			return c
		}
		panic(&PathError{Ref: s.path()})
	})
	s.t.collapsePending()
}

// assign records one member write, choosing the op quality upstream's set
// trap chooses: a local diff between two containers, an anchored string, or a
// whole-value set. A container diff addresses the primary position only, as
// upstream's does; a string is anchored at every live position, because each
// one's anchor is the value the replica holds there.
func (s State) assign(key Seg, previous, value any) {
	many, primary := s.t.positionsAt(s.c)
	if primary == nil {
		return
	}
	if isContainer(previous) && isContainer(value) {
		s.t.diffInto(previous, value, childPath(s.t.pathOf(primary), key))
		return
	}
	if p, ok := previous.(string); ok {
		if v, ok := value.(string); ok {
			if many == nil {
				s.t.recordString(childPath(s.t.pathOf(primary), key), p, v)
				return
			}
			for _, c := range many {
				s.t.recordString(childPath(s.t.pathOf(c), key), p, v)
			}
			return
		}
	}
	s.emit(Set{Path: childPath(s.t.pathOf(primary), key), Value: cloneJSON(value)})
}

// Delete removes an object property. Deleting one that is absent records
// nothing, as `delete` on a missing property publishes nothing. An array
// element cannot be deleted — that would leave a hole; use Splice.
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
			k := propertyKey(key)
			if _, ok := c[k]; ok {
				s.emit(Delete{Path: childPath(s.writePath(), key)})
			}
			delete(c, k)
			return c
		}
		panic(&PathError{Ref: s.path()})
	})
	s.t.collapsePending()
}

// Push appends to an array and returns the new length. Adjacent pushes before
// one flush normally coalesce into one tail splice.
func (s State) Push(items ...any) (length int) {
	adoptItems(items)
	s.arrayOp("Push", func(xs []any) ([]any, structural) {
		before := len(xs)
		if len(items) > 0 {
			s.emit(Splice{Path: slices.Clone(s.writePath()), Index: before, Items: cloneItems(items)})
		}
		xs = append(xs, items...)
		length = len(xs)
		// An append shifts nothing, so no cell needs renumbering.
		return xs, structural{insertAt: before, insertCount: len(items)}
	})
	return length
}

// Unshift inserts at the front and returns the new length.
func (s State) Unshift(items ...any) (length int) {
	adoptItems(items)
	s.arrayOp("Unshift", func(xs []any) ([]any, structural) {
		if len(items) > 0 {
			s.emit(Splice{Path: slices.Clone(s.writePath()), Items: cloneItems(items)})
		}
		xs = slices.Insert(xs, 0, items...)
		length = len(xs)
		return xs, structural{shifts: true, insert: len(items), insertCount: len(items)}
	})
	return length
}

// Pop removes and returns the last element; false when the array is empty.
// Popping an element pushed since the last flush cancels the push.
func (s State) Pop() (value any, ok bool) {
	s.arrayOp("Pop", func(xs []any) ([]any, structural) {
		before := len(xs)
		shape := structural{shifts: true, index: before - 1, remove: min(before, 1), insertAt: -1}
		if before == 0 {
			return xs, shape
		}
		last := before - 1
		s.emit(Splice{Path: slices.Clone(s.writePath()), Index: last, Remove: 1, Items: []any{}})
		value, ok = xs[last], true
		xs[last] = nil
		return xs[:last], shape
	})
	return value, ok
}

// Shift removes and returns the first element; false when the array is empty.
func (s State) Shift() (value any, ok bool) {
	s.arrayOp("Shift", func(xs []any) ([]any, structural) {
		shape := structural{shifts: true, remove: min(len(xs), 1), insertAt: -1}
		if len(xs) == 0 {
			return xs, shape
		}
		s.emit(Splice{Path: slices.Clone(s.writePath()), Remove: 1, Items: []any{}})
		value, ok = xs[0], true
		return slices.Delete(xs, 0, 1), shape
	})
	return value, ok
}

// Splice is Array.prototype.splice: remove `remove` elements at index and
// insert items there, returning the removed elements. A negative index counts
// from the end; an index past the end appends; a remove count past the end
// is clamped. A splice that clears the whole array publishes as a set (or, on
// the root, a Replace).
func (s State) Splice(index, remove int, items ...any) (removed []any) {
	adoptItems(items)
	s.arrayOp("Splice", func(xs []any) ([]any, structural) {
		before := len(xs)
		index, remove = spliceRange(before, index, remove)
		if remove > 0 || len(items) > 0 {
			if index == 0 && remove == before {
				// A splice that clears the whole array is a replacement of it.
				s.snapshot(cloneItems(items))
			} else {
				s.emit(Splice{Path: slices.Clone(s.writePath()), Index: index, Remove: remove, Items: cloneItems(items)})
			}
		}
		removed = slices.Clone(xs[index : index+remove])
		return splice(xs, index, remove, items), structural{
			shifts: true, index: index, remove: remove, insert: len(items),
			insertAt: index, insertCount: len(items),
		}
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

// snapshot publishes the array whole — a Replace at the root, a Set below it.
// The whole-array mutators have no op of their own, so they record what they
// produced.
func (s State) snapshot(value []any) {
	if len(s.writePath()) == 0 {
		s.emit(Replace{Value: value})
		return
	}
	s.emit(Set{Path: slices.Clone(s.writePath()), Value: value})
}

// permuted is what sort, reverse, fill and copyWithin report: they move
// elements without a uniform shift, so a held child is relocated by identity.
var permuted = structural{permutes: true, insertAt: -1}

// Sort sorts the array in place, stably, by cmp. JavaScript's default order
// — by string form, in UTF-16 code units — is a footgun on numbers, so there
// is no default; pass the order you mean.
func (s State) Sort(cmp func(a, b any) int) {
	s.arrayOp("Sort", func(xs []any) ([]any, structural) {
		slices.SortStableFunc(xs, cmp)
		s.snapshot(cloneItems(xs))
		return xs, permuted
	})
}

// Reverse reverses the array in place.
func (s State) Reverse() {
	s.arrayOp("Reverse", func(xs []any) ([]any, structural) {
		slices.Reverse(xs)
		s.snapshot(cloneItems(xs))
		return xs, permuted
	})
}

// Fill is Array.prototype.fill: it sets the elements in [start, end) to
// value, with negative bounds counted from the end and both clamped. A map
// filled into several slots lives at several positions and is published at
// each, as JavaScript's reference semantics require.
func (s State) Fill(value any, start, end int) {
	adoptItems([]any{value})
	s.arrayOp("Fill", func(xs []any) ([]any, structural) {
		from, to := relativeIndex(start, len(xs)), relativeIndex(end, len(xs))
		for i := from; i < to; i++ {
			xs[i] = value
		}
		s.snapshot(cloneItems(xs))
		return xs, permuted
	})
}

// CopyWithin is Array.prototype.copyWithin: it copies the elements in
// [start, end) to target, within the array, with the same bounds rules as
// Fill. Reference semantics apply, as they do in JavaScript.
func (s State) CopyWithin(target, start, end int) {
	s.arrayOp("CopyWithin", func(xs []any) ([]any, structural) {
		to := relativeIndex(target, len(xs))
		from, final := relativeIndex(start, len(xs)), relativeIndex(end, len(xs))
		if count := min(final-from, len(xs)-to); count > 0 {
			copy(xs[to:to+count], xs[from:from+count])
		}
		s.snapshot(cloneItems(xs))
		return xs, permuted
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
	s.arrayOp("SetLen", func(xs []any) ([]any, structural) {
		before := len(xs)
		switch {
		case n < 0:
			panic(fmt.Errorf("delta: invalid array length %d", n))
		case n < before:
			if n == 0 {
				s.snapshot([]any{})
			} else {
				s.emit(Splice{Path: slices.Clone(s.writePath()), Index: n, Remove: before - n, Items: []any{}})
			}
			clear(xs[n:])
			// Truncation removes elements, so held children shift like a splice.
			return xs[:n], structural{shifts: true, index: n, remove: before - n, insertAt: -1}
		case n > before:
			s.emit(Splice{Path: slices.Clone(s.writePath()), Index: before, Items: make([]any, n-before)})
			return append(xs, make([]any, n-before)...), structural{insertAt: -1}
		}
		return xs, structural{insertAt: -1}
	})
}

var (
	errSparseDelete  = errors.New("delta: delete would create a sparse array; use Splice instead")
	errAliasedCursor = errors.New("delta: a State cursor is not a value; insert a copy of its Value() instead")
)

// mutate resolves the container the cursor addresses, hands it to fn, and
// stores what fn returns in its place — an append re-headers a slice, so the
// write back is what keeps a root or nested array attached.
//
// A cursor whose container has no live position mutates that container
// directly and records nothing: it has left the document, and a plain Go
// write through a removed element behaves the same way.
func (s State) mutate(fn func(container any) any) {
	if s.blocked != nil {
		panic(&UnsafePathError{Segment: s.blocked})
	}
	_, primary := s.t.positionsAt(s.c)
	if primary == nil && isContainer(s.c.target) {
		s.t.refreshTarget(s.c, fn(s.c.target))
		return
	}
	at := s.path()
	if primary != nil {
		at = s.t.pathOf(primary)
	}
	root, err := walk(s.t.root, at, false, func(node any) (any, error) {
		out := fn(node)
		s.t.refreshTarget(s.c, out)
		return out, nil
	})
	if err != nil {
		panic(err)
	}
	s.t.root = root
}

// arrayOp is mutate for the array methods, which have no meaning elsewhere.
// fn mutates the array and records what it did; the shape it reports drives
// the renumbering of the cells held through that array, and the registration
// of an inserted value that already lives elsewhere.
func (s State) arrayOp(verb string, fn func(xs []any) ([]any, structural)) {
	var after []any
	var shape structural
	s.mutate(func(container any) any {
		xs, ok := container.([]any)
		if !ok {
			panic(fmt.Errorf("delta: %s on %s, which is %s, not an array", verb, s.path(), describe(container)))
		}
		after, shape = fn(xs)
		return after
	})
	_, primary := s.t.positionsAt(s.c)
	if primary == nil {
		// The array has left the document: nothing about the tree it is no
		// longer part of needs correcting.
		return
	}
	s.t.collapsePending()
	s.t.renumber(s.c, after, shape)
	s.t.registerInserted(primary, after, shape.insertAt, shape.insertCount)
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

// normSeg is upstream's norm: a property key in JavaScript is always a
// string, and only an array coerces a canonical numeric one back to an index.
// So on an array a numeric string is the index it spells, and on an object an
// index is the key it spells — which is the key a pi replica writes.
func normSeg(seg Seg, container any) Seg {
	switch container.(type) {
	case []any:
		if k, ok := seg.(Key); ok {
			if i, ok := canonicalIndex(string(k)); ok {
				return Index(i)
			}
		}
	case map[string]any:
		if i, ok := seg.(Index); ok {
			return Key(strconv.Itoa(int(i)))
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

// same is `previous === value`: scalars by value, a container by identity.
// Assigning a member what it already holds is not a change.
//
// A Go slice is its header, so identity is the backing array it points at
// together with its length; two independently allocated empty slices are
// therefore indistinguishable, where two empty JavaScript arrays are not.
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
		return ok && len(x) == len(y) && reflect.ValueOf(x).Pointer() == reflect.ValueOf(y).Pointer()
	}
	if fa, ok := number(a); ok {
		fb, ok := number(b)
		return ok && fa == fb
	}
	return false
}

// ─── Flush: the value diff ───────────────────────────────────────────────────

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
	if hasReservedKey(before) || hasReservedKey(after) {
		d.emitSet(path, after)
		return
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

// number reads any Go numeric kind as the one JSON number it is, json.Number
// included — a UseNumber decoder yields those, and pi holds every JSON number
// in one type.
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
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
