package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/sky-valley/pi/ai"
	"github.com/sky-valley/pi/internal/jstext"
)

const googleDefaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// googleToolCallCounter is a monotonic counter for synthesizing unique tool-call
// IDs (pi's module-level toolCallCounter). Guarded by googleToolCallCounterMu so
// concurrent streams don't collide.
var (
	googleToolCallCounter   int
	googleToolCallCounterMu sync.Mutex
)

func nextGoogleToolCallID(name string) string {
	googleToolCallCounterMu.Lock()
	googleToolCallCounter++
	c := googleToolCallCounter
	googleToolCallCounterMu.Unlock()
	return fmt.Sprintf("%s_%d_%d", name, nowMillis(), c)
}

// GoogleOptions are provider-native options for the Gemini stream.
type GoogleOptions struct {
	ai.StreamOptions
	// ThinkingProvided mirrors pi's optional `thinking` object being present at all.
	// When false, no thinkingConfig (enabled or disabled) is emitted.
	ThinkingProvided bool
	ThinkingEnabled  bool
	ThinkingBudget   *int   // -1 dynamic, 0 disable
	ThinkingLevel    string // level models (Gemini 3, Gemma 4): MINIMAL|LOW|MEDIUM|HIGH
	ToolChoice       string // auto|none|any
}

// StreamSimpleGoogle maps unified reasoning to GoogleOptions then streams.
func StreamSimpleGoogle(ctx context.Context, model *ai.Model, req ai.TranscriptContext, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	g := &GoogleOptions{}
	if opts != nil {
		g.StreamOptions = opts.StreamOptions
		g.ToolChoice = string(opts.ToolChoice)
	}
	// pi asserts the api key at the very top of streamSimple, ahead of the base
	// options and of everything StreamGoogle does (upstream 8b5899dce):
	//
	//	const apiKey = options?.apiKey;
	//	if (!apiKey) throw new Error(`No API key for provider: ${model.provider}`);
	//
	// A plain check, with none of the header escape hatches the anthropic and
	// openai adapters grant — do not unify them. Upstream raises it by throwing;
	// under G3 (ai/stream.go:90) the port renders that as the stream's single
	// terminal error event. What carries over is the PRECEDENCE: an eager throw
	// preempts every other setup failure on this path — the thinking-level
	// mapping below, and inside StreamGoogle the custom-fetch guard that
	// deliberately precedes its own in-stream key check. That in-stream ordering
	// is pi's for the full Stream path, which 8b5899dce did not touch, so it
	// stays exactly as it is.
	if g.APIKey == "" {
		return ai.ErrorStream(model, fmt.Errorf("No API key for provider: %s", model.Provider))
	}
	// pi buildBaseOptions: maxTokens = clamp(options?.maxTokens ?? model.maxTokens),
	// samplingParams = model defaults with the request's merged over them. Google
	// ignores samplingParams when building its body, exactly like pi.
	mt := ai.ClampMaxTokensToContext(model, req, ai.SimpleMaxTokensDefault(model, opts))
	g.MaxTokens = &mt
	g.SamplingParams = ai.MergeSamplingParams(model, opts)
	reasoning := ai.ThinkingLevel("")
	if opts != nil {
		reasoning = opts.Reasoning
	}
	if reasoning == "" {
		g.ThinkingProvided = true
		g.ThinkingEnabled = false
		return StreamGoogle(ctx, model, req, g)
	}
	clamped := ai.ClampThinkingLevel(model, ai.ModelThinkingLevel(reasoning))
	// The check above is pi's `!options?.reasoning`, which an explicit "off"
	// passes; a level that is, or clamps to, "off" disables thinking here
	// instead of reaching the level mapping (upstream 16235fd93, #9455).
	if clamped == "off" {
		g.ThinkingProvided = true
		g.ThinkingEnabled = false
		return StreamGoogle(ctx, model, req, g)
	}
	// pi resolves the clamped level through the model's thinkingLevelMap before
	// picking a level or a budget (upstream af2c35223). A level that resolves to
	// something outside Google's four standard levels is an error now, where it
	// used to fall through both tables into thinkingConfig:{includeThoughts:true}
	// with neither thinkingLevel nor thinkingBudget.
	effort, err := resolveGoogleThinkingLevel(model, ai.ThinkingLevel(clamped))
	if err != nil {
		return ai.ErrorStream(model, err)
	}
	g.ThinkingProvided = true
	g.ThinkingEnabled = true
	if usesGoogleThinkingLevel(model) {
		g.ThinkingLevel = toGoogleThinkingLevel(effort)
	} else {
		var custom *ai.ThinkingBudgets
		if opts != nil {
			custom = opts.ThinkingBudgets
		}
		g.ThinkingBudget = googleBudget(model.ID, effort, custom)
	}
	return StreamGoogle(ctx, model, req, g)
}

var (
	// Gemini 3 Pro/Flash ids with or without a minor version, such as
	// gemini-3-flash-preview, gemini-3.1-pro-preview and gemini-3.8-flash.
	gemini3LevelRe = regexp.MustCompile(`gemini-3(?:\.\d+)?-(?:pro|flash)`)
	// Both hosted Gemma 4 naming forms: gemma-4-* and gemma4-*.
	gemma4Re = regexp.MustCompile(`gemma-?4`)
)

// usesGoogleThinkingLevel reports whether the model uses Gemini's discrete
// thinkingLevel control instead of the token-based thinkingBudget control (pi
// google-shared.ts, upstream 16235fd93). It only selects the wire format: the
// levels the model supports come from its thinkingLevelMap.
func usesGoogleThinkingLevel(model *ai.Model) bool {
	id := strings.ToLower(model.ID)
	return gemini3LevelRe.MatchString(id) ||
		id == "gemini-flash-latest" ||
		id == "gemini-flash-lite-latest" ||
		gemma4Re.MatchString(id)
}

// toGoogleThinkingLevel maps a resolved level to Google's ThinkingLevel enum
// value (pi toGoogleThinkingLevel). resolveGoogleThinkingLevel yields only the
// four levels handled here; any other input returns "".
func toGoogleThinkingLevel(level string) string {
	switch level {
	case "minimal":
		return "MINIMAL"
	case "low":
		return "LOW"
	case "medium":
		return "MEDIUM"
	case "high":
		return "HIGH"
	}
	return ""
}

