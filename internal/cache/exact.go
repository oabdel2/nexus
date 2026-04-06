package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"
)

type CacheEntry struct {
	Response  []byte
	CreatedAt time.Time
	HitCount  atomic.Int64
}

type ExactCache struct {
	entries    map[string]*CacheEntry
	lru        *lruList
	mu         sync.RWMutex
	ttl        time.Duration
	maxEntries int
	hits       atomic.Int64
	misses     atomic.Int64
	cancel     context.CancelFunc
}

func NewExactCache(ctx context.Context, ttl time.Duration, maxEntries int) *ExactCache {
	ctx, cancel := context.WithCancel(ctx)
	c := &ExactCache{
		entries:    make(map[string]*CacheEntry),
		lru:        newLRUList(),
		ttl:        ttl,
		maxEntries: maxEntries,
		cancel:     cancel,
	}
	go c.cleanup(ctx)
	return c
}

func HashKey(prompt string, model string) string {
	h := sha256.New()
	h.Write([]byte(prompt))
	h.Write([]byte{0}) // null separator to prevent "ab"+"c" == "a"+"bc"
	h.Write([]byte(model))
	return hex.EncodeToString(h.Sum(nil))
}

func (c *ExactCache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		c.misses.Add(1)
		return nil, false
	}

	if time.Since(entry.CreatedAt) > c.ttl {
		c.mu.Lock()
		delete(c.entries, key)
		c.lru.Remove(key)
		c.mu.Unlock()
		c.misses.Add(1)
		return nil, false
	}

	entry.HitCount.Add(1)
	c.hits.Add(1)

	c.mu.Lock()
	c.lru.Touch(key)
	c.mu.Unlock()

	return entry.Response, true
}

func (c *ExactCache) Set(key string, response []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= c.maxEntries {
		c.evictOldest()
	}

	c.entries[key] = &CacheEntry{
		Response:  response,
		CreatedAt: time.Now(),
	}
	c.lru.Add(key)
}

func (c *ExactCache) Stats() (hits, misses int64, size int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hits.Load(), c.misses.Load(), len(c.entries)
}

// Stop cancels the background cleanup goroutine.
func (c *ExactCache) Stop() {
	c.cancel()
}

func (c *ExactCache) evictOldest() {
	evictKey := c.lru.Evict()
	if evictKey != "" {
		delete(c.entries, evictKey)
	}
}

func (c *ExactCache) cleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.cleanupExpired()
		}
	}
}

func (c *ExactCache) cleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, v := range c.entries {
		if now.Sub(v.CreatedAt) > c.ttl {
			delete(c.entries, k)
			c.lru.Remove(k)
		}
	}
}
