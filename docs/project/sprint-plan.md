# Nexus — Prioritized Sprint Plan

> **Created:** 2025-07-18
> **Author:** Sprint Prioritizer (first-time review)
> **Basis:** reality-check.md audit, verified-metrics.md, api-contracts.md, `go test -cover ./...` output, roadmap.md, nexus.yaml config
> **Planning horizon:** 8 weeks (4 × 2-week sprints)
> **Team size:** 1 solo founder

---

## Current State Summary

| Signal | Value |
|--------|-------|
| Tests | 923 passing, 0 failures, 0 race conditions |
| Gateway coverage | **53.5%** ← critical path, lowest coverage |
| Storage coverage | **32.7%** ← also dangerously low |
| Router coverage | 94.8% ✅ |
| MCP coverage | 98.1% ✅ |
| Audit grade | **B-** ("not production ready") |
| Biggest tech debt | `handleChat` at 592 lines |
| Headline features disabled | Cascade (now enabled in config), Adaptive routing (disabled) |
| Unsubstantiated claims | "40-70% cost savings", "20-35% compression savings" |
| Release status | No tagged release; `.goreleaser.yml` exists but unused |
| CI | Build + lint + security + Docker + Trivy — but no coverage reporting |

---

## 1. RICE Scoring Matrix

**Formula:** RICE = (Reach × Impact × Confidence) / Effort

| # | Feature | Reach (1-10) | Impact (1-5) | Confidence | Effort (pw) | RICE | Rationale |
|---|---------|:---:|:---:|:---:|:---:|---:|-----------|
| 1 | **v0.1.0 tagged release** | 10 | 5 | 1.0 | 0.5 | **100.0** | Unlocks everything: adoption, downloads, credibility. goreleaser exists. |
| 2 | **Contributor onboarding docs** | 6 | 3 | 1.0 | 0.5 | **36.0** | CONTRIBUTING.md exists but needs architecture guide, good-first-issues. Multiplier on community growth. |
| 3 | **Config validation improvements** | 8 | 2 | 1.0 | 0.5 | **32.0** | Every user hits config first. Validation exists at 79.7% — add better error messages, schema docs. |
| 4 | **Load testing with real benchmarks** | 8 | 4 | 1.0 | 1 | **32.0** | Validates production claims. Audit flagged "no load testing results". Establishes P50/P95/P99 baseline. |
| 5 | **Discord community** | 5 | 3 | 1.0 | 0.5 | **30.0** | Feedback loop for early adopters. Trivial to set up, high ongoing value. |
| 6 | **GitHub Actions CI improvements** | 5 | 3 | 1.0 | 0.5 | **30.0** | Add coverage reporting, badge automation, Go 1.23+ matrix. Currently no coverage upload. |
| 7 | **Blog launch post** | 8 | 4 | 0.8 | 1 | **25.6** | Primary acquisition channel. Must be honest (audit says fix claims first). |
| 8 | **Domain + hosted version** | 8 | 4 | 0.8 | 1 | **25.6** | Railway config exists. Removes "must self-host" friction. Demo instance for try-before-install. |
| 9 | **Gateway coverage → 80%** | 8 | 5 | 0.8 | 2 | **16.0** | Audit's #1 technical concern. Critical path at 53.5%. Streaming, cascade, and error paths undertested. |
| 10 | **Rate limiting per API key** | 6 | 3 | 0.8 | 1 | **14.4** | Rate limiter exists but is global. Per-key limits needed for multi-tenant hosted version. |
| 11 | **OpenAPI/Swagger spec** | 7 | 3 | 1.0 | 2 | **10.5** | 51 API contract tests exist — spec can be derived. Enables SDK generation and external tooling. |
| 12 | **MCP server enhancements** | 4 | 3 | 0.8 | 1 | **9.6** | 98.1% coverage, 7 tools. Add resource endpoints, better agent workflow support. Differentiator. |
| 13 | **Stripe integration (production)** | 3 | 4 | 0.8 | 2 | **4.8** | Billing code exists at 75.8% coverage. Needs production Stripe testing, webhook hardening. |
| 14 | **Plugin examples** | 3 | 2 | 0.8 | 1 | **4.8** | Plugin system at 86.5% coverage. 2-3 examples make it tangible for contributors. |
| 15 | **More providers (Gemini, Bedrock, Azure)** | 7 | 3 | 0.8 | 4 | **4.2** | Provider interface is clean. Each new provider ≈1 week. Broadens appeal to GCP/AWS users. |
| 16 | **Dashboard improvements** | 5 | 2 | 0.8 | 2 | **4.0** | Dashboard at 89.1% coverage. UX polish, real-time charts, cost attribution view. |
| 17 | **Legal documents (ToS, Privacy)** | 3 | 3 | 0.8 | 2 | **3.6** | Required before accepting payments. Template-based, needs legal review (~$500-1500). |
| 18 | **Streaming cascade routing** | 6 | 4 | 0.5 | 4 | **3.0** | Complex: SSE + cascade + confidence eval intersection. Low confidence it ships clean in 4 weeks. |

