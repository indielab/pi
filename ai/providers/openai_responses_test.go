package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sky-valley/pi/ai"
)

const responsesSSE = `data: {"type":"response.created","response":{"id":"resp_1"}}

data: {"type":"response.output_item.added","item":{"type":"reasoning","id":"rs_1"}}

data: {"type":"response.reasoning_summary_part.added","part":{"type":"summary_text","text":""}}

data: {"type":"response.reasoning_summary_text.delta","delta":"pondering"}

data: {"type":"response.output_item.done","item":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"pondering"}]}}

data: {"type":"response.output_item.added","item":{"type":"message","id":"msg_1"}}

data: {"type":"response.content_part.added","part":{"type":"output_text","text":""}}

data: {"type":"response.output_text.delta","delta":"Answer: "}

data: {"type":"response.output_text.delta","delta":"42"}

data: {"type":"response.output_item.done","item":{"type":"message","id":"msg_1","content":[{"type":"output_text","text":"Answer: 42"}]}}

data: {"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"calc","arguments":""}}

data: {"type":"response.function_call_arguments.delta","delta":"{\"x\":1}"}

data: {"type":"response.output_item.done","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"calc","arguments":"{\"x\":1}"}}

data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":20,"output_tokens":8,"total_tokens":28,"input_tokens_details":{"cached_tokens":5}}}}

`

func TestOpenAIResponsesProviderParsesStream(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, responsesSSE)
	}))
	defer server.Close()

	model := &ai.Model{
		ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "openai", BaseURL: server.URL,
		Reasoning: true, MaxTokens: 4096, Cost: ai.ModelCost{Input: 1.25, Output: 10},
	}
	req := ai.Context{
		SystemPrompt: "be terse",
		Messages:     []ai.Message{ai.NewUserText("what is 6*7?", 1)},
		Tools:        []ai.Tool{{Name: "calc", Description: "calc", Parameters: ai.Object(ai.Prop("x", ai.Integer()))}},
	}
	final := StreamOpenAIResponses(context.Background(), model, req, &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "sk"}}, ReasoningEffort: "medium"}).Result()

	if final.StopReason != ai.StopToolUse {
		t.Fatalf("expected toolUse, got %s (%s)", final.StopReason, final.ErrorMessage)
	}
	var thinking, text string
	var tool *ai.ToolCall
	for _, c := range final.Content {
		switch v := c.(type) {
		case ai.ThinkingContent:
			thinking = v.Thinking
			if v.ThinkingSignature == "" {
				t.Errorf("reasoning signature not captured")
			}
		case ai.TextContent:
			text = v.Text
		case ai.ToolCall:
			tc := v
			tool = &tc
		}
	}
	if thinking != "pondering" {
		t.Fatalf("thinking wrong: %q", thinking)
	}
	if text != "Answer: 42" {
		t.Fatalf("text wrong: %q", text)
	}
	if tool == nil || tool.Name != "calc" || tool.ID != "call_1|fc_1" {
		t.Fatalf("tool wrong: %#v", tool)
	}
	if v, _ := tool.Arguments["x"].(float64); v != 1 {
		t.Fatalf("tool args wrong: %#v", tool.Arguments)
	}
	if final.Usage.Input != 15 || final.Usage.CacheRead != 5 || final.Usage.Output != 8 {
		t.Fatalf("usage wrong: %+v", final.Usage)
	}
	// Request must use developer role for reasoning model + input array.
	if _, ok := gotBody["input"]; !ok {
		t.Fatalf("input not sent: %v", gotBody)
	}
	if _, ok := gotBody["reasoning"]; !ok {
		t.Fatalf("reasoning param not sent: %v", gotBody)
	}
}

// Regression (found via live reasoning round-trip, 2026-06-08): with store:false,
// a reasoning request must set include:["reasoning.encrypted_content"] so the
// reasoning item can be replayed inline on the next turn without a 404.
func TestResponsesReasoningIncludesEncryptedContent(t *testing.T) {
	model := &ai.Model{ID: "gpt-5-mini", Api: ai.APIOpenAIResponses, Provider: "openai", Reasoning: true, MaxTokens: 1024}
	body := mustBuildResponsesParams(t, model, ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}},
		&OpenAIResponsesOptions{ReasoningEffort: "medium"})

	inc, ok := body["include"].([]any)
	if !ok || len(inc) != 1 || inc[0] != "reasoning.encrypted_content" {
		t.Fatalf("expected include=[reasoning.encrypted_content], got %v", body["include"])
	}
	r, _ := body["reasoning"].(map[string]any)
	if r["effort"] != "medium" || r["summary"] != "auto" {
		t.Fatalf("reasoning block wrong: %v", r)
	}
	if body["store"] != false {
		t.Fatalf("store should be false")
	}
}

// Non-reasoning requests must not send include/reasoning.
func TestResponsesNonReasoningNoInclude(t *testing.T) {
	model := &ai.Model{ID: "gpt-4o-mini", Api: ai.APIOpenAIResponses, Provider: "openai", Reasoning: false, MaxTokens: 1024}
	body := mustBuildResponsesParams(t, model, ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, &OpenAIResponsesOptions{})
	if _, ok := body["include"]; ok {
		t.Fatalf("non-reasoning model must not send include")
	}
	if _, ok := body["reasoning"]; ok {
		t.Fatalf("non-reasoning model must not send reasoning")
	}
}

// A set ToolChoice is forwarded verbatim as the tool_choice request param
// alongside the tools; unset (nil) omits it so the API defaults to "auto".
// Mirrors pi's "forwards required tool choice" case (upstream eacaa130).
func TestResponsesToolChoiceForwarded(t *testing.T) {
	model := &ai.Model{ID: "gpt-5.4", Api: ai.APIOpenAIResponses, Provider: "openai", Reasoning: true, MaxTokens: 4096}
	req := ai.Context{
		Messages: []ai.Message{ai.NewUserText("Do not call ping. Respond with text instead.", 1)},
		Tools:    []ai.Tool{{Name: "ping", Description: "Ping", Parameters: ai.Object(ai.Prop("value", ai.String()))}},
	}

	with := mustBuildResponsesParams(t, model, req, &OpenAIResponsesOptions{ToolChoice: "required"})
	if with["tool_choice"] != "required" {
		t.Fatalf("tool_choice = %v, want required", with["tool_choice"])
	}
	if names := dtResponsesToolNames(with); len(names) != 1 || names[0] != "ping" {
		t.Fatalf("tools = %v, want [ping]", names)
	}

	without := mustBuildResponsesParams(t, model, req, &OpenAIResponsesOptions{})
	if _, ok := without["tool_choice"]; ok {
		t.Fatalf("tool_choice must be omitted when ToolChoice is unset, got %v", without["tool_choice"])
	}
}

// mustResponsesInput converts messages, failing the test on conversion errors.
func mustResponsesInput(t *testing.T, model *ai.Model, req ai.Context) []any {
	t.Helper()
	// Same gate as buildResponsesParams, so a helper-driven test sees the
	// deferral the production path would.
	placement := ai.SplitDeferredTools(req, responsesDeferredToolsMode(getResponsesCompat(model)) != deferredToolsNone, nil)
	in, err := responsesInput(model, req, placement.ByName)
	if err != nil {
		t.Fatalf("responsesInput: %v", err)
	}
	return in
}

// mustBuildResponsesParams builds request params, failing the test on errors.
func mustBuildResponsesParams(t *testing.T, model *ai.Model, req ai.Context, opts *OpenAIResponsesOptions) map[string]any {
	t.Helper()
	params, err := buildResponsesParams(model, req, opts)
	if err != nil {
		t.Fatalf("buildResponsesParams: %v", err)
	}
	return params
}

// runResponsesSSE streams a raw SSE body through the provider and returns the
// final assistant message.
func runResponsesSSE(t *testing.T, model *ai.Model, req ai.Context, sse string) *ai.AssistantMessage {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, sse)
	}))
	t.Cleanup(server.Close)
	m := *model
	m.BaseURL = server.URL
	return StreamOpenAIResponses(context.Background(), &m, req,
		&OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "sk"}}}).Result()
}

func reasoningModel() *ai.Model {
	return &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "openai", Reasoning: true, MaxTokens: 4096}
}

// Multi-part reasoning summary parts must be joined by "\n\n".
func TestResponsesMultiPartReasoningSummary(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.output_item.added","item":{"type":"reasoning","id":"rs_1"}}

data: {"type":"response.reasoning_summary_part.added","part":{"type":"summary_text","text":""}}

data: {"type":"response.reasoning_summary_text.delta","delta":"first"}

data: {"type":"response.reasoning_summary_part.done"}

data: {"type":"response.reasoning_summary_part.added","part":{"type":"summary_text","text":""}}

data: {"type":"response.reasoning_summary_text.delta","delta":"second"}

data: {"type":"response.output_item.done","item":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"first"},{"type":"summary_text","text":"second"}]}}

data: {"type":"response.completed","response":{"id":"r","status":"completed"}}

`
	final := runResponsesSSE(t, reasoningModel(), ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, sse)
	var thinking string
	for _, c := range final.Content {
		if tc, ok := c.(ai.ThinkingContent); ok {
			thinking = tc.Thinking
		}
	}
	if thinking != "first\n\nsecond" {
		t.Fatalf("summary join wrong: %q", thinking)
	}
}

// Azure OpenAI can omit reasoning.encrypted_content from
// response.output_item.done and provide it only in
// response.completed.response.output. The persisted reasoning signature must be
// backfilled from the terminal response so store:false replay stays stateless
// (port of upstream 1f0dbc00, https://github.com/earendil-works/pi/issues/6409).
func TestResponsesBackfillReasoningEncryptedContent(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.output_item.added","item":{"type":"reasoning","id":"rs_1"}}

data: {"type":"response.reasoning_summary_text.delta","delta":"think"}

data: {"type":"response.output_item.done","item":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"think"}]}}

data: {"type":"response.completed","response":{"id":"r","status":"completed","output":[{"type":"reasoning","id":"rs_1","encrypted_content":"ENC-123"}]}}

`
	final := runResponsesSSE(t, reasoningModel(), ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, sse)
	var sig string
	for _, c := range final.Content {
		if tc, ok := c.(ai.ThinkingContent); ok {
			sig = tc.ThinkingSignature
		}
	}
	if sig == "" {
		t.Fatalf("no thinking signature captured")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(sig), &parsed); err != nil {
		t.Fatalf("signature is not valid JSON: %q (%v)", sig, err)
	}
	if parsed["encrypted_content"] != "ENC-123" {
		t.Fatalf("encrypted_content not backfilled onto signature: %q", sig)
	}
	// The original reasoning fields must survive the backfill.
	if parsed["id"] != "rs_1" || parsed["type"] != "reasoning" {
		t.Fatalf("backfill dropped original reasoning fields: %q", sig)
	}
}

// A refusal must surface as text via response.refusal.delta.
func TestResponsesRefusalDelta(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.output_item.added","item":{"type":"message","id":"msg_1"}}

data: {"type":"response.content_part.added","part":{"type":"refusal","refusal":""}}

data: {"type":"response.refusal.delta","delta":"I cannot "}

data: {"type":"response.refusal.delta","delta":"help with that"}

data: {"type":"response.output_item.done","item":{"type":"message","id":"msg_1","content":[{"type":"refusal","refusal":"I cannot help with that"}]}}

data: {"type":"response.completed","response":{"id":"r","status":"completed"}}

`
	final := runResponsesSSE(t, reasoningModel(), ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, sse)
	var text, sig string
	for _, c := range final.Content {
		if tc, ok := c.(ai.TextContent); ok {
			text = tc.Text
			sig = tc.TextSignature
		}
	}
	if text != "I cannot help with that" {
		t.Fatalf("refusal text wrong: %q", text)
	}
	if sig != `{"v":1,"id":"msg_1"}` {
		t.Fatalf("text signature wrong: %q", sig)
	}
}

// pi 2d597f02: a message item whose `content` is null must not crash and yields
// empty text (pi guards with `item.content?.map(...) ?? "" `). In Go this is
// structurally safe — ranging a nil slice is a no-op, so no guard is needed; the
// rebuild produces "". This test locks that equivalence so a future refactor that
// dereferences content can't regress it.
func TestResponsesNullMessageContent(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.output_item.added","item":{"type":"message","id":"msg_1"}}

data: {"type":"response.content_part.added","part":{"type":"output_text","text":""}}

data: {"type":"response.output_text.delta","delta":"partial"}

data: {"type":"response.output_item.done","item":{"type":"message","id":"msg_1","content":null}}

data: {"type":"response.completed","response":{"id":"r","status":"completed"}}

`
	final := runResponsesSSE(t, reasoningModel(), ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, sse)
	for _, c := range final.Content {
		if tc, ok := c.(ai.TextContent); ok && tc.Text != "" {
			t.Fatalf("null content must rebuild to empty text, got %q", tc.Text)
		}
	}
}

