package client

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/sky-valley/pi/protocol"
)

// Options configures a Client. Token and TransportFactory are required.
type Options struct {
	// Token is the bearer token presented in the handshake.
	Token string
	// TransportFactory produces a fresh transport per connection attempt, so a
	// Client can reconnect without its owner rebuilding anything.
	TransportFactory TransportFactory
	// MaxFrameLength bounds one framed message in both directions. A nil
	// pointer takes protocol.DefaultMaxFrameLength.
	MaxFrameLength *int
	// OnListenerError reports subscriber failures. Subscribers run inside the
	// client's own goroutines, so a panicking one is contained and reported
	// here rather than being allowed to corrupt client state.
	OnListenerError func(error)
}

// CreateSessionOptions are the optional properties of a new session. Every
// field is a pointer because pi omits unset properties from the wire message
// entirely, and an omitted `cwd` is not the same request as an empty one.
type CreateSessionOptions struct {
	Cwd           *string
	Name          *string
	Model         *protocol.ModelRef
	ThinkingLevel *protocol.ThinkingLevel
}

// Client is a connected view of a pi server: the connection, the cached state,
// the in-flight requests, and the session leases this process holds. It is the
// port of pi's PiClient (packages/client/src/client.ts).
//
// All exported methods are safe for concurrent use. Listeners registered
// through Subscribe, OnEvent, OnConnectionStateChange, or a SessionHandle run
// on whichever goroutine produced the notification — usually the transport's
// reading goroutine — so a listener must not block, and must start a new
// goroutine if it wants to issue a request or reconnect.
//
// Lock ordering inside the package is sessionLease.mu, then Client.mu, then
// Connection.mu or State.mu. No lock is ever held while a callback runs.
type Client struct {
	conn       *Connection
	state      *State
	frameLimit int

	mu                       sync.Mutex
	disposed                 bool
	requestSequence          uint64
	pendingRequests          map[string]*pendingRequest
	leaseCounts              map[string]int
	exclusiveLeases          map[string]*leaseToken
	leaseGenerations         map[string]uint64
	attachments              map[string]*sharedOp
	detachments              map[string]*sharedOp
	reconciliations          map[string]*sharedOp
	cleanupRequired          map[string]bool
	connectionStateListeners *listenerSet[ConnectionStateChange]

	closeOnce sync.Once
}

// pendingRequest is one request waiting for its response. It settles once,
// either from the reader goroutine correlating a response or from the
// disconnect path rejecting everything in flight.
type pendingRequest struct {
	command protocol.Command
	done    chan struct{}
	once    sync.Once
	result  protocol.CommandResult
	err     error
}

func (p *pendingRequest) settle(result protocol.CommandResult, err error) {
	p.once.Do(func() {
		p.result = result
		p.err = err
		close(p.done)
	})
}

// sharedOp is one in-flight operation several callers wait on together — pi
// stores a shared promise in a Map for exactly this, so that a second acquirer
// joins the attach already in flight instead of issuing a duplicate.
//
// DIVERGENCE (deliberate): the caller that creates the op also drives it, on
// its own goroutine and with its own context, rather than the work running
// detached. That keeps cancellation attached to a real caller; the cost is that
// cancelling the first caller's context fails the shared op for everyone
// waiting on it, which pi cannot express because it has no cancellation at all.
type sharedOp struct {
	done chan struct{}
	once sync.Once
	err  error
}

func newSharedOp() *sharedOp { return &sharedOp{done: make(chan struct{})} }

func (o *sharedOp) settle(err error) {
	o.once.Do(func() {
		o.err = err
		close(o.done)
	})
}

