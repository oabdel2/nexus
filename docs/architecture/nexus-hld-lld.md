# Nexus Gateway — High-Level Design & Low-Level Design
## Definitive Technical Reference v1.0

---

> **Nexus** is an open-source, agentic-first inference optimization gateway written in Go.
> It sits between AI agent frameworks and LLM providers, intelligently routing requests,
> caching responses, enforcing budgets, and providing enterprise-grade security — all
> through a single OpenAI-compatible API endpoint.

---

# PART I — HIGH-LEVEL DESIGN (HLD)

---

## 1. System Overview

### 1.1 What Is Nexus?

Nexus is a transparent HTTP reverse-proxy gateway purpose-built for **multi-agent AI
workflows**. It accepts OpenAI-compatible `/v1/chat/completions` requests and applies
a pipeline of optimizations before forwarding them to upstream LLM providers (Ollama,
OpenAI, Anthropic, or any OpenAI-compatible API).

### 1.2 Problem Statement

Modern AI agent systems (AutoGPT, CrewAI, LangGraph, custom orchestrators) make dozens
to hundreds of LLM calls per workflow. Without a gateway layer, teams face:

| Problem | Impact |
|---------|--------|
| **Cost explosion** | Every call hits the most expensive model |
| **No caching** | Identical/similar prompts re-invoke LLMs |
| **No observability** | Impossible to trace agent decision chains |
| **No budget control** | Runaway workflows burn unlimited tokens |
| **Security gaps** | Prompt injection, no auth, no rate limiting |
| **Vendor lock-in** | Switching providers requires code changes |

### 1.3 Key Design Principles

1. **Drop-in Compatibility** — Any OpenAI SDK client works unchanged; just point `base_url` at Nexus
2. **Agentic-First** — Workflow-scoped budgets, step-aware routing, agent role classification
3. **Zero External Dependencies** — Single static Go binary; Redis/Qdrant optional for scale
4. **Defense in Depth** — 14-layer security middleware chain
5. **Observable by Default** — Prometheus metrics, distributed tracing, real-time dashboard
6. **Intelligent Caching** — 3-layer cache (exact → BM25 → semantic) with false-positive filtering
7. **Self-Learning** — Synonym registry grows from usage; feedback loop improves cache quality

### 1.4 Feature Summary

| Category | Features |
|----------|----------|
| **Routing** | Complexity-based model selection, 4 tiers, budget-aware downgrade |
| **Caching** | L1 exact, L2a BM25, L2b semantic with reranker; opposite-intent and key-noun filters |
| **Security** | TLS/mTLS, OIDC SSO, RBAC, rate limiting, prompt injection guard, IP allowlist |
| **Billing** | Stripe integration, subscription plans, API key management, device tracking |
| **Observability** | Prometheus metrics, W3C distributed tracing, OTLP export, SSE dashboard |
| **Workflows** | Budget tracking, step recording, auto-detection, feedback collection |
| **Notifications** | SMTP email, 12 event types, templated messages, bulk marketing |
| **NEW: Cascade Routing** | Try cheap model first → score confidence → escalate if low |
| **NEW: Eval Pipeline** | Shadow-compare outputs, build confidence map per task-type |
| **NEW: Prompt Compression** | Strip redundant tokens before forwarding to reduce cost |

---

## 2. Architecture Diagram

```
                                    ┌─────────────────────────────────────────────────────────┐
                                    │                   NEXUS GATEWAY                         │
                                    │                                                         │
  ┌──────────┐    HTTP/HTTPS        │  ┌─────────────────────────────────────────────────┐    │
  │  Agent    │ ──────────────────► │  │           MIDDLEWARE CHAIN (14 layers)           │    │
  │Framework  │  POST /v1/chat/     │  │                                                 │    │
  │(CrewAI,   │  completions        │  │  BillingAuth → Tracing → PanicRecovery →       │    │
  │AutoGPT,   │                     │  │  BodySizeLimit → RequestTimeout →              │    │
  │LangGraph) │                     │  │  SecurityHeaders → RequestID →                 │    │
  └──────────┘                      │  │  RequestLogger → CORS → IPAllowlist →          │    │
                                    │  │  RateLimit → OIDC → InputValidator →           │    │
  ┌──────────┐                      │  │  PromptGuard → AuditLog                        │    │
  │  OpenAI   │ ──────────────────► │  └──────────────────────┬──────────────────────────┘    │
  │  SDK      │                     │                         │                               │
  │  Client   │                     │                         ▼                               │
  └──────────┘                      │  ┌──────────────────────────────────────────────┐       │
                                    │  │              handleChat()                     │       │
                                    │  │                                               │       │
                                    │  │  1. Parse request + extract Nexus headers     │       │
                                    │  │  2. Get/create workflow state                 │       │
                                    │  │  3. Check prompt injection                    │       │
                                    │  │  4. ──► CACHE LOOKUP ◄──                     │       │
                                    │  │  5. ──► COMPLEXITY ROUTER ◄──                │       │
                                    │  │  6. ──► [NEW] CASCADE ROUTING ◄──            │       │
                                    │  │  7. ──► [NEW] PROMPT COMPRESSION ◄──         │       │
                                    │  │  8. Circuit breaker check + failover          │       │
                                    │  │  9. Forward to provider (stream/non-stream)   │       │
                                    │  │ 10. ──► [NEW] EVAL PIPELINE (shadow) ◄──     │       │
                                    │  │ 11. Cache response + record metrics           │       │
                                    │  │ 12. Return with Nexus headers                 │       │
                                    │  └──────┬────────┬────────┬────────┬─────────────┘       │
                                    │         │        │        │        │                      │
                                    │         ▼        ▼        ▼        ▼                      │
                                    │  ┌─────────┐ ┌────────┐ ┌───────┐ ┌──────────┐           │
                                    │  │  CACHE   │ │ ROUTER │ │WRKFLW │ │TELEMETRY │           │
                                    │  │  STORE   │ │        │ │TRACKER│ │          │           │
                                    │  │         │ │Classify│ │       │ │ Metrics  │           │
                                    │  │ L1:Exact │ │ Route  │ │Budget │ │ Tracing  │           │
                                    │  │L2a:BM25 │ │ Budget │ │ Steps │ │ Cost     │           │
                                    │  │L2b:Sema │ │        │ │Feedbk │ │ Exporter │           │
                                    │  │         │ │Cascade │ │ Auto  │ │Dashboard │           │
                                    │  │Reranker │ │ (NEW)  │ │Detect │ │  SSE     │           │
                                    │  │Synonyms │ │        │ │       │ │          │           │
                                    │  │Feedback │ │        │ │       │ │          │           │
                                    │  │Shadow   │ │        │ │       │ │          │           │
                                    │  └────┬────┘ └───┬────┘ └───┬───┘ └────┬─────┘           │
                                    │       │         │          │          │                   │
                                    └───────┼─────────┼──────────┼──────────┼───────────────────┘
                                            │         │          │          │
                              ┌─────────────┼─────────┼──────────┘          │
                              │             │         │                     │
                              ▼             ▼         ▼                     ▼
                    ┌──────────────┐  ┌──────────┐  ┌───────────┐  ┌───────────────┐
                    │   PROVIDERS  │  │ BILLING  │  │  STORAGE  │  │  EXTERNAL     │
                    │              │  │          │  │           │  │  SERVICES     │
                    │ ┌──────────┐ │  │ Stripe   │  │ Qdrant   │  │               │
                    │ │  Ollama  │ │  │ Subs     │  │ Redis    │  │ Prometheus    │
                    │ │ (local)  │ │  │ API Keys │  │ Memory   │  │ Grafana       │
                    │ └──────────┘ │  │ Devices  │  │          │  │ OTLP/Jaeger   │
                    │ ┌──────────┐ │  │ Notify   │  │          │  │ SMTP          │
                    │ │  OpenAI  │ │  └──────────┘  └──────────┘  └───────────────┘
                    │ │  (cloud) │ │
                    │ └──────────┘ │
                    │ ┌──────────┐ │
                    │ │Anthropic │ │
                    │ │  (cloud) │ │
                    │ └──────────┘ │
                    │ Circuit      │
                    │ Breakers     │
                    │ per provider │
                    └──────────────┘
```

---

## 3. Component Descriptions

### 3.1 Gateway Server (`internal/gateway/`)

The core HTTP server that binds all components together. It initializes providers,
cache layers, router, workflow tracker, billing stores, and the telemetry stack.
The `Start()` method assembles the 14-layer middleware chain and registers all HTTP
routes. The `handleChat()` method implements the complete request pipeline: parse →
cache check → route → forward → record → respond. Supports both streaming (SSE) and
non-streaming responses. Includes a `StartupValidator` that performs pre-flight checks:
config validation, provider reachability, Ollama model verification, and GPU model warmup.

### 3.2 Router (`internal/router/`)

The intelligence layer that decides which model handles each request. The `Classifier`
scores prompts on 7 dimensions (keyword complexity, prompt length, structural complexity,
context length, agent role, workflow position, budget pressure) using a weighted formula.
The `Router` maps final scores to 4 tiers (economy/cheap/mid/premium) with configurable
thresholds, applies budget-aware downgrades, and implements a fallback chain when a
requested tier is unavailable. The `BudgetManager` enforces workflow-scoped spending limits.

### 3.3 Cache Store (`internal/cache/`)

A 3-layer hierarchical cache system designed for LLM response deduplication:

- **L1 (Exact)**: SHA256 hash matching for byte-identical prompts (~1us lookup)
- **L2a (BM25)**: Keyword-based similarity with TF-IDF scoring, stemming, and stopword removal
- **L2b (Semantic)**: Dense embedding vectors (Ollama/OpenAI) with cosine similarity and optional cross-encoder reranking

Advanced false-positive prevention includes opposite-intent detection (70+ antonym pairs),
key-noun filtering (250+ technology terms), query-type-adaptive thresholds, and context
fingerprinting for multi-turn conversations. A self-learning synonym registry (base →
learned → candidate tiers) grows from near-misses and feedback. Shadow mode enables
parallel validation without serving wrong results.

### 3.4 Provider Layer (`internal/provider/`)

An abstraction over LLM backends via the `Provider` interface (Name, Send, SendStream,
HealthCheck). The `OpenAIProvider` implements this for any OpenAI-compatible API (Ollama,
OpenAI, Anthropic via proxy, vLLM, etc.) with connection pooling, HTTP/2 support, and
120-second timeouts. The `CircuitBreaker` implements a 3-state (closed/open/half-open)
pattern with exponential backoff, jitter, and configurable thresholds. The `ProviderPool`
manages circuit breakers for all providers and enables automatic failover. The `HealthChecker`
runs background health probes every 30 seconds.

### 3.5 Security (`internal/security/`)

A comprehensive security framework with 14 middleware components:

- **TLS/mTLS**: TLS 1.2+ with strong cipher suites, optional mutual TLS with CA verification
- **OIDC SSO**: OpenID Connect auto-discovery, JWT validation via UserInfo endpoint, domain restriction
- **RBAC**: Role-based access control with wildcard permissions and path-to-permission mapping
- **Rate Limiting**: Per-tenant token bucket algorithm with configurable RPM and burst
- **Prompt Guard**: 16 regex patterns + 8 phrase blockers for injection detection, block or sanitize mode
- **IP Allowlist**: CIDR-based access control for admin endpoints
- **Input Validator**: JSON schema validation for chat requests
- Plus: body size limits, request timeouts, panic recovery, security headers, CORS, request ID, audit logging

