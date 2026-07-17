package ai

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// Models runtime ported from pi packages/ai/src/models.ts (732bb161; facade
// merge ff28097a): the Provider/Models object-model, createModels/
// createProvider, and auth application. The pre-existing global free functions
// (Stream/GetModel/GetModels/GetProviders/GetEnvApiKey, models.go + stream.go)
// are the compat surface — pi's "@earendil-works/pi-ai/compat" — and stay
// available.
//
// pi defers provider resolution into the returned stream via lazyStream
// (async). The Go port keeps its existing contract (G3, stream.go): resolution
// runs synchronously and failures are encoded as a terminal stream error, so
// applyAuth runs inline and errors flow through errorStream.

// ProviderStreams binds an API's stream implementations (pi ProviderStreams).
type ProviderStreams struct {
	Stream       StreamFunction
	StreamSimple StreamSimpleFunction
}

// RefreshModelsContext is the input to a dynamic provider's model refresh
// (pi RefreshModelsContext). Cancellation travels on the context.Context
// passed to RefreshModels (pi's signal).
type RefreshModelsContext struct {
	// Credential is the effective configured credential. OAuth credentials are
	// refreshed before network access.
	Credential *Credential
	// Store is persistent model storage scoped to this provider id.
	Store ProviderModelsStore
	// AllowNetwork is false during offline/cache-only initialization.
	AllowNetwork bool
	// Force bypasses provider freshness checks and fetches immediately when
	// network access is allowed (pi 97f9978f).
	Force bool
}

// Provider is the concrete runtime unit (pi Provider). It owns id/name/base
// metadata, auth, model listing, and stream behavior.
type Provider interface {
	ID() string
	Name() string
	BaseURL() string
	Headers() map[string]string

	// Auth reports the provider's auth semantics. At least one of
	// APIKey/OAuth is set, even for ambient/keyless providers.
	Auth() ProviderAuth

	// GetModels returns the current known models: the static baseline plus the
	// last-known dynamic overlay. Must not panic.
	GetModels() []*Model

	// DynamicModels reports whether the provider has a dynamic model source
	// (pi: refreshModels !== undefined). Models.Refresh skips providers
	// without one.
	DynamicModels() bool

	// RefreshModels restores the provider-scoped stored catalog and optionally
	// fetches a newer list using the effective credential (dynamic providers
	// only; a no-op otherwise). Implementations must retain their previous
	// list on failure and honor ctx for network requests. Concurrent calls
	// share one in-flight refresh.
	RefreshModels(ctx context.Context, req RefreshModelsContext) error

	// FilterModels applies provider policy for credential-specific model
	// availability (pi Provider.filterModels; identity when the provider has
	// none). GetModels remains the complete synchronous catalog;
	// Models.GetAvailable applies this filter after confirming that provider
	// auth is configured.
	FilterModels(models []*Model, credential *Credential) []*Model

	Stream(ctx context.Context, model *Model, req Context, opts *StreamOptions) *AssistantMessageEventStream
	StreamSimple(ctx context.Context, model *Model, req Context, opts *SimpleStreamOptions) *AssistantMessageEventStream
}

// CreateProviderOptions are the parts createProvider assembles into a Provider.
// Exactly one of API / APIByApi is used: API streams all models; APIByApi
// dispatches on model.Api (a model whose api has no entry produces a stream
// error). FetchModels is nil for static providers.
type CreateProviderOptions struct {
	ID      string
	Name    string
	BaseURL string
	Headers map[string]string
	Auth    ProviderAuth
	// Models is the static baseline model list (empty for purely dynamic
	// providers).
	Models []*Model
	// FetchModels fetches a dynamic model overlay (pi fetchModels).
	// CreateProvider restores/persists it through the ProviderModelsStore.
	FetchModels func(ctx context.Context, req RefreshModelsContext) ([]*Model, error)
	// FilterModels is the optional credential-specific availability policy.
	FilterModels func(models []*Model, credential *Credential) []*Model
	API          *ProviderStreams
	APIByApi     map[Api]ProviderStreams
}

