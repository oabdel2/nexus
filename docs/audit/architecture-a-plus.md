# Architecture Audit: A+ Grade Target

**Project:** Nexus — Agentic-First Inference Optimization Gateway  
**Date:** 2025-07-19  
**Baseline Grade:** A-  
**Post-Fix Grade:** A

---

## Summary

Six architectural issues were identified and fixed. All changes pass `go build ./...`
and `go test ./...` with zero regressions.

---

## Issues Found & Fixed

### 1. Mutable Global State — `cache/filters.go`

**Problem:** `var defaultRegistry *SynonymRegistry` was a package-level mutable global,
set via `SetSynonymRegistry()` during `Store` construction. This created hidden coupling:
`SemanticCache.Lookup()` and `SemanticCache.Store()` silently depended on a global being
set by an unrelated constructor. Tests manipulated the global directly. This is
thread-unsafe by design and violates dependency injection principles.

**Fix:** Removed the global variable and `SetSynonymRegistry`/`GetSynonymRegistry`
exports. Added `registry *SynonymRegistry` field to `SemanticCache`. Changed
`expandSynonyms(text)` → `expandSynonyms(text, registry)` and
`hasDifferentKeyNoun(a, b)` → `hasDifferentKeyNoun(a, b, registry)`. Registry is now
threaded from `Store` → `SemanticCache` → function calls. Tests pass `nil` for the
static-map fallback path.

**Files changed:**
- `internal/cache/filters.go` — removed global, added registry param to functions
- `internal/cache/semantic.go` — added `registry` field, updated all call sites
- `internal/cache/store.go` — removed `SetSynonymRegistry()` call, passes registry to constructor
- `internal/cache/semantic_test.go` — updated ~25 function calls to pass `nil`

---

### 2. Naming Stutter — `provider.ProviderPool`

**Problem:** `provider.ProviderPool` stutters: the type name repeats the package name.
Go convention says the package name is part of the qualified name, so
`provider.Pool` reads naturally while `provider.ProviderPool` is redundant.

**Fix:** Renamed `ProviderPool` → `Pool` and `NewProviderPool` → `NewPool`.

**Files changed:**
- `internal/provider/circuitbreaker.go` — renamed type and constructor
- `internal/provider/circuitbreaker_test.go` — updated test names and calls
- `internal/gateway/server.go` — updated field type and constructor call

---

### 3. Dead Code — Unused Error Constructors

**Problem:** Four error factory functions were defined but never called anywhere in
the codebase:
- `errBodyTooLarge()` — 0 call sites
- `errAuthFailed()` — 0 call sites
- `errQuotaExceeded()` — 0 call sites
- `errConfigError()` — 0 call sites

These inflate the error catalog without serving any purpose. Dead code is a
maintenance burden and confuses contributors.

**Fix:** Removed all four functions.

**Files changed:**
- `internal/gateway/errors.go` — removed 4 dead functions (~40 lines)

---

### 4. Reinvented Standard Library — Custom `itoa`

**Problem:** `errors.go` contained a hand-rolled `itoa(n int) string` function
(20 lines) that reimplements `strconv.Itoa`. The comment said "without importing
strconv" — but there's no valid reason to avoid a stdlib import. This is
unmaintainable cleverness.

**Fix:** Replaced `itoa()` with `strconv.Itoa()` and removed the custom function.

**Files changed:**
- `internal/gateway/errors.go` — added `strconv` import, removed custom `itoa`
- `internal/gateway/shadow_eval_test.go` — updated test to use `strconv.Itoa`

---

### 5. Duplicate API Key Expansion — `cmd/nexus/main.go`

**Problem:** In `runServe()`, provider API keys were expanded twice:
1. Lines 141-143: manual loop calling `os.ExpandEnv()` on each provider
2. Line 144: `cfg.ExpandSecrets()` which does the exact same loop internally

The operation is idempotent so no bug, but duplicate code signals a maintenance
hazard — future changes to one path might miss the other.

**Fix:** Removed the manual loop (lines 141-143). `ExpandSecrets()` handles
all secret fields including provider API keys.

**Files changed:**
- `cmd/nexus/main.go` — removed redundant 3-line loop

---

### 6. God File — `handler_chat.go` (707 lines)