// wait blocks until the op settles, returning its error.
func (o *sharedOp) wait(ctx context.Context) error {
	select {
	case <-o.done:
		return o.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// waitSettled blocks until the op settles, discarding its error. pi spells this
// `await detachment.catch(() => {})`: a failed detach must serialize a fresh
// acquisition behind it, not veto it.
func (o *sharedOp) waitSettled(ctx context.Context) error {
	select {
	case <-o.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// New builds an unconnected Client.
func New(opts Options) (*Client, error) {
	c := &Client{
		state:                    NewState(opts.OnListenerError),
		pendingRequests:          map[string]*pendingRequest{},
		leaseCounts:              map[string]int{},
		exclusiveLeases:          map[string]*leaseToken{},
		leaseGenerations:         map[string]uint64{},
		attachments:              map[string]*sharedOp{},
		detachments:              map[string]*sharedOp{},
		reconciliations:          map[string]*sharedOp{},
		cleanupRequired:          map[string]bool{},
		connectionStateListeners: newListenerSet[ConnectionStateChange](),
	}
	conn, err := NewConnection(ConnectionOptions{
		Token:            opts.Token,
		TransportFactory: opts.TransportFactory,
		MaxFrameLength:   opts.MaxFrameLength,
		OnHandshake:      c.state.ApplyServerSnapshot,
		OnMessage:        c.handleMessage,
		OnStateChange:    c.handleConnectionStateChange,
	})
	if err != nil {
		return nil, err
	}
	c.conn = conn
	c.frameLimit = conn.MaxFrameLength()
	return c, nil
}

// Connect builds a Client and completes its handshake, disposing of it if the
// handshake fails so a caller never has to clean up a client it never got.
func Connect(ctx context.Context, opts Options) (*Client, error) {
	c, err := New(opts)
	if err != nil {
		return nil, err
	}
	if _, err := c.Connect(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// Disposed reports whether Close has been called.
func (c *Client) Disposed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disposed
}

// ConnectionState reports where the underlying connection sits.
func (c *Client) ConnectionState() ConnectionState { return c.conn.State() }

// Connected reports whether the handshake has completed and not yet ended.
func (c *Client) Connected() bool { return c.conn.State() == Connected }

// Snapshot returns the latest server snapshot, or nil before the handshake.
func (c *Client) Snapshot() *protocol.ServerSnapshot { return c.state.Snapshot() }

// Connect performs the handshake and returns the server's snapshot. Reconnects
// start from a clean cache: a new server, or a restarted one, must not be
// described by the previous connection's leftovers.
func (c *Client) Connect(ctx context.Context) (*protocol.ServerSnapshot, error) {
	if err := c.assertNotDisposed(); err != nil {
		return nil, err
	}
	if c.conn.State() == Disconnected {
		c.state.Reset()
	}
	return c.conn.Connect(ctx)
}

// Reconnect is Connect under pi's other name for it, kept so ported call sites
// read unchanged.
func (c *Client) Reconnect(ctx context.Context) (*protocol.ServerSnapshot, error) {
	return c.Connect(ctx)
}

// Disconnect ends the current connection. An empty reason takes pi's default.
func (c *Client) Disconnect(reason string) {
	if reason == "" {
		reason = defaultConnectionDisconnectReason
	}
	c.conn.Disconnect(&DisconnectedError{Message: reason})
}

// Subscribe registers a listener for every server snapshot.
func (c *Client) Subscribe(listener func(*protocol.ServerSnapshot)) (Unsubscribe, error) {
	if err := c.assertNotDisposed(); err != nil {
		return nil, err
	}
	return c.state.Subscribe(listener), nil
}

// OnEvent registers a listener for every server event.
func (c *Client) OnEvent(listener func(protocol.ServerEvent)) (Unsubscribe, error) {
	if err := c.assertNotDisposed(); err != nil {
		return nil, err
	}
	return c.state.OnEvent(listener), nil
}

// OnConnectionStateChange registers a listener for connection lifecycle
// transitions.
func (c *Client) OnConnectionStateChange(listener func(ConnectionStateChange)) (Unsubscribe, error) {
	if err := c.assertNotDisposed(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	listeners := c.connectionStateListeners
	id := listeners.add(listener)
	var done sync.Once
	return func() {
		done.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			listeners.remove(id)
		})
	}, nil
}

// ListSessions asks the server for every session it knows about.
func (c *Client) ListSessions(ctx context.Context) ([]protocol.SessionSummary, error) {
	result, err := c.request(ctx, protocol.NewListCommand())
	if err != nil {
		return nil, err
	}
	list, ok := result.(*protocol.ListResult)
	if !ok {
		return nil, unexpectedResult("list", result)
	}
	return list.Sessions, nil
}

// CreateSession starts a new session and returns an exclusive lease on it. The
// creator owns what it created, so the lease is exclusive without being asked.
func (c *Client) CreateSession(ctx context.Context, opts CreateSessionOptions) (*SessionHandle, error) {
	result, err := c.request(ctx, &protocol.CreateCommand{
		Command:       "create",
		Cwd:           opts.Cwd,
		Name:          opts.Name,
		Model:         opts.Model,
		ThinkingLevel: opts.ThinkingLevel,
	})
	if err != nil {
		return nil, err
	}
	session, ok := result.(*protocol.SessionResult)
	if !ok {
		return nil, unexpectedResult("create", result)
	}
	token, err := c.reserveSessionLease(session.Session.ID, LeaseExclusive)
	if err != nil {
		return nil, err
	}
	return c.newLease(session.Session.ID, token), nil
}

// AttachSession takes a shared lease on an existing session.
func (c *Client) AttachSession(ctx context.Context, sessionID string) (*SessionHandle, error) {
	return c.AcquireSession(ctx, sessionID, LeaseShared)
}

// AcquireSession takes a lease on an existing session, attaching to it first if
// this client is not attached already.
//
// Acquisition serializes behind any detach still in flight for the same
// session, and behind the cleanup detach owed by a lease that was closed after
// its own detach failed — otherwise a reattach could race the server's teardown
// of the previous attachment and leave the session attached to nobody.
func (c *Client) AcquireSession(
	ctx context.Context,
	sessionID string,
	mode SessionLeaseMode,
) (*SessionHandle, error) {
	if err := c.assertNotDisposed(); err != nil {
		return nil, err
	}
	token, err := c.reserveSessionLease(sessionID, mode)
	if err != nil {
		return nil, err
	}
	handle, err := c.acquire(ctx, sessionID, token)
	if err != nil {
		c.releaseSessionLease(sessionID, token)
		return nil, err
	}
	return handle, nil
}

func (c *Client) acquire(ctx context.Context, sessionID string, token *leaseToken) (*SessionHandle, error) {
	c.mu.Lock()
	detachment := c.detachments[sessionID]
	needsCleanup := c.cleanupRequired[sessionID]
	c.mu.Unlock()

	if detachment != nil {
		if err := detachment.waitSettled(ctx); err != nil {
			return nil, err
		}
	}

	reconciled := false
	if needsCleanup {
		var err error
		reconciled, err = c.reconcileSessionCleanup(ctx, sessionID)
		if err != nil {
			return nil, err
		}
	}

	// A reconciled session was just detached on the server, so the cached
	// "attached" bit cannot be trusted and the attach is reissued regardless.
	if reconciled || !c.state.IsSessionAttached(sessionID) {
		if err := c.ensureAttached(ctx, sessionID); err != nil {
			return nil, err
		}
	}
	return c.newLease(sessionID, token), nil
}

// ensureAttached issues the attach, or joins the one already in flight.
func (c *Client) ensureAttached(ctx context.Context, sessionID string) error {
	c.mu.Lock()
	if existing := c.attachments[sessionID]; existing != nil {
		c.mu.Unlock()
		return existing.wait(ctx)
	}
	op := newSharedOp()
	c.attachments[sessionID] = op
	c.mu.Unlock()

	err := c.attachSession(ctx, sessionID)

	c.mu.Lock()
	if c.attachments[sessionID] == op {
		delete(c.attachments, sessionID)
	}
	c.mu.Unlock()
	op.settle(err)
	return err
}

// attachSession drops the cached snapshot before attaching and puts it back if
// the attach fails.
//
// The server answers an attach with a snapshot whose revision counts from the
// new attachment, which can be lower than the one already cached. Forgetting
// first is what lets that lower revision be accepted instead of discarded by
// State's staleness guard.
func (c *Client) attachSession(ctx context.Context, sessionID string) error {
	previous := c.state.ForgetSessionSnapshot(sessionID)
	if _, err := c.request(ctx, protocol.NewAttachCommand(sessionID)); err != nil {
		if previous != nil {
			c.state.RestoreSessionSnapshot(previous)
		}
		return err
	}
	return nil
}

// reconcileSessionCleanup pays off the detach owed by a lease that was closed
// after its own detach failed. It reports whether a reconciliation ran, so the
// caller knows to reattach.
func (c *Client) reconcileSessionCleanup(ctx context.Context, sessionID string) (bool, error) {
	c.mu.Lock()
	if !c.cleanupRequired[sessionID] {
		c.mu.Unlock()
		return false, nil
	}
	if existing := c.reconciliations[sessionID]; existing != nil {
		c.mu.Unlock()
		return true, existing.wait(ctx)
	}
	op := newSharedOp()
	c.reconciliations[sessionID] = op
	c.mu.Unlock()

	_, err := c.request(ctx, protocol.NewDetachCommand(sessionID))

	c.mu.Lock()
	if err == nil {
		delete(c.cleanupRequired, sessionID)
	}
	if c.reconciliations[sessionID] == op {
		delete(c.reconciliations, sessionID)
	}
	c.mu.Unlock()
	op.settle(err)
	return true, err
}

// request issues one command and waits for its response.
//
// A cancelled context abandons the wait but leaves the request registered, so
// the response the server eventually sends is still correlated and discarded.
// Forgetting it instead would make that response look unsolicited, and an
// unsolicited response tears the connection down.
func (c *Client) request(ctx context.Context, command protocol.Command) (protocol.CommandResult, error) {
	if err := c.assertNotDisposed(); err != nil {
		return nil, err
	}
	if !c.Connected() {
		return nil, &DisconnectedError{}
	}

	c.mu.Lock()
	c.requestSequence++
	id := "request-" + strconv.FormatUint(c.requestSequence, 10)
	pending := &pendingRequest{command: command, done: make(chan struct{})}
	c.pendingRequests[id] = pending
	c.mu.Unlock()

	frame, err := protocol.EncodeClientMessage(
		protocol.NewRequest(id, command),
		&protocol.FrameOptions{MaxFrameLength: &c.frameLimit},
	)
	if err == nil {
		err = c.conn.Send(frame)
	}
	// A send that fails terminally has already rejected this request through
	// the disconnect path, in which case the take below finds nothing and the
	// wait returns that rejection instead.
	if err != nil {
		if taken := c.takePendingRequest(id); taken != nil {
			return nil, err
		}
	}

	select {
	case <-pending.done:
		return pending.result, pending.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// handleMessage dispatches one post-handshake server message.
func (c *Client) handleMessage(message protocol.ServerMessage) {
	switch typed := message.(type) {
	case *protocol.EventEnvelope:
		// Leases are invalidated before the event is applied, so a subscriber
		// woken by it already sees the session as gone.
		if removed, ok := typed.Event.(*protocol.SessionRemovedEvent); ok {
			c.invalidateSessionLeases(removed.SessionID)
		}
		c.state.ApplyEvent(typed.Event)
	case *protocol.ResponseEnvelope:
		c.handleResponse(typed)
	}
}

func (c *Client) handleResponse(response *protocol.ResponseEnvelope) {
	pending := c.takePendingRequest(response.ID)
	if pending == nil {
		// The server answered something we never asked. The request/response
		// correlation is the only thing keeping results attached to callers, so
		// a violation of it is not recoverable.
		c.conn.Disconnect(&protocol.ValidationError{Msg: "Response has no matching request"})
		return
	}
	if !response.OK {
		pending.settle(nil, newServerError(response.Error))
		return
	}
	if response.Result.ResultCommandName() != pending.command.CommandName() {
		err := &protocol.ValidationError{Msg: fmt.Sprintf(
			"Response command %s does not match %s",
			response.Result.ResultCommandName(),
			pending.command.CommandName(),
		)}
		pending.settle(nil, err)
		c.conn.Disconnect(err)
		return
	}
	c.state.ApplyResult(response.Result)
	pending.settle(response.Result, nil)
}

func (c *Client) handleConnectionStateChange(change ConnectionStateChange) {
	if change.State == Disconnected {
		c.state.ClearAttachments()
		c.invalidateAllSessionLeases()
		err := change.Err
		if err == nil {
			err = &DisconnectedError{}
		}
		c.rejectPendingRequests(err)
	}
	c.notifyConnectionStateListeners(change)
}

func (c *Client) takePendingRequest(id string) *pendingRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	pending, ok := c.pendingRequests[id]
	if !ok {
		return nil
	}
	delete(c.pendingRequests, id)
	return pending
}

func (c *Client) rejectPendingRequests(err error) {
	c.mu.Lock()
	pending := c.pendingRequests
	c.pendingRequests = map[string]*pendingRequest{}
	c.mu.Unlock()
	for _, request := range pending {
		request.settle(nil, err)
	}
}

// Close disposes of the client: it stops accepting work, rejects everything in
// flight, drops the connection, and invalidates every lease. It is pi's
// dispose(), named for Go's io.Closer convention, and is idempotent.
//
// It always reports nil: disposal is local teardown with nothing left to fail,
// and the error result exists only so Client satisfies io.Closer.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.disposed = true
		c.mu.Unlock()

		err := &DisposedError{}
		// Rejecting first means the disconnect below finds nothing in flight,
		// so callers learn the client was disposed rather than merely dropped.
		c.rejectPendingRequests(err)
		c.conn.Disconnect(err)
		c.state.Dispose()
		c.invalidateAllSessionLeases()

		// Cleared last, so subscribers still see the disconnect that disposal
		// caused.
		c.mu.Lock()
		c.connectionStateListeners = newListenerSet[ConnectionStateChange]()
		c.mu.Unlock()
	})
	return nil
}

func (c *Client) assertNotDisposed() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disposed {
		return &DisposedError{}
	}
	return nil
}

func (c *Client) notifyConnectionStateListeners(change ConnectionStateChange) {
	c.mu.Lock()
	listeners := c.connectionStateListeners.snapshotFns()
	c.mu.Unlock()
	for _, listener := range listeners {
		// Reuses State's containment so a panicking connection-state listener
		// is reported through the same handler as a panicking subscriber, and
		// cannot stop the listeners behind it.
		c.state.call(func() { listener(change) })
	}
}

// unexpectedResult reports a result whose type does not match the command that
// asked for it. The response correlator already rejects a mismatched command
// name, so reaching this is a bug in this package rather than a hostile peer.
func unexpectedResult(command string, result protocol.CommandResult) error {
	name := "nothing"
	if result != nil {
		name = result.ResultCommandName()
	}
	return &protocol.ValidationError{Msg: fmt.Sprintf("Command %s answered with %s", command, name)}
}

// leaseToken identifies one lease. Its identity, not its contents, is what
// distinguishes the exclusive holder from a later lease on the same session.
type leaseToken struct{ mode SessionLeaseMode }

func (c *Client) reserveSessionLease(sessionID string, mode SessionLeaseMode) (*leaseToken, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := c.leaseCounts[sessionID]
	if mode == LeaseExclusive && count > 0 {
		return nil, &SessionOwnershipError{
			SessionID: sessionID,
			Message:   "Session " + sessionID + " already has an active lease",
		}
	}
	if mode == LeaseShared {
		if _, exclusive := c.exclusiveLeases[sessionID]; exclusive {
			return nil, &SessionOwnershipError{
				SessionID: sessionID,
				Message:   "Session " + sessionID + " has an exclusive lease",
			}
		}
	}
	token := &leaseToken{mode: mode}
	c.leaseCounts[sessionID] = count + 1
	if mode == LeaseExclusive {
		c.exclusiveLeases[sessionID] = token
	}
	return token, nil
}

func (c *Client) releaseSessionLease(sessionID string, token *leaseToken) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if count := c.leaseCounts[sessionID]; count <= 1 {
		delete(c.leaseCounts, sessionID)
	} else {
		c.leaseCounts[sessionID] = count - 1
	}
	if c.exclusiveLeases[sessionID] == token {
		delete(c.exclusiveLeases, sessionID)
	}
}

