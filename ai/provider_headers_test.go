package ai

import (
	"context"
	"encoding/json"
	"testing"
)

// A model's headers must round-trip all three states through JSON: catalog data
// carrying an explicit `null` has to decode as a deletion marker, not as an
// absent key, or the suppression it encodes is silently lost (upstream
// a24fb9e96).
func TestProviderHeadersJSONRoundTrip(t *testing.T) {
	const raw = `{"id":"m","name":"M","api":"openai-completions","provider":"p","baseUrl":"u",` +
		`"headers":{"X-Present":"v","X-Empty":"","X-Suppressed":null}}`
	var model Model
	if err := json.Unmarshal([]byte(raw), &model); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	value, ok := model.Headers["X-Present"]
	if !ok || value == nil || *value != "v" {
		t.Fatalf(`X-Present = %v, want a present "v"`, value)
	}
	value, ok = model.Headers["X-Empty"]
	if !ok || value == nil || *value != "" {
		t.Fatalf("X-Empty = %v, want a present empty string, not a marker", value)
	}
	value, ok = model.Headers["X-Suppressed"]
	if !ok {
		t.Fatal("X-Suppressed decoded as absent; an explicit null is a deletion marker")
	}
	if value != nil {
		t.Fatalf("X-Suppressed = %q, want a nil deletion marker", *value)
	}
	if _, ok := model.Headers["X-Absent"]; ok {
		t.Fatal("X-Absent must stay absent")
	}

	out, err := json.Marshal(model.Headers)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := string(out), `{"X-Empty":"","X-Present":"v","X-Suppressed":null}`; got != want {
		t.Fatalf("re-marshalled headers = %s, want %s", got, want)
	}
}

// mergeHeaders carries markers through: a null override replaces the base value
// with the marker instead of dropping the entry (which would silently restore
// the default the marker exists to suppress).
func TestMergeHeadersPreservesDeletionMarkers(t *testing.T) {
	merged := mergeHeaders(
		ProviderHeaders{"Authorization": HeaderValue("Bearer base"), "X-Keep": HeaderValue("base")},
		ProviderHeaders{"authorization": nil},
	)
	if _, ok := merged["Authorization"]; ok {
		t.Fatal("the case-insensitive base entry should have been replaced by the override")
	}
	marker, ok := merged["authorization"]
	if !ok {
		t.Fatal("null override dropped; the deletion marker must survive the merge")
	}
	if marker != nil {
		t.Fatalf("authorization = %q, want a nil deletion marker", *marker)
	}
	if merged["X-Keep"] == nil || *merged["X-Keep"] != "base" {
		t.Fatalf("X-Keep = %v, want base", merged["X-Keep"])
	}
}

// applyAuth threads markers from resolved auth and from the request options all
// the way to the options the provider is dispatched with — the path upstream's
// model-registry stopped stripping.
func TestApplyAuthPreservesDeletionMarkers(t *testing.T) {
	models := CreateModels(nil)
	models.SetProvider(CreateProvider(CreateProviderOptions{
		ID: "cf",
		Auth: ProviderAuth{APIKey: &ApiKeyAuth{
			Name: "cf",
			Resolve: func(context.Context, AuthContext, *Credential) (*AuthResult, error) {
				return &AuthResult{Auth: ModelAuth{Headers: ProviderHeaders{
					"cf-aig-authorization": HeaderValue("Bearer gateway"),
					"Authorization":        nil,
				}}}, nil
			},
		}},
	}))
	model := &Model{ID: "m", Provider: "cf", Api: APIOpenAICompletions,
		Headers: ProviderHeaders{"x-api-key": nil, "X-Model": HeaderValue("")}}

	_, opts, err := models.(*modelsImpl).applyAuth(context.Background(), model,
		&StreamOptions{Headers: ProviderHeaders{"X-Consumer": nil}}, ModelsStreamTransforms{})
	if err != nil {
		t.Fatalf("applyAuth: %v", err)
	}
	for _, name := range []string{"Authorization", "x-api-key", "X-Consumer"} {
		value, ok := opts.Headers[name]
		if !ok {
			t.Fatalf("%s missing; the deletion marker was stripped", name)
		}
		if value != nil {
			t.Fatalf("%s = %q, want a nil deletion marker", name, *value)
		}
	}
	if value := opts.Headers["X-Model"]; value == nil || *value != "" {
		t.Fatalf("X-Model = %v, want a present empty value, not a marker", value)
	}
	if value := opts.Headers["cf-aig-authorization"]; value == nil || *value != "Bearer gateway" {
		t.Fatalf("cf-aig-authorization = %v, want the resolved gateway credential", value)
	}
}
