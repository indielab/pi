package delta

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"unicode"
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
	// fresh holds the arrays this change created — every array a write copied
	// in — which upstream's context.owned marks as the transaction's own: see
	// finalize for what that changes.
	fresh map[identity]bool
	// committed is the first error the store's commit would raise for a fresh
	// array, which upstream reports only once finalize has walked the tree.
	committed error
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
	tx := &transaction{active: true, states: map[identity]*draftState{}, fresh: map[identity]bool{}}
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
	c := tx.cloner()
	return c.clone(v, nil)
}

// cloner is the copy a draft write makes: assignRules, and every array it
// allocates recorded as fresh.
func (tx *transaction) cloner() cloner {
	return cloner{rules: assignRules, fresh: tx.fresh}
}

// release is upstream's: the transaction ends, and every state forgets its
// containers, so a handle retained past the change retains nothing else.
func (tx *transaction) release() {
	tx.active = false
	for _, s := range tx.created {
		s.base, s.own, s.named = nil, nil, nil
	}
	tx.created, tx.states, tx.root, tx.fresh = nil, nil, nil, nil
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

// finish is upstream's finish() and the store's commit: fold the draft into a
// revision, then release it either way.
func (tx *transaction) finish() (any, error) {
	defer tx.release()
	v, err := tx.finalize(tx.root, map[*draftState]bool{}, map[*draftState]any{})
	if err == nil {
		err = tx.committed
	}
	if err != nil {
		return nil, err
	}
	return v, nil
}

// finalize is upstream's: a state whose copy still equals its base drops the
// copy; each member container with a state of its own is finalized in turn
// (an object's in keyOrder) and, if it changed, written into this container's
// copy (made now if the container itself was not written). An array that was
// written must be dense.
//
// Which check reports a sparse array depends on where it came from. An array
// of the revision is checked here, after its members, with draft.ts's text.
// An array this change created is the transaction's own, which upstream
// writes in place: its copy IS its base, so finalize drops it as unchanged and
// never checks it, and the store's commit does — after finalize has walked
// the whole tree, parent before child, with value.ts's text. That check is
// recorded here in the commit's order, and finish reports it.
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
	xs, isArray := current.([]any)
	fresh := false
	if isArray && writable {
		id, _ := identityOf(s.base)
		fresh = tx.fresh[id]
	}
	if fresh && tx.committed == nil {
		tx.committed = denseError(xs, s.named, importRules)
	}
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
		for _, k := range keyOrder(c) {
			f, changed, err := member(c[k])
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
		if s.own != nil && !fresh {
			if err := denseError(xs, s.named, assignRules); err != nil {
				return nil, err
			}
		}
	}
	finalized[s] = result
	return result, nil
}

// checkDense is denseError for the array a state holds now; nil for an
// object.
func (s *draftState) checkDense(rules cloneRules) error {
	xs, ok := s.current().([]any)
	if !ok {
		return nil
	}
	return denseError(xs, s.named, rules)
}

