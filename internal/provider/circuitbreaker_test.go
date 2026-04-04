package provider

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCircuitBreakerStates(t *testing.T) {
	cb := NewCircuitBreaker("test-provider", CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		MaxTimeout:       1 * time.Second,
		HalfOpenMax:      1,
	})

	// Initially closed
	if cb.State() != StateClosed {
		t.Fatalf("expected closed, got %s", cb.State())
	}

	// Should allow requests
	if !cb.Allow() {
		t.Fatal("expected allow in closed state")
	}
}

func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker("test", CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 1,
		Timeout:          100 * time.Millisecond,
		MaxTimeout:       1 * time.Second,
		HalfOpenMax:      1,
	})

	// Record failures up to threshold
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateClosed {
		t.Fatal("should still be closed before threshold")
	}

	cb.RecordFailure() // hits threshold
	if cb.State() != StateOpen {
		t.Fatalf("expected open after %d failures, got %s", 3, cb.State())
	}

	// Should reject requests
	if cb.Allow() {
		t.Fatal("should not allow in open state")
	}
}

func TestCircuitBreakerTransitionsToHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker("test", CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          50 * time.Millisecond,
		MaxTimeout:       1 * time.Second,
		HalfOpenMax:      1,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatal("expected open")
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Should transition to half-open
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected half-open, got %s", cb.State())
	}

	// Should allow one probe request
	if !cb.Allow() {
		t.Fatal("should allow in half-open")
	}
}

func TestCircuitBreakerRecovery(t *testing.T) {
	cb := NewCircuitBreaker("test", CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          50 * time.Millisecond,
		MaxTimeout:       1 * time.Second,
		HalfOpenMax:      1,
	})

	// Open circuit
	cb.RecordFailure()
	cb.RecordFailure()

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Probe succeeds
	cb.Allow()
	cb.RecordSuccess()

	if cb.State() != StateClosed {
		t.Fatalf("expected closed after recovery, got %s", cb.State())
	}

	// Should allow normal requests
	if !cb.Allow() {
		t.Fatal("should allow after recovery")
	}
}

func TestCircuitBreakerHalfOpenFailReopens(t *testing.T) {
	cb := NewCircuitBreaker("test", CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          50 * time.Millisecond,
		MaxTimeout:       500 * time.Millisecond,
		HalfOpenMax:      1,
	})

	// Open circuit
	cb.RecordFailure()
	cb.RecordFailure()

	// Wait, then fail in half-open
	time.Sleep(60 * time.Millisecond)
	cb.Allow()
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Fatalf("expected re-open, got %s", cb.State())
	}

	// Backoff should be doubled
	if cb.currentTimeout < 100*time.Millisecond {
		t.Fatalf("expected doubled timeout, got %s", cb.currentTimeout)
	}
}

