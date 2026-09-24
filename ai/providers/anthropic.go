package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"regexp"
	"strings"

	"github.com/sky-valley/pi/ai"
	"github.com/sky-valley/pi/internal/jstext"
)

const (
	anthropicVersion          = "2023-06-01"
	fineGrainedToolStreamBeta = "fine-grained-tool-streaming-2025-05-14"
	interleavedThinkingBeta   = "interleaved-thinking-2025-05-14"
	serverSideFallbackBeta    = "server-side-fallback-2026-07-01"
	// The two betas that turn on managed ("mid-conversation") effort: an
	// effort-only system message per turn, plus the thinking block_binding
	// controls that let a prefix mismatch drop a block instead of 400ing
	// (upstream 4e69b0c28).
	midConvoOutputConfigBeta = "mid-conversation-output-config-2026-07-01"
	thinkingBindingBeta      = "thinking-binding-controls-2026-08-01"
	// midConvoToolChangesBeta turns on tool_addition/tool_removal blocks in
	// mid-conversation system messages (upstream 9e05370b2).
	midConvoToolChangesBeta = "mid-conversation-tool-changes-2026-07-01"
	claudeCodeVersion       = "2.1.280"
	anthropicDefaultBaseURL = "https://api.anthropic.com"
)

// deferredToolPlaceholder is pi's DEFERRED_TOOL_PLACEHOLDER: a stable deferred
// tool declared whenever native tool changes are in use. Anthropic adds hidden
// prompt scaffolding as soon as any tool has `defer_loading`; declaring this
// placeholder from the first request keeps that scaffolding in the cached
// prefix, so the first real late tool does not invalidate the cache (measured
// upstream: a full miss without it). It is never activated and the model cannot
// see it.
//
// pi shares one module-level object; this builds a fresh one per request,
// because the request body is handed to OnPayload, and a hook that edits the
// map it was given must not change the next request's placeholder.
func deferredToolPlaceholder() map[string]any {
	return map[string]any{
		"name":          "__pi_deferred_placeholder__",
		"description":   "Reserved placeholder. Never available. Never call this.",
		"input_schema":  map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{}},
		"defer_loading": true,
	}
}

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
}

// AnthropicCompat holds resolved Anthropic-messages compatibility flags.
type anthropicCompat struct {
	supportsEagerToolInputStreaming bool
	supportsLongCacheRetention      bool
	sendSessionAffinityHeaders      bool
	// sessionAffinityFormat selects the session-affinity header name (pi
	// AnthropicMessagesCompat.sessionAffinityFormat, upstream bbb61e34a).
	// "openrouter" sends x-session-id; EVERY other value, "" (pi's undefined)
	// included, sends x-session-affinity. Unlike the completions compat this is
	// a single literal upstream, not the three-member format union — it selects
	// a name, and it is never a second enable switch.
	sessionAffinityFormat       string
	supportsCacheControlOnTools bool
	supportsTemperature         bool
	allowEmptySignature         bool
	forceAdaptiveThinking       bool
	// supportsStrictTools reports whether the provider accepts Anthropic strict
	// tool schemas. Default: false.
	supportsStrictTools bool
	// supportsMidConvoEffort reports whether this exact model transport accepts
	// effort-only system messages and thinking binding controls (pi
	// AnthropicMessagesCompat.supportsMidConvoEffort, upstream 4e69b0c28).
	// Default: false. It is a transport capability, not a preference: when set,
	// EVERY request on the model is adaptive-thinking with drop_block binding and
	// carries its per-turn effort in the message list instead of in
	// output_config, and temperature is never sent.
	supportsMidConvoEffort bool
	// supportsMidConvoSystemMessages reports whether the model accepts system
	// messages after the conversation has started (pi
	// AnthropicMessagesCompat.supportsMidConvoSystemMessages, upstream
	// 9e05370b2). Default: false, which folds every later system message into
	// the leading system prompt before the request is built.
	supportsMidConvoSystemMessages bool
	// supportsMidConvoToolChanges reports whether the model accepts
	// tool_addition/tool_removal blocks in those system messages (pi
	// AnthropicMessagesCompat.supportsMidConvoToolChanges, upstream 9e05370b2).
	// Default: false. It takes effect only together with
	// supportsMidConvoSystemMessages.
	supportsMidConvoToolChanges bool
	// allowedFallbackModels are the catalog's permitted server-side fallback
	// models: what the request's `fallbacks` field lists, what turns the fallback
	// beta on, and where the local pricing used to cost a response one of them
	// actually served comes from (pi
	// AnthropicMessagesCompat.allowedFallbackModels).
	allowedFallbackModels []anthropicAllowedFallbackModel
}

// anthropicAllowedFallbackModel is one entry of the catalog's
// `compat.allowedFallbackModels` (pi AnthropicAllowedFallbackModel, upstream
// ed867e909). Go models the compat blob as raw JSON, so the shape lives next to
// the decoder that consumes it rather than in the ai package.
//
// Cost is a pointer even though pi now types it as required, because pi's
// requirement is a TypeScript contract over a value the catalog delivers as
// untyped JSON, and the runtime read is still `find(...)?.cost` under a
// truthiness gate. A pointer reproduces that read on every blob a generator
// could emit: an entry with pricing swaps (a zero-priced one included — an
// object is truthy in JS), while a missing or null `cost` yields no swap, the
// same as `undefined`. Reading it as a value would invent free pricing for both.
type anthropicAllowedFallbackModel struct {
	Provider string        `json:"provider"`
	Model    string        `json:"model"`
	Cost     *ai.ModelCost `json:"cost"`
}

// anthropicFallbackWire is the provider-facing form of one permitted fallback:
// pi projects every entry down to `{ model }` before the request, so neither the
// provider id nor the local pricing reaches Anthropic (pi's explicit
// `.map(fallback => ({ model: fallback.model }))` in buildParams, upstream
// ed867e909). Keeping the projection in a type rather than in a literal makes
// the stripping structural — there is no field here for a provider or a cost to
// travel in.
type anthropicFallbackWire struct {
	Model string `json:"model"`
}

func getAnthropicCompat(model *ai.Model) anthropicCompat {
	// pi 6184307c removed provider/baseUrl auto-detection here, and bbb61e34a
	// brought it back for the two session-affinity keys ALONE: OpenRouter
	// defaults to sending, under its own header name. Every other default stays
	// OpenAI-standard, with fireworks / cloudflare-ai-gateway-anthropic values
	// supplied explicitly by the catalog (model.compat), and
	// sendSessionAffinityHeaders still defaults to false everywhere else.
	isOpenRouter := model.Provider == "openrouter" || strings.Contains(model.BaseURL, "openrouter.ai")
	c := anthropicCompat{
		supportsEagerToolInputStreaming: true,
		supportsLongCacheRetention:      true,
		sendSessionAffinityHeaders:      isOpenRouter,
		supportsCacheControlOnTools:     true,
		supportsTemperature:             true,
	}
	// pi's `?? (isOpenRouter ? "openrouter" : undefined)`: off OpenRouter the
	// format stays unset, which is why sessionAffinityFormatFor is not reused —
	// its "openai" would name a format this adapter does not have.
	if isOpenRouter {
		c.sessionAffinityFormat = sessionAffinityOpenRouter
	}

	// Each key is resolved on its own, as pi's `model.compat?.<key> ?? default`
	// does — a type-mismatched key costs only itself, so the non-bool
	// allowedFallbackModels sits alongside the flags rather than needing its own
	// decode. See compatOverrides.
	o := newCompatOverrides(model.Compat)
	applyCompat(o, "supportsEagerToolInputStreaming", &c.supportsEagerToolInputStreaming)
	applyCompat(o, "supportsLongCacheRetention", &c.supportsLongCacheRetention)
	applyCompat(o, "sendSessionAffinityHeaders", &c.sendSessionAffinityHeaders)
	applyCompat(o, "sessionAffinityFormat", &c.sessionAffinityFormat)
	applyCompat(o, "supportsCacheControlOnTools", &c.supportsCacheControlOnTools)
	applyCompat(o, "supportsTemperature", &c.supportsTemperature)
	applyCompat(o, "allowEmptySignature", &c.allowEmptySignature)
	applyCompat(o, "forceAdaptiveThinking", &c.forceAdaptiveThinking)
	applyCompat(o, "supportsStrictTools", &c.supportsStrictTools)
	applyCompat(o, "supportsMidConvoEffort", &c.supportsMidConvoEffort)
	applyCompat(o, "supportsMidConvoSystemMessages", &c.supportsMidConvoSystemMessages)
	applyCompat(o, "supportsMidConvoToolChanges", &c.supportsMidConvoToolChanges)
	applyCompat(o, "allowedFallbackModels", &c.allowedFallbackModels)
	return c
}