type providerImpl struct {
	id, name, baseURL string
	headers           map[string]string
	auth              ProviderAuth
	single            *ProviderStreams
	byAPI             map[Api]ProviderStreams
	fetchFn           func(ctx context.Context, req RefreshModelsContext) ([]*Model, error)
	filterFn          func(models []*Model, credential *Credential) []*Model

	mu             sync.Mutex
	baseline       []*Model
	dynamic        []*Model
	inflight       chan struct{}
	lastRefreshErr error
}

// CreateProvider builds a Provider from parts (pi createProvider). Built-in
// factories and custom-model providers both go through this.
func CreateProvider(input CreateProviderOptions) Provider {
	name := input.Name
	if name == "" {
		name = input.ID
	}
	return &providerImpl{
		id:       input.ID,
		name:     name,
		baseURL:  input.BaseURL,
		headers:  input.Headers,
		auth:     input.Auth,
		single:   input.API,
		byAPI:    input.APIByApi,
		fetchFn:  input.FetchModels,
		filterFn: input.FilterModels,
		baseline: input.Models,
	}
}

func (p *providerImpl) ID() string                 { return p.id }
func (p *providerImpl) Name() string               { return p.name }
func (p *providerImpl) BaseURL() string            { return p.baseURL }
func (p *providerImpl) Headers() map[string]string { return p.headers }
func (p *providerImpl) Auth() ProviderAuth         { return p.auth }
func (p *providerImpl) DynamicModels() bool        { return p.fetchFn != nil }

// GetModels merges the static baseline with the dynamic overlay: a dynamic
// model replaces the baseline entry with its id, otherwise it is appended
// (pi createProvider currentModels).
func (p *providerImpl) GetModels() []*Model {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.dynamic) == 0 {
		return p.baseline
	}
	merged := make([]*Model, len(p.baseline), len(p.baseline)+len(p.dynamic))
	copy(merged, p.baseline)
	for _, model := range p.dynamic {
		replaced := false
		for i, entry := range merged {
			if entry.ID == model.ID {
				merged[i] = model
				replaced = true
				break
			}
		}
		if !replaced {
			merged = append(merged, model)
		}
	}
	return merged
}

func (p *providerImpl) FilterModels(models []*Model, credential *Credential) []*Model {
	if p.filterFn == nil {
		return models
	}
	return p.filterFn(models, credential)
}

// RefreshModels restores the stored dynamic overlay and, when the network is
// allowed, fetches and persists a fresh one; concurrent callers share the one
// in-flight refresh (pi's inflightRefresh): a joiner blocks until the leader
// completes and returns its error — the joiner's ctx and req are not consulted
// until then, matching pi's shared-promise semantics (an awaited promise can't
// be cancelled or re-parameterized). Static providers (nil fetchFn) are a
// no-op.
func (p *providerImpl) RefreshModels(ctx context.Context, req RefreshModelsContext) error {
	p.mu.Lock()
	if p.fetchFn == nil {
		p.mu.Unlock()
		return nil
	}
	if p.inflight != nil {
		ch := p.inflight
		p.mu.Unlock()
		<-ch
		p.mu.Lock()
		err := p.lastRefreshErr
		p.mu.Unlock()
		return err
	}
	ch := make(chan struct{})
	p.inflight = ch
	fetch := p.fetchFn
	p.mu.Unlock()

	err := func() error {
		stored, err := req.Store.Read()
		if err != nil {
			return err
		}
		if stored != nil {
			restored := make([]*Model, 0, len(stored.Models))
			for _, model := range stored.Models {
				if model.Provider == p.id {
					restored = append(restored, model)
				}
			}
			p.mu.Lock()
			p.dynamic = restored
			p.mu.Unlock()
		}
		if !req.AllowNetwork || ctx.Err() != nil {
			return nil
		}
		refreshed, err := fetch(ctx, req)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		p.mu.Lock()
		p.dynamic = refreshed
		p.mu.Unlock()
		return req.Store.Write(ModelsStoreEntry{Models: refreshed, CheckedAt: nowMillis()})
	}()

	p.mu.Lock()
	p.lastRefreshErr = err
	p.inflight = nil
	close(ch)
	p.mu.Unlock()
	return err
}

// streamsFor selects the ProviderStreams for a model's api.
func (p *providerImpl) streamsFor(model *Model) (ProviderStreams, bool) {
	if p.single != nil {
		return *p.single, true
	}
	s, ok := p.byAPI[model.Api]
	return s, ok
}

