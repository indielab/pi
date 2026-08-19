package providers

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// Ported from pi's anthropic refusal-fallback change (upstream eb1f87fa9).

// anthropicSSEWithModel is the shared fixture plus the `model` field real
// Anthropic responses always carry on message_start.
const anthropicSSEWithModel = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","model":"claude-opus-4-8","usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}

event: message_stop
data: {"type":"message_stop"}

`

// anthropicSSESameModel is the same stream served by the requested model, i.e. a
// response no server-side fallback stood in for.
const anthropicSSESameModel = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}

event: message_stop
data: {"type":"message_stop"}

`

// anthropicSSERepricedBack delivers message_start twice: once naming a fallback
// that served the response, then again naming the requested model. pi assigns
// usageModel in BOTH arms of its ternary, so the second one has to reprice back
// rather than leave the fallback's rates standing.
const anthropicSSERepricedBack = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","model":"claude-opus-4-8","usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}

event: message_start
data: {"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}

event: message_stop
data: {"type":"message_stop"}

`

// anthropicFallbackStub is a stub Anthropic endpoint replaying one SSE stream,
// along with the request it was called with. Read headers and body only once the
// stream has finished.
type anthropicFallbackStub struct {
	url     string
	headers http.Header
	body    map[string]any
}

// startAnthropicFallbackStub serves sse from an endpoint torn down with the test.
func startAnthropicFallbackStub(t *testing.T, sse string) *anthropicFallbackStub {
	t.Helper()
	stub := &anthropicFallbackStub{body: map[string]any{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.headers = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &stub.body)
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(200)
		io.WriteString(w, sse)
	}))
	t.Cleanup(server.Close)
	stub.url = server.URL
	return stub
}

func TestAnthropicRefusalFallbacksOnWire(t *testing.T) {
	stub := startAnthropicFallbackStub(t, anthropicSSEWithModel)

	model := &ai.Model{
		ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic",
		Input: []string{"text"}, MaxTokens: 4096, BaseURL: stub.url,
	}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	opts := &ai.SimpleStreamOptions{
		RefusalFallbacks: &ai.AnthropicRefusalFallback{Targets: []ai.AnthropicRefusalFallbackTarget{
			// Priced, to pin that local pricing never reaches Anthropic.
			{Model: "claude-opus-4-8", Cost: &ai.ModelCost{Input: 5, Output: 25}},
		}},
	}
	opts.APIKey = "k"
	StreamSimpleAnthropic(context.Background(), model, req, opts).Result()

	fallbacks, ok := stub.body["fallbacks"].([]any)
	if !ok {
		t.Fatalf("fallbacks missing or not an array: %#v", stub.body["fallbacks"])
	}
	if len(fallbacks) != 1 {
		t.Fatalf("want 1 fallback entry, got %v", fallbacks)
	}
	entry, _ := fallbacks[0].(map[string]any)
	if len(entry) != 1 || entry["model"] != "claude-opus-4-8" {
		t.Fatalf("fallbacks entry must carry only {model: claude-opus-4-8}, got %v", entry)
	}
	if !strings.Contains(stub.headers.Get("anthropic-beta"), serverSideFallbackBeta) {
		t.Fatalf("fallback beta missing: %q", stub.headers.Get("anthropic-beta"))
	}
}

func TestAnthropicNoRefusalFallbacksByDefault(t *testing.T) {
	stub := startAnthropicFallbackStub(t, anthropicSSEWithModel)

	model := &ai.Model{
		ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic",
		Input: []string{"text"}, MaxTokens: 4096, BaseURL: stub.url,
	}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	opts := &ai.SimpleStreamOptions{}
	opts.APIKey = "k"
	StreamSimpleAnthropic(context.Background(), model, req, opts).Result()

	if _, has := stub.body["fallbacks"]; has {
		t.Fatalf("fallbacks must be omitted when unset: %v", stub.body["fallbacks"])
	}
	if strings.Contains(stub.headers.Get("anthropic-beta"), serverSideFallbackBeta) {
		t.Fatalf("fallback beta must not be sent when unset: %q", stub.headers.Get("anthropic-beta"))
	}
}

// The "default" arm is the other half of pi's union: the literal string, not an
// array. An empty chain is how the Go type spells it. The beta rides on the
// option being set at all, either way.
func TestAnthropicRefusalFallbackDefaultArm(t *testing.T) {
	stub := startAnthropicFallbackStub(t, anthropicSSEWithModel)

	model := &ai.Model{
		ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic",
		Input: []string{"text"}, MaxTokens: 4096, BaseURL: stub.url,
	}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	opts := &ai.SimpleStreamOptions{RefusalFallbacks: &ai.AnthropicRefusalFallback{}}
	opts.APIKey = "k"
	StreamSimpleAnthropic(context.Background(), model, req, opts).Result()

	if stub.body["fallbacks"] != "default" {
		t.Fatalf("want fallbacks=\"default\", got %#v", stub.body["fallbacks"])
	}
	if !strings.Contains(stub.headers.Get("anthropic-beta"), serverSideFallbackBeta) {
		t.Fatalf("fallback beta missing: %q", stub.headers.Get("anthropic-beta"))
	}
}

// message_start reports the model Anthropic actually served, which is how a
// server-side fallback becomes visible on the returned message.
func TestAnthropicCapturesServedModel(t *testing.T) {
	stub := startAnthropicFallbackStub(t, anthropicSSEWithModel)

	model := &ai.Model{
		ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic",
		Input: []string{"text"}, MaxTokens: 4096, BaseURL: stub.url,
	}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	opts := &ai.SimpleStreamOptions{}
	opts.APIKey = "k"
	msg := StreamSimpleAnthropic(context.Background(), model, req, opts).Result()

	if msg.Model != "claude-opus-4-8" {
		t.Fatalf("want the served model claude-opus-4-8, got %q", msg.Model)
	}
}

// Cost accounting for server-side refusal fallbacks (upstream 4809c2abc).

// anthropicFallbackCosts runs one stream and reports the cost recorded at
// message_start (carried on the first text_start event's partial) alongside the
// final cost. Both are needed: the closing message_delta recomputes the figure,
// so a message_start-only regression is invisible in the final message.
func anthropicFallbackCosts(t *testing.T, model *ai.Model, opts *ai.SimpleStreamOptions) (start, final ai.CostBreakdown) {
	t.Helper()
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	stream := StreamSimpleAnthropic(context.Background(), model, req, opts)
	for ev := range stream.Events() {
		if ev.Type == ai.EventTextStart && ev.Partial != nil {
			start = ev.Partial.Usage.Cost
		}
	}
	msg := stream.Result()
	if msg == nil {
		t.Fatal("stream produced no message")
	}
	return start, msg.Usage.Cost
}

func assertCost(t *testing.T, label string, got, want ai.CostBreakdown) {
	t.Helper()
	const eps = 1e-12
	if math.Abs(got.Input-want.Input) > eps || math.Abs(got.Output-want.Output) > eps ||
		math.Abs(got.CacheRead-want.CacheRead) > eps || math.Abs(got.CacheWrite-want.CacheWrite) > eps ||
		math.Abs(got.Total-want.Total) > eps {
		t.Fatalf("%s cost = %+v, want %+v", label, got, want)
	}
}

// Every case below requests claude-opus-5, priced at 100/200 per Mtok, and is
// served a stream reporting 10 input tokens and 1 output token on message_start,
// then 2 output tokens on the closing message_delta.
func TestAnthropicFallbackCosting(t *testing.T) {
	// The pricing a fallback target carries, in the request option and in the
	// catalog, and the two rate cards a stream can end up billed at.
	fallbackPrice := &ai.ModelCost{Input: 5, Output: 25, CacheRead: 0.5, CacheWrite: 6.25}
	catalogCompat := json.RawMessage(`{"allowedFallbackModels":[{"model":"claude-opus-4-8","cost":{"input":5,"output":25,"cacheRead":0.5,"cacheWrite":6.25}}]}`)
	requestedStart := ai.CostBreakdown{Input: 1e-3, Output: 2e-4, Total: 1.2e-3}
	requestedFinal := ai.CostBreakdown{Input: 1e-3, Output: 4e-4, Total: 1.4e-3}
	fallbackStart := ai.CostBreakdown{Input: 5e-5, Output: 2.5e-5, Total: 7.5e-5}
	fallbackFinal := ai.CostBreakdown{Input: 5e-5, Output: 5e-5, Total: 1e-4}

	tests := []struct {
		name         string
		sse          string
		compat       json.RawMessage
		fallbacks    *ai.AnthropicRefusalFallback
		start, final ai.CostBreakdown
	}{
		{
			// A response a fallback served is costed at the fallback's rates, taken
			// from the request option, at both cost sites.
			name: "request option prices the served model",
			sse:  anthropicSSEWithModel,
			fallbacks: &ai.AnthropicRefusalFallback{Targets: []ai.AnthropicRefusalFallbackTarget{
				{Model: "claude-opus-4-8", Cost: fallbackPrice},
			}},
			start: fallbackStart, final: fallbackFinal,
		},
		{
			// pi joins the request option and the catalog with `??`, so a target
			// listed without local pricing falls through to the catalog rather than
			// claiming the lookup and pinning the requested model's rates.
			name:   "an unpriced target falls through to the catalog",
			sse:    anthropicSSEWithModel,
			compat: catalogCompat,
			fallbacks: &ai.AnthropicRefusalFallback{Targets: []ai.AnthropicRefusalFallbackTarget{
				{Model: "claude-opus-4-8"},
			}},
			start: fallbackStart, final: fallbackFinal,
		},
		{
			// ...and the request option is the LEFT side of that `??`, so it decides
			// whenever it carries pricing of its own.
			name:   "the request option outranks the catalog",
			sse:    anthropicSSEWithModel,
			compat: json.RawMessage(`{"allowedFallbackModels":[{"model":"claude-opus-4-8","cost":{"input":1,"output":1}}]}`),
			fallbacks: &ai.AnthropicRefusalFallback{Targets: []ai.AnthropicRefusalFallbackTarget{
				{Model: "claude-opus-4-8", Cost: fallbackPrice},
			}},
			start: fallbackStart, final: fallbackFinal,
		},
		{
			// pi guards the whole lookup on the served model differing from the
			// requested one, so a self-referential catalog entry cannot reprice
			// ordinary traffic.
			name:   "a served model equal to the requested one keeps its own pricing",
			sse:    anthropicSSESameModel,
			compat: json.RawMessage(`{"allowedFallbackModels":[{"model":"claude-opus-5","cost":{"input":1,"output":1,"cacheRead":0,"cacheWrite":0}}]}`),
			start:  requestedStart, final: requestedFinal,
		},
		{
			// pi gates the swap on a pricing being FOUND, not on the served model
			// differing: an unknown one leaves the requested model's rates in place
			// rather than blanking them.
			name:  "a served model unknown to both lookups keeps the requested pricing",
			sse:   anthropicSSEWithModel,
			start: requestedStart, final: requestedFinal,
		},
		{
			// A fallback priced at all zeroes IS a found pricing though — every JS
			// object is truthy — so it swaps, to a zero-cost response.
			name: "a fallback priced at zero still swaps",
			sse:  anthropicSSEWithModel,
			fallbacks: &ai.AnthropicRefusalFallback{Targets: []ai.AnthropicRefusalFallbackTarget{
				{Model: "claude-opus-4-8", Cost: &ai.ModelCost{}},
			}},
			start: ai.CostBreakdown{}, final: ai.CostBreakdown{},
		},
		{
			// pi's ternary assigns usageModel in both arms, so a message_start naming
			// the requested model again drops the fallback's rates.
			name: "a second message_start reprices back to the requested model",
			sse:  anthropicSSERepricedBack,
			fallbacks: &ai.AnthropicRefusalFallback{Targets: []ai.AnthropicRefusalFallbackTarget{
				{Model: "claude-opus-4-8", Cost: fallbackPrice},
			}},
			start: requestedStart, final: requestedFinal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := startAnthropicFallbackStub(t, tc.sse)
			model := &ai.Model{
				ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic",
				Input: []string{"text"}, MaxTokens: 4096, BaseURL: stub.url,
				Cost:   ai.ModelCost{Input: 100, Output: 200},
				Compat: tc.compat,
			}
			opts := &ai.SimpleStreamOptions{RefusalFallbacks: tc.fallbacks}
			opts.APIKey = "k"

			start, final := anthropicFallbackCosts(t, model, opts)
			assertCost(t, "message_start", start, tc.start)
			assertCost(t, "final", final, tc.final)
		})
	}
}