// A provider that emits only function_call_arguments.done (no deltas) must still
// yield full args, and the trailing delta must be emitted.
func TestResponsesFunctionCallArgumentsDoneOnly(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"calc","arguments":""}}

data: {"type":"response.function_call_arguments.done","arguments":"{\"x\":7}"}

data: {"type":"response.output_item.done","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"calc","arguments":"{\"x\":7}"}}

data: {"type":"response.completed","response":{"id":"r","status":"completed"}}

`
	model := reasoningModel()
	req := ai.Context{
		Messages: []ai.Message{ai.NewUserText("hi", 1)},
		Tools:    []ai.Tool{{Name: "calc", Description: "calc", Parameters: ai.Object(ai.Prop("x", ai.Integer()))}},
	}
	var sawDelta bool
	stream := func() *ai.AssistantMessageEventStream {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("content-type", "text/event-stream")
			io.WriteString(w, sse)
		}))
		t.Cleanup(server.Close)
		m := *model
		m.BaseURL = server.URL
		return StreamOpenAIResponses(context.Background(), &m, req,
			&OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "sk"}}})
	}()
	for ev := range stream.Events() {
		if ev.Type == ai.EventToolCallDelta && ev.Delta == `{"x":7}` {
			sawDelta = true
		}
	}
	final := stream.Result()
	if !sawDelta {
		t.Fatalf("expected trailing toolcall_delta with full args")
	}
	var tool *ai.ToolCall
	for _, c := range final.Content {
		if tc, ok := c.(ai.ToolCall); ok {
			v := tc
			tool = &v
		}
	}
	if tool == nil {
		t.Fatalf("no tool call")
	}
	if v, _ := tool.Arguments["x"].(float64); v != 7 {
		t.Fatalf("args wrong: %#v", tool.Arguments)
	}
}

// Assistant text with a textSignature replays as a message item carrying that id.
func TestResponsesAssistantTextReplaySignature(t *testing.T) {
	model := reasoningModel()
	req := ai.Context{
		Messages: []ai.Message{
			ai.NewUserText("hi", 1),
			ai.AssistantMessage{
				Content:    ai.ContentList{ai.TextContent{Text: "prior", TextSignature: `{"v":1,"id":"msg_abc","phase":"final_answer"}`}},
				Api:        ai.APIOpenAIResponses,
				Provider:   "openai",
				Model:      "gpt-5",
				StopReason: ai.StopStop,
			},
			ai.NewUserText("again", 2),
		},
	}
	input := mustResponsesInput(t, model, req)
	var msgItem map[string]any
	for _, it := range input {
		if m, ok := it.(map[string]any); ok && m["type"] == "message" && m["role"] == "assistant" {
			msgItem = m
		}
	}
	if msgItem == nil {
		t.Fatalf("no assistant message item: %#v", input)
	}
	if msgItem["id"] != "msg_abc" {
		t.Fatalf("id wrong: %v", msgItem["id"])
	}
	if msgItem["phase"] != "final_answer" {
		t.Fatalf("phase wrong: %v", msgItem["phase"])
	}
}

// Foreign assistant tool-call ids get hashed into fc_<shortHash>; cross-model
// (same provider/api, different model id) fc_ ids are dropped.
func TestResponsesToolCallIDNormalization(t *testing.T) {
	model := reasoningModel()
	// Foreign source (anthropic): item id hashed into fc_ prefix.
	foreign := ai.Context{Messages: []ai.Message{
		ai.NewUserText("hi", 1),
		ai.AssistantMessage{
			Content:    ai.ContentList{ai.ToolCall{ID: "call_1|toolu_xyz", Name: "calc", Arguments: map[string]any{}}},
			Api:        ai.APIAnthropicMessages,
			Provider:   "anthropic",
			Model:      "claude",
			StopReason: ai.StopToolUse,
		},
		ai.ToolResultMessage{ToolCallID: "call_1|toolu_xyz", ToolName: "calc", Content: ai.ContentList{ai.TextContent{Text: "ok"}}, Timestamp: 2},
	}}
	in := mustResponsesInput(t, model, foreign)
	var fc map[string]any
	for _, it := range in {
		if m, ok := it.(map[string]any); ok && m["type"] == "function_call" {
			fc = m
		}
	}
	if fc == nil {
		t.Fatalf("no function_call item: %#v", in)
	}
	id, _ := fc["id"].(string)
	if !strings.HasPrefix(id, "fc_") {
		t.Fatalf("foreign item id should be fc_-hashed, got %q", id)
	}
	if id == "fc_toolu_xyz" {
		t.Fatalf("foreign item id should be hashed, not raw: %q", id)
	}
	if fc["call_id"] != "call_1" {
		t.Fatalf("call_id wrong: %v", fc["call_id"])
	}

	// Cross-model (same provider/api, different model id): fc_ item id dropped.
	crossModel := ai.Context{Messages: []ai.Message{
		ai.NewUserText("hi", 1),
		ai.AssistantMessage{
			Content:    ai.ContentList{ai.ToolCall{ID: "call_9|fc_old", Name: "calc", Arguments: map[string]any{}}},
			Api:        ai.APIOpenAIResponses,
			Provider:   "openai",
			Model:      "gpt-4.1",
			StopReason: ai.StopToolUse,
		},
		ai.ToolResultMessage{ToolCallID: "call_9|fc_old", ToolName: "calc", Content: ai.ContentList{ai.TextContent{Text: "ok"}}, Timestamp: 2},
	}}
	in2 := mustResponsesInput(t, model, crossModel)
	for _, it := range in2 {
		if m, ok := it.(map[string]any); ok && m["type"] == "function_call" {
			if _, has := m["id"]; has {
				t.Fatalf("cross-model fc_ id should be dropped, got %v", m["id"])
			}
			if m["call_id"] != "call_9" {
				t.Fatalf("call_id wrong: %v", m["call_id"])
			}
		}
	}
}

// Tool results containing images are emitted as input_image content parts.
func TestResponsesImageToolResult(t *testing.T) {
	model := &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "openai", Reasoning: true, Input: []string{"text", "image"}}
	req := ai.Context{Messages: []ai.Message{
		ai.NewUserText("hi", 1),
		ai.AssistantMessage{
			Content: ai.ContentList{ai.ToolCall{ID: "call_1|fc_1", Name: "shot", Arguments: map[string]any{}}},
			Api:     ai.APIOpenAIResponses, Provider: "openai", Model: "gpt-5", StopReason: ai.StopToolUse,
		},
		ai.ToolResultMessage{
			ToolCallID: "call_1|fc_1", ToolName: "shot",
			Content:   ai.ContentList{ai.TextContent{Text: "captured"}, ai.ImageContent{MimeType: "image/png", Data: "QUJD"}},
			Timestamp: 2,
		},
	}}
	in := mustResponsesInput(t, model, req)
	var out any
	for _, it := range in {
		if m, ok := it.(map[string]any); ok && m["type"] == "function_call_output" {
			out = m["output"]
		}
	}
	parts, ok := out.([]any)
	if !ok {
		t.Fatalf("image tool result output should be content-parts, got %T %#v", out, out)
	}
	var sawText, sawImage bool
	for _, p := range parts {
		pm, _ := p.(map[string]any)
		switch pm["type"] {
		case "input_text":
			sawText = pm["text"] == "captured"
		case "input_image":
			img, _ := pm["image_url"].(string)
			sawImage = strings.HasPrefix(img, "data:image/png;base64,QUJD")
		}
	}
	if !sawText || !sawImage {
		t.Fatalf("expected input_text + input_image parts, got %#v", parts)
	}
}

// Non-vision model: image-only tool result falls back to "(see attached image)".
func TestResponsesImageToolResultNonVision(t *testing.T) {
	model := &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "openai", Reasoning: true, Input: []string{"text"}}
	req := ai.Context{Messages: []ai.Message{
		ai.NewUserText("hi", 1),
		ai.AssistantMessage{
			Content: ai.ContentList{ai.ToolCall{ID: "call_1|fc_1", Name: "shot", Arguments: map[string]any{}}},
			Api:     ai.APIOpenAIResponses, Provider: "openai", Model: "gpt-5", StopReason: ai.StopToolUse,
		},
		ai.ToolResultMessage{
			ToolCallID: "call_1|fc_1", ToolName: "shot",
			Content:   ai.ContentList{ai.ImageContent{MimeType: "image/png", Data: "QUJD"}},
			Timestamp: 2,
		},
	}}
	in := mustResponsesInput(t, model, req)
	for _, it := range in {
		if m, ok := it.(map[string]any); ok && m["type"] == "function_call_output" {
			// transform downgrades the image to a placeholder for non-vision models,
			// so the text path is taken (placeholder text), not "(see attached image)".
			if _, isParts := m["output"].([]any); isParts {
				t.Fatalf("non-vision model must not emit input_image parts: %#v", m["output"])
			}
		}
	}
}

// Upstream 24bace27 flipped supportsStrictMode's default to false and made
// convertResponsesTools emit `strict` only where the provider supports it, so
// responses tools no longer carry an unconditional strict:false. pi 0.82.0:
//
//	compat undefined            -> [{"type":"function","name":"calc",…}]
//	{"supportsStrictMode":true} -> [{…,"strict":false}]
func TestResponsesToolsStrictOnlyWhenSupported(t *testing.T) {
	req := ai.Context{
		Messages: []ai.Message{ai.NewUserText("hi", 1)},
		Tools:    []ai.Tool{{Name: "calc", Description: "calc", Parameters: ai.Object(ai.Prop("x", ai.Integer()))}},
	}
	toolOf := func(compat json.RawMessage) map[string]any {
		t.Helper()
		model := reasoningModel()
		model.Compat = compat
		body := mustBuildResponsesParams(t, model, req, &OpenAIResponsesOptions{})
		tools, ok := body["tools"].([]map[string]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("tools wrong: %#v", body["tools"])
		}
		return tools[0]
	}

	tool := toolOf(nil)
	if _, has := tool["strict"]; has {
		t.Fatalf("strict must be omitted when the model does not set supportsStrictMode: %#v", tool)
	}
	if tool["type"] != "function" || tool["name"] != "calc" {
		t.Fatalf("tool body shape wrong: %#v", tool)
	}

	tool = toolOf(json.RawMessage(`{"supportsStrictMode":true}`))
	if strict, has := tool["strict"]; !has || strict != false {
		t.Fatalf("supportsStrictMode:true must send strict:false, got %v (has=%v)", strict, has)
	}
}

// System role is developer only when reasoning && compat.supportsDeveloperRole != false.
func TestResponsesDeveloperRoleGating(t *testing.T) {
	firstRole := func(in []any) string {
		for _, it := range in {
			if m, ok := it.(map[string]any); ok {
				if r, has := m["role"]; has {
					return r.(string)
				}
			}
		}
		return ""
	}
	req := ai.Context{SystemPrompt: "sys", Messages: []ai.Message{ai.NewUserText("hi", 1)}}

	reasoning := &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "openai", Reasoning: true}
	if got := firstRole(mustResponsesInput(t, reasoning, req)); got != "developer" {
		t.Fatalf("reasoning model should use developer role, got %q", got)
	}

	nonReasoning := &ai.Model{ID: "gpt-4o", Api: ai.APIOpenAIResponses, Provider: "openai", Reasoning: false}
	if got := firstRole(mustResponsesInput(t, nonReasoning, req)); got != "system" {
		t.Fatalf("non-reasoning model should use system role, got %q", got)
	}

	noDevRole := &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "openai", Reasoning: true,
		Compat: json.RawMessage(`{"supportsDeveloperRole":false}`)}
	if got := firstRole(mustResponsesInput(t, noDevRole, req)); got != "system" {
		t.Fatalf("supportsDeveloperRole:false should use system role, got %q", got)
	}
}

// github-copilot reasoning-off must not send a reasoning block.
func TestResponsesReasoningOffExcludesCopilot(t *testing.T) {
	copilot := &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "github-copilot", Reasoning: true}
	body := mustBuildResponsesParams(t, copilot, ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, &OpenAIResponsesOptions{})
	if _, ok := body["reasoning"]; ok {
		t.Fatalf("github-copilot reasoning-off must omit reasoning, got %v", body["reasoning"])
	}

	other := &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "openai", Reasoning: true}
	body2 := mustBuildResponsesParams(t, other, ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, &OpenAIResponsesOptions{})
	r, ok := body2["reasoning"].(map[string]any)
	if !ok || r["effort"] != "none" {
		t.Fatalf("non-copilot reasoning-off should send effort:none, got %v", body2["reasoning"])
	}
}

// mapResponsesStatus ports pi's status mapping incl. unknown→error. Upstream
// 32850ef7 qualifies "incomplete" by the provider's incomplete_details.reason:
// only max_output_tokens is a length stop, everything else is an error carrying
// the message the stream fails with.
func TestResponsesMapStatus(t *testing.T) {
	cases := []struct {
		status  string
		reason  string
		want    ai.StopReason
		wantMsg string
		err     bool
	}{
		{status: "", want: ai.StopStop},
		{status: "completed", want: ai.StopStop},
		{status: "incomplete", reason: "max_output_tokens", want: ai.StopLength},
		{status: "incomplete", reason: "content_filter", want: ai.StopError, wantMsg: "Response incomplete: content_filter"},
		{status: "incomplete", reason: "max_time_limit", want: ai.StopError, wantMsg: "Response incomplete: max_time_limit"},
		{status: "incomplete", want: ai.StopError, wantMsg: "Response incomplete without a provider reason"},
		{status: "failed", want: ai.StopError},
		{status: "cancelled", want: ai.StopError},
		{status: "in_progress", want: ai.StopStop},
		{status: "queued", want: ai.StopStop},
		{status: "weird", want: ai.StopStop, err: true},
	}
	for _, c := range cases {
		got, msg, err := mapResponsesStatus(c.status, c.reason)
		if (err != nil) != c.err {
			t.Fatalf("status %q/%q err=%v want err=%v", c.status, c.reason, err, c.err)
		}
		if c.err {
			continue
		}
		if got != c.want {
			t.Fatalf("status %q/%q got %s want %s", c.status, c.reason, got, c.want)
		}
		if msg != c.wantMsg {
			t.Fatalf("status %q/%q message = %q, want %q", c.status, c.reason, msg, c.wantMsg)
		}
	}
}

// response.failed must surface error.code/message or incomplete_details.reason.
func TestResponsesFailedSurfacesDetail(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.failed","response":{"id":"r","status":"failed","error":{"code":"rate_limit","message":"slow down"}}}

`
	final := runResponsesSSE(t, reasoningModel(), ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, sse)
	if final.StopReason != ai.StopError {
		t.Fatalf("expected error stop reason, got %s", final.StopReason)
	}
	if final.ErrorMessage != "rate_limit: slow down" {
		t.Fatalf("error message wrong: %q", final.ErrorMessage)
	}
}

