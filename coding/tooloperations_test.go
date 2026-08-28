package coding

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each tool's Operations seam must actually be consulted — a seam the tool
// ignores is worse than no seam, because it reads as pluggable and is not.

func TestReadToolOperationsAreConsulted(t *testing.T) {
	var readCalls, accessCalls, detectCalls int
	ops := ReadOperations{
		ReadFile: func(_ context.Context, p string) ([]byte, error) {
			readCalls++
			return []byte("injected content\n"), nil
		},
		Access:              func(context.Context, string) error { accessCalls++; return nil },
		DetectImageMimeType: func(context.Context, string) string { detectCalls++; return "" },
	}
	tool := readToolOps("/nowhere", ops)
	res, err := tool.Execute(context.Background(), "1", map[string]any{"path": "x.txt"}, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(resultText(res), "injected content") {
		t.Fatalf("injected ReadFile was not used: %q", resultText(res))
	}
	if readCalls == 0 || accessCalls == 0 || detectCalls == 0 {
		t.Fatalf("ops not all consulted: read=%d access=%d detect=%d", readCalls, accessCalls, detectCalls)
	}
}

// pi calls access BEFORE reading, so an access failure must stop the read.
func TestReadToolAccessFailureStopsTheRead(t *testing.T) {
	denied := errors.New("EACCES: permission denied")
	ops := ReadOperations{
		Access: func(context.Context, string) error { return denied },
		ReadFile: func(context.Context, string) ([]byte, error) {
			t.Fatal("ReadFile ran after Access failed")
			return nil, nil
		},
	}
	tool := readToolOps("/nowhere", ops)
	if _, err := tool.Execute(context.Background(), "1", map[string]any{"path": "x"}, nil); !errors.Is(err, denied) {
		t.Fatalf("err = %v, want the Access error", err)
	}
}

func TestWriteToolOperationsAreConsulted(t *testing.T) {
	var wrotePath, wroteContent, madeDir string
	ops := WriteOperations{
		WriteFile: func(_ context.Context, p, content string) error { wrotePath, wroteContent = p, content; return nil },
		Mkdir:     func(_ context.Context, d string) error { madeDir = d; return nil },
	}
	tool := writeToolOps("/nowhere", ops)
	if _, err := tool.Execute(context.Background(), "1",
		map[string]any{"path": "sub/new.txt", "content": "hello"}, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.HasSuffix(wrotePath, filepath.Join("sub", "new.txt")) || wroteContent != "hello" {
		t.Fatalf("WriteFile got (%q, %q)", wrotePath, wroteContent)
	}
	if !strings.HasSuffix(madeDir, "sub") {
		t.Fatalf("Mkdir got %q, want the parent directory", madeDir)
	}
	// Nothing reached the real filesystem.
	if _, err := os.Stat("/nowhere"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("injected write must not touch disk (stat = %v)", err)
	}
}

func TestEditToolOperationsAreConsulted(t *testing.T) {
	var wrote string
	ops := EditOperations{
		ReadFile:  func(context.Context, string) ([]byte, error) { return []byte("alpha\nbeta\n"), nil },
		WriteFile: func(_ context.Context, _ string, content string) error { wrote = content; return nil },
		Access:    func(context.Context, string) error { return nil },
	}
	tool := editToolOps("/nowhere", ops)
	_, err := tool.Execute(context.Background(), "1", map[string]any{
		"path":  "x.txt",
		"edits": []any{map[string]any{"oldText": "beta", "newText": "gamma"}},
	}, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if wrote != "alpha\ngamma\n" {
		t.Fatalf("edit wrote %q, want %q", wrote, "alpha\ngamma\n")
	}
}

// A nil member must fall back to the local default — pi's spread-over-defaults.
// Overriding one member must not blank the others.
func TestOperationsPartialOverrideKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("on disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Only DetectImageMimeType is overridden; ReadFile and Access must still be
	// the local defaults, so the on-disk content comes back.
	tool := readToolOps(dir, ReadOperations{
		DetectImageMimeType: func(context.Context, string) string { return "" },
	})
	res, err := tool.Execute(context.Background(), "1", map[string]any{"path": "f.txt"}, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(resultText(res), "on disk") {
		t.Fatalf("defaults were not preserved: %q", resultText(res))
	}
}

// The local default reproduces Node's EISDIR text, because that emulation
// belongs to the filesystem implementation rather than to the tool.
func TestDefaultReadOperationsReportsEISDIR(t *testing.T) {
	dir := t.TempDir()
	if _, err := DefaultReadOperations().ReadFile(context.Background(), dir); err == nil ||
		!strings.Contains(err.Error(), "EISDIR") {
		t.Fatalf("reading a directory = %v, want pi's EISDIR text", err)
	}
}