### 3.6 Telemetry (`internal/telemetry/`)

Full observability stack with three pillars:

- **Metrics**: Lock-free Prometheus-format counters, histograms, and gauges exposed at `/metrics`
- **Tracing**: W3C traceparent-compatible distributed tracing with span hierarchy, OTLP batch export
- **Cost Tracking**: Per-workflow and per-team cost aggregation with cache savings calculation

### 3.7 Billing (`internal/billing/`)

Stripe-integrated subscription management with:

- **Subscription Plans**: Free (1K/mo, 10 RPM), Starter (50K/mo, 60 RPM), Team (500K/mo, 300 RPM), Enterprise (unlimited)
- **API Key Management**: SHA-256 hashed keys with `nxs_live_` / `nxs_test_` prefixes, per-key usage tracking
- **Device Tracking**: Fingerprint-based device identification (User-Agent + truncated IP), per-plan limits
- **Stripe Webhooks**: HMAC-SHA256 verified webhook handling for subscription lifecycle events

### 3.8 Workflow Tracker (`internal/workflow/`)

Manages multi-step agent workflow state:

- Per-workflow budget tracking with configurable limits
- Step recording with model, tier, tokens, cost, latency, and cache hit status
- Budget ratio and step ratio calculations for routing decisions
- Auto-detection of workflow boundaries from request patterns (fingerprinting)
- Feedback collection for outcome recording

### 3.9 Dashboard (`internal/dashboard/`)

Real-time monitoring dashboard served via Server-Sent Events (SSE):

- Live request feed with complexity scores, tier selections, and costs
- Aggregate statistics: total requests, cache hit rate, cumulative cost/savings
- Routing distribution visualization (economy/cheap/mid/premium percentages)
- Workflow budget monitoring with remaining budget visualization
- Embedded HTML/CSS/JS with GitHub-inspired dark theme

### 3.10 Notification (`internal/notification/`)

SMTP-based email notification system:

- 12 event types covering the full user lifecycle (welcome, usage warnings, subscription events, payment events)
- Go template-based email rendering
- Async worker with 500-slot queue and graceful drain on shutdown
- Event logging with persistence for audit trails

### 3.11 Storage (`internal/storage/`)

Pluggable storage backends with factory pattern:

- **Vector Store**: Memory (default) or Qdrant for semantic embeddings
- **KV Store**: Memory (default) or Redis for key-value data
- Qdrant integration via HTTP API with collection management
- Redis integration via raw TCP/RESP protocol (no external dependencies)

### 3.12 Events (`internal/events/`)

Event bus system for internal and external event dispatch:

- Webhook delivery with HMAC-SHA256 signatures
- Async event dispatch to registered handlers
- Supports custom event types and payloads

### 3.13 Plugin System (`internal/plugin/`)

Extensibility framework for custom behavior:

- Plugin registry for classifiers, routers, and hooks
- Lifecycle event emission at key pipeline stages
- Hook points for pre-route, post-route, pre-cache, post-cache

### 3.14 Auth (`internal/auth/`)

API key validation and management:

- Key validation with rate limiting and budget tracking
- Usage monitoring and quota enforcement

---

## 4. Request Lifecycle

### 4.1 Complete Request Flow (Non-Streaming)

```
Client                    Nexus Gateway                     LLM Provider
  |                            |                                 |
  |  POST /v1/chat/completions |                                 |
  |  ------------------------->|                                 |
  |                            |                                 |
  |                     +------+------+                          |
  |                     |  MIDDLEWARE  |                          |
  |                     |  CHAIN (14) |                          |
  |                     |             |                          |
  |                     | 1. BillingAuth: Validate nxs_ key     |
  |                     | 2. Tracing: Create root span          |
  |                     | 3. PanicRecovery: defer recover()     |
  |                     | 4. BodySizeLimit: Check < 1MB         |
  |                     | 5. RequestTimeout: Set 30s deadline   |
  |                     | 6. SecurityHeaders: Add HSTS, CSP     |
  |                     | 7. RequestID: Generate/propagate      |
  |                     | 8. RequestLogger: Start timer         |
  |                     | 9. CORS: Check origin                 |
  |                     | 10. IPAllowlist: Check CIDR           |
  |                     | 11. RateLimit: Token bucket check     |
  |                     | 12. OIDC: Validate Bearer token       |
  |                     | 13. InputValidator: Schema check      |
  |                     | 14. PromptGuard: Injection check      |
  |                     |     AuditLog: Record request          |
  |                     +------+------+                          |
  |                            |                                 |
  |                     +------+------+                          |
  |                     | handleChat()|                          |
  |                     |             |                          |
  |                     | Parse JSON body                       |
  |                     | Extract X-Workflow-ID,                |
  |                     |   X-Agent-Role, X-Team headers        |
  |                     | Get/create workflow state              |
  |                     | Extract prompt text                    |
  |                     +------+------+                          |
  |                            |                                 |
  |                     +------+------+                          |
  |                     | CACHE CHECK |                          |
  |                     |             |                          |
  |                     | L1: SHA256 exact match?               |
  |                     |   Hit -> Return cached                |
  |                     |          (X-Nexus-Cache: L1)          |
  |                     | L2a: BM25 keyword match?              |
  |                     |   Hit -> Return cached                |
  |                     |          (X-Nexus-Cache: L2a)         |
  |                     | L2b: Semantic cosine match?            |
  |                     |   Check: opposite intent?             |
  |                     |   Check: different key noun?          |
  |                     |   Check: reranker verification?       |
  |                     |   Hit -> Return cached                |
  |                     |          (X-Nexus-Cache: L2b)         |
  |                     +------+------+                          |
  |                            | Cache Miss                      |
  |                     +------+------+                          |
  |                     |   ROUTING   |                          |
  |                     |             |                          |
  |                     | ClassifyComplexity()                  |
  |                     |   > Keyword scoring (high/mid/low)    |
  |                     |   > Length scoring                     |
  |                     |   > Structure scoring                  |
  |                     |   > Context scoring                    |
  |                     |   > Role scoring                       |
  |                     |   > Position scoring                   |
  |                     |   > Budget scoring                     |
  |                     | Weighted FinalScore calculation        |
  |                     | Tier selection by thresholds           |
  |                     | Budget override check                  |
  |                     | selectModelWithFallback()              |
  |                     +------+------+                          |
  |                            |                                 |
  |                     +------+------+                          |
  |                     |CIRCUIT BREAK|                          |
  |                     |             |                          |
  |                     | cb.Allow()? |                          |
  |                     |  No -> findFallbackProvider()         |
  |                     |  Yes -> Continue                      |
  |                     +------+------+                          |
  |                            |                                 |
  |                            |  POST /chat/completions         |
  |                            | ------------------------------>|
  |                            |                                 |
  |                            |<------------- JSON Response     |
  |                            |                                 |
  |                     +------+------+                          |
  |                     |  POST-PROC  |                          |
  |                     |             |                          |
  |                     | Record circuit breaker success         |
  |                     | Calculate cost (tokens/1000 * rate)    |
  |                     | Cache response in all enabled layers   |
  |                     | Record workflow step                   |
  |                     | Record Prometheus metrics              |
  |                     | Push dashboard event                   |
  |                     | Set response headers:                  |
  |                     |   X-Nexus-Model, X-Nexus-Tier         |
  |                     |   X-Nexus-Provider, X-Nexus-Cost      |
  |                     |   X-Nexus-Workflow                     |
  |                     +------+------+                          |
  |                            |                                 |
  |<---------------------------|                                 |
  |  200 OK + JSON response    |                                 |
  |  + Nexus extension headers |                                 |
```

### 4.2 Streaming Request Flow

For streaming requests (`"stream": true`):

1. Same middleware + cache check + routing flow
2. Response headers set immediately: `Content-Type: text/event-stream`
3. Provider `SendStream()` called with `http.ResponseWriter`
4. Each SSE chunk forwarded and flushed to client in real-time
5. Usage extracted from final `[DONE]` chunk
6. Cost calculated after stream completes
7. Cached response is NOT stored (stream responses bypass cache)

---

## 5. Data Flow Diagrams

### 5.1 Cache Lookup Chain

```
                    Incoming Prompt
                         |
                         v
                +------------------+
                |  L1: Exact       |
                |  SHA256(prompt)  |
                |  lookup in map   |
                +--------+---------+
                    Hit? |
              +---Yes----+---No----+
              |          |         |
              v          |         v
         Return L1       |  +------------------+
         cached          |  |  L2a: BM25       |
                         |  |  tokenize()      |
                         |  |  termFreq()      |
                         |  |  BM25 score      |
                         |  |  threshold>15.0  |
                         |  +--------+---------+
                         |      Hit? |
                         | +--Yes----+---No----+
                         | |         |         |
                         | v         |         v
                         |Return L2a |  +-------------------+
                         |cached     |  |  L2b: Semantic    |
                         |           |  |  1. Expand syns   |
                         |           |  |  2. Get embedding  |
                         |           |  |  3. Normalize      |
                         |           |  |  4. Best cosine    |
                         |           |  |  5. Adaptive thr   |
                         |           |  |  6. Opp. intent?   |
                         |           |  |  7. Diff noun?     |
                         |           |  |  8. Reranker?      |
                         |           |  +---------+----------+
                         |           |       Hit? |
                         |           | +---Yes----+---No---+
                         |           | |          |        |
                         |           | v          |        v
                         |           |Return      |   CACHE MISS
                         |           |L2b         |   -> Route to
                         |           |cached      |     provider
```

### 5.2 Provider Selection Flow

```
   ComplexityScore
        |
        v
   +----------------------------------------------+
   |  FinalScore = basePrompt*0.30 +              |
   |               contextScore*0.15 +            |
   |               roleScore*0.20 +               |
   |               positionScore*0.15 +           |
   |               budgetScore*0.20               |
   +----------------+-----------------------------+
                    |
           +--------+--------+
           |  Tier Selection  |
           +--------+--------+
                    |
   +----------------+------------------+
   |                |                  |
   v                v                  v                v
score>=T*0.8   score>=T*0.5      score>=T*0.3     score<T*0.3
  PREMIUM        MID               CHEAP           ECONOMY
   |                |                  |                |
   |         +------+------+          |                |
   |         |Budget Check |          |                |
   |         |ratio<0.15?  |          |                |
   |         |->downgrade  |          |                |
   |         |ratio<0.05?  |          |                |
   |         |->force econ |          |                |
   |         +------+------+          |                |
   |                |                  |                |
   +----------------+------------------+----------------+
                    |
                    v
   +-----------------------------+
   |  selectModelWithFallback()  |
   |  Try requested tier first   |
   |  Fallback: economy->cheap-> |
   |            mid->premium     |
   |  Ultimate: first available  |
   +-----------------------------+
```

---

## 6. Integration Points

### 6.1 LLM Providers

| Provider | Protocol | Endpoints Used | Auth |
|----------|----------|---------------|------|
| **Ollama** | HTTP | `/chat/completions`, `/api/embed`, `/api/tags`, `/api/rerank` | None (local) |
| **OpenAI** | HTTPS | `/chat/completions`, `/v1/embeddings`, `/models` | Bearer API key |
| **Anthropic** | HTTPS | `/chat/completions` (via proxy) | Bearer API key + custom headers |
| **Any OpenAI-compatible** | HTTP/S | `/chat/completions` | Configurable |

### 6.2 Storage Backends

