package providers

// pi-messages API implementation.
//
// Streams pi's own message protocol directly to a backend: the request is a
// single POST of `{ model, context, options }` to `<baseUrl>/messages`, the
// response is an SSE stream of serialized assistant-message events plus a
// terminal `done`/`error` event. This is the wire protocol spoken by the Radius
// gateway, but any backend implementing it can be used, e.g. via a models.json
// custom provider with `"api": "pi-messages"`.
//
// Port of packages/ai/src/api/pi-messages.ts (upstream 961fa6c1).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/sky-valley/pi/ai"
	"github.com/sky-valley/pi/internal/jstext"
)

// PiMessagesOptions are provider-native options for the pi-messages stream.
type PiMessagesOptions struct {
	ai.StreamOptions
	// Reasoning is the unified thinking level forwarded to the backend.
	Reasoning ai.ThinkingLevel
	// ToolChoice is "auto"|"none"|"required" or a
	// {type:"function",function:{name}} object (pi's union). Any non-nil value is
	// serialized verbatim into the request options; nil omits the field.
	ToolChoice any
	// Debug asks the backend for debug metadata (e.g. routing response headers).
	Debug bool
}

// piMessagesEvent is a serialized assistant-message event as sent by a
// pi-messages backend, read the way pi's createEventConverter reads the
// event's properties (piMessagesEventOf). It is the flattened union of every
// event variant; the converter reads only the fields its `type` implies. Port
// of PiMessagesEvent.
type piMessagesEvent struct {
	Type         string
	ContentIndex int
	// Delta is the delta the pushed event carries: the event's own when it is
	// a string. delta is the raw value, whose String() pi's `+=` appends (5 as
	// "5", an absent delta as "undefined").
	Delta     string
	delta     json.RawMessage
	Content   string
	Signature *string
	Redacted  bool
	ID        string
	ToolName  string
	ToolCall  *ai.ToolCall
	Reason    ai.StopReason
	Usage     *ai.Usage
	// ResponseID is the backend's response id.
	ResponseID string
	// ProviderThinkingLevel is the effort the upstream provider actually ran the
	// turn at, forwarded so the next request can replay it. A pointer because pi
	// only copies it when the field is present: an absent one leaves the
	// message's own level standing rather than blanking it.
	ProviderThinkingLevel *string
	ErrorMessage          string
	// Rewrite is the details of the pi_messages_rewrite diagnostic, nil when
	// the event carries no rewrite (see piMessagesRewriteDetails).
	Rewrite map[string]any
}

// piMessagesEventOf reads an object frame's members as pi's converter reads
// the event's properties: each by its exact name and on its own, so a member
// of an unexpected JSON type never drops the frame — pi converts the event
// whatever its members hold. A member pi stores as-is into a field Go types as
// string, bool or int reads as absent unless it is that type (a count accepts
// any finite number: the 10.0 a non-JavaScript backend writes is 10).
func piMessagesEventOf(o rawObject) piMessagesEvent {
	ev := piMessagesEvent{delta: o["delta"]}
	ev.Type, _ = rawString(o["type"])
	// A contentIndex that addresses no slot (absent, a string, fractional,
	// negative) reads as 0, the port's reading of an absent one.
	ev.ContentIndex, _ = rawArrayIndex(o["contentIndex"])
	ev.Delta, _ = rawString(o["delta"])
	ev.Content, _ = rawString(o["content"])
	if s, ok := rawString(o["contentSignature"]); ok {
		ev.Signature = &s
	}
	ev.Redacted = jsonValueKind(o["redacted"]) == 't'
	ev.ID, _ = rawString(o["id"])
	ev.ToolName, _ = rawString(o["toolName"])
	if jsonValueKind(o["toolCall"]) == '{' {
		var tc ai.ToolCall
		if json.Unmarshal(o["toolCall"], &tc) == nil {
			ev.ToolCall = &tc
		}
	}
	if reason, ok := rawString(o["reason"]); ok {
		ev.Reason = ai.StopReason(reason)
	}
	ev.Usage = piMessagesUsage(o["usage"])
	ev.ResponseID, _ = rawString(o["responseId"])
	if level, ok := rawString(o["providerThinkingLevel"]); ok {
		ev.ProviderThinkingLevel = &level
	}
	ev.ErrorMessage, _ = rawString(o["errorMessage"])
	ev.Rewrite = piMessagesRewriteDetails(o["rewrite"])
	return ev
}

