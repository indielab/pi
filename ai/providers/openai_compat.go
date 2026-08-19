package providers

import (
	"encoding/json"
	"strings"

	"github.com/sky-valley/pi/ai"
)

// Session-affinity header formats (pi SessionAffinityFormat). "openai" sends
// session_id + x-client-request-id (+ x-session-affinity on completions);
// "openai-nosession" drops session_id; "openrouter" sends x-session-id.
const (
	sessionAffinityOpenAI     = "openai"
	sessionAffinityOpenRouter = "openrouter"
)

// sessionAffinityFormatFor is pi's `isOpenRouter ? "openrouter" : "openai"`
// default, shared by the completions and responses providers.
func sessionAffinityFormatFor(isOpenRouter bool) string {
	if isOpenRouter {
		return sessionAffinityOpenRouter
	}
	return sessionAffinityOpenAI
}

// vercelGatewayRouting mirrors pi's vercelGatewayRouting object shape.
type vercelGatewayRouting struct {
	Only  []string `json:"only,omitempty"`
	Order []string `json:"order,omitempty"`
}

// openAICompletionsCompat is the resolved compatibility profile for an
// OpenAI-compatible chat-completions provider (port of ResolvedOpenAICompletionsCompat).
type openAICompletionsCompat struct {
	SupportsStore            bool
	SupportsDeveloperRole    bool
	SupportsReasoningEffort  bool
	SupportsUsageInStreaming bool
	// SupportsFinishReason reports whether streamed responses include
	// finish_reason. When false, pi infers stop/toolUse at end of stream instead
	// of failing with "Stream ended without finish_reason" (upstream 2c3041242).
	// Default: true.
	SupportsFinishReason bool
	MaxTokensField       string // "max_tokens" | "max_completion_tokens"
	ThinkingFormat       string
	SupportsStrictMode   bool
	// SupportsOpenAIGrammarTools reports whether the provider accepts OpenAI
	// custom tools with Lark/regex grammar formats. When false, grammar-
	// constrained tools fall back to normal function tools. Default: false.
	SupportsOpenAIGrammarTools                  bool
	SupportsLongCacheRetention                  bool
	RequiresReasoningContentOnAssistantMessages bool
	RequiresToolResultName                      bool
	RequiresAssistantAfterToolResult            bool
	RequiresThinkingAsText                      bool
	ZaiToolStream                               bool
	// ThinkingTokenBudgetField is the top-level request field used to cap
	// reasoning tokens from the caller's ThinkingBudgets (pi types.ts:596-606,
	// upstream b23741269). Reasoning and the answer share max_tokens on these
	// endpoints, so without a budget a reasoning-heavy turn can consume the
	// whole response and emit no answer. "thinking_token_budget" is vLLM,
	// "thinking_budget" is Qwen/DashScope/SGLang, "thinking_budget_tokens" is
	// llama.cpp. "" is pi's undefined (off); the generated catalog never sets it.
	ThinkingTokenBudgetField string
	// SupportsThinkingTokenBudget is pi's retained alias for
	// ThinkingTokenBudgetField: "thinking_token_budget" (vLLM). Prefer the field
	// name (pi types.ts:607-608, upstream b23741269). Default: false.
	SupportsThinkingTokenBudget bool
	SendSessionAffinityHeaders  bool
	// DeferredToolsMode selects a provider-specific deferred-tool serialization
	// (pi OpenAICompletionsCompat.deferredToolsMode). "kimi" withholds tools
	// introduced by a tool result's addedToolNames from the top-level tools
	// param and re-declares them in a system message after that tool-result
	// run. "" (pi undefined) disables deferral.
	DeferredToolsMode string
	// SessionAffinityFormat selects the session-affinity header shape (pi
	// SessionAffinityFormat). Auto-detected: openrouter → sessionAffinityOpenRouter,
	// else sessionAffinityOpenAI.
	SessionAffinityFormat string
	CacheControlFormat    string // "" | "anthropic"
	// OpenRouterRouting is an arbitrary provider-routing object sent as `provider`.
	OpenRouterRouting map[string]any
	// HasOpenRouterRouting records that model.compat carried a non-null
	// openRouterRouting (pi sends `provider` for any truthy object, even {}).
	HasOpenRouterRouting bool
	// VercelGatewayRouting carries only/order routing for the Vercel AI Gateway.
	VercelGatewayRouting vercelGatewayRouting
	// ChatTemplateKwargs carries the ordered kwargs sent as `chat_template_kwargs`
	// when ThinkingFormat is "chat-template" (pi: compat.chatTemplateKwargs).
	ChatTemplateKwargs []chatTemplateKwarg
	// ChatTemplateArgs carries the ordered args sent as `chat_template_args`
	// when ThinkingFormat is "baseten" (pi: compat.chatTemplateArgs).
	ChatTemplateArgs []chatTemplateKwarg
}