// getDisabledGoogleThinkingConfig ports pi getDisabledGoogleThinkingConfig
// (google-shared.ts, upstream 16235fd93): the thinkingConfig sent when thinking
// is disabled. Models on thinkingBudget, and level models whose
// thinkingLevelMap supports "off", disable with a zero budget. A level model
// that cannot turn thinking off (the catalog's Gemini 3 and Gemma 4 rows map
// "off" to null) gets its lowest supported level, resolved through the map,
// without includeThoughts so the hidden thinking stays invisible — and that
// resolution fails the request the same way an unmappable enabled level does.
func getDisabledGoogleThinkingConfig(model *ai.Model) (map[string]any, error) {
	if !usesGoogleThinkingLevel(model) {
		return map[string]any{"thinkingBudget": 0}, nil
	}
	fallback := ai.ClampThinkingLevel(model, "off")
	if fallback == "off" {
		return map[string]any{"thinkingBudget": 0}, nil
	}
	resolved, err := resolveGoogleThinkingLevel(model, ai.ThinkingLevel(fallback))
	if err != nil {
		return nil, err
	}
	return map[string]any{"thinkingLevel": toGoogleThinkingLevel(resolved)}, nil
}

// resolveGoogleThinkingLevel ports pi resolveGoogleThinkingLevel (google-shared.ts,
// upstream af2c35223, 16235fd93): map a supported pi level onto one of Google's
// four standard levels. Callers handle "off" before resolving. The model's
// thinkingLevelMap only participates when it holds a string for that level — pi
// guards with `typeof mapped === "string"`, so an explicit null (the map's
// "unsupported" marker) falls back to the level itself, exactly like an absent key.
//
// The error text is model-visible and byte-exact against pi's template, including
// JS `String(mapped)` rendering an absent key as "undefined" and a null entry as
// "null".
func resolveGoogleThinkingLevel(model *ai.Model, level ai.ThinkingLevel) (string, error) {
	mapped, present := model.ThinkingLevelMap[ai.ModelThinkingLevel(level)]
	resolved := string(level)
	if mapped != nil {
		resolved = strings.ToLower(*mapped)
	}
	switch resolved {
	case "minimal", "low", "medium", "high":
		return resolved, nil
	}
	rendered := "undefined"
	if present {
		if mapped == nil {
			rendered = "null"
		} else {
			rendered = *mapped
		}
	}
	return "", fmt.Errorf("Unsupported Google thinking level mapping for %s/%s: %s -> %s",
		model.Provider, model.ID, level, rendered)
}

// googleBudget mirrors pi getGoogleBudget (google.ts:463-503). A nil return means
// "no thinkingBudget" (pi's per-family tables have no xhigh key, so budgets[effort]
// is undefined); the final default of -1 applies to unmatched model families for
// ANY effort, xhigh included.
func googleBudget(id, effort string, custom *ai.ThinkingBudgets) *int {
	if custom != nil {
		if b := budgetForEffort(custom, effort); b != nil {
			return b
		}
	}
	switch {
	case strings.Contains(id, "2.5-pro"):
		return pick(effort, 128, 2048, 8192, 32768)
	case strings.Contains(id, "2.5-flash-lite"):
		return pick(effort, 512, 2048, 8192, 24576)
	case strings.Contains(id, "2.5-flash"):
		return pick(effort, 128, 2048, 8192, 24576)
	default:
		v := -1
		return &v
	}
}

func budgetForEffort(b *ai.ThinkingBudgets, effort string) *int {
	switch effort {
	case "minimal":
		return b.Minimal
	case "low":
		return b.Low
	case "medium":
		return b.Medium
	case "high":
		return b.High
	}
	return nil
}

func pick(effort string, minimal, low, medium, high int) *int {
	switch effort {
	case "minimal":
		return &minimal
	case "low":
		return &low
	case "medium":
		return &medium
	case "high":
		return &high
	}
	return nil
}

// requiresToolCallID reports whether a model reached via the Google APIs needs
// explicit tool call IDs in its function calls/responses (pi google-shared).
// Gemini 3 joined Claude and gpt-oss in cbaca6038.
func requiresToolCallID(modelID string) bool {
	if v, ok := getGeminiMajorVersion(modelID); ok && v >= 3 {
		return true
	}
	return strings.HasPrefix(modelID, "claude-") || strings.HasPrefix(modelID, "gpt-oss-")
}

// base64SignaturePattern matches the base64 alphabet pi requires for thought
// signatures (TYPE_BYTES). Signatures must also be a multiple of 4 in length.
var base64SignaturePattern = regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)

func isValidThoughtSignature(sig string) bool {
	if sig == "" {
		return false
	}
	if len(sig)%4 != 0 {
		return false
	}
	return base64SignaturePattern.MatchString(sig)
}

// resolveThoughtSignature only keeps a signature from the same provider/model with
// valid base64 (pi google-shared resolveThoughtSignature).
func resolveThoughtSignature(isSameProviderAndModel bool, sig string) string {
	if isSameProviderAndModel && isValidThoughtSignature(sig) {
		return sig
	}
	return ""
}

// getGeminiMajorVersion extracts the leading Gemini major version (pi google-shared).
func getGeminiMajorVersion(modelID string) (int, bool) {
	m := geminiMajorRe.FindStringSubmatch(strings.ToLower(modelID))
	if m == nil {
		return 0, false
	}
	n := 0
	for _, c := range m[1] {
		n = n*10 + int(c-'0')
	}
	return n, true
}

// supportsMultimodalFunctionResponse reports whether the model nests tool-result
// images inside functionResponse.parts (Gemini ≥ 3); others need a separate user
// image turn. Non-Gemini models default to true (pi google-shared).
func supportsMultimodalFunctionResponse(modelID string) bool {
	if v, ok := getGeminiMajorVersion(modelID); ok {
		return v >= 3
	}
	return true
}

var geminiMajorRe = regexp.MustCompile(`^gemini(?:-live)?-(\d+)`)

func modelSupportsImageInput(model *ai.Model) bool {
	for _, in := range model.Input {
		if in == "image" {
			return true
		}
	}
	return false
}