### RICE Ranking (sorted)

```
 #  Feature                          RICE    Sprint
 1. v0.1.0 tagged release           100.0    Sprint 1
 2. Contributor onboarding docs      36.0    Sprint 1
 3. Config validation improvements   32.0    Sprint 1
 4. Load testing with benchmarks     32.0    Sprint 2
 5. Discord community                30.0    Sprint 1
 6. CI improvements                  30.0    Sprint 1
 7. Blog launch post                 25.6    Sprint 2
 8. Domain + hosted version          25.6    Sprint 2
 9. Gateway coverage → 80%           16.0    Sprint 2
10. Rate limiting per API key        14.4    Sprint 3
11. OpenAPI/Swagger spec             10.5    Sprint 3
12. MCP server enhancements           9.6    Sprint 3
13. Stripe integration (production)   4.8    Sprint 4
14. Plugin examples                   4.8    Sprint 3
15. More providers                    4.2    Sprint 4
16. Dashboard improvements            4.0    Sprint 4
17. Legal documents                   3.6    Sprint 4
18. Streaming cascade routing         3.0    Sprint 4+
```

---

## 2. MoSCoW Classification (for v0.1.0 launch)

### Must Have — launch is blocked without these

| Item | Why |
|------|-----|
| v0.1.0 tagged release | No release = no adoption. goreleaser is configured, just needs a tag + changelog. |
| CI improvements (coverage reporting) | Audit graded coverage gaps as #1 issue. Must be visible and tracked. |
| Gateway coverage ≥ 70% | Audit says 53.5% on the critical path is unacceptable. Minimum viable: 70%. |
| Load testing baseline | Audit says "no load testing results" — can't claim production-ready without P50/P95/P99 numbers. |
| Config validation improvements | First thing new users hit. Bad config errors = immediate churn. |

### Should Have — strongly desired for credible launch

| Item | Why |
|------|-----|
| Blog launch post | Primary awareness driver. Honest post that addresses audit findings. |
| Domain + hosted demo | Removes self-hosting friction. Railway config exists. |
| Discord community | Feedback channel. Takes 30 minutes to set up, creates ongoing value. |
| Contributor onboarding docs | Multiplier on community growth. Architecture guide + good-first-issues. |
| Rate limiting per API key | Required for hosted multi-tenant version. |

### Could Have — nice for launch, not blocking

| Item | Why |
|------|-----|
| OpenAPI/Swagger spec | Enables SDK generation. Can ship post-launch. |
| MCP server enhancements | Differentiator but niche. Current 7 tools are functional. |
| Plugin examples | Plugin system works, examples are a convenience. |
| Dashboard improvements | Dashboard exists and works. Polish is a nice-to-have. |

### Won't Have (for v0.1.0 — deferred to v0.2.0+)

| Item | Why |
|------|-----|
| More providers (Gemini, Bedrock, Azure) | 4 person-weeks for low immediate reach. OpenAI + Anthropic + Ollama cover 80%+ of users. |
| Streaming cascade routing | Low confidence (0.5), high effort (4 pw). Cascade works for non-streaming. Ship streaming cascade in v0.2.0. |
| Stripe integration (production) | Revenue isn't the v0.1.0 goal. Billing code at 75.8% can wait for Phase 3. |
| Legal documents | Only needed when accepting payments. Premature for open-source launch. |

---

## 3. Sprint Plan (4 × 2-week sprints)

### Sprint 1: "Ship It" (Weeks 1-2)

**Goal:** Produce a tagged v0.1.0 release that a developer can discover, clone, and run in under 5 minutes. Fix the CI pipeline so every PR shows coverage.

**Committed items:**