func (c *Client) leaseCount(sessionID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.leaseCounts[sessionID]
}

func (c *Client) leaseGeneration(sessionID string) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.leaseGenerations[sessionID]
}

// invalidateSessionLeases retires every lease on a session by bumping its
// generation. Leases notice lazily, which is what lets an invalidated lease be
// closed without issuing a detach for a session that no longer exists.
func (c *Client) invalidateSessionLeases(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.invalidateSessionLeasesLocked(sessionID)
}

func (c *Client) invalidateSessionLeasesLocked(sessionID string) {
	delete(c.leaseCounts, sessionID)
	delete(c.exclusiveLeases, sessionID)
	delete(c.cleanupRequired, sessionID)
	c.leaseGenerations[sessionID]++
}

func (c *Client) invalidateAllSessionLeases() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for sessionID := range c.leaseCounts {
		c.invalidateSessionLeasesLocked(sessionID)
	}
	clear(c.cleanupRequired)
}

func (c *Client) markCleanupRequired(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanupRequired[sessionID] = true
}

func (c *Client) setDetachment(sessionID string, op *sharedOp) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.detachments[sessionID] = op
}

func (c *Client) clearDetachment(sessionID string, op *sharedOp) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.detachments[sessionID] == op {
		delete(c.detachments, sessionID)
	}
}