// usesServerSideFallbackBeta ports pi's shouldUseServerSideFallbackBeta,
// `(model.compat?.allowedFallbackModels?.length ?? 0) > 0` (upstream ed867e909):
// the fallback beta rides on the catalog listing permitted fallback models, not
// on a caller-supplied option. It hangs off the decoded compat rather than off
// *ai.Model so the one call site can reuse the compat it already has instead of
// decoding the blob a second time; pi reads a plain property there and pays
// nothing for it.
func (c anthropicCompat) usesServerSideFallbackBeta() bool {
	return len(c.allowedFallbackModels) > 0
}

// anthropicFallbackModelCost ports pi's
// `allowedFallbackModels?.find(f => f.provider === model.provider && f.model === served)?.cost`
// (anthropic-messages.ts, upstream ed867e909). Both halves of the match matter:
// a catalog entry for another provider's same-named model must not price this
// response. The FIRST match decides, so an entry carrying no pricing yields
// nothing rather than scanning on to a later duplicate — in TS `find` returns the
// first hit and `?.cost` on it is `undefined`.
func anthropicFallbackModelCost(models []anthropicAllowedFallbackModel, provider, modelID string) *ai.ModelCost {
	for _, f := range models {
		if f.Provider == provider && f.Model == modelID {
			return f.Cost
		}
	}
	return nil
}

