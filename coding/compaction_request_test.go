package coding

import (
	"context"
	"strings"
	"testing"

	"github.com/sky-valley/pi/agent"
	"github.com/sky-valley/pi/ai"
	"github.com/sky-valley/pi/ai/providers"
)

// TestSummarizationRequestShape pins the faithful summarization request builder
// (pi compaction.ts generateSummary + utils.ts): a dedicated system prompt, the
// serialized conversation wrapped in <conversation>...</conversation>, tool-result
// truncation to 2000 chars, a capped maxTokens, and the read/modified file lists
// appended to the returned summary text.
func TestSummarizationRequestShape(t *testing.T) {
	reg := providers.RegisterFauxProvider(providers.RegisterFauxProviderOptions{
		Models: []providers.FauxModelDefinition{{ID: "faux-1", ContextWindow: 200000}},
	})
	defer reg.Unregister()
	model := reg.GetModel()
	model.MaxTokens = 1_000_000 // large so the 0.8*reserve cap wins

	var captured ai.Context
	var capturedMax int
	reg.SetResponses([]providers.FauxResponseStep{
		func(req ai.Context, opts *ai.SimpleStreamOptions, st *providers.FauxState, m *ai.Model) *ai.AssistantMessage {
			captured = req
			if opts != nil && opts.MaxTokens != nil {
				capturedMax = *opts.MaxTokens
			}
			return providers.FauxAssistantMessage(ai.ContentList{ai.TextContent{Text: "## Goal\ncheckpoint"}}, ai.StopStop)
		},
	})

	sess := NewSession(SessionOptions{Model: model, Cwd: t.TempDir(), NoTools: NoToolsAll})

	bigResult := strings.Repeat("z", 5000) // > 2000 chars, must be truncated
	older := []agent.AgentMessage{
		ai.NewUserText("please refactor the parser", 1),
		ai.AssistantMessage{
			Content: ai.ContentList{
				ai.TextContent{Text: "reading files"},
				ai.ToolCall{ID: "r1", Name: "read", Arguments: map[string]any{"path": "/a/only_read.go"}},
				ai.ToolCall{ID: "e1", Name: "edit", Arguments: map[string]any{"path": "/a/changed.go"}},
				ai.ToolCall{ID: "r2", Name: "read", Arguments: map[string]any{"path": "/a/changed.go"}},
			},
			StopReason: ai.StopToolUse, Timestamp: 2,
		},
		ai.ToolResultMessage{ToolCallID: "r1", ToolName: "read", Content: ai.ContentList{ai.TextContent{Text: bigResult}}, Timestamp: 3},
	}

	const reserve = 16384
	summary := sess.summarize(context.Background(), older, reserve)

	// System prompt present and exact.
	if captured.SystemPrompt != summarizationSystemPrompt {
		t.Fatalf("summarization system prompt missing/wrong:\n%q", captured.SystemPrompt)
	}

	// Single user message with the <conversation> wrapper + the summarization prompt.
	if len(captured.Messages) != 1 {
		t.Fatalf("expected 1 summarization message, got %d", len(captured.Messages))
	}
	um, ok := captured.Messages[0].(ai.UserMessage)
	if !ok {
		t.Fatalf("expected user message, got %T", captured.Messages[0])
	}
	text := textOf(um.Content)
	if !strings.HasPrefix(text, "<conversation>\n") || !strings.Contains(text, "\n</conversation>\n\n") {
		t.Fatalf("conversation wrapper missing: %q", text)
	}
	if !strings.HasSuffix(text, summarizationPrompt) {
		t.Fatalf("summarization prompt not appended after </conversation>")
	}
	if !strings.Contains(text, "[User]: please refactor the parser") {
		t.Fatalf("user turn not serialized: %q", text)
	}
	if !strings.Contains(text, "[Assistant tool calls]: read(path=\"/a/only_read.go\")") {
		t.Fatalf("tool-call serialization missing: %q", text)
	}

	// Tool result truncated to 2000 chars + marker.
	if !strings.Contains(text, "[... 3000 more characters truncated]") {
		t.Fatalf("tool result not truncated to 2000 chars: %q", text[len(text)-200:])
	}
	if strings.Count(text, "z") > 2100 {
		t.Fatalf("tool result kept too many chars (truncation failed)")
	}

	// maxTokens = floor(0.8 * reserve) since model.MaxTokens is huge.
	if capturedMax != 13107 {
		t.Fatalf("maxTokens = floor(0.8*%d) expected 13107, got %d", reserve, capturedMax)
	}

	// File lists appended to the summary: only_read.go is read-only; changed.go
	// was edited (so excluded from read-files, present in modified-files).
	if !strings.Contains(summary, "<read-files>\n/a/only_read.go\n</read-files>") {
		t.Fatalf("read-files list missing/wrong:\n%s", summary)
	}
	if !strings.Contains(summary, "<modified-files>\n/a/changed.go\n</modified-files>") {
		t.Fatalf("modified-files list missing/wrong:\n%s", summary)
	}
	if strings.Contains(summary, "<read-files>\n/a/changed.go") {
		t.Fatalf("changed.go must not appear in read-files")
	}
}

