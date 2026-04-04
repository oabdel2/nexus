# Nexus Platform Architecture

## Vision
Nexus is an integrated inference optimization platform that supports any product at any integration depth — from zero-config proxy to full workflow-aware SDK.

## Integration Tiers (Progressive Enhancement)

### Tier 0: Transparent Proxy (Zero Code Changes)
**For any product that uses OpenAI-compatible APIs.**

Just set one environment variable:
```bash
OPENAI_API_BASE=http://nexus:8080/v1
```

Works immediately with:
- Cursor, Continue, GitHub Copilot (IDE tools)
- LangChain, CrewAI, AutoGen, Haystack (agent frameworks)
- Any OpenAI Python/JS SDK user
- Custom apps using the OpenAI API format

What you get at Tier 0:
- Automatic complexity-based routing (saves 40-70%)
- Per-request cost tracking
- Prometheus metrics
- Dashboard visibility
- L1 response caching

**Auto-Workflow Detection**: Even without explicit headers, Nexus detects workflow boundaries by analyzing:
- Request fingerprinting (same API key + similar system prompt = same workflow)
- Temporal clustering (requests within 30s window = same workflow)
- Conversation threading (messages that reference prior context)
- Session affinity (same source IP + User-Agent)

### Tier 1: Header-Enhanced (2 Lines of Code)
Add two HTTP headers to unlock workflow-aware routing:
```
X-Workflow-ID: my-workflow-123
X-Step-Number: 3
```

Additional features:
- Per-workflow budget tracking
- Step-aware routing (step 1 gets premium, step 5 gets economy)
- Workflow-level cost reports
- Budget alerts and auto-downgrades

### Tier 2: SDK Integration (pip install)
```python
pip install nexus-gateway
```

Full SDK with:
- Automatic step counting
- Workflow context management
- Feedback submission
- LangChain ChatModel drop-in
- CrewAI LLM wrapper
- Async support
- Streaming support

### Tier 3: Platform API (Full Control)
REST API for programmatic control:
- Create/manage API keys
- Set per-key budgets and rate limits
- Configure custom routing rules
- Access analytics and cost reports
- Manage team/org hierarchies
- Webhook subscriptions

## Multi-Tenant Architecture

### API Key System
```yaml
# Each product/team gets a key
api_keys:
  - key: "nxk_prod_abc123"
    name: "ChatBot Production"
    team: "customer-success"
    budget:
      monthly: 500.00
      alert_threshold: 0.80
    rate_limit:
      requests_per_minute: 100
    allowed_tiers: [economy, cheap, mid, premium]
    allowed_models: ["gpt-4.1", "claude-sonnet-4"]
    
  - key: "nxk_dev_xyz789"
    name: "Dev Environment"
    team: "engineering"
    budget:
      monthly: 50.00
    rate_limit:
      requests_per_minute: 30
    allowed_tiers: [economy, cheap]
```

### Cost Attribution
Every request is tagged with:
- API key → team → organization
- Workflow ID → step number
- Model used → tier selected
- Actual cost → estimated savings

Reports available per:
- Hour / Day / Week / Month
- Team / Product / Workflow
- Model / Tier / Provider
- Exportable as CSV, JSON, or sent via webhook

### Organization Hierarchy
```
Organization (billing entity)
├── Team: Engineering
│   ├── Product: Code Review Bot (nxk_xxx)
│   └── Product: Test Generator (nxk_yyy)
├── Team: Customer Success
│   └── Product: Support Chatbot (nxk_zzz)
└── Team: Data Science
    └── Product: Analysis Pipeline (nxk_aaa)
```

## Plugin System

### Plugin Interface
```go
// Classifier plugin — bring your own complexity scorer
type ClassifierPlugin interface {
    Name() string
    Score(ctx context.Context, req *ClassifyRequest) (float64, error)
}

// Router plugin — custom routing strategy
type RouterPlugin interface {
    Name() string
    Route(ctx context.Context, score float64, budget *BudgetState) (string, error)
}

// Provider plugin — custom LLM backend
type ProviderPlugin interface {
    Name() string
    Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    Models() []string
}

// Hook plugin — react to events
type HookPlugin interface {
    Name() string
    OnRequest(ctx context.Context, event *RequestEvent) error
    OnResponse(ctx context.Context, event *ResponseEvent) error
    OnBudgetAlert(ctx context.Context, event *BudgetEvent) error
}
```

