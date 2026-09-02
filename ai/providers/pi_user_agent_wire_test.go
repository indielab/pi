package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// --- pi 87af49dec: pi's runtime user agent is a DEFAULT, not an override ---
//
// Upstream used to FORCE this string over every other header source for two
// providers (kimi-coding on anthropic-messages, 9d2ec7ffa; xai on both openai
// adapters, 70e878d4c). 87af49dec deletes that forcing and instead spreads
// `{"User-Agent": getPiUserAgent()}` FIRST into each adapter's header merge, so
// the precedence reverses: every later source wins, and four adapters that sent
// no user agent at all now send one by default.
//
// Request headers are outside the bodies-only differential harness, so this
// surface is pinned here on the wire instead.

// captureOpenAIResponsesHeaders runs one openai-responses request against a
// local server and returns the headers that reached the wire.
func captureOpenAIResponsesHeaders(t *testing.T, model *ai.Model, opts ai.StreamOptions) http.Header {
	t.Helper()
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, responsesSSE)
	}))
	defer server.Close()
	model.BaseURL = server.URL
	final := StreamOpenAIResponses(context.Background(), model,
		ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}},
		&OpenAIResponsesOptions{StreamOptions: opts}).Result()
	if final.StopReason == ai.StopError {
		t.Fatalf("stream failed: %s", final.ErrorMessage)
	}
	return got
}

// captureGoogleHeaders runs one google-generative-ai request against a local
// server and returns the headers that reached the wire.
func captureGoogleHeaders(t *testing.T, model *ai.Model, opts ai.StreamOptions) http.Header {
	t.Helper()
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, googleSSE)
	}))
	defer server.Close()
	model.BaseURL = server.URL
	final := StreamGoogle(context.Background(), model,
		ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}},
		&GoogleOptions{StreamOptions: opts}).Result()
	if final.StopReason == ai.StopError {
		t.Fatalf("stream failed: %s", final.ErrorMessage)
	}
	return got
}

// captureAnthropicHeaders runs one anthropic-messages request and returns the
// headers that reached the wire.
func captureAnthropicHeaders(t *testing.T, model *ai.Model, opts *AnthropicOptions) http.Header {
	t.Helper()
	headers, _ := anthropicCapture(t, model, ai.Context{Messages: []ai.Message{ai.NewUserText("hi", 1)}}, opts, anthropicSSE)
	return headers
}

// wantUserAgent asserts that exactly one user-agent value reached the wire and
// that it is the expected one. Exactly-once matters on its own: pi collapses
// case-variant header names to a single wire header, and net/http canonicalizes
// to a single key, so a second value would mean the port had grown a header pi
// cannot send.
func wantUserAgent(t *testing.T, h http.Header, want string) {
	t.Helper()
	if got := h.Values("User-Agent"); len(got) != 1 || got[0] != want {
		t.Fatalf("user-agent = %v, want exactly [%q]", got, want)
	}
}

func anthropicUAModel() *ai.Model {
	return &ai.Model{ID: "claude-test", Api: ai.APIAnthropicMessages, Provider: "anthropic",
		Input: []string{"text"}, MaxTokens: 4096}
}

func anthropicUAOptions(opts ai.ProviderRequestOptions) *AnthropicOptions {
	return &AnthropicOptions{StreamOptions: ai.StreamOptions{ProviderRequestOptions: opts}}
}

