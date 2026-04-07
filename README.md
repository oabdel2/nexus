# Nexus

```
 _   _                      
| \ | | _____  ___   _ ___  
|  \| |/ _ \ \/ / | | / __| 
| |\  |  __/>  <| |_| \__ \ 
|_| \_|\___/_/\_\\__,_|___/ 
```

**Agentic-first inference optimization gateway with adaptive model routing**

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev)
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

## Quick Start (60 seconds)

### Option 1: Docker (recommended)

```bash
docker run -p 8080:8080 -e OPENAI_API_KEY=sk-... ghcr.io/oabdel2/nexus
```

### Option 2: Binary (zero-config)

```bash
export OPENAI_API_KEY=sk-...
nexus serve
```

Nexus auto-detects providers from environment variables — no config file needed.

### Option 3: With config file

```bash
nexus serve -config nexus.yaml
```

### Option 4: Ollama (no API keys)

```bash
# 1. Install Ollama (https://ollama.com) then pull a model
ollama pull llama3.1

# 2. Clone & build
git clone https://github.com/oabdel2/nexus.git && cd nexus
go build -o nexus ./cmd/nexus/

# 3. Run with the minimal config
./nexus serve -config configs/nexus.minimal.yaml
```

### Send a request

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Hello!"}]}'
```

Open **http://localhost:8080/dashboard** to see your savings in real time.

### Prerequisites

- Go 1.23 or later
- At least one LLM provider: an API key for OpenAI / Anthropic, or a local [Ollama](https://ollama.com) instance (no API key needed)

### Build & Run

```bash
# Clone
git clone https://github.com/oabdel2/nexus.git
cd nexus

# Build
go build -o nexus ./cmd/nexus/

# Run with zero-config (auto-detects from env vars)
export OPENAI_API_KEY=sk-...
./nexus serve

# Or run with an explicit config file
./nexus serve -config configs/nexus.yaml
```

### CLI Commands

| Command | Description |
|---------|-------------|
| `nexus serve` | Start the gateway server (default if no command given) |
| `nexus init` | Interactive configuration wizard |
| `nexus status` | Show gateway health and stats |
| `nexus validate` | Validate a configuration file |
| `nexus inspect <prompt>` | Analyze how a prompt would be routed |
| `nexus version` | Show version information |

### Serve Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-config` | `configs/nexus.yaml` | Path to configuration file |
| `-port` | `0` (use config) | Override server port |
| `-log-level` | (from config) | Log level: `debug`, `info`, `warn`, `error` |

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

### `GET /health/live`

Kubernetes liveness probe. Returns `200` if the process is running.

### `GET /health/ready`

Kubernetes readiness probe. Returns `200` only when providers are connected.

### `GET /dashboard`

Real-time cost savings dashboard (HTML). Open in a browser.

### `GET /dashboard/events`

Server-Sent Events stream powering the live dashboard.

### `GET /dashboard/api/stats`

Aggregate statistics JSON: total requests, savings, cache hit rate, model distribution.

### Admin & Diagnostics Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/synonyms/stats` | Synonym system statistics |
| `GET` | `/api/synonyms/candidates` | Synonym candidates pending review |
| `GET` | `/api/synonyms/learned` | Active learned synonyms |
| `POST` | `/api/synonyms/promote` | Promote a candidate to active |
| `POST` | `/api/synonyms/add` | Manually add a synonym mapping |
| `GET` | `/api/circuit-breakers` | Circuit breaker status per provider |
| `GET` | `/api/eval/stats` | Confidence map data per model/task type |
| `GET` | `/api/compression/stats` | Compression metrics and config |
| `GET` | `/api/shadow/stats` | Shadow evaluation comparison results |
| `GET` | `/api/adaptive/stats` | Adaptive routing statistics |
| `GET` | `/api/experiments` | List A/B experiments |
| `POST` | `/api/experiments` | Create a new A/B experiment |
| `GET` | `/api/experiments/{id}/results` | Results for a specific experiment |
| `POST` | `/api/experiments/{id}/toggle` | Enable/disable an experiment |
| `POST` | `/api/inspect` | Dry-run routing analysis (no provider call) |
| `GET` | `/api/events/recent` | Recent event bus events |
| `GET` | `/api/events/stats` | Event bus statistics |
| `GET` | `/api/plugins` | List loaded plugins |
| `POST` | `/api/keys/generate` | Generate a new API key |
| `POST` | `/api/keys/revoke` | Revoke an API key |
| `GET` | `/api/usage` | Usage stats by team/model/day |
| `POST` | `/webhooks/stripe` | Stripe webhook handler |
| `GET` | `/api/admin/subscriptions` | List active subscriptions |
| `GET` | `/api/admin/keys` | List all API keys |
| `GET` | `/api/admin/devices` | List registered devices |

## Configuration Reference

Create a `nexus.yaml` configuration file. All fields have sensible defaults.

