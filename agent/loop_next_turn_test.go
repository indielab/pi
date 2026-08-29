package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// noopToolFor builds a tool that records nothing and always succeeds.
func noopToolFor(name string) AgentTool {
	return AgentTool{
		Name:        name,
		Description: "Noop tool",
		Parameters:  ai.Object(),
		Execute: func(ctx context.Context, id string, params map[string]any, onUpdate ToolUpdateFunc) (AgentToolResult, error) {
			return AgentToolResult{Content: ai.ContentList{ai.TextContent{Text: "ok"}}}, nil
		},
	}
}

// nextTurnProbe records the interleaving of provider requests, hook calls and
// turn_start events for one run.
type nextTurnProbe struct {
	mu    sync.Mutex
	trace []string
}

func (p *nextTurnProbe) log(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.trace = append(p.trace, s)
}

func (p *nextTurnProbe) got() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.trace, " ")
}

// runProbeLoop drives runAgentLoop with a scripted stream, recording the trace.
func runProbeLoop(t *testing.T, p *nextTurnProbe, cfg AgentLoopConfig, msgs ...*ai.AssistantMessage) {
	t.Helper()
	scripted := scriptedStream(msgs...)
	cfg.Model = testModel
	agentCtx := AgentContext{Tools: []AgentTool{noopToolFor("noop")}}
	runAgentLoop(context.Background(), []AgentMessage{ai.UserMessage{Content: ai.ContentList{ai.TextContent{Text: "start"}}}},
		agentCtx, cfg,
		func(e AgentEvent) error {
			if e.Type == EvTurnStart {
				p.log("turn_start")
			}
			return nil
		},
		func(ctx context.Context, model *ai.Model, req ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			p.log("request")
			return scripted(ctx, model, req, opts)
		})
}

// TestPrepareNextTurnRunsOnlyBeforeAnActualNextRequest pins upstream 56700d42e:
// preparation moved out of the post-turn_end block and to the head of the next
// inner-loop iteration, so it runs immediately before the provider request it
// prepares for — and NOT at all after the final turn, which no longer has a next
// request to prepare. Under the previous shape this hook fired twice, the second
// time for a turn that never happened.
func TestPrepareNextTurnRunsOnlyBeforeAnActualNextRequest(t *testing.T) {
	p := &nextTurnProbe{}
	runProbeLoop(t, p, AgentLoopConfig{
		PrepareNextTurn: func(c ShouldStopAfterTurnContext) *AgentLoopTurnUpdate {
			p.log("prepare")
			return nil
		},
	},
		assistantWithToolCall("tool-1", "noop", map[string]any{}),
		&ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "done"}}, StopReason: ai.StopStop},
	)

	// turn_start for turn 1 is emitted by runAgentLoop before the loop starts.
	want := "turn_start request prepare turn_start request"
	if got := p.got(); got != want {
		t.Fatalf("trace = %q, want %q", got, want)
	}
}

// TestShouldStopAfterTurnRunsBeforePrepareNextTurn pins the other half of the
// reordering: the stop check now sees the completed-turn context and runs first,
// so a run that stops after its turn never prepares a turn it will not take.
func TestShouldStopAfterTurnRunsBeforePrepareNextTurn(t *testing.T) {
	p := &nextTurnProbe{}
	runProbeLoop(t, p, AgentLoopConfig{
		ShouldStopAfterTurn: func(c ShouldStopAfterTurnContext) bool {
			p.log("should_stop")
			return true
		},
		PrepareNextTurn: func(c ShouldStopAfterTurnContext) *AgentLoopTurnUpdate {
			p.log("prepare")
			return nil
		},
	},
		assistantWithToolCall("tool-1", "noop", map[string]any{}),
		&ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "should not run"}}, StopReason: ai.StopStop},
	)

	want := "turn_start request should_stop"
	if got := p.got(); got != want {
		t.Fatalf("trace = %q, want %q", got, want)
	}
}

