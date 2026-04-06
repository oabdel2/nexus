# Nexus Gateway — curl Examples

**Nexus is a drop-in proxy — any OpenAI-compatible HTTP request works, just change the URL.**

These examples work with any Nexus deployment.

---

## 3-Step Quickstart

### 1. Set your gateway URL

```bash
export NEXUS_URL="https://nexus-gateway.example.com"
export NEXUS_API_KEY="your-api-key"
```

### 2. No install needed — you already have curl

### 3. Send your first request

```bash
curl -s "${NEXUS_URL}/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${NEXUS_API_KEY}" \
  -d '{
    "model": "auto",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

That's it. Identical to calling OpenAI directly — the only difference is the URL.

---

## Nexus-Specific Headers

### Request Headers (you send)

| Header | Description | Example |
|---|---|---|
| `X-Workflow-ID` | Groups related requests into a workflow for cumulative cost tracking | `"design-session-42"` |
| `X-Agent-Role` | Hints the router about task type — affects model/tier selection | `"architect"`, `"researcher"`, `"chat"`, `"tester"` |
| `X-Team` | Team identifier for billing and cost attribution | `"platform-eng"` |
| `X-Budget` | Maximum USD budget for this workflow | `"2.50"` |
| `X-Step-Number` | Position in a multi-step workflow | `"3"` |
| `X-Request-ID` | Trace ID for request correlation (auto-generated if omitted) | `"req-abc123"` |

### Response Headers (Nexus returns)

Use `curl -D -` or `curl -i` to see these:

| Header | Description | Example |
|---|---|---|
| `X-Nexus-Model` | Model Nexus actually used | `"gpt-4o"` |
| `X-Nexus-Tier` | Routing tier selected | `"cheap"`, `"mid"`, `"premium"` |
| `X-Nexus-Provider` | Backend provider used | `"openai"`, `"anthropic"`, `"cache/L1"` |
| `X-Nexus-Cost` | Estimated cost in USD | `"0.003200"` |
| `X-Nexus-Cache` | Cache layer if served from cache | `"L1"`, `"L2a"`, `"L2b"` |
| `X-Nexus-Confidence` | Response quality score (0–1) | `"0.850"` |
| `X-Nexus-Workflow-ID` | Echoed workflow ID | `"design-session-42"` |
| `X-Nexus-Workflow-Step` | Current step in workflow | `"2"` |

---

## All Examples

See [`examples.sh`](examples.sh) for a complete runnable script covering:

- Non-streaming and streaming chat completions
- Nexus routing headers with verbose response headers
- Multi-step workflow (3 steps with different agent roles)
- Feedback submission
- Health, readiness, and liveness probes
- Prometheus metrics
- Synonym, evaluation, circuit breaker, and compression stats
- Gateway info endpoint

---

## Quick Reference

```bash
# Chat completion
curl -s "${NEXUS_URL}/v1/chat/completions" -H "Authorization: Bearer ${NEXUS_API_KEY}" ...

# Streaming
curl -sN "${NEXUS_URL}/v1/chat/completions" ... -d '{"stream": true, ...}'

# With Nexus headers (see response headers with -D -)
curl -s -D - "${NEXUS_URL}/v1/chat/completions" \
  -H "X-Workflow-ID: my-workflow" \
  -H "X-Agent-Role: architect" ...

# Health / readiness / liveness
curl -s "${NEXUS_URL}/health"
curl -s "${NEXUS_URL}/health/ready"
curl -s "${NEXUS_URL}/health/live"

# Metrics (Prometheus)
curl -s "${NEXUS_URL}/metrics"

# Diagnostics
curl -s "${NEXUS_URL}/api/synonyms/stats"
curl -s "${NEXUS_URL}/api/eval/stats"
curl -s "${NEXUS_URL}/api/circuit-breakers"
curl -s "${NEXUS_URL}/api/compression/stats"
```

## Docs

Full documentation: [nexus-gateway/nexus](https://github.com/nexus-gateway/nexus)

## License

Apache-2.0
