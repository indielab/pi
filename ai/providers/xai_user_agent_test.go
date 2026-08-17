package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// --- pi 70e878d4c: xAI requests enforce pi's runtime user agent ---

// captureOpenAIResponsesHeaders runs one openai-responses request against a
// local server and returns the headers that reached the wire.
func captureOpenAIResponsesHeaders(t *testing.T, model *ai.Model, opts ai.StreamOptions) http.Header {
	t.Helper()
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, responsesSSE)
	}))
	defer server.Close()
	model.BaseURL = server.URL
	final := StreamOpenAIResponses(context.Background(), model,
		ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}},
		&OpenAIResponsesOptions{StreamOptions: opts}).Result()
	if final.StopReason == ai.StopError {
		t.Fatalf("stream failed: %s", final.ErrorMessage)
	}
	return got
}

// xAI Responses requests always carry pi's runtime user agent for
// provider-side attribution (pi openai-responses.ts createClient calls
// forcePiUserAgent after the options merge), so the override outranks a
// consumer-supplied user-agent and appears exactly once on the wire.
func TestOpenAIResponsesXaiForcesPiUserAgent(t *testing.T) {
	model := &ai.Model{ID: "grok-4.5", Api: ai.APIOpenAIResponses, Provider: "xai",
		Input: []string{"text"}, MaxTokens: 4096}
	h := captureOpenAIResponsesHeaders(t, model, ai.StreamOptions{
		ProviderRequestOptions: ai.ProviderRequestOptions{
			APIKey:  "xai-test-token",
			Headers: ai.ProviderHeaders{"user-agent": strPtr("custom-agent")},
		},
	})
	if got := h.Values("User-Agent"); len(got) != 1 || got[0] != piUserAgent() {
		t.Fatalf("xai responses user-agent = %v, want exactly [%q]", got, piUserAgent())
	}
}

// Custom xAI models on the Completions API get the same forced user agent (pi
// openai-completions.ts createClient), even over caller headers.
func TestOpenAICompletionsXaiForcesPiUserAgent(t *testing.T) {
	model := &ai.Model{ID: "grok-custom", Api: ai.APIOpenAICompletions, Provider: "xai",
		Input: []string{"text"}, MaxTokens: 4096}
	h := captureOpenAIHeaders(t, model, ai.StreamOptions{
		ProviderRequestOptions: ai.ProviderRequestOptions{
			APIKey:  "xai-test-token",
			Headers: ai.ProviderHeaders{"User-Agent": strPtr("custom-agent")},
		},
	})
	if got := h.Values("User-Agent"); len(got) != 1 || got[0] != piUserAgent() {
		t.Fatalf("xai completions user-agent = %v, want exactly [%q]", got, piUserAgent())
	}
}

// The override is scoped to provider "xai": other openai-responses providers
// keep whatever the header merge produced — here the consumer's user-agent
// (upstream keeps the OpenAI SDK's own agent when no consumer header is set).
func TestOpenAIResponsesNonXaiKeepsConsumerUserAgent(t *testing.T) {
	model := &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "openai",
		Input: []string{"text"}, MaxTokens: 4096}
	h := captureOpenAIResponsesHeaders(t, model, ai.StreamOptions{
		ProviderRequestOptions: ai.ProviderRequestOptions{
			APIKey:  "test-token",
			Headers: ai.ProviderHeaders{"user-agent": strPtr("custom-agent")},
		},
	})
	if got := h.Get("user-agent"); got != "custom-agent" {
		t.Fatalf("non-xai responses user-agent = %q, want the consumer value untouched", got)
	}
}
