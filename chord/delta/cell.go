package delta

import (
	"reflect"
	"slices"
)

// ─── Cells: where a cursor points ────────────────────────────────────────────
//
// A cursor does not bake its path. It holds a cell — a parent link plus one
// segment — and the path is walked at use time, so renumbering one element
// corrects every descendant of it. A reference taken from tracked state
// therefore stays correct across a structural mutation of the array holding
// it, and goes quiet when the element it points at leaves the document.
//
// One container can occupy several positions in the tree. A cell is one
// position; a write emits an op per live position, which is what a diff
// against a published baseline produced for the same shape.
//
// Paths are cached per cell against a counter only structural mutation bumps,
// and the per-write position lookup is skipped until a document actually
// aliases something.

// cell is one position of one container in the tracked tree. The root's cell
// has no parent and contributes no segment.
type cell struct {
	parent *cell
	seg    Seg
	dead   bool
	// target is the container this cell addressed when it was taken. It is
	// what a write through a cell that left the document mutates, and what
	// tells a reuse of the cell from a new container at the same index.
	target any

	at     uint64 // the shape counter pathOf cached against
	cached Path

	// kids is the reuse cache for child cells, keyed by segment and dropped
	// whenever a structural mutation renumbers them. held is the durable list
	// renumbering walks: two shifting children can collide on one cache key,
	// and the evicted one would silently stop tracking. Only an array's cell
	// fills held — nothing renumbers an object's members.
	kids map[Seg]*kid
	held []*kid
}

// kid is one remembered child: the container that was there, and its cell.
type kid struct {
	target any
	cell   *cell
}

// detached reports whether this position has left the document — itself or
// any ancestor removed.
func (c *cell) detached() bool {
	for at := c; at != nil; at = at.parent {
		if at.dead {
			return true
		}
	}
	return false
}

// pathOf is the cell's path from the root. The walk allocates, and writes are
// far more frequent than renumbers, so the answer is cached until the next
// structural mutation.
func (t *tracker) pathOf(c *cell) Path {
	if c.cached != nil && c.at == t.shape {
		return c.cached
	}
	depth := 0
	for at := c; at.parent != nil; at = at.parent {
		depth++
	}
	out := make(Path, depth)
	for at := c; at.parent != nil; at = at.parent {
		depth--
		out[depth] = at.seg
	}
	c.at, c.cached = t.shape, out
	return out
}

// childCell is the cell for one member of the container at parent, reusing the
// one already taken there when it still addresses the same container.
//
// A member that is not a container gets a bare cell: there is nothing to hold
// a position of, and pi returns the raw value rather than a proxy. So does a
// member reached through a reserved key, which pi gives a wrapper of its own
// precisely so the guard cannot be bypassed through a safe alias.
func (t *tracker) childCell(parent *cell, seg Seg, value any, inArray, blocked bool) *cell {
	if blocked || !isContainer(value) {
		return &cell{parent: parent, seg: seg, target: value}
	}
	if k, ok := parent.kids[seg]; ok && !k.cell.dead && same(k.target, value) {
		return k.cell
	}
	c := &cell{parent: parent, seg: seg, target: value}
	k := &kid{target: value, cell: c}
	if parent.kids == nil {
		parent.kids = map[Seg]*kid{}
	}
	parent.kids[seg] = k
	if inArray {
		parent.held = append(parent.held, k)
	}
	t.register(value, c)
	return c
}

// refreshTarget updates the container remembered at c after a mutation
// re-headered it. A Go slice is a header, so an append replaces the value at
// the position; a JavaScript array's identity never changes, so pi has
// nothing to do here.
func (t *tracker) refreshTarget(c *cell, value any) {
	c.target = value
	if c.parent == nil {
		return
	}
	if k, ok := c.parent.kids[c.seg]; ok && k.cell == c {
		k.target = value
		return
	}
	for _, k := range c.parent.held {
		if k.cell == c {
			k.target = value
			return
		}
	}
}

