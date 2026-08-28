package coding

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// Pluggable operations for the built-in tools, ported from pi's per-tool
// `*Operations` seams (packages/coding-agent/src/core/tools/*.ts at ccfe79ed2).
// pi's own comment on ReadOperations states the intent: "Override these to
// delegate file reading to remote systems (for example SSH)."
//
// These are STRUCTS OF FUNCTIONS rather than Go interfaces, deliberately.
// pi's seams are object literals whose members are individually overridable and
// in one case optional (`detectImageMimeType?`), and callers override one or
// two members while inheriting the rest. A Go interface would force every
// implementer to supply all members and could not express the optional one; a
// struct of funcs maps pi's shape exactly and lets a nil field mean "use the
// default", which is what pi's `?` and its spread-over-defaults do.
//
// Every tool keeps its existing env-free constructor, which uses the local
// defaults, so none of this changes behavior until a host injects something.
//
// Note these are the CODING-AGENT seams. pi's separate harness tools take a
// single broad ExecutionEnv instead (see execenv.go); the port carries both,
// with EnvReadOperations and friends bridging one to the other.

// ReadOperations are the file operations the read tool performs.
type ReadOperations struct {
	// ReadFile reads a file's contents. Required.
	ReadFile func(ctx context.Context, absolutePath string) ([]byte, error)
	// Access reports whether the file is readable, returning an error if not.
	// Required.
	Access func(ctx context.Context, absolutePath string) error
	// DetectImageMimeType returns the image MIME type for a path, or "" for a
	// non-image. Optional — nil uses the built-in detector, matching pi's `?`.
	DetectImageMimeType func(ctx context.Context, absolutePath string) string
}

// EditOperations are the file operations the edit tool performs.
type EditOperations struct {
	ReadFile  func(ctx context.Context, absolutePath string) ([]byte, error)
	WriteFile func(ctx context.Context, absolutePath, content string) error
	// Access reports whether the file is readable AND writable (pi passes
	// R_OK|W_OK here, where the read tool passes R_OK alone).
	Access func(ctx context.Context, absolutePath string) error
}

// WriteOperations are the file operations the write tool performs.
type WriteOperations struct {
	WriteFile func(ctx context.Context, absolutePath, content string) error
	// Mkdir creates a directory and its parents.
	Mkdir func(ctx context.Context, dir string) error
}

// BashExecOptions are the per-command controls pi passes to BashOperations.exec.
type BashExecOptions struct {
	// OnData receives output as it is produced.
	OnData func(data []byte)
	// TimeoutSeconds bounds the command; zero or less means pi's default.
	TimeoutSeconds float64
	// Env is the child environment.
	Env []string
}

// BashOperations is the process execution the shell tools perform. pi shares
// one interface between bash and powershell (`PowerShellOperations =
// BashOperations`), so the port does too.
type BashOperations struct {
	// Exec runs a command and streams its output, returning the exit code.
	// A nil ExitCode means the process was killed rather than exiting, which is
	// pi's `exitCode: number | null`.
	Exec func(ctx context.Context, command, cwd string, options BashExecOptions) (exitCode *int, err error)
}

// GrepOperations are the file operations the grep tool performs.
type GrepOperations struct {
	// IsDirectory reports whether a path is a directory, erroring if it does
	// not exist.
	IsDirectory func(ctx context.Context, absolutePath string) (bool, error)
	// ReadFile reads a file's contents for context lines.
	ReadFile func(ctx context.Context, absolutePath string) (string, error)
}

// FindOperations are the file operations the find tool performs.
type FindOperations struct {
	Exists func(ctx context.Context, absolutePath string) (bool, error)
	// Glob returns paths matching a pattern. Nil keeps pi's behavior, where the
	// default is a placeholder and real matching happens in the tool via fd.
	Glob func(ctx context.Context, pattern, cwd string, ignore []string, limit int) ([]string, error)
}

// LsOperations are the file operations the ls tool performs.
type LsOperations struct {
	Exists func(ctx context.Context, absolutePath string) (bool, error)
	// Stat reports whether a path is a directory, erroring if not found.
	Stat func(ctx context.Context, absolutePath string) (isDir bool, err error)
	// Readdir lists a directory's entry names.
	Readdir func(ctx context.Context, absolutePath string) ([]string, error)
}

// --- local defaults (pi's default*Operations) ---

// DefaultReadOperations reads from the local filesystem.
func DefaultReadOperations() ReadOperations {
	return ReadOperations{
		ReadFile: func(_ context.Context, p string) ([]byte, error) { return localReadFile(p) },
		Access:   func(_ context.Context, p string) error { return accessReadable(p) },
		DetectImageMimeType: func(_ context.Context, p string) string {
			return DetectSupportedImageMimeTypeFromFile(p)
		},
	}
}

