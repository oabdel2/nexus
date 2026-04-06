package eval

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ==================== Stress Tests for ConfidenceMap ====================

// 100 goroutines recording simultaneously → no panics.
func TestStress_100Goroutines(t *testing.T) {
	cm := NewConfidenceMap()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			taskType := fmt.Sprintf("task-%d", n%10)
			tier := fmt.Sprintf("tier-%d", n%3)
			cm.Record(taskType, tier, float64(n%100)/100.0)
		}(i)
	}

	wg.Wait()

	// Verify no data loss: we should have entries across task types
	totalSamples := 0
	for i := 0; i < 10; i++ {
		for j := 0; j < 3; j++ {
			r := cm.Lookup(fmt.Sprintf("task-%d", i), fmt.Sprintf("tier-%d", j))
			if r.Found {
				totalSamples += r.SampleCount
			}
		}
	}
	if totalSamples != 100 {
		t.Errorf("expected 100 total samples, got %d", totalSamples)
	}
}

// 1000 task types → map handles it without issues.
func TestStress_1000TaskTypes(t *testing.T) {
	cm := NewConfidenceMap()

	for i := 0; i < 1000; i++ {
		taskType := fmt.Sprintf("task-type-%04d", i)
		cm.Record(taskType, "tier1", 0.5+float64(i%50)/100.0)
	}

	// Verify all 1000 entries are present
	found := 0
	for i := 0; i < 1000; i++ {
		r := cm.Lookup(fmt.Sprintf("task-type-%04d", i), "tier1")
		if r.Found && r.SampleCount == 1 {
			found++
		}
	}
	if found != 1000 {
		t.Errorf("expected 1000 entries found, got %d", found)
	}
}

// Save/Load round trip with 10K entries → data integrity preserved.
func TestStress_SaveLoad10K(t *testing.T) {
	cm := NewConfidenceMap()

	// Insert 10K entries (100 task types × 10 tiers × 10 samples each)
	for i := 0; i < 100; i++ {
		taskType := fmt.Sprintf("task-%03d", i)
		for j := 0; j < 10; j++ {
			tier := fmt.Sprintf("tier-%d", j)
			for k := 0; k < 10; k++ {
				cm.Record(taskType, tier, float64(k)/10.0)
			}
		}
	}

	// Save
	dir := t.TempDir()
	path := filepath.Join(dir, "stress_10k.json")
	if err := cm.Save(path); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Check file exists and has content
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("saved file is empty")
	}

	// Load into new map
	cm2 := NewConfidenceMap()
	if err := cm2.Load(path); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	// Verify sample integrity
	for i := 0; i < 100; i++ {
		taskType := fmt.Sprintf("task-%03d", i)
		for j := 0; j < 10; j++ {
			tier := fmt.Sprintf("tier-%d", j)
			r1 := cm.Lookup(taskType, tier)
			r2 := cm2.Lookup(taskType, tier)

			if !r1.Found || !r2.Found {
				t.Errorf("entry %s/%s missing after round-trip", taskType, tier)
				continue
			}
			if r1.SampleCount != r2.SampleCount {
				t.Errorf("%s/%s: sample count mismatch: %d vs %d",
					taskType, tier, r1.SampleCount, r2.SampleCount)
			}
			if math.Abs(r1.AverageConfidence-r2.AverageConfidence) > 0.0001 {
				t.Errorf("%s/%s: avg confidence mismatch: %f vs %f",
					taskType, tier, r1.AverageConfidence, r2.AverageConfidence)
			}
		}
	}
}

// Concurrent Record + Lookup → consistent results (no torn reads).
func TestStress_ConcurrentRecordLookup(t *testing.T) {
	cm := NewConfidenceMap()
	var wg sync.WaitGroup

	// Pre-seed so lookups can find something
	cm.Record("shared-task", "tier-a", 0.5)

	// Writers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cm.Record("shared-task", "tier-a", float64(n%10)/10.0)
			}
		}(i)
	}

	// Readers (concurrent with writers)
	errCh := make(chan string, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				r := cm.Lookup("shared-task", "tier-a")
				if r.Found {
					if r.AverageConfidence < 0.0 || r.AverageConfidence > 1.0 {
						errCh <- fmt.Sprintf("invalid avg confidence: %f", r.AverageConfidence)
						return
					}
					if r.SampleCount < 1 {
						errCh <- fmt.Sprintf("invalid sample count: %d", r.SampleCount)
						return
					}
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for errMsg := range errCh {
		t.Errorf("torn read detected: %s", errMsg)
	}

	// Final verification
	r := cm.Lookup("shared-task", "tier-a")
	if !r.Found {
		t.Fatal("shared-task not found after stress test")
	}
	// 1 seed + 50 writers * 100 = 5001
	if r.SampleCount != 5001 {
		t.Errorf("expected 5001 samples, got %d", r.SampleCount)
	}
}

// Concurrent RecordFromPrompt — tests classification + recording under contention.
func TestStress_ConcurrentRecordFromPrompt(t *testing.T) {
	cm := NewConfidenceMap()
	var wg sync.WaitGroup

	prompts := []string{
		"implement a function to sort an array",
		"analyze the performance of this code",
		"explain what a goroutine is",
		"write a creative story about AI",
		"deploy the kubernetes cluster",
	}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			prompt := prompts[n%len(prompts)]
			cm.RecordFromPrompt(prompt, "tier1", float64(n%10)/10.0)
		}(i)
	}

	wg.Wait()

	// Verify some classified entries exist
	totalSamples := 0
	for _, taskType := range []string{"coding", "analysis", "informational", "creative", "operational", "general"} {
		r := cm.Lookup(taskType, "tier1")
		if r.Found {
			totalSamples += r.SampleCount
		}
	}
	if totalSamples != 100 {
		t.Errorf("expected 100 total samples from concurrent RecordFromPrompt, got %d", totalSamples)
	}
}

// ConfidenceStats.Average edge cases.
func TestStress_AverageEdgeCases(t *testing.T) {
	// Zero samples
	cs := ConfidenceStats{}
	if cs.Average() != 0.0 {
		t.Errorf("zero samples average should be 0.0, got %f", cs.Average())
	}

	// Single sample
	cs = ConfidenceStats{TotalConfidence: 0.75, SampleCount: 1}
	if cs.Average() != 0.75 {
		t.Errorf("single sample average should be 0.75, got %f", cs.Average())
	}

	// Many samples
	cs = ConfidenceStats{TotalConfidence: 100.0, SampleCount: 200}
	if math.Abs(cs.Average()-0.5) > 0.001 {
		t.Errorf("200 samples average should be 0.5, got %f", cs.Average())
	}
}
