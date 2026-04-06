package router

import (
	"log/slog"

	"github.com/nexus-gateway/nexus/internal/config"
)

type ModelSelection struct {
	Provider string          `json:"provider"`
	Model    string          `json:"model"`
	Tier     string          `json:"tier"`
	Score    ComplexityScore `json:"score"`
	Reason   string          `json:"reason"`
}

type Router struct {
	cfg             config.RouterConfig
	providers       []config.ProviderConfig
	logger          *slog.Logger
	smartClassifier *SmartClassifier
}

func New(cfg config.RouterConfig, providers []config.ProviderConfig, logger *slog.Logger) *Router {
	r := &Router{
		cfg:       cfg,
		providers: providers,
		logger:    logger,
	}
	if cfg.SmartClassifier {
		r.smartClassifier = NewSmartClassifier()
	}
	return r
}

// tierFallbackOrder defines the upgrade path when a tier has no available model.
var tierFallbackOrder = []string{"economy", "cheap", "mid", "premium"}

func (r *Router) Route(prompt string, role string, stepRatio float64, budgetRatio float64, contextLen int) ModelSelection {
	var score ComplexityScore
	if r.smartClassifier != nil {
		score = r.smartClassifier.Classify(prompt, role, stepRatio, budgetRatio, contextLen)
	} else {
		score = ClassifyComplexity(prompt, role, stepRatio, budgetRatio, contextLen)
	}

	w := r.cfg.ComplexityWeights
	// Include length and structure scores in the prompt complexity weight
	basePrompt := score.PromptScore*0.6 + score.LengthScore*0.2 + score.StructScore*0.2
	score.FinalScore = basePrompt*w.PromptComplexity +
		score.ContextScore*w.ContextLength +
		score.RoleScore*w.AgentRole +
		score.PositionScore*w.StepPosition +
		score.BudgetScore*w.BudgetPressure

	var tier string
	var reason string

	threshold := r.cfg.Threshold
	if score.FinalScore >= threshold*0.8 {
		tier = "premium"
		reason = "high complexity score"
	} else if score.FinalScore >= threshold*0.5 {
		tier = "mid"
		reason = "moderate complexity"
	} else if score.FinalScore >= threshold*0.3 {
		tier = "cheap"
		reason = "low complexity score"
	} else {
		tier = "economy"
		reason = "trivial task"
	}

	// Budget override: if budget is very low, force cheaper tier
	if budgetRatio < 0.15 && tier == "premium" {
		tier = "mid"
		reason = "budget pressure override"
	}
	if budgetRatio < 0.05 {
		tier = "economy"
		reason = "budget nearly exhausted"
	}

	provider, model := r.selectModelWithFallback(tier)

	selection := ModelSelection{
		Provider: provider,
		Model:    model,
		Tier:     tier,
		Score:    score,
		Reason:   reason,
	}

	r.logger.Info("routing decision",
		"tier", tier,
		"model", model,
		"provider", provider,
		"final_score", score.FinalScore,
		"threshold", threshold,
		"reason", reason,
		"prompt_score", score.PromptScore,
		"length_score", score.LengthScore,
		"struct_score", score.StructScore,
		"context_score", score.ContextScore,
		"role_score", score.RoleScore,
		"position_score", score.PositionScore,
		"budget_score", score.BudgetScore,
	)

	return selection
}

// selectModelWithFallback tries the requested tier first, then falls back
// through the tier order (economy→cheap→mid→premium) until a model is found.
func (r *Router) selectModelWithFallback(tier string) (string, string) {
	// Find the starting index in the fallback order
	startIdx := 0
	for i, t := range tierFallbackOrder {
		if t == tier {
			startIdx = i
			break
		}
	}

	// Try each tier from the requested one upward
	for i := startIdx; i < len(tierFallbackOrder); i++ {
		candidate := tierFallbackOrder[i]
		for _, p := range r.providers {
			if !p.Enabled {
				continue
			}
			for _, m := range p.Models {
				if m.Tier == candidate {
					return p.Name, m.Name
				}
			}
		}
	}

	// Ultimate fallback: first available model from any enabled provider
	for _, p := range r.providers {
		if !p.Enabled {
			continue
		}
		if len(p.Models) > 0 {
			return p.Name, p.Models[0].Name
		}
	}

	return "", ""
}

func (r *Router) GetModelCost(provider, model string) float64 {
	for _, p := range r.providers {
		if p.Name == provider {
			for _, m := range p.Models {
				if m.Name == model {
					return m.CostPer1K
				}
			}
		}
	}
	return 0.005 // default fallback
}

// ForceSelectTier selects a model from a specific tier, using the same
// fallback logic as selectModelWithFallback.
func (r *Router) ForceSelectTier(tier string) (provider string, model string) {
	return r.selectModelWithFallback(tier)
}
