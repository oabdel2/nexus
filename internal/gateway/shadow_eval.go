package gateway

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"time"

	"github.com/nexus-gateway/nexus/internal/cache"
	"github.com/nexus-gateway/nexus/internal/eval"
	"github.com/nexus-gateway/nexus/internal/provider"
	"github.com/nexus-gateway/nexus/internal/router"
)

// runShadowEval sends the same request to a comparison tier and compares results.
// It runs in a background goroutine and failures are silent.
func (s *Server) runShadowEval(ctx context.Context, req provider.ChatRequest, prompt string,
	primarySelection router.ModelSelection, primaryResp *provider.ChatResponse) {

	comparisonTier := getComparisonTier(primarySelection.Tier)

	compProvider, compModel := s.router.ForceSelectTier(comparisonTier)
	if compProvider == "" {
		return
	}

	shadowCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	compReq := req
	compReq.Model = compModel
	p, ok := s.providers[compProvider]
	if !ok {
		return
	}
	compResp, err := p.Send(shadowCtx, compReq)
	if err != nil {
		return
	}

	if len(primaryResp.Choices) == 0 || len(compResp.Choices) == 0 {
		return
	}

	primaryText := primaryResp.Choices[0].Message.Content
	compText := compResp.Choices[0].Message.Content

	// Confidence scoring on both responses
	var primaryConf, compConf eval.ConfidenceResult
	if s.confidenceScorer != nil {
		primaryConf = s.confidenceScorer.CombinedScore(
			primaryText,
			primaryResp.Usage.PromptTokens,
			primaryResp.Usage.CompletionTokens,
			primaryResp.Choices[0].FinishReason,
		)
		compConf = s.confidenceScorer.CombinedScore(
			compText,
			compResp.Usage.PromptTokens,
			compResp.Usage.CompletionTokens,
			compResp.Choices[0].FinishReason,
		)
	}

	similarity := computeResponseSimilarity(primaryText, compText)

	agreement := similarity > 0.70
	taskType := classifyTaskType(prompt)

	s.cache.Shadow().RecordResult(cache.ShadowResult{
		Query:          prompt,
		CachedResponse: primaryText,
		FreshResponse:  compText,
		CacheHit:       false,
		CacheLayer:     "shadow_eval",
		Similarity:     similarity,
		Agreement:      agreement,
	})

	if s.confidenceMap != nil {
		s.confidenceMap.Record(taskType, primarySelection.Tier, primaryConf.Score)
		s.confidenceMap.Record(taskType, comparisonTier, compConf.Score)
	}

	s.logger.Info("shadow eval completed",
		"task_type", taskType,
		"primary_tier", primarySelection.Tier,
		"primary_confidence", primaryConf.Score,
		"comparison_tier", comparisonTier,
		"comparison_confidence", compConf.Score,
		"similarity", similarity,
		"agreement", agreement,
	)
}

// shouldShadow uses deterministic hash-based sampling.
func shouldShadow(workflowID string, rate float64) bool {
	h := fnv.New32a()
	h.Write([]byte(workflowID))
	return float64(h.Sum32()%1000)/1000.0 < rate
}

// getComparisonTier returns the tier to compare against.
func getComparisonTier(primaryTier string) string {
	switch primaryTier {
	case "economy", "cheap":
		return "premium"
	case "mid":
		return "premium"
	case "premium":
		return "cheap"
	default:
		return "premium"
	}
}

// computeResponseSimilarity computes a fast text similarity score
// using Jaccard similarity on word sets weighted with a length ratio.
func computeResponseSimilarity(a, b string) float64 {
	wordsA := tokenize(a)
	wordsB := tokenize(b)

	setA := make(map[string]bool, len(wordsA))
	for _, w := range wordsA {
		setA[w] = true
	}
	setB := make(map[string]bool, len(wordsB))
	for _, w := range wordsB {
		setB[w] = true
	}

	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 1.0
	}

	jaccard := float64(intersection) / float64(union)

	lenA, lenB := float64(len(a)), float64(len(b))
	if lenA == 0 && lenB == 0 {
		return 1.0
	}
	lengthRatio := math.Min(lenA, lenB) / math.Max(lenA, lenB)

	return jaccard*0.7 + lengthRatio*0.3
}

// tokenize splits text into lowercase word tokens.
func tokenize(text string) []string {
	lower := strings.ToLower(text)
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	result := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) > 0 {
			result = append(result, f)
		}
	}
	return result
}

// classifyTaskType wraps the eval package's classifier.
func classifyTaskType(prompt string) string {
	return eval.ClassifyTaskType(prompt)
}
