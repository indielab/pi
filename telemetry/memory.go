package telemetry

import (
	"fmt"
	"reflect"
	"sync"
)

// RecordedEvent is one point-in-time event captured on a recorded span
// (pi RecordedTelemetryEvent).
type RecordedEvent struct {
	Name       string
	Attributes SpanAttributes
}

// RecordedSpan is a detached snapshot of one span an InMemoryContext observed
// (pi RecordedTelemetrySpan).
type RecordedSpan struct {
	// ID is assigned in span-start order, from 1.
	ID int
	// ParentID is the enclosing span's ID, or 0 for a root span. pi models
	// this as `number | null`; IDs start at 1, so 0 is unambiguous here.
	ParentID int
	Name     string
	// Attributes are the start attributes merged with every SetAttributes
	// call the span accepted.
	Attributes SpanAttributes
	Events     []RecordedEvent
	Status     SpanStatus
	Settled    bool
	// EndSequence orders settlement across spans, from 1, and is 0 while the
	// span is still open. pi omits the key entirely in that case.
	EndSequence int
}

// InMemoryContext is the backend-neutral reference Context that records spans
// in process memory (pi InMemoryTelemetryContext). Create a fresh one to
// isolate tests or independent recording scopes.
//
// Unlike pi's single-threaded original this is safe for concurrent use: Go
// callers routinely start sibling spans from several goroutines, so the
// recorder takes a mutex around its state. The lock is never held across a
// callback — a callback starting a child span would otherwise deadlock.
type InMemoryContext struct {
	mu              sync.Mutex
	spans           []*recordedSpan
	nextSpanID      int
	nextEndSequence int
}

// NewInMemoryContext returns an empty recorder.
func NewInMemoryContext() *InMemoryContext {
	return &InMemoryContext{nextSpanID: 1, nextEndSequence: 1}
}

// recordedSpan is the mutable state behind a live span
// (pi MutableRecordedTelemetrySpan).
type recordedSpan struct {
	id             int
	parentID       int
	name           string
	attributes     SpanAttributes
	events         []RecordedEvent
	status         SpanStatus
	explicitStatus bool
	settled        bool
	endSequence    int
}

// StartSpan opens a root span on this recorder.
func (c *InMemoryContext) StartSpan(options SpanOptions, callback func(span Span) error) error {
	return c.startSpan(nil, options, callback)
}

// Spans returns detached snapshots in span-start order. Mutating the result
// cannot reach the recorder's state.
func (c *InMemoryContext) Spans() []RecordedSpan {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]RecordedSpan, 0, len(c.spans))
	for _, s := range c.spans {
		events := make([]RecordedEvent, 0, len(s.events))
		for _, e := range s.events {
			events = append(events, RecordedEvent{Name: e.Name, Attributes: copyAttributes(e.Attributes)})
		}
		out = append(out, RecordedSpan{
			ID:          s.id,
			ParentID:    s.parentID,
			Name:        s.name,
			Attributes:  copyAttributes(s.attributes),
			Events:      events,
			Status:      copyStatus(s.status),
			Settled:     s.settled,
			EndSequence: s.endSequence,
		})
	}
	return out
}

func (c *InMemoryContext) startSpan(parent *recordedSpan, options SpanOptions, callback func(span Span) error) error {
	// pi: a child started under an already-settled parent is not recorded, it
	// runs against the noop context instead.
	if parent != nil {
		c.mu.Lock()
		settled := parent.settled
		c.mu.Unlock()
		if settled {
			return NoopContext.StartSpan(options, callback)
		}
	}

	span := &recordedSpan{
		name:       options.Name,
		attributes: copyAttributes(options.Attributes),
		status:     SpanStatus{Code: StatusOK},
	}
	c.mu.Lock()
	span.id = c.nextSpanID
	c.nextSpanID++
	if parent != nil {
		span.parentID = parent.id
	}
	c.spans = append(c.spans, span)
	c.mu.Unlock()

	live := &inMemorySpan{ctx: c, span: span}

	// pi settles the span on a synchronous throw and rethrows. Go's analog is
	// a panic: settle it as a failure, then let it keep unwinding. settle is
	// idempotent, so the normal path below is unaffected.
	defer func() {
		if r := recover(); r != nil {
			c.settle(span, true, fmt.Errorf("panic: %v", r))
			panic(r)
		}
	}()

	err := callback(live)
	c.settle(span, err != nil, err)
	return err
}

