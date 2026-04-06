# Nexus Gateway — Expert Audit

**Auditor focus**: AI inference optimization, token economics, production Go systems  
**Date**: 2025  
**Codebase version**: v0.1.0 (`go 1.26.1`, single dependency: `gopkg.in/yaml.v3`)

---

## 1. Executive Summary

Nexus is a genuinely ambitious inference gateway with a coherent architectural vision: route prompts to cost-appropriate model tiers, compress tokens before they leave, cache responses at multiple layers, and learn from confidence feedback. The core routing and caching pipeline is real, functional code — not scaffolding. However, it has a **critical mutex deadlock** in the TF-IDF online learning path, **O(n) linear scans** on every cache lookup and eviction that will degrade badly at scale, and the "zero external dependencies" purity has led to reinventing several wheels (metrics, rate limiting, tracing) at lower quality than well-tested libraries would provide. The compression saves real tokens but the savings are modest (whitespace + comments), and the semantic cache makes an embedding API call on every miss, which can *increase* total latency and cost rather than reduce it.

---

## 2. What's Done Well

### 2.1 Coherent end-to-end architecture
The hot path in `server.go:524-1082` is a well-sequenced pipeline: parse → compress → cache lookup → route → circuit-breaker check → send → score confidence → cache store → respond. Each step has tracing spans and the data flows cleanly. This is genuinely good architectural design for a gateway.

### 2.2 Anthropic → OpenAI SSE translation (anthropic.go:216-284)
The Anthropic streaming adapter correctly translates Anthropic's SSE event types (`message_start`, `content_block_delta`, `message_delta`, `message_stop`) into OpenAI-compatible `data: {...}\n\n` chunks. The `buildOpenAIStreamChunk` helper (L288-319) is clean. This is legitimately useful for anyone wanting a unified streaming API.

### 2.3 TF-IDF classifier with built-in corpus (tfidf.go)
The TF-IDF implementation is textbook-correct: proper IDF computation (`log(N/df)`), term frequency normalization, cosine similarity, and k-NN with k=5 weighted voting. The built-in 64-example training corpus (L275-353) is well-curated with balanced tier representation. The partial sort for top-k (L152-165) avoids a full sort — good performance instinct.

### 2.4 SmartClassifier hybrid approach (smart_classifier.go)
Blending TF-IDF (50%), keywords (25%), structural analysis (15%), and length (10%) is a sound approach. The structural analysis (L152-230) goes beyond toy quality — it detects code blocks, multiple questions, conditional language, negation constraints, enumerated lists, and technical jargon density. The `commonEnglishWords` dictionary (L233-249) for jargon detection is a clever hack.

### 2.5 Confidence scoring with multiple signals (eval/scorer.go)
The `CombinedScore` function (L209-254) uses five independent signals (hedging, completeness, structure, consistency, finish reason) with reasonable weights. The contradiction detector (L146-190) checking 18 positive/negative phrase pairs is not just a stub — it would catch real model self-contradictions. The hedging phrase list (L42-59) with 16 patterns is practical.

### 2.6 3-layer cache with BM25 (cache/store.go, bm25.go)
The L1 (exact hash) → L2a (BM25 keyword) → L2b (semantic embedding) cache hierarchy is architecturally sound. The BM25 implementation (bm25.go) is correct with proper IDF formula, k1=1.5, b=0.75 parameters, and a nice `simpleStem` function (L78-106) that handles plurals and common suffixes. The document frequency bookkeeping during eviction (L246-252) is correctly maintained.

### 2.7 Test quality — actually testing behavior
The tests are notably good for a project at this stage. `adaptive_test.go` has 18 focused tests covering: insufficient samples (no override), downgrade, upgrade, middle range (no override), counter tracking, concurrent access with `sync.WaitGroup` (L135-154), disabled passthrough, edge cases at exact thresholds, and multi-task-type scenarios. These test *behavior*, not just that code runs.

### 2.8 Zero external Go dependencies
`go.mod` has exactly one dependency: `gopkg.in/yaml.v3`. The metrics system, circuit breakers, rate limiter, TLS config, tracing, and event bus are all hand-rolled. This eliminates dependency supply-chain risk entirely — a genuine advantage for a security-sensitive gateway.