// anthropicUsageModel is the model a response is COSTED against: the requested
// model, or — when a server-side refusal fallback served it and the catalog
// prices the serving model — that model repriced (pi's
// `fallbackCost ? { ...model, id: responseModel, cost: fallbackCost } : model`,
// upstream ed867e909, 1283afd0d). servedID is message_start's model, which since
// 1283afd0d is no longer the message's own Model.
//
// The swap needs a pricing to have been FOUND, not merely a different served
// model: one the catalog does not list, or lists for another provider, keeps the
// requested model's rates rather than blanking them. The no-swap arm returns the
// caller's own *ai.Model, as pi's `: model` does.
func anthropicUsageModel(model *ai.Model, servedID string) *ai.Model {
	if servedID == model.ID {
		return model
	}
	// Only reached for a served model that differs, so a response no fallback
	// stood in for re-parses no compat blob.
	cost := anthropicFallbackModelCost(getAnthropicCompat(model).allowedFallbackModels, model.Provider, servedID)
	if cost == nil {
		return model
	}
	// pi's spread is a shallow copy: Input, ThinkingLevelMap, SamplingParams,
	// Headers and Compat alias the catalog entry, so nothing beyond ID and Cost may
	// be mutated here — the shared catalog model itself is never written to.
	priced := *model
	priced.ID, priced.Cost = servedID, *cost
	return &priced
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

// hasAnthropicAuthHeader reports whether the request headers already carry the
// credential, which is the escape hatch in pi's assertRequestAuth
// (anthropic-messages.ts:301, upstream 129eb460c):
//
//	if (apiKey) return;
//	if (hasHeader(headers, "authorization") || hasHeader(headers, "x-api-key")
//	    || hasHeader(headers, "cf-aig-authorization")) return;
//	throw new Error(`No API key for provider: ${provider}`);
//
// This is header-OWNED auth: pi's resolver hands the adapter an Authorization
// bearer for ANTHROPIC_AUTH_TOKEN and a cf-aig-authorization for a Cloudflare AI
// Gateway credential, with no apiKey at all, and a consumer may pass any of the
// three itself. Only these three names count, and only for this API — the openai
// adapters take two (clientAPIKey) and google takes none, so the three gates stay
// separate.
//
// pi's hasHeader lowercases the name it compares and requires `value !== null &&
// value.trim().length > 0`, so a deletion marker (nil here) and a blank value are
// not credentials.
func hasAnthropicAuthHeader(headers ai.ProviderHeaders) bool {
	for name, value := range headers {
		switch strings.ToLower(name) {
		case "authorization", "x-api-key", "cf-aig-authorization":
			if value != nil && jstext.Trim(*value) != "" {
				return true
			}
		}
	}
	return false
}

// StreamSimpleAnthropic maps unified reasoning to AnthropicOptions then streams.
func StreamSimpleAnthropic(ctx context.Context, model *ai.Model, req ai.TranscriptContext, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
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
	if opts != nil {
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
//
// The transcript is resolved before anything reads it: a model with
// supportsMidConvoSystemMessages keeps later system messages in place, and
// every other model has them folded into the leading one. The request, the
// Copilot headers and the OAuth tool-name mapping all see the resolved
// transcript and its current tools.
func StreamAnthropic(ctx context.Context, model *ai.Model, req ai.TranscriptContext, opts *AnthropicOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	if opts == nil {
		opts = &AnthropicOptions{}
	}
	normalized := ai.ResolveTranscript(req, getAnthropicCompat(model).supportsMidConvoSystemMessages)
	currentTools := ai.GetCurrentTools(normalized.Messages)

	go func() {
		output := &ai.AssistantMessage{
			Content: ai.ContentList{}, Api: model.Api, Provider: model.Provider, Model: model.ID,
			StopReason: ai.StopPending, Timestamp: nowMillis(),
		}
		// A managed-effort response records the effort it ran at, so the NEXT
		// request can replay this turn at the same level rather than at the
		// current one (pi 4e69b0c28). Unmanaged models leave it empty, pi's
		// `undefined`.
		if getAnthropicCompat(model).supportsMidConvoEffort {
			output.ProviderThinkingLevel = anthropicActiveEffort(opts)
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
		// pi's gate is assertRequestAuth (see hasAnthropicAuthHeader): the api key,
		// or the credential already sitting in a request header. Upstream 8b5899dce
		// re-attached that same assertion eagerly at the top of streamSimple, where
		// it throws before a stream exists; under G3 (ai/stream.go:90) the port
		// renders a thrown setup failure as the stream's single terminal error
		// event, and this gate is the first thing on both the Stream and the
		// StreamSimple path — so the eager assertion's PRECEDENCE is already what
		// the port has, and only its acceptance set had to be widened.
		//
		// The authToken arm is the port's own, and has no counterpart in pi's
		// adapter: pi resolves ANTHROPIC_AUTH_TOKEN one layer up into the
		// Authorization header the arm above accepts, whereas this adapter reads
		// the env value directly so the compat path (ai.StreamSimple, which leaves
		// APIKey empty for it) authenticates too.
		if apiKey == "" && authToken == "" && !hasAnthropicAuthHeader(opts.Headers) {
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

		body, err := buildAnthropicParams(model, normalized, oauth, opts)
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
			// pi: any `!== undefined` return replaces the params wholesale —
			// except `stream`, which is re-forced to true immediately afterwards
			// (`params = {...next, stream: true}`, upstream 4e69b0c28). A hook
			// cannot turn a streaming request into a non-streaming one.
			//
			// The spread builds a FRESH object, so the map the hook returned stays
			// the hook's: copying rather than writing through it keeps a hook that
			// kept a reference (or handed back the very map it was given) from
			// seeing the request mutate under it. Copying also gives a nil map the
			// meaning pi gives `{...null, stream: true}` — a body of just the
			// re-forced flag — instead of panicking on a write to a nil map.
			if m, ok := next.(map[string]any); ok {
				overridden := make(map[string]any, len(m)+1)
				maps.Copy(overridden, m)
				overridden["stream"] = true
				body = overridden
			}
		}
		// The beta namespace rewrites a deprecated top-level `output_format` into
		// `output_config.format` before it looks at anything else, and refuses a
		// params object carrying both (transformOutputFormat, @anthropic-ai/sdk
		// resources/beta/messages/messages.js). pi never emits `output_format`, so
		// only an onPayload hook reaches either path.
		body, err = transformAnthropicOutputFormat(body)
		if err != nil {
			fail(err)
			return
		}
		// The SDK's beta namespace destructures `betas` OUT of the params object
		// and re-emits it as the per-request `anthropic-beta` header, so it never
		// reaches the wire body — but it is in the object `onPayload` sees, which
		// is why buildAnthropicParams puts it there.
		body, betaHeader := splitAnthropicBetas(body)
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
		// pi calls `client.beta.messages.create(...)`, and the SDK's beta
		// namespace posts to `/v1/messages?beta=true` (upstream 4e69b0c28). The
		// query string is part of the request pi makes; difftest compares bodies
		// only, so it is pinned by test instead.
		url := strings.TrimRight(baseURL, "/") + "/v1/messages?beta=true"
		build := func() (*http.Request, error) {
			r, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
			if err != nil {
				return nil, err
			}
			applyAnthropicHeaders(r, model, opts, oauth, apiKey, authToken, normalized.Messages)
			// The betas header is a PER-REQUEST header in the SDK, so it beats
			// every default header the merge above produced — including one the
			// consumer spelled differently, and including the empty-string value an
			// empty `betas` list produces, which REPLACES the inherited header
			// rather than leaving it standing. Writing it (Set, not Add) after
			// applyAsDefaultHeaders is what reproduces that precedence.
			if betaHeader != nil {
				r.Header.Set("anthropic-beta", *betaHeader)
			}
			return r, nil
		}
		resp, err := sendWithRetry(ctx, build, retryFromOptions(opts.StreamOptions, anthropicSDKErrorMessage))
		if err != nil {
			fail(err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			data, _ := io.ReadAll(resp.Body)
			fail(formatProviderError("Anthropic", resp.StatusCode, data))
			return
		}
		// pi awaits onResponse once the SDK has returned the response — the SDK
		// throws on a non-2xx status first, so an error response never reaches
		// it — and before pushing start; a throw fails the stream.
		if opts.OnResponse != nil {
			if err := opts.OnResponse(ai.ProviderResponse{Status: resp.StatusCode, Headers: flattenHeaders(resp.Header)}, model); err != nil {
				fail(err)
				return
			}
		}

		stream.Push(ai.AssistantMessageEvent{Type: ai.EventStart, Partial: output.Clone()})

		// blocks is pi's `output.content` as its handler holds it: each block's
		// builder with the two fields pi deletes on content_block_stop — the
		// event's raw `index` a block is found by (nil once deleted: undefined)
		// and whether its partialJson is gone.
		type anthropicBlock struct {
			*blockBuilder
			index          json.RawMessage
			partialDeleted bool
		}
		var blocks []*anthropicBlock
		materialize := func() {
			content := make(ai.ContentList, len(blocks))
			for i, b := range blocks {
				content[i] = b.toContent()
			}
			output.Content = content
		}
		// findBlock is pi's `blocks.findIndex((b) => b.index === event.index)`:
		// the first block whose index is strictly equal, so a string "0" finds
		// no block started at 0, and an event with no index finds the first
		// block whose index is gone.
		findBlock := func(index json.RawMessage) int {
			for i, b := range blocks {
				if rawStrictEqual(b.index, index) {
					return i
				}
			}
			return -1
		}

		// The model to cost against, recomputed on every message_start and read by
		// both cost sites below. A stream that never delivers one prices at the
		// requested model, as pi does (anthropic-messages.ts:543).
		usageModel := model

		// The last input_transformations list the stream reported. Anthropic
		// resends the whole list rather than a delta, so a later event REPLACES
		// what an earlier one said — including an empty list, which retracts it.
		var inputTransformations []anthropicInputTransformation

		// pi awaits options.onProviderStreamEvent(event, model) first thing in
		// the loop, with the model the stream was called with.
		var onEvent func(any) error
		if opts.OnProviderStreamEvent != nil {
			onEvent = func(data any) error { return opts.OnProviderStreamEvent(data, model) }
		}

		// Each branch reads the event's properties as pi's handler does (see
		// jsread.go): a member of an unexpected type is whatever value it is,
		// and a property read from a missing or null sub-object throws V8's
		// TypeError in pi's read order, failing the stream.
		sawStart, sawStop := false, false
		// errorMessageSet is whether pi's output.errorMessage is truthy: once a
		// stop reason assigned one, however empty its text (see
		// mapAnthropicStopReason).
		errorMessageSet := false
		err = iterateAnthropicSSE(resp.Body, ctx, onEvent, func(ev rawObject) error {
			typ, _ := rawString(ev["type"])
			switch typ {
			case "message_start":
				sawStart = true
				msg, err := rawRead(ev["message"], "id")
				if err != nil {
					return err
				}
				output.ResponseID, _ = rawString(msg["id"])
				if list, ok := parseAnthropicInputTransformations(msg["input_transformations"]); ok {
					inputTransformations = list
				}
				// pi 1283afd0d: the message keeps the REQUESTED id, so signed
				// thinking still replays as this model's own when a relay relabels
				// the model; a served model that differs (a server-side refusal
				// fallback, or a relabel) is recorded beside it. A message_start
				// naming the requested id leaves an earlier one standing, while
				// one with no model clears it (pi assigns undefined).
				served, _ := rawString(msg["model"])
				if served != model.ID {
					output.ResponseModel = served
				}
				// pi ed867e909: a response a server-side refusal fallback served is
				// billed at the SERVING model's rates, taken from the catalog.
				// Assigned unconditionally, as pi's ternary is, so a second
				// message_start reprices rather than sticking to the first.
				usageModel = anthropicUsageModel(model, served)
				usage, err := rawRead(msg["usage"], "input_tokens")
				if err != nil {
					return err
				}
				applyMessageStartUsage(&output.Usage, usage)
				ai.CalculateCost(usageModel, &output.Usage)
			case "content_block_start":
				block, err := rawRead(ev["content_block"], "type")
				if err != nil {
					return err
				}
				blockType, _ := rawString(block["type"])
				// A `fallback` block announces that Anthropic served the request
				// with a different model. Before any content it is informational
				// and skipped; after content it means the swap happened mid-output,
				// which pi refuses rather than stitching two models' text together
				// (upstream 4e69b0c28).
				if blockType == "fallback" {
					if len(blocks) > 0 {
						return fmt.Errorf("Anthropic performed an unsupported mid-output model fallback")
					}
					return nil
				}
				var b *blockBuilder
				var evType ai.EventType
				switch blockType {
				case "text":
					// The start event may already carry the block's first chunk; seed
					// the builder with it so the deltas that follow append to it.
					b = &blockBuilder{kind: "text"}
					b.text.WriteString(anthropicSeedText(block["text"]))
					evType = ai.EventTextStart
				case "thinking":
					b = &blockBuilder{kind: "thinking", thinkingSig: anthropicSeedText(block["signature"])}
					b.thinking.WriteString(anthropicSeedText(block["thinking"]))
					evType = ai.EventThinkingStart
				case "redacted_thinking":
					data, _ := rawString(block["data"])
					b = &blockBuilder{kind: "thinking", redacted: true, thinkingSig: data}
					b.thinking.WriteString("[Reasoning redacted]")
					evType = ai.EventThinkingStart
				case "tool_use":
					id, _ := rawString(block["id"])
					name, _ := rawString(block["name"])
					if oauth {
						name = fromClaudeCodeName(name, currentTools)
					}
					// pi: `arguments: event.content_block.input ?? {}` — the
					// arguments stand until the first input_json_delta (or the
					// block's stop) re-parses them from the streamed JSON.
					b = &blockBuilder{kind: "toolCall", toolID: id, toolName: name, args: map[string]any{}}
					if jsonValueKind(block["input"]) == '{' {
						if args, order, err := ai.DecodeOrderedObject(block["input"]); err == nil {
							b.args, b.argsOrder = args, order
						}
					}
					evType = ai.EventToolCallStart
				default:
					return nil
				}
				blocks = append(blocks, &anthropicBlock{blockBuilder: b, index: ev["index"]})
				materialize()
				stream.Push(ai.AssistantMessageEvent{Type: evType, ContentIndex: len(blocks) - 1, Partial: output.Clone()})
			case "content_block_delta":
				delta, err := rawRead(ev["delta"], "type")
				if err != nil {
					return err
				}
				// pi only applies a delta when the indexed block has the matching
				// type; mismatches are dropped silently.
				deltaType, _ := rawString(delta["type"])
				var kind, member string
				switch deltaType {
				case "text_delta":
					kind, member = "text", "text"
				case "thinking_delta":
					kind, member = "thinking", "thinking"
				case "input_json_delta":
					kind, member = "toolCall", "partial_json"
				case "signature_delta":
					kind, member = "thinking", "signature"
				default:
					return nil
				}
				idx := findBlock(ev["index"])
				if idx < 0 || blocks[idx].kind != kind {
					return nil
				}
				b := blocks[idx]
				// pi's `+=` appends String() of the member ("undefined" when it is
				// absent); the pushed event carries the member itself.
				piece, err := rawToString(delta[member])
				if err != nil {
					return err
				}
				pushed, _ := rawString(delta[member])
				switch deltaType {
				case "text_delta":
					b.text.WriteString(piece)
					materialize()
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventTextDelta, ContentIndex: idx, Delta: pushed, Partial: output.Clone()})
				case "thinking_delta":
					b.thinking.WriteString(piece)
					materialize()
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventThinkingDelta, ContentIndex: idx, Delta: pushed, Partial: output.Clone()})
				case "input_json_delta":
					if b.partialDeleted {
						// A block's stop deleted partialJson: `undefined += piece`.
						b.partialJSON.Reset()
						b.partialJSON.WriteString("undefined")
						b.partialDeleted = false
					}
					b.partialJSON.WriteString(piece)
					b.args, b.argsOrder = parseStreamingJSON(b.partialJSON.String())
					materialize()
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventToolCallDelta, ContentIndex: idx, Delta: pushed, Partial: output.Clone()})
				case "signature_delta":
					b.thinkingSig += piece
				}
			case "content_block_stop":
				idx := findBlock(ev["index"])
				if idx < 0 {
					return nil
				}
				b := blocks[idx]
				b.index = nil // pi: delete block.index
				materialize()
				switch b.kind {
				case "text":
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventTextEnd, ContentIndex: idx, Content: b.text.String(), Partial: output.Clone()})
				case "thinking":
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventThinkingEnd, ContentIndex: idx, Content: b.thinking.String(), Partial: output.Clone()})
				case "toolCall":
					partial := b.partialJSON.String()
					if b.partialDeleted {
						partial = "" // parseStreamingJson(undefined) is {}
					}
					b.args, b.argsOrder = parseStreamingJSON(partial)
					b.partialDeleted = true // pi: delete block.partialJson
					materialize()
					tc := b.toContent().(ai.ToolCall)
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventToolCallEnd, ContentIndex: idx, ToolCall: &tc, Partial: output.Clone()})
				}
			case "message_delta":
				if list, ok := parseAnthropicInputTransformations(ev["input_transformations"]); ok {
					inputTransformations = list
				}
				delta, err := rawRead(ev["delta"], "stop_reason")
				if err != nil {
					return err
				}
				if rawTruthy(delta["stop_reason"]) {
					output.RawStopReason, _ = rawString(delta["stop_reason"])
					sr, errMsg, err := mapAnthropicStopReason(delta["stop_reason"], delta["stop_details"])
					if err != nil {
						return err
					}
					output.StopReason = sr
					if errMsg != nil {
						output.ErrorMessage = *errMsg
						errorMessageSet = true
					}
				}
				// Only update usage fields if present (not null), preserving
				// message_start's input_tokens when a proxy omits it here.
				if rawTruthy(ev["usage"]) {
					applyMessageDeltaUsage(&output.Usage, rawOptional(ev["usage"]))
				}
				output.Usage.TotalTokens = output.Usage.Input + output.Usage.Output + output.Usage.CacheRead + output.Usage.CacheWrite
				ai.CalculateCost(usageModel, &output.Usage)
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
			// pi: `new Error(output.errorMessage || "An unknown error occurred")`.
			msg := "An unknown error occurred"
			if errorMessageSet {
				msg = output.ErrorMessage
			}
			fail(errors.New(msg))
			return
		}
		// Appended only on a stream that completed successfully — pi's throws for
		// an aborted/errored turn all fire above this point.
		appendAnthropicInputTransformationsDiagnostic(output, inputTransformations)

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

