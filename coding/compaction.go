package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/sky-valley/pi/agent"
	"github.com/sky-valley/pi/ai"
)

// CompactionSettings configures automatic context-window compaction (port of
// pi's CompactionSettings / DEFAULT_COMPACTION_SETTINGS).
type CompactionSettings struct {
	Enabled          bool
	ReserveTokens    int
	KeepRecentTokens int
	// SessionID, when set, is the routing session ID summarization requests
	// reuse instead of minting a fresh one per request; cache retention stays
	// "none" either way (pi compact()'s optional sessionId param, upstream
	// 58302d34e "support compaction routing sessions").
	SessionID string
}

// DefaultCompactionSettings mirrors pi's defaults.
var DefaultCompactionSettings = CompactionSettings{
	Enabled:          true,
	ReserveTokens:    16384,
	KeepRecentTokens: 20000,
}

// estimatedImageChars mirrors pi's ESTIMATED_IMAGE_CHARS (compaction.ts:228):
// the per-image char estimate used by the token heuristic.
const estimatedImageChars = 4800

// toolResultMaxChars mirrors pi's TOOL_RESULT_MAX_CHARS (utils.ts:89): tool
// results are truncated to this many characters when serialized for summarization.
const toolResultMaxChars = 2000

// summarizationSystemPrompt is pi's SUMMARIZATION_SYSTEM_PROMPT (utils.ts:168),
// the dedicated system prompt for the summarization request. Byte-for-byte.
const summarizationSystemPrompt = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI coding assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`

const summarizationPrompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

// updateSummarizationPrompt is pi's UPDATE_SUMMARIZATION_PROMPT (compaction.ts:487),
// used when a previous compaction summary exists. Byte-for-byte from the npm build.
const updateSummarizationPrompt = `The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.

Update the existing structured summary with new information. RULES:
- PRESERVE all existing information from the previous summary
- ADD new progress, decisions, and context from the new messages
- UPDATE the Progress section: move items from "In Progress" to "Done" when completed
- UPDATE "Next Steps" based on what was accomplished
- PRESERVE exact file paths, function names, and error messages
- If something is no longer relevant, you may remove it

Use this EXACT format:

## Goal
[Preserve existing goals, add new ones if the task expanded]

## Constraints & Preferences
- [Preserve existing, add new ones discovered]

## Progress
### Done
- [x] [Include previously done items AND newly completed items]

### In Progress
- [ ] [Current work - update based on progress]

### Blocked
- [Current blockers - remove if resolved]

## Key Decisions
- **[Decision]**: [Brief rationale] (preserve all previous, add new)

## Next Steps
1. [Update based on current state]

## Critical Context
- [Preserve important context, add new if needed]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

// turnPrefixSummarizationPrompt is pi's TURN_PREFIX_SUMMARIZATION_PROMPT
// (compaction.ts:725), used for the prefix of a split turn. Byte-for-byte.
const turnPrefixSummarizationPrompt = `This is the PREFIX of a turn that was too large to keep. The SUFFIX (recent work) is retained.

Summarize the prefix to provide context for the retained suffix:

## Original Request
[What did the user ask for in this turn?]

## Early Progress
- [Key decisions and work done in the prefix]

## Context for Suffix
- [Information needed to understand the retained recent work]

Be concise. Focus on what's needed to understand the kept suffix.`

// EstimateMessageTokens estimates the token cost of a message (port of
// estimateTokens: char count / 4, rounded up).
func EstimateMessageTokens(m agent.AgentMessage) int {
	chars := 0
	switch v := m.(type) {
	case ai.UserMessage:
		chars = contentChars(v.Content)
	case *ai.AssistantMessage:
		chars = assistantChars(v)
	case ai.AssistantMessage:
		chars = assistantChars(&v)
	case ai.ToolResultMessage:
		chars = contentChars(v.Content)
	}
	return int(math.Ceil(float64(chars) / 4))
}

func assistantChars(a *ai.AssistantMessage) int {
	chars := 0
	for _, c := range a.Content {
		switch b := c.(type) {
		case ai.TextContent:
			chars += len(b.Text)
		case ai.ThinkingContent:
			chars += len(b.Thinking)
		case ai.ToolCall:
			args, _ := json.Marshal(b.Arguments)
			chars += len(b.Name) + len(args)
		}
	}
	return chars
}

