package unix

import (
	"bytes"
	"errors"
	"net"
	"sync"
	"time"
)

// errConnClosed is what a caller gets for writing to a connection that is
// closing or closed.
var errConnClosed = errors.New("Unix connection is closed")

// errPendingLimit is what a caller gets when a peer is too slow to keep up.
var errPendingLimit = errors.New("Unix connection exceeded its pending byte limit")

// conn is one accepted socket presented as a server.ByteConn.
//
// Writes go through a single goroutine, so chunks reach the peer in the order
// Send was called and no caller ever blocks on the socket. A peer that stops
// reading is bounded by maxPendingBytes rather than by memory.
type conn struct {
	socket               net.Conn
	gracefulCloseTimeout time.Duration
	maxPendingBytes      int

	notify   chan struct{}
	closedCh chan struct{}

	mu           sync.Mutex
	queue        [][]byte
	pendingBytes int
	closing      bool
	closed       bool
	finalChunk   []byte
	closeTimer   *time.Timer
	writeErr     error
}

func newConn(socket net.Conn, gracefulCloseTimeout time.Duration, maxPendingBytes int) *conn {
	c := &conn{
		socket:               socket,
		gracefulCloseTimeout: gracefulCloseTimeout,
		maxPendingBytes:      maxPendingBytes,
		notify:               make(chan struct{}, 1),
		closedCh:             make(chan struct{}),
	}
	go c.writeLoop()
	return c
}

// Closed reports whether the socket has finished closing. A connection that is
// draining its final bytes is not yet closed, which is what stops the server
// from queuing more onto it.
func (c *conn) Closed() bool {
	select {
	case <-c.closedCh:
		return true
	default:
		return false
	}
}

func (c *conn) Send(chunk []byte) error {
	c.mu.Lock()
	if c.closed || c.closing {
		c.mu.Unlock()
		return errConnClosed
	}
	if c.pendingBytes+len(chunk) > c.maxPendingBytes {
		c.mu.Unlock()
		return errPendingLimit
	}
	c.pendingBytes += len(chunk)
	c.queue = append(c.queue, bytes.Clone(chunk))
	c.mu.Unlock()
	c.wake()
	return nil
}

// Close flushes whatever is queued, writes finalChunk last, and shuts the
// socket down.
//
// DIVERGENCE (deliberate): pi's close resolves only once the socket has fully
// closed. Waiting for that here would deadlock: the server closes a connection
// from the goroutine that is reading it, and that goroutine is the one that
// observes the close. So Close returns once the shutdown is committed — the
// final chunk is still written before the socket goes away, and the graceful
// timeout still tears down a peer that will not finish.
func (c *conn) Close(finalChunk []byte) error {
	c.mu.Lock()
	if c.closed || c.closing {
		c.mu.Unlock()
		return nil
	}
	c.closing = true
	c.finalChunk = bytes.Clone(finalChunk)
	c.closeTimer = time.AfterFunc(c.gracefulCloseTimeout, func() { _ = c.socket.Close() })
	c.mu.Unlock()
	c.wake()
	return nil
}

// markClosed records that the socket is gone. The listener's read loop calls it
// when the socket ends, whichever side ended it.
func (c *conn) markClosed() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.closing = true
	timer := c.closeTimer
	c.closeTimer = nil
	c.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
	close(c.closedCh)
	c.wake()
}

func (c *conn) wake() {
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

func (c *conn) writeLoop() {
	for {
		c.mu.Lock()
		switch {
		case c.closed:
			c.mu.Unlock()
			return
		case len(c.queue) > 0:
			chunk := c.queue[0]
			c.queue = c.queue[1:]
			c.mu.Unlock()
			if _, err := c.socket.Write(chunk); err != nil {
				c.recordWriteError(err)
				return
			}
			c.mu.Lock()
			c.pendingBytes -= len(chunk)
			c.mu.Unlock()
			continue
		case c.closing:
			final := c.finalChunk
			c.finalChunk = nil
			c.mu.Unlock()
			if len(final) > 0 {
				if _, err := c.socket.Write(final); err != nil {
					c.recordWriteError(err)
					return
				}
			}
			c.halfClose()
			return
		}
		c.mu.Unlock()
		<-c.notify
	}
}

// halfClose sends FIN and leaves the read side open, so a peer still reading
// receives everything that was written before the socket disappears. The
// graceful timer set by Close tears the socket down if the peer never answers.
func (c *conn) halfClose() {
	if half, ok := c.socket.(interface{ CloseWrite() error }); ok {
		if err := half.CloseWrite(); err == nil {
			return
		}
	}
	_ = c.socket.Close()
}

func (c *conn) recordWriteError(err error) {
	c.mu.Lock()
	if c.writeErr == nil {
		c.writeErr = err
	}
	c.mu.Unlock()
	_ = c.socket.Close()
}
