package providers

import (
	"encoding/json"
	"os"
	"testing"
)

// A system message that replaces the prompt after the leading one collapses
// the transcript on every provider, including models that take system messages
// in place (upstream e4c75a732): the replayed state becomes the leading
// prompt, because no provider can retract the prompt it already received.
// Bodies are pi's, captured under node at e4c75a732 by
// testdata/transcript/capture-replace.mts.

const replaceCaptureFile = "testdata/transcript/replace-e4c75a732.json"

func replaceCapture(t *testing.T, name string) completionsNativeCase {
	t.Helper()
	data, err := os.ReadFile(replaceCaptureFile)
	if err != nil {
		t.Fatal(err)
	}
	var capture map[string]json.RawMessage
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatalf("%s: %v", replaceCaptureFile, err)
	}
	raw, ok := capture[name]
	if !ok {
		t.Fatalf("%s has no %q entry; rerun capture-replace.mts", replaceCaptureFile, name)
	}
	var c completionsNativeCase
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("%s %q: %v", replaceCaptureFile, name, err)
	}
	return c
}

// A forced prompt on Anthropic with native system messages and tool changes:
// the replacement is the top-level system prompt, no system message is sent in
// place, and the replayed tools anchor the native tool list.
func TestAnthropicNativeLateReplacementCollapses(t *testing.T) {
	model, req := replaceCapture(t, "anthropic-native-forced-prompt").decode(t)
	payload := payloadJSON(t, captureStreamSimplePayload(t, model, req))

	var system []string
	for _, block := range jsonItems(payload["system"]) {
		text, _ := block["text"].(string)
		system = append(system, text)
	}
	if len(system) != 1 || system[0] != "Exact prompt." {
		t.Fatalf("system = %q, want the forced prompt alone", system)
	}
	for i, message := range jsonItems(payload["messages"]) {
		if message["role"] == "system" {
			t.Fatalf("messages[%d] is a system message %v; a late replacement must collapse", i, message)
		}
	}
}

// Every key and value of the built body equals pi's.
func TestReplacementRequestBodies(t *testing.T) {
	for _, name := range []string{
		"anthropic-native-forced-prompt",
		"anthropic-native-replacement-then-patch",
		"anthropic-native-leading-replacement",
		"anthropic-native-replace-false",
		"anthropic-system-messages-only-forced-prompt",
		"completions-native-forced-prompt",
		"responses-native-forced-prompt",
	} {
		t.Run(name, func(t *testing.T) {
			c := replaceCapture(t, name)
			model, req := c.decode(t)
			assertRequestBody(t, captureStreamSimplePayload(t, model, req), c.Body)
		})
	}
}
