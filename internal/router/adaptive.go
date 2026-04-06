package router

import (
	"log/slog"
	"sync"

	"github.com/nexus-gateway/nexus/internal/config"
	"github.com/nexus-gateway/nexus/internal/eval"
)

// AdaptiveRouter wraps the base router and uses confidence map data
// to make better-informed routing decisions.
type AdaptiveRouter struct {
	base           *Router
	confidenceMap  *eval.ConfidenceMap
	minSamples     int
	highConfidence float64
	lowConfidence  float64
	enabled        bool
	logger         *slog.Logger

	mu         sync.RWMutex
	overrides  int64
	downgrades int64
	upgrades   int64
}

// AdaptiveStats holds adaptive routing statistics.
type AdaptiveStats struct {
	Enabled    bool                       `json:"enabled"`
	Overrides  int64                      `json:"overrides"`
	Downgrades int64                      `json:"downgrades"`
	Upgrades   int64                      `json:"upgrades"`
	TaskTypes  map[string]TaskTypeStats   `json:"task_types"`
}

// TaskTypeStats holds per-task-type confidence and decision info.
type TaskTypeStats struct {
	CheapConfidence   float64 `json:"cheap_confidence"`
	CheapSamples      int     `json:"cheap_samples"`
	PremiumConfidence float64 `json:"premium_confidence"`
	PremiumSamples    int     `json:"premium_samples"`
	CurrentDecision   string  `json:"current_decision"`
}

// NewAdaptiveRouter creates an AdaptiveRouter with the given config.
func NewAdaptiveRouter(base *Router, cm *eval.ConfidenceMap, cfg config.AdaptiveConfig) *AdaptiveRouter {
	minSamples := cfg.MinSamples
	if minSamples <= 0 {
		minSamples = 50
	}
	highConf := cfg.HighConfidence
	if highConf <= 0 {
		highConf = 0.90
	}
	lowConf := cfg.LowConfidence
	if lowConf <= 0 {
		lowConf = 0.50
	}

	return &AdaptiveRouter{
		base:           base,
		confidenceMap:  cm,
		minSamples:     minSamples,
		highConfidence: highConf,
		lowConfidence:  lowConf,
		enabled:        cfg.Enabled,
		logger:         base.logger,
	}
}

// Route checks the confidence map first, then falls back to the base router.
func (ar *AdaptiveRouter) Route(prompt, role string, stepRatio, budgetRatio float64, contextLen int) ModelSelection {
	if !ar.enabled {
		return ar.base.Route(prompt, role, stepRatio, budgetRatio, contextLen)
	}

	taskType := eval.ClassifyTaskType(prompt)

	cheapConf := ar.confidenceMap.Lookup(taskType, "cheap")

	// Downgrade: cheap tier has high confidence with enough samples
	if cheapConf.SampleCount >= ar.minSamples && cheapConf.AverageConfidence >= ar.highConfidence {
		ar.mu.Lock()
		ar.downgrades++
		ar.overrides++
		ar.mu.Unlock()
		ar.logger.Info("adaptive downgrade: confidence map says cheap is fine",
			"task_type", taskType,
			"cheap_confidence", cheapConf.AverageConfidence,
			"samples", cheapConf.SampleCount,
		)
		return ar.base.ForceRoute("cheap", prompt, role, stepRatio, budgetRatio, contextLen)
	}

	// Upgrade: cheap tier has low confidence with enough samples
	if cheapConf.SampleCount >= ar.minSamples && cheapConf.AverageConfidence <= ar.lowConfidence {
		ar.mu.Lock()
		ar.upgrades++
		ar.overrides++
		ar.mu.Unlock()
		ar.logger.Info("adaptive upgrade: confidence map says cheap isn't good enough",
			"task_type", taskType,
			"cheap_confidence", cheapConf.AverageConfidence,
			"samples", cheapConf.SampleCount,
		)
		return ar.base.ForceRoute("premium", prompt, role, stepRatio, budgetRatio, contextLen)
	}

	// Not enough data — use normal routing
	return ar.base.Route(prompt, role, stepRatio, budgetRatio, contextLen)
}

// Stats returns adaptive routing statistics.
func (ar *AdaptiveRouter) Stats() AdaptiveStats {
	ar.mu.RLock()
	overrides := ar.overrides
	downgrades := ar.downgrades
	upgrades := ar.upgrades
	ar.mu.RUnlock()

	taskTypes := make(map[string]TaskTypeStats)
	for _, tt := range ar.confidenceMap.TaskTypes() {
		cheapLookup := ar.confidenceMap.Lookup(tt, "cheap")
		premiumLookup := ar.confidenceMap.Lookup(tt, "premium")

		decision := "insufficient_data"
		if cheapLookup.SampleCount >= ar.minSamples {
			if cheapLookup.AverageConfidence >= ar.highConfidence {
				decision = "use_cheap"
			} else if cheapLookup.AverageConfidence <= ar.lowConfidence {
				decision = "use_premium"
			} else {
				decision = "insufficient_data"
			}
		}

		taskTypes[tt] = TaskTypeStats{
			CheapConfidence:   cheapLookup.AverageConfidence,
			CheapSamples:      cheapLookup.SampleCount,
			PremiumConfidence: premiumLookup.AverageConfidence,
			PremiumSamples:    premiumLookup.SampleCount,
			CurrentDecision:   decision,
		}
	}

	return AdaptiveStats{
		Enabled:    ar.enabled,
		Overrides:  overrides,
		Downgrades: downgrades,
		Upgrades:   upgrades,
		TaskTypes:  taskTypes,
	}
}
