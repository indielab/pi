package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/sky-valley/pi/ai"
)

const gatewayBindingBaseURL = "https://gateway.ai.cloudflare.com/v1/account-id/my-gateway"

type capturedGatewayRun struct {
	gatewayID string
	req       AIGatewayUniversalRequest
	ctx       context.Context
}

type fakeGatewayBinding struct {
	runs     []capturedGatewayRun
	response *http.Response
}

type fakeGatewayBindingGateway struct {
	binding *fakeGatewayBinding
	id      string
}

func (b *fakeGatewayBinding) Gateway(id string) AIGatewayBindingGateway {
	return &fakeGatewayBindingGateway{binding: b, id: id}
}

func (g *fakeGatewayBindingGateway) Run(ctx context.Context, req AIGatewayUniversalRequest) (*http.Response, error) {
	g.binding.runs = append(g.binding.runs, capturedGatewayRun{gatewayID: g.id, req: req, ctx: ctx})
	if g.binding.response != nil {
		return g.binding.response, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("{}")),
	}, nil
}

func newGatewayBindingTestDoer(t *testing.T) (ai.HTTPDoer, *fakeGatewayBinding) {
	t.Helper()
	binding := &fakeGatewayBinding{}
	doer, err := NewGatewayBindingDoer(GatewayBindingDoerOptions{
		Binding: binding, BaseURL: gatewayBindingBaseURL, Gateway: "my-gateway",
	})
	if err != nil {
		t.Fatalf("NewGatewayBindingDoer: %v", err)
	}
	return doer, binding
}

func gatewayPost(t *testing.T, rawURL, body string) *http.Request {
	t.Helper()
	return gatewayRequest(t, http.MethodPost, rawURL, body)
}

func gatewayRequest(t *testing.T, method, rawURL, body string) *http.Request {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, rawURL, reader)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	return req
}

func TestGatewayBindingDerivesProviderAndEndpoint(t *testing.T) {
	doer, binding := newGatewayBindingTestDoer(t)

	for _, tc := range []struct{ path, body string }{
		{"/anthropic/v1/messages", `{"model":"claude"}`},
		{"/openai/responses", `{"model":"gpt"}`},
		{"/workers-ai/v1/chat/completions", `{"model":"@cf/meta/llama"}`},
	} {
		if _, err := doer.Do(gatewayPost(t, gatewayBindingBaseURL+tc.path, tc.body)); err != nil {
			t.Fatalf("Do(%s): %v", tc.path, err)
		}
	}

	var got [][2]string
	for _, run := range binding.runs {
		got = append(got, [2]string{run.req.Provider, run.req.Endpoint})
		if run.gatewayID != "my-gateway" {
			t.Errorf("gatewayID = %q, want my-gateway", run.gatewayID)
		}
	}
	want := [][2]string{
		{"anthropic", "v1/messages"},
		{"openai", "responses"},
		{"workers-ai", "v1/chat/completions"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("provider/endpoint splits = %v, want %v", got, want)
	}
	if q, ok := binding.runs[0].req.Query.(map[string]any); !ok || q["model"] != "claude" {
		t.Fatalf("query = %#v, want map with model=claude", binding.runs[0].req.Query)
	}
}

func TestGatewayBindingKeepsQueryStringInEndpoint(t *testing.T) {
	doer, binding := newGatewayBindingTestDoer(t)

	if _, err := doer.Do(gatewayPost(t, gatewayBindingBaseURL+"/openai/responses?stream=true&x=1", `{}`)); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got := binding.runs[0].req.Endpoint; got != "responses?stream=true&x=1" {
		t.Fatalf("endpoint = %q, want responses?stream=true&x=1", got)
	}
}

func TestGatewayBindingLowercasesAndCollapsesHeaderNames(t *testing.T) {
	doer, binding := newGatewayBindingTestDoer(t)

	req := gatewayPost(t, gatewayBindingBaseURL+"/openai/responses", `{}`)
	req.Header["Content-Type"] = []string{"application/json"}
	// Go canonicalizes on Set/Add, so a case-variant duplicate needs a raw write.
	req.Header["x-custom"] = []string{"a"}
	req.Header["X-Custom"] = []string{"b"}
	if _, err := doer.Do(req); err != nil {
		t.Fatalf("Do: %v", err)
	}

	headers := binding.runs[0].req.Headers
	if headers["content-type"] != "application/json" {
		t.Errorf("content-type = %q, want application/json", headers["content-type"])
	}
	for name := range headers {
		if name != strings.ToLower(name) {
			t.Errorf("header name %q is not lowercased", name)
		}
	}
	if got := headers["x-custom"]; got != "a" && got != "b" && got != "a, b" && got != "b, a" {
		t.Errorf("case-variant duplicates did not collapse onto x-custom: %q", got)
	}
}

func TestGatewayBindingStripsGatewayAuthAndDerivedHeaders(t *testing.T) {
	doer, binding := newGatewayBindingTestDoer(t)

	req := gatewayPost(t, gatewayBindingBaseURL+"/anthropic/v1/messages", `{"model":"claude"}`)
	req.Header.Set("cf-aig-authorization", "Bearer "+CloudflareGatewayBindingAuthSentinel)
	req.Header.Set("Content-Length", "17")
	req.Header.Set("Host", "gateway.ai.cloudflare.com")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	if _, err := doer.Do(req); err != nil {
		t.Fatalf("Do: %v", err)
	}

	headers := binding.runs[0].req.Headers
	for _, stripped := range []string{"cf-aig-authorization", "content-length", "host"} {
		if _, ok := headers[stripped]; ok {
			t.Errorf("header %q must not reach the binding", stripped)
		}
	}
	if headers["anthropic-version"] != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want 2023-06-01", headers["anthropic-version"])
	}
	if headers["content-type"] != "application/json" {
		t.Errorf("content-type = %q, want application/json", headers["content-type"])
	}
}

