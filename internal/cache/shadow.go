package cache

import (
	"sync"
	"sync/atomic"
	"time"
)

// ShadowMode runs cache lookups in parallel with real LLM calls
// to measure cache quality without serving potentially wrong results.
type ShadowMode struct {
	enabled       bool
	mu            sync.RWMutex
	results       []ShadowResult
	maxResults    int
	totalChecks   int64
	agreements    int64
	disagreements int64
}

// ShadowResult records a comparison between cached and fresh LLM responses.
type ShadowResult struct {
	Query          string        `json:"query"`
	CachedResponse string        `json:"cached_response"`
	FreshResponse  string        `json:"fresh_response"`
	CacheHit       bool          `json:"cache_hit"`
	CacheLayer     string        `json:"cache_layer"`
	Similarity     float64       `json:"similarity"`
	Agreement      bool          `json:"agreement"`
	Timestamp      time.Time     `json:"timestamp"`
	LatencyCache   time.Duration `json:"latency_cache"`
	LatencyFresh   time.Duration `json:"latency_fresh"`
}

// ShadowStats provides aggregate shadow mode statistics.
type ShadowStats struct {
	TotalChecks     int64   `json:"total_checks"`
	Agreements      int64   `json:"agreements"`
	Disagreements   int64   `json:"disagreements"`
	AgreementRate   float64 `json:"agreement_rate"`
	CacheHitRate    float64 `json:"cache_hit_rate"`
	AvgLatencySaved float64 `json:"avg_latency_saved_ms"`
}

// NewShadowMode creates a new shadow mode tracker.
func NewShadowMode(enabled bool, maxResults int) *ShadowMode {
	if maxResults <= 0 {
		maxResults = 1000
	}
	return &ShadowMode{
		enabled:    enabled,
		maxResults: maxResults,
	}
}

// IsEnabled returns whether shadow mode is active.
func (sm *ShadowMode) IsEnabled() bool {
	return sm.enabled
}

// RecordResult stores a shadow comparison result.
func (sm *ShadowMode) RecordResult(result ShadowResult) {
	result.Timestamp = time.Now()

	if result.Agreement {
		atomic.AddInt64(&sm.agreements, 1)
	} else {
		atomic.AddInt64(&sm.disagreements, 1)
	}
	atomic.AddInt64(&sm.totalChecks, 1)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if len(sm.results) >= sm.maxResults {
		cutoff := sm.maxResults / 10
		sm.results = sm.results[cutoff:]
	}
	sm.results = append(sm.results, result)
}

// Stats returns aggregate shadow mode statistics.
func (sm *ShadowMode) Stats() ShadowStats {
	total := atomic.LoadInt64(&sm.totalChecks)
	agreements := atomic.LoadInt64(&sm.agreements)
	disagreements := atomic.LoadInt64(&sm.disagreements)

	stats := ShadowStats{
		TotalChecks:   total,
		Agreements:    agreements,
		Disagreements: disagreements,
	}

	if total > 0 {
		stats.AgreementRate = float64(agreements) / float64(total)
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	hitCount := 0
	var totalSaved float64
	for _, r := range sm.results {
		if r.CacheHit {
			hitCount++
			saved := float64(r.LatencyFresh-r.LatencyCache) / float64(time.Millisecond)
			if saved > 0 {
				totalSaved += saved
			}
		}
	}

	if len(sm.results) > 0 {
		stats.CacheHitRate = float64(hitCount) / float64(len(sm.results))
	}
	if hitCount > 0 {
		stats.AvgLatencySaved = totalSaved / float64(hitCount)
	}

	return stats
}

// RecentDisagreements returns recent cases where cache and fresh responses differed.
func (sm *ShadowMode) RecentDisagreements(limit int) []ShadowResult {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var disagreements []ShadowResult
	for i := len(sm.results) - 1; i >= 0 && len(disagreements) < limit; i-- {
		if !sm.results[i].Agreement {
			disagreements = append(disagreements, sm.results[i])
		}
	}
	return disagreements
}
