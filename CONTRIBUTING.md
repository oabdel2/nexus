# Contributing to Nexus

Welcome! We're excited you want to contribute to Nexus. Whether it's a bug fix, a new feature, better docs, or improved test coverage — every contribution matters. This guide will get you up and running.

---

## Quick Start for Contributors

```bash
git clone https://github.com/oabdel2/nexus.git
cd nexus
go build ./...
go test -race ./...
```

That's it. If all tests pass, you're ready to contribute. Nexus has **zero external build tools** — just Go 1.23+.

---

## Project Structure

```
nexus/
├── cmd/nexus/          # CLI entrypoint (serve, validate, inspect, doctor, version)
├── internal/
│   ├── gateway/        # Core HTTP server, middleware chain, request handler
│   ├── router/         # Complexity classifier, tier mapping, adaptive routing
│   ├── cache/          # 3-layer cache: L1 exact, L2a BM25, L2b semantic
│   ├── provider/       # LLM provider adapters (OpenAI, Anthropic, Ollama)
│   ├── compress/       # Prompt compression (whitespace, code blocks, history)
│   ├── eval/           # Response confidence scoring (6-signal heuristic)
│   ├── security/       # Prompt injection guard, input validation
│   ├── billing/        # Usage tracking, Stripe webhooks, API key management
│   ├── auth/           # Authentication, OIDC, RBAC
│   ├── telemetry/      # Prometheus metrics, W3C distributed tracing
│   ├── events/         # Event bus for inter-package communication
│   ├── experiment/     # A/B testing framework
│   ├── plugin/         # Plugin system for extensibility
│   ├── workflow/        # Multi-step workflow tracking (X-Workflow-ID)
│   ├── mcp/            # Model Context Protocol server
│   ├── dashboard/      # Built-in analytics dashboard
│   ├── notification/   # Alert system
│   ├── storage/        # Persistent storage layer
│   └── config/         # Configuration loading and validation
├── configs/            # Example YAML configuration files
├── benchmarks/         # Performance benchmarks (go test -bench)
├── tests/              # Integration and E2E test scaffolding
├── site/               # Website and documentation HTML
├── sdk/                # Client SDKs
├── deploy/             # Helm charts, Kubernetes manifests
├── monitoring/         # Grafana dashboards, Prometheus configs
└── docs/               # Architecture docs, research, growth strategy
```

---

## How to Contribute

### Bug Reports

Open a [Bug Report](https://github.com/oabdel2/nexus/issues/new?template=bug_report.md) and include:

- **Nexus version** (`nexus version`)
- **Go version** (`go version`)
- **OS and architecture**
- **Steps to reproduce** — ideally a `curl` command or minimal config
- **Expected vs. actual behavior**
- **Relevant logs** — run with `-log-level debug` to capture detail

### Feature Requests

Open a [Feature Request](https://github.com/oabdel2/nexus/issues/new?template=feature_request.md) and describe:

- **The problem** you're trying to solve
- **Your proposed solution** with example usage
- **Alternatives** you've considered

### Pull Requests

1. **Branch naming:** `feature/description`, `fix/description`, or `docs/description`
2. **Commit format:** Use clear, descriptive messages. One logical change per commit.
   ```
   Add Gemini provider adapter

   Implements the Google Gemini provider following the same pattern
   as anthropic.go. Includes unit tests and config validation.
   ```
3. **Test requirements:** Every PR must pass:
   - `go build ./...`
   - `go test -race ./...`
   - `go vet ./...`
4. **No external dependencies.** Nexus has exactly one dependency (`gopkg.in/yaml.v3`). If your change requires a new dependency, open an issue to discuss it first.
5. **Add tests** for any new functionality. Aim for >70% coverage on new code.

### Code Style

- Run `go vet ./...` — must report zero issues
- Run `gofmt` — all code must be formatted
- Keep functions focused; avoid functions over 100 lines
- No TODO/FIXME/HACK comments — fix it or file an issue
- Test file naming: `foo_test.go` alongside `foo.go`
- Every exported function needs a doc comment

---

## Good First Issues

Looking for a place to start? Here are beginner-friendly tasks:

| Issue | Description | Skills |
|-------|-------------|--------|
| **Add Google Gemini provider adapter** | Follow the pattern in `internal/provider/anthropic.go` to add Gemini support | Go, HTTP APIs |
| **Add `nexus benchmark` CLI command** | Expose the benchmark suite via a CLI subcommand for quick perf checks | Go, CLI |
| **Improve gateway test coverage to 70%** | The gateway package is at ~53% coverage — add tests for streaming, cascade, and error paths | Go, testing |
| **Generate OpenAPI spec from endpoint definitions** | Auto-generate a Swagger/OpenAPI spec from the registered routes | Go, OpenAPI |
| **Create example plugins** | Build example plugins (logging, custom routing) using the plugin system in `internal/plugin/` | Go |

Check [issues labeled `good first issue`](https://github.com/oabdel2/nexus/labels/good%20first%20issue) for the latest list.

---

## Development Workflow

```
1. Fork the repo on GitHub
2. Clone your fork locally
   git clone https://github.com/YOUR_USERNAME/nexus.git
   cd nexus

3. Create a feature branch
   git checkout -b feature/my-change

4. Make your changes and write tests

5. Run the full check suite
   go build ./...
   go vet ./...
   go test -race ./...

6. Commit and push
   git add .
   git commit -m "Add my feature"
   git push origin feature/my-change

7. Open a Pull Request against main
   Fill out the PR template. Link any related issues.

8. Address review feedback

9. Merge! 🎉
```

### CI Requirements

Every PR automatically runs:

- **Build:** `go build ./...`
- **Vet:** `go vet ./...`
- **Race detector:** `go test -race ./...`
- **All 923 tests** must pass

### Running Locally

```bash
# Start Nexus with the example config
export OPENAI_API_KEY=sk-...
go run ./cmd/nexus serve -config configs/nexus.yaml

# Or use Docker Compose for the full stack (Prometheus + Grafana)
docker-compose up --build
```

### Running Benchmarks

```bash
go test ./benchmarks/... -bench=. -benchmem -count=3
```

---

## Architecture Overview

For a deep dive into Nexus internals, see the [architecture documentation](docs/).

**Request flow at a glance:**

```
Client → Auth → Validate → Compress → Cache Lookup → Classify → Route → Cascade → Provider → Eval → Cache Store → Response
```

Key design decisions:
- **Single binary:** No runtime dependencies, no sidecar containers
- **1 external dep:** Entire HTTP server, BM25 engine, TF-IDF classifier, circuit breaker, rate limiter, and Prometheus exporter built on Go stdlib
- **Middleware chain:** 17 composable middleware functions — each can be enabled/disabled via config
- **Event-driven:** Inter-package communication via an event bus (no circular imports)

---

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](https://www.contributor-covenant.org/version/2/1/code_of_conduct/). By participating, you agree to uphold a welcoming, inclusive, and harassment-free environment.

---

Thank you for helping make Nexus better! If you have questions, open a [Discussion](https://github.com/oabdel2/nexus/discussions) or reach out in an issue.
