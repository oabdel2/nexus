package eval

import (
	"math"
	"strings"
)

// ScorerConfig controls the heuristic confidence scorer.
type ScorerConfig struct {
	HedgingPenalty   float64 // weight for hedging signal (default 0.15)
	BrevityThreshold int     // min completion tokens for confident response
	StructureBonus   float64 // bonus weight for structured output (default 0.10)
}

// ConfidenceResult holds the overall confidence and individual signal scores.
type ConfidenceResult struct {
	Score          float64            `json:"score"`          // 0.0 to 1.0
	Signals        map[string]float64 `json:"signals"`        // individual signal scores
	Recommendation string             `json:"recommendation"` // "accept", "escalate", "uncertain"
}

// ConfidenceScorer evaluates response quality using heuristic signals.
type ConfidenceScorer struct {
	cfg ScorerConfig
}

// DefaultScorerConfig returns sensible defaults.
func DefaultScorerConfig() ScorerConfig {
	return ScorerConfig{
		HedgingPenalty:   0.15,
		BrevityThreshold: 20,
		StructureBonus:   0.10,
	}
}

// NewScorer creates a ConfidenceScorer with the given config.
func NewScorer(cfg ScorerConfig) *ConfidenceScorer {
	return &ConfidenceScorer{cfg: cfg}
}

// hedgingPhrases lists uncertain language markers.
var hedgingPhrases = []string{
	"i think",
	"maybe",
	"i'm not sure",
	"it's possible",
	"might",
	"could be",
	"arguably",
	"perhaps",
	"not certain",
	"not entirely sure",
	"i believe",
	"it seems",
	"possibly",
	"probably",
	"i guess",
	"not confident",
}

// HedgingScore returns 1.0 (no hedging) to 0.0 (maximum hedging).
func HedgingScore(response string) float64 {
	if len(response) == 0 {
		return 0.5
	}
	lower := strings.ToLower(response)
	count := 0
	for _, phrase := range hedgingPhrases {
		count += strings.Count(lower, phrase)
	}
	if count == 0 {
		return 1.0
	}
	// Each hedge reduces confidence; diminishing returns via log
	penalty := math.Min(float64(count)*0.12, 0.8)
	return clamp(1.0-penalty, 0.0, 1.0)
}

// CompletenessScore evaluates whether the response length is proportional to prompt complexity.
// promptTokens is estimated token count of the prompt, responseTokens of the response.
func CompletenessScore(promptTokens, responseTokens, brevityThreshold int) float64 {
	if promptTokens == 0 {
		return 0.5
	}
	if responseTokens < brevityThreshold {
		// Very short response — scale linearly
		return clamp(float64(responseTokens)/float64(brevityThreshold), 0.0, 0.6)
	}
	// Proportional: ratio of response to prompt. Ideal range: 0.5x to 3x.
	ratio := float64(responseTokens) / float64(promptTokens)
	if ratio < 0.3 {
		return 0.4
	}
	if ratio > 5.0 {
		// Over-verbose may indicate rambling
		return 0.7
	}
	return clamp(0.5+ratio*0.15, 0.5, 1.0)
}

// StructureScore gives a bonus for structured output (code, lists, headers).
func StructureScore(response string) float64 {
	if len(response) == 0 {
		return 0.3
	}

	score := 0.5 // baseline
	lower := response

	// Code blocks
	codeBlocks := strings.Count(lower, "```")
	if codeBlocks >= 2 {
		score += 0.2
	}

	// Numbered lists (e.g., "1. ", "2. ")
	lines := strings.Split(response, "\n")
	numberedCount := 0
	bulletCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 2 && trimmed[0] >= '1' && trimmed[0] <= '9' && (strings.Contains(trimmed[:3], ". ") || strings.Contains(trimmed[:3], ") ")) {
			numberedCount++
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			bulletCount++
		}
	}
	if numberedCount >= 3 {
		score += 0.15
	}
	if bulletCount >= 3 {
		score += 0.1
	}

	// Headers (markdown)
	if strings.Contains(response, "## ") || strings.Contains(response, "### ") {
		score += 0.05
	}

	return clamp(score, 0.0, 1.0)
}

// ConsistencyScore detects self-contradictions in the response.
// Returns 1.0 for consistent text, lower for contradictions.
func ConsistencyScore(response string) float64 {
	if len(response) == 0 {
		return 0.5
	}

	lower := strings.ToLower(response)

	contradictionPairs := []struct {
		positive string
		negative string
	}{
		{"is true", "is not true"},
		{"is correct", "is not correct"},
		{"is correct", "is incorrect"},
		{"will work", "will not work"},
		{"will work", "won't work"},
		{"is possible", "is not possible"},
		{"is possible", "is impossible"},
		{"should work", "should not work"},
		{"should work", "shouldn't work"},
		{"is valid", "is not valid"},
		{"is valid", "is invalid"},
		{"is safe", "is not safe"},
		{"is safe", "is unsafe"},
		{"you can", "you cannot"},
		{"you can", "you can't"},
		{"does exist", "does not exist"},
		{"does exist", "doesn't exist"},
		{"is supported", "is not supported"},
		{"is recommended", "is not recommended"},
	}

	contradictions := 0
	for _, pair := range contradictionPairs {
		if strings.Contains(lower, pair.positive) && strings.Contains(lower, pair.negative) {
			contradictions++
		}
	}

	if contradictions == 0 {
		return 1.0
	}
	penalty := math.Min(float64(contradictions)*0.25, 0.7)
	return clamp(1.0-penalty, 0.0, 1.0)
}

// FinishScore maps the finish_reason to a confidence signal.
func FinishScore(finishReason string) float64 {
	switch strings.ToLower(finishReason) {
	case "stop":
		return 1.0
	case "length":
		return 0.4
	case "content_filter":
		return 0.3
	case "":
		return 0.5
	default:
		return 0.6
	}
}

// CombinedScore computes a weighted average of all signals.
func (s *ConfidenceScorer) CombinedScore(
	response string,
	promptTokens int,
	responseTokens int,
	finishReason string,
) ConfidenceResult {
	signals := map[string]float64{
		"hedging":      HedgingScore(response),
		"completeness": CompletenessScore(promptTokens, responseTokens, s.cfg.BrevityThreshold),
		"structure":    StructureScore(response),
		"consistency":  ConsistencyScore(response),
		"finish":       FinishScore(finishReason),
	}

	// Weighted combination
	weights := map[string]float64{
		"hedging":      0.20,
		"completeness": 0.20,
		"structure":    0.15,
		"consistency":  0.25,
		"finish":       0.20,
	}

	total := 0.0
	weightSum := 0.0
	for signal, score := range signals {
		w := weights[signal]
		total += score * w
		weightSum += w
	}

	finalScore := clamp(total/weightSum, 0.0, 1.0)

	recommendation := "uncertain"
	if finalScore >= 0.75 {
		recommendation = "accept"
	} else if finalScore < 0.45 {
		recommendation = "escalate"
	}

	return ConfidenceResult{
		Score:          finalScore,
		Signals:        signals,
		Recommendation: recommendation,
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
