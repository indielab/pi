package agent

import (
	"context"
	"reflect"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// runToolTurn prompts an agent whose model calls tool once with args, then
// stops, and returns the tool result message and the tool_execution_end event.
func runToolTurn(t *testing.T, tool AgentTool, args map[string]any, after func(context.Context, AfterToolCallContext) *AfterToolCallResult) (ai.ToolResultMessage, AgentEvent) {
	t.Helper()
	a := NewAgent(AgentOptions{
		InitialState: &AgentState{Model: testModel, Tools: []AgentTool{tool}},
		StreamFn: scriptedStream(
			assistantWithToolCall("c1", tool.Name, args),
			&ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "done"}}, StopReason: ai.StopStop},
		),
		AfterToolCall: after,
	})
	var end AgentEvent
	a.Subscribe(func(_ context.Context, e AgentEvent) error {
		if e.Type == EvToolExecutionEnd {
			end = e
		}
		return nil
	})
	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	for _, m := range a.State().Messages {
		if trm, ok := m.(ai.ToolResultMessage); ok {
			return trm, end
		}
	}
	t.Fatal("no tool result message")
	return ai.ToolResultMessage{}, end
}

// Upstream 2fd386840 (#6671, agent-loop.test.ts "should handle tool calls and
// results"): a tool can report the usage its own execution cost; afterToolCall
// observes it and can replace it (usage ?? result.usage), and the tool result
// message carries the final value.
func TestLoopCarriesToolResultUsage(t *testing.T) {
	toolUsage := &ai.Usage{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, TotalTokens: 10,
		Cost: ai.CostBreakdown{Input: 0.1, Output: 0.2, CacheRead: 0.3, CacheWrite: 0.4, Total: 1}}
	patched := &ai.Usage{Input: 5, Output: 6, CacheRead: 7, CacheWrite: 8, TotalTokens: 26,
		Cost: ai.CostBreakdown{Input: 0.5, Output: 0.6, CacheRead: 0.7, CacheWrite: 0.8, Total: 2.6}}
	echo := AgentTool{
		Name:       "echo",
		Parameters: ai.Object(ai.Prop("value", ai.String())),
		Execute: func(ctx context.Context, id string, params map[string]any, onUpdate ToolUpdateFunc) (AgentToolResult, error) {
			return AgentToolResult{Content: ai.ContentList{ai.TextContent{Text: "echoed"}}, Usage: toolUsage}, nil
		},
	}

	t.Run("the tool's usage", func(t *testing.T) {
		trm, _ := runToolTurn(t, echo, map[string]any{"value": "hello"}, nil)
		if !reflect.DeepEqual(trm.Usage, toolUsage) {
			t.Fatalf("tool result usage = %+v, want the tool's %+v", trm.Usage, toolUsage)
		}
	})
	t.Run("replaced by afterToolCall", func(t *testing.T) {
		var observed *ai.Usage
		trm, _ := runToolTurn(t, echo, map[string]any{"value": "hello"}, func(_ context.Context, c AfterToolCallContext) *AfterToolCallResult {
			observed = c.Result.Usage
			return &AfterToolCallResult{Usage: patched}
		})
		if !reflect.DeepEqual(observed, toolUsage) {
			t.Fatalf("afterToolCall observed usage %+v, want %+v", observed, toolUsage)
		}
		if !reflect.DeepEqual(trm.Usage, patched) {
			t.Fatalf("tool result usage = %+v, want the patched %+v", trm.Usage, patched)
		}
	})
}
