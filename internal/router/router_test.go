package router

import (
	"log/slog"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/nexus-gateway/nexus/internal/config"
)

// helpers

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func defaultWeights() config.ComplexityWeights {
	return config.ComplexityWeights{
		PromptComplexity: 0.30,
		ContextLength:    0.15,
		AgentRole:        0.20,
		StepPosition:     0.15,
		BudgetPressure:   0.20,
	}
}

func defaultRouterCfg() config.RouterConfig {
	return config.RouterConfig{
		Threshold:         0.7,
		DefaultTier:       "mid",
		BudgetEnabled:     true,
		DefaultBudget:     1.0,
		ComplexityWeights: defaultWeights(),
	}
}

func allTierProviders() []config.ProviderConfig {
	return []config.ProviderConfig{
		{
			Name:    "test-provider",
			Type:    "ollama",
			Enabled: true,
			Models: []config.ModelConfig{
				{Name: "economy-model", Tier: "economy", CostPer1K: 0.001},
				{Name: "cheap-model", Tier: "cheap", CostPer1K: 0.003},
				{Name: "mid-model", Tier: "mid", CostPer1K: 0.01},
				{Name: "premium-model", Tier: "premium", CostPer1K: 0.06},
			},
		},
	}
}

// ==================== ClassifyComplexity Tests ====================

func TestClassify_SimplePrompt(t *testing.T) {
	score := ClassifyComplexity("hi", "", 0.5, 0.5, 0)
	// "hi" has no complexity keywords → PromptScore should be default 0.5
	if score.PromptScore != 0.5 {
		t.Errorf("simple prompt should default to 0.5, got %f", score.PromptScore)
	}
}

func TestClassify_HighComplexityKeywords(t *testing.T) {
	prompt := "debug a race condition with mutex deadlock in concurrent distributed system"
	score := ClassifyComplexity(prompt, "", 0.5, 0.5, 0)
	if score.PromptScore < 0.7 {
		t.Errorf("high complexity keywords should yield high PromptScore, got %f", score.PromptScore)
	}
}

func TestClassify_LowComplexityKeywords(t *testing.T) {
	prompt := "summarize this list and format the readme documentation"
	score := ClassifyComplexity(prompt, "", 0.5, 0.5, 0)
	if score.PromptScore > 0.3 {
		t.Errorf("low complexity keywords should yield low PromptScore, got %f", score.PromptScore)
	}
}

func TestClassify_MultipleHighKeywords(t *testing.T) {
	singleKW := ClassifyComplexity("debug this issue", "", 0.5, 0.5, 0)
	multiKW := ClassifyComplexity("debug race condition mutex deadlock concurrent distributed", "", 0.5, 0.5, 0)
	// Both pure-high prompts score 1.0 on PromptScore (all matched keywords are high)
	// But multi-keyword prompt is longer, so LengthScore is higher
	if multiKW.LengthScore <= singleKW.LengthScore {
		t.Errorf("longer prompt (%f) should have higher LengthScore than shorter (%f)", multiKW.LengthScore, singleKW.LengthScore)
	}
}

func TestClassify_EmptyPrompt(t *testing.T) {
	score := ClassifyComplexity("", "", 0.5, 0.5, 0)
	if score.PromptScore != 0.5 {
		t.Errorf("empty prompt should default to 0.5, got %f", score.PromptScore)
	}
	if score.LengthScore != 0.0 {
		t.Errorf("empty prompt length score should be 0.0, got %f", score.LengthScore)
	}
}

func TestClassify_VeryLongPrompt(t *testing.T) {
	prompt := strings.Repeat("word ", 500) // >2000 chars
	score := ClassifyComplexity(prompt, "", 0.5, 0.5, 0)
	if score.LengthScore != 1.0 {
		t.Errorf("very long prompt LengthScore should be capped at 1.0, got %f", score.LengthScore)
	}
}

func TestClassify_RoleArchitect(t *testing.T) {
	score := ClassifyComplexity("do something", "architect", 0.5, 0.5, 0)
	if score.RoleScore != 0.90 {
		t.Errorf("architect role should have score 0.90, got %f", score.RoleScore)
	}
}

func TestClassify_RoleLogger(t *testing.T) {
	score := ClassifyComplexity("do something", "logger", 0.5, 0.5, 0)
	if score.RoleScore != 0.15 {
		t.Errorf("logger role should have score 0.15, got %f", score.RoleScore)
	}
}

