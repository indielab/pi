package ai

import (
	"strings"
	"testing"
)

// Ported from pi packages/ai/test/deferred-tools.test.ts (upstream 3d8f7435),
// covering the provider-agnostic SplitDeferredTools core and the estimate delta.

func dtTool(name string) Tool {
	return Tool{Name: name, Description: "The " + name + " tool", Parameters: Object(Prop("value", String()))}
}

func dtAssistantCall(name string) AssistantMessage {
	return AssistantMessage{
		Content:    ContentList{ToolCall{ID: "call_1", Name: name, Arguments: map[string]any{}}},
		StopReason: StopToolUse,
		Timestamp:  2,
	}
}

func dtToolResult(added ...string) ToolResultMessage {
	return ToolResultMessage{
		ToolCallID:     "call_1",
		ToolName:       "base_tool",
		Content:        ContentList{TextContent{Text: "done"}},
		AddedToolNames: added,
		Timestamp:      3,
	}
}

func toolNames(tools []Tool) []string {
	out := make([]string, len(tools))
	for i, t := range tools {
		out[i] = t.Name
	}
	return out
}

func TestSplitDeferredToolsDisabledReturnsAllImmediate(t *testing.T) {
	ctx := Context{
		Messages: []Message{dtAssistantCall("base_tool"), dtToolResult("late_tool")},
		Tools:    []Tool{dtTool("base_tool"), dtTool("late_tool")},
	}
	split := SplitDeferredTools(ctx, false, nil)
	if got := toolNames(split.Immediate); strings.Join(got, ",") != "base_tool,late_tool" {
		t.Fatalf("disabled immediate = %v, want [base_tool late_tool]", got)
	}
	if len(split.Deferred) != 0 {
		t.Fatalf("disabled deferred = %v, want none", split.Deferred)
	}
}

func TestSplitDeferredToolsMarksLateTool(t *testing.T) {
	ctx := Context{
		Messages: []Message{dtAssistantCall("base_tool"), dtToolResult("late_tool")},
		Tools:    []Tool{dtTool("base_tool"), dtTool("late_tool")},
	}
	split := SplitDeferredTools(ctx, true, nil)
	if got := toolNames(split.Immediate); strings.Join(got, ",") != "base_tool" {
		t.Fatalf("immediate = %v, want [base_tool]", got)
	}
	if got := toolNames(split.Deferred); strings.Join(got, ",") != "late_tool" {
		t.Fatalf("deferred = %v, want [late_tool]", got)
	}
	if _, ok := split.ByName["late_tool"]; !ok {
		t.Fatalf("ByName missing late_tool: %v", split.ByName)
	}
}

func TestSplitDeferredToolsUsedBeforeMarkerStaysImmediate(t *testing.T) {
	// late_tool is called (used) before its marker, so it must not be deferred.
	ctx := Context{
		Messages: []Message{dtAssistantCall("late_tool"), dtToolResult("late_tool")},
		Tools:    []Tool{dtTool("base_tool"), dtTool("late_tool")},
	}
	split := SplitDeferredTools(ctx, true, nil)
	if got := toolNames(split.Immediate); strings.Join(got, ",") != "base_tool,late_tool" {
		t.Fatalf("immediate = %v, want [base_tool late_tool]", got)
	}
	if len(split.Deferred) != 0 {
		t.Fatalf("deferred = %v, want none", split.Deferred)
	}
}

func TestSplitDeferredToolsIgnoresUnknownMarkedTool(t *testing.T) {
	// late_tool is marked but absent from Context.Tools -> nothing to defer.
	ctx := Context{
		Messages: []Message{dtAssistantCall("base_tool"), dtToolResult("late_tool")},
		Tools:    []Tool{dtTool("base_tool")},
	}
	split := SplitDeferredTools(ctx, true, nil)
	if got := toolNames(split.Immediate); strings.Join(got, ",") != "base_tool" {
		t.Fatalf("immediate = %v, want [base_tool]", got)
	}
	if len(split.Deferred) != 0 {
		t.Fatalf("deferred = %v, want none", split.Deferred)
	}
}

