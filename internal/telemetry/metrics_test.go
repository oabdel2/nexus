package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// Counter tests
// ---------------------------------------------------------------------------

func TestNewMetrics(t *testing.T) {
	m := NewMetrics()
	if m == nil {
		t.Fatal("NewMetrics returned nil")
	}
	if m.RequestsTotal.Load() != 0 {
		t.Errorf("expected RequestsTotal=0, got %d", m.RequestsTotal.Load())
	}
	if m.startTime.IsZero() {
		t.Error("startTime should be set")
	}
}

func TestRecordRequest_Basic(t *testing.T) {
	m := NewMetrics()
	m.RecordRequest("openai", "gpt-4", "standard", 100, 0.005, 250, false)

	if m.RequestsTotal.Load() != 1 {
		t.Errorf("expected RequestsTotal=1, got %d", m.RequestsTotal.Load())
	}
}

func TestRecordRequest_CacheHit(t *testing.T) {
	m := NewMetrics()
	m.RecordRequest("cache", "l1", "cached", 0, 0, 5, true)

	if m.RequestsTotal.Load() != 1 {
		t.Errorf("expected RequestsTotal=1, got %d", m.RequestsTotal.Load())
	}

	// Cache hit should be recorded by layer
	hits := sumMap(&m.cacheHitsByLayer)
	if hits != 1 {
		t.Errorf("expected 1 cache hit, got %d", hits)
	}
}

func TestRecordRequest_CacheMiss(t *testing.T) {
	m := NewMetrics()
	m.RecordRequest("openai", "gpt-4", "fast", 50, 0.001, 100, false)

	if m.cacheMissesTotal.Load() != 1 {
		t.Errorf("expected 1 cache miss, got %d", m.cacheMissesTotal.Load())
	}
}

func TestRecordRequest_WithTokens(t *testing.T) {
	m := NewMetrics()
	m.RecordRequest("openai", "gpt-4", "standard", 500, 0.01, 200, false)

	var totalTokens int64
	m.tokensByDirection.Range(func(_, v any) bool {
		totalTokens += v.(*atomic.Int64).Load()
		return true
	})
	if totalTokens != 500 {
		t.Errorf("expected 500 tokens, got %d", totalTokens)
	}
}

func TestRecordRequest_WithCost(t *testing.T) {
	m := NewMetrics()
	m.RecordRequest("openai", "gpt-4", "standard", 100, 0.005, 200, false)

	var totalMicros int64
	m.costByLabel.Range(func(_, v any) bool {
		totalMicros += v.(*atomic.Int64).Load()
		return true
	})
	// 0.005 * 1_000_000 = 5000
	if totalMicros != 5000 {
		t.Errorf("expected 5000 microdollars, got %d", totalMicros)
	}
}

func TestRecordCacheHit_ByLayer(t *testing.T) {
	m := NewMetrics()
	m.RecordCacheHit("l1")
	m.RecordCacheHit("l1")
	m.RecordCacheHit("bm25")
	m.RecordCacheHit("semantic")

	total := sumMap(&m.cacheHitsByLayer)
	if total != 4 {
		t.Errorf("expected 4 total cache hits, got %d", total)
	}
}

func TestRecordCacheMiss(t *testing.T) {
	m := NewMetrics()
	m.RecordCacheMiss()
	m.RecordCacheMiss()

	if m.cacheMissesTotal.Load() != 2 {
		t.Errorf("expected 2 cache misses, got %d", m.cacheMissesTotal.Load())
	}
}

func TestRecordCacheEviction(t *testing.T) {
	m := NewMetrics()
	m.RecordCacheEviction("l1")
	m.RecordCacheEviction("l1")
	m.RecordCacheEviction("semantic")

	total := sumMap(&m.cacheEvictions)
	if total != 3 {
		t.Errorf("expected 3 evictions, got %d", total)
	}
}

func TestRecordSecurityBlock(t *testing.T) {
	m := NewMetrics()
	m.RecordSecurityBlock("prompt_injection")
	m.RecordSecurityBlock("rate_limit")
	m.RecordSecurityBlock("auth_failure")
	m.RecordSecurityBlock("rbac_denied")
	m.RecordSecurityBlock("prompt_injection")

	total := sumMap(&m.securityBlocks)
	if total != 5 {
		t.Errorf("expected 5 security blocks, got %d", total)
	}

	// Verify prompt_injection count
	v, ok := m.securityBlocks.Load(`reason="prompt_injection"`)
	if !ok {
		t.Fatal("prompt_injection key not found")
	}
	if v.(*atomic.Int64).Load() != 2 {
		t.Errorf("expected 2 prompt_injection blocks, got %d", v.(*atomic.Int64).Load())
	}
}

func TestRecordSynonymPromotion(t *testing.T) {
	m := NewMetrics()
	m.RecordSynonymPromotion()
	m.RecordSynonymPromotion()

	if m.synonymPromotions.Load() != 2 {
		t.Errorf("expected 2 promotions, got %d", m.synonymPromotions.Load())
	}
}

