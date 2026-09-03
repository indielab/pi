package ai

import (
	"context"
	"encoding/json"
	"testing"
)

func errResp(msg string) *AssistantMessage {
	return &AssistantMessage{StopReason: StopError, ErrorMessage: msg}
}
func okResp() *AssistantMessage {
	return &AssistantMessage{StopReason: StopStop}
}

// TestRetryAssistantCall locks pi's retryAssistantCall (upstream 65dd2e0e):
// success/non-retryable/abort short-circuits, bounded retry with backoff, and
// callback ordering.
func TestRetryAssistantCall(t *testing.T) {
	policy := &RetryPolicy{Enabled: true, MaxRetries: 3, BaseDelayMs: 0}

	t.Run("nil policy is passthrough", func(t *testing.T) {
		calls := 0
		got := RetryAssistantCall(context.Background(), func() *AssistantMessage { calls++; return errResp("overloaded") }, nil, nil)
		if calls != 1 || got.StopReason != StopError {
			t.Fatalf("calls=%d stop=%v", calls, got.StopReason)
		}
	})

	t.Run("success returns immediately", func(t *testing.T) {
		calls := 0
		got := RetryAssistantCall(context.Background(), func() *AssistantMessage { calls++; return okResp() }, policy, nil)
		if calls != 1 || got.StopReason != StopStop {
			t.Fatalf("calls=%d stop=%v", calls, got.StopReason)
		}
	})

	t.Run("non-retryable error fails fast", func(t *testing.T) {
		calls := 0
		got := RetryAssistantCall(context.Background(), func() *AssistantMessage { calls++; return errResp("insufficient_quota") }, policy, nil)
		if calls != 1 || got.ErrorMessage != "insufficient_quota" {
			t.Fatalf("non-retryable retried: calls=%d", calls)
		}
	})

	t.Run("retries transient then succeeds", func(t *testing.T) {
		calls := 0
		var scheduled, started, finished int
		var finishedSuccess bool
		cb := &RetryCallbacks{
			OnRetryScheduled:    func(a, m, d int, e string) { scheduled++ },
			OnRetryAttemptStart: func() { started++ },
			OnRetryFinished:     func(s bool, a int, e string) { finished++; finishedSuccess = s },
		}
		got := RetryAssistantCall(context.Background(), func() *AssistantMessage {
			calls++
			if calls < 3 {
				return errResp("overloaded")
			}
			return okResp()
		}, policy, cb)
		if calls != 3 || got.StopReason != StopStop {
			t.Fatalf("calls=%d stop=%v", calls, got.StopReason)
		}
		if scheduled != 2 || started != 2 || finished != 1 || !finishedSuccess {
			t.Fatalf("callbacks: scheduled=%d started=%d finished=%d success=%v", scheduled, started, finished, finishedSuccess)
		}
	})

	t.Run("exhausts budget and returns last error", func(t *testing.T) {
		calls := 0
		var finished int
		var finalErr string
		cb := &RetryCallbacks{OnRetryFinished: func(s bool, a int, e string) { finished++; finalErr = e }}
		got := RetryAssistantCall(context.Background(), func() *AssistantMessage { calls++; return errResp("overloaded") }, policy, cb)
		// initial call + 3 retries = 4 produce() calls.
		if calls != 4 || got.StopReason != StopError {
			t.Fatalf("calls=%d stop=%v", calls, got.StopReason)
		}
		if finished != 1 || finalErr != "overloaded" {
			t.Fatalf("finished=%d finalErr=%q", finished, finalErr)
		}
		// The positive counterpart to the absence pin in the abort case below:
		// an exhausted budget keeps the last error under pi's own key. Without
		// this, that pin would survive the field losing its key entirely.
		if raw, present := marshalField(t, got, "errorMessage"); !present || string(raw) != `"overloaded"` {
			t.Fatalf("errorMessage = %s (present=%v), want \"overloaded\"", raw, present)
		}
	})

	t.Run("abort during backoff normalizes to aborted", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // ctx.Done ready before the sleep
		longPolicy := &RetryPolicy{Enabled: true, MaxRetries: 3, BaseDelayMs: 1_000_000}
		calls := 0
		got := RetryAssistantCall(ctx, func() *AssistantMessage { calls++; return errResp("overloaded") }, longPolicy, nil)
		if got.StopReason != StopAborted || got.ErrorMessage != "" {
			t.Fatalf("expected normalized abort, got stop=%v err=%q", got.StopReason, got.ErrorMessage)
		}
		if calls != 1 {
			t.Fatalf("aborted during first backoff should call produce once, got %d", calls)
		}
		// pi 86bac52f9 destructures errorMessage away rather than setting it to
		// undefined, so the aborted message carries no errorMessage key at all —
		// the field is observable in serialized session JSON. Go's omitempty on
		// the cleared string is the same wire shape; this pins it.
		if raw, present := marshalField(t, got, "errorMessage"); present {
			t.Fatalf("errorMessage must be absent on an aborted retry, got %s", raw)
		}
	})
}

// marshalField serializes msg and returns one top-level field of the result,
// reporting whether the key is there at all.
func marshalField(t *testing.T, msg *AssistantMessage, key string) (json.RawMessage, bool) {
	t.Helper()
	encoded, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	raw, present := fields[key]
	return raw, present
}
