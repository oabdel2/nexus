# Nexus Agent Orchestrator — Operating Manual

> **Purpose**: Define the multi-agent system that develops, maintains, and evolves the Nexus product.
> Each agent is a specialist with bounded context. The orchestrator routes work to the right agent
> and spins up sub-specialists when context grows beyond what one agent should hold.

---

## 1. Agent Roster

### Core Agents (always available)

| Agent | Domain | Owns | Context Boundary |
|-------|--------|------|-----------------|
| **Orchestrator** | Routing & coordination | Task decomposition, agent selection, conflict resolution | Project-wide awareness, no implementation |
| **R&D Scientist** | Research & experimentation | Algorithm design, benchmarking, paper reviews, A/B test design | Research docs, academic papers, experimental data |
| **Architect** | System design | HLD/LLD, API contracts, data models, integration patterns | Architecture docs, config schema, package structure |
| **Backend Engineer** | Go implementation | Source code, tests, benchmarks, build pipeline | `internal/` packages, `cmd/`, `configs/`, Go toolchain |
| **Infra Engineer** | Deployment & ops | Docker, Helm, CI/CD, monitoring, cloud infra | `deploy/`, `docker-compose*`, `.github/`, `monitoring/` |
| **Security Engineer** | Security & compliance | Auth, TLS, OWASP hardening, audit, legal compliance | `internal/security/`, `internal/billing/`, security configs |
| **Product Manager** | Roadmap & priorities | Sprint planning, KPIs, milestone tracking, decision log | `docs/project/`, todo tracking, success metrics |
| **Marketing/GTM** | Positioning & growth | Messaging, content, competitive analysis, pricing | `docs/product/`, `site/`, customer-facing copy |
| **QA Engineer** | Testing & quality | E2E tests, load tests, regression, test infrastructure | `tests/`, test configs, quality gates |

### Specialist Agents (spun up on demand)

| Specialist | Trigger | Parent Agent | Context |
|------------|---------|-------------|---------|
| **Cascade Routing Specialist** | When implementing cascade feature | Backend Engineer | `internal/router/`, `internal/eval/`, cascade research |
| **Cache Optimization Specialist** | When tuning cache layers | Backend Engineer | `internal/cache/` (12 files), embedding configs |
| **Compression Specialist** | When implementing/tuning compression | Backend Engineer | `internal/compress/`, token analysis |
| **Billing/Payments Specialist** | When integrating Stripe/Clerk | Security Engineer | `internal/billing/`, `internal/notification/`, payment flows |
| **Frontend Specialist** | When building UI/dashboard | Marketing/GTM | `site/`, `internal/dashboard/`, CSS/HTML |
| **SDK Specialist** | When building client SDKs | Backend Engineer | `sdk/`, API compatibility, language-specific patterns |
| **Load Testing Specialist** | When running performance benchmarks | QA Engineer | vegeta/wrk configs, benchmark results, profiling |
| **Legal/Compliance Specialist** | When drafting ToS/DPA/SOC2 | Product Manager | Legal templates, regulatory requirements |

---

## 2. Orchestration Rules

### 2.1 Task Routing

When a task comes in, the orchestrator classifies it by domain:

```
User Request
     │
     ▼
┌─────────────────────────────────────────────────────────┐
│                    ORCHESTRATOR                          │
│                                                         │
│  Classify task → Route to agent → Monitor → Integrate   │
│                                                         │
│  Classification keywords:                               │
│  ┌─────────────────┬───────────────────────────────┐    │
│  │ "research"      │ → R&D Scientist               │    │
│  │ "benchmark"     │ → R&D Scientist               │    │
│  │ "design"        │ → Architect                   │    │
│  │ "implement"     │ → Backend Engineer            │    │
│  │ "deploy"        │ → Infra Engineer              │    │
│  │ "secure"        │ → Security Engineer           │    │
│  │ "test"          │ → QA Engineer                 │    │
│  │ "plan"          │ → Product Manager             │    │
│  │ "market"        │ → Marketing/GTM               │    │
│  │ "fix bug"       │ → Backend + QA (parallel)     │    │
│  │ "new feature"   │ → Architect → Backend → QA    │    │
│  │ "launch"        │ → ALL agents coordinated      │    │
│  └─────────────────┴───────────────────────────────┘    │
└─────────────────────────────────────────────────────────┘
```

### 2.2 Context Management (The Key Innovation)

**Problem**: Large codebases exceed what a single agent can hold in context.
**Solution**: Each agent only loads files relevant to its domain. When a task
crosses domains, the orchestrator provides a minimal interface contract.

