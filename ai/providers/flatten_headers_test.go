package providers

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"testing"
)

// responseHeadersCaptureFile is written by
// testdata/response-headers/capture-response-headers.mts, which runs pi's
// headersToRecord on node's fetch Response for each row's raw bytes.
const responseHeadersCaptureFile = "testdata/response-headers/response-headers-8676a0dcd.json"

// serveRaw answers one request on a loopback listener with raw, byte for byte,
// and returns the URL to request.
func serveRaw(t *testing.T, raw string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 64*1024)
		_, _ = conn.Read(buf)
		_, _ = io.WriteString(conn, raw)
	}()
	return "http://" + ln.Addr().String() + "/"
}

// TestFlattenHeadersMatchesPi reads each captured raw response through Go's
// client and requires flattenHeaders to build pi's headersToRecord record:
// names lowercased, a repeated name's values joined with ", " in wire order,
// and set-cookie holding its last value.
func TestFlattenHeadersMatchesPi(t *testing.T) {
	data, err := os.ReadFile(responseHeadersCaptureFile)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate it with testdata/response-headers/capture-response-headers.mts)", responseHeadersCaptureFile, err)
	}
	var capture struct {
		Rows []struct {
			Name     string            `json:"name"`
			Response string            `json:"response"`
			Record   map[string]string `json:"record"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatalf("decode %s: %v", responseHeadersCaptureFile, err)
	}
	if len(capture.Rows) == 0 {
		t.Fatalf("%s has no rows", responseHeadersCaptureFile)
	}
	for _, row := range capture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			resp, err := http.Get(serveRaw(t, row.Response))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			_, _ = io.ReadAll(resp.Body)
			if got := flattenHeaders(resp.Header); !reflect.DeepEqual(got, row.Record) {
				t.Errorf("flattenHeaders = %v, want pi's %v", got, row.Record)
			}
		})
	}
}