// structural describes what an array mutator did to the array's shape.
type structural struct {
	// shifts is false for an append, which moves nothing. permutes is true
	// for sort, reverse, fill and copyWithin, which move elements without a
	// uniform shift, so held children are relocated by identity instead.
	shifts, permutes bool
	// index, remove and insert are the shift: elements in [index, index+remove)
	// are gone and insert took their place.
	index, remove, insert int
	// insertAt and insertCount are the range the mutator wrote, which is where
	// a value that already lives elsewhere can have arrived.
	insertAt, insertCount int
}

// renumber shifts the cells held through the array at c so a reference taken
// before the mutation keeps addressing its own element. Children whose element
// was removed are marked dead and record nothing afterwards.
func (t *tracker) renumber(c *cell, xs []any, s structural) {
	t.shape++ // every cached path below here is now suspect
	if !s.shifts && !s.permutes {
		return
	}
	if len(c.held) == 0 { // nobody took a reference
		return
	}
	kept := c.held[:0]
	if s.permutes {
		// sort/reverse/fill/copyWithin permute rather than shift: locate each
		// held child by identity. Held children are few and these are rare.
		for _, k := range c.held {
			at := indexOfRef(xs, k.target)
			if at < 0 {
				k.cell.dead = true
				continue
			}
			k.cell.seg = Index(at)
			kept = append(kept, k)
		}
		c.held = kept
		c.kids = nil
		return
	}
	delta := s.insert - s.remove
	for _, k := range c.held {
		i, ok := k.cell.seg.(Index)
		switch {
		case !ok:
		case int(i) >= s.index && int(i) < s.index+s.remove:
			k.cell.dead = true
			continue
		case int(i) >= s.index+s.remove:
			k.cell.seg = Index(int(i) + delta)
		}
		kept = append(kept, k)
	}
	c.held = kept
	c.kids = nil // the cache is keyed by index; rebuild it lazily
}

// indexOfRef is Array.prototype.indexOf under ===: the first element that is
// the same container as target.
func indexOfRef(xs []any, target any) int {
	for i, v := range xs {
		if same(v, target) {
			return i
		}
	}
	return -1
}

// ─── Positions: one container, several paths ─────────────────────────────────

// positionSet is the known positions of one tracked container. target is held
// so the address keying the set can never be reused by another container.
type positionSet struct {
	target any
	cells  []*cell
}

// containerRef identifies a tracked container. Only an object qualifies: a Go
// slice is a header, so "the same array at two positions" has no Go meaning —
// an append through one position re-headers it and the two stop being the
// same array (D62).
func containerRef(v any) (uintptr, bool) {
	if m, ok := v.(map[string]any); ok && m != nil {
		return reflect.ValueOf(m).Pointer(), true
	}
	return 0, false
}

func (t *tracker) positionsOf(v any) *positionSet {
	ref, ok := containerRef(v)
	if !ok {
		return nil
	}
	return t.positions[ref]
}

// register remembers c as a position of the container it addresses. A
// container is registered when a cursor first reaches it, which is what lets
// a later assignment of that container recognise it as already tracked.
func (t *tracker) register(v any, c *cell) {
	ref, ok := containerRef(v)
	if !ok {
		return
	}
	set := t.positions[ref]
	if set == nil {
		t.prunePositions()
		if t.positions == nil {
			t.positions = map[uintptr]*positionSet{}
		}
		set = &positionSet{target: v}
		t.positions[ref] = set
	}
	set.cells = append(set.cells, c)
}

// alias records a second position for a container already tracked elsewhere,
// so a later write through it emits an op for both.
func (t *tracker) alias(set *positionSet, c *cell) {
	set.cells = append(set.cells, c)
	t.aliased = true
}