// DefaultEditOperations reads and writes the local filesystem.
func DefaultEditOperations() EditOperations {
	return EditOperations{
		ReadFile:  func(_ context.Context, p string) ([]byte, error) { return os.ReadFile(p) },
		WriteFile: func(_ context.Context, p, content string) error { return os.WriteFile(p, []byte(content), 0o644) },
		Access:    func(_ context.Context, p string) error { return accessReadWritable(p) },
	}
}

// DefaultWriteOperations writes the local filesystem.
func DefaultWriteOperations() WriteOperations {
	return WriteOperations{
		WriteFile: func(_ context.Context, p, content string) error { return os.WriteFile(p, []byte(content), 0o644) },
		Mkdir:     func(_ context.Context, dir string) error { return os.MkdirAll(dir, 0o755) },
	}
}

// DefaultGrepOperations reads the local filesystem.
func DefaultGrepOperations() GrepOperations {
	return GrepOperations{
		IsDirectory: func(_ context.Context, p string) (bool, error) {
			st, err := os.Stat(p)
			if err != nil {
				return false, err
			}
			return st.IsDir(), nil
		},
		ReadFile: func(_ context.Context, p string) (string, error) {
			data, err := os.ReadFile(p)
			return string(data), err
		},
	}
}

// DefaultFindOperations checks the local filesystem. Glob is nil, matching pi,
// whose default is a placeholder because real matching happens in the tool.
func DefaultFindOperations() FindOperations {
	return FindOperations{
		Exists: func(_ context.Context, p string) (bool, error) { return pathExistsErr(p) },
		Glob:   nil,
	}
}

// DefaultLsOperations lists the local filesystem.
func DefaultLsOperations() LsOperations {
	return LsOperations{
		Exists: func(_ context.Context, p string) (bool, error) { return pathExistsErr(p) },
		Stat: func(_ context.Context, p string) (bool, error) {
			st, err := os.Stat(p)
			if err != nil {
				return false, err
			}
			return st.IsDir(), nil
		},
		Readdir: func(_ context.Context, p string) ([]string, error) {
			entries, err := os.ReadDir(p)
			if err != nil {
				return nil, err
			}
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			return names, nil
		},
	}
}

// --- filling: a nil member falls back to the local default, which is pi's
// spread-over-defaults (`{...defaults, ...operations}`) ---

func (o ReadOperations) withDefaults() ReadOperations {
	d := DefaultReadOperations()
	if o.ReadFile == nil {
		o.ReadFile = d.ReadFile
	}
	if o.Access == nil {
		o.Access = d.Access
	}
	if o.DetectImageMimeType == nil {
		o.DetectImageMimeType = d.DetectImageMimeType
	}
	return o
}

func (o EditOperations) withDefaults() EditOperations {
	d := DefaultEditOperations()
	if o.ReadFile == nil {
		o.ReadFile = d.ReadFile
	}
	if o.WriteFile == nil {
		o.WriteFile = d.WriteFile
	}
	if o.Access == nil {
		o.Access = d.Access
	}
	return o
}

func (o WriteOperations) withDefaults() WriteOperations {
	d := DefaultWriteOperations()
	if o.WriteFile == nil {
		o.WriteFile = d.WriteFile
	}
	if o.Mkdir == nil {
		o.Mkdir = d.Mkdir
	}
	return o
}

func (o GrepOperations) withDefaults() GrepOperations {
	d := DefaultGrepOperations()
	if o.IsDirectory == nil {
		o.IsDirectory = d.IsDirectory
	}
	if o.ReadFile == nil {
		o.ReadFile = d.ReadFile
	}
	return o
}

func (o FindOperations) withDefaults() FindOperations {
	d := DefaultFindOperations()
	if o.Exists == nil {
		o.Exists = d.Exists
	}
	return o
}

func (o LsOperations) withDefaults() LsOperations {
	d := DefaultLsOperations()
	if o.Exists == nil {
		o.Exists = d.Exists
	}
	if o.Stat == nil {
		o.Stat = d.Stat
	}
	if o.Readdir == nil {
		o.Readdir = d.Readdir
	}
	return o
}

// --- the bridge: an ExecutionEnv can back the coding-agent seams ---
//
// This is what shape (b) means in practice (see docs/UPSTREAM.md "Harness
// shape"): the port carries ONE set of tools, and pi's two injection designs —
// coding-agent's narrow per-tool Operations and the harness's broad
// ExecutionEnv — meet here instead of being duplicated into two tool trees.