// TestSummarizationMaxTokensClampedByModel verifies model.maxTokens caps the
// 0.8*reserve budget when it is smaller.
func TestSummarizationMaxTokensClampedByModel(t *testing.T) {
	reg := providers.RegisterFauxProvider(providers.RegisterFauxProviderOptions{
		Models: []providers.FauxModelDefinition{{ID: "faux-1", ContextWindow: 200000}},
	})
	defer reg.Unregister()
	model := reg.GetModel()
	model.MaxTokens = 4096 // smaller than floor(0.8*16384)=13107

	var capturedMax int
	reg.SetResponses([]providers.FauxResponseStep{
		func(req ai.Context, opts *ai.SimpleStreamOptions, st *providers.FauxState, m *ai.Model) *ai.AssistantMessage {
			if opts != nil && opts.MaxTokens != nil {
				capturedMax = *opts.MaxTokens
			}
			return providers.FauxAssistantMessage(ai.ContentList{ai.TextContent{Text: "ok"}}, ai.StopStop)
		},
	})

	sess := NewSession(SessionOptions{Model: model, Cwd: t.TempDir(), NoTools: NoToolsAll})
	sess.summarize(context.Background(), []agent.AgentMessage{ai.NewUserText("hi", 1)}, 16384)

	if capturedMax != 4096 {
		t.Fatalf("expected maxTokens clamped to model.MaxTokens 4096, got %d", capturedMax)
	}
}

// TestSummarizationIsolatesRouting mirrors pi's "uses fresh routing sessions
// without prompt caching" (9b3a2059): every summarization request carries
// cacheRetention "none" and its own session id, so summaries neither reuse nor
// pollute the main session's cache and routing.
func TestSummarizationIsolatesRouting(t *testing.T) {
	reg := providers.RegisterFauxProvider(providers.RegisterFauxProviderOptions{
		Models: []providers.FauxModelDefinition{{ID: "faux-1", ContextWindow: 200000}},
	})
	defer reg.Unregister()
	model := reg.GetModel()

	var retentions []ai.CacheRetention
	var sessionIDs []string
	capture := func(req ai.Context, opts *ai.SimpleStreamOptions, st *providers.FauxState, m *ai.Model) *ai.AssistantMessage {
		retentions = append(retentions, opts.CacheRetention)
		sessionIDs = append(sessionIDs, opts.SessionID)
		return providers.FauxAssistantMessage(ai.ContentList{ai.TextContent{Text: "ok"}}, ai.StopStop)
	}
	reg.SetResponses([]providers.FauxResponseStep{capture, capture})

	sess := NewSession(SessionOptions{Model: model, Cwd: t.TempDir(), NoTools: NoToolsAll, SessionID: "main-session"})
	older := []agent.AgentMessage{ai.NewUserText("hi", 1)}
	sess.summarize(context.Background(), older, 16384)
	sess.summarize(context.Background(), older, 16384)

	if len(retentions) != 2 {
		t.Fatalf("expected 2 summarization requests, got %d", len(retentions))
	}
	for i, r := range retentions {
		if r != ai.CacheNone {
			t.Fatalf("request %d cacheRetention = %q, want %q", i, r, ai.CacheNone)
		}
	}
	for i, id := range sessionIDs {
		if id == "" {
			t.Fatalf("request %d has no session id, want a fresh one", i)
		}
		if id == "main-session" {
			t.Fatalf("request %d reused the session's own id %q", i, id)
		}
	}
	if sessionIDs[0] == sessionIDs[1] {
		t.Fatalf("both summarization requests shared session id %q, want distinct ids", sessionIDs[0])
	}
}
