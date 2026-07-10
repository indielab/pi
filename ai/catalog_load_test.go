package ai

import "testing"

func TestCatalogLoads(t *testing.T) {
	// pi-ai 0.80.6 pruned all legacy claude-3.x / claude-4-0 models from the
	// anthropic catalog; smoke-test loading against a dated, still-present model.
	m := GetModel("anthropic", "claude-haiku-4-5-20251001")
	if m == nil || m.MaxTokens != 64000 {
		t.Fatalf("haiku-4-5 not loaded: %#v", m)
	}
	if len(GetProviders()) < 10 {
		t.Fatalf("too few providers: %d", len(GetProviders()))
	}
}
