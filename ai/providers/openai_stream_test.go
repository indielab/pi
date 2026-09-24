package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/sky-valley/pi/ai"
	"github.com/sky-valley/pi/internal/jstext"
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

// openaiSDKReading is what the SDK's stream iterator yielded (each item
// JSON.stringify'd) and what it threw.
type openaiSDKReading struct {
	Yields []string            `json:"yields"`
	Threw  *openaiStreamThrown `json:"threw"`
}

type openaiStreamRow struct {
	SSE string `json:"sse"`
	SDK *struct {
		openaiSDKReading
		// Aborted and ReadFailed are the body read to its end and then
		// failing: with an AbortError, as a cancelled request's read does, and
		// with TypeError "terminated".
		Aborted    openaiSDKReading `json:"aborted"`
		ReadFailed openaiSDKReading `json:"readFailed"`
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
		Adapter string `json:"adapter"`
		SSE     string `json:"sse"`
		Status  int    `json:"status"`
		// Hold keeps the connection open after the body; AbortOnEvent aborts
		// the request as the observer sees that event (1-based, 0 never).
		Hold         bool                `json:"hold"`
		AbortOnEvent int                 `json:"abortOnEvent"`
		Outcome      openaiStreamOutcome `json:"outcome"`
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

// openaiStreamHooks are capture.mts's Hooks: which of the stream's hooks fail,
// and whether the server holds the connection open after the body.
type openaiStreamHooks struct {
	failOnResponse bool
	// failOnEvent is "throw" (the provider stream event observer fails) or
	// "abort-then-throw" (it cancels the request first).
	failOnEvent string
	// abortOnEvent cancels the request, without failing, as the observer sees
	// that event (1-based).
	abortOnEvent int
	hold         bool
}

// runOpenAIStreamAdapter streams body, served with status, through the Go
// adapter the capture calls adapter, the way capture.mts's piRun does.
func runOpenAIStreamAdapter(t *testing.T, adapter string, status int, body string, hooks openaiStreamHooks) openaiStreamOutcome {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status == http.StatusOK {
			w.Header().Set("content-type", "text/event-stream")
		} else {
			w.Header().Set("content-type", "application/json")
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
		if hooks.hold {
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
			case <-time.After(30 * time.Second):
				t.Errorf("the client never gave up on the held connection")
			}
		}
	}))
	t.Cleanup(server.Close)
	model := openaiStreamModel(adapter, server.URL+"/v1")
	req := ai.NormalizeContext(ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opts := &ai.SimpleStreamOptions{}
	opts.APIKey = "k"
	var observed []string
	sameModel := true
	opts.OnProviderStreamEvent = func(data any, eventModel *ai.Model) error {
		text, err := jstext.Stringify(data)
		if err != nil {
			t.Errorf("observed %#v, which has no JSON form: %v", data, err)
		}
		observed = append(observed, text)
		sameModel = sameModel && eventModel == model
		if hooks.abortOnEvent == len(observed) {
			cancel()
		}
		if hooks.failOnEvent == "abort-then-throw" {
			cancel()
		}
		if hooks.failOnEvent != "" {
			return errors.New("observer boom")
		}
		return nil
	}
	onResponseCalls := 0
	opts.OnResponse = func(ai.ProviderResponse, *ai.Model) error {
		onResponseCalls++
		if hooks.failOnResponse {
			return errors.New("response veto")
		}
		return nil
	}
	var final *ai.AssistantMessage
	if adapter == "completions" {
		final = StreamSimpleOpenAICompletions(ctx, model, req, opts).Result()
	} else {
		final = StreamSimpleOpenAIResponses(ctx, model, req, opts).Result()
	}
	return openaiStreamOutcome{
		Observed:        observed,
		SameModel:       sameModel,
		OnResponseCalls: onResponseCalls,
		StopReason:      string(final.StopReason),
		ErrorMessage:    final.ErrorMessage,
		Text:            jstrimText(final),
		ResponseID:      final.ResponseID,
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

// The responses adapter ends each of its bodies the way pi's does. Among them:
// a `data: null` event reaches pi's processResponsesStream as null, whose
// event.type read throws, and an `error` event fails with a template literal
// over whatever code and message it carries.
func TestOpenAIResponsesEndsLikePi(t *testing.T) {
	for name, row := range loadOpenAIStreamCapture(t).Responses {
		t.Run(name, func(t *testing.T) {
			got := runOpenAIStreamAdapter(t, "responses", http.StatusOK, row.SSE, openaiStreamHooks{})
			compareOpenAIStreamEnding(t, got, *row.Responses)
		})
	}
}

// openaiStreamParsers are the two loops' JSON acceptance: completions repairs
// what JSON.parse rejects where repairJSON can, responses never did.
var openaiStreamParsers = map[string]func(string) ([]byte, bool){
	"completions": openaiStreamJSONWithRepair,
	"responses":   openaiStreamJSON,
}

// openaiStreamReads are the ways a test hands a body to the loops: in one read,
// and one byte per read, so that every line ending also arrives split across
// reads ("\r" | "\n"). The capture measured that the SDK reads a body the same
// either way.
var openaiStreamReads = map[string]func(string) io.Reader{
	"whole":    func(body string) io.Reader { return strings.NewReader(body) },
	"one-byte": func(body string) io.Reader { return iotest.OneByteReader(strings.NewReader(body)) },
}

// Both loops read a body into exactly the items the openai SDK's stream
// iterator yields — blank-line dispatch, joined multi-line data, every line
// ending wherever the reads split it, a "[DONE]" prefix ending the stream, no
// dispatch of an event the body ends inside, "thread.*" events wrapped — and
// fail on an item carrying an error with the SDK's APIError message. Where the
// SDK's JSON.parse throws, the port skips the event instead (a deliberate
// leniency); only the items before it are compared.
func TestOpenAIStreamReadsLikeTheSDK(t *testing.T) {
	c := loadOpenAIStreamCapture(t)
	for name, row := range c.Dispatch {
		for loop, parse := range openaiStreamParsers {
			for read, reader := range openaiStreamReads {
				t.Run(name+"/"+loop+"/"+read, func(t *testing.T) {
					readOpenAIStreamLikeTheSDK(t, nil, reader(row.SSE), parse, row.SDK.openaiSDKReading, nil)
				})
			}
		}
	}
}

// A cancelled request's failed body read ends the reading where the SDK's
// does and without an error: the SDK's Stream swallows the AbortError, so pi's
// adapter goes on to its post-loop checks. Everything read before it is still
// dispatched — the SDK looks at the signal only through that read — except
// what its iterSSEChunks held back past the last event separator and a line
// its LineDecoder had not yet ended, which only the body's end flushes.
func TestOpenAIStreamAbortedReadEndsLikeTheSDK(t *testing.T) {
	c := loadOpenAIStreamCapture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, row := range c.Dispatch {
		for loop, parse := range openaiStreamParsers {
			for read, reader := range openaiStreamReads {
				t.Run(name+"/"+loop+"/"+read, func(t *testing.T) {
					body := io.MultiReader(reader(row.SSE), iotest.ErrReader(ctx.Err()))
					readOpenAIStreamLikeTheSDK(t, ctx, body, parse, row.SDK.Aborted, nil)
				})
			}
		}
	}
}

// A body read that fails while the request is live fails the stream with the
// read's error, after dispatching exactly what the SDK dispatches before its
// read throws.
func TestOpenAIStreamFailedReadLikeTheSDK(t *testing.T) {
	c := loadOpenAIStreamCapture(t)
	errTerminated := errors.New("terminated")
	for name, row := range c.Dispatch {
		for loop, parse := range openaiStreamParsers {
			for read, reader := range openaiStreamReads {
				t.Run(name+"/"+loop+"/"+read, func(t *testing.T) {
					body := io.MultiReader(reader(row.SSE), iotest.ErrReader(errTerminated))
					readOpenAIStreamLikeTheSDK(t, context.Background(), body, parse, row.SDK.ReadFailed, errTerminated)
				})
			}
		}
	}
}

// readOpenAIStreamLikeTheSDK iterates body and compares the items and the
// error with what the SDK made of it. readErr, when set, is the error the
// body's read fails with, which stands for the SDK's TypeError "terminated".
func readOpenAIStreamLikeTheSDK(t *testing.T, ctx context.Context, body io.Reader, parse func(string) ([]byte, bool), want openaiSDKReading, readErr error) {
	t.Helper()
	var got []string
	err := iterateOpenAIStream(body, ctx, parse, func(item []byte) error {
		text, ok := jsStringify(item)
		if !ok {
			t.Fatalf("yielded item %q is not one JSON value", item)
		}
		got = append(got, text)
		return nil
	})
	switch {
	case want.Threw != nil && want.Threw.Name == "SyntaxError":
		if len(got) < len(want.Yields) || !slices.Equal(got[:len(want.Yields)], want.Yields) {
			t.Fatalf("items before the SDK's SyntaxError:\n got %q\nsdk %q", got, want.Yields)
		}
		return
	case want.Threw != nil && readErr != nil && want.Threw.Name == "TypeError" && want.Threw.Message == readErr.Error():
		if !errors.Is(err, readErr) {
			t.Errorf("error = %v, want the failed read's %v", err, readErr)
		}
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
				compareOpenAIStreamEnding(t, runOpenAIStreamAdapter(t, adapter, http.StatusOK, row.SSE, openaiStreamHooks{}), *want)
			})
		}
	}
}

