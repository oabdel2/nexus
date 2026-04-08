package cache

import (
	"context"
	"sync"
	"time"
)

// Store orchestrates a 3-layer cache: L1 (exact), L2a (BM25), L2b (semantic).
type Store struct {
	exact      *ExactCache
	bm25       *BM25Cache
	semantic   *SemanticCache
	l1Enabled  bool
	l2aEnabled bool
	l2bEnabled bool
	feedback   *FeedbackStore
	shadow     *ShadowMode
	context    *ContextFingerprint
	registry   *SynonymRegistry
	cancel     context.CancelFunc

	// Per-layer latency tracking for the most recent Lookup call.
	lastLookupMu    sync.RWMutex
	lastLookupStats LookupStats
}

// LookupStats holds per-layer latency information from the most recent Lookup.
type LookupStats struct {
	L1LookupNs  int64 `json:"l1_lookup_ns"`
	L2aLookupNs int64 `json:"l2a_lookup_ns"`
	L2bLookupNs int64 `json:"l2b_lookup_ns"`
}

// StoreConfig holds configuration for all cache layers.
type StoreConfig struct {
	L1Enabled             bool
	L1TTL                 time.Duration
	L1MaxEntries          int
	L2aEnabled            bool // BM25
	L2aTTL                time.Duration
	L2aMaxEntries         int
	L2aThreshold          float64
	L2bEnabled            bool // Semantic
	L2bTTL                time.Duration
	L2bMaxEntries         int
	L2bThreshold          float64
	L2bBackend            string // "ollama" or "openai"
	L2bModel              string // embedding model name
	L2bEndpoint           string // embedding endpoint URL
	L2bAPIKey             string // for OpenAI
	RerankerEnabled       bool
	RerankerModel         string
	RerankerEndpoint      string
	RerankerThreshold     float64
	FeedbackEnabled       bool
	FeedbackMaxSize       int
	ShadowEnabled         bool
	ShadowMaxResults      int
	SynonymDataDir        string // directory for learned synonym persistence
	SynonymPromoThreshold int    // confirmations needed (default: 3)
}

// NewStore creates a Store from a StoreConfig, initializing all enabled layers.
func NewStore(cfg StoreConfig) *Store {
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize synonym registry
	registry := NewSynonymRegistry(RegistryConfig{
		DataDir:            cfg.SynonymDataDir,
		PromotionThreshold: cfg.SynonymPromoThreshold,
	})

	s := &Store{
		l1Enabled:  cfg.L1Enabled,
		l2aEnabled: cfg.L2aEnabled,
		l2bEnabled: cfg.L2bEnabled,
		feedback:   NewFeedbackStore(cfg.FeedbackMaxSize),
		shadow:     NewShadowMode(cfg.ShadowEnabled, cfg.ShadowMaxResults),
		context:    NewContextFingerprint(3),
		registry:   registry,
		cancel:     cancel,
	}

	if cfg.L1Enabled {
		s.exact = NewExactCache(ctx, cfg.L1TTL, cfg.L1MaxEntries)
	}
	if cfg.L2aEnabled {
		threshold := cfg.L2aThreshold
		if threshold == 0 {
			threshold = 15.0
		}
		s.bm25 = NewBM25Cache(ctx, cfg.L2aTTL, cfg.L2aMaxEntries, threshold)
	}
	if cfg.L2bEnabled {
		reranker := NewReranker(RerankerConfig{
			Enabled:   cfg.RerankerEnabled,
			Model:     cfg.RerankerModel,
			Endpoint:  cfg.RerankerEndpoint,
			Threshold: cfg.RerankerThreshold,
		})
		threshold := cfg.L2bThreshold
		if threshold == 0 {
			threshold = 0.70
		}
		s.semantic = NewSemanticCache(ctx, cfg.L2bTTL, cfg.L2bMaxEntries, threshold, cfg.L2bBackend, cfg.L2bModel, cfg.L2bEndpoint, cfg.L2bAPIKey, reranker, registry)
	}

	return s
}

