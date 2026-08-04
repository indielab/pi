package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/sky-valley/pi/ai"
)

const (
	// defaultMaxRetryDelayMs is the ceiling on a server-requested Retry-After
	// delay. Exceeding it fails the request outright (pi's
	// validateServerRetryDelayMs); set MaxRetryDelayMs to 0 to disable.
	defaultMaxRetryDelayMs = 60_000
	defaultTimeoutMs       = 600_000 // 10 minutes (matches the OpenAI/Anthropic SDK default)
	retryBaseDelayMs       = 500     // openai SDK initialRetryDelay = 0.5s
	retryBackoffCapMs      = 8_000   // openai SDK maxRetryDelay = 8s
)

// retryConfig captures the retry/timeout knobs resolved from StreamOptions.
type retryConfig struct {
	maxRetries      int
	maxRetryDelayMs int
	timeoutMs       int
	// providerError renders the SDK APIError message that pi interpolates into
	// the fail-fast error, and selects whether fail-fast applies at all. pi
	// wraps only the providers whose requests go through retryProviderRequest
	// (anthropic-messages, openai-completions, openai-responses); its Google
	// provider streams via @google/genai and was untouched by 7af8533c. A nil
	// renderer therefore means "pi does not fail fast here", and an oversized
	// server delay keeps the pre-7af8533c behavior of falling back to backoff.
	// b9d360a2c later routed Google through retryProviderRequest too, but its
	// ApiError carries no headers, so no server delay ever reaches the check
	// and the nil renderer still describes it.
	providerError func(status int, body []byte) string
	// httpClient overrides the shared client (pi StreamOptions.fetch). Nil keeps
	// sharedClient, whose transport carries the timeoutMs response-header cap.
	httpClient ai.HTTPDoer
}

// retryFromOptions mirrors pi's `maxRetries: options?.maxRetries ?? 0` passed
// to the SDKs: an unset/zero MaxRetries means ZERO retries (single attempt).
// providerError is the caller's SDK-message renderer; see retryConfig.
func retryFromOptions(o ai.StreamOptions, providerError func(status int, body []byte) string) retryConfig {
	cfg := retryConfig{
		maxRetries:      o.MaxRetries,
		maxRetryDelayMs: defaultMaxRetryDelayMs,
		timeoutMs:       o.TimeoutMs,
		providerError:   providerError,
	}
	if c, ok := customHTTPClient(o.HTTPClient); ok {
		cfg.httpClient = c
	}
	if cfg.maxRetries < 0 {
		cfg.maxRetries = 0
	}
	if o.MaxRetryDelayMs != nil {
		cfg.maxRetryDelayMs = *o.MaxRetryDelayMs
	}
	if cfg.timeoutMs <= 0 {
		cfg.timeoutMs = defaultTimeoutMs
	}
	return cfg
}

// customHTTPClient reports whether opts carries an HTTP client that actually
// overrides the provider default, and returns it. pi blesses
// `fetch === globalThis.fetch` as equivalent to unset, so http.DefaultClient —
// the Go stand-in for that default — must mean "unset" everywhere, not just in
// the google adapter's guard.
func customHTTPClient(c ai.HTTPDoer) (ai.HTTPDoer, bool) {
	if c == nil || c == ai.HTTPDoer(http.DefaultClient) {
		return nil, false
	}
	return c, true
}

// clientCache memoizes http.Clients keyed by response-header timeout so we reuse
// connection pools across requests.
var (
	clientMu    sync.Mutex
	clientCache = map[int]*http.Client{}
)

// sharedClient returns an http.Client whose transport caps the time to first
// response byte at timeoutMs (ResponseHeaderTimeout). It deliberately leaves the
// streaming body read uncapped so long SSE responses are not severed.
func sharedClient(timeoutMs int) *http.Client {
	clientMu.Lock()
	defer clientMu.Unlock()
	if c, ok := clientCache[timeoutMs]; ok {
		return c
	}
	var tr *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = base.Clone()
	} else {
		// http.DefaultTransport was replaced with a non-*http.Transport (e.g.
		// by instrumentation); fall back to a fresh transport mirroring Go's
		// defaults instead of dereferencing a nil from the failed assertion.
		tr = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}
	tr.ResponseHeaderTimeout = time.Duration(timeoutMs) * time.Millisecond
	c := &http.Client{Transport: tr}
	clientCache[timeoutMs] = c
	return c
}

// shouldRetryResponse implements the openai SDK retry matrix (which pi
// delegates to): only non-2xx responses are considered; an explicit
// `x-should-retry` header overrides the status logic; otherwise 408, 409,
// 429, and all >=500 statuses are retryable.
func shouldRetryResponse(resp *http.Response) bool {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return false
	}
	switch resp.Header.Get("x-should-retry") {
	case "true":
		return true
	case "false":
		return false
	}
	switch resp.StatusCode {
	case http.StatusRequestTimeout, // 408
		http.StatusConflict,        // 409
		http.StatusTooManyRequests: // 429
		return true
	}
	return resp.StatusCode >= 500
}

