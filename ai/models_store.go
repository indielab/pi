package ai

import "sync"

// Model-catalog persistence ported from pi packages/ai/src/models-store.ts
// (ff28097a): dynamic providers restore their last-known catalog from a
// ModelsStore and persist refreshed lists back to it.

// ModelsStore is persistent model-catalog storage keyed by provider id. Apps
// inject persistent stores; the default is in-memory. Read returns (nil, nil)
// when nothing is stored.
type ModelsStore interface {
	Read(providerID string) ([]*Model, error)
	Write(providerID string, models []*Model) error
	Delete(providerID string) error
}

// ProviderModelsStore is a ModelsStore scoped to one provider. Providers
// cannot access other providers' catalogs.
type ProviderModelsStore interface {
	Read() ([]*Model, error)
	Write(models []*Model) error
	Delete() error
}

// InMemoryModelsStore is the default in-memory ModelsStore.
//
// pi structuredClones entries on read/write so callers cannot mutate stored
// state. The Go SDK treats *Model as immutable shared catalog pointers
// (Provider.GetModels returns its backing slice by reference), so the store
// copies the slice and shares the Model pointers.
type InMemoryModelsStore struct {
	mu     sync.Mutex
	models map[string][]*Model
}

// NewInMemoryModelsStore returns an empty in-memory store.
func NewInMemoryModelsStore() *InMemoryModelsStore {
	return &InMemoryModelsStore{models: map[string][]*Model{}}
}

// Read returns the stored models for a provider, or (nil, nil) when none.
func (s *InMemoryModelsStore) Read(providerID string) ([]*Model, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.models[providerID]
	if !ok {
		return nil, nil
	}
	out := make([]*Model, len(stored))
	copy(out, stored)
	return out, nil
}

// Write stores the models for a provider, replacing any previous list.
func (s *InMemoryModelsStore) Write(providerID string, models []*Model) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := make([]*Model, len(models))
	copy(stored, models)
	s.models[providerID] = stored
	return nil
}

// Delete removes a provider's stored models.
func (s *InMemoryModelsStore) Delete(providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.models, providerID)
	return nil
}

// providerModelsStore scopes a ModelsStore to one provider id (pi's inline
// store object in ModelsImpl.refresh).
type providerModelsStore struct {
	store ModelsStore
	id    string
}

func (p providerModelsStore) Read() ([]*Model, error)     { return p.store.Read(p.id) }
func (p providerModelsStore) Write(models []*Model) error { return p.store.Write(p.id, models) }
func (p providerModelsStore) Delete() error               { return p.store.Delete(p.id) }