// piMessagesUsage reads the usage a terminal event carries, which pi assigns
// to the message whole: its counts as finite numbers (rawCount) and its cost
// as finite numbers; nil when the event has no usage object.
func piMessagesUsage(raw json.RawMessage) *ai.Usage {
	o := rawOptional(raw)
	if o == nil {
		return nil
	}
	var u ai.Usage
	for key, dst := range map[string]*int{
		"input": &u.Input, "output": &u.Output, "cacheRead": &u.CacheRead, "cacheWrite": &u.CacheWrite,
		"cacheWrite1h": &u.CacheWrite1h, "reasoning": &u.Reasoning, "totalTokens": &u.TotalTokens,
	} {
		*dst, _ = rawCount(o[key])
	}
	cost := rawOptional(o["cost"])
	for key, dst := range map[string]*float64{
		"input": &u.Cost.Input, "output": &u.Cost.Output, "cacheRead": &u.Cost.CacheRead,
		"cacheWrite": &u.Cost.CacheWrite, "total": &u.Cost.Total,
	} {
		if f, ok := rawNumber(cost[key]); ok && !math.IsInf(f, 0) {
			*dst = f
		}
	}
	return &u
}

// piMessagesRewriteDetails is pi appendRewriteDiagnostic's `if (!rewrite)`
// and `{ ...rewrite }`: nil for a falsy rewrite (no diagnostic), else what the
// spread copies — an object's members, whatever they are (numbers as
// json.Number, jstext.Parse's form); an array's elements under their indices;
// a string's UTF-16 code units under theirs (a surrogate half, which a Go
// string cannot hold, as U+FFFD); nothing from a number or true.
func piMessagesRewriteDetails(raw json.RawMessage) map[string]any {
	if !rawTruthy(raw) {
		return nil
	}
	details := map[string]any{}
	value, err := jstext.Parse(raw)
	if err != nil {
		return details // unreachable: raw is a member of a decoded document
	}
	switch v := value.(type) {
	case map[string]any:
		return v
	case []any:
		for i, e := range v {
			details[strconv.Itoa(i)] = e
		}
	case string:
		for i, unit := range utf16.Encode([]rune(v)) {
			details[strconv.Itoa(i)] = string(rune(unit))
		}
	}
	return details
}

// piMessagesResponseError is a non-2xx HTTP failure carrying redacted diagnostic
// details. Port of PiMessagesResponseError.
type piMessagesResponseError struct {
	message           string
	code              string
	diagnosticDetails map[string]any
}

func (e *piMessagesResponseError) Error() string { return e.message }

// parsePiMessagesErrorBody parses a JSON error body, returning the nested
// `error` object only when the top level and its `error` are both JSON objects
// (pi's isRecord && isRecord(error) guard). Port of parsePiMessagesErrorBody.
func parsePiMessagesErrorBody(body string) (map[string]any, bool) {
	var top map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &top) != nil {
		return nil, false
	}
	raw, ok := top["error"]
	if !ok {
		return nil, false
	}
	var errObj map[string]any
	if json.Unmarshal(raw, &errObj) != nil || errObj == nil {
		return nil, false
	}
	return errObj, true
}

// truncateDiagnosticString caps a raw body at 8192 UTF-16 code units, appending
// "…" when truncated (pi's value.length / value.slice(0, 8192)). The cut lands
// on a rune boundary; a surrogate pair straddling exactly 8192 is kept whole
// rather than split into a lone surrogate (a benign adaptation of JS slicing).
func truncateDiagnosticString(value string) string {
	const maxLength = 8192
	if utf16Length(value) <= maxLength {
		return value
	}
	units := 0
	for i, r := range value {
		w := 1
		if r > 0xFFFF {
			w = 2
		}
		if units+w > maxLength {
			return value[:i] + "…"
		}
		units += w
	}
	return value + "…"
}

// formatPiMessagesResponseError builds "<status> <statusText>: <message or
// body><(code)>". Go has no Response.statusText, so http.StatusText(status)
// stands in. Port of formatPiMessagesResponseError.
func formatPiMessagesResponseError(status int, body string, errObj map[string]any) string {
	suffix := body
	if msg, ok := errObj["message"].(string); ok {
		suffix = msg
	}
	codeSuffix := ""
	if code, ok := errObj["code"].(string); ok && code != "" {
		codeSuffix = fmt.Sprintf(" (%s)", code)
	}
	return fmt.Sprintf("%d %s: %s%s", status, http.StatusText(status), suffix, codeSuffix)
}

