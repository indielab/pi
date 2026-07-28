package ai

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeAuthContext is an injectable AuthContext over a fixed env map.
type fakeAuthContext struct {
	env   map[string]string
	files map[string]bool
}

func (f fakeAuthContext) Env(name string) string   { return f.env[name] }
func (f fakeAuthContext) FileExists(p string) bool { return f.files[p] }

func TestInMemoryCredentialStore(t *testing.T) {
	s := NewInMemoryCredentialStore()

	if c, err := s.Read("p"); err != nil || c != nil {
		t.Fatalf("empty read = (%v, %v), want (nil, nil)", c, err)
	}

	// Modify sets a credential and returns the post-write value.
	got, err := s.Modify("p", func(cur *Credential) (*Credential, error) {
		if cur != nil {
			t.Errorf("fn should see nil current, got %v", cur)
		}
		return &Credential{Type: CredentialAPIKey, Key: "k1"}, nil
	})
	if err != nil || got == nil || got.Key != "k1" {
		t.Fatalf("modify = (%v, %v), want key k1", got, err)
	}

	// Read returns a copy (mutating it must not affect the store).
	c, _ := s.Read("p")
	c.Key = "mutated"
	if again, _ := s.Read("p"); again.Key != "k1" {
		t.Errorf("store credential was aliased: got %q", again.Key)
	}

	// fn returning nil leaves the entry unchanged and returns current.
	got, _ = s.Modify("p", func(cur *Credential) (*Credential, error) { return nil, nil })
	if got == nil || got.Key != "k1" {
		t.Errorf("no-op modify should return current k1, got %v", got)
	}

	// fn error leaves the entry unchanged.
	if _, err := s.Modify("p", func(cur *Credential) (*Credential, error) {
		return nil, errors.New("boom")
	}); err == nil {
		t.Error("modify should propagate fn error")
	}
	if after, _ := s.Read("p"); after == nil || after.Key != "k1" {
		t.Errorf("entry should survive a failed modify, got %v", after)
	}

	// Delete removes it.
	_ = s.Delete("p")
	if after, _ := s.Read("p"); after != nil {
		t.Errorf("delete should remove the entry, got %v", after)
	}
}

// TestCredentialAPIKeyJSON locks the on-disk auth.json format aligned in pi
// 49fbe683: the discriminator serializes as "api_key" (underscore, not the old
// "api-key") and the provider-scoped values serialize under "env" (not the old
// "metadata"). Serialization-visible: this is the persisted credential shape.
func TestCredentialAPIKeyJSON(t *testing.T) {
	cred := &Credential{
		Type: CredentialAPIKey,
		Key:  "secret",
		Env:  map[string]string{"accountId": "acct-123", "gatewayId": "gw-1"},
	}

	raw, err := json.Marshal(cred)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)

	// Exact on-disk discriminator and env key.
	if !strings.Contains(got, `"type":"api_key"`) {
		t.Errorf("type must serialize as api_key, got %s", got)
	}
	if !strings.Contains(got, `"env":{`) {
		t.Errorf("provider env must serialize under \"env\", got %s", got)
	}
	// The old format must be gone.
	if strings.Contains(got, "api-key") {
		t.Errorf("legacy \"api-key\" discriminator must not appear, got %s", got)
	}
	if strings.Contains(got, "metadata") {
		t.Errorf("legacy \"metadata\" key must not appear, got %s", got)
	}

	// Round-trips back to an equal value, and parses on the new api_key type.
	var back Credential
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Type != CredentialAPIKey {
		t.Errorf("decoded type = %q, want %q", back.Type, CredentialAPIKey)
	}
	if !reflect.DeepEqual(&back, cred) {
		t.Errorf("round-trip mismatch: %+v vs %+v", &back, cred)
	}

	// Decoding the literal on-disk shape selects the api-key resolve path.
	var fromDisk Credential
	if err := json.Unmarshal([]byte(`{"type":"api_key","key":"k","env":{"accountId":"a"}}`), &fromDisk); err != nil {
		t.Fatalf("unmarshal disk shape: %v", err)
	}
	if fromDisk.Type != CredentialAPIKey {
		t.Fatalf("on-disk api_key must match CredentialAPIKey, got %q", fromDisk.Type)
	}
	res, err := resolveProviderAuth(
		"test",
		ProviderAuth{APIKey: EnvAPIKeyAuth("Test")},
		seededStore(t, "test", &fromDisk),
		fakeAuthContext{},
		nil,
	)
	if err != nil || res == nil || res.Auth.APIKey != "k" {
		t.Fatalf("api_key credential must resolve via the api-key path: %+v (err %v)", res, err)
	}
}