// detectOpenAICompat infers compatibility settings from provider + baseUrl,
// matching pi's detectCompat. Provider takes precedence over URL detection.
func detectOpenAICompat(model *ai.Model) openAICompletionsCompat {
	provider := model.Provider
	baseURL := model.BaseURL
	has := func(s string) bool { return strings.Contains(baseURL, s) }

	isZai := provider == "zai" || provider == "zai-coding-cn" || has("api.z.ai") || has("open.bigmodel.cn")
	isTogether := provider == "together" || has("api.together.ai") || has("api.together.xyz")
	isMoonshot := provider == "moonshotai" || provider == "moonshotai-cn" || has("api.moonshot.")
	isOpenRouter := provider == "openrouter" || has("openrouter.ai")
	isCloudflareWorkersAI := provider == "cloudflare-workers-ai" || has("api.cloudflare.com")
	isCloudflareAiGateway := provider == "cloudflare-ai-gateway" || has("gateway.ai.cloudflare.com")
	isNvidia := provider == "nvidia" || has("integrate.api.nvidia.com")
	isAntLing := provider == "ant-ling" || has("api.ant-ling.com")
	// pi b647d1879: the DeepSeek probe alone folds case, and it is hoisted above
	// isNonStandard so both disjunctions read from it. Every other probe here
	// stays case-sensitive, exactly as upstream leaves them.
	isDeepSeek := provider == "deepseek" || strings.Contains(strings.ToLower(baseURL), "deepseek.com")

	isNonStandard := isNvidia || provider == "cerebras" || has("cerebras.ai") ||
		provider == "xai" || has("api.x.ai") || isTogether || has("chutes.ai") ||
		isDeepSeek || isZai || isMoonshot || provider == "opencode" ||
		has("opencode.ai") || isCloudflareWorkersAI || isCloudflareAiGateway || isAntLing
	useMaxTokens := has("chutes.ai") || isDeepSeek || isMoonshot || isCloudflareAiGateway || isTogether || isNvidia || isAntLing || isZai

	isGrok := provider == "xai" || has("api.x.ai")
	isOpenRouterDeveloperRoleModel := isOpenRouter && (strings.HasPrefix(model.ID, "anthropic/") || strings.HasPrefix(model.ID, "openai/"))
	cacheControlFormat := ""
	if provider == "openrouter" && strings.HasPrefix(model.ID, "anthropic/") {
		cacheControlFormat = "anthropic"
	}

	thinkingFormat := "openai"
	switch {
	case isDeepSeek:
		thinkingFormat = "deepseek"
	case isZai:
		thinkingFormat = "zai"
	case isTogether:
		thinkingFormat = "together"
	case isAntLing:
		thinkingFormat = "ant-ling"
	case isOpenRouter:
		thinkingFormat = "openrouter"
	}

	maxTokensField := "max_completion_tokens"
	if useMaxTokens {
		maxTokensField = "max_tokens"
	}

	return openAICompletionsCompat{
		SupportsStore:                               !isNonStandard,
		SupportsDeveloperRole:                       isOpenRouterDeveloperRoleModel || (!isNonStandard && !isOpenRouter),
		SupportsReasoningEffort:                     !isGrok && !isZai && !isMoonshot && !isTogether && !isCloudflareAiGateway && !isNvidia && !isAntLing,
		SupportsUsageInStreaming:                    true,
		SupportsFinishReason:                        true,
		MaxTokensField:                              maxTokensField,
		ThinkingFormat:                              thinkingFormat,
		SupportsStrictMode:                          !isMoonshot && !isTogether && !isCloudflareAiGateway && !isNvidia,
		SupportsOpenAIGrammarTools:                  false,
		SupportsLongCacheRetention:                  !(isTogether || isCloudflareWorkersAI || isCloudflareAiGateway || isNvidia || isAntLing),
		RequiresReasoningContentOnAssistantMessages: isDeepSeek,
		RequiresToolResultName:                      false,
		RequiresAssistantAfterToolResult:            false,
		RequiresThinkingAsText:                      false,
		ZaiToolStream:                               false,
		ThinkingTokenBudgetField:                    "",
		SupportsThinkingTokenBudget:                 false,
		SendSessionAffinityHeaders:                  false,
		DeferredToolsMode:                           "",
		SessionAffinityFormat:                       sessionAffinityFormatFor(isOpenRouter),
		CacheControlFormat:                          cacheControlFormat,
		// pi defaults these routing objects to {} (no routing emitted).
		OpenRouterRouting:    nil,
		VercelGatewayRouting: vercelGatewayRouting{},
	}
}