// denseError is upstream's assertDenseArray over an array a draft holds: its
// own keys — the indices that hold a value, "length" and any named property —
// must number exactly its length plus one, or it is not "dense and contain
// only indexed entries"; then no index may be empty, or it does not "contain
// enumerable indexed data properties with defined values". The second text
// needs as many named properties as holes.
func denseError(xs []any, named map[string]any, rules cloneRules) error {
	holes := 0
	for _, v := range xs {
		if isHole(v) {
			holes++
		}
	}
	switch {
	case len(named) != holes:
		return &ValueError{Message: rules.dense}
	case holes > 0:
		return &ValueError{Message: rules.defined}
	}
	return nil
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

// errArrayLength is upstream's RangeError for a length that is not an integer
// from 0 to 2^32 - 1 once JavaScript has converted it.
func errArrayLength(v any) error {
	return fmt.Errorf("delta: Invalid array length: %s (a length is an integer from 0 to 4294967295, or a value JavaScript's Number() makes one)", describe(v))
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
// can no longer be prepared. "length" is SetLen, with value converted as
// JavaScript converts an assigned length: null is 0, a boolean 0 or 1, a
// string the number it spells, an array the number its elements' string
// spells ([2] is 2).
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
			// Reflect.set(copy, "length", stored): ArraySetLength, which takes
			// ToNumber of the value.
			n, err := jsToNumber(stored)
			if err != nil {
				return err
			}
			return d.setLen(s, n, stored)
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
	return d.setLen(s, float64(n), n)
}

// setLen is ArraySetLength with the length already converted to n; v is the
// value as assigned, for the error.
func (d *Draft) setLen(s *draftState, n float64, v any) error {
	c := s.current().([]any)
	if n == float64(len(c)) {
		return nil
	}
	xs := s.ensureCopy().([]any)
	if !(n >= 0 && n <= maxArrayIndex+1 && n == math.Trunc(n)) {
		return errArrayLength(v)
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
// cmp is JavaScript's default order: by each element's String(), in UTF-16
// code units; it fails, leaving the array as it was, when an element has no
// string form (an object with a "toString" member — see jsString). Holes sort
// last.
func (d *Draft) Sort(cmp func(a, b any) int) error {
	s, xs, err := d.array("Sort")
	if err != nil {
		return err
	}
	present := slices.DeleteFunc(slices.Clone(xs), isHole)
	if cmp == nil {
		if err := s.txn.sortByString(present); err != nil {
			return err
		}
	} else {
		slices.SortStableFunc(present, func(a, b any) int {
			return cmp(s.txn.draftValue(a), s.txn.draftValue(b))
		})
	}
	n := copy(xs, present)
	for i := n; i < len(xs); i++ {
		xs[i] = hole
	}
	return nil
}

// sortByString is the default sort order. Each element's String() is taken
// once, before anything moves, so an element without one fails the sort with
// the array untouched, as in V8, which sorts a copy and writes it back only
// when the sort completes. A single element is never compared, so it never
// fails.
func (tx *transaction) sortByString(items []any) error {
	if len(items) < 2 {
		return nil
	}
	type keyed struct {
		key  string
		item any
	}
	keys := make([]keyed, len(items))
	for i, item := range items {
		key, err := jsString(tx.draftValue(item), tx)
		if err != nil {
			return err
		}
		keys[i] = keyed{key, item}
	}
	slices.SortStableFunc(keys, func(a, b keyed) int { return compareUTF16(a.key, b.key) })
	for i, k := range keys {
		items[i] = k.item
	}
	return nil
}

// errNoPrimitive is the TypeError JavaScript's ToPrimitive throws for a JSON
// object with an own "toString" member.
var errNoPrimitive = errors.New(`delta: Cannot convert object to primitive value (an object with a "toString" member, or an array draft with a "toString" property, has no string form in JavaScript, because the member is not a function; rename the member, or sort with a comparator)`)

// jsString is String(v) — ToPrimitive, then ToString — for a JSON value, the
// conversion JavaScript's default sort order and an assigned array length
// apply. A container read through a draft (a *Draft, or a member of one when
// through is its transaction) is converted as the draft holds it now.
func jsString(v any, through *transaction) (string, error) {
	switch x := v.(type) {
	case nil:
		return "null", nil
	case bool:
		return strconv.FormatBool(x), nil
	case string:
		return x, nil
	case *Draft:
		return containerString(x.s.current(), x.s.named, x.s.txn)
	case map[string]any, []any:
		if through != nil {
			// Upstream's join reads each element through the proxy's get trap,
			// which makes it a draft.
			s := through.stateFor(x)
			return containerString(s.current(), s.named, through)
		}
		return containerString(x, nil, nil)
	}
	f, _ := number(v)
	return jsNumber(f), nil
}

// containerString is String() of an object or array. Neither has a primitive
// value of its own: valueOf is Object.prototype's, which returns the object,
// so the string is what toString makes of it — "[object Object]", or an
// array's elements' strings joined with commas, null and holes as "". A JSON
// value can only shadow those methods with members that are not functions:
// an own "toString" leaves no conversion at all (TypeError), and an array's
// named "join" sends Array.prototype.toString to Object.prototype.toString.
func containerString(c any, named map[string]any, through *transaction) (string, error) {
	switch x := c.(type) {
	case map[string]any:
		if _, ok := x["toString"]; ok {
			return "", errNoPrimitive
		}
		return "[object Object]", nil
	case []any:
		if _, ok := named["toString"]; ok {
			return "", errNoPrimitive
		}
		if _, ok := named["join"]; ok {
			return "[object Array]", nil
		}
		var b strings.Builder
		for i, item := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if item == nil || isHole(item) {
				continue
			}
			s, err := jsString(item, through)
			if err != nil {
				return "", err
			}
			b.WriteString(s)
		}
		return b.String(), nil
	}
	return "", nil
}

// jsToNumber is ToNumber(v) for a JSON value: null 0, booleans 0 and 1, a
// string by StringToNumber, and a container by the number its string spells
// (ToPrimitive with hint number tries valueOf first, which never yields a
// primitive for JSON, then toString).
func jsToNumber(v any) (float64, error) {
	switch x := v.(type) {
	case nil:
		return 0, nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case string:
		return stringToNumber(x), nil
	case map[string]any, []any:
		s, err := jsString(x, nil)
		if err != nil {
			return 0, err
		}
		return stringToNumber(s), nil
	}
	if f, ok := number(v); ok {
		return f, nil
	}
	return math.NaN(), nil
}

// stringToNumber is JavaScript's StringToNumber: JavaScript's whitespace and
// line terminators trimmed, "" as 0, then an unsigned 0x, 0o or 0b integer, or
// a decimal literal with an optional sign ("Infinity" included). Anything
// else is NaN; so is every spelling Go's ParseFloat reads and JavaScript does
// not ("inf", "NaN", "0x1p1", "1_0").
func stringToNumber(s string) float64 {
	s = strings.TrimFunc(s, isJSWhitespace)
	if s == "" {
		return 0
	}
	if len(s) > 2 && s[0] == '0' {
		base := 0
		switch s[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			digits := s[2:]
			for _, r := range digits {
				if d := digitValue(r); d < 0 || d >= base {
					return math.NaN()
				}
			}
			n, _ := new(big.Int).SetString(digits, base)
			f, _ := new(big.Float).SetInt(n).Float64()
			return f
		}
	}
	unsigned := s
	if s[0] == '+' || s[0] == '-' {
		unsigned = s[1:]
	}
	switch {
	case unsigned == "Infinity" && s[0] == '-':
		return math.Inf(-1)
	case unsigned == "Infinity":
		return math.Inf(1)
	case !isDecimalLiteral(unsigned):
		return math.NaN()
	}
	f, _ := strconv.ParseFloat(s, 64) // out of range is ±Inf or ±0, as in JavaScript
	return f
}

// isDecimalLiteral is StrUnsignedDecimalLiteral without Infinity: digits with
// an optional fraction (either side of the point may be empty, not both) and
// an optional exponent.
func isDecimalLiteral(s string) bool {
	digits := func(s string) (int, string) {
		n := 0
		for n < len(s) && s[n] >= '0' && s[n] <= '9' {
			n++
		}
		return n, s[n:]
	}
	whole, rest := digits(s)
	fraction := 0
	if rest != "" && rest[0] == '.' {
		fraction, rest = digits(rest[1:])
	}
	if whole+fraction == 0 {
		return false
	}
	if rest != "" && (rest[0] == 'e' || rest[0] == 'E') {
		rest = rest[1:]
		if rest != "" && (rest[0] == '+' || rest[0] == '-') {
			rest = rest[1:]
		}
		exponent, after := digits(rest)
		if exponent == 0 {
			return false
		}
		rest = after
	}
	return rest == ""
}

// digitValue is r's value as a digit of base up to 16, or -1.
func digitValue(r rune) int {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0')
	case r >= 'a' && r <= 'f':
		return int(r-'a') + 10
	case r >= 'A' && r <= 'F':
		return int(r-'A') + 10
	}
	return -1
}

// isJSWhitespace is StrWhiteSpaceChar: JavaScript's WhiteSpace (tab, vertical
// tab, form feed, the byte order mark and every space separator) and
// LineTerminator. U+0085, which Go's unicode.IsSpace includes, is neither.
func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\v', '\f', 0xFEFF, '\n', '\r', 0x2028, 0x2029:
		return true
	}
	return unicode.Is(unicode.Zs, r)
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
	c := s.txn.cloner()
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
