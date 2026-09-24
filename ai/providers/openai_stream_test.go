package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
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

// openaiStreamParsers are the two loops' JSON acceptance: completions repairs
// what JSON.parse rejects where repairJSON can, responses never did.
var openaiStreamParsers = map[string]func(string) ([]byte, bool){
	"completions": openaiStreamJSONWithRepair,
	"responses":   openaiStreamJSON,
}

// Both loops read a body into exactly the items the openai SDK's stream
// iterator yields — blank-line dispatch, joined multi-line data, every line
// ending, a "[DONE]" prefix ending the stream, no dispatch of an event the body
// ends inside, "thread.*" events wrapped — and fail on an item carrying an
// error with the SDK's APIError message. Where the SDK's JSON.parse throws, the
// port skips the event instead (a deliberate leniency); only the items before
// it are compared.
func TestOpenAIStreamReadsLikeTheSDK(t *testing.T) {
	c := loadOpenAIStreamCapture(t)
	for name, row := range c.Dispatch {
		for loop, parse := range openaiStreamParsers {
			t.Run(name+"/"+loop, func(t *testing.T) {
				var got []string
				err := iterateOpenAIStream(strings.NewReader(row.SSE), nil, parse, func(item []byte) error {
					text, ok := jsStringify(item)
					if !ok {
						t.Fatalf("yielded item %q is not one JSON value", item)
					}
					got = append(got, text)
					return nil
				})
				want := row.SDK
				switch {
				case want.Threw != nil && want.Threw.Name == "SyntaxError":
					if len(got) < len(want.Yields) || !slices.Equal(got[:len(want.Yields)], want.Yields) {
						t.Fatalf("items before the SDK's SyntaxError:\n got %q\nsdk %q", got, want.Yields)
					}
					return
				case want.Threw != nil:
					if err == nil || err.Error() != want.Threw.Message {
						t.Errorf("error = %v, sdk threw %s %q", err, want.Threw.Name, want.Threw.Message)
					}
				case err != nil:
					t.Errorf("error = %v, sdk threw nothing", err)
				}
				if !slices.Equal(got, want.Yields) {
					t.Errorf("items:\n got %q\nsdk %q", got, want.Yields)
				}
			})
		}
	}
}

// Both adapters end a dispatch body the way pi's do: the stop reason, the
// error message — the SDK's APIError message for an error item, with
// completions appending error.metadata.raw as String() writes it — the text
// and the response id.
func TestOpenAIStreamEndsLikePi(t *testing.T) {
	c := loadOpenAIStreamCapture(t)
	for name, row := range c.Dispatch {
		if row.SDK.Threw != nil && row.SDK.Threw.Name == "SyntaxError" {
			continue // pi fails with V8's SyntaxError text; the port skips the event
		}
		for adapter, want := range map[string]*openaiStreamOutcome{"completions": row.Completions, "responses": row.Responses} {
			t.Run(name+"/"+adapter, func(t *testing.T) {
				compareOpenAIStreamEnding(t, runOpenAIStreamAdapter(t, adapter, http.StatusOK, row.SSE), *want)
			})
		}
	}
}