// Lookup checks caches in order: L1 (exact) → L2a (BM25) → L2b (semantic).
// Per-layer latency is recorded and retrievable via LastLookupStats().
func (s *Store) Lookup(prompt string, model string) ([]byte, bool, string) {
	var stats LookupStats

	// L1: exact match
	if s.l1Enabled && s.exact != nil {
		start := time.Now()
		key := HashKey(prompt, model)
		if data, ok := s.exact.Get(key); ok {
			stats.L1LookupNs = time.Since(start).Nanoseconds()
			s.setLastLookupStats(stats)
			return data, true, "l1_exact"
		}
		stats.L1LookupNs = time.Since(start).Nanoseconds()
	}

	// L2a: BM25 keyword matching
	if s.l2aEnabled && s.bm25 != nil {
		start := time.Now()
		if data, ok := s.bm25.Lookup(prompt, model); ok {
			stats.L2aLookupNs = time.Since(start).Nanoseconds()
			s.setLastLookupStats(stats)
			return data, true, "l2_bm25"
		}
		stats.L2aLookupNs = time.Since(start).Nanoseconds()
	}

	// L2b: semantic embedding matching
	if s.l2bEnabled && s.semantic != nil {
		start := time.Now()
		if data, ok := s.semantic.Lookup(prompt, model); ok {
			stats.L2bLookupNs = time.Since(start).Nanoseconds()
			s.setLastLookupStats(stats)
			return data, true, "l2_semantic"
		}
		stats.L2bLookupNs = time.Since(start).Nanoseconds()
	}

	s.setLastLookupStats(stats)
	return nil, false, ""
}

// LastLookupStats returns per-layer latency information from the most recent Lookup.
func (s *Store) LastLookupStats() LookupStats {
	s.lastLookupMu.RLock()
	defer s.lastLookupMu.RUnlock()
	return s.lastLookupStats
}

func (s *Store) setLastLookupStats(stats LookupStats) {
	s.lastLookupMu.Lock()
	s.lastLookupStats = stats
	s.lastLookupMu.Unlock()
}

// StoreResponse stores the response in ALL enabled cache layers.
func (s *Store) StoreResponse(prompt string, model string, response []byte) {
	if s.l1Enabled && s.exact != nil {
		key := HashKey(prompt, model)
		s.exact.Set(key, response)
	}
	if s.l2aEnabled && s.bm25 != nil {
		s.bm25.Store(prompt, model, response)
	}
	if s.l2bEnabled && s.semantic != nil {
		s.semantic.Store(prompt, model, response)
	}
}

// Stats returns combined stats from all enabled cache layers.
func (s *Store) Stats() (hits, misses int64, size int) {
	var totalHits, totalMisses int64
	var totalSize int

	if s.exact != nil {
		h, m, sz := s.exact.Stats()
		totalHits += h
		totalMisses += m
		totalSize += sz
	}
	if s.bm25 != nil {
		h, m, sz := s.bm25.Stats()
		totalHits += h
		totalMisses += m
		totalSize += sz
	}
	if s.semantic != nil {
		h, m, sz := s.semantic.Stats()
		totalHits += h
		totalMisses += m
		totalSize += sz
	}

	return totalHits, totalMisses, totalSize
}

// Feedback returns the feedback store for recording cache quality feedback.
func (s *Store) Feedback() *FeedbackStore {
	return s.feedback
}

// Shadow returns the shadow mode tracker.
func (s *Store) Shadow() *ShadowMode {
	return s.shadow
}

// Context returns the context fingerprint generator.
func (s *Store) Context() *ContextFingerprint {
	return s.context
}

// Registry returns the synonym registry.
func (s *Store) Registry() *SynonymRegistry {
	return s.registry
}

// Stop cancels all background cleanup goroutines.
func (s *Store) Stop() {
	s.cancel()
}
