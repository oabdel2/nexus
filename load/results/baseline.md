# Nexus Load Test Baseline

**Date:** _[YYYY-MM-DD]_
**Commit:** _[git SHA]_
**Environment:**
- OS: _[e.g., Windows 11 / Linux]_
- CPU: _[e.g., AMD Ryzen 9 / Apple M2]_
- RAM: _[e.g., 32 GB]_
- Go version: _[e.g., 1.26.1]_
- Ollama model: qwen2.5:1.5b
- Nexus config: `configs/nexus.test.yaml`

---

## Scenario 1: Steady State (50 RPS, 30s)

| Metric | Value |
|--------|-------|
| Total requests | |
| Successful (2xx) | |
| Success rate | |
| Achieved RPS | |
| P50 latency | |
| P90 latency | |
| P95 latency | |
| P99 latency | |
| Max latency | |
| Cache hit rate | |
| Tier distribution | |

---

## Scenario 2: Ramp Up (0 → 100 RPS, 60s)

| Metric | Value |
|--------|-------|
| Total requests | |
| Successful (2xx) | |
| Success rate | |
| P50 latency | |
| P95 latency | |
| P99 latency | |
| Saturation point (RPS) | |

---

## Scenario 3: Cache Warmup (100 identical prompts)

| Metric | Value |
|--------|-------|
| Total requests | |
| Cache hit rate | |
| First request latency | |
| Avg cached latency | |
| P95 cached latency | |
| Cache source breakdown | |

---

## Scenario 4: Circuit Breaker (fake-model, 60 requests)

| Metric | Value |
|--------|-------|
| Total requests | |
| Requests before CB opened | |
| Avg latency (pre-open) | |
| Avg latency (post-open) | |
| Status code distribution | |

---

## Scenario 5: Concurrent Workflows (10 × 5 steps)

| Metric | Value |
|--------|-------|
| Total requests | |
| Successful (2xx) | |
| P50 latency | |
| P95 latency | |
| Unique workflows | |
| Avg steps/workflow | |

---

## Scenario 6: Mixed Traffic (60/30/10 split, 50 RPS, 30s)

| Metric | Value |
|--------|-------|
| Total requests | |
| Success rate | |
| P50 latency | |
| P95 latency | |
| Tier: cheap | |
| Tier: mid | |
| Tier: premium | |
| Tier: cached | |

---

## Scenario 7: Burst (2 × 150 req bursts)

| Metric | Value |
|--------|-------|
| Total requests | |
| Success rate | |
| Burst 1 — P95 latency | |
| Burst 2 — P95 latency | |
| Rate limited (429s) | |

---

## Notes

_Record any anomalies, configuration changes, or observations here._

---

## Comparison

| Metric | Previous | Current | Delta |
|--------|----------|---------|-------|
| Steady P50 | | | |
| Steady P95 | | | |
| Cache hit rate | | | |
| Max RPS (ramp) | | | |