// parseFloatPrefix mirrors JavaScript's Number.parseFloat, which pi uses to read
// the Retry-After headers: leading whitespace is skipped, the longest valid
// numeric prefix is consumed, and trailing junk is ignored (so "3600s" parses as
// 3600). ok=false stands for JS NaN — no numeric prefix at all. A prefix that
// overflows float64 yields ±Inf, matching JS ("1e400" is Infinity, not NaN);
// the caller clamps it before building a Duration. The "Infinity" literal is
// accepted too, because parseFloat does accept it — case-sensitively and as a
// prefix, so "Infinityx" is Infinity while "Inf" and "infinity" are NaN.
func parseFloatPrefix(s string) (float64, bool) {
	// JS StrWhiteSpace is the Unicode space separators plus the BOM; Go's
	// unicode.IsSpace covers the former (U+1680, U+2000-200A, U+2028/9,
	// U+202F, U+205F, U+3000) which a hand-written set kept missing.
	s = strings.TrimLeftFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || r == '\ufeff'
	})
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	// parseFloat accepts the Infinity literal, exact-case, as a prefix.
	if strings.HasPrefix(s[i:], "Infinity") {
		if s[0] == '-' {
			return math.Inf(-1), true
		}
		return math.Inf(1), true
	}
	digits := 0
	for ; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for ; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
			digits++
		}
	}
	if digits == 0 {
		return 0, false
	}
	end := i
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		k := j
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k > j {
			end = k
		}
	}
	f, err := strconv.ParseFloat(s[:end], 64)
	// An out-of-range prefix is ±Inf in JS, not NaN, so only a genuine syntax
	// error counts as "no numeric prefix".
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}
	return f, true
}

// serverRetryDelayMs extracts a server-requested retry delay in milliseconds,
// mirroring pi's getRetryDelayMs header handling. `retry-after-ms` wins when it
// parses; otherwise `Retry-After` is read as seconds, falling back to an HTTP
// date. A present-but-unparseable `Retry-After` still counts as server-dictated:
// pi's `Date.parse(...) - Date.now()` yields NaN there, which its abortable
// sleep clamps to an immediate retry rather than falling back to backoff.
// ok=false means no header dictated the delay.
func serverRetryDelayMs(resp *http.Response) (float64, bool) {
	if resp == nil {
		return 0, false
	}
	if v := resp.Header.Get("retry-after-ms"); v != "" {
		if ms, ok := parseFloatPrefix(v); ok {
			return ms, true
		}
	}
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, ok := parseFloatPrefix(ra); ok {
			return secs * 1000, true
		}
		if t, err := http.ParseTime(ra); err == nil {
			return float64(time.Until(t).Milliseconds()), true
		}
		return 0, true
	}
	return 0, false
}

// validateServerRetryDelay ports pi's validateServerRetryDelayMs: a
// server-requested delay above maxRetryDelayMs fails the request immediately
// instead of being clamped or ignored, so the visible agent-level retry policy
// handles it. maxRetryDelayMs <= 0 disables the limit.
//
// providerMsg is the already-rendered SDK APIError message, matching the
// `providerErrorMessage: string` parameter pi passes.
//
// errServerRetryDelayTooLong wraps the result so the agent-level retry policy
// this defers to can recognize the condition; pi throws a plain Error, but
// string-matching is not an API.
func validateServerRetryDelay(delayMs float64, maxRetryDelayMs int, providerMsg string) error {
	if maxRetryDelayMs <= 0 || delayMs <= float64(maxRetryDelayMs) {
		return nil
	}
	// The message is pi's, byte-for-byte, capitalization included. Do not
	// "fix" it — tests byte-compare it against the TS template literal.
	return &serverRetryDelayError{msg: fmt.Sprintf(
		"Server requested %ss retry delay (max: %ss). %s",
		ceilSeconds(delayMs),
		ceilSeconds(float64(maxRetryDelayMs)),
		providerMsg)}
}

// errServerRetryDelayTooLong marks a request abandoned because the server asked
// to be retried later than maxRetryDelayMs allows. pi throws a plain Error here;
// a sentinel lets the agent-level retry policy this defers to recognize the
// condition without string-matching, while Error() stays byte-identical to pi.
var errServerRetryDelayTooLong = errors.New("server retry delay exceeds limit")

type serverRetryDelayError struct{ msg string }

func (e *serverRetryDelayError) Error() string { return e.msg }

func (e *serverRetryDelayError) Is(target error) bool { return target == errServerRetryDelayTooLong }