func TestGatewayBindingForwardsContext(t *testing.T) {
	doer, binding := newGatewayBindingTestDoer(t)

	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")
	req := gatewayPost(t, gatewayBindingBaseURL+"/openai/responses", `{}`).WithContext(ctx)
	if _, err := doer.Do(req); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got := binding.runs[0].ctx.Value(ctxKey{}); got != "marker" {
		t.Fatalf("binding ctx value = %v, want marker (request context must be forwarded)", got)
	}
}

func TestGatewayBindingReturnsBindingResponseUntouched(t *testing.T) {
	binding := &fakeGatewayBinding{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Cf-Aig-Log-Id": []string{"log-1"}},
		Body:       io.NopCloser(strings.NewReader("data: {}\n\n")),
	}}
	doer, err := NewGatewayBindingDoer(GatewayBindingDoerOptions{
		Binding: binding, BaseURL: gatewayBindingBaseURL, Gateway: "my-gateway",
	})
	if err != nil {
		t.Fatalf("NewGatewayBindingDoer: %v", err)
	}

	resp, err := doer.Do(gatewayPost(t, gatewayBindingBaseURL+"/openai/responses", `{}`))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp != binding.response {
		t.Fatal("response must be the binding's own response, not a copy")
	}
	if got := resp.Header.Get("cf-aig-log-id"); got != "log-1" {
		t.Errorf("cf-aig-log-id = %q, want log-1", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "data: {}\n\n" {
		t.Errorf("body = %q, want the streamed bytes untouched", body)
	}
}

func TestGatewayBindingRejectsUnexpressibleRequests(t *testing.T) {
	doer, binding := newGatewayBindingTestDoer(t)

	for _, tc := range []struct {
		name, method, path, body, wantErr string
	}{
		{"non-POST", http.MethodGet, "/anthropic/v1/messages", "", "cannot express GET"},
		{"non-JSON body", http.MethodPost, "/anthropic/v1/messages", "not json", "non-JSON body"},
		{"missing body", http.MethodPost, "/anthropic/v1/messages", "", "missing body"},
		{"no endpoint", http.MethodPost, "/anthropic", `{}`, "missing provider/endpoint path"},
		{"trailing slash only", http.MethodPost, "/", `{}`, "missing provider/endpoint path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := doer.Do(gatewayRequest(t, tc.method, gatewayBindingBaseURL+tc.path, tc.body))
			if err == nil {
				t.Fatalf("Do(%s %s) succeeded, want error", tc.method, tc.path)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), "route it over HTTPS with gateway auth instead") {
				t.Errorf("error %q must carry the HTTPS-instead resolution hint", err)
			}
		})
	}
	if len(binding.runs) != 0 {
		t.Fatalf("binding was called %d times, want 0", len(binding.runs))
	}
}