// getOpenAICompat applies explicit model.compat overrides over the detected profile.
func getOpenAICompat(model *ai.Model) openAICompletionsCompat {
	c := detectOpenAICompat(model)
	if len(model.Compat) == 0 {
		return c
	}
	var raw struct {
		SupportsStore                               *bool                 `json:"supportsStore"`
		SupportsDeveloperRole                       *bool                 `json:"supportsDeveloperRole"`
		SupportsReasoningEffort                     *bool                 `json:"supportsReasoningEffort"`
		SupportsUsageInStreaming                    *bool                 `json:"supportsUsageInStreaming"`
		SupportsFinishReason                        *bool                 `json:"supportsFinishReason"`
		MaxTokensField                              *string               `json:"maxTokensField"`
		ThinkingFormat                              *string               `json:"thinkingFormat"`
		SupportsStrictMode                          *bool                 `json:"supportsStrictMode"`
		SupportsOpenAIGrammarTools                  *bool                 `json:"supportsOpenAIGrammarTools"`
		SupportsLongCacheRetention                  *bool                 `json:"supportsLongCacheRetention"`
		RequiresReasoningContentOnAssistantMessages *bool                 `json:"requiresReasoningContentOnAssistantMessages"`
		RequiresToolResultName                      *bool                 `json:"requiresToolResultName"`
		RequiresAssistantAfterToolResult            *bool                 `json:"requiresAssistantAfterToolResult"`
		RequiresThinkingAsText                      *bool                 `json:"requiresThinkingAsText"`
		ZaiToolStream                               *bool                 `json:"zaiToolStream"`
		ThinkingTokenBudgetField                    *string               `json:"thinkingTokenBudgetField"`
		SupportsThinkingTokenBudget                 *bool                 `json:"supportsThinkingTokenBudget"`
		SendSessionAffinityHeaders                  *bool                 `json:"sendSessionAffinityHeaders"`
		DeferredToolsMode                           *string               `json:"deferredToolsMode"`
		SessionAffinityFormat                       *string               `json:"sessionAffinityFormat"`
		CacheControlFormat                          *string               `json:"cacheControlFormat"`
		OpenRouterRouting                           map[string]any        `json:"openRouterRouting"`
		VercelGatewayRouting                        *vercelGatewayRouting `json:"vercelGatewayRouting"`
		ChatTemplateKwargs                          json.RawMessage       `json:"chatTemplateKwargs"`
		ChatTemplateArgs                            json.RawMessage       `json:"chatTemplateArgs"`
	}
	if json.Unmarshal(model.Compat, &raw) != nil {
		return c
	}
	if raw.SupportsStore != nil {
		c.SupportsStore = *raw.SupportsStore
	}
	if raw.SupportsDeveloperRole != nil {
		c.SupportsDeveloperRole = *raw.SupportsDeveloperRole
	}
	if raw.SupportsReasoningEffort != nil {
		c.SupportsReasoningEffort = *raw.SupportsReasoningEffort
	}
	if raw.SupportsUsageInStreaming != nil {
		c.SupportsUsageInStreaming = *raw.SupportsUsageInStreaming
	}
	if raw.SupportsFinishReason != nil {
		c.SupportsFinishReason = *raw.SupportsFinishReason
	}
	if raw.MaxTokensField != nil {
		c.MaxTokensField = *raw.MaxTokensField
	}
	if raw.ThinkingFormat != nil {
		c.ThinkingFormat = *raw.ThinkingFormat
	}
	if raw.SupportsStrictMode != nil {
		c.SupportsStrictMode = *raw.SupportsStrictMode
	}
	if raw.SupportsOpenAIGrammarTools != nil {
		c.SupportsOpenAIGrammarTools = *raw.SupportsOpenAIGrammarTools
	}
	if raw.SupportsLongCacheRetention != nil {
		c.SupportsLongCacheRetention = *raw.SupportsLongCacheRetention
	}
	if raw.RequiresReasoningContentOnAssistantMessages != nil {
		c.RequiresReasoningContentOnAssistantMessages = *raw.RequiresReasoningContentOnAssistantMessages
	}
	if raw.RequiresToolResultName != nil {
		c.RequiresToolResultName = *raw.RequiresToolResultName
	}
	if raw.RequiresAssistantAfterToolResult != nil {
		c.RequiresAssistantAfterToolResult = *raw.RequiresAssistantAfterToolResult
	}
	if raw.RequiresThinkingAsText != nil {
		c.RequiresThinkingAsText = *raw.RequiresThinkingAsText
	}
	if raw.ZaiToolStream != nil {
		c.ZaiToolStream = *raw.ZaiToolStream
	}
	if raw.ThinkingTokenBudgetField != nil {
		c.ThinkingTokenBudgetField = *raw.ThinkingTokenBudgetField
	}
	if raw.SupportsThinkingTokenBudget != nil {
		c.SupportsThinkingTokenBudget = *raw.SupportsThinkingTokenBudget
	}
	if raw.SendSessionAffinityHeaders != nil {
		c.SendSessionAffinityHeaders = *raw.SendSessionAffinityHeaders
	}
	if raw.DeferredToolsMode != nil {
		c.DeferredToolsMode = *raw.DeferredToolsMode
	}
	if raw.SessionAffinityFormat != nil {
		c.SessionAffinityFormat = *raw.SessionAffinityFormat
	}
	if raw.CacheControlFormat != nil {
		c.CacheControlFormat = *raw.CacheControlFormat
	}
	// pi: openRouterRouting falls back to {} (override always replaces). An
	// explicit {} in model.compat is truthy in JS, so record its presence.
	if raw.OpenRouterRouting != nil {
		c.OpenRouterRouting = raw.OpenRouterRouting
		c.HasOpenRouterRouting = true
	}
	if raw.VercelGatewayRouting != nil {
		c.VercelGatewayRouting = *raw.VercelGatewayRouting
	}
	// pi: chatTemplateKwargs/chatTemplateArgs overrides always replace the
	// detected defaults ({}).
	if raw.ChatTemplateKwargs != nil {
		c.ChatTemplateKwargs = parseChatTemplateValues(raw.ChatTemplateKwargs)
	}
	if raw.ChatTemplateArgs != nil {
		c.ChatTemplateArgs = parseChatTemplateValues(raw.ChatTemplateArgs)
	}
	return c
}