| System | Protocol | Purpose | Configuration |
|--------|----------|---------|---------------|
| **Qdrant** | HTTP | Vector storage for semantic embeddings | `qdrant_host`, `qdrant_port`, `qdrant_collection` |
| **Redis** | TCP/RESP | Key-value cache, session storage | `redis_addr`, `redis_password`, `redis_db` |
| **Filesystem** | Local | Synonym persistence, billing data, event logs | `data_dir` paths |

### 6.3 External Services

| Service | Protocol | Purpose |
|---------|----------|---------|
| **Stripe** | HTTPS webhook | Subscription lifecycle, payment processing |
| **SMTP** | TCP/STARTTLS | Email notifications (12 event types) |
| **Prometheus** | HTTP scrape | Metrics collection from `/metrics` |
| **Grafana** | N/A | Dashboard visualization (consumes Prometheus) |
| **OTLP Collector** | HTTPS | Distributed trace export (Jaeger/Zipkin/etc.) |
| **OIDC IdP** | HTTPS | SSO authentication (Okta, Auth0, Keycloak, etc.) |

---

## 7. Security Architecture

### 7.1 Middleware Chain (Execution Order)

```
Request --> [1]  BillingAuth
            [2]  Tracing
            [3]  PanicRecovery
            [4]  BodySizeLimit (1MB default)
            [5]  RequestTimeout (30s default)
            [6]  SecurityHeaders (HSTS, CSP, X-Frame-Options, etc.)
            [7]  RequestID (generate/propagate)
            [8]  RequestLogger (structured JSON)
            [9]  CORS (configurable origins)
            [10] IPAllowlist (CIDR-based, admin paths)
            [11] RateLimit (token bucket, per-tenant)
            [12] OIDC SSO (Bearer token -> UserInfo)
            [13] InputValidator (JSON schema for /v1/chat/completions)
            [14] PromptGuard (16 regex + 8 phrase detectors)
                 AuditLog (compliance logging)
            --> Handler
```

### 7.2 Authentication Flow

```
                   +----------------+
                   |  API Key Auth  |
                   |  (nxs_ prefix) |
                   +-------+--------+
                           |
              +------------+------------+
              |                         |
              v                         v
       +------------+           +------------+
       | Billing Key|           |  OIDC SSO  |
       | Validation |           |  Token     |
       |            |           |  Validation|
       | Check hash |           |            |
       | Check expiry|          | UserInfo   |
       | Check quota|           | endpoint   |
       | Check device|          | Domain     |
       | Record usage|          | restriction|
       +------------+           +------------+
```

### 7.3 Prompt Injection Protection

The `PromptGuard` uses a layered detection approach:

1. **Length Check**: Reject prompts exceeding `max_prompt_length` (default 32,000 chars)
2. **Regex Patterns** (16 built-in): Detect "ignore previous instructions", "jailbreak", template injection, etc.
3. **Phrase Matching** (8 built-in): Block known attack phrases ("DAN mode", "sudo mode", etc.)
4. **Risk Scoring**: `score = min(threatCount * 0.3, 1.0)`
5. **Action**: Block (return 400) or Sanitize (redact and forward)

### 7.4 TLS Configuration

- Minimum TLS 1.2 (configurable to 1.3)
- Strong cipher suites only (ECDHE + AES-GCM/ChaCha20)
- Optional mutual TLS with CA certificate verification
- Certificate chain validation
- Auto-cert support for automated certificate management

---

## 8. Deployment Architecture

### 8.1 Single Binary

```bash
# Build
go build -o nexus ./cmd/nexus

# Run
./nexus -config configs/nexus.yaml
```

The Go binary is fully self-contained with zero external dependencies.
In-memory storage backends are used by default.

### 8.2 Docker

Multi-stage build: golang:1.23-alpine builder -> alpine:3.19 runtime.
Static binary with `CGO_ENABLED=0` and `-ldflags="-s -w"`.
Exposes ports 8080 (gateway) and 9090 (metrics).

### 8.3 Docker Compose (Basic)

3-service stack: Nexus + Prometheus + Grafana

### 8.4 Docker Compose (Enterprise)

7-service stack: Nexus + Qdrant + Redis + Ollama + Prometheus + Grafana + AlertManager

### 8.5 Kubernetes (Helm Chart)

14-file Helm chart in `deploy/helm/nexus/`:

- Deployment with configurable replicas, resources, probes
- Service (ClusterIP), Ingress with TLS
- ConfigMap for nexus.yaml
- ServiceMonitor for Prometheus Operator
- HPA for auto-scaling
- PDB for pod disruption budget
- ServiceAccount with RBAC

```
deploy/helm/nexus/
  Chart.yaml
  values.yaml
  templates/
    deployment.yaml
    service.yaml
    configmap.yaml
    ingress.yaml
    hpa.yaml
    pdb.yaml
    serviceaccount.yaml
    servicemonitor.yaml
    _helpers.tpl
```

---

## 9. New Features Architecture

### 9.1 Cascade Routing (NEW)

**Problem**: Current routing makes a single model selection. If a cheap model produces
a low-quality response, there is no recovery mechanism.

**Solution**: Try the cheapest viable model first. If the response confidence is below
a threshold, transparently escalate to a higher-tier model.

```
   Request --> Router selects initial tier (e.g., "cheap")
                    |
                    v
            +---------------+
            | Cheap Model   |
            | Generate resp |
            +-------+-------+
                    |
                    v
            +---------------+
            | Confidence    |
            | Scorer        |
            |               |
            | embedding sim |
            | + heuristics  |
            | + judge LLM   |
            +-------+-------+
                    |
            score >= threshold?
           +--Yes---+---No--+
           |        |       |
           v        |       v
       Return       |  +---------------+
       cheap        |  | Escalate to   |
       response     |  | mid/premium   |
                    |  | model         |
                    |  +-------+-------+
                    |          |
                    |          v
                    |      Return
                    |      premium
                    |      response
```

**Integration Point**: Between router tier selection and provider forwarding in `handleChat()`.

### 9.2 Eval Pipeline (NEW)

**Problem**: No systematic way to measure whether cache hits return correct responses
or whether the router selects appropriate models.

**Solution**: Shadow evaluation that compares outputs without affecting the user,
building a per-task-type confidence map over time.

```
   Request --> Normal pipeline --> Response to user
                    |
                    | (async, shadow)
                    v
            +---------------+
            | Eval Pipeline |
            |               |
            | 1. Run same   |
            |    prompt on  |
            |    reference  |
            |    model      |
            |               |
            | 2. Compare:   |
            |    embedding  |
            |    similarity |
            |    + semantic |
            |    match      |
            |               |
            | 3. Record:    |
            |    task_type  |
            |    model_used |
            |    confidence |
            |    agreement  |
            +-------+-------+
                    |
                    v
            +---------------+
            | Confidence    |
            | Map           |
            |               |
            | Per task-type:|
            |  code: 0.85   |
            |  debug: 0.72  |
            |  factual: 0.95|
            |  howto: 0.88  |
            +---------------+
```

**Integration Point**: Extends existing `ShadowMode` in the cache package. Runs async
after response is sent to user.

### 9.3 Prompt Compression (NEW)

**Problem**: Agent frameworks often send verbose prompts with repeated context,
boilerplate instructions, and redundant tokens, inflating costs.

**Solution**: A 4-strategy compression pipeline that strips redundant tokens before
forwarding to the provider.

```
   Original Prompt
        |
        v
   +---------------------+
   | Strategy 1:          |
   | System Prompt Dedup  |
   | Remove repeated      |
   | system instructions  |
   +----------+----------+
              |
              v
   +---------------------+
   | Strategy 2:          |
   | Whitespace Normal.   |
   | Collapse runs of     |
   | spaces/newlines      |
   +----------+----------+
              |
              v
   +---------------------+
   | Strategy 3:          |
   | Context Window Trim  |
   | Keep only last N     |
   | turns of conversation|
   +----------+----------+
              |
              v
   +---------------------+
   | Strategy 4:          |
   | Filler Removal       |
   | Strip known filler   |
   | patterns (e.g.,      |
   | "Please note that",  |
   | "As mentioned before"|
   +----------+----------+
              |
              v
   Compressed Prompt
   (forwarded to provider)
```

**Integration Point**: After routing, before provider forwarding in `handleChat()`.
Compression ratio tracked in metrics.

---

## 10. Non-Functional Requirements

### 10.1 Performance Targets

| Metric | Target | Mechanism |
|--------|--------|-----------|
| L1 cache hit latency | < 1ms | In-memory SHA256 map lookup |
| L2a cache hit latency | < 10ms | In-memory BM25 scoring |
| L2b cache hit latency | < 50ms | Embedding + cosine similarity |
| Middleware overhead | < 2ms | Minimal per-middleware processing |
| Streaming TTFB | < 100ms overhead | Direct SSE forwarding with flush |
| Metrics scrape | < 100ms | Lock-free atomic operations |
| Startup time | < 5s (cold), < 30s (with warmup) | Parallel provider checks |

### 10.2 Scalability

- **Horizontal**: Stateless gateway; multiple instances behind load balancer
- **Vertical**: In-memory caches scale with RAM; 50K entries ~= 200MB
- **Storage**: Qdrant + Redis for shared state across instances
- **Concurrency**: Go goroutines; RWMutex for cache; lock-free metrics

### 10.3 Reliability

- **Circuit Breakers**: Per-provider with exponential backoff (30s -> 5m max)
- **Retry with Backoff**: 3 retries, 100ms -> 5s, 50% jitter
- **Failover**: Automatic tier upgrade when provider unavailable
- **Graceful Shutdown**: Context cancellation, queue drain, final metric flush
- **Health Probes**: `/health/live` (liveness), `/health/ready` (readiness)

### 10.4 Data Durability

- Synonym registry persisted to JSON file every 5 minutes
- Billing data persisted to disk (configurable data_dir)
- Event logs persisted with ring buffer (10K max entries)
- Cache is ephemeral (by design — LLM responses can be regenerated)

---

# PART II — LOW-LEVEL DESIGN (LLD)

---

## 1. Package Structure

