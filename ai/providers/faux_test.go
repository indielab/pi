package providers

import (
	"context"
	"strings"
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

// TestFauxProviderDeferredResponses locks pi 382aa641c's faux half: a
// submission that asks for deferral streams an empty "deferred" message
// carrying a handle, fetches answer with that same handle while the script
// says the response is still pending, and the next fetch redeems the scripted
// response.
func TestFauxProviderDeferredResponses(t *testing.T) {
	reg := RegisterFauxProvider(RegisterFauxProviderOptions{
		Deferred: &FauxDeferredOptions{PendingFetches: 1, PollAfterMs: 25},
	})
	defer reg.Unregister()
	reg.SetResponses([]FauxResponseStep{
		FauxStatic(FauxAssistantMessage(ai.ContentList{FauxText("ready")}, ai.StopStop)),
	})

	model := reg.GetModel()
	api, ok := ai.GetApiProvider(model.Api)
	if !ok || api.FetchDeferred == nil || api.CancelDeferred == nil {
		t.Fatal("the faux api must register both deferred entry points")
	}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}

	submission := ai.StreamSimple(context.Background(), model, req,
		&ai.SimpleStreamOptions{Deferred: &ai.DeferredRequest{Window: ai.DeferredWindow1h}})
	var types []ai.EventType
	for e := range submission.Events() {
		types = append(types, e.Type)
	}
	accepted := submission.Result()
	if len(types) != 2 || types[0] != ai.EventStart || types[1] != ai.EventDone {
		t.Fatalf("a deferred submission streams no content, got %v", types)
	}
	if accepted.StopReason != ai.StopDeferred || len(accepted.Content) != 0 {
		t.Fatalf("submission = %q with %d blocks, want an empty deferred message", accepted.StopReason, len(accepted.Content))
	}
	if accepted.Deferred == nil {
		t.Fatal("a deferred submission must carry a handle")
	}
	handle := *accepted.Deferred
	if handle.Provider != model.Provider || handle.ModelID != model.ID || handle.Api != model.Api || handle.ID == "" {
		t.Fatalf("handle does not identify the request: %+v", handle)
	}
	if handle.PollAfterMs != 25 {
		t.Fatalf("handle pollAfterMs = %d, want the scripted 25", handle.PollAfterMs)
	}

	// The scripted pending fetch answers with the same handle, not the response.
	pending := api.FetchDeferred(context.Background(), model, handle, &ai.DeferredFetchOptions{}).Result()
	if pending.StopReason != ai.StopDeferred || pending.Deferred == nil || pending.Deferred.ID != handle.ID {
		t.Fatalf("a pending fetch must return the handle again, got %q", pending.StopReason)
	}

	ready := api.FetchDeferred(context.Background(), model, handle, &ai.DeferredFetchOptions{}).Result()
	if ready.StopReason != ai.StopStop || len(ready.Content) != 1 {
		t.Fatalf("the next fetch must redeem the response, got %q (%s)", ready.StopReason, ready.ErrorMessage)
	}
	if text := ready.Content[0].(ai.TextContent).Text; text != "ready" {
		t.Fatalf("redeemed text = %q", text)
	}
	if ready.Usage.TotalTokens == 0 {
		t.Fatal("a redeemed response must be usage-estimated like a streamed one")
	}
	// The submission consumed one scripted response; both fetches were counted.
	if reg.State.CallCount != 1 || reg.State.DeferredFetchCount != 2 {
		t.Fatalf("state = callCount %d, deferredFetchCount %d; want 1 and 2",
			reg.State.CallCount, reg.State.DeferredFetchCount)
	}
}

// TestFauxProviderDeferredCancelAndFailures locks the faux's failure surface:
// cancellation is recorded and makes later fetches fail, and an unknown handle
// fails in-band rather than panicking.
func TestFauxProviderDeferredCancelAndFailures(t *testing.T) {
	reg := RegisterFauxProvider(RegisterFauxProviderOptions{})
	defer reg.Unregister()
	reg.SetResponses([]FauxResponseStep{
		FauxStatic(FauxAssistantMessage(ai.ContentList{FauxText("cancelled")}, ai.StopStop)),
	})

	model := reg.GetModel()
	api, _ := ai.GetApiProvider(model.Api)
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}

	accepted := ai.StreamSimple(context.Background(), model, req,
		&ai.SimpleStreamOptions{Deferred: &ai.DeferredRequest{}}).Result()
	if accepted.Deferred == nil {
		t.Fatalf("bare deferral must still produce a handle, got %q", accepted.StopReason)
	}
	handle := *accepted.Deferred

	if err := api.CancelDeferred(context.Background(), model, handle, nil); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if len(reg.State.CancelledDeferred) != 1 || reg.State.CancelledDeferred[0].ID != handle.ID {
		t.Fatalf("cancellation not recorded: %+v", reg.State.CancelledDeferred)
	}
	cancelled := api.FetchDeferred(context.Background(), model, handle, &ai.DeferredFetchOptions{}).Result()
	if cancelled.StopReason != ai.StopError || !strings.Contains(cancelled.ErrorMessage, "was cancelled") {
		t.Fatalf("fetching a cancelled response must fail in-band, got %q (%s)",
			cancelled.StopReason, cancelled.ErrorMessage)
	}

	unknown := api.FetchDeferred(context.Background(), model,
		ai.DeferredHandle{Provider: model.Provider, ModelID: model.ID, Api: model.Api, ID: "nope"},
		&ai.DeferredFetchOptions{}).Result()
	if unknown.StopReason != ai.StopError || !strings.Contains(unknown.ErrorMessage, "Unknown faux deferred response") {
		t.Fatalf("an unknown handle must fail in-band, got %q (%s)", unknown.StopReason, unknown.ErrorMessage)
	}
}
