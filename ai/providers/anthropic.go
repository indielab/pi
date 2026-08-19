package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/sky-valley/pi/ai"
)

const (
	anthropicVersion          = "2023-06-01"
	fineGrainedToolStreamBeta = "fine-grained-tool-streaming-2025-05-14"
	interleavedThinkingBeta   = "interleaved-thinking-2025-05-14"
	serverSideFallbackBeta    = "server-side-fallback-2026-07-01"
	claudeCodeVersion         = "2.1.75"
	anthropicDefaultBaseURL   = "https://api.anthropic.com"
)

// claudeCodeTools is the canonical Claude Code 2.x tool-name casing used in
// OAuth "stealth" mode.
var claudeCodeTools = []string{
	"Read", "Write", "Edit", "Bash", "Grep", "Glob", "AskUserQuestion",
	"EnterPlanMode", "ExitPlanMode", "KillShell", "NotebookEdit", "Skill",
	"Task", "TaskOutput", "TodoWrite", "WebFetch", "WebSearch",
}

var ccToolLookup = func() map[string]string {
	m := map[string]string{}
	for _, t := range claudeCodeTools {
		m[strings.ToLower(t)] = t
	}
	return m
}()

func toClaudeCodeName(name string) string {
	if c, ok := ccToolLookup[strings.ToLower(name)]; ok {
		return c
	}
	return name
}

func fromClaudeCodeName(name string, tools []ai.Tool) string {
	lower := strings.ToLower(name)
	for _, t := range tools {
		if strings.ToLower(t.Name) == lower {
			return t.Name
		}
	}
	return name
}

// AnthropicOptions are the provider-native options for streamAnthropic.
type AnthropicOptions struct {
	ai.StreamOptions
	// ThinkingProvided mirrors pi's tri-state `thinkingEnabled?: boolean`
	// (anthropic.ts:951,975-977): when false (the zero value, pi `undefined`)
	// the `thinking` key is omitted entirely; when true, ThinkingEnabled
	// selects enabled/adaptive vs an explicit {type:"disabled"}.
	ThinkingProvided     bool
	ThinkingEnabled      bool
	ThinkingBudgetTokens int
	Effort               string // low|medium|high|xhigh|max
	ThinkingDisplay      string // summarized|omitted
	InterleavedThinking  *bool
	ToolChoice           any
	// RefusalFallbacks asks Anthropic to retry an eligible refusal on another
	// model server-side. Non-nil also opts the request into the fallback beta.
	RefusalFallbacks *ai.AnthropicRefusalFallback
}

// AnthropicCompat holds resolved Anthropic-messages compatibility flags.
type anthropicCompat struct {
	supportsEagerToolInputStreaming bool
	supportsLongCacheRetention      bool
	sendSessionAffinityHeaders      bool
	supportsCacheControlOnTools     bool
	supportsTemperature             bool
	allowEmptySignature             bool
	forceAdaptiveThinking           bool
	// supportsStrictTools reports whether the provider accepts Anthropic strict
	// tool schemas. Default: false.
	supportsStrictTools    bool
	supportsToolReferences bool
	// allowedFallbackModels are the catalog's permitted server-side fallback
	// targets, each carrying the local pricing used to cost a response that target
	// actually served (pi AnthropicMessagesCompat.allowedFallbackModels).
	allowedFallbackModels []ai.AnthropicRefusalFallbackTarget
}

func getAnthropicCompat(model *ai.Model) anthropicCompat {
	// pi 6184307c: no provider/baseUrl auto-detection — OpenAI-standard
	// defaults, with fireworks / cloudflare-ai-gateway-anthropic values supplied
	// explicitly by the catalog (model.compat). sendSessionAffinityHeaders
	// defaults to false.
	c := anthropicCompat{
		supportsEagerToolInputStreaming: true,
		supportsLongCacheRetention:      true,
		supportsCacheControlOnTools:     true,
		supportsTemperature:             true,
		supportsToolReferences:          defaultSupportsToolReferences(model),
	}

	// Apply explicit model.compat overrides.
	if len(model.Compat) > 0 {
		var raw struct {
			SupportsEagerToolInputStreaming *bool `json:"supportsEagerToolInputStreaming"`
			SupportsLongCacheRetention      *bool `json:"supportsLongCacheRetention"`
			SendSessionAffinityHeaders      *bool `json:"sendSessionAffinityHeaders"`
			SupportsCacheControlOnTools     *bool `json:"supportsCacheControlOnTools"`
			SupportsTemperature             *bool `json:"supportsTemperature"`
			AllowEmptySignature             *bool `json:"allowEmptySignature"`
			ForceAdaptiveThinking           *bool `json:"forceAdaptiveThinking"`
			SupportsStrictTools             *bool `json:"supportsStrictTools"`
			SupportsToolReferences          *bool `json:"supportsToolReferences"`
		}
		if json.Unmarshal(model.Compat, &raw) == nil {
			setBool(&c.supportsEagerToolInputStreaming, raw.SupportsEagerToolInputStreaming)
			setBool(&c.supportsLongCacheRetention, raw.SupportsLongCacheRetention)
			setBool(&c.sendSessionAffinityHeaders, raw.SendSessionAffinityHeaders)
			setBool(&c.supportsCacheControlOnTools, raw.SupportsCacheControlOnTools)
			setBool(&c.supportsTemperature, raw.SupportsTemperature)
			setBool(&c.allowEmptySignature, raw.AllowEmptySignature)
			setBool(&c.forceAdaptiveThinking, raw.ForceAdaptiveThinking)
			setBool(&c.supportsStrictTools, raw.SupportsStrictTools)
			setBool(&c.supportsToolReferences, raw.SupportsToolReferences)
		}

		// Decoded on its own, not folded into raw: encoding/json reports a type
		// error for the whole blob even though it populates the sibling fields, and
		// the guard above drops every override when the blob errors — so one catalog
		// shape change in this non-bool field would silently revert every
		// compatibility flag.
		var fb struct {
			AllowedFallbackModels []ai.AnthropicRefusalFallbackTarget `json:"allowedFallbackModels"`
		}
		if json.Unmarshal(model.Compat, &fb) == nil {
			c.allowedFallbackModels = fb.AllowedFallbackModels
		}
	}
	return c
}

// anthropicFallbackTargetCost ports pi's
// `list.find(f => f.model === id)?.cost` (anthropic-messages.ts, upstream
// 4809c2abc): the FIRST matching target decides, so a match carrying no local
// pricing yields nothing rather than scanning on to a later duplicate — in TS
// `find` returns the first hit and `?.cost` on it is `undefined`, which is what
// the `??` chain then falls through on.
func anthropicFallbackTargetCost(targets []ai.AnthropicRefusalFallbackTarget, modelID string) *ai.ModelCost {
	for _, t := range targets {
		if t.Model == modelID {
			return t.Cost
		}
	}
	return nil
}

