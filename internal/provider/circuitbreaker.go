package provider

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

// Circuit breaker states
type CBState int

const (
	StateClosed   CBState = iota // normal operation, requests flow through
	StateOpen                    // failures exceeded threshold, fast-fail all requests
	StateHalfOpen                // probing: allow limited requests to test recovery
)

func (s CBState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

var (
	ErrCircuitOpen = errors.New("circuit breaker is open")
	ErrAllFailed   = errors.New("all providers failed")
)

// CircuitBreakerConfig configures a single provider's circuit breaker.
type CircuitBreakerConfig struct {
	FailureThreshold int           // failures before opening (default: 5)
	SuccessThreshold int           // successes in half-open before closing (default: 2)
	Timeout          time.Duration // how long to stay open before probing (default: 30s)
	MaxTimeout       time.Duration // max backoff timeout (default: 5m)
	HalfOpenMax      int           // max concurrent requests in half-open (default: 1)
}

func defaultCBConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
		MaxTimeout:       5 * time.Minute,
		HalfOpenMax:      1,
	}
}

// CircuitBreaker implements a thread-safe 3-state circuit breaker with
// exponential backoff for a single provider.
type CircuitBreaker struct {
	mu sync.Mutex

	state     CBState
	config    CircuitBreakerConfig
	provider  string

	// counters
	failures       int
	successes      int // successes during half-open
	consecutiveFails int

	// timing
	lastFailure time.Time
	openedAt    time.Time
	currentTimeout time.Duration

	// stats (exported for observability)
	TotalRequests int64
	TotalFailures int64
	TotalOpens    int64

	// optional callback
	OnStateChange func(provider string, from, to CBState)
}

// NewCircuitBreaker creates a circuit breaker for a named provider.
func NewCircuitBreaker(provider string, cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = defaultCBConfig().FailureThreshold
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = defaultCBConfig().SuccessThreshold
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultCBConfig().Timeout
	}
	if cfg.MaxTimeout <= 0 {
		cfg.MaxTimeout = defaultCBConfig().MaxTimeout
	}
	if cfg.HalfOpenMax <= 0 {
		cfg.HalfOpenMax = defaultCBConfig().HalfOpenMax
	}

	return &CircuitBreaker{
		state:          StateClosed,
		config:         cfg,
		provider:       provider,
		currentTimeout: cfg.Timeout,
	}
}

// Allow checks whether a request should be allowed through.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.openedAt) >= cb.currentTimeout {
			cb.setState(StateHalfOpen)
			cb.successes = 0
			return true
		}
		return false
	case StateHalfOpen:
		return cb.successes < cb.config.HalfOpenMax
	}
	return false
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.TotalRequests++
	cb.consecutiveFails = 0

	switch cb.state {
	case StateHalfOpen:
		cb.successes++
		if cb.successes >= cb.config.SuccessThreshold {
			cb.setState(StateClosed)
			cb.failures = 0
			cb.successes = 0
			cb.currentTimeout = cb.config.Timeout // reset backoff
		}
	case StateClosed:
		cb.failures = 0
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.TotalRequests++
	cb.TotalFailures++
	cb.consecutiveFails++
	cb.failures++
	cb.lastFailure = time.Now()

	switch cb.state {
	case StateClosed:
		if cb.failures >= cb.config.FailureThreshold {
			cb.openCircuit()
		}
	case StateHalfOpen:
		cb.openCircuit()
	}
}

func (cb *CircuitBreaker) openCircuit() {
	// Exponential backoff: double the timeout each time, up to max
	if cb.state == StateHalfOpen || cb.TotalOpens > 0 {
		cb.currentTimeout = time.Duration(float64(cb.currentTimeout) * 2)
		if cb.currentTimeout > cb.config.MaxTimeout {
			cb.currentTimeout = cb.config.MaxTimeout
		}
	}
	cb.openedAt = time.Now()
	cb.TotalOpens++
	cb.setState(StateOpen)
}

