package ai

import (
	"encoding/json"
	"reflect"
	"testing"
)

// These mirror the catalog assertions upstream's tests make for the models the
// 0.87.1 regen brought in, one case per upstream `it`:
//
//   - b4588f26a  supports-xhigh.test.ts "includes Claude Opus 5.5 with its
//     always-on effort levels and official pricing" (with b7f4b05c7's effort map)
//   - 27c072e98  github-copilot-anthropic.test.ts "applies Copilot-specific
//     adaptive thinking effort overrides" (the opus55 block) and
//     model-catalog-types.test.ts (the gpt-6-sol / gpt-6-luna block)
//   - db91e0331  supports-xhigh.test.ts "includes xhigh and max for OpenAI %s",
//     "includes official metadata for OpenAI and Codex %s" and
//     max-thinking.test.ts "exposes xhigh and max for openai-codex/%s"
//   - 1a584a7a5  xai-responses.test.ts "includes Grok 4.7 capabilities and
//     long-context pricing" and its supported-levels line
//
// The expectations are upstream's own. The catalog is the published build's
// data, so these pass exactly when that data is what pi's tests say it is.
//
// Upstream's own CI at b313731b8 FAILS the Copilot Opus 5.5 levels assertion
// ([off minimal low ...]). That run regenerates the catalog from live
// models.dev, which by then listed github-copilot/claude-opus-5.5 itself, so the
// generator's hard-coded entry (off/minimal null) was skipped. The published
// 0.87.1 build carries the hard-coded entry, and its own
// getSupportedThinkingLevels answers [low medium high xhigh max] for it; the
// build wins.

type catalogCase struct {
	provider, id  string
	api           Api
	contextWindow int
	maxTokens     int
	cost          *ModelCost
	compat        map[string]any
	thinkingMap   map[ModelThinkingLevel]string
	levels        []ModelThinkingLevel
}

func (c catalogCase) check(t *testing.T) {
	t.Helper()
	m := GetModel(c.provider, c.id)
	if m == nil {
		t.Fatalf("catalog has no %s/%s", c.provider, c.id)
	}
	if c.api != "" && m.Api != c.api {
		t.Errorf("api = %q, want %q", m.Api, c.api)
	}
	if c.contextWindow != 0 && m.ContextWindow != c.contextWindow {
		t.Errorf("contextWindow = %d, want %d", m.ContextWindow, c.contextWindow)
	}
	if c.maxTokens != 0 && m.MaxTokens != c.maxTokens {
		t.Errorf("maxTokens = %d, want %d", m.MaxTokens, c.maxTokens)
	}
	if c.cost != nil && !reflect.DeepEqual(m.Cost, *c.cost) {
		t.Errorf("cost = %+v, want %+v", m.Cost, *c.cost)
	}
	if len(c.compat) > 0 {
		var compat map[string]any
		if err := json.Unmarshal(m.Compat, &compat); err != nil {
			t.Fatalf("compat %s: %v", m.Compat, err)
		}
		for k, want := range c.compat {
			if got, ok := compat[k]; !ok || got != want {
				t.Errorf("compat.%s = %v (present %t), want %v", k, got, ok, want)
			}
		}
	}
	for level, want := range c.thinkingMap {
		got, ok := m.ThinkingLevelMap[level]
		if !ok || got == nil || *got != want {
			t.Errorf("thinkingLevelMap.%s = %v (present %t), want %q", level, got, ok, want)
		}
	}
	if c.levels != nil {
		if got := GetSupportedThinkingLevels(m); !reflect.DeepEqual(got, c.levels) {
			t.Errorf("GetSupportedThinkingLevels = %v, want %v", got, c.levels)
		}
	}
}

// withLongContext is generate-models.ts withOpenAiLongContextPricing: above
// 272k input tokens input and cache rates double and output goes 1.5x.
func withLongContext(c ModelCost) *ModelCost {
	c.Tiers = []ModelCostTier{{
		InputTokensAbove: 272000,
		Input:            c.Input * 2,
		Output:           c.Output * 1.5,
		CacheRead:        c.CacheRead * 2,
		CacheWrite:       c.CacheWrite * 2,
	}}
	return &c
}