| Task | RICE | Effort | Acceptance Criteria |
|------|------|--------|---------------------|
| v0.1.0 tagged release | 100.0 | 2d | `goreleaser` produces binaries for linux/mac/windows. GitHub Release page has changelog, install instructions, and SHA checksums. |
| CI improvements | 30.0 | 2d | Coverage uploaded to Codecov/Coveralls. Badge in README shows real %. Go matrix updated to 1.23+1.24. Coverage report on every PR. |
| Config validation improvements | 32.0 | 2d | `nexus validate` catches 10+ common misconfigurations with actionable error messages. Tested. |
| Discord community setup | 30.0 | 0.5d | Server live with #general, #help, #contributing, #announcements channels. Welcome bot. Link in README. |
| Contributor onboarding docs | 36.0 | 3d | Architecture overview doc (package diagram, request flow). "Good first issue" labels on 10 issues. CONTRIBUTING.md expanded with dev setup, test commands, PR checklist. |

**Stretch items:**
- Fix README claims flagged by audit (7-layer → 3-layer, remove "40-70%" unsubstantiated claim)
- Add `nexus doctor` output to CI as a smoke test

**Capacity:** ~10 working days (solo founder)
**Committed load:** ~9.5 days (95% — tight but achievable since most items are docs/config)

**Success criteria:**
- [ ] `go install github.com/nexus-gateway/nexus@v0.1.0` works
- [ ] CI shows coverage badge ≥ 53% (current baseline, not yet improved)
- [ ] `nexus validate configs/nexus.yaml` produces clean output
- [ ] Discord invite link works
- [ ] A developer unfamiliar with the project can set up a dev environment using only the contributor docs

**Risk:**
| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| goreleaser config is stale/broken | Medium | Test goreleaser locally with `--snapshot` before tagging |
| Coverage service integration takes too long | Low | Fall back to `go test -cover` output parsed in CI + badge shield.io |

---

### Sprint 2: "Prove It" (Weeks 3-4)

**Goal:** Validate every production claim with real data. Get gateway coverage above 70%. Launch the blog post and hosted demo so people can try Nexus without installing anything.

**Committed items:**

| Task | RICE | Effort | Acceptance Criteria |
|------|------|--------|---------------------|
| Load testing with real benchmarks | 32.0 | 4d | vegeta/wrk load tests in `load/`. Published results: P50, P95, P99 latency, max RPS, memory under load. Results in `docs/benchmarks.md`. |
| Gateway coverage → 70%+ | 16.0 | 4d | `go test -cover ./internal/gateway/` reports ≥ 70%. New tests for streaming path, cascade path, error paths, and the `handleChat` function's untested branches. |
| Blog launch post | 25.6 | 1d | Published post with honest framing. Mentions real benchmarks (from load test). Links to GitHub, Discord, demo. |
| Domain + hosted demo | 25.6 | 1d | nexus-gateway.dev (or similar) live on Railway. `/health` returns 200. Demo page shows how to send requests. |

**Stretch items:**
- Gateway coverage to 75% (additional edge cases)
- Begin `handleChat` decomposition (extract streaming, cascade, cache phases into sub-functions)
- Submit launch post to Hacker News

**Capacity:** ~10 working days
**Committed load:** ~10 days (100% — zero slack)

**Success criteria:**
- [ ] Load test proves ≥ 500 RPS on single node with P99 < 50ms (cached)
- [ ] Gateway coverage ≥ 70% (up from 53.5%)
- [ ] Blog post published and shared on ≥ 3 channels (HN, Reddit, X)
- [ ] Hosted demo at public URL, health check green

**Risk:**
| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Load test reveals a bottleneck | Medium | Budget 1 extra day for hotfixes. Likely candidates: mutex contention in cache, large request body parsing. |
| Gateway test coverage plateau at ~65% | Medium | Focus on highest-value untested paths (streaming, cascade). Accept 70% as MVP, push to 80% in Sprint 4. |
| Railway deployment issues | Low | Fallback: Fly.io or Render (both have free tiers with Docker support). |

---

### Sprint 3: "Developer Experience" (Weeks 5-6)

**Goal:** Make Nexus a pleasure to integrate with. Ship the OpenAPI spec, per-key rate limiting, and MCP enhancements that differentiate Nexus from LiteLLM/OpenRouter.

**Committed items:**

| Task | RICE | Effort | Acceptance Criteria |
|------|------|--------|---------------------|
| Rate limiting per API key | 14.4 | 3d | Rate limiter supports per-key limits configurable in YAML. Billing tier sets rate limit. Returns standard `429` with `Retry-After` header. Tested. |
| OpenAPI/Swagger spec | 10.5 | 3d | `docs/openapi.yaml` validates against OpenAPI 3.1. Covers all 20+ endpoints from api-contracts.md. Served at `/docs` or `/swagger`. |
| MCP server enhancements | 9.6 | 2d | Add `resources/list` and `prompts/list` MCP methods. Document MCP integration for Claude Desktop, Cursor, etc. |
| Plugin examples | 4.8 | 2d | 2-3 example plugins in `examples/plugins/`: rate-limit-logger, cost-alert webhook, custom-router. Each with README. |