**Rule 1: Bounded Context**
```
Backend Engineer sees:  internal/, cmd/, configs/, go.mod
Infra Engineer sees:    deploy/, docker-compose*, .github/, monitoring/, Dockerfile
Marketing sees:         site/, docs/product/, README.md
```

**Rule 2: Specialist Spin-Up Trigger**
If an agent's working set exceeds ~15 files OR the task requires deep expertise
in a sub-domain, spin up a specialist:

```
Backend Engineer working on cache + router + eval + compress (40+ files)
  → Spin up Cascade Routing Specialist (router/ + eval/ only, ~10 files)
  → Backend continues on compress/ independently
  → Both finish → Orchestrator merges & validates
```

**Rule 3: Interface Contracts**
When agents need to coordinate, they communicate through:
1. **Struct definitions** — agreed data types (e.g., `ConfidenceResult`)
2. **Function signatures** — agreed interfaces (e.g., `Scorer.Score(prompt, response) float64`)
3. **Config schema** — agreed YAML keys
4. **API endpoints** — agreed HTTP paths and request/response shapes

The Architect agent owns these contracts. Other agents implement them.

### 2.3 Parallel vs Sequential Execution

```
PARALLEL (no shared state):
├── R&D researches cascade parameters
├── Marketing writes competitor analysis
├── PM creates sprint plan
└── QA designs test matrix

SEQUENTIAL (shared state / dependency):
  Architect designs API ──→ Backend implements ──→ QA tests ──→ Infra deploys

HYBRID (common pattern for features):
  Phase 1 (parallel):
  ├── R&D: research + benchmarks
  ├── Architect: design contracts
  └── PM: define acceptance criteria
  
  Phase 2 (sequential):
  Backend implements ──→ QA validates ──→ Security reviews
  
  Phase 3 (parallel):
  ├── Infra: deploy configs
  ├── Marketing: update docs/site
  └── PM: update metrics
```

---

## 3. Agent Prompt Templates

### 3.1 Orchestrator Prompt (Me)

```
I am the Nexus Orchestrator. I:
1. Decompose user requests into agent-sized tasks
2. Route tasks to the right specialist agent
3. Provide cross-agent context (minimal, interface-level)
4. Validate outputs don't conflict across agents
5. Merge results and commit when verified

I DO NOT: write code, make architecture decisions, or implement features.
I DO: coordinate, validate, and integrate.
```

### 3.2 Agent Prompt Template

Each agent receives:
```
You are the [ROLE] for Nexus — [one-line project description].

## Your Domain
[List of files/dirs you own]

## Current State
[Relevant excerpt from plan.md / roadmap.md]

## Task
[Specific deliverable with acceptance criteria]

## Interfaces You Must Respect
[Struct definitions, function signatures, config keys owned by other agents]

## Constraints
- Zero external Go dependencies (only yaml.v3)
- Must not modify files outside your domain
- Must pass: go build ./... && go test ./... -count=1
```

---

## 4. Feature Development Workflow

### Example: Adding Cascade Routing

```
Step 1: ORCHESTRATOR decomposes
  ├── R&D: finalize cascade threshold (done: 0.78)
  ├── Architect: design CascadeConfig, CascadeResult types + handleChat changes
  ├── Backend: implement internal/router/cascade.go
  ├── QA: add cascade E2E tests
  ├── Infra: add cascade config to Helm values.yaml
  └── Marketing: update how-it-works.html

Step 2: ARCHITECT produces contracts (first)
  Output: cascade_types.go with CascadeConfig, CascadeResult
  Output: updated handleChat pseudocode showing integration point
  
Step 3: BACKEND + QA in parallel
  Backend: implements cascade.go against Architect's contracts
  QA: writes tests against Architect's contracts (TDD)
  
Step 4: ORCHESTRATOR merges
  - go build ./... ✅
  - go test ./... ✅
  - E2E tests pass ✅
  - git commit

Step 5: PARALLEL finishing
  ├── Infra: helm + docker config updates
  ├── Marketing: site updates
  └── PM: update roadmap, close todos
```

---

## 5. When to Split an Agent

### Triggers for Specialist Spin-Up

| Signal | Action |
|--------|--------|
| Agent reading >15 files in one task | Split into sub-domain specialist |
| Agent task estimated >500 lines of code | Split implementation from testing |
| Two sub-tasks in same agent have no shared state | Run as parallel specialists |
| Agent working in both `internal/cache/` and `internal/router/` | Cache Specialist + Routing Specialist |
| Agent needs web research + code writing | R&D Specialist (research) + Backend (code) |

### Merge Protocol

