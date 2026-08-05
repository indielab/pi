package providers

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sky-valley/pi/ai"
)

// TestRetryDefaultIsZeroRetries locks pi's `maxRetries ?? 0`: with MaxRetries
// unset, exactly one attempt is made and the error surfaces immediately.
func TestRetryDefaultIsZeroRetries(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"error":"unavailable"}`)
	}))
	defer server.Close()

	model := &ai.Model{ID: "gpt-test", Api: ai.APIOpenAICompletions, Provider: "openai", BaseURL: server.URL, MaxTokens: 100}
	final := StreamOpenAICompletions(context.Background(), model,
		ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}},
		&OpenAIOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "k"}}}).Result()

	if final.StopReason != ai.StopError {
		t.Fatalf("expected error with zero default retries, got %s", final.StopReason)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected exactly 1 attempt (default 0 retries), got %d", calls)
	}
}

func TestProviderRetriesOn429ThenSucceeds(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("retry-after-ms", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"error":"rate limited"}`)
			return
		}
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, openAISSE)
	}))
	defer server.Close()

	model := &ai.Model{ID: "gpt-test", Api: ai.APIOpenAICompletions, Provider: "openai", BaseURL: server.URL, MaxTokens: 100}
	final := StreamOpenAICompletions(context.Background(), model,
		ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}},
		&OpenAIOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "k", MaxRetries: 2}}}).Result()

	if final.StopReason == ai.StopError {
		t.Fatalf("expected success after retry, got error: %s", final.ErrorMessage)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 attempts (1 retry), got %d", calls)
	}
}

