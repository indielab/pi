// Package servertest is an in-memory Backend and a raw protocol client, used
// to exercise the server package and its transports.
//
// It is the Go counterpart of pi's packages/server/src/testing tree. That tree
// is published upstream; this one is internal, because the Go port has no
// reason to make a fake backend part of its public API — the tests that need it
// all live under server/.
package servertest

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/sky-valley/pi/protocol"
	"github.com/sky-valley/pi/server"
)

// Token is the shared secret every test server is configured with.
const Token = "server-conformance-token"

// Model is the single model the fake backend publishes.
var Model = protocol.ModelMetadata{
	Provider:                "test",
	ID:                      "small",
	Name:                    "Test Small",
	API:                     "test-api",
	Reasoning:               true,
	Input:                   []protocol.ModelInputModality{protocol.InputText, protocol.InputImage},
	ContextWindow:           16_000,
	MaxTokens:               2_000,
	Cost:                    protocol.ModelCost{},
	SupportedThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingOff, protocol.ThinkingMedium, protocol.ThinkingHigh},
	Authenticated:           true,
}

// ModelRef points at Model.
func ModelRef() protocol.ModelRef {
	return protocol.ModelRef{Provider: Model.Provider, ID: Model.ID}
}

// Gate is a one-shot rendezvous: a caller reaching it announces itself on
// Entered and blocks until Release is closed.
type Gate struct {
	Entered chan struct{}
	Release chan struct{}

	once sync.Once
}

// NewGate returns an unentered, unreleased gate.
func NewGate() *Gate {
	return &Gate{Entered: make(chan struct{}), Release: make(chan struct{})}
}

func (g *Gate) enter() {
	g.once.Do(func() { close(g.Entered) })
	<-g.Release
}

// Open releases everyone waiting at the gate.
func (g *Gate) Open() { close(g.Release) }

type storedSession struct {
	mu       sync.Mutex
	snapshot protocol.SessionSnapshot
}

func (s *storedSession) get() protocol.SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneSnapshot(s.snapshot)
}

func cloneSnapshot(snapshot protocol.SessionSnapshot) protocol.SessionSnapshot {
	clone := snapshot
	clone.Transcript = append([]protocol.TranscriptItem(nil), snapshot.Transcript...)
	clone.QueuedSteer = append([]protocol.UserTranscriptItem(nil), snapshot.QueuedSteer...)
	return clone
}

// Backend is an in-memory server.Backend that records every runtime it hands
// out, so a test can inspect what the server did with them.
type Backend struct {
	mu            sync.Mutex
	sessions      map[string]*storedSession
	order         []string
	runtimes      map[string][]*Runtime
	locked        map[string]bool
	lastCreatedID string
	listGate      *Gate

	listSessionsHook func(call int) error
	listModelsHook   func(call int) error
	createIDOverride string
	listCalls        int
	modelCalls       int
}

// SetListSessionsHook installs a hook that runs at the top of ListSessions;
// returning an error fails the call. It is safe to call on a running server.
func (b *Backend) SetListSessionsHook(hook func(call int) error) {
	b.mu.Lock()
	b.listSessionsHook = hook
	b.mu.Unlock()
}

// SetListModelsHook installs a hook that runs at the top of ListModels.
func (b *Backend) SetListModelsHook(hook func(call int) error) {
	b.mu.Lock()
	b.listModelsHook = hook
	b.mu.Unlock()
}

// SetCreateSessionIDOverride rewrites the ID the backend persists, which is how
// a test makes a backend disobey its server-assigned ID.
func (b *Backend) SetCreateSessionIDOverride(id string) {
	b.mu.Lock()
	b.createIDOverride = id
	b.mu.Unlock()
}

var _ server.Backend = (*Backend)(nil)

// NewBackend returns an empty backend.
func NewBackend() *Backend {
	return &Backend{
		sessions: map[string]*storedSession{},
		runtimes: map[string][]*Runtime{},
		locked:   map[string]bool{},
	}
}

// Seed stores one idle session with default fields.
func (b *Backend) Seed(id string) {
	if id == "" {
		id = "session-1"
	}
	name := "Session " + id
	b.SeedWith(id, &name, "/tmp/pi-server-conformance", ModelRef(), protocol.ThinkingOff)
}

