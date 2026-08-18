package providers

import (
	"errors"
	"strings"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// Ported from pi packages/ai/test/google-thinking-level-map.test.ts (upstream
// af2c35223). The vertex half of that file has no Go counterpart — google-vertex
// is on the deliberate non-port list.

func googleLevelMapModel(id string, levels map[ai.ModelThinkingLevel]*string) *ai.Model {
	return &ai.Model{
		ID:               id,
		Name:             id,
		Api:              ai.APIGoogleGenerativeAI,
		Provider:         "test-google",
		BaseURL:          "https://example.invalid/v1beta",
		Reasoning:        true,
		ThinkingLevelMap: levels,
		Input:            []string{"text"},
		ContextWindow:    128000,
		MaxTokens:        4096,
	}
}

func levelMap(pairs map[ai.ModelThinkingLevel]string) map[ai.ModelThinkingLevel]*string {
	out := make(map[ai.ModelThinkingLevel]*string, len(pairs))
	for k, v := range pairs {
		value := v
		out[k] = &value
	}
	return out
}

func TestResolveGoogleThinkingLevel(t *testing.T) {
	// Logical levels resolve to themselves; "off" is the one coercion.
	defaults := map[ai.ModelThinkingLevel]string{
		"off":     "high",
		"minimal": "minimal",
		"low":     "low",
		"medium":  "medium",
		"high":    "high",
	}
	for level, want := range defaults {
		t.Run(string(level), func(t *testing.T) {
			got, err := resolveGoogleThinkingLevel(googleLevelMapModel("gemini-3.7-flash", nil), level)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != want {
				t.Fatalf("want %q, got %q", want, got)
			}
		})
	}

	// A mapping value wins over the requested level, case-insensitively.
	for _, mapped := range []string{"minimal", "low", "medium", "high", "MINIMAL", "LOW", "MEDIUM", "HIGH"} {
		want := strings.ToLower(mapped)
		model := googleLevelMapModel("gemini-3.7-flash", levelMap(map[ai.ModelThinkingLevel]string{
			"high": mapped, "xhigh": mapped, "max": mapped,
		}))
		for _, level := range []ai.ModelThinkingLevel{"high", "xhigh", "max"} {
			got, err := resolveGoogleThinkingLevel(model, level)
			if err != nil {
				t.Fatalf("%s -> %s: unexpected error: %v", level, mapped, err)
			}
			if got != want {
				t.Fatalf("%s -> %s: want %q, got %q", level, mapped, want, got)
			}
		}
	}
}

func TestResolveGoogleThinkingLevelErrors(t *testing.T) {
	// The message is model-visible and byte-exact against pi's template, JS
	// String(mapped) rendering included.
	cases := []struct {
		name  string
		model *ai.Model
		level ai.ModelThinkingLevel
		want  string
	}{
		{
			name:  "unmappable value",
			model: googleLevelMapModel("gemini-3.7-flash", levelMap(map[ai.ModelThinkingLevel]string{"xhigh": "extreme"})),
			level: "xhigh",
			want:  "Unsupported Google thinking level mapping for test-google/gemini-3.7-flash: xhigh -> extreme",
		},
		{
			name:  "absent key renders undefined",
			model: googleLevelMapModel("gemini-3.7-flash", nil),
			level: "max",
			want:  "Unsupported Google thinking level mapping for test-google/gemini-3.7-flash: max -> undefined",
		},
		{
			// pi's `typeof mapped === "string"` guard sends an explicit null down
			// the same fallback as an absent key, but String(null) prints "null".
			name:  "null entry renders null",
			model: googleLevelMapModel("gemini-3.7-flash", map[ai.ModelThinkingLevel]*string{"max": nil}),
			level: "max",
			want:  "Unsupported Google thinking level mapping for test-google/gemini-3.7-flash: max -> null",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveGoogleThinkingLevel(tc.model, tc.level)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if err.Error() != tc.want {
				t.Fatalf("want %q, got %q", tc.want, err.Error())
			}
		})
	}
}