// A request cancelled while the server holds the connection open ends like
// pi's: the SDK's Stream swallows its body read's AbortError, so each adapter
// runs its post-loop checks — completions finishes its blocks and fails
// "Request was aborted"; responses fails first for a missing terminal event,
// and with "Request was aborted" only once it saw one.
func TestOpenAIStreamAbortLikePi(t *testing.T) {
	c := loadOpenAIStreamCapture(t)
	for _, name := range []string{"completions/abort-held", "completions/abort-held-after-finish", "responses/abort-held", "responses/abort-held-after-terminal"} {
		t.Run(name, func(t *testing.T) {
			row, ok := c.Hooks[name]
			if !ok || !row.Hold || row.AbortOnEvent == 0 {
				t.Fatalf("%s has no held, aborting hooks row %s; rerun capture.mts", openaiStreamCaptureFile, name)
			}
			got := runOpenAIStreamAdapter(t, row.Adapter, row.Status, row.SSE, openaiStreamHooks{hold: true, abortOnEvent: row.AbortOnEvent})
			compareOpenAIStreamObserved(t, got, row.Outcome)
			compareOpenAIStreamEnding(t, got, row.Outcome)
		})
	}
}

// pi awaits onResponse only once the SDK has a 2xx response, and a throw from it
// fails the stream in both adapters.
func TestOpenAIStreamOnResponseLikePi(t *testing.T) {
	c := loadOpenAIStreamCapture(t)
	for _, name := range []string{"on-response-throws", "http-400", "http-400-on-response-throws"} {
		for _, adapter := range []string{"completions", "responses"} {
			t.Run(adapter+"/"+name, func(t *testing.T) {
				row, ok := c.Hooks[adapter+"/"+name]
				if !ok {
					t.Fatalf("%s has no hooks row %s/%s; rerun capture.mts", openaiStreamCaptureFile, adapter, name)
				}
				got := runOpenAIStreamAdapter(t, adapter, row.Status, row.SSE, openaiStreamHooks{failOnResponse: strings.Contains(name, "on-response-throws")})
				want := row.Outcome
				if got.OnResponseCalls != want.OnResponseCalls {
					t.Errorf("onResponse ran %d times, pi %d", got.OnResponseCalls, want.OnResponseCalls)
				}
				if adapter == "completions" && row.Status != http.StatusOK {
					// The completions HTTP error text is the port's own
					// (docs/UPSTREAM.md D20); only how the stream ended is pi's.
					want.ErrorMessage = got.ErrorMessage
				}
				compareOpenAIStreamEnding(t, got, want)
			})
		}
	}
}

