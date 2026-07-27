package providers

import (
	"context"
	"testing"

	"github.com/sky-valley/pi/ai"
)

func TestFauxProviderStreamsProtocol(t *testing.T) {
	reg := RegisterFauxProvider(RegisterFauxProviderOptions{})
	defer reg.Unregister()

	reg.SetResponses([]FauxResponseStep{
		FauxStatic(FauxAssistantMessage(ai.ContentList{
			FauxThinking("let me think"),
			FauxText("hello world this is a longer message to force multiple deltas"),
		}, ai.StopStop)),
	})

	model := reg.GetModel()
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}

	stream := ai.StreamSimple(context.Background(), model, req, nil)

	var sawStart, sawTextDelta, sawThinkingDelta, sawDone bool
	var startStop ai.StopReason
	for e := range stream.Events() {
		switch e.Type {
		case ai.EventStart:
			sawStart = true
			if e.Partial != nil {
				startStop = e.Partial.StopReason
			}
		case ai.EventTextDelta:
			sawTextDelta = true
		case ai.EventThinkingDelta:
			sawThinkingDelta = true
		case ai.EventDone:
			sawDone = true
		}
	}
	final := stream.Result()
	if !sawStart || !sawTextDelta || !sawThinkingDelta || !sawDone {
		t.Fatalf("missing protocol events: start=%v textDelta=%v thinkDelta=%v done=%v", sawStart, sawTextDelta, sawThinkingDelta, sawDone)
	}
	// Upstream f9a49869: even though the resolved message is a normal stop, the
	// in-flight partial reports pending until the terminal event.
	if startStop != ai.StopPending {
		t.Fatalf("in-flight partial should be pending, got %q", startStop)
	}
	if final == nil || final.StopReason != ai.StopStop {
		t.Fatalf("unexpected final message: %#v", final)
	}
	// Final text must be fully assembled from deltas.
	gotText := ""
	for _, c := range final.Content {
		if tc, ok := c.(ai.TextContent); ok {
			gotText = tc.Text
		}
	}
	if gotText != "hello world this is a longer message to force multiple deltas" {
		t.Fatalf("text not assembled correctly: %q", gotText)
	}
}

// Upstream f9a49869: a faux response whose stop reason was never resolved
// (still "pending") is a test-authoring error — the stream fails rather than
// emitting done, and the in-flight partial carries the pending reason.
func TestFauxProviderPendingStopReasonFailsStream(t *testing.T) {
	reg := RegisterFauxProvider(RegisterFauxProviderOptions{})
	defer reg.Unregister()

	// Build the message directly so we bypass FauxAssistantMessage's "" → stop
	// coercion and leave the stop reason genuinely pending.
	pending := &ai.AssistantMessage{
		Content:    ai.ContentList{FauxText("half a thought")},
		Api:        reg.Api,
		Provider:   reg.Models[0].Provider,
		Model:      reg.Models[0].ID,
		StopReason: ai.StopPending,
	}
	reg.SetResponses([]FauxResponseStep{FauxStatic(pending)})

	model := reg.GetModel()
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}

	stream := ai.StreamSimple(context.Background(), model, req, nil)

	var startPartialStop ai.StopReason
	sawStart, sawDone, sawError := false, false, false
	for e := range stream.Events() {
		switch e.Type {
		case ai.EventStart:
			sawStart = true
			if e.Partial != nil {
				startPartialStop = e.Partial.StopReason
			}
		case ai.EventDone:
			sawDone = true
		case ai.EventError:
			sawError = true
		}
	}
	final := stream.Result()

	if !sawStart || startPartialStop != ai.StopPending {
		t.Fatalf("in-flight partial should be pending, got start=%v stop=%q", sawStart, startPartialStop)
	}
	if sawDone || !sawError {
		t.Fatalf("pending faux response must error, not complete: done=%v error=%v", sawDone, sawError)
	}
	if final.StopReason != ai.StopError {
		t.Fatalf("expected error stop, got %s", final.StopReason)
	}
	if final.ErrorMessage != "Faux response ended without a stop reason" {
		t.Fatalf("error message wrong: %q", final.ErrorMessage)
	}
}

func TestFauxProviderUnknownApiRegisteredAndRemoved(t *testing.T) {
	reg := RegisterFauxProvider(RegisterFauxProviderOptions{Api: "faux-test-x"})
	if _, ok := ai.GetApiProvider("faux-test-x"); !ok {
		t.Fatal("provider not registered")
	}
	reg.Unregister()
	if _, ok := ai.GetApiProvider("faux-test-x"); ok {
		t.Fatal("provider not unregistered")
	}
}

func TestFauxProviderCachePrefix(t *testing.T) {
	reg := RegisterFauxProvider(RegisterFauxProviderOptions{})
	defer reg.Unregister()
	reg.SetResponses([]FauxResponseStep{
		FauxStatic(FauxAssistantMessage(ai.ContentList{FauxText("a")}, ai.StopStop)),
		FauxStatic(FauxAssistantMessage(ai.ContentList{FauxText("b")}, ai.StopStop)),
	})
	model := reg.GetModel()
	opts := &ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{SessionID: "s1", CacheRetention: ai.CacheShort}}

	req := ai.Context{Messages: []ai.Message{ai.NewUserText("the quick brown fox jumps", 1)}}
	first := ai.StreamSimple(context.Background(), model, req, opts).Result()
	if first.Usage.CacheWrite == 0 {
		t.Fatalf("first call should write cache, got %+v", first.Usage)
	}
	// Same prefix on next call should produce cache reads.
	second := ai.StreamSimple(context.Background(), model, req, opts).Result()
	if second.Usage.CacheRead == 0 {
		t.Fatalf("second call should read cache, got %+v", second.Usage)
	}
}