When specialists finish:
1. Orchestrator reads all outputs
2. Checks for conflicts (same file edited, incompatible types)
3. Runs build + test
4. Resolves conflicts (or asks Architect to arbitrate)
5. Commits as single coherent change

---

## 6. Hallucination & Context Loss Detection

### Signals That an Agent Is Losing Context

| Signal | Detection | Action |
|--------|-----------|--------|
| **Referencing files that don't exist** | Agent edits/reads non-existent paths | Kill agent, spin up fresh specialist with correct file list |
| **Contradicting prior decisions** | Output conflicts with design docs or earlier commits | Flag to Architect for arbitration |
| **Repeating completed work** | Agent re-implements something already built | Stop, provide explicit "already done" context, restart |
| **Generating placeholder/stub code** | Functions that return nil/empty without logic | Reject output, re-prompt with examples from codebase |
| **Wrong package/import paths** | Using import paths that don't exist in go.mod | Build failure catches this — reject and re-prompt |
| **Inventing external dependencies** | Adding imports beyond yaml.v3 | Build failure + explicit rejection |
| **Drifting off-task** | Agent starts working on unrelated features | Stop agent, re-scope with narrower prompt |
| **Inconsistent naming** | Types/functions don't match established conventions | QA catches in review — send back for fix |

### Prevention Rules

1. **Every agent prompt includes**: explicit file list of its domain + "do NOT modify files outside your domain"
2. **Fresh specialists over overloaded agents**: If an agent's task requires reading >15 files, split it
3. **Verification gate**: Every agent output goes through `go build ./... && go test ./... -count=1` before commit
4. **Cross-reference check**: Orchestrator diffs agent output against existing code to catch contradictions
5. **Short-lived specialists**: Spin up, do one task, deliver, terminate. Don't reuse stale context.

---

## 7. MCP Servers for Market Analysis & Audience Research

### Available MCP Integrations (for Marketing/GTM agent)

| MCP Server | Purpose | Use For Nexus |
|------------|---------|---------------|
| **GA4 MCP** | Google Analytics data | Track site/docs visitors, conversion funnel |
| **Google Search Console MCP** | SEO performance | Monitor "AI gateway" keyword rankings |
| **Semrush / Ahrefs MCP** | Competitor SEO analysis | Track competitor content, backlinks, traffic |
| **Xpoz MCP** | Social media analytics | Monitor Twitter/Reddit sentiment about AI gateways |
| **DataWhisker MCP** | Twitter/X deep analytics | Track competitor mentions, trending discussions |
| **HubSpot MCP** | CRM + audience segmentation | Track leads, pipeline, customer segments |
| **Aidelly MCP** | Social media management | Schedule/publish Nexus content across platforms |
| **SegmentStream MCP** | Cross-channel attribution | Measure which channels drive signups |
| **Stripe MCP** | Payment analytics | Revenue tracking, churn analysis, MRR |
| **GitHub MCP** | Repo analytics | Stars, forks, issues, contributor activity |

### Integration Strategy
Phase 1 (Launch): GitHub MCP + GA4 MCP + Google Search Console MCP (free, immediate value)
Phase 2 (Growth): Semrush MCP + Xpoz MCP + Stripe MCP (paid, data-driven optimization)
Phase 3 (Scale): HubSpot MCP + SegmentStream MCP (enterprise CRM + attribution)

---

## 8. QA & Testing Strategy

### Testing Pyramid for Nexus

```
                    ╱╲
                   ╱  ╲        Manual Exploratory
                  ╱ 5% ╲       (human-driven, edge cases)
                 ╱──────╲
                ╱        ╲     E2E Integration (32 tests)
               ╱   10%    ╲    (live Ollama, full pipeline)
              ╱────────────╲
             ╱              ╲   Contract Tests
            ╱     15%        ╲  (API schema validation)
           ╱──────────────────╲
          ╱                    ╲  Component Tests
         ╱       30%            ╲ (cache, router, provider)
        ╱────────────────────────╲
       ╱                          ╲ Unit Tests (62+ compress/eval)
      ╱          40%                ╲ (pure logic, no I/O)
     ╱────────────────────────────────╲
```

### Test Categories & Tools

| Layer | Tool | What It Tests | Runs When |
|-------|------|---------------|-----------|
| **Unit** | Go `testing` + stdlib | Pure functions, algorithms, data structures | Every commit (CI) |
| **Component** | Go `testing` + httptest | Individual packages with mocked deps | Every commit (CI) |
| **Contract** | Custom JSON schema validator | API request/response shapes match OpenAI spec | Every PR |
| **E2E Integration** | `tests/e2e/main.go` (32 tests) | Full gateway pipeline with live Ollama | Pre-release |
| **Load** | k6 (JavaScript) | Throughput, P99 latency, concurrent connections | Weekly / pre-release |
| **Security** | gosec + govulncheck + Trivy | SAST, dependency vulns, container scanning | Every commit (CI) |
| **Manual Exploratory** | Human tester | Edge cases, UX, unexpected inputs | Pre-release |

