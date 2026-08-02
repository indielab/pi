package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sky-valley/pi/protocol"
)

// DefaultHandshakeTimeout is how long a connection has to complete its
// handshake before the server closes it.
const DefaultHandshakeTimeout = 5 * time.Second

// maxHandshakeTimeout is Node's maximum timer delay.
//
// This bound is a Node artifact — Go timers have no such limit — but it is kept
// so a configuration a Node pi server rejects is rejected here too. A
// deployment that shares its configuration between the two implementations
// should get the same answer from both.
const maxHandshakeTimeout = 2147483647 * time.Millisecond

const maxUint32 = 0xffff_ffff

// Options configures a Server.
type Options struct {
	// Token is the shared secret a client must present in its hello. It must
	// not be empty.
	Token string
	// Listeners are the transports to serve on. It must not be nil; an empty
	// non-nil slice is a server with no transport, which is useful in tests.
	Listeners []Listener
	// MaxFrameLength bounds one encoded protocol message. nil takes
	// protocol.DefaultMaxFrameLength.
	MaxFrameLength *int
	// HandshakeTimeout bounds the handshake. Zero takes
	// DefaultHandshakeTimeout.
	HandshakeTimeout time.Duration
	// ServerID is the stable identity this server reports in its snapshots.
	//
	// DIVERGENCE (deliberate): pi distinguishes an absent serverId (generate
	// one) from an empty string (a TypeError). Go's zero value cannot carry
	// that distinction, so empty means "generate one" and there is nothing left
	// to reject.
	ServerID string
	// OnError observes failures that have nowhere else to go: backend errors
	// hidden from clients, transport failures, dispose failures. It may be nil,
	// may be called from any goroutine, and its own panics are swallowed.
	OnError func(error)
}

// Server serves pi's session protocol over any set of Listeners.
//
// DIVERGENCE (deliberate): pi wires its three collaborators together with
// option bags full of closures back into PiServer. Here the session manager and
// the snapshot publisher hold a *Server directly. The indirection bought pi
// nothing that a Go package boundary does not already give, and the closures
// obscured the lock order these three now share: a caller never holds the
// Server's mutex while calling into the session manager.
type Server struct {
	id                  string
	listeners           []Listener
	expectedTokenDigest [32]byte
	maxFrameLength      int
	handshakeTimeout    time.Duration
	onError             func(error)

	// ctx bounds backend and runtime calls. It is cancelled once Close has
	// finished, so a backend can use it to retire whatever it started; it is
	// deliberately still live while sessions are being disposed.
	ctx    context.Context
	cancel context.CancelFunc

	sessions  *sessionManager
	snapshots *snapshotPublisher

	mu          sync.Mutex
	connections map[*connState]struct{}
	closing     bool
	started     bool
	startDone   chan struct{}
	closeDone   chan struct{}
	closeErr    error
}

// New validates options and builds a Server. It does not bind anything; call
// Start for that.
func New(backend Backend, options Options) (*Server, error) {
	if backend == nil {
		return nil, errors.New("server backend must not be nil")
	}
	if options.Listeners == nil {
		return nil, errors.New("server listeners must not be nil; pass an empty slice for a server with no transport")
	}
	if options.Token == "" {
		return nil, errors.New("server token must not be empty")
	}
	maxFrameLength := protocol.DefaultMaxFrameLength
	if options.MaxFrameLength != nil {
		maxFrameLength = *options.MaxFrameLength
	}
	if maxFrameLength <= 0 || maxFrameLength > maxUint32 {
		return nil, fmt.Errorf("server maxFrameLength must be an integer between 1 and %d", maxUint32)
	}
	handshakeTimeout := options.HandshakeTimeout
	if handshakeTimeout == 0 {
		handshakeTimeout = DefaultHandshakeTimeout
	}
	if handshakeTimeout < 0 || handshakeTimeout > maxHandshakeTimeout {
		return nil, fmt.Errorf("server handshakeTimeout must be between 1ms and %s", maxHandshakeTimeout)
	}

	id := options.ServerID
	if id == "" {
		id = newUUID()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		id:                  id,
		listeners:           options.Listeners,
		expectedTokenDigest: sha256.Sum256([]byte(options.Token)),
		maxFrameLength:      maxFrameLength,
		handshakeTimeout:    handshakeTimeout,
		onError:             options.OnError,
		ctx:                 ctx,
		cancel:              cancel,
		connections:         map[*connState]struct{}{},
	}
	s.sessions = newSessionManager(s, backend)
	s.snapshots = newSnapshotPublisher(s, backend)
	return s, nil
}