func TestSplitDeferredToolsDedupsByNormalizedName(t *testing.T) {
	// Two tools normalize to the same key; the later value wins, the first
	// position is kept (JS Map.set semantics).
	norm := func(name string) string {
		if strings.EqualFold(name, "read") {
			return "Read"
		}
		return name
	}
	ctx := Context{
		Messages: []Message{NewUserText("hi", 1)},
		Tools: []Tool{
			dtTool("read"),
			{Name: "Read", Description: "Canonical definition", Parameters: Object(Prop("value", String()))},
		},
	}
	split := SplitDeferredTools(ctx, true, norm)
	if len(split.Immediate) != 1 {
		t.Fatalf("immediate = %v, want single deduped tool", toolNames(split.Immediate))
	}
	if split.Immediate[0].Name != "Read" || split.Immediate[0].Description != "Canonical definition" {
		t.Fatalf("deduped tool = %+v, want canonical Read", split.Immediate[0])
	}
}

func TestSplitDeferredToolsNormalizerCanonicalizesMarker(t *testing.T) {
	// Marker "Read" must match tool "read" once the OAuth-style normalizer runs.
	norm := func(name string) string {
		if strings.EqualFold(name, "read") {
			return "Read"
		}
		return name
	}
	ctx := Context{
		Messages: []Message{dtAssistantCall("base_tool"), dtToolResult("Read")},
		Tools:    []Tool{dtTool("base_tool"), dtTool("read")},
	}
	split := SplitDeferredTools(ctx, true, norm)
	if got := toolNames(split.Deferred); strings.Join(got, ",") != "read" {
		t.Fatalf("deferred = %v, want the read tool deferred", got)
	}
}

func TestEstimateContextTokensCountsToolsMarkedAfterUsage(t *testing.T) {
	// Mirrors pi's "counts definitions marked after the latest usage checkpoint".
	assistant := AssistantMessage{
		Content:    ContentList{TextContent{Text: "done"}},
		StopReason: StopStop,
		Usage:      Usage{Input: 50, Output: 50, TotalTokens: 100},
		Timestamp:  2,
	}
	plain := estimateContextTokens(Context{
		Messages: []Message{assistant, NewUserText("hello", 4)},
		Tools:    []Tool{},
	})
	lateTool := dtTool("late_tool")
	lateTool.Description = strings.Repeat("x", 4000)
	marked := estimateContextTokens(Context{
		Messages: []Message{assistant, dtToolResult("late_tool")},
		Tools:    []Tool{lateTool},
	})

	if marked.Tokens <= plain.Tokens+500 {
		t.Fatalf("marked.Tokens=%d not > plain.Tokens=%d+500", marked.Tokens, plain.Tokens)
	}
	if marked.TrailingTokens <= plain.TrailingTokens+500 {
		t.Fatalf("marked.TrailingTokens=%d not > plain.TrailingTokens=%d+500", marked.TrailingTokens, plain.TrailingTokens)
	}
}

func TestEstimateContextTokensIgnoresMarkersWithoutMatchingTool(t *testing.T) {
	// With a usage anchor but no matching tool for the marker, tokens are unchanged
	// (guards the byte-identical no-op path for existing golden scenarios).
	assistant := AssistantMessage{
		StopReason: StopStop,
		Usage:      Usage{TotalTokens: 100},
		Timestamp:  2,
	}
	base := estimateContextTokens(Context{Messages: []Message{assistant, dtToolResult()}, Tools: []Tool{dtTool("late_tool")}})
	marked := estimateContextTokens(Context{Messages: []Message{assistant, dtToolResult("late_tool")}, Tools: []Tool{dtTool("other")}})
	if base.Tokens != marked.Tokens {
		t.Fatalf("unmatched marker changed tokens: base=%d marked=%d", base.Tokens, marked.Tokens)
	}
}