// ---------------------------------------------------------------------------
// Histogram tests
// ---------------------------------------------------------------------------

func TestRecordDuration_RequestHistogram(t *testing.T) {
	m := NewMetrics()
	// Observe a 0.03s request (should fall in le=0.05 and above)
	m.RecordDuration("request", "fast", 0.03)

	v, ok := m.requestDuration.instances.Load(`tier="fast"`)
	if !ok {
		t.Fatal("histogram instance for tier=fast not found")
	}
	h := v.(*histogram)

	// Boundaries: 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0
	// 0.03 > 0.01, 0.03 > 0.025, 0.03 <= 0.05 → buckets 2..9 and +Inf should be 1
	if h.buckets[0].Load() != 0 {
		t.Errorf("bucket le=0.01 should be 0, got %d", h.buckets[0].Load())
	}
	if h.buckets[1].Load() != 0 {
		t.Errorf("bucket le=0.025 should be 0, got %d", h.buckets[1].Load())
	}
	if h.buckets[2].Load() != 1 {
		t.Errorf("bucket le=0.05 should be 1, got %d", h.buckets[2].Load())
	}
	// +Inf (last bucket)
	if h.buckets[len(m.requestDuration.boundaries)].Load() != 1 {
		t.Errorf("+Inf bucket should be 1, got %d", h.buckets[len(m.requestDuration.boundaries)].Load())
	}
	if h.count.Load() != 1 {
		t.Errorf("count should be 1, got %d", h.count.Load())
	}
}

func TestRecordCacheLookup_Histogram(t *testing.T) {
	m := NewMetrics()
	m.RecordCacheLookup("l1", 0.0003) // between 0.0001 and 0.0005

	v, ok := m.cacheLookup.instances.Load(`layer="l1"`)
	if !ok {
		t.Fatal("histogram instance for layer=l1 not found")
	}
	h := v.(*histogram)

	// Boundaries: 0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1
	// 0.0003 > 0.0001, 0.0003 <= 0.0005
	if h.buckets[0].Load() != 0 {
		t.Errorf("bucket le=0.0001 should be 0, got %d", h.buckets[0].Load())
	}
	if h.buckets[1].Load() != 1 {
		t.Errorf("bucket le=0.0005 should be 1, got %d", h.buckets[1].Load())
	}
}

func TestRecordEmbedding_Histogram(t *testing.T) {
	m := NewMetrics()
	m.RecordEmbedding(0.15) // between 0.1 and 0.25

	v, ok := m.embeddingDuration.instances.Load("")
	if !ok {
		t.Fatal("histogram instance for empty labels not found")
	}
	h := v.(*histogram)

	// Boundaries: 0.01, 0.05, 0.1, 0.25, 0.5, 1.0
	// 0.15 > 0.1, 0.15 <= 0.25
	if h.buckets[2].Load() != 0 {
		t.Errorf("bucket le=0.1 should be 0, got %d", h.buckets[2].Load())
	}
	if h.buckets[3].Load() != 1 {
		t.Errorf("bucket le=0.25 should be 1, got %d", h.buckets[3].Load())
	}
	if h.count.Load() != 1 {
		t.Errorf("count should be 1, got %d", h.count.Load())
	}
}

// ---------------------------------------------------------------------------
// Gauge tests
// ---------------------------------------------------------------------------

func TestSetCacheEntries(t *testing.T) {
	m := NewMetrics()
	m.SetCacheEntries("l1", 42)
	m.SetCacheEntries("semantic", 10)

	v, ok := m.cacheEntries.Load(`layer="l1"`)
	if !ok {
		t.Fatal("l1 entry not found")
	}
	if v.(*atomic.Int64).Load() != 42 {
		t.Errorf("expected 42 l1 entries, got %d", v.(*atomic.Int64).Load())
	}
}

func TestActiveRequests(t *testing.T) {
	m := NewMetrics()
	m.IncActiveRequests()
	m.IncActiveRequests()
	m.IncActiveRequests()
	m.DecActiveRequests()

	if m.activeRequests.Load() != 2 {
		t.Errorf("expected 2 active requests, got %d", m.activeRequests.Load())
	}
}

func TestCacheHitRate(t *testing.T) {
	m := NewMetrics()
	m.RecordCacheHit("l1")
	m.RecordCacheHit("l1")
	m.RecordCacheHit("bm25")
	m.RecordCacheMiss()

	totalHits := sumMap(&m.cacheHitsByLayer)
	totalMisses := m.cacheMissesTotal.Load()
	total := totalHits + totalMisses
	rate := float64(totalHits) / float64(total)
	if rate != 0.75 {
		t.Errorf("expected hit rate 0.75, got %f", rate)
	}
}

// ---------------------------------------------------------------------------
// Handler / Prometheus format tests
// ---------------------------------------------------------------------------