// anthropicActiveEffort is pi's `options?.effort ?? "high"`: the effort a
// managed-effort turn runs at, and the value the response records so a later
// turn can replay it.
func anthropicActiveEffort(opts *AnthropicOptions) string {
	if opts != nil && opts.Effort != "" {
		return opts.Effort
	}
	return "high"
}

// isAnthropicEffort reports whether a recorded providerThinkingLevel is a value
// the Anthropic effort union accepts. Port of pi's isAnthropicEffort: a level
// from some other provider (or a future one this build does not know) is
// ignored rather than replayed into a request Anthropic would reject.
func isAnthropicEffort(value string) bool {
	switch value {
	case "low", "medium", "high", "xhigh", "max":
		return true
	}
	return false
}

// getAnthropicBetaFeatures is pi's getBetaFeatures (upstream 4e69b0c28): the
// betas that ride in the request body and, via the SDK, in the `anthropic-beta`
// header. Beta assembly used to live in createClient with a per-auth-branch
// header; it is now one list computed per request.
//
// An `anthropic-beta` header configured on the model or by the consumer REPLACES
// the computed set rather than joining it — the later source (options) wins, a
// deletion marker yields nothing at all, and the value is split/trimmed/deduped.
//
// Every gate below is per pi, and the auth gate is the sharp one: BOTH
// "claude-code-20250219" and "oauth-2025-04-20" hang off isOAuthToken together,
// so an api-key, ANTHROPIC_AUTH_TOKEN, Copilot or Cloudflare-gateway request
// sends neither.
// compat is passed in rather than re-decoded: pi reads plain properties here,
// so a second decode of the raw compat blob would be duplicated per-request work
// and a second place for the two reads to drift (the same reasoning as
// buildAnthropicParams' fallbacks list). nativeToolChanges is buildAnthropicParams'
// decision to send tool changes as tool_addition/tool_removal blocks, which
// needs its own beta.
func getAnthropicBetaFeatures(model *ai.Model, req ai.TranscriptContext, oauth, nativeToolChanges bool, opts *AnthropicOptions, compat anthropicCompat) []string {
	var optHeaders ai.ProviderHeaders
	if opts != nil {
		optHeaders = opts.Headers
	}
	var configured *string
	configuredFound := false
	for _, headers := range []ai.ProviderHeaders{model.Headers, optHeaders} {
		// Sorted within one source: a Go map has no key order to reproduce, the
		// standing tie-break for this divergence class (see headerObject.merge).
		// It only decides between two spellings inside a single literal.
		for _, name := range sortedNames(headers) {
			if strings.EqualFold(name, "anthropic-beta") {
				configured, configuredFound = headers[name], true
			}
		}
	}
	if configuredFound {
		if configured == nil {
			return nil
		}
		var out []string
		seen := map[string]bool{}
		for _, feature := range strings.Split(*configured, ",") {
			feature = jstext.Trim(feature)
			if feature == "" || seen[feature] {
				continue
			}
			seen[feature] = true
			out = append(out, feature)
		}
		return out
	}

	// The six sources below contribute disjoint literals, so pi's `new Set`
	// round-trip cannot drop anything here.
	var features []string
	if oauth {
		features = append(features, "claude-code-20250219", "oauth-2025-04-20")
	}
	if len(ai.GetCurrentTools(req.Messages)) > 0 && !compat.supportsEagerToolInputStreaming {
		features = append(features, fineGrainedToolStreamBeta)
	}
	// Narrowed by 4e69b0c28: interleaved thinking is now asked for only when the
	// model reasons AND thinking is explicitly on. It used to ride on every
	// request whose model was not forceAdaptiveThinking.
	interleaved := true
	if opts != nil && opts.InterleavedThinking != nil {
		interleaved = *opts.InterleavedThinking
	}
	if model.Reasoning && opts != nil && opts.ThinkingProvided && opts.ThinkingEnabled &&
		interleaved && !compat.forceAdaptiveThinking {
		features = append(features, interleavedThinkingBeta)
	}
	if compat.usesServerSideFallbackBeta() {
		features = append(features, serverSideFallbackBeta)
	}
	if compat.supportsMidConvoEffort {
		features = append(features, midConvoOutputConfigBeta, thinkingBindingBeta)
	}
	if nativeToolChanges {
		features = append(features, midConvoToolChangesBeta)
	}
	return features
}

// splitAnthropicBetas reproduces the SDK's `const { betas, ...body } = params`
// together with the header it derives from them: it returns the body with the
// key removed and the `anthropic-beta` value to send, nil for "send no header".
// The body is copied rather than mutated, since after an onPayload override the
// map belongs to the caller.
//
// The presence rule is the SDK's `betas?.toString() != null` (resources/beta/
// messages/messages.js): only an absent or null `betas` sends nothing. An EMPTY
// list does not — `[].toString()` is "", which is not null — so it sends a
// present, empty header, which then deletes any `anthropic-beta` inherited from
// the default headers. buildAnthropicParams never emits an empty list, so this
// case belongs to onPayload, which is the surface `betas` is exposed on.
//
// Only the `[]string` arm is reachable from the port's own code —
// buildAnthropicParams writes nothing else — and it is the one difftest's
// body comparison covers. The rest exist for hook fidelity alone.
//
// A hook may hand back any JSON shape, so the value is stringified the way
// `toString()` would: a list joins with "," (a null element contributing ""), a
// bare string stands for itself. Any other non-null value still sends a header,
// rendered Go-side rather than as JavaScript's "[object Object]" — a shape
// neither pi nor this port ever produces, where only the presence carries over.
func splitAnthropicBetas(body map[string]any) (map[string]any, *string) {
	raw, present := body["betas"]
	if !present {
		return body, nil
	}
	stripped := make(map[string]any, len(body)-1)
	for k, v := range body {
		if k != "betas" {
			stripped[k] = v
		}
	}
	if raw == nil {
		return stripped, nil
	}
	var value string
	switch v := raw.(type) {
	case []string:
		value = strings.Join(v, ",")
	case string:
		value = v
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			if item != nil {
				parts[i] = fmt.Sprint(item)
			}
		}
		value = strings.Join(parts, ",")
	default:
		value = fmt.Sprint(raw)
	}
	return stripped, &value
}

