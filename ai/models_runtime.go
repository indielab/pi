package ai

import (
	"context"
	"errors"
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

// ModelsPublication is one atomic catalog publication (pi ModelsPublication,
// upstream fed6009c). Persistence policy stays provider-owned; Update runs
// synchronously right after the selected persistence mutation, so a provider's
// in-memory catalog can never disagree with what was just stored.
type ModelsPublication struct {
	// Persist writes this entry for the provider. Nil leaves storage unchanged
	// unless DeletePersisted is set (pi: persist omitted / an entry / null).
	Persist *ModelsStoreEntry
	// DeletePersisted deletes the provider's stored entry (pi persist: null).
	// Ignored when Persist is non-nil.
	DeletePersisted bool
	// Update applies provider-private in-memory catalog state. It runs
	// synchronously under the publication lock, only after the persistence
	// mutation succeeded and only while the refresh is still current.
	Update func()
}

// RefreshModelsContext is the input to a dynamic provider's model refresh
// (pi RefreshModelsContext). Cancellation travels on the context.Context
// passed to RefreshModels (pi's signal, which fed6009c made required — the
// Models runtime always supplies a live context).
type RefreshModelsContext struct {
	// Credential is the effective configured credential. OAuth credentials are
	// refreshed before network access.
	Credential *Credential
	// Stored is an immutable provider-scoped catalog snapshot captured before
	// this refresh phase; nil when nothing is stored (pi's `stored`, which
	// replaced the mutable per-provider store handle).
	Stored *ModelsStoreEntry
	// Publish is generation-checked publication. It reports false when the
	// refresh has been superseded by a newer one — the caller must then stop —
	// and an error when publication was cancelled or storage failed.
	Publish func(publication ModelsPublication) (bool, error)
	// AllowNetwork is false during offline/cache-only initialization.
	AllowNetwork bool
	// Force bypasses provider freshness checks and fetches immediately when
	// network access is allowed (pi 97f9978f).
	Force bool
}

// publish routes through Publish, reporting a resolvable error when a caller
// built the context by hand instead of going through Models.Refresh.
func (c RefreshModelsContext) publish(publication ModelsPublication) (bool, error) {
	if c.Publish == nil {
		return false, errors.New(
			"RefreshModelsContext.Publish is nil: obtain the refresh context from Models.Refresh, " +
				"which supplies generation-checked publication")
	}
	return c.Publish(publication)
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

	// RefreshModels restores req.Stored and optionally fetches a newer list
	// using the effective credential (dynamic providers only; a no-op
	// otherwise). Implementations must retain their previous list on failure,
	// publish persistence and in-memory state through req.Publish, and honor
	// ctx for blocking work.
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
	// CreateProvider restores it from the snapshot and publishes the fetched
	// list transactionally.
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

	mu       sync.Mutex
	baseline []*Model
	dynamic  []*Model
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

// setDynamic replaces the dynamic overlay.
func (p *providerImpl) setDynamic(models []*Model) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dynamic = models
}

// RefreshModels restores the stored dynamic overlay and, when the network is
// allowed, fetches a fresh one and publishes it (pi createProvider's
// refreshModels after fed6009c). Both the restore and the fetched list go
// through req.Publish, so the in-memory overlay and the persisted catalog move
// together and a superseded refresh stops instead of clobbering a newer one.
// pi's inflightRefresh dedup is gone upstream — a newer refresh now supersedes
// an older one rather than joining it. Static providers (nil fetchFn) are a
// no-op.
func (p *providerImpl) RefreshModels(ctx context.Context, req RefreshModelsContext) error {
	if p.fetchFn == nil {
		return nil
	}

	if req.Stored != nil {
		restored := make([]*Model, 0, len(req.Stored.Models))
		for _, model := range req.Stored.Models {
			if model.Provider == p.id {
				restored = append(restored, model)
			}
		}
		published, err := req.publish(ModelsPublication{Update: func() { p.setDynamic(restored) }})
		if err != nil {
			return err
		}
		if !published {
			return nil
		}
	}

	if !req.AllowNetwork || ctx.Err() != nil {
		return nil
	}
	refreshed, err := p.fetchFn(ctx, req)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return nil
	}
	_, err = req.publish(ModelsPublication{
		Persist: &ModelsStoreEntry{Models: refreshed, CheckedAt: nowMillis()},
		Update:  func() { p.setDynamic(refreshed) },
	})
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
	// Providers restricts the refresh to these provider ids; nil refreshes
	// every dynamic provider. Unknown and static ids are ignored (fed6009c).
	Providers []string
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
// ClearProviders) supersedes any in-flight refresh for the affected provider,
// so a catalog published by work that started against an older provider set is
// dropped rather than applied (pi fed6009c).
type Models interface {
	GetProviders() []Provider
	GetProvider(id string) Provider

	// GetModels returns last-known models for one provider, or for all when
	// provider is "" (pi getModels(provider?)). Best-effort.
	GetModels(provider string) []*Model
	GetModel(provider, id string) *Model

	// Refresh refreshes the selected configured dynamic providers concurrently,
	// or every one when Providers is unset (pi refresh(options?)). Provider
	// errors and cancellation are returned in the result without failing the
	// sweep; static, unknown, and unconfigured providers are skipped.
	Refresh(ctx context.Context, options *ModelsRefreshOptions) ModelsRefreshResult

	// CheckAuth checks whether a provider has complete auth configuration
	// without refreshing OAuth (pi checkAuth). (nil, nil) when the provider is
	// unknown or unconfigured.
	CheckAuth(ctx context.Context, providerID string) (*AuthCheck, error)

	// GetAvailable returns models whose providers have complete auth
	// configuration, for one provider or all when providerID is "" (pi
	// getAvailable(providerId?)).
	GetAvailable(ctx context.Context, providerID string) ([]*Model, error)

	// GetAuth resolves request auth for a model: provider auth plus the
	// model's static headers (pi getAuth(model, overrides?)). Returns
	// (nil, nil) when the provider is unknown or unconfigured; a ModelsError
	// on refresh/store failure.
	GetAuth(ctx context.Context, model *Model, overrides *AuthResolutionOverrides) (*AuthResult, error)

	// GetProviderAuth resolves provider-scoped auth by provider id (pi's
	// getAuth(providerId, overrides?) overload).
	GetProviderAuth(ctx context.Context, providerID string, overrides *AuthResolutionOverrides) (*AuthResult, error)

	// Login runs a provider-owned login flow and persists its returned
	// credential (pi login). ctx cancels the flow; a mutation cancelled before
	// it reached the store never runs.
	Login(ctx context.Context, providerID string, authType CredentialKind, interaction AuthInteraction) (*Credential, error)

	// Logout removes the stored credential for a provider (pi logout).
	Logout(ctx context.Context, providerID string) error

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

	// refreshMu guards the per-provider refresh generation counters and the
	// cancel funcs of the refreshes currently in flight (pi's
	// refreshGenerations / refreshControllers).
	refreshMu     sync.Mutex
	refreshGens   map[string]uint64
	refreshCancel map[string]*providerRefresh
	// publishQueue serializes publication per provider (pi's publicationChains).
	publishQueue *keyedLock
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
	return &modelsImpl{
		providers:     map[string]Provider{},
		credentials:   creds,
		modelsStore:   store,
		authContext:   ac,
		refreshGens:   map[string]uint64{},
		refreshCancel: map[string]*providerRefresh{},
		publishQueue:  newKeyedLock(),
	}
}

func (m *modelsImpl) SetProvider(provider Provider) {
	m.supersedeProviderRefresh(provider.ID())
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.providers[provider.ID()]; !exists {
		m.order = append(m.order, provider.ID())
	}
	m.providers[provider.ID()] = provider
}

func (m *modelsImpl) DeleteProvider(id string) {
	m.supersedeProviderRefresh(id)
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
	m.mu.RLock()
	ids := make([]string, 0, len(m.order))
	ids = append(ids, m.order...)
	m.mu.RUnlock()
	for _, id := range ids {
		m.supersedeProviderRefresh(id)
	}
	m.supersedeAllProviderRefreshes()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers = map[string]Provider{}
	m.order = nil
}

// providerRefresh is one in-flight provider refresh, identified by pointer (pi
// compares AbortController identity).
type providerRefresh struct{ cancel context.CancelFunc }

// supersedeProviderRefresh bumps a provider's refresh generation and cancels
// the refresh currently in flight for it, returning the new generation (pi
// supersedeProviderRefresh). Publications carrying an older generation are
// dropped.
func (m *modelsImpl) supersedeProviderRefresh(providerID string) uint64 {
	m.refreshMu.Lock()
	generation := m.refreshGens[providerID] + 1
	m.refreshGens[providerID] = generation
	previous := m.refreshCancel[providerID]
	delete(m.refreshCancel, providerID)
	m.refreshMu.Unlock()
	if previous != nil {
		previous.cancel()
	}
	return generation
}

// supersedeAllProviderRefreshes supersedes every refresh still in flight,
// including ones whose provider is already gone from the collection (pi
// clearProviders unions the provider ids with the live controller keys).
func (m *modelsImpl) supersedeAllProviderRefreshes() {
	m.refreshMu.Lock()
	ids := make([]string, 0, len(m.refreshCancel))
	for id := range m.refreshCancel {
		ids = append(ids, id)
	}
	m.refreshMu.Unlock()
	for _, id := range ids {
		m.supersedeProviderRefresh(id)
	}
}

// beginProviderRefresh supersedes any refresh in flight for the provider and
// registers a fresh generation plus its cancellable context (pi
// beginProviderRefresh). The returned cancel must be passed to
// endProviderRefresh.
func (m *modelsImpl) beginProviderRefresh(ctx context.Context, providerID string) (uint64, context.Context, *providerRefresh) {
	generation := m.supersedeProviderRefresh(providerID)
	refreshCtx, cancel := context.WithCancel(ctx)
	entry := &providerRefresh{cancel: cancel}
	m.refreshMu.Lock()
	m.refreshCancel[providerID] = entry
	m.refreshMu.Unlock()
	return generation, refreshCtx, entry
}

// endProviderRefresh releases a finished refresh's context, deregistering it
// only if it is still the current one.
func (m *modelsImpl) endProviderRefresh(providerID string, entry *providerRefresh) {
	m.refreshMu.Lock()
	if m.refreshCancel[providerID] == entry {
		delete(m.refreshCancel, providerID)
	}
	m.refreshMu.Unlock()
	entry.cancel()
}

// currentRefreshGeneration reports a provider's latest refresh generation.
func (m *modelsImpl) currentRefreshGeneration(providerID string) uint64 {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	return m.refreshGens[providerID]
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

// publishProviderModels performs one generation-checked publication (pi
// publishProviderModels). Publications for a provider are serialized; a
// publication whose refresh has been superseded reports false and mutates
// nothing, and Update runs only after the persistence mutation succeeded and
// only while the refresh is still current.
func (m *modelsImpl) publishProviderModels(
	ctx context.Context,
	providerID string,
	generation uint64,
	publication ModelsPublication,
) (bool, error) {
	if err := m.publishQueue.lock(ctx, providerID); err != nil {
		return false, err
	}
	defer m.publishQueue.unlock(providerID)

	if err := ctx.Err(); err != nil {
		return false, err
	}
	if m.currentRefreshGeneration(providerID) != generation {
		return false, nil
	}

	switch {
	case publication.Persist != nil:
		if err := m.modelsStore.Write(ctx, providerID, *publication.Persist.clone()); err != nil {
			return false, err
		}
	case publication.DeletePersisted:
		if err := m.modelsStore.Delete(ctx, providerID); err != nil {
			return false, err
		}
	}

	if err := ctx.Err(); err != nil {
		return false, err
	}
	if m.currentRefreshGeneration(providerID) != generation {
		return false, nil
	}
	if publication.Update != nil {
		publication.Update()
	}
	return true, nil
}

// runProviderRefreshPhase runs one refresh phase for a provider: snapshot the
// stored catalog, then hand the provider that snapshot plus a
// generation-checked publish (pi runProviderRefreshPhase).
func (m *modelsImpl) runProviderRefreshPhase(
	ctx context.Context,
	p Provider,
	credential *Credential,
	allowNetwork bool,
	force bool,
	generation uint64,
) error {
	stored, err := m.modelsStore.Read(ctx, p.ID())
	if err != nil {
		return err
	}
	return p.RefreshModels(ctx, RefreshModelsContext{
		Credential: credential,
		Stored:     stored.clone(),
		Publish: func(pub ModelsPublication) (bool, error) {
			return m.publishProviderModels(ctx, p.ID(), generation, pub)
		},
		AllowNetwork: allowNetwork,
		Force:        force,
	})
}

// Refresh refreshes the selected configured dynamic providers concurrently (pi
// ModelsImpl.refresh). Each provider first runs a cache-only phase that
// restores its stored catalog before any auth resolution or network access,
// then — when the network is allowed and the provider is configured — a network
// phase. Failures are collected per provider unless that provider's refresh was
// cancelled or superseded. The sweep stops waiting as soon as ctx is done and
// returns a snapshot of the errors collected so far; provider work abandoned
// that way cannot publish, because its generation is no longer current.
func (m *modelsImpl) Refresh(ctx context.Context, options *ModelsRefreshOptions) ModelsRefreshResult {
	allowNetwork := true
	force := false
	var selected map[string]bool
	if options != nil {
		if options.AllowNetwork != nil {
			allowNetwork = *options.AllowNetwork
		}
		force = options.Force
		if options.Providers != nil {
			selected = make(map[string]bool, len(options.Providers))
			for _, id := range options.Providers {
				selected[id] = true
			}
		}
	}

	var (
		errsMu sync.Mutex
		errs   = map[string]error{}
		wg     sync.WaitGroup
	)
	if ctx.Err() != nil {
		return ModelsRefreshResult{Aborted: true, Errors: errs}
	}

	for _, provider := range m.GetProviders() {
		if !provider.DynamicModels() || (selected != nil && !selected[provider.ID()]) {
			continue
		}
		generation, refreshCtx, entry := m.beginProviderRefresh(ctx, provider.ID())
		wg.Add(1)
		go func(p Provider) {
			defer wg.Done()
			defer m.endProviderRefresh(p.ID(), entry)

			err := m.refreshProvider(refreshCtx, p, allowNetwork, force, generation)
			if err != nil && refreshCtx.Err() == nil {
				errsMu.Lock()
				errs[p.ID()] = err
				errsMu.Unlock()
			}
		}(provider)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}

	errsMu.Lock()
	defer errsMu.Unlock()
	snapshot := make(map[string]error, len(errs))
	for id, err := range errs {
		snapshot[id] = err
	}
	return ModelsRefreshResult{Aborted: ctx.Err() != nil, Errors: snapshot}
}

// refreshProvider runs one provider's cache phase and, when allowed, its
// network phase. A credential-store failure is reported only after the cache
// phase has had its chance to restore the last-known catalog.
func (m *modelsImpl) refreshProvider(
	ctx context.Context,
	p Provider,
	allowNetwork bool,
	force bool,
	generation uint64,
) error {
	stored, credentialErr := readCredential(ctx, m.credentials, p.ID())

	// Restore cached provider state before auth resolution or network access.
	if err := m.runProviderRefreshPhase(ctx, p, stored, false, false, generation); err != nil {
		return err
	}
	if credentialErr != nil {
		return credentialErr
	}
	if !allowNetwork || ctx.Err() != nil {
		return nil
	}

	credential, err := m.resolveRefreshCredential(ctx, p, stored)
	if err != nil {
		return err
	}
	if credential == nil {
		return nil // unconfigured: skip
	}
	return m.runProviderRefreshPhase(ctx, p, credential, true, force, generation)
}

// resolveRefreshCredential resolves the effective credential for a model
// refresh (pi resolveRefreshCredential): stored OAuth is refreshed when
// expired (under the store lock); otherwise api-key auth resolves to a
// synthetic api_key credential. nil means unconfigured. It only runs in the
// network phase, so pi's allowNetwork parameter is gone (fed6009c).
func (m *modelsImpl) resolveRefreshCredential(
	ctx context.Context,
	p Provider,
	stored *Credential,
) (*Credential, error) {
	if stored != nil && stored.Type == CredentialOAuth {
		oauth := p.Auth().OAuth
		if oauth == nil {
			return nil, nil
		}
		if nowMillis() < stored.Expires {
			return stored, nil
		}
		if ctx.Err() != nil {
			return nil, nil
		}
		post, err := m.credentials.Modify(ctx, p.ID(), func(current *Credential) (*Credential, error) {
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
	result, err := apiKey.Resolve(ctx, m.authContext, credential)
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
func (m *modelsImpl) checkProviderAuth(ctx context.Context, p Provider, credential *Credential) (*AuthCheck, error) {
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
		check, err := apiKey.Check(ctx, m.authContext, cred)
		if err != nil {
			if isCancellation(ctx, err) {
				return nil, err
			}
			return nil, newModelsError(ErrAuth, "API key auth check failed for provider "+p.ID(), err)
		}
		return check, nil
	}

	resolution, err := resolveProviderAuth(ctx, p.ID(), p.Auth(), m.credentials, m.authContext, nil)
	if err != nil {
		return nil, err
	}
	if resolution == nil {
		return nil, nil
	}
	return &AuthCheck{Source: resolution.Source, Type: CredentialAPIKey}, nil
}

func (m *modelsImpl) CheckAuth(ctx context.Context, providerID string) (*AuthCheck, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p := m.GetProvider(providerID)
	if p == nil {
		return nil, nil
	}
	credential, err := readCredential(ctx, m.credentials, providerID)
	if err != nil {
		return nil, err
	}
	return m.checkProviderAuth(ctx, p, credential)
}

func (m *modelsImpl) GetAvailable(ctx context.Context, providerID string) ([]*Model, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
		credential, err := readCredential(ctx, m.credentials, p.ID())
		if err != nil {
			return nil, err
		}
		auth, err := m.checkProviderAuth(ctx, p, credential)
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

func (m *modelsImpl) GetProviderAuth(ctx context.Context, providerID string, overrides *AuthResolutionOverrides) (*AuthResult, error) {
	p := m.GetProvider(providerID)
	if p == nil {
		return nil, nil
	}
	return resolveProviderAuth(ctx, p.ID(), p.Auth(), m.credentials, m.authContext, overrides)
}

// GetAuth resolves provider auth for a model and merges the model's static
// headers on top (pi getAuth(model, overrides?)).
func (m *modelsImpl) GetAuth(ctx context.Context, model *Model, overrides *AuthResolutionOverrides) (*AuthResult, error) {
	result, err := m.GetProviderAuth(ctx, model.Provider, overrides)
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

func (m *modelsImpl) Login(
	ctx context.Context,
	providerID string,
	authType CredentialKind,
	interaction AuthInteraction,
) (*Credential, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p := m.GetProvider(providerID)
	if p == nil {
		return nil, newModelsError(ErrProvider, "Unknown provider: "+providerID, nil)
	}
	var login func(context.Context, AuthInteraction) (*Credential, error)
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
	credential, err := login(ctx, interaction)
	if err != nil {
		return nil, err
	}
	// A mutation cancelled while still queued never runs; one that has reached
	// the store settles before the caller is released (pi's mutationStarted
	// hand-off, expressed by the store's cancellable queue).
	if _, err := m.credentials.Modify(ctx, providerID, func(*Credential) (*Credential, error) {
		return credential, nil
	}); err != nil {
		if isCancellation(ctx, err) {
			return nil, err
		}
		return nil, newModelsError(ErrAuth, "Credential store modify failed for "+providerID, err)
	}
	return credential, nil
}

func (m *modelsImpl) Logout(ctx context.Context, providerID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.credentials.Delete(ctx, providerID); err != nil {
		if isCancellation(ctx, err) {
			return err
		}
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
	ctx context.Context,
	model *Model,
	opts *StreamOptions,
	transforms ModelsStreamTransforms,
) (*Model, *StreamOptions, error) {
	var overrides *AuthResolutionOverrides
	if opts != nil {
		overrides = &AuthResolutionOverrides{APIKey: opts.APIKey, Env: opts.Env}
	}
	resolution, err := m.GetAuth(ctx, model, overrides)
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
	requestModel, requestOptions, err := m.applyAuth(ctx, model, base, transforms)
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
	requestModel, requestOptions, err := m.applyAuth(ctx, model, base, transforms)
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
