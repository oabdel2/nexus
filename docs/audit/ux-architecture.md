# Nexus UX Architecture Audit

**Auditor:** UX Architect (first-time reviewer, zero prior context)
**Date:** 2025-07-17
**Scope:** Complete user journey, landing page, onboarding, dashboard, CLI, documentation

---

## 1. User Journey Mapping

### Persona A: Startup Developer ("Try It Fast")

| Stage | Action | Friction Points |
|-------|--------|----------------|
| **Awareness** | Finds Nexus via GitHub, Hacker News, or search | Hero headline is strong but "TF-IDF classification" in subtitle is jargon — devs care about savings, not algorithm names |
| **Discovery** | Lands on index.html, scans hero | ✅ "Before/After" code comparison is excellent — instant understanding. ⚠️ "Smart TF-IDF classification, cascade routing, and shadow evaluation" is three unfamiliar terms in one sentence |
| **Evaluation** | Clicks "Get Started Free" → signup.html | ✅ 3-step flow is clean. ⚠️ API key is demo-generated (client-side), no actual account creation. No email capture means no re-engagement |
| **Adoption** | Changes base_url, sends first request | ✅ Code examples in 4 languages are ready to copy. ⚠️ Step 3 (code examples) is hidden until key is generated — should be visible immediately |
| **Expansion** | Uses dashboard, adds workflow headers | ⚠️ Dashboard onboarding card is good but links to `/docs.html` for dashboard — should link to actual `/dashboard` path |

**Time-to-first-value estimate:** ~5 minutes (excellent for a gateway product)
**Key drop-off risk:** Signup page generates a demo key — user may think it's real, hit errors, and abandon

### Persona B: Platform Engineer ("Evaluate for Team")

| Stage | Action | Friction Points |
|-------|--------|----------------|
| **Awareness** | Evaluating alternatives, compares feature tables | ✅ Comparison table on landing page is very strong |
| **Discovery** | Reads docs.html, security.html, how-it-works.html | ✅ Docs have sidebar nav, endpoint cards, config examples. ⚠️ No search functionality in docs at all — for a 70KB+ page, this is a significant gap |
| **Evaluation** | Runs `nexus doctor`, `nexus validate` | ✅ Doctor output is thorough and well-structured. ⚠️ No `nexus benchmark` or `nexus test-connection` command for quick validation |
| **Adoption** | Deploys via Docker Compose or Helm | ✅ Quick start configs exist. ⚠️ `docker-compose.quickstart.yml` referenced in README — separate from main `docker-compose.yml` which could confuse |
| **Expansion** | Configures SSO, RBAC, Prometheus | ⚠️ Security docs are on a separate page (`security.html`) but not deeply linked from docs.html sidebar |

**Key drop-off risk:** No docs search means platform engineers can't quickly find specific config options

### Persona C: CTO/VP Engineering ("Purchase Decision")

| Stage | Action | Friction Points |
|-------|--------|----------------|
| **Awareness** | Forwarded link from team, or googles "AI cost optimization" | ✅ Meta description is clear: "reducing costs up to 50%" |
| **Discovery** | Scans landing page top-to-bottom | ✅ Problem section, ROI calculator, comparison table — all present. ⚠️ No case studies with named companies / concrete $ amounts (testimonials exist but feel synthetic) |
| **Evaluation** | Checks pricing, security, compliance | ✅ 4-tier pricing is clear. ⚠️ Pricing meta description says "Starter (/mo)" — price is missing from meta tag. Enterprise "Contact Sales" has no form, just links to pricing.html |
| **Adoption** | Approves budget, team deploys | ⚠️ No ROI case study PDF or "share with your team" link. CTOs need ammunition for internal approval |
| **Expansion** | Wants SLA, dedicated support | ⚠️ Enterprise tier mentions "Guaranteed SLA" but no specifics (99.9%? 99.99%?) |

**Key drop-off risk:** Testimonials look fabricated (no company links, no photos). CTOs are trained to spot this.

---

## 2. Landing Page UX Critique

### Does the hero convey value in 5 seconds?
**Grade: B+**
- ✅ "The AI Gateway That Learns Which Model to Use — and Proves It" is memorable and differentiating
- ✅ Before/after code comparison is the strongest element — immediate visual proof
- ⚠️ Subtitle mentions "Smart TF-IDF classification, cascade routing, and shadow evaluation" — three implementation details. CTOs don't care about TF-IDF; developers care about results
- **Fix:** Change subtitle to focus on outcome: "Route every LLM request to the cheapest model that delivers quality — cutting costs up to 50% with zero code changes."