// TestPrepareNextTurnPicksUpSteeringQueuedWhilePreparing pins the re-poll:
// preparation can be long-running (compaction), so steering queued while it ran
// must reach the turn it was typed for rather than the one after it. Note this
// outcome also held before 56700d42e, by a different mechanism (preparation ran
// BEFORE the end-of-turn poll), so it is not a red-green discriminator for the
// reorder — it is locked by mutation instead: deleting the re-poll turns it red.
func TestPrepareNextTurnPicksUpSteeringQueuedWhilePreparing(t *testing.T) {
	p := &nextTurnProbe{}
	var queued []AgentMessage
	var typed bool

	steered := ai.UserMessage{Content: ai.ContentList{ai.TextContent{Text: "steered"}}}
	var secondRequest ai.Context
	scripted := scriptedStream(
		assistantWithToolCall("tool-1", "noop", map[string]any{}),
		&ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "done"}}, StopReason: ai.StopStop},
	)

	cfg := AgentLoopConfig{
		Model: testModel,
		GetSteeringMessages: func() []AgentMessage {
			out := queued
			queued = nil
			return out
		},
		PrepareNextTurn: func(c ShouldStopAfterTurnContext) *AgentLoopTurnUpdate {
			// The user typed while compaction was running. Once only — this hook
			// runs before every continued turn.
			if !typed {
				typed = true
				queued = []AgentMessage{steered}
			}
			return nil
		},
	}

	var requests int
	agentCtx := AgentContext{Tools: []AgentTool{noopToolFor("noop")}}
	runAgentLoop(context.Background(), []AgentMessage{ai.UserMessage{Content: ai.ContentList{ai.TextContent{Text: "start"}}}},
		agentCtx, cfg,
		func(e AgentEvent) error { return nil },
		func(ctx context.Context, model *ai.Model, req ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			requests++
			if requests == 2 {
				secondRequest = req
			}
			return scripted(ctx, model, req, opts)
		})

	if requests != 2 {
		t.Fatalf("expected 2 provider requests, got %d", requests)
	}
	var texts []string
	for _, m := range secondRequest.Messages {
		if u, ok := m.(ai.UserMessage); ok {
			for _, c := range u.Content {
				if tc, ok := c.(ai.TextContent); ok {
					texts = append(texts, tc.Text)
				}
			}
		}
	}
	if !contains(texts, "steered") {
		t.Fatalf("steering queued during preparation never reached the turn it prepared; user texts = %v", texts)
	}
	_ = p
}

// TestPrepareNextTurnDoesNotDoubleDeliverSteering pins the "len(pending) == 0"
// guard on that re-poll: when the end-of-turn poll already produced a message, preparation
// must NOT poll again, or one-at-a-time steering would deliver two messages in
// a single turn.
func TestPrepareNextTurnDoesNotDoubleDeliverSteering(t *testing.T) {
	first := ai.UserMessage{Content: ai.ContentList{ai.TextContent{Text: "first"}}}
	second := ai.UserMessage{Content: ai.ContentList{ai.TextContent{Text: "second"}}}

	// One message per poll, in order — pi's QueueOneAtATime.
	pending := []AgentMessage{first, second}
	pollAt := 0
	poll := func() []AgentMessage {
		pollAt++
		if pollAt == 1 || len(pending) == 0 {
			return nil // the initial poll, before any turn has run
		}
		out := []AgentMessage{pending[0]}
		pending = pending[1:]
		return out
	}

	scripted := scriptedStream(
		assistantWithToolCall("tool-1", "noop", map[string]any{}),
		&ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "done"}}, StopReason: ai.StopStop},
	)

	var requests int
	var secondRequest ai.Context
	cfg := AgentLoopConfig{
		Model:               testModel,
		GetSteeringMessages: poll,
		PrepareNextTurn:     func(c ShouldStopAfterTurnContext) *AgentLoopTurnUpdate { return nil },
	}
	agentCtx := AgentContext{Tools: []AgentTool{noopToolFor("noop")}}
	runAgentLoop(context.Background(), []AgentMessage{ai.UserMessage{Content: ai.ContentList{ai.TextContent{Text: "start"}}}},
		agentCtx, cfg,
		func(e AgentEvent) error { return nil },
		func(ctx context.Context, model *ai.Model, req ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			requests++
			if requests == 2 {
				secondRequest = req
			}
			return scripted(ctx, model, req, opts)
		})

	var texts []string
	for _, m := range secondRequest.Messages {
		if u, ok := m.(ai.UserMessage); ok {
			for _, c := range u.Content {
				if tc, ok := c.(ai.TextContent); ok {
					texts = append(texts, tc.Text)
				}
			}
		}
	}
	if contains(texts, "first") && contains(texts, "second") {
		t.Fatalf("both steering messages landed in one turn; user texts = %v", texts)
	}
	if !contains(texts, "first") {
		t.Fatalf("the polled steering message never reached the turn; user texts = %v", texts)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
