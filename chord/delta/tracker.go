package delta

import (
	"errors"
	"fmt"
)

// ─── Immutable tracking ──────────────────────────────────────────────────────
//
// Upstream's delta/tracker.ts. A tracker holds one committed, immutable
// revision. A change drafts the next one; preparing it diffs the draft's
// result against the committed revision into a batch of ops; adopting the
// prepared result commits it. Nothing is published until the caller adopts —
// which is what lets a replicated-state runtime commit, persist or discard a
// change as one unit.

// The lifecycle errors, with upstream's text. Adopt checks them in this order:
// owner, used, aborted, active change, stale.
var (
	ErrChangeActive      = errors.New("delta: Tracker already has an active change (prepare or abort it before beginning another, or before PrepareReplace)")
	ErrChangeSettled     = errors.New("delta: Change has already been settled (it was prepared or aborted; begin a new change with Tracker.BeginChange)")
	ErrForeignPrepared   = errors.New("delta: Prepared change belongs to a different tracker (adopt it on the tracker that prepared it)")
	ErrPreparedUsed      = errors.New("delta: Prepared change has already been used (each prepared change is adopted at most once; prepare the next one from Tracker.Value)")
	ErrPreparedAborted   = errors.New("delta: Prepared change has been aborted (its change was aborted after it was prepared; prepare it again)")
	ErrAdoptDuringChange = errors.New("delta: Cannot adopt while a change is active (prepare or abort the active change first)")
	ErrStalePrepared     = errors.New("delta: Prepared change is stale (the tracker adopted another revision after it was prepared; prepare it again against Tracker.Value)")

	// ErrScalarRevision is BeginChange's refusal of a scalar revision, with
	// the text of the TypeError pi's draft throws: it keys its states by the
	// root in a WeakMap, which takes only objects.
	ErrScalarRevision = errors.New("delta: Invalid value used as weak map key (a change drafts a JSON object or array, and the committed revision is a scalar; replace it with PrepareReplace)")

	// ErrZeroTracker is Go's own: a Tracker that Track did not make holds no
	// revision, so BeginChange, PrepareReplace and Adopt refuse it.
	ErrZeroTracker = errors.New("delta: Tracker holds no revision (it is a zero Tracker; make one with Track)")
)

// owner is a tracker's identity as a prepared change records it. It is not
// the tracker itself, so that a retained Prepared does not retain the tracker
// (upstream's `#owner = {}`); it has a size so that two owners never share an
// address.
type owner struct{ _ byte }

type preparedStatus uint8

const (
	statusPrepared preparedStatus = iota
	statusConsumed
	statusAborted
)

// preparedMeta is upstream's PreparedMetadata: what Adopt checks, shared with
// the Change that prepared it so that aborting the change invalidates it.
type preparedMeta struct {
	owner    *owner
	revision int
	status   preparedStatus
}

// Tracker holds one committed revision of a JSON value and turns changes to it
// into batches of ops. T is the root's Go type: map[string]any, []any, or any
// when it is not known. The revision is an object or array to draft; a scalar
// is accepted too, as pi's runtime accepts one under its `T extends object`
// type, and only PrepareReplace can change it.
//
// The committed revision is immutable: Value, and the Base, Value and Ops of
// every Prepared, share its containers, and none of them may be modified.
// Change it through BeginChange's draft, or replace it with PrepareReplace;
// either way the result takes effect only when adopted.
//
// A Tracker is not safe for concurrent use. At most one change is active at a
// time. The zero Tracker holds no revision and refuses every change with
// ErrZeroTracker; make one with Track.
type Tracker[T any] struct {
	owner    *owner
	value    T
	revision int
	changing bool
}

// Track starts tracking a deep copy of initial, a JSON object
// (map[string]any) or array ([]any) — or a scalar, which BeginChange cannot
// draft but PrepareReplace can replace. The copy is the first committed
// revision: numbers become float64, a nil map or slice an empty one, and an
// object or array reachable twice becomes two independent values. A cycle, a
// number that is not finite, or a Go type with no JSON form is a *ValueError.
func Track[T any](initial T) (*Tracker[T], error) {
	v, err := importRoot[T](initial)
	if err != nil {
		return nil, err
	}
	return &Tracker[T]{owner: &owner{}, value: v}, nil
}

// importRoot imports a root value and returns it as T.
func importRoot[T any](root T) (T, error) {
	var zero T
	v, err := importValue(root)
	if err != nil {
		return zero, err
	}
	if v == nil {
		// JSON null: only an interface T holds it, and root was one.
		return zero, nil
	}
	out, ok := v.(T)
	if !ok {
		return zero, fmt.Errorf("delta: a tracked revision is imported as nil, bool, float64, string, map[string]any or []any, and %T cannot hold the %T this one became (track with T = any, or with that type)", zero, v)
	}
	return out, nil
}

// Value is the committed revision.
func (t *Tracker[T]) Value() T { return t.value }

// BeginChange opens a change: a draft of the committed revision that the
// caller mutates, then prepares or aborts. A tracker has at most one active
// change, and nothing can be adopted while it is open. A scalar revision has
// no draft: ErrScalarRevision.
func (t *Tracker[T]) BeginChange() (*Change[T], error) {
	if t.owner == nil {
		return nil, ErrZeroTracker
	}
	if t.changing {
		return nil, ErrChangeActive
	}
	if !isContainer(any(t.value)) {
		return nil, ErrScalarRevision
	}
	t.changing = true
	tx := newTransaction(any(t.value))
	return &Change[T]{tracker: t, txn: tx, root: tx.root.draft}, nil
}