// anthropicUsageModel is the model a response is COSTED against: the requested
// model, or — when a server-side refusal fallback served it and local pricing for
// the serving model is known — that model repriced (pi's
// `fallbackCost ? { ...model, id: output.model, cost: fallbackCost } : model`,
// anthropic-messages.ts:606-613, upstream 4809c2abc). Request-supplied pricing
// wins over the catalog, but pi joins the two with `??`, so a target listed
// WITHOUT pricing falls through to the catalog rather than pinning the requested
// model's rates. The swap needs a pricing to have been found, not merely a
// different served model: an unknown one keeps the requested rates rather than
// blanking them.
func anthropicUsageModel(model *ai.Model, servedID string, fallbacks *ai.AnthropicRefusalFallback) *ai.Model {
	if servedID == model.ID {
		return model
	}
	var cost *ai.ModelCost
	if fallbacks != nil {
		cost = anthropicFallbackTargetCost(fallbacks.Targets, servedID)
	}
	if cost == nil {
		// Only reached for a served model that differs, so a response no fallback
		// stood in for re-parses no compat blob.
		cost = anthropicFallbackTargetCost(getAnthropicCompat(model).allowedFallbackModels, servedID)
	}
	if cost == nil {
		return model
	}
	// pi's spread is a shallow copy: Input, ThinkingLevelMap, SamplingParams,
	// Headers and Compat alias the catalog entry, so nothing beyond ID and Cost may
	// be mutated here.
	priced := *model
	priced.ID, priced.Cost = servedID, *cost
	return &priced
}

// toolReferenceVersionRe matches first-party Claude model ids to extract the
// major/optional-minor version (port of the regex in defaultSupportsToolReferences).
var toolReferenceVersionRe = regexp.MustCompile(`^claude-(?:opus|sonnet|fable)-(\d+)(?:-(\d+))?(?:-|$)`)

// defaultSupportsToolReferences is the default for supportsToolReferences:
// first-party Anthropic models except Haiku (rejects client-side tool_reference
// blocks) and models that predate tool search (Claude 3.x, Opus/Sonnet 4.0,
// Opus 4.1). Port of pi's helper of the same name.
func defaultSupportsToolReferences(model *ai.Model) bool {
	if model.Provider != "anthropic" || strings.Contains(model.ID, "haiku") {
		return false
	}
	m := toolReferenceVersionRe.FindStringSubmatch(model.ID)
	if m == nil {
		return false
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return false
	}
	// A second group of 8+ digits is a date suffix (e.g. claude-sonnet-4-20250514),
	// not a minor version; treat minor as 0.
	minor := 0
	if m[2] != "" && len(m[2]) < 8 {
		if v, err := strconv.Atoi(m[2]); err == nil {
			minor = v
		}
	}
	return major > 4 || (major == 4 && minor >= 5)
}

func setBool(dst *bool, v *bool) {
	if v != nil {
		*dst = *v
	}
}

func resolveCacheRetention(r ai.CacheRetention, env map[string]string) ai.CacheRetention {
	if r != "" {
		return r
	}
	// Match pi: PI_CACHE_RETENTION=long opts the default into long retention.
	// Provider-scoped env overrides win over the OS environment (pi 7f29e7a3).
	if getProviderEnvValue("PI_CACHE_RETENTION", env) == "long" {
		return ai.CacheLong
	}
	return ai.CacheShort
}

type cacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

func getCacheControl(model *ai.Model, retention ai.CacheRetention, env map[string]string) (ai.CacheRetention, *cacheControl) {
	r := resolveCacheRetention(retention, env)
	if r == ai.CacheNone {
		return r, nil
	}
	cc := &cacheControl{Type: "ephemeral"}
	if r == ai.CacheLong && getAnthropicCompat(model).supportsLongCacheRetention {
		cc.TTL = "1h"
	}
	return r, cc
}

func isOAuthToken(apiKey string) bool { return strings.Contains(apiKey, "sk-ant-oat") }

// StreamSimpleAnthropic maps unified reasoning to AnthropicOptions then streams.
func StreamSimpleAnthropic(ctx context.Context, model *ai.Model, req ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	var base ai.StreamOptions
	if opts != nil {
		base = opts.StreamOptions
	}
	// pi buildBaseOptions: maxTokens = clamp(options?.maxTokens ?? model.maxTokens),
	// samplingParams = model defaults with the request's merged over them.
	// Anthropic ignores samplingParams when building its body, exactly like pi.
	baseMaxTokens := ai.ClampMaxTokensToContext(model, req, ai.SimpleMaxTokensDefault(model, opts))
	base.MaxTokens = &baseMaxTokens
	base.SamplingParams = ai.MergeSamplingParams(model, opts)
	aopts := AnthropicOptions{StreamOptions: base}
	// pi threads refusalFallbacks into each of streamSimple's three stream()
	// calls; the single options value here covers all three return paths.
	if opts != nil {
		aopts.RefusalFallbacks = opts.RefusalFallbacks
		// The unified option carries only pi's "auto"/"none"; buildParams wraps a
		// bare string as {type: ...}, which is the shape those two need.
		if opts.ToolChoice != "" {
			aopts.ToolChoice = string(opts.ToolChoice)
		}
	}

	reasoning := ai.ThinkingLevel("")
	if opts != nil {
		reasoning = opts.Reasoning
	}
	// pi streamSimpleAnthropic always passes thinkingEnabled explicitly
	// (false for no reasoning, true otherwise) — so Provided is always set here.
	aopts.ThinkingProvided = true
	if reasoning == "" {
		aopts.ThinkingEnabled = false
		return StreamAnthropic(ctx, model, req, &aopts)
	}

	compat := getAnthropicCompat(model)
	if compat.forceAdaptiveThinking {
		aopts.ThinkingEnabled = true
		aopts.Effort = mapThinkingLevelToEffort(model, reasoning)
		return StreamAnthropic(ctx, model, req, &aopts)
	}

	var budgets *ai.ThinkingBudgets
	if opts != nil {
		budgets = opts.ThinkingBudgets
	}
	adjustedMaxTokens, thinkingBudget := adjustMaxTokensForThinking(base.MaxTokens, model.MaxTokens, reasoning, budgets)
	// pi: maxTokens = clampMaxTokensToContext(model, context, adjusted.maxTokens);
	// thinkingBudgetTokens = min(adjusted.thinkingBudget, max(0, maxTokens-1024)).
	mt := ai.ClampMaxTokensToContext(model, req, adjustedMaxTokens)
	aopts.MaxTokens = &mt
	aopts.ThinkingEnabled = true
	aopts.ThinkingBudgetTokens = min(thinkingBudget, max(0, mt-1024))
	return StreamAnthropic(ctx, model, req, &aopts)
}

func mapThinkingLevelToEffort(model *ai.Model, level ai.ThinkingLevel) string {
	if model.ThinkingLevelMap != nil {
		if mapped, ok := model.ThinkingLevelMap[ai.ModelThinkingLevel(level)]; ok && mapped != nil {
			return *mapped
		}
	}
	switch level {
	case ai.ThinkingMinimal, ai.ThinkingLow:
		return "low"
	case ai.ThinkingMedium:
		return "medium"
	case ai.ThinkingHigh:
		return "high"
	default:
		return "high"
	}
}

