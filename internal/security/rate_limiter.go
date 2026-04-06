package security

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter provides per-tenant token bucket rate limiting.
type RateLimiter struct {
	mu         sync.RWMutex
	buckets    map[string]*tokenBucket
	rpm        int
	burst      int
	enabled    bool
	maxBuckets int // cap to prevent memory exhaustion
}

type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
}

// RateLimiterConfig configures the rate limiter.
type RateLimiterConfig struct {
	Enabled    bool `yaml:"enabled"`
	DefaultRPM int  `yaml:"default_rpm"`
	BurstSize  int  `yaml:"burst_size"`
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(cfg RateLimiterConfig) *RateLimiter {
	if cfg.DefaultRPM <= 0 {
		cfg.DefaultRPM = 60
	}
	if cfg.BurstSize <= 0 {
		cfg.BurstSize = 10
	}
	return &RateLimiter{
		buckets:    make(map[string]*tokenBucket),
		rpm:        cfg.DefaultRPM,
		burst:      cfg.BurstSize,
		enabled:    cfg.Enabled,
		maxBuckets: 100000,
	}
}

// Allow checks if a request from a tenant is allowed.
func (rl *RateLimiter) Allow(tenant string) bool {
	if !rl.enabled {
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, exists := rl.buckets[tenant]
	if !exists {
		// Evict stale buckets when at capacity to prevent memory exhaustion
		if len(rl.buckets) >= rl.maxBuckets {
			rl.evictStaleLocked()
		}
		bucket = &tokenBucket{
			tokens:     float64(rl.burst),
			maxTokens:  float64(rl.burst),
			refillRate: float64(rl.rpm) / 60.0,
			lastRefill: time.Now(),
		}
		rl.buckets[tenant] = bucket
	}

	// Refill tokens
	now := time.Now()
	elapsed := now.Sub(bucket.lastRefill).Seconds()
	bucket.tokens += elapsed * bucket.refillRate
	if bucket.tokens > bucket.maxTokens {
		bucket.tokens = bucket.maxTokens
	}
	bucket.lastRefill = now

	// Check if we can consume a token
	if bucket.tokens >= 1.0 {
		bucket.tokens -= 1.0
		return true
	}
	return false
}

// evictStaleLocked removes the oldest half of buckets by last refill time.
// Caller must hold rl.mu (write lock).
func (rl *RateLimiter) evictStaleLocked() {
	cutoff := time.Now().Add(-5 * time.Minute)
	for k, b := range rl.buckets {
		if b.lastRefill.Before(cutoff) {
			delete(rl.buckets, k)
		}
	}
	// If still over capacity, remove oldest entries regardless of age
	if len(rl.buckets) >= rl.maxBuckets {
		for k := range rl.buckets {
			delete(rl.buckets, k)
			if len(rl.buckets) < rl.maxBuckets/2 {
				break
			}
		}
	}
}

// Middleware returns HTTP middleware for rate limiting.
func (rl *RateLimiter) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Skip health checks
			if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}

			tenant, _ := r.Context().Value(ContextKeyTenant).(string)
			if tenant == "" {
				tenant = r.RemoteAddr
			}

			if !rl.Allow(tenant) {
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
