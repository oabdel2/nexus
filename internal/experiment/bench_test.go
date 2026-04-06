package experiment

import (
	"fmt"
	"testing"
)

func BenchmarkAssignment(b *testing.B) {
	exp := CascadeThresholdExperiment()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exp.Assign(fmt.Sprintf("wf-%d", i))
	}
}

func BenchmarkRecordMetric(b *testing.B) {
	mgr := NewManager()
	exp := CascadeThresholdExperiment()
	mgr.RegisterExperiment(exp)

	// Pre-assign workflows
	for i := 0; i < b.N; i++ {
		mgr.GetAssignmentForExperiment(fmt.Sprintf("wf-bench-%d", i), exp.ID)
	}

	evt := MetricEvent{Cost: 0.01, Tokens: 100, LatencyMs: 200, CacheHit: true}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.RecordMetric(fmt.Sprintf("wf-bench-%d", i), evt)
	}
}

func BenchmarkZTest(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ZTest(500, 10000, 550, 10000)
	}
}

func BenchmarkGetResults(b *testing.B) {
	mgr := NewManager()
	exp := CascadeThresholdExperiment()
	mgr.RegisterExperiment(exp)
	for i := 0; i < 1000; i++ {
		wfID := fmt.Sprintf("wf-res-%d", i)
		mgr.GetAssignmentForExperiment(wfID, exp.ID)
		mgr.RecordMetric(wfID, MetricEvent{Cost: 0.01, Tokens: 100, LatencyMs: 200})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.GetResults(exp.ID)
	}
}
