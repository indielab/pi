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

// readOpenAISSE is the SDK's _iterSSEMessages. The body reaches its line
// splitting in iterSSEChunks' pieces (openaiSSEChunkReader). Lines end at
// "\n", "\r\n" or a lone "\r" (LineDecoder), and each line is decoded on its
// own by a TextDecoder (jstext.DecodeUTF8), which writes one U+FFFD per
// maximal subpart of an invalid sequence and drops one leading byte-order
// mark. A blank line dispatches the event collected so far, unless it has
// neither a name nor any data; `data` lines join with "\n"; a line starting
// with ":" is a comment; every other field (id, retry, …) is ignored. An event
// the body ends before its blank line is never dispatched.
//
// A field's value is everything after the line's first colon less exactly one
// leading space. Nothing else is trimmed, and JSON.parse accepts only JSON's
// own whitespace around a value, so data padded with anything else (U+00A0,
// U+FEFF, U+0085, VT, FF) does not parse.
//
// A line longer than maxOpenAISSELine fails the reading, a port limit.
//
// A read that fails ends the reading where the SDK's does: the LineDecoder is
// flushed only at the body's end, so a line still waiting for its ending is
// dropped. A cancelled request ends the stream rather than failing it, as the
// SDK's Stream swallows the AbortError its body read throws (`if
// (isAbortError(e)) return;`) and pi's adapter carries on to its post-loop
// checks. The SDK never looks at the signal between events, only through that
// read, so what has already been read is still dispatched; here a read that
// fails once ctx is done is the abort. The adapters' own ctx checks then decide
// the message.
func readOpenAISSE(body io.Reader, ctx context.Context, dispatch func(openaiSSEEvent) error) error {
	chunks := &openaiSSEChunkReader{body: body}
	scanner := bufio.NewScanner(chunks)
	scanner.Buffer(make([]byte, 0, 64*1024), maxOpenAISSELine)
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		return scanOpenAISSELines(data, atEOF && chunks.err == io.EOF)
	})
	var event string
	var data []string
	for scanner.Scan() {
		line := strings.TrimPrefix(jstext.DecodeUTF8(scanner.Bytes()), utf8BOM)
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
	if err := scanner.Err(); err != nil && (ctx == nil || ctx.Err() == nil) {
		if errors.Is(err, bufio.ErrTooLong) {
			return fmt.Errorf("an openai stream line is longer than the port's %d MiB limit (pi reads a line of any length); this is a port limit, report it with the provider and model: %w", maxOpenAISSELine>>20, err)
		}
		return err
	}
	return nil
}

// maxOpenAISSELine is the longest line readOpenAISSE reads. The SDK's
// LineDecoder has no limit; a longer line fails the stream with the port's
// own error.
const maxOpenAISSELine = 16 << 20

// openaiSSEChunkReader is the SDK's iterSSEChunks: it passes the body on only
// up to the end of its last event separator ("\n\n", "\r\r" or "\r\n\r\n",
// findDoubleNewlineIndex), holding the rest back until more arrives or the
// body ends. What is held back when a read fails is lost, as it is in the SDK.
type openaiSSEChunkReader struct {
	body io.Reader
	// buf is the read-ahead: buf[:ready] is passed on, buf[ready:] is held.
	buf   []byte
	ready int
	// err is the read error that ended the body; io.EOF at its end.
	err error
	// emptyReads counts the body's reads in a row that returned nothing.
	emptyReads int
}

// maxEmptyBodyReads is how many reads in a row may return no data and no
// error before the body is taken for broken, bufio.Scanner's own limit.
const maxEmptyBodyReads = 100

// errOpenAIBodyNoProgress fails a body whose reads keep returning nothing,
// which io.Reader's contract discourages; the SDK would wait on it forever.
var errOpenAIBodyNoProgress = fmt.Errorf("the openai response body returned no data and no error %d reads in a row; this is a port guard against a broken body reader, report it with the provider: %w", maxEmptyBodyReads, io.ErrNoProgress)

