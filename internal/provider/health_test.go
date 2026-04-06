package provider

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
)

// --- mockHealthProvider ---

type mockHealthProvider struct {
	name    string
	healthy bool
	err     error
}

func (m *mockHealthProvider) Name() string { return m.name }
func (m *mockHealthProvider) Send(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return nil, nil
}
func (m *mockHealthProvider) SendStream(ctx context.Context, req ChatRequest, w io.Writer) (*Usage, error) {
	return nil, nil
}
func (m *mockHealthProvider) HealthCheck(ctx context.Context) error {
	if m.healthy {
		return nil
	}
	return m.err
}

func testHealthLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// --- HealthChecker Tests ---

func TestHealthChecker_RegisterAndIsHealthy(t *testing.T) {
	hc := NewHealthChecker(testHealthLogger())
	p := &mockHealthProvider{name: "openai", healthy: true}
	hc.Register(p)

	if !hc.IsHealthy("openai") {
		t.Error("newly registered provider should be healthy")
	}
}

func TestHealthChecker_UnknownProviderNotHealthy(t *testing.T) {
	hc := NewHealthChecker(testHealthLogger())
	if hc.IsHealthy("nonexistent") {
		t.Error("unknown provider should not be healthy")
	}
}

func TestHealthChecker_RecordFailure_OpensCircuit(t *testing.T) {
	hc := NewHealthChecker(testHealthLogger())
	p := &mockHealthProvider{name: "openai", healthy: true}
	hc.Register(p)

	testErr := errors.New("connection refused")
	for i := 0; i < 3; i++ {
		hc.RecordFailure("openai", testErr)
	}

	if hc.IsHealthy("openai") {
		t.Error("provider should be unhealthy after maxFailures")
	}

	status := hc.GetStatus()
	if s, ok := status["openai"]; ok {
		if !s.CircuitOpen {
			t.Error("circuit should be open")
		}
		if s.LastError != "connection refused" {
			t.Errorf("expected 'connection refused', got %q", s.LastError)
		}
		if s.FailureCount != 3 {
			t.Errorf("expected 3 failures, got %d", s.FailureCount)
		}
	} else {
		t.Error("expected status for openai")
	}
}

func TestHealthChecker_RecordSuccess_ClosesCircuit(t *testing.T) {
	hc := NewHealthChecker(testHealthLogger())
	p := &mockHealthProvider{name: "openai", healthy: true}
	hc.Register(p)

	for i := 0; i < 5; i++ {
		hc.RecordFailure("openai", errors.New("fail"))
	}
	if hc.IsHealthy("openai") {
		t.Error("should be unhealthy after failures")
	}

	hc.RecordSuccess("openai")
	if !hc.IsHealthy("openai") {
		t.Error("should be healthy after success")
	}

	status := hc.GetStatus()
	s := status["openai"]
	if s.CircuitOpen {
		t.Error("circuit should be closed after success")
	}
	if s.FailureCount != 0 {
		t.Errorf("failure count should be reset to 0, got %d", s.FailureCount)
	}
}

func TestHealthChecker_RecordFailure_UnknownProvider(t *testing.T) {
	hc := NewHealthChecker(testHealthLogger())
	// Should not panic
	hc.RecordFailure("unknown", errors.New("test"))
	hc.RecordSuccess("unknown")
}

func TestHealthChecker_GetStatus_ReturnsCopy(t *testing.T) {
	hc := NewHealthChecker(testHealthLogger())
	p := &mockHealthProvider{name: "test", healthy: true}
	hc.Register(p)

	status1 := hc.GetStatus()
	status1["test"].Healthy = false

	status2 := hc.GetStatus()
	if !status2["test"].Healthy {
		t.Error("GetStatus should return a copy, not a reference")
	}
}

func TestHealthChecker_MultipleProviders(t *testing.T) {
	hc := NewHealthChecker(testHealthLogger())
	hc.Register(&mockHealthProvider{name: "openai", healthy: true})
	hc.Register(&mockHealthProvider{name: "anthropic", healthy: true})
	hc.Register(&mockHealthProvider{name: "ollama", healthy: true})

	hc.RecordFailure("openai", errors.New("down"))
	hc.RecordFailure("openai", errors.New("down"))
	hc.RecordFailure("openai", errors.New("down"))

	if hc.IsHealthy("openai") {
		t.Error("openai should be unhealthy")
	}
	if !hc.IsHealthy("anthropic") {
		t.Error("anthropic should still be healthy")
	}
	if !hc.IsHealthy("ollama") {
		t.Error("ollama should still be healthy")
	}
}

func TestHealthChecker_ConcurrentAccess(t *testing.T) {
	hc := NewHealthChecker(testHealthLogger())
	hc.Register(&mockHealthProvider{name: "provider", healthy: true})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			hc.RecordFailure("provider", errors.New("err"))
		}()
		go func() {
			defer wg.Done()
			hc.RecordSuccess("provider")
		}()
		go func() {
			defer wg.Done()
			hc.IsHealthy("provider")
			hc.GetStatus()
		}()
	}
	wg.Wait()
}