func (p *providerImpl) Stream(ctx context.Context, model *Model, req Context, opts *StreamOptions) *AssistantMessageEventStream {
	s, ok := p.streamsFor(model)
	if !ok || s.Stream == nil {
		return errorStream(model, newModelsError(ErrStream, "Provider "+p.id+" has no API implementation for \""+model.Api+"\"", nil))
	}
	return s.Stream(ctx, model, req, opts)
}

func (p *providerImpl) StreamSimple(ctx context.Context, model *Model, req Context, opts *SimpleStreamOptions) *AssistantMessageEventStream {
	s, ok := p.streamsFor(model)
	if !ok || s.StreamSimple == nil {
		return errorStream(model, newModelsError(ErrStream, "Provider "+p.id+" has no API implementation for \""+model.Api+"\"", nil))
	}
	return s.StreamSimple(ctx, model, req, opts)
}

// ModelsRefreshOptions configure Models.Refresh (pi ModelsRefreshOptions).
// Cancellation travels on the context passed to Refresh (pi's signal).
type ModelsRefreshOptions struct {
	// AllowNetwork gates network fetches; nil defaults to true (pi
	// allowNetwork ?? true).
	AllowNetwork *bool
	// Force bypasses provider freshness checks and fetches immediately when
	// network access is allowed (pi 97f9978f).
	Force bool
}

// ModelsRefreshResult reports a refresh sweep (pi ModelsRefreshResult).
// Provider errors and cancellation are returned without failing the sweep.
type ModelsRefreshResult struct {
	Aborted bool
	Errors  map[string]error
}

// ModelsStreamTransforms are Models-only stream hooks (pi
// ModelsStreamTransforms); they are stripped before provider dispatch.
type ModelsStreamTransforms struct {
	// TransformHeaders transforms the fully assembled model/auth/request
	// headers before provider dispatch.
	TransformHeaders func(headers map[string]string) (map[string]string, error)
}

// ModelsStreamOptions are Models.Stream/Complete options: provider stream
// options plus Models-only transforms (pi ModelsApiStreamOptions).
type ModelsStreamOptions struct {
	StreamOptions
	ModelsStreamTransforms
}

// ModelsSimpleStreamOptions are Models.StreamSimple/CompleteSimple options
// (pi ModelsSimpleStreamOptions).
type ModelsSimpleStreamOptions struct {
	SimpleStreamOptions
	ModelsStreamTransforms
}

// Models is the runtime collection of providers plus auth application and
// stream convenience (pi Models). Providers own stream behavior; Models
// resolves auth and delegates each request to the provider that owns the model.
//
// Concurrency: provider registration (SetProvider/DeleteProvider/
// ClearProviders) is guarded but not coordinated with in-flight work —
// register providers before concurrent use; Refresh/Stream and the stores are
// safe once the provider set is stable (mirroring pi's construct-then-use
// object model).
type Models interface {
	GetProviders() []Provider
	GetProvider(id string) Provider

	// GetModels returns last-known models for one provider, or for all when
	// provider is "" (pi getModels(provider?)). Best-effort.
	GetModels(provider string) []*Model
	GetModel(provider, id string) *Model

	// Refresh refreshes every configured dynamic provider concurrently (pi
	// refresh(options?)). Provider errors and cancellation are returned in the
	// result without failing the sweep; static and unconfigured providers are
	// skipped.
	Refresh(ctx context.Context, options *ModelsRefreshOptions) ModelsRefreshResult

	// CheckAuth checks whether a provider has complete auth configuration
	// without refreshing OAuth (pi checkAuth). (nil, nil) when the provider is
	// unknown or unconfigured.
	CheckAuth(providerID string) (*AuthCheck, error)

	// GetAvailable returns models whose providers have complete auth
	// configuration, for one provider or all when providerID is "" (pi
	// getAvailable(providerId?)).
	GetAvailable(providerID string) ([]*Model, error)

	// GetAuth resolves request auth for a model: provider auth plus the
	// model's static headers (pi getAuth(model, overrides?)). Returns
	// (nil, nil) when the provider is unknown or unconfigured; a ModelsError
	// on refresh/store failure.
	GetAuth(model *Model, overrides *AuthResolutionOverrides) (*AuthResult, error)

	// GetProviderAuth resolves provider-scoped auth by provider id (pi's
	// getAuth(providerId, overrides?) overload).
	GetProviderAuth(providerID string, overrides *AuthResolutionOverrides) (*AuthResult, error)

	// Login runs a provider-owned login flow and persists its returned
	// credential (pi login).
	Login(providerID string, authType CredentialKind, interaction AuthInteraction) (*Credential, error)

	// Logout removes the stored credential for a provider (pi logout).
	Logout(providerID string) error

	Stream(ctx context.Context, model *Model, req Context, opts *ModelsStreamOptions) *AssistantMessageEventStream
	Complete(ctx context.Context, model *Model, req Context, opts *ModelsStreamOptions) *AssistantMessage
	StreamSimple(ctx context.Context, model *Model, req Context, opts *ModelsSimpleStreamOptions) *AssistantMessageEventStream
	CompleteSimple(ctx context.Context, model *Model, req Context, opts *ModelsSimpleStreamOptions) *AssistantMessage
}

