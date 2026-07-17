package providers

import (
	"encoding/json"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// Ported from pi packages/ai/test/deferred-tools.test.ts (upstream 3d8f7435),
// covering the Anthropic tool_reference and OpenAI Responses tool_search request
// surfaces. Built directly against buildAnthropicParams / buildResponsesParams so
// the request bodies are inspected exactly like pi captures its payload.

func dtTool(name string) ai.Tool {
	return ai.Tool{Name: name, Description: "The " + name + " tool", Parameters: ai.Object(ai.Prop("value", ai.String()))}
}

func dtAnthropicModel(id string) *ai.Model {
	return &ai.Model{ID: id, Api: ai.APIAnthropicMessages, Provider: "anthropic", Input: []string{"text", "image"}, MaxTokens: 4096}
}

// dtAnthropicContext mirrors makeContext: a base_tool call, its result carrying
// the added markers, and a trailing user turn. The assistant is same-model so the
// tool call survives transformMessages.
func dtAnthropicContext(model *ai.Model, tools []ai.Tool, added ...string) ai.Context {
	return ai.Context{
		Messages: []ai.Message{
			ai.NewUserText("hi", 1),
			&ai.AssistantMessage{
				Api: model.Api, Provider: model.Provider, Model: model.ID,
				Content:    ai.ContentList{ai.ToolCall{ID: "call_1", Name: "base_tool", Arguments: map[string]any{}}},
				StopReason: ai.StopToolUse, Timestamp: 2,
			},
			ai.ToolResultMessage{
				ToolCallID: "call_1", ToolName: "base_tool",
				Content: ai.ContentList{ai.TextContent{Text: "done"}}, AddedToolNames: added, Timestamp: 3,
			},
			ai.NewUserText("next", 4),
		},
		Tools: tools,
	}
}

func dtTools(body map[string]any) []map[string]any {
	raw, ok := body["tools"].([]map[string]any)
	if !ok {
		return nil
	}
	return raw
}

func dtToolNames(tools []map[string]any) []string {
	out := make([]string, len(tools))
	for i, t := range tools {
		out[i], _ = t["name"].(string)
	}
	return out
}

// dtAnthropicToolResultContent finds the user message that carries the
// tool_result blocks and returns its content array.
func dtAnthropicToolResultContent(t *testing.T, body map[string]any) []any {
	t.Helper()
	msgs, _ := body["messages"].([]map[string]any)
	for _, m := range msgs {
		content, ok := m["content"].([]any)
		if !ok {
			continue
		}
		for _, block := range content {
			if b, ok := block.(map[string]any); ok && b["type"] == "tool_result" {
				return content
			}
		}
	}
	t.Fatalf("no tool_result content in body")
	return nil
}

func dtFirstToolResult(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	for _, block := range dtAnthropicToolResultContent(t, body) {
		if b, ok := block.(map[string]any); ok && b["type"] == "tool_result" {
			return b
		}
	}
	t.Fatalf("no tool_result block")
	return nil
}

func TestDeferredToolsAnthropicLoadsAtMarker(t *testing.T) {
	model := dtAnthropicModel("claude-opus-4-6")
	ctx := dtAnthropicContext(model, []ai.Tool{dtTool("base_tool"), dtTool("late_tool")}, "late_tool")
	body := buildAnthropicParams(model, ctx, false, &AnthropicOptions{})

	tools := dtTools(body)
	if got := dtToolNames(tools); len(got) != 2 || got[0] != "base_tool" || got[1] != "late_tool" {
		t.Fatalf("tools = %v, want [base_tool late_tool]", got)
	}
	if tools[0]["defer_loading"] != nil {
		t.Fatalf("base_tool must not defer_loading")
	}
	if tools[1]["defer_loading"] != true {
		t.Fatalf("late_tool must carry defer_loading:true, got %v", tools[1]["defer_loading"])
	}
	content, _ := dtFirstToolResult(t, body)["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("tool_result content = %v, want single tool_reference", content)
	}
	ref, _ := content[0].(map[string]any)
	if ref["type"] != "tool_reference" || ref["tool_name"] != "late_tool" {
		t.Fatalf("reference = %v, want tool_reference late_tool", ref)
	}
}

func TestDeferredToolsAnthropicSiblingDisplacement(t *testing.T) {
	model := dtAnthropicModel("claude-opus-4-6")
	ctx := dtAnthropicContext(model, []ai.Tool{dtTool("base_tool"), dtTool("late_tool")}, "late_tool")
	// Two tool calls; the first result carries text+image content plus the marker,
	// the second is a plain follow-up result.
	assistant := ctx.Messages[1].(*ai.AssistantMessage)
	assistant.Content = ai.ContentList{
		ai.ToolCall{ID: "call_1", Name: "base_tool", Arguments: map[string]any{}},
		ai.ToolCall{ID: "call_2", Name: "base_tool", Arguments: map[string]any{}},
	}
	ctx.Messages[2] = ai.ToolResultMessage{
		ToolCallID: "call_1", ToolName: "base_tool", AddedToolNames: []string{"late_tool"}, Timestamp: 3,
		Content: ai.ContentList{ai.TextContent{Text: "work completed"}, ai.ImageContent{MimeType: "image/png", Data: "aW1hZ2U="}},
	}
	// Insert the second result before the trailing user turn.
	rest := append([]ai.Message{ai.ToolResultMessage{
		ToolCallID: "call_2", ToolName: "base_tool", Content: ai.ContentList{ai.TextContent{Text: "second result"}}, Timestamp: 3,
	}}, ctx.Messages[3:]...)
	ctx.Messages = append(ctx.Messages[:3], rest...)

	body := buildAnthropicParams(model, ctx, false, &AnthropicOptions{})
	content := dtAnthropicToolResultContent(t, body)
	if len(content) != 4 {
		t.Fatalf("content len = %d, want 4 (2 tool_result + text + image): %#v", len(content), content)
	}
	tr0 := content[0].(map[string]any)
	if tr0["type"] != "tool_result" || tr0["tool_use_id"] != "call_1" {
		t.Fatalf("block0 = %v, want tool_result call_1", tr0)
	}
	if refs, _ := tr0["content"].([]any); len(refs) != 1 || refs[0].(map[string]any)["type"] != "tool_reference" {
		t.Fatalf("block0 content = %v, want [tool_reference]", tr0["content"])
	}
	tr1 := content[1].(map[string]any)
	if tr1["type"] != "tool_result" || tr1["tool_use_id"] != "call_2" || tr1["content"] != "second result" {
		t.Fatalf("block1 = %v, want tool_result call_2 content 'second result'", tr1)
	}
	if sib := content[2].(map[string]any); sib["type"] != "text" || sib["text"] != "work completed" {
		t.Fatalf("block2 = %v, want displaced text 'work completed'", sib)
	}
	if img := content[3].(map[string]any); img["type"] != "image" {
		t.Fatalf("block3 = %v, want displaced image", img)
	}
}

func TestDeferredToolsAnthropicFromOpenAIHistory(t *testing.T) {
	model := dtAnthropicModel("claude-opus-4-8")
	ctx := dtAnthropicContext(model, []ai.Tool{dtTool("base_tool"), dtTool("late_tool")}, "late_tool")
	// The tool call came from an OpenAI turn before switching to Anthropic.
	assistant := ctx.Messages[1].(*ai.AssistantMessage)
	assistant.Api = ai.APIOpenAIResponses
	assistant.Provider = "openai"
	assistant.Model = "gpt-5.4"

	body := buildAnthropicParams(model, ctx, false, &AnthropicOptions{})
	tools := dtTools(body)
	if got := dtToolNames(tools); len(got) != 2 || got[1] != "late_tool" || tools[1]["defer_loading"] != true {
		t.Fatalf("tools = %v (defer=%v), want late_tool deferred", got, tools[1]["defer_loading"])
	}
	content, _ := dtFirstToolResult(t, body)["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["tool_name"] != "late_tool" {
		t.Fatalf("content = %v, want [tool_reference late_tool]", content)
	}
}

func TestDeferredToolsAnthropicNotResurrected(t *testing.T) {
	model := dtAnthropicModel("claude-opus-4-6")
	ctx := dtAnthropicContext(model, []ai.Tool{dtTool("base_tool")}, "late_tool")
	body := buildAnthropicParams(model, ctx, false, &AnthropicOptions{})
	if got := dtToolNames(dtTools(body)); len(got) != 1 || got[0] != "base_tool" {
		t.Fatalf("tools = %v, want [base_tool]", got)
	}
	// With no reference emitted the block keeps its ordinary string content.
	if content := dtFirstToolResult(t, body)["content"]; content != "done" {
		t.Fatalf("content = %#v, want plain string 'done'", content)
	}
}

func TestDeferredToolsAnthropicUsedBeforeMarker(t *testing.T) {
	model := dtAnthropicModel("claude-opus-4-6")
	ctx := dtAnthropicContext(model, []ai.Tool{dtTool("base_tool"), dtTool("late_tool")}, "late_tool")
	ctx.Messages[1].(*ai.AssistantMessage).Content = ai.ContentList{ai.ToolCall{ID: "call_1", Name: "late_tool", Arguments: map[string]any{}}}
	body := buildAnthropicParams(model, ctx, false, &AnthropicOptions{})
	tools := dtTools(body)
	if got := dtToolNames(tools); len(got) != 2 || got[0] != "base_tool" || got[1] != "late_tool" {
		t.Fatalf("tools = %v, want [base_tool late_tool]", got)
	}
	for _, tool := range tools {
		if tool["defer_loading"] != nil {
			t.Fatalf("no tool may defer_loading when used before marker: %v", tool)
		}
	}
}

func TestDeferredToolsAnthropicUnsupportedModels(t *testing.T) {
	for _, id := range []string{"claude-haiku-4-5", "claude-sonnet-4-20250514"} {
		model := dtAnthropicModel(id)
		ctx := dtAnthropicContext(model, []ai.Tool{dtTool("base_tool"), dtTool("late_tool")}, "late_tool")
		body := buildAnthropicParams(model, ctx, false, &AnthropicOptions{})
		tools := dtTools(body)
		if got := dtToolNames(tools); len(got) != 2 || got[0] != "base_tool" || got[1] != "late_tool" {
			t.Fatalf("%s tools = %v, want normal list", id, got)
		}
		for _, tool := range tools {
			if tool["defer_loading"] != nil {
				t.Fatalf("%s must not defer_loading: %v", id, tool)
			}
		}
	}
}

func TestDeferredToolsAnthropicPromoteWhenAllDeferred(t *testing.T) {
	model := dtAnthropicModel("claude-opus-4-6")
	ctx := dtAnthropicContext(model, []ai.Tool{dtTool("late_tool")}, "late_tool")
	body := buildAnthropicParams(model, ctx, false, &AnthropicOptions{})
	tools := dtTools(body)
	if got := dtToolNames(tools); len(got) != 1 || got[0] != "late_tool" {
		t.Fatalf("tools = %v, want [late_tool]", got)
	}
	if tools[0]["defer_loading"] != nil {
		t.Fatalf("promoted tool must not defer_loading")
	}
	if content := dtFirstToolResult(t, body)["content"]; content != "done" {
		t.Fatalf("content = %#v, want plain string 'done' (no reference)", content)
	}
}

func TestDeferredToolsAnthropicCompatOverride(t *testing.T) {
	// A non-Anthropic provider defaults supportsToolReferences false; the explicit
	// compat override turns native deferral back on.
	model := dtAnthropicModel("claude-opus-4-6")
	model.Provider = "anthropic-proxy"
	model.Compat = json.RawMessage(`{"supportsToolReferences":true}`)
	ctx := dtAnthropicContext(model, []ai.Tool{dtTool("base_tool"), dtTool("late_tool")}, "late_tool")
	body := buildAnthropicParams(model, ctx, false, &AnthropicOptions{})
	tools := dtTools(body)
	if tools[1]["name"] != "late_tool" || tools[1]["defer_loading"] != true {
		t.Fatalf("override did not defer late_tool: %v", tools[1])
	}
}

func TestDeferredToolsAnthropicOAuthCanonicalMarker(t *testing.T) {
	// Marker "Read" must match the "read" tool once OAuth canonicalization runs.
	model := dtAnthropicModel("claude-opus-4-6")
	ctx := dtAnthropicContext(model, []ai.Tool{dtTool("base_tool"), dtTool("read")}, "Read")
	body := buildAnthropicParams(model, ctx, true, &AnthropicOptions{})
	tools := dtTools(body)
	if got := dtToolNames(tools); len(got) != 2 || got[1] != "Read" || tools[1]["defer_loading"] != true {
		t.Fatalf("tools = %v (defer=%v), want Read deferred", got, tools[1]["defer_loading"])
	}
	content, _ := dtFirstToolResult(t, body)["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["tool_name"] != "Read" {
		t.Fatalf("content = %v, want [tool_reference Read]", content)
	}
}

func TestDefaultSupportsToolReferencesGating(t *testing.T) {
	cases := []struct {
		provider string
		id       string
		want     bool
	}{
		{"anthropic", "claude-opus-4-6", true},
		{"anthropic", "claude-opus-4-8", true},
		{"anthropic", "claude-sonnet-4-5", true},
		{"anthropic", "claude-fable-5", true},
		{"anthropic", "claude-opus-4-5-20250101", true},
		{"anthropic", "claude-haiku-4-5", false},
		{"anthropic", "claude-sonnet-4-20250514", false}, // date suffix -> minor 0
		{"anthropic", "claude-opus-4-4", false},          // pins the minor >= 5 boundary
		{"anthropic", "claude-opus-4-1", false},
		{"anthropic", "claude-opus-4", false},
		{"anthropic", "claude-3-5-sonnet-20241022", false},
		{"openai", "claude-opus-4-6", false}, // wrong provider
	}
	for _, c := range cases {
		m := &ai.Model{ID: c.id, Provider: ai.ProviderId(c.provider), Api: ai.APIAnthropicMessages}
		if got := defaultSupportsToolReferences(m); got != c.want {
			t.Errorf("defaultSupportsToolReferences(%s/%s) = %v, want %v", c.provider, c.id, got, c.want)
		}
	}
}

// --- OpenAI Responses tool search ---

func dtResponsesModel(id string, toolSearch bool) *ai.Model {
	m := &ai.Model{ID: id, Api: ai.APIOpenAIResponses, Provider: "openai", Input: []string{"text"}, MaxTokens: 4096}
	if toolSearch {
		m.Compat = json.RawMessage(`{"supportsToolSearch":true}`)
	}
	return m
}

func dtResponsesContext(model *ai.Model, tools []ai.Tool, added ...string) ai.Context {
	return ai.Context{
		Messages: []ai.Message{
			ai.NewUserText("hi", 1),
			&ai.AssistantMessage{
				Api: model.Api, Provider: model.Provider, Model: model.ID,
				Content:    ai.ContentList{ai.ToolCall{ID: "call_1", Name: "base_tool", Arguments: map[string]any{}}},
				StopReason: ai.StopToolUse, Timestamp: 2,
			},
			ai.ToolResultMessage{
				ToolCallID: "call_1", ToolName: "base_tool",
				Content: ai.ContentList{ai.TextContent{Text: "done"}}, AddedToolNames: added, Timestamp: 3,
			},
			ai.NewUserText("next", 4),
		},
		Tools: tools,
	}
}

func dtResponsesInputItems(t *testing.T, body map[string]any) []any {
	t.Helper()
	in, ok := body["input"].([]any)
	if !ok {
		t.Fatalf("input not []any: %T", body["input"])
	}
	return in
}

func dtResponsesToolNames(body map[string]any) []string {
	tools, _ := body["tools"].([]map[string]any)
	out := make([]string, len(tools))
	for i, tool := range tools {
		out[i], _ = tool["name"].(string)
	}
	return out
}

func TestDeferredToolsResponsesToolSearch(t *testing.T) {
	model := dtResponsesModel("gpt-5.4", true)
	ctx := dtResponsesContext(model, []ai.Tool{dtTool("base_tool"), dtTool("late_tool")}, "late_tool")
	body, err := buildResponsesParams(model, ctx, &OpenAIResponsesOptions{})
	if err != nil {
		t.Fatalf("buildResponsesParams: %v", err)
	}
	if got := dtResponsesToolNames(body); len(got) != 1 || got[0] != "base_tool" {
		t.Fatalf("tools = %v, want [base_tool]", got)
	}
	var call, out map[string]any
	for _, item := range dtResponsesInputItems(t, body) {
		m, _ := item.(map[string]any)
		switch m["type"] {
		case "tool_search_call":
			call = m
		case "tool_search_output":
			out = m
		}
	}
	if call == nil || out == nil {
		t.Fatalf("missing tool_search items: call=%v out=%v", call, out)
	}
	if call["execution"] != "client" || call["status"] != "completed" {
		t.Fatalf("tool_search_call = %v, want client/completed", call)
	}
	if call["call_id"] != out["call_id"] {
		t.Fatalf("call_id mismatch: %v vs %v", call["call_id"], out["call_id"])
	}
	// call_id is the shortHash of "<toolCallId>:<names joined by comma>".
	wantID := "pi_tool_load_" + shortHash("call_1:late_tool")
	if call["call_id"] != wantID {
		t.Fatalf("call_id = %v, want %v", call["call_id"], wantID)
	}
	args, _ := call["arguments"].(map[string]any)
	if args["query"] != "late_tool" || args["limit"] != 1 {
		t.Fatalf("arguments = %v, want query 'late_tool' limit 1", args)
	}
	outTools, _ := out["tools"].([]map[string]any)
	if len(outTools) != 1 || outTools[0]["name"] != "late_tool" || outTools[0]["defer_loading"] != true {
		t.Fatalf("tool_search_output tools = %v, want [late_tool defer_loading]", outTools)
	}
}

func TestDeferredToolsResponsesDisabled(t *testing.T) {
	model := dtResponsesModel("gpt-5.4", false)
	ctx := dtResponsesContext(model, []ai.Tool{dtTool("base_tool"), dtTool("late_tool")}, "late_tool")
	body, err := buildResponsesParams(model, ctx, &OpenAIResponsesOptions{})
	if err != nil {
		t.Fatalf("buildResponsesParams: %v", err)
	}
	if got := dtResponsesToolNames(body); len(got) != 2 || got[0] != "base_tool" || got[1] != "late_tool" {
		t.Fatalf("tools = %v, want normal [base_tool late_tool]", got)
	}
	for _, item := range dtResponsesInputItems(t, body) {
		if m, _ := item.(map[string]any); m["type"] == "tool_search_output" || m["type"] == "tool_search_call" {
			t.Fatalf("unexpected tool_search item when disabled: %v", m)
		}
	}
}

// ---- Kimi deferred tools on openai-completions (upstream f16b4e0c) ----

// dtKimiModel mirrors pi's makeKimiModel: an openai-completions moonshotai
// model, optionally with compat.deferredToolsMode="kimi".
func dtKimiModel(deferred bool) *ai.Model {
	m := &ai.Model{
		ID: "deferred-tools-model", Name: "Deferred Tools Model",
		Api: ai.APIOpenAICompletions, Provider: "moonshotai",
		BaseURL: "http://127.0.0.1:9/v1", Input: []string{"text"},
		ContextWindow: 128000, MaxTokens: 4096,
	}
	if deferred {
		m.Compat = json.RawMessage(`{"deferredToolsMode":"kimi"}`)
	}
	return m
}

// dtKimiContext mirrors pi's makeContext for the completions provider.
func dtKimiContext(model *ai.Model, tools []ai.Tool, added ...string) ai.Context {
	return ai.Context{
		Messages: []ai.Message{
			ai.NewUserText("hi", 1),
			&ai.AssistantMessage{
				Api: model.Api, Provider: model.Provider, Model: model.ID,
				Content:    ai.ContentList{ai.ToolCall{ID: "call_1", Name: "base_tool", Arguments: map[string]any{}}},
				StopReason: ai.StopToolUse, Timestamp: 2,
			},
			ai.ToolResultMessage{
				ToolCallID: "call_1", ToolName: "base_tool",
				Content: ai.ContentList{ai.TextContent{Text: "done"}}, AddedToolNames: added, Timestamp: 3,
			},
			ai.NewUserText("next", 4),
		},
		Tools: tools,
	}
}

// dtCompletionsToolNames extracts function names from a completions tools array
// (top-level params or a Kimi system message).
func dtCompletionsToolNames(tools any) []string {
	var names []string
	list, _ := tools.([]map[string]any)
	if list == nil {
		if anyList, ok := tools.([]any); ok {
			for _, item := range anyList {
				if m, ok := item.(map[string]any); ok {
					list = append(list, m)
				}
			}
		}
	}
	for _, tool := range list {
		if fn, ok := tool["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok {
				names = append(names, name)
			}
		}
	}
	return names
}