### Is the CTA clear and compelling?
**Grade: A-**
- ✅ "Get Started Free →" is prominent, gradient-styled, above the fold
- ✅ GitHub link provides credibility as secondary CTA
- ⚠️ No social proof near the CTA (star count, user count, download count)

### Does the page answer "why should I care?"
**Grade: A**
- ✅ Problem section ("Every request hits GPT-4") is relatable
- ✅ Calculator section lets users see their own potential savings
- ✅ Comparison table differentiates from competitors
- ✅ CLI demo shows real developer workflow

### Is the pricing section clear?
**Grade: B+**
- ✅ 4 tiers with clear feature differentiation
- ⚠️ "Popular" badge on Starter is standard but the jump from Free (1K) to Starter (50K) is 50x — there's a gap for teams doing 5K-20K requests
- ⚠️ Enterprise "Contact Sales" links to `pricing.html` — not a contact form

### Information hierarchy
**Grade: A-**
- Current flow: Hero → Problem → How It Works → Features → Calculator → Comparison → Pricing → CLI → Quickstart → Testimonials → Footer
- ✅ Problem before solution is correct
- ⚠️ Testimonials are buried at the bottom — social proof should be higher
- ⚠️ CLI demo section could be merged into "How It Works" or moved up

### What's missing?
1. **Docs search** — critical for a developer tool
2. **Logo bar** — "Trusted by teams at..." even if early-stage, show tech logos or GitHub stars
3. **Live demo link** — playground exists but isn't prominent enough
4. **Video walkthrough** — 60-second Loom/demo showing real cost savings

---

## 3. Onboarding Flow Critique

### Is signup intimidating or inviting?
**Grade: A-**
- ✅ "Start Free" headline, "No credit card required" — low friction
- ✅ 3-step visual flow (Plan → Key → Code) is clean
- ✅ Plan selection with radio-button cards is well-designed
- ⚠️ No actual account creation (no email, no password) — this is a demo flow, not real signup
- ⚠️ "Popular" badge on Starter plan might pressure users away from Free

### First-run experience
- ✅ After key generation, Step 3 appears with code examples in 4 languages
- ✅ "What Happens Next" section explains the value chain
- ⚠️ Code examples reference `https://api.nexus-gateway.dev/v1` — this appears to be a cloud endpoint. For self-hosted users, this is confusing. Should show both cloud and self-hosted URLs
- ⚠️ Step 3 is `display:none` until key is generated — user doesn't know what's coming next

### Time-to-first-value
- ✅ ~2 minutes from landing page to having code they can run
- ⚠️ But the generated key is client-side demo — actual TTFV requires Docker setup, which is a separate flow entirely

### What could be smoother?
1. Show Step 3 code examples immediately (dimmed/preview) so users know what's coming
2. Add a "Try it live" button that hits the playground instead of requiring local setup
3. After key generation, auto-scroll to Step 3
4. Add an email capture field (optional) for follow-up / key recovery

---

## 4. Dashboard UX Critique

### Is "Cost Saved" prominent enough?
**Grade: A**
- ✅ The "Cost Saved" card has a `.hero` class with green accent border, larger font (32px vs 26px), and gradient background glow
- ✅ Positioned as the 3rd of 5 cards — center position draws the eye
- ✅ "Actual spend" shown as subtitle for context

### Are the charts readable?
**Grade: B+**
- ✅ SVG savings chart with premium (dashed) vs actual (solid green) lines is clear
- ✅ Gradient fill between lines visually shows the savings gap
- ⚠️ No axis labels on the chart — users can't tell the scale
- ⚠️ Chart shows "Waiting for data…" as empty state — good, but could show a sample/demo view

### Is the request feed useful?
**Grade: A-**
- ✅ Shows Time, Tier (color-coded badges), Model, Latency, Cost, Status
- ✅ Scrollable with sticky headers, 50 request limit prevents DOM bloat
- ✅ Real-time SSE updates with fadeIn animation
- ⚠️ No filtering or search capability — for high-volume gateways, finding specific requests is impossible
- ⚠️ All statuses show ✓ — there's no error state rendering. What happens on failure?

### What's the most important thing a user should see?
**The savings number** — and it IS prominent. The dashboard correctly prioritizes the "money saved" narrative.

