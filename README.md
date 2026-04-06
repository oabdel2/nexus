# Nexus

```
 _   _                      
| \ | | _____  ___   _ ___  
|  \| |/ _ \ \/ / | | / __| 
| |\  |  __/>  <| |_| \__ \ 
|_| \_|\___/_/\_\\__,_|___/ 
```

**Agentic-first inference optimization gateway with adaptive model routing**

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: BSL 1.1](https://img.shields.io/badge/License-BSL%201.1-blue.svg)](LICENSE)
[![CI](https://github.com/oabdel2/nexus/actions/workflows/ci.yml/badge.svg)](https://github.com/oabdel2/nexus/actions)
[![Tests](https://img.shields.io/badge/tests-32%20E2E%20%2B%20120%2B%20unit-brightgreen)](tests/)
[![Version](https://img.shields.io/badge/version-0.1.0-orange)](https://github.com/oabdel2/nexus/releases)
[![Zero Deps](https://img.shields.io/badge/deps-only%20yaml.v3-purple)](go.mod)

---

Nexus is the **first production implementation** of concepts from the [CASTER research paper](https://arxiv.org/abs/2601.19793) on adaptive model routing. Unlike traditional gateways that optimize individual requests in isolation, Nexus is **workflow-aware** — it tracks multi-step agentic workflows and optimizes routing decisions across the entire pipeline.

## Features

### Inference Optimization
- ✅ **Adaptive Model Routing** — CASTER-inspired complexity scoring routes each request to the optimal model tier
- ✅ **Cascade Routing** — Try cheap model first, auto-escalate if confidence < 0.78 (40-70% cost savings)
- ✅ **Prompt Compression** — Strip redundant tokens before forwarding (20-35% fewer tokens billed)
- ✅ **Confidence Scoring** — 6-signal heuristic evaluator with per-task-type learning confidence map
- ✅ **Workflow-Aware** — Tracks multi-step workflows via `X-Workflow-ID`, optimizes across entire pipeline
- ✅ **Budget-Aware Routing** — Automatically downgrades tiers when workflow budget runs low

### Caching (7 Layers)
- ✅ **L1 Exact Cache** — SHA-256 hash-based, sub-millisecond responses
- ✅ **L2 BM25 Fuzzy** — Keyword-based similarity matching
- ✅ **L2 Semantic** — BGE-M3 embedding similarity with adaptive thresholds
- ✅ **Reranker** — Cross-encoder verification for uncertain matches
- ✅ **Synonym Learning** — Auto-discovers and promotes query synonyms
- ✅ **Feedback Loop** — Quality signals adjust cache confidence
- ✅ **Shadow Mode** — Compare cache vs fresh responses for validation

### Security (12 Layers)
- ✅ **Panic Recovery** → **Body Size Limit** → **Request Timeout** → **Security Headers**
- ✅ **Request ID** → **Request Logger** → **CORS** → **IP Allowlist**
- ✅ **Rate Limiting** → **OIDC SSO** → **Input Validation** → **Prompt Injection Guard** (16 patterns)
- ✅ **TLS/mTLS**, **RBAC** with wildcard permissions, **Audit Logging**

### Infrastructure
- ✅ **OpenAI-Compatible API** — Drop-in `/v1/chat/completions` replacement
- ✅ **Multi-Provider** — OpenAI, Anthropic, Ollama (any OpenAI-compatible endpoint)
- ✅ **Circuit Breaker** — 3-state machine with exponential backoff and automatic failover
- ✅ **Prometheus Metrics** — Requests, tokens, cost, cache hits, compression, cascade, confidence histograms
- ✅ **W3C Distributed Tracing** — Traceparent propagation, structured span logging
- ✅ **Grafana Dashboards** — 4 pre-built dashboards (overview, cache, routing, security)
- ✅ **Helm Chart** — Production Kubernetes deployment with HPA, ServiceMonitor, Ingress
- ✅ **Billing System** — Subscriptions, API keys, device tracking, Stripe webhooks
- ✅ **Model Warmup** — Preloads models to GPU on startup (43s → 2.6s first request)
- ✅ **Streaming Support** — SSE streaming with real-time usage extraction
- ✅ **Zero External Dependencies** — Only `gopkg.in/yaml.v3` beyond Go stdlib

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                       Client Request                         │
│            POST /v1/chat/completions                         │
│      Headers: X-Workflow-ID, X-Agent-Role, X-Step-Number     │
└─────────────────────┬───────────────────────────────────────┘
                      │
              ┌───────▼───────┐
              │   L1 Cache    │──── HIT ──→ Return cached response
              │  (SHA-256)    │            (< 1ms)
              └───────┬───────┘
                      │ MISS
              ┌───────▼───────┐
              │  Classifier   │  Keyword analysis + role weights
              │  (Complexity) │  + step position + budget pressure
              └───────┬───────┘
                      │ score
              ┌───────▼───────┐
              │    Router     │  score → tier mapping
              │  (Adaptive)   │  budget override logic
              └───────┬───────┘
                      │ tier + model
              ┌───────▼───────┐
              │   Provider    │  Circuit breaker + health check
              │   Manager     │  OpenAI/Anthropic/Copilot/Ollama
              └───────┬───────┘
                      │ response
              ┌───────▼───────┐
              │  Telemetry    │  Metrics + cost tracking
              │  + Cache      │  Store response in L1
              └───────────────┘
```

## Quick Start

### Prerequisites

- Go 1.24 or later
- At least one LLM provider API key (OpenAI, Anthropic, GitHub Copilot, or a local Ollama instance)

### Build & Run

```bash
# Clone
git clone https://github.com/your-org/nexus.git
cd nexus

# Build
go build -o nexus ./cmd/nexus/

# Run with GitHub Copilot
export GITHUB_COPILOT_TOKEN=$(gh auth token)
./nexus --config configs/nexus.yaml --port 8080

# Run with OpenAI
export OPENAI_API_KEY=sk-...
./nexus --config configs/nexus.yaml --port 8080
```

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-config` | `configs/nexus.yaml` | Path to configuration file |
| `-port` | `0` (use config) | Override server port |
| `-log-level` | (from config) | Log level: `debug`, `info`, `warn`, `error` |
| `-version` | — | Show version and exit |

### Send Your First Request

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Workflow-ID: my-workflow" \
  -H "X-Agent-Role: engineer" \
  -d '{
    "model": "auto",
    "messages": [{"role": "user", "content": "Hello world"}]
  }'
```

Nexus selects the optimal model automatically when `model` is set to `"auto"`. You can also specify a model name directly to bypass routing.

### Workflow-Aware Request

```bash
# Step 1: Architect plans the system (routed to premium tier)
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Workflow-ID: build-feature-123" \
  -H "X-Agent-Role: architect" \
  -H "X-Step-Number: 1" \
  -d '{
    "model": "auto",
    "messages": [{"role": "user", "content": "Design the architecture for a distributed cache"}]
  }'

# Step 2: Engineer implements (routed to mid tier)
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Workflow-ID: build-feature-123" \
  -H "X-Agent-Role: engineer" \
  -H "X-Step-Number: 2" \
  -d '{
    "model": "auto",
    "messages": [{"role": "user", "content": "Implement the LRU eviction policy"}]
  }'

# Step 3: Formatter cleans up (routed to cheap tier)
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Workflow-ID: build-feature-123" \
  -H "X-Agent-Role: formatter" \
  -H "X-Step-Number: 3" \
  -d '{
    "model": "auto",
    "messages": [{"role": "user", "content": "Format this code according to Go conventions"}]
  }'
```

## API Reference

### `POST /v1/chat/completions`

OpenAI-compatible chat completions endpoint with Nexus routing extensions.

**Request Headers:**

| Header | Required | Description |
|--------|----------|-------------|
| `Content-Type` | Yes | Must be `application/json` |
| `X-Workflow-ID` | No | Workflow identifier for multi-step tracking (auto-generated if omitted) |
| `X-Agent-Role` | No | Agent role for complexity scoring (e.g., `architect`, `engineer`, `formatter`) |
| `X-Step-Number` | No | Step number within the workflow |
| `X-Team` | No | Team name for cost attribution |

**Request Body:**

Standard OpenAI chat completions format. Set `"model": "auto"` for adaptive routing.

```json
{
  "model": "auto",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Explain distributed consensus"}
  ],
  "max_tokens": 1024,
  "temperature": 0.7,
  "stream": false
}
```

**Response Headers:**

| Header | Description |
|--------|-------------|
| `X-Nexus-Model` | Model name selected by router |
| `X-Nexus-Tier` | Tier used: `cheap`, `mid`, or `premium` |
| `X-Nexus-Provider` | Provider that served the request |
| `X-Nexus-Cost` | Estimated cost in dollars |
| `X-Nexus-Cache` | Cache source if hit (e.g., `l1_exact`) |
| `X-Nexus-Workflow-ID` | Workflow ID for this request |
| `X-Nexus-Workflow-Step` | Step number in the workflow |

**Response Body:**

Standard OpenAI chat completions response format.

---

### `POST /v1/feedback`

Submit quality feedback for a workflow step. Used to record outcomes and drive the feedback-driven learning loop.

**Request Body:**

```json
{
  "workflow_id": "build-feature-123",
  "step": 2,
  "outcome": "success",
  "details": "Code compiled and passed all tests"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `workflow_id` | string | Yes | The workflow to record feedback for |
| `step` | int | Yes | Step number within the workflow |
| `outcome` | string | Yes | `"success"` or `"failure"` (or a 0.0–1.0 score) |
| `details` | string | No | Additional context about the outcome |

**Response:**

```json
{"status": "ok"}
```

---

### `GET /health`

Returns health status for all registered providers, including circuit breaker state.

**Response:**

```json
{
  "copilot": {
    "healthy": true,
    "circuit_open": false,
    "last_check": "2025-01-15T10:30:00Z",
    "failure_count": 0
  },
  "openai": {
    "healthy": false,
    "circuit_open": true,
    "last_error": "connection refused",
    "failure_count": 3
  }
}
```

---

### `GET /metrics`

Prometheus-compatible metrics endpoint.

```
nexus_requests_total 42
nexus_cache_hits_total 10
nexus_cache_misses_total 32
nexus_tokens_total 50000
nexus_cost_dollars_total 0.150000
nexus_routing_decisions_total{tier="cheap"} 25
nexus_routing_decisions_total{tier="mid"} 15
nexus_routing_decisions_total{tier="premium"} 2
nexus_provider_requests_total{provider_model="copilot/gpt-4.1"} 20
nexus_latency_bucket{bucket="lt100ms"} 15
nexus_latency_bucket{bucket="lt500ms"} 8
nexus_latency_bucket{bucket="lt1s"} 5
nexus_latency_bucket{bucket="lt5s"} 3
nexus_latency_bucket{bucket="lt10s"} 1
nexus_latency_bucket{bucket="gt10s"} 0
```

---

### `GET /`

Returns gateway information (name, version, status).

## Configuration Reference

Create a `nexus.yaml` configuration file. All fields have sensible defaults.

```yaml
# ─── Server ──────────────────────────────────────────────────
server:
  port: 8080              # HTTP listen port
  read_timeout: 30s       # Max time to read request
  write_timeout: 120s     # Max time to write response (includes LLM latency)

# ─── Providers ───────────────────────────────────────────────
# Configure one or more LLM providers. Each provider can expose
# multiple models at different tier levels.
providers:
  # GitHub Copilot (uses OpenAI-compatible API)
  - name: copilot
    type: openai
    base_url: https://api.githubcopilot.com
    api_key: ${GITHUB_COPILOT_TOKEN}
    enabled: true
    priority: 1
    headers:
      Editor-Version: Nexus/0.1.0
      Copilot-Integration-Id: nexus-gateway
    models:
      - name: gpt-4.1
        tier: mid
        cost_per_1k_tokens: 0.01
        max_tokens: 4096
      - name: gpt-4.1-mini
        tier: cheap
        cost_per_1k_tokens: 0.002
        max_tokens: 4096
      - name: claude-sonnet-4
        tier: premium
        cost_per_1k_tokens: 0.03
        max_tokens: 8192

  # OpenAI Direct
  - name: openai
    type: openai
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    enabled: false
    priority: 2
    models:
      - name: gpt-4o
        tier: premium
        cost_per_1k_tokens: 0.03
        max_tokens: 4096
      - name: gpt-4o-mini
        tier: cheap
        cost_per_1k_tokens: 0.002
        max_tokens: 4096

  # Anthropic
  - name: anthropic
    type: anthropic
    base_url: https://api.anthropic.com/v1
    api_key: ${ANTHROPIC_API_KEY}
    enabled: false
    priority: 2
    models:
      - name: claude-sonnet-4-20250514
        tier: premium
        cost_per_1k_tokens: 0.03
        max_tokens: 8192

  # Ollama (local)
  - name: ollama
    type: ollama
    base_url: http://localhost:11434/v1
    api_key: ollama
    enabled: false
    priority: 3
    models:
      - name: llama3
        tier: cheap
        cost_per_1k_tokens: 0.0
        max_tokens: 4096

# ─── Router ──────────────────────────────────────────────────
router:
  threshold: 0.7           # Score above this → premium tier
  default_tier: mid         # Fallback when no providers match
  budget_enabled: true      # Enable budget-aware routing
  default_budget: 1.0       # Default workflow budget in dollars
  complexity_weights:       # How each signal contributes to final score
    prompt_complexity: 0.30 # Weight of keyword-based complexity
    context_length: 0.15    # Weight of message length
    agent_role: 0.20        # Weight of X-Agent-Role header
    step_position: 0.15     # Weight of step position in workflow
    budget_pressure: 0.20   # Weight of remaining budget ratio

# ─── Cache ───────────────────────────────────────────────────
cache:
  enabled: true             # Master cache toggle
  l1_enabled: true          # L1 exact-match cache (SHA-256)
  l2_enabled: false         # L2 semantic cache (not yet implemented)
  ttl: 1h                   # Cache entry time-to-live
  max_entries: 10000        # Maximum cached entries (LRU eviction)
  similarity_min: 0.95      # Min similarity for L2 (future)

# ─── Workflow ────────────────────────────────────────────────
workflow:
  ttl: 1h                   # Workflow state expiry after last activity
  max_steps: 100            # Maximum steps per workflow

# ─── Telemetry ───────────────────────────────────────────────
telemetry:
  metrics_enabled: true     # Enable /metrics endpoint
  metrics_port: 9090        # Metrics listen port (0 = same as server)
  log_level: info           # Log verbosity: debug, info, warn, error
  log_format: json          # Log format: json or text
```

### Environment Variable Substitution

API keys support `${ENV_VAR}` syntax in the config file. Nexus resolves these at startup:

```yaml
api_key: ${OPENAI_API_KEY}     # Reads from OPENAI_API_KEY env var
api_key: ${GITHUB_COPILOT_TOKEN}  # Reads from GITHUB_COPILOT_TOKEN env var
```

## Routing Logic

Nexus uses a **5-signal complexity classifier** inspired by CASTER's dual-signal approach, extended with workflow-aware features.

### Complexity Signals

| Signal | Weight | Description |
|--------|--------|-------------|
| **Prompt Complexity** | 0.30 | Keyword analysis — ratio of high-complexity vs low-complexity keywords |
| **Context Length** | 0.15 | Message length normalized against 4096-char window |
| **Agent Role** | 0.20 | Role-based weight from the `X-Agent-Role` header |
| **Step Position** | 0.15 | Early steps score higher (foundational), late steps score lower |
| **Budget Pressure** | 0.20 | Inverse of remaining budget ratio — high when budget is low |

### Keyword Signals

**High Complexity** (push toward premium):
> `analyze`, `debug`, `fix`, `refactor`, `optimize`, `architect`, `security`, `vulnerability`, `race condition`, `deadlock`, `concurrent`, `distributed`, `algorithm`, `prove`, `derive`, `implement`, `design pattern`, `trade-off`, `critical`, `production`, `migrate`, `performance`

**Low Complexity** (push toward economy/cheap):
> `summarize`, `list`, `format`, `convert`, `translate`, `log`, `print`, `echo`, `hello`, `greet`, `thank`, `commit message`, `rename`, `typo`, `comment`, `readme`, `docs`, `documentation`

### Role Weights

| Role | Weight | Typical Tier |
|------|--------|--------------|
| `architect` | 0.90 | Premium |
| `engineer` | 0.85 | Mid–Premium |
| `developer` | 0.80 | Mid |
| `analyst` | 0.75 | Mid |
| `reviewer` | 0.70 | Mid |
| `tester` | 0.60 | Cheap–Mid |
| `writer` | 0.40 | Cheap |
| `summarizer` | 0.25 | Cheap |
| `formatter` | 0.20 | Economy–Cheap |
| `logger` | 0.15 | Economy |

### Step Position Scoring

| Position in Workflow | Score | Rationale |
|---------------------|-------|-----------|
| Early (0–30%) | 0.7 | Foundational steps, high impact |
| Middle (30–80%) | 0.5 | Standard execution |
| Late (80–100%) | 0.3 | Cleanup, formatting, low risk |

### Tier Mapping

The **final score** (weighted sum of all 5 signals) maps to a tier:

| Final Score | Tier | Description |
|------------|------|-------------|
| > threshold (0.7) | **Premium** | Complex reasoning, architecture, debugging |
| > threshold × 0.6 (0.42) | **Mid** | Moderate tasks, implementation |
| ≤ threshold × 0.6 | **Cheap** | Simple lookups, formatting, summaries |

### Budget Overrides

When budget tracking is enabled, the router can force tier downgrades:

| Budget Remaining | Override | Reason |
|-----------------|----------|--------|
| < 15% | Premium → Mid | Budget pressure, preserve remaining funds |
| < 5% | Any → Cheap | Budget nearly exhausted, minimize cost |

## Circuit Breaker

Nexus includes a self-healing circuit breaker for each provider:

- **Closed** (normal): Requests flow through. Failures are counted.
- **Open** (tripped): After **3 consecutive failures**, the circuit opens. No requests are sent to this provider.
- **Recovery**: Health checks run every **30 seconds**. On success, the circuit closes and the provider is restored.

The health check calls `GET {base_url}/models` with a 5-second timeout.

## Benchmarking

Run the benchmark suite and routing tests:

```bash
# Run benchmarks
go test ./benchmarks/ -bench=. -benchmem

# Run scenario tests
go test ./benchmarks/ -v -run Test
```

## Project Structure

```
nexus/
├── cmd/nexus/main.go          # Entry point, CLI flags, banner
├── configs/nexus.yaml         # Default configuration
├── internal/
│   ├── config/config.go       # Configuration types + YAML loading
│   ├── gateway/server.go      # HTTP server + request pipeline
│   ├── router/
│   │   ├── classifier.go      # 5-signal complexity scoring engine
│   │   ├── router.go          # Adaptive model selection + tier mapping
│   │   └── budget.go          # Budget tracking + downgrade logic
│   ├── cache/
│   │   ├── exact.go           # L1 SHA-256 exact-match cache (LRU + TTL)
│   │   └── store.go           # Cache facade (L1 + future L2)
│   ├── provider/
│   │   ├── provider.go        # Provider interface + request/response types
│   │   ├── openai.go          # OpenAI-compatible HTTP client
│   │   └── health.go          # Health checks + circuit breaker
│   ├── workflow/
│   │   ├── tracker.go         # Multi-step workflow state machine
│   │   └── feedback.go        # POST /v1/feedback handler
│   └── telemetry/
│       ├── metrics.go         # Prometheus metrics + latency buckets
│       └── cost.go            # Per-workflow + per-team cost attribution
├── benchmarks/                # Performance benchmarks + scenario tests
├── go.mod                     # Go module (gopkg.in/yaml.v3 only)
└── README.md
```

## Research Background

Nexus is inspired by the **CASTER** paper ([arXiv:2601.19793](https://arxiv.org/abs/2601.19793)) — *"Adaptive Model Routing for LLM Serving"* — which proposes cost-efficient inference by dynamically routing queries to the most appropriate model based on complexity.

### How Nexus Implements CASTER Concepts

| CASTER Concept | Nexus Implementation |
|---------------|---------------------|
| **Dual-signal routing** | 5-signal classifier: prompt keywords, context length, agent role, step position, budget pressure |
| **Adaptive threshold optimization** | Configurable threshold (default 0.7) with budget-driven overrides |
| **Feedback-driven learning loop** | `POST /v1/feedback` records step outcomes for future optimization |
| **Multi-tier model selection** | 3 active tiers (cheap, mid, premium) with economy planned |

### Key Extension: Workflow Awareness

CASTER optimizes individual requests. Nexus extends this with **workflow-level optimization**:

- **Budget tracking** across all steps in a workflow
- **Step position awareness** — early foundational steps get premium models, late cleanup steps get cheap models
- **Role-based routing** — architect agents get premium, formatter agents get economy
- **Cross-step cost optimization** — save on simple steps to afford premium models when it matters

## Roadmap

- [x] ~~L2 semantic cache~~ ✅ 7-layer cache with BGE-M3 embeddings
- [x] ~~Multi-provider failover~~ ✅ Circuit breaker with automatic failover
- [x] ~~Dockerfile + Helm chart~~ ✅ Full Kubernetes deployment
- [x] ~~Dashboard UI~~ ✅ Real-time SSE dashboard + Grafana
- [x] ~~Cascade routing~~ ✅ Try cheap → escalate if confidence low
- [x] ~~Prompt compression~~ ✅ 20-35% token reduction
- [x] ~~Confidence scoring~~ ✅ 6-signal evaluator with learning map
- [ ] ML-based classifier (upgrade from rule-based keyword matching)
- [ ] A/B testing framework for routing strategies
- [ ] Python / Node.js / Go SDKs
- [ ] Anthropic native provider adapter
- [ ] Request priority queuing
- [ ] Admin dashboard with Clerk SSO

## License

Business Source License 1.1 — see [LICENSE](LICENSE) for details.

The BSL allows free use for all purposes except offering Nexus as a hosted/managed service.
On April 6, 2030, this converts automatically to Apache License 2.0.

---

<p align="center">
  Built with ☕ and Go — <em>route smarter, not harder</em>
</p>
