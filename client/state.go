package client

import (
	"fmt"
	"sync"

	"github.com/sky-valley/pi/protocol"
)

// State is the client's view of the server: the latest server snapshot, the
// per-session snapshots, which sessions this connection has attached, and the
// subscribers watching all of it.
//
// pi's ClientState is single-threaded. Here mutations arrive from the
// connection's reader goroutine while reads come from caller goroutines, so
// every field is guarded. Listeners are invoked after the lock is released, so
// a subscriber may call back into State without deadlocking; ordering still
// holds because all mutation comes from the one reader goroutine.
type State struct {
	mu               sync.Mutex
	snapshot         *protocol.ServerSnapshot
	sessionSnapshots map[string]*protocol.SessionSnapshot
	attached         map[string]bool

	snapshotListeners        *listenerSet[*protocol.ServerSnapshot]
	eventListeners           *listenerSet[protocol.ServerEvent]
	sessionSnapshotListeners map[string]*listenerSet[*protocol.SessionSnapshot]
	sessionEventListeners    map[string]*listenerSet[protocol.ServerEvent]

	onListenerError func(error)
}

// NewState returns an empty State. onListenerError, if set, receives panics
// raised by subscribers; it may be nil.
func NewState(onListenerError func(error)) *State {
	return &State{
		sessionSnapshots:         map[string]*protocol.SessionSnapshot{},
		attached:                 map[string]bool{},
		snapshotListeners:        newListenerSet[*protocol.ServerSnapshot](),
		eventListeners:           newListenerSet[protocol.ServerEvent](),
		sessionSnapshotListeners: map[string]*listenerSet[*protocol.SessionSnapshot]{},
		sessionEventListeners:    map[string]*listenerSet[protocol.ServerEvent]{},
		onListenerError:          onListenerError,
	}
}

// listenerSet holds callbacks in registration order with stable identity.
//
// It is a slice rather than a map for two reasons: Go funcs are not
// comparable, so removal needs an explicit id, and pi stores listeners in a Set
// — which iterates in insertion order. Notifying in map order instead would
// make delivery order vary run to run, which is both a divergence and a source
// of tests that pass by luck. Subscriber counts are small, so the linear
// removal scan is cheaper than keeping a parallel index.
type listenerSet[T any] struct {
	next    uint64
	entries []listenerEntry[T]
}

type listenerEntry[T any] struct {
	id uint64
	fn func(T)
}

func newListenerSet[T any]() *listenerSet[T] {
	return &listenerSet[T]{}
}

func (s *listenerSet[T]) add(fn func(T)) uint64 {
	s.next++
	s.entries = append(s.entries, listenerEntry[T]{id: s.next, fn: fn})
	return s.next
}

func (s *listenerSet[T]) remove(id uint64) {
	for i, entry := range s.entries {
		if entry.id == id {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			return
		}
	}
}

func (s *listenerSet[T]) len() int { return len(s.entries) }

// snapshotFns copies the callbacks, in registration order, so they can be
// invoked outside the lock.
func (s *listenerSet[T]) snapshotFns() []func(T) {
	if s == nil {
		return nil
	}
	out := make([]func(T), 0, len(s.entries))
	for _, entry := range s.entries {
		out = append(out, entry.fn)
	}
	return out
}

// Snapshot returns the latest server snapshot, or nil before the handshake.
func (s *State) Snapshot() *protocol.ServerSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot
}

// Reset drops all server-derived state, keeping subscribers.
func (s *State) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetLocked()
}

func (s *State) resetLocked() {
	s.snapshot = nil
	clear(s.sessionSnapshots)
	clear(s.attached)
}

// ClearAttachments forgets which sessions were attached, keeping snapshots. A
// reconnect loses the attachments but the cached transcripts are still worth
// showing.
func (s *State) ClearAttachments() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.attached)
}

// Dispose drops all state and all subscribers.
func (s *State) Dispose() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetLocked()
	s.snapshotListeners = newListenerSet[*protocol.ServerSnapshot]()
	s.eventListeners = newListenerSet[protocol.ServerEvent]()
	clear(s.sessionSnapshotListeners)
	clear(s.sessionEventListeners)
}

