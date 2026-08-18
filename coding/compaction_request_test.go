package coding

import (
	"context"
	"strings"
	"testing"
	"time"

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

// TestSummarizationUsesAuthResolvedBaseURL ports pi's regression test
// 6768-copilot-compaction-base-url.test.ts ("uses the auth-resolved base URL
// through the SDK-style stream wrapper"): a Copilot compaction request must go
// to the credential-resolved Enterprise endpoint, not the catalog's Individual
// one.
//
// The provider mirrors upstream's: the Enterprise base URL is reachable only
// through the stored OAuth credential's toAuth. An explicit apiKey override
// short-circuits resolution to the api-key path, so the Models runtime resolves
// no base URL and never rebuilds the request model — summarization always passes
// the session's key, which is exactly how the Individual endpoint leaked
// through. This exercises the real ai.Models runtime, not a stream double.
func TestSummarizationUsesAuthResolvedBaseURL(t *testing.T) {
	const individualBaseURL = "https://api.individual.githubcopilot.com"
	const enterpriseBaseURL = "https://api.enterprise.githubcopilot.com"

	var requestBaseURL string
	respond := func(_ context.Context, model *ai.Model, _ ai.Context, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		requestBaseURL = model.BaseURL
		s := ai.NewAssistantMessageEventStream()
		s.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
			Api: model.Api, Provider: model.Provider, Model: model.ID,
			Content: ai.ContentList{ai.TextContent{Text: "summary"}}, StopReason: ai.StopStop,
		}})
		s.End()
		return s
	}

	catalogModel := &ai.Model{Provider: "github-copilot", ID: "gpt", Api: "copilot-api", BaseURL: individualBaseURL}
	provider := ai.CreateProvider(ai.CreateProviderOptions{
		ID: catalogModel.Provider,
		Auth: ai.ProviderAuth{
			APIKey: &ai.ApiKeyAuth{
				Name: "Copilot token",
				Resolve: func(_ context.Context, _ ai.AuthContext, credential *ai.Credential) (*ai.AuthResult, error) {
					if credential == nil || credential.Key == "" {
						return nil, nil
					}
					return &ai.AuthResult{Auth: ai.ModelAuth{APIKey: credential.Key}, Source: "explicit token"}, nil
				},
			},
			OAuth: &ai.OAuthAuth{
				Name:    "Copilot OAuth",
				Refresh: func(_ context.Context, c ai.OAuthCredentials) (ai.OAuthCredentials, error) { return c, nil },
				ToAuth: func(c ai.OAuthCredentials) (ai.ModelAuth, error) {
					return ai.ModelAuth{APIKey: c.Access, BaseURL: enterpriseBaseURL}, nil
				},
			},
		},
		Models: []*ai.Model{catalogModel},
		APIByApi: map[ai.Api]ai.ProviderStreams{catalogModel.Api: {
			Stream: func(ctx context.Context, m *ai.Model, req ai.Context, _ *ai.StreamOptions) *ai.AssistantMessageEventStream {
				return respond(ctx, m, req, nil)
			},
			StreamSimple: respond,
		}},
	})

	credentials := ai.NewInMemoryCredentialStore()
	if _, err := credentials.Modify(context.Background(), catalogModel.Provider, func(*ai.Credential) (*ai.Credential, error) {
		return &ai.Credential{
			Type: ai.CredentialOAuth, Access: "enterprise-token", Refresh: "refresh-token",
			Expires: time.Now().Add(time.Hour).UnixMilli(),
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	models := ai.CreateModels(&ai.CreateModelsOptions{Credentials: credentials})
	models.SetProvider(provider)

	// The SDK-style wrapper upstream installs as the session's stream function.
	streamFn := func(ctx context.Context, m *ai.Model, req ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		var runtimeOpts ai.ModelsSimpleStreamOptions
		if opts != nil {
			runtimeOpts.SimpleStreamOptions = *opts
		}
		return models.StreamSimple(ctx, m, req, &runtimeOpts)
	}

	sess := NewSession(SessionOptions{
		Model: catalogModel, APIKey: "session-key", Cwd: t.TempDir(),
		NoTools: NoToolsAll, StreamFn: streamFn, Models: models,
	})
	older := []agent.AgentMessage{
		ai.NewUserText("summarize me", 1),
		ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "ok"}}, StopReason: ai.StopStop, Timestamp: 2},
	}
	if summary := sess.summarize(context.Background(), older, 16384); summary == "" {
		t.Fatal("summarization produced no summary")
	}

	if requestBaseURL != enterpriseBaseURL {
		t.Fatalf("compaction request base URL = %q, want %q", requestBaseURL, enterpriseBaseURL)
	}
}