### What would I change?
1. **Add Y-axis labels** to the savings chart
2. **Add error state rendering** in the request feed (red badge, error message)
3. **Add a "time range" selector** (1h, 24h, 7d, 30d) to the stats cards
4. **Add request feed filtering** by tier, model, or cache status
5. **Show onboarding card by default** — currently it's `display:none` then set to `display:''` in JS, causing a flash

---

## 5. CLI UX Critique

### Is `nexus doctor` output clear?
**Grade: A**
- ✅ Checklist format with ✅/⚠️/❌ icons is immediately scannable
- ✅ Checks: Go version, config file, providers (with reachability), embedding model, chat model, cache layers, security settings, compression, cascade, data directory, port availability
- ✅ Overall summary with error/warning counts
- ⚠️ No suggested fix for errors (e.g., "❌ Chat model not found" → should suggest `ollama pull llama3.1`)

### Is `nexus inspect` output actionable?
**Grade: A-**
- ✅ Shows complexity score breakdown (Keywords, Length, Structure, Context, Role, Position, Budget)
- ✅ Shows matched keywords from the prompt
- ✅ Shows tier decision, reason, provider, and model selection
- ✅ Shows cascade prediction
- ⚠️ Missing: estimated cost, cache check result, and compression estimate
- ⚠️ Missing: "what-if" comparison (e.g., "if this were routed to premium, it would cost $X more")

### What's the error experience like?
**Grade: B+**
- ✅ Unknown command prints helpful usage
- ✅ `runStatus` gives specific remediation steps when gateway is unreachable
- ⚠️ `runServe` with bad config does `slog.Warn` then falls back to defaults silently — user might not realize their config was ignored
- ⚠️ No structured error codes (e.g., `NEXUS-001: Config not found`)

### What's missing from the CLI?
1. `nexus logs` — tail gateway logs
2. `nexus benchmark` — run a quick latency/throughput test
3. `nexus config show` — dump effective (merged) configuration
4. Colored output for tier names in `nexus inspect` (economy=green, premium=red)
5. `--json` flag for machine-readable output on all commands

---

## 6. Documentation UX Critique

### Is docs.html navigable?
**Grade: B+**
- ✅ Fixed sidebar with sections: Getting Started, Reference (API, Headers, Config, CLI, SDK), Deep Dive (Caching, Routing, Security, Observability), Help (Troubleshooting)
- ✅ Mobile sidebar toggle with floating button
- ✅ Active link highlighting on scroll
- ⚠️ **No search** — this is a 73KB single-page doc. Finding a specific config key requires manual scrolling or Ctrl+F
- ⚠️ Sidebar doesn't show sub-sections — you can't jump to a specific endpoint or config option

### Can someone find what they need in 30 seconds?
**Grade: C+** (without search, this is the weakest area)
- For "How do I set up rate limiting?" → user must: click Security in sidebar → scroll through security section → find rate limiting subsection. ~45 seconds.
- For "What's the cascade confidence threshold?" → user must guess it's under Routing → scroll → find it. ~60 seconds.

### Are code examples copy-pasteable and correct?
**Grade: A-**
- ✅ Code blocks have syntax highlighting via span classes
- ✅ Examples cover curl, Python, Node.js, Go
- ⚠️ No "Copy" button on code blocks — user must manually select text
- ⚠️ Config example in docs uses `models: [gpt-4o, gpt-4o-mini]` shorthand but actual YAML config (`nexus.minimal.yaml`) uses nested model objects with tier/cost — inconsistency

### Is the search experience acceptable?
**Grade: F** — there is no search.

---

## 7. Improvement Recommendations (Prioritized by Impact)

