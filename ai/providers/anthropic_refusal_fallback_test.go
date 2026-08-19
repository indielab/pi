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

func anthropicFallbackServer(t *testing.T, gotHeaders *http.Header, gotBody *map[string]any, sse string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotHeaders = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, gotBody)
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(200)
		io.WriteString(w, sse)
	}))
}

func TestAnthropicRefusalFallbacksOnWire(t *testing.T) {
	var gotHeaders http.Header
	gotBody := map[string]any{}
	server := anthropicFallbackServer(t, &gotHeaders, &gotBody, anthropicSSEWithModel)
	defer server.Close()

	model := &ai.Model{
		ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic",
		Input: []string{"text"}, MaxTokens: 4096, BaseURL: server.URL,
	}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	opts := &ai.SimpleStreamOptions{
		RefusalFallbacks: &ai.AnthropicRefusalFallback{Targets: []ai.AnthropicRefusalFallbackTarget{{Model: "claude-opus-4-8"}}},
	}
	opts.APIKey = "k"
	StreamSimpleAnthropic(context.Background(), model, req, opts).Result()

	fallbacks, ok := gotBody["fallbacks"].([]any)
	if !ok {
		t.Fatalf("fallbacks missing or not an array: %#v", gotBody["fallbacks"])
	}
	if len(fallbacks) != 1 {
		t.Fatalf("want 1 fallback entry, got %v", fallbacks)
	}
	entry, _ := fallbacks[0].(map[string]any)
	if entry == nil || entry["model"] != "claude-opus-4-8" {
		t.Fatalf("want [{model: claude-opus-4-8}], got %v", fallbacks)
	}
	if !strings.Contains(gotHeaders.Get("anthropic-beta"), serverSideFallbackBeta) {
		t.Fatalf("fallback beta missing: %q", gotHeaders.Get("anthropic-beta"))
	}
}

func TestAnthropicNoRefusalFallbacksByDefault(t *testing.T) {
	var gotHeaders http.Header
	gotBody := map[string]any{}
	server := anthropicFallbackServer(t, &gotHeaders, &gotBody, anthropicSSEWithModel)
	defer server.Close()

	model := &ai.Model{
		ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic",
		Input: []string{"text"}, MaxTokens: 4096, BaseURL: server.URL,
	}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	opts := &ai.SimpleStreamOptions{}
	opts.APIKey = "k"
	StreamSimpleAnthropic(context.Background(), model, req, opts).Result()

	if _, has := gotBody["fallbacks"]; has {
		t.Fatalf("fallbacks must be omitted when unset: %v", gotBody["fallbacks"])
	}
	if strings.Contains(gotHeaders.Get("anthropic-beta"), serverSideFallbackBeta) {
		t.Fatalf("fallback beta must not be sent when unset: %q", gotHeaders.Get("anthropic-beta"))
	}
}

// The "default" arm is the other half of pi's union: the literal string, not an
// array. An empty chain is how the Go type spells it. The beta rides on the
// option being set at all, either way.
func TestAnthropicRefusalFallbackDefaultArm(t *testing.T) {
	var gotHeaders http.Header
	gotBody := map[string]any{}
	server := anthropicFallbackServer(t, &gotHeaders, &gotBody, anthropicSSEWithModel)
	defer server.Close()

	model := &ai.Model{
		ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic",
		Input: []string{"text"}, MaxTokens: 4096, BaseURL: server.URL,
	}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	opts := &ai.SimpleStreamOptions{RefusalFallbacks: &ai.AnthropicRefusalFallback{}}
	opts.APIKey = "k"
	StreamSimpleAnthropic(context.Background(), model, req, opts).Result()

	if gotBody["fallbacks"] != "default" {
		t.Fatalf("want fallbacks=\"default\", got %#v", gotBody["fallbacks"])
	}
	if !strings.Contains(gotHeaders.Get("anthropic-beta"), serverSideFallbackBeta) {
		t.Fatalf("fallback beta missing: %q", gotHeaders.Get("anthropic-beta"))
	}
}