// minAnswerTokens is pi's MIN_ANSWER_TOKENS (simple-options.ts, d07889da0):
// tokens always left for the answer when a thinking budget shares the response
// ceiling. Shared with the openai-completions top-level thinking budget field
// and with clampThinkingBudgetToAnswerRoom (pi b23741269).
const minAnswerTokens = 1024

// resolveThinkingBudgets merges the caller's per-level overrides over pi's
// default thinking budgets (pi: `{...defaultBudgets, ...customBudgets}`).
func resolveThinkingBudgets(custom *ai.ThinkingBudgets) map[ai.ThinkingLevel]int {
	budgets := map[ai.ThinkingLevel]int{
		ai.ThinkingMinimal: 1024, ai.ThinkingLow: 2048, ai.ThinkingMedium: 8192, ai.ThinkingHigh: 16384,
	}
	if custom != nil {
		if custom.Minimal != nil {
			budgets[ai.ThinkingMinimal] = *custom.Minimal
		}
		if custom.Low != nil {
			budgets[ai.ThinkingLow] = *custom.Low
		}
		if custom.Medium != nil {
			budgets[ai.ThinkingMedium] = *custom.Medium
		}
		if custom.High != nil {
			budgets[ai.ThinkingHigh] = *custom.High
		}
	}
	return budgets
}

// clampReasoning is pi's clampReasoning (simple-options.ts): the token-budget
// tables have no xhigh/max rows, so both levels collapse to high.
func clampReasoning(level ai.ThinkingLevel) ai.ThinkingLevel {
	if level == ai.ThinkingXHigh || level == ai.ThinkingMax {
		return ai.ThinkingHigh
	}
	return level
}

// thinkingBudgetForLevel is pi's thinkingBudgetForLevel (simple-options.ts:68,
// upstream b23741269): the per-level budget after xhigh/max collapse to high,
// read from pi's defaults with the caller's overrides merged over.
func thinkingBudgetForLevel(level ai.ThinkingLevel, custom *ai.ThinkingBudgets) int {
	return resolveThinkingBudgets(custom)[clampReasoning(level)]
}

// clampThinkingBudgetToAnswerRoom is pi's clampThinkingBudgetToAnswerRoom
// (simple-options.ts:75, upstream b23741269): cap a thinking budget so at least
// minAnswerTokens remain when it shares a response ceiling with the answer.
func clampThinkingBudgetToAnswerRoom(thinkingBudget, ceiling int) int {
	return min(thinkingBudget, max(0, ceiling-minAnswerTokens))
}

func adjustMaxTokensForThinking(baseMaxTokens *int, modelMaxTokens int, level ai.ThinkingLevel, custom *ai.ThinkingBudgets) (int, int) {
	thinkingBudget := thinkingBudgetForLevel(level, custom)
	var maxTokens int
	if baseMaxTokens == nil {
		maxTokens = modelMaxTokens
	} else {
		maxTokens = *baseMaxTokens + thinkingBudget
		if maxTokens > modelMaxTokens {
			maxTokens = modelMaxTokens
		}
	}
	if maxTokens <= thinkingBudget {
		thinkingBudget = clampThinkingBudgetToAnswerRoom(thinkingBudget, maxTokens)
	}
	return maxTokens, thinkingBudget
}

var toolIDCleaner = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func normalizeToolCallID(id string) string {
	cleaned := toolIDCleaner.ReplaceAllString(id, "_")
	if len(cleaned) > 64 {
		cleaned = cleaned[:64]
	}
	return cleaned
}

