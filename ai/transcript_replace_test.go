package ai

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// A system message with Replace discards the replayed prompt, sections and
// tools before it applies, and a replacement after the leading message makes
// every provider collapse (upstream e4c75a732). The expected values are pi's:
// testdata/transcript/capture-replace.mts runs the fixtures below under node at
// e4c75a732.

const replaceCaptureFile = "testdata/transcript/replace-e4c75a732.json"

type replaceReplay struct {
	Messages     []json.RawMessage `json:"messages"`
	Current      json.RawMessage   `json:"current"`
	Prompt       string            `json:"prompt"`
	Tools        []string          `json:"tools"`
	NativeIsSame bool              `json:"nativeIsSame"`
	Native       json.RawMessage   `json:"native"`
	NonNative    json.RawMessage   `json:"nonNative"`
}

type replaceCapture struct {
	Sha      string `json:"sha"`
	Upstream struct {
		Current                json.RawMessage `json:"current"`
		ResolvedNative         json.RawMessage `json:"resolvedNative"`
		Collapsed              json.RawMessage `json:"collapsed"`
		TranscriptNativeIsSame bool            `json:"transcriptNativeIsSame"`
		LeadingNativeIsSame    bool            `json:"leadingNativeIsSame"`
	} `json:"upstream"`
	Replays   map[string]replaceReplay `json:"replays"`
	Producers map[string]string        `json:"producers"`
}

func loadReplaceCapture(t *testing.T) replaceCapture {
	t.Helper()
	data, err := os.ReadFile(replaceCaptureFile)
	if err != nil {
		t.Fatal(err)
	}
	var capture replaceCapture
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatalf("%s: %v", replaceCaptureFile, err)
	}
	if len(capture.Replays) == 0 || len(capture.Producers) == 0 {
		t.Fatalf("%s holds no cases; rerun capture-replace.mts", replaceCaptureFile)
	}
	return capture
}