// Every ported adapter now sends pi's runtime user agent when nothing else
// supplies one. Before 87af49dec only kimi-coding and xai requests carried it;
// plain anthropic, openai-completions, openai-responses and google sent none.
func TestPiUserAgentIsTheDefaultOnEveryAdapter(t *testing.T) {
	t.Run("anthropic api key", func(t *testing.T) {
		h := captureAnthropicHeaders(t, anthropicUAModel(), anthropicUAOptions(ai.ProviderRequestOptions{APIKey: "k"}))
		wantUserAgent(t, h, piUserAgent())
	})
	// The ANTHROPIC_AUTH_TOKEN bearer branch (upstream 24e5cc04) sits ahead of
	// the other three and merges the same client headers.
	t.Run("anthropic auth token bearer", func(t *testing.T) {
		h := captureAnthropicHeaders(t, anthropicUAModel(), anthropicUAOptions(ai.ProviderRequestOptions{
			Env: map[string]string{ai.AnthropicAuthTokenEnv: "my-auth-token"},
		}))
		if got := h.Get("authorization"); got != "Bearer my-auth-token" {
			t.Fatalf("authorization = %q, want the bearer branch", got)
		}
		wantUserAgent(t, h, piUserAgent())
	})
	// The copilot branch, on a model that carries no catalog user agent of its
	// own (the shipped ones do — see TestModelHeadersOverridePiUserAgent).
	t.Run("anthropic github-copilot", func(t *testing.T) {
		model := anthropicUAModel()
		model.Provider = "github-copilot"
		h := captureAnthropicHeaders(t, model, anthropicUAOptions(ai.ProviderRequestOptions{APIKey: "gh-token"}))
		if got := h.Get("authorization"); got != "Bearer gh-token" {
			t.Fatalf("authorization = %q, want the copilot branch", got)
		}
		wantUserAgent(t, h, piUserAgent())
	})
	t.Run("openai-completions", func(t *testing.T) {
		h := captureOpenAIHeaders(t, openAITestModel(), ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "k"},
		})
		wantUserAgent(t, h, piUserAgent())
	})
	t.Run("openai-responses", func(t *testing.T) {
		model := &ai.Model{ID: "gpt-5", Api: ai.APIOpenAIResponses, Provider: "openai",
			Input: []string{"text"}, MaxTokens: 4096}
		h := captureOpenAIResponsesHeaders(t, model, ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "k"},
		})
		wantUserAgent(t, h, piUserAgent())
	})
	t.Run("google", func(t *testing.T) {
		model := &ai.Model{ID: "gemini-2.5-flash", Api: ai.APIGoogleGenerativeAI, Provider: "google",
			Input: []string{"text"}, MaxTokens: 4096}
		h := captureGoogleHeaders(t, model, ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "g-key"},
		})
		wantUserAgent(t, h, piUserAgent())
	})
	// xai is the interesting one on the openai adapters: it used to force pi's
	// agent last; now it merely gets the same default as everyone else.
	t.Run("xai completions", func(t *testing.T) {
		model := &ai.Model{ID: "grok-custom", Api: ai.APIOpenAICompletions, Provider: "xai",
			Input: []string{"text"}, MaxTokens: 4096}
		h := captureOpenAIHeaders(t, model, ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "xai-test-token"},
		})
		wantUserAgent(t, h, piUserAgent())
	})
	t.Run("xai responses", func(t *testing.T) {
		model := &ai.Model{ID: "grok-4.5", Api: ai.APIOpenAIResponses, Provider: "xai",
			Input: []string{"text"}, MaxTokens: 4096}
		h := captureOpenAIResponsesHeaders(t, model, ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "xai-test-token"},
		})
		wantUserAgent(t, h, piUserAgent())
	})
}

