package coding

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ExecutionEnv is the filesystem and process-execution environment the agent
// runs against, ported from pi's `ExecutionEnv extends FileSystem, Shell`
// (packages/agent/src/harness/types.ts:231-315 at ccfe79ed2).
//
// It exists so that "where do files live and how do commands run" is an
// injected capability rather than a hard-coded call to the local OS: a host can
// point the agent at a sandbox, a container, or a remote machine without the
// tools knowing. The port's own default is LocalEnv, which is exactly the
// behavior the tools had before this seam existed.
//
// Two deliberate renderings of pi's shape, both standing conventions here:
// pi's `Result<T, FileError>` becomes Go's `(T, error)`, and pi's trailing
// `abortSignal?: AbortSignal` becomes a leading `context.Context`.
type ExecutionEnv interface {
	FileSystem
	Shell
}

// FileKind is the sort of thing a path addresses (pi FileInfo.kind).
type FileKind string

const (
	FileKindFile      FileKind = "file"
	FileKindDirectory FileKind = "directory"
	FileKindSymlink   FileKind = "symlink"
	FileKindOther     FileKind = "other"
)

// FileInfo describes a path without following symlinks (pi FileInfo).
type FileInfo struct {
	Path    string
	Name    string
	Kind    FileKind
	Size    int64
	ModTime time.Time
}

// FileSystem is the file half of an ExecutionEnv. Paths that are not absolute
// resolve against Cwd.
type FileSystem interface {
	// Cwd is the working directory relative paths resolve against.
	Cwd() string
	// AbsolutePath returns an absolute path without requiring it to exist and
	// without resolving symlinks.
	AbsolutePath(ctx context.Context, path string) (string, error)
	// JoinPath joins segments in the filesystem's namespace without requiring
	// the result to exist.
	JoinPath(ctx context.Context, parts []string) (string, error)
	// ReadTextFile reads a whole UTF-8 file.
	ReadTextFile(ctx context.Context, path string) (string, error)
	// ReadTextLines reads UTF-8 lines, stopping once maxLines have been read.
	// A maxLines of 0 or less means "no limit".
	ReadTextLines(ctx context.Context, path string, maxLines int) ([]string, error)
	// ReadBinaryFile reads a whole file as bytes.
	ReadBinaryFile(ctx context.Context, path string) ([]byte, error)
	// WriteFile creates or overwrites a file, creating parent directories.
	WriteFile(ctx context.Context, path string, content []byte) error
	// AppendFile creates or appends to a file, creating parent directories.
	AppendFile(ctx context.Context, path string, content []byte) error
	// RenameFile atomically renames a file, replacing an existing destination.
	// It does not copy across filesystems.
	RenameFile(ctx context.Context, sourcePath, destinationPath string) error
	// Stat returns metadata for a path without following symlinks.
	//
	// Named Stat rather than pi's `fileInfo` because a Go method may not share
	// a name with the type it returns.
	Stat(ctx context.Context, path string) (FileInfo, error)
	// ListDir lists a directory's direct children without following symlinks.
	ListDir(ctx context.Context, path string) ([]FileInfo, error)
	// CanonicalPath resolves symlinks for an existing path.
	CanonicalPath(ctx context.Context, path string) (string, error)
	// Exists reports whether a path exists. A missing path is (false, nil);
	// anything else — a permission failure, say — is an error.
	Exists(ctx context.Context, path string) (bool, error)
}

// ShellExecOptions are the per-command controls for Shell.Exec (pi
// ShellExecOptions). The zero value is pi's default in every field: run in the
// env's Cwd, inherit the ambient environment, no timeout, no streaming.
type ShellExecOptions struct {
	// Cwd overrides the env's working directory for this command. A relative
	// value resolves against the env's Cwd.
	Cwd string
	// Env carries variables for the command. Values here win over inherited
	// ones when InheritEnv is true.
	Env map[string]string
	// InheritEnv controls whether the ambient environment is inherited. Nil
	// means pi's default, which is true — the pointer exists to keep that
	// default reachable from the zero value.
	InheritEnv *bool
	// TimeoutSeconds bounds the command. Zero or less means no timeout.
	TimeoutSeconds int
	// OnStdout and OnStderr receive output chunks as they are produced.
	OnStdout func(chunk string)
	OnStderr func(chunk string)
}

