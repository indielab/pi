package providers

import (
	"context"
	"fmt"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// Mirrors pi's packages/ai/test/sampling-options.test.ts (upstream 25a2c8dc):
// arbitrary sampling parameters ride into the request body of the
// OpenAI-compatible adapters, merged model-then-request and applied after the
// named request fields, while other APIs ignore them.

// capturedSimplePayload runs a StreamSimple entry point far enough to build the
// request body and returns it, mirroring pi's onPayload-throws capture. The
// error aborts the stream before any network call.
func capturedSimplePayload(
	t *testing.T,
	stream ai.StreamSimpleFunction,
	model *ai.Model,
	opts ai.SimpleStreamOptions,
) map[string]any {
	t.Helper()
	var captured map[string]any
	opts.APIKey = "fake-key"
	opts.OnPayload = func(payload any, _ *ai.Model) (any, error) {
		params, ok := payload.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("payload is %T, want map[string]any", payload)
		}
		captured = params
		return nil, fmt.Errorf("payload captured")
	}
	final := stream(context.Background(), model, baseReq(), &opts).Result()
	if captured == nil {
		t.Fatalf("payload was never built (stream ended %s: %q)", final.StopReason, final.ErrorMessage)
	}
	return captured
}

func samplingModel(overrides func(*ai.Model)) *ai.Model {
	return openAIModel(func(m *ai.Model) {
		m.ID = "custom-model"
		m.Provider = "custom-provider"
		m.BaseURL = "http://127.0.0.1:9/v1"
		if overrides != nil {
			overrides(m)
		}
	})
}

func TestSamplingParamsRequestLevel(t *testing.T) {
	body := capturedSimplePayload(t, StreamSimpleOpenAICompletions, samplingModel(nil), ai.SimpleStreamOptions{
		StreamOptions: ai.StreamOptions{SamplingParams: map[string]any{"top_p": 0.95, "top_k": 0, "min_p": 0}},
	})
	for key, want := range map[string]any{"top_p": 0.95, "top_k": 0, "min_p": 0} {
		if body[key] != want {
			t.Errorf("%s = %v, want %v", key, body[key], want)
		}
	}
}

func TestSamplingParamsAbsentByDefault(t *testing.T) {
	body := capturedSimplePayload(t, StreamSimpleOpenAICompletions, samplingModel(nil), ai.SimpleStreamOptions{})
	if has(body, "temperature") || has(body, "top_p") {
		t.Fatalf("no sampling config must leave the body untouched, got temperature=%v top_p=%v",
			body["temperature"], body["top_p"])
	}
}

func TestSamplingParamsModelLevel(t *testing.T) {
	model := samplingModel(func(m *ai.Model) {
		m.SamplingParams = map[string]any{"temperature": 1, "top_p": 0.95}
	})
	body := capturedSimplePayload(t, StreamSimpleOpenAICompletions, model, ai.SimpleStreamOptions{})
	if body["temperature"] != 1 || body["top_p"] != 0.95 {
		t.Fatalf("model sampling params not applied: temperature=%v top_p=%v", body["temperature"], body["top_p"])
	}
}

func TestSamplingParamsRequestOverridesModelPerKey(t *testing.T) {
	model := samplingModel(func(m *ai.Model) {
		m.SamplingParams = map[string]any{"top_p": 0.95, "min_p": 0.05}
	})
	body := capturedSimplePayload(t, StreamSimpleOpenAICompletions, model, ai.SimpleStreamOptions{
		StreamOptions: ai.StreamOptions{SamplingParams: map[string]any{"top_p": 0.5}},
	})
	if body["top_p"] != 0.5 {
		t.Errorf("request key must win: top_p = %v, want 0.5", body["top_p"])
	}
	if body["min_p"] != 0.05 {
		t.Errorf("untouched model key must survive: min_p = %v, want 0.05", body["min_p"])
	}
	// The merge must not write back into the model's own map.
	if model.SamplingParams["top_p"] != 0.95 {
		t.Errorf("model defaults mutated: top_p = %v, want 0.95", model.SamplingParams["top_p"])
	}
}

func TestSamplingParamsOverrideNamedFields(t *testing.T) {
	zero := 0.0
	body := capturedSimplePayload(t, StreamSimpleOpenAICompletions, samplingModel(nil), ai.SimpleStreamOptions{
		StreamOptions: ai.StreamOptions{
			Temperature:    &zero,
			SamplingParams: map[string]any{"temperature": 1},
		},
	})
	if body["temperature"] != 1 {
		t.Fatalf("sampling params are applied last: temperature = %v, want 1", body["temperature"])
	}
}

func TestSamplingParamsResponsesRequestBody(t *testing.T) {
	model := samplingModel(func(m *ai.Model) {
		m.Api = ai.APIOpenAIResponses
		m.SamplingParams = map[string]any{"top_p": 0.95, "temperature": 0.3}
	})
	zero := 0.0
	body := capturedSimplePayload(t, StreamSimpleOpenAIResponses, model, ai.SimpleStreamOptions{
		StreamOptions: ai.StreamOptions{
			Temperature:    &zero,
			SamplingParams: map[string]any{"temperature": 1},
		},
	})
	if body["top_p"] != 0.95 {
		t.Errorf("model key must reach the responses body: top_p = %v, want 0.95", body["top_p"])
	}
	if body["temperature"] != 1 {
		t.Errorf("request key must win and be applied last: temperature = %v, want 1", body["temperature"])
	}
}

// Other APIs carry the merged params but never emit them, exactly like pi.
func TestSamplingParamsIgnoredByAnthropic(t *testing.T) {
	model := &ai.Model{
		ID: "vendor--claude", Name: "Vendor Proxy Claude", Api: ai.APIAnthropicMessages,
		Provider: "vendor-proxy", BaseURL: "http://127.0.0.1:9", Reasoning: true,
		Input: []string{"text"}, ContextWindow: 200000, MaxTokens: 32000,
	}
	body := capturedSimplePayload(t, StreamSimpleAnthropic, model, ai.SimpleStreamOptions{
		StreamOptions: ai.StreamOptions{SamplingParams: map[string]any{"top_p": 0.9, "top_k": 40}},
	})
	if has(body, "top_p") || has(body, "top_k") {
		t.Fatalf("anthropic must ignore sampling params, got top_p=%v top_k=%v", body["top_p"], body["top_k"])
	}
}

// MergeSamplingParams is pi's buildBaseOptions merge: nil only when neither side
// supplies anything, request keys over model keys otherwise.
func TestMergeSamplingParams(t *testing.T) {
	model := &ai.Model{SamplingParams: map[string]any{"top_p": 0.95, "min_p": 0.05}}
	if got := ai.MergeSamplingParams(&ai.Model{}, nil); got != nil {
		t.Errorf("no sampling config anywhere should be nil, got %v", got)
	}
	if got := ai.MergeSamplingParams(&ai.Model{}, &ai.SimpleStreamOptions{}); got != nil {
		t.Errorf("empty options should stay nil, got %v", got)
	}
	merged := ai.MergeSamplingParams(model, &ai.SimpleStreamOptions{
		StreamOptions: ai.StreamOptions{SamplingParams: map[string]any{"top_p": 0.5, "top_k": 40}},
	})
	want := map[string]any{"top_p": 0.5, "min_p": 0.05, "top_k": 40}
	if len(merged) != len(want) {
		t.Fatalf("merged = %v, want %v", merged, want)
	}
	for k, v := range want {
		if merged[k] != v {
			t.Errorf("merged[%q] = %v, want %v", k, merged[k], v)
		}
	}
}
