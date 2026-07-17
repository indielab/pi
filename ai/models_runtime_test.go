package ai

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// capture builds ProviderStreams whose Stream records the model + options it
// was handed and returns a closed stream.
func capture(gotModel **Model, gotOpts **StreamOptions) ProviderStreams {
	return ProviderStreams{
		Stream: func(_ context.Context, model *Model, _ Context, opts *StreamOptions) *AssistantMessageEventStream {
			*gotModel = model
			*gotOpts = opts
			s := NewAssistantMessageEventStream()
			s.End()
			return s
		},
	}
}

func TestCreateProviderDispatch(t *testing.T) {
	var gotA, gotB **Model = new(*Model), new(*Model)
	var optsA, optsB **StreamOptions = new(*StreamOptions), new(*StreamOptions)

	p := CreateProvider(CreateProviderOptions{
		ID:   "multi",
		Auth: ProviderAuth{APIKey: EnvAPIKeyAuth("multi", "X")},
		Models: []*Model{
			{Provider: "multi", ID: "a", Api: "api-a"},
			{Provider: "multi", ID: "b", Api: "api-b"},
		},
		APIByApi: map[Api]ProviderStreams{
			"api-a": capture(gotA, optsA),
			"api-b": capture(gotB, optsB),
		},
	})

	p.Stream(context.Background(), &Model{Provider: "multi", ID: "a", Api: "api-a"}, Context{}, nil)
	if *gotA == nil || (*gotA).Api != "api-a" {
		t.Fatalf("api-a not dispatched, got %v", *gotA)
	}
	// A model whose api has no implementation yields a stream error.
	res := p.Stream(context.Background(), &Model{Provider: "multi", ID: "c", Api: "api-z"}, Context{}, nil).Result()
	if res.StopReason != StopError {
		t.Fatalf("missing api should produce a stream error, got %v", res.StopReason)
	}
}

func TestModelsCollection(t *testing.T) {
	m := CreateModels(nil)
	pa := CreateProvider(CreateProviderOptions{ID: "a", Auth: ProviderAuth{APIKey: EnvAPIKeyAuth("a", "A")}, Models: []*Model{{Provider: "a", ID: "m1"}}})
	pb := CreateProvider(CreateProviderOptions{ID: "b", Auth: ProviderAuth{APIKey: EnvAPIKeyAuth("b", "B")}, Models: []*Model{{Provider: "b", ID: "m2"}}})
	m.SetProvider(pa)
	m.SetProvider(pb)

	if ps := m.GetProviders(); len(ps) != 2 || ps[0].ID() != "a" || ps[1].ID() != "b" {
		t.Fatalf("provider order wrong: %v", ps)
	}
	if m.GetProvider("b") == nil {
		t.Fatal("GetProvider(b) nil")
	}
	if all := m.GetModels(""); len(all) != 2 {
		t.Fatalf("GetModels(all) = %d, want 2", len(all))
	}
	if one := m.GetModels("a"); len(one) != 1 || one[0].ID != "m1" {
		t.Fatalf("GetModels(a) wrong: %v", one)
	}
	if m.GetModel("b", "m2") == nil || m.GetModel("b", "nope") != nil {
		t.Fatal("GetModel lookup wrong")
	}

	m.DeleteProvider("a")
	if m.GetProvider("a") != nil || len(m.GetProviders()) != 1 {
		t.Fatal("DeleteProvider failed")
	}
	m.ClearProviders()
	if len(m.GetProviders()) != 0 {
		t.Fatal("ClearProviders failed")
	}
}

func TestModelsApplyAuthEnvKey(t *testing.T) {
	t.Setenv("APPLY_KEY", "from-env")
	var gotModel *Model
	var gotOpts *StreamOptions
	m := CreateModels(nil)
	m.SetProvider(CreateProvider(CreateProviderOptions{
		ID:     "p",
		Auth:   ProviderAuth{APIKey: EnvAPIKeyAuth("p", "APPLY_KEY")},
		Models: []*Model{{Provider: "p", ID: "m", Api: "api"}},
		API:    ptrStreams(capture(&gotModel, &gotOpts)),
	}))

	model := &Model{Provider: "p", ID: "m", Api: "api"}
	m.Stream(context.Background(), model, Context{}, nil)
	if gotOpts == nil || gotOpts.APIKey != "from-env" {
		t.Fatalf("auth key not applied: %+v", gotOpts)
	}
	// Explicit apiKey wins.
	m.Stream(context.Background(), model, Context{}, &ModelsStreamOptions{StreamOptions: StreamOptions{APIKey: "explicit"}})
	if gotOpts.APIKey != "explicit" {
		t.Fatalf("explicit key should win: %q", gotOpts.APIKey)
	}
}