func (c *openaiSSEChunkReader) Read(p []byte) (int, error) {
	for c.ready == 0 {
		if c.err != nil {
			return 0, c.err
		}
		if cap(c.buf)-len(c.buf) < 4096 {
			c.buf = append(c.buf, make([]byte, 32*1024)...)[:len(c.buf)]
		}
		scanned := max(0, len(c.buf)-3) // a separator ends past what was scanned
		n, err := c.body.Read(c.buf[len(c.buf):cap(c.buf)])
		c.buf = c.buf[:len(c.buf)+n]
		if n > 0 || err != nil {
			c.emptyReads = 0
		} else if c.emptyReads++; c.emptyReads >= maxEmptyBodyReads {
			err = errOpenAIBodyNoProgress
		}
		for at := scanned; ; {
			end := openaiSSEChunkEnd(c.buf[at:])
			if end < 0 {
				break
			}
			at += end
			c.ready = at
		}
		if err != nil {
			c.err = err
			if err == io.EOF {
				c.ready = len(c.buf)
			} else {
				c.buf = c.buf[:c.ready]
			}
		}
	}
	n := copy(p, c.buf[:c.ready])
	c.buf = c.buf[:copy(c.buf, c.buf[n:])]
	c.ready -= n
	return n, nil
}

// openaiSSEChunkEnd is the SDK's findDoubleNewlineIndex: the index just past
// the first "\n\n", "\r\r" or "\r\n\r\n" in b, or -1.
func openaiSSEChunkEnd(b []byte) int {
	for i := 0; i+1 < len(b); i++ {
		switch {
		case b[i] == '\n' && b[i+1] == '\n', b[i] == '\r' && b[i+1] == '\r':
			return i + 2
		case b[i] == '\r' && b[i+1] == '\n' && i+3 < len(b) && b[i+2] == '\r' && b[i+3] == '\n':
			return i + 4
		}
	}
	return -1
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

// openaiStreamItem is one item the SDK's Stream yields.
type openaiStreamItem struct {
	// value is what JSON.parse made of the item, in ai.DecodeOrderedValue's
	// shapes: the value pi's loops read, with jsvalue.go's JavaScript
	// semantics, and the value OnProviderStreamEvent receives.
	value any
	// text is the item's JSON text.
	text []byte
}

// iterateOpenAIStream is the SDK's Stream.fromSSEResponse iterator over
// readOpenAISSE: it hands yield each item the SDK yields, decoded once.
//
//   - An event whose data starts with "[DONE]" ends the stream, and every
//     event after it is ignored. The body is still read to its end, so a read
//     failure after it still surfaces (an aborted one ends the stream; see
//     readOpenAISSE).
//   - Data is JSON.parse'd (openaiStreamJSON). Data it rejects throws its
//     SyntaxError, which ends the stream with V8's message, as pi's adapters
//     surface it; nothing is repaired, and the event is not yielded.
//   - An event named "thread.*" is yielded as {"event": name, "data": value}.
//   - Any other event whose value has a truthy `error` member throws the SDK's
//     APIError, as *openaiStreamChunkError, instead of being yielded — unless
//     its message names FetchRequestCanceledException. The Stream's catch
//     swallows every error isAbortError matches (`if (isAbortError(e))
//     return;`), and that test also matches such a message (Expo fetch's
//     cancellation), so the stream just ends there, unread past that event,
//     and pi's adapter runs its post-loop checks.
func iterateOpenAIStream(body io.Reader, ctx context.Context, yield func(openaiStreamItem) error) error {
	done := false
	err := readOpenAISSE(body, ctx, func(sse openaiSSEEvent) error {
		if done {
			return nil
		}
		if strings.HasPrefix(sse.data, "[DONE]") {
			done = true
			return nil
		}
		text := []byte(sse.data)
		value, err := openaiStreamJSON(text)
		if err != nil {
			return err
		}
		if strings.HasPrefix(sse.event, "thread.") {
			name, _ := json.Marshal(sse.event)
			return yield(openaiStreamItem{
				value: ai.OrderedObject{{Key: "event", Value: sse.event}, {Key: "data", Value: value}},
				text:  bytes.Join([][]byte{[]byte(`{"event":`), name, []byte(`,"data":`), text, []byte(`}`)}, nil),
			})
		}
		if errorValue := jsGet(value, "error"); jsTruthy(errorValue) {
			chunkErr := newOpenAIStreamChunkError(text, errorValue)
			if strings.Contains(chunkErr.message, "FetchRequestCanceledException") {
				return errOpenAIStreamSwallowed
			}
			return chunkErr
		}
		return yield(openaiStreamItem{value: value, text: text})
	})
	if errors.Is(err, errOpenAIStreamSwallowed) {
		return nil
	}
	return err
}

// errOpenAIStreamSwallowed ends iterateOpenAIStream's reading where the SDK's
// Stream swallows an error isAbortError matches.
var errOpenAIStreamSwallowed = errors.New("the openai SDK's Stream swallowed an error it takes for an abort")

// openaiStreamJSON is the SDK's `JSON.parse(sse.data)`: the value JSON.parse
// returns (ai.DecodeOrderedValue), else the SyntaxError it throws.
//
// Past encoding/json's nesting limit the port cannot read an event whichever
// way JSON.parse goes, so such data fails with the port's own error rather
// than V8's message: the decoder and jstext.JSONSyntaxError recurse once per
// level, and a line of the 16 MiB the reader allows would overflow the
// goroutine stack.
func openaiStreamJSON(data []byte) (any, error) {
	if jsonNestsDeeperThan(data, maxJSONNesting) {
		return nil, fmt.Errorf("an openai stream event nests deeper than %d levels, which the port's JSON decoder cannot read; this is a port limit, report it with the event", maxJSONNesting)
	}
	if value, err := ai.DecodeOrderedValue(data); err == nil {
		return value, nil
	}
	if err := jstext.JSONSyntaxError(string(data)); err != nil {
		return nil, err
	}
	return nil, errors.New("encoding/json rejects an openai stream event that JSON.parse accepts; this is a port bug, report it with the event")
}

// maxJSONNesting is encoding/json's nesting limit (its scanner's
// maxNestingDepth).
const maxJSONNesting = 10000

// jsonNestsDeeperThan reports whether b opens more than limit arrays and
// objects inside one another, counting only brackets outside strings.
func jsonNestsDeeperThan(b []byte, limit int) bool {
	depth, inString, escaped := 0, false, false
	for _, c := range b {
		switch {
		case escaped:
			escaped = false
		case inString:
			escaped = c == '\\'
			inString = c != '"'
		case c == '"':
			inString = true
		case c == '[' || c == '{':
			if depth++; depth > limit {
				return true
			}
		case c == ']' || c == '}':
			depth--
		}
	}
	return false
}

// openaiStreamChunkError is the openai SDK's APIError for a stream item
// carrying an error: `new APIError(undefined, data.error, undefined, headers)`.
// Its message is makeMessage's with no status, which is also what pi's
// adapters surface, since formatProviderError adds nothing without one.
type openaiStreamChunkError struct {
	message string
	// value is data.error, the APIError's `error`.
	value any
}

// newOpenAIStreamChunkError is the APIError for the item text whose `error`
// member, value, is truthy. makeMessage reads the member's JSON text through
// openaiSDKErrorDetail, which the error-body path shares.
func newOpenAIStreamChunkError(text []byte, value any) *openaiStreamChunkError {
	var members map[string]json.RawMessage
	_ = json.Unmarshal(text, &members) // an object JSON.parse accepted; the last "error" wins, as in JS
	message := openaiSDKErrorDetail(members["error"])
	if message == "" {
		message = "(no status code or body)"
	}
	return &openaiStreamChunkError{message: message, value: value}
}

func (e *openaiStreamChunkError) Error() string { return e.message }

// metadataRaw is pi's `(error as any)?.error?.metadata?.raw` for this error,
// as the String() the completions catch block appends: ok is false unless raw
// is truthy. A raw String() would throw on (an object with its own toString
// member) is not appended; pi's catch block itself throws there and never
// ends the stream, which the port does not reproduce.
func (e *openaiStreamChunkError) metadataRaw() (string, bool) {
	raw := jsGet(jsGet(e.value, "metadata"), "raw")
	if !jsTruthy(raw) {
		return "", false
	}
	text, err := jsToString(raw)
	return text, err == nil
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
