package eval

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ==================== HedgingScore Tests ====================

func TestHedging_ManyHedgingWords(t *testing.T) {
	response := "I think maybe this might work, but I'm not sure. Perhaps it could be correct, arguably. I believe it's possible."
	score := HedgingScore(response)
	if score > 0.5 {
		t.Errorf("heavily hedged response should score low, got %f", score)
	}
}

func TestHedging_ConfidentResponse(t *testing.T) {
	response := "The function returns an integer. Use the sort.Slice method to sort the array. The time complexity is O(n log n)."
	score := HedgingScore(response)
	if score < 0.9 {
		t.Errorf("confident response should score high, got %f", score)
	}
}

func TestHedging_EmptyResponse(t *testing.T) {
	score := HedgingScore("")
	if score != 0.5 {
		t.Errorf("empty response should score 0.5, got %f", score)
	}
}

func TestHedging_SingleHedge(t *testing.T) {
	response := "This is definitely the right approach, though I think the edge case needs checking."
	score := HedgingScore(response)
	if score < 0.7 || score > 0.95 {
		t.Errorf("single hedge should score moderately high, got %f", score)
	}
}

// ==================== CompletenessScore Tests ====================

func TestCompleteness_VeryShortAnswer(t *testing.T) {
	// 5 tokens response to complex prompt (100 tokens)
	score := CompletenessScore(100, 5, 20)
	if score > 0.3 {
		t.Errorf("very short answer should score low, got %f", score)
	}
}

func TestCompleteness_ProportionalAnswer(t *testing.T) {
	score := CompletenessScore(50, 75, 20)
	if score < 0.5 {
		t.Errorf("proportional answer should score well, got %f", score)
	}
}

func TestCompleteness_ZeroPrompt(t *testing.T) {
	score := CompletenessScore(0, 100, 20)
	if score != 0.5 {
		t.Errorf("zero prompt tokens should return 0.5, got %f", score)
	}
}

func TestCompleteness_VeryLongResponse(t *testing.T) {
	score := CompletenessScore(10, 500, 20)
	// Over-verbose
	if score > 0.8 {
		t.Errorf("over-verbose response should not score very high, got %f", score)
	}
}

// ==================== StructureScore Tests ====================

func TestStructure_WithCodeBlock(t *testing.T) {
	response := "Here's the solution:\n```go\nfmt.Println(\"hello\")\n```\nThis prints hello."
	score := StructureScore(response)
	if score < 0.6 {
		t.Errorf("response with code block should get bonus, got %f", score)
	}
}

func TestStructure_PlainText(t *testing.T) {
	response := "Yes, you can do that by calling the function directly."
	score := StructureScore(response)
	if score > 0.6 {
		t.Errorf("plain text should score neutral, got %f", score)
	}
}

func TestStructure_NumberedList(t *testing.T) {
	response := "Steps:\n1. Open the file\n2. Read contents\n3. Parse JSON\n4. Process data\n5. Close file"
	score := StructureScore(response)
	if score < 0.6 {
		t.Errorf("numbered list should get bonus, got %f", score)
	}
}

func TestStructure_EmptyResponse(t *testing.T) {
	score := StructureScore("")
	if score != 0.3 {
		t.Errorf("empty response should score 0.3, got %f", score)
	}
}

func TestStructure_BulletList(t *testing.T) {
	response := "Key points:\n- First point\n- Second point\n- Third point\n- Fourth point"
	score := StructureScore(response)
	if score < 0.55 {
		t.Errorf("bullet list should get bonus, got %f", score)
	}
}

// ==================== ConsistencyScore Tests ====================

func TestConsistency_Contradicting(t *testing.T) {
	response := "This approach is correct for the use case. However, this approach is not correct when you consider edge cases."
	score := ConsistencyScore(response)
	if score > 0.8 {
		t.Errorf("contradicting response should score low, got %f", score)
	}
}

func TestConsistency_Consistent(t *testing.T) {
	response := "Use the http.Get function to make the request. Parse the response body using json.Decoder. Handle errors appropriately."
	score := ConsistencyScore(response)
	if score < 0.9 {
		t.Errorf("consistent response should score high, got %f", score)
	}
}

func TestConsistency_MultipleContradictions(t *testing.T) {
	response := "This is valid code. Actually this is not valid. It will work in production. But it will not work in edge cases. The approach is safe. Actually it is not safe."
	score := ConsistencyScore(response)
	if score > 0.5 {
		t.Errorf("multiple contradictions should score very low, got %f", score)
	}
}

