# Nexus Load Tester

A self-contained Go load tester for the Nexus gateway — **zero external dependencies**.

## Quick Start

```bash
# Build the load tester
go build -o loadtest.exe ./load/

# Start Nexus (in another terminal)
go run ./cmd/nexus -config configs/nexus.test.yaml

# Run a scenario
./loadtest.exe -scenario steady -rps 50 -duration 30s
```

## Command Line Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-url` | `http://localhost:18080` | Nexus gateway base URL |
| `-rps` | `50` | Target requests per second |
| `-duration` | `30s` | Test duration |
| `-concurrent` | `10` | Number of concurrent workers |
| `-scenario` | `steady` | Which scenario to run (see below) |
| `-v` | `false` | Verbose per-request logging |

## Scenarios

### 1. `steady` — Constant Load
Sends requests at a fixed RPS for the full duration. Best for measuring baseline latency and throughput.

```bash
./loadtest.exe -scenario steady -rps 50 -duration 60s
```

**What to expect:** Stable P50/P95 latencies. If P99 spikes, the server is near capacity.

### 2. `ramp` — Ramp-Up
Linearly increases RPS from 0 to the target over the duration (10 steps). Useful for finding the saturation point where latency degrades.

```bash
./loadtest.exe -scenario ramp -rps 200 -duration 60s
```

**What to expect:** Latency should stay flat initially, then curve upward as capacity is reached.

### 3. `cache-warmup` — Cache Hit Rate
Sends the **same prompt** 100 times to measure how effectively the cache layer works. The first request is a miss; subsequent ones should hit L1/L2 cache.

```bash
./loadtest.exe -scenario cache-warmup
```

**What to expect:** First request ~slow (backend call), subsequent requests much faster. Cache hit rate should approach 99%.

### 4. `circuit-breaker` — Fault Tolerance
Targets the `fake-model` (routed to `fake-down` provider on port 19999, which doesn't exist). Verifies the circuit breaker opens after repeated failures.

```bash
./loadtest.exe -scenario circuit-breaker
```

**What to expect:** First ~5 requests fail with connection errors. After circuit opens, subsequent requests should fail faster (circuit open) or be routed to a fallback.

### 5. `concurrent-workflows` — Parallel Workflows
Launches N parallel multi-step workflows, each with unique `X-Workflow-ID` headers. Tests workflow tracking, budget management, and step sequencing under concurrency.

```bash
./loadtest.exe -scenario concurrent-workflows -concurrent 20
```

**What to expect:** Each workflow runs independently. Latency per step should be consistent across workflows.

### 6. `mixed` — Realistic Traffic
Simulates production-like traffic distribution:
- **60%** simple prompts (short, factual)
- **30%** medium prompts (technical explanations)
- **10%** complex prompts (architecture/design questions)

```bash
./loadtest.exe -scenario mixed -rps 100 -duration 60s
```

**What to expect:** Tier distribution in output should show routing to different model tiers based on complexity.

### 7. `burst` — Burst Traffic
Two quiet periods (5s each) followed by sudden bursts of `3 × RPS` requests. Tests queue handling, rate limiting, and recovery.

```bash
./loadtest.exe -scenario burst -rps 50 -concurrent 20
```

**What to expect:** First burst may show higher latency. Second burst tests recovery. Rate limiting (429s) may appear if burst exceeds configured limits.

## Output Metrics

Each scenario reports:

| Metric | Description |
|--------|-------------|
| Total requests | Number of requests sent |
| Successful (2xx) | Requests with 200-299 status |
| Errors | Connection/IO errors |
| Success rate | Percentage of successful requests |
| Achieved RPS | Actual requests per second |
| P50/P90/P95/P99 | Latency percentiles |
| Max/Min/Avg | Latency extremes and average |
| Cache hit rate | From `X-Nexus-Cache` header |
| Tier distribution | From `X-Nexus-Tier` header |
| Status codes | HTTP status code breakdown |

## Interpreting Results

### Healthy Baseline
- P95 latency < 2× P50
- Error rate < 1%
- Cache hit rate > 80% for repeated prompts
- Tier distribution matches complexity weights

### Warning Signs
- P99 > 10× P50 → tail latency issues
- Error rate > 5% → capacity or stability problem
- Cache hit rate < 50% on cache-warmup → cache misconfiguration
- All requests routed to same tier → router not classifying

### Prerequisites
- **Nexus** running on the target URL (default: `localhost:18080`)
- **Ollama** running on `localhost:11434` with `qwen2.5:1.5b` for actual LLM responses
- Without Ollama, requests to the LLM will fail (but cache/circuit-breaker scenarios still work for cached entries)

## Saving Results

Redirect output to a file for later comparison:

```bash
./loadtest.exe -scenario steady -rps 50 -duration 30s 2>&1 | tee load/results/run-$(date +%Y%m%d-%H%M).txt
```

Baseline template is in `load/results/baseline.md`.