func TestHandler_PrometheusFormat(t *testing.T) {
	m := NewMetrics()
	m.RecordRequest("openai", "gpt-4", "standard", 100, 0.005, 250, false)
	m.RecordSecurityBlock("prompt_injection")
	m.RecordCacheEviction("l1")
	m.RecordSynonymPromotion()
	m.RecordEmbedding(0.1)
	m.SetCacheEntries("l1", 50)
	m.IncActiveRequests()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	m.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()

	// Content-Type
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("unexpected Content-Type: %s", ct)
	}

	// Check TYPE and HELP for key metrics
	requiredTypes := []string{
		"# TYPE nexus_requests_total counter",
		"# TYPE nexus_cache_hits_total counter",
		"# TYPE nexus_cache_misses_total counter",
		"# TYPE nexus_tokens_total counter",
		"# TYPE nexus_cost_dollars_total counter",
		"# TYPE nexus_security_blocks_total counter",
		"# TYPE nexus_cache_evictions_total counter",
		"# TYPE nexus_synonym_promotions_total counter",
		"# TYPE nexus_cache_entries gauge",
		"# TYPE nexus_active_requests gauge",
		"# TYPE nexus_uptime_seconds gauge",
		"# TYPE nexus_cache_hit_rate gauge",
	}
	for _, rt := range requiredTypes {
		if !strings.Contains(body, rt) {
			t.Errorf("missing TYPE line: %s", rt)
		}
	}

	requiredHelp := []string{
		"# HELP nexus_requests_total",
		"# HELP nexus_cache_misses_total",
		"# HELP nexus_uptime_seconds",
	}
	for _, rh := range requiredHelp {
		if !strings.Contains(body, rh) {
			t.Errorf("missing HELP line: %s", rh)
		}
	}

	// Check labeled values
	if !strings.Contains(body, `nexus_requests_total{provider="openai",model="gpt-4",tier="standard",status="ok"} 1`) {
		t.Error("missing nexus_requests_total labeled entry")
	}
	if !strings.Contains(body, `nexus_security_blocks_total{reason="prompt_injection"} 1`) {
		t.Error("missing nexus_security_blocks_total entry")
	}
	if !strings.Contains(body, `nexus_cache_entries{layer="l1"} 50`) {
		t.Error("missing nexus_cache_entries gauge")
	}
	if !strings.Contains(body, "nexus_active_requests 1") {
		t.Error("missing nexus_active_requests gauge")
	}
}

func TestHandler_ContainsHistogramBuckets(t *testing.T) {
	m := NewMetrics()
	m.RecordRequest("openai", "gpt-4", "fast", 100, 0.001, 50, false)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	m.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()

	if !strings.Contains(body, "# TYPE nexus_request_duration_seconds histogram") {
		t.Error("missing histogram TYPE line")
	}
	if !strings.Contains(body, `nexus_request_duration_seconds_bucket{tier="fast",le="+Inf"}`) {
		t.Error("missing +Inf bucket")
	}
	if !strings.Contains(body, `nexus_request_duration_seconds_sum{tier="fast"}`) {
		t.Error("missing _sum line")
	}
	if !strings.Contains(body, `nexus_request_duration_seconds_count{tier="fast"}`) {
		t.Error("missing _count line")
	}
}

func TestHandler_EmbeddingHistogramNoLabels(t *testing.T) {
	m := NewMetrics()
	m.RecordEmbedding(0.5)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	m.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()

	// Unlabeled histogram: le should be the only label in bucket
	if !strings.Contains(body, `nexus_embedding_duration_seconds_bucket{le="0.5"} 1`) {
		t.Error("missing embedding bucket le=0.5")
	}
	if !strings.Contains(body, `nexus_embedding_duration_seconds_bucket{le="+Inf"} 1`) {
		t.Error("missing embedding +Inf bucket")
	}
	if !strings.Contains(body, "nexus_embedding_duration_seconds_sum ") {
		t.Error("missing unlabeled _sum")
	}
	if !strings.Contains(body, "nexus_embedding_duration_seconds_count 1") {
		t.Error("missing unlabeled _count")
	}
}

// ---------------------------------------------------------------------------
// Concurrency test
// ---------------------------------------------------------------------------

func TestConcurrentAccess(t *testing.T) {
	m := NewMetrics()
	const goroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				m.RecordRequest("openai", "gpt-4", "fast", 10, 0.001, 50, j%3 == 0)
				m.RecordSecurityBlock("rate_limit")
				m.RecordCacheEviction("l1")
				m.RecordSynonymPromotion()
				m.RecordCacheLookup("l1", 0.001)
				m.RecordEmbedding(0.05)
				m.SetCacheEntries("l1", int64(j))
				m.IncActiveRequests()
				m.DecActiveRequests()
			}
		}()
	}
	wg.Wait()

	expected := int64(goroutines * opsPerGoroutine)
	if m.RequestsTotal.Load() != expected {
		t.Errorf("expected %d requests, got %d", expected, m.RequestsTotal.Load())
	}

	// Handler should not panic under concurrent load
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}