func (cb *CircuitBreaker) setState(newState CBState) {
	old := cb.state
	cb.state = newState
	if cb.OnStateChange != nil && old != newState {
		go cb.OnStateChange(cb.provider, old, newState)
	}
}

// State returns the current state.
func (cb *CircuitBreaker) State() CBState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	// Check if timeout elapsed
	if cb.state == StateOpen && time.Since(cb.openedAt) >= cb.currentTimeout {
		return StateHalfOpen
	}
	return cb.state
}

// Stats returns circuit breaker statistics.
func (cb *CircuitBreaker) Stats() map[string]any {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return map[string]any{
		"provider":         cb.provider,
		"state":            cb.state.String(),
		"failures":         cb.failures,
		"consecutive_fails": cb.consecutiveFails,
		"total_requests":   cb.TotalRequests,
		"total_failures":   cb.TotalFailures,
		"total_opens":      cb.TotalOpens,
		"current_timeout":  cb.currentTimeout.String(),
		"last_failure":     cb.lastFailure,
	}
}

// ProviderPool manages circuit breakers for all providers and handles failover.
type ProviderPool struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
	config   CircuitBreakerConfig
}

// NewProviderPool creates a pool of circuit-broken providers.
func NewProviderPool(cfg CircuitBreakerConfig) *ProviderPool {
	if cfg.FailureThreshold <= 0 {
		cfg = defaultCBConfig()
	}
	return &ProviderPool{
		breakers: make(map[string]*CircuitBreaker),
		config:   cfg,
	}
}

// Register adds a provider's circuit breaker.
func (pp *ProviderPool) Register(name string) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	if _, exists := pp.breakers[name]; !exists {
		pp.breakers[name] = NewCircuitBreaker(name, pp.config)
	}
}

// Get returns the circuit breaker for a provider.
func (pp *ProviderPool) Get(name string) *CircuitBreaker {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	return pp.breakers[name]
}

// IsAvailable checks if a provider's circuit is not open.
func (pp *ProviderPool) IsAvailable(name string) bool {
	pp.mu.RLock()
	cb, ok := pp.breakers[name]
	pp.mu.RUnlock()
	if !ok {
		return true // unknown providers are allowed
	}
	return cb.Allow()
}

// AllStats returns stats for all circuit breakers.
func (pp *ProviderPool) AllStats() map[string]any {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	result := make(map[string]any)
	for name, cb := range pp.breakers {
		result[name] = cb.Stats()
	}
	return result
}

// RetryConfig configures retry behavior.
type RetryConfig struct {
	MaxRetries  int           // max retry attempts (default: 3)
	BaseDelay   time.Duration // initial backoff delay (default: 100ms)
	MaxDelay    time.Duration // max backoff delay (default: 5s)
	JitterRatio float64       // jitter as fraction of delay (default: 0.5)
}

func defaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:  3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    5 * time.Second,
		JitterRatio: 0.5,
	}
}

// RetryWithBackoff executes fn with exponential backoff and jitter.
// It respects the circuit breaker for the given provider.
func RetryWithBackoff(cfg RetryConfig, fn func() error) error {
	if cfg.MaxRetries <= 0 {
		cfg = defaultRetryConfig()
	}

	var lastErr error
	delay := cfg.BaseDelay

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		// Don't sleep after last attempt
		if attempt == cfg.MaxRetries {
			break
		}

		// Exponential backoff with jitter
		jitter := time.Duration(rand.Float64() * cfg.JitterRatio * float64(delay))
		sleepDuration := delay + jitter
		time.Sleep(sleepDuration)

		// Double the delay for next iteration
		delay = time.Duration(math.Min(float64(delay*2), float64(cfg.MaxDelay)))
	}

	return fmt.Errorf("after %d retries: %w", cfg.MaxRetries, lastErr)
}
