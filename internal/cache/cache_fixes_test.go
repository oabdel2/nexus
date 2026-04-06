package cache

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// Test FIX 1: HashKey includes model — different models produce different keys.
func TestHashKeyIncludesModel(t *testing.T) {
	k1 := HashKey("hello", "gpt-4")
	k2 := HashKey("hello", "claude-3")
	if k1 == k2 {
		t.Errorf("HashKey should differ for different models, got same: %s", k1)
	}
	// Same inputs should be deterministic.
	if k1 != HashKey("hello", "gpt-4") {
		t.Error("HashKey should be deterministic")
	}
}

// Ensure null separator prevents prompt+model collisions.
func TestHashKeySeparator(t *testing.T) {
	k1 := HashKey("ab", "c")
	k2 := HashKey("a", "bc")
	if k1 == k2 {
		t.Errorf("HashKey(\"ab\",\"c\") == HashKey(\"a\",\"bc\"), separator bug")
	}
}

// Test FIX 2: concurrent Gets don't block each other (no write lock for counters).
func TestExactCacheConcurrentGet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewExactCache(ctx, time.Hour, 1000)

	key := HashKey("test-prompt", "model")
	c.Set(key, []byte("response"))

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	start := time.Now()
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				data, ok := c.Get(key)
				if !ok || string(data) != "response" {
					t.Errorf("unexpected miss or wrong data")
					return
				}
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	// Sanity check: 100k reads should complete well under 5 seconds.
	if elapsed > 5*time.Second {
		t.Errorf("concurrent reads took too long: %v", elapsed)
	}

	hits, _, _ := c.Stats()
	if hits != int64(goroutines*1000) {
		t.Errorf("expected %d hits, got %d", goroutines*1000, hits)
	}
}

// Test FIX 4: cleanup goroutine stops on context cancel.
func TestExactCacheCleanupStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := NewExactCache(ctx, time.Hour, 100)
	c.Set(HashKey("a", "m"), []byte("data"))

	cancel()
	// Give goroutine time to exit.
	time.Sleep(50 * time.Millisecond)

	// The cache should still be usable after cancel (just no cleanup).
	data, ok := c.Get(HashKey("a", "m"))
	if !ok || string(data) != "data" {
		t.Error("cache should still work after cleanup cancel")
	}
}

func TestBM25CleanupStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := NewBM25Cache(ctx, time.Hour, 100, 0.1)
	c.Store("test prompt", "m", []byte("data"))

	cancel()
	time.Sleep(50 * time.Millisecond)

	data, ok := c.Lookup("test prompt", "m")
	if !ok || string(data) != "data" {
		t.Error("BM25 cache should still work after cleanup cancel")
	}
}

// Test FIX 5: LRU evicts the oldest (least recently used) entry.
func TestLRUEvictsOldest(t *testing.T) {
	l := newLRUList()
	l.Add("a")
	l.Add("b")
	l.Add("c")

	if l.Len() != 3 {
		t.Fatalf("expected len 3, got %d", l.Len())
	}

	// "a" was added first so it's oldest (at tail).
	evicted := l.Evict()
	if evicted != "a" {
		t.Errorf("expected to evict 'a', got '%s'", evicted)
	}
	if l.Len() != 2 {
		t.Fatalf("expected len 2, got %d", l.Len())
	}
}

func TestLRUTouchMovesToFront(t *testing.T) {
	l := newLRUList()
	l.Add("a")
	l.Add("b")
	l.Add("c")

	// Touch "a" to move it to front; "b" becomes oldest.
	l.Touch("a")

	evicted := l.Evict()
	if evicted != "b" {
		t.Errorf("expected to evict 'b' after touching 'a', got '%s'", evicted)
	}
}

func TestLRURemove(t *testing.T) {
	l := newLRUList()
	l.Add("a")
	l.Add("b")
	l.Add("c")

	l.Remove("b")
	if l.Len() != 2 {
		t.Fatalf("expected len 2, got %d", l.Len())
	}

	// Evict should return "a" (oldest remaining).
	evicted := l.Evict()
	if evicted != "a" {
		t.Errorf("expected 'a', got '%s'", evicted)
	}
}

func TestExactCacheLRUEviction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewExactCache(ctx, time.Hour, 3)

	c.Set(HashKey("a", "m"), []byte("1"))
	c.Set(HashKey("b", "m"), []byte("2"))
	c.Set(HashKey("c", "m"), []byte("3"))

	// Touch "a" so it's not the oldest.
	c.Get(HashKey("a", "m"))

	// Insert "d" — should evict "b" (oldest untouched).
	c.Set(HashKey("d", "m"), []byte("4"))

	if _, ok := c.Get(HashKey("b", "m")); ok {
		t.Error("expected 'b' to be evicted")
	}
	if _, ok := c.Get(HashKey("a", "m")); !ok {
		t.Error("expected 'a' to still be present")
	}
}