// MutableModels adds provider mutation (pi MutableModels).
type MutableModels interface {
	Models
	SetProvider(provider Provider)
	DeleteProvider(id string)
	ClearProviders()
}

// CreateModelsOptions configure a Models collection (pi CreateModelsOptions).
type CreateModelsOptions struct {
	Credentials CredentialStore
	ModelsStore ModelsStore
	AuthContext AuthContext
}

type modelsImpl struct {
	mu          sync.RWMutex
	providers   map[string]Provider
	order       []string // insertion order, mirroring pi's Map iteration
	credentials CredentialStore
	modelsStore ModelsStore
	authContext AuthContext
}

// CreateModels builds an empty Models collection (pi createModels). Defaults:
// an InMemoryCredentialStore, an InMemoryModelsStore, and the OS-backed
// AuthContext.
func CreateModels(options *CreateModelsOptions) MutableModels {
	var creds CredentialStore = NewInMemoryCredentialStore()
	var store ModelsStore = NewInMemoryModelsStore()
	var ac AuthContext = DefaultProviderAuthContext()
	if options != nil {
		if options.Credentials != nil {
			creds = options.Credentials
		}
		if options.ModelsStore != nil {
			store = options.ModelsStore
		}
		if options.AuthContext != nil {
			ac = options.AuthContext
		}
	}
	return &modelsImpl{providers: map[string]Provider{}, credentials: creds, modelsStore: store, authContext: ac}
}

func (m *modelsImpl) SetProvider(provider Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.providers[provider.ID()]; !exists {
		m.order = append(m.order, provider.ID())
	}
	m.providers[provider.ID()] = provider
}

func (m *modelsImpl) DeleteProvider(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.providers[id]; !exists {
		return
	}
	delete(m.providers, id)
	for i, pid := range m.order {
		if pid == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
}

func (m *modelsImpl) ClearProviders() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers = map[string]Provider{}
	m.order = nil
}

func (m *modelsImpl) GetProviders() []Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Provider, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.providers[id])
	}
	return out
}

func (m *modelsImpl) GetProvider(id string) Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.providers[id]
}

func (m *modelsImpl) GetModels(provider string) []*Model {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if provider != "" {
		p := m.providers[provider]
		if p == nil {
			return nil
		}
		return p.GetModels()
	}
	var out []*Model
	for _, id := range m.order {
		out = append(out, m.providers[id].GetModels()...)
	}
	return out
}

func (m *modelsImpl) GetModel(provider, id string) *Model {
	for _, model := range m.GetModels(provider) {
		if model.ID == id {
			return model
		}
	}
	return nil
}

