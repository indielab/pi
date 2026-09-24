package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// piMessagesEventsCaptureFile is written by
// testdata/pi-messages/capture-pi-messages-events.mts, which streams pi's
// pi-messages adapter from source at the named sha.
const piMessagesEventsCaptureFile = "testdata/pi-messages/pi-messages-events-8676a0dcd.json"

type piMessagesEventsRow struct {
	Name string `json:"name"`
	SSE  string `json:"sse"`
	// V8Error is the frame data whose JSON.parse failure is pi's errorMessage.
	V8Error    string   `json:"v8Error"`
	ThrowAt    *int     `json:"throwAt"`
	AbortFirst bool     `json:"abortFirst"`
	Observed   []string `json:"observed"`
	// Pushed holds each pushed event's type; pi's null (no type) decodes to "".
	Pushed  []string `json:"pushed"`
	Message struct {
		StopReason   ai.StopReason   `json:"stopReason"`
		ErrorMessage string          `json:"errorMessage"`
		ResponseID   string          `json:"responseId"`
		Content      json.RawMessage `json:"content"`
		// Usage is decoded into ai.Usage, so pi's explicit zero and the port's
		// omitted zero (omitempty) compare equal, as the in-memory values are.
		Usage       ai.Usage `json:"usage"`
		Diagnostics []struct {
			Type    string          `json:"type"`
			Details json.RawMessage `json:"details"`
		} `json:"diagnostics"`
	} `json:"message"`
}

func loadPiMessagesEventsCapture(t *testing.T) []piMessagesEventsRow {
	t.Helper()
	data, err := os.ReadFile(piMessagesEventsCaptureFile)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate it with testdata/pi-messages/capture-pi-messages-events.mts)", piMessagesEventsCaptureFile, err)
	}
	var capture struct {
		Rows []piMessagesEventsRow `json:"rows"`
	}
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatalf("decode %s: %v", piMessagesEventsCaptureFile, err)
	}
	if len(capture.Rows) == 0 {
		t.Fatalf("%s has no rows", piMessagesEventsCaptureFile)
	}
	return capture.Rows
}

// streamPiMessagesEvents streams body through StreamSimplePiMessages on the
// upstream suite's model and returns the model, the type of every pushed event
// and the final message.
func streamPiMessagesEvents(t *testing.T, ctx context.Context, body string, opts ai.StreamOptions) (*ai.Model, []string, *ai.AssistantMessage) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, body)
	}))
	defer server.Close()
	model := piMessagesTestModel(server.URL + "/v1")
	opts.APIKey = "test-key"
	stream := StreamSimplePiMessages(ctx, model, ai.NormalizeContext(piMessagesTestContext()), &ai.SimpleStreamOptions{StreamOptions: opts})
	var pushed []string
	for ev := range stream.Events() {
		pushed = append(pushed, string(ev.Type))
	}
	return model, pushed, stream.Result()
}

