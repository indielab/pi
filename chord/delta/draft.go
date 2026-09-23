package delta

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// ─── Drafts ──────────────────────────────────────────────────────────────────
//
// Upstream's delta/draft.ts: a revocable copy-on-write view of a revision,
// which a change's caller mutates freely and the tracker then diffs. Upstream
// hands out a Proxy per container; Go has no Proxy, so a Draft is a handle to
// the same per-container state the Proxy's handler holds, and its methods are
// the handler's traps (see doc.go for the mapping).
//
// A draft state belongs to a container IDENTITY, not a position: reading a
// member yields the one handle for that container, and writes through a held
// handle follow the container wherever array operations move it — or nowhere,
// once it has been removed. Nothing is recorded while a draft is mutated;
// finishing walks from the root, folds each touched container's copy back
// into a fresh revision that shares every untouched subtree, and the tracker
// diffs that against the base.

// transaction is upstream's DraftContext: every draft state of one change,
// keyed by the identity of the container each was created for.
type transaction struct {
	active  bool
	root    *draftState
	states  map[identity]*draftState
	created []*draftState
}

// draftState is upstream's DraftState: one container of the base (or one a
// write inserted), and the transaction's own shallow copy of it once written.
type draftState struct {
	txn   *transaction
	base  any // map[string]any or []any
	own   any // nil until the first write
	draft *Draft
	// named holds an array's named (non-index) properties, which JavaScript
	// lets a draft array carry and no revision can: while there are any, the
	// change cannot be prepared.
	named map[string]any
}

// hole is an array slot that SetLen, or a write past the end, left without a
// value — JavaScript's hole. Reading one yields nothing, and finishing a
// change whose array still has one fails, as upstream's dense check does.
type holeValue struct{}

var hole any = holeValue{}

func isHole(v any) bool {
	_, ok := v.(holeValue)
	return ok
}

func newTransaction(base any) *transaction {
	tx := &transaction{active: true, states: map[identity]*draftState{}}
	tx.root = tx.stateFor(base)
	return tx
}

// stateFor is upstream's getState: the state of container v, created on
// first sight.
func (tx *transaction) stateFor(v any) *draftState {
	id, _ := identityOf(v)
	if s := tx.states[id]; s != nil {
		return s
	}
	s := &draftState{txn: tx, base: v}
	s.draft = &Draft{s: s}
	tx.states[id] = s
	tx.created = append(tx.created, s)
	return s
}

// stateOf is the state of container v, if one was created.
func (tx *transaction) stateOf(v any) *draftState {
	id, ok := identityOf(v)
	if !ok {
		return nil
	}
	return tx.states[id]
}

// draftValue is upstream's: a container read out of a draft comes back as
// its draft, anything else as itself.
func (tx *transaction) draftValue(v any) any {
	if !isContainer(v) {
		return v
	}
	return tx.stateFor(v).draft
}

// assign is upstream's cloneAssigned / assertJsonPrimitive: a value written
// into a draft is checked and deep-copied at once, so later changes to the
// caller's value — or to the draft it was read from — do not reach it.
func (tx *transaction) assign(v any) (any, error) {
	c := cloner{rules: assignRules}
	return c.clone(v, nil)
}

// release is upstream's: the transaction ends, and every state forgets its
// containers, so a handle retained past the change retains nothing else.
func (tx *transaction) release() {
	tx.active = false
	for _, s := range tx.created {
		s.base, s.own, s.named = nil, nil, nil
	}
	tx.created, tx.states, tx.root = nil, nil, nil
}

// current is the container as the draft holds it now: the copy once there is
// one, the base until then.
func (s *draftState) current() any {
	if s.own != nil {
		return s.own
	}
	return s.base
}

// ensureCopy is upstream's: the state's own shallow copy, made on first write.
func (s *draftState) ensureCopy() any {
	if s.own == nil {
		switch b := s.base.(type) {
		case map[string]any:
			s.own = maps.Clone(b)
		case []any:
			s.own = cloneArray(b)
		}
	}
	return s.own
}

// ─── Finishing ───────────────────────────────────────────────────────────────

// finish is upstream's finish(): fold the draft into a revision, then release
// it either way.
func (tx *transaction) finish() (any, error) {
	defer tx.release()
	return tx.finalize(tx.root, map[*draftState]bool{}, map[*draftState]any{})
}

