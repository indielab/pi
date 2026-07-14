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
	"net/http"
	"strings"

	"github.com/sky-valley/pi/ai"
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

// piMessagesRewriteImpact is the impact summary of a server-side message rewrite
// (e.g. a gateway policy). Port of PiMessagesRewriteImpact.
type piMessagesRewriteImpact struct {
	PolicyID            string `json:"policyId"`
	PolicyVersion       int    `json:"policyVersion"`
	Changed             bool   `json:"changed"`
	TokenCountChange    int    `json:"tokenCountChange"`
	MessageCountChange  int    `json:"messageCountChange"`
	SystemPromptChanged bool   `json:"systemPromptChanged"`
}

// piMessagesEvent is a serialized assistant-message event as sent by a
// pi-messages backend. It is the flattened union of every event variant; the
// converter reads only the fields its `type` implies. Port of PiMessagesEvent.
type piMessagesEvent struct {
	Type         string                   `json:"type"`
	ContentIndex int                      `json:"contentIndex"`
	Delta        string                   `json:"delta"`
	Content      string                   `json:"content"`
	Signature    *string                  `json:"contentSignature"`
	Redacted     bool                     `json:"redacted"`
	ID           string                   `json:"id"`
	ToolName     string                   `json:"toolName"`
	ToolCall     *ai.ToolCall             `json:"toolCall"`
	Reason       ai.StopReason            `json:"reason"`
	Usage        *ai.Usage                `json:"usage"`
	ResponseID   string                   `json:"responseId"`
	ErrorMessage string                   `json:"errorMessage"`
	Rewrite      *piMessagesRewriteImpact `json:"rewrite"`
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
// "pi_messages_rewrite" diagnostic whose details are the rewrite impact fields.
func appendPiMessagesRewriteDiagnostic(msg *ai.AssistantMessage, rewrite *piMessagesRewriteImpact) {
	if rewrite == nil {
		return
	}
	msg.Diagnostics = append(msg.Diagnostics, ai.Diagnostic{
		Type:      "pi_messages_rewrite",
		Timestamp: nowMillis(),
		Details: map[string]any{
			"policyId":            rewrite.PolicyID,
			"policyVersion":       rewrite.PolicyVersion,
			"changed":             rewrite.Changed,
			"tokenCountChange":    rewrite.TokenCountChange,
			"messageCountChange":  rewrite.MessageCountChange,
			"systemPromptChanged": rewrite.SystemPromptChanged,
		},
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
			StopReason: ai.StopStop,
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

func (c *piMessagesConverter) convert(ev piMessagesEvent) ai.AssistantMessageEvent {
	idx := ev.ContentIndex
	switch ev.Type {
	case "done":
		c.partial.StopReason = ev.Reason
		if ev.Usage != nil {
			c.partial.Usage = *ev.Usage
		}
		c.partial.ResponseID = ev.ResponseID
		appendPiMessagesRewriteDiagnostic(c.partial, ev.Rewrite)
		return ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ev.Reason, Message: c.partial}
	case "error":
		c.partial.StopReason = ev.Reason
		if ev.Usage != nil {
			c.partial.Usage = *ev.Usage
		}
		c.partial.ErrorMessage = ev.ErrorMessage
		c.partial.ResponseID = ev.ResponseID
		appendPiMessagesRewriteDiagnostic(c.partial, ev.Rewrite)
		return ai.AssistantMessageEvent{Type: ai.EventError, Reason: ev.Reason, Error: c.partial}
	case "start":
		return ai.AssistantMessageEvent{Type: ai.EventStart, Partial: c.partial.Clone()}
	case "text_start":
		c.ensureContent(idx)
		c.partial.Content[idx] = ai.TextContent{Text: ""}
		return ai.AssistantMessageEvent{Type: ai.EventTextStart, ContentIndex: idx, Partial: c.partial.Clone()}
	case "text_delta":
		if tc, ok := c.partial.Content[idx].(ai.TextContent); ok {
			tc.Text += ev.Delta
			c.partial.Content[idx] = tc
		}
		return ai.AssistantMessageEvent{Type: ai.EventTextDelta, ContentIndex: idx, Delta: ev.Delta, Partial: c.partial.Clone()}
	case "text_end":
		if tc, ok := c.partial.Content[idx].(ai.TextContent); ok {
			tc.Text = ev.Content
			if ev.Signature != nil {
				tc.TextSignature = *ev.Signature
			}
			c.partial.Content[idx] = tc
		}
		return ai.AssistantMessageEvent{Type: ai.EventTextEnd, ContentIndex: idx, Content: ev.Content, Partial: c.partial.Clone()}
	case "thinking_start":
		c.ensureContent(idx)
		c.partial.Content[idx] = ai.ThinkingContent{Thinking: ""}
		return ai.AssistantMessageEvent{Type: ai.EventThinkingStart, ContentIndex: idx, Partial: c.partial.Clone()}
	case "thinking_delta":
		if tc, ok := c.partial.Content[idx].(ai.ThinkingContent); ok {
			tc.Thinking += ev.Delta
			c.partial.Content[idx] = tc
		}
		return ai.AssistantMessageEvent{Type: ai.EventThinkingDelta, ContentIndex: idx, Delta: ev.Delta, Partial: c.partial.Clone()}
	case "thinking_end":
		if tc, ok := c.partial.Content[idx].(ai.ThinkingContent); ok {
			tc.Thinking = ev.Content
			if ev.Signature != nil {
				tc.ThinkingSignature = *ev.Signature
			}
			tc.Redacted = ev.Redacted
			c.partial.Content[idx] = tc
		}
		return ai.AssistantMessageEvent{Type: ai.EventThinkingEnd, ContentIndex: idx, Content: ev.Content, Partial: c.partial.Clone()}
	case "toolcall_start":
		c.ensureContent(idx)
		c.partial.Content[idx] = ai.ToolCall{ID: ev.ID, Name: ev.ToolName, Arguments: map[string]any{}}
		c.toolJSON[idx] = ""
		return ai.AssistantMessageEvent{Type: ai.EventToolCallStart, ContentIndex: idx, Partial: c.partial.Clone()}
	case "toolcall_delta":
		j := c.toolJSON[idx] + ev.Delta
		c.toolJSON[idx] = j
		if tc, ok := c.partial.Content[idx].(ai.ToolCall); ok {
			tc.Arguments = parseStreamingJSON(j)
			c.partial.Content[idx] = tc
		}
		return ai.AssistantMessageEvent{Type: ai.EventToolCallDelta, ContentIndex: idx, Delta: ev.Delta, Partial: c.partial.Clone()}
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
		return ai.AssistantMessageEvent{Type: ai.EventToolCallEnd, ContentIndex: idx, ToolCall: tc, Partial: c.partial.Clone()}
	}
	// Unknown event type: emit nothing meaningful (pi returns {...event,partial}
	// for the exhaustive-known set; an unmodeled type has no unified analogue).
	return ai.AssistantMessageEvent{Type: ai.EventType(ev.Type), Partial: c.partial.Clone()}
}

// parsePiMessagesFrame extracts the JSON of a single SSE frame's `data:` line,
// ignoring `[DONE]`. Returns ok=false when the frame carries no parseable event.
// Port of parsePiMessagesEvent.
func parsePiMessagesFrame(raw string) (piMessagesEvent, bool) {
	var data string
	found := false
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(line[5:])
			found = true
			break
		}
	}
	if !found || data == "" || data == "[DONE]" {
		return piMessagesEvent{}, false
	}
	var ev piMessagesEvent
	if json.Unmarshal([]byte(data), &ev) != nil {
		return piMessagesEvent{}, false
	}
	return ev, true
}

// readPiMessagesEvents consumes the SSE body: frames are separated by "\n\n",
// CRLF is normalized to "\n", and a trailing non-terminal buffer is flushed.
// handle returns false to stop early (a terminal event was seen). Port of
// readPiMessagesEvents.
func readPiMessagesEvents(body io.Reader, ctx context.Context, handle func(piMessagesEvent) bool) error {
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
				if ev, ok := parsePiMessagesFrame(frame); ok {
					if !handle(ev) {
						return nil
					}
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
	if strings.TrimSpace(pending) != "" {
		if ev, ok := parsePiMessagesFrame(pending); ok {
			handle(ev)
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
func StreamPiMessages(ctx context.Context, model *ai.Model, req ai.Context, opts *PiMessagesOptions) *ai.AssistantMessageEventStream {
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
		// bundle, no model.headers — after the three fixed headers.
		for k, v := range opts.Headers {
			httpReq.Header.Set(k, v)
		}

		resp, err := http.DefaultClient.Do(httpReq)
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

		terminal := false
		perr := readPiMessagesEvents(resp.Body, ctx, func(ev piMessagesEvent) bool {
			out := conv.convert(ev)
			stream.Push(out)
			if out.Type == ai.EventDone || out.Type == ai.EventError {
				terminal = true
				return false
			}
			return true
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
// reasoning (and toolChoice/debug when the caller passed a *PiMessagesOptions).
// Port of the `streamSimple` export.
func StreamSimplePiMessages(ctx context.Context, model *ai.Model, req ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	p := &PiMessagesOptions{}
	if opts != nil {
		p.StreamOptions = opts.StreamOptions
		p.Reasoning = opts.Reasoning
	}
	return StreamPiMessages(ctx, model, req, p)
}

// RegisterPiMessages registers the pi-messages api provider.
func RegisterPiMessages() {
	ai.RegisterApiProvider(ai.ApiProvider{
		Api: ai.APIPiMessages,
		Stream: func(ctx context.Context, model *ai.Model, req ai.Context, opts *ai.StreamOptions) *ai.AssistantMessageEventStream {
			p := &PiMessagesOptions{}
			if opts != nil {
				p.StreamOptions = *opts
			}
			return StreamPiMessages(ctx, model, req, p)
		},
		StreamSimple: StreamSimplePiMessages,
	})
}
