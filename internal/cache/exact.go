package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

type CacheEntry struct {
	Response  []byte
	CreatedAt time.Time
	HitCount  int64
}

type ExactCache struct {
	entries    map[string]*CacheEntry
	mu         sync.RWMutex
	ttl        time.Duration
	maxEntries int
	hits       int64
	misses     int64
}

func NewExactCache(ttl time.Duration, maxEntries int) *ExactCache {
	c := &ExactCache{
		entries:    make(map[string]*CacheEntry),
		ttl:        ttl,
		maxEntries: maxEntries,
	}
	go c.cleanup()
	return c
}

func HashKey(prompt string, model string) string {
	h := sha256.New()
	h.Write([]byte(prompt))
	return hex.EncodeToString(h.Sum(nil))
}

func (c *ExactCache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return nil, false
	}

	if time.Since(entry.CreatedAt) > c.ttl {
		c.mu.Lock()
		delete(c.entries, key)
		c.misses++
		c.mu.Unlock()
		return nil, false
	}

	c.mu.Lock()
	entry.HitCount++
	c.hits++
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
}

func (c *ExactCache) Stats() (hits, misses int64, size int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hits, c.misses, len(c.entries)
}

func (c *ExactCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for k, v := range c.entries {
		if oldestKey == "" || v.CreatedAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.CreatedAt
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

func (c *ExactCache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for k, v := range c.entries {
			if now.Sub(v.CreatedAt) > c.ttl {
				delete(c.entries, k)
			}
		}
		c.mu.Unlock()
	}
}