func TestModelsApplyAuthBaseURLHeadersEnv(t *testing.T) {
	var gotModel *Model
	var gotOpts *StreamOptions
	auth := &ApiKeyAuth{
		Name: "custom",
		Resolve: func(_ AuthContext, _ *Credential) (*AuthResult, error) {
			return &AuthResult{
				Auth: ModelAuth{APIKey: "k", BaseURL: "https://auth.example", Headers: map[string]string{"H": "auth", "Keep": "auth"}},
				Env:  map[string]string{"E": "auth", "KeepEnv": "auth"},
			}, nil
		},
	}
	m := CreateModels(nil)
	m.SetProvider(CreateProvider(CreateProviderOptions{
		ID:     "p",
		Auth:   ProviderAuth{APIKey: auth},
		Models: []*Model{{Provider: "p", ID: "m", Api: "api"}},
		API:    ptrStreams(capture(&gotModel, &gotOpts)),
	}))

	model := &Model{Provider: "p", ID: "m", Api: "api", BaseURL: "https://model"}
	m.Stream(context.Background(), model, Context{}, &ModelsStreamOptions{StreamOptions: StreamOptions{
		Headers: map[string]string{"H": "explicit"},
		Env:     map[string]string{"E": "explicit"},
	}})
	if gotModel.BaseURL != "https://auth.example" {
		t.Errorf("auth baseURL should override: %q", gotModel.BaseURL)
	}
	if model.BaseURL != "https://model" {
		t.Errorf("original model must not be mutated: %q", model.BaseURL)
	}
	if gotOpts.Headers["H"] != "explicit" || gotOpts.Headers["Keep"] != "auth" {
		t.Errorf("header merge wrong (explicit wins per key): %v", gotOpts.Headers)
	}
	if gotOpts.Env["E"] != "explicit" || gotOpts.Env["KeepEnv"] != "auth" {
		t.Errorf("env merge wrong (explicit wins per key): %v", gotOpts.Env)
	}
}

func TestModelsUnknownProvider(t *testing.T) {
	m := CreateModels(nil)
	res := m.Stream(context.Background(), &Model{Provider: "ghost", ID: "x", Api: "api"}, Context{}, nil).Result()
	if res.StopReason != StopError {
		t.Fatalf("unknown provider should error, got %v", res.StopReason)
	}
}

func TestModelsGetAuthUnconfigured(t *testing.T) {
	m := CreateModels(nil)
	m.SetProvider(CreateProvider(CreateProviderOptions{
		ID:     "p",
		Auth:   ProviderAuth{APIKey: EnvAPIKeyAuth("p", "DEFINITELY_UNSET_KEY_XYZ")},
		Models: []*Model{{Provider: "p", ID: "m", Api: "api"}},
	}))
	res, err := m.GetAuth(&Model{Provider: "p", ID: "m", Api: "api"}, nil)
	if err != nil || res != nil {
		t.Fatalf("unconfigured provider should resolve (nil, nil), got (%+v, %v)", res, err)
	}
	// Unknown provider also resolves nil.
	if res, err := m.GetAuth(&Model{Provider: "ghost"}, nil); err != nil || res != nil {
		t.Fatalf("unknown provider GetAuth = (%+v, %v)", res, err)
	}
	// Streaming against an unconfigured provider is an error (ff28097a: the
	// pre-facade runtime passed the request through untouched).
	sres := m.Stream(context.Background(), &Model{Provider: "p", ID: "m", Api: "api"}, Context{}, nil).Result()
	if sres.StopReason != StopError || !strings.Contains(sres.ErrorMessage, "Provider is not configured: p") {
		t.Fatalf("unconfigured stream should error with pi's message, got %q / %q", sres.StopReason, sres.ErrorMessage)
	}
}