// THE REVERSAL. A user agent on model.Headers now beats pi's default, where
// before 87af49dec pi's agent was re-set after the whole merge for exactly
// these two providers. Both are asserted by name because both changed meaning.
func TestModelHeadersOverridePiUserAgent(t *testing.T) {
	t.Run("kimi-coding anthropic", func(t *testing.T) {
		model := &ai.Model{ID: "kimi-for-coding", Api: ai.APIAnthropicMessages, Provider: "kimi-coding",
			Input: []string{"text"}, MaxTokens: 4096,
			Headers: ai.ProviderHeaders{"User-Agent": strPtr("KimiCLI/1.5")}}
		h := captureAnthropicHeaders(t, model, anthropicUAOptions(ai.ProviderRequestOptions{APIKey: "kimi-key"}))
		wantUserAgent(t, h, "KimiCLI/1.5")
	})
	t.Run("xai completions", func(t *testing.T) {
		model := &ai.Model{ID: "grok-custom", Api: ai.APIOpenAICompletions, Provider: "xai",
			Input: []string{"text"}, MaxTokens: 4096,
			Headers: ai.ProviderHeaders{"User-Agent": strPtr("model-agent")}}
		h := captureOpenAIHeaders(t, model, ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "xai-test-token"},
		})
		wantUserAgent(t, h, "model-agent")
	})
	t.Run("xai responses", func(t *testing.T) {
		model := &ai.Model{ID: "grok-4.5", Api: ai.APIOpenAIResponses, Provider: "xai",
			Input: []string{"text"}, MaxTokens: 4096,
			Headers: ai.ProviderHeaders{"User-Agent": strPtr("model-agent")}}
		h := captureOpenAIResponsesHeaders(t, model, ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "xai-test-token"},
		})
		wantUserAgent(t, h, "model-agent")
	})
	t.Run("google", func(t *testing.T) {
		model := &ai.Model{ID: "gemini-2.5-flash", Api: ai.APIGoogleGenerativeAI, Provider: "google",
			Input: []string{"text"}, MaxTokens: 4096,
			Headers: ai.ProviderHeaders{"User-Agent": strPtr("model-agent")}}
		h := captureGoogleHeaders(t, model, ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "g-key"},
		})
		wantUserAgent(t, h, "model-agent")
	})
	// The live catalog case: shipped github-copilot models carry
	// User-Agent: GitHubCopilotChat/… , which the reversal now lets through.
	t.Run("github-copilot catalog model", func(t *testing.T) {
		model := ai.GetModel("github-copilot", "claude-haiku-4.5")
		if model == nil {
			t.Skip("github-copilot/claude-haiku-4.5 not in catalog")
		}
		want := model.Headers["User-Agent"]
		if want == nil || *want == "" {
			t.Fatalf("catalog model lost its User-Agent header: %v", model.Headers)
		}
		// The clone exists so anthropicCapture's BaseURL write cannot land on
		// the shared catalog entry ai.GetModel hands back; Headers still alias
		// the catalog map and must stay read-only here.
		clone := *model
		h := captureAnthropicHeaders(t, &clone, anthropicUAOptions(ai.ProviderRequestOptions{APIKey: "gh-token"}))
		wantUserAgent(t, h, *want)
	})
}

// The anthropic OAuth branch supplies its own `user-agent: claude-cli/<v>`
// AFTER the seeded default in pi's merge, so the Claude Code identity still
// wins — the reversal did not disturb it.
func TestAnthropicOAuthUserAgentBeatsPiDefault(t *testing.T) {
	h := captureAnthropicHeaders(t, anthropicUAModel(), anthropicUAOptions(ai.ProviderRequestOptions{
		APIKey: "sk-ant-oat-token",
	}))
	wantUserAgent(t, h, "claude-cli/"+claudeCodeVersion)
}

