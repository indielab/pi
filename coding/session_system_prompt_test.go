package coding

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/sky-valley/pi/agent"
	"github.com/sky-valley/pi/ai"
	"github.com/sky-valley/pi/ai/providers"
)

// The session declares its prompt in the transcript (upstream 9e05370b2): pi's
// sdk.ts builds the Agent with no prompt and no tools, and prompt() puts a
// sections patch ahead of the user message; the agent loop attaches the tool
// declarations to that pending system message. Both reach the provider and the
// session file like any other message.

func messageRoles(messages []agent.AgentMessage) []string {
	out := make([]string, 0, len(messages))
	for _, m := range messages {
		out = append(out, string(m.MessageRole()))
	}
	return out
}

func sectionNames(sections ai.SystemSections) []string {
	names := []string{}
	for _, section := range sections.Entries() {
		names = append(names, section.Name)
	}
	return names
}

// expectedSessionPrompt is the prompt NewSession must declare for its default
// tools in cwd, built independently of the Session.
func expectedSessionPrompt(t *testing.T, cwd string) string {
	t.Helper()
	tools := resolveTools(cwd, SessionOptions{}, nil)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return mustBuildSystemPrompt(t, BuildSystemPromptOptions{
		SelectedTools:  names,
		ToolSnippets:   ToolSnippets,
		ToolGuidelines: toolPromptGuidelines(tools),
		Cwd:            cwd,
		ContextFiles:   LoadProjectContextFiles(cwd),
		Skills:         sessionSkills(cwd, false),
	})
}

func fauxText(text string) providers.FauxResponseStep {
	return providers.FauxStatic(providers.FauxAssistantMessage(ai.ContentList{ai.TextContent{Text: text}}, ai.StopStop))
}

