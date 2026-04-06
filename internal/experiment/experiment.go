package experiment

import (
	"crypto/sha256"
	"encoding/binary"
	"time"
)

// Experiment defines an A/B test with one or more variants.
type Experiment struct {
	ID           string
	Name         string
	Description  string
	Enabled      bool
	StartTime    time.Time
	EndTime      time.Time // zero value means no end
	Variants     []Variant
	TrafficSplit []float64 // must sum to 1.0, e.g., [0.5, 0.5]
}

// Variant represents one arm of an experiment.
type Variant struct {
	ID     string // "control", "treatment_a", "treatment_b"
	Name   string
	Config map[string]interface{} // variant-specific config overrides
}

// Assignment records which variant a workflow was placed into.
type Assignment struct {
	ExperimentID string
	VariantID    string
	WorkflowID   string
	Timestamp    time.Time
}

// IsActive returns true when the experiment is enabled and the current time
// falls within [StartTime, EndTime). A zero EndTime means no expiry.
func (e *Experiment) IsActive() bool {
	if !e.Enabled {
		return false
	}
	now := time.Now()
	if now.Before(e.StartTime) {
		return false
	}
	if !e.EndTime.IsZero() && now.After(e.EndTime) {
		return false
	}
	return true
}

// Assign deterministically maps a workflowID to a variant based on a
// consistent hash so the same workflow always receives the same variant.
func (e *Experiment) Assign(workflowID string) *Assignment {
	if len(e.Variants) == 0 {
		return nil
	}

	bucket := hashToBucket(e.ID, workflowID)

	var cumulative float64
	for i, split := range e.TrafficSplit {
		cumulative += split
		if bucket < cumulative {
			return &Assignment{
				ExperimentID: e.ID,
				VariantID:    e.Variants[i].ID,
				WorkflowID:   workflowID,
				Timestamp:    time.Now(),
			}
		}
	}

	// Floating-point edge case — assign to last variant.
	return &Assignment{
		ExperimentID: e.ID,
		VariantID:    e.Variants[len(e.Variants)-1].ID,
		WorkflowID:   workflowID,
		Timestamp:    time.Now(),
	}
}

// hashToBucket returns a deterministic value in [0, 1) for a given
// experiment+workflow pair using SHA-256.
func hashToBucket(experimentID, workflowID string) float64 {
	h := sha256.New()
	h.Write([]byte(experimentID))
	h.Write([]byte(":"))
	h.Write([]byte(workflowID))
	sum := h.Sum(nil)
	// Take the first 8 bytes as a uint64, then normalise to [0, 1).
	v := binary.BigEndian.Uint64(sum[:8])
	return float64(v) / float64(^uint64(0))
}