### 2.9 Cascade routing pattern (cascade.go, server.go:719-747)
The try-cheap-first-then-escalate pattern is genuinely innovative for cost optimization. The implementation respects sample rates, only cascades when the complexity score is below threshold, and tracks cost savings. The `tryCheapFirst` function (server.go:1332-1389) correctly uses a context timeout to cap cheap model latency.

### 2.10 Adaptive routing with learning (adaptive.go)
Using a confidence map to learn over time which task types perform well on cheap models is a strong idea. The min-samples guard (default 50) prevents premature optimization, and the three-zone approach (high confidence → downgrade, low confidence → upgrade, middle → no change) is sound.

---

## 3. What's Done Terribly

### 3.1 🔴 DEADLOCK: TF-IDF `AddExample` (tfidf.go:103-118) — CRITICAL

```go
func (tc *TFIDFClassifier) AddExample(text, tier string) {
    tc.mu.Lock()
    defer tc.mu.Unlock()
    // ...
    tc.mu.Unlock()      // manually unlock
    tc.Train(examples)  // Train() calls tc.mu.Lock() internally!
    tc.mu.Lock()        // re-acquire for deferred unlock
}
```

This is a **guaranteed deadlock** when called concurrently. `Train()` at line 42 does `tc.mu.Lock()`, but between the manual `Unlock()` on L115 and the `Lock()` on L117, another goroutine can call `AddExample`, acquire the lock, manually unlock, then call `Train` — creating a race where the deferred unlock on L105 runs on a lock that's been transferred to another goroutine. Even in the single-goroutine case, the `Lock()`/`Unlock()` dance with `defer` is fragile and error-prone.

**Impact**: If the TF-IDF classifier's `AddExample` is ever called from the hot path or from an admin endpoint, the server hangs.

### 3.2 🔴 O(n) linear scan on every semantic cache lookup (semantic.go:120-131) — PERFORMANCE

```go
for i := range c.entries {
    entry := &c.entries[i]
    if now.Sub(entry.createdAt) > c.ttl { continue }
    score := dotProduct(emb, entry.embedding)
    if score > bestScore { bestScore = score; bestIdx = i }
}
```

Every `Lookup()` iterates ALL entries, computing a dot product for each. With config `max_entries: 50000` and a 1024-dim embedding, that's 50,000 × 1024 floating-point multiplications per cache lookup. At ~50M FLOPs per lookup, this adds **multiple milliseconds** of CPU time to every request that reaches L2b.

The BM25 cache (bm25.go:179-210) has the same O(n) problem.

**Impact**: Cache lookups that are supposed to save latency may actually *add* latency as the cache fills up.

### 3.3 🔴 Embedding API call on every cache miss (semantic.go:86-95) — TOKEN WASTE

```go
func (c *SemanticCache) Lookup(prompt, model string) ([]byte, bool) {
    expanded := expandSynonyms(prompt)
    emb, err := c.getEmbedding(expanded)  // <-- HTTP call to Ollama/OpenAI
```

Every L2b cache miss (and every cache store) triggers an HTTP embedding API call. If semantic cache is enabled but the cache is cold or miss-heavy, every single user request now incurs an *additional* API call just for the cache lookup attempt.

**Impact**: A cold semantic cache with 100 requests doesn't save anything — it adds 200 extra embedding API calls (100 lookups + 100 stores). With OpenAI embeddings, that's real cost.

### 3.4 🟡 Cascade sends the FULL request to the cheap model, then discards it (server.go:1332-1389)

When cascade is enabled, `tryCheapFirst` sends the complete request to the cheap model, waits for the full response, scores it, and if confidence is below threshold... throws it away and sends the same request to the expensive model. 

```go
resp, err := p.Send(cheapCtx, cheapReq)
// ... score confidence ...
escalated := confidence < s.cascade.Threshold()
```

But the cascade result only sets `UsedCheapModel` and `CostSaved` — it doesn't actually return the cheap response to the caller! Look at server.go:720-729: when `!cascadeResult.Escalated`, it changes `selection` to the cheap model but **doesn't use the already-obtained response**. The request will be sent *again* in the main path (server.go:892-900).

**Impact**: Cascade "acceptance" still sends the request twice — once in the cascade trial and once in the main path. Cascade "escalation" sends it three times (cheap + retry + expensive). This doubles/triples latency and token cost.

### 3.5 🟡 `go.mod` declares `go 1.26.1` which doesn't exist

```go
go 1.26.1
```