**Problem:** `handler_chat.go` was 707 lines, exceeding the 500-line threshold.
It contained the main `handleChat` handler plus 7 helper functions and types
(`tryCheapFirst`, `extractPromptText`, `fullPromptText`, message converters,
`findFallbackProvider`, `streamTeeWriter`).

**Fix:** Extracted all helpers to `handler_helpers.go` (166 lines). The main
`handleChat` handler remains in `handler_chat.go` at 558 lines — the remaining
length is inherent to the handler's linear request-processing pipeline (parse →
compress → guard → cache → route → cascade → send → record → respond).

**Files changed:**
- `internal/gateway/handler_chat.go` — removed helper functions (707 → 558 lines)
- `internal/gateway/handler_helpers.go` — **new file** with extracted helpers

---

## Issues Analyzed — No Fix Needed

### A. Package Dependency Direction (gateway imports everything)

The `gateway` package imports 14 internal packages. This is expected for an
application composition root. The `gateway.Server` struct is the wiring point
that assembles all subsystems. Crucially:

- **No import cycles exist** — dependency flows one-way from gateway to leaf packages
- Leaf packages (`cache`, `router`, `provider`, `eval`) do NOT import `gateway`
- The `provider.Provider` interface is well-defined at the package boundary
- The `router` and `cache` packages depend on `config` types, not on `gateway`

**Verdict:** This is correct composition-root architecture, not a dependency problem.

### B. Config Struct Size (18 sub-configs)

The `Config` struct has 18 fields, each a sub-config type. This looks large but:

- Each sub-config is a separate, focused type (single responsibility)
- The pattern is standard Go YAML config
- Sub-configs are grouped logically (Server, Providers, Router, Cache, Security, etc.)
- The `setDefaults()` function is the only real size concern at ~170 lines

**Verdict:** Acceptable. The config matches the system's feature surface.

### C. Remaining Package-Level Variables

Several package-level `var` declarations exist but are all **immutable data**:

| Variable | Package | Type | Notes |
|----------|---------|------|-------|
| `tierFallbackOrder` | router | `[]string` | Immutable lookup table |
| `highComplexityKeywords` | router | `[]string` | Immutable classification data |
| `stopwords` | cache | `map[string]bool` | Immutable lookup table |
| `keyNounsMap` | cache | `map[string]bool` | Immutable lookup table |
| `hedgingPhrases` | eval | `[]string` | Immutable scoring data |
| `now` | experiment | `func()` | Test indirection (standard pattern) |
| `sensitivePatterns` | security | `[]*regexp.Regexp` | Compiled once, read-only |
| `content` | dashboard | `embed.FS` | Compile-time constant |
| `spanContextKey` | telemetry | struct{} | Context key (standard pattern) |

**Verdict:** These are all effectively constants. No mutable global state remains.

### D. Interface Design

- `provider.Provider` (4 methods) — well-scoped, not too wide
- `security.Middleware` (function type) — idiomatic Go middleware pattern
- Cache layers implement no shared interface but are orchestrated by `Store` — acceptable
  since they have different signatures (`Lookup` returns different types)

**Verdict:** Interface boundaries are clean.

### E. Error Types

`NexusError` is a well-designed error type with structured fields (`Code`,
`Message`, `Suggestion`, `DocsURL`, `RequestID`). Error factories like
`errProviderUnavailable()` provide consistent, actionable errors.
Internal errors use `fmt.Errorf` with `%w` wrapping appropriately.

**Verdict:** Error handling is above average.

---

## Verification

```
$ go build ./...    # ✓ zero errors
$ go test ./...     # ✓ all 20 packages pass
```

---

## Line Count After Fixes

| File | Before | After |
|------|--------|-------|
| `handler_chat.go` | 707 | 558 |
| `handler_helpers.go` | — | 166 (new) |
| `errors.go` | 171 | 101 |
| `circuitbreaker.go` | 330 | 330 |

---

## Remaining A+ Opportunities (future work)

1. **Extract streaming path** from `handleChat` into `handleChatStream` method
   to bring `handler_chat.go` under 500 lines. Requires a `chatContext` struct
   to avoid excessive parameter passing.

2. **Interface for Router** — `gateway.Server` uses `*router.Router` concrete type.
   Defining a `Router` interface would enable testing without the real router.

3. **Interface for cache.Store** — same reasoning; would improve testability.

4. **Config validation method** — `Config.Validate() error` as a method instead
   of the current `StartupValidator` which reaches into `Server` internals.
