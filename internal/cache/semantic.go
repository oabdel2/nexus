package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"
	"time"
)

// SemanticCache provides dense embedding-based similarity matching.
// It calls an embedding endpoint (Ollama or OpenAI) to convert prompts
// to vectors, then uses cosine similarity to find matches.
type SemanticCache struct {
	mu         sync.RWMutex
	entries    []semanticEntry
	ttl        time.Duration
	maxEntries int
	threshold  float64
	backend    string // "ollama" or "openai"
	model      string
	endpoint   string
	apiKey     string
	client     *http.Client
	hits       int64
	misses     int64
	reranker   *Reranker
}

type semanticEntry struct {
	prompt    string
	model     string
	embedding []float64
	response  []byte
	createdAt time.Time
}

// NewSemanticCache creates a new embedding-based semantic cache.
func NewSemanticCache(ttl time.Duration, maxEntries int, threshold float64, backend, model, endpoint, apiKey string, reranker *Reranker) *SemanticCache {
	c := &SemanticCache{
		ttl:        ttl,
		maxEntries: maxEntries,
		threshold:  threshold,
		backend:    backend,
		model:      model,
		endpoint:   endpoint,
		apiKey:     apiKey,
		reranker:   reranker,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
	go c.cleanup()
	return c
}

func (c *SemanticCache) Store(prompt, model string, response []byte) {
	expanded := expandSynonyms(prompt)
	emb, err := c.getEmbedding(expanded)
	if err != nil {
		// Graceful fallback: skip storing if embedding fails
		return
	}
	emb = normalizeVector(emb)

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= c.maxEntries {
		c.evictOldest()
	}

	c.entries = append(c.entries, semanticEntry{
		prompt:    prompt,
		model:     model,
		embedding: emb,
		response:  response,
		createdAt: time.Now(),
	})
}

func (c *SemanticCache) Lookup(prompt, model string) ([]byte, bool) {
	expanded := expandSynonyms(prompt)
	emb, err := c.getEmbedding(expanded)
	if err != nil {
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return nil, false
	}
	emb = normalizeVector(emb)

	// Adaptive threshold based on query type
	qt := ClassifyQueryType(prompt)
	threshold := AdaptiveThreshold(qt, c.threshold)

	// Confident hit zone: anything above this skips reranker
	confidentHitThreshold := threshold + 0.15
	if confidentHitThreshold > 0.95 {
		confidentHitThreshold = 0.95
	}

	c.mu.RLock()
	if len(c.entries) == 0 {
		c.mu.RUnlock()
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return nil, false
	}

	bestScore := -1.0
	bestIdx := -1
	now := time.Now()

	for i := range c.entries {
		entry := &c.entries[i]
		if now.Sub(entry.createdAt) > c.ttl {
			continue
		}
		score := dotProduct(emb, entry.embedding)
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	c.mu.RUnlock()

	if bestIdx < 0 || bestScore < threshold {
		// Record near-miss for synonym learning
		if bestIdx >= 0 && bestScore >= 0.55 && defaultRegistry != nil {
			c.mu.RLock()
			cachedPrompt := c.entries[bestIdx].prompt
			c.mu.RUnlock()
			defaultRegistry.RecordNearMiss(prompt, cachedPrompt, bestScore)
		}
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return nil, false
	}

	// Get cached prompt for filter checks
	c.mu.RLock()
	cachedPrompt := c.entries[bestIdx].prompt
	c.mu.RUnlock()

	// Apply negation and key noun filters
	if hasOppositeIntent(prompt, cachedPrompt) || hasDifferentKeyNoun(prompt, cachedPrompt) {
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return nil, false
	}

	// Confidence gating: uncertain zone goes through reranker
	if bestScore < confidentHitThreshold && c.reranker != nil {
		if !c.reranker.Verify(prompt, cachedPrompt) {
			c.mu.Lock()
			c.misses++
			c.mu.Unlock()
			return nil, false
		}
	}

	c.mu.Lock()
	c.hits++
	c.mu.Unlock()
	c.mu.RLock()
	resp := c.entries[bestIdx].response
	c.mu.RUnlock()
	return resp, true
}

func (c *SemanticCache) Stats() (hits, misses int64, size int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hits, c.misses, len(c.entries)
}

// getEmbedding calls the configured embedding backend.
func (c *SemanticCache) getEmbedding(text string) ([]float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch c.backend {
	case "ollama":
		return c.getOllamaEmbedding(ctx, text)
	case "openai":
		return c.getOpenAIEmbedding(ctx, text)
	default:
		return nil, fmt.Errorf("unsupported embedding backend: %s", c.backend)
	}
}

func (c *SemanticCache) getOllamaEmbedding(ctx context.Context, text string) ([]float64, error) {
	body, err := json.Marshal(map[string]interface{}{
		"model": c.model,
		"input": text,
	})
	if err != nil {
		return nil, err
	}

	url := c.endpoint + "/api/embed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Embeddings) == 0 {
		return nil, fmt.Errorf("ollama returned no embeddings")
	}
	return result.Embeddings[0], nil
}

func (c *SemanticCache) getOpenAIEmbedding(ctx context.Context, text string) ([]float64, error) {
	body, err := json.Marshal(map[string]interface{}{
		"model": c.model,
		"input": text,
	})
	if err != nil {
		return nil, err
	}

	url := c.endpoint + "/v1/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai embeddings returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("openai returned no embeddings")
	}
	return result.Data[0].Embedding, nil
}

// normalizeVector normalizes a vector to unit length for fast cosine computation.
func normalizeVector(v []float64) []float64 {
	norm := 0.0
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return v
	}
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = x / norm
	}
	return out
}

// dotProduct computes the dot product of two vectors.
func dotProduct(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	sum := 0.0
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

// CosineSimilarity computes cosine similarity between two unnormalized vectors.
func CosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	dot := 0.0
	normA := 0.0
	normB := 0.0
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

func (c *SemanticCache) evictOldest() {
	if len(c.entries) == 0 {
		return
	}
	oldestIdx := 0
	for i := 1; i < len(c.entries); i++ {
		if c.entries[i].createdAt.Before(c.entries[oldestIdx].createdAt) {
			oldestIdx = i
		}
	}
	c.entries = append(c.entries[:oldestIdx], c.entries[oldestIdx+1:]...)
}

func (c *SemanticCache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		i := 0
		for i < len(c.entries) {
			if now.Sub(c.entries[i].createdAt) > c.ttl {
				c.entries = append(c.entries[:i], c.entries[i+1:]...)
			} else {
				i++
			}
		}
		c.mu.Unlock()
	}
}
