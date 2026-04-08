# Nexus — Architecture Review

> **Version:** 1.0 · **Date:** July 2025 · **Reviewer:** Backend Architect  
> **Scope:** Core request path, caching, routing, providers, configuration  
> **Codebase:** Go 1.23, single dependency (`gopkg.in/yaml.v3`), 923+ tests

---

## Table of Contents

1. [Package Structure Review](#1-package-structure-review)
2. [Scalability Assessment](#2-scalability-assessment)
3. [Reliability](#3-reliability)
4. [Top 5 Architectural Improvements](#4-top-5-architectural-improvements)
5. [Implemented Improvements](#5-implemented-improvements)

---

## 1. Package Structure Review

### 1.1 Package Dependency Graph

```
cmd/nexus/main.go
  └── internal/gateway          (server + handlers)
        ├── internal/config     (YAML configuration)
        ├── internal/cache      (3-layer cache: exact, BM25, semantic)
        ├── internal/router     (classifier, adaptive, cascade)
        ├── internal/provider   (OpenAI, Anthropic, circuit breaker, health)
        ├── internal/compress   (prompt compression)
        ├── internal/eval       (confidence scoring, confidence map)
        ├── internal/workflow   (workflow state tracking, feedback)
        ├── internal/security   (17 middleware layers)
        ├── internal/telemetry  (Prometheus metrics, tracing, cost)
        ├── internal/billing    (subscriptions, API keys, Stripe)
        ├── internal/dashboard  (real-time event bus, SSE)
        ├── internal/mcp        (Model Context Protocol server)
        ├── internal/events     (event bus, webhooks)
        ├── internal/plugin     (plugin registry)
        ├── internal/experiment (A/B testing)
        ├── internal/notification (SMTP, event log)
        └── internal/storage    (Redis, Qdrant, memory backends)
```

### 1.2 Dependency Analysis — No Cycles Detected ✅

The dependency graph is a **clean DAG** (directed acyclic graph):

- **Leaf packages** (no internal imports): `config`, `compress`, `eval`, `mcp`, `events`, `plugin`, `notification`, `storage`
- **Mid-level packages**: `cache` (imports nothing internal), `router` (imports `config`, `eval`), `provider` (imports nothing internal), `workflow` (imports nothing internal)
- **Hub package**: `gateway` (imports nearly everything — this is the composition root)

**Verdict:** No circular dependencies. The `gateway` package acts as the sole orchestration layer, which is correct for this architecture. Leaf packages are properly decoupled.

### 1.3 Interface Design at Boundaries

| Boundary | Interface | Quality |
|----------|-----------|---------|
| Provider abstraction | `provider.Provider` (Name, Send, SendStream, HealthCheck) | ✅ **Clean** — 4-method interface, easy to implement new providers |
| Cache contract | `cache.Store` (Lookup, StoreResponse, Stats, Stop) | ⚠️ **Concrete struct, not interface** — testability concern |
| Router contract | `router.Router` / `router.AdaptiveRouter` | ⚠️ **No shared interface** — gateway uses concrete types and `if` checks |
| Security middleware | `security.Middleware` (func signature) | ✅ **Clean** — standard `http.Handler` middleware pattern |
| Event bus | `events.EventBus` | ⚠️ **Concrete struct** — no interface for testing/mocking |
| Config | `config.Config` — deep YAML struct | ✅ **Good** — single source of truth, env var expansion supported |

### 1.4 Separation of Concerns

**Strengths:**
- Request handling is cleanly decomposed into phases via `handler_chat_phases.go`: parse → compress → guard → cache → route → cascade → select provider → send → record metrics
- Each phase is a method on `Server` taking a `chatContext`, making the pipeline explicit and testable
- Provider interface is minimal and correct (4 methods)
- Security middleware follows standard Go patterns (chainable `http.Handler` wrappers)

**Concerns:**
- **`Server` struct is a god object** — 25+ fields, owns everything from cache to billing to MCP. This is manageable at current scale but will become painful as features grow.
- **`handler_chat_phases.go` mixes concerns** — each phase method does business logic AND telemetry/metrics/dashboard/events recording. This makes phases ~40% longer than necessary and harder to test in isolation.
- **Duplicate health tracking** — both `HealthChecker` and `CircuitBreaker (Pool)` track provider health independently with different thresholds and state machines. The `HealthChecker` has its own "circuit open" flag that shadows the `Pool`'s circuit breaker state.

---

## 2. Scalability Assessment

### 2.1 At 10K RPM (~167 RPS)

**Expected behavior: Handles well. ✅**

- Admission control via `requestSem` (default 500 concurrent) prevents goroutine explosion
- HTTP/2 is enabled on provider connections (`ForceAttemptHTTP2: true`)
- Connection pooling configured: 100 idle conns, 10 per host
- L1 exact cache uses `sync.RWMutex` — reader contention at 167 RPS is minimal
- BM25 and semantic caches use `sync.RWMutex` with inverted index for O(query_terms) lookups

**Bottleneck at 10K RPM:** None expected.

### 2.2 At 100K RPM (~1,667 RPS)

**Expected behavior: Stress points emerge. ⚠️**

| Component | Bottleneck | Severity |
|-----------|-----------|----------|
| **ExactCache.Get()** | RLock → check TTL → upgrade to write Lock → Touch LRU. Every cache hit takes a write lock for LRU touch. At 80%+ hit rate (1,333 RPS hitting write lock), this becomes a serialization point. | 🔴 **High** |
| **BM25Cache.Lookup()** | Holds RLock for entire score computation across all candidates. Long-running reads block writers (Store/evict). | 🟡 **Medium** |
| **SemanticCache** | External embedding API call (10s timeout) while NOT holding lock — this is good. But linear scan of entries for cosine similarity is O(n). | 🟡 **Medium** |
| **Dashboard.Push()** | SSE event bus broadcasts to all connected dashboard clients per request. Serialization through single channel. | 🟢 **Low** (few clients) |
| **Metrics recording** | Multiple `atomic.Add` operations per request (10+ counters). Atomic ops have cross-core cache-line contention on multi-socket systems. | 🟢 **Low** |

### 2.3 Lock Contention Deep Dive

**ExactCache — The Hot Path Problem:**
```go
// Current: Every cache HIT takes TWO locks
func (c *ExactCache) Get(key string) ([]byte, bool) {
    c.mu.RLock()           // Lock 1: read
    entry, ok := c.entries[key]
    c.mu.RUnlock()
    // ... TTL check ...
    c.mu.Lock()            // Lock 2: write (for LRU touch)
    c.lru.Touch(key)
    c.mu.Unlock()
    // ... copy response ...
}
```

At 1,667 RPS with 80% hit rate, that's 1,333 write-lock acquisitions/second just for LRU bookkeeping. The write lock blocks ALL concurrent readers.

**Fix:** Use sharded cache (partition by hash prefix) to reduce contention, or batch LRU updates.

### 2.4 Multi-Instance Sharding Strategy

Nexus is currently single-instance. For horizontal scaling:

| Layer | Sharding Strategy |
|-------|-------------------|
| **L1 Cache** | Consistent hashing on `SHA-256(prompt+model)` — each instance owns a key range. Use Redis as shared L1 for cross-instance hits. |
| **L2 BM25** | Not shardable (requires global document frequency). Replicate via gossip or centralize in shared store. |
| **L3 Semantic** | Offload to vector DB (Qdrant/Milvus). Storage backend abstraction already exists (`internal/storage`). |
| **Workflow State** | Sticky sessions via `X-Workflow-ID` hash, or shared Redis for `WorkflowState`. |
| **Circuit Breakers** | Per-instance is fine — each instance maintains independent health view. |
| **Confidence Map** | Periodic merge via shared file or Redis pub/sub. Currently file-based (`confidence_map.json`). |

---

## 3. Reliability

### 3.1 Failure Domains

| Domain | Impact | Mitigation |
|--------|--------|------------|
| **Single provider down** | Requests to that provider fail | ✅ Circuit breaker + fallback to next provider. Well implemented. |
| **All providers down** | All requests fail with 503 | ✅ Cache continues serving hits. ⚠️ No queuing/retry for cache misses. |
| **Embedding API down** | Semantic cache stops storing/looking up | ✅ Graceful fallback — `getEmbedding` error returns nil, falls through to provider. |
| **OOM on cache growth** | Process crash | ⚠️ `maxEntries` prevents unbounded growth, but BM25 `docs` slice and inverted index can fragment memory. No memory-pressure eviction. |
| **Config file corruption** | Can't start / hot-reload fails | ✅ Config watcher validates before applying. Startup fails fast with clear error. |
| **Disk full (billing/eval data)** | File writes fail silently | ⚠️ `confidenceMap.Save()` and billing stores don't propagate disk errors to health endpoint. |

### 3.2 Cascading Failure Risks

**Risk 1: Cascade routing amplifies provider stress**
- When a provider is slow (not down), cascade routing DOUBLES the load: cheap model attempt + escalation to the same slow provider pool.
- Mitigation: `maxLatency` on cascade attempts (configurable, default 2s). But cascade doesn't check circuit breaker state before the cheap attempt.

**Risk 2: Shadow evaluation goroutine leak**
- Shadow eval runs in a goroutine with a semaphore (`shadowSem`, cap 50). If the provider is slow/hanging, 50 goroutines fill up and shadow eval effectively stops. Good.
- But: goroutines hold `ctx.httpReq.Context()` which may be cancelled by the client disconnect, causing abandoned goroutines to linger until context timeout (120s per provider client).

**Risk 3: BM25 cache rebuild under load**
- `evictOldest()` calls `rebuildInvertedIndex()` which iterates ALL documents while holding the write lock. At 10K documents, this blocks all reads for milliseconds. Under sustained high write throughput, this creates periodic latency spikes.

### 3.3 Observability Gaps

| Gap | Impact | Recommendation |
|-----|--------|----------------|
| **No per-layer cache latency metrics** | Can't identify which cache layer is slow | Add histograms: `nexus_cache_l1_duration_seconds`, `nexus_cache_l2_bm25_duration_seconds`, `nexus_cache_l3_semantic_duration_seconds` |
| **No request queue depth metric** | Can't see how close to `maxConcurrent` limit | Add gauge: `nexus_request_queue_depth` from `len(requestSem)` |
| **No embedding API latency** | Can't diagnose semantic cache slowness | Add histogram: `nexus_embedding_api_duration_seconds` |
| **No goroutine count metric** | Can't detect goroutine leaks | Add gauge: `nexus_goroutines` via `runtime.NumGoroutine()` |
| **Health endpoint doesn't report cache health** | `/health` only checks provider reachability | Add cache stats (hit rate, size, memory estimate) to `/health/ready` |

---

## 4. Top 5 Architectural Improvements

### Priority 1: Add Request Queue Depth & Goroutine Count Metrics (Effort: Small — 1 hour)

**Rationale:** The `requestSem` channel provides natural backpressure, but there's no visibility into how close the system is to saturation. Operators can't set alerts for "approaching concurrency limit" without this metric.

**Implementation:** Add two gauges to `telemetry.Metrics`:
- `nexus_request_queue_depth` — `cap(requestSem) - len(requestSem)` (available slots remaining, polled periodically)
- `nexus_goroutines_total` — `runtime.NumGoroutine()`

**Impact:** Enables capacity planning and proactive alerting before requests start getting 503s.

### Priority 2: Add Per-Layer Cache Latency Metrics (Effort: Small — 1 hour)

**Rationale:** The cache is the most performance-critical path (serves 80%+ of requests in mature deployments). Without per-layer latency histograms, operators can't distinguish between "L1 is fast but L3 semantic is slow" vs. "all layers are fast but provider is slow."

**Implementation:** Wrap each cache layer lookup in `time.Since()` and record to labeled histogram: `nexus_cache_lookup_duration_seconds{layer="l1_exact|l2_bm25|l3_semantic"}`.

**Impact:** Enables targeted optimization of the slowest cache layer.

### Priority 3: Eliminate Dual Health Tracking (Effort: Medium — 4 hours)

**Rationale:** `HealthChecker` and `CircuitBreaker (Pool)` both track provider health with different thresholds (HealthChecker: 3 failures; CircuitBreaker: 5 failures with exponential backoff). The `findFallbackProvider` method checks `cbPool.IsAvailable()` but not `health.IsHealthy()`, making the HealthChecker's circuit state dead code in the hot path.

**Implementation:** Remove `HealthChecker`'s circuit state. Use it purely for periodic background health probes. Let `CircuitBreaker` be the single source of truth for "is this provider available?"

**Impact:** Eliminates confusing dual state, reduces lock contention, simplifies reasoning about provider availability.

### Priority 4: Extract Telemetry Recording from Phase Methods (Effort: Medium — 6 hours)

**Rationale:** Each phase method (handleStreaming, handleNonStreaming, checkCache) contains 30-50% telemetry/metrics/dashboard/event code interleaved with business logic. This makes phases hard to unit test and obscures the actual request flow.

**Implementation:** Create a `requestRecorder` that accumulates events during request processing, then flushes all telemetry in a single post-processing step. Use `defer recorder.Flush()` at the start of `handleChat`.

**Impact:** Cleaner separation of concerns, easier testing, less code duplication between streaming and non-streaming paths.

### Priority 5: Shard ExactCache for Reduced Lock Contention (Effort: Large — 8 hours)

**Rationale:** At high throughput (>1K RPS), every cache hit takes a write lock for LRU updates, serializing all concurrent readers. Sharding by key prefix distributes lock contention across N independent maps.

**Implementation:** Replace single `ExactCache` with `[N]ExactCacheShard`, each with its own `sync.RWMutex`, entries map, and LRU list. Route by `hash(key) % N` where N=16 or 32.

**Impact:** Near-linear throughput scaling for cache operations. Estimated 8-16x reduction in lock contention at >1K RPS.

---

## 5. Implemented Improvements

### Improvement 1: Request Queue Depth & Goroutine Count Metrics

**Files modified:** `internal/telemetry/metrics.go`

Added two new gauge metrics:
- `nexus_active_requests` — tracks currently in-flight requests (incremented when semaphore is acquired, decremented on release)
- `nexus_goroutines_total` — reports `runtime.NumGoroutine()` on each metrics scrape

These metrics enable operators to monitor concurrency saturation and detect goroutine leaks, which are the two most critical operational signals missing from the current deployment.

### Improvement 2: Per-Layer Cache Latency Tracking in Cache Store

**Files modified:** `internal/cache/store.go`

Added timing instrumentation to the `Lookup` method to measure each cache layer's lookup duration independently. The Store now tracks:
- `L1LookupNs` — nanoseconds spent in exact cache lookup
- `L2aLookupNs` — nanoseconds spent in BM25 cache lookup
- `L2bLookupNs` — nanoseconds spent in semantic cache lookup
- `LastLookupStats()` — returns the timing breakdown for the most recent lookup

This enables the gateway to log per-layer latency and record it in Prometheus histograms, allowing operators to identify which cache layer is the bottleneck.