// SeedWith stores one idle session with explicit fields.
func (b *Backend) SeedWith(
	id string,
	name *string,
	cwd string,
	model protocol.ModelRef,
	thinkingLevel protocol.ThinkingLevel,
) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.sessions[id]; !ok {
		b.order = append(b.order, id)
	}
	b.sessions[id] = &storedSession{snapshot: protocol.SessionSnapshot{
		ID:            id,
		Name:          name,
		Cwd:           cwd,
		CreatedAt:     1,
		UpdatedAt:     1,
		Phase:         protocol.PhaseIdle,
		Model:         model,
		ThinkingLevel: thinkingLevel,
	}}
}

// DelayNextList makes the next ListSessions block at the returned gate.
func (b *Backend) DelayNextList() *Gate {
	gate := NewGate()
	b.mu.Lock()
	b.listGate = gate
	b.mu.Unlock()
	return gate
}

// ForceLock marks a session held by somebody the server cannot see, which is
// how a test provokes a session_locked failure from OpenSession.
func (b *Backend) ForceLock(id string) {
	b.mu.Lock()
	b.locked[id] = true
	b.mu.Unlock()
}

// Locked reports whether the backend still considers a session held.
func (b *Backend) Locked(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.locked[id]
}

// LastCreatedID is the server-assigned ID of the most recent CreateSession.
func (b *Backend) LastCreatedID() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastCreatedID
}

// Runtimes is every runtime ever handed out for a session, oldest first.
func (b *Backend) Runtimes(id string) []*Runtime {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]*Runtime(nil), b.runtimes[id]...)
}

// LatestRuntime is the most recent runtime handed out for a session.
func (b *Backend) LatestRuntime(id string) *Runtime {
	runtimes := b.Runtimes(id)
	if len(runtimes) == 0 {
		return nil
	}
	return runtimes[len(runtimes)-1]
}

func (b *Backend) ListSessions(context.Context) ([]protocol.SessionSummary, error) {
	b.mu.Lock()
	gate := b.listGate
	b.listGate = nil
	b.listCalls++
	call := b.listCalls
	hook := b.listSessionsHook
	b.mu.Unlock()

	if hook != nil {
		if err := hook(call); err != nil {
			return nil, err
		}
	}
	if gate != nil {
		gate.enter()
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	summaries := make([]protocol.SessionSummary, 0, len(b.order))
	for _, id := range b.order {
		stored := b.sessions[id]
		if stored == nil {
			continue
		}
		snapshot := stored.get()
		summary := snapshot.Summary()
		summary.Attached = false
		summary.Locked = b.locked[id]
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func (b *Backend) ListModels(context.Context) ([]protocol.ModelMetadata, error) {
	b.mu.Lock()
	b.modelCalls++
	call := b.modelCalls
	hook := b.listModelsHook
	b.mu.Unlock()
	if hook != nil {
		if err := hook(call); err != nil {
			return nil, err
		}
	}
	return []protocol.ModelMetadata{Model}, nil
}

func (b *Backend) CreateSession(_ context.Context, options server.CreateSessionOptions) (server.SessionRuntime, error) {
	b.mu.Lock()
	id := options.ID
	if b.createIDOverride != "" {
		id = b.createIDOverride
	}
	b.lastCreatedID = id
	_, exists := b.sessions[id]
	b.mu.Unlock()
	if exists {
		return nil, server.NewLockedError("Session already exists", nil)
	}

	cwd := "/tmp/pi-server-conformance"
	if options.Cwd != nil {
		cwd = *options.Cwd
	}
	model := ModelRef()
	if options.Model != nil {
		model = *options.Model
	}
	thinkingLevel := protocol.ThinkingOff
	if options.ThinkingLevel != nil {
		thinkingLevel = *options.ThinkingLevel
	}
	name := options.Name
	if name == nil {
		fallback := "Session " + id
		name = &fallback
	}
	b.SeedWith(id, name, cwd, model, thinkingLevel)
	return b.acquire(id)
}

func (b *Backend) OpenSession(_ context.Context, sessionID string) (server.SessionRuntime, error) {
	b.mu.Lock()
	_, exists := b.sessions[sessionID]
	locked := b.locked[sessionID]
	b.mu.Unlock()
	if !exists {
		return nil, server.NewNotFoundError("Unknown session: "+sessionID, nil)
	}
	if locked {
		return nil, server.NewLockedError("Session is locked: "+sessionID, nil)
	}
	return b.acquire(sessionID)
}

func (b *Backend) acquire(id string) (*Runtime, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	stored := b.sessions[id]
	if stored == nil {
		return nil, fmt.Errorf("unknown session: %s", id)
	}
	b.locked[id] = true
	runtime := &Runtime{
		backend:   b,
		stored:    stored,
		id:        id,
		listeners: map[int]func(server.RuntimeEvent){},
		disposed:  make(chan struct{}),
	}
	b.runtimes[id] = append(b.runtimes[id], runtime)
	return runtime, nil
}

func (b *Backend) release(id string) {
	b.mu.Lock()
	delete(b.locked, id)
	b.mu.Unlock()
}

type promptOutcome int

const (
	promptCompleted promptOutcome = iota
	promptAborted
)

// Runtime is one acquired fake session.
type Runtime struct {
	backend *Backend
	stored  *storedSession
	id      string

	mu           sync.Mutex
	listeners    map[int]func(server.RuntimeEvent)
	nextListener int
	disposeCount int
	steers       []server.SteerInput
	pending      chan promptOutcome

	disposed  chan struct{}
	closeOnce sync.Once
}

var _ server.SessionRuntime = (*Runtime)(nil)

// Disposed is closed after the first Dispose.
func (r *Runtime) Disposed() <-chan struct{} { return r.disposed }

// DisposeCount is how many times the server released this runtime.
func (r *Runtime) DisposeCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.disposeCount
}

// Steers is every steer this runtime accepted.
func (r *Runtime) Steers() []server.SteerInput {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]server.SteerInput(nil), r.steers...)
}

