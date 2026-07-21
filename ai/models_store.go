package ai

import "sync"

// Model-catalog persistence ported from pi packages/ai/src/models-store.ts
// (ff28097a; entry shape bd9e09db): dynamic providers restore their last-known
// catalog from a ModelsStore and persist refreshed lists back to it.

// ModelsStoreEntry is one provider's stored catalog (pi ModelsStoreEntry).
type ModelsStoreEntry struct {
	Models []*Model `json:"models"`
	// LastModified is the Unix-millisecond timestamp from the remote catalog's
	// Last-Modified header; 0 when unknown (pi lastModified?, upstream 54fad505).
	// Latent in the SDK: consumed by hosts that compare a stored catalog's mtime
	// against remote/built-in catalogs.
	LastModified int64 `json:"lastModified,omitempty"`
	// CheckedAt is the Unix-millisecond timestamp of the last completed remote
	// check; 0 when never checked (pi checkedAt?).
	CheckedAt int64 `json:"checkedAt,omitempty"`
}

// ModelsStore is persistent model-catalog storage keyed by provider id. Apps
// inject persistent stores; the default is in-memory. Read returns (nil, nil)
// when nothing is stored.
type ModelsStore interface {
	Read(providerID string) (*ModelsStoreEntry, error)
	Write(providerID string, entry ModelsStoreEntry) error
	Delete(providerID string) error
}

// ProviderModelsStore is a ModelsStore scoped to one provider. Providers
// cannot access other providers' catalogs.
type ProviderModelsStore interface {
	Read() (*ModelsStoreEntry, error)
	Write(entry ModelsStoreEntry) error
	Delete() error
}

// InMemoryModelsStore is the default in-memory ModelsStore.
//
// pi structuredClones entries on read/write so callers cannot mutate stored
// state. The Go SDK treats *Model as immutable shared catalog pointers
// (Provider.GetModels returns its backing slice by reference), so the store
// copies the entry and its slice and shares the Model pointers.
type InMemoryModelsStore struct {
	mu      sync.Mutex
	entries map[string]ModelsStoreEntry
}

// NewInMemoryModelsStore returns an empty in-memory store.
func NewInMemoryModelsStore() *InMemoryModelsStore {
	return &InMemoryModelsStore{entries: map[string]ModelsStoreEntry{}}
}

// Read returns the stored entry for a provider, or (nil, nil) when none.
func (s *InMemoryModelsStore) Read(providerID string) (*ModelsStoreEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.entries[providerID]
	if !ok {
		return nil, nil
	}
	out := ModelsStoreEntry{Models: make([]*Model, len(stored.Models)), LastModified: stored.LastModified, CheckedAt: stored.CheckedAt}
	copy(out.Models, stored.Models)
	return &out, nil
}

// Write stores the entry for a provider, replacing any previous one.
func (s *InMemoryModelsStore) Write(providerID string, entry ModelsStoreEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := ModelsStoreEntry{Models: make([]*Model, len(entry.Models)), LastModified: entry.LastModified, CheckedAt: entry.CheckedAt}
	copy(stored.Models, entry.Models)
	s.entries[providerID] = stored
	return nil
}

// Delete removes a provider's stored entry.
func (s *InMemoryModelsStore) Delete(providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, providerID)
	return nil
}

// providerModelsStore scopes a ModelsStore to one provider id (pi's inline
// store object in ModelsImpl.refresh).
type providerModelsStore struct {
	store ModelsStore
	id    string
}

func (p providerModelsStore) Read() (*ModelsStoreEntry, error)   { return p.store.Read(p.id) }
func (p providerModelsStore) Write(entry ModelsStoreEntry) error { return p.store.Write(p.id, entry) }
func (p providerModelsStore) Delete() error                      { return p.store.Delete(p.id) }