// message_start reports the model Anthropic actually served, which is how a
// server-side fallback becomes visible on the returned message.
func TestAnthropicCapturesServedModel(t *testing.T) {
	var gotHeaders http.Header
	gotBody := map[string]any{}
	server := anthropicFallbackServer(t, &gotHeaders, &gotBody, anthropicSSEWithModel)
	defer server.Close()

	model := &ai.Model{
		ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic",
		Input: []string{"text"}, MaxTokens: 4096, BaseURL: server.URL,
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
func anthropicFallbackCosts(t *testing.T, model *ai.Model, opts *ai.SimpleStreamOptions) (start, final ai.CostBreakdown, msg *ai.AssistantMessage) {
	t.Helper()
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	stream := StreamSimpleAnthropic(context.Background(), model, req, opts)
	for ev := range stream.Events() {
		if ev.Type == ai.EventTextStart && ev.Partial != nil {
			start = ev.Partial.Usage.Cost
		}
	}
	msg = stream.Result()
	if msg == nil {
		t.Fatal("stream produced no message")
	}
	return start, msg.Usage.Cost, msg
}

func assertCost(t *testing.T, label string, got ai.CostBreakdown, wantInput, wantOutput, wantTotal float64) {
	t.Helper()
	const eps = 1e-12
	if math.Abs(got.Input-wantInput) > eps || math.Abs(got.Output-wantOutput) > eps || math.Abs(got.Total-wantTotal) > eps {
		t.Fatalf("%s cost.input = %v, want %v; cost.output = %v, want %v; cost.total = %v, want %v",
			label, got.Input, wantInput, got.Output, wantOutput, got.Total, wantTotal)
	}
}

// A response served by a fallback is costed at the fallback's rates, taken from
// the request option, at both cost sites — and the pricing that did the costing
// never reaches the wire.
func TestAnthropicFallbackCostFromRequestOption(t *testing.T) {
	var gotHeaders http.Header
	gotBody := map[string]any{}
	server := anthropicFallbackServer(t, &gotHeaders, &gotBody, anthropicSSEWithModel)
	defer server.Close()

	model := &ai.Model{
		ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic",
		Input: []string{"text"}, MaxTokens: 4096, BaseURL: server.URL,
		Cost: ai.ModelCost{Input: 100, Output: 200},
	}
	opts := &ai.SimpleStreamOptions{
		RefusalFallbacks: &ai.AnthropicRefusalFallback{Targets: []ai.AnthropicRefusalFallbackTarget{
			{Model: "claude-opus-4-8", Cost: &ai.ModelCost{Input: 5, Output: 25, CacheRead: 0.5, CacheWrite: 6.25}},
		}},
	}
	opts.APIKey = "k"
	start, final, msg := anthropicFallbackCosts(t, model, opts)

	if msg.Model != "claude-opus-4-8" {
		t.Fatalf("want the served model claude-opus-4-8, got %q", msg.Model)
	}
	// message_start: 10 input, 1 output at the fallback's 5/25 per Mtok.
	assertCost(t, "message_start", start, 5e-5, 2.5e-5, 7.5e-5)
	// final: 10 input, 2 output at the same rates.
	assertCost(t, "final", final, 5e-5, 5e-5, 1e-4)

	fallbacks, _ := gotBody["fallbacks"].([]any)
	if len(fallbacks) != 1 {
		t.Fatalf("want 1 fallback entry, got %#v", gotBody["fallbacks"])
	}
	entry, _ := fallbacks[0].(map[string]any)
	if len(entry) != 1 || entry["model"] != "claude-opus-4-8" {
		t.Fatalf("fallbacks entry must carry only {model}, got %v", entry)
	}
}

// pi joins the request option and the catalog with `??`, so a target listed
// without local pricing falls through to the catalog rather than claiming the
// lookup and pinning the requested model's rates.
func TestAnthropicFallbackCostFallsThroughToCatalog(t *testing.T) {
	var gotHeaders http.Header
	gotBody := map[string]any{}
	server := anthropicFallbackServer(t, &gotHeaders, &gotBody, anthropicSSEWithModel)
	defer server.Close()

	model := &ai.Model{
		ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic",
		Input: []string{"text"}, MaxTokens: 4096, BaseURL: server.URL,
		Cost:   ai.ModelCost{Input: 100, Output: 200},
		Compat: []byte(`{"allowedFallbackModels":[{"model":"claude-opus-4-8","cost":{"input":5,"output":25,"cacheRead":0.5,"cacheWrite":6.25}}]}`),
	}
	opts := &ai.SimpleStreamOptions{
		RefusalFallbacks: &ai.AnthropicRefusalFallback{Targets: []ai.AnthropicRefusalFallbackTarget{
			{Model: "claude-opus-4-8"},
		}},
	}
	opts.APIKey = "k"
	_, final, _ := anthropicFallbackCosts(t, model, opts)

	assertCost(t, "final", final, 5e-5, 5e-5, 1e-4)
}

// pi guards the whole lookup on the served model differing from the requested
// one, so a self-referential catalog entry cannot reprice ordinary traffic.
func TestAnthropicServedModelEqualToRequestedKeepsOwnPricing(t *testing.T) {
	var gotHeaders http.Header
	gotBody := map[string]any{}
	server := anthropicFallbackServer(t, &gotHeaders, &gotBody, anthropicSSESameModel)
	defer server.Close()

	model := &ai.Model{
		ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic",
		Input: []string{"text"}, MaxTokens: 4096, BaseURL: server.URL,
		Cost:   ai.ModelCost{Input: 100, Output: 200},
		Compat: []byte(`{"allowedFallbackModels":[{"model":"claude-opus-5","cost":{"input":1,"output":1,"cacheRead":0,"cacheWrite":0}}]}`),
	}
	opts := &ai.SimpleStreamOptions{}
	opts.APIKey = "k"
	_, final, _ := anthropicFallbackCosts(t, model, opts)

	assertCost(t, "final", final, 1e-3, 4e-4, 1.4e-3)
}

// pi gates the swap on a pricing being FOUND, not on the served model differing:
// an unknown served model leaves the requested model's rates in place rather
// than blanking them. A fallback priced at all zeroes is a found pricing though
// — every JS object is truthy — so it does swap, to a zero-cost response.
func TestAnthropicUnknownServedModelKeepsRequestedPricing(t *testing.T) {
	newModel := func(baseURL string) *ai.Model {
		return &ai.Model{
			ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic",
			Input: []string{"text"}, MaxTokens: 4096, BaseURL: baseURL,
			Cost: ai.ModelCost{Input: 100, Output: 200},
		}
	}

	t.Run("served model unknown to both lookups", func(t *testing.T) {
		var gotHeaders http.Header
		gotBody := map[string]any{}
		server := anthropicFallbackServer(t, &gotHeaders, &gotBody, anthropicSSEWithModel)
		defer server.Close()

		opts := &ai.SimpleStreamOptions{}
		opts.APIKey = "k"
		_, final, _ := anthropicFallbackCosts(t, newModel(server.URL), opts)

		assertCost(t, "final", final, 1e-3, 4e-4, 1.4e-3)
	})

	t.Run("fallback priced at zero still swaps", func(t *testing.T) {
		var gotHeaders http.Header
		gotBody := map[string]any{}
		server := anthropicFallbackServer(t, &gotHeaders, &gotBody, anthropicSSEWithModel)
		defer server.Close()

		opts := &ai.SimpleStreamOptions{
			RefusalFallbacks: &ai.AnthropicRefusalFallback{Targets: []ai.AnthropicRefusalFallbackTarget{
				{Model: "claude-opus-4-8", Cost: &ai.ModelCost{}},
			}},
		}
		opts.APIKey = "k"
		_, final, _ := anthropicFallbackCosts(t, newModel(server.URL), opts)

		assertCost(t, "final", final, 0, 0, 0)
	})
}
