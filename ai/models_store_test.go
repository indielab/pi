package ai

import "testing"

// TestModelsStoreEntryLastModifiedRoundTrip locks that the InMemoryModelsStore
// preserves the lastModified timestamp across Write/Read (upstream 54fad505).
func TestModelsStoreEntryLastModifiedRoundTrip(t *testing.T) {
	store := NewInMemoryModelsStore()
	if err := store.Write("p", ModelsStoreEntry{LastModified: 1721577600000, CheckedAt: 1721577601000}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := store.Read("p")
	if err != nil || got == nil {
		t.Fatalf("read: %+v (err %v)", got, err)
	}
	if got.LastModified != 1721577600000 {
		t.Errorf("LastModified = %d, want 1721577600000", got.LastModified)
	}
	if got.CheckedAt != 1721577601000 {
		t.Errorf("CheckedAt = %d, want 1721577601000", got.CheckedAt)
	}
}
