package providers

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/sky-valley/pi/ai"
	"github.com/sky-valley/pi/internal/jstext"
)

// googleStreamCapture is testdata/google-stream-events/google-stream-events-*.json:
// what pi's google-generative-ai adapter, running on @google/genai, does with
// each of a table of raw HTTP responses. capture.mts beside it says how it is
// made.
type googleStreamCapture struct {
	Source    string                 `json:"source"`
	Scenarios []googleStreamScenario `json:"scenarios"`
}

type googleStreamScenario struct {
	Name       string `json:"name"`
	Divergence string `json:"divergence"`
	Framing    string `json:"framing"` // "close" or "chunked"
	Gzip       bool   `json:"gzip"`
	// Headers is the response head, one [name, value] per line.
	Headers [][2]string `json:"headers"`
	// Segments are the body, each written as its own network read.
	Segments     []string `json:"segments"`
	ThrowOn      *int     `json:"throwOn"`
	ThrowMessage string   `json:"throwMessage"`
	// ReadBoundariesMatter reports whether pi's outcome changes when the
	// segments arrive as one read.
	ReadBoundariesMatter bool `json:"readBoundariesMatter"`
	Pi                   struct {
		Events    []string `json:"events"`
		SameModel bool     `json:"sameModel"`
		Stream    []struct {
			Type  string  `json:"type"`
			Delta *string `json:"delta"`
		} `json:"stream"`
		StopReason   string `json:"stopReason"`
		ErrorMessage string `json:"errorMessage"`
		ResponseID   string `json:"responseId"`
		Content      string `json:"content"`
		Usage        struct {
			Input       int `json:"input"`
			Output      int `json:"output"`
			CacheRead   int `json:"cacheRead"`
			CacheWrite  int `json:"cacheWrite"`
			TotalTokens int `json:"totalTokens"`
		} `json:"usage"`
	} `json:"pi"`
}

func loadGoogleStreamCapture(t *testing.T) googleStreamCapture {
	t.Helper()
	raw, err := os.ReadFile("testdata/google-stream-events/google-stream-events-8676a0dcd.json")
	if err != nil {
		t.Fatal(err)
	}
	var c googleStreamCapture
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// readsReader returns one of its reads per Read call, so a body arrives in
// exactly the network reads a scenario prescribes.
type readsReader struct{ reads []string }

func (r *readsReader) Read(p []byte) (int, error) {
	if len(r.reads) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.reads[0])
	if r.reads[0] = r.reads[0][n:]; r.reads[0] == "" {
		r.reads = r.reads[1:]
	}
	return n, nil
}

// TestGoogleSSEReadChunksMatchPi replays, read by read, every captured
// scenario whose outcome the SDK's read loop decides — a bare JSON read that
// throws ApiError, or a tail left unconsumed — and requires pi's error. The
// SDK checks each network read, before buffering it, for a bare JSON
// {"error":{...}} payload (processStreamResponse), so where the reads fall is
// part of the outcome.
func TestGoogleSSEReadChunksMatchPi(t *testing.T) {
	ran := 0
	for _, sc := range loadGoogleStreamCapture(t).Scenarios {
		msg := sc.Pi.ErrorMessage
		if sc.Divergence != "" || !(strings.HasPrefix(msg, "got status: ") || msg == "Incomplete JSON segment at the end") {
			continue
		}
		ran++
		t.Run(sc.Name, func(t *testing.T) {
			err := iterateGoogleSSE(&readsReader{reads: append([]string(nil), sc.Segments...)}, context.Background(),
				func(googleChunk) error { return nil })
			if err == nil || err.Error() != msg {
				t.Fatalf("error %v\npi:   %s", err, msg)
			}
		})
	}
	if ran < 10 {
		t.Fatalf("only %d read-loop scenarios in the capture", ran)
	}
}

// googleCaptureScenario returns the named scenario of the capture.
func googleCaptureScenario(t *testing.T, name string) googleStreamScenario {
	t.Helper()
	for _, sc := range loadGoogleStreamCapture(t).Scenarios {
		if sc.Name == name {
			return sc
		}
	}
	t.Fatalf("no scenario %q in the capture", name)
	return googleStreamScenario{}
}

// TestGoogleToolCallArgumentsKeepModelOrder: pi's tool-call arguments are the
// parsed functionCall.args object, so JSON.stringify of them (the
// toolcall_delta) and every later replay keep the model's key order, nested
// objects included.
func TestGoogleToolCallArgumentsKeepModelOrder(t *testing.T) {
	sc := googleCaptureScenario(t, "thinking, text and a function call")
	stream := googleServe(t, "gemini-2.5-flash", strings.Join(sc.Segments, ""))
	var deltas []string
	for ev := range stream.Events() {
		if ev.Type == ai.EventToolCallDelta {
			deltas = append(deltas, ev.Delta)
		}
	}
	var piDeltas []string
	for _, ev := range sc.Pi.Stream {
		if ev.Type == "toolcall_delta" {
			piDeltas = append(piDeltas, *ev.Delta)
		}
	}
	if !slices.Equal(deltas, piDeltas) {
		t.Fatalf("toolcall_delta %q\npi:            %q", deltas, piDeltas)
	}
	content, err := jstext.Stringify(stream.Result().Content)
	if err != nil {
		t.Fatal(err)
	}
	if content != sc.Pi.Content {
		t.Fatalf("content %s\npi:      %s", content, sc.Pi.Content)
	}
}