// StreamGoogle streams from the Gemini generateContent SSE endpoint. Gemini has
// no mid-conversation system messages, so the transcript is always collapsed:
// the replayed prompt is the systemInstruction.
func StreamGoogle(ctx context.Context, model *ai.Model, req ai.TranscriptContext, opts *GoogleOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	if opts == nil {
		opts = &GoogleOptions{}
	}
	normalized := ai.CollapseSystemMessages(req)

	go func() {
		output := &ai.AssistantMessage{
			Content: ai.ContentList{}, Api: ai.APIGoogleGenerativeAI, Provider: model.Provider, Model: model.ID,
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

		// pi guards `options.fetch && options.fetch !== globalThis.fetch`: the
		// @google/genai client cannot take a custom fetch, so only the default
		// one is accepted. http.DefaultClient is the Go stand-in for that
		// default. The check precedes the api-key check, as it does in pi.
		if _, custom := customHTTPClient(opts.HTTPClient); custom {
			fail(errors.New("Custom fetch is not supported by the Google Generative AI adapter"))
			return
		}

		if opts.APIKey == "" {
			fail(fmt.Errorf("No API key for provider: %s", model.Provider))
			return
		}

		body, err := buildGoogleParams(model, normalized, opts)
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
			if m, ok := next.(map[string]any); ok && m != nil {
				body = m
			}
		}
		payload, _ := json.Marshal(body)

		baseURL := model.BaseURL
		if baseURL == "" {
			baseURL = googleDefaultBaseURL
		}
		url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", strings.TrimRight(baseURL, "/"), model.ID)
		build := func() (*http.Request, error) {
			r, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
			if err != nil {
				return nil, err
			}
			r.Header.Set("content-type", "application/json")
			r.Header.Set("x-goog-api-key", opts.APIKey)
			// pi builds one merged object — mergeProviderAttributionHeaders puts
			// the attribution bundle at the bottom, then model.headers, then the
			// consumer's options.headers — and hands it to the SDK as
			// providerHeadersToRecord({"User-Agent": getPiUserAgent(),
			// ...model.headers, ...optionsHeaders}) (upstream 87af49dec added
			// the leading user agent; google sent none before).
			// Merging before converting is what lets a deletion marker cancel a
			// value an earlier source supplied; the conversion then drops the
			// markers rather than deleting, because this adapter builds the
			// request itself and cannot unset a header the SDK owns
			// (x-goog-api-key, content-type). That is also why a marker on the
			// user agent behaves differently here than on the SDK adapters: it
			// cancels pi's default only when it collides with it by name, and
			// never removes a header this adapter set literally.
			//
			// This is the one adapter where the merged object's slot order does
			// NOT fully decide the wire value: @google/genai builds its request
			// headers with Headers.append, so two case-variant names arrive
			// comma-joined there and as the last slot's value alone here. That
			// gap is recorded in docs/UPSTREAM.md; the slot order at least
			// picks the same winner pi puts last in the join.
			o := &headerObject{}
			o.merge(piUserAgentHeaders())
			o.mergeStrings(getSessionAttributionHeaders(model, opts.SessionID))
			o.mergeStrings(getDefaultAttributionHeaders(model))
			o.merge(model.Headers)
			o.merge(opts.Headers)
			o.applyAsRecord(r.Header)
			return r, nil
		}
		resp, err := sendWithRetry(ctx, build, retryFromOptions(opts.StreamOptions, nil))
		if err != nil {
			fail(err)
			return
		}
		defer resp.Body.Close()
		// No OnResponse: pi hands the request to the @google/genai client,
		// which owns the fetch, and its google adapter never calls onResponse.
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			data, _ := io.ReadAll(resp.Body)
			// Upstream 6fbeba51's google change is a no-op for the on-the-wire
			// HTTP-error case: the @google/genai SDK's ApiError already folds the
			// full body into error.message (JSON.stringify(errorBody)), so
			// normalizeProviderError sets messageCarriesBody=true and
			// formatProviderError returns the message unchanged (no status/body
			// reshaping, no truncation). Go reads the raw body here directly, so
			// no behavior delta to port; the existing structured-message shape is
			// retained. (Truncation lives in formatProviderError for parity.)
			fail(formatProviderError("Google", resp.StatusCode, data))
			return
		}

		stream.Push(ai.AssistantMessageEvent{Type: ai.EventStart, Partial: output.Clone()})

		var builders []*blockBuilder
		var current *blockBuilder // text or thinking
		// textSigs / toolCallSigs carry per-block thoughtSignatures that the shared
		// blockBuilder.toContent() does not model (text textSignature, toolCall
		// thoughtSignature), keyed by builder index.
		textSigs := map[int]string{}
		toolCallSigs := map[int]string{}
		materialize := func() {
			content := make(ai.ContentList, len(builders))
			for i, b := range builders {
				c := b.toContent()
				if sig, ok := textSigs[i]; ok && sig != "" {
					if tc, ok := c.(ai.TextContent); ok {
						tc.TextSignature = sig
						c = tc
					}
				}
				if sig, ok := toolCallSigs[i]; ok && sig != "" {
					if tc, ok := c.(ai.ToolCall); ok {
						tc.ThoughtSignature = sig
						c = tc
					}
				}
				content[i] = c
			}
			output.Content = content
		}
		endCurrent := func() {
			if current == nil {
				return
			}
			idx := len(builders) - 1
			if current.kind == "text" {
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventTextEnd, ContentIndex: idx, Content: current.text.String(), Partial: output.Clone()})
			} else {
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventThinkingEnd, ContentIndex: idx, Content: current.thinking.String(), Partial: output.Clone()})
			}
			current = nil
		}

		// pi: `await options?.onProviderStreamEvent?.(chunk, model)` first
		// thing for every chunk @google/genai yields (upstream 002fc8385), so
		// what it observes is the SDK's converted response, not the wire JSON.
		var observe func(data any) error
		if opts.OnProviderStreamEvent != nil {
			headers := googleSDKResponseHeaders(resp)
			observe = func(data any) error {
				// The SDK builds a fresh header record for every chunk.
				return opts.OnProviderStreamEvent(googleGenerateContentResponse(data, slices.Clone(headers)), model)
			}
		}
		err = iterateGoogleSSE(resp.Body, ctx, observe, func(chunk googleChunk) error {
			if chunk.ResponseID != "" && output.ResponseID == "" {
				output.ResponseID = chunk.ResponseID
			}
			if len(chunk.Candidates) > 0 {
				cand := chunk.Candidates[0]
				for _, part := range cand.Content.Parts {
					// pi runs INDEPENDENT checks (google.ts:97,158): `text !== undefined`
					// first, then `functionCall` — a part carrying both processes both;
					// a part with neither (signature-only, inlineData-only) produces
					// nothing at all.
					if part.Text != nil {
						text := *part.Text
						isThinking := part.Thought
						want := "text"
						if isThinking {
							want = "thinking"
						}
						if current == nil || current.kind != want {
							endCurrent()
							current = &blockBuilder{kind: want}
							builders = append(builders, current)
							idx := len(builders) - 1
							materialize()
							if isThinking {
								stream.Push(ai.AssistantMessageEvent{Type: ai.EventThinkingStart, ContentIndex: idx, Partial: output.Clone()})
							} else {
								stream.Push(ai.AssistantMessageEvent{Type: ai.EventTextStart, ContentIndex: idx, Partial: output.Clone()})
							}
						}
						idx := len(builders) - 1
						if isThinking {
							current.thinking.WriteString(text)
							if part.ThoughtSignature != "" {
								current.thinkingSig = part.ThoughtSignature
							}
							materialize()
							stream.Push(ai.AssistantMessageEvent{Type: ai.EventThinkingDelta, ContentIndex: idx, Delta: text, Partial: output.Clone()})
						} else {
							current.text.WriteString(text)
							// A thoughtSignature can appear on a text part (pi: textSignature via
							// retainThoughtSignature — keep last non-empty for the block).
							if part.ThoughtSignature != "" {
								textSigs[idx] = part.ThoughtSignature
							}
							materialize()
							stream.Push(ai.AssistantMessageEvent{Type: ai.EventTextDelta, ContentIndex: idx, Delta: text, Partial: output.Clone()})
						}
					}
					if part.FunctionCall != nil {
						endCurrent()
						// Regenerate the ID when it is empty OR a duplicate of one already
						// seen in this response (pi google.ts: needsNewId).
						providedID := part.FunctionCall.ID
						needsNewID := providedID == ""
						if !needsNewID {
							for _, b := range builders {
								if b.kind == "toolCall" && b.toolID == providedID {
									needsNewID = true
									break
								}
							}
						}
						id := providedID
						if needsNewID {
							id = nextGoogleToolCallID(part.FunctionCall.Name)
						}
						// pi: `arguments: part.functionCall.args ?? {}`, the parsed object
						// itself, so the model's key order rides along.
						args, order := part.FunctionCall.Args.values, part.FunctionCall.Args.order
						if args == nil {
							args = map[string]any{}
						}
						b := &blockBuilder{kind: "toolCall", toolID: id, toolName: part.FunctionCall.Name, args: args, argsOrder: order}
						builders = append(builders, b)
						idx := len(builders) - 1
						// pi sets thoughtSignature on the ToolCall object BEFORE pushing
						// toolcall_start (google.ts:186-195), so partials already carry it.
						if part.ThoughtSignature != "" {
							toolCallSigs[idx] = part.ThoughtSignature
						}
						materialize()
						stream.Push(ai.AssistantMessageEvent{Type: ai.EventToolCallStart, ContentIndex: idx, Partial: output.Clone()})
						tc := b.toContent().(ai.ToolCall)
						argsJSON, _ := jstext.Stringify(tc.OrderedArguments())
						stream.Push(ai.AssistantMessageEvent{Type: ai.EventToolCallDelta, ContentIndex: idx, Delta: argsJSON, Partial: output.Clone()})
						if part.ThoughtSignature != "" {
							tc.ThoughtSignature = part.ThoughtSignature
						}
						stream.Push(ai.AssistantMessageEvent{Type: ai.EventToolCallEnd, ContentIndex: idx, ToolCall: &tc, Partial: output.Clone()})
					}
				}
				if cand.FinishReason != "" {
					output.RawStopReason = cand.FinishReason
					reason, rerr := mapGoogleStopReason(cand.FinishReason)
					if rerr != nil {
						return rerr
					}
					output.StopReason = reason
					if reason == ai.StopStop {
						for _, b := range builders {
							if b.kind == "toolCall" {
								output.StopReason = ai.StopToolUse
								break
							}
						}
					}
				}
			}
			if chunk.UsageMetadata != nil {
				u := chunk.UsageMetadata
				output.Usage = ai.Usage{
					Input:       u.PromptTokenCount - u.CachedContentTokenCount,
					Output:      u.CandidatesTokenCount + u.ThoughtsTokenCount,
					CacheRead:   u.CachedContentTokenCount,
					CacheWrite:  0,
					Reasoning:   u.ThoughtsTokenCount,
					TotalTokens: u.TotalTokenCount,
				}
				ai.CalculateCost(model, &output.Usage)
			}
			return nil
		})

		if err != nil {
			// pi's throw leaves the for-await with the open block unclosed:
			// its catch pushes the error without a text_end/thinking_end.
			fail(err)
			return
		}
		endCurrent()
		if ctx != nil && ctx.Err() != nil {
			fail(fmt.Errorf("Request was aborted"))
			return
		}
		// pi (upstream f9a49869): a stream that ended without resolving the
		// pending stop reason fails with this exact message.
		if output.StopReason == ai.StopPending {
			fail(fmt.Errorf("Google stream ended without a finish reason"))
			return
		}
		// pi: a terminal error/aborted stopReason surfaces as a thrown error. Since
		// d7b02636 the provider's own finish reason is named when we captured one.
		// RawStopReason is assigned above under the same non-empty finishReason
		// guard that is the only route to an error/aborted stop reason, so the
		// "unknown error" fallback is unreachable today — but it is kept as
		// defensive parity with pi's ternary.
		if output.StopReason == ai.StopError || output.StopReason == ai.StopAborted {
			if output.RawStopReason != "" {
				fail(fmt.Errorf("%s%s", providerStoppedPrefix, output.RawStopReason))
			} else {
				fail(fmt.Errorf("An unknown error occurred"))
			}
			return
		}
		materialize()
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: output.StopReason, Message: output})
		stream.End()
	}()

	return stream
}

