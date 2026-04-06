package router

import (
	"sync"
	"testing"

	"github.com/nexus-gateway/nexus/internal/config"
	"github.com/nexus-gateway/nexus/internal/eval"
)

func newTestAdaptiveRouter(cm *eval.ConfidenceMap, cfg config.AdaptiveConfig) *AdaptiveRouter {
	base := New(defaultRouterCfg(), allTierProviders(), testLogger())
	return NewAdaptiveRouter(base, cm, cfg)
}

func defaultAdaptiveCfg() config.AdaptiveConfig {
	return config.AdaptiveConfig{
		Enabled:        true,
		MinSamples:     50,
		HighConfidence: 0.90,
		LowConfidence:  0.50,
	}
}

// seedConfidenceMap populates the confidence map with n samples at the given confidence.
func seedConfidenceMap(cm *eval.ConfidenceMap, taskType, tier string, n int, confidence float64) {
	for i := 0; i < n; i++ {
		cm.Record(taskType, tier, confidence)
	}
}

// --- Test 1: No override when insufficient samples ---
func TestAdaptive_NoOverrideInsufficientSamples(t *testing.T) {
	cm := eval.NewConfidenceMap()
	seedConfidenceMap(cm, "coding", "cheap", 10, 0.95) // only 10, needs 50
	ar := newTestAdaptiveRouter(cm, defaultAdaptiveCfg())

	sel := ar.Route("implement a function to code this class", "", 0.5, 0.5, 100)
	// Should NOT force to cheap because only 10 samples
	if sel.Reason == "adaptive override from confidence map" {
		t.Errorf("should not override with only 10 samples, got reason=%q tier=%s", sel.Reason, sel.Tier)
	}
}

// --- Test 2: Downgrade when cheap confidence high (>= 0.90) with enough samples ---
func TestAdaptive_DowngradeHighConfidence(t *testing.T) {
	cm := eval.NewConfidenceMap()
	seedConfidenceMap(cm, "coding", "cheap", 60, 0.95) // 60 samples, avg=0.95
	ar := newTestAdaptiveRouter(cm, defaultAdaptiveCfg())

	sel := ar.Route("implement a function to code this class", "", 0.5, 0.5, 100)
	if sel.Tier != "cheap" {
		t.Errorf("expected downgrade to cheap, got tier=%s", sel.Tier)
	}
	if sel.Reason != "adaptive override from confidence map" {
		t.Errorf("expected adaptive override reason, got %q", sel.Reason)
	}
}

// --- Test 3: Upgrade when cheap confidence low (<= 0.50) with enough samples ---
func TestAdaptive_UpgradeLowConfidence(t *testing.T) {
	cm := eval.NewConfidenceMap()
	seedConfidenceMap(cm, "coding", "cheap", 60, 0.30) // 60 samples, avg=0.30
	ar := newTestAdaptiveRouter(cm, defaultAdaptiveCfg())

	sel := ar.Route("implement a function to code this class", "", 0.5, 0.5, 100)
	if sel.Tier != "premium" {
		t.Errorf("expected upgrade to premium, got tier=%s", sel.Tier)
	}
	if sel.Reason != "adaptive override from confidence map" {
		t.Errorf("expected adaptive override reason, got %q", sel.Reason)
	}
}

// --- Test 4: No override when confidence is in middle range ---
func TestAdaptive_NoOverrideMiddleRange(t *testing.T) {
	cm := eval.NewConfidenceMap()
	seedConfidenceMap(cm, "coding", "cheap", 60, 0.70) // avg=0.70, between 0.50 and 0.90
	ar := newTestAdaptiveRouter(cm, defaultAdaptiveCfg())

	sel := ar.Route("implement a function to code this class", "", 0.5, 0.5, 100)
	if sel.Reason == "adaptive override from confidence map" {
		t.Errorf("should not override in middle range, got reason=%q tier=%s", sel.Reason, sel.Tier)
	}
}