// ceilSeconds renders the numeric part of pi's `${Math.ceil(ms / 1000)}s`,
// including JS's "Infinity" spelling for a header value that overflowed float64.
func ceilSeconds(ms float64) string {
	secs := math.Ceil(ms / 1000)
	if math.IsInf(secs, 1) {
		return "Infinity"
	}
	return strconv.FormatFloat(secs, 'f', -1, 64)
}

// maxServerDelayMs is the largest millisecond delay representable as a Duration.
const maxServerDelayMs = float64(math.MaxInt64 / int64(time.Millisecond))

// serverDelayDuration converts a validated server-requested delay. Negative
// values (a Retry-After date in the past) retry immediately, matching pi's
// `Math.max(0, ms)`, and the upper clamp keeps an absurd delay from wrapping
// int64 nanoseconds into a negative Duration.
func serverDelayDuration(ms float64) time.Duration {
	switch {
	case ms < 0:
		ms = 0
	case ms > maxServerDelayMs:
		ms = maxServerDelayMs
	}
	// JS setTimeout truncates its delay to an integer millisecond count.
	return time.Duration(ms) * time.Millisecond
}

// backoffDelay is pi's computed fallback: min(0.5s * 2^attempt, 8s) with up to
// 25% downward jitter. maxRetryDelayMs bounds only a server-requested delay,
// never this.
func backoffDelay(attempt int) time.Duration {
	backoff := math.Min(float64(retryBaseDelayMs)*math.Pow(2, float64(attempt)), retryBackoffCapMs)
	jitter := 1 - rand.Float64()*0.25
	return time.Duration(backoff*jitter) * time.Millisecond
}

// retryDelay computes the wait before the next attempt, mirroring pi's
// getRetryDelayMs: a server-dictated delay wins once it passes
// validateServerRetryDelay, otherwise the computed backoff applies.
func retryDelay(resp *http.Response, attempt int, cfg retryConfig, providerMsg string) (time.Duration, error) {
	if ms, ok := serverRetryDelayMs(resp); ok && cfg.providerError != nil {
		if err := validateServerRetryDelay(ms, cfg.maxRetryDelayMs, providerMsg); err != nil {
			return 0, err
		}
		return serverDelayDuration(ms), nil
	} else if ok {
		// pi does not wrap this provider, so an oversized delay is not fatal;
		// honor what fits and otherwise fall through to the backoff.
		if ms >= 0 && (cfg.maxRetryDelayMs <= 0 || ms <= float64(cfg.maxRetryDelayMs)) {
			return serverDelayDuration(ms), nil
		}
	}
	return backoffDelay(attempt), nil
}

// sendWithRetry issues the request built by build, retrying transient network
// errors (like 5xx) and retryable HTTP statuses with backoff. build must
// produce a fresh *http.Request on each call (request bodies are single-use).
// With cfg.maxRetries == 0 (pi's default) exactly one attempt is made.
//
// For providers pi wraps (cfg.providerError non-nil) a server-requested delay
// above cfg.maxRetryDelayMs terminates the loop with the fail-fast error from
// validateServerRetryDelay.
func sendWithRetry(ctx context.Context, build func() (*http.Request, error), cfg retryConfig) (*http.Response, error) {
	client := cfg.httpClient
	if client == nil {
		client = sharedClient(cfg.timeoutMs)
	}
	attempts := cfg.maxRetries + 1
	var lastErr error

	for attempt := 0; attempt < attempts; attempt++ {
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		req, err := build()
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt == attempts-1 {
				return nil, err
			}
			// No response, so no server-requested delay: pure backoff.
			if !sleepCtx(ctx, backoffDelay(attempt)) {
				return nil, ctx.Err()
			}
			continue
		}
		if shouldRetryResponse(resp) && attempt < attempts-1 {
			// The body is only needed to quote the provider in a fail-fast
			// error, so render it lazily.
			var providerMsg string
			body := readAndCloseBody(resp)
			if cfg.providerError != nil {
				providerMsg = cfg.providerError(resp.StatusCode, body)
			}
			delay, err := retryDelay(resp, attempt, cfg, providerMsg)
			if err != nil {
				return nil, err
			}
			if !sleepCtx(ctx, delay) {
				return nil, ctx.Err()
			}
			continue
		}
		return resp, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("request failed after %d attempts", attempts)
}

// readAndCloseBody consumes and closes a retryable response body, returning what
// it read so a fail-fast error can quote the provider's message. The 1 MiB cap
// matches the volume the previous discard-only drain read, so connection reuse
// is unchanged.
func readAndCloseBody(resp *http.Response) []byte {
	if resp == nil || resp.Body == nil {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	return body
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	if ctx == nil {
		time.Sleep(d)
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
