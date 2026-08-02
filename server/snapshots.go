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
	backend Backend
	queue   serialQueue

	mu       sync.Mutex
	revision int64
}

func newSnapshotPublisher(srv *Server, backend Backend) *snapshotPublisher {
	return &snapshotPublisher{srv: srv, backend: backend}
}

// currentRevision is the revision of the most recent completed broadcast.
func (p *snapshotPublisher) currentRevision() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.revision
}

// get builds the server snapshot as conn would see it. models, when non-nil,
// is reused instead of asking the backend again; conn may be nil, which marks
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
		models, err = p.backend.ListModels(ctx)
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

// broadcast queues one snapshot pass. It never blocks.
func (p *snapshotPublisher) broadcast() {
	p.queue.Go(func() {
		if err := p.perform(); err != nil {
			p.srv.reportError(err)
		}
	})
}

// wait blocks until every queued broadcast has run.
func (p *snapshotPublisher) wait() { p.queue.Wait() }

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

	models, err := p.backend.ListModels(ctx)
	if err != nil {
		return err
	}
	for _, conn := range ready {
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