// finalize is upstream's: a state whose copy still equals its base drops the
// copy; each member container with a state of its own is finalized in turn
// and, if it changed, written into this container's copy (made now if the
// container itself was not written). An array that was written must be dense.
func (tx *transaction) finalize(s *draftState, finalizing map[*draftState]bool, finalized map[*draftState]any) (any, error) {
	if v, ok := finalized[s]; ok {
		return v, nil
	}
	if finalizing[s] {
		return nil, errors.New("delta: Cyclic draft state is not supported (a draft container reaches itself)")
	}
	finalizing[s] = true
	defer delete(finalizing, s)

	if s.own != nil && len(s.named) == 0 && shallowEqual(s.base, s.own) {
		s.own = nil
	}
	current := s.current()
	result := current
	writable := s.own != nil
	member := func(v any) (any, bool, error) {
		if !isContainer(v) {
			return nil, false, nil
		}
		c := tx.stateOf(v)
		if c == nil {
			return nil, false, nil
		}
		f, err := tx.finalize(c, finalizing, finalized)
		if err != nil || same(f, v) {
			return nil, false, err
		}
		return f, true, nil
	}
	switch c := current.(type) {
	case map[string]any:
		for k, v := range c {
			f, changed, err := member(v)
			if err != nil {
				return nil, err
			}
			if !changed {
				continue
			}
			if !writable {
				result, writable = maps.Clone(c), true
			}
			result.(map[string]any)[k] = f
		}
	case []any:
		for i, v := range c {
			f, changed, err := member(v)
			if err != nil {
				return nil, err
			}
			if !changed {
				continue
			}
			if !writable {
				result, writable = cloneArray(c), true
			}
			result.([]any)[i] = f
		}
		if s.own != nil && (len(s.named) > 0 || slices.ContainsFunc(c, isHole)) {
			return nil, &ValueError{Message: assignRules.dense}
		}
	}
	finalized[s] = result
	return result, nil
}

// shallowEqual is upstream's: the same kind, the same members, each Object.is
// its counterpart.
func shallowEqual(left, right any) bool {
	switch l := left.(type) {
	case map[string]any:
		r, ok := right.(map[string]any)
		if !ok || len(l) != len(r) {
			return false
		}
		for k, v := range l {
			w, ok := r[k]
			if !ok || !objectIs(v, w) {
				return false
			}
		}
		return true
	case []any:
		r, ok := right.([]any)
		if !ok || len(l) != len(r) {
			return false
		}
		for i, v := range l {
			if isHole(r[i]) || !objectIs(v, r[i]) {
				return false
			}
		}
		return true
	}
	return false
}

// ─── Draft ───────────────────────────────────────────────────────────────────

// Draft is a mutable view of one object or array inside a change: the Go form
// of upstream's Draft<T> proxy. Change.State is the root; Get and At hand out
// the drafts of member containers, one handle per container, so a handle may
// be held and compared.
//
// Writes are copy-on-write against the committed revision, which they never
// touch, and every value written is checked and deep-copied at once: a later
// change to the caller's value, or to a draft it was read from, does not
// reach it. A handle follows its container through array reordering and
// insertion; once the container is removed from the tree, writes through the
// handle are dropped.
//
// Keys are given as any: a string is an object key, an int (or any integral Go
// number) an array index, and a Key or Index is taken as is. On an array a
// canonical numeric string is the index it spells; on an object an index is
// the key it spells — as JavaScript coerces both.
//
// A draft is valid until its change is prepared or aborted. After that a write
// returns ErrDraftRevoked and a read panics with it: a read has no error to
// return, and using a settled draft is a bug in the caller, like a send on a
// closed channel. A nil *Draft — what At returns for a member that is not a
// container — reads as empty, and every write to it returns an error; so does
// a zero Draft, which no change handed out.
//
// A Draft is not safe for concurrent use.
type Draft struct{ s *draftState }

var (
	// ErrDraftRevoked is returned by a write through a draft whose change was
	// already prepared or aborted, and is the value a read through one panics
	// with.
	ErrDraftRevoked = errors.New("delta: Cannot use a draft outside its change callback (its change was prepared or aborted; begin a new change and read the draft from its State)")
	errNilDraft     = errors.New("delta: no draft here: the member is absent or not an object or array (check At's result, or Set the member first)")
)

// live is the state behind a draft that may be written, or why it may not.
func (d *Draft) live() (*draftState, error) {
	if d == nil || d.s == nil {
		return nil, errNilDraft
	}
	if !d.s.txn.active {
		return nil, ErrDraftRevoked
	}
	return d.s, nil
}

// read is the state behind a draft being read: nil for a nil or zero draft; a
// panic for a revoked one.
func (d *Draft) read() *draftState {
	if d == nil || d.s == nil {
		return nil
	}
	if !d.s.txn.active {
		panic(ErrDraftRevoked)
	}
	return d.s
}

