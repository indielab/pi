package coding

import (
	"strings"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// I12: model resolution ports pi's resolveCliModel (model-resolver.ts).

// A slash prefix that is NOT a known provider is part of the model id:
// OpenRouter-style ids resolve across providers.
func TestResolveModelOpenRouterSlashedID(t *testing.T) {
	r, err := ResolveModelPattern("ai21/jamba-large-1.7")
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Model.Provider) != "openrouter" || r.Model.ID != "ai21/jamba-large-1.7" {
		t.Fatalf("expected openrouter/ai21/jamba-large-1.7, got %s/%s", r.Model.Provider, r.Model.ID)
	}
}

// A slash prefix that IS a known provider is preferred — but when nothing
// matches within that provider, the full input falls back to a raw model id
// across all models (pi: "openai/gpt-4o:extended" style openrouter ids).
func TestResolveModelProviderPrefixFallsBackToFullID(t *testing.T) {
	r, err := ResolveModelPattern("anthropic/claude-opus-4.8-fast")
	if err != nil {
		t.Fatal(err)
	}
	// "anthropic" is a known provider but has no "claude-opus-4.8-fast" model, so
	// the full input falls back to a raw model id across all providers; that id is
	// hosted solely under openrouter, so the fallback resolves there (pi's registry
	// .find() lands on the same sole copy). npm 0.80.7 dropped the previous fixture
	// (vercel-ai-gateway/anthropic/claude-3.5-haiku); re-point on catalog churn.
	if string(r.Model.Provider) != "openrouter" || r.Model.ID != "anthropic/claude-opus-4.8-fast" {
		t.Fatalf("expected openrouter fallback for full id, got %s/%s", r.Model.Provider, r.Model.ID)
	}
}

func TestResolveModelCaseInsensitive(t *testing.T) {
	r, err := ResolveModelPattern("ANTHROPIC/CLAUDE-SONNET-4-5")
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Model.Provider) != "anthropic" || r.Model.ID != "claude-sonnet-4-5" {
		t.Fatalf("case-insensitive resolution failed: %s/%s", r.Model.Provider, r.Model.ID)
	}
}

// A ":<level>" suffix parses off and surfaces alongside the model
// (parseModelPattern). Levels: off|minimal|low|medium|high|xhigh.
func TestResolveModelThinkingLevelSuffix(t *testing.T) {
	r, err := ResolveModelPattern("anthropic/claude-sonnet-4-5:high")
	if err != nil {
		t.Fatal(err)
	}
	if r.Model.ID != "claude-sonnet-4-5" || r.ThinkingLevel != "high" {
		t.Fatalf("suffix parse wrong: id=%s level=%q", r.Model.ID, r.ThinkingLevel)
	}
	// Bare-id pattern with suffix. ("claude-sonnet-4-5" is ambiguous across
	// providers in the catalog, so like pi the fuzzy matcher picks an alias —
	// only the model presence and parsed level are asserted here.)
	r, err = ResolveModelPattern("claude-sonnet-4-5:xhigh")
	if err != nil {
		t.Fatal(err)
	}
	if r.Model == nil || r.ThinkingLevel != "xhigh" {
		t.Fatalf("bare-id suffix parse wrong: model=%v level=%q", r.Model, r.ThinkingLevel)
	}
	// No suffix → empty level.
	r, err = ResolveModelPattern("anthropic/claude-sonnet-4-5")
	if err != nil {
		t.Fatal(err)
	}
	if r.ThinkingLevel != "" {
		t.Fatalf("unexpected level without suffix: %q", r.ThinkingLevel)
	}
}

// pi's exact error text (resolveCliModel).
func TestResolveModelUnknownErrorText(t *testing.T) {
	_, err := ResolveModelPattern("definitely-not-a-model-xyz")
	if err == nil {
		t.Fatal("expected error")
	}
	want := `Model "definitely-not-a-model-xyz" not found. Use --list-models to see available models.`
	if err.Error() != want {
		t.Fatalf("error text drift:\n got: %s\nwant: %s", err, want)
	}
}