func TestConsistency_Empty(t *testing.T) {
	score := ConsistencyScore("")
	if score != 0.5 {
		t.Errorf("empty response should score 0.5, got %f", score)
	}
}

// ==================== FinishScore Tests ====================

func TestFinish_Stop(t *testing.T) {
	score := FinishScore("stop")
	if score != 1.0 {
		t.Errorf("stop should score 1.0, got %f", score)
	}
}

func TestFinish_Length(t *testing.T) {
	score := FinishScore("length")
	if score > 0.5 {
		t.Errorf("length (truncated) should score low, got %f", score)
	}
}

func TestFinish_ContentFilter(t *testing.T) {
	score := FinishScore("content_filter")
	if score > 0.4 {
		t.Errorf("content_filter should score low, got %f", score)
	}
}

func TestFinish_Empty(t *testing.T) {
	score := FinishScore("")
	if score != 0.5 {
		t.Errorf("empty finish reason should score 0.5, got %f", score)
	}
}

func TestFinish_Unknown(t *testing.T) {
	score := FinishScore("tool_calls")
	if score != 0.6 {
		t.Errorf("unknown finish reason should score 0.6, got %f", score)
	}
}

// ==================== CombinedScore Tests ====================

func TestCombined_HighConfidence(t *testing.T) {
	scorer := NewScorer(DefaultScorerConfig())
	response := "Here's the solution:\n```go\nfunc Add(a, b int) int {\n\treturn a + b\n}\n```\n\n1. The function takes two integers\n2. Returns their sum\n3. Time complexity is O(1)"
	result := scorer.CombinedScore(response, 50, 80, "stop")
	if result.Score < 0.65 {
		t.Errorf("high-quality response should score high, got %f", result.Score)
	}
	if result.Recommendation != "accept" {
		t.Errorf("expected 'accept' recommendation, got %q", result.Recommendation)
	}
}

func TestCombined_LowConfidence(t *testing.T) {
	scorer := NewScorer(DefaultScorerConfig())
	response := "I think maybe this might work, but I'm not sure. Perhaps try something else? It could be wrong."
	result := scorer.CombinedScore(response, 200, 15, "length")
	if result.Score > 0.6 {
		t.Errorf("low-quality response should score low, got %f", result.Score)
	}
}

func TestCombined_SignalsPresent(t *testing.T) {
	scorer := NewScorer(DefaultScorerConfig())
	result := scorer.CombinedScore("Test response", 10, 10, "stop")
	expected := []string{"hedging", "completeness", "structure", "consistency", "finish"}
	for _, sig := range expected {
		if _, ok := result.Signals[sig]; !ok {
			t.Errorf("missing signal %q in result", sig)
		}
	}
}

func TestCombined_ScoreClamped(t *testing.T) {
	scorer := NewScorer(DefaultScorerConfig())
	result := scorer.CombinedScore("x", 1, 1, "stop")
	if result.Score < 0.0 || result.Score > 1.0 {
		t.Errorf("score should be clamped to [0,1], got %f", result.Score)
	}
}

// ==================== ConfidenceMap Tests ====================

func TestConfidenceMap_RecordAndLookup(t *testing.T) {
	cm := NewConfidenceMap()
	cm.Record("coding", "tier1", 0.8)
	cm.Record("coding", "tier1", 0.9)
	cm.Record("coding", "tier1", 0.7)

	result := cm.Lookup("coding", "tier1")
	if !result.Found {
		t.Fatal("expected to find entry")
	}
	if result.SampleCount != 3 {
		t.Errorf("expected 3 samples, got %d", result.SampleCount)
	}
	expected := 0.8
	if math.Abs(result.AverageConfidence-expected) > 0.01 {
		t.Errorf("expected avg ~%f, got %f", expected, result.AverageConfidence)
	}
}

func TestConfidenceMap_LookupMissing(t *testing.T) {
	cm := NewConfidenceMap()
	result := cm.Lookup("nonexistent", "tier1")
	if result.Found {
		t.Error("expected not found for missing entry")
	}
}

func TestConfidenceMap_MultipleTiers(t *testing.T) {
	cm := NewConfidenceMap()
	cm.Record("coding", "tier1", 0.9)
	cm.Record("coding", "tier2", 0.5)

	r1 := cm.Lookup("coding", "tier1")
	r2 := cm.Lookup("coding", "tier2")

	if r1.AverageConfidence <= r2.AverageConfidence {
		t.Errorf("tier1 should have higher confidence: tier1=%f, tier2=%f",
			r1.AverageConfidence, r2.AverageConfidence)
	}
}

