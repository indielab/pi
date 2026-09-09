package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// CHARACTERIZATION tests — they were green before they were written, and that
// is the finding they record.
//
// They transliterate pi's packages/ai/test/opencode-provider-headers.test.ts,
// added by upstream 561a2e066 ("fix(ai): send OpenCode session header"). That
// commit wraps the opencode / opencode-go provider api tables in
// withOpenCodeSessionHeader, which injects x-opencode-session from
// options.sessionId when no source has already named the header — closing a gap
// where a bare @earendil-works/pi-ai consumer sent nothing, because pi only ever
// set the header from the coding-agent layer (core/provider-attribution.ts).
//
// The Go port has no such gap to close: it homes pi's coding-agent attribution
// in ai/providers/attribution.go, so ai.Stream* has always emitted the header
// for opencode models whenever SessionID is set. The port's delta for
// 561a2e066 is therefore EMPTY, and these assertions — pi's own, values
// included — are what says so rather than a category argument.
//
// They assert only the cells where the port matches pi on BOTH of pi's paths.
// Where pi's two paths disagree (a Model.Headers spelling of the same name, the
// opencode.ai host gate, and x-opencode-client on the bare-SDK path) the port
// necessarily matches one and not the other, because one Go function stands in
// for both. That residual is tracked as Scope queue entry 15, not asserted here:
// a green test carrying our own value would immunise it.

const opencodeDoneSSE = "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"

// captureOpenCodeHeaders runs one opencode request against a stub server and
// returns the request headers. entry selects pi's stream vs streamSimple, which
// the wrapper covers alike.
func captureOpenCodeHeaders(t *testing.T, entry string, provider ai.ProviderId, sessionID string,
	retention ai.CacheRetention, modelHeaders, optsHeaders ai.ProviderHeaders) http.Header {
	t.Helper()
	t.Setenv("PI_TELEMETRY", "1")
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, opencodeDoneSSE)
	}))
	defer server.Close()

	model := &ai.Model{
		ID: "test-model", Name: "Test model", Api: ai.APIOpenAICompletions, Provider: provider,
		BaseURL: server.URL, Input: []string{"text"}, MaxTokens: 100, ContextWindow: 1000,
		Headers: modelHeaders,
	}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 0)}}
	stream := ai.StreamOptions{
		ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "test-key", Headers: optsHeaders},
		SessionID:              sessionID,
		CacheRetention:         retention,
	}
	switch entry {
	case "stream":
		StreamOpenAICompletions(context.Background(), model, req, &OpenAIOptions{StreamOptions: stream}).Result()
	case "streamSimple":
		StreamSimpleOpenAICompletions(context.Background(), model, req, &ai.SimpleStreamOptions{StreamOptions: stream}).Result()
	default:
		t.Fatalf("unknown entry %q", entry)
	}
	return got
}

// pi: "maps sessionId for %s requests even without cache retention" — the
// header is independent of cacheRetention, which gates only the separate
// prompt-cache and session-affinity headers.
func TestOpenCodeSessionHeaderWithoutCacheRetention(t *testing.T) {
	for _, provider := range []ai.ProviderId{"opencode", "opencode-go"} {
		for _, entry := range []string{"stream", "streamSimple"} {
			t.Run(string(provider)+"/"+entry, func(t *testing.T) {
				h := captureOpenCodeHeaders(t, entry, provider, "conversation-1", ai.CacheNone, nil, nil)
				if got := h.Get("x-opencode-session"); got != "conversation-1" {
					t.Fatalf("x-opencode-session = %q, want conversation-1", got)
				}
			})
		}
	}
}

// pi: "preserves a case-insensitive caller override" — the wrapper's hasHeader
// is key-only and case-insensitive, so a caller value stands however it is
// spelled. The port reaches the same place from the other side: opts.Headers is
// the highest-precedence source, and net/http canonicalizes the name.
func TestOpenCodeSessionHeaderCallerOverride(t *testing.T) {
	h := captureOpenCodeHeaders(t, "streamSimple", "opencode", "generated-value", "",
		nil, ai.ProviderHeaders{"X-OpenCode-Session": ai.HeaderValue("caller-value")})
	if got := h.Get("x-opencode-session"); got != "caller-value" {
		t.Fatalf("x-opencode-session = %q, want caller-value (the caller's spelling wins)", got)
	}
}

// pi: the same test's null case. A caller deletion marker is preserved rather
// than substituted, and downstream that means the header is not sent at all.
func TestOpenCodeSessionHeaderCallerDeletionMarker(t *testing.T) {
	h := captureOpenCodeHeaders(t, "streamSimple", "opencode", "generated-value", "",
		nil, ai.ProviderHeaders{"X-OpenCode-Session": nil})
	if got := h.Get("x-opencode-session"); got != "" {
		t.Fatalf("a caller deletion marker must suppress the header, got %q", got)
	}
}

// pi: "does not fabricate a session header when sessionId is absent".
func TestOpenCodeSessionHeaderAbsentWithoutSessionID(t *testing.T) {
	h := captureOpenCodeHeaders(t, "streamSimple", "opencode", "", "",
		nil, ai.ProviderHeaders{"x-custom": ai.HeaderValue("value")})
	if got := h.Get("x-opencode-session"); got != "" {
		t.Fatalf("x-opencode-session must be absent without a session id, got %q", got)
	}
	if got := h.Get("x-custom"); got != "value" {
		t.Fatalf("x-custom = %q, want value", got)
	}
}
