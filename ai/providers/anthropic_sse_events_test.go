package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// anthropicSSEEventsCaptureFile is written by
// testdata/anthropic-sse/capture-anthropic-sse-events.mts, which streams pi's
// anthropic-messages adapter from source at the named sha.
const anthropicSSEEventsCaptureFile = "testdata/anthropic-sse/anthropic-sse-events-8676a0dcd.json"

type anthropicSSEEventsRow struct {
	Name       string   `json:"name"`
	SSE        string   `json:"sse"`
	V8Cause    bool     `json:"v8Cause"`
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

func loadAnthropicSSEEventsCapture(t *testing.T) []anthropicSSEEventsRow {
	t.Helper()
	data, err := os.ReadFile(anthropicSSEEventsCaptureFile)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate it with testdata/anthropic-sse/capture-anthropic-sse-events.mts)", anthropicSSEEventsCaptureFile, err)
	}
	var capture struct {
		Model string                  `json:"model"`
		Rows  []anthropicSSEEventsRow `json:"rows"`
	}
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatalf("decode %s: %v", anthropicSSEEventsCaptureFile, err)
	}
	if capture.Model != "anthropic/claude-haiku-4-5" || len(capture.Rows) == 0 {
		t.Fatalf("%s: model %q, %d rows", anthropicSSEEventsCaptureFile, capture.Model, len(capture.Rows))
	}
	return capture.Rows
}

// streamAnthropicSSEEvents streams body on claude-haiku-4-5 and returns the
// type of every pushed event and the final message.
func streamAnthropicSSEEvents(t *testing.T, ctx context.Context, body string, opts ai.StreamOptions) ([]string, *ai.AssistantMessage) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(200)
		io.WriteString(w, body)
	}))
	defer server.Close()
	base := ai.GetModel("anthropic", "claude-haiku-4-5")
	if base == nil {
		t.Fatal("catalog has no anthropic/claude-haiku-4-5")
	}
	model := *base
	model.BaseURL = server.URL
	opts.APIKey = "k"
	stream := StreamAnthropic(ctx, &model, ai.NormalizeContext(ai.Context{Messages: []ai.Message{ai.NewUserText("Hello", 1)}}),
		&AnthropicOptions{StreamOptions: opts})
	var pushed []string
	for ev := range stream.Events() {
		pushed = append(pushed, string(ev.Type))
	}
	return pushed, stream.Result()
}

// assertAnthropicMessageMatchesPi compares the final message with pi's. Where
// pi's error embeds V8's JSON.parse text (v8Cause), which the port does not
// reproduce, the text on either side of it must still be pi's exactly.
func assertAnthropicMessageMatchesPi(t *testing.T, row anthropicSSEEventsRow, final *ai.AssistantMessage) {
	t.Helper()
	want := row.Message
	if final.StopReason != want.StopReason {
		t.Errorf("stopReason = %s, want %s (%s)", final.StopReason, want.StopReason, final.ErrorMessage)
	}
	if row.V8Cause {
		gotHead, gotCause, gotTail, gotOK := splitAnthropicParseFailure(final.ErrorMessage)
		wantHead, _, wantTail, wantOK := splitAnthropicParseFailure(want.ErrorMessage)
		if !gotOK || !wantOK || gotHead != wantHead || gotTail != wantTail || gotCause == "" {
			t.Errorf("errorMessage = %q, want %q around the JSON.parse cause", final.ErrorMessage, want.ErrorMessage)
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

// splitAnthropicParseFailure splits "Could not parse Anthropic SSE event <name>:
// <cause>; data=<data>; raw=<raw>" around its cause.
func splitAnthropicParseFailure(msg string) (head, cause, tail string, ok bool) {
	const lead = "Could not parse Anthropic SSE event "
	if !strings.HasPrefix(msg, lead) {
		return "", "", "", false
	}
	colon := strings.Index(msg[len(lead):], ": ")
	if colon < 0 {
		return "", "", "", false
	}
	headEnd := len(lead) + colon + 2
	data := strings.Index(msg[headEnd:], "; data=")
	if data < 0 {
		return "", "", "", false
	}
	return msg[:headEnd], msg[headEnd : headEnd+data], msg[headEnd+data:], true
}

// TestAnthropicSSEEventsMatchPi replays each captured body and requires pi's
// pushed events and final message: named events whose data is JSON but not an
// object are passed over, a `null` event fails with pi's TypeError text, and a
// parse failure carries pi's data= and raw= suffix — raw being every line of
// the event, comments included, joined with a literal backslash-n.
func TestAnthropicSSEEventsMatchPi(t *testing.T) {
	for _, row := range loadAnthropicSSEEventsCapture(t) {
		if row.ThrowAt != nil {
			continue // observer rows need onProviderStreamEvent
		}
		t.Run(row.Name, func(t *testing.T) {
			pushed, final := streamAnthropicSSEEvents(t, context.Background(), row.SSE, ai.StreamOptions{})
			if !reflect.DeepEqual(pushed, row.Pushed) {
				t.Errorf("pushed = %q, want %q", pushed, row.Pushed)
			}
			assertAnthropicMessageMatchesPi(t, row, final)
		})
	}
}
