package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sky-valley/pi/agent"
	"github.com/sky-valley/pi/ai"
	"github.com/sky-valley/pi/ai/providers"
	"github.com/sky-valley/pi/internal/jstext"
)

// testdata/compaction/capture-resume.mts writes session files with pi's own
// SessionManager, each holding one compaction entry, and records how pi extends
// that compaction: prepareCompaction reads the entry's summary as the previous
// summary, starts the cut search after it, and merges its details' file lists
// (unless an extension produced it); compact() then sends its requests, and the
// new compaction's context follows. Resumed as cmd/pi resumes a file
// (LoadSessionTree, BuildContext, LoadBranch), the port must send the same
// requests and rebuild the same context.
func TestResumedCompactionMatchesPiCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/compaction/resume-0.87.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		ReserveTokens  int `json:"reserveTokens"`
		ContextWindow  int `json:"contextWindow"`
		ModelMaxTokens int `json:"modelMaxTokens"`
		Scenarios      []struct {
			Name    string            `json:"name"`
			Entries []json.RawMessage `json:"entries"`
			Resumed []string          `json:"resumed"`
			// PreviousSummary is absent when pi's prepareCompaction found no
			// previous compaction.
			PreviousSummary *string `json:"previousSummary"`
			Requests        []struct {
				SystemPrompt string `json:"systemPrompt"`
				Text         string `json:"text"`
				MaxTokens    int    `json:"maxTokens"`
			} `json:"requests"`
			Summary   string   `json:"summary"`
			Compacted []string `json:"compacted"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	if len(capture.Scenarios) == 0 {
		t.Fatal("capture holds no scenarios")
	}
	for _, scenario := range capture.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			var file strings.Builder
			for _, entry := range scenario.Entries {
				line, err := json.Marshal(entry)
				if err != nil {
					t.Fatal(err)
				}
				file.Write(line)
				file.WriteByte('\n')
			}
			path := filepath.Join(t.TempDir(), "session.jsonl")
			if err := os.WriteFile(path, []byte(file.String()), 0o600); err != nil {
				t.Fatal(err)
			}
			tree, err := LoadSessionTree(path)
			if err != nil {
				t.Fatal(err)
			}
			branch := tree.BuildContext()
			if got := describeMessages(branch.Messages); !slices.Equal(got, scenario.Resumed) {
				t.Fatalf("resumed context drifts from pi:\n got %q\n  pi %q", got, scenario.Resumed)
			}

			reg := providers.RegisterFauxProvider(providers.RegisterFauxProviderOptions{
				Models: []providers.FauxModelDefinition{{ID: "faux-1", ContextWindow: capture.ContextWindow, MaxTokens: capture.ModelMaxTokens}},
			})
			t.Cleanup(reg.Unregister)
			type request struct {
				systemPrompt, text string
				maxTokens          int
			}
			var got []request
			record := func(req ai.TranscriptContext, opts *ai.SimpleStreamOptions, _ *providers.FauxState, _ *ai.Model) *ai.AssistantMessage {
				lead, _ := ai.GetInitialSystemMessage(req.Messages)
				r := request{systemPrompt: ai.GetSystemMessageText(lead), text: userText(ai.WithoutInitialSystemMessage(req.Messages)[0])}
				if opts != nil && opts.MaxTokens != nil {
					r.maxTokens = *opts.MaxTokens
				}
				got = append(got, r)
				return providers.FauxAssistantMessage(ai.ContentList{ai.TextContent{Text: fmt.Sprintf("SUMMARY %d", len(got))}}, ai.StopStop)
			}
			reg.SetResponses([]providers.FauxResponseStep{record, record, record})
			sess := NewSession(SessionOptions{Model: reg.GetModel(), Cwd: t.TempDir(), NoTools: NoToolsAll,
				Compaction: &CompactionSettings{Enabled: true, ReserveTokens: capture.ReserveTokens, KeepRecentTokens: 1}})
			sess.LoadBranch(branch)
			checkpoint := sess.compactState.checkpoint
			if (checkpoint != nil) != (scenario.PreviousSummary != nil) {
				t.Fatalf("resumed checkpoint %+v, but pi found a previous compaction: %v", checkpoint, scenario.PreviousSummary != nil)
			}
			// Until the next compaction, the checkpoint rebuilds the context pi
			// resumed.
			view := sess.History()
			if checkpoint != nil {
				view = checkpoint.apply(view)
			}
			if got := describeMessages(view); !slices.Equal(got, scenario.Resumed) {
				t.Fatalf("the resumed checkpoint rebuilds a context that drifts from pi:\n got %q\n  pi %q", got, scenario.Resumed)
			}

			out := sess.compact(context.Background(), sess.compactState, sess.History())
			if len(got) != len(scenario.Requests) {
				t.Fatalf("sent %d summarization requests, pi sent %d", len(got), len(scenario.Requests))
			}
			for i, want := range scenario.Requests {
				if got[i].systemPrompt != want.SystemPrompt {
					t.Errorf("request %d system prompt drifts from pi.\n--- got ---\n%s\n--- pi ---\n%s", i+1, got[i].systemPrompt, want.SystemPrompt)
				}
				if got[i].text != want.Text {
					t.Errorf("request %d text drifts from pi.\n--- got ---\n%s\n--- pi ---\n%s", i+1, got[i].text, want.Text)
				}
				if got[i].maxTokens != want.MaxTokens {
					t.Errorf("request %d maxTokens = %d, pi %d", i+1, got[i].maxTokens, want.MaxTokens)
				}
			}
			if checkpoint := checkpointOf(t, sess.compactState); checkpoint.summary != scenario.Summary {
				t.Errorf("summary drifts from pi.\n--- got ---\n%q\n--- pi ---\n%q", checkpoint.summary, scenario.Summary)
			}
			if got := describeMessages(out); !slices.Equal(got, scenario.Compacted) {
				t.Errorf("compacted context drifts from pi:\n got %q\n  pi %q", got, scenario.Compacted)
			}
		})
	}
}

// describeMessages renders each message as capture-resume.mts's describe does:
// its role, then its text; an assistant's tool calls follow as
// [name JSON.stringify(arguments)].
func describeMessages(messages []agent.AgentMessage) []string {
	out := make([]string, 0, len(messages))
	for _, m := range messages {
		switch v := m.(type) {
		case ai.SystemMessage:
			out = append(out, "system:"+ai.GetSystemMessageText(v))
		case ai.UserMessage:
			out = append(out, "user:"+textOf(v.Content))
		case ai.ToolResultMessage:
			out = append(out, "toolResult:"+textOf(v.Content))
		case ai.AssistantMessage:
			line := "assistant:" + textOf(v.Content)
			for _, c := range v.Content {
				if call, ok := c.(ai.ToolCall); ok {
					args, _ := jstext.Stringify(call.OrderedArguments())
					line += " [" + call.Name + " " + args + "]"
				}
			}
			out = append(out, line)
		default:
			out = append(out, fmt.Sprintf("%T", m))
		}
	}
	return out
}