| # | Improvement | Why | Impact | Effort |
|---|------------|-----|--------|--------|
| **1** | **Add search to docs.html** | 73KB doc page with no search is the #1 usability blocker for platform engineers. A simple JS fuzzy-search over headings + content would transform the experience. | 🔴 Critical | Medium (2-3 days) |
| **2** | **Rewrite hero subtitle to remove jargon** | "TF-IDF classification, cascade routing, and shadow evaluation" means nothing to first-time visitors. Lead with outcome, not implementation. | 🔴 High | 🟢 Quick (5 min) |
| **3** | **Add "Copy" buttons to all code blocks** | Developer tools live and die by copy-paste UX. Every code block in docs, signup, and landing page needs a one-click copy button. | 🔴 High | 🟢 Quick (30 min) |
| **4** | **Fix dashboard onboarding card flash** | Card starts `display:none` then JS sets it to visible, causing a layout shift. Should start visible and hide when requests arrive. | 🟡 Medium | 🟢 Quick (5 min) |
| **5** | **Add Y-axis labels and time range to dashboard chart** | Chart without axes is hard to interpret. Users need to know scale ($0.10? $1000?) and time range. | 🟡 Medium | Medium (1 day) |
| **6** | **Show Step 3 code examples immediately on signup** | Hiding the "Start Using Nexus" panel until key generation adds mystery and reduces perceived value of the flow. Show it as preview. | 🟡 Medium | 🟢 Quick (10 min) |
| **7** | **Add error states to dashboard request feed** | Currently only shows ✓ status. Failed requests need red indicators and error details. | 🟡 Medium | Small (half day) |
| **8** | **Add suggested fixes to `nexus doctor` errors** | "❌ Chat model not found" is diagnostic but not actionable. Add: "Run: ollama pull llama3.1" | 🟡 Medium | 🟢 Quick (30 min) |
| **9** | **Add `--json` output flag to CLI commands** | Platform engineers need machine-parseable output for CI/CD pipelines and monitoring scripts. | 🟡 Medium | Medium (1-2 days) |
| **10** | **Enterprise "Contact Sales" → actual contact form** | Current link just goes to pricing.html. CTOs clicking "Contact Sales" expect a form or at least a mailto link. | 🟡 Medium | Small (half day) |
| **11** | **Add social proof near hero CTA** | GitHub stars badge, user count, or company logos near the "Get Started Free" button increases conversion. | 🟡 Medium | 🟢 Quick (15 min) |
| **12** | **Make testimonials less synthetic** | Add real photos, company links, specific metrics. Current testimonials have placeholder initials and read like generated copy. | 🟠 Low-Med | Medium (depends on real users) |
| **13** | **Add a docs-search sidebar with sub-section links** | The sidebar shows only top-level sections. Expanding to show H3-level links would help navigation. | 🟠 Low-Med | Medium (1 day) |
| **14** | **Add `nexus benchmark` command** | Platform engineers evaluating for adoption want to see latency/throughput numbers from their own infra. | 🟠 Low-Med | Large (3-5 days) |
| **15** | **Consistent config examples between docs and actual YAML** | Docs show `models: [gpt-4o]` shorthand but `nexus.minimal.yaml` uses nested objects. Pick one and be consistent. | 🟠 Low-Med | Small (half day) |

---

## 8. Quick Wins Implemented

### QW-1: Hero Subtitle Rewrite (index.html)
**Before:** "Smart TF-IDF classification, cascade routing, and shadow evaluation reduce your inference costs up to 50% — with statistical proof that quality holds."
**After:** "Route every LLM request to the cheapest model that delivers — cutting inference costs up to 50% with zero code changes and statistical proof that quality holds."
**Rationale:** Removes three jargon terms (TF-IDF, cascade routing, shadow evaluation) that first-time visitors won't understand. Leads with the action and outcome instead.

### QW-2: Dashboard Onboarding Card Visibility Fix (internal/dashboard/index.html)
**Before:** Onboarding card starts `display:none` in HTML, then JS immediately sets it visible — causing a layout flash.
**After:** Card starts visible in HTML (no `display:none` in the style attribute). JS hides it when requests arrive (existing behavior preserved).
**Rationale:** Eliminates layout shift on dashboard load. The onboarding card IS what users should see first.

### QW-3: Signup Step 3 Preview (site/signup.html)
**Before:** Step 3 ("Start Using Nexus") is completely hidden (`display:none`) until the API key is generated.
**After:** Step 3 is visible but dimmed with reduced opacity, showing users what comes next. Full opacity activates after key generation.
**Rationale:** Users should see the complete flow upfront. Hiding the code examples creates uncertainty about what happens after signup.

---

## Summary

Nexus has an **unusually strong foundation for an early-stage product**. The landing page information hierarchy is solid, the dashboard is well-designed with the right metric prominence, and the CLI is one of the best developer-facing CLIs I've reviewed (doctor output is particularly good).

**The three biggest gaps are:**
1. **No docs search** — this is the single highest-impact missing feature
2. **Jargon-heavy copy** — the product speaks in implementation details rather than outcomes
3. **Demo vs real onboarding blur** — the signup flow generates client-side demo keys, which will confuse users expecting a real service

**Overall UX Grade: B+** — strong visual design, good information architecture, excellent CLI. Documentation search and copy-paste UX are the most impactful improvements to prioritize.