// An unknown id under a KNOWN provider falls back to a synthetic custom-id
// model with a warning (pi buildFallbackModel).
func TestResolveModelCustomIDFallback(t *testing.T) {
	r, err := ResolveModelPattern("anthropic/my-custom-model-id")
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Model.Provider) != "anthropic" || r.Model.ID != "my-custom-model-id" || r.Model.Name != "my-custom-model-id" {
		t.Fatalf("custom-id fallback wrong: %s/%s (%s)", r.Model.Provider, r.Model.ID, r.Model.Name)
	}
	if !strings.Contains(r.Warning, `Model "my-custom-model-id" not found for provider "anthropic". Using custom model id.`) {
		t.Fatalf("fallback warning drift: %q", r.Warning)
	}
	if r.ThinkingLevel != "" {
		t.Fatalf("fallback without suffix must not carry a level: %q", r.ThinkingLevel)
	}
}

// pi 9fd75b8a (#5560): a ":<level>" suffix on a custom id is stripped in the
// fallback path — it must NOT leak into the model id sent to the API — and is
// surfaced as the thinking level. The warning quotes the STRIPPED id.
func TestResolveModelCustomIDFallbackThinkingSuffix(t *testing.T) {
	r, err := ResolveModelPattern("anthropic/my-custom-model-id:high")
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Model.Provider) != "anthropic" || r.Model.ID != "my-custom-model-id" {
		t.Fatalf("suffix leaked into custom id: %s/%s", r.Model.Provider, r.Model.ID)
	}
	if r.ThinkingLevel != "high" {
		t.Fatalf("fallback thinking level wrong: %q", r.ThinkingLevel)
	}
	if !strings.Contains(r.Warning, `Model "my-custom-model-id" not found for provider "anthropic". Using custom model id.`) {
		t.Fatalf("fallback warning must quote the stripped id: %q", r.Warning)
	}
}

// pi 1fc80f4f (#5552): a requested thinking level on a custom-id fallback must
// set reasoning:true even when the provider's template model is non-reasoning,
// so the level is honored. mistral's fallback template is non-reasoning, so the
// flip is observable here.
func TestResolveModelCustomIDFallbackThinkingSuffixSetsReasoning(t *testing.T) {
	r, err := ResolveModelPattern("mistral/my-custom-model-id:high")
	if err != nil {
		t.Fatal(err)
	}
	if r.Model.ID != "my-custom-model-id" || r.ThinkingLevel != "high" {
		t.Fatalf("suffix parse wrong: id=%s level=%q", r.Model.ID, r.ThinkingLevel)
	}
	if !r.Model.Reasoning {
		t.Fatalf("requested thinking level must set reasoning:true on the fallback model")
	}
}

// The :off level is not a request to think: reasoning must stay false on a
// non-reasoning fallback template (pi gates on requestedThinking !== "off").
func TestResolveModelCustomIDFallbackThinkingOffKeepsReasoningFalse(t *testing.T) {
	r, err := ResolveModelPattern("mistral/my-custom-model-id:off")
	if err != nil {
		t.Fatal(err)
	}
	if r.Model.ID != "my-custom-model-id" || r.ThinkingLevel != "off" {
		t.Fatalf("suffix parse wrong: id=%s level=%q", r.Model.ID, r.ThinkingLevel)
	}
	if r.Model.Reasoning {
		t.Fatalf(":off must not enable reasoning on a non-reasoning fallback template")
	}
}

// All valid thinking levels work in the fallback path (upstream test parity).
func TestResolveModelCustomIDFallbackAllLevels(t *testing.T) {
	for _, level := range []string{"off", "minimal", "low", "medium", "high", "xhigh"} {
		r, err := ResolveModelPattern("anthropic/my-custom-model-id:" + level)
		if err != nil {
			t.Fatal(err)
		}
		if r.Model.ID != "my-custom-model-id" {
			t.Fatalf("level %s: suffix leaked into custom id: %s", level, r.Model.ID)
		}
		if r.ThinkingLevel != level {
			t.Fatalf("level %s: fallback thinking level wrong: %q", level, r.ThinkingLevel)
		}
	}
}