// TestResponsesRawStopReason mirrors pi's openai-responses-terminal-event.test.ts
// additions in d7b02636: the response status is preserved verbatim on
// rawStopReason from every terminal event, including response.failed.
func TestResponsesRawStopReason(t *testing.T) {
	tests := []struct {
		name     string
		event    string
		wantStop ai.StopReason
		wantRaw  string
		wantErr  string
	}{
		{
			name:     "completed",
			event:    `data: {"type":"response.completed","response":{"id":"r","status":"completed"}}`,
			wantStop: ai.StopStop,
			wantRaw:  "completed",
		},
		{
			// Upstream 32850ef7 qualified this fixture with incomplete_details:
			// a truncated response is a length stop and rawStopReason carries
			// the provider's reason.
			name:     "incomplete truncated at max output tokens",
			event:    `data: {"type":"response.incomplete","response":{"id":"r","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`,
			wantStop: ai.StopLength,
			wantRaw:  "incomplete.max_output_tokens",
		},
		{
			// Content filtering is not resumable: an error carrying the reason.
			name:     "incomplete from content filtering",
			event:    `data: {"type":"response.incomplete","response":{"id":"r","status":"incomplete","incomplete_details":{"reason":"content_filter"}}}`,
			wantStop: ai.StopError,
			wantRaw:  "incomplete.content_filter",
			wantErr:  "Response incomplete: content_filter",
		},
		{
			// Unknown provider reasons are preserved verbatim, still as errors.
			name:     "incomplete for an unknown provider reason",
			event:    `data: {"type":"response.incomplete","response":{"id":"r","status":"incomplete","incomplete_details":{"reason":"max_time_limit"}}}`,
			wantStop: ai.StopError,
			wantRaw:  "incomplete.max_time_limit",
			wantErr:  "Response incomplete: max_time_limit",
		},
		{
			// No incomplete_details at all: nothing to qualify rawStopReason
			// with, and no reason to believe the stop is resumable.
			name:     "incomplete without a provider reason",
			event:    `data: {"type":"response.incomplete","response":{"id":"r","status":"incomplete"}}`,
			wantStop: ai.StopError,
			wantRaw:  "incomplete",
			wantErr:  "Response incomplete without a provider reason",
		},
		{
			name:     "failed",
			event:    `data: {"type":"response.failed","response":{"id":"r","status":"failed","error":{"code":"server_error","message":"boom"}}}`,
			wantStop: ai.StopError,
			wantRaw:  "failed",
			wantErr:  "server_error: boom",
		},
		{
			// pi reads `response?.status`, so a terminal event without a response
			// leaves rawStopReason unset.
			name:     "completed without a response object",
			event:    `data: {"type":"response.completed"}`,
			wantStop: ai.StopStop,
			wantRaw:  "",
		},
		{
			// pi assigns `event.response?.status` unconditionally on
			// response.failed, so a failure event carrying no response CLEARS the
			// status an earlier terminal event recorded — it must not report a
			// stale "completed".
			name: "failed without a response object clears an earlier status",
			event: `data: {"type":"response.completed","response":{"id":"r","status":"completed"}}` + "\n\n" +
				`data: {"type":"response.failed"}`,
			wantStop: ai.StopError,
			wantRaw:  "",
			wantErr:  "Unknown error (no error details in response)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sse := `data: {"type":"response.created","response":{"id":"r"}}` + "\n\n" + tt.event + "\n\n"
			final := runResponsesSSE(t, reasoningModel(), ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, sse)
			if final.StopReason != tt.wantStop {
				t.Fatalf("stopReason = %s, want %s (%s)", final.StopReason, tt.wantStop, final.ErrorMessage)
			}
			if final.RawStopReason != tt.wantRaw {
				t.Fatalf("rawStopReason = %q, want %q", final.RawStopReason, tt.wantRaw)
			}
			if final.ErrorMessage != tt.wantErr {
				t.Fatalf("errorMessage = %q, want %q", final.ErrorMessage, tt.wantErr)
			}
		})
	}
}