// createPiMessagesResponseError builds the error + its diagnostic details from a
// non-2xx response. Port of createPiMessagesResponseError.
func createPiMessagesResponseError(model *ai.Model, url string, status int, body string) *piMessagesResponseError {
	errObj, hasErr := parsePiMessagesErrorBody(body)
	code := ""
	if hasErr {
		if c, ok := errObj["code"].(string); ok {
			code = c
		}
	}
	details := map[string]any{
		"version":     1,
		"provider":    model.Provider,
		"model":       model.ID,
		"url":         url,
		"status":      status,
		"statusText":  http.StatusText(status),
		"timestampMs": nowMillis(),
	}
	// pi sets error:errorBody?.error and body:errorBody?undefined:truncated —
	// exactly one is present on the wire; the other is dropped as undefined.
	if hasErr {
		details["error"] = errObj
	} else {
		details["body"] = truncateDiagnosticString(body)
	}
	return &piMessagesResponseError{
		message:           formatPiMessagesResponseError(status, body, errObj),
		code:              code,
		diagnosticDetails: details,
	}
}

// appendPiMessagesRewriteDiagnostic mirrors pi appendRewriteDiagnostic: attach a
// "pi_messages_rewrite" diagnostic whose details are what the event's rewrite
// spreads into (piMessagesRewriteDetails); nil details attach nothing.
func appendPiMessagesRewriteDiagnostic(msg *ai.AssistantMessage, details map[string]any) {
	if details == nil {
		return
	}
	msg.Diagnostics = append(msg.Diagnostics, ai.Diagnostic{
		Type:      "pi_messages_rewrite",
		Timestamp: nowMillis(),
		Details:   details,
	})
}

// piMessagesConverter maintains a partial assistant message across the SSE
// stream, converting each backend event into a unified AssistantMessageEvent.
// Port of createEventConverter (1:1 on event semantics).
type piMessagesConverter struct {
	model    *ai.Model
	partial  *ai.AssistantMessage
	toolJSON map[int]string
}

func newPiMessagesConverter(model *ai.Model) *piMessagesConverter {
	return &piMessagesConverter{
		model: model,
		partial: &ai.AssistantMessage{
			Content:    ai.ContentList{},
			Api:        model.Api,
			Provider:   model.Provider,
			Model:      model.ID,
			Usage:      ai.Usage{},
			StopReason: ai.StopPending,
			Timestamp:  nowMillis(),
		},
		toolJSON: map[int]string{},
	}
}

// ensureContent grows the partial content slice so index i is addressable,
// mirroring JS array assignment past the current length (holes become nil).
func (c *piMessagesConverter) ensureContent(i int) {
	for len(c.partial.Content) <= i {
		c.partial.Content = append(c.partial.Content, nil)
	}
}

