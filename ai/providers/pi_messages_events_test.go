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
}

// assertPiMessagesFrameSyntaxError requires reading a v8Error row's body to
// fail with a JSON syntax error, as pi's JSON.parse throws a SyntaxError.
func assertPiMessagesFrameSyntaxError(t *testing.T, row piMessagesEventsRow) {
	t.Helper()
	err := readPiMessagesEvents(strings.NewReader(row.SSE), nil, func(piMessagesEvent) bool { return true })
	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		t.Errorf("reading the body returned %v, want the JSON syntax error of frame data %q", err, row.V8Error)
	}
}

// TestPiMessagesEventsMatchPi replays each captured body and requires pi's
// pushed events and final message: a JS-falsy frame is skipped, any other
// value that is not an object converts to an event with no type, and a frame
// that is not JSON fails the stream.
func TestPiMessagesEventsMatchPi(t *testing.T) {
	for _, row := range loadPiMessagesEventsCapture(t) {
		if row.ThrowAt != nil {
			continue // observer rows need onProviderStreamEvent
		}
		t.Run(row.Name, func(t *testing.T) {
			_, pushed, final := streamPiMessagesEvents(t, context.Background(), row.SSE, ai.StreamOptions{})
			if !reflect.DeepEqual(pushed, row.Pushed) {
				t.Errorf("pushed = %q, want %q", pushed, row.Pushed)
			}
			assertPiMessagesMessageMatchesPi(t, row, final)
			if row.V8Error != "" {
				assertPiMessagesFrameSyntaxError(t, row)
			}
		})
	}
}