// Unknown response.completed status fails the stream (pi throws).
func TestResponsesUnknownStatusFails(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.completed","response":{"id":"r","status":"bogus"}}

`
	final := runResponsesSSE(t, reasoningModel(), ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, sse)
	if final.StopReason != ai.StopError {
		t.Fatalf("unknown status should fail, got %s", final.StopReason)
	}
	if !strings.Contains(final.ErrorMessage, "Unhandled stop reason") {
		t.Fatalf("error message wrong: %q", final.ErrorMessage)
	}
}

// Prompt-cache-key clamp must count code points, not bytes.
func TestResponsesPromptCacheKeyClampMultibyte(t *testing.T) {
	// 70 multibyte runes (each 3 bytes in UTF-8) -> 210 bytes.
	key := strings.Repeat("あ", 70)
	got := clampPromptCacheKey(key)
	if n := len([]rune(got)); n != 64 {
		t.Fatalf("clamp should keep 64 code points, got %d", n)
	}
	if got != strings.Repeat("あ", 64) {
		t.Fatalf("clamp result wrong: %q", got)
	}

	short := strings.Repeat("a", 10)
	if clampPromptCacheKey(short) != short {
		t.Fatalf("short key should pass through")
	}
}

// ---------------------------------------------------------------------------
// Parity sweep 2: A7 + D2-D7
// ---------------------------------------------------------------------------

// runResponsesSSEOpts is runResponsesSSE with caller-controlled options.
func runResponsesSSEOpts(t *testing.T, model *ai.Model, req ai.Context, sse string, opts *OpenAIResponsesOptions) *ai.AssistantMessage {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, sse)
	}))
	t.Cleanup(server.Close)
	m := *model
	m.BaseURL = server.URL
	if opts == nil {
		opts = &OpenAIResponsesOptions{}
	}
	if opts.APIKey == "" {
		opts.APIKey = "sk"
	}
	return StreamOpenAIResponses(context.Background(), &m, req, opts).Result()
}

func findResponsesItem(in []any, itemType string) map[string]any {
	for _, it := range in {
		if m, ok := it.(map[string]any); ok && m["type"] == itemType {
			return m
		}
	}
	return nil
}

// A7: same-model replays send tool-call ids verbatim — no normalization, no
// truncation (pi transform-messages.ts:133 gates normalizeToolCallId on
// !isSameModel; raw Responses ids are 450+ chars).
func TestResponsesSameModelToolCallIDReplayedVerbatim(t *testing.T) {
	model := reasoningModel() // gpt-5 / openai / openai-responses
	longCall := "call_" + strings.Repeat("x", 80)
	longItem := "fc_" + strings.Repeat("Y", 460)
	rawID := longCall + "|" + longItem
	req := ai.Context{Messages: []ai.Message{
		ai.NewUserText("hi", 1),
		ai.AssistantMessage{
			Content:    ai.ContentList{ai.ToolCall{ID: rawID, Name: "calc", Arguments: map[string]any{}}},
			Api:        ai.APIOpenAIResponses,
			Provider:   "openai",
			Model:      "gpt-5",
			StopReason: ai.StopToolUse,
		},
		ai.ToolResultMessage{ToolCallID: rawID, ToolName: "calc", Content: ai.ContentList{ai.TextContent{Text: "ok"}}, Timestamp: 2},
	}}
	in := mustResponsesInput(t, model, req)
	fc := findResponsesItem(in, "function_call")
	if fc == nil {
		t.Fatalf("no function_call item: %#v", in)
	}
	if fc["call_id"] != longCall {
		t.Fatalf("same-model call_id must be raw, got %v", fc["call_id"])
	}
	if fc["id"] != longItem {
		t.Fatalf("same-model item id must be raw (>64 chars untouched), got %v", fc["id"])
	}
	fco := findResponsesItem(in, "function_call_output")
	if fco == nil || fco["call_id"] != longCall {
		t.Fatalf("tool result call_id must be raw, got %#v", fco)
	}
}

// A7 (cross-model still normalized): special characters in the callId half are
// sanitized when the source is a different model.
func TestResponsesCrossModelToolCallIDStillNormalized(t *testing.T) {
	model := reasoningModel()
	req := ai.Context{Messages: []ai.Message{
		ai.NewUserText("hi", 1),
		ai.AssistantMessage{
			Content:    ai.ContentList{ai.ToolCall{ID: "call@9|fc_old", Name: "calc", Arguments: map[string]any{}}},
			Api:        ai.APIOpenAIResponses,
			Provider:   "openai",
			Model:      "gpt-4.1", // different model id, same provider/api
			StopReason: ai.StopToolUse,
		},
		ai.ToolResultMessage{ToolCallID: "call@9|fc_old", ToolName: "calc", Content: ai.ContentList{ai.TextContent{Text: "ok"}}, Timestamp: 2},
	}}
	in := mustResponsesInput(t, model, req)
	fc := findResponsesItem(in, "function_call")
	if fc == nil || fc["call_id"] != "call_9" {
		t.Fatalf("cross-model call_id should be sanitized to call_9, got %#v", fc)
	}
	if _, has := fc["id"]; has {
		t.Fatalf("cross-model fc_ item id should be dropped, got %v", fc["id"])
	}
	fco := findResponsesItem(in, "function_call_output")
	if fco == nil || fco["call_id"] != "call_9" {
		t.Fatalf("tool result should pick up the normalized id, got %#v", fco)
	}
}

// D2: service_tier is sent when set and omitted otherwise.
func TestResponsesServiceTierParam(t *testing.T) {
	model := reasoningModel()
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	body := mustBuildResponsesParams(t, model, req, &OpenAIResponsesOptions{ServiceTier: "flex"})
	if body["service_tier"] != "flex" {
		t.Fatalf("service_tier not sent: %v", body["service_tier"])
	}
	body2 := mustBuildResponsesParams(t, model, req, &OpenAIResponsesOptions{})
	if _, has := body2["service_tier"]; has {
		t.Fatalf("service_tier must be omitted when unset")
	}
}

const responsesPricingSSEFmt = `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.output_item.added","item":{"type":"message","id":"msg_1"}}

data: {"type":"response.content_part.added","part":{"type":"output_text","text":""}}

data: {"type":"response.output_text.delta","delta":"hi"}

data: {"type":"response.output_item.done","item":{"type":"message","id":"msg_1","content":[{"type":"output_text","text":"hi"}]}}

data: {"type":"response.completed","response":{"id":"r","status":"completed",%s"usage":{"input_tokens":20,"output_tokens":8,"total_tokens":28,"input_tokens_details":{"cached_tokens":0}}}}

`

// TestResponsesReasoningTokens checks output_tokens_details.reasoning_tokens
// populates Usage.Reasoning (pi's `reasoning: ... || 0`).
func TestResponsesReasoningTokens(t *testing.T) {
	model := &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "openai", Reasoning: true,
		Cost: ai.ModelCost{Input: 1.25, Output: 10}}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	sse := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\"}}\n\n" +
		"data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"message\",\"id\":\"msg_1\"}}\n\n" +
		"data: {\"type\":\"response.content_part.added\",\"part\":{\"type\":\"output_text\",\"text\":\"\"}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n" +
		"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"content\":[{\"type\":\"output_text\",\"text\":\"hi\"}]}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"usage\":{\"input_tokens\":20,\"output_tokens\":18,\"total_tokens\":38,\"input_tokens_details\":{\"cached_tokens\":0},\"output_tokens_details\":{\"reasoning_tokens\":12}}}}\n\n"
	final := runResponsesSSEOpts(t, model, req, sse, &OpenAIResponsesOptions{})
	if final.Usage.Reasoning != 12 {
		t.Fatalf("expected reasoning 12, got %+v", final.Usage)
	}
	if final.Usage.Output != 18 {
		t.Fatalf("output wrong: %+v", final.Usage)
	}
}

// D2: flex halves cost, priority doubles it (×2.5 for the exact id gpt-5.5),
// and the response-reported service tier wins over the requested option.
func TestResponsesServiceTierPricing(t *testing.T) {
	costModel := func(id string) *ai.Model {
		return &ai.Model{ID: id, Api: ai.APIOpenAIResponses, Provider: "openai", Reasoning: true,
			Cost: ai.ModelCost{Input: 1.25, Output: 10}}
	}
	base := 1.25/1_000_000*20 + 10.0/1_000_000*8
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	approx := func(a, b float64) bool {
		d := a - b
		return d < 1e-15 && d > -1e-15
	}

	cases := []struct {
		name     string
		model    *ai.Model
		opts     *OpenAIResponsesOptions
		respTier string // injected into response.completed when non-empty
		want     float64
	}{
		{"default", costModel("gpt-5"), &OpenAIResponsesOptions{}, "", base},
		{"flex-halves", costModel("gpt-5"), &OpenAIResponsesOptions{ServiceTier: "flex"}, "", base * 0.5},
		{"priority-doubles", costModel("gpt-5"), &OpenAIResponsesOptions{ServiceTier: "priority"}, "", base * 2},
		{"gpt-5.5-priority-x2.5", costModel("gpt-5.5"), &OpenAIResponsesOptions{ServiceTier: "priority"}, "", base * 2.5},
		{"response-tier-wins", costModel("gpt-5"), &OpenAIResponsesOptions{ServiceTier: "priority"}, `"service_tier":"default",`, base},
		{"response-flex-without-option", costModel("gpt-5"), &OpenAIResponsesOptions{}, `"service_tier":"flex",`, base * 0.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sse := fmt.Sprintf(responsesPricingSSEFmt, c.respTier)
			final := runResponsesSSEOpts(t, c.model, req, sse, c.opts)
			if final.StopReason != ai.StopStop {
				t.Fatalf("unexpected stop: %s (%s)", final.StopReason, final.ErrorMessage)
			}
			if !approx(final.Usage.Cost.Total, c.want) {
				t.Fatalf("cost total %v want %v", final.Usage.Cost.Total, c.want)
			}
		})
	}
}

// D3: session cache headers — shape selected by compat.sessionAffinityFormat
// ("openai" sends session_id + x-client-request-id; "openai-nosession" drops
// session_id; "openrouter" sends x-session-id), all suppressed when
// cacheRetention is "none" (pi openai-responses.ts:115,207-217; upstream 298665cf).
func TestResponsesSessionCacheHeaders(t *testing.T) {
	capture := func(model *ai.Model, opts *OpenAIResponsesOptions) http.Header {
		var got http.Header
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Header.Clone()
			w.Header().Set("content-type", "text/event-stream")
			io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\"}}\n\n")
		}))
		defer server.Close()
		m := *model
		m.BaseURL = server.URL
		opts.APIKey = "sk"
		StreamOpenAIResponses(context.Background(), &m, ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, opts).Result()
		return got
	}

	// Default (openai auto-detect): session_id + x-client-request-id, no x-session-id.
	h := capture(reasoningModel(), &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{SessionID: "sess-1"}})
	if h.Get("session_id") != "sess-1" || h.Get("x-client-request-id") != "sess-1" {
		t.Fatalf("expected both session headers, got session_id=%q x-client-request-id=%q", h.Get("session_id"), h.Get("x-client-request-id"))
	}
	if h.Get("x-session-id") != "" {
		t.Fatalf("openai format must not send x-session-id, got %q", h.Get("x-session-id"))
	}

	// "openai-nosession" drops session_id but keeps x-client-request-id.
	noSidModel := reasoningModel()
	noSidModel.Compat = json.RawMessage(`{"sessionAffinityFormat":"openai-nosession"}`)
	h2 := capture(noSidModel, &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{SessionID: "sess-1"}})
	if h2.Get("session_id") != "" {
		t.Fatalf("openai-nosession must suppress session_id, got %q", h2.Get("session_id"))
	}
	if h2.Get("x-client-request-id") != "sess-1" {
		t.Fatalf("x-client-request-id must still be sent, got %q", h2.Get("x-client-request-id"))
	}

	// "openrouter" sends only x-session-id (auto-detected from provider here).
	orModel := reasoningModel()
	orModel.Provider = "openrouter"
	h4 := capture(orModel, &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{SessionID: "sess-1"}})
	if h4.Get("x-session-id") != "sess-1" {
		t.Fatalf("openrouter must send x-session-id, got %q", h4.Get("x-session-id"))
	}
	if h4.Get("session_id") != "" || h4.Get("x-client-request-id") != "" {
		t.Fatalf("openrouter must not send openai headers, got session_id=%q x-client-request-id=%q", h4.Get("session_id"), h4.Get("x-client-request-id"))
	}

	h3 := capture(reasoningModel(), &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{SessionID: "sess-1", CacheRetention: ai.CacheNone}})
	if h3.Get("session_id") != "" || h3.Get("x-client-request-id") != "" || h3.Get("x-session-id") != "" {
		t.Fatalf("cacheRetention none must suppress all session headers, got session_id=%q x-client-request-id=%q x-session-id=%q", h3.Get("session_id"), h3.Get("x-client-request-id"), h3.Get("x-session-id"))
	}
}

// D4: shortHash iterates UTF-16 code units (JS charCodeAt); vectors verified
// against the pi npm build with node.
func TestResponsesShortHashUTF16Vectors(t *testing.T) {
	cases := map[string]string{
		"":                   "k4n83c7h0j2b",
		"emoji 🙈 id":         "jk0b7r1xq9646",
		"toolu_xyz":          "1j6u6f41xacfv1",
		"🙈🙉🙊":                "1pd5f9x1j6a281",
		"héllo wörld":        "1slrdvn1t61j5h",
		"call_abc123|fc_456": "13c60wm1owxk5l",
	}
	if got := shortHash(strings.Repeat("a", 500)); got != "d33jejlgylnv" {
		t.Errorf("shortHash(a*500) = %q want d33jejlgylnv", got)
	}
	for in, want := range cases {
		if got := shortHash(in); got != want {
			t.Errorf("shortHash(%q) = %q want %q", in, got, want)
		}
	}
}

// D4: normalizeResponsesIDPart replaces each UTF-16 code unit, so an astral
// character becomes TWO underscores (JS regex without /u); node-verified.
func TestResponsesNormalizeIDPartUTF16(t *testing.T) {
	cases := map[string]string{
		"ab🙈cd":                 "ab__cd",
		"call_1|fc_x":           "call_1_fc_x",
		"abc__":                 "abc",
		strings.Repeat("x", 70): strings.Repeat("x", 64),
	}
	for in, want := range cases {
		if got := normalizeResponsesIDPart(in); got != want {
			t.Errorf("normalizeResponsesIDPart(%q) = %q want %q", in, got, want)
		}
	}
}

// D5: a stream whose response.completed carries an error-grade status must
// fail with "An unknown error occurred", never emit done (pi :140-142).
func TestResponsesErrorStopReasonFailsStream(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.completed","response":{"id":"r","status":"cancelled"}}

`
	final := runResponsesSSE(t, reasoningModel(), ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, sse)
	if final.StopReason != ai.StopError {
		t.Fatalf("expected error stop, got %s", final.StopReason)
	}
	if final.ErrorMessage != "An unknown error occurred" {
		t.Fatalf("error message wrong: %q", final.ErrorMessage)
	}
}

// D6: providers outside OPENAI_TOOL_CALL_PROVIDERS sanitize the WHOLE raw id
// (pipe → underscore) into call_id and send NO item id (shared :110 + the
// undefined split).
func TestResponsesNonAllowedProviderPipeID(t *testing.T) {
	model := &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "github-copilot", Reasoning: true}
	req := ai.Context{Messages: []ai.Message{
		ai.NewUserText("hi", 1),
		ai.AssistantMessage{
			Content:    ai.ContentList{ai.ToolCall{ID: "call_1|fc_x", Name: "calc", Arguments: map[string]any{}}},
			Api:        ai.APIOpenAIResponses,
			Provider:   "openai", // foreign source → normalization applies
			Model:      "gpt-5",
			StopReason: ai.StopToolUse,
		},
		ai.ToolResultMessage{ToolCallID: "call_1|fc_x", ToolName: "calc", Content: ai.ContentList{ai.TextContent{Text: "ok"}}, Timestamp: 2},
	}}
	in := mustResponsesInput(t, model, req)
	fc := findResponsesItem(in, "function_call")
	if fc == nil {
		t.Fatalf("no function_call item: %#v", in)
	}
	if fc["call_id"] != "call_1_fc_x" {
		t.Fatalf("whole id should be sanitized into call_id, got %v", fc["call_id"])
	}
	if _, has := fc["id"]; has {
		t.Fatalf("non-allowed provider must not send an item id, got %v", fc["id"])
	}
	fco := findResponsesItem(in, "function_call_output")
	if fco == nil || fco["call_id"] != "call_1_fc_x" {
		t.Fatalf("tool result should pick up the sanitized id, got %#v", fco)
	}
}

// D6: github-copilot requests carry the dynamic copilot headers
// (pi openai-responses.ts:191-198).
func TestResponsesCopilotDynamicHeaders(t *testing.T) {
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\"}}\n\n")
	}))
	defer server.Close()
	model := &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "github-copilot", Reasoning: true, BaseURL: server.URL}

	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	StreamOpenAIResponses(context.Background(), model, req, &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "sk"}}}).Result()
	if got.Get("X-Initiator") != "user" || got.Get("Openai-Intent") != "conversation-edits" {
		t.Fatalf("copilot headers missing: X-Initiator=%q Openai-Intent=%q", got.Get("X-Initiator"), got.Get("Openai-Intent"))
	}
	if got.Get("Copilot-Vision-Request") != "" {
		t.Fatalf("vision header must be absent without images")
	}

	visionReq := ai.Context{Messages: []ai.Message{
		ai.UserMessage{Content: ai.ContentList{ai.ImageContent{MimeType: "image/png", Data: "QUJD"}}, Timestamp: 1},
		ai.NewUserText("done?", 2),
	}}
	model.Input = []string{"text", "image"}
	StreamOpenAIResponses(context.Background(), model, visionReq, &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "sk"}}}).Result()
	if got.Get("Copilot-Vision-Request") != "true" {
		t.Fatalf("vision header missing with image input")
	}
}

// D6: cloudflare-ai-gateway resolves {VAR} placeholders in baseUrl, sends the
// API key via cf-aig-authorization, and suppresses the default Authorization
// (pi openai-responses.ts:212-223).
func TestResponsesCloudflareAIGateway(t *testing.T) {
	var gotPath string
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		got = r.Header.Clone()
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\"}}\n\n")
	}))
	defer server.Close()
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct42")
	model := &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "cloudflare-ai-gateway",
		BaseURL: server.URL + "/{CLOUDFLARE_ACCOUNT_ID}"}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	final := StreamOpenAIResponses(context.Background(), model, req, &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "cfkey"}}}).Result()
	if final.StopReason != ai.StopStop {
		t.Fatalf("stream failed: %s", final.ErrorMessage)
	}
	if gotPath != "/acct42/responses" {
		t.Fatalf("baseURL placeholder not resolved, path %q", gotPath)
	}
	if got.Get("cf-aig-authorization") != "Bearer cfkey" {
		t.Fatalf("cf-aig-authorization wrong: %q", got.Get("cf-aig-authorization"))
	}
	if got.Get("authorization") != "" {
		t.Fatalf("default Authorization must be suppressed for cloudflare-ai-gateway, got %q", got.Get("authorization"))
	}
}

