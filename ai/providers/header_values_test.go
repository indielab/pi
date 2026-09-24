package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// Every request pi makes builds its headers in a fetch Headers object — the
// pi-messages adapter's fetch call, and the Headers each vendor SDK (openai,
// @anthropic-ai/sdk, @google/genai) assembles before it calls fetch — and
// Headers.append/set normalize a value by stripping leading and trailing HTTP
// whitespace (space, tab, CR, LF). net/http does neither: it refuses a value
// holding a CR or LF, failing the whole request, and its HTTP/2 encoder sends a
// value's leading and trailing spaces and tabs verbatim. Only its HTTP/1.1
// writer trims those, so the space and tab rows here run over HTTP/2, the
// protocol every TLS provider endpoint negotiates with Go.
//
// Every want was measured on pi's wire: upstream 8676a0dcd's src
// (packages/ai/src/api/*.ts) run under node v26.4.0 with openai 6.40.0,
// @anthropic-ai/sdk 0.124.0 and @google/genai 2.21.0 — the versions
// package-lock.json locks at that sha — against a raw socket that recorded the
// header lines as sent.

// wireAdapter drives one adapter against baseURL.
type wireAdapter struct {
	name string
	sse  string
	// auth is the header the api key reaches, and bearer whether the adapter
	// prefixes it (the prefix is joined before the value is normalized, so a
	// key's leading space survives inside "Bearer  k").
	auth   string
	bearer bool
	// http1 marks google: pi refuses a custom fetch there, so the port refuses
	// a custom client, and the default one cannot reach a self-signed HTTP/2
	// test server. Its requests go over HTTP/1.1, where only the CR and LF rows
	// are observable.
	http1 bool
	// run sends one request; client is nil for an http1 adapter.
	run func(baseURL string, client *http.Client, opts ai.StreamOptions) *ai.AssistantMessage
}

func wireAdapters() []wireAdapter {
	req := func() ai.TranscriptContext {
		return ai.NormalizeContext(ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}})
	}
	model := func(id string, api ai.Api, provider ai.ProviderId, baseURL string) *ai.Model {
		return &ai.Model{ID: id, Api: api, Provider: provider, BaseURL: baseURL, Input: []string{"text"}, MaxTokens: 4096}
	}
	return []wireAdapter{
		{name: "pi-messages", sse: piMessagesSSE(
			`{"type":"start"}`,
			`{"type":"done","reason":"stop","usage":`+piMessagesUsageJSON+`,"responseId":"resp_1"}`,
		), auth: "authorization", bearer: true, run: func(baseURL string, client *http.Client, opts ai.StreamOptions) *ai.AssistantMessage {
			opts.HTTPClient = client
			return StreamPiMessages(context.Background(), piMessagesTestModel(baseURL+"/v1"), req(),
				&PiMessagesOptions{StreamOptions: opts}).Result()
		}},
		{name: "google-generative-ai", sse: googleSSE, auth: "x-goog-api-key", http1: true, run: func(baseURL string, _ *http.Client, opts ai.StreamOptions) *ai.AssistantMessage {
			return StreamGoogle(context.Background(), model("gemini-2.5-flash", ai.APIGoogleGenerativeAI, "google", baseURL), req(),
				&GoogleOptions{StreamOptions: opts}).Result()
		}},
		{name: "openai-completions", sse: attrDoneSSE, auth: "authorization", bearer: true, run: func(baseURL string, client *http.Client, opts ai.StreamOptions) *ai.AssistantMessage {
			opts.HTTPClient = client
			return StreamOpenAICompletions(context.Background(), model("gpt-test", ai.APIOpenAICompletions, "openai", baseURL), req(),
				&OpenAIOptions{StreamOptions: opts}).Result()
		}},
		{name: "openai-responses", sse: responsesSSE, auth: "authorization", bearer: true, run: func(baseURL string, client *http.Client, opts ai.StreamOptions) *ai.AssistantMessage {
			opts.HTTPClient = client
			return StreamOpenAIResponses(context.Background(), model("gpt-test", ai.APIOpenAIResponses, "openai", baseURL), req(),
				&OpenAIResponsesOptions{StreamOptions: opts}).Result()
		}},
		{name: "anthropic-messages", sse: anthropicSSE, auth: "x-api-key", run: func(baseURL string, client *http.Client, opts ai.StreamOptions) *ai.AssistantMessage {
			opts.HTTPClient = client
			return StreamAnthropic(context.Background(), model("claude-test", ai.APIAnthropicMessages, "anthropic", baseURL), req(),
				&AnthropicOptions{StreamOptions: opts}).Result()
		}},
	}
}

// captureWireHeaders runs one request through adapter — over HTTP/2 unless the
// adapter is http1 — and returns the headers that reached the server.
func captureWireHeaders(t *testing.T, adapter wireAdapter, opts ai.StreamOptions) http.Header {
	t.Helper()
	var got http.Header
	var proto string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, proto = r.Header.Clone(), r.Proto
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, adapter.sse)
	}))
	var client *http.Client
	wantProto := "HTTP/1.1"
	if adapter.http1 {
		server.Start()
	} else {
		server.EnableHTTP2 = true
		server.StartTLS()
		client = server.Client()
		wantProto = "HTTP/2.0"
	}
	defer server.Close()
	final := adapter.run(server.URL, client, opts)
	if final.StopReason == ai.StopError {
		t.Fatalf("stream failed: %s", final.ErrorMessage)
	}
	if proto != wantProto {
		t.Fatalf("the request went over %s, want %s", proto, wantProto)
	}
	return got
}