**Stretch items:**
- Begin OpenAPI → SDK auto-generation (Go client from spec)
- Add request/response examples to OpenAPI spec
- Storage coverage improvement (32.7% → 50%+)

**Capacity:** ~10 working days
**Committed load:** ~10 days

**Success criteria:**
- [ ] `curl -H "X-API-Key: key1" ...` gets rate-limited independently from `key2`
- [ ] OpenAPI spec passes `swagger-cli validate docs/openapi.yaml`
- [ ] MCP `tools/list` returns ≥ 9 tools (up from 7)
- [ ] Each plugin example has a working test

**Risk:**
| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| OpenAPI spec takes longer than expected (20+ endpoints) | Medium | Start with core endpoints (chat, health, inspect). Add admin endpoints in Sprint 4. |
| Per-key rate limiting requires billing system changes | Low | Decouple: use API key header directly, no billing dependency. Billing integration comes in Sprint 4. |

---

### Sprint 4: "Revenue & Hardening" (Weeks 7-8)

**Goal:** Lay the foundation for revenue. Harden what we shipped. Push gateway coverage toward 80%. Start the first non-core provider (Gemini).

**Committed items:**

| Task | RICE | Effort | Acceptance Criteria |
|------|------|--------|---------------------|
| Stripe integration (production-ready) | 4.8 | 3d | End-to-end: signup → Stripe checkout → API key issued → request succeeds → usage tracked. Webhook signature verification tested against Stripe CLI. |
| Gateway coverage → 80% | — | 3d | Coverage hits 80%. `handleChat` decomposed into ≤ 3 sub-functions. Each sub-function individually tested. |
| More providers: Gemini | 4.2* | 2d | `provider/gemini.go` implements Gemini API. Tested with mocks. Configurable in nexus.yaml. (*partial: 1 of 3 providers) |
| Legal documents (ToS, Privacy) | 3.6 | 1d | Template-based ToS + Privacy Policy in `docs/legal/`. Linked from hosted version footer. |
| Dashboard improvements | 4.0 | 1d | Cost attribution per workflow. Real-time request chart. Mobile-responsive layout. |

**Stretch items:**
- Streaming cascade routing (begin investigation, design doc)
- Bedrock provider (2nd provider)
- Storage coverage 32.7% → 55%
- `handleChat` full decomposition (592 lines → 3 focused functions under 150 lines each)

**Capacity:** ~10 working days
**Committed load:** ~10 days

**Success criteria:**
- [ ] Stripe test-mode checkout completes end-to-end
- [ ] Gateway coverage ≥ 80% per `go test -cover`
- [ ] Gemini provider passes unit tests with mock responses
- [ ] ToS and Privacy Policy published at `/legal/terms` and `/legal/privacy`

**Risk:**
| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Stripe account approval delayed | Medium | Apply in Sprint 1. Backup: LemonSqueezy (no approval needed). |
| handleChat decomposition introduces regressions | Medium | Existing 53.5% coverage + new Sprint 2 tests form safety net. Refactor in small PRs. |
| Gemini API differences cause adapter complexity | Low | Gemini has an OpenAI-compatible mode. Use that first, native adapter later. |

---

## 4. Dependencies & Critical Path

