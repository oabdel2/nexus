# Nexus — Verified Metrics (Machine-Generated)

All metrics below were measured programmatically. No estimates.

**Generated:** Run via `go test`, `go build`, and PowerShell on Windows (Go 1.26.1, amd64)
**CPU:** 13th Gen Intel Core i7-13700KF (24 threads)

---

## Test Suite
- Test functions: **923**
- Passing: **923**
- Failing: **0**
- Packages tested: **20**
- Race detector: **PASS** (all 20 packages clean)

## Coverage
| Package | Coverage |
|---|---|
| internal/gateway | 52.8% |
| internal/router | 94.8% |
| internal/cache | 71.2% |
| internal/eval | 90.7% |
| internal/provider | 69.0% |
| internal/billing | 75.8% |
| internal/security | 70.3% |
| internal/compress | 94.9% |
| internal/experiment | 78.5% |
| internal/mcp | 98.1% |
| internal/auth | 97.7% |
| internal/dashboard | 89.1% |
| internal/events | 76.3% |
| internal/plugin | 86.5% |
| internal/workflow | 89.5% |
| internal/telemetry | 74.1% |
| internal/notification | 78.2% |
| internal/storage | 32.7% |

## Benchmarks

All values are **median of 3 runs** (`-count=3 -benchmem`).

### Core Routing (benchmarks/)
| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| ClassifyComplexity/simple_greeting | 246 | 8 | 1 |
| ClassifyComplexity/short_question | 472 | 16 | 1 |
| ClassifyComplexity/medium_code | 668 | 48 | 1 |
| ClassifyComplexity/complex_debug | 1,068 | 64 | 1 |
| ClassifyComplexity/complex_security | 1,276 | 80 | 1 |
| ExactCacheSet | 671 | 292 | 2 |
| ExactCacheGet | 305 | 245 | 5 |
| HashKey | 169 | 208 | 4 |
| RouterRoute/economy_simple | 2,272 | 432 | 11 |
| RouterRoute/mid_code | 2,759 | 488 | 13 |
| RouterRoute/premium_debug | 3,761 | 512 | 14 |
| CacheOperations/SetThenGet | 1,381 | 334 | 3 |
| CacheOperations/HashOnly | 270 | 224 | 4 |
| RouterDecision/EconomyPath | 2,292 | 432 | 11 |
| RouterDecision/PremiumPath | 3,515 | 512 | 14 |

### Compression (internal/compress/)
| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| Whitespace_1KB | 26,694 | 6,215 | 9 |
| Whitespace_10KB | 343,423 | 61,610 | 9 |
| Whitespace_100KB | 4,801,309 | 640,674 | 11 |
| CodeBlockCompress_1KB | 47,795 | 9,666 | 107 |
| CodeBlockCompress_10KB | 426,474 | 111,527 | 1,039 |
| CompressMessages_Combined | 279,105 | 169,061 | 493 |
| HistoryTruncate | 43,169 | 24,571 | 61 |

### Eval / Confidence (internal/eval/)
| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| HedgingScore | 10,257 | 2,048 | 1 |
| StructureScore | 249 | 192 | 1 |
| ConsistencyScore | 17,040 | 2,048 | 1 |
| CombinedScore | 28,832 | 4,368 | 5 |
| CombinedScore_Hedging | 28,633 | 4,368 | 5 |
| ConfidenceMap_ConcurrentReadWrite | 71 | 0 | 0 |
| ConfidenceMap_Write | 46 | 0 | 0 |
| ConfidenceMap_Read | 18 | 0 | 0 |
| ClassifyTaskType | 1,771 | 96 | 1 |
| ConfidenceMap_HighContention | 1,593 | 96 | 4 |

### Experiment / A/B (internal/experiment/)
| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| Assignment | 172 | 136 | 4 |
| RecordMetric | 121,447 | 23 | 1 |
| ZTest | 24 | 0 | 0 |
| GetResults | 258 | 472 | 5 |

### Router Classifier (internal/router/)
| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| Classify_TFIDF | 9,941 | 4,174 | 5 |
| Classify_Smart | 14,993 | 4,297 | 6 |

## Binary
- Size: **8.88 MB** (stripped, `-ldflags="-s -w"`)
- Dependencies: **1** (`gopkg.in/yaml.v3`)
- Go version: 1.23+

## Security
- Middleware count: **17** (in gateway server chain)
- Middleware chain: BillingAuth → Telemetry/Trace → PanicRecovery → BodySizeLimit → RequestTimeout → SecurityHeaders → RequestID → RequestLogger → CORS → IPAllowlist → AdminRequired → RateLimiter → OIDC → InputValidator → PromptGuard → AuditLog → ErrorSanitizer
- Prompt guard patterns: **16** built-in regex patterns (injection, jailbreak, system prompt extraction, XSS, template injection)

## Cache
- Distinct lookup layers: **3** (L1 exact → L2a BM25 → L2b semantic)
- Enhancement features: reranker verification, synonym learning (with persistence), feedback loop, shadow mode, context fingerprint

## Code
- Source lines (non-test .go): **22,110** (85 files)
- Test lines (*_test.go): **18,506** (38 files)
- Source/test ratio: **1.19 : 1**
- Total benchmark functions: **31**