func dtCompletionsMessages(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["messages"].([]map[string]any)
	if !ok {
		t.Fatalf("messages missing or wrong type: %T", body["messages"])
	}
	return raw
}

// TestDeferredToolsKimiSystemDefinitions mirrors pi "serializes Kimi deferred
// tools as system tool definitions": the deferred tool leaves the top-level
// tools param and lands in a system message after its tool-result run.
func TestDeferredToolsKimiSystemDefinitions(t *testing.T) {
	model := dtKimiModel(true)
	ctx := dtKimiContext(model, []ai.Tool{dtTool("base_tool"), dtTool("late_tool")}, "late_tool")
	body := buildOpenAIParams(model, ctx, &OpenAIOptions{})

	if got := dtCompletionsToolNames(body["tools"]); len(got) != 1 || got[0] != "base_tool" {
		t.Fatalf("top-level tools = %v, want [base_tool]", got)
	}
	messages := dtCompletionsMessages(t, body)
	toolResultIndex, systemToolIndex := -1, -1
	for i, m := range messages {
		if m["role"] == "tool" && toolResultIndex < 0 {
			toolResultIndex = i
		}
		if _, ok := m["tools"]; ok && systemToolIndex < 0 {
			systemToolIndex = i
		}
	}
	if toolResultIndex < 0 {
		t.Fatal("no tool-result message serialized")
	}
	if systemToolIndex <= toolResultIndex {
		t.Fatalf("system tools message must follow the tool result: tool=%d system=%d", toolResultIndex, systemToolIndex)
	}
	sys := messages[systemToolIndex]
	if sys["role"] != "system" {
		t.Fatalf("tools carrier role = %v, want system", sys["role"])
	}
	if _, hasContent := sys["content"]; hasContent {
		t.Fatalf("Kimi system tools message must omit the content field: %v", sys)
	}
	if got := dtCompletionsToolNames(sys["tools"]); len(got) != 1 || got[0] != "late_tool" {
		t.Fatalf("system message tools = %v, want [late_tool]", got)
	}
}

