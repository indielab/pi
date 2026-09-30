package coding

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// fullOutputCapture is testdata/fulloutput: what pi's OutputAccumulator made of
// each row's chunks (capture.mts).
type fullOutputCapture struct {
	SHA  string `json:"sha"`
	Rows []struct {
		Name     string   `json:"name"`
		Chunks   []string `json:"chunks"`
		MaxLines int      `json:"maxLines"`
		MaxBytes int      `json:"maxBytes"`
		ReadMax  int      `json:"readMax"`
		Snapshot struct {
			Content         string  `json:"content"`
			Truncated       bool    `json:"truncated"`
			TruncatedBy     *string `json:"truncatedBy"`
			TotalLines      int     `json:"totalLines"`
			TotalBytes      int     `json:"totalBytes"`
			OutputLines     int     `json:"outputLines"`
			OutputBytes     int     `json:"outputBytes"`
			LastLinePartial bool    `json:"lastLinePartial"`
			TempFile        bool    `json:"tempFile"`
			// TempFileBeforeFinish is whether a snapshot that does not
			// persist names a temp file before finish: the accumulator opens
			// one as soon as the raw bytes, the decoded bytes or the lines
			// pass its limits.
			TempFileBeforeFinish bool `json:"tempFileBeforeFinish"`
		} `json:"snapshot"`
		Full struct {
			Content   string `json:"content"`
			Truncated bool   `json:"truncated"`
		} `json:"full"`
	} `json:"rows"`
}

func loadFullOutputCapture(t *testing.T) fullOutputCapture {
	t.Helper()
	data, err := os.ReadFile("testdata/fulloutput/fulloutput-3dd803d7e.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture fullOutputCapture
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	if len(capture.Rows) < 15 {
		t.Fatalf("only %d rows in the capture", len(capture.Rows))
	}
	return capture
}

// TestOutputAccumulatorReadFullOutputMatchesPi replays each row as bash.ts
// runs the accumulator: append, finish, snapshot (persistIfTruncated), close
// the temp file, then readFullOutput (upstream 1ff5b6fdd).
func TestOutputAccumulatorReadFullOutputMatchesPi(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	capture := loadFullOutputCapture(t)
	for _, row := range capture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			acc := newOutputAccumulator(row.MaxLines, row.MaxBytes, "pi-capture")
			for _, chunk := range row.Chunks {
				b, err := hex.DecodeString(chunk)
				if err != nil {
					t.Fatal(err)
				}
				acc.append(b)
			}
			acc.finish()
			acc.snapshot(true)
			acc.closeTempFile()
			content, truncated, err := acc.readFullOutput(row.ReadMax)
			if err != nil {
				t.Fatal(err)
			}
			if content != row.Full.Content || truncated != row.Full.Truncated {
				t.Errorf("readFullOutput(%d) = (%+q, %t), pi at %s (%+q, %t)", row.ReadMax, content, truncated, capture.SHA, row.Full.Content, row.Full.Truncated)
			}
		})
	}
}

// TestOutputAccumulatorSnapshotMatchesPi replays each row's display snapshot,
// the text and truncation the model sees. pi decodes the stream with a
// TextDecoder as it arrives, so a byte-order mark before the first character
// is dropped, invalid bytes become U+FFFD per maximal subpart, and the limits
// count the decoded text's bytes.
func TestOutputAccumulatorSnapshotMatchesPi(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	capture := loadFullOutputCapture(t)
	for _, row := range capture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			acc := newOutputAccumulator(row.MaxLines, row.MaxBytes, "pi-capture")
			for _, chunk := range row.Chunks {
				b, err := hex.DecodeString(chunk)
				if err != nil {
					t.Fatal(err)
				}
				acc.append(b)
			}
			tempFileBeforeFinish := acc.snapshot(false).fullOutputPath != ""
			acc.finish()
			snap := acc.snapshot(true)
			acc.closeTempFile()
			want := row.Snapshot
			truncatedBy := ""
			if want.TruncatedBy != nil {
				truncatedBy = *want.TruncatedBy
			}
			tr := snap.truncation
			got := []any{snap.content, tr.Truncated, tr.TruncatedBy, tr.TotalLines, tr.TotalBytes, tr.OutputLines, tr.OutputBytes, tr.LastLinePartial, snap.fullOutputPath != "", tempFileBeforeFinish}
			wantAll := []any{want.Content, want.Truncated, truncatedBy, want.TotalLines, want.TotalBytes, want.OutputLines, want.OutputBytes, want.LastLinePartial, want.TempFile, want.TempFileBeforeFinish}
			names := []string{"content", "truncated", "truncatedBy", "totalLines", "totalBytes", "outputLines", "outputBytes", "lastLinePartial", "tempFile", "tempFileBeforeFinish"}
			for i := range names {
				if got[i] != wantAll[i] {
					t.Errorf("%s = %#v, pi at %s %#v", names[i], got[i], capture.SHA, wantAll[i])
				}
			}
		})
	}
}

// A temp file the output cannot be saved to fails the call where pi's
// closeTempFile rejects with its stream's error (D90). Nothing is written after
// the first error.
func TestOutputAccumulatorReportsTempFileErrors(t *testing.T) {
	t.Run("the file cannot be created", func(t *testing.T) {
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
		acc := newOutputAccumulator(2, 1000, "pi-test")
		acc.append([]byte("1\n2\n3\n")) // three lines pass maxLines
		acc.finish()
		acc.snapshot(true)
		if err := acc.closeTempFile(); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("closeTempFile() = %v, want the create error (%v)", err, fs.ErrNotExist)
		}
	})
	t.Run("a write fails", func(t *testing.T) {
		t.Setenv("TMPDIR", t.TempDir())
		acc := newOutputAccumulator(2, 1000, "pi-test")
		acc.append([]byte("1\n2\n3\n"))
		if acc.tempFile == nil {
			t.Fatal("precondition: three lines should open the temp file")
		}
		acc.tempFile.Close() // every later write fails
		acc.append([]byte("4\n"))
		acc.finish()
		acc.snapshot(true)
		if err := acc.closeTempFile(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("closeTempFile() = %v, want the write error (%v)", err, os.ErrClosed)
		}
	})
}
