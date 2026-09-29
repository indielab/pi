package coding

import (
	"reflect"
	"testing"

	"github.com/sky-valley/pi/agent"
	"github.com/sky-valley/pi/ai"
)

// Upstream 8562bcf66 (compaction-nested-calls.test.ts): files touched by the
// nested calls recorded on a tool result count as file operations; a call
// whose arguments were omitted for size contributes nothing.
func TestCompactionFileOpsIncludeNestedCalls(t *testing.T) {
	result := ai.ToolResultMessage{
		ToolCallID: "codemode-1",
		ToolName:   "codemode",
		Content:    ai.ContentList{},
		NestedCalls: &ai.NestedToolCalls{
			Calls: []ai.NestedToolCallRecord{
				{ID: "codemode-1/1", Name: "read", Arguments: map[string]any{"path": "a.ts"}, Status: ai.NestedToolCallOK},
				{ID: "codemode-1/2", Name: "edit", Arguments: map[string]any{"path": "b.ts", "edits": []any{}}, Status: ai.NestedToolCallOK},
				{ID: "codemode-1/3", Name: "write", ArgumentsBytes: 40000, Status: ai.NestedToolCallOK},
			},
			Complete: false,
		},
	}
	readFiles, modifiedFiles := computeFileLists([]agent.AgentMessage{result})
	texts := func(items []fileListItem) []string {
		out := []string{}
		for _, it := range items {
			out = append(out, it.text)
		}
		return out
	}
	if got := texts(readFiles); !reflect.DeepEqual(got, []string{"a.ts"}) {
		t.Errorf("readFiles = %q, want [a.ts]", got)
	}
	if got := texts(modifiedFiles); !reflect.DeepEqual(got, []string{"b.ts"}) {
		t.Errorf("modifiedFiles = %q, want [b.ts]", got)
	}
}