// StreamAnthropic streams an assistant response from the Anthropic Messages API.
func StreamAnthropic(ctx context.Context, model *ai.Model, req ai.Context, opts *AnthropicOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	if opts == nil {
		opts = &AnthropicOptions{}
	}

	go func() {
		output := &ai.AssistantMessage{
			Content: ai.ContentList{}, Api: model.Api, Provider: model.Provider, Model: model.ID,
			StopReason: ai.StopPending, Timestamp: nowMillis(),
		}
		fail := func(err error) {
			if ctx != nil && ctx.Err() != nil {
				output.StopReason = ai.StopAborted
			} else {
				output.StopReason = ai.StopError
			}
			output.ErrorMessage = err.Error()
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: output.StopReason, Error: output})
			stream.End()
		}

		apiKey := opts.APIKey
		// pi anthropicApiKeyAuth.resolve() consults ANTHROPIC_AUTH_TOKEN ahead of
		// the OAuth/API-key env vars and, when set, authenticates via
		// Authorization: Bearer (upstream 24e5cc04). GetEnvApiKey deliberately
		// skips it and withEnvAPIKey leaves APIKey empty when it is active, so the
		// token surfaces here only when no key was resolved — an explicit request
		// key or a stored credential (apiKey != "") wins over it, matching pi's
		// credential-first precedence.
		authToken := ""
		if model.Provider == "anthropic" && apiKey == "" {
			authToken = ai.ProviderEnvValue(ai.AnthropicAuthTokenEnv, opts.Env)
		}
		if apiKey == "" && authToken == "" {
			fail(fmt.Errorf("No API key for provider: %s", model.Provider))
			return
		}
		// pi never sniffs an OAuth token for these two: github-copilot has its own
		// createClient branch ahead of the sniff, and cloudflare-ai-gateway
		// resolves to header-owned auth with no apiKey at all, so
		// isOAuthToken(undefined) is false however the gateway key looks. An
		// auth-token request is a plain bearer credential, not OAuth, so it never
		// triggers the OAuth body either.
		oauth := authToken == "" &&
			model.Provider != "cloudflare-ai-gateway" &&
			model.Provider != "github-copilot" &&
			isOAuthToken(apiKey)

		body, err := buildAnthropicParams(model, req, oauth, opts)
		if err != nil {
			fail(err)
			return
		}
		if opts.OnPayload != nil {
			next, perr := opts.OnPayload(body, model)
			if perr != nil {
				// pi: a throw from onPayload propagates and fails the stream.
				fail(perr)
				return
			}
			// pi: any `!== undefined` return replaces the params wholesale.
			if next != nil {
				if m, ok := next.(map[string]any); ok {
					body = m
				}
			}
		}
		payload, err := json.Marshal(body)
		if err != nil {
			fail(err)
			return
		}

		baseURL := model.BaseURL
		if model.Provider == "cloudflare-ai-gateway" {
			// pi: resolveCloudflareBaseUrl(model) throws on a missing env var,
			// which surfaces as a failed stream.
			resolved, rerr := resolveCloudflareBaseURL(model, opts.Env)
			if rerr != nil {
				fail(rerr)
				return
			}
			baseURL = resolved
		}
		if baseURL == "" {
			baseURL = anthropicDefaultBaseURL
		}
		url := strings.TrimRight(baseURL, "/") + "/v1/messages"
		build := func() (*http.Request, error) {
			r, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
			if err != nil {
				return nil, err
			}
			applyAnthropicHeaders(r, model, opts, oauth, apiKey, authToken, len(req.Tools) > 0, req.Messages)
			return r, nil
		}
		resp, err := sendWithRetry(ctx, build, retryFromOptions(opts.StreamOptions, anthropicSDKErrorMessage))
		if err != nil {
			fail(err)
			return
		}
		defer resp.Body.Close()

		if opts.OnResponse != nil {
			_ = opts.OnResponse(ai.ProviderResponse{Status: resp.StatusCode, Headers: flattenHeaders(resp.Header)}, model)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			data, _ := io.ReadAll(resp.Body)
			fail(formatProviderError("Anthropic", resp.StatusCode, data))
			return
		}

		stream.Push(ai.AssistantMessageEvent{Type: ai.EventStart, Partial: output.Clone()})

		builders := []*blockBuilder{}
		indexMap := map[int]int{}
		materialize := func() {
			content := make(ai.ContentList, len(builders))
			for i, b := range builders {
				content[i] = b.toContent()
			}
			output.Content = content
		}

		// The model to cost against, recomputed on every message_start and read by
		// both cost sites below. A stream that never delivers one prices at the
		// requested model, as pi does (anthropic-messages.ts:543).
		usageModel := model

		sawStart, sawStop := false, false
		err = iterateAnthropicSSE(resp.Body, ctx, func(ev anthropicStreamEvent) error {
			switch ev.Type {
			case "message_start":
				sawStart = true
				if ev.Message != nil {
					output.ResponseID = ev.Message.ID
					// pi eb1f87fa9: the served model replaces the requested one, so a
					// server-side refusal fallback is visible on the message.
					output.Model = ev.Message.Model
					// pi 4809c2abc: a response a server-side refusal fallback served is
					// billed at the SERVING model's rates. Assigned unconditionally, as
					// pi's ternary is, so a second message_start reprices rather than
					// sticking to the first.
					usageModel = anthropicUsageModel(model, output.Model, opts.RefusalFallbacks)
					applyUsage(&output.Usage, ev.Message.Usage, true)
					ai.CalculateCost(usageModel, &output.Usage)
				}
			case "content_block_start":
				if ev.ContentBlock == nil {
					return nil
				}
				var b *blockBuilder
				var evType ai.EventType
				switch ev.ContentBlock.Type {
				case "text":
					// The start event may already carry the block's first chunk; seed
					// the builder with it so the deltas that follow append to it.
					b = &blockBuilder{kind: "text"}
					b.text.WriteString(ev.ContentBlock.Text)
					evType = ai.EventTextStart
				case "thinking":
					b = &blockBuilder{kind: "thinking", thinkingSig: ev.ContentBlock.Signature}
					b.thinking.WriteString(ev.ContentBlock.Thinking)
					evType = ai.EventThinkingStart
				case "redacted_thinking":
					b = &blockBuilder{kind: "thinking", redacted: true, thinkingSig: ev.ContentBlock.Data}
					b.thinking.WriteString("[Reasoning redacted]")
					evType = ai.EventThinkingStart
				case "tool_use":
					name := ev.ContentBlock.Name
					if oauth {
						name = fromClaudeCodeName(name, req.Tools)
					}
					b = &blockBuilder{kind: "toolCall", toolID: ev.ContentBlock.ID, toolName: name, args: map[string]any{}}
					evType = ai.EventToolCallStart
				default:
					return nil
				}
				builders = append(builders, b)
				indexMap[ev.Index] = len(builders) - 1
				materialize()
				stream.Push(ai.AssistantMessageEvent{Type: evType, ContentIndex: len(builders) - 1, Partial: output.Clone()})
			case "content_block_delta":
				idx, ok := indexMap[ev.Index]
				if !ok || ev.Delta == nil {
					return nil
				}
				b := builders[idx]
				// pi only applies a delta when the indexed block has the matching
				// type (anthropic.ts:586-627); mismatches are dropped silently.
				switch ev.Delta.Type {
				case "text_delta":
					if b.kind != "text" {
						return nil
					}
					b.text.WriteString(ev.Delta.Text)
					materialize()
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventTextDelta, ContentIndex: idx, Delta: ev.Delta.Text, Partial: output.Clone()})
				case "thinking_delta":
					if b.kind != "thinking" {
						return nil
					}
					b.thinking.WriteString(ev.Delta.Thinking)
					materialize()
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventThinkingDelta, ContentIndex: idx, Delta: ev.Delta.Thinking, Partial: output.Clone()})
				case "input_json_delta":
					if b.kind != "toolCall" {
						return nil
					}
					b.partialJSON.WriteString(ev.Delta.PartialJSON)
					b.args, b.argsOrder = parseStreamingJSON(b.partialJSON.String())
					materialize()
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventToolCallDelta, ContentIndex: idx, Delta: ev.Delta.PartialJSON, Partial: output.Clone()})
				case "signature_delta":
					if b.kind != "thinking" {
						return nil
					}
					b.thinkingSig += ev.Delta.Signature
				}
			case "content_block_stop":
				idx, ok := indexMap[ev.Index]
				if !ok {
					return nil
				}
				b := builders[idx]
				materialize()
				switch b.kind {
				case "text":
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventTextEnd, ContentIndex: idx, Content: b.text.String(), Partial: output.Clone()})
				case "thinking":
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventThinkingEnd, ContentIndex: idx, Content: b.thinking.String(), Partial: output.Clone()})
				case "toolCall":
					b.args, b.argsOrder = parseStreamingJSON(b.partialJSON.String())
					materialize()
					tc := b.toContent().(ai.ToolCall)
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventToolCallEnd, ContentIndex: idx, ToolCall: &tc, Partial: output.Clone()})
				}
			case "message_delta":
				if ev.Delta != nil && ev.Delta.StopReason != "" {
					output.RawStopReason = ev.Delta.StopReason
					sr, errMsg, err := mapAnthropicStopReason(ev.Delta.StopReason, ev.Delta.StopDetails)
					if err != nil {
						return err
					}
					output.StopReason = sr
					if errMsg != "" {
						output.ErrorMessage = errMsg
					}
				}
				if ev.Usage != nil {
					applyUsage(&output.Usage, *ev.Usage, false)
					ai.CalculateCost(usageModel, &output.Usage)
				}
			case "message_stop":
				sawStop = true
			}
			return nil
		})

		if err != nil {
			fail(err)
			return
		}
		if sawStart && !sawStop {
			fail(fmt.Errorf("Anthropic stream ended before message_stop"))
			return
		}
		if ctx != nil && ctx.Err() != nil {
			fail(fmt.Errorf("Request was aborted"))
			return
		}
		if output.StopReason == ai.StopPending {
			fail(fmt.Errorf("Anthropic stream ended without a stop reason"))
			return
		}
		if output.StopReason == ai.StopAborted || output.StopReason == ai.StopError {
			msg := output.ErrorMessage
			if msg == "" {
				msg = "An unknown error occurred"
			}
			fail(fmt.Errorf("%s", msg))
			return
		}

		materialize()
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: output.StopReason, Message: output})
		stream.End()
	}()

	return stream
}