// convert applies one event to the partial message and returns the event to
// push. Its error is the TypeError pi's `+=` throws on a delta with no string
// form (see rawToString), which fails the stream.
func (c *piMessagesConverter) convert(ev piMessagesEvent) (ai.AssistantMessageEvent, error) {
	idx := ev.ContentIndex
	switch ev.Type {
	case "done":
		c.partial.StopReason = ev.Reason
		if ev.Usage != nil {
			c.partial.Usage = *ev.Usage
		}
		c.partial.ResponseID = ev.ResponseID
		if ev.ProviderThinkingLevel != nil {
			c.partial.ProviderThinkingLevel = *ev.ProviderThinkingLevel
		}
		appendPiMessagesRewriteDiagnostic(c.partial, ev.Rewrite)
		return ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ev.Reason, Message: c.partial}, nil
	case "error":
		c.partial.StopReason = ev.Reason
		if ev.Usage != nil {
			c.partial.Usage = *ev.Usage
		}
		c.partial.ErrorMessage = ev.ErrorMessage
		c.partial.ResponseID = ev.ResponseID
		if ev.ProviderThinkingLevel != nil {
			c.partial.ProviderThinkingLevel = *ev.ProviderThinkingLevel
		}
		appendPiMessagesRewriteDiagnostic(c.partial, ev.Rewrite)
		return ai.AssistantMessageEvent{Type: ai.EventError, Reason: ev.Reason, Error: c.partial}, nil
	case "start":
		return ai.AssistantMessageEvent{Type: ai.EventStart, Partial: c.partial.Clone()}, nil
	case "text_start":
		c.ensureContent(idx)
		c.partial.Content[idx] = ai.TextContent{Text: ""}
		return ai.AssistantMessageEvent{Type: ai.EventTextStart, ContentIndex: idx, Partial: c.partial.Clone()}, nil
	case "text_delta":
		if tc, ok := c.partial.Content[idx].(ai.TextContent); ok {
			piece, err := rawToString(ev.delta)
			if err != nil {
				return ai.AssistantMessageEvent{}, err
			}
			tc.Text += piece
			c.partial.Content[idx] = tc
		}
		return ai.AssistantMessageEvent{Type: ai.EventTextDelta, ContentIndex: idx, Delta: ev.Delta, Partial: c.partial.Clone()}, nil
	case "text_end":
		if tc, ok := c.partial.Content[idx].(ai.TextContent); ok {
			tc.Text = ev.Content
			if ev.Signature != nil {
				tc.TextSignature = *ev.Signature
			}
			c.partial.Content[idx] = tc
		}
		return ai.AssistantMessageEvent{Type: ai.EventTextEnd, ContentIndex: idx, Content: ev.Content, Partial: c.partial.Clone()}, nil
	case "thinking_start":
		c.ensureContent(idx)
		c.partial.Content[idx] = ai.ThinkingContent{Thinking: ""}
		return ai.AssistantMessageEvent{Type: ai.EventThinkingStart, ContentIndex: idx, Partial: c.partial.Clone()}, nil
	case "thinking_delta":
		if tc, ok := c.partial.Content[idx].(ai.ThinkingContent); ok {
			piece, err := rawToString(ev.delta)
			if err != nil {
				return ai.AssistantMessageEvent{}, err
			}
			tc.Thinking += piece
			c.partial.Content[idx] = tc
		}
		return ai.AssistantMessageEvent{Type: ai.EventThinkingDelta, ContentIndex: idx, Delta: ev.Delta, Partial: c.partial.Clone()}, nil
	case "thinking_end":
		if tc, ok := c.partial.Content[idx].(ai.ThinkingContent); ok {
			tc.Thinking = ev.Content
			if ev.Signature != nil {
				tc.ThinkingSignature = *ev.Signature
			}
			tc.Redacted = ev.Redacted
			c.partial.Content[idx] = tc
		}
		return ai.AssistantMessageEvent{Type: ai.EventThinkingEnd, ContentIndex: idx, Content: ev.Content, Partial: c.partial.Clone()}, nil
	case "toolcall_start":
		c.ensureContent(idx)
		c.partial.Content[idx] = ai.ToolCall{ID: ev.ID, Name: ev.ToolName, Arguments: map[string]any{}}
		c.toolJSON[idx] = ""
		return ai.AssistantMessageEvent{Type: ai.EventToolCallStart, ContentIndex: idx, Partial: c.partial.Clone()}, nil
	case "toolcall_delta":
		piece, err := rawToString(ev.delta)
		if err != nil {
			return ai.AssistantMessageEvent{}, err
		}
		j := c.toolJSON[idx] + piece
		c.toolJSON[idx] = j
		if tc, ok := c.partial.Content[idx].(ai.ToolCall); ok {
			tc.Arguments, tc.ArgumentsOrder = parseStreamingJSON(j)
			c.partial.Content[idx] = tc
		}
		return ai.AssistantMessageEvent{Type: ai.EventToolCallDelta, ContentIndex: idx, Delta: ev.Delta, Partial: c.partial.Clone()}, nil
	case "toolcall_end":
		if ev.ToolCall != nil {
			c.ensureContent(idx)
			c.partial.Content[idx] = *ev.ToolCall
		}
		delete(c.toolJSON, idx)
		var tc *ai.ToolCall
		if v, ok := c.partial.Content[idx].(ai.ToolCall); ok {
			cp := v
			tc = &cp
		}
		return ai.AssistantMessageEvent{Type: ai.EventToolCallEnd, ContentIndex: idx, ToolCall: tc, Partial: c.partial.Clone()}, nil
	}
	// Unknown event type: emit nothing meaningful (pi returns {...event,partial}
	// for the exhaustive-known set; an unmodeled type has no unified analogue).
	return ai.AssistantMessageEvent{Type: ai.EventType(ev.Type), Partial: c.partial.Clone()}, nil
}

