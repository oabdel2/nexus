package router

import (
	"math/rand"
	"time"
)

// CascadeRouter handles try-cheap-first-then-escalate logic.
type CascadeRouter struct {
	router     *Router
	threshold  float64
	maxLatency time.Duration
	sampleRate float64
}

// NewCascadeRouter creates a CascadeRouter.
func NewCascadeRouter(r *Router, threshold float64, maxLatencyMs int, sampleRate float64) *CascadeRouter {
	return &CascadeRouter{
		router:     r,
		threshold:  threshold,
		maxLatency: time.Duration(maxLatencyMs) * time.Millisecond,
		sampleRate: sampleRate,
	}
}

// CascadeResult captures the outcome of a cascade attempt.
type CascadeResult struct {
	UsedCheapModel  bool          `json:"used_cheap_model"`
	Escalated       bool          `json:"escalated"`
	CheapConfidence float64       `json:"cheap_confidence"`
	CostSaved       float64       `json:"cost_saved"`
	LatencyAdded    time.Duration `json:"latency_added"`
}

// ShouldCascade returns true if this request should try the cheap model first.
// Only cascades when the router picked "mid" or "premium" tier.
func (c *CascadeRouter) ShouldCascade(score ComplexityScore, tier string) bool {
	// Never cascade economy or cheap — already the cheapest
	if tier == "economy" || tier == "cheap" {
		return false
	}

	// Respect sample rate
	if c.sampleRate < 1.0 && rand.Float64() > c.sampleRate {
		return false
	}

	// Only cascade if the complexity score is below the threshold,
	// meaning the task might be simple enough for a cheap model.
	return score.FinalScore < c.threshold
}

// CheapSelection returns the cheapest available model selection.
func (c *CascadeRouter) CheapSelection() ModelSelection {
	provider, model := c.router.selectModelWithFallback("cheap")
	return ModelSelection{
		Provider: provider,
		Model:    model,
		Tier:     "cheap",
		Reason:   "cascade cheap attempt",
	}
}

// MaxLatency returns the maximum latency allowed for the cheap model attempt.
func (c *CascadeRouter) MaxLatency() time.Duration {
	return c.maxLatency
}

// Threshold returns the confidence threshold for cascade acceptance.
func (c *CascadeRouter) Threshold() float64 {
	return c.threshold
}