// buildGoogleParams builds the REST body the @google/genai SDK actually sends to
// generativelanguage.googleapis.com. The SDK lifts systemInstruction / tools /
// toolConfig to the top level (alongside contents) and keeps generation params
// (temperature, maxOutputTokens, thinkingConfig) under generationConfig.
func buildGoogleParams(model *ai.Model, req ai.TranscriptContext, opts *GoogleOptions) (map[string]any, error) {
	initialSystemMessage, hasInitialSystemMessage := ai.GetInitialSystemMessage(req.Messages)
	currentTools := ai.GetCurrentTools(req.Messages)
	params := map[string]any{
		"contents": googleContents(model, req),
	}

	// generationConfig holds only generation params (temperature, maxOutputTokens,
	// thinkingConfig). SDK: generateContentConfigToMldev writes these to toObject,
	// which becomes generationConfig.
	gen := map[string]any{}
	if opts.Temperature != nil {
		gen["temperature"] = *opts.Temperature
	}
	if opts.MaxTokens != nil {
		gen["maxOutputTokens"] = *opts.MaxTokens
	}

	// The genai SDK always sends generationConfig (unconditional setValueByPath),
	// even as an empty {} when no generation params are set.
	params["generationConfig"] = gen

	// systemInstruction / tools / toolConfig are lifted to the top level by the SDK.
	systemInstruction := ""
	if hasInitialSystemMessage {
		systemInstruction = ai.GetSystemMessageText(initialSystemMessage)
	}
	if systemInstruction != "" {
		params["systemInstruction"] = map[string]any{
			"role":  "user",
			"parts": []any{map[string]any{"text": sanitizeSurrogates(systemInstruction)}},
		}
	}
	if len(currentTools) > 0 {
		supportsStrictMode := supportsGoogleStrictToolSampling(model.ID)
		tools, err := googleTools(currentTools, useParameters(model.ID), supportsStrictMode)
		if err != nil {
			return nil, err
		}
		params["tools"] = tools
		mode, err := resolveGoogleFunctionCallingMode(currentTools, opts.ToolChoice, supportsStrictMode)
		if err != nil {
			return nil, err
		}
		if mode != "" {
			params["toolConfig"] = map[string]any{
				"functionCallingConfig": map[string]any{"mode": mode},
			}
		}
	}

	// thinkingConfig lives under generationConfig per the SDK. pi sets it after
	// building the tools, so a tool error wins over a disabled config whose
	// fallback level cannot be resolved.
	if model.Reasoning && opts.ThinkingProvided && opts.ThinkingEnabled {
		tc := map[string]any{"includeThoughts": true}
		if opts.ThinkingLevel != "" {
			tc["thinkingLevel"] = opts.ThinkingLevel
		} else if opts.ThinkingBudget != nil {
			tc["thinkingBudget"] = *opts.ThinkingBudget
		}
		gen["thinkingConfig"] = tc
	} else if model.Reasoning && opts.ThinkingProvided && !opts.ThinkingEnabled {
		tc, err := getDisabledGoogleThinkingConfig(model)
		if err != nil {
			return nil, err
		}
		gen["thinkingConfig"] = tc
	}
	return params, nil
}