func TestConfidenceMap_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "confidence.json")

	cm := NewConfidenceMap()
	cm.Record("analysis", "tier1", 0.85)
	cm.Record("analysis", "tier1", 0.75)
	cm.Record("coding", "tier2", 0.60)

	if err := cm.Save(path); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	cm2 := NewConfidenceMap()
	if err := cm2.Load(path); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	r := cm2.Lookup("analysis", "tier1")
	if !r.Found || r.SampleCount != 2 {
		t.Errorf("persistence round-trip failed: %+v", r)
	}
	if math.Abs(r.AverageConfidence-0.80) > 0.01 {
		t.Errorf("expected avg 0.80, got %f", r.AverageConfidence)
	}

	r2 := cm2.Lookup("coding", "tier2")
	if !r2.Found || r2.SampleCount != 1 {
		t.Errorf("coding/tier2 round-trip failed: %+v", r2)
	}
}

func TestConfidenceMap_PersistenceFileNotFound(t *testing.T) {
	cm := NewConfidenceMap()
	err := cm.Load(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err == nil {
		t.Error("expected error loading nonexistent file")
	}
}

func TestConfidenceMap_ThreadSafety(t *testing.T) {
	cm := NewConfidenceMap()
	var wg sync.WaitGroup

	// Concurrent writers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			taskType := "coding"
			if n%3 == 0 {
				taskType = "analysis"
			}
			cm.Record(taskType, "tier1", float64(n%10)/10.0)
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cm.Lookup("coding", "tier1")
			cm.Lookup("analysis", "tier1")
		}()
	}

	wg.Wait()

	// Verify data integrity — we should have some records
	r := cm.Lookup("coding", "tier1")
	if !r.Found || r.SampleCount == 0 {
		t.Error("expected some coding records after concurrent writes")
	}
}

// ==================== Task Classification Tests ====================

func TestClassifyTaskType_Coding(t *testing.T) {
	taskType := ClassifyTaskType("Please implement a function to sort an array")
	if taskType != "coding" {
		t.Errorf("expected 'coding', got %q", taskType)
	}
}

func TestClassifyTaskType_Analysis(t *testing.T) {
	taskType := ClassifyTaskType("Analyze the performance of this security vulnerability")
	if taskType != "analysis" {
		t.Errorf("expected 'analysis', got %q", taskType)
	}
}

func TestClassifyTaskType_Informational(t *testing.T) {
	taskType := ClassifyTaskType("Explain what a goroutine is and how does it work")
	if taskType != "informational" {
		t.Errorf("expected 'informational', got %q", taskType)
	}
}

func TestClassifyTaskType_General(t *testing.T) {
	taskType := ClassifyTaskType("xyz abc")
	if taskType != "general" {
		t.Errorf("expected 'general' for unknown input, got %q", taskType)
	}
}

func TestRecordFromPrompt(t *testing.T) {
	cm := NewConfidenceMap()
	cm.RecordFromPrompt("implement a function to sort data", "tier1", 0.85)
	r := cm.Lookup("coding", "tier1")
	if !r.Found {
		t.Error("expected record from prompt to classify as coding")
	}
}

// ==================== Edge Case Tests ====================

func TestEdge_VeryLongResponse(t *testing.T) {
	response := strings.Repeat("This is a detailed technical explanation. ", 500)
	score := HedgingScore(response)
	if score < 0.9 {
		t.Errorf("long confident response should score high, got %f", score)
	}
}

func TestEdge_NonEnglishContent(t *testing.T) {
	response := "这是一个技术解决方案。使用Go语言实现排序算法。"
	scorer := NewScorer(DefaultScorerConfig())
	result := scorer.CombinedScore(response, 20, 15, "stop")
	if result.Score < 0.0 || result.Score > 1.0 {
		t.Errorf("non-English should still produce valid score, got %f", result.Score)
	}
}

func TestEdge_SaveToReadOnlyDir(t *testing.T) {
	cm := NewConfidenceMap()
	cm.Record("coding", "tier1", 0.8)
	// Try saving to a path that can't be created
	err := cm.Save(filepath.Join(string(os.PathSeparator), "nonexistent_root_dir_abc123", "test.json"))
	if err == nil {
		t.Error("expected error saving to invalid path")
	}
}
