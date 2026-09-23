package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sky-valley/pi/ai"
	"github.com/sky-valley/pi/ai/providers"
	"github.com/sky-valley/pi/coding"
)

// Resuming a session file resumes its compaction: the next compaction extends
// the file's with the update prompt, its summary as <previous-summary>, as pi's
// does. A resume that loaded only the messages would summarize the old summary
// as a [User] turn under the initial prompt. The file is one pi 0.87.1 wrote
// (coding/testdata/compaction/capture-resume.mts).
func TestResumeExtendsTheSessionsCompaction(t *testing.T) {
	data, err := os.ReadFile("../../coding/testdata/compaction/resume-0.87.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Scenarios []struct {
			Name    string            `json:"name"`
			Entries []json.RawMessage `json:"entries"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	var file strings.Builder
	for _, scenario := range capture.Scenarios {
		if scenario.Name != "previous-summary-with-files" {
			continue
		}
		for _, entry := range scenario.Entries {
			line, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			file.Write(line)
			file.WriteByte('\n')
		}
	}
	if file.Len() == 0 {
		t.Fatal("the capture holds no previous-summary-with-files scenario")
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(file.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	tree, err := coding.LoadSessionTree(path)
	if err != nil {
		t.Fatal(err)
	}

	reg := providers.RegisterFauxProvider(providers.RegisterFauxProviderOptions{
		Models: []providers.FauxModelDefinition{{ID: "faux-1", ContextWindow: 2000, MaxTokens: 8192}},
	})
	t.Cleanup(reg.Unregister)
	var summarizations []string
	step := func(req ai.TranscriptContext, _ *ai.SimpleStreamOptions, _ *providers.FauxState, _ *ai.Model) *ai.AssistantMessage {
		for _, m := range req.Messages {
			if u, ok := m.(ai.UserMessage); ok {
				if text := ai.ContentText(u.Content); strings.HasPrefix(text, "<conversation>\n") {
					summarizations = append(summarizations, text)
					return providers.FauxAssistantMessage(ai.ContentList{ai.TextContent{Text: "SUMMARY"}}, ai.StopStop)
				}
			}
		}
		return providers.FauxAssistantMessage(ai.ContentList{ai.TextContent{Text: "reply"}}, ai.StopStop)
	}
	reg.SetResponses([]providers.FauxResponseStep{step, step, step})
	// A reserve as large as the window compacts on every request.
	sess := coding.NewSession(coding.SessionOptions{Model: reg.GetModel(), Cwd: t.TempDir(), SystemPrompt: "test", NoTools: coding.NoToolsAll,
		Compaction: &coding.CompactionSettings{Enabled: true, ReserveTokens: 2000, KeepRecentTokens: 1}})
	if rec := resume(sess, path, tree.BuildProjection(), true); rec != nil {
		t.Cleanup(func() { rec.Close() })
	}
	if _, err := sess.Run(context.Background(), "q5"); err != nil {
		t.Fatal(err)
	}
	if len(summarizations) == 0 {
		t.Fatal("the resumed session never compacted")
	}
	const previous = "<previous-summary>\n## Goal\nold work\n\n<read-files>\n/a/old.go\n</read-files>\n\n<modified-files>\n/a/changed.go\n</modified-files>\n</previous-summary>"
	if !strings.Contains(summarizations[0], previous) {
		t.Fatalf("the first compaction after resuming does not extend the file's.\n--- request ---\n%s", summarizations[0])
	}
}