// fixedAuthContext returns a Models collection whose auth context reads from a
// fixed env map (no OS environment).
func modelsWithEnv(env map[string]string, opts *CreateModelsOptions) MutableModels {
	if opts == nil {
		opts = &CreateModelsOptions{}
	}
	opts.AuthContext = fakeAuthContext{env: env}
	return CreateModels(opts)
}

// TestModelsRefreshDynamic mirrors pi "refresh() updates every configured
// dynamic provider and reports failures": errors are collected per provider
// without failing the sweep, and successful providers still update.
func TestModelsRefreshDynamic(t *testing.T) {
	m := modelsWithEnv(map[string]string{"K": "key"}, nil)
	m.SetProvider(CreateProvider(CreateProviderOptions{
		ID:   "dyn",
		Auth: ProviderAuth{APIKey: EnvAPIKeyAuth("dyn", "K")},
		FetchModels: func(_ context.Context, _ RefreshModelsContext) ([]*Model, error) {
			return []*Model{{Provider: "dyn", ID: "fetched"}}, nil
		},
	}))
	m.SetProvider(CreateProvider(CreateProviderOptions{
		ID:   "boom",
		Auth: ProviderAuth{APIKey: EnvAPIKeyAuth("boom", "K")},
		FetchModels: func(_ context.Context, _ RefreshModelsContext) ([]*Model, error) {
			return nil, errors.New("network")
		},
	}))

	result := m.Refresh(context.Background(), nil)
	if result.Aborted {
		t.Fatal("refresh should not report aborted")
	}
	if got := m.GetModels("dyn"); len(got) != 1 || got[0].ID != "fetched" {
		t.Fatalf("refresh did not update model list: %v", got)
	}
	if err := result.Errors["boom"]; err == nil || !strings.Contains(err.Error(), "network") {
		t.Fatalf("boom's failure should be reported: %v", result.Errors)
	}
	if _, ok := result.Errors["dyn"]; ok {
		t.Fatal("dyn should not report an error")
	}
}

// TestModelsRefreshPersistsAndRestores mirrors pi "persists dynamic catalogs
// and restores them without network access": a refresh writes through the
// ModelsStore, and a network-disabled refresh on a fresh collection restores
// the stored overlay without fetching.
func TestModelsRefreshPersistsAndRestores(t *testing.T) {
	store := NewInMemoryModelsStore()
	env := map[string]string{"K": "key"}
	fetched := []*Model{{Provider: "dyn", ID: "remote"}}
	newProvider := func(calls *int) Provider {
		return CreateProvider(CreateProviderOptions{
			ID:   "dyn",
			Auth: ProviderAuth{APIKey: EnvAPIKeyAuth("dyn", "K")},
			FetchModels: func(_ context.Context, _ RefreshModelsContext) ([]*Model, error) {
				*calls++
				return fetched, nil
			},
		})
	}

	var calls int
	m := modelsWithEnv(env, &CreateModelsOptions{ModelsStore: store})
	m.SetProvider(newProvider(&calls))
	if res := m.Refresh(context.Background(), nil); len(res.Errors) != 0 || calls != 1 {
		t.Fatalf("initial refresh: errors=%v calls=%d", res.Errors, calls)
	}
	stored, _ := store.Read("dyn")
	if stored == nil || len(stored.Models) != 1 || stored.Models[0].ID != "remote" {
		t.Fatalf("refresh must persist through the store: %v", stored)
	}
	if stored.CheckedAt == 0 {
		t.Fatal("a completed remote check must stamp checkedAt (bd9e09db)")
	}

	// A fresh collection sharing the store restores offline.
	var calls2 int
	m2 := modelsWithEnv(env, &CreateModelsOptions{ModelsStore: store})
	m2.SetProvider(newProvider(&calls2))
	allowNetwork := false
	res := m2.Refresh(context.Background(), &ModelsRefreshOptions{AllowNetwork: &allowNetwork})
	if len(res.Errors) != 0 || calls2 != 0 {
		t.Fatalf("offline refresh must not fetch: errors=%v calls=%d", res.Errors, calls2)
	}
	if got := m2.GetModels("dyn"); len(got) != 1 || got[0].ID != "remote" {
		t.Fatalf("offline refresh must restore the stored catalog: %v", got)
	}
}