// SessionSnapshot returns the cached snapshot for a session, if any.
func (s *State) SessionSnapshot(sessionID string) *protocol.SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionSnapshots[sessionID]
}

// IsSessionAttached reports whether this connection has the session attached.
func (s *State) IsSessionAttached(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attached[sessionID]
}

// ForgetSessionSnapshot removes and returns a cached session snapshot.
func (s *State) ForgetSessionSnapshot(sessionID string) *protocol.SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.sessionSnapshots[sessionID]
	delete(s.sessionSnapshots, sessionID)
	return previous
}

// RestoreSessionSnapshot puts a snapshot back only if none is cached. Restore
// exists to undo a failed detach, and a fresher snapshot may have arrived in
// the meantime — that one wins.
func (s *State) RestoreSessionSnapshot(snapshot *protocol.SessionSnapshot) {
	if snapshot == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessionSnapshots[snapshot.ID]; !exists {
		s.sessionSnapshots[snapshot.ID] = snapshot
	}
}

// Subscribe registers a server-snapshot listener.
func (s *State) Subscribe(listener func(*protocol.ServerSnapshot)) Unsubscribe {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.snapshotListeners.add(listener)
	return s.once(func() { s.snapshotListeners.remove(id) })
}

// OnEvent registers a listener for every server event.
func (s *State) OnEvent(listener func(protocol.ServerEvent)) Unsubscribe {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.eventListeners.add(listener)
	return s.once(func() { s.eventListeners.remove(id) })
}

// SubscribeSession registers a snapshot listener scoped to one session.
func (s *State) SubscribeSession(sessionID string, listener func(*protocol.SessionSnapshot)) Unsubscribe {
	s.mu.Lock()
	defer s.mu.Unlock()
	return addScoped(s, s.sessionSnapshotListeners, sessionID, listener)
}

// OnSessionEvent registers an event listener scoped to one session.
func (s *State) OnSessionEvent(sessionID string, listener func(protocol.ServerEvent)) Unsubscribe {
	s.mu.Lock()
	defer s.mu.Unlock()
	return addScoped(s, s.sessionEventListeners, sessionID, listener)
}

// addScoped registers a per-session listener, creating the bucket on demand and
// dropping it when its last listener leaves — otherwise a long-lived client
// accumulates one map entry per session it ever watched.
func addScoped[T any](s *State, buckets map[string]*listenerSet[T], sessionID string, listener func(T)) Unsubscribe {
	set, exists := buckets[sessionID]
	if !exists {
		set = newListenerSet[T]()
		buckets[sessionID] = set
	}
	id := set.add(listener)
	return s.once(func() {
		set.remove(id)
		if set.len() == 0 {
			delete(buckets, sessionID)
		}
	})
}

// once wraps an unsubscribe so repeated calls are harmless — callers hold these
// and may run them from a defer as well as explicitly.
func (s *State) once(remove func()) Unsubscribe {
	var done sync.Once
	return func() {
		done.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			remove()
		})
	}
}

// ApplyResult folds a command result into the cached state.
func (s *State) ApplyResult(result protocol.CommandResult) {
	s.mu.Lock()
	var pending []func()
	switch typed := result.(type) {
	case *protocol.ListResult:
		// A list result carries summaries, not snapshots; pi leaves cached
		// state untouched.
	case *protocol.DetachResult:
		delete(s.attached, typed.SessionID)
		if current := s.sessionSnapshots[typed.SessionID]; current != nil {
			// A detach result carries no new snapshot, so the cached one is
			// force-updated: its revision has not moved and the normal guard
			// would discard the change.
			detached := *current
			detached.Attached = false
			pending = s.applySessionSnapshotLocked(&detached, true)
		}
	case *protocol.SessionResult:
		session := typed.Session
		pending = s.applySessionSnapshotLocked(&session, false)
	}
	s.mu.Unlock()
	s.deliver(pending)
}

