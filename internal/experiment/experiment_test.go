package experiment

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 1. Deterministic assignment — same workflowID → same variant always
// ---------------------------------------------------------------------------

func TestDeterministicAssignment(t *testing.T) {
	exp := CascadeThresholdExperiment()
	first := exp.Assign("wf-abc-123")
	for i := 0; i < 100; i++ {
		a := exp.Assign("wf-abc-123")
		if a.VariantID != first.VariantID {
			t.Fatalf("assignment changed on iteration %d: got %s, want %s", i, a.VariantID, first.VariantID)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. Traffic split 50/50 accuracy
// ---------------------------------------------------------------------------

func TestTrafficSplit5050(t *testing.T) {
	exp := CascadeThresholdExperiment()
	counts := map[string]int{}
	n := 10_000
	for i := 0; i < n; i++ {
		a := exp.Assign(fmt.Sprintf("wf-%d", i))
		counts[a.VariantID]++
	}
	for _, v := range exp.Variants {
		ratio := float64(counts[v.ID]) / float64(n)
		if math.Abs(ratio-0.5) > 0.05 {
			t.Errorf("variant %s: got %.2f, want ~0.50 (±0.05)", v.ID, ratio)
		}
	}
}

// ---------------------------------------------------------------------------
// 3. Traffic split 80/20 accuracy
// ---------------------------------------------------------------------------

func TestTrafficSplit8020(t *testing.T) {
	exp := Experiment{
		ID:      "split-80-20",
		Name:    "80/20 Test",
		Enabled: true,
		Variants: []Variant{
			{ID: "control", Name: "Control"},
			{ID: "treatment_a", Name: "Treatment A"},
		},
		TrafficSplit: []float64{0.8, 0.2},
		StartTime:    time.Now().Add(-time.Hour),
	}
	counts := map[string]int{}
	n := 10_000
	for i := 0; i < n; i++ {
		a := exp.Assign(fmt.Sprintf("wf-8020-%d", i))
		counts[a.VariantID]++
	}
	controlRatio := float64(counts["control"]) / float64(n)
	if math.Abs(controlRatio-0.8) > 0.05 {
		t.Errorf("control ratio: got %.3f, want ~0.80", controlRatio)
	}
	treatmentRatio := float64(counts["treatment_a"]) / float64(n)
	if math.Abs(treatmentRatio-0.2) > 0.05 {
		t.Errorf("treatment ratio: got %.3f, want ~0.20", treatmentRatio)
	}
}

// ---------------------------------------------------------------------------
// 4. Experiment time window (active/inactive)
// ---------------------------------------------------------------------------

func TestIsActive(t *testing.T) {
	tests := []struct {
		name   string
		exp    Experiment
		active bool
	}{
		{
			name: "enabled, within window",
			exp: Experiment{
				Enabled:   true,
				StartTime: time.Now().Add(-time.Hour),
				EndTime:   time.Now().Add(time.Hour),
			},
			active: true,
		},
		{
			name: "disabled",
			exp: Experiment{
				Enabled:   false,
				StartTime: time.Now().Add(-time.Hour),
			},
			active: false,
		},
		{
			name: "before start",
			exp: Experiment{
				Enabled:   true,
				StartTime: time.Now().Add(time.Hour),
			},
			active: false,
		},
		{
			name: "after end",
			exp: Experiment{
				Enabled:   true,
				StartTime: time.Now().Add(-2 * time.Hour),
				EndTime:   time.Now().Add(-time.Hour),
			},
			active: false,
		},
		{
			name: "no end time (perpetual)",
			exp: Experiment{
				Enabled:   true,
				StartTime: time.Now().Add(-time.Hour),
			},
			active: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.exp.IsActive(); got != tc.active {
				t.Errorf("IsActive() = %v, want %v", got, tc.active)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 5. Metric recording and aggregation
// ---------------------------------------------------------------------------

func TestMetricRecordingAndAggregation(t *testing.T) {
	mgr := NewManager()
	exp := CascadeThresholdExperiment()
	mgr.RegisterExperiment(exp)

	a := mgr.GetAssignmentForExperiment("wf-1", exp.ID)
	if a == nil {
		t.Fatal("expected assignment")
	}

	mgr.RecordMetric("wf-1", MetricEvent{Cost: 0.01, Tokens: 100, LatencyMs: 200, CacheHit: true, Confidence: 0.9})
	mgr.RecordMetric("wf-1", MetricEvent{Cost: 0.02, Tokens: 200, LatencyMs: 300, CacheHit: false, Confidence: 0.8, Escalation: true})

	res := mgr.GetResults(exp.ID)
	if res == nil {
		t.Fatal("no results")
	}
	vs := res.VariantStats[a.VariantID]
	if vs.RequestCount != 2 {
		t.Errorf("RequestCount = %d, want 2", vs.RequestCount)
	}
	if vs.CacheHits != 1 {
		t.Errorf("CacheHits = %d, want 1", vs.CacheHits)
	}
	if vs.Escalations != 1 {
		t.Errorf("Escalations = %d, want 1", vs.Escalations)
	}
	if math.Abs(vs.TotalCost-0.03) > 1e-9 {
		t.Errorf("TotalCost = %f, want 0.03", vs.TotalCost)
	}
}

// ---------------------------------------------------------------------------
// 6. Z-test: known significant result
// ---------------------------------------------------------------------------

func TestZTestSignificant(t *testing.T) {
	// 100/1000 vs 150/1000 — should be significant
	r := ZTest(100, 1000, 150, 1000)
	if !r.Significant {
		t.Errorf("expected significant, got p=%f", r.PValue)
	}
	if r.PValue >= 0.05 {
		t.Errorf("PValue = %f, want < 0.05", r.PValue)
	}
	if r.Lift <= 0 {
		t.Errorf("Lift = %f, want > 0", r.Lift)
	}
}

// ---------------------------------------------------------------------------
// 7. Z-test: known non-significant result
// ---------------------------------------------------------------------------

func TestZTestNotSignificant(t *testing.T) {
	// 100/1000 vs 105/1000 — should NOT be significant
	r := ZTest(100, 1000, 105, 1000)
	if r.Significant {
		t.Errorf("expected not significant, got p=%f", r.PValue)
	}
}

// ---------------------------------------------------------------------------
// 8. Z-test edge cases
// ---------------------------------------------------------------------------

func TestZTestEdgeCases(t *testing.T) {
	// Zero samples
	r := ZTest(0, 0, 0, 0)
	if r.Significant {
		t.Error("zero samples should not be significant")
	}

	// Equal rates
	r = ZTest(50, 100, 50, 100)
	if r.Significant {
		t.Errorf("equal rates should not be significant, p=%f", r.PValue)
	}
	if r.ZScore != 0 {
		t.Errorf("ZScore = %f, want 0", r.ZScore)
	}

	// One side zero samples
	r = ZTest(50, 100, 0, 0)
	if r.Significant {
		t.Error("one side zero should not be significant")
	}
}

// ---------------------------------------------------------------------------
// 9. Welch's t-test: significant difference
// ---------------------------------------------------------------------------

func TestWelchTTestSignificant(t *testing.T) {
	// Control mean=100, sd=10, n=100; Treatment mean=110, sd=10, n=100
	r := WelchTTest(100, 10, 100, 110, 10, 100)
	if !r.Significant {
		t.Errorf("expected significant, got p=%f", r.PValue)
	}
	if r.MeanDiff != 10 {
		t.Errorf("MeanDiff = %f, want 10", r.MeanDiff)
	}
}

// ---------------------------------------------------------------------------
// 10. Welch's t-test: non-significant difference
// ---------------------------------------------------------------------------

func TestWelchTTestNotSignificant(t *testing.T) {
	// Control mean=100, sd=50, n=10; Treatment mean=102, sd=50, n=10
	r := WelchTTest(100, 50, 10, 102, 50, 10)
	if r.Significant {
		t.Errorf("expected not significant, got p=%f", r.PValue)
	}
}

// ---------------------------------------------------------------------------
// 11. Manager concurrent access
// ---------------------------------------------------------------------------

func TestManagerConcurrentAccess(t *testing.T) {
	mgr := NewManager()
	exp := CascadeThresholdExperiment()
	mgr.RegisterExperiment(exp)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			wfID := fmt.Sprintf("wf-concurrent-%d", i)
			mgr.GetAssignmentForExperiment(wfID, exp.ID)
			mgr.RecordMetric(wfID, MetricEvent{Cost: 0.01, Tokens: 50, LatencyMs: 100, CacheHit: i%2 == 0})
		}(i)
	}
	wg.Wait()

	res := mgr.GetResults(exp.ID)
	if res == nil {
		t.Fatal("no results after concurrent access")
	}
	var total int64
	for _, vs := range res.VariantStats {
		total += vs.RequestCount
	}
	if total != 100 {
		t.Errorf("total requests = %d, want 100", total)
	}
}

// ---------------------------------------------------------------------------
// 12. Predefined experiments have valid config
// ---------------------------------------------------------------------------

func TestPredefinedExperimentsValid(t *testing.T) {
	exps := []Experiment{
		CascadeThresholdExperiment(),
		CompressionExperiment(),
		TierThresholdExperiment(),
		CacheAggressivenessExperiment(),
	}
	for _, exp := range exps {
		t.Run(exp.ID, func(t *testing.T) {
			if exp.ID == "" {
				t.Error("empty ID")
			}
			if len(exp.Variants) < 2 {
				t.Errorf("need at least 2 variants, got %d", len(exp.Variants))
			}
			if len(exp.TrafficSplit) != len(exp.Variants) {
				t.Errorf("variants=%d, splits=%d", len(exp.Variants), len(exp.TrafficSplit))
			}
			var sum float64
			for _, s := range exp.TrafficSplit {
				sum += s
			}
			if math.Abs(sum-1.0) > 1e-9 {
				t.Errorf("traffic split sums to %f, want 1.0", sum)
			}
			for _, v := range exp.Variants {
				if v.Config == nil {
					t.Errorf("variant %s has nil config", v.ID)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 13. Assignment consistency under concurrent reads
// ---------------------------------------------------------------------------

func TestAssignmentConsistencyConcurrent(t *testing.T) {
	exp := CascadeThresholdExperiment()
	wfID := "wf-consistent"
	first := exp.Assign(wfID)

	var wg sync.WaitGroup
	errs := make(chan string, 200)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a := exp.Assign(wfID)
			if a.VariantID != first.VariantID {
				errs <- fmt.Sprintf("got %s, want %s", a.VariantID, first.VariantID)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// ---------------------------------------------------------------------------
// 14. Results aggregation correctness
// ---------------------------------------------------------------------------

func TestResultsAggregation(t *testing.T) {
	mgr := NewManager()
	exp := Experiment{
		ID:      "agg-test",
		Name:    "Aggregation",
		Enabled: true,
		Variants: []Variant{
			{ID: "control", Name: "Control", Config: map[string]interface{}{}},
			{ID: "treatment_a", Name: "Treatment", Config: map[string]interface{}{}},
		},
		TrafficSplit: []float64{0.5, 0.5},
		StartTime:    time.Now().Add(-time.Hour),
	}
	mgr.RegisterExperiment(exp)

	// Assign 50 workflows and record metrics
	for i := 0; i < 50; i++ {
		wfID := fmt.Sprintf("wf-agg-%d", i)
		mgr.GetAssignmentForExperiment(wfID, exp.ID)
		mgr.RecordMetric(wfID, MetricEvent{
			Cost:      0.01,
			Tokens:    100,
			LatencyMs: 200,
			CacheHit:  i%3 == 0,
			Error:     i%10 == 0,
		})
	}

	res := mgr.GetResults(exp.ID)
	if res == nil {
		t.Fatal("no results")
	}

	var totalReqs int64
	var totalCost float64
	var totalTokens int64
	var totalErrors int64
	for _, vs := range res.VariantStats {
		totalReqs += vs.RequestCount
		totalCost += vs.TotalCost
		totalTokens += vs.TotalTokens
		totalErrors += vs.Errors
	}
	if totalReqs != 50 {
		t.Errorf("total requests = %d, want 50", totalReqs)
	}
	if math.Abs(totalCost-0.50) > 1e-9 {
		t.Errorf("total cost = %f, want 0.50", totalCost)
	}
	if totalTokens != 5000 {
		t.Errorf("total tokens = %d, want 5000", totalTokens)
	}
	if totalErrors != 5 {
		t.Errorf("total errors = %d, want 5", totalErrors)
	}
}

// ---------------------------------------------------------------------------
// 15. Multiple active experiments simultaneously
// ---------------------------------------------------------------------------

func TestMultipleActiveExperiments(t *testing.T) {
	mgr := NewManager()
	exp1 := CascadeThresholdExperiment()
	exp2 := CompressionExperiment()
	mgr.RegisterExperiment(exp1)
	mgr.RegisterExperiment(exp2)

	active := mgr.ActiveExperiments()
	if len(active) != 2 {
		t.Errorf("active experiments = %d, want 2", len(active))
	}

	// Each experiment should produce independent assignments
	a1 := mgr.GetAssignmentForExperiment("wf-multi", exp1.ID)
	a2 := mgr.GetAssignmentForExperiment("wf-multi", exp2.ID)
	if a1 == nil || a2 == nil {
		t.Fatal("expected assignments for both experiments")
	}
	if a1.ExperimentID == a2.ExperimentID {
		t.Error("assignments should reference different experiments")
	}
}

// ---------------------------------------------------------------------------
// 16. Empty variant list
// ---------------------------------------------------------------------------

func TestAssignNoVariants(t *testing.T) {
	exp := Experiment{
		ID:      "empty",
		Enabled: true,
	}
	if a := exp.Assign("wf-1"); a != nil {
		t.Error("expected nil assignment for empty variant list")
	}
}

// ---------------------------------------------------------------------------
// 17. Z-test lift calculation
// ---------------------------------------------------------------------------

func TestZTestLift(t *testing.T) {
	r := ZTest(100, 1000, 200, 1000)
	expectedLift := 100.0 // 0.10 → 0.20 is +100%
	if math.Abs(r.Lift-expectedLift) > 1.0 {
		t.Errorf("Lift = %f, want ~%f", r.Lift, expectedLift)
	}
}

// ---------------------------------------------------------------------------
// 18. Welch's t-test edge case: equal means
// ---------------------------------------------------------------------------

func TestWelchTTestEqualMeans(t *testing.T) {
	r := WelchTTest(100, 10, 50, 100, 10, 50)
	if r.TStatistic != 0 {
		t.Errorf("TStatistic = %f, want 0", r.TStatistic)
	}
	if r.MeanDiff != 0 {
		t.Errorf("MeanDiff = %f, want 0", r.MeanDiff)
	}
}

// ---------------------------------------------------------------------------
// 19. Welch's t-test edge case: insufficient samples
// ---------------------------------------------------------------------------

func TestWelchTTestInsufficientSamples(t *testing.T) {
	r := WelchTTest(100, 10, 1, 110, 10, 1)
	// With n=1 for each group we can't compute degrees of freedom
	if r.Significant {
		t.Error("n=1 should not produce significant result")
	}
}

// ---------------------------------------------------------------------------
// 20. Manager GetResults for unknown experiment
// ---------------------------------------------------------------------------

func TestGetResultsUnknown(t *testing.T) {
	mgr := NewManager()
	if r := mgr.GetResults("nonexistent"); r != nil {
		t.Error("expected nil for unknown experiment")
	}
}

// ---------------------------------------------------------------------------
// 21. normalCDF basic properties
// ---------------------------------------------------------------------------

func TestNormalCDF(t *testing.T) {
	// CDF(0) ≈ 0.5
	if math.Abs(normalCDF(0)-0.5) > 1e-6 {
		t.Errorf("normalCDF(0) = %f, want 0.5", normalCDF(0))
	}
	// CDF(-8) ≈ 0
	if normalCDF(-8) > 1e-6 {
		t.Errorf("normalCDF(-8) = %f, want ~0", normalCDF(-8))
	}
	// CDF(8) ≈ 1
	if normalCDF(8) < 1-1e-6 {
		t.Errorf("normalCDF(8) = %f, want ~1", normalCDF(8))
	}
	// CDF(1.96) ≈ 0.975
	if math.Abs(normalCDF(1.96)-0.975) > 0.001 {
		t.Errorf("normalCDF(1.96) = %f, want ~0.975", normalCDF(1.96))
	}
}

// ---------------------------------------------------------------------------
// 22. Welch's t-test lift calculation
// ---------------------------------------------------------------------------

func TestWelchTTestLift(t *testing.T) {
	r := WelchTTest(100, 10, 50, 120, 10, 50)
	expectedLift := 20.0 // 100 → 120 is +20%
	if math.Abs(r.Lift-expectedLift) > 1.0 {
		t.Errorf("Lift = %f, want ~%f", r.Lift, expectedLift)
	}
}

// ---------------------------------------------------------------------------
// 23. Manager error recording
// ---------------------------------------------------------------------------

func TestManagerErrorRecording(t *testing.T) {
	mgr := NewManager()
	exp := CascadeThresholdExperiment()
	mgr.RegisterExperiment(exp)
	mgr.GetAssignmentForExperiment("wf-err", exp.ID)

	mgr.RecordMetric("wf-err", MetricEvent{Error: true})
	mgr.RecordMetric("wf-err", MetricEvent{Error: true})
	mgr.RecordMetric("wf-err", MetricEvent{Error: false})

	res := mgr.GetResults(exp.ID)
	found := false
	for _, vs := range res.VariantStats {
		if vs.RequestCount == 3 {
			if vs.Errors != 2 {
				t.Errorf("Errors = %d, want 2", vs.Errors)
			}
			found = true
		}
	}
	if !found {
		t.Error("did not find variant with 3 requests")
	}
}