// transformAnthropicOutputFormat ports the beta namespace's
// transformOutputFormat (@anthropic-ai/sdk resources/beta/messages/messages.js),
// which runs on the params — after onPayload has had them — on the way into the
// request: a truthy deprecated `output_format` moves to `output_config.format`,
// and supplying both is an error rather than a silent preference. pi builds
// neither key for an Anthropic request, so this only ever fires on a hook's
// params; it returns a modified copy, as the SDK does, and leaves a params
// object without the deprecated key untouched.
func transformAnthropicOutputFormat(body map[string]any) (map[string]any, error) {
	format := body["output_format"]
	if !jsTruthy(format) {
		return body, nil
	}
	config, _ := body["output_config"].(map[string]any)
	if jsTruthy(config["format"]) {
		return nil, fmt.Errorf("Both output_format and output_config.format were provided. " +
			"Please use only output_config.format (output_format is deprecated).")
	}
	out := make(map[string]any, len(body))
	for k, v := range body {
		if k != "output_format" {
			out[k] = v
		}
	}
	merged := make(map[string]any, len(config)+1)
	maps.Copy(merged, config)
	merged["format"] = format
	out["output_config"] = merged
	return out, nil
}

// anthropicInputTransformation is one entry of Anthropic's
// `input_transformations`: what the server dropped or rewrote from the prompt
// before sampling (pi BetaThinkingDroppedInputTransformation). Every field is
// optional and pi maps an explicit null to "absent" (`?? undefined`), so an
// absent, null and present value must stay distinguishable.
//
// The fields are `any` rather than `*string` because pi's only guard on the
// whole value is `Array.isArray`: it reads `.type/.path/.reason` off whatever
// each entry turns out to be and forwards what it finds into the diagnostic
// unexamined. A stricter decode here would not merely drop a bad field, it
// would reject the array — see parseAnthropicInputTransformations.
type anthropicInputTransformation struct {
	Type   any
	Path   any
	Reason any
}

// parseAnthropicInputTransformations decodes an `input_transformations` value,
// reporting whether it was an array — pi's `Array.isArray(...)` guard, which
// lets a non-array (including null) leave the previous list standing while an
// empty array retracts it. The field is held as raw JSON so a malformed value
// costs only itself instead of failing the whole event decode.
//
// The guard is `Array.isArray` and nothing else: ANY array is accepted, and an
// entry that is not an object simply contributes no fields. Refusing a
// wrong-shaped entry would cost far more than the entry — the event would stop
// being a replacement and would resurrect the list a previous event set.
func parseAnthropicInputTransformations(raw json.RawMessage) ([]anthropicInputTransformation, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, false
	}
	var entries []any
	if err := json.Unmarshal(trimmed, &entries); err != nil {
		return nil, false
	}
	list := make([]anthropicInputTransformation, len(entries))
	for i, entry := range entries {
		obj, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		list[i] = anthropicInputTransformation{Type: obj["type"], Path: obj["path"], Reason: obj["reason"]}
	}
	return list, true
}

// appendAnthropicInputTransformationsDiagnostic records what the server dropped
// from the prompt, so a turn whose thinking blocks were silently discarded is
// visible after the fact (pi's `anthropic_input_transformations` diagnostic).
// An absent field is omitted rather than written empty, matching pi's
// `?? undefined` in a JSON payload.
func appendAnthropicInputTransformationsDiagnostic(output *ai.AssistantMessage, list []anthropicInputTransformation) {
	if len(list) == 0 {
		return
	}
	details := make([]map[string]any, len(list))
	for i, transformation := range list {
		entry := map[string]any{}
		// `?? undefined` omits only null and absent; a present false, 0 or "" is
		// kept, and so is a value of the wrong type.
		if transformation.Type != nil {
			entry["type"] = transformation.Type
		}
		if transformation.Path != nil {
			entry["path"] = transformation.Path
		}
		if transformation.Reason != nil {
			entry["reason"] = transformation.Reason
		}
		details[i] = entry
	}
	output.Diagnostics = append(output.Diagnostics, ai.Diagnostic{
		Type:      "anthropic_input_transformations",
		Timestamp: nowMillis(),
		Details:   map[string]any{"transformations": details},
	})
}

// insertAnthropicThinkingLevelMessages is pi's insertThinkingLevelMessages: an
// effort-only system message before every assistant turn that recorded a
// provider-native level, and one more at the end carrying the effort THIS turn
// runs at. That trailing message — not output_config — is what sets the current
// effort on a managed-effort model.
func insertAnthropicThinkingLevelMessages(messages []map[string]any, levels map[int]string, activeEffort string) []map[string]any {
	out := make([]map[string]any, 0, len(messages)+len(levels)+1)
	effortMessage := func(effort string) map[string]any {
		return map[string]any{"role": "system", "content": []any{}, "output_config": map[string]any{"effort": effort}}
	}
	for i, message := range messages {
		if effort, ok := levels[i]; ok {
			out = append(out, effortMessage(effort))
		}
		out = append(out, message)
	}
	return append(out, effortMessage(activeEffort))
}