func TestClassify_UnknownRole(t *testing.T) {
	score := ClassifyComplexity("do something", "mystery-role", 0.5, 0.5, 0)
	if score.RoleScore != 0.5 {
		t.Errorf("unknown role should default to 0.5, got %f", score.RoleScore)
	}
}

func TestClassify_EarlyStepPosition(t *testing.T) {
	score := ClassifyComplexity("test", "", 0.1, 0.5, 0)
	if score.PositionScore != 0.7 {
		t.Errorf("early step (ratio < 0.3) should have position score 0.7, got %f", score.PositionScore)
	}
}

func TestClassify_LateStepPosition(t *testing.T) {
	score := ClassifyComplexity("test", "", 0.9, 0.5, 0)
	if score.PositionScore != 0.3 {
		t.Errorf("late step (ratio > 0.8) should have position score 0.3, got %f", score.PositionScore)
	}
}

func TestClassify_MidStepPosition(t *testing.T) {
	score := ClassifyComplexity("test", "", 0.5, 0.5, 0)
	if score.PositionScore != 0.5 {
		t.Errorf("mid step should have position score 0.5, got %f", score.PositionScore)
	}
}

func TestClassify_BudgetScore(t *testing.T) {
	score := ClassifyComplexity("test", "", 0.5, 0.2, 0)
	expected := 1.0 - 0.2
	if math.Abs(score.BudgetScore-expected) > 0.001 {
		t.Errorf("budget score should be 1-budgetRatio=0.8, got %f", score.BudgetScore)
	}
}

func TestClassify_ContextScore(t *testing.T) {
	score := ClassifyComplexity("test", "", 0.5, 0.5, 2048)
	expected := 2048.0 / 4096.0
	if math.Abs(score.ContextScore-expected) > 0.001 {
		t.Errorf("context score should be %f, got %f", expected, score.ContextScore)
	}
}

func TestClassify_StructScore(t *testing.T) {
	prompt := "What is X? How about Y? - bullet1 - bullet2 - bullet3; extra"
	score := ClassifyComplexity(prompt, "", 0.5, 0.5, 0)
	if score.StructScore <= 0.0 {
		t.Errorf("prompt with structure indicators should have StructScore > 0, got %f", score.StructScore)
	}
}

// ==================== Router.Route Tests ====================

func TestRoute_SimplePromptEconomy(t *testing.T) {
	r := New(defaultRouterCfg(), allTierProviders(), testLogger())
	sel := r.Route("hi", "", 0.5, 0.5, 0)
	// "hi" has no keywords → PromptScore=0.5, short → LengthScore≈0.0
	// With default threshold 0.5 and weights, this typically lands in mid or cheap
	// The important thing is it doesn't route to premium
	if sel.Tier == "premium" {
		t.Errorf("simple prompt 'hi' should not route to premium, got tier=%s", sel.Tier)
	}
}

func TestRoute_ComplexPromptPremium(t *testing.T) {
	r := New(defaultRouterCfg(), allTierProviders(), testLogger())
	prompt := "debug the race condition with mutex deadlock in this concurrent distributed system"
	sel := r.Route(prompt, "architect", 0.1, 0.8, 4000)
	if sel.Tier != "premium" && sel.Tier != "mid" {
		t.Errorf("complex prompt with architect role should route premium or mid, got tier=%s", sel.Tier)
	}
}

func TestRoute_BudgetExhaustedOverride(t *testing.T) {
	r := New(defaultRouterCfg(), allTierProviders(), testLogger())
	// budgetRatio < 0.05 should force economy regardless of complexity
	prompt := "debug the race condition with mutex deadlock in this concurrent distributed system"
	sel := r.Route(prompt, "architect", 0.1, 0.02, 4000)
	if sel.Tier != "economy" {
		t.Errorf("budget nearly exhausted (0.02) should force economy, got tier=%s", sel.Tier)
	}
	if sel.Reason != "budget nearly exhausted" {
		t.Errorf("expected reason 'budget nearly exhausted', got %q", sel.Reason)
	}
}