```
nexus/
  cmd/
    nexus/
      main.go              -- Entry point: config loading, server init, signal handling
  internal/
    auth/
      apikeys.go           -- API key validation, rate limiting, budget tracking
    billing/
      apikey.go            -- API key generation, validation, quota management
      billing_test.go      -- 30+ billing tests
      device_tracker.go    -- Device fingerprinting and limit enforcement
      stripe_webhook.go    -- Stripe webhook handling with HMAC verification
      subscription.go      -- Subscription plans, lifecycle, Stripe integration
    cache/
      bm25.go              -- BM25 keyword cache (L2a)
      context.go           -- Context fingerprinting for multi-turn
      exact.go             -- Exact match cache (L1)
      feedback.go          -- Cache quality feedback collection
      filters.go           -- Opposite intent + key noun detection
      querytype.go         -- Query type classification + adaptive thresholds
      reranker.go          -- Cross-encoder verification
      semantic.go          -- Semantic embedding cache (L2b)
      semantic_test.go     -- 2,072 lines of cache tests
      shadow.go            -- Shadow mode for parallel validation
      store.go             -- 3-layer cache orchestrator
      synonym_registry.go  -- Self-learning synonym system
    config/
      config.go            -- YAML config types, loading, defaults (395 lines)
    dashboard/
      dashboard.go         -- SSE event bus, real-time stats
      index.html           -- Embedded dashboard UI (430 lines)
    events/
      events.go            -- Event bus with webhook support
    gateway/
      server.go            -- Core HTTP server (1,155 lines)
      startup.go           -- Pre-flight validation (290 lines)
    notification/
      event_log.go         -- Event log persistence
      notifier.go          -- SMTP email notifier
      notification_test.go -- Notification tests
    plugin/
      plugin.go            -- Plugin registry for extensibility
    provider/
      circuitbreaker.go    -- 3-state circuit breaker with backoff
      circuitbreaker_test.go -- Circuit breaker tests
      health.go            -- Background health checker
      openai.go            -- OpenAI-compatible provider
      provider.go          -- Provider interface definition
    router/
      budget.go            -- Budget enforcement
      classifier.go        -- 7-dimension complexity scoring
      router.go            -- Tier selection + model fallback
    security/
      hardening.go         -- BodySizeLimit, RequestTimeout, PanicRecovery, IPAllowlist,
                              InputValidator, RequestLogger, extractClientIP
      middleware.go        -- Chain, SecurityHeaders, RequestID, AuditLog, CORS
      oidc.go              -- OIDC/SSO authentication
      prompt_guard.go      -- Prompt injection detection
      rate_limiter.go      -- Token bucket rate limiter
      rbac.go              -- Role-based access control
      security_test.go     -- Security tests
      tls.go               -- TLS/mTLS configuration
    storage/
      factory.go           -- Storage factory functions
      memory.go            -- In-memory vector + KV store
      qdrant.go            -- Qdrant vector database client
      redis.go             -- Redis KV client (raw RESP)
      storage_test.go      -- Storage backend tests
      vector_store.go      -- Storage interfaces
    telemetry/
      cost.go              -- Workflow + team cost tracking
      exporter.go          -- OTLP span batch exporter
      metrics.go           -- Prometheus-format metrics
      metrics_test.go      -- Metrics tests
      trace_middleware.go  -- HTTP tracing middleware
      tracing.go           -- Distributed tracing engine
      tracing_test.go      -- Tracing tests
    workflow/
      autodetect.go        -- Workflow boundary auto-detection
      feedback.go          -- Feedback collection handler
      tracker.go           -- Workflow state + budget tracking
  configs/
    nexus.yaml             -- Standard config
    nexus.enterprise.yaml  -- Enterprise config (Qdrant+Redis+TLS)
    nexus.test.yaml        -- Test config (port 18080)
  tests/
    e2e/
      main.go              -- 32 E2E integration tests (1,234 lines)
  deploy/
    helm/
      nexus/               -- 14-file Helm chart
  monitoring/              -- Prometheus + Grafana configs
  benchmarks/              -- Performance benchmarks
  docker-compose.yml           -- Basic 3-service stack
  docker-compose.enterprise.yml -- Enterprise 7-service stack
  Dockerfile                   -- Multi-stage Alpine build
  go.mod                       -- gopkg.in/yaml.v3 (sole dependency)
  go.sum
```

---

## 2. Data Models

### 2.1 Provider Types

```go
// Provider interface -- implemented by OpenAIProvider
type Provider interface {
    Name() string
    Send(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    SendStream(ctx context.Context, req ChatRequest, w io.Writer) (*Usage, error)
    HealthCheck(ctx context.Context) error
}

// ChatRequest -- OpenAI-compatible request
type ChatRequest struct {
    Model       string    `json:"model"`
    Messages    []Message `json:"messages"`
    MaxTokens   int       `json:"max_tokens,omitempty"`
    Temperature float64   `json:"temperature,omitempty"`
    Stream      bool      `json:"stream,omitempty"`
    WorkflowID  string    `json:"-"`  // Nexus-internal, stripped before forwarding
    StepNumber  int       `json:"-"`
    AgentRole   string    `json:"-"`
}

// Message -- Chat message
type Message struct {
    Role    string `json:"role"`    // "system", "user", "assistant"
    Content string `json:"content"`
}

// ChatResponse -- OpenAI-compatible response
type ChatResponse struct {
    ID      string   `json:"id"`
    Object  string   `json:"object"`    // "chat.completion"
    Model   string   `json:"model"`
    Choices []Choice `json:"choices"`
    Usage   Usage    `json:"usage"`
}

// Choice -- Single completion choice
type Choice struct {
    Index        int     `json:"index"`
    Message      Message `json:"message"`
    FinishReason string  `json:"finish_reason"`  // "stop", "length"
}

// Usage -- Token counts
type Usage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}
```

### 2.2 Router Types

```go
// ComplexityScore -- Multi-dimensional prompt analysis
type ComplexityScore struct {
    PromptScore   float64  // Keyword-based complexity (0-1)
    ContextScore  float64  // Context length factor (0-1)
    RoleScore     float64  // Agent role weight (0-1)
    PositionScore float64  // Workflow step position (0-1)
    BudgetScore   float64  // Budget pressure = 1 - budgetRatio (0-1)
    LengthScore   float64  // Prompt length factor (0-1)
    StructScore   float64  // Structural complexity (0-1)
    FinalScore    float64  // Weighted combination (0-1)
}

// ModelSelection -- Router output
type ModelSelection struct {
    Provider string          // Selected provider name
    Model    string          // Selected model name
    Tier     string          // "economy", "cheap", "mid", "premium"
    Score    ComplexityScore // Full scoring breakdown
    Reason   string          // Human-readable selection reason
}

// BudgetManager -- Workflow budget enforcement
type BudgetManager struct {
    enabled       bool
    defaultBudget float64
}
```

### 2.3 Cache Types

```go
// Store -- 3-layer cache orchestrator
type Store struct {
    exact    *ExactCache
    bm25     *BM25Cache
    semantic *SemanticCache
    l1Enabled, l2aEnabled, l2bEnabled bool
    feedback *FeedbackStore
    shadow   *ShadowMode
    context  *ContextFingerprint
    registry *SynonymRegistry
}

// CacheEntry -- Stored cached response
type CacheEntry struct {
    Response  []byte
    CreatedAt time.Time
    HitCount  int
}

// SynonymRegistry -- 3-tier self-learning synonym system
type SynonymRegistry struct {
    mu               sync.RWMutex
    base             map[string]string        // Tier 1: compiled (read-only, 60+ entries)
    learned          map[string]string        // Tier 2: persisted to disk
    candidates       map[string]*SynonymCandidate  // Tier 3: pending promotion
    baseKeyNouns     map[string]bool
    learnedKeyNouns  map[string]bool
    config           RegistryConfig
    stopCh, doneCh   chan struct{}
}

// SynonymCandidate -- Pending synonym awaiting promotion
type SynonymCandidate struct {
    Term          string
    Expansion     string
    Confirmations int
    Source        string   // "near_miss", "feedback", "cooccurrence"
    FirstSeen     time.Time
    LastConfirmed time.Time
    Examples      []string // Up to 5 example contexts
}

// ContextFingerprint -- Multi-turn context hashing
type ContextFingerprint struct {
    MaxTurns int  // Default: 3
}

// Reranker -- Cross-encoder verification
type Reranker struct {
    endpoint  string
    model     string         // Default: "bge-reranker-v2-m3"
    threshold float64        // Default: 0.5
    client    http.Client    // 5s timeout
    enabled   bool
}
```

### 2.4 Circuit Breaker Types

```go
type CBState int
const (
    StateClosed   CBState = iota  // Normal operation
    StateOpen                     // Fast-fail all requests
    StateHalfOpen                 // Probing: limited requests
)

type CircuitBreakerConfig struct {
    FailureThreshold int            // Default: 5
    SuccessThreshold int            // Default: 2
    Timeout          time.Duration  // Default: 30s
    MaxTimeout       time.Duration  // Default: 5m
    HalfOpenMax      int            // Default: 1
}

type CircuitBreaker struct {
    mu               sync.Mutex
    state            CBState
    config           CircuitBreakerConfig
    provider         string
    failures         int
    successes        int
    consecutiveFails int
    lastFailure      time.Time
    openedAt         time.Time
    currentTimeout   time.Duration
    TotalRequests    int64
    TotalFailures    int64
    TotalOpens       int64
    OnStateChange    func(provider string, from, to CBState)
}

type ProviderPool struct {
    mu       sync.RWMutex
    breakers map[string]*CircuitBreaker
    config   CircuitBreakerConfig
}

type RetryConfig struct {
    MaxRetries  int            // Default: 3
    BaseDelay   time.Duration  // Default: 100ms
    MaxDelay    time.Duration  // Default: 5s
    JitterRatio float64        // Default: 0.5
}
```

### 2.5 Billing Types

```go
type Plan struct {
    Name         string
    MaxRequests  int      // Monthly limit
    MaxRPM       int      // Requests per minute
    MaxDevices   int      // Concurrent devices
    PriceMonthly float64  // USD
}

// Default plans:
// Free:       1,000/mo,  10 RPM, 1 device,  $0
// Starter:   50,000/mo,  60 RPM, 3 devices, $29
// Team:     500,000/mo, 300 RPM, 10 devices, $99
// Enterprise: unlimited

type Subscription struct {
    ID                 string
    UserID             string
    PlanID             string
    StripeSubID        string
    Status             string     // "active", "past_due", "canceled", "expired"
    CurrentPeriodStart time.Time
    CurrentPeriodEnd   time.Time
    CreatedAt          time.Time
    UpdatedAt          time.Time
}

type APIKey struct {
    ID             string
    KeyHash        string    // SHA-256
    KeyPrefix      string    // "nxs_live_" or "nxs_test_"
    UserID         string
    SubscriptionID string
    Scopes         []string
    CreatedAt      time.Time
    ExpiresAt      time.Time
    Revoked        bool
    MonthlyUsage   int
    LastUsed       time.Time
}

type Device struct {
    ID           string
    UserID       string
    Fingerprint  string    // SHA256(UserAgent + truncated IP)
    FirstSeen    time.Time
    LastSeen     time.Time
    RequestCount int64
}
```

### 2.6 Telemetry Types

```go
type Metrics struct {
    // Counters (atomic int64)
    requestsTotal      atomic.Int64
    cacheHitsTotal     atomic.Int64
    cacheMissesTotal   atomic.Int64
    tokensTotal        atomic.Int64
    securityBlocks     atomic.Int64
    synonymsLearned    atomic.Int64
    // Cost (atomic float64 via CAS)
    costTotal          uint64  // atomic, stores float64 bits
    costSaved          uint64
    // Histograms
    requestDuration    *histogram
    cacheLookup        *histogram
    embeddingDuration  *histogram
    // Gauges
    cacheEntries       atomic.Int64
    activeRequests     atomic.Int64
}

type Span struct {
    TraceID    string
    SpanID     string
    ParentID   string
    Name       string
    StartTime  time.Time
    EndTime    time.Time
    Attributes map[string]string
    Events     []SpanEvent
    Status     string    // "ok", "error"
    Sampled    bool
}

type WorkflowCost struct {
    WorkflowID   string
    TotalCost    float64
    TotalTokens  int
    Steps        int
    CacheHits    int
    CacheSavings float64
}
```

### 2.7 Workflow Types

```go
type StepRecord struct {
    StepNumber int
    Model      string
    Tier       string
    Tokens     int
    Cost       float64
    Latency    time.Duration
    CacheHit   bool
    Outcome    string    // "success", "failure", "timeout"
    Timestamp  time.Time
}

type WorkflowState struct {
    ID         string
    TotalSteps int
    TotalCost  float64
    Budget     float64     // Default: 1.0
    Steps      []StepRecord
}

type Tracker struct {
    mu        sync.RWMutex
    workflows map[string]*WorkflowState
    ttl       time.Duration  // Default: 1 hour
}
```

### 2.8 Notification Types