type blockBuilder struct {
	kind        string
	text        strings.Builder
	thinking    strings.Builder
	thinkingSig string
	redacted    bool
	toolID      string
	toolName    string
	// toolNamespace is the OpenAI Responses namespace of a namespaced or
	// dynamically loaded tool call; empty for every other provider.
	toolNamespace string
	partialJSON   strings.Builder
	args          map[string]any
	// argsOrder is args in the key order the model streamed them in.
	argsOrder ai.OrderedObject
	// grammar is set on custom (grammar-constrained) tool calls, whose raw input
	// is re-synthesized into JSON deltas instead of being parsed from partialJSON.
	grammar *grammarInputBuffer
}

func (b *blockBuilder) toContent() ai.Content {
	switch b.kind {
	case "text":
		return ai.TextContent{Text: b.text.String()}
	case "thinking":
		return ai.ThinkingContent{Thinking: b.thinking.String(), ThinkingSignature: b.thinkingSig, Redacted: b.redacted}
	case "toolCall":
		args := b.args
		if args == nil {
			args = map[string]any{}
		}
		return ai.ToolCall{ID: b.toolID, Name: b.toolName, Arguments: args, ArgumentsOrder: b.argsOrder, Namespace: b.toolNamespace}
	}
	return ai.TextContent{}
}

// ---- request building ----

func buildAnthropicParams(model *ai.Model, req ai.Context, oauth bool, opts *AnthropicOptions) (map[string]any, error) {
	retention := ai.CacheRetention("")
	var env map[string]string
	if opts != nil {
		retention = opts.CacheRetention
		env = opts.Env
	}
	_, cc := getCacheControl(model, retention, env)
	compat := getAnthropicCompat(model)

	maxTokens := model.MaxTokens
	if opts != nil && opts.MaxTokens != nil {
		maxTokens = *opts.MaxTokens
	}

	// pi hoists transformMessages out of convertMessages so the tool split and the
	// message conversion see the same normalized transcript.
	transformedMessages := transformMessages(req.Messages, model, normalizeToolCallID)
	normalizeToolName := ai.ToolNameNormalizer(func(name string) string { return name })
	if oauth {
		normalizeToolName = toClaudeCodeName
	}
	placement := ai.SplitDeferredTools(
		ai.Context{SystemPrompt: req.SystemPrompt, Messages: transformedMessages, Tools: req.Tools},
		compat.supportsToolReferences,
		normalizeToolName,
	)
	immediateTools := placement.Immediate
	deferredTools := placement.Deferred
	// If every current tool is deferred there is no prefix to anchor references
	// against; promote them back to immediate (the safe, cache-wiping path).
	if len(immediateTools) == 0 && len(deferredTools) > 0 {
		immediateTools = deferredTools
		deferredTools = nil
	}
	deferredToolNames := map[string]bool{}
	for _, tool := range deferredTools {
		deferredToolNames[normalizeToolName(tool.Name)] = true
	}

	params := map[string]any{
		"model":      model.ID,
		"messages":   convertAnthropicMessages(transformedMessages, oauth, cc, compat.allowEmptySignature, deferredToolNames, normalizeToolName),
		"max_tokens": maxTokens,
		"stream":     true,
	}

	textBlock := func(text string) map[string]any {
		blk := map[string]any{"type": "text", "text": sanitizeSurrogates(text)}
		if cc != nil {
			blk["cache_control"] = cc
		}
		return blk
	}
	if oauth {
		system := []any{textBlock("You are Claude Code, Anthropic's official CLI for Claude.")}
		if req.SystemPrompt != "" {
			system = append(system, textBlock(req.SystemPrompt))
		}
		params["system"] = system
	} else if req.SystemPrompt != "" {
		params["system"] = []any{textBlock(req.SystemPrompt)}
	}

	// pi: `!options?.thinkingEnabled` — only an explicit thinkingEnabled:true
	// suppresses temperature; unset (not Provided) behaves like false.
	thinkingOn := opts != nil && opts.ThinkingProvided && opts.ThinkingEnabled
	if opts != nil && opts.Temperature != nil && !thinkingOn && compat.supportsTemperature {
		params["temperature"] = *opts.Temperature
	}

	if len(immediateTools) > 0 || len(deferredTools) > 0 {
		var toolCC *cacheControl
		if compat.supportsCacheControlOnTools {
			toolCC = cc
		}
		tools, err := convertAnthropicTools(immediateTools, oauth, compat.supportsEagerToolInputStreaming, compat.supportsStrictTools, toolCC, false)
		if err != nil {
			return nil, err
		}
		deferred, err := convertAnthropicTools(deferredTools, oauth, compat.supportsEagerToolInputStreaming, compat.supportsStrictTools, nil, true)
		if err != nil {
			return nil, err
		}
		params["tools"] = append(tools, deferred...)
	}

	// pi tri-state (anthropic.ts:950-978): thinkingEnabled undefined omits the
	// thinking key entirely; explicit true enables; explicit false sends
	// {type:"disabled"} — unless the model's thinkingLevelMap carries an
	// explicit off:null (present-nil), which marks "disabled" as unsupported
	// and omits the key too (pi 9ccfcd7c: `thinkingLevelMap?.off !== null`).
	if model.Reasoning && opts != nil && opts.ThinkingProvided {
		if opts.ThinkingEnabled {
			display := opts.ThinkingDisplay
			if display == "" {
				display = "summarized"
			}
			if compat.forceAdaptiveThinking {
				thinking := map[string]any{"type": "adaptive", "display": display}
				params["thinking"] = thinking
				if opts.Effort != "" {
					params["output_config"] = map[string]any{"effort": opts.Effort}
				}
			} else {
				budget := opts.ThinkingBudgetTokens
				if budget == 0 {
					budget = 1024
				}
				params["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget, "display": display}
			}
		} else if off, present := model.ThinkingLevelMap["off"]; !present || off != nil {
			params["thinking"] = map[string]any{"type": "disabled"}
		}
	}

	if opts != nil && opts.Metadata != nil {
		if uid, ok := opts.Metadata["user_id"].(string); ok {
			params["metadata"] = map[string]any{"user_id": uid}
		}
	}

	if opts != nil && opts.ToolChoice != nil {
		switch tc := opts.ToolChoice.(type) {
		case string:
			params["tool_choice"] = map[string]any{"type": tc}
		default:
			params["tool_choice"] = tc
		}
	}

	// Server-side refusal fallback. pi appends this last, after every other key
	// (anthropic-messages.ts buildParams). The value serializes to "default" or
	// [{model}] only: local pricing on a target is stripped by the union's
	// MarshalJSON, which is where pi's explicit `.map(f => ({ model: f.model }))`
	// lands — one projection there covers every serialization path of the
	// collapsed Go union (upstream 4809c2abc).
	if opts != nil && opts.RefusalFallbacks != nil {
		params["fallbacks"] = *opts.RefusalFallbacks
	}

	return params, nil
}

func convertAnthropicTools(tools []ai.Tool, oauth, eager, supportsStrictTools bool, cc *cacheControl, deferLoading bool) ([]map[string]any, error) {
	out := make([]map[string]any, len(tools))
	for i, t := range tools {
		name := t.Name
		if oauth {
			name = toClaudeCodeName(name)
		}
		strict, err := resolveJSONSchemaStrictSampling(t, supportsStrictTools)
		if err != nil {
			return nil, err
		}
		parameters, err := jsonSchemaToolParameters(t, strict)
		if err != nil {
			return nil, err
		}
		var full map[string]any
		props := map[string]any{}
		var required []string
		if parameters != nil {
			if raw, err := json.Marshal(parameters); err == nil {
				if strict {
					// Only strict tools carry the full schema, so skip this
					// decode on the common path (it runs per tool per request).
					// Errors are ignored throughout: a schema that will not
					// round-trip yields the legacy shape, as pi's spread does.
					_ = json.Unmarshal(raw, &full)
				}
				var sch struct {
					Properties json.RawMessage `json:"properties"`
					Required   []string        `json:"required"`
				}
				_ = json.Unmarshal(raw, &sch)
				if len(sch.Properties) > 0 {
					_ = json.Unmarshal(sch.Properties, &props)
				}
				required = sch.Required
			}
		}
		if required == nil {
			required = []string{}
		}
		// The legacy {type, properties, required} shape is always sent; strict
		// tools carry the rest of the JSON Schema underneath it (pi spreads the
		// full parameters first, then the legacy shape over the top).
		inputSchema := map[string]any{
			"type":       "object",
			"properties": props,
			"required":   required,
		}
		if strict {
			if full == nil {
				full = map[string]any{}
			}
			maps.Copy(full, inputSchema)
			inputSchema = full
		}
		tool := map[string]any{
			"name":        name,
			"description": t.Description,
		}
		if eager {
			tool["eager_input_streaming"] = true
		}
		if strict {
			tool["strict"] = true
		}
		tool["input_schema"] = inputSchema
		if deferLoading {
			tool["defer_loading"] = true
		}
		if cc != nil && i == len(tools)-1 {
			tool["cache_control"] = cc
		}
		out[i] = tool
	}
	return out, nil
}

func convertAnthropicMessages(transformed []ai.Message, oauth bool, cc *cacheControl, allowEmptySig bool, deferredToolNames map[string]bool, normalizeToolName ai.ToolNameNormalizer) []map[string]any {
	if normalizeToolName == nil {
		normalizeToolName = func(name string) string { return name }
	}
	var params []map[string]any
	loadedToolNames := map[string]bool{}

	for i := 0; i < len(transformed); i++ {
		m := transformed[i]
		if um, ok := asUserMsg(m); ok {
			blocks := convertUserBlocks(um.Content)
			if len(blocks) == 0 {
				continue
			}
			params = append(params, map[string]any{"role": "user", "content": blocks})
		} else if am, ok := asAssistantMsg(m); ok {
			blocks := convertAssistantBlocks(am, oauth, allowEmptySig)
			if len(blocks) == 0 {
				continue
			}
			params = append(params, map[string]any{"role": "assistant", "content": blocks})
		} else if _, ok := asToolResultMsg(m); ok {
			// Collect all consecutive toolResult messages (needed for z.ai's
			// Anthropic endpoint). Reference-bearing results displace their ordinary
			// content to sibling blocks, since Anthropic rejects tool references
			// mixed with tool-result content.
			var toolResults []any
			var siblingContent []any
			j := i
			for j < len(transformed) {
				next, ok := asToolResultMsg(transformed[j])
				if !ok {
					break
				}
				res := convertToolResult(next, oauth, deferredToolNames, loadedToolNames, normalizeToolName)
				toolResults = append(toolResults, res.toolResult)
				siblingContent = append(siblingContent, res.siblingContent...)
				j++
			}
			i = j - 1
			// Displaced reference-bearing results must follow every tool_result block.
			content := append(toolResults, siblingContent...)
			params = append(params, map[string]any{"role": "user", "content": content})
		}
	}

	// Cache the conversation history by marking the last user block.
	if cc != nil && len(params) > 0 {
		last := params[len(params)-1]
		if last["role"] == "user" {
			if content, ok := last["content"].([]any); ok && len(content) > 0 {
				if blk, ok := content[len(content)-1].(map[string]any); ok {
					t, _ := blk["type"].(string)
					if t == "text" || t == "image" || t == "tool_result" {
						blk["cache_control"] = cc
					}
				}
			}
		}
	}
	return params
}

func convertUserBlocks(content ai.ContentList) []any {
	var blocks []any
	for _, b := range content {
		switch v := b.(type) {
		case ai.TextContent:
			if strings.TrimSpace(v.Text) == "" {
				continue
			}
			blocks = append(blocks, map[string]any{"type": "text", "text": sanitizeSurrogates(v.Text)})
		case ai.ImageContent:
			blocks = append(blocks, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type": "base64", "media_type": v.MimeType, "data": v.Data,
				},
			})
		}
	}
	return blocks
}