// compactJSON is captured JSON without the capture's indentation.
func compactJSON(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// sameContext reports whether got is context itself, as `===` does: the same
// messages, not a rebuilt list.
func sameContext(got, context TranscriptContext) bool {
	return len(got.Messages) == len(context.Messages) &&
		(len(context.Messages) == 0 || &got.Messages[0] == &context.Messages[0])
}

// systemMessagesJSON is each system message's bytes and every message's role:
// the suite's non-system fixtures carry no Go-complete JSON (an assistant
// message without api, provider or usage), so only their roles compare.
func systemMessagesJSON(t *testing.T, messages []Message) string {
	t.Helper()
	var parts []string
	for _, message := range messages {
		if message.MessageRole() == RoleSystem {
			parts = append(parts, jsJSON(t, message))
		} else {
			parts = append(parts, string(message.MessageRole()))
		}
	}
	return strings.Join(parts, "\n")
}

func systemMessagesCaptureJSON(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var context struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(raw, &context); err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, message := range context.Messages {
		var head struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(message, &head); err != nil {
			t.Fatal(err)
		}
		if head.Role == "system" {
			parts = append(parts, compactJSON(t, message))
		} else {
			parts = append(parts, head.Role)
		}
	}
	return strings.Join(parts, "\n")
}

// system-message-replay.test.ts at e4c75a732, 'a replacement discards replayed
// state and collapses even for native providers'.
func TestAReplacementDiscardsReplayedStateAndCollapsesEvenForNativeProviders(t *testing.T) {
	capture := loadReplaceCapture(t).Upstream
	transcript := replayTranscript()
	replacement := withSystemText("forced", 15, func(m *SystemMessage) {
		m.ToolsAdded = []Tool{replayTool("third")}
		m.Replace = true
	})
	patch := withSystemText("", 16, func(m *SystemMessage) {
		m.Sections = SystemSections{{Name: "d", Value: strp("<d>1</d>")}}
	})
	context := TranscriptContext{Messages: slices.Concat(transcript.Messages, []Message{replacement, patch})}

	current, ok := GetCurrentSystemMessage(context.Messages)
	if !ok {
		t.Fatal("GetCurrentSystemMessage reported no system message")
	}
	if got, want := jsJSON(t, current), compactJSON(t, capture.Current); got != want {
		t.Fatalf("current system message\n got %s\nwant %s", got, want)
	}
	resolved := ResolveTranscript(context, true)
	if got, want := systemMessagesJSON(t, resolved.Messages), systemMessagesCaptureJSON(t, capture.ResolvedNative); got != want {
		t.Fatalf("resolveTranscript(context, true)\n got %s\nwant %s", got, want)
	}
	if got, want := systemMessagesJSON(t, resolved.Messages), systemMessagesJSON(t, CollapseSystemMessages(context).Messages); got != want {
		t.Fatalf("resolveTranscript(context, true) is not the collapsed transcript\n got %s\nwant %s", got, want)
	}
	if got, want := systemMessagesJSON(t, resolved.Messages), systemMessagesCaptureJSON(t, capture.Collapsed); got != want {
		t.Fatalf("collapsed transcript\n got %s\nwant %s", got, want)
	}
	if got := sameContext(ResolveTranscript(transcript, true), transcript); got != capture.TranscriptNativeIsSame {
		t.Fatalf("resolveTranscript(transcript, true) === transcript: %v, pi %v", got, capture.TranscriptNativeIsSame)
	}
	leading := TranscriptContext{Messages: []Message{replacement, patch}}
	if got := sameContext(ResolveTranscript(leading, true), leading); got != capture.LeadingNativeIsSame {
		t.Fatalf("resolveTranscript(leading, true) === leading: %v, pi %v", got, capture.LeadingNativeIsSame)
	}
}

// Replay across replacements: tool deltas on and after a replacement, sections
// and content on the new baseline, the first message's timestamp, empty and
// repeated replacements, `replace: false`, and which position counts as
// leading.
func TestReplacementReplayMatchesPi(t *testing.T) {
	for name, replay := range loadReplaceCapture(t).Replays {
		t.Run(name, func(t *testing.T) {
			var messages []Message
			for i, raw := range replay.Messages {
				message, err := UnmarshalMessage(raw)
				if err != nil {
					t.Fatalf("messages[%d]: %v", i, err)
				}
				messages = append(messages, message)
			}
			context := TranscriptContext{Messages: messages}

			current, ok := GetCurrentSystemMessage(messages)
			if want := compactJSON(t, replay.Current); !ok || jsJSON(t, current) != want {
				t.Fatalf("current system message (found %v)\n got %s\nwant %s", ok, jsJSON(t, current), want)
			}
			if got := GetCurrentSystemPrompt(messages); got != replay.Prompt {
				t.Fatalf("GetCurrentSystemPrompt = %q, want %q", got, replay.Prompt)
			}
			var tools []string
			for _, tool := range GetCurrentTools(messages) {
				tools = append(tools, tool.Name)
			}
			if !slices.Equal(tools, replay.Tools) {
				t.Fatalf("GetCurrentTools = %v, want %v", tools, replay.Tools)
			}
			native := ResolveTranscript(context, true)
			if got := sameContext(native, context); got != replay.NativeIsSame {
				t.Fatalf("resolveTranscript(context, true) === context: %v, pi %v", got, replay.NativeIsSame)
			}
			if got, want := jsJSON(t, native), compactJSON(t, replay.Native); got != want {
				t.Fatalf("resolveTranscript(context, true)\n got %s\nwant %s", got, want)
			}
			if got, want := jsJSON(t, ResolveTranscript(context, false)), compactJSON(t, replay.NonNative); got != want {
				t.Fatalf("resolveTranscript(context, false)\n got %s\nwant %s", got, want)
			}
		})
	}
}

// The replace key sits where each producer puts it: after content and
// sections and before timestamp in _preparePromptAndToolLoadout's literal,
// wherever the document had it when decoded; and a decoded false survives.
func TestSystemMessageReplaceKeyOrderMatchesPi(t *testing.T) {
	producers := loadReplaceCapture(t).Producers

	forced := NewSystemText("Exact prompt.", 5)
	forced.Replace = true
	if got, want := jsJSON(t, forced), producers["replaceForced"]; got != want {
		t.Fatalf("forced replacement\n got %s\nwant %s", got, want)
	}
	sections := SystemMessage{
		Sections:  SystemSections{{Name: "preamble", Value: strp("p")}, {Name: "cwd", Value: strp("<cwd>\n/x\n</cwd>")}},
		Replace:   true,
		Timestamp: 5,
	}
	if got, want := jsJSON(t, sections), producers["replaceSections"]; got != want {
		t.Fatalf("sections replacement\n got %s\nwant %s", got, want)
	}
	for _, name := range []string{"decodedMisplaced", "decodedFalse"} {
		message, err := UnmarshalMessage([]byte(producers[name]))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got, want := jsJSON(t, message), producers[name]; got != want {
			t.Fatalf("%s round trip\n got %s\nwant %s", name, got, want)
		}
	}
	misplaced, _ := UnmarshalMessage([]byte(producers["decodedMisplaced"]))
	if system, ok := misplaced.(SystemMessage); !ok || !system.Replace {
		t.Fatalf("decoded replace = %#v, want Replace true", misplaced)
	}
}

// Replace must be a JSON boolean; anything else is refused with what to write.
func TestSystemMessageReplaceMustBeABoolean(t *testing.T) {
	_, err := UnmarshalMessage([]byte(`{"role":"system","content":"x","replace":"yes","timestamp":1}`))
	if err == nil || !strings.Contains(err.Error(), "replace") || !strings.Contains(err.Error(), "true or false") {
		t.Fatalf("error = %v, want a replace error naming true or false", err)
	}
}
