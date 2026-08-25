package coding_test

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/sky-valley/pi/coding"
)

// TestPublishedMimeSurface locks which image-MIME helpers the coding package
// publishes. Upstream de82e5367 promoted detectSupportedImageMimeTypeFromFile
// to pi's package index while deliberately leaving the buffer variant internal
// to utils/mime.ts, so the port exports exactly that one and no more. Asserted
// over the package source rather than by calling it, so the "and no more" half
// is checkable too.
func TestPublishedMimeSurface(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	funcs := map[string]bool{}
	for _, name := range pkg.GoFiles {
		file, err := parser.ParseFile(fset, filepath.Join(pkg.Dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
				funcs[fn.Name.Name] = true
			}
		}
	}
	if !funcs["DetectSupportedImageMimeTypeFromFile"] {
		t.Error("DetectSupportedImageMimeTypeFromFile must be exported (pi index.ts publishes it)")
	}
	if funcs["DetectSupportedImageMimeType"] {
		t.Error("the buffer variant must stay unexported: pi's index.ts does not publish it")
	}
}

// TestDetectSupportedImageMimeTypeFromFile exercises the published detector
// from outside the package, the way an SDK consumer reaches it.
func TestDetectSupportedImageMimeTypeFromFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, content []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	cases := []struct {
		path string
		want string
	}{
		{write("a.gif", []byte("GIF89a")), "image/gif"},
		{write("b.txt", []byte("not an image")), ""},
		{filepath.Join(dir, "missing.png"), ""},
	}
	for _, tc := range cases {
		if got := coding.DetectSupportedImageMimeTypeFromFile(tc.path); got != tc.want {
			t.Errorf("DetectSupportedImageMimeTypeFromFile(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}
