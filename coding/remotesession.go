package coding

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/sky-valley/pi/client"
	"github.com/sky-valley/pi/protocol"
)

// RemoteSessionOperation names the one mutation a RemoteSession may have in
// flight.
type RemoteSessionOperation string

const (
	OperationOpen        RemoteSessionOperation = "open"
	OperationCreate      RemoteSessionOperation = "create"
	OperationSubmit      RemoteSessionOperation = "submit"
	OperationAbort       RemoteSessionOperation = "abort"
	OperationSetModel    RemoteSessionOperation = "setModel"
	OperationSetThinking RemoteSessionOperation = "setThinking"
	OperationReconnect   RemoteSessionOperation = "reconnect"
)

// RemoteSessionStatus is where a RemoteSession sits in its lifecycle.
type RemoteSessionStatus string

const (
	// LifecycleUnbound means no session is attached.
	LifecycleUnbound RemoteSessionStatus = "unbound"
	// LifecycleReady means a session is attached and idle locally.
	LifecycleReady RemoteSessionStatus = "ready"
	// LifecycleBusy means an operation is in flight.
	LifecycleBusy RemoteSessionStatus = "busy"
	// LifecycleDisposed means the session has been closed and will not work again.
	LifecycleDisposed RemoteSessionStatus = "disposed"
)

// RemoteSessionLifecycle is pi's discriminated lifecycle union flattened into
// one comparable value: Operation is meaningful only while Status is
// LifecycleBusy, and is empty otherwise, so two lifecycles compare equal exactly
// when pi's would deep-equal.
type RemoteSessionLifecycle struct {
	Status    RemoteSessionStatus
	Operation RemoteSessionOperation
}

// RemoteSessionState is everything a subscriber needs to render the session.
type RemoteSessionState struct {
	Lifecycle RemoteSessionLifecycle
	// Snapshot is the authoritative session state, or nil while unbound. It
	// must be treated as read-only.
	Snapshot *protocol.SessionSnapshot
	// Transcript is the snapshot's transcript with streaming progress projected
	// over it. It is never nil; an unbound session renders as empty.
	Transcript []protocol.TranscriptItem
}

// CreateRemoteSessionOptions are the properties of a session to create.
type CreateRemoteSessionOptions struct {
	Cwd           string
	Model         *protocol.ModelRef
	ThinkingLevel *protocol.ThinkingLevel
}

// RemoteSessionOptions configures a RemoteSession.
type RemoteSessionOptions struct {
	// OnListenerError receives failures raised by subscribers. Subscribers run
	// on whichever goroutine produced the notification, so a panicking one is
	// contained and reported here rather than allowed to corrupt session state.
	OnListenerError func(error)
}

// ErrRemoteSessionDisposed reports work attempted on a closed RemoteSession, and
// is also what an in-flight operation returns when disposal preempts it.
var ErrRemoteSessionDisposed = errors.New("Remote session is disposed")

// ErrNoRemoteSession reports an operation that needs an attached session when
// none is attached.
var ErrNoRemoteSession = errors.New("No remote session is attached")

// RemoteSessionBusyError reports an operation refused because another one is
// already in flight. Only abort may preempt, and only a submit.
type RemoteSessionBusyError struct{ Operation RemoteSessionOperation }

func (e *RemoteSessionBusyError) Error() string {
	return "Remote session is busy with " + string(e.Operation)
}

// disposedAfterAwaitError is pi's RemoteSessionDisposedError: the same message as
// ErrRemoteSessionDisposed, raised specifically when disposal beat an operation
// to a freshly acquired handle. Disposal drops it from its own cleanup errors —
// it is the disposal reporting itself — so it needs an identity of its own,
// while still matching errors.Is(err, ErrRemoteSessionDisposed) for callers who
// only care that the session is gone.
type disposedAfterAwaitError struct{}

func (disposedAfterAwaitError) Error() string { return ErrRemoteSessionDisposed.Error() }
func (disposedAfterAwaitError) Unwrap() error { return ErrRemoteSessionDisposed }

var errDisposedAfterAwait = disposedAfterAwaitError{}

// aggregateError is pi's AggregateError: several failures under one message.
// errors.Join cannot carry a message of its own, and these messages are
// peer-visible, so the shape is reproduced rather than approximated.
type aggregateError struct {
	message string
	errs    []error
}

func (e *aggregateError) Error() string   { return e.message }
func (e *aggregateError) Unwrap() []error { return e.errs }