func contentChars(content ai.ContentList) int {
	chars := 0
	for _, c := range content {
		switch b := c.(type) {
		case ai.TextContent:
			chars += len(b.Text)
		case ai.ImageContent:
			chars += estimatedImageChars // fixed estimate for an inline image
		}
	}
	return chars
}

// EstimateContextTokens sums estimated tokens across messages (pure heuristic).
func EstimateContextTokens(messages []agent.AgentMessage) int {
	total := 0
	for _, m := range messages {
		total += EstimateMessageTokens(m)
	}
	return total
}

// estimateContextTokensUsageAware blends the real token usage reported by the
// last assistant turn with a heuristic estimate of the trailing messages (port
// of pi's estimateContextTokens). This is far more accurate than the pure
// char/4 heuristic on large contexts (big repos), where it matters most.
func estimateContextTokensUsageAware(messages []agent.AgentMessage) int {
	lastIdx := -1
	var lastUsage ai.Usage
	for i, m := range messages {
		am, ok := messageAsAssistant(m)
		if !ok {
			continue
		}
		// pi's getAssistantUsage (compaction.ts:143-151) returns the last valid
		// usage: it skips aborted, error, AND all-zero-usage messages (upstream
		// cd95c274 added the calculateContextTokens(usage) > 0 guard so a malformed
		// all-zero usage response is not trusted as the anchor).
		if am.StopReason == ai.StopAborted || am.StopReason == ai.StopError {
			continue
		}
		if contextTokensFromUsage(am.Usage) > 0 {
			lastIdx = i
			lastUsage = am.Usage
		}
	}
	if lastIdx == -1 {
		return EstimateContextTokens(messages)
	}
	total := contextTokensFromUsage(lastUsage)
	for i := lastIdx + 1; i < len(messages); i++ {
		total += EstimateMessageTokens(messages[i])
	}
	return total
}

func contextTokensFromUsage(u ai.Usage) int {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.Input + u.Output + u.CacheRead + u.CacheWrite
}

// shouldCompact reports whether the context exceeds the safe budget.
func shouldCompact(contextTokens, contextWindow int, s CompactionSettings) bool {
	if !s.Enabled || contextWindow <= 0 {
		return false
	}
	return contextTokens > contextWindow-s.ReserveTokens
}

// cutPointResult mirrors pi's CutPointResult (compaction.ts:361-368).
type cutPointResult struct {
	// firstKeptIndex is the index of the first message to keep.
	firstKeptIndex int
	// turnStartIndex is the user message starting the turn being split, or -1.
	turnStartIndex int
	// isSplitTurn is true when the cut lands mid-turn (not on a user message).
	isSplitTurn bool
}

// findCutPoint ports pi's findCutPoint + findValidCutPoints (compaction.ts:380-448)
// to a flat message list. Valid cut points are any non-tool-result message (a kept
// run must never start on a tool result). Walking backwards from the newest
// message, tokens accumulate until KeepRecentTokens is reached; the cut then snaps
// FORWARD to the first valid cut point at or after the crossing index (so a
// boundary tool-result goes into the summarized portion). If the budget is never
// reached, the cut defaults to the first valid cut point (keep everything).
// Only messages in [startIndex, endIndex) are considered.
func findCutPoint(messages []agent.AgentMessage, startIndex, endIndex, keepRecentTokens int) cutPointResult {
	var cutPoints []int
	for i := startIndex; i < endIndex; i++ {
		if messages[i].MessageRole() != ai.RoleToolResult {
			cutPoints = append(cutPoints, i)
		}
	}
	if len(cutPoints) == 0 {
		return cutPointResult{firstKeptIndex: startIndex, turnStartIndex: -1}
	}

	// Walk backwards from newest, accumulating estimated message sizes.
	acc := 0
	cutIndex := cutPoints[0] // default: keep from first message
	for i := endIndex - 1; i >= startIndex; i-- {
		acc += EstimateMessageTokens(messages[i])
		if acc >= keepRecentTokens {
			// Snap to the closest valid cut point at or after this index.
			for _, c := range cutPoints {
				if c >= i {
					cutIndex = c
					break
				}
			}
			break
		}
	}

	// Determine if this is a split turn (pi compaction.ts:438-447): a cut not on
	// a user message splits the turn started by the nearest preceding user message.
	isUser := messages[cutIndex].MessageRole() == ai.RoleUser
	turnStart := -1
	if !isUser {
		for i := cutIndex; i >= startIndex; i-- {
			if messages[i].MessageRole() == ai.RoleUser {
				turnStart = i
				break
			}
		}
	}
	return cutPointResult{
		firstKeptIndex: cutIndex,
		turnStartIndex: turnStart,
		isSplitTurn:    !isUser && turnStart != -1,
	}
}