// D6: a missing {VAR} env fails the stream with pi's exact message.
func TestResponsesCloudflareMissingEnvFailsStream(t *testing.T) {
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "")
	model := &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "cloudflare-ai-gateway",
		BaseURL: "https://gateway.example/{CLOUDFLARE_ACCOUNT_ID}"}
	final := StreamOpenAIResponses(context.Background(), model, ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}},
		&OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "cfkey"}}}).Result()
	if final.StopReason != ai.StopError {
		t.Fatalf("expected error, got %s", final.StopReason)
	}
	if final.ErrorMessage != "CLOUDFLARE_ACCOUNT_ID is required for provider cloudflare-ai-gateway but is not set." {
		t.Fatalf("error message wrong: %q", final.ErrorMessage)
	}
}

// D7a: HTTP errors use pi's Responses format — formatOpenAIResponsesError
// wrapping the openai SDK APIError message (`${status} ${msg}`).
func TestResponsesHTTPErrorFormat(t *testing.T) {
	run := func(status int, body string) string {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			io.WriteString(w, body)
		}))
		defer server.Close()
		m := *reasoningModel()
		m.BaseURL = server.URL
		final := StreamOpenAIResponses(context.Background(), &m, ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}},
			&OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "sk"}}}).Result()
		return final.ErrorMessage
	}
	if got := run(429, `{"error":{"message":"slow down"}}`); got != "OpenAI API error (429): 429 slow down" {
		t.Errorf("json error body: %q", got)
	}
	if got := run(500, "oops"); got != "OpenAI API error (500): 500 oops" {
		t.Errorf("text error body: %q", got)
	}
	if got := run(503, ""); got != "OpenAI API error (503): 503 status code (no body)" {
		t.Errorf("empty error body: %q", got)
	}
	if got := run(400, `{"error":"boom"}`); got != `OpenAI API error (400): 400 "boom"` {
		t.Errorf("string error field: %q", got)
	}
}

// D7b: max_output_tokens is omitted for 0 (JS truthiness).
func TestResponsesMaxTokensZeroOmitted(t *testing.T) {
	model := reasoningModel()
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	zero := 0
	body := mustBuildResponsesParams(t, model, req, &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{MaxTokens: &zero}})
	if _, has := body["max_output_tokens"]; has {
		t.Fatalf("max_output_tokens must be omitted for 0")
	}
	hundred := 100
	body2 := mustBuildResponsesParams(t, model, req, &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{MaxTokens: &hundred}})
	if body2["max_output_tokens"] != 100 {
		t.Fatalf("max_output_tokens wrong: %v", body2["max_output_tokens"])
	}
}

// max_output_tokens is floored at 16 — the Responses API rejects lower (#6265).
func TestResponsesMaxTokensFloor(t *testing.T) {
	model := reasoningModel()
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	small := 8
	body := mustBuildResponsesParams(t, model, req, &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{MaxTokens: &small}})
	if body["max_output_tokens"] != openaiResponsesMinOutputTokens {
		t.Fatalf("max_output_tokens = %v, want %d floor", body["max_output_tokens"], openaiResponsesMinOutputTokens)
	}
	atFloor := 16
	body2 := mustBuildResponsesParams(t, model, req, &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{MaxTokens: &atFloor}})
	if body2["max_output_tokens"] != 16 {
		t.Fatalf("at-floor value must pass through: %v", body2["max_output_tokens"])
	}
}

// Upstream b8b873b98: compat.supportsMaxOutputTokens gates max_output_tokens
// entirely — some Codex-protocol gateways reject the parameter. Unlike the
// other Responses compat opt-ins it defaults to TRUE, so only an explicit
// false drops the parameter, and the floor never resurrects it.
func TestResponsesSupportsMaxOutputTokens(t *testing.T) {
	tests := []struct {
		name      string
		compat    json.RawMessage
		maxTokens int
		want      any // nil means the parameter must be absent
	}{
		{"unset compat defaults to true", nil, 1024, 1024},
		{"explicit true", json.RawMessage(`{"supportsMaxOutputTokens":true}`), 1024, 1024},
		{"unrelated compat keys keep the default", json.RawMessage(`{"supportsToolSearch":true}`), 1024, 1024},
		{"explicit false omits the parameter", json.RawMessage(`{"supportsMaxOutputTokens":false}`), 1024, nil},
		{"explicit false outranks the floor", json.RawMessage(`{"supportsMaxOutputTokens":false}`), 8, nil},
		// NOTE: a type-mismatched sibling key (e.g. {"supportsToolSearch":"yes",
		// "supportsMaxOutputTokens":false}) makes getResponsesCompat's one-shot
		// json.Unmarshal fail, discarding EVERY override including the explicit
		// false, so the port emits max_output_tokens where pi omits it. That is
		// a real PRE-EXISTING, port-wide divergence, not a behavior to pin
		// green here — it is tracked against real pi by the difftest scenario
		// `responses-compat-malformed-key` and its known-divergences entry.
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			model := reasoningModel()
			model.Compat = tc.compat
			req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
			body := mustBuildResponsesParams(t, model, req,
				&OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{MaxTokens: &tc.maxTokens}})
			got, has := body["max_output_tokens"]
			if tc.want == nil {
				if has {
					t.Fatalf("max_output_tokens must be omitted, got %v", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("max_output_tokens = %v (present=%v), want %v", got, has, tc.want)
			}
		})
	}
}

// No catalog model sets supportsMaxOutputTokens, so the default request body
// must stay byte-identical to the pre-b8b873b98 wire shape. Golden captured
// from this port at 65ffeec, before the flag existed.
func TestResponsesDefaultBodyUnchangedByMaxOutputTokensFlag(t *testing.T) {
	model := reasoningModel()
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	maxTokens := 1024
	body := mustBuildResponsesParams(t, model, req,
		&OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{MaxTokens: &maxTokens}})
	const want = `{"input":[{"content":[{"text":"hi","type":"input_text"}],"role":"user"}],"max_output_tokens":1024,"model":"gpt-5","reasoning":{"effort":"none"},"store":false,"stream":true}`
	if diff := gotWantJSON(t, body, want); diff != "" {
		t.Fatal(diff)
	}
}

// The contract b8b873b98 introduces is "the default body MINUS exactly one
// key", not merely "max_output_tokens is absent". Asserting the FULL body under
// both flag values — with the optional params that sit immediately around the
// gate populated — pins the difference to that one key: a gate that swallows a
// neighbouring block (temperature nesting inside it) or that reaches back into
// params it has no business touching (dropping store) changes the body without
// changing whether max_output_tokens is present, and only a whole-body golden
// sees it.
func TestResponsesFullBodyUnderMaxOutputTokensFlag(t *testing.T) {
	tests := []struct {
		name   string
		compat json.RawMessage
		want   string
	}{
		{
			name:   "flag true (the catalog default)",
			compat: json.RawMessage(`{"supportsMaxOutputTokens":true}`),
			want:   `{"input":[{"content":[{"text":"hi","type":"input_text"}],"role":"user"}],"max_output_tokens":1024,"model":"gpt-5","reasoning":{"effort":"none"},"service_tier":"priority","store":false,"stream":true,"temperature":0.5}`,
		},
		{
			name:   "flag false drops max_output_tokens and nothing else",
			compat: json.RawMessage(`{"supportsMaxOutputTokens":false}`),
			want:   `{"input":[{"content":[{"text":"hi","type":"input_text"}],"role":"user"}],"model":"gpt-5","reasoning":{"effort":"none"},"service_tier":"priority","store":false,"stream":true,"temperature":0.5}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			model := reasoningModel()
			model.Compat = tc.compat
			req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
			maxTokens := 1024
			temperature := 0.5
			body := mustBuildResponsesParams(t, model, req, &OpenAIResponsesOptions{
				StreamOptions: ai.StreamOptions{MaxTokens: &maxTokens, Temperature: &temperature},
				ServiceTier:   "priority",
			})
			if diff := gotWantJSON(t, body, tc.want); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

// D7c: an invalid thinkingSignature on a same-model replay fails the stream
// (pi's JSON.parse throws) instead of silently dropping the block.
func TestResponsesInvalidThinkingSignatureFailsStream(t *testing.T) {
	model := reasoningModel()
	req := ai.Context{Messages: []ai.Message{
		ai.NewUserText("hi", 1),
		ai.AssistantMessage{
			Content:    ai.ContentList{ai.ThinkingContent{Thinking: "deep", ThinkingSignature: "not-json"}},
			Api:        ai.APIOpenAIResponses,
			Provider:   "openai",
			Model:      "gpt-5",
			StopReason: ai.StopStop,
		},
		ai.NewUserText("again", 2),
	}}
	if _, err := responsesInput(model, req, nil); err == nil {
		t.Fatalf("expected responsesInput to error on invalid thinkingSignature")
	}
	final := runResponsesSSE(t, model, req, "")
	if final.StopReason != ai.StopError {
		t.Fatalf("expected error stop, got %s (%q)", final.StopReason, final.ErrorMessage)
	}
	if final.ErrorMessage == "" {
		t.Fatalf("expected a parse error message")
	}
}

// D7e: a function_call output_item.done without a prior output_item.added
// still emits toolcall_end with the constructed toolCall (pi shared :481-491;
// faithfully NOT appended to content).
func TestResponsesFunctionCallDoneWithoutAdded(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.output_item.done","item":{"type":"function_call","id":"fc_9","call_id":"call_9","name":"calc","arguments":"{\"x\":3}"}}

data: {"type":"response.completed","response":{"id":"r","status":"completed"}}

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, sse)
	}))
	defer server.Close()
	m := *reasoningModel()
	m.BaseURL = server.URL
	stream := StreamOpenAIResponses(context.Background(), &m, ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}},
		&OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "sk"}}})
	var end *ai.ToolCall
	var sawStart bool
	for ev := range stream.Events() {
		if ev.Type == ai.EventToolCallStart {
			sawStart = true
		}
		if ev.Type == ai.EventToolCallEnd {
			end = ev.ToolCall
		}
	}
	final := stream.Result()
	if end == nil {
		t.Fatalf("expected toolcall_end for done-without-added")
	}
	if end.ID != "call_9|fc_9" || end.Name != "calc" {
		t.Fatalf("constructed toolCall wrong: %#v", end)
	}
	if v, _ := end.Arguments["x"].(float64); v != 3 {
		t.Fatalf("constructed args wrong: %#v", end.Arguments)
	}
	// pi 8c9dbffa: getOrCreateSlot now materializes the block for a
	// done-without-added, so a toolcall_start fires and the block lands in
	// content (and the stop reason promotes to toolUse).
	if !sawStart {
		t.Fatalf("expected toolcall_start for done-without-added")
	}
	var toolInContent *ai.ToolCall
	for _, c := range final.Content {
		if tc, ok := c.(ai.ToolCall); ok {
			v := tc
			toolInContent = &v
		}
	}
	if toolInContent == nil || toolInContent.ID != "call_9|fc_9" {
		t.Fatalf("toolCall must be appended to content: %#v", final.Content)
	}
	if final.StopReason != ai.StopToolUse {
		t.Fatalf("stop reason wrong: %s (%s)", final.StopReason, final.ErrorMessage)
	}
}

// D7f: response.completed with a null response still maps the stop reason and
// promotes toolUse (pi shared :518-521 runs outside the response null-check).
func TestResponsesCompletedNullResponse(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"calc","arguments":""}}

data: {"type":"response.function_call_arguments.delta","delta":"{\"x\":1}"}

data: {"type":"response.output_item.done","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"calc","arguments":"{\"x\":1}"}}

data: {"type":"response.completed"}

`
	final := runResponsesSSE(t, reasoningModel(), ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, sse)
	if final.StopReason != ai.StopToolUse {
		t.Fatalf("null response.completed should still promote toolUse, got %s (%s)", final.StopReason, final.ErrorMessage)
	}
}

// D7h: onPayload/onResponse errors fail the stream; a non-nil onPayload return
// replaces the params wholesale.
func TestResponsesOnPayloadOnResponsePropagation(t *testing.T) {
	model := reasoningModel()
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}

	final := runResponsesSSEOpts(t, model, req, "", &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{
		ProviderRequestOptions: ai.ProviderRequestOptions{OnPayload: func(payload any, m *ai.Model) (any, error) { return nil, fmt.Errorf("payload veto") }},
	}})
	if final.StopReason != ai.StopError || final.ErrorMessage != "payload veto" {
		t.Fatalf("onPayload error must fail stream: %s %q", final.StopReason, final.ErrorMessage)
	}

	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\"}}\n\n")
	}))
	defer server.Close()
	m := *model
	m.BaseURL = server.URL
	StreamOpenAIResponses(context.Background(), &m, req, &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{
		ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "sk", OnPayload: func(payload any, mm *ai.Model) (any, error) { return map[string]any{"replaced": true}, nil }},
	}}).Result()
	if gotBody == nil || gotBody["replaced"] != true || len(gotBody) != 1 {
		t.Fatalf("onPayload replacement must be wholesale: %#v", gotBody)
	}

	final3 := runResponsesSSEOpts(t, model, req, "", &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{
		ProviderRequestOptions: ai.ProviderRequestOptions{OnResponse: func(resp ai.ProviderResponse, mm *ai.Model) error { return fmt.Errorf("response veto") }},
	}})
	if final3.StopReason != ai.StopError || final3.ErrorMessage != "response veto" {
		t.Fatalf("onResponse error must fail stream: %s %q", final3.StopReason, final3.ErrorMessage)
	}
}