// IsArray reports whether the draft is an array.
func (d *Draft) IsArray() bool {
	s := d.read()
	if s == nil {
		return false
	}
	_, ok := s.current().([]any)
	return ok
}

// Len is an array's length or an object's member count.
func (d *Draft) Len() int {
	s := d.read()
	if s == nil {
		return 0
	}
	switch c := s.current().(type) {
	case []any:
		return len(c)
	case map[string]any:
		return len(c)
	}
	return 0
}

// Keys is Object.keys: an object's keys, integer-like first; or an array's
// indices that hold a value, as strings.
func (d *Draft) Keys() []string {
	s := d.read()
	if s == nil {
		return nil
	}
	switch c := s.current().(type) {
	case map[string]any:
		return keyOrder(c)
	case []any:
		keys := make([]string, 0, len(c)+len(s.named))
		for i, v := range c {
			if !isHole(v) {
				keys = append(keys, Index(i).String())
			}
		}
		return append(keys, keyOrder(s.named)...)
	}
	return nil
}

// Has reports whether the member exists: an own property, or an array index
// that holds a value ("length" included, as JavaScript's `in` has it).
func (d *Draft) Has(key any) bool {
	_, ok := d.Get(key)
	return ok
}

// Get is the member's value and whether it exists: a *Draft for an object or
// array, the value itself otherwise. An array's "length" is its length.
func (d *Draft) Get(key any) (any, bool) {
	s := d.read()
	if s == nil {
		return nil, false
	}
	seg, err := parseSeg(key)
	if err != nil {
		return nil, false
	}
	switch c := s.current().(type) {
	case map[string]any:
		v, ok := c[propertyKey(seg)]
		if !ok {
			return nil, false
		}
		return s.txn.draftValue(v), true
	case []any:
		if k, ok := seg.(Key); ok && k == "length" {
			return float64(len(c)), true
		}
		i, ok := arrayIndexOf(seg)
		if !ok {
			v, ok := s.named[propertyKey(seg)]
			if !ok {
				return nil, false
			}
			return s.txn.draftValue(v), true
		}
		if i >= len(c) || isHole(c[i]) {
			return nil, false
		}
		return s.txn.draftValue(c[i]), true
	}
	return nil, false
}

// At is the draft of a member object or array, or nil when the member is
// absent or a scalar. Writes to a nil draft return an error, so a chain like
// state.At("a").At("b").Set("c", 1) reports a missing link rather than
// panicking.
func (d *Draft) At(key any) *Draft {
	v, _ := d.Get(key)
	child, _ := v.(*Draft)
	return child
}

// arrayIndexOf is the index seg names on an array: an Index, or a Key that
// spells one canonically. false for anything else — a named property, which
// a JSON array cannot hold.
func arrayIndexOf(seg Seg) (int, bool) {
	switch s := seg.(type) {
	case Index:
		if s >= 0 && s <= maxArrayIndex {
			return int(s), true
		}
	case Key:
		return canonicalIndex(string(s))
	}
	return 0, false
}

var errArrayHole = errors.New("delta: Draft arrays cannot contain holes (remove array elements with Splice, Pop or Shift, not Delete)")

func errArrayLength(n any) error {
	return fmt.Errorf("delta: Invalid array length: %v (a length is an integer from 0 to 4294967295)", n)
}

// Set assigns a member: an object property, or an array element — an existing
// one, the slot one past the end, or any later slot, which leaves holes the
// change must fill before it is prepared. nil is JSON null; a member is removed
// with Delete. Assigning a scalar member the value it already has changes
// nothing; a container is always copied in, and assigning a *Draft copies what
// that draft holds now.
//
// On an array, a key that is not an index names a property, as it does in
// JavaScript: the array holds it, Get reads it back, and — since a JSON array
// has no such thing and an array's properties cannot be deleted — the change
// can no longer be prepared. "length" is SetLen.
func (d *Draft) Set(key any, value any) error {
	s, err := d.live()
	if err != nil {
		return err
	}
	seg, err := parseSeg(key)
	if err != nil {
		return err
	}
	stored, err := s.txn.assign(value)
	if err != nil {
		return err
	}
	switch c := s.current().(type) {
	case map[string]any:
		k := propertyKey(seg)
		if previous, ok := c[k]; ok && objectIs(previous, stored) {
			return nil
		}
		s.ensureCopy().(map[string]any)[k] = stored
	case []any:
		if k, ok := seg.(Key); ok && k == "length" {
			n, ok := number(stored)
			if !ok {
				return errArrayLength(stored)
			}
			return d.setLen(s, n)
		}
		i, ok := arrayIndexOf(seg)
		if !ok {
			// A named property: JavaScript holds it on the array, and no revision
			// can, so the change can no longer be prepared (and an array's
			// properties cannot be deleted).
			k := propertyKey(seg)
			if previous, ok := s.named[k]; ok && objectIs(previous, stored) {
				return nil
			}
			s.ensureCopy()
			if s.named == nil {
				s.named = map[string]any{}
			}
			s.named[k] = stored
			return nil
		}
		if i < len(c) && !isHole(c[i]) && objectIs(c[i], stored) {
			return nil
		}
		xs := s.ensureCopy().([]any)
		for len(xs) < i {
			xs = append(xs, hole)
		}
		if i == len(xs) {
			xs = append(xs, stored)
		} else {
			xs[i] = stored
		}
		s.own = xs
	}
	return nil
}

