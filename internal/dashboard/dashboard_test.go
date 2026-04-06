package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestNewEventBus(t *testing.T) {
	eb := NewEventBus()
	if eb == nil {
		t.Fatal("expected non-nil EventBus")
	}
	if len(eb.recentRequests) != 0 {
		t.Fatal("expected empty recent requests")
	}
}

func TestPush_AggregatesRequestCount(t *testing.T) {
	eb := NewEventBus()
	eb.Push(RequestEvent{WorkflowID: "w1", TierSelected: "cheap", Cost: 0.01, LatencyMs: 100})
	eb.Push(RequestEvent{WorkflowID: "w2", TierSelected: "premium", Cost: 0.05, LatencyMs: 200})

	eb.mu.RLock()
	defer eb.mu.RUnlock()
	if eb.totalRequests != 2 {
		t.Fatalf("expected 2 total requests, got %d", eb.totalRequests)
	}
}

func TestPush_TracksCacheHits(t *testing.T) {
	eb := NewEventBus()
	eb.Push(RequestEvent{CacheHit: true, TierSelected: "cached"})
	eb.Push(RequestEvent{CacheHit: false, TierSelected: "cheap"})

	eb.mu.RLock()
	defer eb.mu.RUnlock()
	if eb.cacheHits != 1 {
		t.Fatalf("expected 1 cache hit, got %d", eb.cacheHits)
	}
}

func TestPush_TierCounts(t *testing.T) {
	eb := NewEventBus()
	tiers := []string{"economy", "cheap", "mid", "premium", "cached"}
	for _, tier := range tiers {
		eb.Push(RequestEvent{TierSelected: tier})
	}

	eb.mu.RLock()
	defer eb.mu.RUnlock()
	if eb.economyCount != 1 || eb.cheapCount != 1 || eb.midCount != 1 || eb.premiumCount != 1 || eb.cachedCount != 1 {
		t.Fatal("tier counts mismatch")
	}
}

func TestPush_ProviderStats(t *testing.T) {
	eb := NewEventBus()
	eb.Push(RequestEvent{Provider: "openai", LatencyMs: 100, TierSelected: "mid"})
	eb.Push(RequestEvent{Provider: "openai", LatencyMs: 200, TierSelected: "mid"})
	eb.Push(RequestEvent{Provider: "anthropic", LatencyMs: 50, TierSelected: "cheap"})

	eb.mu.RLock()
	defer eb.mu.RUnlock()
	ps := eb.providerStats["openai"]
	if ps.Requests != 2 {
		t.Fatalf("expected 2 openai requests, got %d", ps.Requests)
	}
	if ps.AvgLatencyMs != 150 {
		t.Fatalf("expected avg latency 150, got %f", ps.AvgLatencyMs)
	}
}

func TestPush_EmptyProvider(t *testing.T) {
	eb := NewEventBus()
	eb.Push(RequestEvent{TierSelected: "cheap"})
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	if _, ok := eb.providerStats["unknown"]; !ok {
		t.Fatal("expected 'unknown' provider stat for empty provider")
	}
}

func TestPush_RecentRequestsRingBuffer(t *testing.T) {
	eb := NewEventBus()
	for i := 0; i < 110; i++ {
		eb.Push(RequestEvent{WorkflowID: "w", TierSelected: "cheap", Cost: float64(i)})
	}
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	if len(eb.recentRequests) != 100 {
		t.Fatalf("expected ring buffer capped at 100, got %d", len(eb.recentRequests))
	}
	// Oldest should be index 10 (first 10 were evicted)
	if eb.recentRequests[0].Cost != 10 {
		t.Fatalf("expected oldest cost=10, got %f", eb.recentRequests[0].Cost)
	}
}

func TestUpdateWorkflow(t *testing.T) {
	eb := NewEventBus()
	eb.UpdateWorkflow("wf1", 10.0, 8.0, 0.8, 2, 2.0)

	eb.mu.RLock()
	defer eb.mu.RUnlock()
	wf, ok := eb.workflows["wf1"]
	if !ok {
		t.Fatal("expected workflow wf1")
	}
	if wf.Budget != 10.0 || wf.BudgetLeft != 8.0 || wf.BudgetRatio != 0.8 {
		t.Fatalf("unexpected workflow state: %+v", wf)
	}
}

func TestRecordCascade(t *testing.T) {
	eb := NewEventBus()
	eb.RecordCascade(true)
	eb.RecordCascade(false)
	eb.RecordCascade(true)

	eb.mu.RLock()
	defer eb.mu.RUnlock()
	if eb.cascadeAttempts != 3 {
		t.Fatalf("expected 3 cascade attempts, got %d", eb.cascadeAttempts)
	}
	if eb.cascadeAccepted != 2 {
		t.Fatalf("expected 2 cascade accepted, got %d", eb.cascadeAccepted)
	}
}

