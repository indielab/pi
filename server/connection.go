package server

import (
	"sync"
	"time"

	"github.com/sky-valley/pi/protocol"
)

// ByteConn is a connected, ordered byte sink handed to the Server by a
// transport.
//
// Send must be safe to call from several goroutines and must preserve
// invocation order: two chunks sent one after another arrive in that order. It
// should enqueue rather than block on the peer — the Server calls it from the
// goroutine that produced the event, and a transport that blocks there stalls
// the session that produced it.
type ByteConn interface {
	// Closed reports whether the connection has finished closing.
	Closed() bool
	// Send queues one chunk. A returned error is terminal for the connection.
	Send(chunk []byte) error
	// Close shuts the connection down, writing finalChunk first when it is not
	// nil. Repeated calls must be harmless.
	Close(finalChunk []byte) error
}

// ConnHandler receives everything a transport observes on one connection.
//
// DIVERGENCE (deliberate): pi declares this as an interface a transport
// implements. A struct of funcs matches client.TransportHandlers and lets a
// transport close over its socket without declaring a type.
type ConnHandler struct {
	// OnData delivers an inbound chunk. Calls must be serialised: the Server
	// drives a stateful frame decoder from them.
	OnData func(chunk []byte)
	// OnClose reports an orderly terminal close.
	OnClose func()
	// OnError reports a terminal transport failure.
	OnError func(err error)
}

// Acceptor is what a Listener calls for each accepted connection.
type Acceptor func(conn ByteConn) ConnHandler

// connStage is where a connection sits in its handshake lifecycle.
type connStage int

const (
	stageAwaitingHello connStage = iota
	stageReady
	stageClosing
	stageClosed
)

// connState is one accepted connection.
//
// DIVERGENCE (deliberate): pi has a fifth "handshaking" stage because its
// handshake is a promise that later messages chain onto. Here the handshake
// runs inline on the connection's read goroutine, so no message can be
// dispatched while it is in flight and the stage cannot be observed. Dropping
// it removes the queue-behind-the-handshake path entirely.
type connState struct {
	id      string
	conn    ByteConn
	decoder *protocol.ClientMessageDecoder

	// mu guards everything below. The decoder is not guarded: it is touched
	// only from the read goroutine, which is exactly the contract it declares.
	mu                sync.Mutex
	sessionIDs        map[string]struct{}
	stage             connStage
	disconnected      bool
	handshakeComplete bool
	handshakeTimer    *time.Timer
}

// terminal reports whether the connection can no longer process messages.
func (c *connState) terminal() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.terminalLocked()
}

func (c *connState) terminalLocked() bool {
	return c.disconnected || c.stage == stageClosing || c.stage == stageClosed
}

// ready reports whether the connection completed its handshake and is still
// usable.
func (c *connState) ready() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stage == stageReady && !c.disconnected
}

// attached reports whether this connection holds sessionID.
func (c *connState) attached(sessionID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.sessionIDs[sessionID]
	return ok
}