```go
type EventType string
const (
    EventWelcome          EventType = "welcome"
    EventUsageWarning80   EventType = "usage_warning_80"
    EventUsageWarning90   EventType = "usage_warning_90"
    EventSubExpiring7d    EventType = "sub_expiring_7d"
    EventSubExpiring3d    EventType = "sub_expiring_3d"
    EventSubExpiring1d    EventType = "sub_expiring_1d"
    EventSubExpired       EventType = "sub_expired"
    EventKeyRevoked       EventType = "key_revoked"
    EventPaymentFailed    EventType = "payment_failed"
    EventPaymentSucceeded EventType = "payment_succeeded"
    EventDeviceLimitHit   EventType = "device_limit_hit"
    EventMarketingUpdate  EventType = "marketing_update"
)

type Notification struct {
    Event   EventType
    Email   string
    Subject string
    Body    string
}

type EventLogEntry struct {
    ID        string
    EventType EventType
    UserID    string
    Email     string
    Data      map[string]string
    Status    string    // "sent", "failed", "queued"
    Timestamp time.Time
}
```

### 2.9 Dashboard Types

```go
type RequestEvent struct {
    Timestamp       time.Time
    WorkflowID      string
    Step            int
    ComplexityScore float64
    Tier            string
    Model           string
    Latency         time.Duration
    Cost            float64
    CacheHit        bool
}

type AggregateStats struct {
    TotalRequests int64
    CacheHits     int64
    CacheHitRate  float64
    TotalCost     float64
    TotalSavings  float64
    AvgLatency    float64
    TierCounts    map[string]int64  // economy, cheap, mid, premium
}

type DashboardUpdate struct {
    Type      string         // "request", "stats", "workflow"
    Request   *RequestEvent
    Stats     *AggregateStats
    Workflows []WorkflowBudget
}
```

---

## 3. API Specification

### 3.1 Chat Completions

```
POST /v1/chat/completions
Content-Type: application/json

Request Headers (optional):
  X-Workflow-ID: <workflow-uuid>
  X-Agent-Role: <role>          # engineer, architect, tester, etc.
  X-Team: <team-name>
  Authorization: Bearer <token>  # OIDC or API key (nxs_...)
  Traceparent: <w3c-traceparent>

Request Body:
{
  "model": "string",              // Model name (routed by Nexus)
  "messages": [
    {"role": "system|user|assistant", "content": "string"}
  ],
  "max_tokens": int,              // Optional
  "temperature": float,           // Optional (0.0-2.0)
  "stream": bool                  // Optional (default: false)
}

Response (non-streaming) - HTTP 200:
{
  "id": "chatcmpl-xxx",
  "object": "chat.completion",
  "model": "actual-model-used",
  "choices": [
    {
      "index": 0,
      "message": {"role": "assistant", "content": "..."},
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": int,
    "completion_tokens": int,
    "total_tokens": int
  }
}

Response Headers:
  X-Nexus-Model: <actual-model>
  X-Nexus-Tier: <economy|cheap|mid|premium>
  X-Nexus-Provider: <provider-name>
  X-Nexus-Cost: <cost-usd>
  X-Nexus-Workflow: <workflow-id>
  X-Nexus-Cache: <L1|L2a|L2b>    # Only present on cache hits
  X-Request-ID: <uuid>
  Traceparent: <w3c-traceparent>

Response (streaming) - HTTP 200:
Content-Type: text/event-stream

  data: {"id":"chatcmpl-xxx","choices":[{"delta":{"content":"Hello"}}]}
  data: {"id":"chatcmpl-xxx","choices":[{"delta":{"content":" world"}}]}
  data: [DONE]

Error Responses:
  400: {"error": "missing required field: messages"}
  400: {"error": "prompt injection detected", "threats": [...]}
  413: {"error": "request body too large"}
  429: {"error": "rate limit exceeded"} + Retry-After: 1
  500: {"error": "internal server error"}
```

### 3.2 Health Endpoints

```
GET /health        -> 200 {"status": "ok", "providers": {...}}
GET /health/live   -> 200 {"status": "alive"}
GET /health/ready  -> 200 {"status": "ready"} | 503 {"status": "not ready"}
```

### 3.3 Service Info

```
GET /  -> 200 {"service": "nexus-gateway", "version": "1.0.0", "providers": [...], ...}
```

### 3.4 Metrics

```
GET /metrics  -> 200 (Prometheus text format)
```

### 3.5 Synonym Management

```
GET  /api/synonyms/stats       -> 200 {"base_synonyms": N, "learned_synonyms": N, ...}
GET  /api/synonyms/candidates  -> 200 [{"term": "...", "confirmations": N, ...}]
GET  /api/synonyms/learned     -> 200 {"term1": "expansion1", ...}
POST /api/synonyms/promote     -> 200 {"status": "promoted"}     Body: {"term": "k8s"}
POST /api/synonyms/add         -> 200 {"status": "added"}        Body: {"canonical": "...", "synonyms": [...]}
```

### 3.6 Circuit Breaker Status

```
GET /api/circuit-breakers  -> 200 {"provider": {"state": "closed", "failures": N, ...}}
```

### 3.7 Dashboard

```
GET /dashboard         -> 200 (HTML page)
GET /dashboard/events  -> 200 (SSE stream: text/event-stream)
GET /dashboard/stats   -> 200 {"total_requests": N, "cache_hit_rate": F, ...}
```

### 3.8 Billing and Admin

```
POST /billing/webhooks/stripe         -> 200   (Stripe webhook with HMAC verification)
POST /api/keys/generate               -> 201   {"key": "nxs_live_...", ...}
POST /api/keys/revoke                 -> 200   {"status": "revoked"}
GET  /api/keys/usage?key_hash=...     -> 200   {"monthly_usage": N, "quota": N, ...}
GET  /api/admin/subscriptions         -> 200   [{"id": "...", "plan": "...", ...}]
GET  /api/admin/keys?user_id=...      -> 200   [{"key_prefix": "...", ...}]
GET  /api/admin/devices?user_id=...   -> 200   [{"fingerprint": "...", ...}]
```

### 3.9 Workflow Feedback

```
POST /v1/feedback  -> 200 {"status": "recorded"}
  Body: {"workflow_id": "...", "step": N, "outcome": "success|failure", "details": "..."}
```

---

## 4. Configuration Schema

### 4.1 Complete YAML Reference

```yaml
# --- Server ---
server:
  port: 8080                    # HTTP listen port
  read_timeout: 30s             # Max time to read request
  write_timeout: 120s           # Max time to write response (includes LLM latency)

# --- Providers ---
providers:
  - name: "ollama-local"        # Unique provider identifier
    type: "openai"              # Protocol type: openai (covers Ollama, vLLM, etc.)
    base_url: "http://localhost:11434/v1"
    api_key: ""                 # Empty for local Ollama
    headers: {}                 # Custom headers per provider
    enabled: true
    priority: 1                 # Lower = preferred for same tier
    models:
      - name: "qwen2.5:1.5b"
        tier: "economy"         # economy | cheap | mid | premium
        cost_per_1k_tokens: 0.0001
        max_tokens: 32768

  - name: "openai"
    type: "openai"
    base_url: "https://api.openai.com/v1"
    api_key: "${OPENAI_API_KEY}"
    enabled: true
    priority: 2
    models:
      - name: "gpt-4o-mini"
        tier: "cheap"
        cost_per_1k_tokens: 0.00015
        max_tokens: 128000
      - name: "gpt-4o"
        tier: "premium"
        cost_per_1k_tokens: 0.005
        max_tokens: 128000

# --- Router ---
router:
  threshold: 0.7                # Score threshold for tier boundaries
  default_tier: "mid"           # Fallback tier
  budget_enabled: true          # Enable workflow budget enforcement
  default_budget: 1.0           # Default USD budget per workflow
  complexity_weights:           # Must sum to ~1.0
    prompt_complexity: 0.30     # Keyword analysis weight
    context_length: 0.15        # Context window utilization
    agent_role: 0.20            # Role-based weight
    step_position: 0.15         # Workflow progress
    budget_pressure: 0.20       # Remaining budget

# --- Cache ---
cache:
  enabled: true
  l1_enabled: true
  l2_enabled: true
  ttl: 1h
  max_entries: 10000
  similarity_min: 0.95
  l1:
    enabled: true
    ttl: 15m
    max_entries: 10000
  l2_bm25:
    enabled: true
    ttl: 1h
    max_entries: 50000
    threshold: 15.0             # BM25 score threshold
  l2_semantic:
    enabled: true
    ttl: 1h
    max_entries: 50000
    threshold: 0.92             # Cosine similarity threshold
    backend: "ollama"           # "ollama" or "openai"
    model: "bge-m3"             # Embedding model
    endpoint: "http://localhost:11434"
    api_key: ""
    reranker:
      enabled: false
      model: "bge-reranker-v2-m3"
      endpoint: "http://localhost:11434"
      threshold: 0.5
  feedback:
    enabled: true
    max_size: 10000
  shadow:
    enabled: false
    max_results: 1000
  synonym:
    data_dir: "./data"
    promotion_threshold: 3      # Confirmations before candidate -> learned

# --- Workflow ---
workflow:
  ttl: 1h
  max_steps: 100

# --- Telemetry ---
telemetry:
  metrics_enabled: true
  metrics_port: 9090
  log_level: "info"             # debug | info | warn | error
  log_format: "json"            # json | text

# --- Tracing ---
tracing:
  enabled: true
  service_name: "nexus-gateway"
  sample_rate: 1.0
  export_url: ""                # OTLP endpoint
  log_spans: false

# --- Security ---
security:
  body_size_limit: 1048576      # 1MB
  request_timeout: "30s"
  panic_recovery: true
  audit_log: true
  input_validation: true
  request_logging: true
  tls:
    enabled: false
    cert_file: ""
    key_file: ""
    ca_file: ""
    min_version: "1.2"
    mutual_tls: false
  prompt_guard:
    enabled: true
    mode: "block"               # "block" or "sanitize"
    max_prompt_length: 32000
    custom_patterns: []
    custom_phrases: []
  oidc:
    enabled: false
    issuer: ""
    client_id: ""
    client_secret: ""
    redirect_url: ""
    scopes: ["openid", "profile", "email"]
    allowed_domains: []
  rbac:
    enabled: false
    roles:
      admin:
        permissions: ["chat", "admin", "synonyms:read", "synonyms:write", "dashboard", "feedback"]
      user:
        permissions: ["chat", "feedback"]
        max_rpm: 60
        max_budget: 10.0
        allowed_tiers: ["economy", "cheap", "mid"]
      viewer:
        permissions: ["dashboard", "synonyms:read"]
  rate_limit:
    enabled: true
    default_rpm: 60
    burst_size: 10
  cors:
    allowed_origins: ["*"]
  ip_allowlist:
    enabled: false
    allowed_ips: ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"]
    paths: ["/api/admin/", "/api/synonyms/"]

# --- Storage ---
storage:
  vector_backend: "memory"      # "memory" or "qdrant"
  kv_backend: "memory"          # "memory" or "redis"
  qdrant_host: "localhost"
  qdrant_port: 6333
  qdrant_collection: "nexus_embeddings"
  qdrant_api_key: ""
  qdrant_dimension: 1024
  redis_addr: "localhost:6379"
  redis_password: ""
  redis_db: 0
  redis_tls: false

# --- Billing ---
billing:
  enabled: false
  data_dir: "./data/billing"
  stripe_webhook_secret: ""
  default_plan: "free"

# --- Notification ---
notification:
  enabled: false
  smtp_host: ""
  smtp_port: 587
  smtp_user: ""
  smtp_password: ""
  from_email: "noreply@nexus-gateway.com"
  from_name: "Nexus Gateway"
```

