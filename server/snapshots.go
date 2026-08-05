package server

import (
	"context"
	"sync"

	"github.com/sky-valley/pi/protocol"
)

// snapshotPublisher owns the server-wide snapshot and its revision counter.
//
// Every broadcast is a separate pass that bumps the revision once and sends the
// same revision to every ready connection, so a client can order two snapshots
// it received on different connections. Passes never overlap: a client that
// sees revision 2 has already seen revision 1.
type snapshotPublisher struct {
	srv     *Server
	service Service

	mu       sync.Mutex
	revision int64
	running  bool
	pending  bool
	idle     chan struct{}
}

func newSnapshotPublisher(srv *Server, service Service) *snapshotPublisher {
	return &snapshotPublisher{srv: srv, service: service}
}

// currentRevision is the revision of the most recent completed broadcast.
func (p *snapshotPublisher) currentRevision() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.revision
}

// get builds the server snapshot as conn would see it. models, when non-nil,
// is reused instead of asking the service again; conn may be nil, which marks
// every session unattached.
func (p *snapshotPublisher) get(
	ctx context.Context,
	models []protocol.ModelMetadata,
	conn *connState,
) (*protocol.ServerSnapshot, error) {
	// The revision is read before the sessions are listed, not after. pi gets
	// that ordering from evaluating its object literal left to right, and it is
	// load-bearing: a snapshot must be stamped with the revision it was started
	// at, so a change that lands while it is being built leaves it visibly
	// stale and the handshake knows to send a catch-up.
	revision := p.currentRevision()
	sessions, err := p.srv.sessions.listSummaries(ctx, conn)
	if err != nil {
		return nil, err
	}
	if models == nil {
		models, err = p.service.ListModels(ctx)
		if err != nil {
			return nil, err
		}
	}
	return &protocol.ServerSnapshot{
		ServerID:        p.srv.id,
		ProtocolVersion: protocol.ProtocolVersion,
		Revision:        revision,
		Sessions:        sessions,
		Models:          models,
	}, nil
}

// broadcast asks for a snapshot pass. It never blocks.
//
// DIVERGENCE (deliberate): pi chains every broadcast onto a promise, so N calls
// run N passes. Every pass does the same thing — read the service once, then
// build and send the current state to each ready connection — so a pass that
// has not started yet is indistinguishable from the one already running, and
// at most one is kept waiting behind it. Without that, a peer pipelining
// attach and detach enqueues passes faster than they retire, each of them
// costing O(connections × live sessions) service calls. What a client sees is
// fewer server_snapshot events carrying the same information: the revision
// still only moves forward, and every snapshot is still whole.
func (p *snapshotPublisher) broadcast() {
	p.mu.Lock()
	if p.running {
		p.pending = true
		p.mu.Unlock()
		return
	}
	p.running = true
	p.mu.Unlock()
	go p.run()
}

func (p *snapshotPublisher) run() {
	for {
		p.performSafely()

		p.mu.Lock()
		if !p.pending {
			p.running = false
			if p.idle != nil {
				close(p.idle)
				p.idle = nil
			}
			p.mu.Unlock()
			return
		}
		p.pending = false
		p.mu.Unlock()
	}
}

// wait blocks until no pass is running or waiting, giving up when ctx is done.
func (p *snapshotPublisher) wait(ctx context.Context) error {
	for {
		p.mu.Lock()
		if !p.running {
			p.mu.Unlock()
			return nil
		}
		if p.idle == nil {
			p.idle = make(chan struct{})
		}
		idle := p.idle
		p.mu.Unlock()

		select {
		case <-idle:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// performSafely runs one pass behind a panic barrier: the pass reads the
// Service from a goroutine nobody outside this package can see, and a panic
// there would take the process rather than the broadcast.
func (p *snapshotPublisher) performSafely() {
	defer func() {
		if recovered := recover(); recovered != nil {
			p.srv.reportError(panicError(recovered, "broadcasting the server snapshot"))
		}
	}()
	if err := p.perform(); err != nil {
		p.srv.reportError(err)
	}
}

func (p *snapshotPublisher) perform() error {
	ready := p.srv.readyConnections()
	if len(ready) == 0 || p.srv.isClosing() {
		return nil
	}
	ctx := p.srv.opContext()

	p.mu.Lock()
	p.revision++
	revision := p.revision
	p.mu.Unlock()

	models, err := p.service.ListModels(ctx)
	if err != nil {
		return err
	}
	for _, conn := range ready {
		// Abandoning the pass on the first failure is pi's behaviour, not an
		// oversight: its loop awaits each snapshot in turn, so a service that
		// fails for one connection ends the pass for the rest too. The
		// revision it consumed is simply never delivered, and the next pass
		// carries a later one.
		snapshot, err := p.get(ctx, models, conn)
		if err != nil {
			return err
		}
		snapshot.Revision = revision
		p.srv.sendMessage(conn, &protocol.EventEnvelope{
			Type:  "event",
			Event: &protocol.ServerSnapshotEvent{Type: "server_snapshot", Snapshot: *snapshot},
		})
	}
	return nil
}