// options.Headers are spread last in every adapter, so a consumer user agent
// beats both pi's default and model.Headers.
func TestOptionsHeadersOverridePiUserAgentAndModelHeaders(t *testing.T) {
	t.Run("anthropic over kimi model header", func(t *testing.T) {
		model := &ai.Model{ID: "kimi-for-coding", Api: ai.APIAnthropicMessages, Provider: "kimi-coding",
			Input: []string{"text"}, MaxTokens: 4096,
			Headers: ai.ProviderHeaders{"User-Agent": strPtr("KimiCLI/1.5")}}
		h := captureAnthropicHeaders(t, model, anthropicUAOptions(ai.ProviderRequestOptions{
			APIKey:  "kimi-key",
			Headers: ai.ProviderHeaders{"User-Agent": strPtr("custom-client")},
		}))
		wantUserAgent(t, h, "custom-client")
	})
	// …with ONE exception, and it is a spelling exception, not a precedence
	// one: the anthropic OAuth branch holds its identity under the lowercase
	// name at a later slot than the seeded "User-Agent", so a caller spelling
	// the name "User-Agent" writes back into slot 0 and still loses. Executed
	// against @anthropic-ai/sdk 0.91.1 from ~/.cache/pi-npm/0.84.2, driving the
	// real client with the object mergeClientHeaders builds at 87af49dec:
	// {"User-Agent": custom-agent} → wire `user-agent: claude-cli/<v>`,
	// {"user-agent": custom-agent} → wire `user-agent: custom-agent`.
	t.Run("anthropic oauth identity keeps the User-Agent slot", func(t *testing.T) {
		h := captureAnthropicHeaders(t, anthropicUAModel(), anthropicUAOptions(ai.ProviderRequestOptions{
			APIKey:  "sk-ant-oat-token",
			Headers: ai.ProviderHeaders{"User-Agent": strPtr("custom-client")},
		}))
		wantUserAgent(t, h, "claude-cli/"+claudeCodeVersion)
	})
	t.Run("anthropic oauth identity loses to another spelling", func(t *testing.T) {
		h := captureAnthropicHeaders(t, anthropicUAModel(), anthropicUAOptions(ai.ProviderRequestOptions{
			APIKey:  "sk-ant-oat-token",
			Headers: ai.ProviderHeaders{"user-agent": strPtr("custom-client")},
		}))
		wantUserAgent(t, h, "custom-client")
	})
	// A third spelling takes a slot of its own, after the identity's, so it
	// wins there too — the slot, not the letter case, is what decides.
	t.Run("anthropic oauth identity loses to a third spelling", func(t *testing.T) {
		h := captureAnthropicHeaders(t, anthropicUAModel(), anthropicUAOptions(ai.ProviderRequestOptions{
			APIKey:  "sk-ant-oat-token",
			Headers: ai.ProviderHeaders{"USER-AGENT": strPtr("custom-client")},
		}))
		wantUserAgent(t, h, "custom-client")
	})
	t.Run("xai completions", func(t *testing.T) {
		model := &ai.Model{ID: "grok-custom", Api: ai.APIOpenAICompletions, Provider: "xai",
			Input: []string{"text"}, MaxTokens: 4096,
			Headers: ai.ProviderHeaders{"User-Agent": strPtr("model-agent")}}
		h := captureOpenAIHeaders(t, model, ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{
				APIKey:  "xai-test-token",
				Headers: ai.ProviderHeaders{"User-Agent": strPtr("custom-agent")},
			},
		})
		wantUserAgent(t, h, "custom-agent")
	})
	t.Run("xai responses", func(t *testing.T) {
		model := &ai.Model{ID: "grok-4.5", Api: ai.APIOpenAIResponses, Provider: "xai",
			Input: []string{"text"}, MaxTokens: 4096,
			Headers: ai.ProviderHeaders{"User-Agent": strPtr("model-agent")}}
		h := captureOpenAIResponsesHeaders(t, model, ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{
				APIKey:  "xai-test-token",
				Headers: ai.ProviderHeaders{"User-Agent": strPtr("custom-agent")},
			},
		})
		wantUserAgent(t, h, "custom-agent")
	})
	t.Run("google", func(t *testing.T) {
		model := &ai.Model{ID: "gemini-2.5-flash", Api: ai.APIGoogleGenerativeAI, Provider: "google",
			Input: []string{"text"}, MaxTokens: 4096,
			Headers: ai.ProviderHeaders{"User-Agent": strPtr("model-agent")}}
		h := captureGoogleHeaders(t, model, ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{
				APIKey:  "g-key",
				Headers: ai.ProviderHeaders{"User-Agent": strPtr("custom-agent")},
			},
		})
		wantUserAgent(t, h, "custom-agent")
	})
	// A lowercase consumer spelling must win too: net/http canonicalizes, and
	// so does the SDK pi hands its merged object to.
	t.Run("lowercase spelling on anthropic", func(t *testing.T) {
		h := captureAnthropicHeaders(t, anthropicUAModel(), anthropicUAOptions(ai.ProviderRequestOptions{
			APIKey:  "k",
			Headers: ai.ProviderHeaders{"user-agent": strPtr("custom-client")},
		}))
		wantUserAgent(t, h, "custom-client")
	})
}