func TestRecordCompressionSaved(t *testing.T) {
	eb := NewEventBus()
	eb.RecordCompressionSaved(100)
	eb.RecordCompressionSaved(200)

	eb.mu.RLock()
	defer eb.mu.RUnlock()
	if eb.compressionTokensSaved != 300 {
		t.Fatalf("expected 300 tokens saved, got %d", eb.compressionTokensSaved)
	}
}

func TestServeStats_JSONOutput(t *testing.T) {
	eb := NewEventBus()
	eb.Push(RequestEvent{WorkflowID: "w1", TierSelected: "cheap", Cost: 0.01, LatencyMs: 50, Provider: "openai"})
	eb.UpdateWorkflow("w1", 10.0, 9.99, 0.999, 1, 0.01)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/api/stats", nil)
	w := httptest.NewRecorder()
	eb.ServeStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"stats", "workflows", "recent_requests", "provider_stats"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("missing key %s in response", key)
		}
	}
}

func TestServeStats_EmptyBus(t *testing.T) {
	eb := NewEventBus()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/api/stats", nil)
	w := httptest.NewRecorder()
	eb.ServeStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

func TestSubscribeBroadcast(t *testing.T) {
	eb := NewEventBus()
	ch := eb.subscribe()
	defer eb.unsubscribe(ch)

	eb.Push(RequestEvent{WorkflowID: "w1", TierSelected: "cheap", Cost: 0.01})

	select {
	case data := <-ch:
		var update DashboardUpdate
		if err := json.Unmarshal(data, &update); err != nil {
			t.Fatalf("invalid SSE data: %v", err)
		}
		if update.Type != "request" {
			t.Fatalf("expected type 'request', got %s", update.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for broadcast")
	}
}

func TestConcurrentPush(t *testing.T) {
	eb := NewEventBus()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			eb.Push(RequestEvent{WorkflowID: "w", TierSelected: "cheap", Cost: 0.01})
		}(i)
	}
	wg.Wait()

	eb.mu.RLock()
	defer eb.mu.RUnlock()
	if eb.totalRequests != 100 {
		t.Fatalf("expected 100 requests, got %d", eb.totalRequests)
	}
}

func TestConcurrentUpdateWorkflow(t *testing.T) {
	eb := NewEventBus()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			eb.UpdateWorkflow("wf", 10, float64(10-i), 0.5, i, float64(i))
		}(i)
	}
	wg.Wait()
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	if _, ok := eb.workflows["wf"]; !ok {
		t.Fatal("expected workflow present after concurrent updates")
	}
}

func TestGetWorkflowsSorted(t *testing.T) {
	eb := NewEventBus()
	eb.UpdateWorkflow("cheap", 10, 9, 0.9, 1, 1.0)
	eb.UpdateWorkflow("expensive", 10, 2, 0.2, 5, 8.0)
	eb.UpdateWorkflow("mid", 10, 5, 0.5, 3, 5.0)

	eb.mu.RLock()
	wfs := eb.getWorkflowsLocked()
	eb.mu.RUnlock()

	if len(wfs) != 3 {
		t.Fatalf("expected 3 workflows, got %d", len(wfs))
	}
	if wfs[0].ID != "expensive" {
		t.Fatalf("expected most expensive first, got %s", wfs[0].ID)
	}
}

func TestGetProviderStatsSorted(t *testing.T) {
	eb := NewEventBus()
	for i := 0; i < 5; i++ {
		eb.Push(RequestEvent{Provider: "openai", TierSelected: "cheap"})
	}
	for i := 0; i < 2; i++ {
		eb.Push(RequestEvent{Provider: "anthropic", TierSelected: "cheap"})
	}

	eb.mu.RLock()
	stats := eb.getProviderStatsLocked()
	eb.mu.RUnlock()

	if len(stats) < 2 {
		t.Fatalf("expected at least 2 providers, got %d", len(stats))
	}
	if stats[0].Name != "openai" {
		t.Fatalf("expected openai first (most requests), got %s", stats[0].Name)
	}
}

func TestSSE_InitMessage(t *testing.T) {
	eb := NewEventBus()
	eb.Push(RequestEvent{WorkflowID: "w1", TierSelected: "cheap", Cost: 0.01})

	req := httptest.NewRequest(http.MethodGet, "/dashboard/events", nil)
	cctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(cctx)

	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		eb.ServeSSE(w, req)
		close(done)
	}()

	// Give it time to write init message then cancel
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	body := w.Body.String()
	if len(body) == 0 {
		t.Fatal("expected SSE init message")
	}
	if body[:5] != "data:" {
		t.Fatalf("expected SSE format 'data: ...', got %q", body[:20])
	}
}