// Upstream cd95c274: a response.incomplete terminal event finalizes usage and
// stop reason identically to response.completed (status "incomplete" -> length).
func TestResponsesIncompleteFinalizesUsageAndStopReason(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.output_item.added","item":{"type":"message","id":"msg_1"}}

data: {"type":"response.content_part.added","part":{"type":"output_text","text":""}}

data: {"type":"response.output_text.delta","delta":"partial"}

data: {"type":"response.output_item.done","item":{"type":"message","id":"msg_1","content":[{"type":"output_text","text":"partial"}]}}

data: {"type":"response.incomplete","response":{"id":"r","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":20,"output_tokens":8,"total_tokens":28,"input_tokens_details":{"cached_tokens":5}}}}

`
	model := &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "openai", Reasoning: true,
		Cost: ai.ModelCost{Input: 1.25, Output: 10}}
	final := runResponsesSSE(t, model, ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, sse)
	if final.StopReason != ai.StopLength {
		t.Fatalf("incomplete should map to length stop, got %s (%s)", final.StopReason, final.ErrorMessage)
	}
	if final.Usage.Input != 15 || final.Usage.CacheRead != 5 || final.Usage.Output != 8 || final.Usage.TotalTokens != 28 {
		t.Fatalf("incomplete usage not finalized: %+v", final.Usage)
	}
	if final.Usage.Cost.Total <= 0 {
		t.Fatalf("incomplete must run cost calc, got %v", final.Usage.Cost.Total)
	}
	var text string
	for _, c := range final.Content {
		if tc, ok := c.(ai.TextContent); ok {
			text = tc.Text
		}
	}
	if text != "partial" {
		t.Fatalf("text wrong: %q", text)
	}
}

// Upstream f9a49869: the streaming partial starts with a pending stop reason and
// resolves to "stop" as soon as a message item reaches the "final_answer" phase,
// exposing the terminal reason mid-stream before response.completed arrives.
func TestResponsesFinalAnswerPhaseResolvesPendingStop(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.output_item.added","item":{"type":"message","id":"msg_1"}}

data: {"type":"response.content_part.added","part":{"type":"output_text","text":""}}

data: {"type":"response.output_text.delta","delta":"Answer: 42"}

data: {"type":"response.output_item.done","item":{"type":"message","id":"msg_1","phase":"final_answer","content":[{"type":"output_text","text":"Answer: 42"}]}}

data: {"type":"response.completed","response":{"id":"r","status":"completed","usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8}}}

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, sse)
	}))
	t.Cleanup(server.Close)
	model := reasoningModel()
	model.BaseURL = server.URL
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	stream := StreamOpenAIResponses(context.Background(), model, req,
		&OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "sk"}}})

	var startStop, textEndStop ai.StopReason
	sawStart, sawTextEnd := false, false
	for e := range stream.Events() {
		switch e.Type {
		case ai.EventStart:
			sawStart = true
			startStop = e.Partial.StopReason
		case ai.EventTextEnd:
			sawTextEnd = true
			textEndStop = e.Partial.StopReason
		}
	}
	final := stream.Result()

	if !sawStart || startStop != ai.StopPending {
		t.Fatalf("stream should start pending, got start=%v stop=%q", sawStart, startStop)
	}
	if !sawTextEnd || textEndStop != ai.StopStop {
		t.Fatalf("final_answer phase must resolve partial to stop before terminal, got textEnd=%v stop=%q", sawTextEnd, textEndStop)
	}
	if final.StopReason != ai.StopStop {
		t.Fatalf("final stop reason wrong: %s (%s)", final.StopReason, final.ErrorMessage)
	}
}

// Upstream f9a49869: the final_answer phase also resolves the pending stop when
// it arrives on the output_item.added event (the createSlot call site), not only
// on output_item.done — the partial is stop from the very first *_start event.
func TestResponsesFinalAnswerPhaseOnAddedResolvesStop(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.output_item.added","item":{"type":"message","id":"msg_1","phase":"final_answer"}}

data: {"type":"response.content_part.added","part":{"type":"output_text","text":""}}

data: {"type":"response.output_text.delta","delta":"hi"}

data: {"type":"response.output_item.done","item":{"type":"message","id":"msg_1","phase":"final_answer","content":[{"type":"output_text","text":"hi"}]}}

data: {"type":"response.completed","response":{"id":"r","status":"completed","usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6}}}

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, sse)
	}))
	t.Cleanup(server.Close)
	model := reasoningModel()
	model.BaseURL = server.URL
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	stream := StreamOpenAIResponses(context.Background(), model, req,
		&OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "sk"}}})

	var textStartStop ai.StopReason
	sawTextStart := false
	for e := range stream.Events() {
		if e.Type == ai.EventTextStart {
			sawTextStart = true
			textStartStop = e.Partial.StopReason
		}
	}
	final := stream.Result()

	if !sawTextStart || textStartStop != ai.StopStop {
		t.Fatalf("phase on output_item.added must resolve partial to stop at text start, got textStart=%v stop=%q", sawTextStart, textStartStop)
	}
	if final.StopReason != ai.StopStop {
		t.Fatalf("final stop reason wrong: %s (%s)", final.StopReason, final.ErrorMessage)
	}
}

// Upstream f9a49869: a provisional "stop" set by the final_answer phase must be
// overridden by the terminal event's real reason — an incomplete response still
// maps to length, not the phase's stop.
func TestResponsesIncompleteOverridesFinalAnswerStop(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.output_item.added","item":{"type":"message","id":"msg_1"}}

data: {"type":"response.content_part.added","part":{"type":"output_text","text":""}}

data: {"type":"response.output_text.delta","delta":"partial answer"}

data: {"type":"response.output_item.done","item":{"type":"message","id":"msg_1","phase":"final_answer","content":[{"type":"output_text","text":"partial answer"}]}}

data: {"type":"response.incomplete","response":{"id":"r","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":20,"output_tokens":8,"total_tokens":28}}}

`
	final := runResponsesSSE(t, reasoningModel(), ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, sse)
	if final.StopReason != ai.StopLength {
		t.Fatalf("terminal incomplete must override the provisional final_answer stop, got %s (%s)", final.StopReason, final.ErrorMessage)
	}
}

// Upstream cd95c274: a stream that ends without response.completed/.incomplete/
// .failed fails with this exact message.
func TestResponsesNoTerminalEventFailsStream(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.output_item.added","item":{"type":"message","id":"msg_1"}}

data: {"type":"response.content_part.added","part":{"type":"output_text","text":""}}

data: {"type":"response.output_text.delta","delta":"partial"}

data: {"type":"response.output_item.done","item":{"type":"message","id":"msg_1","content":[{"type":"output_text","text":"partial"}]}}

`
	final := runResponsesSSE(t, reasoningModel(), ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, sse)
	if final.StopReason != ai.StopError {
		t.Fatalf("missing terminal event should fail, got %s", final.StopReason)
	}
	if final.ErrorMessage != "OpenAI Responses stream ended before a terminal response event" {
		t.Fatalf("error message wrong: %q", final.ErrorMessage)
	}
}

// C5 (responses half): prompt_cache_retention is independent of sessionId;
// prompt_cache_key still requires one.
func TestResponsesCacheRetentionWithoutSessionID(t *testing.T) {
	model := reasoningModel()
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	body := mustBuildResponsesParams(t, model, req, &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{CacheRetention: ai.CacheLong}})
	if body["prompt_cache_retention"] != "24h" {
		t.Fatalf("prompt_cache_retention must be sent without sessionId, got %v", body["prompt_cache_retention"])
	}
	if _, has := body["prompt_cache_key"]; has {
		t.Fatalf("prompt_cache_key requires a sessionId")
	}
}

// Upstream 241431c6: `prompt_cache_options: {mode: "explicit"}` is emitted only
// when cacheRetention is "none" AND compat.supportsExplicitPromptCacheMode.
// Expectations captured from pi 0.82.0 (dist/api/openai-responses.js onPayload).
func TestResponsesExplicitPromptCacheMode(t *testing.T) {
	explicitCompat := json.RawMessage(`{"supportsExplicitPromptCacheMode":true}`)
	tests := []struct {
		name      string
		compat    json.RawMessage
		retention ai.CacheRetention
		want      bool
	}{
		{"unset compat, none", nil, ai.CacheNone, false},
		{"explicit compat, none", explicitCompat, ai.CacheNone, true},
		{"explicit compat, short", explicitCompat, "", false},
		{"explicit compat, long", explicitCompat, ai.CacheLong, false},
		{"compat false, none", json.RawMessage(`{"supportsExplicitPromptCacheMode":false}`), ai.CacheNone, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			model := reasoningModel()
			model.Compat = tc.compat
			req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
			body := mustBuildResponsesParams(t, model, req,
				&OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{SessionID: "sess-1", CacheRetention: tc.retention}})
			got, has := body["prompt_cache_options"]
			if has != tc.want {
				t.Fatalf("prompt_cache_options present=%v want %v (value %#v)", has, tc.want, got)
			}
			if !tc.want {
				return
			}
			if diff := gotWantJSON(t, got, `{"mode":"explicit"}`); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

// gotWantJSON renders got as JSON and compares it byte-for-byte with want.
func gotWantJSON(t *testing.T, got any, want string) string {
	t.Helper()
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) == want {
		return ""
	}
	return "got " + string(b) + ", want " + want
}

// Regression for #6009 (upstream 8c9dbffa): when output items interleave, a
// delta for an earlier item that arrives AFTER a later item's block was started
// must route to the earlier item's block by output_index, not to whatever block
// was appended last. Here the reasoning item (output_index 0) gets a
// reasoning_text.delta only after the message item (output_index 1) has opened
// and received a text delta — the thinking must land on content[0], the text on
// content[1].
func TestResponsesOutOfOrderItemsPreserveReasoning(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r"}}

data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1"}}

data: {"type":"response.output_item.added","output_index":1,"item":{"type":"message","id":"msg_1"}}

data: {"type":"response.output_text.delta","output_index":1,"delta":"Answer: "}

data: {"type":"response.reasoning_text.delta","output_index":0,"delta":"thinking hard"}

data: {"type":"response.output_text.delta","output_index":1,"delta":"42"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","content":[{"type":"reasoning_text","text":"thinking hard"}]}}

data: {"type":"response.output_item.done","output_index":1,"item":{"type":"message","id":"msg_1","content":[{"type":"output_text","text":"Answer: 42"}]}}

data: {"type":"response.completed","response":{"id":"r","status":"completed"}}

`
	// Capture the stream events so we can assert the interleaved reasoning delta
	// targeted content[0] (the reasoning block), not content[1].
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, sse)
	}))
	defer server.Close()
	m := *reasoningModel()
	m.BaseURL = server.URL
	stream := StreamOpenAIResponses(context.Background(), &m, ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}},
		&OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "sk"}}})

	var thinkingDeltaIndex = -1
	for ev := range stream.Events() {
		if ev.Type == ai.EventThinkingDelta && ev.Delta == "thinking hard" {
			thinkingDeltaIndex = ev.ContentIndex
		}
	}
	final := stream.Result()

	// The interleaved thinking delta must have been emitted against content[0].
	if thinkingDeltaIndex != 0 {
		t.Fatalf("thinking delta routed to contentIndex %d, want 0", thinkingDeltaIndex)
	}

	if len(final.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d: %#v", len(final.Content), final.Content)
	}
	think, ok := final.Content[0].(ai.ThinkingContent)
	if !ok {
		t.Fatalf("content[0] not thinking: %#v", final.Content[0])
	}
	if think.Thinking != "thinking hard" {
		t.Fatalf("reasoning landed on wrong block: %q", think.Thinking)
	}
	text, ok := final.Content[1].(ai.TextContent)
	if !ok {
		t.Fatalf("content[1] not text: %#v", final.Content[1])
	}
	if text.Text != "Answer: 42" {
		t.Fatalf("text landed on wrong block: %q", text.Text)
	}
}