// assertPiMessagesMessageMatchesPi compares the final message with pi's. A
// v8Error row's message is V8's JSON.parse text for that frame's data, which
// the port does not reproduce (encoding/json words the failure its own way);
// assertPiMessagesFrameSyntaxError pins what is pi's there — that the read
// fails on that frame with a JSON syntax error.
func assertPiMessagesMessageMatchesPi(t *testing.T, row piMessagesEventsRow, final *ai.AssistantMessage) {
	t.Helper()
	want := row.Message
	if final.StopReason != want.StopReason {
		t.Errorf("stopReason = %s, want %s (%s)", final.StopReason, want.StopReason, final.ErrorMessage)
	}
	if row.V8Error != "" {
		if final.ErrorMessage == "" {
			t.Errorf("errorMessage is empty, want the JSON parse failure of %q (pi: %q)", row.V8Error, want.ErrorMessage)
		}
	} else if final.ErrorMessage != want.ErrorMessage {
		t.Errorf("errorMessage = %q, want %q", final.ErrorMessage, want.ErrorMessage)
	}
	if final.ResponseID != want.ResponseID {
		t.Errorf("responseId = %q, want %q", final.ResponseID, want.ResponseID)
	}
	gotContent, err := json.Marshal(final.Content)
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}
	var g, w any
	if err := json.Unmarshal(gotContent, &g); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	if err := json.Unmarshal(want.Content, &w); err != nil {
		t.Fatalf("decode pi content: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("content = %s, want %s", gotContent, want.Content)
	}
	if final.Usage != want.Usage {
		t.Errorf("usage = %+v, want %+v", final.Usage, want.Usage)
	}
	if len(final.Diagnostics) != len(want.Diagnostics) {
		t.Fatalf("diagnostics = %+v, want %d", final.Diagnostics, len(want.Diagnostics))
	}
	for i, d := range final.Diagnostics {
		gotDetails, err := json.Marshal(d.Details)
		if err != nil {
			t.Fatalf("marshal diagnostic details: %v", err)
		}
		var gd, wd any
		if err := json.Unmarshal(gotDetails, &gd); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(want.Diagnostics[i].Details, &wd); err != nil {
			t.Fatal(err)
		}
		if d.Type != want.Diagnostics[i].Type || !reflect.DeepEqual(gd, wd) {
			t.Errorf("diagnostic %d = %s %s, want %s %s", i, d.Type, gotDetails, want.Diagnostics[i].Type, want.Diagnostics[i].Details)
		}
	}
}

// assertPiMessagesFrameSyntaxError requires reading a v8Error row's body to
// fail with a JSON syntax error, as pi's JSON.parse throws a SyntaxError.
func assertPiMessagesFrameSyntaxError(t *testing.T, row piMessagesEventsRow) {
	t.Helper()
	err := readPiMessagesEvents(strings.NewReader(row.SSE), nil, nil, func(piMessagesEvent) (bool, error) { return true, nil })
	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		t.Errorf("reading the body returned %v, want the JSON syntax error of frame data %q", err, row.V8Error)
	}
}

// TestPiMessagesEventsMatchPi replays each captured body and requires pi's
// observed events, pushed events and final message: a JS-falsy frame is
// skipped unobserved, any other value that is not an object is observed and
// converts to an event with no type, an object converts whatever its members
// hold, a frame that is not JSON fails the stream, the terminal done is
// observed before it converts, and an observer error fails the stream with its
// message — aborted when the request was cancelled, and never done even when
// thrown on the done event.
//
// A row whose observer does not throw is replayed with no observer too: pi's
// pushed events and message do not depend on observing, and without an
// observer the port decodes each frame once, never into the observer's value.
func TestPiMessagesEventsMatchPi(t *testing.T) {
	for _, row := range loadPiMessagesEventsCapture(t) {
		t.Run(row.Name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			obs := &providerStreamObserver{t: t, throwAt: row.ThrowAt, abortFirst: row.AbortFirst, cancel: cancel}
			model, pushed, final := streamPiMessagesEvents(t, ctx, row.SSE, ai.StreamOptions{OnProviderStreamEvent: obs.observe})
			if !reflect.DeepEqual(obs.observed, row.Observed) && (len(obs.observed) > 0 || len(row.Observed) > 0) {
				t.Errorf("observed = %q, want %q", obs.observed, row.Observed)
			}
			obs.assertModel(model)
			if !reflect.DeepEqual(pushed, row.Pushed) {
				t.Errorf("pushed = %q, want %q", pushed, row.Pushed)
			}
			assertPiMessagesMessageMatchesPi(t, row, final)
			if row.V8Error != "" {
				assertPiMessagesFrameSyntaxError(t, row)
			}
			if row.ThrowAt == nil {
				_, pushed, final := streamPiMessagesEvents(t, context.Background(), row.SSE, ai.StreamOptions{})
				if !reflect.DeepEqual(pushed, row.Pushed) {
					t.Errorf("without an observer: pushed = %q, want %q", pushed, row.Pushed)
				}
				assertPiMessagesMessageMatchesPi(t, row, final)
			}
		})
	}
}

