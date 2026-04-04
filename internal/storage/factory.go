package storage

import "fmt"

// NewVectorStore creates a VectorStore from configuration.
func NewVectorStore(cfg StoreConfig) (VectorStore, error) {
	switch cfg.VectorBackend {
	case "qdrant":
		return NewQdrantStore(cfg.QdrantHost, cfg.QdrantPort, cfg.QdrantCollection, cfg.QdrantAPIKey, cfg.QdrantDimension)
	case "memory", "":
		return NewMemoryVectorStore(50000), nil
	default:
		return nil, fmt.Errorf("unsupported vector backend: %s", cfg.VectorBackend)
	}
}

// NewKVStore creates a KVStore from configuration.
func NewKVStore(cfg StoreConfig) (KVStore, error) {
	switch cfg.KVBackend {
	case "redis":
		return NewRedisStore(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.RedisTLS)
	case "memory", "":
		return NewMemoryKVStore(), nil
	default:
		return nil, fmt.Errorf("unsupported KV backend: %s", cfg.KVBackend)
	}
}