// An invalid suffix is not a thinking level: it stays part of the custom id.
func TestResolveModelCustomIDFallbackInvalidSuffix(t *testing.T) {
	r, err := ResolveModelPattern("anthropic/my-custom-model-id:banana")
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Model.Provider) != "anthropic" || r.Model.ID != "my-custom-model-id:banana" {
		t.Fatalf("invalid suffix must stay in the id: %s/%s", r.Model.Provider, r.Model.ID)
	}
	if r.ThinkingLevel != "" {
		t.Fatalf("invalid suffix must not surface a level: %q", r.ThinkingLevel)
	}
	if !strings.Contains(r.Warning, `Model "my-custom-model-id:banana" not found for provider "anthropic". Using custom model id.`) {
		t.Fatalf("fallback warning drift: %q", r.Warning)
	}
}

// Upstream c1019d920 (Baseten) added one line to defaultModelPerProvider that the
// port's first pass missed, because the commit's model-resolver.ts hunk sits in a
// file that is otherwise host-only and triaged n/a — but this ONE table is ported.
// Inert today: baseten has no catalog models until a release regen carries them,
// so buildFallbackModel returns nil for it. It stops being inert on that regen,
// at which point a missing entry would silently clone providerModels[0] instead
// of GLM-5.2 — the same failure mode the qwen-token-plan entries below describe.
// Locked now so the regen cannot introduce it quietly.
func TestDefaultModelPerProviderBaseten(t *testing.T) {
	if got := defaultModelPerProvider["baseten"]; got != "zai-org/GLM-5.2" {
		t.Fatalf("default model for %q: got %q, want %q", "baseten", got, "zai-org/GLM-5.2")
	}
}

// pi 77428858: the openai default model advanced gpt-5.4 → gpt-5.5. Only openai
// moved — azure-openai-responses and github-copilot stay on gpt-5.4 (and
// openai-codex was already gpt-5.5). Lock the buildFallbackModel template ids.
func TestDefaultModelPerProviderOpenAI(t *testing.T) {
	cases := map[string]string{
		"openai":                 "gpt-5.5",
		"azure-openai-responses": "gpt-5.4",
		"github-copilot":         "gpt-5.4",
		"openai-codex":           "gpt-5.5",
	}
	for provider, want := range cases {
		if got := defaultModelPerProvider[provider]; got != want {
			t.Fatalf("default model for %q: got %q, want %q", provider, got, want)
		}
	}
}

// The port was missing three of pi's defaultModelPerProvider entries. The two
// qwen-token-plan ones are load-bearing: without them a custom model id under
// those providers falls through to providerModels[0] — MiniMax-M2.5 after the
// sort — and clones its contextWindow/maxTokens (196608/32768) instead of
// qwen3.7-max's (1000000/131072), which changes the emitted max_tokens and the
// context clamp. "radius" is absent from the catalog, so it is inert, but it is
// carried for faithfulness. Values taken from pi 0.83.0's model-resolver.
func TestDefaultModelPerProviderQwenTokenPlanAndRadius(t *testing.T) {
	cases := map[string]string{
		"qwen-token-plan":            "qwen3.7-max",
		"qwen-token-plan-cn":         "qwen3.7-max",
		"qwen-token-plan-individual": "qwen3.8-max",
		"radius":                     "auto",
	}
	for provider, want := range cases {
		t.Run(provider, func(t *testing.T) {
			if got := defaultModelPerProvider[provider]; got != want {
				t.Fatalf("default model for %q: got %q, want %q", provider, got, want)
			}
		})
	}

	// Lock the consequence, not just the table entry: the qwen-token-plan
	// fallback must inherit qwen3.7-max's limits, never MiniMax-M2.5's.
	for _, provider := range []string{"qwen-token-plan", "qwen-token-plan-cn"} {
		t.Run(provider+"/limits", func(t *testing.T) {
			tmpl := ai.GetModel(provider, defaultModelPerProvider[provider])
			if tmpl == nil {
				t.Fatalf("%s/%s missing from catalog", provider, defaultModelPerProvider[provider])
			}
			if tmpl.ContextWindow != 1000000 || tmpl.MaxTokens != 131072 {
				t.Fatalf("%s template limits = %d/%d, want 1000000/131072",
					provider, tmpl.ContextWindow, tmpl.MaxTokens)
			}
		})
	}
}