// supportsGoogleStrictToolSampling reports whether the model enforces required
// function parameters in validated tool-calling modes — Gemini 3 and newer.
func supportsGoogleStrictToolSampling(modelID string) bool {
	major, ok := getGeminiMajorVersion(modelID)
	return ok && major >= 3
}

// resolveGoogleFunctionCallingMode picks the functionCallingConfig mode, or ""
// to omit toolConfig entirely (port of resolveGoogleFunctionCallingMode).
func resolveGoogleFunctionCallingMode(tools []ai.Tool, toolChoice string, supportsStrictMode bool) (string, error) {
	useStrictMode := false
	for _, tool := range tools {
		strict, err := resolveJSONSchemaStrictSampling(tool, supportsStrictMode)
		if err != nil {
			return "", err
		}
		if strict {
			// Matches pi's `tools.some(...)` short-circuit. (Unobservable either
			// way: resolve can only throw when supportsStrictMode is false, and
			// can only return true when it is true.)
			useStrictMode = true
			break
		}
	}
	if toolChoice == "none" || toolChoice == "any" {
		return mapToolChoice(toolChoice), nil
	}
	if useStrictMode {
		return "VALIDATED", nil
	}
	if toolChoice != "" {
		return mapToolChoice(toolChoice), nil
	}
	return "", nil
}

// mapToolChoice mirrors pi google-shared mapToolChoice (auto/none/any → upper).
func mapToolChoice(choice string) string {
	switch choice {
	case "auto":
		return "AUTO"
	case "none":
		return "NONE"
	case "any":
		return "ANY"
	default:
		return "AUTO"
	}
}

// useParameters selects the legacy OpenAPI `parameters` field (vs full-JSON-Schema
// `parametersJsonSchema`). pi's convertTools (google-shared.ts) defaults
// useParameters=false, and BOTH runtime callers — google-generative-ai.ts and
// google-vertex.ts — pass useParameters=false explicitly (upstream 7915cdac6).
// So the google-generative-ai / google-vertex providers ALWAYS emit
// `parametersJsonSchema`; the `parameters` branch is a library affordance for
// out-of-tree callers (Cloud Code Assist) that pi never exercises here. Returning
// false unconditionally pins pi's actual runtime field choice for all models,
// including Claude-via-Google.
func useParameters(modelID string) bool {
	return false
}

func googleContents(model *ai.Model, req ai.TranscriptContext) []any {
	// Gemini has no mid-conversation system messages; the leading prompt is sent
	// as systemInstruction.
	conversation := ai.WithoutInitialSystemMessage(ai.CollapseSystemMessages(req).Messages)
	normalizeID := func(id string) string {
		if !requiresToolCallID(model.ID) {
			return id
		}
		return normalizeToolCallID(id)
	}
	transformed := transformMessages(conversation, model, normalizeID)
	var contents []any
	for _, m := range transformed {
		if um, ok := asUserMsg(m); ok {
			var parts []any
			for _, c := range um.Content {
				switch v := c.(type) {
				case ai.TextContent:
					parts = append(parts, map[string]any{"text": sanitizeSurrogates(v.Text)})
				case ai.ImageContent:
					parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": v.MimeType, "data": v.Data}})
				}
			}
			if len(parts) == 0 {
				continue
			}
			contents = append(contents, map[string]any{"role": "user", "parts": parts})
		} else if am, ok := asAssistantMsg(m); ok {
			isSame := am.Provider == model.Provider && am.Model == model.ID
			var parts []any
			for _, c := range am.Content {
				switch v := c.(type) {
				case ai.TextContent:
					// thoughtSignature can ride on a text part for context replay
					// (pi: textSignature). Only keep same-model + valid base64.
					sig := resolveThoughtSignature(isSame, v.TextSignature)
					// Empty text is dropped only when unsigned: Gemini can attach the
					// signature to a part whose visible text is empty and requires it
					// echoed back, and dropping it breaks the reasoning chain — the model
					// then intermittently ends mid-task turns with a thought-only STOP
					// (empty completion, no tool call).
					if jstext.Trim(v.Text) == "" && sig == "" {
						continue
					}
					p := map[string]any{"text": sanitizeSurrogates(v.Text)}
					if sig != "" {
						p["thoughtSignature"] = sig
					}
					parts = append(parts, p)
				case ai.ThinkingContent:
					if isSame {
						sig := resolveThoughtSignature(isSame, v.ThinkingSignature)
						// Same rule as text parts: an empty thinking block is dropped only
						// when it carries no signature.
						if jstext.Trim(v.Thinking) == "" && sig == "" {
							continue
						}
						p := map[string]any{"thought": true, "text": sanitizeSurrogates(v.Thinking)}
						if sig != "" {
							p["thoughtSignature"] = sig
						}
						parts = append(parts, p)
					} else {
						// Unreachable: transformMessages (transform.go:122) already
						// rewrites cross-model thinking to text and drops the empty
						// ones before googleContents runs, and isSame here
						// (provider+model) is strictly weaker than transform's
						// isSameModel (provider+api+model), so isSame == false implies
						// no ThinkingContent survives to this branch. pi's converter has
						// the same dead branch behind the same ordering
						// (google-shared.ts:99); kept for shape parity.
						if jstext.Trim(v.Thinking) == "" {
							continue
						}
						parts = append(parts, map[string]any{"text": sanitizeSurrogates(v.Thinking)})
					}
				case ai.ToolCall:
					fc := map[string]any{"name": v.Name, "args": orEmptyArguments(v)}
					if requiresToolCallID(model.ID) {
						fc["id"] = v.ID
					}
					part := map[string]any{"functionCall": fc}
					if sig := resolveThoughtSignature(isSame, v.ThoughtSignature); sig != "" {
						part["thoughtSignature"] = sig
					}
					parts = append(parts, part)
				}
			}
			if len(parts) == 0 {
				continue
			}
			contents = append(contents, map[string]any{"role": "model", "parts": parts})
		} else if tr, ok := asToolResultMsg(m); ok {
			var texts []string
			var imageParts []any
			modelTakesImages := modelSupportsImageInput(model)
			for _, c := range tr.Content {
				switch cv := c.(type) {
				case ai.TextContent:
					texts = append(texts, cv.Text)
				case ai.ImageContent:
					if modelTakesImages {
						imageParts = append(imageParts, map[string]any{
							"inlineData": map[string]any{"mimeType": cv.MimeType, "data": cv.Data},
						})
					}
				}
			}
			textResult := strings.Join(texts, "\n")
			hasText := len(textResult) > 0
			hasImages := len(imageParts) > 0

			// responseValue: text if present, else placeholder for image-only, else "".
			responseValue := ""
			if hasText {
				responseValue = sanitizeSurrogates(textResult)
			} else if hasImages {
				responseValue = "(see attached image)"
			}

			respKey := "output"
			if tr.IsError {
				respKey = "error"
			}
			nested := supportsMultimodalFunctionResponse(model.ID)
			fr := map[string]any{"name": tr.ToolName, "response": map[string]any{respKey: responseValue}}
			// Gemini ≥ 3 nests images inside functionResponse.parts.
			if hasImages && nested {
				fr["parts"] = imageParts
			}
			if requiresToolCallID(model.ID) {
				fr["id"] = tr.ToolCallID
			}
			part := map[string]any{"functionResponse": fr}
			// Merge consecutive function responses into the last user turn
			// (Cloud Code Assist requires a single user turn).
			merged := false
			if n := len(contents); n > 0 {
				if last, ok := contents[n-1].(map[string]any); ok && last["role"] == "user" {
					if parts, ok := last["parts"].([]any); ok && hasFunctionResponse(parts) {
						last["parts"] = append(parts, part)
						merged = true
					}
				}
			}
			if !merged {
				contents = append(contents, map[string]any{"role": "user", "parts": []any{part}})
			}

			// Gemini < 3 / Claude-via-Google: images go in a separate user turn.
			if hasImages && !nested {
				imgTurnParts := append([]any{map[string]any{"text": "Tool result image:"}}, imageParts...)
				contents = append(contents, map[string]any{"role": "user", "parts": imgTurnParts})
			}
		}
	}
	return contents
}

