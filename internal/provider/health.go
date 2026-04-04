package provider

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type HealthStatus struct {
	Healthy       bool      `json:"healthy"`
	LastCheck     time.Time `json:"last_check"`
	LastError     string    `json:"last_error,omitempty"`
	FailureCount  int       `json:"failure_count"`
	CircuitOpen   bool      `json:"circuit_open"`
}

type HealthChecker struct {
	providers map[string]Provider
	status    map[string]*HealthStatus
	mu        sync.RWMutex
	logger    *slog.Logger
	maxFailures int
	checkInterval time.Duration
}

func NewHealthChecker(logger *slog.Logger) *HealthChecker {
	return &HealthChecker{
		providers:     make(map[string]Provider),
		status:        make(map[string]*HealthStatus),
		logger:        logger,
		maxFailures:   3,
		checkInterval: 30 * time.Second,
	}
}

func (h *HealthChecker) Register(p Provider) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.providers[p.Name()] = p
	h.status[p.Name()] = &HealthStatus{Healthy: true}
}

func (h *HealthChecker) IsHealthy(name string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if s, ok := h.status[name]; ok {
		return s.Healthy && !s.CircuitOpen
	}
	return false
}

func (h *HealthChecker) RecordFailure(name string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, ok := h.status[name]
	if !ok {
		return
	}

	s.FailureCount++
	s.LastError = err.Error()
	if s.FailureCount >= h.maxFailures {
		s.CircuitOpen = true
		s.Healthy = false
		h.logger.Warn("circuit breaker opened", "provider", name, "failures", s.FailureCount)
	}
}

func (h *HealthChecker) RecordSuccess(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if s, ok := h.status[name]; ok {
		s.FailureCount = 0
		s.Healthy = true
		s.CircuitOpen = false
	}
}

func (h *HealthChecker) Start(ctx context.Context) {
	ticker := time.NewTicker(h.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.checkAll(ctx)
		}
	}
}

func (h *HealthChecker) checkAll(ctx context.Context) {
	h.mu.RLock()
	providers := make(map[string]Provider)
	for k, v := range h.providers {
		providers[k] = v
	}
	h.mu.RUnlock()

	for name, p := range providers {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := p.HealthCheck(checkCtx)
		cancel()

		if err != nil {
			h.RecordFailure(name, err)
		} else {
			h.RecordSuccess(name)
			h.mu.Lock()
			h.status[name].LastCheck = time.Now()
			h.mu.Unlock()
		}
	}
}

func (h *HealthChecker) GetStatus() map[string]*HealthStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make(map[string]*HealthStatus)
	for k, v := range h.status {
		copy := *v
		result[k] = &copy
	}
	return result
}