// effortValue maps a unified reasoning level to the provider-specific wire value
// via the model's thinkingLevelMap, falling back to the level itself.
func effortValue(model *ai.Model, level string) string {
	if model.ThinkingLevelMap != nil {
		if v, ok := model.ThinkingLevelMap[ai.ModelThinkingLevel(level)]; ok && v != nil {
			return *v
		}
	}
	return level
}

// offEffortValue returns the model's mapped "off" reasoning value, if it is a
// concrete string (some providers need "none"/"minimal" rather than omission).
func offEffortValue(model *ai.Model) (string, bool) {
	if model.ThinkingLevelMap != nil {
		if v, ok := model.ThinkingLevelMap["off"]; ok && v != nil {
			return *v, true
		}
	}
	return "", false
}

// offEffortOrDefault ports pi's `thinkingLevelMap?.off !== null` branch used by
// the openrouter / string-thinking formats. It distinguishes:
//   - off present and null   -> omit reasoning entirely (send=false)
//   - off present and string -> send that string
//   - off absent (undefined) -> send the provided default ("none")
func offEffortOrDefault(model *ai.Model, def string) (value string, send bool) {
	if model.ThinkingLevelMap != nil {
		if v, ok := model.ThinkingLevelMap["off"]; ok {
			if v == nil {
				return "", false // present-null: pi omits reasoning
			}
			return *v, true
		}
	}
	return def, true // absent: pi falls back to default
}