---

## 5. Algorithm Details

### 5.1 Complexity Scoring Formula

The classifier evaluates prompts on 7 independent dimensions:

**Step 1: Keyword Analysis (PromptScore)**

```
high_keywords = [analyze, debug, fix, refactor, optimize, architect, security,
                 vulnerability, concurrency, distributed, algorithm, ...]
mid_keywords  = [explain, compare, review, test, write, create, build, ...]
low_keywords  = [summarize, list, format, convert, translate, log, print, ...]

matched_count = high_matches + mid_matches + low_matches
PromptScore = (high_matches*1.0 + mid_matches*0.5 + low_matches*0.0) / matched_count
Default: 0.5 if no keywords matched
```

**Step 2: Length Analysis (LengthScore)**

```
LengthScore = min(len(prompt) / 2000.0, 1.0)
```

**Step 3: Structure Analysis (StructScore)**

```
indicators = count('?') + count(';') + count('-') + count('*')
StructScore = min(indicators / 10.0, 1.0)
```

**Step 4: Context Analysis (ContextScore)**

```
ContextScore = min(contextLen / 4096.0, 1.0)
```

**Step 5: Role Analysis (RoleScore)**

```
roleWeights = {
    "engineer": 0.85, "architect": 0.90, "security-engineer": 0.90,
    "developer": 0.80, "debugger": 0.85, "reviewer": 0.70,
    "analyst": 0.75, "tester": 0.60, "qa": 0.65, "ops": 0.55,
    "coordinator": 0.60, "assistant": 0.50, "planner": 0.65,
    "writer": 0.40, "formatter": 0.20, "summarizer": 0.25,
    "logger": 0.15, "calculator": 0.15
}
RoleScore = roleWeights[role] or 0.5
```

**Step 6: Position Analysis (PositionScore)**

```
if stepRatio < 0.3:  PositionScore = 0.7  (early = less context accumulated)
elif stepRatio > 0.8: PositionScore = 0.3  (late = more context, simpler tasks)
else:                 PositionScore = 0.5  (middle)
```

**Step 7: Budget Analysis (BudgetScore)**

```
BudgetScore = 1.0 - budgetRatio
(Higher pressure when budget is nearly exhausted)
```

**Step 8: Weighted Final Score**

```
basePrompt = PromptScore*0.6 + LengthScore*0.2 + StructScore*0.2

FinalScore = basePrompt    * w.PromptComplexity     (default: 0.30)
           + ContextScore  * w.ContextLength         (default: 0.15)
           + RoleScore     * w.AgentRole             (default: 0.20)
           + PositionScore * w.StepPosition           (default: 0.15)
           + BudgetScore   * w.BudgetPressure         (default: 0.20)
```

### 5.2 Cache Lookup Chain

**L1 (Exact Match)**

```
key = SHA256(prompt)
entry = map[key]
if entry exists AND time.Since(entry.CreatedAt) < ttl:
    entry.HitCount++
    return entry.Response, "L1"
```

**L2a (BM25 Keyword Match)**

```
Parameters: k1=1.5, b=0.75

queryTokens = tokenize(prompt)  // lowercase, stopword removal, suffix stemming
N = len(docs)
avgDL = average(len(doc.tokens) for doc in docs)

For each doc:
    if doc.model != request.model: skip
    if time.Since(doc.createdAt) > ttl: skip

    score = 0
    for term in queryTokens:
        df = documentFrequency[term]
        idf = ln((N - df + 0.5) / (df + 0.5) + 1)
        tf = doc.termFreq[term]
        dl = len(doc.tokens)
        score += idf * (tf * (k1 + 1)) / (tf + k1 * (1 - b + b * dl / avgDL))

    // Cosine normalization
    magnitude = sqrt(sum(tf^2 for tf in doc.termFreq.values()))
    if magnitude > 0: score /= magnitude

if bestScore >= threshold (default: 15.0):
    return bestDoc.Response, "L2a"
```

**L2b (Semantic Match)**

```
1. Expand query synonyms using SynonymRegistry
2. Get embedding vector from Ollama/OpenAI
3. Normalize vector to unit length (L2 norm)
4. Classify query type (factual, code, debug, comparison, etc.)
5. Calculate adaptive threshold:
   adaptiveThreshold = baseThreshold + queryTypeAdjustment
   Clamped to [0.55, 0.95]

6. For each cached entry:
   if time.Since(entry.createdAt) > ttl: skip
   if entry.model != request.model: skip

   similarity = dotProduct(queryEmbedding, entry.embedding)  // normalized = cosine

   if similarity < adaptiveThreshold: skip
   if hasOppositeIntent(query, entry.prompt): skip
   if hasDifferentKeyNoun(query, entry.prompt): skip

   if similarity < 0.85 AND reranker.enabled:
       if NOT reranker.Verify(query, entry.prompt): skip

   bestMatch = max(similarity, bestMatch)

7. Return bestMatch.Response, "L2b"
8. If near-miss (similarity > threshold-0.05 but filtered):
   registry.RecordNearMiss(query, entry.prompt, similarity)
```

### 5.3 Circuit Breaker State Machine

```
                    +---------------------+
                    |                     |
          success   |      CLOSED         |  failures < threshold
        +---------->|  (normal operation) |<-------------+
        |           |                     |              |
        |           +----------+----------+              |
        |                      |                         |
        |           failures >= threshold                |
        |                      |                         |
        |                      v                         |
        |           +---------------------+              |
        |           |                     |              |
        |           |       OPEN          |              |
        |           |  (fast-fail all)    |              |
        |           |                     |              |
        |           |  timeout doubles    |              |
        |           |  each time (exp     |              |
        |           |  backoff up to 5m)  |              |
        |           +----------+----------+              |
        |                      |                         |
        |           timeout elapsed                      |
        |                      |                         |
        |                      v                         |
        |           +---------------------+              |
        |           |                     |              |
        |           |    HALF-OPEN        |-- failure -->|
        |           |  (probe: max 1 req) |              |
        |           |                     |    (reopen   |
        |           +----------+----------+     with 2x  |
        |                      |                timeout) |
        |           successes >= 2                       |
        |                      |                         |
        +----------------------+                         |

State Transitions:
  CLOSED -> OPEN:      failures >= FailureThreshold (default: 5)
  OPEN -> HALF-OPEN:   time.Since(openedAt) >= currentTimeout
  HALF-OPEN -> CLOSED: successes >= SuccessThreshold (default: 2)
  HALF-OPEN -> OPEN:   any failure (timeout doubles, max 5m)

Exponential Backoff:
  Initial:  30s
  After 1st reopen: 60s
  After 2nd reopen: 120s
  After 3rd reopen: 240s
  Maximum: 300s (5 minutes)
```

**Retry with Backoff (per request)**

```
delay = 100ms (base)
for attempt = 0; attempt <= 3; attempt++:
    err = fn()
    if err == nil: return nil
    if attempt == 3: break

    jitter = random(0, 0.5 * delay)
    sleep(delay + jitter)
    delay = min(delay * 2, 5s)

return "after 3 retries: <last error>"
```

### 5.4 Budget Management

```
WorkflowState:
    budget = 1.0 (default, USD)
    totalCost = sum(step.Cost for step in steps)

BudgetRatio = (budget - totalCost) / budget

Router Budget Overrides:
    if budgetRatio < 0.15 AND selectedTier == "premium":
        downgrade to "mid"
    if budgetRatio < 0.05:
        force "economy"

BudgetManager.ShouldDowngrade(budgetLeft, totalBudget):
    return budgetLeft / totalBudget < 0.15
```

### 5.5 Synonym Learning Lifecycle

```
+------------+    near-miss or     +------------+   confirmations   +-----------+
|            |    feedback event   |            |   >= threshold    |           |
|  Unknown   | ------------------> | Candidate  | ----------------> |  Learned  |
|  Term      |                     | (Tier 3)   |   (default: 3)   | (Tier 2)  |
|            |                     |            |                   |           |
+------------+                     +------+-----+                   +-----------+
                                          |                               |
                                   +------+------+                        |
                                   | Staleness   |                  Persisted to:
                                   | Check:      |                  data/learned_
                                   | No confirm  |                  synonyms.json
                                   | in 7 days   |                  every 5 min
                                   | -> Remove   |
                                   +-------------+

Sources that create candidates:
  - near_miss: Semantic cache near-miss -> extract unique words -> create pair
  - feedback:  Unhelpful cache hit feedback -> counts double (2 confirmations)
  - manual:    Admin POST /api/synonyms/add -> directly added to Learned

Promotion:
  - Each near-miss of same pair: +1 confirmation
  - Each feedback report: +2 confirmations
  - When confirmations >= promotionThreshold: auto-promote to Learned

False Positive Learning:
  - When a cache hit is reported unhelpful:
  - Extract unique words between query and cached query
  - Add those words as learned key nouns
  - Future lookups will block matches with different key nouns

Base synonyms (Tier 1, read-only, 60+ entries):
  k8s->kubernetes, gc->garbage collection, ssl->tls, db->database, etc.
```

### 5.6 Cascade Routing Decision Tree (NEW)

```
Input: prompt, role, budgetRatio, contextLen

1. Run standard ClassifyComplexity()
2. Determine initial tier from FinalScore

3. CASCADE PHASE:
   if cascade.enabled AND tier in ["cheap", "economy"]:
       response_cheap = provider.Send(cheapModel, prompt)

       confidence = scoreConfidence(prompt, response_cheap):
           a. embedding_score = CosineSimilarity(
                  embed(prompt + " expected good answer"),
                  embed(response_cheap.content))
           b. heuristic_score:
              - length_ratio = len(response) / expected_length
              - contains_code = regex_check for code blocks
              - coherence = sentence_count / expected_sentences
           c. judge_score (optional):
              - Ask small judge model: "Rate this answer 1-5"
              - Normalize to 0-1

           confidence = 0.4*embedding_score + 0.4*heuristic_score + 0.2*judge_score

       if confidence >= cascade.confidence_threshold (default: 0.7):
           return response_cheap  // cheap model was sufficient
       else:
           // Escalate to next tier
           escalated_tier = nextTier(tier)  // cheap->mid, economy->cheap
           response_premium = provider.Send(premiumModel, prompt)
           record cascade_escalation metric
           return response_premium

4. Cost tracking:
   - Record both calls if escalation occurred
   - Net savings when cheap model passes = premium_cost - cheap_cost
```

### 5.7 Confidence Scoring Algorithm (NEW)

```
scoreConfidence(prompt, response) -> float64:

Component 1: Embedding Similarity (weight: 0.40)
  reference = embed(prompt + " comprehensive accurate answer")
  actual    = embed(response.content)
  score     = CosineSimilarity(reference, actual)

Component 2: Heuristic Quality (weight: 0.40)
  features:
    - response_length_ratio: len(response) / max(len(prompt) * 2, 100)
    - has_structure: contains headers/lists/code blocks
    - sentence_count: count('.') + count('!') + count('?')
    - vocabulary_diversity: unique_words / total_words
    - no_hedging: absence of "I'm not sure", "I don't know"
    - no_repetition: max_ngram_repeat < 3
  heuristic_score = weighted_average(features)

Component 3: LLM Judge (weight: 0.20, optional)
  judge_prompt = "Rate 1-5: Is this a good answer to '<prompt>'?\nAnswer: <response>"
  judge_response = cheapModel.Send(judge_prompt)
  judge_score = parse_rating(judge_response) / 5.0

Final:
  confidence = 0.40 * embedding_score
             + 0.40 * heuristic_score
             + 0.20 * judge_score  (or 0.50/0.50 if judge disabled)
```