func hasFunctionResponse(parts []any) bool {
	for _, p := range parts {
		if m, ok := p.(map[string]any); ok {
			if _, has := m["functionResponse"]; has {
				return true
			}
		}
	}
	return false
}

// jsonSchemaMetaDeclarations mirrors pi google-shared JSON_SCHEMA_META_DECLARATIONS.
var jsonSchemaMetaDeclarations = map[string]bool{
	"$schema":        true,
	"$id":            true,
	"$anchor":        true,
	"$dynamicAnchor": true,
	"$vocabulary":    true,
	"$comment":       true,
	"$defs":          true,
	"definitions":    true, // pre-draft-2019-09 equivalent of $defs
}

// sanitizeForOpenApi recursively strips JSON Schema meta-declarations from a schema
// so it can be sent as an OpenAPI 3.0.3 schema (pi google-shared sanitizeForOpenApi).
func sanitizeForOpenApi(schema any) any {
	switch v := schema.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, value := range v {
			if jsonSchemaMetaDeclarations[key] {
				continue
			}
			result[key] = sanitizeForOpenApi(value)
		}
		return result
	default:
		// Arrays and scalars pass through unchanged. pi only recurses into plain
		// objects; arrays are returned as-is (Array.isArray short-circuit).
		return schema
	}
}

