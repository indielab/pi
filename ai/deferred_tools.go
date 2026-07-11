package ai

// Cache-friendly dynamic tool loading, ported from pi
// packages/ai/src/utils/deferred-tools.ts (upstream 3d8f7435).
//
// A tool result may mark tools it introduced via ToolResultMessage.AddedToolNames.
// Providers with native deferred loading (Anthropic tool_reference blocks, OpenAI
// Responses tool_search items) load those definitions at the transcript point they
// appear instead of in the cached prompt prefix; other providers ignore the split
// and send every tool normally.

// ToolNameNormalizer maps a tool name to its canonical form (identity by default;
// the Anthropic OAuth path maps to Claude Code tool names).
type ToolNameNormalizer func(name string) string

// DeferredToolSplit is the result of SplitDeferredTools. It mirrors pi's
// `{ immediate: Tool[]; deferred: Map<string, Tool> }`: Deferred preserves the
// Map's insertion-ordered values (`.values()`) and ByName mirrors `.get(name)`,
// both keyed by normalized name.
type DeferredToolSplit struct {
	Immediate []Tool
	Deferred  []Tool
	ByName    map[string]Tool
}

// SplitDeferredTools splits Context.Tools into the immediate prefix definitions
// and the transcript-loaded (deferred) definitions, preserving insertion order.
// When enabled is false every unique tool is immediate. normalizeName defaults to
// identity.
func SplitDeferredTools(context Context, enabled bool, normalizeName ToolNameNormalizer) DeferredToolSplit {
	if normalizeName == nil {
		normalizeName = func(name string) string { return name }
	}

	// uniqueTools dedups by normalized name; a later definition replaces the value
	// but keeps the first occurrence's position (JS Map.set semantics).
	order := make([]string, 0, len(context.Tools))
	uniqueTools := make(map[string]Tool, len(context.Tools))
	for _, tool := range context.Tools {
		key := normalizeName(tool.Name)
		if _, exists := uniqueTools[key]; !exists {
			order = append(order, key)
		}
		uniqueTools[key] = tool
	}

	if !enabled {
		immediate := make([]Tool, 0, len(order))
		for _, key := range order {
			immediate = append(immediate, uniqueTools[key])
		}
		return DeferredToolSplit{Immediate: immediate}
	}

	// A tool is deferred only if a tool result marks it AND it was not already used
	// by an assistant before its marker. The single forward pass matters: usedNames
	// accumulates in transcript order, so usage after a marker does not un-defer.
	deferredNames := map[string]bool{}
	usedNames := map[string]bool{}
	for _, message := range context.Messages {
		switch m := message.(type) {
		case AssistantMessage:
			collectUsedToolNames(m.Content, normalizeName, usedNames)
		case *AssistantMessage:
			collectUsedToolNames(m.Content, normalizeName, usedNames)
		case ToolResultMessage:
			collectDeferredNames(m.AddedToolNames, normalizeName, usedNames, deferredNames)
		case *ToolResultMessage:
			collectDeferredNames(m.AddedToolNames, normalizeName, usedNames, deferredNames)
		}
	}

	split := DeferredToolSplit{Immediate: []Tool{}, Deferred: []Tool{}, ByName: map[string]Tool{}}
	for _, key := range order {
		tool := uniqueTools[key]
		if deferredNames[key] {
			split.Deferred = append(split.Deferred, tool)
			split.ByName[key] = tool
		} else {
			split.Immediate = append(split.Immediate, tool)
		}
	}
	return split
}

func collectUsedToolNames(content ContentList, normalizeName ToolNameNormalizer, usedNames map[string]bool) {
	for _, block := range content {
		if tc, ok := block.(ToolCall); ok {
			usedNames[normalizeName(tc.Name)] = true
		}
	}
}

func collectDeferredNames(addedToolNames []string, normalizeName ToolNameNormalizer, usedNames, deferredNames map[string]bool) {
	for _, name := range addedToolNames {
		normalized := normalizeName(name)
		if !usedNames[normalized] {
			deferredNames[normalized] = true
		}
	}
}