func TestProviderStopsRetryingPastLimit(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("retry-after-ms", "1")
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"error":"unavailable"}`)
	}))
	defer server.Close()

	model := &ai.Model{ID: "gpt-test", Api: ai.APIOpenAICompletions, Provider: "openai", BaseURL: server.URL, MaxTokens: 100}
	final := StreamOpenAICompletions(context.Background(), model,
		ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}},
		&OpenAIOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "k", MaxRetries: 1}}}).Result()

	if final.StopReason != ai.StopError {
		t.Fatalf("expected error after exhausting retries, got %s", final.StopReason)
	}
	// maxRetries=1 => 2 attempts total.
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls)
	}
}

// TestRetry409Retried locks 409 Conflict into the retry matrix (openai SDK).
func TestRetry409Retried(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("retry-after-ms", "1")
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	build := func() (*http.Request, error) { return http.NewRequest("GET", server.URL, nil) }
	resp, err := sendWithRetry(context.Background(), build, retryConfig{maxRetries: 1, maxRetryDelayMs: defaultMaxRetryDelayMs, timeoutMs: defaultTimeoutMs})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after retrying 409, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls)
	}
}

// TestRetryXShouldRetryTrueForcesRetry: x-should-retry: true makes an
// otherwise non-retryable status (404) retryable.
func TestRetryXShouldRetryTrueForcesRetry(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("x-should-retry", "true")
			w.Header().Set("retry-after-ms", "1")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	build := func() (*http.Request, error) { return http.NewRequest("GET", server.URL, nil) }
	resp, err := sendWithRetry(context.Background(), build, retryConfig{maxRetries: 1, maxRetryDelayMs: defaultMaxRetryDelayMs, timeoutMs: defaultTimeoutMs})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected retry forced by x-should-retry:true, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls)
	}
}

// TestRetryXShouldRetryFalseSuppressesRetry: x-should-retry: false makes an
// otherwise retryable status (429) non-retryable.
func TestRetryXShouldRetryFalseSuppressesRetry(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("x-should-retry", "false")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	build := func() (*http.Request, error) { return http.NewRequest("GET", server.URL, nil) }
	resp, err := sendWithRetry(context.Background(), build, retryConfig{maxRetries: 3, maxRetryDelayMs: defaultMaxRetryDelayMs, timeoutMs: defaultTimeoutMs})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 surfaced, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected 1 attempt (retry suppressed), got %d", calls)
	}
}

// TestRetryAfterMsPreferredOverRetryAfter: retry-after-ms wins when both
// headers are present.
func TestRetryAfterMsPreferredOverRetryAfter(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("retry-after-ms", "5")
	resp.Header.Set("Retry-After", "30") // 30s — would dominate if used

	d := mustRetryDelay(t, resp, 0, defaultMaxRetryDelayMs)
	if d != 5*time.Millisecond {
		t.Fatalf("expected 5ms from retry-after-ms, got %v", d)
	}
}

// TestRetryAfterSecondsParsed: a plain-seconds Retry-After is honored.
func TestRetryAfterSecondsParsed(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "2")
	d := mustRetryDelay(t, resp, 0, defaultMaxRetryDelayMs)
	if d != 2*time.Second {
		t.Fatalf("expected 2s from Retry-After seconds, got %v", d)
	}
}

// TestRetryAfterHTTPDateParsed: an HTTP-date Retry-After is honored.
func TestRetryAfterHTTPDateParsed(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", time.Now().Add(10*time.Second).UTC().Format(http.TimeFormat))
	d := mustRetryDelay(t, resp, 0, defaultMaxRetryDelayMs)
	if d < 8*time.Second || d > 10*time.Second {
		t.Fatalf("expected ~10s from Retry-After http-date, got %v", d)
	}
}

// TestServerRetryDelayAboveLimitFailsFast locks pi's validateServerRetryDelayMs
// (provider-retry.ts): an oversized server-requested delay is no longer ignored
// in favor of backoff — it aborts the request. The message is byte-exact against
// pi's template, including the trailing SDK provider message.
func TestServerRetryDelayAboveLimitFailsFast(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}
	resp.Header.Set("Retry-After", "3600") // 1 hour
	body := []byte(`{"error":{"message":"slow down"}}`)

	_, err := retryDelay(resp, 0, wrapCfg(defaultMaxRetryDelayMs), openaiSDKErrorMessage(resp.StatusCode, body))
	if err == nil {
		t.Fatal("expected an oversized server delay to fail fast, got nil error")
	}
	const want = "Server requested 3600s retry delay (max: 60s). 429 slow down"
	if err.Error() != want {
		t.Fatalf("message mismatch\n got: %q\nwant: %q", err.Error(), want)
	}
}

// TestServerRetryDelayCeilsToSeconds: pi renders both sides with Math.ceil on a
// float division, so sub-second remainders round up.
func TestServerRetryDelayCeilsToSeconds(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}
	resp.Header.Set("retry-after-ms", "1200.5")

	_, err := retryDelay(resp, 0, wrapCfg(1001), openaiSDKErrorMessage(resp.StatusCode, nil))
	if err == nil {
		t.Fatal("expected fail-fast for 1200.5ms against a 1001ms limit")
	}
	const want = "Server requested 2s retry delay (max: 2s). 429 status code (no body)"
	if err.Error() != want {
		t.Fatalf("message mismatch\n got: %q\nwant: %q", err.Error(), want)
	}
}

// TestServerRetryDelayEqualToLimitHonored: pi's comparison is `delayMs >
// maxDelayMs`, so a delay exactly at the limit is slept, not rejected.
func TestServerRetryDelayEqualToLimitHonored(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}
	resp.Header.Set("retry-after-ms", "1000")

	d, err := retryDelay(resp, 0, wrapCfg(1000), "")
	if err != nil {
		t.Fatalf("delay equal to the limit must be honored, got %v", err)
	}
	if d != time.Second {
		t.Fatalf("expected 1s honored at the limit, got %v", d)
	}
}