// TestPiMessagesForwardsWireEventsInOrder transliterates upstream's 'forwards
// parsed wire events in order before converting them' (002fc8385): the
// observer receives each wire event as parsed — unknown fields and the
// terminal done included — with the model the stream was called with.
func TestPiMessagesForwardsWireEventsInOrder(t *testing.T) {
	wireEvents := []string{
		`{"type":"start"}`,
		`{"type":"text_start","contentIndex":0}`,
		`{"type":"text_delta","contentIndex":0,"delta":"Hello","gatewayField":"upstream-value"}`,
		`{"type":"text_end","contentIndex":0,"content":"Hello"}`,
		`{"type":"done","reason":"stop","usage":` + piMessagesUsageJSON + `,"responseId":"resp_1"}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, piMessagesSSE(wireEvents...))
	}))
	defer server.Close()
	model := piMessagesTestModel(server.URL + "/v1")
	var received []any
	var models []*ai.Model
	message := StreamSimplePiMessages(context.Background(), model, ai.NormalizeContext(piMessagesTestContext()), &ai.SimpleStreamOptions{
		StreamOptions: ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "test-key"},
			OnProviderStreamEvent: func(data any, eventModel *ai.Model) error {
				received = append(received, data)
				models = append(models, eventModel)
				return nil
			},
		},
	}).Result()

	want := make([]any, len(wireEvents))
	for i, e := range wireEvents {
		v, err := ai.DecodeOrderedValue([]byte(e))
		if err != nil {
			t.Fatal(err)
		}
		want[i] = v
	}
	if !reflect.DeepEqual(received, want) {
		t.Errorf("received = %v, want %v", received, want)
	}
	if len(models) != len(wireEvents) {
		t.Fatalf("observer called %d times, want %d", len(models), len(wireEvents))
	}
	for i, m := range models {
		if m != model {
			t.Errorf("event %d: observer got model %p, want %p", i, m, model)
		}
	}
	if message.StopReason != ai.StopStop {
		t.Fatalf("stopReason = %s, want stop (%s)", message.StopReason, message.ErrorMessage)
	}
	if message.ResponseID != "resp_1" {
		t.Errorf("responseId = %q, want resp_1", message.ResponseID)
	}
	content, _ := json.Marshal(message.Content)
	if string(content) != `[{"type":"text","text":"Hello"}]` {
		t.Errorf("content = %s, want [{\"type\":\"text\",\"text\":\"Hello\"}]", content)
	}
}

// TestPiMessagesDecodesFramesOnceWithoutAnObserver locks the cost of the
// observer seam (spec: decode for the observer only when there is one): the
// value an observer receives is its own decode of the frame
// (ai.DecodeOrderedValue), so reading a body without an observer must save at
// least those allocations over reading it with one.
func TestPiMessagesDecodesFramesOnceWithoutAnObserver(t *testing.T) {
	frames := []string{
		`{"type":"start"}`,
		`{"type":"text_start","contentIndex":0}`,
		`{"type":"text_delta","contentIndex":0,"delta":"Hello there","gatewayField":"upstream-value"}`,
		`{"type":"text_end","contentIndex":0,"content":"Hello there"}`,
	}
	body := piMessagesSSE(frames...)
	keepReading := func(piMessagesEvent) (bool, error) { return true, nil }
	read := func(onEvent func(any) error) float64 {
		return testing.AllocsPerRun(50, func() {
			if err := readPiMessagesEvents(strings.NewReader(body), nil, onEvent, keepReading); err != nil {
				t.Fatal(err)
			}
		})
	}
	without := read(nil)
	with := read(func(any) error { return nil })
	observerValues := testing.AllocsPerRun(50, func() {
		for _, f := range frames {
			if _, err := ai.DecodeOrderedValue([]byte(f)); err != nil {
				t.Fatal(err)
			}
		}
	})
	if without > with-observerValues*0.9 {
		t.Fatalf("reading without an observer allocates %.0f times, with one %.0f; the observer's values alone take %.0f, so frames are being decoded for an observer that is not there", without, with, observerValues)
	}
}