// compareOpenAIStreamObserved checks the provider stream events against pi's:
// every item pi's loop iterated, in order, as JSON.stringify writes it — key
// order included — each with the stream's own model.
func compareOpenAIStreamObserved(t *testing.T, got, want openaiStreamOutcome) {
	t.Helper()
	if !slices.Equal(got.Observed, want.Observed) {
		t.Errorf("observed:\n got %q\n pi %q", got.Observed, want.Observed)
	}
	if !got.SameModel || !want.SameModel {
		t.Errorf("observer's model is the stream's: got %v, pi %v", got.SameModel, want.SameModel)
	}
}

// Both adapters hand OnProviderStreamEvent every item pi's onProviderStreamEvent
// receives (upstream 002fc8385), before normalizing it: chunks with no choices,
// null, scalars and arrays, events the loop ignores, the event it fails on —
// and nothing the SDK does not yield (an error item, anything after [DONE]).
func TestOpenAIStreamObservesLikePi(t *testing.T) {
	c := loadOpenAIStreamCapture(t)
	type run struct {
		adapter string
		sse     string
		want    *openaiStreamOutcome
	}
	runs := map[string]run{}
	for name, row := range c.Dispatch {
		if row.SDK.Threw != nil && row.SDK.Threw.Name == "SyntaxError" {
			continue // pi fails with V8's SyntaxError text; the port skips the event
		}
		runs["dispatch/"+name+"/completions"] = run{"completions", row.SSE, row.Completions}
		runs["dispatch/"+name+"/responses"] = run{"responses", row.SSE, row.Responses}
	}
	for name, row := range c.Completions {
		runs["completions/"+name] = run{"completions", row.SSE, row.Completions}
	}
	for name, row := range c.Responses {
		runs["responses/"+name] = run{"responses", row.SSE, row.Responses}
	}
	for name, r := range runs {
		t.Run(name, func(t *testing.T) {
			got := runOpenAIStreamAdapter(t, r.adapter, http.StatusOK, r.sse, openaiStreamHooks{})
			compareOpenAIStreamObserved(t, got, *r.want)
			compareOpenAIStreamEnding(t, got, *r.want)
		})
	}
}