// ShellResult is a finished command (pi's `{stdout, stderr, exitCode}`).
type ShellResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Shell is the process half of an ExecutionEnv.
type Shell interface {
	// Exec runs a shell command. A non-zero exit is reported in the result, not
	// as an error; an error means the command could not be run or was cut short.
	Exec(ctx context.Context, command string, options *ShellExecOptions) (ShellResult, error)
	// Cleanup releases shell resources. Best-effort: it must not panic, and a
	// nil return is the normal case.
	Cleanup() error
}

// LocalEnv is the ExecutionEnv backed by the machine the agent runs on. It is
// the port's default and reproduces exactly what the tools did before this seam
// existed.
type LocalEnv struct {
	// Dir is the working directory. An empty Dir means the process's own.
	Dir string
}

// NewLocalEnv returns a LocalEnv rooted at dir.
func NewLocalEnv(dir string) *LocalEnv { return &LocalEnv{Dir: dir} }

// Cwd implements FileSystem.
func (e *LocalEnv) Cwd() string {
	if e.Dir != "" {
		return e.Dir
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// resolve turns a possibly-relative path into an absolute one against Cwd.
func (e *LocalEnv) resolve(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(e.Cwd(), path)
}

// AbsolutePath implements FileSystem.
func (e *LocalEnv) AbsolutePath(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return e.resolve(path), nil
}

// JoinPath implements FileSystem.
func (e *LocalEnv) JoinPath(ctx context.Context, parts []string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return filepath.Join(parts...), nil
}

// ReadTextFile implements FileSystem.
func (e *LocalEnv) ReadTextFile(ctx context.Context, path string) (string, error) {
	data, err := e.ReadBinaryFile(ctx, path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ReadTextLines implements FileSystem.
func (e *LocalEnv) ReadTextLines(ctx context.Context, path string, maxLines int) ([]string, error) {
	text, err := e.ReadTextFile(ctx, path)
	if err != nil {
		return nil, err
	}
	lines := splitLines(text)
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return lines, nil
}

// ReadBinaryFile implements FileSystem.
func (e *LocalEnv) ReadBinaryFile(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return os.ReadFile(e.resolve(path))
}

// WriteFile implements FileSystem.
func (e *LocalEnv) WriteFile(ctx context.Context, path string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	abs := e.resolve(path)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, content, 0o644)
}

// AppendFile implements FileSystem.
func (e *LocalEnv) AppendFile(ctx context.Context, path string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	abs := e.resolve(path)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// RenameFile implements FileSystem.
func (e *LocalEnv) RenameFile(ctx context.Context, sourcePath, destinationPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(e.resolve(sourcePath), e.resolve(destinationPath))
}

// Stat implements FileSystem.
func (e *LocalEnv) Stat(ctx context.Context, path string) (FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return FileInfo{}, err
	}
	abs := e.resolve(path)
	// Lstat, not Stat: pi specifies "without following symlinks".
	st, err := os.Lstat(abs)
	if err != nil {
		return FileInfo{}, err
	}
	return fileInfoFrom(abs, st), nil
}

// ListDir implements FileSystem.
func (e *LocalEnv) ListDir(ctx context.Context, path string) ([]FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	abs := e.resolve(path)
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	out := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		st, err := entry.Info()
		if err != nil {
			// A child that vanished between the read and the stat is not a
			// failure of the listing; skip it, as a directory walk would.
			continue
		}
		out = append(out, fileInfoFrom(filepath.Join(abs, entry.Name()), st))
	}
	return out, nil
}

// CanonicalPath implements FileSystem.
func (e *LocalEnv) CanonicalPath(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(e.resolve(path))
}

// Exists implements FileSystem.
func (e *LocalEnv) Exists(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if _, err := os.Lstat(e.resolve(path)); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// fileInfoFrom projects an os.FileInfo into pi's FileInfo shape.
func fileInfoFrom(path string, st os.FileInfo) FileInfo {
	kind := FileKindOther
	switch {
	case st.Mode()&os.ModeSymlink != 0:
		kind = FileKindSymlink
	case st.IsDir():
		kind = FileKindDirectory
	case st.Mode().IsRegular():
		kind = FileKindFile
	}
	return FileInfo{
		Path:    path,
		Name:    filepath.Base(path),
		Kind:    kind,
		Size:    st.Size(),
		ModTime: st.ModTime(),
	}
}

// --- Shell half of LocalEnv ---

// Exec implements Shell by running the command through the machine's shell,
// the same resolution the bash tool uses (getShellConfig). A non-zero exit is
// reported in the result; an error means the command could not be started, or
// the context ended it.
func (e *LocalEnv) Exec(ctx context.Context, command string, options *ShellExecOptions) (ShellResult, error) {
	if options == nil {
		options = &ShellExecOptions{}
	}
	if err := ctx.Err(); err != nil {
		return ShellResult{}, err
	}

	shell, shellArgs, useStdin, err := getShellConfig()
	if err != nil {
		return ShellResult{}, err
	}

	runCtx := ctx
	if options.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(options.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	var cmd *exec.Cmd
	// Legacy WSL bash takes the command on stdin (`bash -s`); otherwise it is
	// the final argv entry (`bash -c <command>`). Same split as the bash tool.
	if useStdin {
		cmd = exec.CommandContext(runCtx, shell, shellArgs...)
		cmd.Stdin = strings.NewReader(command)
	} else {
		cmd = exec.CommandContext(runCtx, shell, append(shellArgs, command)...)
	}

	dir := options.Cwd
	if dir == "" {
		dir = e.Cwd()
	} else if !filepath.IsAbs(dir) {
		dir = filepath.Join(e.Cwd(), dir)
	}
	cmd.Dir = dir
	cmd.Env = execEnviron(options)

	var stdout, stderr strings.Builder
	cmd.Stdout = chunkWriter(&stdout, options.OnStdout)
	cmd.Stderr = chunkWriter(&stderr, options.OnStderr)

	// Own process group, and on cancel or timeout kill the whole tree so
	// backgrounded grandchildren do not survive — same policy as the bash tool.
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessTree(cmd) }

	runErr := cmd.Run()
	result := ShellResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			// A command that ran and failed is a result, not an error — pi
			// reports exitCode in the success arm of its Result.
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, runErr
	}
	return result, nil
}

// Cleanup implements Shell. LocalEnv holds no shell resources between calls —
// every Exec starts and reaps its own process — so there is nothing to release.
func (e *LocalEnv) Cleanup() error { return nil }

// execEnviron builds the child environment from pi's inheritEnv/env pair:
// inherit by default, with explicit entries overriding inherited ones.
func execEnviron(options *ShellExecOptions) []string {
	inherit := options.InheritEnv == nil || *options.InheritEnv
	var env []string
	if inherit {
		env = os.Environ()
	}
	for k, v := range options.Env {
		env = append(env, k+"="+v)
	}
	return env
}

// chunkWriter tees output into a buffer and, when onChunk is set, hands each
// write to it as it arrives (pi's onStdout/onStderr).
func chunkWriter(buf *strings.Builder, onChunk func(string)) io.Writer {
	if onChunk == nil {
		return buf
	}
	return writerFunc(func(p []byte) (int, error) {
		buf.Write(p)
		onChunk(string(p))
		return len(p), nil
	})
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// splitLines splits on "\n", matching how the read tool and pi both count
// lines. A trailing newline yields a final empty element, as in pi.
func splitLines(text string) []string { return strings.Split(text, "\n") }

// LocalEnv satisfies the full ExecutionEnv contract.
var _ ExecutionEnv = (*LocalEnv)(nil)