func TestCircuitBreakerSuccessResetsFailures(t *testing.T) {
	cb := NewCircuitBreaker("test", CircuitBreakerConfig{
		FailureThreshold: 3,
		Timeout:          100 * time.Millisecond,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess() // should reset
	cb.RecordFailure()
	cb.RecordFailure()

	// Should still be closed (only 2 consecutive)
	if cb.State() != StateClosed {
		t.Fatal("success should have reset failure count")
	}
}

func TestCircuitBreakerStats(t *testing.T) {
	cb := NewCircuitBreaker("my-provider", CircuitBreakerConfig{
		FailureThreshold: 3,
		Timeout:          100 * time.Millisecond,
	})

	cb.RecordSuccess()
	cb.RecordFailure()
	cb.RecordSuccess()

	stats := cb.Stats()
	if stats["provider"] != "my-provider" {
		t.Errorf("expected my-provider, got %v", stats["provider"])
	}
	if stats["total_requests"].(int64) != 3 {
		t.Errorf("expected 3 total requests, got %v", stats["total_requests"])
	}
	if stats["total_failures"].(int64) != 1 {
		t.Errorf("expected 1 failure, got %v", stats["total_failures"])
	}
}

func TestCircuitBreakerStateString(t *testing.T) {
	tests := []struct {
		state CBState
		want  string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{CBState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("CBState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestCircuitBreakerOnStateChange(t *testing.T) {
	var changes []string
	var mu sync.Mutex

	cb := NewCircuitBreaker("test", CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          50 * time.Millisecond,
	})

	cb.OnStateChange = func(provider string, from, to CBState) {
		mu.Lock()
		changes = append(changes, from.String()+"->"+to.String())
		mu.Unlock()
	}

	cb.RecordFailure()
	cb.RecordFailure() // opens
	time.Sleep(20 * time.Millisecond) // let callback fire

	mu.Lock()
	count := len(changes)
	mu.Unlock()

	if count == 0 {
		t.Fatal("expected state change callback to fire")
	}
}

// --- ProviderPool tests ---

func TestProviderPoolRegisterAndGet(t *testing.T) {
	pool := NewProviderPool(defaultCBConfig())
	pool.Register("openai")
	pool.Register("anthropic")

	if pool.Get("openai") == nil {
		t.Fatal("expected openai breaker")
	}
	if pool.Get("anthropic") == nil {
		t.Fatal("expected anthropic breaker")
	}
	if pool.Get("unknown") != nil {
		t.Fatal("expected nil for unknown")
	}
}

func TestProviderPoolAvailability(t *testing.T) {
	pool := NewProviderPool(CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          100 * time.Millisecond,
	})
	pool.Register("openai")

	// Initially available
	if !pool.IsAvailable("openai") {
		t.Fatal("should be available initially")
	}

	// Break it
	cb := pool.Get("openai")
	cb.RecordFailure()
	cb.RecordFailure()

	if pool.IsAvailable("openai") {
		t.Fatal("should not be available when open")
	}

	// Unknown provider should be available
	if !pool.IsAvailable("unknown") {
		t.Fatal("unknown providers should be available")
	}
}

func TestProviderPoolAllStats(t *testing.T) {
	pool := NewProviderPool(defaultCBConfig())
	pool.Register("a")
	pool.Register("b")

	stats := pool.AllStats()
	if len(stats) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(stats))
	}
}

// --- RetryWithBackoff tests ---

func TestRetrySucceedsImmediately(t *testing.T) {
	calls := 0
	err := RetryWithBackoff(RetryConfig{MaxRetries: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestRetrySucceedsOnThirdAttempt(t *testing.T) {
	calls := 0
	err := RetryWithBackoff(RetryConfig{MaxRetries: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}, func() error {
		calls++
		if calls < 3 {
			return errors.New("not yet")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success on attempt 3, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestRetryExhausted(t *testing.T) {
	calls := 0
	err := RetryWithBackoff(RetryConfig{MaxRetries: 2, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}, func() error {
		calls++
		return errors.New("always fail")
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if calls != 3 { // initial + 2 retries
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestRetryBackoffIncreases(t *testing.T) {
	start := time.Now()
	RetryWithBackoff(RetryConfig{MaxRetries: 3, BaseDelay: 10 * time.Millisecond, MaxDelay: 200 * time.Millisecond, JitterRatio: 0.1}, func() error {
		return errors.New("fail")
	})
	elapsed := time.Since(start)
	// 10ms + 20ms + 40ms = 70ms minimum, with jitter could be up to ~85ms
	if elapsed < 50*time.Millisecond {
		t.Fatalf("backoff too fast: %s", elapsed)
	}
}

func TestCircuitBreakerConcurrency(t *testing.T) {
	cb := NewCircuitBreaker("concurrent", CircuitBreakerConfig{
		FailureThreshold: 100,
		Timeout:          100 * time.Millisecond,
	})

	var wg sync.WaitGroup
	var ops int64

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cb.Allow()
				if j%2 == 0 {
					cb.RecordSuccess()
				} else {
					cb.RecordFailure()
				}
				atomic.AddInt64(&ops, 1)
			}
		}(i)
	}

	wg.Wait()

	if atomic.LoadInt64(&ops) != 10000 {
		t.Fatalf("expected 10000 ops, got %d", atomic.LoadInt64(&ops))
	}
	// No panics = thread-safe
}

func TestCircuitBreakerExponentialBackoff(t *testing.T) {
	cb := NewCircuitBreaker("test", CircuitBreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		Timeout:          10 * time.Millisecond,
		MaxTimeout:       100 * time.Millisecond,
		HalfOpenMax:      1,
	})

	// First open: timeout = 10ms
	cb.RecordFailure()
	if cb.currentTimeout != 10*time.Millisecond {
		t.Fatalf("expected 10ms, got %s", cb.currentTimeout)
	}

	// Wait, probe, fail again → timeout doubles
	time.Sleep(15 * time.Millisecond)
	cb.Allow()
	cb.RecordFailure()
	if cb.currentTimeout < 20*time.Millisecond {
		t.Fatalf("expected >= 20ms after backoff, got %s", cb.currentTimeout)
	}

	// Again: should keep doubling
	time.Sleep(time.Duration(float64(cb.currentTimeout) * 1.1))
	cb.Allow()
	cb.RecordFailure()
	if cb.currentTimeout < 40*time.Millisecond {
		t.Fatalf("expected >= 40ms, got %s", cb.currentTimeout)
	}
}

func TestCircuitBreakerMaxTimeout(t *testing.T) {
	cb := NewCircuitBreaker("test", CircuitBreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		Timeout:          10 * time.Millisecond,
		MaxTimeout:       30 * time.Millisecond,
		HalfOpenMax:      1,
	})

	// Force multiple opens to test max cap
	for i := 0; i < 10; i++ {
		cb.RecordFailure()
		time.Sleep(time.Duration(float64(cb.currentTimeout) * 1.1))
		cb.Allow()
	}

	if cb.currentTimeout > 30*time.Millisecond {
		t.Fatalf("timeout %s exceeded max %s", cb.currentTimeout, 30*time.Millisecond)
	}
}

func TestErrCircuitOpenError(t *testing.T) {
	if ErrCircuitOpen.Error() != "circuit breaker is open" {
		t.Fatalf("unexpected error message: %s", ErrCircuitOpen.Error())
	}
	if ErrAllFailed.Error() != "all providers failed" {
		t.Fatalf("unexpected error message: %s", ErrAllFailed.Error())
	}
}