func buildAnthropicParams(model *ai.Model, req ai.TranscriptContext, oauth bool, opts *AnthropicOptions) (map[string]any, error) {
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

	// The leading system message is the top-level system prompt; the conversation
	// is everything transformed after it.
	initialSystemMessage, hasInitialSystemMessage := ai.GetInitialSystemMessage(req.Messages)
	initialSystemText := ""
	if hasInitialSystemMessage {
		initialSystemText = ai.GetSystemMessageText(initialSystemMessage)
	}
	transformedMessages := transformMessages(req.Messages, model, normalizeToolCallID)
	conversationMessages := transformedMessages
	if hasInitialSystemMessage {
		conversationMessages = transformedMessages[1:]
	}
	// Native tool changes reference tools by name, so a redefined name cannot be
	// expressed, and Anthropic rejects a tool list where every tool is deferred,
	// so there must be an initial active tool to anchor the deferred ones.
	// Otherwise the current tool list is sent.
	initialTools := initialSystemMessage.ToolsAdded
	nativeToolChanges := compat.supportsMidConvoSystemMessages &&
		compat.supportsMidConvoToolChanges &&
		len(initialTools) > 0 &&
		!ai.HasToolRedefinitions(req.Messages)

	// A managed-effort model replays each historical turn's own effort, so the
	// conversion records which converted message each recorded level belongs to.
	// The provider is passed in only for such a model — pi's
	// `supportsMidConvoEffort ? model.provider : undefined` — which is also what
	// switches the recording off entirely for every other model.
	managedProvider := ""
	if compat.supportsMidConvoEffort {
		managedProvider = model.Provider
	}
	messages, assistantLevels := convertAnthropicMessages(
		conversationMessages, oauth, cc, compat.allowEmptySignature, managedProvider, nativeToolChanges)
	if compat.supportsMidConvoEffort {
		messages = insertAnthropicThinkingLevelMessages(messages, assistantLevels, anthropicActiveEffort(opts))
	}

	params := map[string]any{
		"model":      model.ID,
		"messages":   messages,
		"max_tokens": maxTokens,
		"stream":     true,
	}
	// `betas` travels in the params object so onPayload observes it; the SDK
	// lifts it back out into the anthropic-beta header before sending.
	if betas := getAnthropicBetaFeatures(model, req, oauth, nativeToolChanges, opts, compat); len(betas) > 0 {
		params["betas"] = betas
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
		if initialSystemText != "" {
			system = append(system, textBlock(initialSystemText))
		}
		params["system"] = system
	} else if initialSystemText != "" {
		params["system"] = []any{textBlock(initialSystemText)}
	}

	// pi: `!options?.thinkingEnabled` — only an explicit thinkingEnabled:true
	// suppresses temperature; unset (not Provided) behaves like false.
	thinkingOn := opts != nil && opts.ThinkingProvided && opts.ThinkingEnabled
	if opts != nil && opts.Temperature != nil && !thinkingOn &&
		!compat.supportsMidConvoEffort && compat.supportsTemperature {
		params["temperature"] = *opts.Temperature
	}

	var toolCC *cacheControl
	if compat.supportsCacheControlOnTools {
		toolCC = cc
	}
	if nativeToolChanges {
		// Initial tools stay active with the cache breakpoint on the last one.
		// Every later declaration is deferred and only surfaced by its
		// tool_addition block; removed tools stay declared and are withdrawn by
		// tool_removal. The request-level list therefore only grows, keeping the
		// cached prefix intact across tool changes.
		initialNames := make(map[string]bool, len(initialTools))
		for _, tool := range initialTools {
			initialNames[tool.Name] = true
		}
		var laterTools []ai.Tool
		for _, tool := range ai.GetDeclaredTools(req.Messages) {
			if !initialNames[tool.Name] {
				laterTools = append(laterTools, tool)
			}
		}
		initial, err := convertAnthropicTools(initialTools, oauth, compat.supportsEagerToolInputStreaming, compat.supportsStrictTools, toolCC)
		if err != nil {
			return nil, err
		}
		later, err := convertAnthropicTools(laterTools, oauth, compat.supportsEagerToolInputStreaming, compat.supportsStrictTools, nil)
		if err != nil {
			return nil, err
		}
		tools := make([]map[string]any, 0, len(initial)+1+len(later))
		tools = append(tools, initial...)
		tools = append(tools, deferredToolPlaceholder())
		for _, tool := range later {
			tool["defer_loading"] = true
			tools = append(tools, tool)
		}
		params["tools"] = tools
	} else if tools := ai.GetCurrentTools(req.Messages); len(tools) > 0 {
		converted, err := convertAnthropicTools(tools, oauth, compat.supportsEagerToolInputStreaming, compat.supportsStrictTools, toolCC)
		if err != nil {
			return nil, err
		}
		params["tools"] = converted
	}

	// pi tri-state (anthropic.ts:950-978): thinkingEnabled undefined omits the
	// thinking key entirely; explicit true enables; explicit false sends
	// {type:"disabled"} — unless the model's thinkingLevelMap carries an
	// explicit off:null (present-nil), which marks "disabled" as unsupported
	// and omits the key too (pi 9ccfcd7c: `thinkingLevelMap?.off !== null`).
	//
	// A managed-effort model short-circuits all of that: it is ALWAYS adaptive,
	// with block_binding so a prefix mismatch drops the offending block instead
	// of surfacing as a persistent 400, and its output_config effort is the
	// literal "high" — the per-turn effort rides in the trailing system message
	// insertAnthropicThinkingLevelMessages appended, not here (upstream
	// 4e69b0c28). It does not consult model.Reasoning or the thinking tri-state.
	if compat.supportsMidConvoEffort {
		display := ""
		if opts != nil {
			display = opts.ThinkingDisplay
		}
		if display == "" {
			display = "summarized"
		}
		params["thinking"] = map[string]any{
			"type":          "adaptive",
			"display":       display,
			"block_binding": map[string]any{"prefix_mismatch_behavior": "drop_block"},
		}
		params["output_config"] = map[string]any{"effort": "high"}
	} else if model.Reasoning && opts != nil && opts.ThinkingProvided {
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

	// Server-side refusal fallback. pi appends this last, after every other key,
	// and derives it from the catalog alone — there is no caller-facing option
	// (upstream ed867e909). Every permitted model is projected onto
	// anthropicFallbackWire, so neither the provider id nor the local pricing
	// reaches Anthropic. An empty list omits the key: a model with no permitted
	// targets must not send `fallbacks` at all, which Anthropic rejects.
	//
	// The list comes off the compat decoded once at the top of this function —
	// pi reads a plain property here, so a second decode would be pure duplicated
	// work and a second place for the two reads to drift.
	if allowed := compat.allowedFallbackModels; len(allowed) > 0 {
		wire := make([]anthropicFallbackWire, len(allowed))
		for i, f := range allowed {
			wire[i].Model = f.Model
		}
		params["fallbacks"] = wire
	}

	return params, nil
}

func convertAnthropicTools(tools []ai.Tool, oauth, eager, supportsStrictTools bool, cc *cacheControl) ([]map[string]any, error) {
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
		if cc != nil && i == len(tools)-1 {
			tool["cache_control"] = cc
		}
		out[i] = tool
	}
	return out, nil
}

// convertAnthropicMessages converts the transcript to Anthropic message params
// and, for a managed-effort model, records the provider-native effort each
// converted assistant message was produced at, keyed by its index in the result.
// managedProvider is empty for every other model, which switches the recording
// off — pi passes `undefined` there.
//
// The transcript is the conversation after the leading system message. A later
// system message only reaches this point on a model that accepts one natively
// (every other transcript was collapsed first); it is rendered as a framed
// update, with its tool changes as tool_removal/tool_addition blocks when
// nativeToolChanges is set.
func convertAnthropicMessages(transformed []ai.Message, oauth bool, cc *cacheControl, allowEmptySig bool, managedProvider string, nativeToolChanges bool) ([]map[string]any, map[int]string) {
	// Seeded non-nil, like pi's `const params: MessageParam[] = []`: a transcript
	// that converts to nothing must still marshal as `[]`, and a nil slice would
	// marshal as `null`.
	params := []map[string]any{}
	// Left nil until a level is actually recorded: only a managed-effort model
	// passes a managedProvider, and every other request would otherwise allocate
	// a map it is guaranteed to leave empty and discard.
	var assistantLevels map[int]string
	// Later system messages are held back and emitted directly before the next
	// assistant message (or at the end of the transcript). Anthropic requires
	// tool_result blocks to immediately follow their tool_use, so a system
	// message between them is rejected; this also mirrors where the
	// managed-effort system messages are inserted. As a result an update placed
	// before a user message in the transcript lands after it on the wire.
	var pendingSystemMessages []map[string]any
	flushPendingSystemMessages := func() {
		params = append(params, pendingSystemMessages...)
		pendingSystemMessages = nil
	}
	toolReference := func(name string) map[string]any {
		if oauth {
			name = toClaudeCodeName(name)
		}
		return map[string]any{"type": "tool_reference", "name": name}
	}

	for i := 0; i < len(transformed); i++ {
		m := transformed[i]
		if sm, ok := asSystemMsg(m); ok {
			var blocks []any
			if text := ai.RenderSystemMessageUpdate(sm); text != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": sanitizeSurrogates(text)})
			}
			if nativeToolChanges {
				for _, tool := range sm.ToolsRemoved {
					blocks = append(blocks, map[string]any{"type": "tool_removal", "tool": toolReference(tool.Name)})
				}
				for _, tool := range sm.ToolsAdded {
					blocks = append(blocks, map[string]any{"type": "tool_addition", "tool": toolReference(tool.Name)})
				}
			}
			if len(blocks) > 0 {
				pendingSystemMessages = append(pendingSystemMessages, map[string]any{"role": "system", "content": blocks})
			}
		} else if um, ok := asUserMsg(m); ok {
			// pi sends string content as a string and block content as blocks.
			if text, isString := um.StringContent(); isString {
				if jstext.Trim(text) != "" {
					params = append(params, map[string]any{"role": "user", "content": sanitizeSurrogates(text)})
				}
				continue
			}
			blocks := convertUserBlocks(um.Content)
			if len(blocks) == 0 {
				continue
			}
			params = append(params, map[string]any{"role": "user", "content": blocks})
		} else if am, ok := asAssistantMsg(m); ok {
			flushPendingSystemMessages()
			blocks := convertAssistantBlocks(am, oauth, allowEmptySig)
			if len(blocks) == 0 {
				continue
			}
			messageIndex := len(params)
			params = append(params, map[string]any{"role": "assistant", "content": blocks})
			// Only a turn this very transport produced may have its effort
			// replayed: same api, same provider, and a level in Anthropic's own
			// union. Anything else is another provider's vocabulary.
			if managedProvider != "" && am.Api == ai.APIAnthropicMessages &&
				am.Provider == managedProvider && isAnthropicEffort(am.ProviderThinkingLevel) {
				if assistantLevels == nil {
					assistantLevels = map[int]string{}
				}
				assistantLevels[messageIndex] = am.ProviderThinkingLevel
			}
		} else if _, ok := asToolResultMsg(m); ok {
			// Collect all consecutive toolResult messages (needed for z.ai's
			// Anthropic endpoint).
			var toolResults []any
			j := i
			for j < len(transformed) {
				next, ok := asToolResultMsg(transformed[j])
				if !ok {
					break
				}
				toolResults = append(toolResults, convertToolResult(next))
				j++
			}
			i = j - 1
			params = append(params, map[string]any{"role": "user", "content": toolResults})
		}
	}

	flushPendingSystemMessages()

	// Cache the conversation history by marking the last block of the last user
	// or system message; string content becomes a single marked text block.
	if cc != nil && len(params) > 0 {
		last := params[len(params)-1]
		if last["role"] == "user" || last["role"] == "system" {
			switch content := last["content"].(type) {
			case []any:
				if len(content) > 0 {
					if blk, ok := content[len(content)-1].(map[string]any); ok {
						switch blk["type"] {
						case "text", "image", "tool_result", "tool_addition", "tool_removal":
							blk["cache_control"] = cc
						}
					}
				}
			case string:
				last["content"] = []any{map[string]any{"type": "text", "text": content, "cache_control": cc}}
			}
		}
	}
	return params, assistantLevels
}

