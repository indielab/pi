package coding

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEnv is an in-memory ExecutionEnv. Its existence is the point of the seam:
// before ExecutionEnv the read tool could only be tested against a real
// filesystem.
type fakeEnv struct {
	dir   string
	files map[string][]byte
	execs []string
}

func newFakeEnv(dir string) *fakeEnv {
	return &fakeEnv{dir: dir, files: map[string][]byte{}}
}

func (f *fakeEnv) Cwd() string { return f.dir }

func (f *fakeEnv) AbsolutePath(_ context.Context, path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Join(f.dir, path), nil
}
func (f *fakeEnv) JoinPath(_ context.Context, parts []string) (string, error) {
	return filepath.Join(parts...), nil
}
func (f *fakeEnv) ReadBinaryFile(_ context.Context, path string) ([]byte, error) {
	data, ok := f.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}
func (f *fakeEnv) ReadTextFile(ctx context.Context, path string) (string, error) {
	data, err := f.ReadBinaryFile(ctx, path)
	return string(data), err
}
func (f *fakeEnv) ReadTextLines(ctx context.Context, path string, maxLines int) ([]string, error) {
	text, err := f.ReadTextFile(ctx, path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(text, "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return lines, nil
}
func (f *fakeEnv) WriteFile(_ context.Context, path string, content []byte) error {
	f.files[path] = content
	return nil
}
func (f *fakeEnv) AppendFile(_ context.Context, path string, content []byte) error {
	f.files[path] = append(f.files[path], content...)
	return nil
}
func (f *fakeEnv) RenameFile(_ context.Context, src, dst string) error {
	data, ok := f.files[src]
	if !ok {
		return os.ErrNotExist
	}
	f.files[dst] = data
	delete(f.files, src)
	return nil
}
func (f *fakeEnv) Stat(_ context.Context, path string) (FileInfo, error) {
	if data, ok := f.files[path]; ok {
		return FileInfo{Path: path, Name: filepath.Base(path), Kind: FileKindFile, Size: int64(len(data))}, nil
	}
	// Any path that is a prefix of a known file is a directory.
	for p := range f.files {
		if strings.HasPrefix(p, strings.TrimSuffix(path, "/")+"/") {
			return FileInfo{Path: path, Name: filepath.Base(path), Kind: FileKindDirectory}, nil
		}
	}
	return FileInfo{}, os.ErrNotExist
}
func (f *fakeEnv) ListDir(_ context.Context, path string) ([]FileInfo, error) {
	var out []FileInfo
	prefix := strings.TrimSuffix(path, "/") + "/"
	for p, data := range f.files {
		if strings.HasPrefix(p, prefix) {
			out = append(out, FileInfo{Path: p, Name: filepath.Base(p), Kind: FileKindFile, Size: int64(len(data))})
		}
	}
	return out, nil
}
func (f *fakeEnv) CanonicalPath(_ context.Context, path string) (string, error) { return path, nil }
func (f *fakeEnv) Exists(_ context.Context, path string) (bool, error) {
	_, ok := f.files[path]
	return ok, nil
}
func (f *fakeEnv) Exec(_ context.Context, command string, _ *ShellExecOptions) (ShellResult, error) {
	f.execs = append(f.execs, command)
	return ShellResult{Stdout: "fake:" + command}, nil
}
func (f *fakeEnv) Cleanup() error { return nil }

var _ ExecutionEnv = (*fakeEnv)(nil)

// The seam's whole purpose: the read tool reads from wherever the env says,
// with no filesystem involved.
func TestReadToolUsesInjectedEnv(t *testing.T) {
	dir := "/virtual/project"
	env := newFakeEnv(dir)
	env.files[filepath.Join(dir, "hello.txt")] = []byte("line one\nline two\n")

	tool := readToolEnv(env)
	res, err := tool.Execute(context.Background(), "1",
		map[string]any{"path": "hello.txt"}, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	text := resultText(res)
	if !strings.Contains(text, "line one") || !strings.Contains(text, "line two") {
		t.Fatalf("read tool did not return the injected content, got:\n%s", text)
	}
	// Nothing was written to disk: /virtual does not exist.
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the fake env must not touch the real filesystem (stat %s = %v)", dir, err)
	}
}

// A directory must report pi's EISDIR text through the env's FileKind rather
// than through os.FileInfo.IsDir.
func TestReadToolEnvRejectsDirectory(t *testing.T) {
	dir := "/virtual/project"
	env := newFakeEnv(dir)
	env.files[filepath.Join(dir, "sub", "a.txt")] = []byte("x")

	tool := readToolEnv(env)
	_, err := tool.Execute(context.Background(), "1", map[string]any{"path": "sub"}, nil)
	if err == nil || !strings.Contains(err.Error(), "EISDIR") {
		t.Fatalf("reading a directory must report EISDIR, got %v", err)
	}
}

// LocalEnv must behave exactly as the tool did before the seam existed —
// otherwise the refactor changed behavior rather than relocating it.
func TestReadToolLocalEnvMatchesDirectRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	viaWrapper := readTool(dir)
	viaEnv := readToolEnv(NewLocalEnv(dir))

	a, err := viaWrapper.Execute(context.Background(), "1", map[string]any{"path": "file.txt"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := viaEnv.Execute(context.Background(), "1", map[string]any{"path": "file.txt"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resultText(a) != resultText(b) {
		t.Fatalf("LocalEnv diverged from the direct read:\n--- wrapper ---\n%s\n--- env ---\n%s",
			resultText(a), resultText(b))
	}
}

func TestLocalEnvFileSystemRoundTrip(t *testing.T) {
	dir := t.TempDir()
	env := NewLocalEnv(dir)
	ctx := context.Background()

	if err := env.WriteFile(ctx, "nested/deep/f.txt", []byte("one\ntwo\nthree\n")); err != nil {
		t.Fatalf("WriteFile must create parent directories: %v", err)
	}
	if got, err := env.ReadTextFile(ctx, "nested/deep/f.txt"); err != nil || got != "one\ntwo\nthree\n" {
		t.Fatalf("ReadTextFile = %q, %v", got, err)
	}
	if lines, err := env.ReadTextLines(ctx, "nested/deep/f.txt", 2); err != nil || len(lines) != 2 {
		t.Fatalf("ReadTextLines(maxLines=2) = %v, %v", lines, err)
	}
	if err := env.AppendFile(ctx, "nested/deep/f.txt", []byte("four\n")); err != nil {
		t.Fatal(err)
	}
	if got, _ := env.ReadTextFile(ctx, "nested/deep/f.txt"); !strings.HasSuffix(got, "four\n") {
		t.Fatalf("AppendFile did not append: %q", got)
	}

	info, err := env.Stat(ctx, "nested/deep/f.txt")
	if err != nil || info.Kind != FileKindFile || info.Name != "f.txt" {
		t.Fatalf("Stat = %+v, %v", info, err)
	}
	if dirInfo, err := env.Stat(ctx, "nested/deep"); err != nil || dirInfo.Kind != FileKindDirectory {
		t.Fatalf("Stat on a directory = %+v, %v", dirInfo, err)
	}

	if ok, err := env.Exists(ctx, "nested/deep/f.txt"); err != nil || !ok {
		t.Fatalf("Exists on a present file = %v, %v", ok, err)
	}
	// A missing path is (false, nil) — not an error. This is pi's contract and
	// the distinction callers depend on.
	if ok, err := env.Exists(ctx, "nope.txt"); err != nil || ok {
		t.Fatalf("Exists on a missing file = %v, %v; want false, nil", ok, err)
	}

	if err := env.RenameFile(ctx, "nested/deep/f.txt", "moved.txt"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := env.Exists(ctx, "nested/deep/f.txt"); ok {
		t.Fatal("RenameFile left the source behind")
	}

	entries, err := env.ListDir(ctx, ".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if !slicesContains(names, "moved.txt") {
		t.Fatalf("ListDir = %v, want it to contain moved.txt", names)
	}

	// A relative path resolves against Cwd, an absolute one does not.
	if got, _ := env.AbsolutePath(ctx, "x.txt"); got != filepath.Join(dir, "x.txt") {
		t.Fatalf("AbsolutePath(relative) = %q", got)
	}
	if got, _ := env.AbsolutePath(ctx, "/tmp/x.txt"); got != filepath.Clean("/tmp/x.txt") {
		t.Fatalf("AbsolutePath(absolute) = %q", got)
	}
}

func TestLocalEnvExec(t *testing.T) {
	if _, _, _, err := getShellConfig(); err != nil {
		t.Skipf("no shell available: %v", err)
	}
	dir := t.TempDir()
	env := NewLocalEnv(dir)
	ctx := context.Background()

	var streamed strings.Builder
	res, err := env.Exec(ctx, "printf 'hi'", &ShellExecOptions{
		OnStdout: func(chunk string) { streamed.WriteString(chunk) },
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.Stdout != "hi" || res.ExitCode != 0 {
		t.Fatalf("Exec = %+v, want stdout \"hi\" exit 0", res)
	}
	if streamed.String() != "hi" {
		t.Fatalf("OnStdout saw %q, want %q", streamed.String(), "hi")
	}

	// A non-zero exit is a RESULT, not an error — pi reports exitCode in the
	// success arm of its Result.
	res, err = env.Exec(ctx, "exit 3", nil)
	if err != nil {
		t.Fatalf("a failing command must not be an error, got %v", err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("ExitCode = %d, want 3", res.ExitCode)
	}

	// Explicit env entries override the inherited ambient value.
	t.Setenv("PI_EXECENV_PROBE", "ambient")
	res, err = env.Exec(ctx, "printf '%s' \"$PI_EXECENV_PROBE\"", &ShellExecOptions{
		Env: map[string]string{"PI_EXECENV_PROBE": "explicit"},
	})
	if err != nil || res.Stdout != "explicit" {
		t.Fatalf("explicit env must win, got %q (%v)", res.Stdout, err)
	}
}

func slicesContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