### 5.8 Prompt Compression Pipeline (NEW)

```
compress(messages []Message) -> []Message:

Strategy 1: System Prompt Deduplication
  system_messages = filter(messages, role=="system")
  if len(system_messages) > 1:
      merged = deduplicate_instructions(system_messages)
      messages = replace_system_messages(messages, merged)
  Savings: Typically 10-30% for multi-agent frameworks

Strategy 2: Whitespace Normalization
  for each message:
      content = collapse_whitespace(content)
      content = strip_trailing_spaces(content)
      content = collapse_blank_lines(content)
  Savings: 5-15% on verbose prompts

Strategy 3: Context Window Trimming
  if len(messages) > max_context_turns (default: 20):
      messages = [system] + [first_user] + messages[-max_context_turns:]
  Savings: Prevents unbounded context growth

Strategy 4: Filler Pattern Removal
  filler_patterns = [
      "Please note that", "As I mentioned earlier",
      "Let me think about this", "To summarize what we discussed",
      "Based on our previous conversation", "As you can see",
      "In other words", "To put it simply",
      "It's worth noting that", "At the end of the day",
  ]
  for each message:
      for pattern in filler_patterns:
          content = remove(content, pattern)
  Savings: 2-8% on conversational prompts

Metrics:
  original_tokens = count_tokens(original_messages)
  compressed_tokens = count_tokens(compressed_messages)
  compression_ratio = compressed_tokens / original_tokens
  -> Recorded in nexus_compression_ratio histogram
```

---

## 6. Database/Storage Schema

### 6.1 In-Memory Stores

| Store | Key | Value | TTL | Max Size |
|-------|-----|-------|-----|----------|
| ExactCache | SHA256(prompt) | CacheEntry{Response, CreatedAt, HitCount} | 15m | 10K |
| BM25Cache | (sequential) | bm25Doc{prompt, model, tokens, termFreq, response} | 1h | 50K |
| SemanticCache | (sequential) | semanticEntry{prompt, model, embedding, response} | 1h | 50K |
| WorkflowTracker | workflowID | WorkflowState{steps, cost, budget} | 1h | unbounded |
| SubscriptionStore | subID/userID | Subscription{...} | none | unbounded |
| APIKeyStore | keyHash | APIKey{...} | none | unbounded |
| DeviceTracker | userID->devices | Device{fingerprint, counts} | 30d | unbounded |
| FeedbackStore | (sequential) | FeedbackEntry{query, cached, helpful, ...} | none | 10K |
| ShadowResults | (sequential) | ShadowResult{query, cached, fresh, ...} | none | 1K |
| EventLog | (sequential) | EventLogEntry{type, user, status, ...} | none | 10K |

### 6.2 File-Based Persistence

| File | Format | Content | Update Frequency |
|------|--------|---------|------------------|
| `data/learned_synonyms.json` | JSON | Synonyms, KeyNouns, Candidates | Every 5 minutes |
| `data/billing/*.json` | JSON | Subscription + API key state | On change |
| `data/events/*.json` | JSON | Notification event log | Periodic |

**learned_synonyms.json schema:**

```json
{
  "synonyms": {"term1": "expansion1", "term2": "expansion2"},
  "key_nouns": {"noun1": true, "noun2": true},
  "candidates": {
    "term": {
      "term": "string",
      "expansion": "string",
      "confirmations": 3,
      "source": "near_miss",
      "first_seen": "2024-01-01T00:00:00Z",
      "last_confirmed": "2024-01-02T00:00:00Z",
      "examples": ["context1", "context2"]
    }
  },
  "updated_at": "2024-01-02T00:00:00Z",
  "version": 1
}
```

### 6.3 Qdrant Schema (Enterprise)

```
Collection: nexus_embeddings
  Vectors: dimension=1024 (BGE-M3)
  Distance: Cosine
  Payload:
    - prompt: string
    - model: string
    - response: bytes
    - created_at: timestamp
```

### 6.4 Redis Schema (Enterprise)

```
Key Pattern                      | Value Type | Purpose
---------------------------------|------------|--------
nexus:cache:exact:{hash}         | String     | Cached response bytes
nexus:cache:bm25:*               | Hash       | BM25 document data
nexus:session:{id}               | Hash       | Workflow state
nexus:billing:sub:{id}           | JSON       | Subscription data
nexus:billing:key:{hash}         | JSON       | API key data
nexus:metrics:*                  | Counter    | Distributed counters
```

---

## 7. Metrics Reference

### 7.1 Counters

| Metric | Labels | Description |
|--------|--------|-------------|
| `nexus_requests_total` | method, path, status | Total HTTP requests |
| `nexus_cache_hits_total` | layer (L1, L2a, L2b) | Cache hits by layer |
| `nexus_cache_misses_total` | - | Total cache misses |
| `nexus_tokens_total` | provider, model, type | Tokens consumed |
| `nexus_cost_total_usd` | provider, model | Total cost in USD |
| `nexus_cost_saved_usd` | - | Cost saved by cache hits |
| `nexus_security_blocks_total` | reason | Blocked requests |
| `nexus_synonyms_learned_total` | - | Synonym promotions |
| `nexus_circuit_breaker_opens_total` | provider | Circuit breaker activations |

### 7.2 Histograms

| Metric | Buckets | Description |
|--------|---------|-------------|
| `nexus_request_duration_seconds` | 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30 | End-to-end latency |
| `nexus_cache_lookup_seconds` | 0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1 | Cache lookup time |
| `nexus_embedding_duration_seconds` | 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2 | Embedding generation |
| `nexus_compression_ratio` | 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 1.0 | NEW: Compression ratio |

### 7.3 Gauges

| Metric | Description |
|--------|-------------|
| `nexus_cache_entries` | Current cached entries (all layers) |
| `nexus_active_requests` | Currently in-flight requests |
| `nexus_circuit_breaker_state` | Per provider (0=closed, 1=open, 2=half-open) |

### 7.4 Exposition Format Example

```
# HELP nexus_requests_total Total number of requests processed
# TYPE nexus_requests_total counter
nexus_requests_total{method="POST",path="/v1/chat/completions",status="200"} 1547

# HELP nexus_request_duration_seconds Request duration histogram
# TYPE nexus_request_duration_seconds histogram
nexus_request_duration_seconds_bucket{le="0.01"} 89
nexus_request_duration_seconds_bucket{le="0.05"} 234
nexus_request_duration_seconds_bucket{le="+Inf"} 1547
nexus_request_duration_seconds_sum 423.7
nexus_request_duration_seconds_count 1547

# HELP nexus_cache_entries Current cache size
# TYPE nexus_cache_entries gauge
nexus_cache_entries 4523
```

---

## 8. Error Handling

### 8.1 Error Codes

| HTTP Status | Error | Cause | Action |
|-------------|-------|-------|--------|
| 400 | missing required field: messages | Invalid request body | Fix request |
| 400 | invalid JSON in request body | Malformed JSON | Fix request |
| 400 | messages must be an array | Wrong type | Fix request |
| 400 | prompt injection detected | Prompt guard triggered | Modify prompt |
| 401 | missing authorization header | OIDC enabled, no token | Add Bearer token |
| 401 | authentication failed | Invalid/expired token | Refresh token |
| 403 | forbidden | IP not in allowlist | Add IP to allowlist |
| 403 | insufficient permissions | RBAC check failed | Request role upgrade |
| 413 | request body too large | Body exceeds limit | Reduce payload |
| 429 | rate limit exceeded | Token bucket empty | Wait and retry |
| 500 | internal server error | Panic recovered | Check server logs |
| 502 | Provider error | Upstream LLM failure | Retry (automatic) |

### 8.2 Retry Logic

```
Non-streaming requests:
  RetryWithBackoff(RetryConfig{
      MaxRetries:  3,
      BaseDelay:   100ms,
      MaxDelay:    5s,
      JitterRatio: 0.5
  })

  Delays: ~100ms -> ~200ms -> ~400ms (with +/-50% jitter)
```

### 8.3 Fallback Behavior

1. **Primary provider circuit open?**
   - `findFallbackProvider()`: iterate all providers
   - Find one with same-tier or higher-tier model
   - If found: use fallback. If not: return error to client

2. **Tier unavailable (no models configured)?**
   - `selectModelWithFallback()`: try tiers in upgrade order
   - economy -> cheap -> mid -> premium
   - Ultimate fallback: first available model from any provider

3. **Embedding service unavailable?**
   - Semantic cache returns miss (not error)
   - Reranker falls back to heuristic scoring (Jaccard + overlap)
   - Request proceeds normally via provider

4. **Stripe webhook verification fails?**
   - Return 400 (invalid signature), log warning

5. **OIDC discovery fails?**
   - Log warning, continue with manual endpoint config
   - If no endpoints configured: OIDC middleware passes through

---

## 9. Concurrency Model

### 9.1 Goroutines

| Component | Goroutines | Lifecycle | Purpose |
|-----------|-----------|-----------|---------|
| HTTP Server | Per-request | Request duration | Handle each HTTP request |
| Health Checker | 1 long-lived | Server lifetime | Background health probes (30s) |
| Cache Cleanup (x3) | 3 long-lived | Server lifetime | TTL eviction (1m per layer) |
| Synonym Registry | 1 long-lived | Server lifetime | Save (5m) + clean stale (1h) |
| Span Exporter | 1 long-lived | Server lifetime | Batch export (100 spans or 5s) |
| Notification Worker | 1 long-lived | Server lifetime | Process email queue (500 slots) |
| Workflow Cleanup | 1 long-lived | Server lifetime | Remove expired workflows (5m) |
| Auto-Detector Cleanup | 1 long-lived | Server lifetime | Remove expired sessions |
| Subscription Checker | 1 long-lived | Server lifetime | Lifecycle checking (1h) |
| CB Callbacks | Per-event | Fire and forget | go OnStateChange(...) |

### 9.2 Synchronization Primitives

| Resource | Lock Type | Contention Pattern |
|----------|-----------|-------------------|
| ExactCache.entries | sync.RWMutex | Read-heavy (many lookups per write) |
| BM25Cache.docs | sync.RWMutex | Read-heavy |
| SemanticCache.entries | sync.RWMutex | Read-heavy |
| SynonymRegistry | sync.RWMutex | Read-heavy (expand on every request) |
| CircuitBreaker | sync.Mutex | Low contention (per-provider) |
| ProviderPool.breakers | sync.RWMutex | Read-heavy |
| WorkflowTracker.workflows | sync.RWMutex | Read-heavy |
| HealthChecker.status | sync.RWMutex | Read-heavy |
| FeedbackStore | sync.Mutex | Low contention |
| ShadowMode.results | sync.Mutex | Low contention |
| Metrics counters | atomic.Int64 | Lock-free (no contention) |
| Metrics gauges | atomic.Int64 | Lock-free |
| Metrics cost (float64) | atomic CAS loop | Lock-free |
| Histogram buckets | atomic.Int64 per bucket | Lock-free |

### 9.3 Atomic Operations

```go
// Lock-free float64 addition using CAS (Compare-And-Swap)
func atomicAddFloat64(addr *uint64, delta float64) {
    for {
        old := atomic.LoadUint64(addr)
        oldFloat := math.Float64frombits(old)
        newFloat := oldFloat + delta
        newBits := math.Float64bits(newFloat)
        if atomic.CompareAndSwapUint64(addr, old, newBits) {
            return
        }
    }
}

// Used for: costTotal, costSaved metrics
// Avoids mutex overhead on hot path (every request)
```