// pi #6290 (upstream 279f53b0): an empty tool result with no image content must
// send "(no tool output)" as the function_call_output, not "(see attached image)".
func TestResponsesEmptyToolResultNoImagePlaceholder(t *testing.T) {
	model := &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "openai", Reasoning: true, Input: []string{"text", "image"}}
	req := ai.Context{Messages: []ai.Message{
		ai.NewUserText("run it", 1),
		ai.AssistantMessage{
			Content: ai.ContentList{ai.ToolCall{ID: "call_1|fc_1", Name: "bash", Arguments: map[string]any{"command": "true"}}},
			Api:     ai.APIOpenAIResponses, Provider: "openai", Model: "gpt-5", StopReason: ai.StopToolUse,
		},
		ai.ToolResultMessage{ToolCallID: "call_1|fc_1", ToolName: "bash", Content: ai.ContentList{ai.TextContent{Text: ""}}, Timestamp: 2},
	}}
	in := mustResponsesInput(t, model, req)
	found := false
	for _, it := range in {
		if m, ok := it.(map[string]any); ok && m["type"] == "function_call_output" {
			found = true
			if m["output"] != "(no tool output)" {
				t.Fatalf("empty no-image output = %#v, want %q", m["output"], "(no tool output)")
			}
		}
	}
	if !found {
		t.Fatalf("no function_call_output emitted: %#v", in)
	}
}

// Mirrors pi openai-responses-terminal-event.test.ts (upstream a9ecf301):
// input_tokens includes both cached and cache-write tokens, so both are
// subtracted (clamped at 0) to get non-cached input, and cache_write_tokens
// surfaces as cacheWrite.
func TestOpenAIResponsesUsageCacheWrite(t *testing.T) {
	sse := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_completed\",\"status\":\"completed\"," +
		"\"usage\":{\"input_tokens\":20,\"output_tokens\":7,\"total_tokens\":27," +
		"\"input_tokens_details\":{\"cached_tokens\":2,\"cache_write_tokens\":3}}}}\n\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, sse)
	}))
	defer server.Close()

	model := &ai.Model{
		ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "openai", BaseURL: server.URL,
		MaxTokens: 4096, Cost: ai.ModelCost{Input: 1.25, Output: 10},
	}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}
	final := StreamOpenAIResponses(context.Background(), model, req, &OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "sk"}}}).Result()

	if final.Usage.Input != 15 || final.Usage.CacheRead != 2 || final.Usage.CacheWrite != 3 ||
		final.Usage.Output != 7 || final.Usage.TotalTokens != 27 {
		t.Fatalf("usage wrong: %+v (want Input 15, CacheRead 2, CacheWrite 3, Output 7, Total 27)", final.Usage)
	}
}

// TestResponsesXaiEncryptedReasoningInclude locks pi 5220aba6: every
// reasoning-capable xai model requests include=["reasoning.encrypted_content"]
// regardless of which reasoning branch fired — including the grok-4.5 shape
// (thinkingLevelMap.off null, no effort → no reasoning object, include still
// sent) — while non-xai models without effort stay include-free.
func TestResponsesXaiEncryptedReasoningInclude(t *testing.T) {
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}

	// grok-4.5 shape: reasoning true, off:null map, no effort requested.
	grok := &ai.Model{
		ID: "grok-4.5", Api: ai.APIOpenAIResponses, Provider: "xai",
		BaseURL: "https://api.x.ai/v1", Reasoning: true,
		ThinkingLevelMap: map[ai.ModelThinkingLevel]*string{"off": nil, "minimal": nil},
		Input:            []string{"text"}, MaxTokens: 4096,
	}
	params := mustBuildResponsesParams(t, grok, req, &OpenAIResponsesOptions{})
	include, ok := params["include"].([]any)
	if !ok || len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("xai include = %v, want [reasoning.encrypted_content]", params["include"])
	}
	if _, hasReasoning := params["reasoning"]; hasReasoning {
		t.Fatalf("off:null with no effort must not send reasoning: %v", params["reasoning"])
	}

	// With an effort both the reasoning object and include are present.
	params = mustBuildResponsesParams(t, grok, req, &OpenAIResponsesOptions{ReasoningEffort: "high"})
	if _, ok := params["include"].([]any); !ok {
		t.Fatalf("xai include missing with effort: %v", params["include"])
	}

	// Non-xai reasoning model without effort: no include.
	other := &ai.Model{
		ID: "some-model", Api: ai.APIOpenAIResponses, Provider: "openai",
		BaseURL: "https://api.openai.com/v1", Reasoning: true,
		Input: []string{"text"}, MaxTokens: 4096,
	}
	params = mustBuildResponsesParams(t, other, req, &OpenAIResponsesOptions{})
	if _, has := params["include"]; has {
		t.Fatalf("non-xai without effort must not send include: %v", params["include"])
	}

	// Non-reasoning xai model: no include (the branch is inside model.reasoning).
	flat := &ai.Model{
		ID: "grok-build-0.1", Api: ai.APIOpenAIResponses, Provider: "xai",
		BaseURL: "https://api.x.ai/v1", Reasoning: false,
		Input: []string{"text"}, MaxTokens: 4096,
	}
	params = mustBuildResponsesParams(t, flat, req, &OpenAIResponsesOptions{})
	if _, has := params["include"]; has {
		t.Fatalf("non-reasoning xai must not send include: %v", params["include"])
	}
}

// ---- Upstream 24bace27: constrained sampling (responses) ----

func grammarResponsesModel(compat string) *ai.Model {
	m := reasoningModel()
	m.Compat = json.RawMessage(compat)
	return m
}

// pi 0.82.0 tools payload for a lark grammar tool on openai-responses:
//
//	[{"type":"custom","name":"gram","description":"gram tool",
//	  "format":{"type":"grammar","syntax":"lark","definition":"start: /.+/"}}]
func TestResponsesGrammarToolSerialization(t *testing.T) {
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}, Tools: []ai.Tool{grammarSamplingTool()}}
	body := mustBuildResponsesParams(t, grammarResponsesModel(`{"supportsOpenAIGrammarTools":true}`), req, &OpenAIResponsesOptions{})
	if got := mustJSON(t, body["tools"]); got != `[{"description":"gram tool","format":{"definition":"start: /.+/","syntax":"lark","type":"grammar"},"name":"gram","type":"custom"}]` {
		t.Fatalf("grammar tool payload: %s", got)
	}

	// Without grammar support it degrades to a function tool (and, since this
	// model does not set supportsStrictMode, carries no strict key).
	plain := mustBuildResponsesParams(t, reasoningModel(), req, &OpenAIResponsesOptions{})
	tools, _ := plain["tools"].([]map[string]any)
	if len(tools) != 1 || tools[0]["type"] != "function" {
		t.Fatalf("grammar tool must fall back to a function tool: %#v", plain["tools"])
	}
}

// pi 0.82.0 replay of a grammar tool call + its result:
//
//	{"type":"custom_tool_call","id":"ctc_item","call_id":"ctc_abc","name":"gram","input":"SELECT 1"}
//	{"type":"custom_tool_call_output","call_id":"ctc_abc","output":"ok"}
func TestResponsesGrammarToolCallReplay(t *testing.T) {
	model := grammarResponsesModel(`{"supportsOpenAIGrammarTools":true}`)
	req := ai.Context{
		Messages: []ai.Message{
			ai.NewUserText("hi", 1),
			ai.AssistantMessage{
				Content:  ai.ContentList{ai.ToolCall{ID: "ctc_abc|ctc_item", Name: "gram", Arguments: map[string]any{"query": "SELECT 1"}}},
				Model:    model.ID,
				Provider: model.Provider,
				Api:      model.Api,
			},
			ai.ToolResultMessage{ToolCallID: "ctc_abc|ctc_item", ToolName: "gram", Content: ai.ContentList{ai.TextContent{Text: "ok"}}},
		},
		Tools: []ai.Tool{grammarSamplingTool()},
	}
	in := mustResponsesInput(t, model, req)
	call := findResponsesItem(in, "custom_tool_call")
	if got := mustJSON(t, call); got != `{"call_id":"ctc_abc","id":"ctc_item","input":"SELECT 1","name":"gram","type":"custom_tool_call"}` {
		t.Fatalf("custom_tool_call: %s", got)
	}
	out := findResponsesItem(in, "custom_tool_call_output")
	if got := mustJSON(t, out); got != `{"call_id":"ctc_abc","output":"ok","type":"custom_tool_call_output"}` {
		t.Fatalf("custom_tool_call_output: %s", got)
	}
}

// Upstream 24bace27 also changed the function_call replay path: an item id that
// is not fc_* cannot ride on a function_call item and is now dropped. pi 0.82.0
// with grammar support OFF replays the same transcript as
// {"type":"function_call","call_id":"ctc_abc",…} — no "id" — while an fc_* id
// still survives.
func TestResponsesFunctionCallReplayDropsNonFCItemID(t *testing.T) {
	model := reasoningModel()
	transcript := func(id string) ai.Context {
		return ai.Context{
			Messages: []ai.Message{
				ai.NewUserText("hi", 1),
				ai.AssistantMessage{
					Content:  ai.ContentList{ai.ToolCall{ID: id, Name: "gram", Arguments: map[string]any{"query": "SELECT 1"}}},
					Model:    model.ID,
					Provider: model.Provider,
					Api:      model.Api,
				},
				ai.ToolResultMessage{ToolCallID: id, ToolName: "gram", Content: ai.ContentList{ai.TextContent{Text: "ok"}}},
			},
			Tools: []ai.Tool{grammarSamplingTool()},
		}
	}

	call := findResponsesItem(mustResponsesInput(t, model, transcript("ctc_abc|ctc_item")), "function_call")
	if _, has := call["id"]; has {
		t.Fatalf("a non-fc_ item id must be dropped from a function_call: %#v", call)
	}
	if call["call_id"] != "ctc_abc" {
		t.Fatalf("call_id must survive: %#v", call)
	}

	call = findResponsesItem(mustResponsesInput(t, model, transcript("fc_abc|fc_item")), "function_call")
	if call["id"] != "fc_item" {
		t.Fatalf("an fc_ item id must survive: %#v", call)
	}
}

// Event sequence captured from pi 0.82.0 driving the same SSE.
func TestResponsesGrammarToolCallStreaming(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r1"}}

data: {"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_item","call_id":"ctc_call","name":"gram","input":""}}

data: {"type":"response.custom_tool_call_input.delta","output_index":0,"delta":"SEL"}

data: {"type":"response.custom_tool_call_input.delta","output_index":0,"delta":"ECT \"a\""}

data: {"type":"response.custom_tool_call_input.done","output_index":0,"input":"SELECT \"a\""}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_item","call_id":"ctc_call","name":"gram","input":"SELECT \"a\""}}

data: {"type":"response.completed","response":{"id":"r1","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}

data: [DONE]

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, sse)
	}))
	defer server.Close()

	model := grammarResponsesModel(`{"supportsOpenAIGrammarTools":true}`)
	model.BaseURL = server.URL
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}, Tools: []ai.Tool{grammarSamplingTool()}}
	stream := StreamOpenAIResponses(context.Background(), model, req,
		&OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "sk"}}})

	var deltas []string
	for ev := range stream.Events() {
		if ev.Type == ai.EventToolCallDelta {
			deltas = append(deltas, ev.Delta)
		}
	}
	want := []string{`{"query":"SEL`, `ECT \"a\"`, `"}`}
	if len(deltas) != len(want) {
		t.Fatalf("deltas = %#v, want %#v", deltas, want)
	}
	for i := range want {
		if deltas[i] != want[i] {
			t.Fatalf("delta %d = %q, want %q", i, deltas[i], want[i])
		}
	}
	final := stream.Result()
	tc, ok := final.Content[0].(ai.ToolCall)
	if !ok || tc.ID != "ctc_call|ctc_item" || tc.Name != "gram" || tc.Arguments["query"] != `SELECT "a"` {
		t.Fatalf("final tool call = %#v", final.Content)
	}
}