func seededStore(t *testing.T, provider string, c *Credential) CredentialStore {
	t.Helper()
	s := NewInMemoryCredentialStore()
	if _, err := s.Modify(provider, func(*Credential) (*Credential, error) { return c, nil }); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	return s
}

// TestResolveProviderAuthRequestOverrides locks ef231c49: a request apiKey
// resolves directly ahead of a stored credential, and request env overlays the
// AuthContext seen by the provider's Resolve.
func TestResolveProviderAuthRequestOverrides(t *testing.T) {
	auth := ProviderAuth{APIKey: &ApiKeyAuth{
		Name: "Test",
		Resolve: func(ctx AuthContext, credential *Credential) (*AuthResult, error) {
			key := ctx.Env("FALLBACK_KEY")
			if credential != nil && credential.Key != "" {
				key = credential.Key
			}
			if key == "" {
				return nil, nil
			}
			return &AuthResult{Auth: ModelAuth{APIKey: key}, Env: map[string]string{"ACCT": ctx.Env("ACCT")}}, nil
		},
	}}
	stored := &Credential{Type: CredentialAPIKey, Key: "stored-key"}

	// Request apiKey override wins over the stored credential.
	res, err := resolveProviderAuth("test", auth, seededStore(t, "test", stored), fakeAuthContext{}, &AuthResolutionOverrides{APIKey: "req-key"})
	if err != nil || res == nil || res.Auth.APIKey != "req-key" {
		t.Fatalf("override apiKey must resolve directly: %+v err %v", res, err)
	}

	// Override env overlays ambient and is visible to Resolve.
	res, err = resolveProviderAuth("test", auth, seededStore(t, "test", stored),
		fakeAuthContext{env: map[string]string{"ACCT": "ambient"}},
		&AuthResolutionOverrides{Env: map[string]string{"ACCT": "scoped"}})
	if err != nil || res == nil || res.Env["ACCT"] != "scoped" {
		t.Fatalf("override env must overlay ambient in Resolve(): %+v err %v", res, err)
	}
}

func TestEnvAPIKeyAuthResolve(t *testing.T) {
	auth := EnvAPIKeyAuth("Test key", "PRIMARY_KEY", "SECONDARY_KEY")

	// Stored credential key wins over env, and its env section passes through
	// (upstream 1942b260 — a stored credential must not drop its `env` overrides).
	ctx := fakeAuthContext{env: map[string]string{"PRIMARY_KEY": "from-env"}}
	res, err := auth.Resolve(ctx, &Credential{Type: CredentialAPIKey, Key: "stored", Env: map[string]string{"PI_CACHE_RETENTION": "1h"}})
	if err != nil || res == nil || res.Auth.APIKey != "stored" || res.Source != "stored credential" {
		t.Fatalf("stored key should win: %+v (err %v)", res, err)
	}
	if res.Env["PI_CACHE_RETENTION"] != "1h" {
		t.Fatalf("stored credential env section dropped: %+v", res.Env)
	}

	// No stored credential: first set env var in order resolves.
	res, _ = auth.Resolve(fakeAuthContext{env: map[string]string{"SECONDARY_KEY": "second"}}, nil)
	if res == nil || res.Auth.APIKey != "second" || res.Source != "SECONDARY_KEY" {
		t.Fatalf("env fallback wrong: %+v", res)
	}

	// Unconfigured -> nil.
	if res, _ := auth.Resolve(fakeAuthContext{}, nil); res != nil {
		t.Fatalf("unconfigured should be nil, got %+v", res)
	}
}