// TestServerRetryDelayLimitDisabled: maxRetryDelayMs of 0 disables the limit
// (pi: `maxDelayMs > 0 && ...`), so an hour-long delay is honored verbatim.
func TestServerRetryDelayLimitDisabled(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}
	resp.Header.Set("Retry-After", "3600")

	d, err := retryDelay(resp, 0, wrapCfg(0), "")
	if err != nil {
		t.Fatalf("limit disabled, expected no error, got %v", err)
	}
	if d != time.Hour {
		t.Fatalf("expected the full 1h delay honored, got %v", d)
	}
}

// TestServerRetryDelayNonPositiveHonored: pi has no lower gate on the server
// delay; zero and negative values are honored and clamp to an immediate retry
// instead of falling back to exponential backoff.
func TestServerRetryDelayNonPositiveHonored(t *testing.T) {
	for _, header := range []string{"0", "-30"} {
		resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}
		resp.Header.Set("Retry-After", header)
		d, err := retryDelay(resp, 0, wrapCfg(defaultMaxRetryDelayMs), "")
		if err != nil {
			t.Fatalf("Retry-After %q: unexpected error %v", header, err)
		}
		if d != 0 {
			t.Fatalf("Retry-After %q: expected an immediate retry, got %v", header, d)
		}
	}
}

// TestServerRetryDelayUnparseableDateIsImmediate: a present-but-unparseable
// Retry-After still counts as server-dictated. pi computes `Date.parse(...) -
// Date.now()` = NaN, which its sleep clamps to zero rather than falling back to
// the exponential backoff.
func TestServerRetryDelayUnparseableDateIsImmediate(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}
	resp.Header.Set("Retry-After", "later")
	d, err := retryDelay(resp, 0, wrapCfg(defaultMaxRetryDelayMs), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 0 {
		t.Fatalf("expected an immediate retry for an unparseable Retry-After, got %v", d)
	}
}