type compactionState struct {
	mu        sync.Mutex
	settings  CompactionSettings
	prefixLen int    // index into the ORIGINAL message list of the first kept message
	summary   string // cached summary text (includes the file-ops appendix, like pi)
	// readFiles/modifiedFiles persist the previous compaction's file lists so the
	// next compaction can merge them (pi extractFileOperations, compaction.ts:41-69).
	readFiles     []string
	modifiedFiles []string
}

// EnableCompaction installs an automatic compaction TransformContext on the
// session's agent using the given settings. When the estimated context exceeds
// the model's window minus ReserveTokens, older turns are summarized (via the
// session's model) into a single checkpoint message and recent turns are kept.
func (s *Session) EnableCompaction(settings CompactionSettings) {
	state := &compactionState{settings: settings}
	s.Agent.TransformContext = func(ctx context.Context, messages []agent.AgentMessage) []agent.AgentMessage {
		return s.compact(ctx, state, messages)
	}
}

// applyCheckpoint builds the compacted view: the checkpoint summary message
// followed by the messages after prefixLen (pi: compaction entry + kept entries).
func applyCheckpoint(summary string, messages []agent.AgentMessage, prefixLen int) []agent.AgentMessage {
	checkpoint := compactionSummaryMessage(summary, nowMillisCoding())
	out := make([]agent.AgentMessage, 0, 1+len(messages)-prefixLen)
	out = append(out, checkpoint)
	out = append(out, messages[prefixLen:]...)
	return out
}

// compact is the per-request TransformContext. pi semantics: compaction is
// PERMANENT — once a summary checkpoint exists it is always applied (pi persists
// a compaction entry; dropped turns never come back). The shouldCompact check
// only decides whether to EXTEND the compaction by summarizing a larger prefix,
// merging via the <previous-summary> update flow (pi prepareCompaction/compact).
//
// state.prefixLen indexes into the ORIGINAL message list, which the agent only
// ever grows by appending, so it stays valid across turns and re-compactions.
func (s *Session) compact(ctx context.Context, state *compactionState, messages []agent.AgentMessage) []agent.AgentMessage {
	window := 0
	if s.Model != nil {
		window = s.Model.ContextWindow
	}

	state.mu.Lock()
	prefixLen := state.prefixLen
	summary := state.summary
	prevRead := state.readFiles
	prevModified := state.modifiedFiles
	state.mu.Unlock()
	if prefixLen > len(messages) {
		prefixLen = len(messages) // defensive: transcript was replaced/shrunk
	}

	// Always re-apply the cached checkpoint first (permanence).
	current := messages
	if summary != "" {
		current = applyCheckpoint(summary, messages, prefixLen)
	}

	tokens := estimateContextTokensUsageAware(current)
	if !shouldCompact(tokens, window, state.settings) {
		return current
	}

	// Extend: find a new cut within the kept tail (pi boundaryStart = previous
	// firstKeptEntryIndex; compaction.ts:660-672).
	cp := findCutPoint(messages, prefixLen, len(messages), state.settings.KeepRecentTokens)
	if cp.firstKeptIndex <= prefixLen {
		return current // nothing new safely summarizable
	}

	historyEnd := cp.firstKeptIndex
	if cp.isSplitTurn {
		historyEnd = cp.turnStartIndex
	}
	history := messages[prefixLen:historyEnd]
	var turnPrefix []agent.AgentMessage
	if cp.isSplitTurn {
		turnPrefix = messages[cp.turnStartIndex:cp.firstKeptIndex]
	}

	// Generate summaries (pi compact, compaction.ts:747-815). pi runs the two
	// split-turn summaries in parallel; we run them sequentially (same output).
	var newSummary string
	if cp.isSplitTurn && len(turnPrefix) > 0 {
		historyResult := "No prior history."
		if len(history) > 0 {
			hr, ok := s.generateSummary(ctx, history, state.settings.ReserveTokens, summary, state.settings.SessionID)
			if !ok {
				return current // summarization failed; keep current view
			}
			historyResult = hr
		}
		tp, ok := s.generateTurnPrefixSummary(ctx, turnPrefix, state.settings.ReserveTokens, state.settings.SessionID)
		if !ok {
			return current
		}
		newSummary = historyResult + "\n\n---\n\n**Turn Context (split turn):**\n\n" + tp
	} else {
		ns, ok := s.generateSummary(ctx, history, state.settings.ReserveTokens, summary, state.settings.SessionID)
		if !ok {
			return current
		}
		newSummary = ns
	}
	if newSummary == "" {
		return current // aborted with no text produced; keep current view
	}

	// Merge file ops from the previous compaction's lists plus the newly
	// summarized messages (pi extractFileOperations + split-turn extraction).
	ops := newFileOps()
	for _, f := range prevRead {
		ops.read[f] = true
	}
	for _, f := range prevModified {
		ops.edited[f] = true
	}
	for _, m := range history {
		extractFileOpsFromMessage(m, ops)
	}
	for _, m := range turnPrefix {
		extractFileOpsFromMessage(m, ops)
	}
	readFiles, modifiedFiles := ops.lists()
	newSummary += formatFileOperations(readFiles, modifiedFiles)

	state.mu.Lock()
	state.prefixLen = cp.firstKeptIndex
	state.summary = newSummary
	state.readFiles = readFiles
	state.modifiedFiles = modifiedFiles
	state.mu.Unlock()

	return applyCheckpoint(newSummary, messages, cp.firstKeptIndex)
}