// Delete removes an object property; deleting one that is absent does
// nothing. An array element cannot be deleted — that would leave a hole — so
// every Delete on an array fails; use Splice, Pop or Shift.
func (d *Draft) Delete(key any) error {
	s, err := d.live()
	if err != nil {
		return err
	}
	seg, err := parseSeg(key)
	if err != nil {
		return err
	}
	switch c := s.current().(type) {
	case []any:
		return errArrayHole
	case map[string]any:
		k := propertyKey(seg)
		if _, ok := c[k]; !ok {
			return nil
		}
		delete(s.ensureCopy().(map[string]any), k)
	}
	return nil
}

// SetLen is `array.length = n`: it truncates, or grows with holes that must
// be filled before the change is prepared.
func (d *Draft) SetLen(n int) error {
	s, err := d.live()
	if err != nil {
		return err
	}
	if _, ok := s.current().([]any); !ok {
		return errObjectMethod("SetLen")
	}
	return d.setLen(s, float64(n))
}

func (d *Draft) setLen(s *draftState, n float64) error {
	c := s.current().([]any)
	if n == float64(len(c)) {
		return nil
	}
	xs := s.ensureCopy().([]any)
	if n < 0 || n > maxArrayIndex+1 || n != float64(int(n)) {
		return errArrayLength(jsNumber(n))
	}
	length := int(n)
	if length < len(xs) {
		clear(xs[length:])
		s.own = xs[:length]
		return nil
	}
	for len(xs) < length {
		xs = append(xs, hole)
	}
	s.own = xs
	return nil
}

func errObjectMethod(method string) error {
	return fmt.Errorf("delta: %s on an object draft (array methods apply to array drafts only)", method)
}

// array is the start of every array method: upstream's wrapped mutator, which
// makes the copy it mutates.
func (d *Draft) array(method string) (*draftState, []any, error) {
	s, err := d.live()
	if err != nil {
		return nil, nil, err
	}
	if _, ok := s.current().([]any); !ok {
		return nil, nil, errObjectMethod(method)
	}
	return s, s.ensureCopy().([]any), nil
}

