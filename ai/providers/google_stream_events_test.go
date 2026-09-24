package providers

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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

// googleScenarioResponse is the *http.Response Go's client makes of the
// scenario's head, read by net/http itself.
func googleScenarioResponse(t *testing.T, sc googleStreamScenario) *http.Response {
	t.Helper()
	var head strings.Builder
	head.WriteString("HTTP/1.1 200 OK\r\n")
	for _, h := range sc.Headers {
		fmt.Fprintf(&head, "%s: %s\r\n", h[0], h[1])
	}
	if sc.Framing == "chunked" {
		head.WriteString("Transfer-Encoding: chunked\r\n")
	}
	head.WriteString("\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(strings.NewReader(head.String())), nil)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestGoogleSSEReadChunksMatchPi replays, read by read, every captured
// scenario whose outcome the SDK's read loop decides — a bare JSON read that
// throws ApiError, or a tail left unconsumed — and requires pi's error and
// the chunks pi observed before it. The SDK checks each network read, before
// buffering it, for a bare JSON {"error":{...}} payload
// (processStreamResponse), so where the reads fall is part of the outcome.
func TestGoogleSSEReadChunksMatchPi(t *testing.T) {
	ran := 0
	for _, sc := range loadGoogleStreamCapture(t).Scenarios {
		msg := sc.Pi.ErrorMessage
		if sc.Divergence != "" || !(strings.HasPrefix(msg, "got status: ") || msg == "Incomplete JSON segment at the end") {
			continue
		}
		ran++
		t.Run(sc.Name, func(t *testing.T) {
			headers := googleSDKResponseHeaders(googleScenarioResponse(t, sc))
			events := []string{}
			observe := func(data any) error {
				text, err := jstext.Stringify(googleGenerateContentResponse(data, headers))
				events = append(events, text)
				return err
			}
			err := iterateGoogleSSE(&readsReader{reads: append([]string(nil), sc.Segments...)}, context.Background(),
				observe, func(googleChunk) error { return nil })
			if err == nil || err.Error() != msg {
				t.Fatalf("error %v\npi:   %s", err, msg)
			}
			if !slices.Equal(events, sc.Pi.Events) {
				t.Fatalf("observed %q\npi:       %q", events, sc.Pi.Events)
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

// serveGoogleScenario answers every request with the scenario's raw response:
// the captured head line for line, then the whole body in one write (gzipped
// when sc.Gzip, as one HTTP chunk when chunked) — the joined form, which is
// pi's outcome too for every scenario whose read boundaries do not matter.
func serveGoogleScenario(t *testing.T, sc googleStreamScenario) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	body := []byte(strings.Join(sc.Segments, ""))
	if sc.Gzip {
		var zipped bytes.Buffer
		zw := gzip.NewWriter(&zipped)
		zw.Write(body)
		zw.Close()
		body = zipped.Bytes()
	}
	var resp bytes.Buffer
	resp.WriteString("HTTP/1.1 200 OK\r\n")
	for _, h := range sc.Headers {
		fmt.Fprintf(&resp, "%s: %s\r\n", h[0], h[1])
	}
	if sc.Framing == "chunked" {
		fmt.Fprintf(&resp, "Transfer-Encoding: chunked\r\n\r\n%x\r\n%s\r\n0\r\n\r\n", len(body), body)
	} else {
		resp.WriteString("\r\n")
		resp.Write(body)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				req, err := http.ReadRequest(bufio.NewReader(conn))
				if err != nil {
					return
				}
				io.Copy(io.Discard, req.Body)
				conn.Write(resp.Bytes())
			}()
		}
	}()
	return "http://" + ln.Addr().String()
}

// googleCaptureModel is the literal model capture.mts streams with.
func googleCaptureModel(baseURL string) *ai.Model {
	return &ai.Model{
		ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", Api: ai.APIGoogleGenerativeAI, Provider: "google",
		BaseURL: baseURL, Reasoning: true, Input: []string{"text", "image"},
		Cost:          ai.ModelCost{Input: 0.3, Output: 2.5, CacheRead: 0.03},
		ContextWindow: 1048576, MaxTokens: 65536,
	}
}

// TestGoogleStreamEventsMatchPi replays each captured scenario whose outcome
// does not hang on where the reads fall, over a real connection, and requires
// pi's result: every value OnProviderStreamEvent received, as JSON.stringify
// writes it (key order included), always with the model the stream was
// called with; the assistant stream event by event (with deltas); the stop
// reason, error message, response id, content JSON and usage tokens.
func TestGoogleStreamEventsMatchPi(t *testing.T) {
	ran := 0
	for _, sc := range loadGoogleStreamCapture(t).Scenarios {
		if sc.Divergence != "" || sc.ReadBoundariesMatter {
			continue
		}
		ran++
		t.Run(sc.Name, func(t *testing.T) {
			model := googleCaptureModel(serveGoogleScenario(t, sc))
			req := ai.NormalizeContext(ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}})
			events := []string{}
			sameModel, calls := true, 0
			stream := StreamGoogle(context.Background(), model, req, &GoogleOptions{StreamOptions: ai.StreamOptions{
				ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "test-api-key"},
				OnProviderStreamEvent: func(data any, eventModel *ai.Model) error {
					if eventModel != model {
						sameModel = false
					}
					if sc.ThrowOn != nil && *sc.ThrowOn == calls {
						return errors.New(sc.ThrowMessage)
					}
					calls++
					text, err := jstext.Stringify(data)
					events = append(events, text)
					return err
				},
			}})
			var got, want []string
			for ev := range stream.Events() {
				switch ev.Type {
				case ai.EventTextDelta, ai.EventThinkingDelta, ai.EventToolCallDelta:
					got = append(got, string(ev.Type)+" "+ev.Delta)
				default:
					got = append(got, string(ev.Type))
				}
			}
			for _, ev := range sc.Pi.Stream {
				if ev.Delta != nil {
					want = append(want, ev.Type+" "+*ev.Delta)
				} else {
					want = append(want, ev.Type)
				}
			}
			if !slices.Equal(got, want) {
				t.Errorf("stream %q\npi:     %q", got, want)
			}
			final := stream.Result()
			if !slices.Equal(events, sc.Pi.Events) {
				t.Errorf("observed %q\npi:       %q", events, sc.Pi.Events)
			}
			if !sameModel || !sc.Pi.SameModel {
				t.Errorf("observer's model: same %v, pi same %v", sameModel, sc.Pi.SameModel)
			}
			if string(final.StopReason) != sc.Pi.StopReason || final.ErrorMessage != sc.Pi.ErrorMessage {
				t.Errorf("stop %s %q, pi %s %q", final.StopReason, final.ErrorMessage, sc.Pi.StopReason, sc.Pi.ErrorMessage)
			}
			if final.ResponseID != sc.Pi.ResponseID {
				t.Errorf("responseId %q, pi %q", final.ResponseID, sc.Pi.ResponseID)
			}
			if content, _ := jstext.Stringify(final.Content); content != sc.Pi.Content {
				t.Errorf("content %s\npi:      %s", content, sc.Pi.Content)
			}
			u, pu := final.Usage, sc.Pi.Usage
			if u.Input != pu.Input || u.Output != pu.Output || u.CacheRead != pu.CacheRead || u.CacheWrite != pu.CacheWrite || u.TotalTokens != pu.TotalTokens {
				t.Errorf("usage %+v, pi %+v", u, pu)
			}
		})
	}
	if ran < 15 {
		t.Fatalf("only %d replayable scenarios in the capture", ran)
	}
}