// piMessagesFrameData is pi parsePiMessagesEvent's text: the first `data:`
// line of a frame, JS-trimmed. ok is false when the frame has none, or when it
// is empty or `[DONE]`, which pi passes over without parsing.
func piMessagesFrameData(frame string) (string, bool) {
	for _, line := range strings.Split(frame, "\n") {
		if strings.HasPrefix(line, "data:") {
			data := jstext.Trim(line[5:])
			return data, data != "" && data != "[DONE]"
		}
	}
	return "", false
}

// decodePiMessagesEvent is pi's JSON.parse of a frame's data plus
// readPiMessagesEvents' `if (event)`: yielded is false for a JS-falsy value
// (null, false, 0, -0, "" — and 1e-400, which parses to 0). An object's members
// are read into the typed event; any other truthy value converts as an event
// with no type, as pi's `{...event, partial}` of it has none. err is the
// syntax error JSON.parse throws on data that is not JSON, which fails the
// stream.
//
// data is decoded once: an object into its members, which validates it on the
// way, anything else only validated. The value an observer receives is a
// separate decode (readPiMessagesEvents), made only when there is one.
func decodePiMessagesEvent(data string) (ev piMessagesEvent, yielded bool, err error) {
	if jsonValueKind(data) == '{' {
		o, err := decodeRawObject([]byte(data))
		if err != nil {
			return piMessagesEvent{}, false, err
		}
		return piMessagesEventOf(o), true, nil
	}
	if !json.Valid([]byte(data)) {
		var v any
		return piMessagesEvent{}, false, json.Unmarshal([]byte(data), &v)
	}
	return piMessagesEvent{}, rawTruthy(json.RawMessage(data)), nil
}

