package coding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sky-valley/pi/ai/providers"
)

// TestNewSessionCollectsPromptGuidelines locks NewSession → BuildSystemPrompt
// wiring for I1: the resolved tools' PromptGuidelines must appear in the
// Guidelines section of the agent's system prompt, in tool order.
func TestNewSessionCollectsPromptGuidelines(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reg := providers.RegisterFauxProvider(providers.RegisterFauxProviderOptions{})
	defer reg.Unregister()
	s := NewSession(SessionOptions{Model: reg.GetModel(), Cwd: t.TempDir()})
	prompt := s.Agent.State().SystemPrompt

	wantOrder := []string{
		"- Use bash for file operations like ls, rg, find",
		"- Use read to examine files instead of cat or sed.",
		"- Use edit for precise changes (edits[].oldText must match exactly)",
		"- Use write only for new files or complete rewrites.",
		"- Be concise in your responses",
	}
	last := -1
	for _, g := range wantOrder {
		idx := strings.Index(prompt, g)
		if idx == -1 {
			t.Fatalf("guideline missing from system prompt: %q\n%s", g, prompt)
		}
		if idx < last {
			t.Fatalf("guideline out of order: %q\n%s", g, prompt)
		}
		last = idx
	}
}

// A session whose only file-reading tool is bash still gets its skills, with
// the block telling the model to load them with bash (upstream 1d6dbf9e3).
// This is the wiring half: NewSession → resolveTools → BuildSystemPrompt.
func TestNewSessionBashOnlyKeepsSkills(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	skillDir := filepath.Join(cwd, ".pi", "skills", "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo-skill\ndescription: Demo skill for tests\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := providers.RegisterFauxProvider(providers.RegisterFauxProviderOptions{})
	defer reg.Unregister()
	s := NewSession(SessionOptions{
		Model:        reg.GetModel(),
		Cwd:          cwd,
		ToolNames:    []string{"bash"},
		TrustProject: true,
	})
	prompt := s.Agent.State().SystemPrompt

	if !strings.Contains(prompt, "<name>demo-skill</name>") {
		t.Fatalf("bash-only session dropped its skills:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Use bash to load a skill's file when the task matches its description.") {
		t.Fatalf("bash-only session should name bash as the skill file reader:\n%s", prompt)
	}
	if strings.Contains(prompt, "Use the read tool to load a skill's file") {
		t.Fatalf("bash-only session must not name the absent read tool:\n%s", prompt)
	}
}

// TestNewSessionCustomPromptStillAssembles locks I2: a custom SystemPrompt
// still gets project context files, skills, date, and cwd appended (pi
// system-prompt.ts:53-80), in pi's order — and never the docs block or the
// default Guidelines section.
func TestNewSessionCustomPromptStillAssembles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "AGENTS.md"), []byte("follow the rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(cwd, ".pi", "skills", "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo-skill\ndescription: Demo skill for tests\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := providers.RegisterFauxProvider(providers.RegisterFauxProviderOptions{})
	defer reg.Unregister()
	s := NewSession(SessionOptions{
		Model:        reg.GetModel(),
		Cwd:          cwd,
		SystemPrompt: "You are a custom agent.",
		// The fixture skill lives in <cwd>/.pi/skills, which is gated on
		// project trust. This test is about assembly ORDER, not the gate —
		// see TestSystemPromptOmitsUntrustedProjectSkill for that.
		TrustProject: true,
	})
	prompt := s.Agent.State().SystemPrompt

	if !strings.HasPrefix(prompt, "You are a custom agent.") {
		t.Fatalf("custom prompt should lead the system prompt:\n%s", prompt)
	}
	// pi's order: custom prompt, project context, skills, cwd.
	ordered := []string{
		"You are a custom agent.",
		"<project_context>",
		"<project_instructions path=\"" + filepath.Join(cwd, "AGENTS.md") + "\">\nfollow the rules\n</project_instructions>",
		"<available_skills>",
		"<name>demo-skill</name>",
		"\nCurrent working directory: ",
	}
	last := -1
	for _, sub := range ordered {
		idx := strings.Index(prompt, sub)
		if idx == -1 {
			t.Fatalf("custom prompt assembly missing %q:\n%s", sub, prompt)
		}
		if idx < last {
			t.Fatalf("custom prompt assembly out of order at %q:\n%s", sub, prompt)
		}
		last = idx
	}
	// Custom prompts get neither the docs block nor the Guidelines section.
	for _, banned := range []string{"Pi documentation", "Guidelines:", "expert coding assistant"} {
		if strings.Contains(prompt, banned) {
			t.Fatalf("custom prompt must not include %q:\n%s", banned, prompt)
		}
	}
}
