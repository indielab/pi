package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/sky-valley/pi/ai"
	"github.com/sky-valley/pi/internal/jstext"
)

// This file ports how pi's two openai adapters read a streamed response: the
// openai SDK's Stream.fromSSEResponse over _iterSSEMessages (openai 6.40.0,
// core/streaming.js and internal/decoders/line.js — the version pi's
// package-lock.json locks). Both adapters iterate the SDK's Stream, so both Go
// loops read their body through iterateOpenAIStream.

// openaiSSEEvent is one server-sent event as the SDK's SSEDecoder dispatches
// it. An absent event name and an empty one read alike everywhere the SDK looks
// at it.
type openaiSSEEvent struct {
	event string
	data  string
}

// utf8BOM is the byte-order mark the SDK's per-line UTF-8 decode drops.
const utf8BOM = "\xef\xbb\xbf"

// readOpenAISSE is the SDK's _iterSSEMessages. Lines end at "\n", "\r\n" or a
// lone "\r" (LineDecoder), and each line is decoded on its own by a
// TextDecoder, which drops one leading byte-order mark. A blank line
// dispatches the event collected so far, unless it has neither a name nor any
// data; `data` lines join with "\n"; a line starting with ":" is a comment;
// every other field (id, retry, …) is ignored. An event the body ends before
// its blank line is never dispatched.
//
// A field's value is everything after the line's first colon less exactly one
// leading space. Nothing else is trimmed, and JSON.parse accepts only JSON's
// own whitespace around a value, so data padded with anything else (U+00A0,
// U+FEFF, U+0085, VT, FF) does not parse.
func readOpenAISSE(body io.Reader, ctx context.Context, dispatch func(openaiSSEEvent) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	scanner.Split(scanOpenAISSELines)
	var event string
	var data []string
	for scanner.Scan() {
		if ctx != nil && ctx.Err() != nil {
			return fmt.Errorf("Request was aborted")
		}
		line := strings.TrimPrefix(scanner.Text(), utf8BOM)
		if line == "" {
			if event == "" && len(data) == 0 {
				continue
			}
			sse := openaiSSEEvent{event: event, data: strings.Join(data, "\n")}
			event, data = "", nil
			if err := dispatch(sse); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			data = append(data, value)
		}
	}
	return scanner.Err()
}

// scanOpenAISSELines splits lines the way the SDK's LineDecoder does: "\r\n"
// is one line ending, and a "\r" not followed by "\n" ends a line of its own.
// A "\r" at the end of what has been read so far waits for the next byte to
// tell the two apart; at the end of the body it ends the line. So no line
// keeps a trailing "\r", which is why SSEDecoder's own strip of one never
// applies here. The body's last line needs no ending (LineDecoder.flush).
func scanOpenAISSELines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		if data[i] == '\n' {
			return i + 1, data[:i], nil
		}
		if i+1 < len(data) {
			if data[i+1] == '\n' {
				return i + 2, data[:i], nil
			}
			return i + 1, data[:i], nil
		}
		if !atEOF {
			return 0, nil, nil
		}
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// iterateOpenAIStream is the SDK's Stream.fromSSEResponse iterator over
// readOpenAISSE: it hands yield the JSON text of each item the SDK yields.
//
//   - An event whose data starts with "[DONE]" ends the stream, and every
//     event after it is ignored. The body is still read to its end, so a read
//     failure after it still surfaces.
//   - Data is JSON-parsed. parse returns the text that parsed, or false where
//     JSON.parse throws; the port skips such an event rather than failing the
//     stream (a deliberate leniency: pi fails with V8's SyntaxError text).
//   - An event named "thread.*" is yielded as {"event": name, "data": value}.
//   - Any other event whose value has a truthy `error` member throws the SDK's
//     APIError, as *openaiStreamChunkError, instead of being yielded.
func iterateOpenAIStream(body io.Reader, ctx context.Context, parse func(data string) ([]byte, bool), yield func(item []byte) error) error {
	done := false
	return readOpenAISSE(body, ctx, func(sse openaiSSEEvent) error {
		if done {
			return nil
		}
		if strings.HasPrefix(sse.data, "[DONE]") {
			done = true
			return nil
		}
		item, ok := parse(sse.data)
		if !ok {
			return nil
		}
		if strings.HasPrefix(sse.event, "thread.") {
			name, _ := json.Marshal(sse.event)
			item = bytes.Join([][]byte{[]byte(`{"event":`), name, []byte(`,"data":`), item, []byte(`}`)}, nil)
		} else if value, ok := openaiStreamErrorMember(item); ok {
			return &openaiStreamChunkError{value: value}
		}
		return yield(item)
	})
}