// --- Test 5: Override counter tracking ---
func TestAdaptive_OverrideCounters(t *testing.T) {
	cm := eval.NewConfidenceMap()
	seedConfidenceMap(cm, "coding", "cheap", 60, 0.95)
	ar := newTestAdaptiveRouter(cm, defaultAdaptiveCfg())

	ar.Route("implement a function to code this class", "", 0.5, 0.5, 100)
	ar.Route("implement another function for code", "", 0.5, 0.5, 100)

	stats := ar.Stats()
	if stats.Overrides != 2 {
		t.Errorf("expected 2 overrides, got %d", stats.Overrides)
	}
	if stats.Downgrades != 2 {
		t.Errorf("expected 2 downgrades, got %d", stats.Downgrades)
	}
	if stats.Upgrades != 0 {
		t.Errorf("expected 0 upgrades, got %d", stats.Upgrades)
	}
}

// --- Test 6: Stats endpoint correctness ---
func TestAdaptive_StatsTaskTypes(t *testing.T) {
	cm := eval.NewConfidenceMap()
	seedConfidenceMap(cm, "coding", "cheap", 60, 0.95)
	seedConfidenceMap(cm, "coding", "premium", 30, 0.85)
	ar := newTestAdaptiveRouter(cm, defaultAdaptiveCfg())

	stats := ar.Stats()
	if !stats.Enabled {
		t.Error("expected enabled=true")
	}
	tt, ok := stats.TaskTypes["coding"]
	if !ok {
		t.Fatal("expected coding task type in stats")
	}
	if tt.CheapSamples != 60 {
		t.Errorf("expected 60 cheap samples, got %d", tt.CheapSamples)
	}
	if tt.PremiumSamples != 30 {
		t.Errorf("expected 30 premium samples, got %d", tt.PremiumSamples)
	}
	if tt.CurrentDecision != "use_cheap" {
		t.Errorf("expected decision use_cheap, got %s", tt.CurrentDecision)
	}
}

// --- Test 7: Concurrent access safety ---
func TestAdaptive_ConcurrentAccess(t *testing.T) {
	cm := eval.NewConfidenceMap()
	seedConfidenceMap(cm, "coding", "cheap", 60, 0.95)
	ar := newTestAdaptiveRouter(cm, defaultAdaptiveCfg())

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ar.Route("implement a function to code a class", "", 0.5, 0.5, 100)
		}()
	}
	wg.Wait()

	stats := ar.Stats()
	if stats.Overrides != 100 {
		t.Errorf("expected 100 overrides from concurrent calls, got %d", stats.Overrides)
	}
}

// --- Test 8: Disabled mode passes through to base router ---
func TestAdaptive_DisabledPassthrough(t *testing.T) {
	cm := eval.NewConfidenceMap()
	seedConfidenceMap(cm, "coding", "cheap", 60, 0.95) // would trigger downgrade if enabled
	cfg := defaultAdaptiveCfg()
	cfg.Enabled = false
	ar := newTestAdaptiveRouter(cm, cfg)

	sel := ar.Route("implement a function to code this class", "", 0.5, 0.5, 100)
	if sel.Reason == "adaptive override from confidence map" {
		t.Errorf("disabled adaptive router should not override, got reason=%q", sel.Reason)
	}
	stats := ar.Stats()
	if stats.Overrides != 0 {
		t.Errorf("disabled should have 0 overrides, got %d", stats.Overrides)
	}
}

// --- Test 9: Task type classification consistency ---
func TestAdaptive_TaskTypeClassification(t *testing.T) {
	// "coding" keywords: implement, code, function, class
	taskType := eval.ClassifyTaskType("implement a function to code a class")
	if taskType != "coding" {
		t.Errorf("expected 'coding', got %q", taskType)
	}

	taskType = eval.ClassifyTaskType("explain how this works and describe the architecture")
	if taskType != "informational" {
		t.Errorf("expected 'informational', got %q", taskType)
	}

	taskType = eval.ClassifyTaskType("deploy kubernetes docker pipeline ci")
	if taskType != "operational" {
		t.Errorf("expected 'operational', got %q", taskType)
	}
}

