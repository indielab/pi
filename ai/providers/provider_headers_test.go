package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// Header deletion markers (upstream a24fb9e96). pi distinguishes three states
// per header name — absent (send the provider default), a string (send it, even
// when empty), and null (suppress the default) — and these tests pin all three
// on every adapter that merges ProviderHeaders.

// captureOpenAIHeaders runs one openai-completions request against a local
// server and returns the headers that reached the wire.
func captureOpenAIHeaders(t *testing.T, model *ai.Model, opts ai.StreamOptions) http.Header {
	t.Helper()
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	model.BaseURL = server.URL
	final := StreamOpenAICompletions(context.Background(), model,
		ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}},
		&OpenAIOptions{StreamOptions: opts}).Result()
	if final.StopReason == ai.StopError {
		t.Fatalf("stream failed: %s", final.ErrorMessage)
	}
	return got
}

func openAITestModel() *ai.Model {
	return &ai.Model{ID: "gpt-test", Api: ai.APIOpenAICompletions, Provider: "openai",
		Input: []string{"text"}, MaxTokens: 4096}
}

// The three states on a header the adapter itself would send.
func TestOpenAICompletionsHeaderStates(t *testing.T) {
	t.Run("absent keeps the default", func(t *testing.T) {
		h := captureOpenAIHeaders(t, openAITestModel(), ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "k"}})
		if got := h.Get("authorization"); got != "Bearer k" {
			t.Fatalf("authorization = %q, want the default Bearer k", got)
		}
	})
	t.Run("empty string sends an empty header", func(t *testing.T) {
		h := captureOpenAIHeaders(t, openAITestModel(), ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "k", Headers: ai.ProviderHeaders{"authorization": strPtr("")}},
		})
		values, ok := h["Authorization"]
		if !ok {
			t.Fatal("an empty string must still send the header, not suppress it")
		}
		if len(values) != 1 || values[0] != "" {
			t.Fatalf("authorization = %v, want one empty value", values)
		}
	})
	t.Run("null suppresses the default", func(t *testing.T) {
		h := captureOpenAIHeaders(t, openAITestModel(), ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "k", Headers: ai.ProviderHeaders{"authorization": nil}},
		})
		if _, ok := h["Authorization"]; ok {
			t.Fatalf("a deletion marker must remove the header, got %q", h.Get("authorization"))
		}
	})
}

// A marker in the consumer's headers suppresses an attribution default, which
// pi allows because attribution sits at the bottom of the same merged object
// (sdk.ts mergeProviderAttributionHeaders).
func TestOpenAICompletionsMarkerSuppressesAttribution(t *testing.T) {
	t.Setenv("PI_TELEMETRY", "1")
	model := openAITestModel()
	model.Provider = "openrouter"
	h := captureOpenAIHeaders(t, model, ai.StreamOptions{
		ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "k", Headers: ai.ProviderHeaders{"HTTP-Referer": nil}},
	})
	if _, ok := h["Http-Referer"]; ok {
		t.Fatalf("attribution default must be suppressible, got %q", h.Get("HTTP-Referer"))
	}
	if got := h.Get("X-OpenRouter-Title"); got != "pi" {
		t.Fatalf("unrelated attribution headers must survive: X-OpenRouter-Title = %q", got)
	}
}

// Precedence is unchanged by markers: model headers outrank attribution, and
// the consumer's headers outrank both — in either direction.
func TestOpenAICompletionsMarkerPrecedence(t *testing.T) {
	model := openAITestModel()
	model.Headers = ai.ProviderHeaders{"X-Both": nil}
	h := captureOpenAIHeaders(t, model, ai.StreamOptions{
		ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "k", Headers: ai.ProviderHeaders{"X-Both": strPtr("consumer")}},
	})
	if got := h.Get("X-Both"); got != "consumer" {
		t.Fatalf("consumer headers merge last: X-Both = %q, want consumer", got)
	}

	model = openAITestModel()
	model.Headers = ai.ProviderHeaders{"X-Both": strPtr("model")}
	h = captureOpenAIHeaders(t, model, ai.StreamOptions{
		ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "k", Headers: ai.ProviderHeaders{"X-Both": nil}},
	})
	if _, ok := h["X-Both"]; ok {
		t.Fatalf("a consumer marker must cancel the model header, got %q", h.Get("X-Both"))
	}
}

