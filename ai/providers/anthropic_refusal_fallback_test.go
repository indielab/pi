package providers

import (
	"context"
	"encoding/json"
	"io"
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

func anthropicFallbackServer(t *testing.T, gotHeaders *http.Header, gotBody *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotHeaders = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, gotBody)
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(200)
		io.WriteString(w, anthropicSSEWithModel)
	}))
}

func TestAnthropicRefusalFallbacksOnWire(t *testing.T) {
	var gotHeaders http.Header
	gotBody := map[string]any{}
	server := anthropicFallbackServer(t, &gotHeaders, &gotBody)
	defer server.Close()

	model := &ai.Model{
		ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic",
		Input: []string{"text"}, MaxTokens: 4096, BaseURL: server.URL,
	}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	opts := &ai.SimpleStreamOptions{
		RefusalFallbacks: &ai.AnthropicRefusalFallback{Models: []string{"claude-opus-4-8"}},
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
	server := anthropicFallbackServer(t, &gotHeaders, &gotBody)
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
// array. The beta rides on the option being set at all, either way.
func TestAnthropicRefusalFallbackDefaultArm(t *testing.T) {
	var gotHeaders http.Header
	gotBody := map[string]any{}
	server := anthropicFallbackServer(t, &gotHeaders, &gotBody)
	defer server.Close()

	model := &ai.Model{
		ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic",
		Input: []string{"text"}, MaxTokens: 4096, BaseURL: server.URL,
	}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	opts := &ai.SimpleStreamOptions{RefusalFallbacks: &ai.AnthropicRefusalFallback{Default: true}}
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
	server := anthropicFallbackServer(t, &gotHeaders, &gotBody)
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