// schemaToGeneric marshals a Schema to its JSON-Schema map form so it can be
// sanitized key-by-key like pi (which operates on plain objects).
func schemaToGeneric(s *ai.Schema) any {
	raw, err := json.Marshal(s)
	if err != nil {
		return map[string]any{}
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

// googleTools converts tools to Gemini functionDeclarations. When useParameters is
// true it emits the legacy OpenAPI `parameters` field (sanitized of meta-keys),
// otherwise the full-JSON-Schema `parametersJsonSchema`; either way the schema
// sent is the strict conversion for tools that resolved strict. (pi convertTools.)
func googleTools(tools []ai.Tool, useParameters, supportsStrictMode bool) ([]any, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	var decls []any
	for _, t := range tools {
		strict, err := resolveJSONSchemaStrictSampling(t, supportsStrictMode)
		if err != nil {
			return nil, err
		}
		parameters, err := jsonSchemaToolParameters(t, strict)
		if err != nil {
			return nil, err
		}
		decl := map[string]any{"name": t.Name, "description": t.Description}
		if useParameters {
			if parameters != nil {
				decl["parameters"] = sanitizeForOpenApi(schemaToGeneric(parameters))
			}
		} else if parameters != nil {
			decl["parametersJsonSchema"] = parameters
		}
		decls = append(decls, decl)
	}
	return []any{map[string]any{"functionDeclarations": decls}}, nil
}

// mapGoogleStopReason maps a Gemini FinishReason to our StopReason, returning a
// non-nil error only for a truly-unknown reason (pi mapStopReason throws via the
// exhaustive-never check). Known safety/recitation/malformed reasons map to error
// without throwing — pi surfaces them as "Provider stopped with: <reason>"
// (d7b02636).
func mapGoogleStopReason(reason string) (ai.StopReason, error) {
	switch reason {
	case "STOP":
		return ai.StopStop, nil
	case "MAX_TOKENS":
		return ai.StopLength, nil
	case "BLOCKLIST",
		"PROHIBITED_CONTENT",
		"SPII",
		"SAFETY",
		"IMAGE_SAFETY",
		"IMAGE_PROHIBITED_CONTENT",
		"IMAGE_RECITATION",
		"IMAGE_OTHER",
		"RECITATION",
		"FINISH_REASON_UNSPECIFIED",
		"OTHER",
		"LANGUAGE",
		"MALFORMED_FUNCTION_CALL",
		"UNEXPECTED_TOOL_CALL",
		"TOO_MANY_TOOL_CALLS",
		"NO_IMAGE":
		return ai.StopError, nil
	default:
		return ai.StopError, fmt.Errorf("Unhandled stop reason: %s", reason)
	}
}

// ---- SSE chunk types ----

type googleChunk struct {
	ResponseID string `json:"responseId"`
	Candidates []struct {
		Content struct {
			Parts []googlePart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount        int `json:"promptTokenCount"`
		CandidatesTokenCount    int `json:"candidatesTokenCount"`
		CachedContentTokenCount int `json:"cachedContentTokenCount"`
		ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
		TotalTokenCount         int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

type googlePart struct {
	// Text is a pointer so presence ("" included) is distinguishable from
	// absence, mirroring pi's `part.text !== undefined` check (google.ts:97).
	Text             *string `json:"text"`
	Thought          bool    `json:"thought"`
	ThoughtSignature string  `json:"thoughtSignature"`
	FunctionCall     *struct {
		ID   string             `json:"id"`
		Name string             `json:"name"`
		Args googleFunctionArgs `json:"args"`
	} `json:"functionCall"`
}

// googleFunctionArgs is a functionCall's args with the model's key order kept:
// pi's tool-call arguments are the parsed object itself, which JSON.stringify
// (the toolcall_delta) and every later replay write in that order. A value
// that is not an object fails the chunk's decode, as a map field did.
type googleFunctionArgs struct {
	values map[string]any
	order  ai.OrderedObject
}

func (a *googleFunctionArgs) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*a = googleFunctionArgs{}
		return nil
	}
	values, order, err := ai.DecodeOrderedObject(data)
	if err != nil {
		return err
	}
	*a = googleFunctionArgs{values: values, order: order}
	return nil
}

// googleBareJSONError is the check @google/genai 2.21.0 runs on every network
// read before buffering it (processStreamResponse): when the whole read
// parses as JSON holding an "error" key, it reads status and code from
// JSON.parse(JSON.stringify(chunkJson.error)), computes
//
//	`got status: ${status}. ${JSON.stringify(chunkJson)}`
//
// and throws that as an ApiError when code >= 400 && code < 600, with
// JavaScript's coercions ("500" and [429] qualify; an absent status reads
// "undefined"; a number past float64's range is null by then). Anything else
// thrown inside the check — a property read on a primitive or null, a value
// with no primitive form — is swallowed by the SDK's catch, which rethrows
// only ApiErrors; so is a read that is not JSON.
func googleBareJSONError(read string) error {
	parsed, err := jstext.Parse([]byte(read))
	if err != nil {
		return nil
	}
	chunk, ok := parsed.(map[string]any)
	if !ok {
		return nil // `'error' in` a primitive throws; an array has no "error"
	}
	e, ok := chunk["error"]
	if !ok {
		return nil
	}
	fields, ok := jstext.Reparse(e).(map[string]any)
	if !ok {
		return nil // null throws reading .status; any other value reads undefined twice
	}
	status := "undefined"
	if v, present := fields["status"]; present {
		if status, ok = jstext.ToString(v); !ok {
			return nil
		}
	}
	code, present := fields["code"]
	if !present {
		return nil
	}
	if n, ok := jstext.ToNumber(code); !ok || !(n >= 400 && n < 600) {
		return nil
	}
	// JSON.stringify(chunkJson): the parsed object's own-property order
	// (array-index keys first), a repeated key once, JavaScript's number
	// spelling and Infinity as null.
	text, _ := jsStringify([]byte(read))
	return fmt.Errorf("got status: %s. %s", status, text)
}

// iterateGoogleSSE consumes the alt=sse stream the way the @google/genai SDK
// does (processStreamResponse): each network read is first checked whole for
// a bare JSON error payload (googleBareJSONError), then buffered; events are
// split on \n\n, \r\r, or \r\n\r\n; only "data:"-prefixed events are decoded;
// and a trailing unconsumed segment fails with the SDK's "Incomplete JSON
// segment at the end".
//
// observe, when set, receives each data: payload's parsed value before handle
// sees the chunk, and its error ends the stream.
func iterateGoogleSSE(body io.Reader, ctx context.Context, observe func(data any) error, handle func(googleChunk) error) error {
	delimiters := []string{"\n\n", "\r\r", "\r\n\r\n"}
	buf := make([]byte, 32*1024)
	var pending string

	processEvent := func(event string) error {
		trimmed := jstext.Trim(event)
		if !strings.HasPrefix(trimmed, "data:") {
			return nil
		}
		data := jstext.Trim(strings.TrimPrefix(trimmed, "data:"))
		if data == "" {
			return nil
		}
		var chunk googleChunk
		decodeErr := parseJSONWithRepair(data, &chunk)
		if observe != nil {
			if value, ok := googleObservedValue(data); ok {
				if err := observe(value); err != nil {
					return err
				}
			}
		}
		if decodeErr != nil {
			return nil
		}
		// No error check here: the SDK checks only whole reads that are bare
		// JSON, and generateContentResponseFromMldev keeps no "error" field, so
		// a data: event carrying one reaches pi as an empty chunk.
		return handle(chunk)
	}

	// An abort rejects the SDK's pending body read with undici's AbortError.
	// (pi's own "Request was aborted" is thrown only once the stream has run
	// out, which StreamGoogle checks after the loop.)
	abortErr := func() error {
		if ctx != nil && ctx.Err() != nil {
			return errors.New("This operation was aborted")
		}
		return nil
	}
	for {
		if err := abortErr(); err != nil {
			return err
		}
		n, readErr := body.Read(buf)
		if n > 0 {
			read := string(buf[:n])
			if err := googleBareJSONError(read); err != nil {
				return err
			}
			pending += read
			for {
				// Earliest delimiter wins (SDK keeps the smallest index).
				delimIdx, delimLen := -1, 0
				for _, d := range delimiters {
					if i := strings.Index(pending, d); i != -1 && (delimIdx == -1 || i < delimIdx) {
						delimIdx, delimLen = i, len(d)
					}
				}
				if delimIdx == -1 {
					break
				}
				event := pending[:delimIdx]
				pending = pending[delimIdx+delimLen:]
				if err := processEvent(event); err != nil {
					return err
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			if err := abortErr(); err != nil {
				return err
			}
			return readErr
		}
	}

	if jstext.Trim(pending) != "" {
		return fmt.Errorf("Incomplete JSON segment at the end")
	}
	return nil
}

// googleObservedValue parses a data: payload for the stream-event observer,
// keeping every object's key order. It reads the payload the way the typed
// decode does — as is, else after the repair pass — so the observer sees each
// event the adapter goes on to handle. (pi's SDK parses strictly and fails
// the stream on a payload that needs repair or cannot be parsed; the port's
// leniency there predates the observer.)
func googleObservedValue(data string) (any, bool) {
	if value, err := ai.DecodeOrderedValue([]byte(data)); err == nil {
		return value, true
	}
	if repaired := repairJSON(data); repaired != data {
		if value, err := ai.DecodeOrderedValue([]byte(repaired)); err == nil {
			return value, true
		}
	}
	return nil, false
}

// googleGenerateContentResponse is the chunk @google/genai 2.21.0 yields for
// one parsed data: payload, which pi hands to onProviderStreamEvent:
// generateContentResponseFromMldev(payload) (index.cjs:10916) with
// sdkHttpResponse = {headers} set on the result (generateContentStreamInternal,
// ~15850). The converter copies only the fields it knows, in its own order,
// and only when they are not null or undefined (`!= null`); everything else
// the payload carries — unknown fields, "error" — never reaches pi. A payload
// that is not an object (null, a scalar, an array) yields no fields at all.
func googleGenerateContentResponse(payload any, headers ai.OrderedObject) ai.OrderedObject {
	src, _ := payload.(ai.OrderedObject)
	out := ai.OrderedObject{}
	// The converter copies a payload's own sdkHttpResponse first; the SDK's
	// record then replaces its value, keeping that first slot.
	if googleField(src, "sdkHttpResponse") != nil {
		out = append(out, ai.OrderedField{Key: "sdkHttpResponse"})
	}
	if v := googleField(src, "candidates"); v != nil {
		if list, ok := v.([]any); ok {
			converted := make([]any, len(list))
			for i, candidate := range list {
				converted[i] = googleCandidateFromMldev(candidate)
			}
			v = converted
		}
		out = append(out, ai.OrderedField{Key: "candidates", Value: v})
	}
	for _, key := range []string{"modelVersion", "promptFeedback", "responseId", "usageMetadata", "modelStatus"} {
		if v := googleField(src, key); v != nil {
			out = append(out, ai.OrderedField{Key: key, Value: v})
		}
	}
	record := ai.OrderedObject{{Key: "headers", Value: headers}}
	if len(out) > 0 && out[0].Key == "sdkHttpResponse" {
		out[0].Value = record
	} else {
		out = append(out, ai.OrderedField{Key: "sdkHttpResponse", Value: record})
	}
	return out
}

// googleCandidateFromMldev is @google/genai's candidateFromMldev
// (index.cjs:9635): the known candidate fields in its order, citationMetadata
// through citationMetadataFromMldev. A candidate that is not an object
// converts to {}.
func googleCandidateFromMldev(candidate any) ai.OrderedObject {
	src, _ := candidate.(ai.OrderedObject)
	out := ai.OrderedObject{}
	for _, key := range []string{
		"content", "citationMetadata", "tokenCount", "finishReason", "groundingMetadata",
		"avgLogprobs", "index", "logprobsResult", "safetyRatings", "urlContextMetadata",
	} {
		v := googleField(src, key)
		if v == nil {
			continue
		}
		if key == "citationMetadata" {
			v = googleCitationMetadataFromMldev(v)
		}
		out = append(out, ai.OrderedField{Key: key, Value: v})
	}
	return out
}

// googleCitationMetadataFromMldev is @google/genai's citationMetadataFromMldev:
// citationSources renamed to citations, every other field dropped.
func googleCitationMetadataFromMldev(metadata any) ai.OrderedObject {
	src, _ := metadata.(ai.OrderedObject)
	if v := googleField(src, "citationSources"); v != nil {
		return ai.OrderedObject{{Key: "citations", Value: v}}
	}
	return ai.OrderedObject{}
}

// googleField is @google/genai's getValueByPath for one key of a parsed
// object: nil when the key is absent or null.
func googleField(o ai.OrderedObject, key string) any {
	for _, f := range o {
		if f.Key == key {
			return f.Value
		}
	}
	return nil
}

// googleSDKResponseHeaders is the header record @google/genai 2.21.0 puts on
// every chunk as sdkHttpResponse.headers. processStreamResponse wraps each
// data: payload in `new Response(payload, {headers: response.headers})`, and
// HttpResponse copies that Response's headers.entries() into a plain object:
//
//   - names are lowercase, in sorted order (the Fetch Headers iterator sorts);
//   - a repeated header's values are joined with ", ", except set-cookie,
//     whose values iterate one by one so the last one wins;
//   - values are the header bytes read as latin1, as undici decodes them;
//   - a response without content-type gets the "text/plain;charset=UTF-8" a
//     string-bodied Response adds.
//
// Go's transport moves Transfer-Encoding out of the header map and, when it
// transparently gunzips, drops Content-Encoding; undici keeps both, so they
// are put back.
func googleSDKResponseHeaders(resp *http.Response) ai.OrderedObject {
	values := map[string][]string{}
	for _, name := range slices.Sorted(maps.Keys(resp.Header)) {
		key := strings.ToLower(name)
		values[key] = append(values[key], resp.Header[name]...)
	}
	if _, ok := values["transfer-encoding"]; !ok && len(resp.TransferEncoding) > 0 {
		values["transfer-encoding"] = []string{strings.Join(resp.TransferEncoding, ", ")}
	}
	if _, ok := values["content-encoding"]; !ok && resp.Uncompressed {
		values["content-encoding"] = []string{"gzip"}
	}
	if _, ok := values["content-type"]; !ok {
		values["content-type"] = []string{"text/plain;charset=UTF-8"}
	}
	names := slices.Sorted(maps.Keys(values))
	out := make(ai.OrderedObject, 0, len(names))
	for _, name := range names {
		vs := values[name]
		value := strings.Join(vs, ", ")
		if name == "set-cookie" {
			value = vs[len(vs)-1]
		}
		out = append(out, ai.OrderedField{Key: name, Value: latin1(value)})
	}
	return out
}

// latin1 reads each byte of s as the character with that code point, the way
// undici decodes header bytes.
func latin1(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			var b strings.Builder
			b.Grow(len(s) * 2)
			for j := 0; j < len(s); j++ {
				b.WriteRune(rune(s[j]))
			}
			return b.String()
		}
	}
	return s
}

// RegisterGoogle registers the google-generative-ai api provider.
func RegisterGoogle() {
	ai.RegisterApiProvider(ai.ApiProvider{
		Api: ai.APIGoogleGenerativeAI,
		Stream: func(ctx context.Context, model *ai.Model, req ai.TranscriptContext, opts *ai.StreamOptions) *ai.AssistantMessageEventStream {
			g := &GoogleOptions{}
			if opts != nil {
				g.StreamOptions = *opts
			}
			return StreamGoogle(ctx, model, req, g)
		},
		StreamSimple: StreamSimpleGoogle,
	})
}
