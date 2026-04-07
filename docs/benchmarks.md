# Nexus Performance Benchmarks

**Platform:** Windows/amd64, 13th Gen Intel Core i7-13700KF, Go 1.26.1  
**Methodology:** `go test -bench=. -benchmem -count=3` (median of 3 runs reported)  
**Target:** <1ms total gateway overhead per request

---

## Router — Prompt Classification

| Operation | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| ClassifyComplexity (TF-IDF) | 9,600 | 4,174 | 5 |
| ClassifyComplexity (Smart/Hybrid) | 10,400 | 4,297 | 6 |

**Interpretation:** Both classifiers complete in **~10µs** — well under the 1ms budget. The Smart classifier adds only ~8% overhead over pure TF-IDF. At 10µs per classification, routing can handle **100K classifications/sec** on a single core. This is negligible compared to LLM inference latency (100ms–10s).

---

## Compression — Prompt Size Reduction

| Operation | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| Whitespace 1KB | 24,200 | 6,213 | 9 |
| Whitespace 10KB | 231,600 | 61,635 | 9 |
| Whitespace 100KB | 3,347,000 | 640,555 | 11 |
| CodeBlock 1KB | 33,700 | 9,670 | 107 |
| CodeBlock 10KB | 346,500 | 111,584 | 1,039 |
| CompressMessages (combined) | 269,200 | 168,980 | 493 |
| HistoryTruncate | 40,950 | 24,571 | 61 |

**Interpretation:** Combined message compression completes in **~270µs** — safely under 1ms. Whitespace and code compression scale linearly with input size. The combined pipeline (whitespace + code + history + boilerplate + JSON + dedup) processes a realistic multi-turn conversation in 270µs. For 100KB inputs (very large prompts), compression takes ~3.3ms — but such inputs are rare and the compression saves far more in inference cost than it costs in latency.

---

## Eval — Confidence Scoring

| Operation | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| HedgingScore | 9,528 | 2,048 | 1 |
| StructureScore | 175 | 192 | 1 |
| ConsistencyScore | 12,323 | 2,048 | 1 |
| CombinedScore | 22,475 | 4,368 | 5 |
| CombinedScore + Hedging | 24,942 | 4,368 | 5 |
| ClassifyTaskType | 1,695 | 96 | 1 |
| ConfidenceMap Read | 19 | 0 | 0 |
| ConfidenceMap Write | 42 | 0 | 0 |
| ConfidenceMap ConcurrentRW | 50 | 0 | 0 |
| ConfidenceMap HighContention | 1,431 | 96 | 4 |

**Interpretation:** Full confidence scoring completes in **~25µs** — negligible. The ConfidenceMap (used for adaptive routing) achieves **19ns reads** and **42ns writes** with zero allocations, supporting millions of operations per second. Even under high contention (8 goroutines), operations complete in ~1.4µs. Shadow evaluation (async) does not add to request latency.

---

## Experiment — A/B Testing Framework

| Operation | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| Assignment (variant selection) | 163 | 136 | 4 |
| RecordMetric (with lock contention) | 120,850 | 23 | 1 |
| ZTest (significance calculation) | 17 | 0 | 0 |
| GetResults | 176 | 472 | 5 |

**Interpretation:** Experiment assignment takes **163ns** — invisible to users. Z-test significance calculation at **17ns** is effectively free. RecordMetric is higher at ~121µs due to lock contention in the concurrent metric store, but this runs asynchronously and does not block request processing.

---

## Total Gateway Overhead Budget

| Component | Latency | % of 1ms Budget |
|---|---:|---:|
| Router classification | 10µs | 1.0% |
| Compression (combined) | 270µs | 27.0% |
| Eval scoring | 25µs | 2.5% |
| Experiment assignment | 0.2µs | 0.02% |
| Cache lookup (L1 exact) | <1µs | <0.1% |
| **Total overhead** | **sub-millisecond** | **~30%** |

**Verdict:** Total gateway overhead is **sub-millisecond** — well within the 1ms target. Exact measurements depend on configuration and hardware. The remaining budget provides ample margin for network I/O, serialization, and middleware processing. LLM inference (100ms–10s) dominates total request latency by 300–30,000x.

---

## Cache

The cache package currently has no standalone benchmarks. Cache performance is dominated by:
- **L1 (exact match):** O(1) hash lookup + LRU touch — sub-microsecond
- **L2 BM25:** Inverted index lookup + BM25 scoring — depends on corpus size
- **L2 Semantic:** Embedding API call (network-bound) + cosine similarity scan

Cache hot-path operations (L1 hit) add negligible latency. L2 lookups are only attempted on L1 miss and are bounded by the configured `max_entries`.