// TestModelsRefreshSkipsUnconfigured mirrors pi "passes effective API-key
// credentials and skips unconfigured providers".
func TestModelsRefreshSkipsUnconfigured(t *testing.T) {
	var gotCredential *Credential
	m := modelsWithEnv(map[string]string{"CONFIGURED_KEY": "k-123"}, nil)
	m.SetProvider(CreateProvider(CreateProviderOptions{
		ID:   "configured",
		Auth: ProviderAuth{APIKey: EnvAPIKeyAuth("configured", "CONFIGURED_KEY")},
		FetchModels: func(_ context.Context, req RefreshModelsContext) ([]*Model, error) {
			gotCredential = req.Credential
			return nil, nil
		},
	}))
	skipped := false
	m.SetProvider(CreateProvider(CreateProviderOptions{
		ID:   "unconfigured",
		Auth: ProviderAuth{APIKey: EnvAPIKeyAuth("unconfigured", "UNSET_KEY")},
		FetchModels: func(_ context.Context, _ RefreshModelsContext) ([]*Model, error) {
			skipped = true
			return nil, nil
		},
	}))

	result := m.Refresh(context.Background(), nil)
	if len(result.Errors) != 0 {
		t.Fatalf("no errors expected: %v", result.Errors)
	}
	if gotCredential == nil || gotCredential.Type != CredentialAPIKey || gotCredential.Key != "k-123" {
		t.Fatalf("configured provider must receive the effective api-key credential: %+v", gotCredential)
	}
	if skipped {
		t.Fatal("unconfigured provider must be skipped, not fetched")
	}
}

// TestModelsRefreshOAuthBeforeModels mirrors pi "refreshes expired OAuth
// before refreshing models": an expired stored OAuth credential is exchanged
// (and persisted) before the provider fetch sees it.
func TestModelsRefreshOAuthBeforeModels(t *testing.T) {
	creds := NewInMemoryCredentialStore()
	_, _ = creds.Modify("dyn", func(*Credential) (*Credential, error) {
		return &Credential{Type: CredentialOAuth, Refresh: "r0", Access: "old", Expires: 1}, nil
	})
	m := modelsWithEnv(nil, &CreateModelsOptions{Credentials: creds})

	var gotAccess string
	m.SetProvider(CreateProvider(CreateProviderOptions{
		ID: "dyn",
		Auth: ProviderAuth{OAuth: &OAuthAuth{
			Name: "dyn",
			Refresh: func(_ context.Context, c OAuthCredentials) (OAuthCredentials, error) {
				return OAuthCredentials{Refresh: "r1", Access: "new", Expires: nowMillis() + 3_600_000}, nil
			},
			ToAuth: func(c OAuthCredentials) (ModelAuth, error) { return ModelAuth{APIKey: c.Access}, nil },
		}},
		FetchModels: func(_ context.Context, req RefreshModelsContext) ([]*Model, error) {
			if req.Credential != nil {
				gotAccess = req.Credential.Access
			}
			return nil, nil
		},
	}))

	if res := m.Refresh(context.Background(), nil); len(res.Errors) != 0 {
		t.Fatalf("refresh errors: %v", res.Errors)
	}
	if gotAccess != "new" {
		t.Fatalf("fetch must see the refreshed OAuth credential, got %q", gotAccess)
	}
	if stored, _ := creds.Read("dyn"); stored == nil || stored.Access != "new" {
		t.Fatalf("rotated credential must be persisted: %+v", stored)
	}
}