// Refresh refreshes every configured dynamic provider concurrently (pi
// ModelsImpl.refresh). Per provider: read the stored credential, resolve the
// effective refresh credential (refreshing expired OAuth under the store
// lock), and hand the provider its scoped store. Failures are collected per
// provider — unless the sweep was cancelled — and a best-effort cache-only
// refresh restores the stored catalog after a failure.
func (m *modelsImpl) Refresh(ctx context.Context, options *ModelsRefreshOptions) ModelsRefreshResult {
	allowNetwork := true
	force := false
	if options != nil {
		if options.AllowNetwork != nil {
			allowNetwork = *options.AllowNetwork
		}
		force = options.Force
	}

	var (
		errsMu sync.Mutex
		errs   = map[string]error{}
		wg     sync.WaitGroup
	)
	for _, provider := range m.GetProviders() {
		if !provider.DynamicModels() {
			continue
		}
		wg.Add(1)
		go func(p Provider) {
			defer wg.Done()
			if ctx.Err() != nil {
				return
			}
			store := providerModelsStore{store: m.modelsStore, id: p.ID()}
			var stored *Credential
			err := func() error {
				var err error
				stored, err = readCredential(m.credentials, p.ID())
				if err != nil {
					return err
				}
				credential, err := m.resolveRefreshCredential(ctx, p, stored, allowNetwork)
				if err != nil {
					return err
				}
				if credential == nil {
					return nil // unconfigured: skip
				}
				return p.RefreshModels(ctx, RefreshModelsContext{
					Credential:   credential,
					Store:        store,
					AllowNetwork: allowNetwork,
					Force:        force,
				})
			}()
			if err != nil {
				if ctx.Err() == nil {
					errsMu.Lock()
					errs[p.ID()] = err
					errsMu.Unlock()
				}
				// Preserve the original auth/network error; cache restoration
				// is best-effort here.
				_ = p.RefreshModels(ctx, RefreshModelsContext{
					Credential:   stored,
					Store:        store,
					AllowNetwork: false,
				})
			}
		}(provider)
	}
	wg.Wait()

	return ModelsRefreshResult{Aborted: ctx.Err() != nil, Errors: errs}
}

// resolveRefreshCredential resolves the effective credential for a model
// refresh (pi resolveRefreshCredential): stored OAuth is refreshed when
// expired (network allowing, under the store lock); otherwise api-key auth
// resolves to a synthetic api_key credential. nil means unconfigured.
func (m *modelsImpl) resolveRefreshCredential(
	ctx context.Context,
	p Provider,
	stored *Credential,
	allowNetwork bool,
) (*Credential, error) {
	if stored != nil && stored.Type == CredentialOAuth {
		oauth := p.Auth().OAuth
		if oauth == nil {
			return nil, nil
		}
		if !allowNetwork || nowMillis() < stored.Expires {
			return stored, nil
		}
		if ctx.Err() != nil {
			return nil, nil
		}
		post, err := m.credentials.Modify(p.ID(), func(current *Credential) (*Credential, error) {
			if current == nil || current.Type != CredentialOAuth || nowMillis() < current.Expires {
				return nil, nil
			}
			refreshed, rerr := oauth.Refresh(ctx, current.OAuthCredentials())
			if rerr != nil {
				return nil, rerr
			}
			return oauthCredential(refreshed), nil
		})
		if err != nil {
			return nil, err
		}
		if post == nil || post.Type != CredentialOAuth {
			return nil, nil
		}
		return post, nil
	}

	apiKey := p.Auth().APIKey
	if apiKey == nil {
		return nil, nil
	}
	var credential *Credential
	if stored != nil && stored.Type == CredentialAPIKey {
		credential = stored
	}
	result, err := apiKey.Resolve(m.authContext, credential)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return &Credential{Type: CredentialAPIKey, Key: result.Auth.APIKey, Env: result.Env}, nil
}

// checkProviderAuth checks auth configuration without refreshing OAuth
// (pi checkProviderAuth).
func (m *modelsImpl) checkProviderAuth(p Provider, credential *Credential) (*AuthCheck, error) {
	if credential != nil && credential.Type == CredentialOAuth {
		if p.Auth().OAuth != nil {
			return &AuthCheck{Source: "OAuth", Type: CredentialOAuth}, nil
		}
		return nil, nil
	}
	apiKey := p.Auth().APIKey
	if apiKey == nil {
		return nil, nil
	}
	if apiKey.Check != nil {
		var cred *Credential
		if credential != nil && credential.Type == CredentialAPIKey {
			cred = credential
		}
		check, err := apiKey.Check(m.authContext, cred)
		if err != nil {
			return nil, newModelsError(ErrAuth, "API key auth check failed for provider "+p.ID(), err)
		}
		return check, nil
	}

	resolution, err := resolveProviderAuth(p.ID(), p.Auth(), m.credentials, m.authContext, nil)
	if err != nil {
		return nil, err
	}
	if resolution == nil {
		return nil, nil
	}
	return &AuthCheck{Source: resolution.Source, Type: CredentialAPIKey}, nil
}