// ID is the server's stable identity, as reported in its snapshots.
func (s *Server) ID() string { return s.id }

// Addresses lists the bound address of every listener that has one.
func (s *Server) Addresses() []string {
	addresses := make([]string, 0, len(s.listeners))
	for _, listener := range s.listeners {
		if address := listener.Address(); address != "" {
			addresses = append(addresses, address)
		}
	}
	return addresses
}

// Start binds every listener. A listener that fails to start closes the ones
// already started, so a failed Start leaves nothing bound.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	switch {
	case s.started:
		s.mu.Unlock()
		return errors.New("server is already started")
	case s.startDone != nil:
		s.mu.Unlock()
		return errors.New("server is already starting")
	case s.closing:
		s.mu.Unlock()
		return errors.New("server is closing or closed")
	}
	done := make(chan struct{})
	s.startDone = done
	s.mu.Unlock()

	err := s.startListeners(ctx)

	s.mu.Lock()
	s.startDone = nil
	if err == nil {
		s.started = true
	}
	s.mu.Unlock()
	close(done)
	return err
}

func (s *Server) startListeners(ctx context.Context) error {
	started := make([]Listener, 0, len(s.listeners))
	for _, listener := range s.listeners {
		if err := listener.Start(ctx, s.Accept); err != nil {
			s.mu.Lock()
			s.closing = true
			s.mu.Unlock()
			for _, opened := range started {
				if closeErr := opened.Close(ctx); closeErr != nil {
					s.reportError(closeErr)
				}
			}
			s.closeServerState(ctx)
			return err
		}
		started = append(started, listener)
	}
	return nil
}

// Accept adopts one transport connection and returns the handler the transport
// must drive. It is the Acceptor a Listener is given, and may also be called
// directly to serve a connection the caller already has.
func (s *Server) Accept(conn ByteConn) ConnHandler {
	if s.isClosing() {
		s.closeConnection(conn, nil)
		return ConnHandler{
			OnData:  func([]byte) {},
			OnClose: func() {},
			OnError: func(err error) { s.reportError(err) },
		}
	}

	maxFrameLength := s.maxFrameLength
	decoder, err := protocol.NewClientMessageDecoder(&protocol.FrameOptions{MaxFrameLength: &maxFrameLength})
	if err != nil {
		// Unreachable: New already validated maxFrameLength against the same
		// bound the decoder enforces.
		s.reportError(err)
		s.closeConnection(conn, nil)
		return ConnHandler{OnData: func([]byte) {}, OnClose: func() {}, OnError: func(error) {}}
	}

	state := &connState{
		id:         newUUID(),
		conn:       conn,
		decoder:    decoder,
		sessionIDs: map[string]struct{}{},
		stage:      stageAwaitingHello,
	}
	// The timer is armed under the connection's own lock, so the callback —
	// which takes that lock before it touches anything — cannot observe the
	// field before it has been assigned.
	state.mu.Lock()
	state.handshakeTimer = time.AfterFunc(s.handshakeTimeout, func() {
		s.failProtocol(state, &protocol.ProtocolError{
			Code:    protocol.ErrorInvalidRequest,
			Message: "Handshake timeout",
		})
	})
	state.mu.Unlock()

	s.mu.Lock()
	s.connections[state] = struct{}{}
	s.mu.Unlock()

	return ConnHandler{
		OnData:  func(chunk []byte) { s.receive(state, chunk) },
		OnClose: func() { s.transportClosed(state) },
		OnError: func(err error) {
			s.reportError(err)
			s.closeConnection(conn, nil)
			s.disconnect(s.opContext(), state)
		},
	}
}