// prunePositions drops positions that have left the document. The registry
// holds its containers so their addresses stay unique, so without this a
// long-lived tracker over a churning tree would retain every container a
// cursor ever visited. pi needs no counterpart: its registry is a WeakMap.
func (t *tracker) prunePositions() {
	if len(t.positions) < max(64, 2*t.positionsPruned) {
		return
	}
	for ref, set := range t.positions {
		kept := set.cells[:0]
		for _, c := range set.cells {
			if c.detached() {
				continue
			}
			if at, ok := resolveValue(t.root, t.pathOf(c)); ok && same(at, set.target) {
				kept = append(kept, c)
			}
		}
		set.cells = kept
		if len(kept) == 0 {
			delete(t.positions, ref)
		}
	}
	t.positionsPruned = len(t.positions)
}

// positionsAt is the live positions of the container at c, primary first.
//
// The common case — no document has aliased anything — is the cursor's own
// cell and needs no slice, which is why it is reported separately. A nil
// primary means the container has no position left: a write through it
// mutates and records nothing, as a plain JavaScript write through a removed
// element does.
func (t *tracker) positionsAt(c *cell) (many []*cell, primary *cell) {
	set := (*positionSet)(nil)
	if t.aliased {
		set = t.positionsOf(c.target)
	}
	if set == nil {
		if c.detached() {
			return nil, nil
		}
		return nil, c
	}
	live := make([]*cell, 0, len(set.cells))
	for _, pc := range set.cells {
		if !pc.detached() {
			live = append(live, pc)
		}
	}
	switch len(live) {
	case 0:
		return nil, nil
	case 1:
		return nil, live[0]
	}
	return live, live[0]
}

// registerInserted gives a new position to each value an array mutator
// inserted that already lives somewhere in the document. Only the inserted
// range is examined: scanning the array would make every structural mutation
// O(n).
func (t *tracker) registerInserted(parent *cell, xs []any, at, count int) {
	for i := at; count > 0 && i >= 0 && i < at+count && i < len(xs); i++ {
		item := xs[i]
		set := t.positionsOf(item)
		if set == nil || same(item, parent.target) {
			continue
		}
		seen, anyLive := false, false
		for _, c := range set.cells {
			if c.dead {
				continue
			}
			anyLive = true
			if c.parent == parent && c.seg == Index(i) {
				seen = true
			}
		}
		if !seen && anyLive {
			t.alias(set, &cell{parent: parent, seg: Index(i), target: item})
		}
	}
}

// rebase is op addressing path instead of its own.
//
// The payload is shared between the copies, as upstream's shallow `[...op]`
// shares it: the positions hold one and the same container, so a later write
// folded into one copy's payload belongs in the other's too. A consumer that
// adopts the batch therefore gets one container at both paths, which is what
// pi's consumer gets; a serialized batch is detached either way.
func rebase(op Op, path Path) Op {
	switch op := op.(type) {
	case Set:
		op.Path = path
		return op
	case Delete:
		op.Path = path
		return op
	case Append:
		op.Path = path
		return op
	case Truncate:
		op.Path = path
		return op
	case Splice:
		op.Path = path
		return op
	}
	return op
}

// emit records op for every live position of the container at the cursor.
// A cursor whose container has no position left records nothing.
func (s State) emit(op Op) {
	many, primary := s.t.positionsAt(s.c)
	if primary == nil {
		return
	}
	path := opPath(op)
	if many == nil || path == nil {
		// One position, or a root replacement, which addresses the root rather
		// than any position of anything.
		s.t.record(op)
		return
	}
	rest := path[len(s.t.pathOf(primary)):]
	for _, c := range many {
		s.t.record(rebase(op, slices.Concat(s.t.pathOf(c), rest)))
	}
}

// writePath is the path a write through this cursor addresses: the cursor's
// own position, or — once a document aliases something — the first live
// position of its container, which is the path pi's shared proxy uses.
func (s State) writePath() Path {
	if _, primary := s.t.positionsAt(s.c); primary != nil {
		return s.t.pathOf(primary)
	}
	return s.path()
}
