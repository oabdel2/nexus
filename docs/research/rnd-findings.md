# Nexus R&D Findings: Cascade Routing, Eval Pipeline & Prompt Compression

**Date:** 2026-04-05  
**Status:** Implementation-Ready Research  
**Authors:** R&D Agent  
**Scope:** Three new features for the Nexus inference optimization gateway

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Cascade Routing Research](#2-cascade-routing-research)
3. [Eval Pipeline / Confidence Scoring Research](#3-eval-pipeline--confidence-scoring-research)
4. [Prompt Compression Research](#4-prompt-compression-research)
5. [A/B Testing Framework](#5-ab-testing-framework)
6. [Recommended Implementation Parameters](#6-recommended-implementation-parameters)
7. [Risk Assessment](#7-risk-assessment)

---

## 1. Executive Summary

This document presents implementation-ready research for three features being added to Nexus, an agentic-first inference optimization gateway written in Go. All three features share a common goal: **reduce cost while maintaining quality**, without requiring local ML model inference within the gateway process.

**Key findings:**

| Feature | Expected Cost Savings | Latency Impact | Implementation Complexity |
|---------|----------------------|----------------|--------------------------|
| Cascade Routing | 40–65% on routable traffic | +150–300ms on escalations (~25% of requests) | Medium |
| Eval Pipeline | Enables cascade tuning; 5–10% indirect savings via better routing | +50–100ms per shadow eval (async, off critical path) | Medium-High |
| Prompt Compression | 20–35% token reduction (pure Go); up to 50% with aggressive strategies | -30–80ms (fewer tokens = faster TTFT) | Low-Medium |

**Combined projected cost reduction:** 45–70% on optimizable traffic, with quality parity verified by the eval pipeline.

---

## 2. Cascade Routing Research

### 2.1 Core Concept

Cascade routing sends a request to a cheap model first, scores the response confidence, and escalates to a premium model only when confidence is below a threshold. This differs from Nexus's current one-shot keyword-based routing, which commits to a tier at request time without verification.

### 2.2 Academic Foundations

#### 2.2.1 Google's "Speculative Cascades" (arXiv:2405.19261, Oct 2024)

Google's Speculative Cascades paper merges two techniques:

- **Classic Cascades:** Small model answers first; if confidence is too low, escalate to a larger model.
- **Speculative Decoding:** Small "drafter" model predicts tokens; large model verifies in parallel.

The hybrid approach lets the small model draft outputs and uses a **nuanced deferral rule** — accept drafts if confidence exceeds a threshold, even without token-by-token match. The paper demonstrates:

- **Benchmark results:** On Gemma and T5 across summarization, translation, reasoning, and coding — speculative cascades consistently outperform both classic cascades and speculative decoding alone.
- **Configurable trade-offs:** Aggressive thresholds maximize speed/savings; conservative thresholds preserve accuracy.

> **Relevance to Nexus:** The speculative decoding aspect requires token-level access (not applicable to an API proxy). However, the **cascade deferral strategies** — confidence-based checks, comparative scoring, top-k list checks — are directly applicable. Nexus can implement the cascade portion using response-level confidence scoring.

**Source:** [research.google/blog/speculative-cascades](https://research.google/blog/speculative-cascades-a-hybrid-approach-for-smarter-faster-llm-inference/), [arXiv:2405.19261](https://arxiv.org/pdf/2405.19261)

#### 2.2.2 Apple's "Learning to Route LLMs with Confidence Tokens" (arXiv:2410.13284, Oct 2024; ICML 2025)

Apple's paper introduces **confidence tokens** — special tokens (`<CN>` confident, `<UN>` unconfident) trained into models via the **Self-REF** (Self-Reflection with Error-based Feedback) method:

- Fine-tune on datasets where correct predictions get `<CN>`, incorrect get `<UN>`
- Model learns to "know what it knows" — a metacognitive signal
- **Results:** Outperforms raw token probabilities and verbalized confidence across routing and rejection benchmarks
- **Lightweight:** Only requires fine-tuning, not architecture changes

> **Relevance to Nexus:** Confidence tokens require model fine-tuning, which is outside Nexus's scope as an API proxy. However, the **routing framework** described is applicable: when a model emits low confidence, route to a stronger model. Nexus can approximate this via proxy-side heuristics (see §2.4).

**Source:** [machinelearning.apple.com/research/learning-to-route](https://machinelearning.apple.com/research/learning-to-route), [arXiv:2410.13284](https://arxiv.org/abs/2410.13284)

#### 2.2.3 ETH Zurich's "A Unified Approach to Routing and Cascading for LLMs" (arXiv:2410.10347, Oct 2024; ICML 2025)

Dekoninck, Baader & Vechev present a **unified linear optimization framework** combining routing and cascading:

- **Selection rule:** `τ_i(x, λ) = q̂_i(x) − λ · ĉ_i(x)` where `q̂` is predicted quality, `ĉ` is predicted cost, and `λ` balances quality vs. cost.
- **Formal optimality proofs** for both routing and cascading strategies.
- **Key finding:** Cascade routing (unified) achieves up to **14% AUC improvement** over standalone routing or cascading on SWE-Bench.
- **Quality estimator centrality:** The paper identifies that robust quality estimators — whether ex-ante (pre-execution) or post-hoc (post-execution) — are the pivotal component.
- **Open source:** [github.com/eth-sri/cascade-routing](https://github.com/eth-sri/cascade-routing)

> **Relevance to Nexus:** Directly applicable. Nexus should implement both ex-ante routing (current keyword classifier, improved) and post-hoc cascading (evaluate cheap model output, escalate if quality estimate is low). The λ parameter maps to Nexus's cost budget controls.

**Source:** [arxiv.org/abs/2410.10347](https://arxiv.org/abs/2410.10347), [ETH SRI Lab](https://www.sri.inf.ethz.ch/publications/dekoninck2024cascaderouting)

#### 2.2.4 Rational Tuning of LLM Cascades (Zellinger & Thomson, arXiv:2501.09345, Jan 2025)

This paper uses a **Markov-copula model** for cascade threshold tuning:

- Demonstrates up to **4% AUC improvement** over static thresholds for three-model cascades
- Key finding: **no universal optimal threshold** — optimal values depend on cascade architecture, calibration, and cost structure
- Recommends data-driven tuning over static choices

### 2.3 Confidence Threshold Analysis

#### Empirical Ranges from Literature

| Source | Recommended Threshold | Context |
|--------|----------------------|---------|
| Academic common practice | 0.70–0.85 | General-purpose; requires calibrated confidence |
| Zellinger & Thomson 2025 | Dynamic (learned) | Markov-copula optimized per deployment |
| TREACLE (NeurIPS 2024) | RL-adapted (no static) | Budget-constrained policy learning |
| Mixture of Thought (SouthNLP 2024) | ~0.75 | Self-consistency based confidence |

#### Nexus-Specific Recommendation

**Start at 0.78 threshold, with per-task-type tuning.**

Rationale:
- 0.78 sits in the empirically validated range (0.70–0.85)
- Biases slightly toward escalation (better to spend a bit more than deliver bad quality)
- Should be tuned per task type within 2–4 weeks using the eval pipeline (§3)

### 2.4 Confidence Scoring Without a Local Model

**This is the key engineering challenge for Nexus.** Since Nexus proxies to external APIs and has no local model, it cannot access token-level logits. Available strategies ranked by feasibility:

#### Strategy 1: Hedging Word Detection (Low Latency, Low Accuracy)
- **Method:** Regex scan for uncertainty markers: "I think", "perhaps", "I'm not sure", "it's possible", "might", "arguably", "unclear"
- **Implementation:** Pure Go `regexp` — zero dependencies
- **Accuracy:** Poor as sole signal. Models can hedge confidently or be wrong without hedging.
- **Latency:** <1ms
- **Score contribution weight:** 0.15

```go
// Example hedging patterns
var hedgingPatterns = []string{
    `(?i)\b(I think|I believe|perhaps|maybe|possibly|might be|not sure|unclear)\b`,
    `(?i)\b(arguably|it seems|appears to|could be|I'm not certain)\b`,
}
```

#### Strategy 2: Response Length Heuristics (Low Latency, Moderate Accuracy)
- **Method:** Compare response length to expected length for task type. Abnormally short or long responses correlate with low quality.
- **Implementation:** Track median response length per task-type; flag responses >2σ from median.
- **Accuracy:** Moderate — catches obvious failures (empty responses, hallucinated rambling)
- **Latency:** <1ms
- **Score contribution weight:** 0.10

#### Strategy 3: Self-Consistency Checking (High Accuracy, Higher Latency + Cost)
- **Method:** Send the same prompt to the cheap model 2–3 times (temperature >0). Measure agreement.
- **Self-consistency score:** Fraction of samples agreeing with the majority answer.
- **Research backing:** Wang et al. 2022 (original self-consistency paper); Confidence-Informed Self-Consistency (ACL 2025) shows 40% reduction in needed samples using confidence-weighted voting.
- **For Nexus:** Send 2 parallel cheap calls instead of 1. If responses agree (cosine similarity >0.85 via BGE-M3 embeddings), accept cheap answer. If divergent, escalate.
- **Accuracy:** High for factual/structured tasks; moderate for creative tasks
- **Latency:** +100–200ms (parallel calls)
- **Cost:** 2–3× cheap model cost (still much less than premium)
- **Score contribution weight:** 0.40

#### Strategy 4: Embedding Similarity to Known-Good Responses (High Accuracy, Moderate Latency)
- **Method:** Maintain a corpus of premium-model responses per task type. Compare cheap response embedding to this corpus using BGE-M3.
- **Threshold:** Cosine similarity ≥0.82 → accept cheap answer; <0.82 → escalate
- **Implementation:** Nexus already has BGE-M3 in its L3 semantic cache layer — reuse this infrastructure.
- **Accuracy:** High for repetitive task types; lower for novel queries
- **Latency:** +30–50ms (embedding computation + similarity search)
- **Score contribution weight:** 0.35

#### Composite Confidence Score

```
confidence = 0.15 × hedge_score + 0.10 × length_score + 0.40 × consistency_score + 0.35 × embedding_score
```

**Decision rule:** If `confidence ≥ 0.78` → accept cheap response. Else → escalate to premium.

**Fallback for latency-sensitive requests:** Skip consistency check (weight redistributed to hedge=0.25, length=0.15, embedding=0.60). This reduces latency overhead to ~40ms.

### 2.5 Latency Impact Analysis

| Scenario | Additional Latency | When It Occurs | Frequency (Est.) |
|----------|--------------------|----------------|-------------------|
| Cheap model accepted (fast path) | +40–50ms (embedding check only) | High-confidence responses | ~60–75% of requests |
| Cheap model accepted (full check) | +150–200ms (parallel consistency + embedding) | Moderate-confidence responses | ~10–15% |
| Escalation to premium | +300–600ms (cheap attempt wasted + premium call) | Low-confidence responses | ~15–25% |
| Direct premium (skip cascade) | 0ms additional | Pre-classified as complex | ~5–10% |

**Break-even analysis:** The cascade is worth it when:
```
(P_cheap × cost_cheap) + (P_escalate × (cost_cheap + cost_premium)) < cost_premium
```

With GPT-4o-mini at ~$0.15/1M input tokens and GPT-4o at ~$2.50/1M input tokens:
- If escalation rate ≤ 40%, cascade saves money
- At 25% escalation rate, savings are ~60%
- Latency penalty is acceptable when P95 cheap response is <500ms

### 2.6 Implementation Architecture for Nexus

```
Request → Complexity Classifier (existing)
    ├── Complex → Premium Model (direct, no cascade)
    └── Simple/Medium → Cheap Model
            ├── Confidence ≥ 0.78 → Return cheap response
            └── Confidence < 0.78 → Premium Model (escalation)
                    └── Log both responses for eval pipeline
```

---

## 3. Eval Pipeline / Confidence Scoring Research

### 3.1 LMSYS Chatbot Arena Methodology

The LMSYS Chatbot Arena is the gold standard for LLM quality evaluation as of 2025:

- **Method:** Anonymous pairwise "battles" — two models answer the same prompt; humans pick the winner.
- **Scale:** 6+ million votes by 2025, crowdsourced from diverse users.
- **Statistical model:** Bradley-Terry model (upgrade from simple Elo) for aggregation; results mapped to Elo scale with confidence intervals.
- **Minimum votes:** 500+ per model for stable ranking.
- **Strengths:** Human-aligned, accounts for subjective nuance, handles ties.
- **Weaknesses:** Crowd sampling bias, not task-specific, potential gaming.

> **Relevance to Nexus:** The pairwise comparison approach is directly applicable. Nexus can run **shadow evaluations** — send the same request to both cheap and premium models, compare outputs, and build a per-task-type quality map. This avoids the need for human judges by using embedding similarity and the premium model's output as a reference.

**Source:** [arxiv.org/abs/2403.04132](https://arxiv.org/html/2403.04132v1), [LMSYS Blog](https://www.lmsys.org/blog/2023-12-07-leaderboard/)

### 3.2 LLM-as-Judge Reliability

#### Key Findings (2024–2025 Literature)

| Finding | Source | Implication |
|---------|--------|-------------|
| Small fine-tuned judges achieve high accuracy on narrow domains but poor generalizability | ACL 2025 (Findings) | Don't rely on a single small judge model |
| Differences of 20–34 points on absolute scales between small judges and humans | "Judging the Judges" (2024) | Use relative (pairwise) comparisons, not absolute scores |
| JudgeLM-7B outperforms larger models at *ranking* (not scoring) | "Judging the Judges" (2024) | Ranking-based eval is more robust than scoring |
| Even GPT-4o performs only modestly above random on hardest cases | JudgeBench (ICLR 2025) | Don't assume any model is a perfect judge |
| Multi-judge voting and ensemble methods significantly reduce bias | Survey on LLM-as-a-Judge (ScienceDirect 2025) | Use 3+ judge evaluations |
| Small models exhibit length bias, position bias, self-enhancement bias | Multiple 2024 sources | Control for these in eval design |

**Sources:**
- "Can You Trust LLM Judgments?" [arXiv:2412.12509](https://arxiv.org/abs/2412.12509)
- "Judging the Judges" [HuggingFace Papers](https://huggingface.co/papers/2406.12624)
- "JudgeBench" [OpenReview ICLR 2025](https://openreview.net/forum?id=G0dksFayVq)
- "A Survey on LLM-as-a-Judge" [ScienceDirect 2025](https://www.sciencedirect.com/science/article/pii/S2666675825004564)

#### Nexus-Applicable Approach

For a Go gateway without local models, LLM-as-Judge is expensive (requires an extra API call per evaluation). Instead, Nexus should use:

1. **Embedding-based comparison** (primary signal): Compare cheap vs. premium response embeddings using BGE-M3. This reuses existing infrastructure.
2. **Structural checks** (secondary): Response format compliance, code syntax validity, JSON parsability.
3. **Periodic LLM-as-Judge sampling** (calibration): Every Nth shadow comparison, use the premium model itself to judge which response is better. This calibrates the embedding threshold.

### 3.3 Self-Consistency Checking

#### Method

Sample the model N times with temperature >0 on the same prompt. The **self-consistency score** = fraction of samples agreeing with the majority answer.

#### Key Research Findings

| Finding | Source |
|---------|--------|
| Error rate decays exponentially with N samples | Wang et al. 2022 (original self-consistency) |
| N=3 captures 80%+ of the benefit of larger N | Empirical consensus in literature |
| Confidence-Informed Self-Consistency (CISC) reduces needed N by 40% | ACL 2025 Findings |
| Adaptive stopping: halt early if consensus reached after 2 samples | ASC / Blend-ASC methods |
| Semantic similarity between responses is more robust than exact-match voting | Multiple 2024 sources |

#### Nexus Implementation

For the cascade use case:
- **Default N=2** (parallel): Two calls to the cheap model. If cosine similarity >0.85 between responses, high confidence. If <0.70, low confidence. Between: moderate confidence.
- **Optional N=3** for high-value requests where the cost of a bad response exceeds 3× cheap model cost.
- **Use adaptive stopping:** If first 2 responses agree (similarity >0.85), don't send a 3rd.

### 3.4 Embedding Similarity Thresholds for BGE-M3

#### Research-Backed Thresholds

| Cosine Similarity Range | Interpretation | Action in Nexus |
|------------------------|----------------|-----------------|
| ≥ 0.90 | Near-paraphrase / identical meaning | Accept cheap response with high confidence |
| 0.82–0.89 | Strong semantic equivalence | Accept cheap response |
| 0.70–0.81 | Topically related but quality gap possible | Flag for review / marginal escalation zone |
| 0.50–0.69 | Moderate similarity; likely quality gap | Escalate to premium |
| < 0.50 | Dissimilar | Definite escalation |

**Nexus threshold recommendation:** Use **0.82** as the accept/escalate boundary for BGE-M3 cosine similarity between cheap and premium model responses.

**Source:** [arxiv.org/abs/2402.03216](https://arxiv.org/html/2402.03216v3) (BGE-M3 paper), MIRACL/MKQA benchmarks

### 3.5 Minimum Shadow Comparisons for Statistical Reliability

#### Power Analysis

To build a statistically reliable confidence map per task type:

| Effect Size (Cohen's d) | Samples per Task Type | Power | α |
|--------------------------|----------------------|-------|-----|
| Large (0.8) | 26 per group | 80% | 0.05 |
| Medium (0.5) | 64 per group | 80% | 0.05 |
| Small (0.2) | 394 per group | 80% | 0.05 |

**For Nexus's shadow comparison pipeline:**

- **Minimum:** 100 shadow comparisons per task type to detect medium effect sizes with 80% power and account for real-world noise (annotator-equivalent variance in embedding comparisons).
- **Recommended:** 200 shadow comparisons per task type for production confidence, especially for nuanced tasks like code generation and summarization.
- **With 10+ task types:** 2,000 total shadow comparisons needed before the confidence map is production-ready.
- **At 1,000 requests/day:** ~2–3 days of shadow traffic (sampling 100% of traffic). At 10% shadow sampling rate: ~20–30 days.

**Recommendation:** Run at 100% shadow rate during the first 2 weeks, then drop to 5–10% for ongoing calibration.

**Sources:** Cohen's power tables, [Statistical Analysis for Evaluation](https://neuromechanist.github.io/blog/007-stat-analysis-evaluation/), [Northwestern Power Calculations Guide](https://www.preventivemedicine.northwestern.edu/docs/applied-statistics-presentation-materials/sample-size-and-power-presentation.pdf)

### 3.6 Eval Pipeline Architecture

```
Every request (during shadow phase):
    ├── Send to Cheap Model (real response, returned to user)
    └── Send to Premium Model (shadow, async, not returned)
            │
            ├── Compute BGE-M3 embeddings for both responses
            ├── Compute cosine similarity
            ├── Record: {task_type, similarity, cheap_model, premium_model, timestamp}
            └── Every 100th pair: LLM-as-Judge calibration check

Aggregation (per task type):
    ├── Compute mean similarity, std deviation
    ├── Compute cheap model "equivalence rate" (% of responses with similarity ≥ 0.82)
    └── Update confidence map: task_type → {threshold, escalation_rate, confidence}
```

---

## 4. Prompt Compression Research

### 4.1 LLMLingua Methodology

**LLMLingua** (Microsoft Research, 2023–2024) is the leading prompt compression framework:

#### Algorithm

1. **Budget Controller:** Allocates compression budgets per prompt segment (instructions get less compression than examples/context).
2. **Token-Level Iterative Pruning:** Uses a small reference model (GPT-2 or LLaMA-7B) to compute per-token perplexity. Low-perplexity tokens (highly predictable) are pruned first.
3. **Distribution Alignment:** Fine-tunes the pruning model's decisions to align with the target LLM's needs.

#### Results

- **Compression:** Up to 20× for in-context learning prompts; 40–70% of tokens safely removable.
- **Quality:** Near-zero degradation on GSM8K, BBH, ShareGPT, Arxiv benchmarks.
- **Limitation for Nexus:** Requires a local reference model (GPT-2 or LLaMA) for perplexity calculation — **not feasible** in a zero-external-dependency Go gateway.

**LLMLingua-2** improves on v1 using XLM-RoBERTa for task-agnostic, cross-lingual compression.

**Source:** [microsoft.com/research/blog/llmlingua](https://www.microsoft.com/en-us/research/blog/llmlingua-innovating-llm-efficiency-with-prompt-compression/), [github.com/microsoft/LLMLingua](https://github.com/microsoft/LLMLingua)

### 4.2 Pure Go Compression Strategies (No ML Models)

Since Nexus has zero external dependencies beyond `yaml.v3`, all compression must use Go stdlib. Here's what's achievable:

#### 4.2.1 Whitespace Normalization
```go
// Collapse multiple spaces/newlines to single
re := regexp.MustCompile(`\s+`)
compressed := re.ReplaceAllString(input, " ")
```
- **Savings:** 5–15% depending on input formatting
- **Risk:** None (purely cosmetic)

#### 4.2.2 Filler Word Removal
```go
var fillerPatterns = regexp.MustCompile(
    `(?i)\b(basically|actually|literally|essentially|obviously|` +
    `simply|just|really|very|quite|rather|somewhat|pretty much|` +
    `in order to|as a matter of fact|it is worth noting that|` +
    `it should be noted that|needless to say)\b`)
```
- **Savings:** 3–8% on conversational prompts
- **Risk:** Low; these words rarely carry semantic weight in LLM prompts

#### 4.2.3 System Prompt Deduplication
- **Problem:** In multi-turn conversations, the system prompt is sent with every request.
- **Solution:** Hash system prompts; on repeated requests, send a reference ID (if provider supports it) or leverage provider-side prefix caching.
- **Savings:** 0% on wire tokens (provider still needs the tokens), but significant cost savings via cache hits (see §4.3).

#### 4.2.4 Conversation History Compression ("Keep Last N + Summarize Rest")

**Research-backed optimal strategy:**

- **Keep last 4–8 user/assistant turn pairs verbatim** (high-fidelity recent context)
- **Summarize older history** into a condensed block
- **Token allocation:** 80% of context budget for raw messages + summary; 20% reserved for system prompt + output generation
- **Summarization:** Since Nexus can't run a local summarizer, use the **cheap model itself** to summarize older turns before the main request. Cost: ~$0.001–0.005 per summarization.

**Implementation:**
```
Context budget: 4096 tokens (example for gpt-4o-mini)
    - System prompt: ~500 tokens (reserved)
    - Output reserve: ~500 tokens
    - Available for messages: ~3096 tokens
    - Last 6 turns: ~2000 tokens (verbatim)
    - Summary of older turns: ~1000 tokens (compressed)
```

**Source:** [Microsoft Agent Framework Blog](https://devblogs.microsoft.com/agent-framework/managing-chat-history-for-large-language-models-llms/), [mem0.ai Blog](https://mem0.ai/blog/llm-chat-history-summarization-guide-2025)

#### 4.2.5 Code Block Compression

Safe operations for code in prompts:

| Technique | Token Savings | Safety |
|-----------|---------------|--------|
| Strip comments (`//`, `/* */`, `#`) | 10–25% | Safe — comments are ignored by compilers |
| Collapse whitespace (non-Python) | 15–34% (C-family), 9% (Python) | Safe if AST-preserving |
| Remove blank lines | 5–10% | Safe |
| Remove import statements for well-known libraries | 3–5% | Moderate risk — LLM may need them for context |
| Minify variable names | 10–20% | **Dangerous** — harms LLM comprehension |

**Recommendation:** Strip comments and collapse whitespace only. Preserve variable names and structure.

**Language-aware compression in pure Go:**
```go
func compressCodeBlock(code string, lang string) string {
    // Strip single-line comments
    code = regexp.MustCompile(`//.*$`).ReplaceAllString(code, "")
    // Strip block comments
    code = regexp.MustCompile(`/\*[\s\S]*?\*/`).ReplaceAllString(code, "")
    // Collapse blank lines
    code = regexp.MustCompile(`\n\s*\n`).ReplaceAllString(code, "\n")
    if lang != "python" { // Preserve significant whitespace
        code = regexp.MustCompile(`\s+`).ReplaceAllString(code, " ")
    }
    return strings.TrimSpace(code)
}
```

**Source:** [Deblank (reversible code minifier)](https://github.com/anpl-code/Deblank), [LoPace (arXiv:2602.13266)](https://arxiv.org/html/2602.13266v1)

#### 4.2.6 Achievable Compression Summary (Pure Go)

| Technique | Compression | Implementation |
|-----------|-------------|----------------|
| Whitespace normalization | 5–15% | `regexp` + `strings` |
| Filler word removal | 3–8% | `regexp` |
| Code comment stripping | 10–25% (code blocks) | `regexp` |
| Code whitespace collapse | 9–34% (code blocks) | `regexp` |
| Markdown formatting removal | 3–5% | `regexp` |
| Duplicate system prompt caching | 0% wire / 50–90% cost | SHA-256 hash + provider caching |
| Conversation summarization | 40–60% of old turns | Cheap model API call |

**Combined realistic savings (typical mixed prompt):** **20–35% token reduction** using pure Go string ops, before considering provider-side caching benefits.

### 4.3 Provider Prefix Caching

#### OpenAI (GPT-4o and newer)

| Property | Value |
|----------|-------|
| Activation | **Automatic**, no code changes needed |
| Minimum prefix | 1024 tokens |
| Cache read price | **50% of regular input token price** |
| Cache retention | 5–10 min idle, up to 1 hour |
| Match requirement | **Byte-for-byte identical prefix** |

**Implication for Nexus:** Structure all requests with static content (system prompt, tool definitions, few-shot examples) at the beginning. Nexus should **canonicalize system prompts** — normalize whitespace, sort tool definitions deterministically — to maximize cache hit rates.

#### Anthropic (Claude 3 family)

| Property | Value |
|----------|-------|
| Activation | **Manual** — requires `cache_control` markers in API request |
| Minimum prefix | 1024 tokens (Sonnet/Opus), 2048 tokens (Haiku) |
| Cache creation price | **1.25× base input price** (first request) |
| Cache read price | **0.1× base input price (90% savings)** |
| Cache retention | 5 min idle, reset on each hit |
| Max breakpoints | 4 per request |

**Implication for Nexus:** Nexus should automatically inject `cache_control` markers for Anthropic requests when the system prompt exceeds 1024 tokens. This is pure JSON manipulation — no dependency needed.

**Break-even:** Anthropic's cache breaks even on the **2nd request** with the same prefix. For Nexus's use case (many users sharing system prompts), ROI is immediate.

**Sources:** [OpenAI API Docs](https://developers.openai.com/api/docs/guides/prompt-caching), [Anthropic Prompt Caching](https://masterprompting.net/blog/prompt-caching-strategies-anthropic-openai)

### 4.4 Comparison: Pure Go vs. ML-Based Compression

| Approach | Token Reduction | Latency | Dependencies | Quality Risk |
|----------|----------------|---------|--------------|--------------|
| Pure Go regex/string ops | 20–35% | <5ms | None (stdlib) | Minimal |
| Statistical (TF-IDF extractive) | 50–70% | 10–50ms | Small Go library | Low-Medium |
| LLMLingua (local model) | 80–95% | 100–500ms | Python + PyTorch + model weights | Minimal |
| LLM-based summarization | 60–80% | 200–1000ms | External API call | Low |

**Recommendation for Nexus:** Phase 1 uses pure Go (20–35%). Phase 2 adds conversation summarization via cheap model calls (up to 50%). Phase 3 evaluates if a sidecar compression service is warranted.

---

## 5. A/B Testing Framework

### 5.1 General Architecture

Nexus should implement A/B testing at the gateway level using **request-level random assignment**:

```go
type ABTest struct {
    ID          string
    Feature     string        // "cascade", "eval", "compression"
    TrafficPct  float64       // % of traffic in treatment group (e.g., 0.10)
    StartTime   time.Time
    EndTime     time.Time
    Metrics     []MetricDef
}

func (gw *Gateway) assignGroup(req *Request, test *ABTest) string {
    // Deterministic assignment based on request hash for consistency
    hash := sha256.Sum256([]byte(req.UserID + test.ID))
    if float64(hash[0]) / 255.0 < test.TrafficPct {
        return "treatment"
    }
    return "control"
}
```

**Key principles:**
- **Deterministic assignment:** Same user always gets same group (hash-based)
- **Isolation:** Only one feature under test per user at a time (avoid interaction effects)
- **Logging:** Every request logs: `{test_id, group, latency_ms, cost_usd, quality_score, model_used, escalated}`

### 5.2 Feature-Specific A/B Tests

#### 5.2.1 Cascade Routing A/B Test

| Parameter | Value |
|-----------|-------|
| **Control** | Current keyword-based routing (direct to classified tier) |
| **Treatment** | Cascade: cheap model first → confidence check → escalate if needed |
| **Traffic split** | 10% treatment initially, ramp to 50% |
| **Primary metrics** | Cost per request ($), quality score (embedding similarity to premium baseline), P95 latency |
| **Secondary metrics** | Escalation rate, user feedback rate (thumbs up/down if available), cache hit rate |
| **Duration** | 14–21 days minimum |
| **Sample size** | ≥3,000 requests per group for medium effect detection (80% power, α=0.05) |
| **Go/No-Go criteria** | Treatment cost ≤70% of control AND quality score ≥95% of control AND P99 latency <3s |

#### 5.2.2 Eval Pipeline A/B Test

| Parameter | Value |
|-----------|-------|
| **Control** | Static confidence thresholds (fixed 0.78) |
| **Treatment** | Dynamic thresholds from eval pipeline (per-task-type adaptive) |
| **Traffic split** | 20% treatment (needs volume for per-task-type stats) |
| **Primary metrics** | Escalation rate, quality score, cost per request |
| **Secondary metrics** | Quality variance across task types, calibration drift |
| **Duration** | 21–28 days (needs time to build per-task-type maps) |
| **Sample size** | ≥200 per task type (≥2,000 total across 10 task types) |
| **Go/No-Go criteria** | Treatment quality ≥ control AND escalation rate ≤ control × 0.9 (10% fewer unnecessary escalations) |

#### 5.2.3 Prompt Compression A/B Test

| Parameter | Value |
|-----------|-------|
| **Control** | Uncompressed prompts sent directly to provider |
| **Treatment** | Compressed prompts (pure Go pipeline) |
| **Traffic split** | 20% treatment |
| **Primary metrics** | Token count (input), cost per request, quality score (embed similarity of compressed vs. uncompressed outputs) |
| **Secondary metrics** | Compression ratio achieved, TTFT (time-to-first-token), provider cache hit rate |
| **Duration** | 7–14 days |
| **Sample size** | ≥1,500 requests per group |
| **Go/No-Go criteria** | Token savings ≥15% AND quality score degradation <2% AND no P95 latency regression |

### 5.3 Statistical Framework

#### Sample Size Formula (Two-Proportion Z-Test)

```
n = (Z_α/2 + Z_β)² × [p₁(1-p₁) + p₂(1-p₂)] / (p₁ - p₂)²
```

For detecting a **10% relative improvement** in success rate from 80% to 88%:
- α = 0.05 (two-tailed) → Z = 1.96
- β = 0.20 (80% power) → Z = 0.84
- **n ≈ 199 per group**

For detecting a **5% relative improvement** (80% to 84%):
- **n ≈ 1,290 per group**

#### Duration Estimation

| Daily traffic | 5% MDE | 10% MDE | 20% MDE |
|---------------|--------|---------|---------|
| 1,000 req/day | 26 days | 4 days | 2 days |
| 5,000 req/day | 6 days | 1 day | 1 day |
| 10,000 req/day | 3 days | 1 day | 1 day |

**Note:** Always run for at least **7 days** regardless of sample size to capture weekly patterns.

### 5.4 Metrics Collection Schema

```go
type ABMetric struct {
    TestID       string    `json:"test_id"`
    Group        string    `json:"group"` // "control" or "treatment"
    RequestID    string    `json:"request_id"`
    Timestamp    time.Time `json:"timestamp"`
    TaskType     string    `json:"task_type"`
    ModelUsed    string    `json:"model_used"`
    Escalated    bool      `json:"escalated"`
    LatencyMs    int64     `json:"latency_ms"`
    InputTokens  int       `json:"input_tokens"`
    OutputTokens int       `json:"output_tokens"`
    CostUSD      float64   `json:"cost_usd"`
    QualityScore float64   `json:"quality_score"` // 0.0–1.0
    Confidence   float64   `json:"confidence"`    // cascade confidence score
    CacheHit     bool      `json:"cache_hit"`
    Compressed   bool      `json:"compressed"`
    ComprRatio   float64   `json:"compr_ratio"`
}
```

---

## 6. Recommended Implementation Parameters

### 6.1 Cascade Routing Parameters

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Default confidence threshold | **0.78** | Center of 0.70–0.85 empirical range, slightly biased toward escalation |
| Hedging word weight | **0.15** | Low reliability as sole signal |
| Length anomaly weight | **0.10** | Supplementary signal only |
| Self-consistency weight | **0.40** | Highest-accuracy proxy method |
| Embedding similarity weight | **0.35** | Leverages existing BGE-M3 infrastructure |
| Self-consistency N | **2** (parallel) | Captures 80%+ of benefit; 3 for high-value requests |
| Consistency similarity threshold | **0.85** (agree), **0.70** (disagree) | BGE-M3 empirical ranges |
| Max cascade latency budget | **500ms** | Beyond this, skip cascade and route directly |
| Direct-to-premium complexity threshold | **0.90** (complexity score) | Skip cascade for clearly complex requests |
| Task types to exclude from cascade | Code generation (>200 lines), legal, medical | High-stakes domains; always use premium |

### 6.2 Eval Pipeline Parameters

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Shadow sampling rate (initial) | **100%** for first 14 days | Build confidence map quickly |
| Shadow sampling rate (steady-state) | **5–10%** | Maintain calibration without excessive cost |
| BGE-M3 equivalence threshold | **0.82** | Accept cheap response as equivalent to premium |
| Minimum samples per task type | **100** (minimum), **200** (recommended) | 80% power for medium effect sizes |
| LLM-as-Judge calibration frequency | **Every 100th shadow pair** | Calibrate embedding threshold against model-based judgment |
| Confidence map update interval | **Every 50 new samples** | Balance freshness vs. stability |
| Confidence map staleness timeout | **7 days** | Revert to conservative routing if no fresh data |
| Quality alert threshold | **>5% drop in similarity** over 24h rolling window | Detect model degradation or distribution shift |

### 6.3 Prompt Compression Parameters

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Whitespace normalization | **Always on** | Zero risk, 5–15% savings |
| Filler word removal | **On for conversation; off for code/structured** | Safe for natural language |
| Code comment stripping | **On, except when user explicitly includes for context** | 10–25% savings on code blocks |
| Code whitespace collapse | **On for non-Python code** | 15–34% savings; Python needs significant whitespace |
| Conversation history window (N) | **6 turns verbatim** | Last 3 user + 3 assistant messages |
| History summarization trigger | **When total turns > 10** | Summarize turns 1 through (total-6) |
| Summarization model | **Cheapest available (e.g., gpt-4o-mini)** | $0.001–0.005 per summarization |
| System prompt canonicalization | **Always on** | Maximize provider cache hits |
| Anthropic cache_control injection | **Auto for system prompts >1024 tokens** | 90% cost savings on cache reads |
| Max compression ratio cap | **50%** | Safety limit — don't over-compress |
| Compression bypass for prompts <100 tokens | **Yes** | Not worth the overhead |

### 6.4 Combined Feature Interaction

```
Request Flow with All Features Enabled:

1. Prompt Compression (§4)
   - Normalize whitespace, remove fillers
   - Compress code blocks
   - Summarize old conversation history
   - Inject cache_control for Anthropic
   
2. Complexity Classification (existing)
   - Score request complexity
   - If complexity ≥ 0.90 → direct to premium (skip cascade)
   
3. Cascade Routing (§2)
   - Send compressed prompt to cheap model
   - Score confidence (composite: hedge + length + consistency + embedding)
   - If confidence ≥ 0.78 → return cheap response
   - If confidence < 0.78 → send compressed prompt to premium model
   
4. Eval Pipeline (§3, async)
   - Shadow-compare cheap vs. premium on sampled requests
   - Update per-task-type confidence maps
   - Adjust cascade thresholds based on observed equivalence rates
```

---

## 7. Risk Assessment

### 7.1 Cascade Routing Risks

| Risk | Severity | Likelihood | Mitigation |
|------|----------|------------|------------|
| Cheap model produces confidently wrong answer (high confidence, low quality) | **High** | Medium | Self-consistency check catches most cases; periodic eval pipeline calibration |
| Cascade adds unacceptable latency for real-time use cases | Medium | Low | Latency budget (500ms cap); fast-path for low-latency requests |
| Escalation rate too high (>40%), negating cost savings | Medium | Medium | Per-task-type threshold tuning; exclude complex task types |
| Provider rate limits hit due to parallel consistency calls | Medium | Low-Medium | Rate limiter integration; graceful degradation to single-call mode |
| Confidence scoring biased toward certain response styles | Medium | Medium | Regular calibration via eval pipeline; monitor per-task-type |

### 7.2 Eval Pipeline Risks

| Risk | Severity | Likelihood | Mitigation |
|------|----------|------------|------------|
| Shadow premium calls double cost during ramp-up phase | **High** | **High** | Budget cap on shadow calls; phase from 100% → 5% over 2 weeks |
| Embedding similarity doesn't capture quality for all task types (e.g., creative writing) | Medium | High | Supplement with periodic LLM-as-Judge; per-task-type thresholds |
| Confidence map becomes stale if traffic patterns shift | Medium | Medium | 7-day staleness timeout; alert on distribution drift |
| BGE-M3 embedding model changes or degrades | Low | Low | Pin model version; monitor embedding consistency |

### 7.3 Prompt Compression Risks

| Risk | Severity | Likelihood | Mitigation |
|------|----------|------------|------------|
| Over-aggressive compression removes semantically important content | **High** | Low-Medium | 50% max compression cap; exclude instructions from aggressive compression |
| Code compression breaks Python (significant whitespace) | Medium | Medium | Language detection; skip whitespace collapse for Python |
| Filler word removal changes meaning in edge cases ("just" as temporal word, "actually" as correction marker) | Low | Medium | Maintain allowlist of context-dependent words; conservative default |
| Provider caching disrupted by non-deterministic prompt elements (timestamps, UUIDs) | Medium | High | Move all variable content to end of prompt; strip timestamps from cached portions |
| Conversation summarization loses critical context from early turns | Medium | Medium | Include entity/fact extraction in summary prompt; keep all tool results verbatim |

### 7.4 Cross-Feature Risks

| Risk | Severity | Likelihood | Mitigation |
|------|----------|------------|------------|
| Compression + Cascade interaction: compressed prompt changes cheap model behavior, invalidating confidence map | Medium | Medium | Build confidence map on compressed prompts (not original) |
| Three features multiplying latency overhead | Medium | Low | Pipeline architecture (compression → classify → cascade), not serial independent steps |
| Debugging complexity with three new features interacting | **High** | **High** | Feature flags per feature; detailed structured logging; gradual rollout |
| A/B test interaction effects (user in multiple treatment groups) | Medium | Medium | One feature test per user at a time; sequential feature rollouts |

### 7.5 Overall Risk Rating

| Feature | Implementation Risk | Operational Risk | Recommendation |
|---------|--------------------|--------------------|----------------|
| Prompt Compression | **Low** | **Low** | Ship first — safest, highest confidence |
| Cascade Routing | **Medium** | **Medium** | Ship second — needs eval pipeline for tuning |
| Eval Pipeline | **Medium-High** | **Medium** | Ship third (or alongside cascade) — highest ROI when combined with cascade |

**Recommended rollout order:**
1. **Week 1–2:** Prompt Compression (pure Go, low risk, immediate savings)
2. **Week 2–4:** Eval Pipeline (shadow mode, building confidence maps)
3. **Week 4–6:** Cascade Routing (using confidence maps from eval pipeline)
4. **Week 6–8:** A/B test all three combined vs. baseline

---

## Appendix A: Key References

### Cascade Routing
1. Google, "Speculative Cascades: Accelerating LLM Inference through Adaptive Cascading and Speculative Decoding" — [arXiv:2405.19261](https://arxiv.org/pdf/2405.19261) (Oct 2024)
2. Chuang et al., "Learning to Route LLMs with Confidence Tokens" — [arXiv:2410.13284](https://arxiv.org/abs/2410.13284) (Oct 2024, ICML 2025)
3. Dekoninck et al., "A Unified Approach to Routing and Cascading for LLMs" — [arXiv:2410.10347](https://arxiv.org/abs/2410.10347) (Oct 2024, ICML 2025)
4. Zellinger & Thomson, "Rational Tuning of LLM Cascades via Probabilistic Modeling" — [arXiv:2501.09345](https://arxiv.org/html/2501.09345v1) (Jan 2025)
5. Zhang et al., "Efficient Contextual LLM Cascades through Budget-Constrained Policy Learning (TREACLE)" — [NeurIPS 2024](https://proceedings.neurips.cc/paper_files/paper/2024/file/a6deba3b2408af45b3f9994c2152b862-Paper-Conference.pdf)
6. Yue et al., "Large Language Model Cascades with Mixture of Thought Representations" — [SouthNLP 2024](https://southnlp.github.io/southnlp2024/papers/southnlp2024-poster-49.pdf)

### Eval Pipeline / Confidence Scoring
7. Chiang et al., "Chatbot Arena: An Open Platform for Evaluating LLMs by Human Preference" — [arXiv:2403.04132](https://arxiv.org/html/2403.04132v1) (2024)
8. Bavaresco et al., "Can You Trust LLM Judgments? Reliability of LLM-as-a-Judge" — [arXiv:2412.12509](https://arxiv.org/abs/2412.12509) (2024)
9. Huang et al., "Judging the Judges: Evaluating Alignment and Vulnerabilities in LLMs-as-Judges" — [HuggingFace Papers 2406.12624](https://huggingface.co/papers/2406.12624) (2024)
10. Tan et al., "JudgeBench: A Benchmark for Evaluating LLM-Based Judges" — [ICLR 2025](https://openreview.net/forum?id=G0dksFayVq)
11. Li et al., "A Survey on LLM-as-a-Judge" — [ScienceDirect 2025](https://www.sciencedirect.com/science/article/pii/S2666675825004564)
12. Wang et al., "Self-Consistency Improves Chain of Thought Reasoning in Language Models" — [arXiv:2203.11171](https://arxiv.org/abs/2203.11171) (2022)
13. "Confidence Improves Self-Consistency in LLMs" — [ACL 2025 Findings](https://aclanthology.org/2025.findings-acl.1030/)
14. Portillo Wightman et al., "Strength in Numbers: Estimating Confidence of LLMs by Prompt Agreement" — [ACL 2023 TrustNLP](https://aclanthology.org/2023.trustnlp-1.28.pdf)
15. Chen et al., "BGE M3-Embedding: Multi-Lingual, Multi-Functionality, Multi-Granularity" — [arXiv:2402.03216](https://arxiv.org/html/2402.03216v3) (2024)

### Prompt Compression
16. Jiang et al., "LLMLingua: Compressing Prompts for Accelerated Inference of Large Language Models" — [Microsoft Research](https://www.microsoft.com/en-us/research/blog/llmlingua-innovating-llm-efficiency-with-prompt-compression/) (2023)
17. "LLMLingua-2: Data Distillation for Efficient and Faithful Task-Agnostic Prompt Compression" — [github.com/microsoft/LLMLingua](https://github.com/microsoft/LLMLingua) (2024)
18. "LoPace: A Lossless Optimized Prompt Accurate Compression Engine" — [arXiv:2602.13266](https://arxiv.org/html/2602.13266v1) (2025)
19. "Deblank: A Reversible Code Minifier for AI" — [github.com/anpl-code/Deblank](https://github.com/anpl-code/Deblank)
20. OpenAI, "Prompt Caching" — [API Documentation](https://developers.openai.com/api/docs/guides/prompt-caching)
21. Anthropic, "Prompt Caching" — [API Documentation](https://masterprompting.net/blog/prompt-caching-strategies-anthropic-openai)

### A/B Testing & Statistics
22. Cohen, J., "Statistical Power Analysis for the Behavioral Sciences" (1988)
23. [Northwestern Power Calculations Guide](https://www.preventivemedicine.northwestern.edu/docs/applied-statistics-presentation-materials/sample-size-and-power-presentation.pdf)

---

## Appendix B: Glossary

| Term | Definition |
|------|-----------|
| **Cascade** | Sequential model invocation: try cheap → escalate to premium if needed |
| **Routing** | One-shot model selection based on pre-execution prediction |
| **Confidence score** | 0.0–1.0 estimate of response quality/correctness |
| **Shadow comparison** | Sending request to both cheap and premium models; comparing outputs |
| **BGE-M3** | BAAI General Embedding M3: multilingual, multi-granularity embedding model |
| **Self-consistency** | Sampling N responses from same model; measuring agreement |
| **Prefix caching** | Provider-side KV cache reuse for identical prompt prefixes |
| **TTFT** | Time to first token — latency until the first response byte |
| **MDE** | Minimum detectable effect — smallest difference an A/B test can detect |
| **Cohen's d** | Standardized measure of effect size (small=0.2, medium=0.5, large=0.8) |
| **Bradley-Terry** | Statistical model for pairwise comparison (used by LMSYS Chatbot Arena) |
| **λ (lambda)** | Quality-cost trade-off parameter in ETH Zurich's cascade-routing formula |