func (m *modelsImpl) CheckAuth(providerID string) (*AuthCheck, error) {
	p := m.GetProvider(providerID)
	if p == nil {
		return nil, nil
	}
	credential, err := readCredential(m.credentials, providerID)
	if err != nil {
		return nil, err
	}
	return m.checkProviderAuth(p, credential)
}

func (m *modelsImpl) GetAvailable(providerID string) ([]*Model, error) {
	var providers []Provider
	if providerID != "" {
		if p := m.GetProvider(providerID); p != nil {
			providers = []Provider{p}
		}
	} else {
		providers = m.GetProviders()
	}
	var out []*Model
	for _, p := range providers {
		credential, err := readCredential(m.credentials, p.ID())
		if err != nil {
			return nil, err
		}
		auth, err := m.checkProviderAuth(p, credential)
		if err != nil {
			return nil, err
		}
		if auth == nil {
			continue
		}
		out = append(out, p.FilterModels(p.GetModels(), credential)...)
	}
	return out, nil
}

func (m *modelsImpl) GetProviderAuth(providerID string, overrides *AuthResolutionOverrides) (*AuthResult, error) {
	p := m.GetProvider(providerID)
	if p == nil {
		return nil, nil
	}
	return resolveProviderAuth(p.ID(), p.Auth(), m.credentials, m.authContext, overrides)
}

// GetAuth resolves provider auth for a model and merges the model's static
// headers on top (pi getAuth(model, overrides?)).
func (m *modelsImpl) GetAuth(model *Model, overrides *AuthResolutionOverrides) (*AuthResult, error) {
	result, err := m.GetProviderAuth(model.Provider, overrides)
	if err != nil || result == nil {
		return result, err
	}
	if len(model.Headers) == 0 {
		return result, nil
	}
	merged := *result
	merged.Auth.Headers = mergeHeaders(result.Auth.Headers, model.Headers)
	return &merged, nil
}

func (m *modelsImpl) Login(providerID string, authType CredentialKind, interaction AuthInteraction) (*Credential, error) {
	p := m.GetProvider(providerID)
	if p == nil {
		return nil, newModelsError(ErrProvider, "Unknown provider: "+providerID, nil)
	}
	var login func(AuthInteraction) (*Credential, error)
	if authType == CredentialOAuth {
		if oauth := p.Auth().OAuth; oauth != nil {
			login = oauth.Login
		}
	} else if apiKey := p.Auth().APIKey; apiKey != nil {
		login = apiKey.Login
	}
	if login == nil {
		return nil, newModelsError(ErrAuth, p.Name()+" does not support "+string(authType)+" login", nil)
	}
	credential, err := login(interaction)
	if err != nil {
		return nil, err
	}
	if _, err := m.credentials.Modify(providerID, func(*Credential) (*Credential, error) {
		return credential, nil
	}); err != nil {
		return nil, newModelsError(ErrAuth, "Credential store modify failed for "+providerID, err)
	}
	return credential, nil
}

func (m *modelsImpl) Logout(providerID string) error {
	if err := m.credentials.Delete(providerID); err != nil {
		return newModelsError(ErrAuth, "Credential store delete failed for "+providerID, err)
	}
	return nil
}