// summarize asks the model to produce a structured checkpoint of older messages
// with no previous summary, appending the read/modified file lists computed from
// those messages (pi compact, compaction.ts:803-819 for the non-split path).
// Branch-summary-style callers carry no routing session ID (pi generateSummary's
// optional sessionId stays undefined for them), so each request gets a fresh one.
func (s *Session) summarize(ctx context.Context, older []agent.AgentMessage, reserveTokens int) string {
	text, ok := s.generateSummary(ctx, older, reserveTokens, "", "")
	if !ok {
		return ""
	}
	readFiles, modifiedFiles := computeFileLists(older)
	return text + formatFileOperations(readFiles, modifiedFiles)
}

// generateSummary ports pi's generateSummary (compaction.ts:558-620): the
// conversation is serialized to text, wrapped in <conversation>...</conversation>
// (followed by <previous-summary>...</previous-summary> and the update prompt
// variant when a previous summary exists), and sent with the dedicated
// SUMMARIZATION_SYSTEM_PROMPT and a capped maxTokens. Returns ok=false where pi
// fails the summarization (stopReason "error" or "length"); an aborted response
// returns the text produced so far, like pi.
func (s *Session) generateSummary(ctx context.Context, older []agent.AgentMessage, reserveTokens int, previousSummary, sessionID string) (string, bool) {
	conversationText := serializeConversation(messagesAsLlm(older))

	promptText := "<conversation>\n" + conversationText + "\n</conversation>\n\n"
	if previousSummary != "" {
		promptText += "<previous-summary>\n" + previousSummary + "\n</previous-summary>\n\n"
		promptText += updateSummarizationPrompt
	} else {
		promptText += summarizationPrompt
	}

	return s.completeSummarization(ctx, promptText, s.summaryMaxTokens(0.8, reserveTokens), sessionID)
}

// generateTurnPrefixSummary ports pi's generateTurnPrefixSummary
// (compaction.ts:836-876): the prefix of a split turn is summarized with the
// dedicated turn-prefix prompt and a smaller (0.5 * reserve) token budget.
func (s *Session) generateTurnPrefixSummary(ctx context.Context, messages []agent.AgentMessage, reserveTokens int, sessionID string) (string, bool) {
	conversationText := serializeConversation(messagesAsLlm(messages))
	promptText := "<conversation>\n" + conversationText + "\n</conversation>\n\n" + turnPrefixSummarizationPrompt
	return s.completeSummarization(ctx, promptText, s.summaryMaxTokens(0.5, reserveTokens), sessionID)
}