// TestDeferredToolsKimiBatchedResults mirrors pi "emits Kimi deferred schemas
// after all tool results in a batch": one system message follows the whole
// tool-result run, carrying both introduced tools in introduction order.
func TestDeferredToolsKimiBatchedResults(t *testing.T) {
	model := dtKimiModel(true)
	ctx := dtKimiContext(model, []ai.Tool{dtTool("base_tool"), dtTool("late_tool"), dtTool("later_tool")}, "late_tool")
	// Insert a second tool result (call_2) into the run, like pi's splice(3, 0, ...).
	second := ai.ToolResultMessage{
		ToolCallID: "call_2", ToolName: "base_tool",
		Content: ai.ContentList{ai.TextContent{Text: "done"}}, AddedToolNames: []string{"later_tool"}, Timestamp: 3,
	}
	msgs := ctx.Messages
	ctx.Messages = append(msgs[:3:3], append([]ai.Message{second}, msgs[3:]...)...)

	body := buildOpenAIParams(model, ctx, &OpenAIOptions{})
	messages := dtCompletionsMessages(t, body)
	var roles []string
	for _, m := range messages {
		roles = append(roles, m["role"].(string))
	}
	want := []string{"user", "assistant", "tool", "tool", "system", "user"}
	if len(roles) != len(want) {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("roles = %v, want %v", roles, want)
		}
	}
	if got := dtCompletionsToolNames(messages[4]["tools"]); len(got) != 2 || got[0] != "late_tool" || got[1] != "later_tool" {
		t.Fatalf("batched system tools = %v, want [late_tool later_tool]", got)
	}
}

