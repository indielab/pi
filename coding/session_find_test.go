package coding

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeSessionFile writes lines to dir/name as a JSONL session file, creating
// dir, and returns the path.
func writeSessionFile(t *testing.T, dir, name string, lines ...string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func sessionHeaderLine(id, cwd string) string {
	return fmt.Sprintf(`{"type":"session","version":3,"id":%q,"timestamp":"2026-09-16T00:00:00.000Z","cwd":%q}`, id, cwd)
}

const sessionMessageLine = `{"type":"message","id":"m1","parentId":null,"timestamp":"2026-09-16T00:00:01.000Z","message":{"role":"user","content":"hello","timestamp":1}}`

// pi: "reopens an exact ID from a renamed session file" — the lookup keys on
// the header id, not the file name pi's recorder would have chosen.
func TestFindSessionByIDMatchesTheHeaderNotTheFileName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	dir := DefaultSessionDir(cwd)
	want := writeSessionFile(t, dir, "imported-session.jsonl", sessionHeaderLine("renamed-id", cwd), sessionMessageLine)
	writeSessionFile(t, dir, "other.jsonl", sessionHeaderLine("other-id", cwd), sessionMessageLine)

	got, ok := FindSessionByID(cwd, "renamed-id", "")
	if !ok || got != want {
		t.Fatalf("FindSessionByID = %q, %v; want %q, true", got, ok, want)
	}
	if got, ok := FindSessionByID(cwd, "missing-id", ""); ok {
		t.Fatalf("FindSessionByID found %q for an id no header carries", got)
	}
}

// pi: "looks up exact IDs without building full session listings" — only the
// first parsed entry decides a file. A later session-typed line is transcript
// content pi never reads, and a file whose first entry is not a header is not
// a session even if a header appears further down.
func TestFindSessionByIDReadsOnlyTheFirstEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	dir := DefaultSessionDir(cwd)
	first := writeSessionFile(t, dir, "a.jsonl",
		sessionHeaderLine("first", cwd), sessionMessageLine, sessionHeaderLine("second", cwd))
	writeSessionFile(t, dir, "b.jsonl", sessionMessageLine, sessionHeaderLine("late", cwd))
	skipped := writeSessionFile(t, dir, "c.jsonl", "", "   ", "not json", `null`, `0`, `""`, `false`, sessionHeaderLine("after-noise", cwd))
	writeSessionFile(t, dir, "d.jsonl", `"a string entry"`, sessionHeaderLine("after-scalar", cwd))

	if got, ok := FindSessionByID(cwd, "first", ""); !ok || got != first {
		t.Fatalf("header id: got %q, %v; want %q, true", got, ok, first)
	}
	if got, ok := FindSessionByID(cwd, "second", ""); ok {
		t.Fatalf("a session-typed line after the header must not be read, found %q", got)
	}
	if got, ok := FindSessionByID(cwd, "late", ""); ok {
		t.Fatalf("a file whose first entry is not a header is not a session, found %q", got)
	}
	if got, ok := FindSessionByID(cwd, "after-noise", ""); !ok || got != skipped {
		t.Fatalf("blank, malformed and falsy lines must be skipped: got %q, %v; want %q, true", got, ok, skipped)
	}
	if got, ok := FindSessionByID(cwd, "after-scalar", ""); ok {
		t.Fatalf("a truthy non-object first entry decides the file is not a session, found %q", got)
	}
}