// busyOperation is one installed busy lifecycle. Its identity, not its name, is
// what tells an operation whether the lifecycle it installed is still the
// current one — pi compares the lifecycle object itself for the same reason.
type busyOperation struct{ operation RemoteSessionOperation }

// attachmentOperation is one in-flight acquisition of a server attachment.
// Disposal waits for these: abandoning one would leave the server attached to a
// session no client holds.
type attachmentOperation struct {
	done chan struct{}
	err  error
}

// sessionBinding identifies one attachment of one handle. Subscriptions capture
// the binding they were made for and go quiet once it is replaced, which closes
// the window between registering a listener and installing the state it reads.
type sessionBinding struct{}

// RemoteSession drives one remote coding session over a client.Client: it owns
// the attachment, serializes the mutations that may act on it, and maintains the
// transcript projection subscribers render. It is the port of pi's RemoteSession
// (packages/coding-agent/src/client/remote-session.ts).
//
// Concurrency. pi serializes by chaining promises on a single-threaded event
// loop; here one mutex guards every field, and it is never held while a network
// request runs or a subscriber is called. Concurrency is refused rather than
// queued, exactly as in pi: a second mutation attempted while one is in flight
// fails with a RemoteSessionBusyError instead of waiting its turn. The one
// exception is Abort, which preempts an in-flight Submit and restores it on the
// way out.
//
// DIVERGENCE (deliberate): every mutation takes a context. pi has no
// cancellation at all, so this is new surface rather than a changed behavior —
// but a Go caller that cannot abandon a blocked network call has no way to shut
// down. Attachment work (Open, Create, Reconnect) runs under
// context.WithoutCancel: Close waits for it, and honoring the caller's
// cancellation there would strand the server holding an attachment nobody owns.
// Close's own context bounds that wait.
type RemoteSession struct {
	client          *client.Client
	onListenerError func(error)

	mu           sync.Mutex
	lifecycle    RemoteSessionLifecycle
	current      *busyOperation
	active       map[*busyOperation]bool
	handle       *client.SessionHandle
	transcript   *TranscriptState
	binding      *sessionBinding
	unsubscribes []client.Unsubscribe
	listeners    []remoteSessionListener
	nextListener uint64
	attachments  map[*attachmentOperation]bool

	disposeOnce sync.Once
	disposed    chan struct{}
	disposeDone chan struct{}
	disposeErr  error
}

type remoteSessionListener struct {
	id uint64
	fn func(RemoteSessionState)
}

// newRemoteSession builds an unbound session. It is unexported for the same
// reason pi's constructor is private: a session is only useful once it holds an
// attachment, and the factories below are what guarantee that.
func newRemoteSession(c *client.Client, opts RemoteSessionOptions) *RemoteSession {
	return &RemoteSession{
		client:          c,
		onListenerError: opts.OnListenerError,
		lifecycle:       RemoteSessionLifecycle{Status: LifecycleUnbound},
		active:          map[*busyOperation]bool{},
		attachments:     map[*attachmentOperation]bool{},
		disposed:        make(chan struct{}),
		disposeDone:     make(chan struct{}),
	}
}

// OpenRemoteSession takes an exclusive lease on an existing session, closing the
// half-built session if the attachment fails so a caller never has to clean up
// something it never got. The client is borrowed, not owned: closing the session
// leaves the client connected.
func OpenRemoteSession(
	ctx context.Context,
	c *client.Client,
	sessionID string,
	opts RemoteSessionOptions,
) (*RemoteSession, error) {
	session := newRemoteSession(c, opts)
	if err := session.Open(ctx, sessionID); err != nil {
		return nil, session.abandon(ctx, err)
	}
	return session, nil
}

// CreateRemoteSession starts a new session and takes it over.
func CreateRemoteSession(
	ctx context.Context,
	c *client.Client,
	create CreateRemoteSessionOptions,
	opts RemoteSessionOptions,
) (*RemoteSession, error) {
	session := newRemoteSession(c, opts)
	if err := session.Create(ctx, create); err != nil {
		return nil, session.abandon(ctx, err)
	}
	return session, nil
}

// abandon closes a session whose construction failed. A failure to clean up
// replaces the original error, as it does in pi: the caller is left holding
// neither a session nor a lease, and the cleanup failure is the one that says
// so.
func (s *RemoteSession) abandon(ctx context.Context, cause error) error {
	if err := s.Close(ctx); err != nil {
		return err
	}
	return cause
}

