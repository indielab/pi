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

// TestRetryAssistantCallCapsBackoff pins pi's 60s default cap on the
// agent-level backoff (upstream c37b0e03b, fixes #8826). The context is
// cancelled before the call, so the loop reports the delay it WOULD sleep and
// then aborts on it — the scheduled value is observable without the sleep.
func TestRetryAssistantCallCapsBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	policy := &RetryPolicy{Enabled: true, MaxRetries: 3, BaseDelayMs: 1_000_000}
	var got []int
	callbacks := &RetryCallbacks{
		OnRetryScheduled: func(_, _, delayMs int, _ string) { got = append(got, delayMs) },
	}
	RetryAssistantCall(ctx, func() *AssistantMessage { return errResp("overloaded") }, policy, callbacks)
	if len(got) != 1 || got[0] != 60_000 {
		t.Fatalf("scheduled delays = %v, want [60000] (pi caps the agent backoff at DEFAULT_MAX_AGENT_RETRY_DELAY_MS)", got)
	}
}

// TestRetryDelayMs is pi's own retryDelayMs oracle (upstream c37b0e03b,
// packages/ai/test/retry.test.ts "caps agent retry delay") plus the cases that
// exist only in Go: pi computes with floats and clamps a non-safe integer,
// while an int shift here wraps — negative, then to zero — and a non-positive
// time.Duration makes time.NewTimer fire immediately, so an uncapped overflow
// would be a hot retry loop rather than a long sleep.
func TestRetryDelayMs(t *testing.T) {
	ptr := func(v int) *int { return &v }
	cases := []struct {
		name    string
		policy  RetryPolicy
		attempt int
		want    int
	}{
		// pi's three assertions, verbatim.
		{"default cap applies", RetryPolicy{BaseDelayMs: 2000}, 6, 60_000},
		{"explicit cap applies", RetryPolicy{BaseDelayMs: 2000, MaxAgentDelayMs: ptr(5000)}, 5, 5000},
		{"zero cap is a real zero", RetryPolicy{BaseDelayMs: 2000, MaxAgentDelayMs: ptr(0)}, 5, 0},
		// Below the cap the raw exponential passes through untouched.
		{"under the cap", RetryPolicy{BaseDelayMs: 2000}, 5, 32_000},
		{"cap above the delay is a no-op", RetryPolicy{BaseDelayMs: 1000, MaxAgentDelayMs: ptr(999_999)}, 3, 4000},
		// pi's Math.max(0, attempt-1): attempt 0 and below yield the base delay,
		// where an unguarded Go shift would panic on a negative count.
		{"attempt 1 is the base delay", RetryPolicy{BaseDelayMs: 1000}, 1, 1000},
		{"attempt 0 floors the exponent", RetryPolicy{BaseDelayMs: 1000}, 0, 1000},
		{"negative attempt floors the exponent", RetryPolicy{BaseDelayMs: 1000}, -5, 1000},
		// Overflow saturates upward into the cap. Unsaturated, 1000<<54 wraps
		// negative and 1000<<61 wraps to exactly zero.
		{"overflow saturates into the cap", RetryPolicy{BaseDelayMs: 1000}, 55, 60_000},
		{"shift past the word size saturates", RetryPolicy{BaseDelayMs: 1000}, 100, 60_000},
		{"overflow saturates above a raised cap", RetryPolicy{BaseDelayMs: 1000, MaxAgentDelayMs: ptr(1 << 30)}, 100, 1 << 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RetryDelayMs(tc.policy, tc.attempt); got != tc.want {
				t.Fatalf("RetryDelayMs(%+v, %d) = %d, want %d", tc.policy, tc.attempt, got, tc.want)
			}
		})
	}
}

// TestRetryAssistantCallReportsCappedDelays transliterates pi's "reports capped
// retry delays" (upstream c37b0e03b, packages/ai/test/retry.test.ts): the whole
// loop, asserted through the delay each OnRetryScheduled reports. The delays are
// real sleeps, which is why pi's tiny 10ms/15ms policy is kept verbatim.
func TestRetryAssistantCallReportsCappedDelays(t *testing.T) {
	maxAgentDelayMs := 15
	policy := &RetryPolicy{Enabled: true, MaxRetries: 4, BaseDelayMs: 10, MaxAgentDelayMs: &maxAgentDelayMs}
	calls := 0
	var got []int
	callbacks := &RetryCallbacks{
		OnRetryScheduled: func(_, _, delayMs int, _ string) { got = append(got, delayMs) },
	}
	produce := func() *AssistantMessage {
		calls++
		if calls == 5 {
			return okResp()
		}
		return errResp("overloaded")
	}
	res := RetryAssistantCall(context.Background(), produce, policy, callbacks)
	if res.StopReason != StopStop {
		t.Fatalf("stop = %v, want StopStop", res.StopReason)
	}
	want := []int{10, 15, 15, 15}
	if len(got) != len(want) {
		t.Fatalf("scheduled delays = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("scheduled delays = %v, want %v", got, want)
		}
	}
}
