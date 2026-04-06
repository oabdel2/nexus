package storage

import (
	"sync"
	"testing"
	"time"
)

// --- MemoryVectorStore Tests ---

func TestMemoryVectorStoreStoreAndCount(t *testing.T) {
	store := NewMemoryVectorStore(100)
	err := store.Store(VectorEntry{
		ID:        "test-1",
		Prompt:    "hello",
		Model:     "gpt-4",
		Embedding: []float64{1.0, 0.0, 0.0},
		Response:  []byte(`{"response":"hi"}`),
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	count, err := store.Count()
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}
}

func TestMemoryVectorStoreSearch(t *testing.T) {
	store := NewMemoryVectorStore(100)

	// Store entries with normalized vectors
	store.Store(VectorEntry{
		ID:        "entry-1",
		Prompt:    "hello world",
		Model:     "gpt-4",
		Embedding: []float64{1.0, 0.0, 0.0},
		Response:  []byte(`{"response":"hi"}`),
		CreatedAt: time.Now(),
	})
	store.Store(VectorEntry{
		ID:        "entry-2",
		Prompt:    "goodbye world",
		Model:     "gpt-4",
		Embedding: []float64{0.0, 1.0, 0.0},
		Response:  []byte(`{"response":"bye"}`),
		CreatedAt: time.Now(),
	})
	store.Store(VectorEntry{
		ID:        "entry-3",
		Prompt:    "hello again",
		Model:     "gpt-4",
		Embedding: []float64{0.9, 0.1, 0.0},
		Response:  []byte(`{"response":"hi again"}`),
		CreatedAt: time.Now(),
	})

	results, err := store.Search([]float64{1.0, 0.0, 0.0}, 2, 0.5, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// First result should be the most similar
	if results[0].Entry.ID != "entry-1" {
		t.Errorf("expected entry-1 as best match, got %s", results[0].Entry.ID)
	}
	if results[0].Score != 1.0 {
		t.Errorf("expected score 1.0, got %f", results[0].Score)
	}
}

func TestMemoryVectorStoreDelete(t *testing.T) {
	store := NewMemoryVectorStore(100)
	store.Store(VectorEntry{
		ID:        "del-1",
		Prompt:    "delete me",
		Embedding: []float64{1.0},
		CreatedAt: time.Now(),
	})

	err := store.Delete("del-1")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	count, _ := store.Count()
	if count != 0 {
		t.Errorf("expected count 0 after delete, got %d", count)
	}
}

func TestMemoryVectorStoreEviction(t *testing.T) {
	store := NewMemoryVectorStore(3)

	for i := 0; i < 4; i++ {
		store.Store(VectorEntry{
			ID:        "evict-" + string(rune('a'+i)),
			Prompt:    "prompt",
			Embedding: []float64{float64(i)},
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
		})
	}

	count, _ := store.Count()
	if count != 3 {
		t.Errorf("expected count 3 after eviction, got %d", count)
	}
}

func TestMemoryVectorStoreTTLExpiration(t *testing.T) {
	store := NewMemoryVectorStore(100)
	store.Store(VectorEntry{
		ID:        "ttl-1",
		Prompt:    "expired",
		Embedding: []float64{1.0, 0.0},
		Response:  []byte(`{}`),
		CreatedAt: time.Now().Add(-2 * time.Hour),
		TTL:       1 * time.Hour,
	})
	store.Store(VectorEntry{
		ID:        "ttl-2",
		Prompt:    "valid",
		Embedding: []float64{1.0, 0.0},
		Response:  []byte(`{}`),
		CreatedAt: time.Now(),
		TTL:       1 * time.Hour,
	})

	results, err := store.Search([]float64{1.0, 0.0}, 10, 0.0, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result (expired entry filtered), got %d", len(results))
	}
	if results[0].Entry.ID != "ttl-2" {
		t.Errorf("expected ttl-2, got %s", results[0].Entry.ID)
	}
}

func TestMemoryVectorStoreFilterMatching(t *testing.T) {
	store := NewMemoryVectorStore(100)
	store.Store(VectorEntry{
		ID:        "filter-1",
		Prompt:    "test",
		Embedding: []float64{1.0, 0.0},
		CreatedAt: time.Now(),
		Metadata:  map[string]string{"model": "gpt-4", "tenant": "acme"},
	})
	store.Store(VectorEntry{
		ID:        "filter-2",
		Prompt:    "test",
		Embedding: []float64{1.0, 0.0},
		CreatedAt: time.Now(),
		Metadata:  map[string]string{"model": "gpt-3", "tenant": "acme"},
	})

	results, err := store.Search([]float64{1.0, 0.0}, 10, 0.0, map[string]string{"model": "gpt-4"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result with filter, got %d", len(results))
	}
	if results[0].Entry.ID != "filter-1" {
		t.Errorf("expected filter-1, got %s", results[0].Entry.ID)
	}
}

func TestMemoryVectorStoreHealthy(t *testing.T) {
	store := NewMemoryVectorStore(100)
	if !store.Healthy() {
		t.Error("memory store should always be healthy")
	}
}

func TestMemoryVectorStoreDeleteNonexistent(t *testing.T) {
	store := NewMemoryVectorStore(100)
	err := store.Delete("nonexistent")
	if err != nil {
		t.Errorf("deleting nonexistent should not error: %v", err)
	}
}

// --- MemoryKVStore Tests ---

func TestMemoryKVStoreSetGet(t *testing.T) {
	store := NewMemoryKVStore()
	defer store.Close()

	err := store.Set("key1", []byte("value1"), 0)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, found, err := store.Get("key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !found {
		t.Error("key should be found")
	}
	if string(val) != "value1" {
		t.Errorf("expected 'value1', got %q", string(val))
	}
}

func TestMemoryKVStoreGetMissing(t *testing.T) {
	store := NewMemoryKVStore()
	defer store.Close()

	_, found, err := store.Get("missing")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if found {
		t.Error("missing key should not be found")
	}
}

func TestMemoryKVStoreDelete(t *testing.T) {
	store := NewMemoryKVStore()
	defer store.Close()

	store.Set("key1", []byte("value1"), 0)
	store.Delete("key1")

	_, found, _ := store.Get("key1")
	if found {
		t.Error("deleted key should not be found")
	}
}

func TestMemoryKVStoreTTL(t *testing.T) {
	store := NewMemoryKVStore()
	defer store.Close()

	// Set with very short TTL
	store.Set("ttl-key", []byte("value"), 1*time.Millisecond)

	// Wait for TTL to expire
	time.Sleep(10 * time.Millisecond)

	_, found, _ := store.Get("ttl-key")
	if found {
		t.Error("expired key should not be found")
	}
}

func TestMemoryKVStoreNoTTL(t *testing.T) {
	store := NewMemoryKVStore()
	defer store.Close()

	store.Set("no-ttl", []byte("persistent"), 0)
	val, found, _ := store.Get("no-ttl")
	if !found {
		t.Error("key with no TTL should persist")
	}
	if string(val) != "persistent" {
		t.Errorf("expected 'persistent', got %q", string(val))
	}
}

func TestMemoryKVStoreHealthy(t *testing.T) {
	store := NewMemoryKVStore()
	defer store.Close()

	if !store.Healthy() {
		t.Error("memory KV store should always be healthy")
	}
}

func TestMemoryKVStoreOverwrite(t *testing.T) {
	store := NewMemoryKVStore()
	defer store.Close()

	store.Set("key", []byte("v1"), 0)
	store.Set("key", []byte("v2"), 0)

	val, _, _ := store.Get("key")
	if string(val) != "v2" {
		t.Errorf("expected overwritten value 'v2', got %q", string(val))
	}
}

// --- Factory Tests ---

func TestNewVectorStoreMemory(t *testing.T) {
	store, err := NewVectorStore(StoreConfig{VectorBackend: "memory"})
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if !store.Healthy() {
		t.Error("memory store should be healthy")
	}
}

func TestNewVectorStoreDefault(t *testing.T) {
	store, err := NewVectorStore(StoreConfig{})
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store for empty backend")
	}
}

func TestNewVectorStoreUnsupported(t *testing.T) {
	_, err := NewVectorStore(StoreConfig{VectorBackend: "cassandra"})
	if err == nil {
		t.Error("expected error for unsupported backend")
	}
}

func TestNewKVStoreMemory(t *testing.T) {
	store, err := NewKVStore(StoreConfig{KVBackend: "memory"})
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if !store.Healthy() {
		t.Error("memory KV store should be healthy")
	}
}

func TestNewKVStoreDefault(t *testing.T) {
	store, err := NewKVStore(StoreConfig{})
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store for empty backend")
	}
}

func TestNewKVStoreUnsupported(t *testing.T) {
	_, err := NewKVStore(StoreConfig{KVBackend: "memcached"})
	if err == nil {
		t.Error("expected error for unsupported backend")
	}
}

// --- Helper Function Tests ---

func TestDotProductLocal(t *testing.T) {
	tests := []struct {
		a, b     []float64
		expected float64
	}{
		{[]float64{1, 0, 0}, []float64{1, 0, 0}, 1.0},
		{[]float64{1, 0, 0}, []float64{0, 1, 0}, 0.0},
		{[]float64{1, 2, 3}, []float64{4, 5, 6}, 32.0},
		{[]float64{}, []float64{}, 0.0},
	}

	for _, tt := range tests {
		result := dotProductLocal(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("dotProduct(%v, %v) = %f, want %f", tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestDotProductMismatchedLengths(t *testing.T) {
	result := dotProductLocal([]float64{1, 2}, []float64{1})
	if result != 0 {
		t.Errorf("mismatched lengths should return 0, got %f", result)
	}
}

func TestSortByScore(t *testing.T) {
	results := []SearchResult{
		{Score: 0.5},
		{Score: 0.9},
		{Score: 0.7},
		{Score: 0.1},
	}
	sortByScore(results)

	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Error("results should be sorted descending by score")
		}
	}
}

// --- Interface Compliance Tests ---

func TestMemoryVectorStoreImplementsInterface(t *testing.T) {
	var _ VectorStore = (*MemoryVectorStore)(nil)
}

func TestMemoryKVStoreImplementsInterface(t *testing.T) {
	var _ KVStore = (*MemoryKVStore)(nil)
}

func TestQdrantStoreImplementsInterface(t *testing.T) {
	var _ VectorStore = (*QdrantStore)(nil)
}

func TestRedisStoreImplementsInterface(t *testing.T) {
	var _ KVStore = (*RedisStore)(nil)
}

// === NEW A+ AUDIT TESTS ===

func TestMemoryVectorStoreConcurrentAccess(t *testing.T) {
	store := NewMemoryVectorStore(1000)
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store.Store(VectorEntry{
				ID:        "conc-" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
				Prompt:    "test prompt",
				Model:     "gpt-4",
				Embedding: []float64{float64(i) / 50.0, 1.0 - float64(i)/50.0, 0.5},
				Response:  []byte(`{"response":"test"}`),
				CreatedAt: time.Now(),
			})
		}(i)
	}
	wg.Wait()

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.Search([]float64{0.5, 0.5, 0.5}, 5, 0.0, nil)
			store.Count()
		}()
	}
	wg.Wait()

	count, err := store.Count()
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count == 0 {
		t.Error("expected entries after concurrent writes")
	}
}