// ApplyEvent folds a server event into the cached state and notifies listeners.
func (s *State) ApplyEvent(event protocol.ServerEvent) {
	s.mu.Lock()
	var pending []func()
	switch typed := event.(type) {
	case *protocol.ServerSnapshotEvent:
		snapshot := typed.Snapshot
		pending = s.applyServerSnapshotLocked(&snapshot)
	case *protocol.SessionSnapshotEvent:
		snapshot := typed.Snapshot
		pending = s.applySessionSnapshotLocked(&snapshot, false)
	case *protocol.SessionRemovedEvent:
		delete(s.sessionSnapshots, typed.SessionID)
		delete(s.attached, typed.SessionID)
	}
	pending = append(pending, queue(s.eventListeners.snapshotFns(), event)...)
	if sessionID := eventSessionID(event); sessionID != "" {
		pending = append(pending, queue(s.sessionEventListeners[sessionID].snapshotFns(), event)...)
	}
	s.mu.Unlock()
	s.deliver(pending)
}

// ApplyServerSnapshot replaces the server snapshot unless it is stale.
func (s *State) ApplyServerSnapshot(snapshot *protocol.ServerSnapshot) {
	s.mu.Lock()
	pending := s.applyServerSnapshotLocked(snapshot)
	s.mu.Unlock()
	s.deliver(pending)
}

func (s *State) applyServerSnapshotLocked(snapshot *protocol.ServerSnapshot) []func() {
	if snapshot == nil {
		return nil
	}
	// Snapshots can arrive out of order behind a reconnect; an older one must
	// not clobber a newer view.
	if s.snapshot != nil && snapshot.Revision < s.snapshot.Revision {
		return nil
	}
	s.snapshot = snapshot
	// The server snapshot is authoritative, so attachments are rebuilt from it
	// rather than merged into what was already there.
	clear(s.attached)
	for _, session := range snapshot.Sessions {
		if session.Attached {
			s.attached[session.ID] = true
		}
	}
	return queue(s.snapshotListeners.snapshotFns(), snapshot)
}

func (s *State) applySessionSnapshotLocked(snapshot *protocol.SessionSnapshot, force bool) []func() {
	if snapshot == nil {
		return nil
	}
	current := s.sessionSnapshots[snapshot.ID]
	if !force && current != nil && snapshot.Revision < current.Revision {
		return nil
	}
	s.sessionSnapshots[snapshot.ID] = snapshot
	if snapshot.Attached {
		s.attached[snapshot.ID] = true
	} else {
		delete(s.attached, snapshot.ID)
	}
	return queue(s.sessionSnapshotListeners[snapshot.ID].snapshotFns(), snapshot)
}

// queue binds a value to each listener so the calls can be made after the lock
// is released.
func queue[T any](fns []func(T), value T) []func() {
	out := make([]func(), 0, len(fns))
	for _, fn := range fns {
		out = append(out, func() { fn(value) })
	}
	return out
}

// deliver runs queued notifications outside the lock, containing each one. pi
// wraps every callback in try/catch so a subscriber cannot corrupt client
// state or stop the subscribers after it; recover is the Go equivalent and is
// justified for exactly that reason.
func (s *State) deliver(pending []func()) {
	for _, notify := range pending {
		s.call(notify)
	}
}

func (s *State) call(notify func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.reportListenerError(recovered)
		}
	}()
	notify()
}

func (s *State) reportListenerError(recovered any) {
	if s.onListenerError == nil {
		return
	}
	// Diagnostics cannot affect client state, so a broken error handler must
	// not escape either.
	defer func() { _ = recover() }()
	err, ok := recovered.(error)
	if !ok {
		err = fmt.Errorf("%v", recovered)
	}
	s.onListenerError(err)
}

// eventSessionID reports which session an event belongs to, if any.
func eventSessionID(event protocol.ServerEvent) string {
	switch typed := event.(type) {
	case *protocol.SessionSnapshotEvent:
		return typed.Snapshot.ID
	case *protocol.SessionProgressEvent:
		return typed.SessionID
	case *protocol.SessionRemovedEvent:
		return typed.SessionID
	default:
		return ""
	}
}