// TestAnthropicAPIKeyAuthResolve locks pi's anthropicApiKeyAuth precedence
// (upstream 24e5cc04): stored key → ANTHROPIC_AUTH_TOKEN (Authorization header) →
// ANTHROPIC_OAUTH_TOKEN/ANTHROPIC_API_KEY (api key).
func TestAnthropicAPIKeyAuthResolve(t *testing.T) {
	auth := anthropicAPIKeyAuth()

	// Stored credential key wins over both the auth token and the api-key envs.
	ctx := fakeAuthContext{env: map[string]string{
		AnthropicAuthTokenEnv: "tok", "ANTHROPIC_API_KEY": "sk-env",
	}}
	res, err := auth.Resolve(ctx, &Credential{Type: CredentialAPIKey, Key: "stored"})
	if err != nil || res == nil || res.Auth.APIKey != "stored" || res.Source != "stored credential" {
		t.Fatalf("stored key should win: %+v (err %v)", res, err)
	}

	// Auth token authenticates via an Authorization header, never x-api-key, and
	// beats a plain ANTHROPIC_API_KEY.
	res, _ = auth.Resolve(fakeAuthContext{env: map[string]string{
		AnthropicAuthTokenEnv: "tok", "ANTHROPIC_API_KEY": "sk-env",
	}}, nil)
	if res == nil || res.Auth.APIKey != "" || res.Auth.Headers["Authorization"] != "Bearer tok" || res.Source != AnthropicAuthTokenEnv {
		t.Fatalf("auth token should resolve to a bearer header: %+v", res)
	}

	// Without the auth token, the api-key envs resolve as a key.
	res, _ = auth.Resolve(fakeAuthContext{env: map[string]string{"ANTHROPIC_API_KEY": "sk-env"}}, nil)
	if res == nil || res.Auth.APIKey != "sk-env" || res.Source != "ANTHROPIC_API_KEY" {
		t.Fatalf("api-key fallback wrong: %+v", res)
	}

	// Unconfigured -> nil.
	if res, _ := auth.Resolve(fakeAuthContext{}, nil); res != nil {
		t.Fatalf("unconfigured should be nil, got %+v", res)
	}
}

func TestResolveProviderAuthAPIKeyAmbient(t *testing.T) {
	auth := ProviderAuth{APIKey: EnvAPIKeyAuth("Test", "TEST_KEY")}
	store := NewInMemoryCredentialStore()
	ctx := fakeAuthContext{env: map[string]string{"TEST_KEY": "ambient"}}

	res, err := resolveProviderAuth("test", auth, store, ctx, nil)
	if err != nil || res == nil || res.Auth.APIKey != "ambient" {
		t.Fatalf("ambient api-key resolution wrong: %+v (err %v)", res, err)
	}
}

func TestResolveProviderAuthOAuthRefreshUnderLock(t *testing.T) {
	store := NewInMemoryCredentialStore()
	// Seed an EXPIRED oauth credential.
	_, _ = store.Modify("oauthp", func(*Credential) (*Credential, error) {
		return &Credential{Type: CredentialOAuth, Refresh: "r0", Access: "old", Expires: 1}, nil
	})

	refreshCalls := 0
	auth := ProviderAuth{OAuth: &OAuthAuth{
		Name: "Test OAuth",
		Refresh: func(_ context.Context, c OAuthCredentials) (OAuthCredentials, error) {
			refreshCalls++
			return OAuthCredentials{Refresh: "r1", Access: "new", Expires: nowMillis() + 3_600_000}, nil
		},
		ToAuth: func(c OAuthCredentials) (ModelAuth, error) {
			return ModelAuth{APIKey: c.Access}, nil
		},
	}}

	res, err := resolveProviderAuth("oauthp", auth, store, ctx(), nil)
	if err != nil || res == nil || res.Auth.APIKey != "new" || res.Source != "OAuth" {
		t.Fatalf("expired oauth should refresh then derive: %+v (err %v)", res, err)
	}
	if refreshCalls != 1 {
		t.Fatalf("expected exactly one refresh, got %d", refreshCalls)
	}
	// The rotated credential must be persisted.
	if stored, _ := store.Read("oauthp"); stored == nil || stored.Access != "new" {
		t.Fatalf("rotated credential not persisted: %+v", stored)
	}

	// A still-valid credential is not refreshed.
	res, _ = resolveProviderAuth("oauthp", auth, store, ctx(), nil)
	if res == nil || res.Auth.APIKey != "new" || refreshCalls != 1 {
		t.Fatalf("valid token should not refresh again: %+v calls=%d", res, refreshCalls)
	}
}