// captureGoogleSimplePayload runs StreamSimpleGoogle and returns the request body
// it would have sent, aborting the stream from OnPayload the way pi's test does.
func captureGoogleSimplePayload(t *testing.T, model *ai.Model, reasoning ai.ThinkingLevel, budgets *ai.ThinkingBudgets) map[string]any {
	t.Helper()
	var captured any
	opts := &ai.SimpleStreamOptions{Reasoning: reasoning, ThinkingBudgets: budgets}
	opts.APIKey = "test"
	opts.OnPayload = func(payload any, _ *ai.Model) (any, error) {
		captured = payload
		return nil, errors.New("payload captured")
	}
	msg := StreamSimpleGoogle(t.Context(), model, ai.Context{
		Messages: []ai.Message{ai.UserMessage{Content: ai.ContentList{ai.TextContent{Text: "Hello"}}}},
	}, opts).Result()
	if !strings.Contains(msg.ErrorMessage, "payload captured") {
		t.Fatalf("stream did not reach OnPayload: %q", msg.ErrorMessage)
	}
	body, _ := captured.(map[string]any)
	if body == nil {
		t.Fatalf("no payload captured (got %T)", captured)
	}
	return roundtripBody(t, body)
}

func googleThinkingConfig(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	gen, _ := body["generationConfig"].(map[string]any)
	if gen == nil {
		t.Fatalf("no generationConfig: %v", body)
	}
	cfg, _ := gen["thinkingConfig"].(map[string]any)
	if cfg == nil {
		t.Fatalf("no thinkingConfig: %v", gen)
	}
	return cfg
}

func TestGoogleSimpleMapsExtendedLevels(t *testing.T) {
	// xhigh/max only survive clamping because the map opts into them; they must
	// then reach the wire as the level they map to.
	model := googleLevelMapModel("gemini-3.7-flash", levelMap(map[ai.ModelThinkingLevel]string{
		"xhigh": "high", "max": "high",
	}))
	for _, reasoning := range []ai.ThinkingLevel{"xhigh", "max"} {
		t.Run(string(reasoning), func(t *testing.T) {
			cfg := googleThinkingConfig(t, captureGoogleSimplePayload(t, model, reasoning, nil))
			if cfg["includeThoughts"] != true || cfg["thinkingLevel"] != "HIGH" {
				t.Fatalf("want includeThoughts+thinkingLevel HIGH, got %v", cfg)
			}
		})
	}
}

func TestGoogleSimpleHonorsUppercaseMappingForStandardLevel(t *testing.T) {
	model := googleLevelMapModel("gemini-3.7-flash", levelMap(map[ai.ModelThinkingLevel]string{"high": "LOW"}))
	cfg := googleThinkingConfig(t, captureGoogleSimplePayload(t, model, "high", nil))
	if cfg["thinkingLevel"] != "LOW" {
		t.Fatalf("want thinkingLevel LOW, got %v", cfg)
	}
}

func TestGoogleSimpleUsesMappedLevelForTokenBudget(t *testing.T) {
	// The custom-budget lookup keys off the RESOLVED level, not the requested one.
	model := googleLevelMapModel("gemini-2.5-flash", levelMap(map[ai.ModelThinkingLevel]string{"xhigh": "high"}))
	high := 1234
	budgets := &ai.ThinkingBudgets{High: &high}
	cfg := googleThinkingConfig(t, captureGoogleSimplePayload(t, model, "xhigh", budgets))
	if cfg["thinkingBudget"] != float64(1234) {
		t.Fatalf("want thinkingBudget 1234, got %v", cfg)
	}
}

func TestGoogleSimpleUnresolvableLevelFailsStream(t *testing.T) {
	// pi throws out of streamSimple; the Go seam encodes it as a terminal error.
	model := googleLevelMapModel("gemini-3.7-flash", levelMap(map[ai.ModelThinkingLevel]string{"xhigh": "extreme"}))
	opts := &ai.SimpleStreamOptions{Reasoning: "xhigh"}
	opts.APIKey = "test"
	// Record rather than t.Fatal: this runs on the provider's stream goroutine,
	// where a Goexit would strand Result() instead of failing the test.
	built := false
	opts.OnPayload = func(payload any, _ *ai.Model) (any, error) {
		built = true
		return nil, errors.New("request must not be built for an unresolvable level")
	}
	msg := StreamSimpleGoogle(t.Context(), model, ai.Context{}, opts).Result()
	if built {
		t.Fatal("request must not be built for an unresolvable level")
	}
	if msg.StopReason != ai.StopError {
		t.Fatalf("want stopReason error, got %q", msg.StopReason)
	}
	want := "Unsupported Google thinking level mapping for test-google/gemini-3.7-flash: xhigh -> extreme"
	if msg.ErrorMessage != want {
		t.Fatalf("want %q, got %q", want, msg.ErrorMessage)
	}
	if msg.Model != model.ID || msg.Provider != model.Provider || msg.Api != model.Api {
		t.Fatalf("error message must identify the model: %+v", msg)
	}
}