// settle closes a span exactly once, stamping the end-sequence that orders
// settlement across spans (pi settleSpan).
func (c *InMemoryContext) settle(span *recordedSpan, failed bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if span.settled {
		return
	}
	if failed && !span.explicitStatus {
		span.status = automaticErrorStatus(err)
	}
	span.settled = true
	span.endSequence = c.nextEndSequence
	c.nextEndSequence++
}

// automaticErrorStatus derives the status of a span that failed without one
// being set explicitly. pi reads a JS Error's name and message; Go's nearest
// equivalent of `error.name` is the error's concrete type.
func automaticErrorStatus(err error) SpanStatus {
	if err == nil {
		return SpanStatus{Code: StatusError}
	}
	return SpanStatus{
		Code:  StatusError,
		Error: &SpanError{Name: fmt.Sprintf("%T", err), Message: err.Error()},
	}
}

// inMemorySpan is the live Span handed to a callback.
type inMemorySpan struct {
	ctx  *InMemoryContext
	span *recordedSpan
}

func (s *inMemorySpan) StartSpan(options SpanOptions, callback func(span Span) error) error {
	return s.ctx.startSpan(s.span, options, callback)
}

// Recording is passive: every mutator below is a no-op once the span settled,
// matching pi's guards.

func (s *inMemorySpan) AddEvent(name string, attributes SpanAttributes) {
	s.ctx.mu.Lock()
	defer s.ctx.mu.Unlock()
	if s.span.settled {
		return
	}
	s.span.events = append(s.span.events, RecordedEvent{Name: name, Attributes: copyAttributes(attributes)})
}

func (s *inMemorySpan) SetAttributes(attributes SpanAttributes) {
	s.ctx.mu.Lock()
	defer s.ctx.mu.Unlock()
	if s.span.settled {
		return
	}
	s.span.attributes = mergeAttributes(s.span.attributes, attributes)
}

func (s *inMemorySpan) SetStatus(status SpanStatus) {
	s.ctx.mu.Lock()
	defer s.ctx.mu.Unlock()
	if s.span.settled {
		return
	}
	s.span.status = copyStatus(status)
	s.span.explicitStatus = true
}

// copyAttributes detaches a caller's attribute map. Per the SpanAttributes
// contract a nil value is pi's `undefined` entry, so it is dropped rather
// than recorded.
func copyAttributes(attributes SpanAttributes) SpanAttributes {
	copied := SpanAttributes{}
	for name, value := range attributes {
		if value == nil {
			continue
		}
		copied[name] = copyAttributeValue(value)
	}
	return copied
}

func mergeAttributes(current, attributes SpanAttributes) SpanAttributes {
	merged := copyAttributes(current)
	for name, value := range attributes {
		if value == nil {
			continue
		}
		merged[name] = copyAttributeValue(value)
	}
	return merged
}

// copyAttributeValue clones slice values so a caller mutating the slice it
// passed cannot reach into recorded state (pi's `[...value]`). Scalars are
// already values in Go and pass through.
func copyAttributeValue(value AttributeValue) AttributeValue {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice || rv.IsNil() {
		return value
	}
	clone := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
	reflect.Copy(clone, rv)
	return clone.Interface()
}

func copyStatus(status SpanStatus) SpanStatus {
	if status.Code != StatusError {
		return SpanStatus{Code: StatusOK}
	}
	if status.Error == nil {
		return SpanStatus{Code: StatusError}
	}
	return SpanStatus{
		Code:  StatusError,
		Error: &SpanError{Name: status.Error.Name, Message: status.Error.Message},
	}
}