### Load Testing Plan (k6)

```
Scenarios:
1. Steady state:     50 RPS for 5 minutes → all requests succeed, P99 < 500ms
2. Ramp up:          0 → 200 RPS over 2 minutes → graceful handling
3. Cache warmup:     100 identical requests → cache hit rate > 95% after 10th
4. Circuit breaker:  Provider returns 500 → failover within 2 requests
5. Concurrent users: 50 parallel workflows → no race conditions
6. Compression:      Compare token counts with/without compression enabled
7. Cascade:          Measure escalation rate and cost savings vs baseline
```

### Manual QA Checklist (pre-release)

- [ ] Fresh `docker compose up` works within 3 minutes
- [ ] Send chat request via curl → valid OpenAI-compatible response
- [ ] Send streaming request → SSE chunks arrive progressively
- [ ] Repeat same request → cache hit (verify X-Nexus-Cache header)
- [ ] Send prompt injection → blocked with 400
- [ ] Send oversized body → rejected with 413
- [ ] Kill Ollama mid-request → circuit breaker opens, error returned cleanly
- [ ] Restart Ollama → circuit breaker recovers, requests succeed
- [ ] Check /metrics → Prometheus format, all counters populated
- [ ] Check /dashboard → SSE events flowing, stats updating
- [ ] Check /health/ready → returns 200 with provider status
- [ ] Verify X-Nexus-Model, X-Nexus-Tier, X-Nexus-Provider on every response
- [ ] Verify X-Request-ID unique per request
- [ ] Run 10 concurrent requests → no panics, all return valid JSON
- [ ] Check Grafana dashboards → data populating correctly

---

## 9. Agent Interaction Map

```
                         ┌──────────────┐
                         │ ORCHESTRATOR │
                         └──────┬───────┘
                                │
            ┌───────────────────┼───────────────────┐
            │                   │                   │
     ┌──────▼──────┐    ┌──────▼──────┐    ┌──────▼──────┐
     │  RESEARCH   │    │   BUILD     │    │  DELIVER    │
     │  CLUSTER    │    │   CLUSTER   │    │  CLUSTER    │
     │             │    │             │    │             │
     │ R&D Sci.    │    │ Architect   │    │ PM          │
     │ (research)  │◄──►│ (design)    │◄──►│ (plan)      │
     │             │    │     │       │    │             │
     └─────────────┘    │ Backend Eng │    │ Marketing   │
                        │ (implement) │    │ (position)  │
                        │     │       │    │             │
                        │ QA Engineer │    │ Frontend    │
                        │ (validate)  │    │ (site/UI)   │
                        │     │       │    │             │
                        │ Infra Eng   │    └─────────────┘
                        │ (deploy)    │
                        │     │       │
                        │ Security    │
                        │ (harden)    │
                        └─────────────┘

  Arrows = can request work from each other
  Clusters = typically execute in parallel
  Orchestrator = only entry point for new tasks
```

---

## 10. Current Agent Assignments

Based on the roadmap (Phase 0-1), here's what each agent owns RIGHT NOW:

| Agent | Current Sprint Tasks | Status |
|-------|---------------------|--------|
| **Orchestrator** | Coordinate Phase 0 launch prep | Active |
| **Architect** | HLD/LLD document (in progress) | 🔄 Running |
| **Backend Engineer** | Integrate compress/ + eval/ into gateway pipeline | Queued |
| **Infra Engineer** | Push to GitHub, activate CI | Queued |
| **Security Engineer** | Review all exposed endpoints, HSTS preload | Queued |
| **QA Engineer** | Add compression + eval to E2E suite | Queued |
| **PM** | Finalize Phase 0 sprint backlog | ✅ Done |
| **Marketing** | Product strategy doc | ✅ Done |
| **R&D** | Cascade/eval/compression research | ✅ Done |

---

## 11. Escalation Protocol

When an agent gets stuck:
1. **Self-unblock**: Try alternative approach within own domain
2. **Peer consult**: Ask related agent for interface/context (via orchestrator)
3. **Escalate to Architect**: Design conflict or unclear contract
4. **Escalate to Orchestrator**: Cross-domain conflict or priority question
5. **Escalate to User**: Ambiguous requirements or business decision needed
