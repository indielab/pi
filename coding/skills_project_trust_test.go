package coding

import (
	"path/filepath"
	"strings"
	"testing"
)

// pi discovers <cwd>/.pi/skills only inside `if (projectTrusted)`
// (package-manager.ts:2417 at ccfe79ed2), and resolveProjectTrusted answers
// false when the host has no UI to prompt with (project-trust.ts:
// `if (!options.projectTrustContext.hasUI) return false`). This port is
// headless, so untrusted is the faithful default. These tests pin BOTH arms:
// the gate closes on the project dir, and it never touches the user dirs.

func TestLoadSkillsUntrustedSkipsProjectDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()

	writeSkill(t, filepath.Join(home, ".pi", "agent", "skills", "user-skill"),
		"---\nname: user-skill\ndescription: from the user directory\n---\n")
	writeSkill(t, filepath.Join(cwd, ".pi", "skills", "repo-skill"),
		"---\nname: repo-skill\ndescription: authored by whoever owns this repo\n---\n")

	untrusted, _ := LoadSkillsWithTrust(cwd, false)
	if len(untrusted) != 1 || untrusted[0].Name != "user-skill" {
		t.Fatalf("untrusted load must see the user skill and only the user skill, got %+v", untrusted)
	}

	// LoadSkills and LoadSkillsWithDiagnostics are the same default.
	if got := LoadSkills(cwd); len(got) != 1 || got[0].Name != "user-skill" {
		t.Fatalf("LoadSkills must default to untrusted, got %+v", got)
	}
	if got, _ := LoadSkillsWithDiagnostics(cwd); len(got) != 1 || got[0].Name != "user-skill" {
		t.Fatalf("LoadSkillsWithDiagnostics must default to untrusted, got %+v", got)
	}

	trusted, _ := LoadSkillsWithTrust(cwd, true)
	if len(trusted) != 2 {
		t.Fatalf("trusted load must add the project skill, got %+v", trusted)
	}
	// Order is pi's: user dir first, then project.
	if trusted[0].Name != "user-skill" || trusted[1].Name != "repo-skill" {
		t.Fatalf("trusted load must keep user-then-project order, got %s, %s", trusted[0].Name, trusted[1].Name)
	}
}

// The exposure this gate closes is the SYSTEM PROMPT: a skill's name and
// description are rendered into it by FormatSkillsForPrompt together with an
// instruction to read the file when the task matches. An untrusted repo must
// not be able to put its own text there.
func TestSystemPromptOmitsUntrustedProjectSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()

	const marker = "ZZ-ATTACKER-CONTROLLED-DESCRIPTION"
	writeSkill(t, filepath.Join(cwd, ".pi", "skills", "repo-skill"),
		"---\nname: repo-skill\ndescription: "+marker+"\n---\n")

	build := func(trust bool) string {
		return BuildSystemPrompt(BuildSystemPromptOptions{
			SelectedTools: defaultActiveToolNames,
			ToolSnippets:  ToolSnippets,
			Cwd:           cwd,
			Skills:        sessionSkills(cwd, trust),
		})
	}

	if got := build(false); strings.Contains(got, marker) {
		t.Fatalf("untrusted project skill reached the system prompt")
	}
	if got := build(true); !strings.Contains(got, marker) {
		t.Fatalf("trusted project skill must still reach the system prompt")
	}
}