// EnvReadOperations backs the read tool with an ExecutionEnv.
func EnvReadOperations(env ExecutionEnv) ReadOperations {
	return ReadOperations{
		ReadFile: env.ReadBinaryFile,
		Access: func(ctx context.Context, p string) error {
			_, err := env.Stat(ctx, p)
			return err
		},
	}
}

// EnvEditOperations backs the edit tool with an ExecutionEnv.
func EnvEditOperations(env ExecutionEnv) EditOperations {
	return EditOperations{
		ReadFile: env.ReadBinaryFile,
		WriteFile: func(ctx context.Context, p, content string) error {
			return env.WriteFile(ctx, p, []byte(content))
		},
		Access: func(ctx context.Context, p string) error {
			_, err := env.Stat(ctx, p)
			return err
		},
	}
}

// EnvWriteOperations backs the write tool with an ExecutionEnv. Mkdir is a
// no-op because ExecutionEnv.WriteFile already creates parent directories,
// which is pi's own contract for it ("creating parent directories when
// supported").
func EnvWriteOperations(env ExecutionEnv) WriteOperations {
	return WriteOperations{
		WriteFile: func(ctx context.Context, p, content string) error {
			return env.WriteFile(ctx, p, []byte(content))
		},
		Mkdir: func(context.Context, string) error { return nil },
	}
}

// EnvGrepOperations backs the grep tool with an ExecutionEnv.
func EnvGrepOperations(env ExecutionEnv) GrepOperations {
	return GrepOperations{
		IsDirectory: func(ctx context.Context, p string) (bool, error) {
			info, err := env.Stat(ctx, p)
			if err != nil {
				return false, err
			}
			return info.Kind == FileKindDirectory, nil
		},
		ReadFile: env.ReadTextFile,
	}
}

// EnvLsOperations backs the ls tool with an ExecutionEnv.
func EnvLsOperations(env ExecutionEnv) LsOperations {
	return LsOperations{
		Exists: env.Exists,
		Stat: func(ctx context.Context, p string) (bool, error) {
			info, err := env.Stat(ctx, p)
			if err != nil {
				return false, err
			}
			return info.Kind == FileKindDirectory, nil
		},
		Readdir: func(ctx context.Context, p string) ([]string, error) {
			entries, err := env.ListDir(ctx, p)
			if err != nil {
				return nil, err
			}
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, filepath.Base(e.Name))
			}
			return names, nil
		},
	}
}

// EnvFindOperations backs the find tool with an ExecutionEnv. Glob stays nil,
// as in pi's default: an env exposes no glob, so matching stays with the tool.
func EnvFindOperations(env ExecutionEnv) FindOperations {
	return FindOperations{Exists: env.Exists}
}

// EnvBashOperations backs the shell tools with an ExecutionEnv's Shell half.
// Streaming is preserved by handing the env's chunk callback straight to OnData.
func EnvBashOperations(env ExecutionEnv) BashOperations {
	return BashOperations{
		Exec: func(ctx context.Context, command, cwd string, options BashExecOptions) (*int, error) {
			res, err := env.Exec(ctx, command, &ShellExecOptions{
				Cwd:            cwd,
				TimeoutSeconds: int(options.TimeoutSeconds),
				OnStdout:       func(chunk string) { emitChunk(options.OnData, chunk) },
				OnStderr:       func(chunk string) { emitChunk(options.OnData, chunk) },
			})
			if err != nil {
				return nil, err
			}
			code := res.ExitCode
			return &code, nil
		},
	}
}

func emitChunk(onData func([]byte), chunk string) {
	if onData != nil {
		onData([]byte(chunk))
	}
}

// localReadFile is os.ReadFile plus Node's EISDIR text. Node's fs.readFile
// raises `EISDIR: illegal operation on a directory, read` for a directory and
// Go's os.ReadFile reports something else, so the local filesystem
// implementation is where that difference is reconciled — which is also where
// Node keeps it.
func localReadFile(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, errors.New("EISDIR: illegal operation on a directory, read")
	}
	return os.ReadFile(path)
}

// accessReadable mirrors pi's fsAccess(path, R_OK).
func accessReadable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return f.Close()
}

// accessReadWritable mirrors pi's fsAccess(path, R_OK|W_OK).
func accessReadWritable(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	return f.Close()
}

// pathExistsErr reports existence, distinguishing "absent" from a real failure.
func pathExistsErr(path string) (bool, error) {
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