func convertUserBlocks(content ai.ContentList) []any {
	var blocks []any
	for _, b := range content {
		switch v := b.(type) {
		case ai.TextContent:
			if jstext.Trim(v.Text) == "" {
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
			if jstext.Trim(v.Text) == "" {
				continue
			}
			blocks = append(blocks, map[string]any{"type": "text", "text": sanitizeSurrogates(v.Text)})
		case ai.ThinkingContent:
			if v.Redacted {
				blocks = append(blocks, map[string]any{"type": "redacted_thinking", "data": v.ThinkingSignature})
				continue
			}
			hasThinkingSignature := jstext.Trim(v.ThinkingSignature) != ""
			// Keep a thinking block when it carries a real signature even if its
			// text is empty (#6457); only drop it when both are empty.
			if jstext.Trim(v.Thinking) == "" && !hasThinkingSignature {
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

// convertToolResult builds a tool_result block (port of pi's convertToolResult).
func convertToolResult(tr ai.ToolResultMessage) map[string]any {
	return map[string]any{
		"type":        "tool_result",
		"tool_use_id": tr.ToolCallID,
		"content":     convertContentBlocks(tr.Content),
		"is_error":    tr.IsError,
	}
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

func applyAnthropicHeaders(r *http.Request, model *ai.Model, opts *AnthropicOptions, oauth bool, apiKey, authToken string, messages []ai.Message) {
	// pi builds ONE header object per request (mergeClientHeaders,
	// anthropic-messages.ts at upstream 87af49dec) and hands it to the SDK as
	// `defaultHeaders`; headerObject is that object, slots and all, so a case
	// collision resolves by insertion slot rather than by which Set ran last.
	//
	// The merge is seeded with pi's runtime user agent FIRST, so every later
	// source outranks it: the attribution bundle, model.Headers (the catalog's
	// GitHubCopilotChat agent on github-copilot models) and the consumer's
	// opts.Headers — and a deletion marker in any of them suppresses it. The
	// OAuth branch below is the one place where the SPELLING matters: it holds
	// its claude-cli identity under the lowercase name, at a later slot, so a
	// later source spelled "User-Agent" updates slot 0 and still loses to it.
	o := &headerObject{}
	o.merge(piUserAgentHeaders())
	o.set("content-type", "application/json")
	o.set("accept", "application/json")
	o.set("anthropic-version", anthropicVersion)
	o.set("anthropic-dangerous-direct-browser-access", "true")

	// pi mergeProviderAttributionHeaders (sdk.ts) puts the attribution bundle at
	// the bottom of the precedence stack: emit session + default attribution
	// first so the provider auth headers, model.Headers, and opts.Headers all
	// override them.
	applyAttributionDefaults(o.set, model, opts.SessionID)

	compat := getAnthropicCompat(model)

	// No beta assembly here any more: upstream 4e69b0c28 moved it out of
	// createClient into getAnthropicBetaFeatures, whose list travels in the
	// request BODY and is turned back into an `anthropic-beta` header by the
	// caller — as a per-request header that outranks everything merged below.
	//
	// Cloudflare AI Gateway auth is header-owned: pi's resolver returns
	// cf-aig-authorization plus markers suppressing x-api-key/Authorization, and
	// no api key at all, so pi's createClient takes its plain api-key branch and
	// the SDK sends no x-api-key. The Go port resolves that auth inline here
	// (2026-06-24 divergence), so the key is consumed by the marker bundle and
	// branchKey — the credential the branches below may emit — is empty, which
	// is what pi's createClient sees. The OAuth sniff is unaffected: the caller
	// computed `oauth` from the real apiKey and already excludes this provider.
	// The bundle is built only from a real key. pi's resolver returns it when it
	// HAS a gateway credential; with none it returns nothing and the request is
	// header-owned, so there is no bearer to send and no x-api-key/Authorization
	// to suppress. Since 8b5899dce widened the gate to accept header-owned auth,
	// an empty key reaches here, and building the bundle from it would synthesize
	// `cf-aig-authorization: "Bearer "` — a credential pi never emits.
	branchKey := apiKey
	var providerAuthHeaders ai.ProviderHeaders
	if model.Provider == "cloudflare-ai-gateway" && apiKey != "" {
		providerAuthHeaders = cloudflareAIGatewayAuthHeaders(apiKey)
		branchKey = ""
	}

	// Branch order mirrors pi createClient (anthropic-messages.ts): github-copilot,
	// then the OAuth sniff, then plain api-key auth. The ANTHROPIC_AUTH_TOKEN
	// bearer path (upstream 24e5cc04) sits ahead of them: it is set only for the
	// anthropic provider and, like pi's resolve(), sends Authorization: Bearer.
	// Each branch now contributes auth and identity only — the betas the OAuth
	// branch used to own are gated on the same `oauth` flag inside
	// getAnthropicBetaFeatures instead.
	switch {
	case authToken != "":
		o.set("authorization", "Bearer "+authToken)
	case model.Provider == "github-copilot":
		// pi: `authToken: apiKey ?? null`. No key means no Authorization header
		// at all — the request rides on whatever header owns its auth — not an
		// empty bearer.
		if branchKey != "" {
			o.set("authorization", "Bearer "+branchKey)
		}
	case oauth:
		o.set("authorization", "Bearer "+branchKey)
		o.set("user-agent", "claude-cli/"+claudeCodeVersion)
		o.set("x-app", "cli")
	default:
		// pi hands the SDK `apiKey: apiKey ?? null`, and a null one sends no
		// x-api-key — its "API key or header-owned auth" branch. A key that
		// reaches createClient is never the empty string (assertRequestAuth
		// rejects a falsy one unless a header carries the credential), so an
		// empty branchKey here is pi's null: emit nothing and let the header
		// that authorized the request do the work.
		if branchKey != "" {
			o.set("x-api-key", branchKey)
		}
		// pi anthropic.ts:496-497: cacheSessionId is dropped when the effective
		// cacheRetention is "none", so no session-affinity header is sent under
		// either name.
		if opts.SessionID != "" && compat.sendSessionAffinityHeaders &&
			resolveCacheRetention(opts.CacheRetention, opts.Env) != ai.CacheNone {
			// One name or the other, never both (pi bbb61e34a).
			if compat.sessionAffinityFormat == sessionAffinityOpenRouter {
				o.set("x-session-id", opts.SessionID)
			} else {
				o.set("x-session-affinity", opts.SessionID)
			}
		}
	}

	// Header-owned provider auth sits above attribution and below the
	// model/consumer headers, where pi's auth.headers enter the merge.
	o.merge(providerAuthHeaders)
	o.merge(model.Headers)
	// pi merges copilotDynamicHeaders after model.headers, before options
	// headers (anthropic-messages.ts createClient).
	if model.Provider == "github-copilot" {
		o.mergeStrings(buildCopilotDynamicHeaders(messages, hasCopilotVisionInput(messages)))
	}
	// pi options.headers (consumer) are spread last and win over everything
	// above, including model.Headers and the attribution defaults — a deletion
	// marker here suppresses any of them.
	o.merge(opts.Headers)

	o.applyAsDefaultHeaders(r.Header)
}

// mapAnthropicStopReason maps an Anthropic stop_reason to the unified
// StopReason and, for a refusal, surfaces the stop_details explanation as an
// error message (pi anthropic.ts mapStopReason returns {stopReason,
// errorMessage?}). reason is the truthy value the event carries: pi's switch
// matches strings only, and any other value is unhandled.
//
// errorMessage is nil where pi's is undefined. Where it is set it is truthy in
// pi — the handler's `if (stopReasonResult.errorMessage)` assigns it, and the
// stream's `errorMessage || "An unknown error occurred"` keeps it — even when
// its String() is empty, as an empty array's is.
func mapAnthropicStopReason(reason, stopDetails json.RawMessage) (ai.StopReason, *string, error) {
	message := func(text string) *string { return &text }
	name, _ := rawString(reason)
	switch name {
	case "end_turn":
		return ai.StopStop, nil, nil
	case "max_tokens":
		return ai.StopLength, nil, nil
	case "tool_use":
		return ai.StopToolUse, nil, nil
	case "refusal":
		// pi: `stopDetails?.explanation || "The model refused..."`. The
		// explanation reaches the stream's error through `new Error(...)`,
		// which takes String() of it; one with no string form makes that
		// throw V8's TypeError, whose text then is the error.
		explanation := rawOptional(stopDetails)["explanation"]
		if !rawTruthy(explanation) {
			return ai.StopError, message("The model refused to complete the request"), nil
		}
		text, err := rawToString(explanation)
		if err != nil {
			text = err.Error()
		}
		return ai.StopError, &text, nil
	case "pause_turn", "stop_sequence":
		return ai.StopStop, nil, nil
	case "sensitive": // Content flagged by safety filters (not yet in SDK types)
		return ai.StopError, message(providerStoppedPrefix + "sensitive"), nil
	default:
		// `Unhandled stop reason: ${reason}` — the template takes String().
		text, err := rawToString(reason)
		if err != nil {
			return "", nil, err
		}
		return "", nil, fmt.Errorf("Unhandled stop reason: %s", text)
	}
}

// ---- SSE parsing ----

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

// iterateAnthropicSSE parses the SSE body and invokes handle for each known
// event (pi iterateSseMessages + iterateAnthropicEvents).
//
// Only events named in anthropicMessageEvents reach handle, and only when
// their data is a JSON object: any other JSON value has no `type` for pi's
// loop to match, so pi carries on past it. A `null` fails the stream the way
// pi's `event.type` read does, inside the same try that wraps a parse failure.
//
// onEvent, when non-nil, observes every event pi's iterateAnthropicEvents
// yields — each named message event whose data parses (after repair) to
// anything but null, objects and other values alike — before it is handled
// (pi's onProviderStreamEvent, upstream 002fc8385). It receives the parsed
// value with objects as ai.OrderedObject, and its error ends the iteration.
// handle gets an object event's members, read as pi reads the event (see
// jsread.go), so no member of an unexpected type can fail the event before
// pi's own reads would.
func iterateAnthropicSSE(body io.Reader, ctx context.Context, onEvent func(any) error, handle func(rawObject) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	scanner.Split(scanSSELines)

	var eventName string
	var dataLines []string
	// rawLines is pi's state.raw: every non-empty line, comments included. It
	// is cleared only when an event is flushed, so the lines of a block that
	// carried neither an event name nor data lead the next event's raw.
	var rawLines []string

	flush := func() error {
		if eventName == "" && len(dataLines) == 0 {
			return nil
		}
		name := eventName
		data := strings.Join(dataLines, "\n")
		raw := rawLines
		eventName = ""
		dataLines = nil
		rawLines = nil

		if name == "error" {
			return fmt.Errorf("%s", data)
		}
		if !anthropicMessageEvents[name] {
			return nil
		}
		// pi: `Could not parse Anthropic SSE event ${sse.event}: ${message};
		// data=${sse.data}; raw=${sse.raw.join("\\n")}` — the raw lines are
		// joined with a literal backslash-n, not a newline.
		parseFailure := func(cause error) error {
			return fmt.Errorf("Could not parse Anthropic SSE event %s: %v; data=%s; raw=%s", name, cause, data, strings.Join(raw, `\n`))
		}
		text, err := jsonTextWithRepair(data)
		if err != nil {
			return parseFailure(err)
		}
		kind := jsonValueKind(text)
		if kind == 'n' {
			return parseFailure(errors.New("Cannot read properties of null (reading 'type')"))
		}
		if onEvent != nil {
			value, err := ai.DecodeOrderedValue([]byte(text))
			if err != nil {
				return parseFailure(err)
			}
			if err := onEvent(value); err != nil {
				return err
			}
		}
		if kind != '{' {
			return nil
		}
		ev, err := decodeRawObject([]byte(text))
		if err != nil {
			return parseFailure(err) // unreachable: text is valid JSON
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
		rawLines = append(rawLines, line)
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

// applyMessageStartUsage is message_start's usage reads: pi assigns each count
// `|| 0` on EVERY message_start, the 1h cache write included, so a later start
// without a breakdown resets an earlier start's value rather than inheriting
// it. It reads no reasoning breakdown; only message_delta's usage carries one.
// A count Go cannot hold (see rawCount) reads as 0.
func applyMessageStartUsage(usage *ai.Usage, u rawObject) {
	usage.Input, _ = rawCount(u["input_tokens"])
	usage.Output, _ = rawCount(u["output_tokens"])
	usage.CacheRead, _ = rawCount(u["cache_read_input_tokens"])
	usage.CacheWrite, _ = rawCount(u["cache_creation_input_tokens"])
	usage.CacheWrite1h, _ = rawCount(rawOptional(u["cache_creation"])["ephemeral_1h_input_tokens"])
	usage.TotalTokens = usage.Input + usage.Output + usage.CacheRead + usage.CacheWrite
}

// applyMessageDeltaUsage is message_delta's usage reads: each count is
// assigned only when it is `!= null`, so a proxy that omits one leaves
// message_start's standing. A count Go cannot hold (see rawCount) is skipped
// as an absent one is.
func applyMessageDeltaUsage(usage *ai.Usage, u rawObject) {
	assign := func(dst *int, raw json.RawMessage) {
		if n, ok := rawCount(raw); ok {
			*dst = n
		}
	}
	assign(&usage.Input, u["input_tokens"])
	assign(&usage.Output, u["output_tokens"])
	assign(&usage.CacheRead, u["cache_read_input_tokens"])
	assign(&usage.CacheWrite, u["cache_creation_input_tokens"])
	// Vercel AI Gateway reports the TTL breakdown in message_delta too, where
	// the SDK types it only on message_start (upstream 667fc3dd3, #9210).
	// pi's `cacheCreation?.ephemeral_1h_input_tokens != null`: an explicit 0
	// is assigned, while an absent or null cache_creation or key — or a
	// cache_creation that is not an object — leaves the earlier value. It does
	// not depend on cache_creation_input_tokens, so a delta can report more 1h
	// tokens than CacheWrite holds, and CalculateCost prices that exactly as pi
	// does.
	assign(&usage.CacheWrite1h, rawOptional(u["cache_creation"])["ephemeral_1h_input_tokens"])
	// Anthropic reports reasoning tokens as a subset of output tokens, in
	// output_tokens_details.thinking_tokens on the final message_delta usage;
	// pi sets reasoning only when that is present (upstream 4e69b0c28).
	assign(&usage.Reasoning, rawOptional(u["output_tokens_details"])["thinking_tokens"])
}

// anthropicSeedText is `value ?? ""` for a text pi then appends deltas to with
// `+=`: "" for undefined or null, a string's text, and String() of any other
// value, which is what the first `+=` makes of it (a value with no string
// form, where that `+=` would throw, seeds nothing).
func anthropicSeedText(raw json.RawMessage) string {
	if rawNullish(raw) {
		return ""
	}
	text, _ := rawToString(raw)
	return text
}

// flattenHeaders is pi's headersToRecord(response.headers), the record every
// adapter hands onResponse. It iterates Headers.entries(), which yields each
// name lowercased with a repeated name's values joined by ", " in wire order —
// except set-cookie, whose values it yields one at a time, so the record keeps
// the last.
func flattenHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		if len(v) == 0 {
			continue
		}
		name := strings.ToLower(k)
		if name == "set-cookie" {
			out[name] = v[len(v)-1]
		} else {
			out[name] = strings.Join(v, ", ")
		}
	}
	return out
}

// RegisterAnthropic registers the anthropic-messages api provider.
func RegisterAnthropic() {
	ai.RegisterApiProvider(ai.ApiProvider{
		Api: ai.APIAnthropicMessages,
		Stream: func(ctx context.Context, model *ai.Model, req ai.TranscriptContext, opts *ai.StreamOptions) *ai.AssistantMessageEventStream {
			aopts := &AnthropicOptions{}
			if opts != nil {
				aopts.StreamOptions = *opts
			}
			return StreamAnthropic(ctx, model, req, aopts)
		},
		StreamSimple: StreamSimpleAnthropic,
	})
}