### 9.4 Channel Usage

| Channel | Buffer Size | Purpose |
|---------|-------------|---------|
| SynonymRegistry.stopCh | 0 (signal) | Graceful shutdown signal |
| SynonymRegistry.doneCh | 0 (signal) | Shutdown completion ack |
| SpanExporter.spanCh | 1024 | Span export pipeline buffer |
| Notifier.queue | 500 | Email notification queue |
| EventBus.clients | Per-client | SSE client channels |

---

## 10. Testing Strategy

### 10.1 Unit Test Coverage

| Package | Test File | Tests | Focus Areas |
|---------|-----------|-------|-------------|
| cache | semantic_test.go | 2,072 lines, 100+ cases | BM25 accuracy, cosine math, filters, query types, reranker, context fingerprint, feedback, shadow mode, synonym registry, false positive prevention (1,250+ cases) |
| provider | circuitbreaker_test.go | ~300 lines, 15+ cases | State transitions, exp backoff, timeout cap, callbacks, pool management, retry, concurrency |
| security | security_test.go | ~200 lines, 12+ cases | Middleware chain, headers, rate limiting, prompt injection, input validation |
| telemetry | metrics_test.go | ~200 lines, 16+ cases | Counter recording, histogram buckets, gauges, concurrent access, Prometheus format |
| telemetry | tracing_test.go | ~300 lines, 21+ cases | Trace/span IDs, traceparent parsing, parent-child spans, sampling, OTLP payload |
| billing | billing_test.go | ~400 lines, 30+ cases | API key lifecycle, quota enforcement, device tracking, subscriptions, Stripe webhooks |
| notification | notification_test.go | ~200 lines, 11+ cases | Event log, template rendering, queuing, bulk sends, persistence |
| storage | storage_test.go | ~300 lines, 15+ cases | Memory stores, Qdrant client, Redis client, factory functions |

### 10.2 E2E Test Matrix

The E2E suite (`tests/e2e/main.go`, 1,234 lines) runs 32 tests across 8 layers:

| Layer | Tests | What Is Verified |
|-------|-------|-----------------|
| 1. Infrastructure | 4 | /health, /health/live, /health/ready, /metrics, / info |
| 2. Security | 5 | Security headers (HSTS, CSP), request ID, body size limit (413), input validation, prompt injection (400) |
| 3. Core Inference | 3 | Non-streaming completion, streaming SSE, workflow headers |
| 4. Caching | 6 | L1 exact hit, BM25 keyword hit, semantic hit (prime + verify) |
| 5. Routing | 3 | Simple prompt -> economy, complex prompt -> premium, CB status API |
| 6. Observability | 3 | W3C traceparent, metrics counters, dashboard SSE |
| 7. Synonym/Admin | 2 | Synonym stats API, synonym add+read roundtrip |
| 8. Resilience | 4 | 5 concurrent requests, cache latency P99<50ms, schema validation, Nexus headers |

**E2E Prerequisites**: Ollama running locally, `qwen2.5:1.5b` pulled, optionally `bge-m3` for semantic tests.

### 10.3 Test Execution

```bash
# Unit tests
go test ./internal/...

# E2E tests (requires Ollama)
go run ./tests/e2e/main.go

# Benchmarks
go test -bench=. ./benchmarks/...
```

### 10.4 Test Quality Gates

| Metric | Target |
|--------|--------|
| Cache false positive rate | < 5% (verified by 1,250+ test cases) |
| Cache true positive preservation | > 95% (verified by 20+ paraphrase tests) |
| BM25 accuracy | > 70% (10 match + 10 non-match pairs) |
| Filter accuracy | > 95% (100+ opposite intent + key noun cases) |
| E2E pass rate | 32/32 (all tests must pass) |
| Cache hit latency P99 | < 50ms |

---

## Appendix A: Opposite Intent Pairs (70+ pairs)

Used for false-positive cache prevention:

```
create <-> kill/delete/remove/destroy/drop
add <-> remove/delete/subtract
enable <-> disable
start <-> stop/halt/kill/terminate/shutdown
install <-> uninstall
encrypt <-> decrypt
read <-> write
deploy <-> rollback
ascending <-> descending
login <-> logout
connect <-> disconnect
open <-> close
increase <-> decrease
allow <-> deny/block/reject
accept <-> reject
push <-> pull
upload <-> download
import <-> export
insert <-> delete
grant <-> revoke
lock <-> unlock
mount <-> unmount
subscribe <-> unsubscribe
compress <-> decompress
encode <-> decode
serialize <-> deserialize
marshal <-> unmarshal
attach <-> detach
register <-> unregister
activate <-> deactivate
show <-> hide
expand <-> collapse
increment <-> decrement
send <-> receive
merge <-> split
sum <-> product
even <-> odd
load <-> unload
raise <-> lower
before <-> after
above <-> below
horizontal <-> vertical
input <-> output
ingress <-> egress
inbound <-> outbound
public <-> private
internal <-> external
head <-> tail
first <-> last
begin <-> end
enqueue <-> dequeue
cache <-> evict
scale up <-> scale down
whitelist <-> blacklist
approve <-> reject
join <-> leave
bind <-> unbind
wrap <-> unwrap
obfuscate <-> deobfuscate
minify <-> prettify
promote <-> demote
upgrade <-> downgrade
backup <-> restore
freeze <-> unfreeze
mute <-> unmute
pin <-> unpin
follow <-> unfollow
archive <-> unarchive
pause <-> resume
ban <-> unban
block <-> unblock
sync <-> async
prefix <-> suffix
prepend <-> append
```

---

## Appendix B: Key Noun Categories (250+ Terms)

Categories used for cross-domain false positive prevention:

- **Languages**: Go, Rust, Python, Java, JavaScript, TypeScript, Ruby, Scala, Kotlin, Swift, C++, C#, PHP, Elixir, Haskell
- **Frontend**: React, Vue, Angular, Svelte, Next.js, Nuxt, Remix, Astro, Solid
- **Databases**: MySQL, PostgreSQL, MongoDB, Redis, Cassandra, DynamoDB, SQLite, MariaDB, CockroachDB, Neo4j
- **Cloud**: AWS, Azure, GCP, Lambda, Fargate, EC2, S3, Aurora, BigQuery, Redshift, Snowflake
- **Containers**: Docker, Kubernetes, Podman, Vagrant, Nomad
- **Testing**: Jest, Pytest, Mocha, RSpec, JUnit, Vitest, Cypress, Playwright
- **API Styles**: REST, GraphQL, gRPC, SOAP, tRPC
- **Message Queues**: Kafka, RabbitMQ, SQS, NATS, Pulsar
- **Search**: Elasticsearch, Solr, OpenSearch, Algolia
- **IaC**: Terraform, Pulumi, Ansible, Chef, Puppet
- **Monitoring**: Prometheus, Grafana, Datadog, NewRelic, Jaeger, Zipkin
- **CI/CD**: Jenkins, CircleCI, TravisCI, ArgoCD, Spinnaker, Tekton
- **ML Frameworks**: TensorFlow, PyTorch, Keras, Scikit, Pandas, NumPy, HuggingFace, LangChain
- **Design Patterns**: Singleton, Factory, Observer, Strategy, Decorator, Adapter, Proxy
- **Data Structures**: LinkedList, Trie, B-tree, AVL, RedBlack, SkipList, Heap, Graph
- **Algorithms**: Dijkstra, BFS, DFS, A*, BinarySearch, Fibonacci, BellmanFord
- **Auth**: JWT, Session, OAuth, SAML, OIDC, Cookies
- **Protocols**: TCP, UDP, HTTP, HTTPS, FTP, SMTP
- **Package Managers**: npm, yarn, pnpm, pip, cargo, maven, gradle, bundler, composer
- **Service Mesh**: Istio, Envoy, Linkerd, Consul
- **Editors**: VSCode, Vim, Neovim, Emacs, IntelliJ
- **CSS**: Tailwind, Bootstrap, Bulma, MaterialUI
- **Runtime**: Node, Deno, Bun, JVM, .NET

*(And 100+ more across 30+ categories)*

---

## Appendix C: Base Synonym Expansions (60+ Entries)

```
k8s          -> kubernetes
gc           -> garbage collection
ci/cd        -> continuous integration/deployment
ssl          -> tls/https/encryption
db           -> database
postgres     -> postgresql
js           -> javascript
ts           -> typescript
py           -> python
goroutine    -> go concurrency lightweight thread
dockerfile   -> docker container image build
jwt          -> json web token authentication
cors         -> cross origin resource sharing
vpc          -> virtual private cloud network
iam          -> identity access management permissions
s3           -> simple storage service
ec2          -> elastic compute cloud
rds          -> relational database service
rbac         -> role based access control
cqrs         -> command query responsibility segregation
ddd          -> domain driven design
tdd          -> test driven development
saas         -> software as a service
paas         -> platform as a service
iaas         -> infrastructure as a service
faas         -> function as a service
etl          -> extract transform load
dag          -> directed acyclic graph
ml           -> machine learning
nlp          -> natural language processing
llm          -> large language model
rag          -> retrieval augmented generation
gpu          -> graphics processing unit
cpu          -> central processing unit
oop          -> object oriented programming
fp           -> functional programming
spa          -> single page application
ssr          -> server side rendering
wasm         -> webassembly
```

---

## Appendix D: Query Type Thresholds

| Query Type | Adjustment | Rationale |
|-----------|------------|-----------|
| General | +0.00 | Default threshold |
| Factual | +0.10 | Facts need exact matches |
| HowTo | +0.00 | Procedures are paraphraseable |
| Code | +0.05 | Code queries need moderate precision |
| Debug | -0.05 | Debug queries benefit from looser matching |
| Comparison | +0.15 | "X vs Y" must match exact pair |
| Architecture | +0.02 | Slight boost for design specificity |

Final threshold clamped to [0.55, 0.95] range.

---

## Appendix E: Billing Plan Matrix

| Plan | Monthly Requests | RPM | Devices | Price |
|------|-----------------|-----|---------|-------|
| Free | 1,000 | 10 | 1 | $0 |
| Starter | 50,000 | 60 | 3 | $29/mo |
| Team | 500,000 | 300 | 10 | $99/mo |
| Enterprise | Unlimited | Unlimited | Unlimited | Custom |

---

## Appendix F: Deployment Configurations

### Docker Compose (Basic - 3 services)

```
nexus:      Port 8080 (gateway), 9090 (metrics)
prometheus: Port 9090 (scrapes nexus:9090/metrics)
grafana:    Port 3000 (dashboards)
```

### Docker Compose (Enterprise - 7 services)

```
nexus:        Port 8080, 9090 - TLS enabled, Qdrant+Redis backends
qdrant:       Port 6333, 6334 - Vector database for semantic cache
redis:        Port 6379 - KV store for shared state
ollama:       Port 11434 - Local LLM inference + embeddings
prometheus:   Port 9090 - Metrics aggregation
grafana:      Port 3000 - Visualization dashboards
alertmanager: Port 9093 - Alert routing
```

---

*Document generated from source code analysis of the Nexus repository.*
*Total codebase: ~15,000 lines of Go across 40+ files.*
*This document covers all existing functionality plus 3 planned features (Cascade Routing, Eval Pipeline, Prompt Compression).*

---

**End of Document**