// Close stops every listener, closes every connection, and disposes every live
// session. It is idempotent: concurrent and repeated calls all wait for the
// first one and return its result.
func (s *Server) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closeDone != nil {
		done := s.closeDone
		s.mu.Unlock()
		<-done
		return s.closeErr
	}
	s.closing = true
	done := make(chan struct{})
	s.closeDone = done
	starting := s.startDone
	s.mu.Unlock()

	if starting != nil {
		<-starting
	}

	var firstErr error
	for _, listener := range s.listeners {
		if err := listener.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := s.closeServerState(ctx); err != nil && firstErr == nil {
		firstErr = err
	}

	s.mu.Lock()
	s.started = false
	s.closeErr = firstErr
	s.mu.Unlock()
	close(done)
	s.cancel()
	return firstErr
}

func (s *Server) receive(state *connState, chunk []byte) {
	if state.terminal() {
		return
	}
	messages, err := state.decoder.Push(chunk)
	if err != nil {
		s.failProtocol(state, s.toProtocolError(err))
		return
	}
	for _, message := range messages {
		if state.terminal() {
			return
		}
		s.dispatch(state, message)
	}
}

// dispatch routes one decoded client message.
//
// DIVERGENCE (deliberate): pi runs the handshake as a promise and chains any
// message that arrives during it onto that promise. Here the handshake runs
// inline on the read goroutine, so nothing can arrive during it — the transport
// serialises OnData, and the peer has nothing worth saying before its hello is
// answered anyway. Requests after the handshake still get a goroutine each, so
// a slow command cannot hold up a fast one behind it.
func (s *Server) dispatch(state *connState, message protocol.ClientMessage) {
	switch msg := message.(type) {
	case *protocol.ClientHello:
		state.mu.Lock()
		first := state.stage == stageAwaitingHello
		state.mu.Unlock()
		if !first {
			s.failProtocol(state, &protocol.ProtocolError{
				Code:    protocol.ErrorInvalidRequest,
				Message: "hello may only be sent as the first message",
			})
			return
		}
		s.finishHandshake(state, msg)

	case *protocol.RequestEnvelope:
		state.mu.Lock()
		stage := state.stage
		state.mu.Unlock()
		switch stage {
		case stageAwaitingHello:
			s.failProtocol(state, &protocol.ProtocolError{
				Code:    protocol.ErrorInvalidRequest,
				Message: "The first client message must be hello",
			})
		case stageReady:
			go s.handleRequest(state, msg)
		}
	}
}