func TestGatewayBindingRejectsOutOfPrefixURLs(t *testing.T) {
	// Silent passthrough would ship the auth sentinel to whatever host the URL
	// names; a misconfigured baseURL must fail loudly instead.
	doer, binding := newGatewayBindingTestDoer(t)

	for _, tc := range []struct{ name, rawURL string }{
		{"other host", "https://api.openai.com/v1/chat/completions"},
		{"other account, same origin", "https://gateway.ai.cloudflare.com/v1/other-account/my-gateway/anthropic/v1/messages"},
		{"scheme mismatch", "http://gateway.ai.cloudflare.com/v1/account-id/my-gateway/anthropic/v1/messages"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := doer.Do(gatewayPost(t, tc.rawURL, `{}`))
			if err == nil {
				t.Fatalf("Do(%s) succeeded, want error", tc.rawURL)
			}
			if !strings.Contains(err.Error(), "outside the configured gateway prefix") {
				t.Fatalf("error = %q, want it to name the prefix violation", err)
			}
		})
	}
	if len(binding.runs) != 0 {
		t.Fatalf("binding was called %d times, want 0", len(binding.runs))
	}
}

func TestGatewayBindingMatchesOnNormalizedPath(t *testing.T) {
	doer, binding := newGatewayBindingTestDoer(t)

	// Dot segments normalize away before the provider/endpoint split, so a lexical
	// variant routes exactly like its normal form (raw string prefixing would split
	// it differently).
	if _, err := doer.Do(gatewayPost(t, gatewayBindingBaseURL+"/anthropic/../anthropic/v1/./messages", `{"model":"claude"}`)); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(binding.runs) != 1 {
		t.Fatalf("binding runs = %d, want 1", len(binding.runs))
	}
	if got := [2]string{binding.runs[0].req.Provider, binding.runs[0].req.Endpoint}; got != [2]string{"anthropic", "v1/messages"} {
		t.Fatalf("normalized split = %v, want [anthropic v1/messages]", got)
	}

	// A dot-segment URL that resolves outside the prefix is rejected even though it
	// starts with the prefix as a raw string.
	_, err := doer.Do(gatewayPost(t, gatewayBindingBaseURL+"/../other-gateway/anthropic/v1/messages", `{}`))
	if err == nil || !strings.Contains(err.Error(), "outside the configured gateway prefix") {
		t.Fatalf("error = %v, want an out-of-prefix rejection", err)
	}
	if len(binding.runs) != 1 {
		t.Fatalf("binding runs = %d, want 1 (the rejected request must not dispatch)", len(binding.runs))
	}
}

func TestGatewayBindingReadsStreamingRequestBody(t *testing.T) {
	doer, binding := newGatewayBindingTestDoer(t)

	// A one-shot body (no GetBody, unknown length) must still reach the JSON probe.
	// Consuming it is fine: unexpressible requests reject rather than replay.
	req := gatewayPost(t, gatewayBindingBaseURL+"/openai/responses", "")
	req.Body = io.NopCloser(bytes.NewReader([]byte(`{"model":"gpt","stream":true}`)))
	req.ContentLength = -1
	if _, err := doer.Do(req); err != nil {
		t.Fatalf("Do: %v", err)
	}
	q, ok := binding.runs[0].req.Query.(map[string]any)
	if !ok {
		t.Fatalf("query = %#v, want a decoded object", binding.runs[0].req.Query)
	}
	if q["model"] != "gpt" || q["stream"] != true {
		t.Fatalf("query = %#v, want model=gpt stream=true", q)
	}
}