```
SPRINT 1                          SPRINT 2                     SPRINT 3                     SPRINT 4
(Ship It)                         (Prove It)                   (Developer Experience)        (Revenue & Hardening)

┌─────────────────┐
│ ★ v0.1.0 RELEASE│─────────────────────────────────────────────────────────────────────────────────────┐
│   goreleaser    │                                                                                     │
└────────┬────────┘                                                                                     │
         │                                                                                              │
         │ unblocks                                                                                     │
         ▼                                                                                              │
┌─────────────────┐          ┌─────────────────┐                                                        │
│ CI improvements │─────────▶│ Coverage → 70%+ │                                                        │
│ (coverage badge)│          │ (gateway tests) │──────────────────────────────────────┐                  │
└─────────────────┘          └────────┬────────┘                                     │                  │
                                      │                                              ▼                  │
┌─────────────────┐                   │                                     ┌─────────────────┐         │
│ Config validation│                  │                                     │ Coverage → 80%  │         │
│ improvements    │                   │                                     │ + handleChat    │         │
└─────────────────┘                   │                                     │   decomposition │         │
                                      │                                     └─────────────────┘         │
┌─────────────────┐          ┌────────▼────────┐                                                        │
│ Discord setup   │─────────▶│ Blog launch post│                                                        │
│                 │          │ (needs metrics) │                                                        │
└─────────────────┘          └─────────────────┘                                                        │
                                                                                                        │
┌─────────────────┐          ┌─────────────────┐          ┌─────────────────┐          ┌────────────────┤
│ Contributor docs│─────────▶│ Domain + hosted │─────────▶│ Rate limit/key  │─────────▶│ Stripe prod   │
│                 │          │ demo version    │          │ (hosted needs   │          │ (needs hosted │
└─────────────────┘          └─────────────────┘          │  per-key limits)│          │  + rate limit)│
                                                          └─────────────────┘          └───────────────┘
                             ┌─────────────────┐
                             │ Load testing    │          ┌─────────────────┐          ┌───────────────┐
                             │ (vegeta/wrk)    │          │ OpenAPI spec    │─────────▶│ Gemini provdr │
                             └─────────────────┘          └─────────────────┘          └───────────────┘
                                                                                       (uses OpenAI-
                                                          ┌─────────────────┐           compat mode)
                                                          │ MCP enhancements│
                                                          └─────────────────┘
                                                                                      ┌───────────────┐
                                                          ┌─────────────────┐         │ Legal docs    │
                                                          │ Plugin examples │         │ (ToS/Privacy) │
                                                          └─────────────────┘         └───────────────┘

★ CRITICAL PATH:
  v0.1.0 Release → CI/Coverage → Gateway 70% → Blog Post → Domain/Hosted → Rate Limit → Stripe

PARALLEL TRACKS:
  Track A: Quality    — CI → Coverage 70% → Coverage 80% + handleChat decomp
  Track B: Community  — Discord → Blog → Contributor Docs
  Track C: DevEx      — Config Validation → OpenAPI → Rate Limit → Stripe
  Track D: Providers  — (independent) Gemini in Sprint 4
```

---

## 5. Success Metrics per Sprint

### Sprint 1 — "Ship It"

| Metric | Target | How to Measure |
|--------|--------|----------------|
| Release downloadable | v0.1.0 on GitHub Releases | `go install ...@v0.1.0` succeeds |
| CI health | Green on main, coverage badge live | GitHub Actions + badge URL |
| Config UX | 0 unhandled config errors on minimal/full configs | `nexus validate` exit code |
| Community seed | Discord link in README, 5+ members by EOW2 | Discord member count |
| Contributor readiness | Architecture doc + 10 labeled issues | Issue count with `good-first-issue` |

### Sprint 2 — "Prove It"

| Metric | Target | How to Measure |
|--------|--------|----------------|
| Throughput | ≥ 500 RPS (cached, single node) | vegeta load test report |
| Latency (cached) | P50 < 10ms, P95 < 30ms, P99 < 50ms | vegeta percentile output |
| Gateway coverage | ≥ 70% (up from 53.5%) | `go test -cover ./internal/gateway/` |
| Launch reach | Blog post on HN, Reddit, X | Link + comment count |
| Demo availability | Hosted version returns 200 on `/health` | curl health check |

### Sprint 3 — "Developer Experience"

| Metric | Target | How to Measure |
|--------|--------|----------------|
| API completeness | OpenAPI spec covers 100% of public endpoints | spec validation + manual check |
| Multi-tenancy | Per-key rate limiting works | Integration test: 2 keys, different limits |
| MCP breadth | ≥ 9 MCP tools (up from 7) | `tools/list` response count |
| Plugin ecosystem | 3 working examples with tests | `go test ./examples/plugins/...` |
| External contributions | ≥ 1 external PR or issue | GitHub contributor graph |

### Sprint 4 — "Revenue & Hardening"

| Metric | Target | How to Measure |
|--------|--------|----------------|
| Gateway coverage | ≥ 80% | `go test -cover` |
| handleChat complexity | ≤ 200 lines per sub-function | `wc -l` on extracted functions |
| Payment flow | Stripe checkout → API key → request works | Manual E2E test against Stripe test mode |
| Provider breadth | Gemini added (3 → 4 providers) | `nexus doctor` lists Gemini |
| Legal readiness | ToS + Privacy published | Pages accessible at public URLs |