// pi bounds header discovery at MAX_SESSION_HEADER_SCAN_BYTES: a header that
// ends exactly at the limit is still found (the EOF probe), one byte more is
// not, and an oversized file never hides its neighbours.
func TestFindSessionByIDBoundsTheHeaderScan(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	dir := DefaultSessionDir(cwd)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, padding int, trailingNewline bool) string {
		t.Helper()
		header := sessionHeaderLine(strings.TrimSuffix(name, ".jsonl"), cwd)
		body := strings.Repeat("\n", padding) + header
		if trailingNewline {
			body += "\n"
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	header := sessionHeaderLine("at-limit", cwd)
	atLimit := write("at-limit.jsonl", maxSessionHeaderScanBytes-len(header), false)
	write("over-limit.jsonl", maxSessionHeaderScanBytes-len(sessionHeaderLine("over-limit", cwd))+1, false)
	write("far-over.jsonl", maxSessionHeaderScanBytes, true)
	small := write("small.jsonl", 0, true)

	if got, ok := FindSessionByID(cwd, "at-limit", ""); !ok || got != atLimit {
		t.Fatalf("header ending exactly at the scan limit: got %q, %v; want %q, true", got, ok, atLimit)
	}
	if got, ok := FindSessionByID(cwd, "over-limit", ""); ok {
		t.Fatalf("header ending one byte past the scan limit must not be found, got %q", got)
	}
	if got, ok := FindSessionByID(cwd, "far-over", ""); ok {
		t.Fatalf("header beyond the scan limit must not be found, got %q", got)
	}
	if got, ok := FindSessionByID(cwd, "small", ""); !ok || got != small {
		t.Fatalf("oversized neighbours must not hide a session: got %q, %v; want %q, true", got, ok, small)
	}
}

// pi: "filters exact IDs by cwd in a custom session directory" — an explicit
// directory that is not cwd's default holds sessions from many working
// directories, and only the one recorded for cwd matches; the default
// directory itself, even when passed explicitly, is never filtered.
func TestFindSessionByIDFiltersByCwdInACustomDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	projectA := filepath.Join(root, "project-a")
	projectB := filepath.Join(root, "project-b")
	sessionDir := filepath.Join(root, "sessions")
	foreign := writeSessionFile(t, sessionDir, "foreign.jsonl", sessionHeaderLine("foreign-id", projectB), sessionMessageLine)
	writeSessionFile(t, sessionDir, "no-cwd.jsonl", `{"type":"session","id":"no-cwd"}`)

	if got, ok := FindSessionByID(projectA, "foreign-id", sessionDir); ok {
		t.Fatalf("a session recorded for another cwd must not match, got %q", got)
	}
	if got, ok := FindSessionByID(projectB, "foreign-id", sessionDir); !ok || got != foreign {
		t.Fatalf("session recorded for cwd: got %q, %v; want %q, true", got, ok, foreign)
	}
	if got, ok := FindSessionByID(projectB, "no-cwd", sessionDir); ok {
		t.Fatalf("a header without a cwd never passes the cwd filter, got %q", got)
	}

	defaultDir := DefaultSessionDir(projectA)
	elsewhere := writeSessionFile(t, defaultDir, "elsewhere.jsonl", sessionHeaderLine("elsewhere-id", projectB))
	if got, ok := FindSessionByID(projectA, "elsewhere-id", defaultDir); !ok || got != elsewhere {
		t.Fatalf("the default directory passed explicitly is not filtered: got %q, %v; want %q, true", got, ok, elsewhere)
	}
}

// One unreadable or corrupt entry must not prevent other sessions from being
// found (pi readSessionHeaderForDiscovery), and a missing directory is simply
// empty.
func TestFindSessionByIDIsBestEffort(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	dir := DefaultSessionDir(cwd)
	if err := os.MkdirAll(filepath.Join(dir, "a-directory.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSessionFile(t, dir, "corrupt.jsonl", "{not json at all")
	want := writeSessionFile(t, dir, "real.jsonl", sessionHeaderLine("real-id", cwd))

	if got, ok := FindSessionByID(cwd, "real-id", ""); !ok || got != want {
		t.Fatalf("FindSessionByID = %q, %v; want %q, true", got, ok, want)
	}
	if got, ok := FindSessionByID(cwd, "real-id", filepath.Join(cwd, "does-not-exist")); ok {
		t.Fatalf("a missing directory has no sessions, found %q", got)
	}
}

// pi's list() shares findById's prologue: an explicit directory that is not
// cwd's default lists only the sessions recorded for cwd (a header without a
// cwd never matches), and the default directory -- implicit or explicit -- is
// never filtered.
func TestListSessionsFiltersByCwdInACustomDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	projectA := filepath.Join(root, "project-a")
	projectB := filepath.Join(root, "project-b")
	sessionDir := filepath.Join(root, "sessions")
	writeSessionFile(t, sessionDir, "a.jsonl", sessionHeaderLine("a-id", projectA), sessionMessageLine)
	writeSessionFile(t, sessionDir, "b.jsonl", sessionHeaderLine("b-id", projectB), sessionMessageLine)
	writeSessionFile(t, sessionDir, "no-cwd.jsonl", `{"type":"session","id":"no-cwd"}`)
	ids := func(infos []SessionInfo) []string {
		var out []string
		for _, info := range infos {
			out = append(out, info.ID)
		}
		return out
	}

	if got := ids(ListSessions(projectA, sessionDir)); !reflect.DeepEqual(got, []string{"a-id"}) {
		t.Fatalf("ListSessions(projectA, custom) = %v; want [a-id]", got)
	}
	if got := ids(ListSessions(projectB, sessionDir)); !reflect.DeepEqual(got, []string{"b-id"}) {
		t.Fatalf("ListSessions(projectB, custom) = %v; want [b-id]", got)
	}
	if latest, ok := LatestSession(projectB, sessionDir); !ok || latest.ID != "b-id" {
		t.Fatalf("LatestSession(projectB, custom) = %+v, %v; want b-id", latest, ok)
	}

	defaultDir := DefaultSessionDir(projectA)
	writeSessionFile(t, defaultDir, "elsewhere.jsonl", sessionHeaderLine("elsewhere-id", projectB), sessionMessageLine)
	if got := ids(ListSessions(projectA, "")); !reflect.DeepEqual(got, []string{"elsewhere-id"}) {
		t.Fatalf("ListSessions(projectA, default) = %v; want [elsewhere-id]", got)
	}
	if got := ids(ListSessions(projectA, defaultDir)); !reflect.DeepEqual(got, []string{"elsewhere-id"}) {
		t.Fatalf("ListSessions(projectA, explicit default) = %v; want [elsewhere-id]", got)
	}
}