// summaryMaxTokens = min(floor(frac * reserveTokens), model.maxTokens if > 0).
func (s *Session) summaryMaxTokens(frac float64, reserveTokens int) int {
	maxTokens := int(math.Floor(frac * float64(reserveTokens)))
	if s.Model != nil && s.Model.MaxTokens > 0 && s.Model.MaxTokens < maxTokens {
		maxTokens = s.Model.MaxTokens
	}
	return maxTokens
}

// messagesAsLlm filters agent messages down to LLM roles (pi convertToLlm).
func messagesAsLlm(messages []agent.AgentMessage) []ai.Message {
	var llmMessages []ai.Message
	for _, m := range messages {
		switch m.MessageRole() {
		case ai.RoleUser, ai.RoleAssistant, ai.RoleToolResult:
			llmMessages = append(llmMessages, m)
		}
	}
	return llmMessages
}

// summarizationRequestModel returns the model a summarization request must
// actually be sent to (pi AgentSession._getSummarizationRequestAuth, upstream
// 18d65de62 / #6768).
//
// A provider can carry its endpoint in the credential rather than in the
// catalog: GitHub Copilot's Business/Enterprise base URL comes from the stored
// OAuth credential's toAuth(). The Models runtime normally rebuilds the request
// model from resolved auth, but a request that carries an explicit apiKey
// override short-circuits resolution to the api-key path (auth_resolve.go:
// "overrides?.apiKey !== undefined"), which never reaches the stored OAuth
// credential — so no base URL is resolved and no rebuild happens. Summarization
// always passes the session's key, so without this a Copilot compaction request
// goes to the Individual endpoint on a Business/Enterprise account.
//
// Resolution is best-effort, exactly like pi's: any failure falls back to the
// session's own model. Unlike pi, the port does not take the resolved apiKey or
// headers here — a coding Session is constructed with the caller's already
// resolved key and headers, where pi's AgentSession resolves them at this point.
func (s *Session) summarizationRequestModel(ctx context.Context) *ai.Model {
	if s.models == nil || s.Model == nil {
		return s.Model
	}
	result, err := s.models.GetAuth(ctx, s.Model, nil)
	if err != nil || result == nil || result.Auth.BaseURL == "" {
		return s.Model
	}
	requestModel := *s.Model
	requestModel.BaseURL = result.Auth.BaseURL
	return &requestModel
}

// summarizationFailed reports whether a summarization response is unsafe to
// persist as a checkpoint (pi getSummarizationFailure, compaction.ts, upstream
// 97fa14e39 / #7048). A "length" stop means generation hit the token cap, so the
// summary is truncated mid-thought and must not become a session checkpoint —
// only "error" used to fail. An abort is not a failure: pi keeps the text it
// produced.
//
// pi builds a "<label> failed: <reason>" message here, with a different label at
// each of its three call sites (Summarization, Turn prefix summarization, Branch
// summarization). The port has one summarization choke point and no channel for
// those strings — the same reason its tool-call guard below carries none — so it
// ports the predicate, which is the whole of the observable behavior.
func summarizationFailed(reason ai.StopReason) bool {
	return reason == ai.StopError || reason == ai.StopLength
}

