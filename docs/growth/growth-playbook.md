# Nexus — Growth Playbook

> **Version:** 1.0 · **Date:** July 2025 · **Classification:** Internal — Growth Strategy
> **Goal:** 10,000 GitHub stars and 500 active deployments within 90 days of launch.

---

## Table of Contents

1. [Product-Led Growth](#1-product-led-growth)
2. [Launch Day Channel Strategy](#2-launch-day-channel-strategy)
3. [30-Day Content Calendar](#3-30-day-content-calendar)
4. [Viral Loops](#4-viral-loops)
5. [Community Plan](#5-community-plan)

---

## 1. Product-Led Growth

### 1.1 Activation Metric: First Cache Hit

The single most important moment in the Nexus user journey is **the first cache hit** — the instant the user sees a request return in <1ms with `X-Nexus-Cache: l1_exact` in the response headers. This is when the product proves its value with zero configuration.

**Activation funnel:**

| Stage | Metric | Target |
|-------|--------|--------|
| **Sign-up / Clone** | `git clone` or `docker pull` | 100% of visitors who try |
| **First request** | `POST /v1/chat/completions` returns 200 | <60 seconds from clone |
| **First cache hit** | Second identical request returns from L1 cache | <2 minutes from clone |
| **Aha moment** | Dashboard shows "$X saved" | <5 minutes from clone |
| **Activated** | User sends >50 requests through Nexus | <24 hours |

### 1.2 The "Aha Moment": Dashboard Shows "$X Saved"

The built-in dashboard (`/dashboard`) shows real-time cost savings. The "aha moment" occurs when the user sees a concrete dollar amount they've saved:

- **Header awareness:** Every response includes `X-Nexus-Cost` header showing the actual cost, and `X-Nexus-Tier` showing the tier selected. Cached responses show `X-Nexus-Cost: 0.000000`.
- **Dashboard savings widget:** Aggregate savings from cache hits, tier downgrades, and prompt compression displayed prominently.
- **Workflow budget tracking:** Teams using `X-Workflow-ID` see per-workflow cost breakdowns and budget utilization.

**Target aha moment:** User sees "$1.00 saved" within first 100 requests.

### 1.3 Time-to-Value Optimization

| Current State | Optimized Target | How |
|--------------|------------------|-----|
| Clone → first request: ~3 min | **60 seconds** | Docker one-liner in README with pre-configured Ollama backend |
| First request → cache hit: ~30s | **Instant** | Auto-replay first request to demonstrate cache hit |
| Cache hit → dashboard view: manual | **Auto-open** | Print dashboard URL with savings after first cache hit |
| Understanding savings: requires `/metrics` | **In-response** | `X-Nexus-Savings-Total` header on every response |

**Quick Start optimization priorities:**
1. Single `docker compose up` that includes Ollama + Nexus + pre-loaded model
2. Startup banner prints: "Dashboard: http://localhost:3000/dashboard"
3. Include `curl` example that sends 2 identical requests and highlights the cache hit
4. README gif showing: clone → request → cache hit → dashboard savings in <60s

---

## 2. Launch Day Channel Strategy

### 2.1 Hacker News

**Post title:**
> Show HN: Nexus – Open-source AI gateway that learns which model to use (Go, 1 dependency, 900+ tests)

**Timing:** Tuesday, 9:00 AM EST (historically best HN engagement window)

**Post body (self-post):**
```
We built Nexus because our AI API bills were $12K/month and climbing.

Nexus is an inference optimization gateway — you point your OpenAI SDK at it,
and it automatically routes each request to the cheapest model that can handle
the task. Repeat queries hit a 3-layer semantic cache (<1ms). Budget controls
prevent runaway spend.

Key stats:
- Single Go binary, zero dependencies (only gopkg.in/yaml.v3)
- 923 tests passing
- 3-layer cache: exact (SHA-256) → BM25 fuzzy → semantic (BGE-M3 embeddings)
- Adaptive routing learns from response quality over time
- Drop-in OpenAI API replacement (/v1/chat/completions)

Architecture: github.com/oabdel2/nexus

Try it: docker compose -f docker-compose.quickstart.yml up
```

**HN engagement plan:**
- Author answers every comment within 15 minutes for first 4 hours
- Prepare answers for predictable questions: "How is this different from LiteLLM?", "What about OpenRouter?", "Why Go?", "How does the classifier work?"
- Have benchmark data ready (latency, memory, RPS comparisons vs LiteLLM)

### 2.2 Reddit (4 Subreddits, Different Angles)

**r/golang** (175K members) — The Engineering Story
- **Title:** "We built an AI inference gateway in Go with 1 dependency — here's what we learned"
- **Angle:** Go engineering decisions — why single-binary matters, stdlib HTTP server perf, zero-alloc patterns, BM25 implementation in pure Go
- **CTA:** "Source: [link]. We'd love feedback on the router/cache architecture."

**r/MachineLearning** (3.1M members) — The Research Angle
- **Title:** "Implementing CASTER (adaptive model routing) in production — 50% cost reduction on real workloads"
- **Angle:** Link to the CASTER paper (arxiv.org/abs/2601.19793), explain complexity scoring, confidence map learning, cascade routing
- **CTA:** "Full implementation: [link]. Interested in collaboration on the eval pipeline."

**r/LocalLLaMA** (670K members) — The Self-Hosting Angle
- **Title:** "Open-source gateway that routes between Ollama models based on task complexity — saves tokens and time"
- **Angle:** Works with Ollama out of the box, semantic caching means repeated queries don't hit GPU, workflow-aware budgeting for multi-agent pipelines
- **CTA:** "Works with any OpenAI-compatible endpoint including Ollama. Docker quickstart: [link]"

**r/selfhosted** (420K members) — The Infrastructure Angle
- **Title:** "Nexus: self-hosted AI gateway — single binary, zero telemetry, TLS/mTLS, RBAC, rate limiting"
- **Angle:** Zero telemetry, zero data sharing, Helm chart for K8s, 17 security middleware layers, Grafana dashboards included
- **CTA:** "Helm chart + docker-compose included. No SaaS dependency ever."

### 2.3 Twitter/X Thread

**Thread structure (10 tweets):**

1. "We built Nexus — an open-source AI gateway that learns which model to use for each request. Result: up to 50% cost reduction, zero vendor lock-in. Here's why and how 🧵"

2. "The problem: AI API costs are exploding. Average enterprise spends $85K/month. 60-80% of that spend goes to over-provisioned models. Most 'hello world' queries don't need GPT-4."

3. "Our approach: CASTER-inspired complexity scoring. Each request gets a complexity score based on 7 signals: prompt analysis, context length, agent role, workflow position, budget pressure, length, and structure."

4. "Low complexity → cheap model. High complexity → premium model. But we don't stop there. We also cache: L1 exact (SHA-256), L2 BM25 fuzzy matching, L3 semantic embeddings. Cache hits return in <1ms."

5. "The cascade router tries the cheap model first. If confidence is low, it auto-escalates. Over time, the adaptive router LEARNS which task types work fine on cheap models."

6. "All of this in a single Go binary. One dependency (yaml.v3). 923 tests. Drop-in replacement for the OpenAI API."

7. "Security: TLS/mTLS, OIDC SSO, RBAC, rate limiting, prompt injection guard (16 patterns), IP allowlisting. 17 middleware layers. Self-hosted. Zero telemetry."

8. "Infra: Helm chart, Grafana dashboards (4 pre-built), Prometheus metrics, W3C distributed tracing, circuit breakers with automatic failover."

9. "It's open source under BSL 1.1. Use it for free, self-host it, own your data."

10. "Try it in 60 seconds: `docker compose -f docker-compose.quickstart.yml up` — GitHub: [link] ⭐"

**Timing:** Post thread at 10:00 AM EST on launch day, immediately after HN post.

### 2.4 Dev.to Article

**Title:** "How We Cut AI API Costs 50% With 1 Go Binary"

**Article outline:**

1. **Hook** — "Our monthly AI API bill hit $12K. Here's what we did about it."
2. **The Problem** — Over-provisioning: most requests sent to GPT-4 that could be handled by GPT-3.5 or a local model. Stats on enterprise AI spend ($85K/mo average).
3. **Why Existing Solutions Failed** — LiteLLM: memory leaks, supply chain breach. Portkey: SaaS lock-in, per-log pricing. OpenRouter: 5% tax, no self-hosting.
4. **Our Architecture** — Diagram of request flow: Cache → Classifier → Router → Provider → Telemetry. Explain each layer with code snippets.
5. **Results** — Before/after cost comparison. Cache hit rates. Latency improvements.
6. **The Secret Sauce: 3-Layer Cache** — Code walkthrough of exact → BM25 → semantic lookup. Show how BM25 catches paraphrased queries.
7. **Getting Started** — Docker one-liner + first `curl` request + dashboard screenshot.
8. **CTA** — "Star us on GitHub. We're building in the open."

**Timing:** Publish 24 hours after HN launch to capture second-wave traffic.

### 2.5 GitHub Stars Strategy (First 100)

**Pre-launch (Week -1):**
- Ensure README has: clear badges, architecture diagram, 60-second quickstart, feature checklist with ✅
- Add GIF/video of: clone → request → cache hit → dashboard
- Optimize GitHub Topics: `llm`, `ai-gateway`, `model-routing`, `go`, `openai`, `cost-optimization`, `inference`

**Launch day:**
- Cross-post HN link to all Reddit threads
- Share on personal networks (LinkedIn, Twitter, Discord servers)

**Week 1:**
- Submit to **awesome-go** list (PR to `avelino/awesome-go` under Middleware/AI)
- Submit to **awesome-selfhosted** list (PR under AI section)
- Submit to **awesome-llm** lists
- Share in Go Slack channels: #general, #performance, #open-source
- Post in AI/ML Discord servers: MLOps Community, Weights & Biases, LangChain

**Week 2-4:**
- GitHub Trending: sustained daily stars from content calendar (see Section 3)
- Reply to every Issue and PR within 24 hours (community responsiveness signals)
- Tag first 10 external contributors in a "Contributors" section of README
- Create "good first issue" labels on 10+ Issues to attract contributors

---

## 3. 30-Day Content Calendar

| Day | Platform | Title | Format | CTA |
|-----|----------|-------|--------|-----|
| 1 | Hacker News | Show HN: Nexus – Open-source AI gateway that learns which model to use | Self-post | Star on GitHub |
| 1 | Twitter/X | "We built Nexus" launch thread (10 tweets) | Thread | Try it, star it |
| 1 | Reddit r/golang | We built an AI inference gateway in Go with 1 dependency | Discussion post | Feedback + star |
| 2 | Reddit r/MachineLearning | Implementing CASTER (adaptive model routing) in production | Research discussion | Paper link + repo |
| 2 | Reddit r/LocalLLaMA | Open-source gateway that routes between Ollama models based on complexity | Discussion post | Docker quickstart |
| 3 | Dev.to | How We Cut AI API Costs 50% With 1 Go Binary | Long-form article | Star + try it |
| 3 | Reddit r/selfhosted | Nexus: self-hosted AI gateway with zero telemetry | Discussion post | Helm chart link |
| 4 | Twitter/X | "Here's how our 3-layer semantic cache works" | Thread with diagrams | GitHub link |
| 5 | LinkedIn | Launched Nexus: open-source AI cost optimization for engineering teams | Professional post | Landing page |
| 6 | Dev.to | Building a BM25 Search Engine in 200 Lines of Go | Tutorial | GitHub link |
| 7 | Twitter/X | "Why we chose Go over Python for our AI gateway" | Thread | Star on GitHub |
| 8 | YouTube | Nexus in 5 Minutes: Setup → Request → Savings Dashboard | Screen recording | Subscribe + star |
| 9 | Hashnode | Circuit Breakers for AI: How Nexus Handles Provider Failures | Technical blog | GitHub link |
| 10 | Twitter/X | "The LiteLLM supply chain breach changed everything" | Thread (security angle) | Security page link |
| 11 | Dev.to | Implementing Cascade Routing: Try Cheap First, Escalate If Needed | Tutorial | GitHub link |
| 12 | Reddit r/devops | Self-hosted AI gateway with Helm chart, Grafana dashboards, and Prometheus | Discussion | Helm chart link |
| 13 | Twitter/X | "Our AI bill dropped 47% in one week" (case study teaser) | Thread with charts | Landing page |
| 14 | Medium | The Economics of AI Model Routing: When GPT-3.5 is Better Than GPT-4 | Thought leadership | GitHub link |
| 15 | Dev.to | Zero to Production: Deploying Nexus on Kubernetes with Helm | Tutorial | Helm chart link |
| 16 | Twitter/X | "Every AI request now gets a complexity score" (technical deep-dive) | Thread with code | GitHub link |
| 17 | Product Hunt | Nexus – Open-source AI gateway with intelligent model routing | Launch | Upvote + try it |
| 18 | Reddit r/golang | Deep dive: How we implemented adaptive routing with confidence maps | Technical discussion | GitHub link |
| 19 | Twitter/X | "We added MCP support — here's how AI agents can use Nexus as a tool" | Thread | Try MCP endpoint |
| 20 | Dev.to | Prompt Compression: How We Strip 30% of Tokens Before They Cost You Money | Tutorial | GitHub link |
| 21 | Twitter/X | "100 stars in 3 weeks — thank you + what's next" | Milestone celebration | Star if you haven't |
| 22 | Hashnode | Building a Synonym-Learning Semantic Cache in Go | Technical deep-dive | GitHub link |
| 23 | LinkedIn | "AI infrastructure costs are the new cloud bill" | Thought leadership | Landing page |
| 24 | Dev.to | A/B Testing Your AI: How Nexus Runs Experiments Across Model Tiers | Tutorial | GitHub link |
| 25 | Twitter/X | "Workflow-aware AI: why single-request optimization isn't enough" | Thread | GitHub link |
| 26 | Reddit r/MachineLearning | Confidence scoring for LLM outputs: our 6-signal heuristic evaluator | Research discussion | GitHub link |
| 27 | Dev.to | 17 Security Layers: How We Hardened an AI Gateway | Security deep-dive | Security page |
| 28 | Twitter/X | "What we learned from 500 GitHub issues in 4 weeks" | Community reflection | Contribute link |
| 29 | YouTube | Nexus Architecture Deep Dive (30 min) | Technical walkthrough | Subscribe + star |
| 30 | Dev.to | Month 1 Retrospective: Metrics, Learnings, and Roadmap | Retrospective | Star + roadmap link |

**Total: 30 pieces across 7 platforms over 30 days.**

---

## 4. Viral Loops

### 4.1 "Powered by Nexus" Badge

**Mechanism:** Opt-in response header and optional HTML badge.

- **Header:** `X-Powered-By: Nexus (nexus-gateway.com)` — added by default, can be disabled via config (`server.powered_by_header: false`).
- **API response field:** When `X-Nexus-Explain: true` is sent, the response includes a `nexus` object with routing explanation. Developers who inspect this learn about Nexus.
- **Dashboard badge:** Embeddable badge for READMEs: `![Powered by Nexus](https://nexus-gateway.com/badge.svg)`

### 4.2 Cost Savings Report Sharing

**Mechanism:** Shareable dashboard URL with read-only analytics.

- **Dashboard URL:** `/dashboard?share=<token>` generates a read-only link showing aggregate stats: total requests, cache hit rate, cost savings, top models used.
- **Weekly email digest:** For teams with billing enabled, auto-generate a weekly "Nexus Savings Report" with:
  - Total cost this week vs. estimated cost without Nexus
  - Cache hit rate trend
  - Top 5 most expensive workflows
  - "Share this report" button that generates a public URL
- **Social proof:** "We saved $X,XXX this month with Nexus" — shareable card image generated from dashboard data.

### 4.3 "This Request Saved $X" Header

**Mechanism:** Every cached response includes a savings calculation.

- **`X-Nexus-Savings`**: Added to every cached response, showing estimated cost of the request if it had gone to the provider.
  ```
  X-Nexus-Savings: $0.0034
  X-Nexus-Savings-Total: $127.45
  ```
- **Developer awareness:** Developers inspecting headers during debugging see the savings. This creates organic word-of-mouth.
- **Dashboard accumulator:** Running total of savings prominently displayed at top of dashboard.
- **Milestone notifications:** When cumulative savings reach $10, $100, $1,000, $10,000 — fire a webhook event (`savings.milestone`) that teams can wire to Slack/email.

### 4.4 Referral Mechanics

- **GitHub star CTA in startup logs:**
  ```
  ⭐ If Nexus saves you money, star us: https://github.com/oabdel2/nexus
  ```
- **"Tell your team" button** in dashboard that generates an internal share link with team-specific savings data.
- **Contributor badge program:** Top 10 contributors each month get listed in README + special Discord role.

---

## 5. Community Plan

### 5.1 Discord Server Structure

```
📢 ANNOUNCEMENTS
├── #announcements     — Releases, features, breaking changes (admin-only post)
├── #changelog         — Auto-posted from GitHub releases

💬 GENERAL
├── #general           — General discussion about Nexus and AI infrastructure
├── #introductions     — New members introduce themselves and their use case
├── #off-topic         — Non-Nexus discussion

🛠️ SUPPORT
├── #support           — Help with setup, configuration, troubleshooting
├── #deployment        — Docker, Kubernetes, Helm-specific help
├── #providers         — OpenAI, Anthropic, Ollama provider-specific questions

🎨 COMMUNITY
├── #show-and-tell     — Share what you built with Nexus (demos, screenshots, stats)
├── #feature-requests  — Request and discuss new features
├── #bug-reports       — Quick bug reports (link to GitHub Issues for tracking)

👨‍💻 DEVELOPMENT
├── #contributors      — For active code contributors
├── #architecture      — Deep technical discussions about Nexus internals
├── #roadmap           — Discussion of upcoming features and priorities

📊 SHOWCASE
├── #savings-flex      — Share your cost savings screenshots 💰
├── #benchmarks        — Performance benchmarks and comparisons
```

### 5.2 First 50 Members Strategy

**Phase 1: Seed members (Day 1-3) — Target: 15 members**
- Personal network: DM 20 developer friends/colleagues who work with AI APIs
- HN commenters: Anyone who comments positively on the HN post gets a Discord invite
- GitHub stargazers: First 50 people who star the repo get a DM with Discord invite (via GitHub notification)

**Phase 2: Content-driven growth (Day 4-14) — Target: 30 members**
- Every content piece (Dev.to, Reddit, Twitter) includes Discord link
- "Join our Discord for support" in README
- Auto-invite in Nexus startup logs: `💬 Community: https://discord.gg/nexus`

**Phase 3: Community-driven growth (Day 15-30) — Target: 50+ members**
- "Invite a friend" challenge: members who invite 3+ people get early access to enterprise features
- Weekly "Office Hours" voice chat (Thursdays 2 PM EST) — live Q&A with maintainers
- "Community Spotlight" — feature one member's deployment/use-case each week in #announcements

### 5.3 Community Engagement Cadence

| Activity | Frequency | Owner |
|----------|-----------|-------|
| Answer #support questions | Within 4 hours | Core team |
| Triage GitHub Issues | Daily | Core team |
| Community Spotlight post | Weekly (Monday) | Community lead |
| Office Hours voice chat | Weekly (Thursday 2 PM EST) | Core team |
| Release notes + changelog | Every release | Maintainer |
| Feature request review | Bi-weekly | Product lead |
| Contributor recognition | Monthly | Community lead |

### 5.4 Community-to-Contribution Pipeline

1. **User** → joins Discord, asks questions in #support
2. **Power user** → answers others' questions, shares in #show-and-tell
3. **Contributor** → picks up a "good first issue", submits PR
4. **Maintainer** → consistent contributions, gets write access
5. **Core team** → leads a feature area, participates in architecture decisions

**Incentives at each level:**
- Power user: Discord role badge, mentioned in monthly update
- Contributor: Listed in README Contributors section, "Contributor" Discord role
- Maintainer: GitHub org membership, early access to roadmap discussions
- Core team: Co-author on blog posts, conference talk opportunities

---

## Appendix: Key Metrics to Track

| Metric | Tool | Target (30 days) |
|--------|------|-------------------|
| GitHub stars | GitHub | 500+ |
| Docker pulls | Docker Hub | 2,000+ |
| Discord members | Discord | 50+ |
| HN front page hours | HN | 4+ hours |
| Dev.to article views | Dev.to | 10,000+ |
| Twitter thread impressions | X Analytics | 100,000+ |
| Unique cloners (git) | GitHub Traffic | 1,000+ |
| Weekly active deployments | Telemetry (opt-in) | 100+ |
| Content pieces published | Internal tracker | 30+ |
| GitHub Issues opened (signal of adoption) | GitHub | 50+ |