// A deletion marker still wins over the new default. The two adapter families
// differ, exactly as pi's do: adapters that hand the merged headers to an SDK
// as `defaultHeaders` let a null DELETE the header, while google converts
// through providerHeadersToRecord, where a null only means "this entry is not
// sent" and therefore cancels pi's default only by colliding with its name.
func TestUserAgentMarkerBeatsPiDefault(t *testing.T) {
	t.Run("anthropic", func(t *testing.T) {
		h := captureAnthropicHeaders(t, anthropicUAModel(), anthropicUAOptions(ai.ProviderRequestOptions{
			APIKey:  "k",
			Headers: ai.ProviderHeaders{"user-agent": nil},
		}))
		// net/http supplies a transport agent once ours is gone, exactly as
		// fetch does for pi; what must not survive is pi's default.
		if got := h.Get("User-Agent"); got == piUserAgent() {
			t.Fatalf("a marker must suppress pi's default user agent, got %q", got)
		}
	})
	// A marker only empties the SLOT it names. On the OAuth branch slot 0 is
	// the seeded "User-Agent" and the claude-cli identity sits at a later slot,
	// so cancelling the default leaves the identity standing — probe against
	// @anthropic-ai/sdk 0.91.1: {"User-Agent": null} → `claude-cli/<v>`.
	t.Run("anthropic oauth marker on the seed slot only", func(t *testing.T) {
		h := captureAnthropicHeaders(t, anthropicUAModel(), anthropicUAOptions(ai.ProviderRequestOptions{
			APIKey:  "sk-ant-oat-token",
			Headers: ai.ProviderHeaders{"User-Agent": nil},
		}))
		wantUserAgent(t, h, "claude-cli/"+claudeCodeVersion)
	})
	t.Run("openai-completions", func(t *testing.T) {
		model := &ai.Model{ID: "grok-custom", Api: ai.APIOpenAICompletions, Provider: "xai",
			Input: []string{"text"}, MaxTokens: 4096}
		h := captureOpenAIHeaders(t, model, ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{
				APIKey:  "xai-test-token",
				Headers: ai.ProviderHeaders{"User-Agent": nil},
			},
		})
		if got := h.Get("User-Agent"); got == piUserAgent() {
			t.Fatalf("a marker must suppress pi's default user agent, got %q", got)
		}
	})
	t.Run("openai-responses", func(t *testing.T) {
		model := &ai.Model{ID: "grok-4.5", Api: ai.APIOpenAIResponses, Provider: "xai",
			Input: []string{"text"}, MaxTokens: 4096}
		h := captureOpenAIResponsesHeaders(t, model, ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{
				APIKey:  "xai-test-token",
				Headers: ai.ProviderHeaders{"User-Agent": nil},
			},
		})
		if got := h.Get("User-Agent"); got == piUserAgent() {
			t.Fatalf("a marker must suppress pi's default user agent, got %q", got)
		}
	})
	// google, name-colliding marker: pi's object becomes {"User-Agent": null},
	// which providerHeadersToRecord drops, so no user agent is sent.
	t.Run("google colliding marker drops the default", func(t *testing.T) {
		model := &ai.Model{ID: "gemini-2.5-flash", Api: ai.APIGoogleGenerativeAI, Provider: "google",
			Input: []string{"text"}, MaxTokens: 4096}
		h := captureGoogleHeaders(t, model, ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{
				APIKey:  "g-key",
				Headers: ai.ProviderHeaders{"User-Agent": nil},
			},
		})
		if got := h.Get("User-Agent"); got == piUserAgent() {
			t.Fatalf("a marker on the same name must drop pi's default, got %q", got)
		}
	})
	// google, differently-spelled marker: pi's object keeps BOTH keys, the
	// record drops only the null one, and pi's default survives. This is the
	// pointed difference from the SDK adapters above, where the same input
	// deletes the header.
	t.Run("google non-colliding marker keeps the default", func(t *testing.T) {
		model := &ai.Model{ID: "gemini-2.5-flash", Api: ai.APIGoogleGenerativeAI, Provider: "google",
			Input: []string{"text"}, MaxTokens: 4096}
		h := captureGoogleHeaders(t, model, ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{
				APIKey:  "g-key",
				Headers: ai.ProviderHeaders{"user-agent": nil},
			},
		})
		wantUserAgent(t, h, piUserAgent())
	})
}