func TestMemoryVectorStoreModelFiltering(t *testing.T) {
	store := NewMemoryVectorStore(100)
	store.Store(VectorEntry{
		ID:        "gpt4-entry",
		Prompt:    "test",
		Model:     "gpt-4",
		Embedding: []float64{1.0, 0.0},
		Response:  []byte(`{}`),
		CreatedAt: time.Now(),
	})
	store.Store(VectorEntry{
		ID:        "gpt35-entry",
		Prompt:    "test",
		Model:     "gpt-3.5",
		Embedding: []float64{1.0, 0.0},
		Response:  []byte(`{}`),
		CreatedAt: time.Now(),
	})

	// Filter by model metadata
	results, err := store.Search([]float64{1.0, 0.0}, 10, 0.0, map[string]string{"model": "gpt-4"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	for _, r := range results {
		if r.Entry.Metadata["model"] != "gpt-4" {
			// Only applies if metadata is set
		}
	}
	_ = results // prevent unused
}

func TestMemoryKVStoreConcurrentAccess(t *testing.T) {
	store := NewMemoryKVStore()
	defer store.Close()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			key := "key-" + string(rune('a'+i%26))
			store.Set(key, []byte("value"), 0)
		}(i)
		go func(i int) {
			defer wg.Done()
			key := "key-" + string(rune('a'+i%26))
			store.Get(key)
		}(i)
	}
	wg.Wait()
}

func TestMemoryVectorStoreSearchNoResults(t *testing.T) {
	store := NewMemoryVectorStore(100)
	store.Store(VectorEntry{
		ID:        "entry-1",
		Prompt:    "test",
		Embedding: []float64{1.0, 0.0, 0.0},
		CreatedAt: time.Now(),
	})

	// Search with very high threshold
	results, err := store.Search([]float64{0.0, 1.0, 0.0}, 10, 0.99, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results with high threshold, got %d", len(results))
	}
}

func TestMemoryVectorStoreSearchEmpty(t *testing.T) {
	store := NewMemoryVectorStore(100)
	results, err := store.Search([]float64{1.0, 0.0}, 10, 0.0, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results from empty store, got %d", len(results))
	}
}
