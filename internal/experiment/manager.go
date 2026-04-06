package experiment

import (
	"sync"
	"time"
)

// MetricEvent carries a single metric data point to be recorded against a variant.
type MetricEvent struct {
	Cost       float64
	Tokens     int64
	LatencyMs  int64
	CacheHit   bool
	Escalation bool
	Confidence float64
	Error      bool
}

// VariantStats aggregates metrics for one variant of an experiment.
type VariantStats struct {
	VariantID      string
	RequestCount   int64
	TotalCost      float64
	TotalTokens    int64
	TotalLatencyMs int64
	CacheHits      int64
	CacheMisses    int64
	Escalations    int64
	AvgConfidence  float64
	Errors         int64

	// internal accumulators for computing the running average
	confidenceSum float64
}

// ExperimentResults holds per-variant stats for an experiment.
type ExperimentResults struct {
	ExperimentID string
	VariantStats map[string]*VariantStats
}

// Manager owns experiments, assignments, and result collection.
type Manager struct {
	experiments map[string]*Experiment
	assignments map[string]*Assignment // workflowID → assignment
	results     map[string]*ExperimentResults
	mu          sync.RWMutex
}

// NewManager creates a ready-to-use Manager.
func NewManager() *Manager {
	return &Manager{
		experiments: make(map[string]*Experiment),
		assignments: make(map[string]*Assignment),
		results:     make(map[string]*ExperimentResults),
	}
}

// RegisterExperiment adds (or replaces) an experiment.
func (m *Manager) RegisterExperiment(exp Experiment) {
	m.mu.Lock()
	defer m.mu.Unlock()

	e := exp // copy
	m.experiments[exp.ID] = &e

	if _, ok := m.results[exp.ID]; !ok {
		m.results[exp.ID] = &ExperimentResults{
			ExperimentID: exp.ID,
			VariantStats: make(map[string]*VariantStats),
		}
		for _, v := range exp.Variants {
			m.results[exp.ID].VariantStats[v.ID] = &VariantStats{VariantID: v.ID}
		}
	}
}

// GetAssignment returns the existing assignment for a workflow, or creates
// one from the first active experiment that applies.
func (m *Manager) GetAssignment(workflowID string) *Assignment {
	m.mu.RLock()
	if a, ok := m.assignments[workflowID]; ok {
		m.mu.RUnlock()
		return a
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock.
	if a, ok := m.assignments[workflowID]; ok {
		return a
	}

	for _, exp := range m.experiments {
		if !exp.IsActive() {
			continue
		}
		a := exp.Assign(workflowID)
		if a != nil {
			m.assignments[workflowID] = a
			return a
		}
	}
	return nil
}

// GetAssignmentForExperiment returns (or creates) an assignment for a specific experiment.
func (m *Manager) GetAssignmentForExperiment(workflowID, experimentID string) *Assignment {
	key := workflowID + ":" + experimentID

	m.mu.RLock()
	if a, ok := m.assignments[key]; ok {
		m.mu.RUnlock()
		return a
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	if a, ok := m.assignments[key]; ok {
		return a
	}

	exp, ok := m.experiments[experimentID]
	if !ok {
		return nil
	}
	a := exp.Assign(workflowID)
	if a != nil {
		m.assignments[key] = a
	}
	return a
}

// RecordMetric records a metric event for the variant assigned to workflowID.
func (m *Manager) RecordMetric(workflowID string, metric MetricEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Walk all assignments whose workflowID matches.
	for _, a := range m.assignments {
		if a.WorkflowID != workflowID {
			continue
		}
		res, ok := m.results[a.ExperimentID]
		if !ok {
			continue
		}
		vs, ok := res.VariantStats[a.VariantID]
		if !ok {
			continue
		}

		vs.RequestCount++
		vs.TotalCost += metric.Cost
		vs.TotalTokens += metric.Tokens
		vs.TotalLatencyMs += metric.LatencyMs
		if metric.CacheHit {
			vs.CacheHits++
		} else {
			vs.CacheMisses++
		}
		if metric.Escalation {
			vs.Escalations++
		}
		if metric.Error {
			vs.Errors++
		}
		vs.confidenceSum += metric.Confidence
		if vs.RequestCount > 0 {
			vs.AvgConfidence = vs.confidenceSum / float64(vs.RequestCount)
		}
	}
}

// GetResults returns a snapshot of the experiment results.
func (m *Manager) GetResults(experimentID string) *ExperimentResults {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res, ok := m.results[experimentID]
	if !ok {
		return nil
	}

	// Return a deep-enough copy so the caller can read without holding the lock.
	out := &ExperimentResults{
		ExperimentID: res.ExperimentID,
		VariantStats: make(map[string]*VariantStats, len(res.VariantStats)),
	}
	for k, v := range res.VariantStats {
		cp := *v
		out.VariantStats[k] = &cp
	}
	return out
}

// ActiveExperiments returns all experiments currently active.
func (m *Manager) ActiveExperiments() []Experiment {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var active []Experiment
	for _, exp := range m.experiments {
		if exp.IsActive() {
			active = append(active, *exp)
		}
	}
	return active
}

// AllExperiments returns every registered experiment.
func (m *Manager) AllExperiments() []Experiment {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]Experiment, 0, len(m.experiments))
	for _, exp := range m.experiments {
		out = append(out, *exp)
	}
	return out
}

// now is an indirection for testing (not exported).
var now = time.Now