// ID is the attached session's identifier, or "" while unbound.
func (s *RemoteSession) ID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle == nil {
		return ""
	}
	return s.handle.ID()
}

// State is everything a subscriber renders, as of now.
func (s *RemoteSession) State() RemoteSessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked()
}

// Snapshot is the authoritative session state, or nil while unbound.
func (s *RemoteSession) Snapshot() *protocol.SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.transcript == nil {
		return nil
	}
	return s.transcript.Snapshot()
}

// Phase is the attached session's phase. The second result is false while
// unbound, which is pi's `undefined` phase.
func (s *RemoteSession) Phase() (protocol.SessionPhase, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.phaseLocked()
}

// Operation is the mutation in flight. The second result is false when none is.
func (s *RemoteSession) Operation() (RemoteSessionOperation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifecycle.Status != LifecycleBusy {
		return "", false
	}
	return s.lifecycle.Operation, true
}

// Models is what the server can run, as of the last server snapshot.
func (s *RemoteSession) Models() []protocol.ModelMetadata {
	if snapshot := s.client.Snapshot(); snapshot != nil {
		return snapshot.Models
	}
	return nil
}

// Sessions is every session the server knows about, as of the last server
// snapshot.
func (s *RemoteSession) Sessions() []protocol.SessionSummary {
	if snapshot := s.client.Snapshot(); snapshot != nil {
		return snapshot.Sessions
	}
	return nil
}

// ConnectionState is where the borrowed client's connection sits.
func (s *RemoteSession) ConnectionState() client.ConnectionState {
	return s.client.ConnectionState()
}

// Disposed reports whether Close has been called.
func (s *RemoteSession) Disposed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lifecycle.Status == LifecycleDisposed
}

