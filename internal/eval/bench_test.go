package eval

import (
	"strings"
	"sync"
	"testing"
)

func generateResponse(size int) string {
	base := "This is a detailed technical explanation of the approach. Use the standard library for best results. "
	var sb strings.Builder
	for sb.Len() < size {
		sb.WriteString(base)
	}
	return sb.String()[:size]
}

func generateHedgingResponse(size int) string {
	base := "I think maybe this could work, but I'm not sure. Perhaps it might be correct. "
	var sb strings.Builder
	for sb.Len() < size {
		sb.WriteString(base)
	}
	return sb.String()[:size]
}

func BenchmarkHedgingScore(b *testing.B) {
	response := generateResponse(2000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		HedgingScore(response)
	}
}

func BenchmarkStructureScore(b *testing.B) {
	response := "Here's the solution:\n```go\nfunc Add(a, b int) int { return a + b }\n```\n\n1. Takes two ints\n2. Returns sum\n3. O(1) complexity\n\n- Clean\n- Simple\n- Tested"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StructureScore(response)
	}
}

func BenchmarkConsistencyScore(b *testing.B) {
	response := generateResponse(2000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ConsistencyScore(response)
	}
}

func BenchmarkCombinedScore(b *testing.B) {
	scorer := NewScorer(DefaultScorerConfig())
	response := generateResponse(2000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scorer.CombinedScore(response, 200, 500, "stop")
	}
}

func BenchmarkCombinedScore_Hedging(b *testing.B) {
	scorer := NewScorer(DefaultScorerConfig())
	response := generateHedgingResponse(2000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scorer.CombinedScore(response, 200, 500, "stop")
	}
}

func BenchmarkConfidenceMap_ConcurrentReadWrite(b *testing.B) {
	cm := NewConfidenceMap()
	// Pre-populate
	for i := 0; i < 100; i++ {
		cm.Record("coding", "tier1", 0.8)
		cm.Record("analysis", "tier2", 0.6)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%3 == 0 {
				cm.Record("coding", "tier1", 0.75)
			} else {
				cm.Lookup("coding", "tier1")
			}
			i++
		}
	})
}

func BenchmarkConfidenceMap_Write(b *testing.B) {
	cm := NewConfidenceMap()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cm.Record("coding", "tier1", 0.8)
	}
}

func BenchmarkConfidenceMap_Read(b *testing.B) {
	cm := NewConfidenceMap()
	cm.Record("coding", "tier1", 0.8)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cm.Lookup("coding", "tier1")
	}
}

func BenchmarkClassifyTaskType(b *testing.B) {
	prompt := "Please implement a function to sort an array and analyze the performance characteristics"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ClassifyTaskType(prompt)
	}
}

func BenchmarkConfidenceMap_HighContention(b *testing.B) {
	cm := NewConfidenceMap()
	var wg sync.WaitGroup
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(4)
		for j := 0; j < 2; j++ {
			go func() {
				defer wg.Done()
				cm.Record("coding", "tier1", 0.8)
			}()
			go func() {
				defer wg.Done()
				cm.Lookup("coding", "tier1")
			}()
		}
		wg.Wait()
	}
}