// observeOpenAIStreamItem hands onEvent, when set, the item as pi's
// onProviderStreamEvent receives it: the value JSON.parse made of it, so an
// object arrives as an ai.OrderedObject in wire order and null, scalars and
// arrays arrive too. Its error fails the stream. An item DecodeOrderedValue
// cannot represent — only a number past float64's range, which JSON.parse
// reads as ±Infinity — is not observed.
func observeOpenAIStreamItem(onEvent func(any) error, item []byte) error {
	if onEvent == nil {
		return nil
	}
	data, err := ai.DecodeOrderedValue(item)
	if err != nil {
		return nil
	}
	return onEvent(data)
}

// openaiStreamJSON is JSON.parse's acceptance test for an event's data.
func openaiStreamJSON(data string) ([]byte, bool) {
	b := []byte(data)
	return b, json.Valid(b)
}

// openaiStreamJSONWithRepair is openaiStreamJSON, falling back to repairJSON's
// fix-ups: the completions loop has always read chunks through
// parseJSONWithRepair, and the provider stream event it reports is the
// repaired value, so what it observes is exactly what it handles.
func openaiStreamJSONWithRepair(data string) ([]byte, bool) {
	if b, ok := openaiStreamJSON(data); ok {
		return b, true
	}
	if repaired := repairJSON(data); repaired != data {
		return openaiStreamJSON(repaired)
	}
	return nil, false
}

// openaiStreamErrorMember is the SDK's `data && data.error` test: the item's
// `error` member when the item is an object whose `error` is truthy. The key
// match is exact, as a JS property read is.
func openaiStreamErrorMember(item []byte) (json.RawMessage, bool) {
	var top map[string]json.RawMessage
	if json.Unmarshal(item, &top) != nil {
		return nil, false
	}
	value, has := top["error"]
	return value, has && rawTruthy(value)
}

// openaiStreamChunkError is the openai SDK's APIError for a stream item
// carrying an error: `new APIError(undefined, data.error, undefined, headers)`.
// Its message is makeMessage's with no status, which is also what pi's
// adapters surface, since formatProviderError adds nothing without one.
type openaiStreamChunkError struct {
	// value is data.error, the APIError's `error`.
	value json.RawMessage
}

func (e *openaiStreamChunkError) Error() string {
	if msg := openaiSDKErrorDetail(e.value); msg != "" {
		return msg
	}
	return "(no status code or body)"
}

// metadataRaw is pi's `(error as any)?.error?.metadata?.raw` for this error,
// as the String() the completions catch block appends: ok is false unless raw
// is truthy. A raw String() would throw on (an object with its own toString
// member) is not appended; pi's catch block itself throws there and never
// ends the stream, which the port does not reproduce.
func (e *openaiStreamChunkError) metadataRaw() (string, bool) {
	value, err := jstext.Parse(e.value)
	if err != nil {
		return "", false
	}
	errObj, _ := value.(map[string]any)
	metadata, _ := errObj["metadata"].(map[string]any)
	raw := metadata["raw"]
	if !jstext.Truthy(raw) {
		return "", false
	}
	return jstext.ToString(raw)
}

// withOpenAIErrorMetadataRaw is the openai-completions catch block's append:
// some providers behind OpenRouter put detail in error.metadata.raw, and pi
// adds it on its own line unless the message already contains it. Only the
// SDK's APIError carries an `error`; any other failure passes through.
func withOpenAIErrorMetadataRaw(err error) error {
	var chunkErr *openaiStreamChunkError
	if !errors.As(err, &chunkErr) {
		return err
	}
	raw, ok := chunkErr.metadataRaw()
	if !ok || strings.Contains(err.Error(), raw) {
		return err
	}
	return errors.New(err.Error() + "\n" + raw)
}