// Cloudflare attribution is a separate surface and still outranks the default:
// pi's host folds it into options.headers, which are spread after the seeded
// agent, so `pi-coding-agent` reaches the wire rather than pi's runtime agent.
func TestCloudflareAttributionUserAgentBeatsPiDefault(t *testing.T) {
	t.Setenv("PI_TELEMETRY", "1")
	model := &ai.Model{ID: "gemini-2.5-flash", Api: ai.APIGoogleGenerativeAI,
		Provider: "cloudflare-workers-ai", Input: []string{"text"}, MaxTokens: 4096}
	h := captureGoogleHeaders(t, model, ai.StreamOptions{
		ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "g-key"},
	})
	wantUserAgent(t, h, "pi-coding-agent")
}

// The record path must not depend on Go map iteration order either. Two
// spellings inside ONE source have no order to reproduce, so sorted name order
// is the tie-break (see headerObject.merge).
func TestHeaderObjectRecordCaseCollisionIsDeterministic(t *testing.T) {
	source := ai.ProviderHeaders{
		"User-Agent": strPtr(piUserAgent()),
		"user-agent": strPtr("from-model"),
		"X-Keep":     strPtr("keep"),
		// A marker is DROPPED on this path rather than deleting, so it must not
		// take the win from the value sorting after it.
		"X-Trace": nil,
	}
	// One run cannot tell a sorted implementation from a lucky one: Go
	// randomizes map iteration per range statement, so repeat enough that an
	// order-dependent implementation is overwhelmingly likely to disagree with
	// itself at least once.
	for i := range 200 {
		h := http.Header{}
		o := &headerObject{}
		o.merge(source)
		o.applyAsRecord(h)
		if got := h.Values("User-Agent"); len(got) != 1 || got[0] != "from-model" {
			t.Fatalf("run %d: user-agent = %v, want exactly [%q] — the name sorting last must win", i, got, "from-model")
		}
		if got := h.Get("X-Keep"); got != "keep" {
			t.Fatalf("run %d: X-Keep = %q, want keep", i, got)
		}
		if _, present := h["X-Trace"]; present {
			t.Fatalf("run %d: X-Trace = %q, want it never written", i, h.Get("X-Trace"))
		}
	}
}

// The same collision reaching google through the real merge, where the order IS
// known: the seed takes slot 0 and a differently-spelled user agent from a
// later source takes a later slot, so the later source wins whatever its
// spelling sorts like. "USER-AGENT" sorts BEFORE "User-Agent" and used to lose
// here for that reason alone.
//
// pi's wire value is not reachable on this adapter: @google/genai appends, so
// it sends `pi (…), model-agent` where the port sends the winner alone
// (executed against @google/genai 1.52.0 from ~/.cache/pi-npm/0.84.2 —
// recorded in docs/UPSTREAM.md, not asserted here).
func TestGoogleCaseCollidingModelUserAgentWins(t *testing.T) {
	for _, name := range []string{"user-agent", "USER-AGENT", "User-agent"} {
		t.Run(name, func(t *testing.T) {
			for i := range 20 {
				model := &ai.Model{ID: "gemini-2.5-flash", Api: ai.APIGoogleGenerativeAI, Provider: "google",
					Input: []string{"text"}, MaxTokens: 4096,
					Headers: ai.ProviderHeaders{name: strPtr("model-agent")}}
				h := captureGoogleHeaders(t, model, ai.StreamOptions{
					ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "g-key"},
				})
				if got := h.Values("User-Agent"); len(got) != 1 || got[0] != "model-agent" {
					t.Fatalf("run %d: user-agent = %v, want exactly [%q]", i, got, "model-agent")
				}
			}
		})
	}
}