// Subscribe registers a listener for every state change and calls it once
// immediately, so a subscriber renders without waiting for the next event.
func (s *RemoteSession) Subscribe(listener func(RemoteSessionState)) (client.Unsubscribe, error) {
	s.mu.Lock()
	if err := s.assertNotDisposedLocked(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.nextListener++
	id := s.nextListener
	s.listeners = append(s.listeners, remoteSessionListener{id: id, fn: listener})
	state := s.stateLocked()
	s.mu.Unlock()

	s.call(listener, state)
	var once sync.Once
	return func() { once.Do(func() { s.removeListener(id) }) }, nil
}

// OnConnectionStateChange forwards the borrowed client's connection lifecycle.
func (s *RemoteSession) OnConnectionStateChange(
	listener func(client.ConnectionStateChange),
) (client.Unsubscribe, error) {
	s.mu.Lock()
	err := s.assertNotDisposedLocked()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.client.OnConnectionStateChange(listener)
}

// Open takes an exclusive lease on a session, replacing the current attachment.
// Reopening the session that is already attached and ready is a no-op.
func (s *RemoteSession) Open(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	settled := s.handle != nil && s.handle.ID() == sessionID && s.lifecycle.Status == LifecycleReady
	s.mu.Unlock()
	if settled {
		return nil
	}
	return s.replace(ctx, OperationOpen, func(ctx context.Context) (*client.SessionHandle, error) {
		return s.client.AcquireSession(ctx, sessionID, client.LeaseExclusive)
	})
}

// Create starts a new session and attaches to it, replacing the current
// attachment.
func (s *RemoteSession) Create(ctx context.Context, opts CreateRemoteSessionOptions) error {
	return s.replace(ctx, OperationCreate, func(ctx context.Context) (*client.SessionHandle, error) {
		return s.client.CreateSession(ctx, client.CreateSessionOptions{
			Cwd:           &opts.Cwd,
			Model:         opts.Model,
			ThinkingLevel: opts.ThinkingLevel,
		})
	})
}

// Submit sends text to the session: a new turn while idle, a steer while one is
// running. Text that is blank once trimmed is not a message and is dropped.
func (s *RemoteSession) Submit(ctx context.Context, text string) error {
	normalized := trimJS(text)
	if normalized == "" {
		return nil
	}
	var (
		handle *client.SessionHandle
		idle   bool
	)
	started, err := s.start(OperationSubmit, func() error {
		var err error
		if handle, err = s.requireHandleLocked(); err != nil {
			return err
		}
		phase, known := s.phaseLocked()
		if !known || (phase != protocol.PhaseIdle && phase != protocol.PhaseTurn) {
			return fmt.Errorf("Session cannot accept input during %s phase", phaseText(phase, known, "unknown"))
		}
		idle = phase == protocol.PhaseIdle
		return nil
	})
	if err != nil {
		return err
	}
	defer s.end(started)
	return s.raceDispose(func() error {
		if idle {
			_, err := handle.Prompt(ctx, normalized)
			return err
		}
		_, err := handle.Steer(ctx, normalized)
		return err
	})
}

// Abort stops the running turn. It is the one operation that may preempt
// another: an abort issued while a submit is still waiting for its response
// takes over, and the submit's lifecycle is restored underneath it if it is
// still running when the abort finishes. Aborting an idle session does nothing.
func (s *RemoteSession) Abort(ctx context.Context) error {
	s.mu.Lock()
	preempting := s.lifecycle.Status == LifecycleBusy && s.lifecycle.Operation == OperationSubmit
	if preempting {
		if err := s.assertNotDisposedLocked(); err != nil {
			s.mu.Unlock()
			return err
		}
	} else if err := s.assertAvailableLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	handle, err := s.requireHandleLocked()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	// An idle session has nothing to abort — unless the abort is chasing a
	// submit whose prompt the server has not applied yet, in which case the
	// phase we can see is simply out of date.
	if phase, known := s.phaseLocked(); known && phase == protocol.PhaseIdle && !preempting {
		s.mu.Unlock()
		return nil
	}
	started := s.beginLocked(OperationAbort, preempting)
	s.mu.Unlock()

	s.notify(started)
	defer s.end(started)
	return s.raceDispose(func() error {
		_, err := handle.Abort(ctx)
		return err
	})
}

// SetModel switches the session's model. Only an idle session may be
// reconfigured.
func (s *RemoteSession) SetModel(ctx context.Context, model protocol.ModelRef) error {
	return s.runIdleOperation(OperationSetModel, "change model", func(handle *client.SessionHandle) error {
		_, err := handle.SetModel(ctx, model)
		return err
	})
}

// SetThinking switches the session's thinking level. Only an idle session may be
// reconfigured.
func (s *RemoteSession) SetThinking(ctx context.Context, level protocol.ThinkingLevel) error {
	return s.runIdleOperation(OperationSetThinking, "change thinking level", func(handle *client.SessionHandle) error {
		_, err := handle.SetThinking(ctx, level)
		return err
	})
}

// Reconnect rebuilds the transport and reacquires the same session. The old
// handle is dropped rather than detached: whatever it was leasing died with the
// connection.
func (s *RemoteSession) Reconnect(ctx context.Context) error {
	var sessionID string
	started, err := s.start(OperationReconnect, func() error {
		handle, err := s.requireHandleLocked()
		if err != nil {
			return err
		}
		sessionID = handle.ID()
		return nil
	})
	if err != nil {
		return err
	}
	defer s.end(started)
	return s.raceDispose(func() error {
		return s.trackAttachment(ctx, func(ctx context.Context) error {
			if _, err := s.client.Reconnect(ctx); err != nil {
				return err
			}
			handle, err := s.client.AcquireSession(ctx, sessionID, client.LeaseExclusive)
			if err != nil {
				return err
			}
			if err := s.assertNotDisposedAfterAwait(ctx, handle); err != nil {
				return err
			}
			return s.bind(ctx, handle, nil)
		})
	})
}

// Close disposes of the session: it stops accepting work, fails whatever is in
// flight, releases the attachment, and drops every subscriber. The borrowed
// client is left connected. It is pi's dispose(), named for Go's io.Closer
// convention, and is safe to call more than once — later callers wait for the
// first and get its result.
//
// Close blocks until the attachment is released, including any acquisition an
// operation had already started, so that no server-side attachment outlives the
// session that owns it.
func (s *RemoteSession) Close(ctx context.Context) error {
	first := false
	s.disposeOnce.Do(func() { first = true })
	if !first {
		select {
		case <-s.disposeDone:
			return s.disposeErr
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	s.mu.Lock()
	handle := s.handle
	s.lifecycle = RemoteSessionLifecycle{Status: LifecycleDisposed}
	s.current = nil
	// Published before anything else is torn down: an operation blocked on the
	// network must learn it has been preempted, not wait out its request.
	close(s.disposed)
	unsubscribes := s.takeUnsubscribesLocked()
	s.handle = nil
	s.transcript = nil
	s.binding = nil
	pending := make([]*attachmentOperation, 0, len(s.attachments))
	for op := range s.attachments {
		pending = append(pending, op)
	}
	started := &startedOperation{state: s.stateLocked(), listeners: s.listenerFnsLocked()}
	s.listeners = nil
	s.mu.Unlock()

	for _, unsubscribe := range unsubscribes {
		unsubscribe()
	}

	var released chan error
	if handle != nil {
		released = make(chan error, 1)
		go func() { released <- handle.Close(ctx) }()
	}
	s.notify(started)

	s.disposeErr = s.settleDisposal(ctx, pending, released)
	close(s.disposeDone)
	return s.disposeErr
}

// settleDisposal collects what cleanup failed. A disposal reporting its own
// preemption is not a failure, so errDisposedAfterAwait is dropped.
func (s *RemoteSession) settleDisposal(
	ctx context.Context,
	pending []*attachmentOperation,
	released chan error,
) error {
	var errs []error
	collect := func(err error) {
		if err != nil && !errors.Is(err, errDisposedAfterAwait) {
			errs = append(errs, err)
		}
	}
	for _, op := range pending {
		select {
		case <-op.done:
			collect(op.err)
		case <-ctx.Done():
			collect(ctx.Err())
		}
	}
	if released != nil {
		select {
		case err := <-released:
			collect(err)
		case <-ctx.Done():
			collect(ctx.Err())
		}
	}
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return &aggregateError{message: "Failed to dispose remote session", errs: errs}
	}
}

// startedOperation is the bookkeeping one installed operation needs to unwind
// itself, plus the notification its installation owes subscribers.
type startedOperation struct {
	busy      *busyOperation
	previous  *busyOperation
	preempt   bool
	state     RemoteSessionState
	listeners []func(RemoteSessionState)
}

// start runs the prologue shared by every non-preempting operation: it refuses a
// disposed or busy session, lets the caller check its own preconditions, and
// installs the busy lifecycle — all under one lock, which is what pi gets for
// free from its event loop. check runs with the lock held and must not block.
func (s *RemoteSession) start(operation RemoteSessionOperation, check func() error) (*startedOperation, error) {
	s.mu.Lock()
	if err := s.assertAvailableLocked(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if check != nil {
		if err := check(); err != nil {
			s.mu.Unlock()
			return nil, err
		}
	}
	started := s.beginLocked(operation, false)
	s.mu.Unlock()
	s.notify(started)
	return started, nil
}

// beginLocked installs a busy lifecycle. s.mu must be held; the caller must
// unlock, deliver the returned notification, and arrange for end to run.
func (s *RemoteSession) beginLocked(operation RemoteSessionOperation, preempt bool) *startedOperation {
	busy := &busyOperation{operation: operation}
	started := &startedOperation{busy: busy, previous: s.current, preempt: preempt}
	s.current = busy
	s.lifecycle = RemoteSessionLifecycle{Status: LifecycleBusy, Operation: operation}
	s.active[busy] = true
	started.state = s.stateLocked()
	started.listeners = s.listenerFnsLocked()
	return started
}

// end retires an operation's lifecycle. A preempting operation hands the
// lifecycle back to the one it interrupted, if that one is still running;
// otherwise the session settles on whether it still holds an attachment. An
// operation whose lifecycle has already been replaced — by disposal, or by a
// preemption that outlived it — changes nothing.
func (s *RemoteSession) end(started *startedOperation) {
	s.mu.Lock()
	delete(s.active, started.busy)
	if s.lifecycle.Status == LifecycleDisposed || s.current != started.busy {
		s.mu.Unlock()
		return
	}
	if started.preempt && started.previous != nil && s.active[started.previous] {
		s.current = started.previous
		s.lifecycle = RemoteSessionLifecycle{Status: LifecycleBusy, Operation: started.previous.operation}
	} else {
		s.current = nil
		if s.handle != nil {
			s.lifecycle = RemoteSessionLifecycle{Status: LifecycleReady}
		} else {
			s.lifecycle = RemoteSessionLifecycle{Status: LifecycleUnbound}
		}
	}
	settled := &startedOperation{state: s.stateLocked(), listeners: s.listenerFnsLocked()}
	s.mu.Unlock()
	s.notify(settled)
}

// raceDispose runs an operation's work and gives up on it the moment the session
// is disposed. It is pi's Promise.race against the disposal signal: the work
// itself keeps going either way, which is what lets Close wait out an attachment
// the preempted operation had already started acquiring.
func (s *RemoteSession) raceDispose(run func() error) error {
	done := make(chan error, 1)
	go func() { done <- run() }()
	// Work that has already finished wins, so a race lost by nanoseconds does
	// not turn a completed operation into a disposal error.
	select {
	case err := <-done:
		return err
	default:
	}
	select {
	case err := <-done:
		return err
	case <-s.disposed:
		return ErrRemoteSessionDisposed
	}
}

// replace swaps the attachment for a freshly prepared one.
func (s *RemoteSession) replace(
	ctx context.Context,
	operation RemoteSessionOperation,
	prepare func(context.Context) (*client.SessionHandle, error),
) error {
	started, err := s.start(operation, func() error {
		if s.handle == nil {
			return nil
		}
		// A session mid-turn cannot be walked away from: detaching would strand
		// work the user is still waiting on.
		if phase, known := s.phaseLocked(); !known || phase != protocol.PhaseIdle {
			return fmt.Errorf("Cannot %s a session while session is %s",
				operation, phaseText(phase, known, "unavailable"))
		}
		return nil
	})
	if err != nil {
		return err
	}
	defer s.end(started)
	return s.raceDispose(func() error {
		return s.trackAttachment(ctx, func(ctx context.Context) error {
			return s.prepareReplacement(ctx, operation, prepare)
		})
	})
}

// prepareReplacement acquires the new attachment before releasing the old one,
// so a failure anywhere leaves the session holding the attachment it started
// with rather than none at all.
func (s *RemoteSession) prepareReplacement(
	ctx context.Context,
	operation RemoteSessionOperation,
	prepare func(context.Context) (*client.SessionHandle, error),
) error {
	s.mu.Lock()
	previous := s.handle
	s.mu.Unlock()

	next, err := prepare(ctx)
	if err != nil {
		return err
	}
	if err := s.assertNotDisposedAfterAwait(ctx, next); err != nil {
		return err
	}
	snapshot := next.Snapshot()
	if snapshot == nil {
		if err := next.Close(ctx); err != nil {
			return err
		}
		return fmt.Errorf("Session %s did not provide a snapshot", next.ID())
	}

	// The phase is re-read here rather than trusted from the precondition: the
	// session may have started a turn while the replacement was being acquired.
	s.mu.Lock()
	phase, known := s.phaseLocked()
	s.mu.Unlock()

	replacing := previous != nil && previous.ID() != next.ID() && previous.Attached()
	if replacing && (!known || phase != protocol.PhaseIdle) {
		if err := next.Close(ctx); err != nil {
			return err
		}
		return fmt.Errorf("Cannot %s a session while session is %s",
			operation, phaseText(phase, known, "unavailable"))
	}
	if replacing {
		if err := previous.Detach(ctx); err != nil {
			// Holding two attachments is worse than holding a stale one, so the
			// replacement is given up even though the old one survived.
			if cleanupErr := next.Close(ctx); cleanupErr != nil {
				return &aggregateError{
					message: "Failed to replace remote session attachment",
					errs:    []error{err, cleanupErr},
				}
			}
			return err
		}
	}
	if err := s.assertNotDisposedAfterAwait(ctx, next); err != nil {
		return err
	}
	return s.bind(ctx, next, snapshot)
}

// runIdleOperation performs a reconfiguration that only an idle session accepts.
func (s *RemoteSession) runIdleOperation(
	operation RemoteSessionOperation,
	description string,
	run func(*client.SessionHandle) error,
) error {
	var handle *client.SessionHandle
	started, err := s.start(operation, func() error {
		var err error
		if handle, err = s.requireHandleLocked(); err != nil {
			return err
		}
		if phase, known := s.phaseLocked(); !known || phase != protocol.PhaseIdle {
			return fmt.Errorf("Cannot %s while session is %s", description, phaseText(phase, known, "unavailable"))
		}
		return nil
	})
	if err != nil {
		return err
	}
	defer s.end(started)
	return s.raceDispose(func() error { return run(handle) })
}

// trackAttachment registers work that acquires a server attachment, so Close
// waits for it instead of leaving the server attached to nobody.
func (s *RemoteSession) trackAttachment(ctx context.Context, run func(context.Context) error) error {
	op := &attachmentOperation{done: make(chan struct{})}
	s.mu.Lock()
	s.attachments[op] = true
	s.mu.Unlock()

	// The caller losing interest must not orphan a server attachment, but a
	// deadline is a promise about how long this can take at all, so it carries
	// over while the cancellation does not.
	detached := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		detached, cancel = context.WithDeadline(detached, deadline)
		defer cancel()
	}
	op.err = run(detached)
	close(op.done)

	s.mu.Lock()
	delete(s.attachments, op)
	s.mu.Unlock()
	return op.err
}

// bind installs a handle and the projection built from its snapshot.
//
// The subscriptions are registered before the lock is taken, because
// registering one reaches into the client while delivering one reaches back
// into this session — taking both locks in both orders would deadlock. The
// binding token is what makes that safe: a listener registered for a binding
// that is not the current one does nothing.
func (s *RemoteSession) bind(
	ctx context.Context,
	handle *client.SessionHandle,
	known *protocol.SessionSnapshot,
) error {
	snapshot := known
	if snapshot == nil {
		snapshot = handle.Snapshot()
	}
	if snapshot == nil {
		return fmt.Errorf("Session %s did not provide a snapshot", handle.ID())
	}

	binding := &sessionBinding{}
	unsubscribeSnapshot, err := handle.Subscribe(func(next *protocol.SessionSnapshot) {
		s.applySnapshot(binding, next)
	})
	if err != nil {
		return err
	}
	unsubscribeEvents, err := handle.OnEvent(func(event protocol.ServerEvent) {
		s.handleEvent(binding, event)
	})
	if err != nil {
		unsubscribeSnapshot()
		return err
	}

	s.mu.Lock()
	// DIVERGENCE (deliberate): pi's #bind is synchronous and cannot be raced by
	// disposal. Here it can, and binding a handle into a disposed session would
	// resurrect it and strand the attachment, so the handle is released instead.
	if s.lifecycle.Status == LifecycleDisposed {
		s.mu.Unlock()
		unsubscribeSnapshot()
		unsubscribeEvents()
		if err := handle.Close(ctx); err != nil {
			return err
		}
		return errDisposedAfterAwait
	}
	previous := s.takeUnsubscribesLocked()
	s.handle = handle
	s.transcript = NewTranscriptState(snapshot)
	s.binding = binding
	s.unsubscribes = []client.Unsubscribe{unsubscribeSnapshot, unsubscribeEvents}
	s.mu.Unlock()

	for _, unsubscribe := range previous {
		unsubscribe()
	}
	return nil
}

// applySnapshot folds an authoritative session snapshot into the projection.
func (s *RemoteSession) applySnapshot(binding *sessionBinding, next *protocol.SessionSnapshot) {
	s.mu.Lock()
	if s.binding != binding || s.transcript == nil {
		s.mu.Unlock()
		return
	}
	s.transcript = s.transcript.ApplySnapshot(next)
	settled := &startedOperation{state: s.stateLocked(), listeners: s.listenerFnsLocked()}
	s.mu.Unlock()
	s.notify(settled)
}

// handleEvent folds one session event into the projection. A removed session
// leaves the RemoteSession unbound and reusable: Open can attach it to something
// else, or to the same id once the server has it again.
func (s *RemoteSession) handleEvent(binding *sessionBinding, event protocol.ServerEvent) {
	s.mu.Lock()
	if s.binding != binding {
		s.mu.Unlock()
		return
	}
	var unsubscribes []client.Unsubscribe
	switch typed := event.(type) {
	case *protocol.SessionRemovedEvent:
		unsubscribes = s.takeUnsubscribesLocked()
		s.handle = nil
		s.transcript = nil
		s.binding = nil
		// An operation in flight keeps its lifecycle: it is still running, and
		// it will settle the session when it unwinds.
		if s.lifecycle.Status != LifecycleBusy {
			s.lifecycle = RemoteSessionLifecycle{Status: LifecycleUnbound}
		}
	case *protocol.SessionProgressEvent:
		if s.transcript == nil {
			s.mu.Unlock()
			return
		}
		s.transcript = s.transcript.ApplyProgress(typed.Progress)
	default:
		s.mu.Unlock()
		return
	}
	settled := &startedOperation{state: s.stateLocked(), listeners: s.listenerFnsLocked()}
	s.mu.Unlock()

	for _, unsubscribe := range unsubscribes {
		unsubscribe()
	}
	s.notify(settled)
}

// assertNotDisposedAfterAwait gives up a handle acquired by an operation that
// disposal has since preempted. It is pi's #assertNotDisposedAfterAwait, and the
// reason disposal has an error identity of its own.
func (s *RemoteSession) assertNotDisposedAfterAwait(ctx context.Context, handle *client.SessionHandle) error {
	if !s.Disposed() {
		return nil
	}
	if err := handle.Close(ctx); err != nil {
		return err
	}
	return errDisposedAfterAwait
}

func (s *RemoteSession) assertNotDisposedLocked() error {
	if s.lifecycle.Status == LifecycleDisposed {
		return ErrRemoteSessionDisposed
	}
	return nil
}

func (s *RemoteSession) assertAvailableLocked() error {
	if err := s.assertNotDisposedLocked(); err != nil {
		return err
	}
	if s.lifecycle.Status == LifecycleBusy {
		return &RemoteSessionBusyError{Operation: s.lifecycle.Operation}
	}
	return nil
}

func (s *RemoteSession) requireHandleLocked() (*client.SessionHandle, error) {
	if s.handle == nil {
		return nil, ErrNoRemoteSession
	}
	return s.handle, nil
}

func (s *RemoteSession) phaseLocked() (protocol.SessionPhase, bool) {
	if s.transcript == nil {
		return "", false
	}
	return s.transcript.Snapshot().Phase, true
}

// phaseText renders a phase inside an error message, standing in for pi's
// `this.phase ?? fallback`.
func phaseText(phase protocol.SessionPhase, known bool, fallback string) string {
	if !known {
		return fallback
	}
	return string(phase)
}

func (s *RemoteSession) stateLocked() RemoteSessionState {
	state := RemoteSessionState{Lifecycle: s.lifecycle, Transcript: []protocol.TranscriptItem{}}
	if s.transcript != nil {
		state.Snapshot = s.transcript.Snapshot()
		state.Transcript = s.transcript.Transcript()
	}
	return state
}

func (s *RemoteSession) takeUnsubscribesLocked() []client.Unsubscribe {
	unsubscribes := s.unsubscribes
	s.unsubscribes = nil
	return unsubscribes
}

func (s *RemoteSession) listenerFnsLocked() []func(RemoteSessionState) {
	fns := make([]func(RemoteSessionState), 0, len(s.listeners))
	for _, listener := range s.listeners {
		fns = append(fns, listener.fn)
	}
	return fns
}

func (s *RemoteSession) removeListener(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, listener := range s.listeners {
		if listener.id == id {
			s.listeners = append(s.listeners[:i:i], s.listeners[i+1:]...)
			return
		}
	}
}

func (s *RemoteSession) notify(started *startedOperation) {
	for _, listener := range started.listeners {
		s.call(listener, started.state)
	}
}

// call contains a subscriber's failure. pi wraps every listener in try/catch so
// one broken renderer cannot corrupt session state or silence the subscribers
// behind it; recover is the Go equivalent and is justified for exactly that.
func (s *RemoteSession) call(listener func(RemoteSessionState), state RemoteSessionState) {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.reportListenerError(recovered)
		}
	}()
	listener(state)
}

func (s *RemoteSession) reportListenerError(recovered any) {
	if s.onListenerError == nil {
		return
	}
	// Diagnostics cannot affect session or transport state, so a broken error
	// handler must not escape either.
	defer func() { _ = recover() }()
	err, ok := recovered.(error)
	if !ok {
		err = fmt.Errorf("%v", recovered)
	}
	s.onListenerError(err)
}

// jsWhitespace is ECMAScript's TrimString cutset — WhiteSpace ∪ LineTerminator
// (ECMA-262 §11.2, §11.3). It differs from unicode.IsSpace in both directions:
// it includes U+FEFF and excludes U+0085, so strings.TrimSpace would accept a
// prompt pi rejects as blank and reject one pi accepts.
const jsWhitespace = "\t\v\f\u0020\u00a0\ufeff" + // WhiteSpace
	"\n\r\u2028\u2029" + // LineTerminator
	"\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007" + // Unicode Zs
	"\u2008\u2009\u200a\u202f\u205f\u3000"

// trimJS is JavaScript's String.prototype.trim.
func trimJS(text string) string { return strings.Trim(text, jsWhitespace) }
