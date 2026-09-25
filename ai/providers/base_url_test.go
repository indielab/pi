package providers

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// baseURLRun is one row of testdata/base-url/base-url-*.json: what pi's
// adapter said for a base URL its request URL could not be made from.
type baseURLRun struct {
	API     string `json:"api"`
	BaseURL string `json:"baseUrl"`
	// Divergence, when set, says how the WHATWG URL parser and net/url
	// differ on the base URL.
	Divergence   string   `json:"divergence"`
	Events       []string `json:"events"`
	StopReason   string   `json:"stopReason"`
	ErrorMessage string   `json:"errorMessage"`
	OnPayload    int      `json:"onPayload"`
}

func loadBaseURLCapture(t *testing.T) []baseURLRun {
	t.Helper()
	raw, err := os.ReadFile("testdata/base-url/base-url-49681e1b7.json")
	if err != nil {
		t.Fatalf("read the base-url capture: %v (regenerate it with testdata/base-url/capture.mts)", err)
	}
	var capture struct {
		Rows []baseURLRun `json:"rows"`
	}
	if err := json.Unmarshal(raw, &capture); err != nil {
		t.Fatal(err)
	}
	if len(capture.Rows) == 0 {
		t.Fatal("the base-url capture has no rows")
	}
	return capture.Rows
}

// TestInvalidBaseURLMatchesPi streams each captured base URL both parsers
// refuse through its adapter and requires pi's outcome: TypeError "Invalid
// URL", after onPayload for the SDK adapters (their request URL is built in
// the SDK's request) and before it for pi-messages (which builds its URL
// first), and no request sent.
func TestInvalidBaseURLMatchesPi(t *testing.T) {
	ran := 0
	for _, run := range loadBaseURLCapture(t) {
		if run.Divergence != "" {
			continue
		}
		ran++
		t.Run(run.API+"/"+run.BaseURL, func(t *testing.T) {
			onPayload := 0
			opts := ai.StreamOptions{ProviderRequestOptions: ai.ProviderRequestOptions{
				APIKey: "test-api-key",
				OnPayload: func(any, *ai.Model) (any, error) {
					onPayload++
					return nil, nil
				},
			}}
			events, final := drain(streamAbortAdapter(t, context.Background(), run.API, run.BaseURL, opts))
			if !slices.Equal(events, run.Events) || string(final.StopReason) != run.StopReason || final.ErrorMessage != run.ErrorMessage {
				t.Errorf("events %v, stop %s %q\npi:    %v, stop %s %q", events, final.StopReason, final.ErrorMessage, run.Events, run.StopReason, run.ErrorMessage)
			}
			if onPayload != run.OnPayload {
				t.Errorf("OnPayload ran %d times; pi %d", onPayload, run.OnPayload)
			}
		})
	}
	if ran < 20 {
		t.Fatalf("only %d agreeing rows in the capture", ran)
	}
}

// TestInvalidBaseURLDivergencesStillDiffer requires each captured divergence
// to still be one: where the WHATWG URL parser refuses a URL net/url takes,
// the port does not say pi's "Invalid URL", and where it takes one net/url
// refuses, the port does. Each is decided by fetchURLError, so no request is
// sent; the day either parser's reading changes, the row must move.
func TestInvalidBaseURLDivergencesStillDiffer(t *testing.T) {
	ran := 0
	for _, run := range loadBaseURLCapture(t) {
		if run.Divergence == "" {
			continue
		}
		ran++
		t.Run(run.API+"/"+run.BaseURL, func(t *testing.T) {
			url := strings.TrimRight(run.BaseURL, "/") + "/x"
			piRefuses := run.ErrorMessage == "Invalid URL"
			if portRefuses := fetchURLError(url) != nil; portRefuses == piRefuses {
				t.Errorf("%s: the port and pi agree now (both refuse: %v); retag the row: %s", run.BaseURL, piRefuses, run.Divergence)
			}
		})
	}
	if ran == 0 {
		t.Fatal("no divergence rows in the capture")
	}
}
