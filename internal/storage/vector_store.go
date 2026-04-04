package storage

import (
	"time"
)

// VectorEntry represents a stored vector with metadata.
type VectorEntry struct {
	ID        string            `json:"id"`
	Prompt    string            `json:"prompt"`
	Model     string            `json:"model"`
	Embedding []float64         `json:"embedding"`
	Response  []byte            `json:"response"`
	CreatedAt time.Time         `json:"created_at"`
	TTL       time.Duration     `json:"ttl"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// SearchResult is a vector similarity search result.
type SearchResult struct {
	Entry VectorEntry
	Score float64
}

// VectorStore defines the interface for vector storage backends.
type VectorStore interface {
	// Store adds a vector entry.
	Store(entry VectorEntry) error

	// Search finds the most similar entries to the given embedding.
	Search(embedding []float64, topK int, threshold float64, filter map[string]string) ([]SearchResult, error)

	// Delete removes an entry by ID.
	Delete(id string) error

	// Count returns the number of stored entries.
	Count() (int64, error)

	// Close releases resources.
	Close() error

	// Healthy checks if the backend is reachable.
	Healthy() bool
}

// KVStore defines the interface for key-value storage (L1 cache, sessions, synonyms).
type KVStore interface {
	// Get retrieves a value by key.
	Get(key string) ([]byte, bool, error)

	// Set stores a value with TTL.
	Set(key string, value []byte, ttl time.Duration) error

	// Delete removes a key.
	Delete(key string) error

	// Close releases resources.
	Close() error

	// Healthy checks if the backend is reachable.
	Healthy() bool
}

// StoreConfig configures storage backends.
type StoreConfig struct {
	VectorBackend    string `yaml:"vector_backend"`
	KVBackend        string `yaml:"kv_backend"`
	QdrantHost       string `yaml:"qdrant_host"`
	QdrantPort       int    `yaml:"qdrant_port"`
	QdrantCollection string `yaml:"qdrant_collection"`
	QdrantAPIKey     string `yaml:"qdrant_api_key"`
	QdrantDimension  int    `yaml:"qdrant_dimension"`
	RedisAddr        string `yaml:"redis_addr"`
	RedisPassword    string `yaml:"redis_password"`
	RedisDB          int    `yaml:"redis_db"`
	RedisTLS         bool   `yaml:"redis_tls"`
}