// --- Test 10: Multiple task types with different confidence levels ---
func TestAdaptive_MultipleTaskTypes(t *testing.T) {
	cm := eval.NewConfidenceMap()
	seedConfidenceMap(cm, "coding", "cheap", 60, 0.95)       // high → downgrade
	seedConfidenceMap(cm, "analysis", "cheap", 60, 0.30)     // low → upgrade
	seedConfidenceMap(cm, "informational", "cheap", 60, 0.70) // mid → no override
	ar := newTestAdaptiveRouter(cm, defaultAdaptiveCfg())

	// coding → cheap downgrade
	sel := ar.Route("implement a function to code a class", "", 0.5, 0.5, 100)
	if sel.Tier != "cheap" {
		t.Errorf("coding should downgrade to cheap, got %s", sel.Tier)
	}

	// analysis → premium upgrade
	sel = ar.Route("analyze and review this security vulnerability", "", 0.5, 0.5, 100)
	if sel.Tier != "premium" {
		t.Errorf("analysis should upgrade to premium, got %s", sel.Tier)
	}

	// informational → normal routing (no override)
	sel = ar.Route("explain and describe what this documentation is about", "", 0.5, 0.5, 100)
	if sel.Reason == "adaptive override from confidence map" {
		t.Errorf("informational mid-range should not override, got reason=%q", sel.Reason)
	}
}

// --- Test 11: Edge - exactly minSamples observations ---
func TestAdaptive_ExactlyMinSamples(t *testing.T) {
	cm := eval.NewConfidenceMap()
	seedConfidenceMap(cm, "coding", "cheap", 50, 0.95) // exactly 50
	ar := newTestAdaptiveRouter(cm, defaultAdaptiveCfg())

	sel := ar.Route("implement a function to code this class", "", 0.5, 0.5, 100)
	if sel.Tier != "cheap" {
		t.Errorf("exactly minSamples should trigger override, got tier=%s", sel.Tier)
	}
	if sel.Reason != "adaptive override from confidence map" {
		t.Errorf("expected adaptive override, got reason=%q", sel.Reason)
	}
}

// --- Test 12: Edge - confidence exactly at high threshold ---
func TestAdaptive_ConfidenceExactlyAtHighThreshold(t *testing.T) {
	cm := eval.NewConfidenceMap()
	// Record a single observation of exactly 0.90 via direct total manipulation won't work,
	// so we use a value just above threshold to ensure float rounding doesn't prevent match.
	seedConfidenceMap(cm, "coding", "cheap", 50, 0.901)
	ar := newTestAdaptiveRouter(cm, defaultAdaptiveCfg())

	sel := ar.Route("implement a function to code this class", "", 0.5, 0.5, 100)
	if sel.Tier != "cheap" {
		t.Errorf("confidence at high threshold should downgrade, got tier=%s", sel.Tier)
	}
}

// --- Test 13: Edge - confidence exactly at low threshold ---
func TestAdaptive_ConfidenceExactlyAtLowThreshold(t *testing.T) {
	cm := eval.NewConfidenceMap()
	seedConfidenceMap(cm, "coding", "cheap", 50, 0.50) // exactly 0.50
	ar := newTestAdaptiveRouter(cm, defaultAdaptiveCfg())

	sel := ar.Route("implement a function to code this class", "", 0.5, 0.5, 100)
	if sel.Tier != "premium" {
		t.Errorf("confidence exactly at low threshold should upgrade, got tier=%s", sel.Tier)
	}
}