```yaml
# ─── Server ──────────────────────────────────────────────────
server:
  port: 8080              # HTTP listen port
  read_timeout: 30s       # Max time to read request
  write_timeout: 120s     # Max time to write response (includes LLM latency)
  max_concurrent: 0       # Max concurrent requests (0 = unlimited)

# ─── Providers ───────────────────────────────────────────────
# Configure one or more LLM providers. Each provider can expose
# multiple models at different tier levels.
providers:
  - name: copilot
    type: openai              # openai, anthropic, or ollama
    base_url: https://api.githubcopilot.com
    api_key: ${GITHUB_COPILOT_TOKEN}
    enabled: true
    priority: 1               # Lower = preferred
    headers:                  # Custom headers for this provider
      Editor-Version: Nexus/0.1.0
    models:
      - name: gpt-4.1
        tier: mid             # economy, cheap, mid, or premium
        cost_per_1k_tokens: 0.002
        max_tokens: 32768
      - name: claude-sonnet-4
        tier: premium
        cost_per_1k_tokens: 0.003
        max_tokens: 64000

  - name: ollama
    type: ollama
    base_url: http://localhost:11434/v1
    enabled: false
    priority: 3
    models:
      - name: llama3.1
        tier: cheap
        cost_per_1k_tokens: 0.0
        max_tokens: 8192

# ─── Router ──────────────────────────────────────────────────
router:
  threshold: 0.7           # Score above this → premium tier
  default_tier: mid         # Fallback when no providers match
  budget_enabled: true      # Enable budget-aware routing
  default_budget: 1.0       # Default workflow budget in dollars
  smart_classifier: true    # Enable TF-IDF hybrid classifier
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
  l2_enabled: false         # L2 fuzzy/semantic cache
  ttl: 1h                   # Cache entry time-to-live
  max_entries: 10000        # Maximum cached entries (LRU eviction)
  similarity_min: 0.95      # Min similarity for L2 matches
  l1:
    enabled: true
    ttl: 15m
    max_entries: 10000
  l2_bm25:                  # BM25 keyword-based fuzzy cache
    enabled: true
    ttl: 1h
    max_entries: 50000
    threshold: 15.0          # Min BM25 score for a match
  l2_semantic:               # BGE-M3 embedding similarity cache
    enabled: true
    ttl: 1h
    max_entries: 50000
    threshold: 0.70          # Min cosine similarity
    backend: ollama          # Embedding provider
    model: bge-m3
    endpoint: http://localhost:11434
    reranker:
      enabled: false
      model: bge-reranker-v2-m3
      endpoint: http://localhost:11434
      threshold: 0.5
  feedback:
    enabled: true
    max_size: 10000
  shadow:
    enabled: false
    max_results: 1000
  synonym:
    data_dir: ./data
    promotion_threshold: 3

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

# ─── Tracing ─────────────────────────────────────────────────
tracing:
  enabled: true
  service_name: nexus-gateway
  sample_rate: 1.0           # 1.0 = trace every request
  log_spans: true
  export_url: ""             # Set to http://jaeger:4318/v1/traces for OTLP

# ─── Compression ─────────────────────────────────────────────
compression:
  enabled: true
  whitespace: true           # Remove unnecessary whitespace
  code_strip: true           # Strip redundant code formatting
  history_truncate: true     # Truncate long message histories
  max_history_turns: 20      # Max conversation turns to keep
  preserve_last_n: 5         # Always preserve the N most recent turns

# ─── Cascade ─────────────────────────────────────────────────
cascade:
  enabled: false
  confidence_threshold: 0.78 # Escalate if cheap model confidence < this
  max_latency_ms: 5000       # Max latency for cheap model attempt
  sample_rate: 1.0           # Fraction of requests eligible for cascade

# ─── Eval ────────────────────────────────────────────────────
eval:
  enabled: true
  data_dir: ./data/eval
  hedging_penalty: 0.15      # Penalty for hedging language
  sample_rate: 1.0           # Fraction of responses to evaluate
  shadow_enabled: false      # Compare cheap vs premium in background
  shadow_sample_rate: 0.10   # Fraction of requests for shadow eval

# ─── Experiment (A/B Testing) ────────────────────────────────
experiment:
  enabled: false
  auto_start: false          # Auto-start experiments on creation

# ─── Adaptive Routing ────────────────────────────────────────
adaptive:
  enabled: false
  min_samples: 50            # Min samples before adjusting routing
  high_confidence: 0.90      # Confidence above this = keep routing
  low_confidence: 0.50       # Confidence below this = escalate tier

# ─── Events ──────────────────────────────────────────────────
events:
  enabled: true
  webhook_urls: []           # External webhook URLs for event delivery
  webhook_secret: ""         # HMAC secret for webhook signatures

# ─── Plugins ─────────────────────────────────────────────────
plugins:
  enabled: true

# ─── Security ────────────────────────────────────────────────
security:
  tls:
    enabled: false
    cert_file: ""
    key_file: ""
    ca_file: ""
    min_version: "1.2"
    mutual_tls: false
  prompt_guard:
    enabled: true
    mode: block              # block or log
    max_prompt_length: 32000
  oidc:
    enabled: false
    issuer: ""
    client_id: ""
    client_secret: ""
  rbac:
    enabled: false
    roles:
      admin:
        permissions: ["chat", "admin", "synonyms:read", "synonyms:write", "dashboard", "feedback"]
      user:
        permissions: ["chat", "feedback"]
  rate_limit:
    enabled: true
    default_rpm: 60
    burst_size: 10
  cors:
    allowed_origins: ["*"]
  audit_log: true
  body_size_limit: 1048576   # 1MB
  request_timeout: "30s"
  panic_recovery: true
  ip_allowlist:
    enabled: false
    allowed_ips: []
    paths: ["/api/admin/"]
  input_validation: true
  request_logging: true

# ─── Billing ─────────────────────────────────────────────────
billing:
  enabled: false
  data_dir: ./data/billing
  stripe_webhook_secret: ${STRIPE_WEBHOOK_SECRET}
  default_plan: free

# ─── Notification ────────────────────────────────────────────
notification:
  enabled: false
  smtp_host: ""
  smtp_port: 587
  smtp_user: ""
  smtp_password: ""
  from_email: noreply@nexus-gateway.com
  from_name: Nexus Gateway

# ─── Storage ─────────────────────────────────────────────────
storage:
  vector_backend: memory     # memory or qdrant
  kv_backend: memory         # memory or redis
  qdrant_host: localhost
  qdrant_port: 6333
  qdrant_collection: nexus_cache
  qdrant_dimension: 1024
  redis_addr: localhost:6379
  redis_password: ""
  redis_db: 0
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
├── cmd/nexus/main.go              # Entry point, CLI subcommands, banner
├── configs/
│   ├── nexus.yaml                 # Full reference configuration
│   └── nexus.minimal.yaml         # Minimal config (Ollama only)
├── internal/
│   ├── auth/                      # Authentication helpers
│   ├── billing/                   # Subscriptions, API keys, Stripe webhooks
│   ├── cache/
│   │   ├── exact.go               # L1 SHA-256 exact-match cache (LRU + TTL)
│   │   ├── bm25.go                # L2 BM25 keyword similarity cache
│   │   ├── semantic.go            # L2 semantic embedding cache
│   │   ├── reranker.go            # Cross-encoder verification
│   │   ├── synonym.go             # Auto-discovered synonym learning
│   │   ├── feedback.go            # Quality feedback loop
│   │   ├── shadow.go              # Shadow mode validation
│   │   └── store.go               # Cache facade (all layers)
│   ├── compress/                  # Prompt compression (whitespace, code, history)
│   ├── config/config.go           # Configuration types + YAML loading + defaults
│   ├── dashboard/                 # Real-time SSE dashboard
│   ├── eval/                      # 6-signal confidence scoring + learning map
│   ├── events/                    # Event bus + webhook delivery
│   ├── experiment/                # A/B testing framework
│   ├── gateway/
│   │   ├── server.go              # HTTP server, route registration, middleware
│   │   ├── handler_chat.go        # POST /v1/chat/completions pipeline
│   │   └── handler_admin.go       # Admin/diagnostics endpoints
│   ├── notification/              # SMTP email notifications
│   ├── plugin/                    # Plugin registry + hooks
│   ├── provider/
│   │   ├── provider.go            # Provider interface + request/response types
│   │   ├── openai.go              # OpenAI-compatible HTTP client
│   │   ├── anthropic.go           # Anthropic adapter
│   │   └── health.go              # Health checks + circuit breaker
│   ├── router/
│   │   ├── classifier.go          # Complexity scoring engine
│   │   ├── router.go              # Adaptive model selection + tier mapping
│   │   ├── budget.go              # Budget tracking + downgrade logic
│   │   └── cascade.go             # Cascade routing (cheap → escalate)
│   ├── security/                  # Prompt guard, RBAC, OIDC, rate limiting
│   ├── storage/                   # Qdrant + Redis storage backends
│   ├── telemetry/
│   │   ├── metrics.go             # Prometheus metrics + latency buckets
│   │   └── cost.go                # Per-workflow + per-team cost attribution
│   └── workflow/
│       ├── tracker.go             # Multi-step workflow state machine
│       └── feedback.go            # POST /v1/feedback handler
├── benchmarks/                    # Performance benchmarks + scenario tests
├── sdk/                           # SDK quickstarts (Python, Node, Go, curl)
├── site/                          # Landing page, docs, how-it-works, pricing
├── deploy/                        # Helm chart, Kubernetes manifests
├── monitoring/                    # Grafana dashboards
├── tests/                         # E2E integration tests
├── go.mod                         # Go module (gopkg.in/yaml.v3 only)
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
- [x] ~~Python / Node.js / Go / curl SDKs~~ ✅ OpenAI-compatible quickstarts
- [x] ~~Anthropic native provider adapter~~ ✅ Full Anthropic support
- [ ] ML-based classifier (upgrade from rule-based keyword matching)
- [ ] A/B testing framework for routing strategies
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