// PrepareReplace prepares replacing the committed revision with a deep copy of
// value. The ops are the diff between the two, so a replacement that differs
// in one member publishes one op; one that is deeply equal publishes none, and
// its Value is the committed revision itself.
func (t *Tracker[T]) PrepareReplace(value T) (*Prepared[T], error) {
	if t.owner == nil {
		return nil, ErrZeroTracker
	}
	if t.changing {
		return nil, ErrChangeActive
	}
	candidate, err := importRoot[T](value)
	if err != nil {
		return nil, err
	}
	return t.prepare(candidate), nil
}

// Adopt commits a prepared change. It must come from this tracker, be
// neither used nor aborted, and have been prepared against the committed
// revision; no change may be active. A nil or zero Prepared belongs to no
// tracker.
func (t *Tracker[T]) Adopt(p *Prepared[T]) error {
	if t.owner == nil {
		return ErrZeroTracker
	}
	if p == nil || p.meta == nil || p.meta.owner != t.owner {
		return ErrForeignPrepared
	}
	switch p.meta.status {
	case statusConsumed:
		return ErrPreparedUsed
	case statusAborted:
		return ErrPreparedAborted
	}
	if t.changing {
		return ErrAdoptDuringChange
	}
	if p.meta.revision != t.revision || !same(any(t.value), any(p.base)) {
		return ErrStalePrepared
	}
	p.meta.status = statusConsumed
	t.value = p.value
	t.revision++
	return nil
}

// prepareDraft finishes a change's draft into a prepared change, releasing
// the tracker for the next change either way.
func (t *Tracker[T]) prepareDraft(tx *transaction) (*Prepared[T], error) {
	defer func() { t.changing = false }()
	v, err := tx.finish()
	if err != nil {
		return nil, err
	}
	return t.prepare(v.(T)), nil
}

// prepare diffs candidate against the committed revision. An empty diff
// prepares the committed revision itself, so a no-op keeps its identity.
func (t *Tracker[T]) prepare(candidate T) *Prepared[T] {
	base := t.value
	ops := DiffRevisions(any(base), any(candidate))
	value := candidate
	if len(ops) == 0 {
		value = base
	}
	return &Prepared[T]{
		meta:  &preparedMeta{owner: t.owner, revision: t.revision, status: statusPrepared},
		base:  base,
		value: value,
		ops:   ops,
	}
}

// Prepared is a change ready to adopt: the revision it was prepared against,
// the revision it produces, and the ops between them. All three are shared
// and must not be modified.
type Prepared[T any] struct {
	meta  *preparedMeta
	base  T
	value T
	ops   []Op
}

// Base is the committed revision the change was prepared against.
func (p *Prepared[T]) Base() T { return p.base }

// Value is the revision adopting the change commits. It is Base itself when
// the change turned out to be a no-op.
func (p *Prepared[T]) Value() T { return p.value }

// Ops is the batch that turns Base into Value — what a replica applies. It is
// empty, not nil, for a no-op.
func (p *Prepared[T]) Ops() []Op { return p.ops }

type changeStatus uint8

const (
	changeOpen changeStatus = iota
	changePrepared
	changeAborted
)

// Change is one open edit of a tracker's committed revision. Mutate State,
// then Prepare it — or Abort it. Either settles the change and revokes its
// drafts; a draft may be held across any amount of work in between, but not
// past that.
//
// A Change is not safe for concurrent use.
type Change[T any] struct {
	root     *Draft
	tracker  *Tracker[T]
	txn      *transaction
	prepared *preparedMeta
	status   changeStatus
}

// State is the draft of the root.
func (c *Change[T]) State() *Draft { return c.root }

// errZeroChange is Go's own: a Change that BeginChange did not open has no
// draft to prepare.
var errZeroChange = errors.New("delta: Change was never begun (it is a zero Change; open one with Tracker.BeginChange)")

// Prepare settles the change: it finishes the draft and diffs the result
// against the revision the change began from. The tracker is free for the next
// change afterwards, whether or not Prepare succeeds; a failure (an array left
// with holes) aborts the change.
func (c *Change[T]) Prepare() (*Prepared[T], error) {
	if c.status != changeOpen {
		return nil, ErrChangeSettled
	}
	if c.tracker == nil {
		return nil, errZeroChange
	}
	t, tx := c.tracker, c.txn
	c.tracker, c.txn = nil, nil
	p, err := t.prepareDraft(tx)
	if err != nil {
		c.status = changeAborted
		return nil, err
	}
	c.prepared = p.meta
	c.status = changePrepared
	return p, nil
}

// Abort settles the change without preparing it, or — after Prepare —
// invalidates the prepared change if it has not been adopted. Aborting twice,
// or aborting a zero Change, does nothing.
func (c *Change[T]) Abort() {
	switch c.status {
	case changeAborted:
		return
	case changePrepared:
		if c.prepared.status == statusPrepared {
			c.prepared.status = statusAborted
		}
		c.prepared = nil
		c.status = changeAborted
		return
	}
	c.status = changeAborted
	t, tx := c.tracker, c.txn
	if t == nil {
		return
	}
	c.tracker, c.txn = nil, nil
	tx.release()
	t.changing = false
}