// TestDeferredToolsKimiDisabled mirrors pi "leaves OpenAI Completions tools
// unchanged without Kimi mode".
func TestDeferredToolsKimiDisabled(t *testing.T) {
	model := dtKimiModel(false)
	ctx := dtKimiContext(model, []ai.Tool{dtTool("base_tool"), dtTool("late_tool")}, "late_tool")
	body := buildOpenAIParams(model, ctx, &OpenAIOptions{})

	if got := dtCompletionsToolNames(body["tools"]); len(got) != 2 || got[0] != "base_tool" || got[1] != "late_tool" {
		t.Fatalf("tools = %v, want unchanged [base_tool late_tool]", got)
	}
	for _, m := range dtCompletionsMessages(t, body) {
		if _, ok := m["tools"]; ok {
			t.Fatalf("no message may carry tools without Kimi mode: %v", m)
		}
	}
}

// TestDeferredToolsKimiAllDeferred pins the fused buildParams structure: when
// every context tool is deferred, the active list is empty and the tool-history
// branch sends the empty tools array (pi: activeTools.length > 0 falls through
// to hasToolHistory).
func TestDeferredToolsKimiAllDeferred(t *testing.T) {
	model := dtKimiModel(true)
	ctx := dtKimiContext(model, []ai.Tool{dtTool("late_tool")}, "late_tool")
	body := buildOpenAIParams(model, ctx, &OpenAIOptions{})

	tools, ok := body["tools"].([]map[string]any)
	if !ok || len(tools) != 0 {
		t.Fatalf("all-deferred must fall through to the empty tool-history array, got %v", body["tools"])
	}
}

// TestDeferredToolsKimiCatalogLive pins the 0.80.10 regen going live: the
// moonshotai kimi-k3 catalog entries carry compat.deferredToolsMode="kimi"
// (upstream 70c57632 data + f16b4e0c behavior), so the deferred path is no
// longer latent for them.
func TestDeferredToolsKimiCatalogLive(t *testing.T) {
	ai.LoadBuiltinModels()
	for _, provider := range []string{"moonshotai", "moonshotai-cn"} {
		model := ai.GetModel(provider, "kimi-k3")
		if model == nil {
			t.Fatalf("%s/kimi-k3 not in catalog", provider)
		}
		if got := getOpenAICompat(model).DeferredToolsMode; got != "kimi" {
			t.Fatalf("%s/kimi-k3 deferredToolsMode = %q, want kimi", provider, got)
		}
	}
}
