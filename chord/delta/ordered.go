package delta

import "iter"

// orderedMap is a JavaScript Map over a Go map: it enumerates in insertion
// order, setting an existing key keeps its place, and a deleted key that is set
// again goes to the end. The overlay emits ops in the order its writes,
// deletions and overrides were first made, as upstream's Maps and Sets do, so
// that order has to be kept.
type orderedMap[K comparable] struct {
	entries []orderedEntry[K]
	at      map[K]int // the index in entries of each live key
}

type orderedEntry[K comparable] struct {
	key   K
	value any
	live  bool
}

func (m *orderedMap[K]) len() int {
	if m == nil {
		return 0
	}
	return len(m.at)
}

func (m *orderedMap[K]) get(k K) (any, bool) {
	if m == nil {
		return nil, false
	}
	i, ok := m.at[k]
	if !ok {
		return nil, false
	}
	return m.entries[i].value, true
}

func (m *orderedMap[K]) has(k K) bool {
	if m == nil {
		return false
	}
	_, ok := m.at[k]
	return ok
}

func (m *orderedMap[K]) set(k K, v any) {
	if i, ok := m.at[k]; ok {
		m.entries[i].value = v
		return
	}
	if m.at == nil {
		m.at = map[K]int{}
	}
	m.at[k] = len(m.entries)
	m.entries = append(m.entries, orderedEntry[K]{key: k, value: v, live: true})
}

func (m *orderedMap[K]) delete(k K) {
	if m == nil {
		return
	}
	i, ok := m.at[k]
	if !ok {
		return
	}
	delete(m.at, k)
	m.entries[i] = orderedEntry[K]{}
	if len(m.at) == 0 {
		m.entries = m.entries[:0]
	} else if len(m.entries) > 32 && len(m.at) < len(m.entries)/2 {
		m.compact()
	}
}

// compact drops the dead entries, keeping the live ones in order.
func (m *orderedMap[K]) compact() {
	live := m.entries[:0]
	for _, e := range m.entries {
		if e.live {
			m.at[e.key] = len(live)
			live = append(live, e)
		}
	}
	clear(m.entries[len(live):])
	m.entries = live
}

// all yields the live entries in insertion order.
func (m *orderedMap[K]) all() iter.Seq2[K, any] {
	return func(yield func(K, any) bool) {
		if m == nil {
			return
		}
		for _, e := range m.entries {
			if e.live && !yield(e.key, e.value) {
				return
			}
		}
	}
}

// keys yields the live keys in insertion order.
func (m *orderedMap[K]) keys() iter.Seq[K] {
	return func(yield func(K) bool) {
		for k := range m.all() {
			if !yield(k) {
				return
			}
		}
	}
}