func TestGatewayBindingAcceptsJSONNullBody(t *testing.T) {
	// pi rejects only an ABSENT body; a literal JSON null parses and dispatches.
	doer, binding := newGatewayBindingTestDoer(t)

	if _, err := doer.Do(gatewayPost(t, gatewayBindingBaseURL+"/openai/responses", `null`)); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(binding.runs) != 1 {
		t.Fatalf("binding runs = %d, want 1", len(binding.runs))
	}
	if binding.runs[0].req.Query != nil {
		t.Fatalf("query = %#v, want nil (JSON null)", binding.runs[0].req.Query)
	}
}

func TestNewGatewayBindingDoerValidatesOptions(t *testing.T) {
	if _, err := NewGatewayBindingDoer(GatewayBindingDoerOptions{BaseURL: gatewayBindingBaseURL, Gateway: "g"}); err == nil {
		t.Error("a nil Binding must be rejected")
	}
	if _, err := NewGatewayBindingDoer(GatewayBindingDoerOptions{Binding: &fakeGatewayBinding{}, BaseURL: "/v1/acct/gw", Gateway: "g"}); err == nil {
		t.Error("a relative BaseURL must be rejected")
	}
}

// TestGatewayBindingComposesWithOpenAICompletions is the composition proof: the
// doer sits under a real provider stream, and the sentinel pairing keeps the
// SDK's placeholder auth headers out of the binding entry (pi's null
// ProviderHeaders suppression, ported under the 2026-08-04 ruling).
func TestGatewayBindingComposesWithOpenAICompletions(t *testing.T) {
	binding := &fakeGatewayBinding{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n" +
				"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
				"data: [DONE]\n\n")),
	}}
	doer, err := NewGatewayBindingDoer(GatewayBindingDoerOptions{
		Binding: binding, BaseURL: gatewayBindingBaseURL, Gateway: "my-gateway",
	})
	if err != nil {
		t.Fatalf("NewGatewayBindingDoer: %v", err)
	}

	model := &ai.Model{
		ID:       "gpt-4o",
		Api:      ai.APIOpenAICompletions,
		Provider: "cloudflare-ai-gateway",
		BaseURL:  gatewayBindingBaseURL + "/openai",
	}
	opts := &ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{
		ProviderRequestOptions: ai.ProviderRequestOptions{
			APIKey:     "unused",
			HTTPClient: doer,
			Headers: ai.ProviderHeaders{
				"Authorization":        nil,
				"x-api-key":            nil,
				"cf-aig-authorization": strPtr("Bearer " + CloudflareGatewayBindingAuthSentinel),
			},
		},
	}}

	result := StreamSimpleOpenAICompletions(context.Background(), model,
		ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, opts).Result()
	if result == nil {
		t.Fatal("stream produced no result")
	}

	if len(binding.runs) != 1 {
		t.Fatalf("binding runs = %d, want 1", len(binding.runs))
	}
	entry := binding.runs[0]
	if entry.req.Provider != "openai" || entry.req.Endpoint != "chat/completions" {
		t.Errorf("split = %s/%s, want openai/chat/completions", entry.req.Provider, entry.req.Endpoint)
	}
	for _, banned := range []string{"authorization", "x-api-key", "cf-aig-authorization"} {
		if v, ok := entry.req.Headers[banned]; ok {
			t.Errorf("header %q reached the binding entry with value %q; it must be suppressed or stripped", banned, v)
		}
	}
	body, err := json.Marshal(entry.req.Query)
	if err != nil {
		t.Fatalf("marshal query: %v", err)
	}
	if !strings.Contains(string(body), `"model":"gpt-4o"`) {
		t.Errorf("query body = %s, want it to carry the model", body)
	}
}