As of the time of this audit, the latest Go version is 1.24.x. Go 1.26 doesn't exist. This will cause build failures on any standard Go toolchain.

### 3.6 🟡 Global mutable state: `defaultRegistry` (cache/synonym.go implied)

`SetSynonymRegistry(registry)` in `store.go:55` and `defaultRegistry` in `semantic.go:135` suggest a package-level global variable shared across cache instances. This breaks isolation and makes testing unreliable.

### 3.7 🟡 Lock churning in cache lookups (semantic.go:107-177, bm25.go:146-227)

The semantic cache Lookup acquires and releases the lock **4-5 times** in a single call:

```
RLock → RUnlock → Lock (miss++) → Unlock      (empty case)
RLock → RUnlock → Lock (miss++) → Unlock      (below threshold)
RLock → RUnlock → Lock (hits++) → Unlock → RLock → RUnlock  (hit case)
```

This is not just ugly — it creates TOCTOU race windows. Between the `RUnlock` at L131 and the `RLock` at L172 (getting the response), another goroutine can evict the entry at `bestIdx`, causing an out-of-bounds panic or stale data return.

### 3.8 🟡 No authentication on admin endpoints

The synonym API (`/api/synonyms/promote`, `/api/synonyms/add`), circuit breaker status, eval stats, adaptive stats, and experiment endpoints are all unauthenticated. The billing auth middleware (server.go:1506) explicitly skips non-`nxs_` keys and passes through. Without billing enabled, ALL admin endpoints are wide open.

### 3.9 🟡 Stream tee writer splits on newlines incorrectly (server.go:1796)

```go
for _, line := range strings.Split(string(p), "\n") {
    line = strings.TrimSpace(line)
    if line != "" { tw.buf.WriteChunk(line) }
}
```

SSE data can arrive in partial chunks across `Write()` calls. A `data: {"content":"hel` / `lo"}\n\n` split across two writes will produce mangled cache entries. The `strings.Split` approach doesn't handle partial lines.

### 3.10 🟡 `extractPromptText` joins ALL messages including system (server.go:1200-1206)

```go
func extractPromptText(messages []provider.Message) string {
    var parts []string
    for _, m := range messages { parts = append(parts, m.Content) }
    return strings.Join(parts, "\n")
}
```

This means the cache key, routing classification, and prompt guard all operate on system prompt + conversation history + user message concatenated together. A long system prompt will dominate the cache key and routing score, making the cache useless for different user questions with the same system prompt.

---

## 4. Token Efficiency Audit

### 4.1 Compression effectiveness: **Marginal savings (~5-15%)**

The compressor does three things:
1. **Whitespace collapse** (compress.go:74-82): Removes trailing spaces, collapses 3+ newlines to 2, collapses multiple horizontal whitespace. Real savings: ~2-5% for typical chat messages. Significant only for copy-pasted code or log dumps.
2. **Code block stripping** (compress.go:96-144): Removes comments and blank lines from fenced code blocks. Real savings: ~10-20% *of code blocks*, which are maybe 30% of message content in coding tasks. Net: ~3-6%.
3. **History truncation** (compress.go:207-245): Replaces middle messages with a keyword summary. This is the biggest saver — it can remove 80%+ of old conversation. But it uses `len(text)/4` for token estimation (L54-63), which is crude and overestimates by ~20% for code (which has more chars per token).

**Verdict**: History truncation is the only strategy that materially saves tokens. Whitespace compression mostly saves bytes, not tokens (tokenizers already normalize whitespace). Code comment stripping saves tokens but at the risk of removing context the model needs.

### 4.2 Token waste patterns

1. **Cascade double-send** (Section 3.4): Accepted cascades send the full prompt twice. Escalated cascades send it 2-3 times. This is the single largest source of token waste.
2. **Embedding calls for cache** (Section 3.3): Each embedding call to `bge-m3` costs ~1024-token input. Two calls per request (lookup + store) = ~2048 embedding tokens per cache miss. You need >3 cache hits per entry to break even on a typical 500-token request.
3. **Full message concatenation for classification** (Section 3.10): The router classifies complexity on the concatenation of ALL messages. A 10-turn conversation with a 2000-char system prompt creates a 20,000+ char classification input, but the classifier only looks at keyword matches — wasting CPU without improving routing quality.
4. **System prompt included in cache key**: With exact (L1) cache, two users asking the same question with different system prompts get separate cache entries. This dramatically reduces hit rates.