// Cross-source precedence is decided by slot, not by spelling: a consumer
// header spelled differently from the catalog's still wins, because its name is
// new to the merge and takes a slot after it.
func TestOptionsHeadersWinAcrossSpellingsOnGoogle(t *testing.T) {
	model := &ai.Model{ID: "gemini-2.5-flash", Api: ai.APIGoogleGenerativeAI, Provider: "google",
		Input: []string{"text"}, MaxTokens: 4096,
		Headers: ai.ProviderHeaders{"user-agent": strPtr("model-agent")}}
	h := captureGoogleHeaders(t, model, ai.StreamOptions{
		ProviderRequestOptions: ai.ProviderRequestOptions{
			APIKey:  "g-key",
			Headers: ai.ProviderHeaders{"USER-AGENT": strPtr("opts-agent")},
		},
	})
	wantUserAgent(t, h, "opts-agent")
}

// An empty-string user agent is a DIVERGENCE, pinned so it cannot drift
// silently. net/http omits the User-Agent header entirely when its value is
// empty (net/http.Request.write special-cases exactly this one header), so the
// port sends no user agent at all. pi sends it present-and-empty: executed
// against @anthropic-ai/sdk 0.91.1 from ~/.cache/pi-npm/0.84.2, the merged
// object {"User-Agent": ""} reaches the wire as `user-agent:` with an empty
// value. Recorded in docs/UPSTREAM.md; not fixable through http.Header.
func TestEmptyUserAgentIsDroppedEntirely(t *testing.T) {
	t.Run("anthropic", func(t *testing.T) {
		h := captureAnthropicHeaders(t, anthropicUAModel(), anthropicUAOptions(ai.ProviderRequestOptions{
			APIKey:  "k",
			Headers: ai.ProviderHeaders{"User-Agent": strPtr("")},
		}))
		if values, present := h["User-Agent"]; present {
			t.Fatalf("User-Agent = %q, want the header absent — net/http drops an empty one", values)
		}
	})
	t.Run("google", func(t *testing.T) {
		model := &ai.Model{ID: "gemini-2.5-flash", Api: ai.APIGoogleGenerativeAI, Provider: "google",
			Input: []string{"text"}, MaxTokens: 4096,
			Headers: ai.ProviderHeaders{"User-Agent": strPtr("")}}
		h := captureGoogleHeaders(t, model, ai.StreamOptions{
			ProviderRequestOptions: ai.ProviderRequestOptions{APIKey: "g-key"},
		})
		if values, present := h["User-Agent"]; present {
			t.Fatalf("User-Agent = %q, want the header absent — net/http drops an empty one", values)
		}
	})
	// Any other header keeps an empty value, so the drop is the transport's
	// User-Agent special case and not this package losing empty strings.
	t.Run("other headers keep an empty value", func(t *testing.T) {
		h := captureAnthropicHeaders(t, anthropicUAModel(), anthropicUAOptions(ai.ProviderRequestOptions{
			APIKey:  "k",
			Headers: ai.ProviderHeaders{"X-Empty": strPtr("")},
		}))
		if values, present := h["X-Empty"]; !present || len(values) != 1 || values[0] != "" {
			t.Fatalf("X-Empty = %q (present=%v), want exactly one empty value", values, present)
		}
	})
}