func (s *Server) finishHandshake(state *connState, hello *protocol.ClientHello) {
	if !s.authenticate(hello) {
		s.failProtocol(state, &protocol.ProtocolError{Code: protocol.ErrorAuth, Message: "Authentication failed"})
		return
	}
	if !protocol.IsSupportedVersion(hello.Version) {
		s.failProtocol(state, &protocol.ProtocolError{
			Code: protocol.ErrorVersion,
			Message: fmt.Sprintf("Unsupported protocol version %d; expected %d",
				hello.Version, protocol.ProtocolVersion),
		})
		return
	}

	ctx := s.opContext()
	snapshot, err := s.snapshots.get(ctx, nil, state)
	if err != nil {
		s.failProtocol(state, s.toProtocolError(err))
		return
	}

	state.mu.Lock()
	abandon := state.disconnected || state.stage != stageAwaitingHello
	state.mu.Unlock()
	if s.isClosing() || abandon || state.conn.Closed() {
		return
	}

	sent := s.sendMessage(state, &protocol.ServerHello{
		Type:         "hello",
		Version:      protocol.ProtocolVersion,
		ConnectionID: state.id,
		Snapshot:     *snapshot,
	})
	if !sent {
		return
	}

	state.mu.Lock()
	promoted := !state.disconnected && state.stage == stageAwaitingHello
	if promoted {
		state.handshakeComplete = true
		state.stage = stageReady
		state.handshakeTimer.Stop()
	}
	state.mu.Unlock()
	if !promoted {
		return
	}

	// A change that landed while the snapshot was being built would otherwise
	// be invisible to this client until the next broadcast, because the
	// broadcast that carried it went out before the connection was ready.
	if snapshot.Revision == s.snapshots.currentRevision() {
		return
	}
	current, err := s.snapshots.get(ctx, nil, state)
	if err != nil {
		s.failProtocol(state, s.toProtocolError(err))
		return
	}
	s.sendMessage(state, &protocol.EventEnvelope{
		Type:  "event",
		Event: &protocol.ServerSnapshotEvent{Type: "server_snapshot", Snapshot: *current},
	})
}

func (s *Server) authenticate(hello *protocol.ClientHello) bool {
	digest := sha256.Sum256([]byte(hello.Token))
	return subtle.ConstantTimeCompare(digest[:], s.expectedTokenDigest[:]) == 1
}

func (s *Server) handleRequest(state *connState, envelope *protocol.RequestEnvelope) {
	result, err := s.sessions.executeCommand(s.opContext(), state, envelope.Request)
	if err != nil {
		s.sendMessage(state, &protocol.ResponseEnvelope{
			Type:  "response",
			ID:    envelope.ID,
			OK:    false,
			Error: s.toProtocolError(err),
		})
		return
	}
	s.sendMessage(state, &protocol.ResponseEnvelope{
		Type:   "response",
		ID:     envelope.ID,
		OK:     true,
		Result: result,
	})
}

func (s *Server) transportClosed(state *connState) {
	state.mu.Lock()
	drain := !state.disconnected && state.stage != stageClosing
	state.mu.Unlock()
	if drain {
		// End reports a stream that stopped mid-frame. The connection is going
		// away either way, so this only records the truncation.
		if err := state.decoder.End(); err != nil {
			s.reportError(err)
		}
	}
	s.disconnect(s.opContext(), state)
}

func (s *Server) disconnect(ctx context.Context, state *connState) {
	state.mu.Lock()
	if state.disconnected {
		state.mu.Unlock()
		return
	}
	handshakeComplete := state.handshakeComplete
	state.disconnected = true
	state.stage = stageClosed
	state.handshakeTimer.Stop()
	state.mu.Unlock()

	s.mu.Lock()
	delete(s.connections, state)
	s.mu.Unlock()

	s.sessions.disconnect(ctx, state)
	if !s.isClosing() && handshakeComplete {
		s.snapshots.broadcast()
	}
}

// sendMessage encodes and queues one server message, reporting whether it was
// accepted. A message that cannot be encoded or queued is terminal for the
// connection: the peer's view of the stream is already incomplete.
func (s *Server) sendMessage(state *connState, message protocol.ServerMessage) bool {
	state.mu.Lock()
	disconnected := state.disconnected
	state.mu.Unlock()
	if disconnected || state.conn.Closed() {
		return false
	}

	maxFrameLength := s.maxFrameLength
	frame, err := protocol.EncodeServerMessage(message, &protocol.FrameOptions{MaxFrameLength: &maxFrameLength})
	if err == nil {
		err = state.conn.Send(frame)
	}
	if err != nil {
		s.reportError(err)
		s.closeConnection(state.conn, nil)
		s.disconnect(s.opContext(), state)
		return false
	}
	return true
}

