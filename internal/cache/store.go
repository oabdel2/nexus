package cache

import "time"

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
	// Initialize synonym registry
	registry := NewSynonymRegistry(RegistryConfig{
		DataDir:            cfg.SynonymDataDir,
		PromotionThreshold: cfg.SynonymPromoThreshold,
	})
	SetSynonymRegistry(registry)

	s := &Store{
		l1Enabled:  cfg.L1Enabled,
		l2aEnabled: cfg.L2aEnabled,
		l2bEnabled: cfg.L2bEnabled,
		feedback:   NewFeedbackStore(cfg.FeedbackMaxSize),
		shadow:     NewShadowMode(cfg.ShadowEnabled, cfg.ShadowMaxResults),
		context:    NewContextFingerprint(3),
		registry:   registry,
	}

	if cfg.L1Enabled {
		s.exact = NewExactCache(cfg.L1TTL, cfg.L1MaxEntries)
	}
	if cfg.L2aEnabled {
		threshold := cfg.L2aThreshold
		if threshold == 0 {
			threshold = 15.0
		}
		s.bm25 = NewBM25Cache(cfg.L2aTTL, cfg.L2aMaxEntries, threshold)
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
		s.semantic = NewSemanticCache(cfg.L2bTTL, cfg.L2bMaxEntries, threshold, cfg.L2bBackend, cfg.L2bModel, cfg.L2bEndpoint, cfg.L2bAPIKey, reranker)
	}

	return s
}

// Lookup checks caches in order: L1 (exact) → L2a (BM25) → L2b (semantic).
func (s *Store) Lookup(prompt string, model string) ([]byte, bool, string) {
	// L1: exact match
	if s.l1Enabled && s.exact != nil {
		key := HashKey(prompt, model)
		if data, ok := s.exact.Get(key); ok {
			return data, true, "l1_exact"
		}
	}

	// L2a: BM25 keyword matching
	if s.l2aEnabled && s.bm25 != nil {
		if data, ok := s.bm25.Lookup(prompt, model); ok {
			return data, true, "l2_bm25"
		}
	}

	// L2b: semantic embedding matching
	if s.l2bEnabled && s.semantic != nil {
		if data, ok := s.semantic.Lookup(prompt, model); ok {
			return data, true, "l2_semantic"
		}
	}

	return nil, false, ""
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
