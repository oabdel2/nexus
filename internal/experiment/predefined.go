package experiment

import "time"

// CascadeThresholdExperiment returns an A/B test comparing no cascade routing
// (control) with cascade routing at a 0.78 confidence threshold (treatment).
func CascadeThresholdExperiment() Experiment {
	return Experiment{
		ID:          "cascade-threshold-ab",
		Name:        "Cascade Threshold A/B",
		Description: "Control: no cascade. Treatment: cascade at 0.78 confidence.",
		Enabled:     true,
		StartTime:   time.Now(),
		Variants: []Variant{
			{
				ID:   "control",
				Name: "No Cascade",
				Config: map[string]interface{}{
					"cascade_enabled": false,
				},
			},
			{
				ID:   "treatment_a",
				Name: "Cascade 0.78",
				Config: map[string]interface{}{
					"cascade_enabled":             true,
					"cascade_confidence_threshold": 0.78,
				},
			},
		},
		TrafficSplit: []float64{0.5, 0.5},
	}
}

// CompressionExperiment returns an A/B test comparing no prompt compression
// (control) with all compression strategies enabled (treatment).
func CompressionExperiment() Experiment {
	return Experiment{
		ID:          "compression-ab",
		Name:        "Compression A/B",
		Description: "Control: no compression. Treatment: whitespace + code_strip + history_truncate.",
		Enabled:     true,
		StartTime:   time.Now(),
		Variants: []Variant{
			{
				ID:   "control",
				Name: "No Compression",
				Config: map[string]interface{}{
					"compression_enabled": false,
				},
			},
			{
				ID:   "treatment_a",
				Name: "All Compression",
				Config: map[string]interface{}{
					"compression_enabled":    true,
					"compression_whitespace": true,
					"compression_code_strip": true,
					"compression_history":    true,
				},
			},
		},
		TrafficSplit: []float64{0.5, 0.5},
	}
}

// TierThresholdExperiment returns an A/B test comparing the current router
// thresholds with a more aggressive down-routing strategy.
func TierThresholdExperiment() Experiment {
	return Experiment{
		ID:          "tier-threshold-ab",
		Name:        "Tier Threshold A/B",
		Description: "Control: current thresholds. Treatment: more aggressive downrouting.",
		Enabled:     true,
		StartTime:   time.Now(),
		Variants: []Variant{
			{
				ID:   "control",
				Name: "Current Thresholds",
				Config: map[string]interface{}{
					"router_threshold": 0.7,
				},
			},
			{
				ID:   "treatment_a",
				Name: "Aggressive Downrouting",
				Config: map[string]interface{}{
					"router_threshold": 0.85,
				},
			},
		},
		TrafficSplit: []float64{0.5, 0.5},
	}
}

// CacheAggressivenessExperiment returns an A/B test comparing the default
// semantic cache threshold (0.70) with a lower, more aggressive threshold (0.65).
func CacheAggressivenessExperiment() Experiment {
	return Experiment{
		ID:          "cache-aggressiveness-ab",
		Name:        "Cache Aggressiveness A/B",
		Description: "Control: 0.70 semantic similarity. Treatment: 0.65 threshold.",
		Enabled:     true,
		StartTime:   time.Now(),
		Variants: []Variant{
			{
				ID:   "control",
				Name: "Standard Cache (0.70)",
				Config: map[string]interface{}{
					"cache_similarity_min": 0.70,
				},
			},
			{
				ID:   "treatment_a",
				Name: "Aggressive Cache (0.65)",
				Config: map[string]interface{}{
					"cache_similarity_min": 0.65,
				},
			},
		},
		TrafficSplit: []float64{0.5, 0.5},
	}
}