func (r *Runtime) Snapshot(context.Context) (*protocol.SessionSnapshot, error) {
	snapshot := r.stored.get()
	return &snapshot, nil
}

// StoredSnapshot is the runtime's current view, for assertions.
func (r *Runtime) StoredSnapshot() protocol.SessionSnapshot { return r.stored.get() }

func (r *Runtime) Phase() protocol.SessionPhase { return r.stored.get().Phase }

func (r *Runtime) Prompt(ctx context.Context, input server.PromptInput) error {
	if r.Phase() != protocol.PhaseIdle {
		return server.NewBusyError("A prompt is already running", nil)
	}
	done := make(chan promptOutcome, 1)
	r.mu.Lock()
	r.pending = done
	r.mu.Unlock()

	r.update(func(snapshot *protocol.SessionSnapshot) {
		snapshot.Phase = protocol.PhaseTurn
		snapshot.Transcript = append(snapshot.Transcript, &protocol.UserTranscriptItem{
			ID:        fmt.Sprintf("user-%d", snapshot.Revision+1),
			Role:      "user",
			Content:   []protocol.Content{&protocol.TextContent{Type: "text", Text: input.Text}},
			Timestamp: snapshot.Revision + 1,
		})
	})

	var outcome promptOutcome
	select {
	case outcome = <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	r.update(func(snapshot *protocol.SessionSnapshot) {
		snapshot.Phase = protocol.PhaseIdle
		item := &protocol.AssistantTranscriptItem{
			ID:        fmt.Sprintf("assistant-%d", snapshot.Revision+1),
			Role:      "assistant",
			Model:     snapshot.Model,
			Timestamp: snapshot.Revision + 1,
		}
		if outcome == promptCompleted {
			stop := protocol.StopStop
			item.Content = []protocol.Content{&protocol.TextContent{Type: "text", Text: "reply:" + input.Text}}
			item.Status = protocol.AssistantComplete
			item.StopReason = &stop
		} else {
			aborted := protocol.StopAborted
			item.Content = []protocol.Content{&protocol.TextContent{Type: "text", Text: ""}}
			item.Status = protocol.AssistantAborted
			item.StopReason = &aborted
		}
		snapshot.Transcript = append(snapshot.Transcript, item)
	})

	r.mu.Lock()
	r.pending = nil
	r.mu.Unlock()
	return nil
}

func (r *Runtime) Steer(_ context.Context, input server.SteerInput) error {
	if r.Phase() == protocol.PhaseIdle {
		return server.NewBusyError("There is no active prompt to steer", nil)
	}
	r.mu.Lock()
	r.steers = append(r.steers, input)
	r.mu.Unlock()
	r.update(func(snapshot *protocol.SessionSnapshot) {
		snapshot.QueuedSteerCount++
		snapshot.QueuedSteer = append(snapshot.QueuedSteer, protocol.UserTranscriptItem{
			ID:        fmt.Sprintf("steer-%d", snapshot.Revision+1),
			Role:      "user",
			Content:   []protocol.Content{&protocol.TextContent{Type: "text", Text: input.Text}},
			Timestamp: snapshot.Revision + 1,
		})
	})
	return nil
}

func (r *Runtime) Abort(context.Context) error {
	r.mu.Lock()
	pending := r.pending
	r.mu.Unlock()
	if pending == nil {
		return server.NewBusyError("There is no active prompt to abort", nil)
	}
	select {
	case pending <- promptAborted:
	default:
	}
	return nil
}

func (r *Runtime) SetModel(_ context.Context, model protocol.ModelRef) error {
	if r.Phase() != protocol.PhaseIdle {
		return server.NewBusyError("", nil)
	}
	r.update(func(snapshot *protocol.SessionSnapshot) { snapshot.Model = model })
	return nil
}

func (r *Runtime) SetThinking(_ context.Context, level protocol.ThinkingLevel) error {
	if r.Phase() != protocol.PhaseIdle {
		return server.NewBusyError("", nil)
	}
	r.update(func(snapshot *protocol.SessionSnapshot) { snapshot.ThinkingLevel = level })
	return nil
}

func (r *Runtime) Subscribe(listener func(server.RuntimeEvent)) server.Unsubscribe {
	r.mu.Lock()
	r.nextListener++
	id := r.nextListener
	r.listeners[id] = listener
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.listeners, id)
		r.mu.Unlock()
	}
}