// system-prompt-updates.test.ts 'declares the prompt and tools once and reuses
// them across resume'.
func TestSessionDeclaresPromptAndToolsOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reg := providers.RegisterFauxProvider(providers.RegisterFauxProviderOptions{})
	defer reg.Unregister()
	cwd := t.TempDir()
	sess := NewSession(SessionOptions{Model: reg.GetModel(), Cwd: cwd})

	reg.SetResponses([]providers.FauxResponseStep{fauxText("first"), fauxText("second")})
	if _, err := sess.Run(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Run(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}

	history := sess.History()
	if got, want := messageRoles(history), []string{"system", "user", "assistant", "user", "assistant"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	head, ok := history[0].(ai.SystemMessage)
	if !ok {
		t.Fatalf("head is %T", history[0])
	}
	if content, isString := head.StringContent(); !isString || content != "" {
		t.Fatalf("head content = %q (string form %v), want \"\"", content, isString)
	}
	if got, want := sectionNames(head.Sections), []string{"preamble", "tools", "rules", "docs", "cwd"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("head sections = %v, want %v", got, want)
	}
	if got, want := toolNames(head.ToolsAdded), []string{"read", "bash", "edit", "write"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("head toolsAdded = %v, want %v", got, want)
	}
	if got, want := ai.GetSystemMessageText(head), expectedSessionPrompt(t, cwd); got != want {
		t.Fatalf("declared prompt drift:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if got, want := ai.GetSystemMessageText(head), sessionSystemPrompt(t, sess); got != want {
		t.Fatalf("declared prompt differs from SystemPrompt():\n--- declared ---\n%s\n--- SystemPrompt ---\n%s", got, want)
	}
}

// system-prompt-updates.test.ts 'keeps tool declarations stable across a
// session JSON round-trip': a resumed transcript replays to the same prompt and
// tools, so the next Run declares nothing.
func TestSessionResumedTranscriptDeclaresNothingNew(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reg := providers.RegisterFauxProvider(providers.RegisterFauxProviderOptions{})
	defer reg.Unregister()
	cwd := t.TempDir()
	sess := NewSession(SessionOptions{Model: reg.GetModel(), Cwd: cwd})
	reg.SetResponses([]providers.FauxResponseStep{fauxText("first"), fauxText("second")})
	if _, err := sess.Run(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}

	resumed := NewSession(SessionOptions{Model: reg.GetModel(), Cwd: cwd})
	var decoded []agent.AgentMessage
	for _, m := range sess.History() {
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		message, err := ai.UnmarshalMessage(raw)
		if err != nil {
			t.Fatal(err)
		}
		decoded = append(decoded, message)
	}
	resumed.LoadHistory(decoded)
	if _, err := resumed.Run(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}
	if got, want := messageRoles(resumed.History()), []string{"system", "user", "assistant", "user", "assistant"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("roles = %v, want %v (nothing re-declared)", got, want)
	}
}

// A transcript declared under another tool loadout, resumed into this session,
// gets exactly the difference: the sections that changed and the tool removals.
func TestSessionResumedTranscriptDeclaresOnlyTheDifference(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reg := providers.RegisterFauxProvider(providers.RegisterFauxProviderOptions{})
	defer reg.Unregister()
	cwd := t.TempDir()
	declaring := NewSession(SessionOptions{Model: reg.GetModel(), Cwd: cwd})
	reg.SetResponses([]providers.FauxResponseStep{fauxText("first"), fauxText("second")})
	if _, err := declaring.Run(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}

	resumed := NewSession(SessionOptions{Model: reg.GetModel(), Cwd: cwd, NoTools: NoToolsAll})
	resumed.LoadHistory(declaring.History())
	result, err := resumed.Run(context.Background(), "two")
	if err != nil {
		t.Fatal(err)
	}
	patch, ok := result.Messages[0].(ai.SystemMessage)
	if !ok {
		t.Fatalf("the run must start with a declaration, got roles %v", messageRoles(result.Messages))
	}
	if got, want := sectionNames(patch.Sections), []string{"tools", "rules"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("patched sections = %v, want %v", got, want)
	}
	var removed []string
	for _, tool := range patch.ToolsRemoved {
		removed = append(removed, tool.Name)
	}
	if want := []string{"read", "bash", "edit", "write"}; !reflect.DeepEqual(removed, want) || patch.ToolsAdded != nil {
		t.Fatalf("tool changes = removed %v added %v, want removed %v", removed, patch.ToolsAdded, want)
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := jsonKeys(t, raw), []string{"role", "content", "sections", "timestamp", "toolsRemoved"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("patch keys = %v, want %v", got, want)
	}
}

// system-prompt-updates.test.ts 'opens a transcript without a system message
// and declares the prompt on the first request': a transcript written before
// transcript-owned prompts (every Go session file until now) gets nothing
// synthesized on load, then the whole declaration — sections and tools — with
// the next prompt.
func TestSessionDeclaresPromptForTranscriptWithoutSystemMessage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reg := providers.RegisterFauxProvider(providers.RegisterFauxProviderOptions{})
	defer reg.Unregister()
	cwd := t.TempDir()
	sess := NewSession(SessionOptions{Model: reg.GetModel(), Cwd: cwd})
	sess.LoadHistory([]agent.AgentMessage{ai.NewUserText("existing", 1)})
	if got := messageRoles(sess.History()); !reflect.DeepEqual(got, []string{"user"}) {
		t.Fatalf("roles after load = %v, want [user]", got)
	}
	if _, ok := ai.GetCurrentSystemMessage(sess.History()); ok {
		t.Fatal("loading a transcript must not synthesize a system message")
	}

	var request ai.TranscriptContext
	reg.SetResponses([]providers.FauxResponseStep{
		func(req ai.TranscriptContext, _ *ai.SimpleStreamOptions, _ *providers.FauxState, _ *ai.Model) *ai.AssistantMessage {
			request = req
			return providers.FauxAssistantMessage(ai.ContentList{ai.TextContent{Text: "ok"}}, ai.StopStop)
		},
	})
	if _, err := sess.Run(context.Background(), "next"); err != nil {
		t.Fatal(err)
	}
	history := sess.History()
	if got, want := messageRoles(history), []string{"user", "system", "user", "assistant"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	declaration := history[1].(ai.SystemMessage)
	if got, want := sectionNames(declaration.Sections), []string{"preamble", "tools", "rules", "docs", "cwd"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("declared sections = %v, want %v", got, want)
	}
	if got, want := toolNames(declaration.ToolsAdded), []string{"read", "bash", "edit", "write"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("declared tools = %v, want %v", got, want)
	}
	if got, want := ai.GetCurrentSystemPrompt(request.Messages), expectedSessionPrompt(t, cwd); got != want {
		t.Fatalf("request prompt drift:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// RunPrint prompts through the same declaration as Run.
func TestSessionRunPrintDeclaresPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reg := providers.RegisterFauxProvider(providers.RegisterFauxProviderOptions{})
	defer reg.Unregister()
	sess := NewSession(SessionOptions{Model: reg.GetModel(), Cwd: t.TempDir()})
	reg.SetResponses([]providers.FauxResponseStep{fauxText("ok")})
	if _, err := sess.RunPrint(context.Background(), &bytes.Buffer{}, "hi"); err != nil {
		t.Fatal(err)
	}
	head, ok := sess.History()[0].(ai.SystemMessage)
	if !ok || head.Sections.Len() == 0 {
		t.Fatalf("RunPrint must declare the sectioned prompt, history roles %v", messageRoles(sess.History()))
	}
}

// The recorder persists the declared system message like any other finished
// message (pi _handleAgentEvent's role === "system" arm): role "system", the
// sections in prompt order, an integer timestamp, and the tool declarations the
// loop attached after it. Reloading the file replays the same prompt text.
func TestSessionRecordsDeclaredSystemMessage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reg := providers.RegisterFauxProvider(providers.RegisterFauxProviderOptions{})
	defer reg.Unregister()
	cwd := t.TempDir()
	sess := NewSession(SessionOptions{Model: reg.GetModel(), Cwd: cwd})
	rec, err := StartSession(cwd, reg.GetModel(), "off")
	if err != nil {
		t.Fatal(err)
	}
	sess.Record(rec)
	reg.SetResponses([]providers.FauxResponseStep{fauxText("ok")})
	if _, err := sess.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	rec.Close()

	file, err := os.Open(rec.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var messageRolesInFile []string
	var system json.RawMessage
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for scanner.Scan() {
		var entry struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatal(err)
		}
		if entry.Type != "message" {
			continue
		}
		var head struct {
			Role string `json:"role"`
		}
		_ = json.Unmarshal(entry.Message, &head)
		messageRolesInFile = append(messageRolesInFile, head.Role)
		if head.Role == "system" {
			system = entry.Message
		}
	}
	if got, want := messageRolesInFile, []string{"system", "user", "assistant"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded message roles = %v, want %v", got, want)
	}
	if got, want := jsonKeys(t, system), []string{"role", "content", "sections", "timestamp", "toolsAdded"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded system message keys = %v, want %v", got, want)
	}
	var fields struct {
		Sections  json.RawMessage `json:"sections"`
		Timestamp json.RawMessage `json:"timestamp"`
	}
	if err := json.Unmarshal(system, &fields); err != nil {
		t.Fatal(err)
	}
	if got, want := jsonKeys(t, fields.Sections), []string{"preamble", "tools", "rules", "docs", "cwd"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded sections = %v, want %v", got, want)
	}
	if ts := string(fields.Timestamp); ts == "" || strings.ContainsAny(ts, ".eE\"") {
		t.Fatalf("recorded timestamp = %s, want an integer", ts)
	}

	messages, err := LoadSessionMessages(rec.Path())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ai.GetCurrentSystemPrompt(messages), expectedSessionPrompt(t, cwd); got != want {
		t.Fatalf("reloaded prompt drift:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// jsonKeys returns an object's keys in document order.
func jsonKeys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("not an object: %s", raw)
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}