func convertAssistantBlocks(am *ai.AssistantMessage, oauth, allowEmptySig bool) []any {
	var blocks []any
	for _, b := range am.Content {
		switch v := b.(type) {
		case ai.TextContent:
			if strings.TrimSpace(v.Text) == "" {
				continue
			}
			blocks = append(blocks, map[string]any{"type": "text", "text": sanitizeSurrogates(v.Text)})
		case ai.ThinkingContent:
			if v.Redacted {
				blocks = append(blocks, map[string]any{"type": "redacted_thinking", "data": v.ThinkingSignature})
				continue
			}
			hasThinkingSignature := strings.TrimSpace(v.ThinkingSignature) != ""
			// Keep a thinking block when it carries a real signature even if its
			// text is empty (#6457); only drop it when both are empty.
			if strings.TrimSpace(v.Thinking) == "" && !hasThinkingSignature {
				continue
			}
			// If the signature is missing/empty (e.g., from an aborted stream),
			// convert to plain text for Anthropic. Some compatible providers emit
			// and accept empty signatures, so let marked models preserve the block.
			if !hasThinkingSignature {
				if allowEmptySig {
					blocks = append(blocks, map[string]any{"type": "thinking", "thinking": sanitizeSurrogates(v.Thinking), "signature": ""})
				} else {
					blocks = append(blocks, map[string]any{"type": "text", "text": sanitizeSurrogates(v.Thinking)})
				}
			} else {
				blocks = append(blocks, map[string]any{"type": "thinking", "thinking": sanitizeSurrogates(v.Thinking), "signature": v.ThinkingSignature})
			}
		case ai.ToolCall:
			name := v.Name
			if oauth {
				name = toClaudeCodeName(name)
			}
			blocks = append(blocks, map[string]any{"type": "tool_use", "id": v.ID, "name": name, "input": orEmptyArguments(v)})
		}
	}
	return blocks
}

// convertedToolResult is a tool_result block plus any ordinary content displaced
// to sibling blocks because the block carries tool references instead.
type convertedToolResult struct {
	toolResult     map[string]any
	siblingContent []any
}