### Built-in Plugins
- **keyword-classifier**: Default CASTER-inspired keyword scoring (current)
- **ml-classifier**: ML-based complexity scoring (v2 — uses a small local model)
- **openai-provider**: OpenAI-compatible API client
- **anthropic-provider**: Native Anthropic API client
- **ollama-provider**: Local model provider
- **slack-hook**: Budget alerts and daily cost reports to Slack
- **pagerduty-hook**: Critical budget exhaustion alerts
- **s3-hook**: Audit log archival to S3/GCS/Azure Blob

### Community Plugins (Ecosystem)
- Custom ML classifiers trained on your data
- Industry-specific routing rules (healthcare, finance, legal)
- Custom cost models for fine-tuned/self-hosted models
- Integration hooks for internal tools

## Webhook Events

### Event Types
```json
{
  "events": [
    "request.completed",
    "request.cached",
    "request.error",
    "budget.warning",
    "budget.critical",
    "budget.exhausted",
    "workflow.started",
    "workflow.completed",
    "cost.anomaly",
    "tier.downgrade",
    "provider.unhealthy",
    "provider.recovered"
  ]
}
```

### Webhook Configuration
```yaml
webhooks:
  - url: "https://hooks.slack.com/xxx"
    events: [budget.warning, budget.critical, cost.anomaly]
    secret: "${WEBHOOK_SECRET}"
  
  - url: "https://your-api.com/nexus-events"
    events: ["*"]  # All events
    headers:
      Authorization: "Bearer ${INTERNAL_TOKEN}"
```

## Deployment Modes

### 1. Docker (Simplest)
```bash
docker compose up  # Nexus + Prometheus + Grafana
```

### 2. Kubernetes (Production)
```bash
helm install nexus nexus-gateway/nexus \
  --set replicas=3 \
  --set prometheus.enabled=true
```

### 3. Sidecar (Zero-Touch)
Inject as a Kubernetes sidecar — automatically intercepts all LLM API calls from pods in the namespace. No code changes, no DNS changes.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    nexus.gateway/inject: "true"
    nexus.gateway/budget: "100.00"
    nexus.gateway/team: "engineering"
```

### 4. Edge/CDN (Global)
Deploy Nexus at the edge (Cloudflare Workers, Fly.io) for lowest latency routing decisions globally.

### 5. Embedded (Library Mode)
Import Nexus as a Go library for embedded use:
```go
import "github.com/nexus-gateway/nexus/pkg/router"

router := router.New(config)
tier, model := router.Route(prompt, workflowCtx)
```

## Roadmap

### v0.1 (Current MVP)
- [x] 4-tier adaptive routing
- [x] Workflow tracking via headers
- [x] Budget-aware downgrades
- [x] L1 exact cache
- [x] Prometheus metrics
- [x] Web dashboard
- [x] Python SDK

### v0.2 (Platform Foundation)
- [ ] Transparent proxy mode with auto-workflow detection
- [ ] API key system with per-key budgets
- [ ] Plugin interface for classifiers and hooks
- [ ] Webhook events (Slack, PagerDuty)
- [ ] Helm chart for Kubernetes

### v0.3 (Intelligence)
- [ ] ML-based classifier (small local model)
- [ ] Semantic cache (embedding-based similarity)
- [ ] A/B testing for routing strategies
- [ ] Cost anomaly detection
- [ ] Auto-tuning classifier weights from feedback

### v0.4 (Enterprise)
- [ ] Multi-org, multi-team hierarchy
- [ ] SSO/SAML integration
- [ ] Audit logging
- [ ] Compliance reporting (SOC2, HIPAA-adjacent)
- [ ] Custom SLA routing (premium path for SLA-bound requests)

### v1.0 (Platform)
- [ ] Marketplace for community plugins
- [ ] Managed cloud offering
- [ ] Global edge deployment
- [ ] Cost optimization recommendations engine
- [ ] "Nexus Autopilot" — fully automated routing optimization

## Why This Wins

| Challenge | TensorZero | Nexus Platform |
|-----------|-----------|----------------|
| Time to first value | Hours (TOML config, function definitions) | Seconds (change 1 env var) |
| Works with existing tools | Requires code changes | Transparent proxy, zero changes |
| Budget management | None built-in | Per-key, per-workflow, per-team |
| Extensibility | Closed architecture | Plugin system, community marketplace |
| Multi-tenant | Single tenant | API keys, teams, orgs |
| Cost visibility | Basic | Per-team, per-product attribution |
| Integration depth | All or nothing | Progressive: proxy → headers → SDK → API |

**The key insight**: TensorZero optimizes inference quality through experimentation. Nexus optimizes inference cost through intelligence. They can actually coexist — but Nexus has a much wider addressable market because it works with ZERO effort.
