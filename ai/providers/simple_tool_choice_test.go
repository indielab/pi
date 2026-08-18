package providers

import (
	"errors"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// Ported from pi's simple tool-choice option (upstream e5dde9a76): a
// provider-neutral SimpleStreamOptions.ToolChoice that each ported provider maps
// onto its own native shape. The unported providers pi also touched
// (bedrock/azure/mistral/codex/vertex) have no Go counterpart.

type simpleStreamFn func(model *ai.Model, req ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream

// captureSimplePayload runs a StreamSimple entry point and returns the payload it
// would have sent, aborting from OnPayload before any request is made.
func captureSimplePayload(t *testing.T, stream simpleStreamFn, model *ai.Model, req ai.Context, choice ai.ToolChoice) map[string]any {
	t.Helper()
	var captured any
	opts := &ai.SimpleStreamOptions{ToolChoice: choice}
	opts.APIKey = "k"
	opts.OnPayload = func(payload any, _ *ai.Model) (any, error) {
		captured = payload
		return nil, errors.New("payload captured")
	}
	stream(model, req, opts).Result()
	body, _ := captured.(map[string]any)
	if body == nil {
		t.Fatalf("no payload captured (got %T)", captured)
	}
	return body
}

func TestSimpleToolChoicePerProvider(t *testing.T) {
	tool := ai.Tool{Name: "read", Description: "read a file", Parameters: ai.Object(ai.Prop("path", ai.String()))}
	req := ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}, Tools: []ai.Tool{tool}}

	cases := []struct {
		name   string
		model  *ai.Model
		stream simpleStreamFn
		// want reports the provider-native tool selection found in the payload,
		// or nil when the payload carries none.
		want func(body map[string]any) any
	}{
		{
			name: "anthropic-messages",
			model: &ai.Model{
				ID: "claude-opus-5", Api: ai.APIAnthropicMessages, Provider: "anthropic",
				Input: []string{"text"}, MaxTokens: 4096,
			},
			stream: func(m *ai.Model, r ai.Context, o *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				return StreamSimpleAnthropic(t.Context(), m, r, o)
			},
			// pi wraps a bare string as {type: ...}.
			want: func(body map[string]any) any {
				tc, _ := body["tool_choice"].(map[string]any)
				if tc == nil {
					return nil
				}
				return tc["type"]
			},
		},
		{
			name: "openai-completions",
			model: &ai.Model{
				ID: "gpt-5.5", Api: ai.APIOpenAICompletions, Provider: "openai",
				Input: []string{"text"}, MaxTokens: 4096,
			},
			stream: func(m *ai.Model, r ai.Context, o *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				return StreamSimpleOpenAICompletions(t.Context(), m, r, o)
			},
			want: func(body map[string]any) any { return body["tool_choice"] },
		},
		{
			name: "openai-responses",
			model: &ai.Model{
				ID: "gpt-5.5", Api: ai.APIOpenAIResponses, Provider: "openai",
				Input: []string{"text"}, MaxTokens: 4096,
			},
			stream: func(m *ai.Model, r ai.Context, o *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				return StreamSimpleOpenAIResponses(t.Context(), m, r, o)
			},
			want: func(body map[string]any) any { return body["tool_choice"] },
		},
		{
			name: "google-generative-ai",
			model: &ai.Model{
				ID: "gemini-3.1-pro-preview", Api: ai.APIGoogleGenerativeAI, Provider: "google",
				Input: []string{"text"}, MaxTokens: 4096, BaseURL: "https://example.invalid/v1beta",
			},
			stream: func(m *ai.Model, r ai.Context, o *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				return StreamSimpleGoogle(t.Context(), m, r, o)
			},
			// Google carries it as a functionCallingConfig mode, upper-cased.
			want: func(body map[string]any) any {
				cfg, _ := body["toolConfig"].(map[string]any)
				if cfg == nil {
					return nil
				}
				fcc, _ := cfg["functionCallingConfig"].(map[string]any)
				if fcc == nil {
					return nil
				}
				return fcc["mode"]
			},
		},
		{
			name: "pi-messages",
			model: &ai.Model{
				ID: "pi-1", Api: ai.APIPiMessages, Provider: "pi",
				Input: []string{"text"}, MaxTokens: 4096, BaseURL: "https://example.invalid",
			},
			stream: func(m *ai.Model, r ai.Context, o *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				return StreamSimplePiMessages(t.Context(), m, r, o)
			},
			want: func(body map[string]any) any {
				opts, _ := body["options"].(map[string]any)
				if opts == nil {
					return nil
				}
				return opts["toolChoice"]
			},
		},
	}

	// The value each provider is expected to put on the wire for "none". Google
	// upper-cases; everyone else passes the literal through.
	noneOnWire := map[string]any{
		"anthropic-messages":   "none",
		"openai-completions":   "none",
		"openai-responses":     "none",
		"google-generative-ai": "NONE",
		"pi-messages":          "none",
	}

	for _, tc := range cases {
		t.Run(tc.name+"/none", func(t *testing.T) {
			got := tc.want(captureSimplePayload(t, tc.stream, tc.model, req, ai.ToolChoiceNone))
			if got != noneOnWire[tc.name] {
				t.Fatalf("want %v, got %v", noneOnWire[tc.name], got)
			}
		})
		t.Run(tc.name+"/unset", func(t *testing.T) {
			// Absent option: providers must not invent a selection. Google is the
			// exception pi shares — it derives AUTO/VALIDATED from the tools alone.
			got := tc.want(captureSimplePayload(t, tc.stream, tc.model, req, ""))
			if tc.name == "google-generative-ai" {
				if got == "NONE" {
					t.Fatalf("unset must not become NONE, got %v", got)
				}
				return
			}
			if got != nil {
				t.Fatalf("want no tool selection, got %v", got)
			}
		})
	}
}