// leaseState is where one lease sits in its own lifecycle.
type leaseState int

const (
	leaseActive leaseState = iota
	leaseReleasing
	leaseReleased
	leaseInvalidated
)

// sessionLease is the client-side half of a SessionHandle: the lease's own
// lifecycle plus the bookkeeping that decides whether releasing it should
// detach the session on the server.
//
// It is pi's closure over `state`, `releasePromise`, and `generation` inside
// PiClient#createSessionLease, made explicit because Go closures over mutable
// state shared between goroutines need a lock to hold anyway.
type sessionLease struct {
	client     *Client
	sessionID  string
	token      *leaseToken
	generation uint64

	mu        sync.Mutex
	state     leaseState
	releasing *sharedOp
}

func (c *Client) newLease(sessionID string, token *leaseToken) *SessionHandle {
	c.mu.Lock()
	generation := c.leaseGenerations[sessionID]
	c.leaseGenerations[sessionID] = generation
	c.mu.Unlock()
	lease := &sessionLease{
		client:     c,
		sessionID:  sessionID,
		token:      token,
		generation: generation,
		state:      leaseActive,
	}
	return newSessionHandle(sessionID, lease)
}

// refreshLocked retires the lease if its session's generation has moved on.
// A lease that is already released or invalidated stays that way.
func (l *sessionLease) refreshLocked() {
	if l.state != leaseActive && l.state != leaseReleasing {
		return
	}
	if l.client.leaseGeneration(l.sessionID) != l.generation {
		l.state = leaseInvalidated
	}
}