func TestResolveProviderAuthOAuthRefreshFailure(t *testing.T) {
	store := NewInMemoryCredentialStore()
	_, _ = store.Modify("oauthp", func(*Credential) (*Credential, error) {
		return &Credential{Type: CredentialOAuth, Refresh: "r0", Access: "old", Expires: 1}, nil
	})

	auth := ProviderAuth{OAuth: &OAuthAuth{
		Refresh: func(_ context.Context, c OAuthCredentials) (OAuthCredentials, error) {
			return OAuthCredentials{}, errors.New("invalid_grant")
		},
		ToAuth: func(c OAuthCredentials) (ModelAuth, error) { return ModelAuth{}, nil },
	}}

	_, err := resolveProviderAuth("oauthp", auth, store, ctx(), nil)
	var me *ModelsError
	if !errors.As(err, &me) || me.Code != ErrOAuth {
		t.Fatalf("refresh failure should be ModelsError code oauth, got %v", err)
	}
	// The stored credential is preserved for retry.
	if stored, _ := store.Read("oauthp"); stored == nil || stored.Access != "old" {
		t.Fatalf("failed refresh must preserve the stored credential, got %+v", stored)
	}
}

// TestCredentialStoreList locks pi ff28097a's list(): non-secret metadata
// only, one entry per provider.
func TestCredentialStoreList(t *testing.T) {
	s := NewInMemoryCredentialStore()
	_, _ = s.Modify("b-oauth", func(*Credential) (*Credential, error) {
		return &Credential{Type: CredentialOAuth, Refresh: "r", Access: "secret", Expires: 1}, nil
	})
	_, _ = s.Modify("a-key", func(*Credential) (*Credential, error) {
		return &Credential{Type: CredentialAPIKey, Key: "secret"}, nil
	})

	infos, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []CredentialInfo{
		{ProviderID: "a-key", Type: CredentialAPIKey},
		{ProviderID: "b-oauth", Type: CredentialOAuth},
	}
	if !reflect.DeepEqual(infos, want) {
		t.Fatalf("list = %+v, want %+v", infos, want)
	}
}

func ctx() AuthContext { return fakeAuthContext{} }

// oauthProviderAuth builds an OAuth provider whose refresh yields a token
// expiring in expiresInMs and counts its calls.
func oauthProviderAuth(calls *int, expiresInMs int64) ProviderAuth {
	return ProviderAuth{OAuth: &OAuthAuth{
		Name: "Test OAuth",
		Refresh: func(_ context.Context, c OAuthCredentials) (OAuthCredentials, error) {
			*calls++
			return OAuthCredentials{Refresh: "r1", Access: "new", Expires: nowMillis() + expiresInMs}, nil
		},
		ToAuth: func(c OAuthCredentials) (ModelAuth, error) { return ModelAuth{APIKey: c.Access}, nil },
	}}
}

// seedOAuth stores an OAuth credential expiring in expiresInMs.
func seedOAuth(store CredentialStore, providerID string, expiresInMs int64) {
	_, _ = store.Modify(providerID, func(*Credential) (*Credential, error) {
		return &Credential{
			Type: CredentialOAuth, Refresh: "r0", Access: "old", Expires: nowMillis() + expiresInMs,
		}, nil
	})
}

// TestResolveStoredOAuthRefreshesWithinDefaultValidityWindow locks upstream
// 99e34013: a token still valid but expiring inside the default five-minute
// window is refreshed, where previously only a fully expired token was.
func TestResolveStoredOAuthRefreshesWithinDefaultValidityWindow(t *testing.T) {
	store := NewInMemoryCredentialStore()
	seedOAuth(store, "oauthp", 2*60*1000) // two minutes left

	calls := 0
	auth := oauthProviderAuth(&calls, 3_600_000)

	res, err := resolveProviderAuth("oauthp", auth, store, ctx(), nil)
	if err != nil || res == nil || res.Auth.APIKey != "new" {
		t.Fatalf("token inside the five-minute window must refresh: %+v (err %v)", res, err)
	}
	if calls != 1 {
		t.Fatalf("expected exactly one refresh, got %d", calls)
	}
}