// convertToolResult builds a tool_result block. When the result's AddedToolNames
// introduce still-unloaded deferred tools, the block's content becomes
// tool_reference blocks (deduped via loadedToolNames) and the ordinary content is
// displaced to sibling blocks. Port of pi's convertToolResult.
func convertToolResult(tr ai.ToolResultMessage, oauth bool, deferredToolNames, loadedToolNames map[string]bool, normalizeToolName ai.ToolNameNormalizer) convertedToolResult {
	var references []any
	for _, name := range tr.AddedToolNames {
		normalizedName := normalizeToolName(name)
		if !deferredToolNames[normalizedName] || loadedToolNames[normalizedName] {
			continue
		}
		loadedToolNames[normalizedName] = true
		refName := name
		if oauth {
			refName = toClaudeCodeName(name)
		}
		references = append(references, map[string]any{"type": "tool_reference", "tool_name": refName})
	}

	convertedContent := convertContentBlocks(tr.Content)
	var content any = convertedContent
	if len(references) > 0 {
		content = references
	}
	result := map[string]any{
		"type":        "tool_result",
		"tool_use_id": tr.ToolCallID,
		"content":     content,
		"is_error":    tr.IsError,
	}

	var sibling []any
	if len(references) > 0 {
		switch cv := convertedContent.(type) {
		case string:
			sibling = []any{map[string]any{"type": "text", "text": cv}}
		case []any:
			sibling = cv
		}
	}
	return convertedToolResult{toolResult: result, siblingContent: sibling}
}