func (r *Runtime) Dispose(context.Context) error {
	r.mu.Lock()
	r.disposeCount++
	r.mu.Unlock()
	r.backend.release(r.id)
	r.closeOnce.Do(func() { close(r.disposed) })
	return nil
}

// SetPhase forces the session's phase without emitting anything.
func (r *Runtime) SetPhase(phase protocol.SessionPhase) {
	r.stored.mu.Lock()
	r.stored.snapshot.Phase = phase
	r.stored.mu.Unlock()
}

// FinishPrompt completes the in-flight prompt.
func (r *Runtime) FinishPrompt() error {
	r.mu.Lock()
	pending := r.pending
	r.mu.Unlock()
	if pending == nil {
		return errors.New("no prompt is pending")
	}
	select {
	case pending <- promptCompleted:
	default:
	}
	return nil
}

// EmitProgress publishes one progress event.
func (r *Runtime) EmitProgress(progress protocol.TranscriptProgress) {
	r.emit(server.RuntimeEvent{Type: server.RuntimeProgressEvent, Progress: progress})
}

// EmitError publishes a terminal runtime failure.
func (r *Runtime) EmitError(err *server.Error) {
	r.emit(server.RuntimeEvent{Type: server.RuntimeErrorEvent, Err: err})
}

// EmitSnapshot tells the server the snapshot changed.
func (r *Runtime) EmitSnapshot() {
	r.emit(server.RuntimeEvent{Type: server.RuntimeSnapshotEvent})
}

func (r *Runtime) emit(event server.RuntimeEvent) {
	r.mu.Lock()
	listeners := make([]func(server.RuntimeEvent), 0, len(r.listeners))
	for _, listener := range r.listeners {
		listeners = append(listeners, listener)
	}
	r.mu.Unlock()
	for _, listener := range listeners {
		listener(event)
	}
}

func (r *Runtime) update(mutate func(*protocol.SessionSnapshot)) {
	r.stored.mu.Lock()
	snapshot := cloneSnapshot(r.stored.snapshot)
	mutate(&snapshot)
	snapshot.Revision++
	snapshot.UpdatedAt++
	r.stored.snapshot = snapshot
	r.stored.mu.Unlock()
	r.EmitSnapshot()
}
