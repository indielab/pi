package coding

import (
	"encoding/hex"
	"encoding/json"
	"os"
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
		Full     struct {
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
