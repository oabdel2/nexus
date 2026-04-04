package router

import (
	"strings"
)

var highComplexityKeywords = []string{
	"analyze", "debug", "fix", "refactor", "optimize", "architect",
	"security", "vulnerability", "race condition", "deadlock",
	"concurrent", "distributed", "algorithm", "prove", "derive",
	"implement", "design pattern", "trade-off", "critical",
	"production", "migrate", "performance",
	"memory leak", "scaling", "sharding", "consensus",
	"encryption", "authentication", "zero-day", "exploit",
	"backward compatible", "fault tolerant", "load balancing",
	"thread safe", "mutex", "semaphore",
}

var mediumComplexityKeywords = []string{
	"explain", "compare", "review", "test", "write",
	"create", "build", "integrate", "configure", "setup",
}

var lowComplexityKeywords = []string{
	"summarize", "list", "format", "convert", "translate",
	"log", "print", "echo", "hello", "greet", "thank",
	"commit message", "rename", "typo", "comment",
	"readme", "docs", "documentation",
	"status", "version", "help", "ping", "date",
	"time", "count", "length", "size", "color",
	"label", "tag", "note", "todo",
}

var roleWeights = map[string]float64{
	"engineer":          0.85,
	"developer":         0.80,
	"architect":         0.90,
	"reviewer":          0.70,
	"analyst":           0.75,
	"tester":            0.60,
	"writer":            0.40,
	"logger":            0.15,
	"summarizer":        0.25,
	"formatter":         0.20,
	"security-engineer": 0.90,
	"planner":           0.65,
	"calculator":        0.15,
	"assistant":         0.50,
	"coordinator":       0.60,
	"debugger":          0.85,
	"ops":               0.55,
	"qa":                0.65,
}

type ComplexityScore struct {
	PromptScore   float64 `json:"prompt_score"`
	ContextScore  float64 `json:"context_score"`
	RoleScore     float64 `json:"role_score"`
	PositionScore float64 `json:"position_score"`
	BudgetScore   float64 `json:"budget_score"`
	LengthScore   float64 `json:"length_score"`
	StructScore   float64 `json:"struct_score"`
	FinalScore    float64 `json:"final_score"`
}

func ClassifyComplexity(prompt string, role string, stepRatio float64, budgetRatio float64, contextLen int) ComplexityScore {
	score := ComplexityScore{}

	promptLower := strings.ToLower(prompt)
	highCount := 0
	midCount := 0
	lowCount := 0
	for _, kw := range highComplexityKeywords {
		if strings.Contains(promptLower, kw) {
			highCount++
		}
	}
	for _, kw := range mediumComplexityKeywords {
		if strings.Contains(promptLower, kw) {
			midCount++
		}
	}
	for _, kw := range lowComplexityKeywords {
		if strings.Contains(promptLower, kw) {
			lowCount++
		}
	}
	total := highCount + midCount + lowCount
	if total > 0 {
		// High keywords push toward 1.0, mid toward 0.5, low toward 0.0
		score.PromptScore = (float64(highCount)*1.0 + float64(midCount)*0.5 + float64(lowCount)*0.0) / float64(total)
	} else {
		score.PromptScore = 0.5
	}

	// Prompt length factor: longer prompts tend to be more complex
	promptLen := float64(len(prompt))
	maxLen := 2000.0
	score.LengthScore = clamp(promptLen/maxLen, 0.0, 1.0)

	// Sentence complexity: question marks, semicolons, bullet points indicate multi-part questions
	questions := float64(strings.Count(prompt, "?"))
	semicolons := float64(strings.Count(prompt, ";"))
	bullets := float64(strings.Count(prompt, "- ")) + float64(strings.Count(prompt, "* "))
	structIndicators := questions + semicolons + bullets
	score.StructScore = clamp(structIndicators/10.0, 0.0, 1.0)

	maxCtx := 4096.0
	score.ContextScore = clamp(float64(contextLen)/maxCtx, 0.0, 1.0)

	roleLower := strings.ToLower(role)
	if w, ok := roleWeights[roleLower]; ok {
		score.RoleScore = w
	} else {
		score.RoleScore = 0.5
	}

	if stepRatio < 0.3 {
		score.PositionScore = 0.7
	} else if stepRatio > 0.8 {
		score.PositionScore = 0.3
	} else {
		score.PositionScore = 0.5
	}

	score.BudgetScore = 1.0 - budgetRatio

	return score
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
