package storage

import (
	"sync"
	"time"
)

// MemoryVectorStore is an in-memory vector store.
type MemoryVectorStore struct {
	mu      sync.RWMutex
	entries []VectorEntry
	maxSize int
}

// NewMemoryVectorStore creates a new in-memory vector store.
func NewMemoryVectorStore(maxSize int) *MemoryVectorStore {
	if maxSize <= 0 {
		maxSize = 50000
	}
	return &MemoryVectorStore{maxSize: maxSize}
}

func (m *MemoryVectorStore) Store(entry VectorEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.entries) >= m.maxSize {
		// Evict oldest
		oldestIdx := 0
		for i := 1; i < len(m.entries); i++ {
			if m.entries[i].CreatedAt.Before(m.entries[oldestIdx].CreatedAt) {
				oldestIdx = i
			}
		}
		m.entries = append(m.entries[:oldestIdx], m.entries[oldestIdx+1:]...)
	}

	m.entries = append(m.entries, entry)
	return nil
}

func (m *MemoryVectorStore) Search(embedding []float64, topK int, threshold float64, filter map[string]string) ([]SearchResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	var results []SearchResult

	for i := range m.entries {
		e := &m.entries[i]
		// TTL check
		if e.TTL > 0 && now.Sub(e.CreatedAt) > e.TTL {
			continue
		}

		// Filter check
		if filter != nil {
			skip := false
			for k, v := range filter {
				if e.Metadata[k] != v {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
		}

		// Cosine similarity via dot product (assuming normalized vectors)
		score := dotProductLocal(embedding, e.Embedding)
		if score >= threshold {
			results = append(results, SearchResult{Entry: *e, Score: score})
		}
	}

	// Sort by score descending
	sortByScore(results)

	if len(results) > topK {
		results = results[:topK]
	}

	return results, nil
}

func (m *MemoryVectorStore) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, e := range m.entries {
		if e.ID == id {
			m.entries = append(m.entries[:i], m.entries[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *MemoryVectorStore) Count() (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return int64(len(m.entries)), nil
}

func (m *MemoryVectorStore) Close() error { return nil }
func (m *MemoryVectorStore) Healthy() bool { return true }

// MemoryKVStore is an in-memory key-value store.
type MemoryKVStore struct {
	mu    sync.RWMutex
	items map[string]*kvItem
}

type kvItem struct {
	value     []byte
	expiresAt time.Time
}

// NewMemoryKVStore creates a new in-memory key-value store.
func NewMemoryKVStore() *MemoryKVStore {
	store := &MemoryKVStore{items: make(map[string]*kvItem)}
	go store.cleanup()
	return store
}

func (m *MemoryKVStore) Get(key string) ([]byte, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.items[key]
	if !ok {
		return nil, false, nil
	}
	if !item.expiresAt.IsZero() && time.Now().After(item.expiresAt) {
		return nil, false, nil
	}
	return item.value, true, nil
}

func (m *MemoryKVStore) Set(key string, value []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := &kvItem{value: value}
	if ttl > 0 {
		item.expiresAt = time.Now().Add(ttl)
	}
	m.items[key] = item
	return nil
}

func (m *MemoryKVStore) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, key)
	return nil
}

func (m *MemoryKVStore) Close() error { return nil }
func (m *MemoryKVStore) Healthy() bool { return true }

func (m *MemoryKVStore) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		now := time.Now()
		for k, v := range m.items {
			if !v.expiresAt.IsZero() && now.After(v.expiresAt) {
				delete(m.items, k)
			}
		}
		m.mu.Unlock()
	}
}

// dotProductLocal computes the dot product of two vectors.
func dotProductLocal(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	sum := 0.0
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

// sortByScore sorts search results by score descending using insertion sort.
func sortByScore(results []SearchResult) {
	for i := 1; i < len(results); i++ {
		key := results[i]
		j := i - 1
		for j >= 0 && results[j].Score < key.Score {
			results[j+1] = results[j]
			j--
		}
		results[j+1] = key
	}
}