### 4.3 Cache effectiveness: **Good design, questionable net ROI**

The 3-layer design is sound:
- L1 (exact hash): Zero overhead, instant hit. Effective for repeated identical requests.
- L2a (BM25): Low overhead (~1μs tokenization), handles paraphrases well. Good ROI.
- L2b (semantic): High overhead (~10-50ms embedding API call + O(n) linear scan). The overhead exceeds savings for small-to-medium caches.

**Break-even analysis for semantic cache**: A cached hit saves one LLM API call (~$0.005 for mid-tier, ~500 tokens). A miss costs one embedding API call (~$0.0001 for OpenAI, $0 for local Ollama) plus ~5-50ms latency. If hit rate is 20%, you need 5 misses per hit, costing $0.0005 in embeddings + 25-250ms added latency across the 5 misses, to save $0.005. Financially positive, but **latency-negative**.

### 4.4 TF-IDF vs keyword classifier

The TF-IDF classifier (with 64 training examples) is **marginally better** than the keyword classifier for ambiguous prompts like "analyze this list of names" (economy) vs "analyze this security vulnerability" (premium). The keyword classifier treats both as high-complexity due to "analyze". The TF-IDF classifier uses surrounding context.

However, the TF-IDF classifier computes cosine similarity against all 64 training documents per request. At ~200 vocabulary terms × 64 docs = ~12,800 FLOPs — negligible. The SmartClassifier's blended approach (50% TF-IDF + 25% keywords + 15% structural + 10% length) is genuinely better than keywords alone. **Recommendation: enable SmartClassifier by default.**

### 4.5 Latency without value

1. **Compression on short prompts**: Compressing a 3-message chat with short messages wastes CPU for <1% savings.
2. **Semantic cache for unique prompts**: Creative/novel prompts will never hit semantic cache but still pay the embedding cost.
3. **Confidence scoring on cache hits**: Currently skipped (good), but the eval system only scores non-streaming responses — streaming responses (the common case for chat) never contribute to the confidence map.

---

## 5. Architecture Concerns

### 5.1 Separation of concerns

`server.go` at 1838 lines is a god-file that handles: request routing, all endpoint handlers, billing auth middleware, stream tee writing, fallback provider selection, cascade logic, experiment recording, event emission, and plugin hooks. The `handleChat` function alone is 558 lines (L524-1082). This should be decomposed into:
- `handler_chat.go` — the main hot path
- `handler_admin.go` — admin/debug endpoints  
- `handler_billing.go` — billing endpoints
- `middleware_billing.go` — billing auth

### 5.2 Concurrency correctness

- **TF-IDF AddExample**: Deadlock (Section 3.1)
- **Semantic cache lock churning**: TOCTOU race (Section 3.7)
- **BM25 cache DF counter**: Correctly maintained on eviction, but `evictOldest` does O(n) scan under write lock, blocking all readers.
- **Adaptive router counters**: Uses `sync.RWMutex` correctly for overrides/downgrades/upgrades.
- **Metrics**: Uses `sync/atomic` and `sync.Map` — correct and lock-free.

### 5.3 Error handling patterns

Generally good — errors are wrapped with `fmt.Errorf("context: %w", err)` throughout provider code. The gateway logs errors and returns appropriate HTTP status codes. One concern: error responses in `handleChat` (e.g., L919) use `errProviderError(err.Error())` which may leak internal error messages to clients. The `ErrorSanitizer` middleware (L451) is supposed to catch this, but it only intercepts 5xx — a 502 from `writeNexusError` with raw provider errors will pass through.

### 5.4 Memory management

