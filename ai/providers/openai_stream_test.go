package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// openaiStreamCaptureFile is what testdata/openai-stream/capture.mts recorded
// from pi's source at 002fc8385 and the openai SDK its lockfile pins: how the
// SDK's stream iterator and pi's two openai adapters read the same SSE bodies.
const openaiStreamCaptureFile = "testdata/openai-stream/openai-stream-002fc8385.json"

type openaiStreamOutcome struct {
	Observed        []string `json:"observed"`
	SameModel       bool     `json:"sameModel"`
	OnResponseCalls int      `json:"onResponseCalls"`
	StopReason      string   `json:"stopReason"`
	ErrorMessage    string   `json:"errorMessage"`
	Text            string   `json:"text"`
	ResponseID      string   `json:"responseId"`
}

type openaiStreamThrown struct {
	Name    string `json:"name"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

type openaiStreamRow struct {
	SSE string `json:"sse"`
	SDK *struct {
		Yields []string            `json:"yields"`
		Threw  *openaiStreamThrown `json:"threw"`
	} `json:"sdk"`
	Completions *openaiStreamOutcome `json:"completions"`
	Responses   *openaiStreamOutcome `json:"responses"`
}

type openaiStreamCapture struct {
	Sha         string                     `json:"sha"`
	OpenAI      string                     `json:"openai"`
	Dispatch    map[string]openaiStreamRow `json:"dispatch"`
	Completions map[string]openaiStreamRow `json:"completions"`
	Responses   map[string]openaiStreamRow `json:"responses"`
	Hooks       map[string]struct {
		Adapter string              `json:"adapter"`
		SSE     string              `json:"sse"`
		Status  int                 `json:"status"`
		Outcome openaiStreamOutcome `json:"outcome"`
	} `json:"hooks"`
}

func loadOpenAIStreamCapture(t *testing.T) openaiStreamCapture {
	t.Helper()
	data, err := os.ReadFile(openaiStreamCaptureFile)
	if err != nil {
		t.Fatal(err)
	}
	var c openaiStreamCapture
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("%s: %v; rerun capture.mts", openaiStreamCaptureFile, err)
	}
	if c.OpenAI != "6.40.0" {
		t.Fatalf("%s was captured with openai %s; 002fc8385's package-lock.json locks 6.40.0", openaiStreamCaptureFile, c.OpenAI)
	}
	return c
}

// openaiStreamModel is the capture's model for adapter "completions" or
// "responses".
func openaiStreamModel(adapter, baseURL string) *ai.Model {
	if adapter == "completions" {
		return &ai.Model{
			ID: "openrouter/auto", Name: "OpenRouter Auto", Api: ai.APIOpenAICompletions, Provider: "openrouter",
			BaseURL: baseURL, Input: []string{"text"}, ContextWindow: 200000, MaxTokens: 8192,
		}
	}
	return &ai.Model{
		ID: "gpt-5-mini", Name: "GPT-5 Mini", Api: ai.APIOpenAIResponses, Provider: "openai",
		BaseURL: baseURL, Reasoning: true, Input: []string{"text"}, ContextWindow: 400000, MaxTokens: 128000,
	}
}

// runOpenAIStreamAdapter streams body, served with status, through the Go
// adapter the capture calls adapter, the way capture.mts's piRun does.
func runOpenAIStreamAdapter(t *testing.T, adapter string, status int, body string) openaiStreamOutcome {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status == http.StatusOK {
			w.Header().Set("content-type", "text/event-stream")
		} else {
			w.Header().Set("content-type", "application/json")
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	model := openaiStreamModel(adapter, server.URL+"/v1")
	req := ai.NormalizeContext(ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}})
	opts := &ai.SimpleStreamOptions{}
	opts.APIKey = "k"
	var final *ai.AssistantMessage
	if adapter == "completions" {
		final = StreamSimpleOpenAICompletions(context.Background(), model, req, opts).Result()
	} else {
		final = StreamSimpleOpenAIResponses(context.Background(), model, req, opts).Result()
	}
	return openaiStreamOutcome{
		StopReason:   string(final.StopReason),
		ErrorMessage: final.ErrorMessage,
		Text:         jstrimText(final),
		ResponseID:   final.ResponseID,
	}
}

// compareOpenAIStreamEnding checks how a stream ended against pi's record.
func compareOpenAIStreamEnding(t *testing.T, got, want openaiStreamOutcome) {
	t.Helper()
	if got.StopReason != want.StopReason || got.ErrorMessage != want.ErrorMessage {
		t.Errorf("ended %s %q, pi %s %q", got.StopReason, got.ErrorMessage, want.StopReason, want.ErrorMessage)
	}
	if got.Text != want.Text {
		t.Errorf("text = %q, pi = %q", got.Text, want.Text)
	}
	if got.ResponseID != want.ResponseID {
		t.Errorf("responseId = %q, pi = %q", got.ResponseID, want.ResponseID)
	}
}

// A `data: null` event reaches pi's processResponsesStream as null, whose
// event.type read throws: the stream fails there instead of reading on to the
// completed response.
func TestOpenAIResponsesNullEventFailsLikePi(t *testing.T) {
	row := loadOpenAIStreamCapture(t).Responses["null-mid-stream"]
	if row.Responses == nil {
		t.Fatalf("%s has no responses/null-mid-stream; rerun capture.mts", openaiStreamCaptureFile)
	}
	got := runOpenAIStreamAdapter(t, "responses", http.StatusOK, row.SSE)
	compareOpenAIStreamEnding(t, got, *row.Responses)
}
