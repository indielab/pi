package ai

import "testing"

func TestCatalogLoads(t *testing.T) {
	// claude-3-5-haiku-* was dropped from the anthropic catalog in pi-ai 0.80.3;
	// smoke-test catalog loading against a still-present stable anthropic model.
	m := GetModel("anthropic", "claude-3-haiku-20240307")
	if m == nil || m.MaxTokens != 4096 {
		t.Fatalf("haiku not loaded: %#v", m)
	}
	if len(GetProviders()) < 10 {
		t.Fatalf("too few providers: %d", len(GetProviders()))
	}
}