// clientAPIKey: pi's hasHeader requires `value !== null`, so a marker is not a
// credential — the request must fail exactly as if the header were absent.
func TestClientAPIKeyIgnoresDeletionMarkers(t *testing.T) {
	_, err := clientAPIKey("custom", "", ai.ProviderHeaders{"Authorization": nil, "cf-aig-authorization": nil})
	if err == nil || err.Error() != "No API key for provider: custom" {
		t.Fatalf("deletion markers must not count as auth, got %v", err)
	}
}

// Cloudflare AI Gateway auth is now expressed as markers rather than as a
// per-adapter "skip the Authorization header" conditional: openai-completions
// writes its normal auth header and the bundle deletes it.
func TestOpenAICompletionsCloudflareAIGatewayAuth(t *testing.T) {
	model := openAITestModel()
	model.Provider = "cloudflare-ai-gateway"
	h := captureOpenAIHeaders(t, model, ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "cf-key"}})
	if got := h.Get("cf-aig-authorization"); got != "Bearer cf-key" {
		t.Fatalf("cf-aig-authorization = %q, want Bearer cf-key", got)
	}
	if _, ok := h["Authorization"]; ok {
		t.Fatalf("the gateway must suppress the placeholder credential, got %q", h.Get("authorization"))
	}

	// The suppression sits where pi's resolved auth headers sit — below the
	// model and consumer headers — so a real upstream credential still gets
	// through to the gateway.
	model = openAITestModel()
	model.Provider = "cloudflare-ai-gateway"
	model.Headers = ai.ProviderHeaders{"Authorization": strPtr("Bearer upstream")}
	h = captureOpenAIHeaders(t, model, ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "cf-key"}})
	if got := h.Get("authorization"); got != "Bearer upstream" {
		t.Fatalf("model headers outrank the auth marker: authorization = %q", got)
	}
	if got := h.Get("cf-aig-authorization"); got != "Bearer cf-key" {
		t.Fatalf("cf-aig-authorization = %q, want Bearer cf-key", got)
	}
}

// With the conditional skip gone, a cloudflare-ai-gateway anthropic model takes
// pi's plain api-key branch (pi resolves the gateway to headers with no apiKey),
// so it sends the session-affinity header its compat asks for — while x-api-key,
// which that branch writes, is removed by the marker.
func TestAnthropicCloudflareAIGatewayTakesApiKeyBranch(t *testing.T) {
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct123")
	t.Setenv("CLOUDFLARE_GATEWAY_ID", "gw456")
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, anthropicSSE)
	}))
	defer server.Close()
	model := &ai.Model{
		ID: "claude-test", Api: ai.APIAnthropicMessages, Provider: "cloudflare-ai-gateway",
		BaseURL: server.URL + "/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/anthropic",
		Input:   []string{"text"}, MaxTokens: 4096,
		Compat: []byte(`{"sendSessionAffinityHeaders":true}`),
	}
	final := StreamAnthropic(context.Background(), model,
		ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}},
		&AnthropicOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "cf-key"}, SessionID: "sess-1"}}).Result()
	if final.StopReason == ai.StopError {
		t.Fatalf("stream failed: %s", final.ErrorMessage)
	}
	if got.Get("x-session-affinity") != "sess-1" {
		t.Fatalf("x-session-affinity = %q, want sess-1", got.Get("x-session-affinity"))
	}
	if _, ok := got["X-Api-Key"]; ok {
		t.Fatalf("x-api-key must be removed by the marker, got %q", got.Get("x-api-key"))
	}
	if got.Get("cf-aig-authorization") != "Bearer cf-key" {
		t.Fatalf("cf-aig-authorization = %q, want Bearer cf-key", got.Get("cf-aig-authorization"))
	}
}

