#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────
# Nexus Gateway — curl Examples
#
# Nexus is a drop-in OpenAI-compatible proxy. These curl commands work
# against any Nexus deployment — just set NEXUS_URL and NEXUS_API_KEY.
# ─────────────────────────────────────────────────────────────────────────

NEXUS_URL="${NEXUS_URL:-https://nexus-gateway.example.com}"
NEXUS_API_KEY="${NEXUS_API_KEY:-your-api-key}"

# ── 1. Non-streaming chat completion ────────────────────────────────────
#
# Identical to an OpenAI request — model "auto" lets Nexus route
# intelligently, or specify a concrete model like "gpt-4o".

echo "=== 1. Non-Streaming Chat Completion ==="
curl -s "${NEXUS_URL}/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${NEXUS_API_KEY}" \
  -d '{
    "model": "auto",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user",   "content": "Explain the CAP theorem in two sentences."}
    ],
    "temperature": 0.7,
    "max_tokens": 256
  }' | python3 -m json.tool 2>/dev/null || cat

echo ""

# ── 2. Streaming chat completion ────────────────────────────────────────
#
# Add "stream": true to get Server-Sent Events (SSE), just like OpenAI.

echo "=== 2. Streaming Chat Completion ==="
curl -sN "${NEXUS_URL}/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${NEXUS_API_KEY}" \
  -d '{
    "model": "auto",
    "messages": [
      {"role": "user", "content": "Write a haiku about distributed systems."}
    ],
    "stream": true
  }'

echo ""
echo ""

# ── 3. Nexus routing headers ────────────────────────────────────────────
#
# Add optional headers to control Nexus routing:
#
#   X-Workflow-ID   — Groups related requests into a workflow for cost tracking.
#   X-Agent-Role    — Hints the router about task type: "architect", "researcher",
#                     "chat", "tester". Affects model/tier selection.
#   X-Team          — Team identifier for billing/cost attribution.
#   X-Budget        — Maximum USD budget for this workflow.
#   X-Request-ID    — Optional trace ID (auto-generated if omitted).
#
# Response headers returned by Nexus (visible with -i or -D):
#
#   X-Nexus-Model         — Model Nexus actually used (e.g. "gpt-4o").
#   X-Nexus-Tier          — Routing tier: "cheap", "mid", "premium".
#   X-Nexus-Provider      — Backend: "openai", "anthropic", "cache/L1", etc.
#   X-Nexus-Cost          — Estimated cost in USD (e.g. "0.003200").
#   X-Nexus-Cache         — Cache layer if served from cache ("L1", "L2a", "L2b").
#   X-Nexus-Confidence    — Response quality score (0–1).
#   X-Nexus-Workflow-ID   — Echoed workflow ID.
#   X-Nexus-Workflow-Step — Current step number in the workflow.

echo "=== 3. Nexus Routing Headers (verbose) ==="
curl -s -D - "${NEXUS_URL}/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${NEXUS_API_KEY}" \
  -H "X-Workflow-ID: design-session-42" \
  -H "X-Agent-Role: architect" \
  -H "X-Team: platform-eng" \
  -H "X-Budget: 2.50" \
  -d '{
    "model": "auto",
    "messages": [
      {"role": "system", "content": "You are a senior software architect."},
      {"role": "user",   "content": "Design a rate-limiting service for 10M RPM."}
    ],
    "max_tokens": 512
  }'

echo ""
echo ""

# ── 4. Multi-step workflow ───────────────────────────────────────────────
#
# Same workflow ID across multiple requests lets Nexus track cumulative
# cost and adapt tier selection based on remaining budget.

echo "=== 4. Multi-Step Workflow ==="

echo "--- Step 1: researcher ---"
curl -s "${NEXUS_URL}/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${NEXUS_API_KEY}" \
  -H "X-Workflow-ID: onboarding-flow-99" \
  -H "X-Agent-Role: researcher" \
  -H "X-Step-Number: 1" \
  -H "X-Budget: 5.00" \
  -d '{
    "model": "auto",
    "messages": [
      {"role": "user", "content": "List the top 3 Python web frameworks and their strengths."}
    ],
    "max_tokens": 512
  }' | python3 -m json.tool 2>/dev/null || cat

echo ""
echo "--- Step 2: architect ---"
curl -s "${NEXUS_URL}/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${NEXUS_API_KEY}" \
  -H "X-Workflow-ID: onboarding-flow-99" \
  -H "X-Agent-Role: architect" \
  -H "X-Step-Number: 2" \
  -H "X-Budget: 5.00" \
  -d '{
    "model": "auto",
    "messages": [
      {"role": "user", "content": "Given Django, FastAPI, and Flask — which is best for a high-traffic API?"}
    ],
    "max_tokens": 512
  }' | python3 -m json.tool 2>/dev/null || cat

echo ""
echo "--- Step 3: tester ---"
curl -s "${NEXUS_URL}/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${NEXUS_API_KEY}" \
  -H "X-Workflow-ID: onboarding-flow-99" \
  -H "X-Agent-Role: tester" \
  -H "X-Step-Number: 3" \
  -H "X-Budget: 5.00" \
  -d '{
    "model": "auto",
    "messages": [
      {"role": "user", "content": "Write a pytest test for a FastAPI health endpoint."}
    ],
    "max_tokens": 512
  }' | python3 -m json.tool 2>/dev/null || cat

echo ""

# ── 5. Submit feedback ───────────────────────────────────────────────────

echo "=== 5. Submit Feedback ==="
curl -s "${NEXUS_URL}/v1/feedback" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${NEXUS_API_KEY}" \
  -d '{
    "workflow_id": "onboarding-flow-99",
    "step": 1,
    "rating": 0.9,
    "comment": "Great framework comparison."
  }' | python3 -m json.tool 2>/dev/null || cat

echo ""

# ── 6. Health endpoints ─────────────────────────────────────────────────

echo "=== 6a. Health Check ==="
curl -s "${NEXUS_URL}/health" | python3 -m json.tool 2>/dev/null || cat
echo ""

echo "=== 6b. Readiness Probe ==="
curl -s "${NEXUS_URL}/health/ready" | python3 -m json.tool 2>/dev/null || cat
echo ""

echo "=== 6c. Liveness Probe ==="
curl -s "${NEXUS_URL}/health/live" | python3 -m json.tool 2>/dev/null || cat
echo ""

# ── 7. Prometheus metrics ────────────────────────────────────────────────

echo "=== 7. Prometheus Metrics (first 20 lines) ==="
curl -s "${NEXUS_URL}/metrics" | head -20
echo ""

# ── 8. Admin / diagnostics endpoints ────────────────────────────────────

echo "=== 8a. Synonym Stats ==="
curl -s "${NEXUS_URL}/api/synonyms/stats" | python3 -m json.tool 2>/dev/null || cat
echo ""

echo "=== 8b. Evaluation Stats ==="
curl -s "${NEXUS_URL}/api/eval/stats" | python3 -m json.tool 2>/dev/null || cat
echo ""

echo "=== 8c. Circuit Breaker Status ==="
curl -s "${NEXUS_URL}/api/circuit-breakers" | python3 -m json.tool 2>/dev/null || cat
echo ""

echo "=== 8d. Compression Stats ==="
curl -s "${NEXUS_URL}/api/compression/stats" | python3 -m json.tool 2>/dev/null || cat
echo ""

echo "=== 8e. Gateway Info ==="
curl -s "${NEXUS_URL}/" | python3 -m json.tool 2>/dev/null || cat
echo ""

echo "Done."
