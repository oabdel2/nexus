package benchmarks

import (
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nexus-gateway/nexus/internal/cache"
	"github.com/nexus-gateway/nexus/internal/config"
	"github.com/nexus-gateway/nexus/internal/router"
	"github.com/nexus-gateway/nexus/internal/workflow"
)

// ---------------------------------------------------------------------------
// Helper: build a router with mock providers covering all 4 tiers
// ---------------------------------------------------------------------------

func testProviders() []config.ProviderConfig {
	return []config.ProviderConfig{
		{
			Name:    "mock-openai",
			Type:    "openai",
			Enabled: true,
			Models: []config.ModelConfig{
				{Name: "gpt-4o", Tier: "premium", CostPer1K: 0.03},
				{Name: "gpt-4o-mini", Tier: "mid", CostPer1K: 0.01},
				{Name: "gpt-3.5-turbo", Tier: "cheap", CostPer1K: 0.002},
				{Name: "gpt-3.5-eco", Tier: "economy", CostPer1K: 0.0005},
			},
		},
	}
}

func testRouter() *router.Router {
	cfg := config.DefaultConfig()
	// Use a discarding logger to avoid log noise in benchmarks
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return router.New(cfg.Router, testProviders(), logger)
}

func testRouterCfg() config.RouterConfig {
	return config.DefaultConfig().Router
}

// ---------------------------------------------------------------------------
// Unit Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkClassifyComplexity(b *testing.B) {
	prompts := []struct {
		name   string
		prompt string
	}{
		{"simple_greeting", "Hello"},
		{"short_question", "What is 2+2?"},
		{"medium_code", "Write a Python function to reverse a string"},
		{"complex_debug", "Debug this race condition in the concurrent goroutine pool"},
		{"complex_security", "Analyze the security vulnerability and implement a production fix"},
	}

	for _, p := range prompts {
		b.Run(p.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				router.ClassifyComplexity(p.prompt, "", 0.0, 1.0, 0)
			}
		})
	}
}

func BenchmarkExactCacheSet(b *testing.B) {
	// Large maxEntries avoids O(n) eviction overhead distorting the benchmark
	c := cache.NewExactCache(1*time.Hour, b.N+1)
	data := []byte(`{"id":"resp-1","choices":[{"message":{"content":"ok"}}]}`)

	// Pre-compute keys to isolate Set cost
	keys := make([]string, b.N)
	for i := range keys {
		keys[i] = cache.HashKey(fmt.Sprintf("prompt-%d", i), "model")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Set(keys[i], data)
	}
}

func BenchmarkExactCacheGet(b *testing.B) {
	c := cache.NewExactCache(1*time.Hour, 100000)
	data := []byte(`{"id":"resp-1","choices":[{"message":{"content":"ok"}}]}`)

	// Pre-populate
	for i := 0; i < 1000; i++ {
		key := cache.HashKey(fmt.Sprintf("prompt-%d", i), "model")
		c.Set(key, data)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := cache.HashKey(fmt.Sprintf("prompt-%d", i%1000), "model")
		c.Get(key)
	}
}

func BenchmarkHashKey(b *testing.B) {
	for i := 0; i < b.N; i++ {
		cache.HashKey("Explain the difference between TCP and UDP", "gpt-4o")
	}
}