// TestRetryAfterAboveLimitAbortsRequest: end-to-end — a huge Retry-After stops
// the retry loop with the fail-fast error instead of retrying after backoff.
func TestRetryAfterAboveLimitAbortsRequest(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"slow down"}}`)
	}))
	defer server.Close()

	build := func() (*http.Request, error) { return http.NewRequest("GET", server.URL, nil) }
	resp, err := sendWithRetry(context.Background(), build, retryConfig{
		maxRetries: 1, maxRetryDelayMs: defaultMaxRetryDelayMs, timeoutMs: defaultTimeoutMs,
		providerError: openaiSDKErrorMessage,
	})
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected the oversized Retry-After to abort the request")
	}
	if !errors.Is(err, errServerRetryDelayTooLong) {
		t.Fatalf("fail-fast error should match the sentinel, got %#v", err)
	}
	const want = "Server requested 3600s retry delay (max: 60s). 429 slow down"
	if err.Error() != want {
		t.Fatalf("message mismatch\n got: %q\nwant: %q", err.Error(), want)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected exactly 1 attempt before failing fast, got %d", calls)
	}
}

// TestParseFloatPrefix locks JS Number.parseFloat semantics on the Retry-After
// headers: a numeric prefix wins over trailing junk, and only a value with no
// numeric prefix at all is NaN (ok=false).
func TestParseFloatPrefix(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"3600", 3600, true},
		{"3600s", 3600, true}, // JS parseFloat stops at the unit suffix
		{" 12.5 ", 12.5, true},
		{"1e3", 1000, true},
		{"1e", 1, true}, // dangling exponent is not consumed
		{"-30", -30, true},
		{".5", 0.5, true},
		{"0x10", 0, true}, // JS stops at 'x'; Go's ParseFloat would read hex
		{"later", 0, false},
		{"", 0, false},
		// "Infinity" IS accepted by parseFloat; see TestParseFloatPrefixInfinityLiteral.
	}
	for _, c := range cases {
		got, ok := parseFloatPrefix(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Fatalf("parseFloatPrefix(%q) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestRetryBackoffJitterBounds: backoff is min(0.5s * 2^attempt, 8s) scaled by
// a jitter factor in (0.75, 1.0].
func TestRetryBackoffJitterBounds(t *testing.T) {
	for attempt, baseMs := range []int{500, 1000, 2000} {
		lo := time.Duration(float64(baseMs)*0.75) * time.Millisecond
		hi := time.Duration(baseMs) * time.Millisecond
		for i := 0; i < 200; i++ {
			d := mustRetryDelay(t, nil, attempt, defaultMaxRetryDelayMs)
			if d < lo || d > hi {
				t.Fatalf("attempt %d: delay %v outside jitter bounds [%v,%v]", attempt, d, lo, hi)
			}
		}
	}
}

// TestRetryBackoffCappedAt8s: the computed backoff never exceeds the openai
// SDK's 8s cap, jitter included.
func TestRetryBackoffCappedAt8s(t *testing.T) {
	for i := 0; i < 200; i++ {
		d := mustRetryDelay(t, nil, 10, defaultMaxRetryDelayMs) // 0.5s*2^10 = 512s uncapped
		if d > 8*time.Second {
			t.Fatalf("backoff exceeded 8s cap: %v", d)
		}
		if d < 6*time.Second { // 8s * 0.75 jitter floor
			t.Fatalf("capped backoff below jitter floor: %v", d)
		}
	}
}

// TestRetryMaxRetryDelayDoesNotCapBackoff: maxRetryDelayMs bounds only the
// server-requested delay. pi's getRetryDelayMs applies no maximum to the
// exponential path, so a 1s limit must not shrink the 8s backoff.
func TestRetryMaxRetryDelayDoesNotCapBackoff(t *testing.T) {
	for i := 0; i < 200; i++ {
		d := mustRetryDelay(t, nil, 10, 1000)
		if d < 6*time.Second || d > 8*time.Second {
			t.Fatalf("expected the uncapped 8s backoff (jitter floor 6s), got %v", d)
		}
	}
}

// TestUnwrappedProviderDoesNotFailFast: pi wraps only the providers whose
// requests go through retryProviderRequest (anthropic-messages,
// openai-completions, openai-responses). Its Google provider streams via
// @google/genai and was untouched by 7af8533c, so an oversized server delay
// there must NOT abort the request — it falls back to the computed backoff,
// exactly as every provider did before this change. b9d360a2c later wrapped
// Google as well, but its ApiError carries no headers, so no server delay ever
// reaches the check and the nil renderer still describes it.
func TestUnwrappedProviderDoesNotFailFast(t *testing.T) {
	resp := &http.Response{StatusCode: 429, Header: http.Header{}}
	resp.Header.Set("Retry-After", "3600") // 1h, far above the 60s limit

	cfg := retryConfig{maxRetryDelayMs: defaultMaxRetryDelayMs} // providerError nil
	d, err := retryDelay(resp, 0, cfg, "")
	if err != nil {
		t.Fatalf("an unwrapped provider must not fail fast, got %v", err)
	}
	// Falls through to backoff: 0.5s with up to 25% downward jitter.
	if d < 375*time.Millisecond || d > 500*time.Millisecond {
		t.Fatalf("expected the computed backoff, got %v", d)
	}
}

// TestServerRetryDelayOverflowClamped: a delay that overflows float64 is
// Infinity in JS (not NaN), so it fails fast — and must not wrap int64
// nanoseconds into a negative Duration on the way there.
func TestServerRetryDelayOverflowClamped(t *testing.T) {
	if f, ok := parseFloatPrefix("1e400"); !ok || !math.IsInf(f, 1) {
		t.Fatalf("parseFloatPrefix(1e400) = (%v, %v), want (+Inf, true)", f, ok)
	}
	resp := &http.Response{StatusCode: 429, Header: http.Header{}}
	resp.Header.Set("retry-after-ms", "1e400")

	_, err := retryDelay(resp, 0, wrapCfg(defaultMaxRetryDelayMs), "429 slow down")
	if err == nil {
		t.Fatal("an infinite server delay must fail fast")
	}
	// pi: `${Math.ceil(Infinity / 1000)}s` renders as "Infinitys".
	const want = "Server requested Infinitys retry delay (max: 60s). 429 slow down"
	if err.Error() != want {
		t.Fatalf("message mismatch\n got: %q\nwant: %q", err.Error(), want)
	}

	// With the limit disabled, the same value must clamp rather than wrap.
	d, err := retryDelay(resp, 0, retryConfig{maxRetryDelayMs: 0, providerError: openaiSDKErrorMessage}, "")
	if err != nil {
		t.Fatalf("limit disabled should not fail fast, got %v", err)
	}
	if d < 0 {
		t.Fatalf("clamped delay must not be negative, got %v", d)
	}
}

// wrapCfg builds a retryConfig for a provider pi wraps with
// retryProviderRequest, i.e. one where an oversized server delay fails fast.
func wrapCfg(maxRetryDelayMs int) retryConfig {
	return retryConfig{maxRetryDelayMs: maxRetryDelayMs, providerError: openaiSDKErrorMessage}
}

func mustRetryDelay(t *testing.T, resp *http.Response, attempt, maxRetryDelayMs int) time.Duration {
	t.Helper()
	d, err := retryDelay(resp, attempt, wrapCfg(maxRetryDelayMs), "")
	if err != nil {
		t.Fatalf("retryDelay: unexpected error: %v", err)
	}
	return d
}

// TestRetryFromOptionsDefaults locks the config resolution: MaxRetries
// zero-value stays 0, negatives clamp to 0, explicit values pass through.
func TestRetryFromOptionsDefaults(t *testing.T) {
	if got := retryFromOptions(ai.StreamOptions{}, openaiSDKErrorMessage); got.maxRetries != 0 {
		t.Fatalf("default maxRetries = %d, want 0", got.maxRetries)
	}
	if got := retryFromOptions(ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{MaxRetries: -3}}, openaiSDKErrorMessage); got.maxRetries != 0 {
		t.Fatalf("negative maxRetries = %d, want 0", got.maxRetries)
	}
	if got := retryFromOptions(ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{MaxRetries: 4}}, openaiSDKErrorMessage); got.maxRetries != 4 {
		t.Fatalf("explicit maxRetries = %d, want 4", got.maxRetries)
	}
	cap := 1234
	if got := retryFromOptions(ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{MaxRetryDelayMs: &cap}}, openaiSDKErrorMessage); got.maxRetryDelayMs != 1234 {
		t.Fatalf("maxRetryDelayMs override = %d, want 1234", got.maxRetryDelayMs)
	}
	if got := retryFromOptions(ai.StreamOptions{}, openaiSDKErrorMessage); got.maxRetryDelayMs != defaultMaxRetryDelayMs {
		t.Fatalf("default maxRetryDelayMs = %d, want %d", got.maxRetryDelayMs, defaultMaxRetryDelayMs)
	}
}

// TestRetryAbortWinsDuringBackoff: context cancellation during the backoff
// sleep aborts immediately instead of retrying.
func TestRetryAbortWinsDuringBackoff(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	build := func() (*http.Request, error) { return http.NewRequestWithContext(ctx, "GET", server.URL, nil) }
	_, err := sendWithRetry(ctx, build, retryConfig{maxRetries: 5, maxRetryDelayMs: defaultMaxRetryDelayMs, timeoutMs: defaultTimeoutMs})
	if err == nil || ctx.Err() == nil {
		t.Fatalf("expected context cancellation error, got %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected 1 attempt before abort, got %d", calls)
	}
}

// TestSharedClientSurvivesReplacedDefaultTransport: if http.DefaultTransport
// is replaced with a non-*http.Transport, sharedClient must not nil-deref.
func TestSharedClientSurvivesReplacedDefaultTransport(t *testing.T) {
	orig := http.DefaultTransport
	http.DefaultTransport = http.RoundTripper(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, nil
	}))
	defer func() { http.DefaultTransport = orig }()

	c := sharedClient(987_654) // unique timeout so the cache misses
	if c == nil || c.Transport == nil {
		t.Fatal("sharedClient returned nil client/transport with replaced DefaultTransport")
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport fallback, got %T", c.Transport)
	}
	if tr.ResponseHeaderTimeout != 987_654*time.Millisecond {
		t.Fatalf("fallback transport timeout = %v", tr.ResponseHeaderTimeout)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestResponsesPromptCacheKey(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = parseJSONWithRepair(string(body), &gotBody)
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, `data: {"type":"response.completed","response":{"id":"r","status":"completed"}}`+"\n\n")
	}))
	defer server.Close()

	model := &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "openai", BaseURL: server.URL, MaxTokens: 100}
	StreamOpenAIResponses(context.Background(), model,
		ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}},
		&OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "k"}, SessionID: "sess-123", CacheRetention: ai.CacheShort}}).Result()

	if gotBody["prompt_cache_key"] != "sess-123" {
		t.Fatalf("prompt_cache_key not sent: %v", gotBody["prompt_cache_key"])
	}
}

// TestParseFloatPrefixInfinityLiteral: JS Number.parseFloat accepts the
// "Infinity" literal — exact-case and as a prefix. Rejecting it inverted the
// outcome for `Retry-After: Infinity`: pi fails fast, and a NaN reading would
// instead retry immediately. Values captured from node.
func TestParseFloatPrefixInfinityLiteral(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"Infinity", math.Inf(1), true},
		{"+Infinity", math.Inf(1), true},
		{"-Infinity", math.Inf(-1), true},
		{"Infinityx", math.Inf(1), true}, // prefix match, trailing junk ignored
		{"  Infinity", math.Inf(1), true},
		{"Inf", 0, false},      // not the full literal
		{"infinity", 0, false}, // case-sensitive
		{"INFINITY", 0, false},
	}
	for _, c := range cases {
		got, ok := parseFloatPrefix(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseFloatPrefix(%q) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestRetryAfterInfinityFailsFast: the end-to-end consequence — an Infinity
// Retry-After must abort, not retry immediately.
func TestRetryAfterInfinityFailsFast(t *testing.T) {
	resp := &http.Response{StatusCode: 429, Header: http.Header{}}
	resp.Header.Set("Retry-After", "Infinity")
	_, err := retryDelay(resp, 0, wrapCfg(defaultMaxRetryDelayMs), "429 slow down")
	if err == nil {
		t.Fatal("an Infinity Retry-After must fail fast")
	}
	const want = "Server requested Infinitys retry delay (max: 60s). 429 slow down"
	if err.Error() != want {
		t.Fatalf("message mismatch\n got: %q\nwant: %q", err.Error(), want)
	}
}

// --- Google adapter retry (pi b9d360a2c retryGoogleRequest) ---

// TestGoogleRetriesTransientStatusThenSucceeds: the point of the upstream fix —
// a transient 5xx before the first token is retried instead of becoming a
// terminal errored assistant message. The Go port issues the request itself
// rather than through @google/genai, so it already sat on the shared retry
// path; this locks that substance against a regression.
func TestGoogleRetriesTransientStatusThenSucceeds(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"error":{"code":503,"message":"unavailable"}}`)
			return
		}
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, googleSSE)
	}))
	defer server.Close()

	final := googleRetryStream(t, server.URL, ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "k", MaxRetries: 1}}).Result()
	if final.StopReason == ai.StopError {
		t.Fatalf("expected success after retry, got error: %s", final.ErrorMessage)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 attempts (1 retry), got %d", got)
	}
}

func googleRetryStream(t *testing.T, baseURL string, opts ai.StreamOptions) *ai.AssistantMessageEventStream {
	t.Helper()
	model := &ai.Model{ID: "gemini-2.5-flash", Api: ai.APIGoogleGenerativeAI, Provider: "google", BaseURL: baseURL}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	return StreamGoogle(context.Background(), model, req, &GoogleOptions{StreamOptions: opts})
}