func wantOneValue(t *testing.T, h http.Header, name, want string) {
	t.Helper()
	if got := h.Values(name); len(got) != 1 || got[0] != want {
		t.Fatalf("%s = %q, want exactly [%q]", name, got, want)
	}
}

// A consumer or model header value reaches the wire trimmed, through the
// record path (pi-messages, google) and the SDK defaultHeaders path (openai,
// anthropic) alike.
func TestHeaderValuesAreTrimmedLikeFetch(t *testing.T) {
	for _, adapter := range wireAdapters() {
		for _, tc := range []struct {
			name  string
			value string
			want  string
			// edges marks a row whose value differs from want only by spaces
			// and tabs, which HTTP/1.1 trims on its own.
			edges bool
		}{
			// net/http fails the request outright on the CR or LF.
			{"trailing LF", "tok\n", "tok", false},
			{"trailing CRLF", "tok\r\n", "tok", false},
			{"spaces and tabs", " \ty\t ", "y", true},
			{"all whitespace", "  ", "", true},
			{"inner whitespace kept", "a  b", "a  b", false},
		} {
			if tc.edges && adapter.http1 {
				continue
			}
			t.Run(adapter.name+"/"+tc.name, func(t *testing.T) {
				h := captureWireHeaders(t, adapter, ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{
					APIKey: "test-key", Headers: ai.ProviderHeaders{"X-A": strPtr(tc.value)},
				}})
				wantOneValue(t, h, "x-a", tc.want)
			})
		}
	}
}

// The api key is normalized with the header it lands in: after the "Bearer "
// prefix where the adapter adds one, so only the ends of the joined value go.
// @google/genai appends x-goog-api-key to its Headers itself, which is why
// google's key is trimmed although no record carries it.
func TestAPIKeyHeaderIsTrimmedLikeFetch(t *testing.T) {
	for _, adapter := range wireAdapters() {
		for _, tc := range []struct {
			name, key, want, wantBearer string
			edges                       bool
		}{
			{"trailing LF", "k\n", "k", "Bearer k", false},
			{"surrounding spaces", " k ", "k", "Bearer  k", true},
		} {
			if tc.edges && adapter.http1 {
				continue
			}
			t.Run(adapter.name+"/"+tc.name, func(t *testing.T) {
				h := captureWireHeaders(t, adapter, ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: tc.key}})
				want := tc.want
				if adapter.bearer {
					want = tc.wantBearer
				}
				wantOneValue(t, h, adapter.auth, want)
			})
		}
	}
}

// The anthropic SDK re-emits the params' `betas` as the per-request
// anthropic-beta header (betas.toString()), and its Headers trims the joined
// value's ends. Only an onPayload hook can put whitespace there: pi's own
// feature list is trimmed when it is parsed.
func TestAnthropicBetaHeaderIsTrimmedLikeFetch(t *testing.T) {
	var anthropic wireAdapter
	for _, adapter := range wireAdapters() {
		if adapter.name == "anthropic-messages" {
			anthropic = adapter
		}
	}
	for _, tc := range []struct {
		name  string
		betas any
		want  string
	}{
		{"string with trailing LF", " x\n", "x"},
		{"list with edge spaces", []any{" a", "b "}, "a,b"},
		{"list with a trailing tab", []any{"a\t"}, "a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := captureWireHeaders(t, anthropic, ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{
				APIKey: "test-key",
				OnPayload: func(payload any, _ *ai.Model) (any, error) {
					body := payload.(map[string]any)
					body["betas"] = tc.betas
					return body, nil
				},
			}})
			wantOneValue(t, h, "anthropic-beta", tc.want)
		})
	}
}

// A record entry spelled differently from an adapter literal is joined onto it
// (see applyAsRecord), and the value it contributes is normalized first, so an
// LF in it no longer fails the request. An empty contribution leaves only the
// separator: genai's wire ends the joined value at the comma, and pi-messages'
// ends it with a space that its HTTP/1.1 receiver strips as whitespace outside
// the field value — a trailing space HTTP/2 does not allow.
func TestJoinedHeaderValueIsTrimmedLikeFetch(t *testing.T) {
	adapters := map[string]wireAdapter{}
	for _, adapter := range wireAdapters() {
		adapters[adapter.name] = adapter
	}
	for _, tc := range []struct {
		name    string
		adapter string
		headers ai.ProviderHeaders
		header  string
		want    string
		edges   bool
	}{
		{"LF in the joined value", "pi-messages", ai.ProviderHeaders{"Authorization": strPtr("x\n")}, "authorization", "Bearer test-key, x", false},
		{"empty joined value", "pi-messages", ai.ProviderHeaders{"Authorization": strPtr("")}, "authorization", "Bearer test-key,", true},
		{"whitespace-only joined value", "pi-messages", ai.ProviderHeaders{"Authorization": strPtr("  ")}, "authorization", "Bearer test-key,", true},
		{"spaces round the joined value", "pi-messages", ai.ProviderHeaders{"Content-Type": strPtr(" text/plain ")}, "content-type", "application/json, text/plain", true},
		{"LF in the joined value", "google-generative-ai", ai.ProviderHeaders{"content-type": strPtr(" text/plain\n")}, "content-type", "application/json, text/plain", false},
	} {
		adapter := adapters[tc.adapter]
		if tc.edges && adapter.http1 {
			t.Fatalf("%s/%s: an edges row needs an HTTP/2 adapter", tc.adapter, tc.name)
		}
		t.Run(tc.adapter+"/"+tc.name, func(t *testing.T) {
			h := captureWireHeaders(t, adapter, ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{
				APIKey: "test-key", Headers: tc.headers,
			}})
			wantOneValue(t, h, tc.header, tc.want)
		})
	}
}
