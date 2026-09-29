package coding

import (
	"context"
	"testing"

	"github.com/sky-valley/pi/agent"
	"github.com/sky-valley/pi/ai"
)

// pi agent-session.ts afterToolCall passes the tool_result hook's usage on
// (`usage: hookResult?.usage`, upstream 2fd386840), so a hook can patch what a
// tool reported. The SDK's AfterToolCall is the Go counterpart of that hook.
func TestToolResultHookPassesUsageThrough(t *testing.T) {
	patched := &ai.Usage{Input: 5, Output: 6, TotalTokens: 11}
	wrapped := withToolResultImageNormalization(func(context.Context, agent.AfterToolCallContext) *agent.AfterToolCallResult {
		return &agent.AfterToolCallResult{Usage: patched}
	}, nil)
	out := wrapped(context.Background(), agent.AfterToolCallContext{
		Result: agent.AgentToolResult{Content: ai.ContentList{ai.TextContent{Text: "ok"}}},
	})
	if out == nil || out.Usage != patched {
		t.Fatalf("hook usage dropped: got %+v", out)
	}
}