// applyAuth resolves auth and folds it into the request model + options
// (pi applyAuth). Explicit request options win per field; headers merge
// case-insensitively, env merges per key, and the Models-only header
// transform runs last. An unconfigured provider is an error (ff28097a; the
// pre-facade runtime passed the request through untouched).
func (m *modelsImpl) applyAuth(
	model *Model,
	opts *StreamOptions,
	transforms ModelsStreamTransforms,
) (*Model, *StreamOptions, error) {
	var overrides *AuthResolutionOverrides
	if opts != nil {
		overrides = &AuthResolutionOverrides{APIKey: opts.APIKey, Env: opts.Env}
	}
	resolution, err := m.GetAuth(model, overrides)
	if err != nil {
		return nil, nil, err
	}
	if resolution == nil {
		return nil, nil, newModelsError(ErrAuth, "Provider is not configured: "+model.Provider, nil)
	}
	auth := resolution.Auth

	ro := StreamOptions{}
	if opts != nil {
		ro = *opts
	}
	if ro.APIKey == "" { // options?.apiKey ?? auth.apiKey
		ro.APIKey = auth.APIKey
	}
	headers := mergeHeaders(auth.Headers, ro.Headers)
	if transforms.TransformHeaders != nil {
		if headers == nil {
			headers = map[string]string{} // pi: transformHeaders(headers ?? {})
		}
		headers, err = transforms.TransformHeaders(headers)
		if err != nil {
			return nil, nil, err
		}
	}
	ro.Headers = headers
	ro.Env = mergeStringMap(resolution.Env, ro.Env) // explicit env override

	requestModel := model
	if auth.BaseURL != "" {
		clone := *model
		clone.BaseURL = auth.BaseURL
		requestModel = &clone
	}
	return requestModel, &ro, nil
}

func (m *modelsImpl) Stream(ctx context.Context, model *Model, req Context, opts *ModelsStreamOptions) *AssistantMessageEventStream {
	p := m.GetProvider(model.Provider)
	if p == nil {
		return errorStream(model, newModelsError(ErrProvider, "Unknown provider: "+model.Provider, nil))
	}
	var base *StreamOptions
	var transforms ModelsStreamTransforms
	if opts != nil {
		base = &opts.StreamOptions
		transforms = opts.ModelsStreamTransforms
	}
	requestModel, requestOptions, err := m.applyAuth(model, base, transforms)
	if err != nil {
		return errorStream(model, err)
	}
	return p.Stream(ctx, requestModel, req, requestOptions)
}

func (m *modelsImpl) Complete(ctx context.Context, model *Model, req Context, opts *ModelsStreamOptions) *AssistantMessage {
	return m.Stream(ctx, model, req, opts).Result()
}

func (m *modelsImpl) StreamSimple(ctx context.Context, model *Model, req Context, opts *ModelsSimpleStreamOptions) *AssistantMessageEventStream {
	p := m.GetProvider(model.Provider)
	if p == nil {
		return errorStream(model, newModelsError(ErrProvider, "Unknown provider: "+model.Provider, nil))
	}
	var base *StreamOptions
	var transforms ModelsStreamTransforms
	if opts != nil {
		base = &opts.SimpleStreamOptions.StreamOptions
		transforms = opts.ModelsStreamTransforms
	}
	requestModel, requestOptions, err := m.applyAuth(model, base, transforms)
	if err != nil {
		return errorStream(model, err)
	}
	simple := SimpleStreamOptions{}
	if opts != nil {
		simple = opts.SimpleStreamOptions
	}
	if requestOptions != nil {
		simple.StreamOptions = *requestOptions
	}
	return p.StreamSimple(ctx, requestModel, req, &simple)
}

func (m *modelsImpl) CompleteSimple(ctx context.Context, model *Model, req Context, opts *ModelsSimpleStreamOptions) *AssistantMessage {
	return m.StreamSimple(ctx, model, req, opts).Result()
}

// HasApi reports whether a model uses the given api (pi hasApi narrowing).
func HasApi(model *Model, api Api) bool {
	return model.Api == api
}

// mergeHeaders returns base overlaid with override, deleting base entries
// whose names match an override key case-insensitively before setting it
// (pi models.ts mergeHeaders). nil when both inputs are nil. Override keys
// are applied in sorted order so case-colliding overrides merge
// deterministically (pi iterates insertion order; Go maps are unordered).
// The nested scan is O(n*m) — fine for header-sized maps.
func mergeHeaders(base, override map[string]string) map[string]string {
	if base == nil && override == nil {
		return nil
	}
	merged := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		merged[k] = v
	}
	names := make([]string, 0, len(override))
	for name := range override {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		lower := strings.ToLower(name)
		for existing := range merged {
			if strings.ToLower(existing) == lower {
				delete(merged, existing)
			}
		}
		merged[name] = override[name]
	}
	return merged
}

// mergeStringMap returns {...base, ...override} or nil when both are empty.
// override wins per key.
func mergeStringMap(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}