// lockRaceStore reports an expired credential to the optimistic read but hands
// the under-lock callback a credential that is unexpired yet expires soon, so
// only the re-check's validity window decides whether a refresh happens.
type lockRaceStore struct {
	CredentialStore
	underLock *Credential
}

func (s lockRaceStore) Read(string) (*Credential, error) {
	return &Credential{Type: CredentialOAuth, Refresh: "r0", Access: "old", Expires: 1}, nil
}

func (s lockRaceStore) Modify(_ string, fn func(*Credential) (*Credential, error)) (*Credential, error) {
	next, err := fn(s.underLock)
	if err != nil {
		return nil, err
	}
	if next == nil {
		return s.underLock, nil
	}
	return next, nil
}

// TestResolveStoredOAuthUnderLockUsesValidityWindow locks that the
// double-checked re-check uses the same window as the optimistic check: a
// credential another request left with two minutes of life still refreshes.
func TestResolveStoredOAuthUnderLockUsesValidityWindow(t *testing.T) {
	store := lockRaceStore{
		CredentialStore: NewInMemoryCredentialStore(),
		underLock: &Credential{
			Type: CredentialOAuth, Refresh: "r0", Access: "short", Expires: nowMillis() + 2*60*1000,
		},
	}

	calls := 0
	auth := oauthProviderAuth(&calls, 3_600_000)

	res, err := resolveProviderAuth("oauthp", auth, store, ctx(), nil)
	if err != nil || res == nil || res.Auth.APIKey != "new" {
		t.Fatalf("under-lock re-check must apply the validity window: %+v (err %v)", res, err)
	}
	if calls != 1 {
		t.Fatalf("expected exactly one refresh, got %d", calls)
	}
}

// TestResolveStoredOAuthExplicitMinValidityUnsatisfied locks the post-refresh
// contract that only explicit callers get: a refreshed token that still expires
// inside the requested window is an error.
func TestResolveStoredOAuthExplicitMinValidityUnsatisfied(t *testing.T) {
	store := NewInMemoryCredentialStore()
	seedOAuth(store, "oauthp", 1000)

	calls := 0
	auth := oauthProviderAuth(&calls, 10*60*1000) // ten minutes, short of the hour asked for

	min := int64(60 * 60 * 1000)
	_, err := resolveProviderAuth("oauthp", auth, store, ctx(), &AuthResolutionOverrides{MinOAuthValidityMs: &min})
	var me *ModelsError
	if !errors.As(err, &me) || me.Code != ErrOAuth {
		t.Fatalf("want an ErrOAuth ModelsError, got %v", err)
	}
	if me.Message != "OAuth refresh returned a token that expires too soon for oauthp" {
		t.Fatalf("error message must match pi byte for byte, got %q", me.Message)
	}
}

// TestResolveStoredOAuthDefaultWindowImposesNoContract locks the asymmetry: the
// default window triggers a refresh but never rejects the refreshed token.
func TestResolveStoredOAuthDefaultWindowImposesNoContract(t *testing.T) {
	store := NewInMemoryCredentialStore()
	seedOAuth(store, "oauthp", 1000)

	calls := 0
	auth := oauthProviderAuth(&calls, 60*1000) // still inside the five-minute window

	res, err := resolveProviderAuth("oauthp", auth, store, ctx(), nil)
	if err != nil || res == nil || res.Auth.APIKey != "new" {
		t.Fatalf("default window must not reject a short-lived refreshed token: %+v (err %v)", res, err)
	}
}

// TestResolveStoredOAuthMinValidityCannotShrinkWindow locks the max(): an
// override below five minutes leaves the default window in force.
func TestResolveStoredOAuthMinValidityCannotShrinkWindow(t *testing.T) {
	store := NewInMemoryCredentialStore()
	seedOAuth(store, "oauthp", 2*60*1000) // two minutes: inside the default window only

	calls := 0
	auth := oauthProviderAuth(&calls, 3_600_000)

	min := int64(1000)
	res, err := resolveProviderAuth("oauthp", auth, store, ctx(), &AuthResolutionOverrides{MinOAuthValidityMs: &min})
	if err != nil || res == nil || res.Auth.APIKey != "new" {
		t.Fatalf("a smaller override must not shrink the window: %+v (err %v)", res, err)
	}
	if calls != 1 {
		t.Fatalf("expected exactly one refresh, got %d", calls)
	}
}