func TestCatalogClaudeOpus55(t *testing.T) {
	adaptive := []ModelThinkingLevel{"low", "medium", "high", "xhigh", "max"}
	for _, c := range []catalogCase{
		{
			provider: "anthropic", id: "claude-opus-5-5",
			cost:          &ModelCost{Input: 4, Output: 20, CacheRead: 0.2, CacheWrite: 5},
			contextWindow: 1_000_000, maxTokens: 128_000,
			compat: map[string]any{
				"forceAdaptiveThinking":          true,
				"supportsMidConvoEffort":         true,
				"supportsMidConvoSystemMessages": true,
				"supportsMidConvoToolChanges":    true,
			},
			levels: adaptive,
		},
		{
			provider: "github-copilot", id: "claude-opus-5.5",
			api: APIAnthropicMessages, contextWindow: 1_000_000,
			levels: adaptive,
		},
	} {
		t.Run(c.provider+"/"+c.id, c.check)
	}
}

func TestCatalogGPT6SolAndLuna(t *testing.T) {
	official := map[string]ModelCost{
		"gpt-6-sol":  {Input: 2, Output: 10, CacheRead: 0.2, CacheWrite: 2.5},
		"gpt-6-luna": {Input: 0.1, Output: 0.5, CacheRead: 0.01, CacheWrite: 0.125},
	}
	compat := map[string]any{
		"supportsAdditionalTools":        true,
		"supportsMidConvoSystemMessages": true,
		"supportsOpenAIGrammarTools":     true,
		"supportsToolSearch":             true,
	}
	var cases []catalogCase
	for _, id := range []string{"gpt-6-sol", "gpt-6-luna"} {
		cases = append(cases,
			catalogCase{
				provider: "openai", id: id,
				cost: withLongContext(official[id]), contextWindow: 272000, maxTokens: 128000,
				compat: compat,
				levels: []ModelThinkingLevel{"off", "low", "medium", "high", "xhigh", "max"},
			},
			catalogCase{
				provider: "openai-codex", id: id,
				cost: withLongContext(official[id]), contextWindow: 272000, maxTokens: 128000,
				compat:      compat,
				thinkingMap: map[ModelThinkingLevel]string{"xhigh": "xhigh", "max": "max"},
				levels:      []ModelThinkingLevel{"off", "minimal", "low", "medium", "high", "xhigh", "max"},
			},
			catalogCase{
				provider: "github-copilot", id: id,
				api: APIOpenAIResponses, contextWindow: 1_000_000, maxTokens: 128000,
				thinkingMap: map[ModelThinkingLevel]string{"off": "none", "max": "max"},
			},
		)
	}
	for _, c := range cases {
		t.Run(c.provider+"/"+c.id, c.check)
	}
	// cache-retention.test.ts "does not enable cache warming from the
	// documented TTL alone for %s" (db91e0331 adds both ids).
	for _, id := range []string{"gpt-6-sol", "gpt-6-luna"} {
		if m := GetModel("openai", id); m != nil && m.PromptCache != nil {
			t.Errorf("openai/%s promptCache = %+v, want absent", id, m.PromptCache)
		}
	}
}

func TestCatalogGrok47(t *testing.T) {
	catalogCase{
		provider: "xai", id: "grok-4.7",
		api: APIOpenAIResponses, contextWindow: 500000, maxTokens: 500000,
		cost: &ModelCost{Input: 2, Output: 6, CacheRead: 0.5, CacheWrite: 0, Tiers: []ModelCostTier{
			{InputTokensAbove: 200000, Input: 4, Output: 12, CacheRead: 1, CacheWrite: 0},
		}},
		levels: []ModelThinkingLevel{"low", "medium", "high", "xhigh"},
	}.check(t)
	m := GetModel("xai", "grok-4.7")
	if !m.Reasoning || !reflect.DeepEqual(m.Input, []string{"text", "image"}) {
		t.Errorf("reasoning = %t, input = %v; want true, [text image]", m.Reasoning, m.Input)
	}
}