// convertContentBlocks returns either a concatenated string (text-only) or a
// content-block array (with images).
func convertContentBlocks(content ai.ContentList) any {
	hasImages := false
	for _, c := range content {
		if _, ok := c.(ai.ImageContent); ok {
			hasImages = true
			break
		}
	}
	if !hasImages {
		var texts []string
		for _, c := range content {
			if tc, ok := c.(ai.TextContent); ok {
				texts = append(texts, tc.Text)
			}
		}
		return sanitizeSurrogates(strings.Join(texts, "\n"))
	}
	var blocks []any
	hasText := false
	for _, c := range content {
		switch v := c.(type) {
		case ai.TextContent:
			hasText = true
			blocks = append(blocks, map[string]any{"type": "text", "text": sanitizeSurrogates(v.Text)})
		case ai.ImageContent:
			blocks = append(blocks, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": v.MimeType, "data": v.Data}})
		}
	}
	if !hasText {
		blocks = append([]any{map[string]any{"type": "text", "text": "(see attached image)"}}, blocks...)
	}
	return blocks
}

func applyAnthropicHeaders(r *http.Request, model *ai.Model, opts *AnthropicOptions, oauth bool, apiKey, authToken string, hasTools bool, messages []ai.Message) {
	r.Header.Set("content-type", "application/json")
	r.Header.Set("accept", "application/json")
	r.Header.Set("anthropic-version", anthropicVersion)
	r.Header.Set("anthropic-dangerous-direct-browser-access", "true")

	// pi mergeProviderAttributionHeaders (sdk.ts) puts the attribution bundle at
	// the bottom of the precedence stack: emit session + default attribution
	// first so the provider auth headers, model.Headers, and opts.Headers all
	// override them.
	applyAttributionDefaults(r.Header.Set, model, opts.SessionID)

	compat := getAnthropicCompat(model)
	var betas []string
	if hasTools && !compat.supportsEagerToolInputStreaming {
		betas = append(betas, fineGrainedToolStreamBeta)
	}
	interleaved := true
	if opts.InterleavedThinking != nil {
		interleaved = *opts.InterleavedThinking
	}
	if interleaved && !compat.forceAdaptiveThinking {
		betas = append(betas, interleavedThinkingBeta)
	}
	// pi appends the fallback beta third, and every auth branch below carries the
	// same list — copilot and OAuth included.
	if opts.RefusalFallbacks != nil {
		betas = append(betas, serverSideFallbackBeta)
	}

	// Cloudflare AI Gateway auth is header-owned: pi's resolver returns
	// cf-aig-authorization plus markers suppressing x-api-key/Authorization, and
	// no api key at all, so pi's createClient takes its plain api-key branch and
	// the SDK sends no x-api-key. The Go port resolves that auth inline here
	// (2026-06-24 divergence), so the key is consumed by the marker bundle and
	// branchKey — the credential the branches below may emit — is empty, which
	// is what pi's createClient sees. The OAuth sniff is unaffected: the caller
	// computed `oauth` from the real apiKey and already excludes this provider.
	branchKey := apiKey
	var providerAuthHeaders ai.ProviderHeaders
	if model.Provider == "cloudflare-ai-gateway" {
		providerAuthHeaders = cloudflareAIGatewayAuthHeaders(apiKey)
		branchKey = ""
	}

	// Branch order mirrors pi createClient (anthropic-messages.ts): github-copilot,
	// then the OAuth sniff, then plain api-key auth. The ANTHROPIC_AUTH_TOKEN
	// bearer path (upstream 24e5cc04) sits ahead of them: it is set only for the
	// anthropic provider and, like pi's resolve(), sends Authorization: Bearer
	// with the normal (non-OAuth) betas.
	switch {
	case authToken != "":
		r.Header.Set("authorization", "Bearer "+authToken)
		if len(betas) > 0 {
			r.Header.Set("anthropic-beta", strings.Join(betas, ","))
		}
	case model.Provider == "github-copilot":
		r.Header.Set("authorization", "Bearer "+branchKey)
		if len(betas) > 0 {
			r.Header.Set("anthropic-beta", strings.Join(betas, ","))
		}
	case oauth:
		r.Header.Set("authorization", "Bearer "+branchKey)
		r.Header.Set("user-agent", "claude-cli/"+claudeCodeVersion)
		r.Header.Set("x-app", "cli")
		oauthBetas := append([]string{"claude-code-20250219", "oauth-2025-04-20"}, betas...)
		r.Header.Set("anthropic-beta", strings.Join(oauthBetas, ","))
	default:
		r.Header.Set("x-api-key", branchKey)
		if len(betas) > 0 {
			r.Header.Set("anthropic-beta", strings.Join(betas, ","))
		}
		// pi anthropic.ts:496-497: cacheSessionId is dropped when the effective
		// cacheRetention is "none", so no session-affinity header is sent.
		if opts.SessionID != "" && compat.sendSessionAffinityHeaders &&
			resolveCacheRetention(opts.CacheRetention, opts.Env) != ai.CacheNone {
			r.Header.Set("x-session-affinity", opts.SessionID)
		}
	}

	// Header-owned provider auth sits above attribution and below the
	// model/consumer headers, where pi's auth.headers enter the merge.
	applyProviderHeaders(r.Header, providerAuthHeaders)
	applyProviderHeaders(r.Header, model.Headers)
	// pi merges copilotDynamicHeaders after model.headers, before options
	// headers (anthropic-messages.ts createClient).
	if model.Provider == "github-copilot" {
		for k, v := range buildCopilotDynamicHeaders(messages, hasCopilotVisionInput(messages)) {
			r.Header.Set(k, v)
		}
	}
	// pi options.headers (consumer) are spread last and win over everything
	// above, including model.Headers and the attribution defaults — a deletion
	// marker here suppresses any of them.
	applyProviderHeaders(r.Header, opts.Headers)
	// pi mergeClientHeaders (anthropic-messages.ts, upstream 9d2ec7ffa): Kimi
	// For Coding requests always carry pi's runtime user agent, so the
	// override outranks the catalog's static header (KimiCLI/1.5 at npm
	// 0.84.1), the OAuth claude-cli identity, and consumer opts.Headers.
	if model.Provider == "kimi-coding" {
		forcePiUserAgent(r.Header)
	}
}

// mapAnthropicStopReason maps an Anthropic stop_reason to the unified
// StopReason and, for a refusal, surfaces the stop_details explanation as an
// error message (pi anthropic.ts mapStopReason returns {stopReason,
// errorMessage?}).
func mapAnthropicStopReason(reason string, stopDetails *struct {
	Type        string `json:"type"`
	Explanation string `json:"explanation"`
}) (ai.StopReason, string, error) {
	switch reason {
	case "end_turn":
		return ai.StopStop, "", nil
	case "max_tokens":
		return ai.StopLength, "", nil
	case "tool_use":
		return ai.StopToolUse, "", nil
	case "refusal":
		explanation := "The model refused to complete the request"
		if stopDetails != nil && stopDetails.Explanation != "" {
			explanation = stopDetails.Explanation
		}
		return ai.StopError, explanation, nil
	case "pause_turn", "stop_sequence":
		return ai.StopStop, "", nil
	case "sensitive": // Content flagged by safety filters (not yet in SDK types)
		return ai.StopError, providerStoppedPrefix + "sensitive", nil
	default:
		return "", "", fmt.Errorf("Unhandled stop reason: %s", reason)
	}
}

// ---- SSE parsing ----

type anthropicUsage struct {
	InputTokens              *int `json:"input_tokens"`
	OutputTokens             *int `json:"output_tokens"`
	CacheReadInputTokens     *int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens"`
	CacheCreation            *struct {
		Ephemeral1hInputTokens *int `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
	// OutputTokensDetails carries the reasoning breakdown on the final
	// message_delta usage. ThinkingTokens is a subset of OutputTokens. pi reads
	// this through a narrow cast since the SDK type omits it; we model it
	// directly and only apply it when present.
	OutputTokensDetails *struct {
		ThinkingTokens *int `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

type anthropicStreamEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		ID string `json:"id"`
		// Model is the model Anthropic actually served, which differs from the
		// requested one when a server-side refusal fallback fired.
		Model string         `json:"model"`
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type string `json:"type"`
		// Text/Thinking/Signature can already carry content on content_block_start;
		// later deltas append to it rather than replacing it.
		Text      string          `json:"text"`
		Thinking  string          `json:"thinking"`
		Signature string          `json:"signature"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		Data      string          `json:"data"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		Signature   string `json:"signature"`
		StopReason  string `json:"stop_reason"`
		StopDetails *struct {
			Type        string `json:"type"`
			Explanation string `json:"explanation"`
		} `json:"stop_details"`
	} `json:"delta"`
	Usage *anthropicUsage `json:"usage"`
}

var anthropicMessageEvents = map[string]bool{
	"message_start": true, "message_delta": true, "message_stop": true,
	"content_block_start": true, "content_block_delta": true, "content_block_stop": true,
}

// scanSSELines is a bufio.SplitFunc that treats \r, \n, and \r\n all as line
// breaks, mirroring pi's SSE decoder (anthropic.ts consumeLine/nextLineBreakIndex).
func scanSSELines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\n':
			return i + 1, data[:i], nil
		case '\r':
			if i+1 < len(data) {
				if data[i+1] == '\n' {
					return i + 2, data[:i], nil
				}
				return i + 1, data[:i], nil
			}
			if atEOF {
				return i + 1, data[:i], nil
			}
			// A trailing \r might be half of a \r\n pair; wait for more data.
			return 0, nil, nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// iterateAnthropicSSE parses the SSE body and invokes handle for each known event.
func iterateAnthropicSSE(body io.Reader, ctx context.Context, handle func(anthropicStreamEvent) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	scanner.Split(scanSSELines)

	var eventName string
	var dataLines []string

	flush := func() error {
		if eventName == "" && len(dataLines) == 0 {
			return nil
		}
		name := eventName
		data := strings.Join(dataLines, "\n")
		eventName = ""
		dataLines = nil

		if name == "error" {
			return fmt.Errorf("%s", data)
		}
		if !anthropicMessageEvents[name] {
			return nil
		}
		var ev anthropicStreamEvent
		if err := parseJSONWithRepair(data, &ev); err != nil {
			return fmt.Errorf("Could not parse Anthropic SSE event %s: %v; data=%s", name, err, data)
		}
		return handle(ev)
	}

	for scanner.Scan() {
		if ctx != nil && ctx.Err() != nil {
			return fmt.Errorf("Request was aborted")
		}
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		idx := strings.IndexByte(line, ':')
		var field, value string
		if idx == -1 {
			field = line
		} else {
			field = line[:idx]
			value = line[idx+1:]
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "event":
			eventName = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

func applyUsage(usage *ai.Usage, u anthropicUsage, isStart bool) {
	if isStart {
		usage.Input = derefOr(u.InputTokens, 0)
		usage.Output = derefOr(u.OutputTokens, 0)
		usage.CacheRead = derefOr(u.CacheReadInputTokens, 0)
		usage.CacheWrite = derefOr(u.CacheCreationInputTokens, 0)
		if u.CacheCreation != nil {
			usage.CacheWrite1h = derefOr(u.CacheCreation.Ephemeral1hInputTokens, 0)
		}
	} else {
		if u.InputTokens != nil {
			usage.Input = *u.InputTokens
		}
		if u.OutputTokens != nil {
			usage.Output = *u.OutputTokens
		}
		if u.CacheReadInputTokens != nil {
			usage.CacheRead = *u.CacheReadInputTokens
		}
		if u.CacheCreationInputTokens != nil {
			usage.CacheWrite = *u.CacheCreationInputTokens
		}
	}
	// Anthropic reports reasoning tokens in output_tokens_details.thinking_tokens
	// on the final message_delta usage (a subset of output_tokens). pi only sets
	// reasoning when the field is present; mirror that (don't overwrite with 0).
	if u.OutputTokensDetails != nil && u.OutputTokensDetails.ThinkingTokens != nil {
		usage.Reasoning = *u.OutputTokensDetails.ThinkingTokens
	}
	usage.TotalTokens = usage.Input + usage.Output + usage.CacheRead + usage.CacheWrite
}

func derefOr(p *int, d int) int {
	if p != nil {
		return *p
	}
	return d
}

func flattenHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		if len(v) > 0 {
			out[strings.ToLower(k)] = v[0]
		}
	}
	return out
}

// RegisterAnthropic registers the anthropic-messages api provider.
func RegisterAnthropic() {
	ai.RegisterApiProvider(ai.ApiProvider{
		Api: ai.APIAnthropicMessages,
		Stream: func(ctx context.Context, model *ai.Model, req ai.Context, opts *ai.StreamOptions) *ai.AssistantMessageEventStream {
			aopts := &AnthropicOptions{}
			if opts != nil {
				aopts.StreamOptions = *opts
			}
			return StreamAnthropic(ctx, model, req, aopts)
		},
		StreamSimple: StreamSimpleAnthropic,
	})
}