// readPiMessagesEvents consumes the SSE body: frames are separated by "\n\n",
// CRLF is normalized to "\n", and a trailing non-terminal buffer is flushed.
// handle returns false to stop early (a terminal event was seen), and its
// error ends the read. A frame that is not JSON fails the read, as pi's
// JSON.parse throw does. Port of readPiMessagesEvents.
//
// onEvent, when non-nil, observes every yielded frame's parsed value — objects
// as ai.OrderedObject, unknown fields and the terminal done/error included —
// before it is converted (pi's onProviderStreamEvent, upstream 002fc8385); its
// error ends the read. The value is a decode of its own, made only when
// onEvent is non-nil, so observing costs nothing when unset and a mutating
// observer cannot change what is converted.
func readPiMessagesEvents(body io.Reader, ctx context.Context, onEvent func(any) error, handle func(piMessagesEvent) (bool, error)) error {
	// emit handles one frame and reports whether to keep reading.
	emit := func(frame string) (bool, error) {
		data, ok := piMessagesFrameData(frame)
		if !ok {
			return true, nil
		}
		ev, yielded, err := decodePiMessagesEvent(data)
		if err != nil {
			return false, err
		}
		if !yielded {
			return true, nil
		}
		if onEvent != nil {
			value, err := ai.DecodeOrderedValue([]byte(data))
			if err != nil {
				return false, err // unreachable: data parsed above
			}
			if err := onEvent(value); err != nil {
				return false, err
			}
		}
		return handle(ev)
	}
	buf := make([]byte, 32*1024)
	var pending string
	for {
		if ctx != nil && ctx.Err() != nil {
			return fmt.Errorf("Request was aborted")
		}
		n, readErr := body.Read(buf)
		if n > 0 {
			// Normalize the whole accumulated buffer (pi: buffer.replace(/\r\n/g,
			// "\n") each read), so a "\r\n" split across two reads still collapses.
			// pending holds only an unframed remainder, so the re-scan stays cheap.
			pending = strings.ReplaceAll(pending+string(buf[:n]), "\r\n", "\n")
			for {
				split := strings.Index(pending, "\n\n")
				if split == -1 {
					break
				}
				frame := pending[:split]
				pending = pending[split+2:]
				more, err := emit(frame)
				if err != nil {
					return err
				}
				if !more {
					return nil
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if jstext.Trim(pending) != "" {
		if _, err := emit(pending); err != nil {
			return err
		}
	}
	return nil
}

// createPiMessagesErrorEvent builds the terminal error event for a thrown
// failure, attaching the response-failure diagnostic for non-aborted
// PiMessagesResponseErrors. Port of createErrorEvent.
func createPiMessagesErrorEvent(model *ai.Model, err error, aborted bool) ai.AssistantMessageEvent {
	reason := ai.StopError
	if aborted {
		reason = ai.StopAborted
	}
	msg := &ai.AssistantMessage{
		Content:      ai.ContentList{},
		Api:          model.Api,
		Provider:     model.Provider,
		Model:        model.ID,
		Usage:        ai.Usage{},
		StopReason:   reason,
		ErrorMessage: err.Error(),
		Timestamp:    nowMillis(),
	}
	if !aborted {
		if re, ok := err.(*piMessagesResponseError); ok {
			msg.Diagnostics = append(msg.Diagnostics, ai.Diagnostic{
				Type:      "pi_messages_response_failure",
				Timestamp: nowMillis(),
				Error:     &ai.DiagnosticErrorInfo{Name: "PiMessagesResponseError", Message: re.message, Code: piMessagesErrorCode(re.code)},
				Details:   re.diagnosticDetails,
			})
		}
	}
	return ai.AssistantMessageEvent{Type: ai.EventError, Reason: reason, Error: msg}
}

// piMessagesErrorCode returns the code as any (nil when empty), matching pi's
// optional code field (dropped when undefined).
func piMessagesErrorCode(code string) any {
	if code == "" {
		return nil
	}
	return code
}

// resolvePiMessagesCacheRetention mirrors pi resolveCacheRetention: an explicit
// retention wins; otherwise the legacy PI_CACHE_RETENTION=long env opt-in maps
// to "long"; else backend defaults apply (empty).
func resolvePiMessagesCacheRetention(cacheRetention ai.CacheRetention, env map[string]string) ai.CacheRetention {
	if cacheRetention != "" {
		return cacheRetention
	}
	if getProviderEnvValue("PI_CACHE_RETENTION", env) == "long" {
		return ai.CacheLong
	}
	return ""
}

// StreamPiMessages streams from a pi-messages backend. Port of the `stream`
// export in pi-messages.ts.
func StreamPiMessages(ctx context.Context, model *ai.Model, req ai.TranscriptContext, opts *PiMessagesOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	if opts == nil {
		opts = &PiMessagesOptions{}
	}
	conv := newPiMessagesConverter(model)

	go func() {
		aborted := func() bool { return ctx != nil && ctx.Err() != nil }
		fail := func(err error) {
			stream.Push(createPiMessagesErrorEvent(model, err, aborted()))
			stream.End()
		}
		// Mirror pi's streaming-block try/catch: any panic — e.g. a
		// non-conformant backend that sends a *_delta/*_end for a contentIndex it
		// never started — becomes a terminal error event, exactly as pi's throw is
		// caught into createErrorEvent, rather than crashing the host process.
		defer func() {
			if r := recover(); r != nil {
				fail(fmt.Errorf("%v", r))
			}
		}()

		apiKey := opts.APIKey
		if apiKey == "" {
			fail(fmt.Errorf("No API key provided for provider %q", model.Provider))
			return
		}

		url := strings.TrimRight(model.BaseURL, "/") + "/messages"
		if opts.Debug {
			url += "?debug=1"
		}

		requestOptions := map[string]any{}
		if opts.Temperature != nil {
			requestOptions["temperature"] = *opts.Temperature
		}
		if opts.MaxTokens != nil {
			requestOptions["maxTokens"] = *opts.MaxTokens
		}
		if opts.Reasoning != "" {
			requestOptions["reasoning"] = opts.Reasoning
		}
		if cr := resolvePiMessagesCacheRetention(opts.CacheRetention, opts.Env); cr != "" {
			requestOptions["cacheRetention"] = cr
		}
		if opts.SessionID != "" {
			requestOptions["sessionId"] = opts.SessionID
		}
		if opts.ToolChoice != nil {
			requestOptions["toolChoice"] = opts.ToolChoice
		}

		var payload any = map[string]any{
			"model":   model.ID,
			"context": req,
			"options": requestOptions,
		}
		if opts.OnPayload != nil {
			next, perr := opts.OnPayload(payload, model)
			if perr != nil {
				fail(perr)
				return
			}
			if next != nil {
				payload = next
			}
		}
		body, err := json.Marshal(payload)
		if err != nil {
			fail(err)
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			fail(err)
			return
		}
		httpReq.Header.Set("authorization", "Bearer "+apiKey)
		httpReq.Header.Set("accept", "text/event-stream")
		httpReq.Header.Set("content-type", "application/json")
		// pi merges only providerHeadersToRecord(options.headers) — no attribution
		// bundle, no model.headers — after the three fixed headers. The record
		// conversion drops deletion markers instead of deleting: pi spreads it
		// into an object literal that already holds the fixed headers, so a
		// marker cannot unset the authorization this adapter just wrote.
		// headerObject carries the merge order so a consumer map holding two
		// spellings of one name cannot let Go's map iteration pick the winner.
		o := &headerObject{}
		o.merge(opts.Headers)
		o.applyAsRecord(httpReq.Header)

		// pi: `(options?.fetch ?? globalThis.fetch)(url, …)` — this provider calls
		// fetch directly rather than through an SDK, so the default stays
		// http.DefaultClient rather than the retry loop's shared client.
		var client ai.HTTPDoer = http.DefaultClient
		if c, ok := customHTTPClient(opts.HTTPClient); ok {
			client = c
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			fail(err)
			return
		}
		defer resp.Body.Close()

		if opts.OnResponse != nil {
			// pi calls onResponse before the response.ok check, so error responses
			// still surface their headers; an awaited rejection propagates to the
			// catch (a terminal error event), so a non-nil error fails the stream.
			if rerr := opts.OnResponse(ai.ProviderResponse{Status: resp.StatusCode, Headers: flattenHeaders(resp.Header)}, model); rerr != nil {
				fail(rerr)
				return
			}
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			data, _ := io.ReadAll(resp.Body)
			fail(createPiMessagesResponseError(model, url, resp.StatusCode, string(data)))
			return
		}

		// pi awaits options.onProviderStreamEvent(piEvent, model) before
		// convertEvent, with the model the stream was called with.
		var onEvent func(any) error
		if opts.OnProviderStreamEvent != nil {
			onEvent = func(data any) error { return opts.OnProviderStreamEvent(data, model) }
		}
		terminal := false
		perr := readPiMessagesEvents(resp.Body, ctx, onEvent, func(ev piMessagesEvent) (bool, error) {
			out, err := conv.convert(ev)
			if err != nil {
				return false, err
			}
			stream.Push(out)
			if out.Type == ai.EventDone || out.Type == ai.EventError {
				terminal = true
				return false, nil
			}
			return true, nil
		})
		if perr != nil {
			fail(perr)
			return
		}
		if terminal {
			stream.End()
			return
		}
		fail(fmt.Errorf("%s stream ended without a terminal event", model.Provider))
	}()

	return stream
}

// StreamSimplePiMessages maps SimpleStreamOptions → the full stream, forwarding
// reasoning and the unified toolChoice. Port of the `streamSimple` export;
// upstream e5dde9a76 moved toolChoice off the provider-extra object onto the
// unified options, so it no longer depends on the caller passing native options.
func StreamSimplePiMessages(ctx context.Context, model *ai.Model, req ai.TranscriptContext, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	p := &PiMessagesOptions{}
	if opts != nil {
		p.StreamOptions = opts.StreamOptions
		p.Reasoning = opts.Reasoning
		if opts.ToolChoice != "" {
			p.ToolChoice = string(opts.ToolChoice)
		}
	}
	return StreamPiMessages(ctx, model, req, p)
}

// RegisterPiMessages registers the pi-messages api provider.
func RegisterPiMessages() {
	ai.RegisterApiProvider(ai.ApiProvider{
		Api: ai.APIPiMessages,
		Stream: func(ctx context.Context, model *ai.Model, req ai.TranscriptContext, opts *ai.StreamOptions) *ai.AssistantMessageEventStream {
			p := &PiMessagesOptions{}
			if opts != nil {
				p.StreamOptions = *opts
			}
			return StreamPiMessages(ctx, model, req, p)
		},
		StreamSimple: StreamSimplePiMessages,
	})
}