- **Unbounded growth: BM25 DF map** — `c.df` grows with unique terms seen across all documents. Even after eviction, terms that appeared in evicted docs may not be cleaned up (they're decremented but might accumulate stale zero-count entries). Fixed by the `delete(c.df, term)` at L249-250 — actually this is correct.
- **Unbounded growth: confidence map** — `ConfidenceMap.data` grows without bound as new task types and tiers are observed. With only 5 task types × 4 tiers = 20 entries max, this is fine.
- **TF-IDF vocabulary**: Grows monotonically via `AddExample`. Each retrain rebuilds from scratch (good), but the vocabulary map grows with every unique token ever seen. With online learning from production prompts, this could grow large.
- **Event bus ring buffer**: Fixed at 100 recent events (events.go:60) — bounded.
- **Cache cleanup goroutines**: Both semantic and BM25 caches start a background goroutine per instance (semantic.go:58, bm25.go:57) that ticks every minute. These goroutines **never stop** — there's no context or shutdown signal. If the server creates/destroys cache instances, these goroutines leak.

### 5.5 Configuration sprawl

The config file is 250 lines with nested objects 4+ levels deep. There are duplicate enable flags (`cache.enabled`, `cache.l1_enabled`, `cache.l1.enabled`) that must be kept in sync (see server.go:88 using `||` to handle both). The `config.Config` struct likely mirrors this sprawl. Adding new features requires touching 3+ places: config struct, YAML, and server initialization.

---

## 6. Top 10 Fixes (Prioritized)

### Fix 1: TF-IDF AddExample deadlock
- **What**: `AddExample()` manually unlocks/relocks around a call to `Train()` which also locks. This deadlocks under concurrent access and is semantically wrong with `defer`.
- **Where**: `internal/router/tfidf.go:103-118`
- **Why**: Server hang if online learning is triggered from any concurrent path.
- **How**: Create an internal `trainLocked()` method that assumes the lock is held. Have `AddExample` call `trainLocked()` directly, and have `Train()` acquire the lock then call `trainLocked()`.

```go
func (tc *TFIDFClassifier) AddExample(text, tier string) {
    tc.mu.Lock()
    defer tc.mu.Unlock()
    examples := /* collect existing + new */
    tc.trainLocked(examples)
}

func (tc *TFIDFClassifier) Train(docs []TrainingExample) {
    tc.mu.Lock()
    defer tc.mu.Unlock()
    tc.trainLocked(docs)
}

func (tc *TFIDFClassifier) trainLocked(docs []TrainingExample) {
    // current Train() body without lock
}
```

### Fix 2: Fix cascade double-send
- **What**: When cascade accepts the cheap model, the cheap response is discarded and the request is sent again through the normal path.
- **Where**: `internal/gateway/server.go:719-747` (cascade check) and `server.go:892-900` (main send)
- **Why**: Doubles token usage and latency on every cascade acceptance.
- **How**: Have `tryCheapFirst` return the actual `*provider.ChatResponse`. When cascade accepts, use that response directly instead of re-sending. Skip the main provider send path entirely.

### Fix 3: Index the semantic cache
- **What**: O(n) linear scan on every semantic cache lookup with dot product against all entries.
- **Where**: `internal/cache/semantic.go:120-131`
- **Why**: With 50K entries and 1024-dim vectors, each lookup takes milliseconds of CPU, negating cache benefits.
- **How**: Use an approximate nearest neighbor index. Options: (a) integrate a Go ANN library like `hnswlib-go`, (b) use the Qdrant vector store already mentioned in config but not wired up, or (c) bucket entries by coarse quantization and only scan within the top bucket. Even a simple 256-bucket LSH would reduce scan from 50K to ~200 entries.

### Fix 4: Fix go.mod Go version
- **What**: `go 1.26.1` doesn't exist as a Go release.
- **Where**: `go.mod:3`
- **Why**: Build failure on any standard Go toolchain.
- **How**: Change to the actual Go version used during development (likely `go 1.22` or `go 1.23`).

### Fix 5: Fix cache TOCTOU race in semantic + BM25 lookups
- **What**: Between releasing RLock (after finding bestIdx) and re-acquiring RLock (to read the response), another goroutine can evict or modify the entry.
- **Where**: `internal/cache/semantic.go:131-176`, `internal/cache/bm25.go:211-221`
- **Why**: Potential panic (index out of bounds), stale data return, or data corruption.
- **How**: Hold the RLock for the entire lookup and response copy. Copy the response bytes while holding the lock:

```go
c.mu.RLock()
defer c.mu.RUnlock()
// ... find bestIdx ...
if bestIdx >= 0 && bestScore >= threshold {
    resp := make([]byte, len(c.entries[bestIdx].response))
    copy(resp, c.entries[bestIdx].response)
    c.hits++ // needs to be atomic or use a separate counter
    return resp, true
}
```

### Fix 6: Extract user message for classification and caching
- **What**: `extractPromptText` concatenates ALL messages (system + history + user) for routing, caching, and prompt guard.
- **Where**: `internal/gateway/server.go:1200-1206`
- **Why**: Long system prompts dominate cache keys (destroying hit rates) and routing scores (making all requests look the same complexity).
- **How**: Extract only the last user message for routing classification and cache key. Use the full concatenation only for prompt guard checks. For cache, hash system prompt separately and use `(systemHash, lastUserMessage, model)` as the composite key.

### Fix 7: Add shutdown signals to cache cleanup goroutines
- **What**: `go c.cleanup()` goroutines in semantic.go:58 and bm25.go:57 run forever with no way to stop them.
- **Where**: `internal/cache/semantic.go:58`, `internal/cache/bm25.go:57`
- **Why**: Goroutine leak on cache recreation or server shutdown. In tests, each test that creates a cache leaks two goroutines.
- **How**: Accept a `context.Context` in the constructor. Use `ctx.Done()` channel alongside the ticker in cleanup:

```go
func (c *SemanticCache) cleanup(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            c.cleanupExpired()
        }
    }
}
```

### Fix 8: Make embedding calls conditional / lazy for semantic cache
- **What**: Every cache lookup calls `getEmbedding()` (an HTTP roundtrip) even if the cache is empty.
- **Where**: `internal/cache/semantic.go:86-95`
- **Why**: Empty or cold cache adds 10-50ms latency to every request for zero benefit.
- **How**: Check `len(c.entries) == 0` *before* computing the embedding. Also consider gating semantic cache behind L1+BM25 misses — if BM25 found nothing, semantic similarity is unlikely to help either.

### Fix 9: Authenticate admin endpoints
- **What**: `/api/synonyms/*`, `/api/experiments/*`, `/api/adaptive/stats`, `/api/eval/stats`, `/api/circuit-breakers`, and `/api/inspect` are all unauthenticated.
- **Where**: `internal/gateway/server.go:289-340`
- **Why**: An attacker can manipulate routing experiments, promote arbitrary synonyms into the cache expansion logic, and exfiltrate operational metrics.
- **How**: At minimum, add the admin endpoints to a path prefix (`/api/admin/`) that the IP allowlist already protects (config L193: `paths: ["/api/admin/"]`). Better: require RBAC `admin` permission for all `/api/` endpoints except `/api/usage`.

### Fix 10: Break up server.go
- **What**: `server.go` is 1838 lines handling 20+ HTTP endpoints, all middleware wiring, streaming, caching, cascade logic, and billing.
- **Where**: `internal/gateway/server.go`
- **Why**: Extremely difficult to review, test, or modify without risking regressions. The `handleChat` function at 558 lines is untestable in isolation.
- **How**: Split into:
  - `server.go` — constructor, Start(), Shutdown(), middleware wiring (~200 lines)
  - `handler_chat.go` — handleChat, tryCheapFirst, helper functions (~400 lines)
  - `handler_admin.go` — synonym, eval, compression, adaptive, experiment, inspect handlers (~300 lines)
  - `handler_billing.go` — stripe webhook, subscription, key, device, usage handlers (~300 lines)
  - `middleware_billing.go` — billingAuthMiddleware (~100 lines)
  - `stream_tee.go` — streamTeeWriter (~50 lines)

---

## Appendix: Rating Summary

| Area | Rating | Notes |
|------|--------|-------|
| Architecture | B+ | Coherent pipeline, good abstractions, too much in server.go |
| Correctness | C | Deadlock in TF-IDF, TOCTOU in cache, cascade double-send |
| Token efficiency | C+ | Compression saves modestly; cache and cascade can waste more than they save |
| Security | C | Admin endpoints unprotected, error messages leak, prompt guard is solid |
| Test quality | A- | Genuinely good behavioral tests, benchmarks, concurrent tests |
| Performance | C | O(n) cache scans, lock churning, embedding calls on every miss |
| Code quality | B | Clean Go, good naming, but 1838-line god file |
| Production readiness | C+ | Needs Fixes 1-5 before any production traffic |

**Bottom line**: This is a ~70% complete gateway with genuine innovation in the routing/caching/confidence feedback loop. It's not scaffolding — the core logic is real and thoughtful. But the concurrency bugs (Fix 1, 5), the cascade double-send waste (Fix 2), and the O(n) cache scaling (Fix 3) must be fixed before production use. The `go.mod` version (Fix 4) must be fixed before it can even compile on a standard toolchain.