// assignAll checks and copies every item before any is written.
func (tx *transaction) assignAll(items []any) ([]any, error) {
	out := make([]any, len(items))
	for i, item := range items {
		v, err := tx.assign(item)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// Push appends items and returns the new length.
func (d *Draft) Push(items ...any) (int, error) {
	s, xs, err := d.array("Push")
	if err != nil {
		return 0, err
	}
	stored, err := s.txn.assignAll(items)
	if err != nil {
		return 0, err
	}
	s.own = append(xs, stored...)
	return len(xs) + len(stored), nil
}

// Pop removes and returns the last element — as a *Draft when it is a
// container. ok is false when the array is empty (or the slot was a hole).
func (d *Draft) Pop() (value any, ok bool, err error) {
	s, xs, err := d.array("Pop")
	if err != nil || len(xs) == 0 {
		return nil, false, err
	}
	last := xs[len(xs)-1]
	xs[len(xs)-1] = nil
	s.own = xs[:len(xs)-1]
	if isHole(last) {
		return nil, false, nil
	}
	return s.txn.draftValue(last), true, nil
}

// Shift removes and returns the first element — as a *Draft when it is a
// container. ok is false when the array is empty (or the slot was a hole).
func (d *Draft) Shift() (value any, ok bool, err error) {
	s, xs, err := d.array("Shift")
	if err != nil || len(xs) == 0 {
		return nil, false, err
	}
	first := xs[0]
	s.own = slices.Delete(xs, 0, 1)
	if isHole(first) {
		return nil, false, nil
	}
	return s.txn.draftValue(first), true, nil
}

// Unshift inserts items at the front and returns the new length.
func (d *Draft) Unshift(items ...any) (int, error) {
	s, xs, err := d.array("Unshift")
	if err != nil {
		return 0, err
	}
	stored, err := s.txn.assignAll(items)
	if err != nil {
		return 0, err
	}
	s.own = slices.Insert(xs, 0, stored...)
	return len(xs) + len(stored), nil
}

// Splice is Array.prototype.splice(start, deleteCount, ...items): it removes
// deleteCount elements at start and inserts items there, returning the
// removed elements (containers as drafts, detached from the tree). A negative
// start counts from the end; both bounds are clamped to the array.
func (d *Draft) Splice(start, deleteCount int, items ...any) ([]any, error) {
	s, xs, err := d.array("Splice")
	if err != nil {
		return nil, err
	}
	start = relativeIndex(start, len(xs))
	remove := min(max(deleteCount, 0), len(xs)-start)
	stored, err := s.txn.assignAll(items)
	if err != nil {
		return nil, err
	}
	removed := make([]any, remove)
	for i, v := range xs[start : start+remove] {
		if !isHole(v) {
			removed[i] = s.txn.draftValue(v)
		}
	}
	s.own = splice(xs, start, remove, stored)
	return removed, nil
}

// relativeIndex is JavaScript's relative-index rule: negative counts from the
// end, and both directions clamp to [0, length].
func relativeIndex(i, length int) int {
	if i < 0 {
		return max(0, length+i)
	}
	return min(i, length)
}

// Sort sorts the array in place, stably. cmp receives the elements as Get
// returns them — containers as drafts, which it may read and even write. A nil
// cmp is JavaScript's default order: by each element's string form, in UTF-16
// code units. Holes sort last.
func (d *Draft) Sort(cmp func(a, b any) int) error {
	s, xs, err := d.array("Sort")
	if err != nil {
		return err
	}
	if cmp == nil {
		cmp = func(a, b any) int { return compareUTF16(s.txn.jsString(a), s.txn.jsString(b)) }
	}
	present := slices.DeleteFunc(slices.Clone(xs), isHole)
	slices.SortStableFunc(present, func(a, b any) int {
		return cmp(s.txn.draftValue(a), s.txn.draftValue(b))
	})
	n := copy(xs, present)
	for i := n; i < len(xs); i++ {
		xs[i] = hole
	}
	return nil
}

// jsString is String(v) for a value read out of a draft: an array joins its
// elements' strings with commas (null and holes as ""), an object is
// "[object Object]".
func (tx *transaction) jsString(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case string:
		return x
	case *Draft:
		switch c := x.s.current().(type) {
		case []any:
			parts := make([]string, len(c))
			for i, item := range c {
				if item != nil && !isHole(item) {
					parts[i] = tx.jsString(tx.draftValue(item))
				}
			}
			return strings.Join(parts, ",")
		}
		return "[object Object]"
	}
	if f, ok := number(v); ok {
		return jsNumber(f)
	}
	return fmt.Sprint(v)
}

// Reverse reverses the array in place.
func (d *Draft) Reverse() error {
	_, xs, err := d.array("Reverse")
	if err != nil {
		return err
	}
	slices.Reverse(xs)
	return nil
}

// Fill is Array.prototype.fill(value, start, end): each slot in [start, end)
// gets its own copy of value. Negative bounds count from the end; both clamp.
// Pass Len() as end to fill to the end.
func (d *Draft) Fill(value any, start, end int) error {
	s, xs, err := d.array("Fill")
	if err != nil {
		return err
	}
	from, to := relativeIndex(start, len(xs)), relativeIndex(end, len(xs))
	for i := from; i < to; i++ {
		stored, err := s.txn.assign(value)
		if err != nil {
			return err
		}
		xs[i] = stored
	}
	return nil
}

// CopyWithin is Array.prototype.copyWithin(target, start, end): each element
// of [start, end) is copied — as the draft holds it now — to the slot at the
// same offset from target. Negative bounds count from the end; all clamp.
// Pass Len() as end to copy to the end.
func (d *Draft) CopyWithin(target, start, end int) error {
	s, xs, err := d.array("CopyWithin")
	if err != nil {
		return err
	}
	to := relativeIndex(target, len(xs))
	from, final := relativeIndex(start, len(xs)), relativeIndex(end, len(xs))
	count := min(max(final-from, 0), len(xs)-to)
	source := slices.Clone(xs[from : from+count])
	c := cloner{rules: assignRules}
	for offset, item := range source {
		if isHole(item) {
			return &ValueError{Message: undefinedMessage}
		}
		stored, err := c.clone(item, s.txn)
		if err != nil {
			return err
		}
		xs[to+offset] = stored
	}
	return nil
}