func BenchmarkRouterRoute(b *testing.B) {
	r := testRouter()

	cases := []struct {
		name   string
		prompt string
		role   string
		step   float64
		budget float64
		ctxLen int
	}{
		{"economy_simple", "Hello", "", 0.0, 1.0, 0},
		{"mid_code", "Write a Python function to reverse a string", "developer", 0.2, 1.0, 500},
		{"premium_debug", "Debug this race condition in the concurrent goroutine pool", "engineer", 0.1, 0.5, 3000},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				r.Route(tc.prompt, tc.role, tc.step, tc.budget, tc.ctxLen)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Integration Tests – Routing Accuracy
// ---------------------------------------------------------------------------
//
// Tier thresholds (default threshold = 0.7):
//   premium  : FinalScore >= 0.56  (threshold * 0.8)
//   mid      : FinalScore >= 0.35  (threshold * 0.5)
//   cheap    : FinalScore >= 0.21  (threshold * 0.3)
//   economy  : FinalScore <  0.21
//
// FinalScore = basePrompt*0.30 + ContextScore*0.15 + RoleScore*0.20
//            + PositionScore*0.15 + BudgetScore*0.20
//   where basePrompt = PromptScore*0.6 + LengthScore*0.2 + StructScore*0.2

func TestRoutingAccuracy(t *testing.T) {
	r := testRouter()

	cases := []struct {
		prompt       string
		role         string
		stepRatio    float64
		budgetRatio  float64
		contextLen   int
		expectedTier string
	}{
		// ── economy tier (FinalScore < 0.21) ────────────────────
		{
			// low kw "hello" → PS=0, len=5 → LS≈0, struct=0
			prompt: "Hello", role: "", stepRatio: 0.0, budgetRatio: 1.0, contextLen: 0,
			expectedTier: "economy",
		},
		{
			// low kw "list" → PS=0, no struct indicators
			prompt: "List all files in the directory", role: "", stepRatio: 0.0, budgetRatio: 1.0, contextLen: 0,
			expectedTier: "economy",
		},
		{
			// low kw "summarize" → PS=0, role summarizer=0.25
			prompt: "Summarize this document for me", role: "summarizer", stepRatio: 0.0, budgetRatio: 1.0, contextLen: 0,
			expectedTier: "economy",
		},
		{
			// low kw "translate" → PS=0, step=0.5 → pos=0.5
			prompt: "Translate this error message to Spanish", role: "", stepRatio: 0.5, budgetRatio: 1.0, contextLen: 0,
			expectedTier: "economy",
		},
		{
			// low kw "format" → PS=0, role formatter=0.20, step=0.9 → pos=0.3
			prompt: "Format the JSON output nicely", role: "formatter", stepRatio: 0.9, budgetRatio: 1.0, contextLen: 0,
			expectedTier: "economy",
		},
		// ── cheap tier (0.21 <= FinalScore < 0.35) ──────────────
		{
			// no kw → PS=0.5, "?" → struct=0.1
			prompt: "What is 2+2?", role: "", stepRatio: 0.0, budgetRatio: 1.0, contextLen: 0,
			expectedTier: "cheap",
		},
		{
			// mid kw "write" + low kw "documentation" → PS=0.25
			prompt: "Write documentation for the API", role: "writer", stepRatio: 0.7, budgetRatio: 1.0, contextLen: 500,
			expectedTier: "cheap",
		},
		// ── mid tier (0.35 <= FinalScore < 0.56) ────────────────
		{
			// mid kw "write" → PS=0.5, role developer=0.80
			prompt: "Write a Python function to reverse a string", role: "developer", stepRatio: 0.2, budgetRatio: 1.0, contextLen: 500,
			expectedTier: "mid",
		},
		{
			// mid kw "explain" → PS=0.5, role analyst=0.75, ctx=2000
			prompt: "Explain the difference between TCP and UDP", role: "analyst", stepRatio: 0.0, budgetRatio: 0.8, contextLen: 2000,
			expectedTier: "mid",
		},
		{
			// mid kw "compare" → PS=0.5, role architect=0.90
			prompt: "Compare microservices vs monolith architecture", role: "architect", stepRatio: 0.3, budgetRatio: 0.7, contextLen: 1000,
			expectedTier: "mid",
		},
		{
			// mid kw "review" → PS=0.5, role reviewer=0.70, ctx=2000
			prompt: "Review this code for potential issues", role: "reviewer", stepRatio: 0.4, budgetRatio: 0.8, contextLen: 2000,
			expectedTier: "mid",
		},
		{
			// high kw: optimize, algorithm, performance, distributed → PS=1.0
			// but step=0.4 pos=0.5, budget=0.8 BS=0.2, ctx=1000 → mid range
			prompt: "Optimize the algorithm performance for distributed systems",
			role: "developer", stepRatio: 0.4, budgetRatio: 0.8, contextLen: 1000,
			expectedTier: "mid",
		},
		// ── premium tier (FinalScore >= 0.56) ───────────────────
		{
			// high kw: debug, race condition, concurrent → PS=1.0
			// role engineer=0.85, ctx=3000, budget=0.5 BS=0.5
			prompt: "Debug this race condition in the concurrent goroutine pool",
			role: "engineer", stepRatio: 0.1, budgetRatio: 0.5, contextLen: 3000,
			expectedTier: "premium",
		},
		{
			// high kw: analyze, security, vulnerability, implement, production, fix → PS=1.0
			// role engineer=0.85, ctx=4000
			prompt: "Analyze the security vulnerability and implement a production fix",
			role: "engineer", stepRatio: 0.1, budgetRatio: 0.6, contextLen: 4000,
			expectedTier: "premium",
		},
		{
			// high kw: architect, distributed → PS=1.0
			// role architect=0.90, ctx=3500, budget=0.4 BS=0.6
			prompt: "Architect a distributed event-sourcing system with CQRS",
			role: "architect", stepRatio: 0.0, budgetRatio: 0.4, contextLen: 3500,
			expectedTier: "premium",
		},
	}

	pass, fail := 0, 0
	for _, tc := range cases {
		sel := r.Route(tc.prompt, tc.role, tc.stepRatio, tc.budgetRatio, tc.contextLen)
		if sel.Tier != tc.expectedTier {
			t.Errorf("FAIL  prompt=%q  role=%q  expected=%s  got=%s  score=%.4f",
				tc.prompt, tc.role, tc.expectedTier, sel.Tier, sel.Score.FinalScore)
			fail++
		} else {
			t.Logf("OK    prompt=%q  tier=%s  score=%.4f", tc.prompt, sel.Tier, sel.Score.FinalScore)
			pass++
		}
	}
	t.Logf("Routing accuracy: %d/%d (%.0f%%)", pass, pass+fail, float64(pass)/float64(pass+fail)*100)
}

// ---------------------------------------------------------------------------
// Integration Tests – Cache Hit Rate
// ---------------------------------------------------------------------------

func TestCacheHitRate(t *testing.T) {
	c := cache.NewExactCache(1*time.Hour, 10000)
	data := []byte(`{"choices":[{"message":{"content":"cached response"}}]}`)

	prompts := []string{
		"Hello",
		"What is 2+2?",
		"Write a function",
		"Explain TCP vs UDP",
		"Debug the race condition",
	}

	// Phase 1: cold – all misses, populate cache
	for _, p := range prompts {
		key := cache.HashKey(p, "gpt-4o")
		_, hit := c.Get(key)
		if hit {
			t.Fatalf("unexpected cache hit on cold cache for %q", p)
		}
		c.Set(key, data)
	}

	// Phase 2: warm – all hits
	hitCount := 0
	for _, p := range prompts {
		key := cache.HashKey(p, "gpt-4o")
		_, hit := c.Get(key)
		if hit {
			hitCount++
		}
	}

	rate := float64(hitCount) / float64(len(prompts)) * 100
	if rate != 100 {
		t.Errorf("cache hit rate = %.0f%%, want 100%%", rate)
	}

	hits, misses, size := c.Stats()
	t.Logf("Cache stats: hits=%d  misses=%d  size=%d  hitRate=%.0f%%", hits, misses, size, rate)
}

// ---------------------------------------------------------------------------
// Integration Tests – Workflow Budget Downgrade
// ---------------------------------------------------------------------------

func TestWorkflowBudgetDowngrade(t *testing.T) {
	tracker := workflow.NewTracker(1.0, 1*time.Hour) // budget = $1.00
	r := testRouter()

	ws := tracker.GetOrCreate("budget-test-wf")
	ws.TotalSteps = 20

	prompt := "Debug this race condition in the concurrent goroutine pool"
	role := "engineer"

	type tierRecord struct {
		step       int
		tier       string
		budgetLeft float64
	}
	var records []tierRecord

	for i := 0; i < 20; i++ {
		budgetRatio := ws.GetBudgetRatio()
		stepRatio := ws.GetStepRatio()
		sel := r.Route(prompt, role, stepRatio, budgetRatio, 3000)

		// $0.06/step so budget drains: 15 steps → 0.10 left (<15%), 17 steps → -0.02
		ws.AddStep(workflow.StepRecord{
			Model:  sel.Model,
			Tier:   sel.Tier,
			Tokens: 2000,
			Cost:   0.06,
		})

		records = append(records, tierRecord{
			step:       i + 1,
			tier:       sel.Tier,
			budgetLeft: ws.BudgetLeft,
		})
	}

	// Budget overrides: premium→mid when ratio<0.15, force economy when ratio<0.05
	sawPremium, sawMid, sawEconomy := false, false, false
	var firstMid, firstEconomy int
	for _, rec := range records {
		switch rec.tier {
		case "premium":
			sawPremium = true
		case "mid":
			if !sawMid {
				firstMid = rec.step
			}
			sawMid = true
		case "economy":
			if !sawEconomy {
				firstEconomy = rec.step
			}
			sawEconomy = true
		}
		t.Logf("step=%2d  tier=%-8s  budgetLeft=$%.4f", rec.step, rec.tier, rec.budgetLeft)
	}

	if !sawPremium {
		t.Error("expected premium tier in early steps when budget is full")
	}
	if !sawMid {
		t.Error("expected mid tier when budget < 15% (premium downgrade)")
	}
	if !sawEconomy {
		t.Error("expected economy tier when budget < 5%")
	}
	if sawMid && sawEconomy && firstMid >= firstEconomy {
		t.Errorf("mid tier (step %d) should appear before economy tier (step %d)", firstMid, firstEconomy)
	}
	t.Logf("Budget downgrade: premium→mid at step %d, →economy at step %d", firstMid, firstEconomy)
}

// ---------------------------------------------------------------------------
// Integration Tests – Tier Fallback
// ---------------------------------------------------------------------------

func TestTierFallback(t *testing.T) {
	// Provider with only a cheap model – premium falls back through the tier order
	providers := []config.ProviderConfig{
		{
			Name:    "limited",
			Type:    "openai",
			Enabled: true,
			Models: []config.ModelConfig{
				{Name: "gpt-3.5-turbo", Tier: "cheap", CostPer1K: 0.002},
			},
		},
	}
	cfg := testRouterCfg()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := router.New(cfg, providers, logger)

	// This prompt routes to premium; fallback should find the cheap model
	sel := r.Route(
		"Debug this race condition in the concurrent goroutine pool",
		"engineer", 0.1, 0.5, 3000,
	)

	if sel.Model == "" {
		t.Fatal("expected a fallback model, got empty")
	}
	if sel.Model != "gpt-3.5-turbo" {
		t.Errorf("expected fallback to gpt-3.5-turbo, got %s", sel.Model)
	}
	t.Logf("Tier=%s requested, fallback model=%s provider=%s", sel.Tier, sel.Model, sel.Provider)
}

// ---------------------------------------------------------------------------
// Integration Tests – Harness Scenarios (dry-run, no HTTP)
// ---------------------------------------------------------------------------

func TestScenarioDryRun(t *testing.T) {
	r := testRouter()
	scenarios := []WorkflowScenario{SimpleBot(), CodeReview(), SecurityAudit()}

	for _, sc := range scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			match, total := 0, len(sc.Steps)
			for i, step := range sc.Steps {
				stepRatio := float64(i) / float64(total)
				sel := r.Route(step.Prompt, step.Role, stepRatio, 1.0, 0)
				if sel.Tier == step.ExpectedTier {
					match++
				}
				t.Logf("  step %d: tier=%s expected=%s score=%.4f prompt=%q",
					i+1, sel.Tier, step.ExpectedTier, sel.Score.FinalScore, step.Prompt)
			}
			accuracy := float64(match) / float64(total) * 100
			t.Logf("Scenario %q accuracy: %d/%d (%.0f%%)", sc.Name, match, total, accuracy)
		})
	}
}

// ---------------------------------------------------------------------------
// Composite Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkCacheOperations(b *testing.B) {
	data := []byte(`{"id":"1","choices":[{"message":{"content":"bench"}}]}`)

	b.Run("SetThenGet", func(b *testing.B) {
		c := cache.NewExactCache(1*time.Hour, b.N+1)
		keys := make([]string, b.N)
		for i := range keys {
			keys[i] = cache.HashKey(fmt.Sprintf("bench-%d", i), "m")
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			c.Set(keys[i], data)
			c.Get(keys[i])
		}
	})

	b.Run("HashOnly", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache.HashKey("Architect a distributed event-sourcing system with CQRS", "gpt-4o")
		}
	})
}

func BenchmarkRouterDecision(b *testing.B) {
	r := testRouter()

	b.Run("EconomyPath", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			r.Route("Hello", "", 0.0, 1.0, 0)
		}
	})
	b.Run("PremiumPath", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			r.Route("Debug this race condition in the concurrent goroutine pool", "engineer", 0.1, 0.5, 3000)
		}
	})
}