// completeSummarization sends one summarization request (pi completeSummarization
// + createSummarizationOptions, compaction.ts:526-552): the session's API key,
// headers, and — when the model supports reasoning and the session's thinking
// level is set and not off — the thinking level are passed through. Returns
// ok=false where pi fails the summarization (see summarizationFailed); aborted
// responses return the text blocks produced so far, joined with "\n" (pi
// .map(c => c.text).join("\n")).
func (s *Session) completeSummarization(ctx context.Context, promptText string, maxTokens int, sessionID string) (string, bool) {
	summarizationMessages := []ai.Message{ai.NewUserText(promptText, nowMillisCoding())}

	// Avoid cache writes for one-off summaries (pi 9b3a2059): retention "none".
	// Reuse caller-supplied routing when available; callers without a session
	// ID, including branch summaries, receive a fresh routing ID (pi
	// `options.sessionId ?? uuidv7()`, upstream 58302d34e).
	if sessionID == "" {
		sessionID = uuidv7()
	}
	requestModel := s.summarizationRequestModel(ctx)
	opts := &ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{
		ProviderRequestOptions: ai.ProviderRequestOptions{
			APIKey:  s.apiKey,
			Headers: s.Agent.Headers,
		},
		MaxTokens:      &maxTokens,
		CacheRetention: ai.CacheNone,
		SessionID:      sessionID,
	}}
	// pi 6b36eb592 withdrew the `toolChoice: "none"` 90305d90a had forced here:
	// summarization leaves the option absent and lets the provider default apply.
	// A summary is still text, never a tool call — the guard below enforces that
	// on the response instead of on the request.
	//
	// pi ed867e909 withdrew getAnthropicSummarizationFallback: server-side refusal
	// fallback is no longer requested per call site. The Anthropic provider derives
	// it from the model's catalog compat, so a summarization request gets it on the
	// same terms as every other request.
	level := s.Agent.State().ThinkingLevel
	if s.Model != nil && s.Model.Reasoning && level != "" && level != agent.ThinkOff {
		opts.Reasoning = ai.ThinkingLevel(level)
	}

	streamFn := s.Agent.StreamFn
	if streamFn == nil {
		streamFn = ai.StreamSimple
	}
	stream := streamFn(ctx, requestModel, ai.Context{SystemPrompt: summarizationSystemPrompt, Messages: summarizationMessages}, opts)
	msg := stream.Result()
	if msg == nil || summarizationFailed(msg.StopReason) {
		return "", false
	}
	// A model that called a tool anyway did not produce a summary, so the
	// compaction fails rather than checkpointing whatever text came with it. pi
	// puts this guard in each of completeSummarization's callers, differing only
	// in the message it throws; the port has one place to put it and no channel
	// for those strings — a failed summarization surfaces as "keep the current
	// view" either way.
	var texts []string
	for _, c := range msg.Content {
		switch v := c.(type) {
		case ai.ToolCall:
			return "", false
		case ai.TextContent:
			texts = append(texts, v.Text)
		}
	}
	return strings.Join(texts, "\n"), true
}