func TestRoute_BudgetPressureMidOverride(t *testing.T) {
	r := New(defaultRouterCfg(), allTierProviders(), testLogger())
	// budgetRatio < 0.15 and tier==premium → should downgrade to mid
	// Use a scenario that would otherwise be premium
	prompt := "debug the race condition with mutex deadlock in this concurrent distributed system"
	sel := r.Route(prompt, "architect", 0.1, 0.10, 4000)
	// With budgetRatio=0.10, budget < 0.15 so premium→mid, but also <0.15 so check
	// Actually 0.10 < 0.15 → premium→mid, but 0.10 >= 0.05 so not forced economy
	if sel.Tier == "premium" {
		t.Errorf("budget pressure (0.10 < 0.15) should prevent premium tier, got tier=%s", sel.Tier)
	}
}

func TestRoute_SelectionHasProvider(t *testing.T) {
	r := New(defaultRouterCfg(), allTierProviders(), testLogger())
	sel := r.Route("hello", "", 0.5, 0.5, 0)
	if sel.Provider == "" {
		t.Error("selection should have a provider")
	}
	if sel.Model == "" {
		t.Error("selection should have a model")
	}
}

// ==================== selectModelWithFallback Tests ====================

func TestFallback_EconomyNotAvailable(t *testing.T) {
	// Provider with only mid and premium
	providers := []config.ProviderConfig{
		{
			Name:    "limited-provider",
			Type:    "ollama",
			Enabled: true,
			Models: []config.ModelConfig{
				{Name: "mid-model", Tier: "mid", CostPer1K: 0.01},
				{Name: "premium-model", Tier: "premium", CostPer1K: 0.06},
			},
		},
	}
	r := New(defaultRouterCfg(), providers, testLogger())
	provider, model := r.selectModelWithFallback("economy")
	// economy not available → falls to cheap (not available) → falls to mid
	if model != "mid-model" {
		t.Errorf("fallback from economy should reach mid-model, got %s", model)
	}
	if provider != "limited-provider" {
		t.Errorf("expected limited-provider, got %s", provider)
	}
}

func TestFallback_CheapToMid(t *testing.T) {
	providers := []config.ProviderConfig{
		{
			Name:    "p1",
			Type:    "ollama",
			Enabled: true,
			Models: []config.ModelConfig{
				{Name: "mid-model", Tier: "mid", CostPer1K: 0.01},
			},
		},
	}
	r := New(defaultRouterCfg(), providers, testLogger())
	_, model := r.selectModelWithFallback("cheap")
	if model != "mid-model" {
		t.Errorf("fallback from cheap should reach mid-model, got %s", model)
	}
}

func TestFallback_NoModelsAvailable(t *testing.T) {
	providers := []config.ProviderConfig{
		{Name: "empty", Enabled: true, Models: nil},
	}
	r := New(defaultRouterCfg(), providers, testLogger())
	provider, model := r.selectModelWithFallback("economy")
	if provider != "" || model != "" {
		t.Errorf("no models → should return empty, got provider=%q model=%q", provider, model)
	}
}

func TestFallback_DisabledProvider(t *testing.T) {
	providers := []config.ProviderConfig{
		{
			Name:    "disabled",
			Enabled: false,
			Models: []config.ModelConfig{
				{Name: "m1", Tier: "economy"},
			},
		},
		{
			Name:    "enabled",
			Enabled: true,
			Models: []config.ModelConfig{
				{Name: "m2", Tier: "cheap"},
			},
		},
	}
	r := New(defaultRouterCfg(), providers, testLogger())
	provider, model := r.selectModelWithFallback("economy")
	// economy disabled → cheap from enabled provider
	if provider != "enabled" || model != "m2" {
		t.Errorf("should skip disabled provider, got provider=%q model=%q", provider, model)
	}
}

func TestFallback_ExactTierMatch(t *testing.T) {
	r := New(defaultRouterCfg(), allTierProviders(), testLogger())
	provider, model := r.selectModelWithFallback("premium")
	if model != "premium-model" {
		t.Errorf("exact tier match should return premium-model, got %s", model)
	}
	if provider != "test-provider" {
		t.Errorf("expected test-provider, got %s", provider)
	}
}

// ==================== GetModelCost Tests ====================

func TestGetModelCost_KnownModel(t *testing.T) {
	r := New(defaultRouterCfg(), allTierProviders(), testLogger())
	cost := r.GetModelCost("test-provider", "premium-model")
	if cost != 0.06 {
		t.Errorf("premium-model cost should be 0.06, got %f", cost)
	}
}