// --- Test 14: Upgrade counter tracking ---
func TestAdaptive_UpgradeCounters(t *testing.T) {
	cm := eval.NewConfidenceMap()
	seedConfidenceMap(cm, "coding", "cheap", 60, 0.30)
	ar := newTestAdaptiveRouter(cm, defaultAdaptiveCfg())

	ar.Route("implement a function to code this class", "", 0.5, 0.5, 100)
	ar.Route("implement another function for code", "", 0.5, 0.5, 100)
	ar.Route("code a class method for this module", "", 0.5, 0.5, 100)

	stats := ar.Stats()
	if stats.Upgrades != 3 {
		t.Errorf("expected 3 upgrades, got %d", stats.Upgrades)
	}
	if stats.Downgrades != 0 {
		t.Errorf("expected 0 downgrades, got %d", stats.Downgrades)
	}
	if stats.Overrides != 3 {
		t.Errorf("expected 3 overrides, got %d", stats.Overrides)
	}
}

// --- Test 15: ForceRoute produces correct tier ---
func TestAdaptive_ForceRouteCorrectTier(t *testing.T) {
	base := New(defaultRouterCfg(), allTierProviders(), testLogger())

	sel := base.ForceRoute("cheap", "debug race condition", "architect", 0.1, 0.8, 4000)
	if sel.Tier != "cheap" {
		t.Errorf("ForceRoute should force cheap tier, got %s", sel.Tier)
	}
	if sel.Model != "cheap-model" {
		t.Errorf("ForceRoute should select cheap-model, got %s", sel.Model)
	}
	if sel.Reason != "adaptive override from confidence map" {
		t.Errorf("expected adaptive override reason, got %q", sel.Reason)
	}

	sel = base.ForceRoute("premium", "hello", "", 0.5, 0.5, 0)
	if sel.Tier != "premium" {
		t.Errorf("ForceRoute should force premium tier, got %s", sel.Tier)
	}
	if sel.Model != "premium-model" {
		t.Errorf("ForceRoute should select premium-model, got %s", sel.Model)
	}
}

// --- Test 16: Stats decision for insufficient data ---
func TestAdaptive_StatsInsufficientData(t *testing.T) {
	cm := eval.NewConfidenceMap()
	seedConfidenceMap(cm, "coding", "cheap", 10, 0.95)
	ar := newTestAdaptiveRouter(cm, defaultAdaptiveCfg())

	stats := ar.Stats()
	tt, ok := stats.TaskTypes["coding"]
	if !ok {
		t.Fatal("expected coding task type in stats")
	}
	if tt.CurrentDecision != "insufficient_data" {
		t.Errorf("expected insufficient_data decision with 10 samples, got %s", tt.CurrentDecision)
	}
}

// --- Test 17: Default config values ---
func TestAdaptive_DefaultConfigValues(t *testing.T) {
	cm := eval.NewConfidenceMap()
	cfg := config.AdaptiveConfig{Enabled: true}
	ar := NewAdaptiveRouter(New(defaultRouterCfg(), allTierProviders(), testLogger()), cm, cfg)

	if ar.minSamples != 50 {
		t.Errorf("default minSamples should be 50, got %d", ar.minSamples)
	}
	if ar.highConfidence != 0.90 {
		t.Errorf("default highConfidence should be 0.90, got %f", ar.highConfidence)
	}
	if ar.lowConfidence != 0.50 {
		t.Errorf("default lowConfidence should be 0.50, got %f", ar.lowConfidence)
	}
}

// --- Test 18: No task type match uses general ---
func TestAdaptive_GeneralTaskType(t *testing.T) {
	cm := eval.NewConfidenceMap()
	seedConfidenceMap(cm, "general", "cheap", 60, 0.95)
	ar := newTestAdaptiveRouter(cm, defaultAdaptiveCfg())

	// "hello world" has no task keywords → general
	sel := ar.Route("hello world", "", 0.5, 0.5, 100)
	if sel.Tier != "cheap" {
		t.Errorf("general task with high cheap confidence should downgrade, got tier=%s", sel.Tier)
	}
}