// serializeConversation serializes LLM messages to text for summarization so the
// model treats it as content to summarize, not a conversation to continue (port
// of utils.ts serializeConversation). Tool results are truncated to
// toolResultMaxChars.
func serializeConversation(messages []ai.Message) string {
	var parts []string
	for _, m := range messages {
		switch msg := m.(type) {
		case ai.UserMessage:
			content := textOf(msg.Content)
			if content != "" {
				parts = append(parts, "[User]: "+content)
			}
		case ai.AssistantMessage:
			parts = append(parts, serializeAssistant(&msg)...)
		case *ai.AssistantMessage:
			parts = append(parts, serializeAssistant(msg)...)
		case ai.ToolResultMessage:
			content := textOf(msg.Content)
			if content != "" {
				parts = append(parts, "[Tool result]: "+truncateForSummary(content, toolResultMaxChars))
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// orderedArguments returns a tool call's arguments as key/value pairs in the
// order the model wrote them, falling back to sorted keys when no order was
// recorded (a tool call built in Go rather than decoded from a model).
func orderedArguments(tc ai.ToolCall) ai.OrderedObject {
	if ordered, ok := tc.OrderedArguments().(ai.OrderedObject); ok {
		return ordered
	}
	keys := make([]string, 0, len(tc.Arguments))
	for k := range tc.Arguments {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make(ai.OrderedObject, 0, len(keys))
	for _, k := range keys {
		fields = append(fields, ai.OrderedField{Key: k, Value: tc.Arguments[k]})
	}
	return fields
}

func serializeAssistant(a *ai.AssistantMessage) []string {
	var textParts, thinkingParts, toolCalls []string
	for _, c := range a.Content {
		switch b := c.(type) {
		case ai.TextContent:
			textParts = append(textParts, b.Text)
		case ai.ThinkingContent:
			thinkingParts = append(thinkingParts, b.Thinking)
		case ai.ToolCall:
			var entries []string
			// pi's Object.entries walks the arguments in the order the model
			// wrote them; Go map iteration is unordered, so follow the recorded
			// order and fall back to sorted keys when there is none.
			for _, f := range orderedArguments(b) {
				v, _ := json.Marshal(f.Value)
				entries = append(entries, f.Key+"="+string(v))
			}
			toolCalls = append(toolCalls, b.Name+"("+strings.Join(entries, ", ")+")")
		}
	}
	var parts []string
	if len(thinkingParts) > 0 {
		parts = append(parts, "[Assistant thinking]: "+strings.Join(thinkingParts, "\n"))
	}
	if len(textParts) > 0 {
		parts = append(parts, "[Assistant]: "+strings.Join(textParts, "\n"))
	}
	if len(toolCalls) > 0 {
		parts = append(parts, "[Assistant tool calls]: "+strings.Join(toolCalls, "; "))
	}
	return parts
}

func textOf(content ai.ContentList) string {
	var b strings.Builder
	for _, c := range content {
		if tc, ok := c.(ai.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// truncateForSummary truncates text to maxChars UTF-16 code units, appending a
// marker (utils.ts truncateForSummary; JS .length/.slice count UTF-16 units).
// Unlike JS slice, a surrogate pair on the boundary is dropped whole rather than
// split, so the output is always valid UTF-8.
func truncateForSummary(text string, maxChars int) string {
	length := utf16Len(text)
	if length <= maxChars {
		return text
	}
	return sliceUTF16(text, maxChars) + fmt.Sprintf("\n\n[... %d more characters truncated]", length-maxChars)
}

// sliceUTF16 returns the longest prefix of s holding at most n UTF-16 code
// units without splitting a rune (an astral rune counts as 2 units and is
// excluded entirely when it straddles the boundary).
func sliceUTF16(s string, n int) string {
	units := 0
	for i, r := range s {
		w := 1
		if r > 0xFFFF {
			w = 2
		}
		if units+w > n {
			return s[:i]
		}
		units += w
	}
	return s
}

// fileOps mirrors pi's FileOperations (utils.ts createFileOps).
type fileOps struct {
	read    map[string]bool
	written map[string]bool
	edited  map[string]bool
}

func newFileOps() *fileOps {
	return &fileOps{read: map[string]bool{}, written: map[string]bool{}, edited: map[string]bool{}}
}

// extractFileOpsFromMessage collects file paths from read/write/edit tool calls
// in an assistant message (port of utils.ts extractFileOpsFromMessage).
func extractFileOpsFromMessage(m agent.AgentMessage, ops *fileOps) {
	am, ok := messageAsAssistant(m)
	if !ok {
		return
	}
	for _, c := range am.Content {
		tc, ok := c.(ai.ToolCall)
		if !ok {
			continue
		}
		path, _ := tc.Arguments["path"].(string)
		if path == "" {
			continue
		}
		switch tc.Name {
		case "read":
			ops.read[path] = true
		case "write":
			ops.written[path] = true
		case "edit":
			ops.edited[path] = true
		}
	}
}

// lists computes the final sorted file lists (port of utils.ts computeFileLists):
// modified = edited + written; readFiles excludes any file that was also modified.
func (ops *fileOps) lists() (readFiles, modifiedFiles []string) {
	modified := map[string]bool{}
	for f := range ops.edited {
		modified[f] = true
	}
	for f := range ops.written {
		modified[f] = true
	}
	for f := range ops.read {
		if !modified[f] {
			readFiles = append(readFiles, f)
		}
	}
	for f := range modified {
		modifiedFiles = append(modifiedFiles, f)
	}
	sort.Strings(readFiles)
	sort.Strings(modifiedFiles)
	return readFiles, modifiedFiles
}

// computeFileLists derives the read-only and modified file lists from read/edit/
// write tool calls in the given messages.
func computeFileLists(messages []agent.AgentMessage) (readFiles, modifiedFiles []string) {
	ops := newFileOps()
	for _, m := range messages {
		extractFileOpsFromMessage(m, ops)
	}
	return ops.lists()
}

// formatFileOperations formats read/modified file lists as XML tags appended to
// the summary (port of utils.ts formatFileOperations).
func formatFileOperations(readFiles, modifiedFiles []string) string {
	var sections []string
	if len(readFiles) > 0 {
		sections = append(sections, "<read-files>\n"+strings.Join(readFiles, "\n")+"\n</read-files>")
	}
	if len(modifiedFiles) > 0 {
		sections = append(sections, "<modified-files>\n"+strings.Join(modifiedFiles, "\n")+"\n</modified-files>")
	}
	if len(sections) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(sections, "\n\n")
}