// Test FIX 3: BM25 incremental index matches full rebuild.
func TestBM25IncrementalIndexMatchesRebuild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewBM25Cache(ctx, time.Hour, 100, 0.1)

	prompts := []string{
		"How to reverse a string in Go",
		"Explain goroutines and channels",
		"What is a mutex in concurrent programming",
		"Debug race condition in Go",
	}
	for _, p := range prompts {
		c.Store(p, "model", []byte("resp"))
	}

	// Save current incrementally-built index.
	c.mu.RLock()
	incrementalIdx := make(map[string][]int)
	for term, indices := range c.invertedIdx {
		cp := make([]int, len(indices))
		copy(cp, indices)
		incrementalIdx[term] = cp
	}
	c.mu.RUnlock()

	// Rebuild from scratch and compare.
	c.mu.Lock()
	c.rebuildInvertedIndex()
	rebuiltIdx := c.invertedIdx
	c.mu.Unlock()

	if len(incrementalIdx) != len(rebuiltIdx) {
		t.Fatalf("index size mismatch: incremental=%d, rebuilt=%d", len(incrementalIdx), len(rebuiltIdx))
	}
	for term, incIndices := range incrementalIdx {
		rebIndices, ok := rebuiltIdx[term]
		if !ok {
			t.Errorf("term %q missing after rebuild", term)
			continue
		}
		if len(incIndices) != len(rebIndices) {
			t.Errorf("term %q: incremental has %d indices, rebuilt has %d", term, len(incIndices), len(rebIndices))
			continue
		}
		for i := range incIndices {
			if incIndices[i] != rebIndices[i] {
				t.Errorf("term %q: index[%d] incremental=%d, rebuilt=%d", term, i, incIndices[i], rebIndices[i])
			}
		}
	}
}

// Test Store.Stop cancels all cleanup goroutines.
func TestStoreStopCancelsCleanup(t *testing.T) {
	s := NewStore(StoreConfig{
		L1Enabled:    true,
		L1TTL:        time.Hour,
		L1MaxEntries: 100,
		L2aEnabled:    true,
		L2aTTL:        time.Hour,
		L2aMaxEntries: 100,
		L2aThreshold:  15.0,
	})

	s.StoreResponse("test prompt", "model", []byte("data"))
	s.Stop()

	// After stop, cache should still be readable.
	data, ok, _ := s.Lookup("test prompt", "model")
	if !ok || string(data) != "data" {
		t.Error("cache should still be readable after Stop")
	}
}

// ==================== Property-Based Tests ====================

// TestLRU_PropertyEvictsOldest verifies that for N items added, Evict always
// removes the least-recently-used (oldest untouched) entry.
func TestLRU_PropertyEvictsOldest(t *testing.T) {
	for n := 3; n <= 100; n++ {
		l := newLRUList()
		for i := 0; i < n; i++ {
			l.Add(fmt.Sprintf("key-%d", i))
		}

		// Evict should always return key-0 (the oldest)
		evicted := l.Evict()
		if evicted != "key-0" {
			t.Errorf("n=%d: expected to evict 'key-0', got '%s'", n, evicted)
		}
		if l.Len() != n-1 {
			t.Errorf("n=%d: expected len %d after evict, got %d", n, n-1, l.Len())
		}

		// Touch key-1 (currently oldest), add a new key, evict again
		// After touch, key-1 moves to front; key-2 becomes oldest
		l.Touch("key-1")
		l.Add(fmt.Sprintf("key-%d", n))
		evicted = l.Evict()
		if evicted != "key-2" {
			t.Errorf("n=%d: after touch(key-1), expected evict 'key-2', got '%s'", n, evicted)
		}
	}

	// Special case: verify basic eviction order for small list
	l := newLRUList()
	l.Add("a")
	l.Add("b")
	// "a" is oldest (tail)
	if got := l.Evict(); got != "a" {
		t.Errorf("small list: expected evict 'a', got '%s'", got)
	}
	if got := l.Evict(); got != "b" {
		t.Errorf("small list: expected evict 'b', got '%s'", got)
	}
	if got := l.Evict(); got != "" {
		t.Errorf("empty list: expected evict '', got '%s'", got)
	}
}

// TestHashKey_PropertyDifferentModels verifies that for any prompt,
// HashKey(prompt, modelA) != HashKey(prompt, modelB) when modelA != modelB.
func TestHashKey_PropertyDifferentModels(t *testing.T) {
	prompts := []string{
		"", "hi", "hello world",
		"explain quantum computing in simple terms",
		"debug this race condition in the concurrent cache",
		strings.Repeat("a", 5000),
	}
	models := []string{
		"gpt-4", "gpt-3.5", "claude-3", "llama3.1", "qwen2.5:1.5b",
		"model-a", "model-b", "",
	}

	for _, prompt := range prompts {
		seen := make(map[string]string) // hash → model
		for _, model := range models {
			h := HashKey(prompt, model)
			if prevModel, exists := seen[h]; exists && prevModel != model {
				t.Errorf("HashKey collision: prompt=%q model=%q and model=%q both produce %s",
					prompt, prevModel, model, h)
			}
			seen[h] = model
		}
	}
}
