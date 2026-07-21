package ai

import (
	"context"
	"time"
)

// Policy-driven assistant-call retry, ported from pi
// packages/ai/src/utils/retry.ts (upstream 65dd2e0e). Kept alongside
// IsRetryableAssistantError so the classifier and the retry loop live together
// and stay reusable by the SDK and other callers. Latent in the Go SDK today:
// the summary/branch-summary retry consumers that wire this in pi live in the
// unported agent-harness/host layer (see the 2026-06-25 ruling — retry.ts
// additions are ported to mirror pi's SDK structure).

// RetryPolicy is bounded retry with exponential backoff
// (baseDelayMs * 2^(attempt-1)). Mirrors pi's RetryPolicy / settings.retry.
type RetryPolicy struct {
	Enabled bool
	// MaxRetries is the max retry attempts (0 = no retries). The initial call
	// never counts as a retry.
	MaxRetries int
	// BaseDelayMs is the base backoff; the per-attempt delay is
	// BaseDelayMs * 2^(attempt-1) before any jitter.
	BaseDelayMs int
}

// RetryCallbacks are the optional hooks pi's retryAssistantCall emits around
// each retry. Any field may be nil.
type RetryCallbacks struct {
	// OnRetryScheduled fires before the backoff sleep of each retry attempt
	// (1-indexed).
	OnRetryScheduled func(attempt, maxAttempts, delayMs int, errorMessage string)
	// OnRetryAttemptStart fires after the backoff sleep, immediately before the
	// retried call starts.
	OnRetryAttemptStart func()
	// OnRetryFinished fires once when the loop ends: success is true if a later
	// call completed normally.
	OnRetryFinished func(success bool, attempt int, finalError string)
}

// RetryAssistantCall runs a single assistant-producing call with bounded retry
// on transient errors (pi retryAssistantCall).
//
// Behavior:
//   - A successful response returns immediately. Aborts are terminal and never
//     retried, but reported as unsuccessful if they happen after a retry was
//     scheduled. Aborts during the backoff sleep are normalized to an aborted
//     AssistantMessage too, so callers need not care when cancellation happened.
//   - A non-retryable error (per IsRetryableAssistantError, including quota/
//     billing exhaustion) returns immediately so deterministic errors fail fast.
//   - Otherwise retries up to policy.MaxRetries times with exponential backoff,
//     emitting OnRetryScheduled before each sleep, OnRetryAttemptStart after each
//     sleep before the retried call starts, and OnRetryFinished once at the end
//     (whether the loop ends in success, exhausted retries, or an aborted sleep).
//
// When policy is nil or disabled, the first response is returned unchanged
// (equivalent to calling produce() directly).
func RetryAssistantCall(ctx context.Context, produce func() *AssistantMessage, policy *RetryPolicy, callbacks *RetryCallbacks) *AssistantMessage {
	maxAttempts := 0
	if policy != nil && policy.Enabled {
		maxAttempts = policy.MaxRetries
	}

	attempt := 0
	lastRetryScheduled := false
	lastRetryAttempt := 0
	for {
		response := produce()

		// Abort: terminal but not successful. Never retry an aborted message.
		if response.StopReason == StopAborted {
			if lastRetryScheduled && callbacks != nil && callbacks.OnRetryFinished != nil {
				callbacks.OnRetryFinished(false, lastRetryAttempt, "")
			}
			return response
		}

		// Success: non-error, non-abort responses return as-is.
		if response.StopReason != StopError {
			if lastRetryScheduled && callbacks != nil && callbacks.OnRetryFinished != nil {
				callbacks.OnRetryFinished(true, lastRetryAttempt, "")
			}
			return response
		}

		// Non-retryable, or budget exhausted: return the final error message.
		if attempt >= maxAttempts || !IsRetryableAssistantError(*response) {
			if lastRetryScheduled && callbacks != nil && callbacks.OnRetryFinished != nil {
				callbacks.OnRetryFinished(false, lastRetryAttempt, response.ErrorMessage)
			}
			return response
		}

		attempt++
		lastRetryScheduled = true
		lastRetryAttempt = attempt
		errorMessage := response.ErrorMessage
		if errorMessage == "" {
			errorMessage = "Unknown error"
		}
		delayMs := policy.BaseDelayMs << (attempt - 1)
		if callbacks != nil && callbacks.OnRetryScheduled != nil {
			callbacks.OnRetryScheduled(attempt, maxAttempts, delayMs, errorMessage)
		}

		// Normalize aborts during retry backoff to the same AssistantMessage shape
		// as provider stream aborts, so callers need not care when cancellation
		// happened.
		timer := time.NewTimer(time.Duration(delayMs) * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			if callbacks != nil && callbacks.OnRetryFinished != nil {
				callbacks.OnRetryFinished(false, attempt, errorMessage)
			}
			aborted := *response
			aborted.StopReason = StopAborted
			aborted.ErrorMessage = ""
			return &aborted
		}
		if callbacks != nil && callbacks.OnRetryAttemptStart != nil {
			callbacks.OnRetryAttemptStart()
		}
	}
}