// TestResponsesGrammarSeededInput: output_item.added may carry a non-empty
// `input`, which pi seeds the tool-call arguments with (`item.input || ""`)
// while leaving the delta buffer empty — so the first emitted delta re-emits the
// seed together with the first streamed chunk.
//
// Expectation captured from pi 0.82.0 (dist/api/openai-responses.js) driven over
// a local SSE server: deltas were ["{\"query\":\"SELECT 1", "\"}"].
func TestResponsesGrammarSeededInput(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r1"}}

data: {"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_item","call_id":"ctc_call","name":"gram","input":"SEL"}}

data: {"type":"response.custom_tool_call_input.delta","output_index":0,"delta":"ECT 1"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_item","call_id":"ctc_call","name":"gram","input":"SELECT 1"}}

data: {"type":"response.completed","response":{"id":"r1","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}

data: [DONE]

`
	deltas, final := runResponsesGrammarSSE(t, sse)
	assertDeltas(t, deltas, []string{`{"query":"SELECT 1`, `"}`})
	assertGrammarArgs(t, final, "query", "SELECT 1")
}

// TestResponsesGrammarDoneWithoutInput: output_item.done may omit `input`
// entirely, where pi's `item.input ?? …` falls back to the input accumulated so
// far. An absent field is NOT the same as an empty string, which is why
// responsesItem.Input is a *string.
//
// Expectation captured from pi 0.82.0 the same way: ["{\"query\":\"SELECT 2", "\"}"].
func TestResponsesGrammarDoneWithoutInput(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r1"}}

data: {"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_item","call_id":"ctc_call","name":"gram","input":""}}

data: {"type":"response.custom_tool_call_input.delta","output_index":0,"delta":"SELECT 2"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_item","call_id":"ctc_call","name":"gram"}}

data: {"type":"response.completed","response":{"id":"r1","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}

data: [DONE]

`
	deltas, final := runResponsesGrammarSSE(t, sse)
	assertDeltas(t, deltas, []string{`{"query":"SELECT 2`, `"}`})
	assertGrammarArgs(t, final, "query", "SELECT 2")
}

func runResponsesGrammarSSE(t *testing.T, sse string) ([]string, *ai.AssistantMessage) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, sse)
	}))
	defer server.Close()

	model := grammarResponsesModel(`{"supportsOpenAIGrammarTools":true}`)
	model.BaseURL = server.URL
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}, Tools: []ai.Tool{grammarSamplingTool()}}
	stream := StreamOpenAIResponses(context.Background(), model, req,
		&OpenAIResponsesOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "sk"}}})

	var deltas []string
	for ev := range stream.Events() {
		if ev.Type == ai.EventToolCallDelta {
			deltas = append(deltas, ev.Delta)
		}
	}
	return deltas, stream.Result()
}

func assertDeltas(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("deltas = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delta %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func assertGrammarArgs(t *testing.T, final *ai.AssistantMessage, property, want string) {
	t.Helper()
	for _, c := range final.Content {
		if tc, ok := c.(ai.ToolCall); ok {
			if got, _ := tc.Arguments[property].(string); got != want {
				t.Fatalf("arguments[%q] = %q, want %q", property, got, want)
			}
			return
		}
	}
	t.Fatalf("no tool call in final message (stop=%s err=%s)", final.StopReason, final.ErrorMessage)
}

// pi 02bd2d1c6: namespaces ride on function_call / custom_tool_call items and
// are echoed back on replay, but only for a call the current model could have
// made itself — its own call, or a deferred tool this request is loading.
func TestResponsesNamespaceParsedFromStream(t *testing.T) {
	const fnAdded = `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"calc","arguments":""%s}}`
	const fnDone = `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"calc","arguments":"{\"x\":7}"%s}}`
	const ctcAdded = `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"ctc_call","name":"gram","input":""%s}}`
	const ctcDone = `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"ctc_call","name":"gram","input":"SELECT \"a\""%s}}`
	const ns = `,"namespace":"mcp_math"`

	body := func(added, done string) string {
		return "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\"}}\n\n" +
			added + "\n\n" + done + "\n\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\"}}\n\n"
	}

	fnModel := reasoningModel()
	fnReq := ai.Context{
		Messages: []ai.Message{ai.NewUserText("hi", 1)},
		Tools:    []ai.Tool{{Name: "calc", Description: "calc", Parameters: ai.Object(ai.Prop("x", ai.Integer()))}},
	}
	grammarModel := grammarResponsesModel(`{"supportsOpenAIGrammarTools":true}`)
	grammarReq := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}, Tools: []ai.Tool{grammarSamplingTool()}}

	for _, tc := range []struct {
		name  string
		model *ai.Model
		req   ai.Context
		sse   string
	}{
		{"function_call, both items", fnModel, fnReq, body(fmt.Sprintf(fnAdded, ns), fmt.Sprintf(fnDone, ns))},
		// Only the done item carries it: this is what the != "" guards are for.
		{"function_call, done only", fnModel, fnReq, body(fmt.Sprintf(fnAdded, ""), fmt.Sprintf(fnDone, ns))},
		{"function_call, added only", fnModel, fnReq, body(fmt.Sprintf(fnAdded, ns), fmt.Sprintf(fnDone, ""))},
		{"custom_tool_call, both items", grammarModel, grammarReq, body(fmt.Sprintf(ctcAdded, ns), fmt.Sprintf(ctcDone, ns))},
		{"custom_tool_call, done only", grammarModel, grammarReq, body(fmt.Sprintf(ctcAdded, ""), fmt.Sprintf(ctcDone, ns))},
		{"custom_tool_call, added only", grammarModel, grammarReq, body(fmt.Sprintf(ctcAdded, ns), fmt.Sprintf(ctcDone, ""))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			final := runResponsesSSE(t, tc.model, tc.req, tc.sse)
			var tool *ai.ToolCall
			for _, c := range final.Content {
				if call, ok := c.(ai.ToolCall); ok {
					tool = &call
				}
			}
			if tool == nil {
				t.Fatalf("no tool call (stop=%s err=%s)", final.StopReason, final.ErrorMessage)
			}
			if tool.Namespace != "mcp_math" {
				t.Fatalf("namespace = %q, want mcp_math", tool.Namespace)
			}
		})
	}
}

// The model's own call replays with its namespace intact.
func TestResponsesNamespaceReplayedForSameModel(t *testing.T) {
	model := reasoningModel() // gpt-5 / openai / openai-responses
	req := ai.Context{Messages: []ai.Message{
		ai.NewUserText("hi", 1),
		ai.AssistantMessage{
			Content: ai.ContentList{ai.ToolCall{
				ID: "call_1|fc_1", Name: "calc", Arguments: map[string]any{}, Namespace: "mcp_math",
			}},
			Api:        ai.APIOpenAIResponses,
			Provider:   "openai",
			Model:      "gpt-5",
			StopReason: ai.StopToolUse,
		},
		ai.ToolResultMessage{ToolCallID: "call_1|fc_1", ToolName: "calc", Content: ai.ContentList{ai.TextContent{Text: "ok"}}, Timestamp: 2},
	}}
	fc := findResponsesItem(mustResponsesInput(t, model, req), "function_call")
	if fc == nil {
		t.Fatalf("no function_call item")
	}
	if fc["namespace"] != "mcp_math" {
		t.Fatalf("namespace = %v, want mcp_math", fc["namespace"])
	}
}

// A different model's call cannot claim this model's namespace.
func TestResponsesNamespaceDroppedForDifferentModel(t *testing.T) {
	model := reasoningModel()
	req := ai.Context{Messages: []ai.Message{
		ai.NewUserText("hi", 1),
		ai.AssistantMessage{
			Content: ai.ContentList{ai.ToolCall{
				ID: "call_1|fc_1", Name: "calc", Arguments: map[string]any{}, Namespace: "mcp_math",
			}},
			Api:        ai.APIOpenAIResponses,
			Provider:   "openai",
			Model:      "gpt-4.1", // different model id, same provider/api
			StopReason: ai.StopToolUse,
		},
		ai.ToolResultMessage{ToolCallID: "call_1|fc_1", ToolName: "calc", Content: ai.ContentList{ai.TextContent{Text: "ok"}}, Timestamp: 2},
	}}
	fc := findResponsesItem(mustResponsesInput(t, model, req), "function_call")
	if fc == nil {
		t.Fatalf("no function_call item")
	}
	if ns, has := fc["namespace"]; has {
		t.Fatalf("cross-model namespace should be dropped, got %v", ns)
	}
}

// A call from another provider or another api is not this model's call either,
// so its namespace cannot be replayed (pi gates all three on provider+api+id).
func TestResponsesNamespaceDroppedAcrossProviderAndApi(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider string
		api      string
	}{
		{"different provider", "azure", ai.APIOpenAIResponses},
		{"different api", "openai", ai.APIOpenAICodexResponses},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := reasoningModel()
			req := ai.Context{Messages: []ai.Message{
				ai.NewUserText("hi", 1),
				ai.AssistantMessage{
					Content: ai.ContentList{ai.ToolCall{
						ID: "call_1|fc_1", Name: "calc", Arguments: map[string]any{}, Namespace: "mcp_math",
					}},
					Api:        tc.api,
					Provider:   tc.provider,
					Model:      "gpt-5", // same model id, so only provider/api can rule it out
					StopReason: ai.StopToolUse,
				},
				ai.ToolResultMessage{ToolCallID: "call_1|fc_1", ToolName: "calc", Content: ai.ContentList{ai.TextContent{Text: "ok"}}, Timestamp: 2},
			}}
			fc := findResponsesItem(mustResponsesInput(t, model, req), "function_call")
			if fc == nil {
				t.Fatalf("no function_call item")
			}
			if ns, has := fc["namespace"]; has {
				t.Fatalf("namespace must not cross provider/api, got %v", ns)
			}
		})
	}
}

// DELIBERATE DIVERGENCE, pinned so it cannot drift silently: pi guards on
// `namespace !== undefined`, so a provider-sent empty string replays there as
// `"namespace": ""`. Go models the field as a plain string (the ThoughtSignature
// precedent) and drops it. Recorded in docs/UPSTREAM.md; see the 2026-08-08 entry.
func TestResponsesEmptyNamespaceDroppedDivergence(t *testing.T) {
	model := reasoningModel()
	req := ai.Context{Messages: []ai.Message{
		ai.NewUserText("hi", 1),
		ai.AssistantMessage{
			Content: ai.ContentList{ai.ToolCall{
				ID: "call_1|fc_1", Name: "calc", Arguments: map[string]any{}, Namespace: "",
			}},
			Api:        ai.APIOpenAIResponses,
			Provider:   "openai",
			Model:      "gpt-5",
			StopReason: ai.StopToolUse,
		},
		ai.ToolResultMessage{ToolCallID: "call_1|fc_1", ToolName: "calc", Content: ai.ContentList{ai.TextContent{Text: "ok"}}, Timestamp: 2},
	}}
	fc := findResponsesItem(mustResponsesInput(t, model, req), "function_call")
	if fc == nil {
		t.Fatalf("no function_call item")
	}
	if ns, has := fc["namespace"]; has {
		t.Fatalf("pi would emit \"\" here and we deliberately do not; got %v", ns)
	}
}

// ...unless the tool is one this request defers, in which case the namespace is
// the loaded tool's and replays regardless of which model called it.
func TestResponsesNamespaceReplayedForDeferredTool(t *testing.T) {
	model := reasoningModel()
	model.Compat = json.RawMessage(`{"supportsToolSearch":true}`)
	req := ai.Context{
		Tools: []ai.Tool{{Name: "calc", Description: "calc", Parameters: ai.Object(ai.Prop("x", ai.Integer()))}},
		Messages: []ai.Message{
			ai.NewUserText("hi", 1),
			// The marker precedes the call, so "calc" is deferred, not immediate.
			ai.ToolResultMessage{
				ToolCallID: "call_0|fc_0", ToolName: "loader", AddedToolNames: []string{"calc"},
				Content: ai.ContentList{ai.TextContent{Text: "loaded"}}, Timestamp: 2,
			},
			ai.AssistantMessage{
				Content: ai.ContentList{ai.ToolCall{
					ID: "call_1|fc_1", Name: "calc", Arguments: map[string]any{}, Namespace: "mcp_math",
				}},
				Api:        ai.APIOpenAIResponses,
				Provider:   "openai",
				Model:      "gpt-4.1", // different model id
				StopReason: ai.StopToolUse,
			},
			ai.ToolResultMessage{ToolCallID: "call_1|fc_1", ToolName: "calc", Content: ai.ContentList{ai.TextContent{Text: "ok"}}, Timestamp: 3},
		},
	}
	fc := findResponsesItem(mustResponsesInput(t, model, req), "function_call")
	if fc == nil {
		t.Fatalf("no function_call item")
	}
	if fc["namespace"] != "mcp_math" {
		t.Fatalf("deferred-tool namespace = %v, want mcp_math", fc["namespace"])
	}
}
