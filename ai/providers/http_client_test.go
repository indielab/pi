package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// Upstream 027a5847 makes the HTTP client injectable per request
// (pi StreamOptions.fetch). These tests pin that each ported provider honors the
// override, that the default path is unchanged, and that google rejects an
// override with pi's byte-exact message.

// countingClient records how many requests it performed and delegates to the
// real transport, so the stream under test still parses a live response.
type countingClient struct {
	calls atomic.Int64
	inner *http.Client
}

func (c *countingClient) Do(req *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return c.inner.Do(req)
}

func TestAnthropicUsesInjectedHTTPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, anthropicSSE)
	}))
	defer server.Close()

	client := &countingClient{inner: server.Client()}
	model := &ai.Model{
		ID: "claude-test", Api: ai.APIAnthropicMessages, Provider: "anthropic",
		BaseURL: server.URL, MaxTokens: 4096,
	}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}

	final := StreamAnthropic(context.Background(), model, req, &AnthropicOptions{
		StreamOptions: ai.StreamOptions{APIKey: "test-key", HTTPClient: client},
	}).Result()

	if final.StopReason == ai.StopError {
		t.Fatalf("stream failed: %s", final.ErrorMessage)
	}
	if got := client.calls.Load(); got != 1 {
		t.Fatalf("injected client performed %d requests, want 1", got)
	}
}

func TestOpenAICompletionsUsesInjectedHTTPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, "data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	client := &countingClient{inner: server.Client()}
	model := &ai.Model{
		ID: "gpt-test", Api: ai.APIOpenAICompletions, Provider: "openai",
		BaseURL: server.URL, MaxTokens: 4096,
	}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}

	final := StreamOpenAICompletions(context.Background(), model, req, &OpenAIOptions{
		StreamOptions: ai.StreamOptions{APIKey: "test-key", HTTPClient: client},
	}).Result()

	if final.StopReason == ai.StopError {
		t.Fatalf("stream failed: %s", final.ErrorMessage)
	}
	if got := client.calls.Load(); got != 1 {
		t.Fatalf("injected client performed %d requests, want 1", got)
	}
}

func TestPiMessagesUsesInjectedHTTPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, piMessagesSSE(
			`{"type":"start"}`,
			`{"type":"done","reason":"stop","usage":`+piMessagesUsageJSON+`}`,
		))
	}))
	defer server.Close()

	client := &countingClient{inner: server.Client()}
	final := StreamPiMessages(context.Background(), piMessagesTestModel(server.URL+"/v1"), piMessagesTestContext(),
		&PiMessagesOptions{StreamOptions: ai.StreamOptions{APIKey: "test-key", HTTPClient: client}}).Result()

	if final.StopReason == ai.StopError {
		t.Fatalf("stream failed: %s", final.ErrorMessage)
	}
	if got := client.calls.Load(); got != 1 {
		t.Fatalf("injected client performed %d requests, want 1", got)
	}
}

// pi's google adapter throws rather than silently ignoring a custom fetch,
// because @google/genai cannot accept one. The message is byte-exact.
func TestGoogleRejectsCustomHTTPClient(t *testing.T) {
	reached := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, googleSSE)
	}))
	defer server.Close()

	model := &ai.Model{
		ID: "gemini-2.5-flash", Api: ai.APIGoogleGenerativeAI, Provider: "google",
		BaseURL: server.URL, MaxTokens: 8192,
	}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}

	final := StreamGoogle(context.Background(), model, req, &GoogleOptions{
		StreamOptions: ai.StreamOptions{APIKey: "g-key", HTTPClient: &countingClient{inner: server.Client()}},
	}).Result()

	if final.StopReason != ai.StopError {
		t.Fatalf("expected error stop, got %s", final.StopReason)
	}
	if final.ErrorMessage != "Custom fetch is not supported by the Google Generative AI adapter" {
		t.Fatalf("error message wrong: %q", final.ErrorMessage)
	}
	if reached {
		t.Fatalf("request must not be issued when the override is rejected")
	}
}

// The guard precedes the api-key check, as it does in pi: a request carrying
// both faults reports the fetch error, not the missing key.
func TestGoogleRejectsCustomHTTPClientBeforeAPIKeyCheck(t *testing.T) {
	final := StreamGoogle(context.Background(),
		&ai.Model{ID: "gemini-2.5-flash", Api: ai.APIGoogleGenerativeAI, Provider: "google", MaxTokens: 8192},
		ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}},
		&GoogleOptions{StreamOptions: ai.StreamOptions{HTTPClient: &countingClient{inner: http.DefaultClient}}},
	).Result()

	if final.ErrorMessage != "Custom fetch is not supported by the Google Generative AI adapter" {
		t.Fatalf("error message wrong: %q", final.ErrorMessage)
	}
}

// pi rejects only a fetch that is not globalThis.fetch; http.DefaultClient is
// the Go stand-in for that default and must stream normally.
func TestGoogleAcceptsDefaultHTTPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, googleSSE)
	}))
	defer server.Close()

	model := &ai.Model{
		ID: "gemini-2.5-flash", Api: ai.APIGoogleGenerativeAI, Provider: "google",
		BaseURL: server.URL, MaxTokens: 8192,
	}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}

	final := StreamGoogle(context.Background(), model, req, &GoogleOptions{
		StreamOptions: ai.StreamOptions{APIKey: "g-key", HTTPClient: http.DefaultClient},
	}).Result()

	if final.StopReason == ai.StopError {
		t.Fatalf("default client must be accepted, got error: %s", final.ErrorMessage)
	}
}

// A nil override keeps the retry loop's shared client, whose transport carries
// the TimeoutMs response-header cap.
func TestNilHTTPClientKeepsSharedClient(t *testing.T) {
	cfg := retryFromOptions(ai.StreamOptions{TimeoutMs: 1234}, nil)
	if cfg.httpClient != nil {
		t.Fatalf("expected nil httpClient for an unset override, got %#v", cfg.httpClient)
	}
	if got := sharedClient(1234); got == nil {
		t.Fatalf("sharedClient returned nil")
	}
}