// TestAnthropicSummarizationFallback pins pi getAnthropicSummarizationFallback
// (upstream eb1f87fa9): only first-party Anthropic models that declare permitted
// fallback targets get one, and only the first target is used.
func TestAnthropicSummarizationFallback(t *testing.T) {
	anthropic := func(compat string) *ai.Model {
		m := &ai.Model{ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic"}
		if compat != "" {
			m.Compat = []byte(compat)
		}
		return m
	}
	cases := []struct {
		name  string
		model *ai.Model
		want  []string
	}{
		{
			name:  "first permitted target only",
			model: anthropic(`{"allowedFallbackModels":["claude-opus-4-8","claude-opus-5"]}`),
			want:  []string{"claude-opus-4-8"},
		},
		{name: "no compat", model: anthropic("")},
		{name: "compat without the key", model: anthropic(`{"supportsTemperature":true}`)},
		{name: "empty target list", model: anthropic(`{"allowedFallbackModels":[]}`)},
		{
			name: "third-party provider on the anthropic api",
			model: &ai.Model{
				ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "github-copilot",
				Compat: []byte(`{"allowedFallbackModels":["claude-opus-4-8"]}`),
			},
		},
		{
			name: "anthropic provider on another api",
			model: &ai.Model{
				ID: "claude-opus-5", Api: ai.APIOpenAICompletions, Provider: "anthropic",
				Compat: []byte(`{"allowedFallbackModels":["claude-opus-4-8"]}`),
			},
		},
		{name: "nil model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := anthropicSummarizationFallback(tc.model)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("want no fallback, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("want a fallback, got nil")
			}
			if got.Default {
				t.Fatalf("want the explicit-models arm, got the default arm: %+v", got)
			}
			if len(got.Models) != len(tc.want) || got.Models[0] != tc.want[0] {
				t.Fatalf("want %v, got %v", tc.want, got.Models)
			}
		})
	}
}

// TestSummarizationDisablesTools pins pi 90305d90a: summarization requests ask
// for no tools, and a response that called one anyway is treated as a failed
// summarization rather than checkpointed.
func TestSummarizationDisablesTools(t *testing.T) {
	reg := providers.RegisterFauxProvider(providers.RegisterFauxProviderOptions{
		Models: []providers.FauxModelDefinition{{ID: "faux-1", ContextWindow: 200000}},
	})
	defer reg.Unregister()
	model := reg.GetModel()

	var capturedChoice ai.ToolChoice
	reg.SetResponses([]providers.FauxResponseStep{
		func(req ai.Context, opts *ai.SimpleStreamOptions, st *providers.FauxState, m *ai.Model) *ai.AssistantMessage {
			if opts != nil {
				capturedChoice = opts.ToolChoice
			}
			return providers.FauxAssistantMessage(ai.ContentList{ai.TextContent{Text: "## Goal\nsummary"}}, ai.StopStop)
		},
	})

	sess := NewSession(SessionOptions{Model: model, Cwd: t.TempDir(), NoTools: NoToolsAll})
	older := []agent.AgentMessage{ai.NewUserText("do the thing", 1)}

	if got := sess.summarize(context.Background(), older, 16384); got == "" {
		t.Fatal("expected a summary from a text-only response")
	}
	if capturedChoice != ai.ToolChoiceNone {
		t.Fatalf("summarization must request toolChoice=none, got %q", capturedChoice)
	}

	// Same request, but the model calls a tool: no summary survives.
	reg.SetResponses([]providers.FauxResponseStep{
		func(req ai.Context, opts *ai.SimpleStreamOptions, st *providers.FauxState, m *ai.Model) *ai.AssistantMessage {
			return providers.FauxAssistantMessage(ai.ContentList{
				ai.TextContent{Text: "## Goal\nsummary"},
				ai.ToolCall{ID: "t1", Name: "read", Arguments: map[string]any{"path": "/a"}},
			}, ai.StopToolUse)
		},
	})
	if got := sess.summarize(context.Background(), older, 16384); got != "" {
		t.Fatalf("a tool call must fail the summarization, got %q", got)
	}
}