// google builds its own request, so pi runs the merged headers through
// providerHeadersToRecord: a marker cancels earlier ProviderHeaders sources but
// cannot unset a header the adapter owns.
func TestGoogleHeaderStates(t *testing.T) {
	t.Setenv("PI_TELEMETRY", "1")
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, googleSSE)
	}))
	defer server.Close()
	model := &ai.Model{ID: "gemini-2.5-flash", Api: ai.APIGoogleGenerativeAI,
		Provider: "cloudflare-workers-ai", BaseURL: server.URL,
		Headers: ai.ProviderHeaders{"X-Model": strPtr("from-model"), "X-Empty": strPtr("")}}
	final := StreamGoogle(context.Background(), model,
		ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}},
		&GoogleOptions{StreamOptions: ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "g-key", Headers: ai.ProviderHeaders{
				"X-Model":        nil,
				"User-Agent":     nil,
				"x-goog-api-key": nil,
			}},
		}}).Result()
	if final.StopReason == ai.StopError {
		t.Fatalf("stream failed: %s", final.ErrorMessage)
	}
	if _, ok := got["X-Model"]; ok {
		t.Fatalf("a marker must cancel the model header before the record is built, got %q", got.Get("X-Model"))
	}
	// net/http supplies its own User-Agent once ours is gone, exactly as fetch
	// does for pi; what must not survive is the attribution value.
	if got := got.Get("User-Agent"); got == "pi-coding-agent" {
		t.Fatal("a marker must cancel the attribution default")
	}
	if got.Get("X-Empty") != "" {
		t.Fatalf("X-Empty = %q, want an empty value", got.Get("X-Empty"))
	}
	if _, ok := got["X-Empty"]; !ok {
		t.Fatal("an empty string must still be sent")
	}
	// pi's providerHeadersToRecord only drops entries; the SDK-owned api-key
	// header is not part of the merged object and survives.
	if got.Get("x-goog-api-key") != "g-key" {
		t.Fatalf("x-goog-api-key = %q, want the adapter's own value", got.Get("x-goog-api-key"))
	}
}

// pi-messages spreads providerHeadersToRecord(options.headers) into an object
// literal that already holds its three fixed headers, so a marker there means
// "not sent" and cannot remove the authorization the adapter just wrote.
func TestPiMessagesHeaderStates(t *testing.T) {
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, piMessagesSSE(
			`{"type":"start"}`,
			`{"type":"done","reason":"stop","usage":`+piMessagesUsageJSON+`,"responseId":"resp_1"}`,
		))
	}))
	defer server.Close()
	final := StreamPiMessages(context.Background(), piMessagesTestModel(server.URL+"/v1"),
		piMessagesTestContext(), &PiMessagesOptions{StreamOptions: ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "test-key", Headers: ai.ProviderHeaders{
				"authorization": nil,
				"x-custom":      nil,
				"x-empty":       strPtr(""),
			}},
		}}).Result()
	if final.StopReason == ai.StopError {
		t.Fatalf("stream failed: %s", final.ErrorMessage)
	}
	if got.Get("authorization") != "Bearer test-key" {
		t.Fatalf("authorization = %q; a marker must not unset the adapter's own header", got.Get("authorization"))
	}
	if _, ok := got["X-Custom"]; ok {
		t.Fatalf("a marker must not be sent as a header, got %q", got.Get("x-custom"))
	}
	if _, ok := got["X-Empty"]; !ok {
		t.Fatal("an empty string must still be sent")
	}
}

// applyProviderHeaders must not depend on Go map iteration order. Two names
// that differ only by case are distinct ProviderHeaders keys but canonicalize
// to one http.Header key, so a marker and a value can collide on it; sorted
// name order is the tie-break (see applyProviderHeaders), matching mergeHeaders.
// Only raw Model.Headers can carry such a pair — the merged path is
// case-deduped before it reaches here.
func TestApplyProviderHeadersCaseCollisionIsDeterministic(t *testing.T) {
	headers := ai.ProviderHeaders{
		// "Authorization" sorts before "authorization" (ASCII), so the Del runs
		// first and the value wins.
		"Authorization": nil,
		"authorization": strPtr("Bearer x"),
		// The mirror case: the value sorts first, so the marker runs last and
		// wins. This is also what separates sorted order from "apply markers
		// first", which would have kept the value here.
		"X-Trace": strPtr("on"),
		"x-trace": nil,
		"X-Keep":  strPtr("keep"),
	}
	// One run cannot tell a sorted implementation from a lucky one: Go
	// randomizes map iteration per range statement, so repeat enough that an
	// order-dependent implementation is overwhelmingly likely to disagree with
	// itself at least once.
	for i := range 200 {
		h := http.Header{}
		applyProviderHeaders(h, headers)
		if got := h.Get("authorization"); got != "Bearer x" {
			t.Fatalf("run %d: authorization = %q, want %q — the name sorting last must win", i, got, "Bearer x")
		}
		if _, present := h["X-Trace"]; present {
			t.Fatalf("run %d: X-Trace = %q, want it deleted — the marker sorts last", i, h.Get("X-Trace"))
		}
		if got := h.Get("X-Keep"); got != "keep" {
			t.Fatalf("run %d: X-Keep = %q, want keep", i, got)
		}
	}
}