### Cumulative Quality Targets

| Metric | Sprint 1 | Sprint 2 | Sprint 3 | Sprint 4 |
|--------|----------|----------|----------|----------|
| Gateway coverage | 53.5% (baseline) | ≥ 70% | ≥ 70% | ≥ 80% |
| Total test count | 923 | ≥ 960 | ≥ 990 | ≥ 1020 |
| Open bugs | 0 | ≤ 3 | ≤ 5 | ≤ 3 |
| CI pass rate | 100% | 100% | 100% | 100% |
| README accuracy | Fix inflated claims | All claims backed by data | — | — |

---

## 6. Risk Register

| # | Risk | Likelihood | Impact | Score | Mitigation |
|---|------|-----------|--------|:-----:|------------|
| **R1** | **Solo founder burnout** — 8 weeks at 100% capacity with zero slack | Very High | Critical | **16** | Build 1 "recovery day" into each sprint. Sprint 1 & 3 have lighter loads. Automate everything possible. If velocity drops, cut stretch items immediately. |
| **R2** | **Load test reveals critical bottleneck** — handleChat monolith or cache mutex blocks throughput | Medium | High | **9** | Sprint 2 has 1 day buffer. Known candidates: cache lock contention, JSON serialization in handleChat. Profile with `pprof` before load test to pre-identify hotspots. |
| **R3** | **Gateway coverage plateau** — 53.5% → 70% requires testing the hard paths (streaming SSE, cascade fallback, provider failover) | Medium | High | **9** | Start with highest-value untested paths. Use table-driven tests. Accept 70% as Sprint 2 target, push to 80% in Sprint 4 with handleChat decomposition making paths testable. |
| **R4** | **Launch post gets zero traction** — blog post and HN submission sink without visibility | Medium | Medium | **6** | Prepare 3 angles (cost savings, CASTER research, single-dep architecture). Post to HN, Reddit r/golang, r/MachineLearning, and X on different days. Have the hosted demo ready as proof. |
| **R5** | **Stripe account approval delays block Sprint 4 revenue work** — verification can take 2-4 weeks | Medium | Medium | **6** | Apply for Stripe account in Sprint 1 (week 1). If delayed, use LemonSqueezy as backup (no approval needed, supports webhooks). Decouple billing tests from live Stripe. |

### Risk Response Plan

```
If R1 triggers (burnout):
  → Cut Sprint 2 stretch items (handleChat decomp, coverage to 75%)
  → Move OpenAPI spec from Sprint 3 to Sprint 4
  → Focus on highest-RICE items only

If R2 triggers (bottleneck found):
  → Steal 2 days from blog post (defer to Sprint 3)
  → Apply targeted fix (likely: add read lock, optimize JSON path)
  → Publish honest benchmarks with known limitations

If R3 triggers (coverage plateau):
  → Accept 65% gateway coverage for Sprint 2
  → Prioritize handleChat decomposition in Sprint 3 instead of Sprint 4
  → Decomposition makes sub-functions independently testable → coverage jumps
```

---

## Appendix: Effort Budget Summary

| Sprint | Committed (days) | Stretch (days) | Slack |
|--------|:---:|:---:|:---:|
| Sprint 1 | 9.5 | 1 | 0.5d |
| Sprint 2 | 10 | 2 | 0d |
| Sprint 3 | 10 | 2 | 0d |
| Sprint 4 | 10 | 3 | 0d |
| **Total** | **39.5** | **8** | **0.5d** |

> ⚠️ **Warning:** This plan has almost zero slack. The solo founder risk (R1) is real.
> **Recommendation:** Treat Sprint 2 stretch items as "Sprint 3 if we're ahead" items. Protect weekends.

---

## Appendix: What We're Deliberately NOT Doing

| Item | Why Not | When |
|------|---------|------|
| Streaming cascade routing | Low confidence (0.5), high complexity. Cascade works for non-streaming. | v0.2.0 (Sprint 5-6) |
| Bedrock + Azure providers | Gemini first (OpenAI-compat mode = low effort). Others follow. | v0.2.0+ |
| SOC 2 compliance | Premature. Need paying customers first. | Phase 5 (Week 29+) |
| Multi-region deployment | Single region is fine until 50+ customers. | Phase 5 |
| Enterprise SSO (SAML) | OIDC stub exists. SAML is enterprise-only complexity. | When first enterprise customer asks |
| ML-based adaptive routing | Heuristic router at 94.8% coverage works. ML needs production data. | After 10K+ routed requests |
