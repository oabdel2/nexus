# Nexus Security Audit Report

**Date:** 2025-07-07
**Scope:** Complete HTTP endpoint inventory, auth requirements, and middleware coverage

---

## 1. HTTP Endpoint Inventory

### Public Endpoints (No Auth Required)

| Endpoint | Method | Rate Limited | Input Validated | Notes |
|---|---|---|---|---|
| `/` | GET | Yes (global) | No (read-only) | Info/version endpoint |
| `/health` | GET | No (bypassed) | No | Health check — bypasses rate limit |
| `/health/live` | GET | No (bypassed) | No | Kubernetes liveness probe |
| `/health/ready` | GET | No (bypassed) | No | Kubernetes readiness probe |
| `/metrics` | GET | Yes (global) | No | Prometheus metrics |
| `/dashboard` | GET | Yes (global) | No | Dashboard HTML page |
| `/dashboard/events` | GET | Yes (global) | No | SSE event stream |
| `/dashboard/api/stats` | GET | Yes (global) | No | Dashboard stats API |
| `/webhooks/stripe` | POST | Yes (global) | Stripe signature verification | Webhook — auth via HMAC signature |

### Protected Endpoints (Auth Required)

| Endpoint | Method | Auth Mechanism | Rate Limited | Input Validated | Notes |
|---|---|---|---|---|---|
| `/v1/chat/completions` | POST | Bearer token / Billing API key | Yes | Yes (InputValidator + PromptGuard) | Main chat endpoint |
| `/v1/feedback` | POST | Bearer token / Billing API key | Yes | No | Feedback submission |

### Admin Endpoints (Auth + IP Allowlist)

| Endpoint | Method | Auth | IP Allowlist | Notes |
|---|---|---|---|---|
| `/api/synonyms/stats` | GET | OIDC/RBAC | Configurable | Synonym statistics |
| `/api/synonyms/candidates` | GET | OIDC/RBAC | Configurable | Synonym candidates |
| `/api/synonyms/learned` | GET | OIDC/RBAC | Configurable | Learned synonyms |
| `/api/synonyms/promote` | POST | OIDC/RBAC | Configurable | Promote synonym |
| `/api/synonyms/add` | POST | OIDC/RBAC | Configurable | Add synonym |
| `/api/circuit-breakers` | GET | OIDC/RBAC | No | Circuit breaker status |
| `/api/eval/stats` | GET | OIDC/RBAC | No | Eval statistics |
| `/api/compression/stats` | GET | OIDC/RBAC | No | Compression stats |

### Billing Admin Endpoints (Conditional — only when billing enabled)

| Endpoint | Method | Auth | Notes |
|---|---|---|---|
| `/api/admin/subscriptions` | GET | Billing API key | List all subscriptions |
| `/api/admin/keys/` | GET | Billing API key | List keys by user |
| `/api/admin/devices/` | GET | Billing API key | List devices by user |
| `/api/keys/generate` | POST | Billing API key | Generate new API key |
| `/api/keys/revoke` | POST | Billing API key | Revoke API key |
| `/api/usage` | GET | Billing API key | Usage statistics |

---

## 2. Middleware Chain Order

```
BillingAuth → Tracing → PanicRecovery → BodySizeLimit →
  RequestTimeout → SecurityHeaders → RequestID → RequestLogger →
  CORS → IPAllowlist → RateLimit → OIDC → InputValidator →
  PromptGuard → ErrorSanitizer → AuditLog
```

---

## 3. Security Headers

All responses include:
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `X-XSS-Protection: 1; mode=block`
- `Strict-Transport-Security: max-age=63072000; includeSubDomains; preload`
- `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Permissions-Policy: camera=(), microphone=(), geolocation=(), interest-cohort=()`
- `Cache-Control: no-store`

---

## 4. API Key Security

- Keys use `nxs_live_` / `nxs_test_` prefix format
- Keys are hashed with SHA-256 before storage (raw key never persisted)
- Validation checks: active status, expiry, subscription status
- Quota enforcement per key (monthly limits)
- Device fingerprinting and limit enforcement per user

---

## 5. Gaps Identified and Fixed

### GAP-1: HSTS Missing `preload` Directive
**Risk:** Browsers cannot preload HSTS without the `preload` directive.
**Fix:** Updated `Strict-Transport-Security` to include `preload` and increased max-age to 2 years (63072000s).

### GAP-2: Content-Security-Policy Too Permissive for API
**Risk:** `default-src 'self'` is overly permissive for a JSON API.
**Fix:** Changed to `default-src 'none'; frame-ancestors 'none'` — APIs serve no HTML/JS.

### GAP-3: Permissions-Policy Missing `interest-cohort`
**Risk:** FLoC tracking not explicitly disabled.
**Fix:** Added `interest-cohort=()` to Permissions-Policy header.

### GAP-4: No ErrorSanitizer Middleware
**Risk:** Error responses from handlers could leak internal details (stack traces, file paths, internal IPs) in production.
**Fix:** Added `ErrorSanitizer` middleware that intercepts 5xx responses and replaces bodies containing sensitive patterns with a generic error.

### GAP-5: Missing Webhook Replay Attack Tests
**Risk:** Timestamp tolerance enforcement was untested.
**Fix:** Added comprehensive tests for replay attacks, missing signatures, and malformed bodies.

### GAP-6: No Secret Scanning Prevention
**Risk:** Developers could accidentally commit API keys or secrets.
**Fix:** Added `.gitignore` patterns for common secret formats and a `scripts/secret-scan.sh` scanning script.

---

## 6. Verification

All changes verified with:
```sh
go build ./...
go test ./... -count=1
```