func (l *sessionLease) isActive() bool {
	l.mu.Lock()
	l.refreshLocked()
	active := l.state == leaseActive
	l.mu.Unlock()
	return active && l.client.state.IsSessionAttached(l.sessionID)
}

// assertActiveLocked is pi's assertActive. l.mu must be held; it deliberately
// does not call isActive, which would deadlock on that lock.
func (l *sessionLease) assertActiveLocked() error {
	if err := l.client.assertNotDisposed(); err != nil {
		return err
	}
	if !l.client.Connected() {
		return &DisconnectedError{}
	}
	if l.state != leaseActive || !l.client.state.IsSessionAttached(l.sessionID) {
		return &SessionDetachedError{SessionID: l.sessionID}
	}
	return nil
}

func (l *sessionLease) assertActive() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refreshLocked()
	return l.assertActiveLocked()
}

func (l *sessionLease) sessionSnapshot() *protocol.SessionSnapshot {
	if !l.isActive() {
		return nil
	}
	return l.client.state.SessionSnapshot(l.sessionID)
}

func (l *sessionLease) subscribe(listener func(*protocol.SessionSnapshot)) (Unsubscribe, error) {
	if err := l.assertActive(); err != nil {
		return nil, err
	}
	// Re-checked at delivery time as well: the lease can end between
	// subscribing and the next snapshot, and a retired lease must go quiet.
	return l.client.state.SubscribeSession(l.sessionID, func(snapshot *protocol.SessionSnapshot) {
		if l.isActive() {
			listener(snapshot)
		}
	}), nil
}

