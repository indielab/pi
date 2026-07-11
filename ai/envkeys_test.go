package ai

import "testing"

// pi 8eeaa2bc threads per-stream scoped ProviderEnv through getEnvApiKey /
// findEnvKeys: a non-empty scoped value wins over the OS environment, an empty
// scoped value falls through, and nil reads only the OS environment.
func TestGetEnvApiKeyScopedEnv(t *testing.T) {
	// Neutralize the ambient OS key so the scoped override is the only source.
	t.Setenv("OPENAI_API_KEY", "")

	if got := GetEnvApiKey("openai", map[string]string{"OPENAI_API_KEY": "scoped"}); got != "scoped" {
		t.Errorf("scoped env should provide the key: got %q, want %q", got, "scoped")
	}

	// Scoped non-empty wins over a set OS value.
	t.Setenv("OPENAI_API_KEY", "os-value")
	if got := GetEnvApiKey("openai", map[string]string{"OPENAI_API_KEY": "scoped"}); got != "scoped" {
		t.Errorf("scoped should win over OS: got %q, want %q", got, "scoped")
	}

	// Empty scoped value falls through to the OS value.
	if got := GetEnvApiKey("openai", map[string]string{"OPENAI_API_KEY": ""}); got != "os-value" {
		t.Errorf("empty scoped should fall through to OS: got %q, want %q", got, "os-value")
	}

	// Nil scoped env reads only the OS environment (prior behavior preserved).
	if got := GetEnvApiKey("openai", nil); got != "os-value" {
		t.Errorf("nil env should read OS: got %q, want %q", got, "os-value")
	}

	// Absent everywhere -> no key.
	t.Setenv("OPENAI_API_KEY", "")
	if got := GetEnvApiKey("openai", nil); got != "" {
		t.Errorf("absent key should be empty: got %q", got)
	}
}

// withEnvAPIKey / withEnvAPIKeySimple must consult the scoped env on the stream
// options when no explicit API key is set, so a host can scope credentials per
// request without touching the process environment (pi 8eeaa2bc).
func TestWithEnvAPIKeyUsesScopedEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	model := &Model{Provider: "openai"}

	got := withEnvAPIKey(model, &StreamOptions{Env: map[string]string{"OPENAI_API_KEY": "scoped"}})
	if got.APIKey != "scoped" {
		t.Errorf("withEnvAPIKey should pull key from scoped env: got %q", got.APIKey)
	}

	gotSimple := withEnvAPIKeySimple(model, &SimpleStreamOptions{
		StreamOptions: StreamOptions{Env: map[string]string{"OPENAI_API_KEY": "scoped"}},
	})
	if gotSimple.APIKey != "scoped" {
		t.Errorf("withEnvAPIKeySimple should pull key from scoped env: got %q", gotSimple.APIKey)
	}

	// An explicit API key still wins over the scoped env.
	explicit := withEnvAPIKey(model, &StreamOptions{APIKey: "explicit", Env: map[string]string{"OPENAI_API_KEY": "scoped"}})
	if explicit.APIKey != "explicit" {
		t.Errorf("explicit API key should win: got %q", explicit.APIKey)
	}
}

// The ambient-auth marker (GetEnvApiKey returns "<authenticated>" when a
// provider is usable via ambient credentials with no explicit key — e.g. AWS
// SDK default chain) must never be injected as a real API key by the compat
// dispatch (pi 850c210b filters AMBIENT_AUTH_MARKER in withEnvApiKey).
func TestWithEnvAPIKeyFiltersAmbientMarker(t *testing.T) {
	model := &Model{Provider: "amazon-bedrock"}
	env := map[string]string{"AWS_PROFILE": "default"}

	// Precondition: this env makes GetEnvApiKey resolve to the ambient marker.
	if key := GetEnvApiKey(model.Provider, env); key != ambientAuthMarker {
		t.Fatalf("precondition: expected ambient marker, got %q", key)
	}

	got := withEnvAPIKey(model, &StreamOptions{Env: env})
	if got.APIKey != "" {
		t.Errorf("withEnvAPIKey must not inject the ambient marker as a key: got %q", got.APIKey)
	}
	if got.Env["AWS_PROFILE"] != "default" {
		t.Errorf("env must survive the ambient-marker skip: got %v", got.Env)
	}

	gotSimple := withEnvAPIKeySimple(model, &SimpleStreamOptions{StreamOptions: StreamOptions{Env: env}})
	if gotSimple.APIKey != "" {
		t.Errorf("withEnvAPIKeySimple must not inject the ambient marker as a key: got %q", gotSimple.APIKey)
	}
}

// pi 2cbce395 merges the resolved provider env into the request options so it
// reaches the API. The Go port has no resolution-time env source (no catalog
// provider resolves one, matching upstream where resolution.env is latent), so
// this is a passthrough: opts.Env must survive auth resolution and reach the
// provider, where it is consulted via ProviderEnvValue (e.g. PI_CACHE_RETENTION,
// Cloudflare base-URL). This locks that the env is not dropped by the key step.
func TestStreamEnvReachesProvider(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	model := &Model{Provider: "openai"}
	env := map[string]string{"PI_CACHE_RETENTION": "long", "OPENAI_API_KEY": "scoped"}

	// Key injected from env: the returned options still carry the full Env.
	got := withEnvAPIKey(model, &StreamOptions{Env: env})
	if got.Env["PI_CACHE_RETENTION"] != "long" {
		t.Errorf("env must survive key injection: got %v", got.Env)
	}

	// Explicit-key passthrough must also preserve Env unchanged.
	passthrough := withEnvAPIKey(model, &StreamOptions{APIKey: "explicit", Env: env})
	if passthrough.Env["PI_CACHE_RETENTION"] != "long" {
		t.Errorf("env must survive explicit-key passthrough: got %v", passthrough.Env)
	}
}