// pi e429d90b8: the Z.AI Coding Plan defaults advance glm-5.1 → glm-5.3.
// glm-5.1 left the zai catalogs at 0.84.1, so the defaults had been dangling
// for a whole release; 0.84.2 adds glm-5.3 and e429d90b8 re-points them.
// Mirrors upstream's "zai, minimax, cerebras, and ant-ling defaults track
// current models" as of that commit.
func TestDefaultModelPerProviderZaiCohort(t *testing.T) {
	cases := map[string]string{
		"zai":           "glm-5.3",
		"zai-coding-cn": "glm-5.3",
		"minimax":       "MiniMax-M2.7",
		"minimax-cn":    "MiniMax-M2.7",
		"cerebras":      "zai-glm-4.7",
		"ant-ling":      "Ring-2.6-1T",
	}
	for provider, want := range cases {
		t.Run(provider, func(t *testing.T) {
			if got := defaultModelPerProvider[provider]; got != want {
				t.Fatalf("default model for %q: got %q, want %q", provider, got, want)
			}
		})
	}
}

// pi 70e878d4c: the xAI default advances grok-4.5 → grok-4.6 alongside the
// move of every built-in xAI model onto the Responses API. grok-4.6 is already
// in the 0.84.2 catalog (as openai-completions until the next regen carries
// the routing flip), so the default is not dangling. Mirrors upstream's "xai
// default tracks current model".
func TestDefaultModelPerProviderXai(t *testing.T) {
	if got := defaultModelPerProvider["xai"]; got != "grok-4.6" {
		t.Fatalf("default model for %q: got %q, want %q", "xai", got, "grok-4.6")
	}
}

// pi e429d90b8 also added "built-in defaults exist in generated provider
// catalogs": every provider in the generated catalog must have a default that
// resolves to a real catalog model, so a catalog regen can never orphan a
// default silently again. Entries for providers absent from the catalog
// (e.g. radius) are deliberately out of scope, as upstream iterates catalog
// providers, not table entries.
func TestDefaultModelsExistInCatalog(t *testing.T) {
	for _, provider := range ai.GetProviders() {
		defaultID, ok := defaultModelPerProvider[provider]
		if !ok {
			t.Errorf("catalog provider %q has no defaultModelPerProvider entry", provider)
			continue
		}
		if ai.GetModel(provider, defaultID) == nil {
			t.Errorf("%s default %s should exist in its generated catalog", provider, defaultID)
		}
	}
}

// pi a01baaae re-pointed defaultModelPerProvider's xai entry to grok-4.5 at
// 0.80.10; pi 70e878d4c advances it to grok-4.6. Pin the constant through the
// custom-id fallback: the synthetic model must be templated from grok-4.6.
func TestResolveModelXaiFallbackDefault(t *testing.T) {
	if defaultModelPerProvider["xai"] != "grok-4.6" {
		t.Fatalf("xai default = %q, want grok-4.6", defaultModelPerProvider["xai"])
	}
	r, err := ResolveModelPattern("xai/my-custom-grok")
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Model.Provider) != "xai" || r.Model.ID != "my-custom-grok" {
		t.Fatalf("xai fallback wrong: %s/%s", r.Model.Provider, r.Model.ID)
	}
	// The template is the grok-4.6 catalog entry (clone carries its limits).
	tmpl, err := ResolveModel("xai/grok-4.6")
	if err != nil {
		t.Fatalf("grok-4.6 must exist in the catalog: %v", err)
	}
	if r.Model.ContextWindow != tmpl.ContextWindow || r.Model.MaxTokens != tmpl.MaxTokens {
		t.Fatalf("fallback not templated from grok-4.6: cw=%d/%d mt=%d/%d",
			r.Model.ContextWindow, tmpl.ContextWindow, r.Model.MaxTokens, tmpl.MaxTokens)
	}
}