// An observer's error fails the stream with its own message, before the event
// it rejected is normalized; when the request was cancelled first, the stream
// ends aborted (pi: stopReason from signal.aborted, errorMessage the throw's).
func TestOpenAIStreamObserverErrorLikePi(t *testing.T) {
	c := loadOpenAIStreamCapture(t)
	for _, adapter := range []string{"completions", "responses"} {
		for hook, failOnEvent := range map[string]string{"callback-throws": "throw", "callback-aborts": "abort-then-throw"} {
			t.Run(adapter+"/"+hook, func(t *testing.T) {
				row, ok := c.Hooks[adapter+"/"+hook]
				if !ok {
					t.Fatalf("%s has no hooks row %s/%s; rerun capture.mts", openaiStreamCaptureFile, adapter, hook)
				}
				got := runOpenAIStreamAdapter(t, adapter, row.Status, row.SSE, openaiStreamHooks{failOnEvent: failOnEvent})
				compareOpenAIStreamObserved(t, got, row.Outcome)
				compareOpenAIStreamEnding(t, got, row.Outcome)
			})
		}
	}
}

// Go-only: the observer's error surfaces as the terminal error event with its
// message verbatim, and the event it rejected is never normalized — the text
// it carried produces no text_delta.
func TestOpenAIProviderStreamEventErrorFailsStream(t *testing.T) {
	c := loadOpenAIStreamCapture(t)
	cases := []struct {
		adapter string
		sse     string
		// rejectAt is the observed event carrying the stream's text.
		rejectAt int
	}{
		{"completions", c.Dispatch["blank-line-dispatch"].SSE, 0},
		{"responses", c.Responses["completed"].SSE, 3},
	}
	for _, tc := range cases {
		for _, cancelFirst := range []bool{false, true} {
			name := tc.adapter
			want := ai.StopError
			if cancelFirst {
				name += "/cancelled"
				want = ai.StopAborted
			}
			t.Run(name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("content-type", "text/event-stream")
					_, _ = io.WriteString(w, tc.sse)
				}))
				t.Cleanup(server.Close)
				model := openaiStreamModel(tc.adapter, server.URL+"/v1")
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				seen := 0
				opts := &ai.SimpleStreamOptions{}
				opts.APIKey = "k"
				opts.OnProviderStreamEvent = func(any, *ai.Model) error {
					seen++
					if seen <= tc.rejectAt {
						return nil
					}
					if cancelFirst {
						cancel()
					}
					return errors.New("observer boom")
				}
				req := ai.NormalizeContext(ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}})
				var stream *ai.AssistantMessageEventStream
				if tc.adapter == "completions" {
					stream = StreamSimpleOpenAICompletions(ctx, model, req, opts)
				} else {
					stream = StreamSimpleOpenAIResponses(ctx, model, req, opts)
				}
				var events []ai.AssistantMessageEvent
				for ev := range stream.Events() {
					events = append(events, ev)
				}
				for _, ev := range events {
					if ev.Type == ai.EventTextDelta {
						t.Fatalf("text_delta %q pushed for the event the observer rejected", ev.Delta)
					}
				}
				last := events[len(events)-1]
				if last.Type != ai.EventError || last.Error == nil {
					t.Fatalf("stream ended with %s, want an error event", last.Type)
				}
				if last.Reason != want || last.Error.StopReason != want || last.Error.ErrorMessage != "observer boom" {
					t.Fatalf("ended %s/%s %q, want %s \"observer boom\"", last.Reason, last.Error.StopReason, last.Error.ErrorMessage, want)
				}
				if seen != tc.rejectAt+1 {
					t.Fatalf("observer ran %d times, want %d: the stream must stop at its error", seen, tc.rejectAt+1)
				}
			})
		}
	}
}