func (l *sessionLease) onEvent(listener func(protocol.ServerEvent)) (Unsubscribe, error) {
	if err := l.assertActive(); err != nil {
		return nil, err
	}
	return l.client.state.OnSessionEvent(l.sessionID, func(event protocol.ServerEvent) {
		// Removal is the one event delivered to a retired lease: it is the
		// reason the lease retired, and a subscriber needs to hear it.
		_, removed := event.(*protocol.SessionRemovedEvent)
		if l.isActive() || removed {
			listener(event)
		}
	}), nil
}

func (l *sessionLease) request(ctx context.Context, command protocol.Command) (protocol.CommandResult, error) {
	if err := l.assertActive(); err != nil {
		return nil, err
	}
	return l.client.request(ctx, command)
}

func (l *sessionLease) detach(ctx context.Context) error { return l.release(ctx, false) }

func (l *sessionLease) close(ctx context.Context) error { return l.release(ctx, true) }

// release gives the lease up. relinquishOnFailure decides what a failed detach
// means: Close gives the lease up anyway and records that the session still
// owes the server a detach, while Detach keeps the lease usable so the caller
// can retry.
func (l *sessionLease) release(ctx context.Context, relinquishOnFailure bool) error {
	l.mu.Lock()
	l.refreshLocked()
	switch l.state {
	case leaseReleased, leaseInvalidated:
		// Nothing to give up, and nothing to tell the server: an invalidated
		// lease's session is already gone.
		l.mu.Unlock()
		return nil
	}
	if l.releasing != nil {
		op := l.releasing
		l.mu.Unlock()
		return op.wait(ctx)
	}
	if err := l.assertActiveLocked(); err != nil {
		l.mu.Unlock()
		return err
	}
	l.state = leaseReleasing
	op := newSharedOp()
	l.releasing = op
	l.mu.Unlock()

	err := l.doRelease(ctx)

	l.mu.Lock()
	switch {
	case err == nil:
		l.state = leaseReleased
	default:
		l.refreshLocked()
		switch {
		case l.state == leaseInvalidated:
			// The session disappeared while we were detaching from it, which
			// makes the detach's failure moot.
			err = nil
		case relinquishOnFailure:
			l.client.releaseSessionLease(l.sessionID, l.token)
			l.client.markCleanupRequired(l.sessionID)
			l.state = leaseReleased
		default:
			l.state = leaseActive
			l.releasing = nil
		}
	}
	l.mu.Unlock()
	op.settle(err)
	return err
}

// doRelease drops this lease's claim, detaching on the server only when it was
// the last claim on the session.
func (l *sessionLease) doRelease(ctx context.Context) error {
	client := l.client
	if client.leaseCount(l.sessionID) > 1 {
		client.releaseSessionLease(l.sessionID, l.token)
		return nil
	}
	// Published before the request so a concurrent acquisition serializes
	// behind this detach instead of racing it.
	op := newSharedOp()
	client.setDetachment(l.sessionID, op)
	_, err := client.request(ctx, protocol.NewDetachCommand(l.sessionID))
	if err == nil {
		client.releaseSessionLease(l.sessionID, l.token)
	}
	client.clearDetachment(l.sessionID, op)
	op.settle(err)
	return err
}
