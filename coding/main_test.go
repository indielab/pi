package coding

import (
	"fmt"
	"os"
	"testing"
)

// TestMain gives the package's tests a temp directory of their own, removed
// afterwards. The shell tools save the full output of a truncated command
// there (pi-bash-*.log), and tests must not leave those files in the user's
// temp directory, where pi's own sessions write the same names.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pi-coding-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "coding tests: cannot create a temp directory:", err)
		os.Exit(1)
	}
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} { // TMP and TEMP are Windows'
		os.Setenv(key, dir)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