// TestModelsRefreshAborted mirrors pi "returns aborted state without
// reporting cancellation as a provider error".
func TestModelsRefreshAborted(t *testing.T) {
	m := modelsWithEnv(map[string]string{"K": "key"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	m.SetProvider(CreateProvider(CreateProviderOptions{
		ID:   "dyn",
		Auth: ProviderAuth{APIKey: EnvAPIKeyAuth("dyn", "K")},
		FetchModels: func(fctx context.Context, _ RefreshModelsContext) ([]*Model, error) {
			cancel()
			return nil, fctx.Err()
		},
	}))

	result := m.Refresh(ctx, nil)
	if !result.Aborted {
		t.Fatal("cancelled refresh must report aborted")
	}
	if len(result.Errors) != 0 {
		t.Fatalf("cancellation must not be reported as a provider error: %v", result.Errors)
	}
}

// TestModelsCheckAuthAndGetAvailable mirrors pi "checks provider auth without
// refreshing OAuth and filters available models".
func TestModelsCheckAuthAndGetAvailable(t *testing.T) {
	creds := NewInMemoryCredentialStore()
	// Expired OAuth: checkAuth must NOT refresh it.
	_, _ = creds.Modify("oauthp", func(*Credential) (*Credential, error) {
		return &Credential{
			Type: CredentialOAuth, Refresh: "r0", Access: "old", Expires: 1,
			AvailableModelIDs: []string{"kept"},
		}, nil
	})
	m := modelsWithEnv(map[string]string{"K": "key"}, &CreateModelsOptions{Credentials: creds})

	refreshed := false
	m.SetProvider(CreateProvider(CreateProviderOptions{
		ID: "oauthp",
		Auth: ProviderAuth{OAuth: &OAuthAuth{
			Name: "oauthp",
			Refresh: func(_ context.Context, c OAuthCredentials) (OAuthCredentials, error) {
				refreshed = true
				return c, nil
			},
			ToAuth: func(c OAuthCredentials) (ModelAuth, error) { return ModelAuth{APIKey: c.Access}, nil },
		}},
		Models: []*Model{
			{Provider: "oauthp", ID: "kept"},
			{Provider: "oauthp", ID: "dropped"},
		},
		FilterModels: func(models []*Model, credential *Credential) []*Model {
			if credential == nil || credential.AvailableModelIDs == nil {
				return models
			}
			available := map[string]bool{}
			for _, id := range credential.AvailableModelIDs {
				available[id] = true
			}
			var out []*Model
			for _, model := range models {
				if available[model.ID] {
					out = append(out, model)
				}
			}
			return out
		},
	}))
	m.SetProvider(CreateProvider(CreateProviderOptions{
		ID:     "unconfigured",
		Auth:   ProviderAuth{APIKey: EnvAPIKeyAuth("unconfigured", "UNSET_KEY")},
		Models: []*Model{{Provider: "unconfigured", ID: "hidden"}},
	}))
	m.SetProvider(CreateProvider(CreateProviderOptions{
		ID:     "keyed",
		Auth:   ProviderAuth{APIKey: EnvAPIKeyAuth("keyed", "K")},
		Models: []*Model{{Provider: "keyed", ID: "visible"}},
	}))

	check, err := m.CheckAuth("oauthp")
	if err != nil || check == nil || check.Type != CredentialOAuth || check.Source != "OAuth" {
		t.Fatalf("oauth checkAuth = (%+v, %v)", check, err)
	}
	if refreshed {
		t.Fatal("checkAuth must not refresh OAuth")
	}
	if check, _ := m.CheckAuth("unconfigured"); check != nil {
		t.Fatalf("unconfigured checkAuth should be nil, got %+v", check)
	}
	if check, _ := m.CheckAuth("keyed"); check == nil || check.Type != CredentialAPIKey || check.Source != "K" {
		t.Fatalf("keyed checkAuth wrong: %+v", check)
	}
	if check, _ := m.CheckAuth("ghost"); check != nil {
		t.Fatal("unknown provider checkAuth should be nil")
	}

	available, err := m.GetAvailable("")
	if err != nil {
		t.Fatalf("getAvailable: %v", err)
	}
	var ids []string
	for _, model := range available {
		ids = append(ids, model.Provider+"/"+model.ID)
	}
	want := []string{"oauthp/kept", "keyed/visible"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("available = %v, want %v", ids, want)
	}
}

// TestModelsCheckAuthUsesCheckHook locks pi ApiKeyAuth.check: when present it
// is preferred over resolving auth, and its failures wrap as ModelsError.
func TestModelsCheckAuthUsesCheckHook(t *testing.T) {
	resolved := false
	m := modelsWithEnv(nil, nil)
	m.SetProvider(CreateProvider(CreateProviderOptions{
		ID: "cmd",
		Auth: ProviderAuth{APIKey: &ApiKeyAuth{
			Name: "cmd",
			Check: func(_ AuthContext, _ *Credential) (*AuthCheck, error) {
				return &AuthCheck{Source: "command", Type: CredentialAPIKey}, nil
			},
			Resolve: func(_ AuthContext, _ *Credential) (*AuthResult, error) {
				resolved = true
				return &AuthResult{Auth: ModelAuth{APIKey: "side-effect"}}, nil
			},
		}},
	}))

	check, err := m.CheckAuth("cmd")
	if err != nil || check == nil || check.Source != "command" {
		t.Fatalf("check hook result wrong: (%+v, %v)", check, err)
	}
	if resolved {
		t.Fatal("check hook must prevent side-effectful resolve")
	}

	m.SetProvider(CreateProvider(CreateProviderOptions{
		ID: "bad",
		Auth: ProviderAuth{APIKey: &ApiKeyAuth{
			Name:  "bad",
			Check: func(_ AuthContext, _ *Credential) (*AuthCheck, error) { return nil, errors.New("exec failed") },
		}},
	}))
	_, err = m.CheckAuth("bad")
	var me *ModelsError
	if !errors.As(err, &me) || me.Code != ErrAuth || !strings.Contains(me.Message, "API key auth check failed for provider bad") {
		t.Fatalf("check failure should wrap as ModelsError auth: %v", err)
	}
}

// fakeInteraction answers every prompt with a fixed string.
type fakeInteraction struct{ answer string }

func (f fakeInteraction) Prompt(AuthPrompt) (string, error) { return f.answer, nil }
func (f fakeInteraction) Notify(AuthEvent)                  {}

// TestModelsLoginLogout mirrors pi "runs provider login and logout through
// the credential store".
func TestModelsLoginLogout(t *testing.T) {
	creds := NewInMemoryCredentialStore()
	m := modelsWithEnv(nil, &CreateModelsOptions{Credentials: creds})
	m.SetProvider(CreateProvider(CreateProviderOptions{
		ID:   "p",
		Auth: ProviderAuth{APIKey: EnvAPIKeyAuth("p", "P_KEY")},
	}))

	credential, err := m.Login("p", CredentialAPIKey, fakeInteraction{answer: "typed-key"})
	if err != nil || credential == nil || credential.Key != "typed-key" {
		t.Fatalf("login = (%+v, %v)", credential, err)
	}
	if stored, _ := creds.Read("p"); stored == nil || stored.Key != "typed-key" {
		t.Fatalf("login must persist the credential: %+v", stored)
	}

	// Unsupported login type errors with pi's message.
	_, err = m.Login("p", CredentialOAuth, fakeInteraction{})
	var me *ModelsError
	if !errors.As(err, &me) || me.Code != ErrAuth || !strings.Contains(me.Message, "does not support oauth login") {
		t.Fatalf("unsupported login should error: %v", err)
	}
	_, err = m.Login("ghost", CredentialAPIKey, fakeInteraction{})
	if !errors.As(err, &me) || me.Code != ErrProvider {
		t.Fatalf("unknown provider login should error: %v", err)
	}

	if err := m.Logout("p"); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if stored, _ := creds.Read("p"); stored != nil {
		t.Fatalf("logout must delete the credential: %+v", stored)
	}
}

// TestModelsModelHeadersAndTransform mirrors pi "adds model headers only for
// model auth and transforms assembled headers once": GetAuth(model) merges the
// model's static headers case-insensitively; GetProviderAuth does not; the
// Models-only TransformHeaders hook runs on the fully assembled headers and is
// stripped before provider dispatch.
func TestModelsModelHeadersAndTransform(t *testing.T) {
	var gotModel *Model
	var gotOpts *StreamOptions
	m := modelsWithEnv(nil, nil)
	m.SetProvider(CreateProvider(CreateProviderOptions{
		ID: "p",
		Auth: ProviderAuth{APIKey: &ApiKeyAuth{
			Name: "p",
			Resolve: func(_ AuthContext, _ *Credential) (*AuthResult, error) {
				return &AuthResult{Auth: ModelAuth{APIKey: "k", Headers: map[string]string{"X-Auth": "auth", "Keep": "auth"}}}, nil
			},
		}},
		Models: []*Model{{Provider: "p", ID: "m", Api: "api"}},
		API:    ptrStreams(capture(&gotModel, &gotOpts)),
	}))
	model := &Model{
		Provider: "p", ID: "m", Api: "api",
		Headers: map[string]string{"x-auth": "model"},
	}

	// Model auth merges model headers, replacing case-insensitive matches.
	res, err := m.GetAuth(model, nil)
	if err != nil || res == nil {
		t.Fatalf("getAuth(model) = (%+v, %v)", res, err)
	}
	if res.Auth.Headers["x-auth"] != "model" || res.Auth.Headers["Keep"] != "auth" {
		t.Fatalf("model header merge wrong: %v", res.Auth.Headers)
	}
	if _, stale := res.Auth.Headers["X-Auth"]; stale {
		t.Fatalf("case-insensitive replace must drop the stale key: %v", res.Auth.Headers)
	}

	// Provider auth carries no model headers.
	pres, err := m.GetProviderAuth("p", nil)
	if err != nil || pres == nil {
		t.Fatalf("getProviderAuth = (%+v, %v)", pres, err)
	}
	if pres.Auth.Headers["X-Auth"] != "auth" {
		t.Fatalf("provider auth must not merge model headers: %v", pres.Auth.Headers)
	}

	// TransformHeaders sees the assembled headers exactly once.
	transformCalls := 0
	m.Stream(context.Background(), model, Context{}, &ModelsStreamOptions{
		StreamOptions: StreamOptions{Headers: map[string]string{"Req": "1"}},
		ModelsStreamTransforms: ModelsStreamTransforms{
			TransformHeaders: func(headers map[string]string) (map[string]string, error) {
				transformCalls++
				headers["Transformed"] = "yes"
				return headers, nil
			},
		},
	})
	if transformCalls != 1 {
		t.Fatalf("transform must run exactly once, ran %d", transformCalls)
	}
	if gotOpts.Headers["Transformed"] != "yes" || gotOpts.Headers["Req"] != "1" || gotOpts.Headers["x-auth"] != "model" {
		t.Fatalf("transformed headers not dispatched: %v", gotOpts.Headers)
	}
}

// TestCreateProviderDynamicOverlay locks pi createProvider's currentModels
// merge: a dynamic model replaces the baseline entry with its id, new ids
// append, and the baseline survives a cleared overlay.
func TestCreateProviderDynamicOverlay(t *testing.T) {
	fetched := []*Model{
		{Provider: "p", ID: "base", Name: "replaced"},
		{Provider: "p", ID: "extra"},
		{Provider: "other", ID: "foreign"}, // filtered on restore only
	}
	p := CreateProvider(CreateProviderOptions{
		ID:     "p",
		Auth:   ProviderAuth{APIKey: EnvAPIKeyAuth("p", "K")},
		Models: []*Model{{Provider: "p", ID: "base", Name: "baseline"}, {Provider: "p", ID: "static"}},
		FetchModels: func(_ context.Context, _ RefreshModelsContext) ([]*Model, error) {
			return fetched, nil
		},
	})
	if !p.DynamicModels() {
		t.Fatal("provider with FetchModels must report DynamicModels")
	}

	store := NewInMemoryModelsStore()
	err := p.RefreshModels(context.Background(), RefreshModelsContext{
		Store:        providerModelsStore{store: store, id: "p"},
		AllowNetwork: true,
	})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got := p.GetModels()
	var ids []string
	for _, model := range got {
		ids = append(ids, model.ID)
	}
	// base is replaced in place, static kept, extra + foreign appended
	// (pi's fetch path stores the fetched overlay verbatim).
	want := []string{"base", "static", "extra", "foreign"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("merged models = %v, want %v", ids, want)
	}
	if got[0].Name != "replaced" {
		t.Fatalf("dynamic model must replace the baseline entry: %+v", got[0])
	}

	// Restore path filters foreign-provider entries (pi: stored.filter by id).
	p2 := CreateProvider(CreateProviderOptions{
		ID:     "p",
		Auth:   ProviderAuth{APIKey: EnvAPIKeyAuth("p", "K")},
		Models: []*Model{{Provider: "p", ID: "static"}},
		FetchModels: func(_ context.Context, _ RefreshModelsContext) ([]*Model, error) {
			return nil, errors.New("offline")
		},
	})
	err = p2.RefreshModels(context.Background(), RefreshModelsContext{
		Store:        providerModelsStore{store: store, id: "p"},
		AllowNetwork: false,
	})
	if err != nil {
		t.Fatalf("offline refresh must not fetch: %v", err)
	}
	ids = nil
	for _, model := range p2.GetModels() {
		ids = append(ids, model.ID)
	}
	if !reflect.DeepEqual(ids, []string{"static", "base", "extra"}) {
		t.Fatalf("restored models = %v (foreign must be filtered)", ids)
	}
}

func ptrStreams(s ProviderStreams) *ProviderStreams { return &s }