// failProtocol reports a protocol violation and closes the connection, writing
// the hello_error as the connection's final bytes so the peer learns why.
func (s *Server) failProtocol(state *connState, protocolError *protocol.ProtocolError) {
	state.mu.Lock()
	if state.terminalLocked() {
		state.mu.Unlock()
		return
	}
	state.stage = stageClosing
	state.handshakeTimer.Stop()
	state.mu.Unlock()

	maxFrameLength := s.maxFrameLength
	frame, err := protocol.EncodeServerMessage(
		&protocol.ServerHelloError{Type: "hello_error", Error: *protocolError},
		&protocol.FrameOptions{MaxFrameLength: &maxFrameLength},
	)
	if err != nil {
		s.reportError(err)
		frame = nil
	}
	s.closeConnection(state.conn, frame)
	s.disconnect(s.opContext(), state)
}

func (s *Server) closeServerState(ctx context.Context) error {
	s.mu.Lock()
	connections := make([]*connState, 0, len(s.connections))
	for state := range s.connections {
		connections = append(connections, state)
	}
	s.mu.Unlock()

	for _, state := range connections {
		state.mu.Lock()
		state.stage = stageClosing
		state.handshakeTimer.Stop()
		state.mu.Unlock()
	}
	for _, state := range connections {
		s.closeConnection(state.conn, nil)
	}
	for _, state := range connections {
		s.disconnect(ctx, state)
	}

	// Broadcasts are already no-ops once closing is set; waiting only makes
	// sure none is still touching the backend when Close returns.
	s.snapshots.wait()
	err := s.sessions.close(ctx)

	s.mu.Lock()
	clear(s.connections)
	s.mu.Unlock()
	return err
}

func (s *Server) closeConnection(conn ByteConn, finalChunk []byte) {
	if err := conn.Close(finalChunk); err != nil {
		s.reportError(err)
	}
}

// toProtocolError decides what a failure is allowed to tell the peer.
func (s *Server) toProtocolError(err error) *protocol.ProtocolError {
	var serverError *Error
	if errors.As(err, &serverError) && serverError.crossesProtocolBoundary() {
		return &protocol.ProtocolError{
			Code:    serverError.Code,
			Message: serverError.Message,
			Details: serverError.Details,
		}
	}
	var validationError *protocol.ValidationError
	if errors.As(err, &validationError) {
		return &protocol.ProtocolError{Code: protocol.ErrorInvalidRequest, Message: validationError.Error()}
	}
	var frameError *protocol.FrameError
	if errors.As(err, &frameError) {
		return &protocol.ProtocolError{Code: protocol.ErrorInvalidRequest, Message: frameError.Error()}
	}
	s.reportError(err)
	return &protocol.ProtocolError{Code: protocol.ErrorInvalidRequest, Message: "Internal server error"}
}

// reportError hands a failure to the error observer. An observer that panics
// cannot affect server state.
func (s *Server) reportError(err error) {
	if s.onError == nil || err == nil {
		return
	}
	defer func() { _ = recover() }()
	s.onError(err)
}

func (s *Server) isClosing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing
}

// opContext is the context backend and runtime calls run under.
func (s *Server) opContext() context.Context { return s.ctx }

// readyConnections snapshots the connections a broadcast may reach.
//
// DIVERGENCE (deliberate): pi iterates a Set, so it visits connections in the
// order they were accepted; a Go map does not. The order is not observable —
// every connection in one pass carries the same revision, and the protocol
// makes no promise about how two different connections are interleaved — and
// keeping an insertion-ordered list would add a removal scan to every
// disconnect for nothing.
func (s *Server) readyConnections() []*connState {
	s.mu.Lock()
	states := make([]*connState, 0, len(s.connections))
	for state := range s.connections {
		states = append(states, state)
	}
	s.mu.Unlock()

	ready := states[:0]
	for _, state := range states {
		if state.ready() {
			ready = append(ready, state)
		}
	}
	return ready
}
