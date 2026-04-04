# Contributing to Nexus

Thank you for your interest in contributing to Nexus! This guide will help you get started.

## Table of Contents

- [Development Environment Setup](#development-environment-setup)
- [Code Style](#code-style)
- [Pull Request Process](#pull-request-process)
- [Reporting Issues](#reporting-issues)
- [Code of Conduct](#code-of-conduct)

## Development Environment Setup

### Prerequisites

- **Go 1.22+** — [Download](https://go.dev/dl/)
- **Docker & Docker Compose** (for running the full stack)
- **Git**

### Getting Started

```bash
# Clone the repository
git clone https://github.com/Nexus-Research/nexus.git
cd nexus

# Download dependencies
go mod download

# Build the binary
go build -o nexus ./cmd/nexus

# Run tests
go test ./...

# Run benchmarks
go test ./benchmarks/... -bench=.
```

### Running Locally

```bash
# Start with default configuration
./nexus --config configs/config.yaml

# Or use Docker Compose for the full stack (Prometheus, Grafana)
docker-compose up --build
```

## Code Style

- **Format your code** with `gofmt` before committing.
- **Lint your code** with [golangci-lint](https://golangci-lint.run/):
  ```bash
  golangci-lint run ./...
  ```
- Keep functions focused and well-documented.
- Write meaningful commit messages.
- Add tests for new functionality.

## Pull Request Process

1. **Fork** the repository and create a feature branch from `main`:
   ```bash
   git checkout -b feature/my-new-feature
   ```
2. **Make your changes** — keep commits atomic and well-described.
3. **Run tests and linting** to ensure nothing is broken:
   ```bash
   go test ./...
   golangci-lint run ./...
   ```
4. **Push** your branch and open a Pull Request against `main`.
5. Fill out the **PR template** with a description of your changes.
6. Wait for **code review** — address any feedback promptly.
7. Once approved, a maintainer will merge your PR.

## Reporting Issues

We use GitHub Issues to track bugs and feature requests. Please use the appropriate template:

- **[Bug Report](.github/ISSUE_TEMPLATE/bug_report.md)** — for reporting bugs
- **[Feature Request](.github/ISSUE_TEMPLATE/feature_request.md)** — for suggesting new features

Before opening a new issue, please search existing issues to avoid duplicates.

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](https://www.contributor-covenant.org/version/2/1/code_of_conduct/).
By participating, you agree to uphold a welcoming, inclusive, and harassment-free environment.

---

Thank you for helping make Nexus better!