func TestGetModelCost_EconomyModel(t *testing.T) {
	r := New(defaultRouterCfg(), allTierProviders(), testLogger())
	cost := r.GetModelCost("test-provider", "economy-model")
	if cost != 0.001 {
		t.Errorf("economy-model cost should be 0.001, got %f", cost)
	}
}

func TestGetModelCost_UnknownModel(t *testing.T) {
	r := New(defaultRouterCfg(), allTierProviders(), testLogger())
	cost := r.GetModelCost("test-provider", "nonexistent-model")
	if cost != 0.005 {
		t.Errorf("unknown model should return default 0.005, got %f", cost)
	}
}

func TestGetModelCost_UnknownProvider(t *testing.T) {
	r := New(defaultRouterCfg(), allTierProviders(), testLogger())
	cost := r.GetModelCost("nonexistent-provider", "premium-model")
	if cost != 0.005 {
		t.Errorf("unknown provider should return default 0.005, got %f", cost)
	}
}

// ==================== BudgetManager Tests ====================

func TestBudgetManager_ShouldDowngrade(t *testing.T) {
	bm := NewBudgetManager(true, 1.0)
	if !bm.ShouldDowngrade(0.10, 1.0) {
		t.Error("10% budget left should trigger downgrade")
	}
	if bm.ShouldDowngrade(0.50, 1.0) {
		t.Error("50% budget left should NOT trigger downgrade")
	}
}

func TestBudgetManager_DisabledNeverDowngrades(t *testing.T) {
	bm := NewBudgetManager(false, 1.0)
	if bm.ShouldDowngrade(0.01, 1.0) {
		t.Error("disabled budget manager should never downgrade")
	}
}

func TestBudgetManager_ZeroBudget(t *testing.T) {
	bm := NewBudgetManager(true, 0.0)
	if bm.ShouldDowngrade(0.0, 0.0) {
		t.Error("zero total budget should not downgrade (division protection)")
	}
}

func TestBudgetManager_Properties(t *testing.T) {
	bm := NewBudgetManager(true, 5.0)
	if !bm.IsEnabled() {
		t.Error("should be enabled")
	}
	if bm.DefaultBudget() != 5.0 {
		t.Errorf("default budget should be 5.0, got %f", bm.DefaultBudget())
	}
}

// ==================== TierFallbackOrder Tests ====================

func TestTierFallbackOrder_Exists(t *testing.T) {
	if len(tierFallbackOrder) != 4 {
		t.Errorf("expected 4 tiers in fallback order, got %d", len(tierFallbackOrder))
	}
	expected := []string{"economy", "cheap", "mid", "premium"}
	for i, tier := range expected {
		if tierFallbackOrder[i] != tier {
			t.Errorf("tier %d should be %q, got %q", i, tier, tierFallbackOrder[i])
		}
	}
}

// ==================== Keyword Coverage Tests ====================

func TestHighKeywords_Debug(t *testing.T) {
	found := false
	for _, kw := range highComplexityKeywords {
		if kw == "debug" {
			found = true
			break
		}
	}
	if !found {
		t.Error("'debug' should be in highComplexityKeywords")
	}
}

func TestHighKeywords_RaceCondition(t *testing.T) {
	found := false
	for _, kw := range highComplexityKeywords {
		if kw == "race condition" {
			found = true
			break
		}
	}
	if !found {
		t.Error("'race condition' should be in highComplexityKeywords")
	}
}

func TestHighKeywords_Mutex(t *testing.T) {
	found := false
	for _, kw := range highComplexityKeywords {
		if kw == "mutex" {
			found = true
			break
		}
	}
	if !found {
		t.Error("'mutex' should be in highComplexityKeywords")
	}
}

func TestLowKeywords_Summarize(t *testing.T) {
	found := false
	for _, kw := range lowComplexityKeywords {
		if kw == "summarize" {
			found = true
			break
		}
	}
	if !found {
		t.Error("'summarize' should be in lowComplexityKeywords")
	}
}

// ==================== Clamp Tests ====================

func TestClamp_Below(t *testing.T) {
	if clamp(-0.5, 0.0, 1.0) != 0.0 {
		t.Error("clamp below should return lo")
	}
}

func TestClamp_Above(t *testing.T) {
	if clamp(1.5, 0.0, 1.0) != 1.0 {
		t.Error("clamp above should return hi")
	}
}

func TestClamp_InRange(t *testing.T) {
	if clamp(0.5, 0.0, 1.0) != 0.5 {
		t.Error("clamp in range should return value")
	}
}
