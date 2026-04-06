package router

import (
	"strings"
)

// SmartWeights controls the blending of classification signals.
type SmartWeights struct {
	TFIDF     float64 // default 0.50
	Keywords  float64 // default 0.25
	Structure float64 // default 0.15
	Length    float64 // default 0.10
}

// DefaultSmartWeights returns sensible defaults.
func DefaultSmartWeights() SmartWeights {
	return SmartWeights{
		TFIDF:     0.50,
		Keywords:  0.25,
		Structure: 0.15,
		Length:    0.10,
	}
}

// SmartClassifier combines TF-IDF, keyword, structural, and length signals.
type SmartClassifier struct {
	tfidf    *TFIDFClassifier
	keywords bool // whether to use keyword scores
	weights  SmartWeights
}

// NewSmartClassifier creates a hybrid classifier with TF-IDF + keyword fallback.
func NewSmartClassifier() *SmartClassifier {
	return &SmartClassifier{
		tfidf:    NewTFIDFClassifier(),
		keywords: true,
		weights:  DefaultSmartWeights(),
	}
}

// NewSmartClassifierWithWeights creates a hybrid classifier with custom weights.
func NewSmartClassifierWithWeights(w SmartWeights) *SmartClassifier {
	return &SmartClassifier{
		tfidf:    NewTFIDFClassifier(),
		keywords: true,
		weights:  w,
	}
}

// tierToScore maps tier names to a [0,1] complexity score.
var tierToScore = map[string]float64{
	"economy": 0.1,
	"cheap":   0.3,
	"mid":     0.6,
	"premium": 0.9,
}

// Classify produces a ComplexityScore by blending all signals.
func (sc *SmartClassifier) Classify(prompt, role string, stepRatio, budgetRatio float64, contextLen int) ComplexityScore {
	score := ComplexityScore{}

	// ── 1. TF-IDF signal ──
	var tfidfScore float64
	if sc.tfidf != nil && sc.tfidf.IsTrained() {
		tier, conf := sc.tfidf.Classify(prompt)
		base := tierToScore[tier]
		// Blend toward 0.5 based on confidence
		tfidfScore = base*conf + 0.5*(1.0-conf)
	} else {
		tfidfScore = 0.5
	}

	// ── 2. Keyword signal (same as ClassifyComplexity) ──
	keywordScore := keywordPromptScore(prompt)

	// ── 3. Structural signal ──
	structScore := structuralScore(prompt)

	// ── 4. Length signal ──
	promptLen := float64(len(prompt))
	maxLen := 2000.0
	lengthScore := clamp(promptLen/maxLen, 0.0, 1.0)

	// ── Blend into PromptScore ──
	w := sc.weights
	if sc.tfidf != nil && sc.tfidf.IsTrained() {
		score.PromptScore = clamp(
			tfidfScore*w.TFIDF+keywordScore*w.Keywords+structScore*w.Structure+lengthScore*w.Length,
			0.0, 1.0,
		)
	} else {
		// Fallback: keywords only (backward-compatible)
		score.PromptScore = keywordScore
	}

	score.LengthScore = lengthScore
	score.StructScore = structScore

	// ── Context, role, position, budget — same as original ──
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

// keywordPromptScore replicates the keyword logic from classifier.go.
func keywordPromptScore(prompt string) float64 {
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
		return (float64(highCount)*1.0 + float64(midCount)*0.5 + float64(lowCount)*0.0) / float64(total)
	}
	return 0.5
}

// structuralScore analyzes prompt structure beyond simple keywords.
func structuralScore(prompt string) float64 {
	s := 0.0
	indicators := 0.0

	// Code blocks → higher complexity
	if strings.Contains(prompt, "```") {
		s += 0.3
		indicators++
	}

	// Question count: multiple questions = multi-part
	qCount := float64(strings.Count(prompt, "?"))
	if qCount > 1 {
		s += clamp(qCount*0.1, 0.0, 0.3)
		indicators++
	}

	// Conditional language
	lower := strings.ToLower(prompt)
	conditionals := []string{"if ", "then ", "when ", "should ", "unless ", "otherwise "}
	condCount := 0
	for _, c := range conditionals {
		if strings.Contains(lower, c) {
			condCount++
		}
	}
	if condCount > 0 {
		s += clamp(float64(condCount)*0.08, 0.0, 0.25)
		indicators++
	}

	// Negation presence (constraints = harder)
	negations := []string{"don't", "dont", "never", "without", "no ", "not ", "can't", "cannot", "shouldn't"}
	negCount := 0
	for _, n := range negations {
		if strings.Contains(lower, n) {
			negCount++
		}
	}
	if negCount > 0 {
		s += clamp(float64(negCount)*0.06, 0.0, 0.2)
		indicators++
	}

	// List/enumeration
	listMarkers := 0
	listMarkers += strings.Count(prompt, "1.")
	listMarkers += strings.Count(prompt, "2.")
	listMarkers += strings.Count(prompt, "- ")
	listMarkers += strings.Count(prompt, "* ")
	if listMarkers > 1 {
		s += clamp(float64(listMarkers)*0.05, 0.0, 0.2)
		indicators++
	}

	// Technical jargon density — ratio of words not in common English
	words := strings.Fields(lower)
	if len(words) > 3 {
		uncommon := 0
		for _, w := range words {
			if !commonEnglishWords[w] && !stopWords[w] && len(w) > 3 {
				uncommon++
			}
		}
		jargonRatio := float64(uncommon) / float64(len(words))
		if jargonRatio > 0.3 {
			s += clamp(jargonRatio*0.3, 0.0, 0.25)
			indicators++
		}
	}

	// Semicolons and bullet points (from original)
	semicolons := float64(strings.Count(prompt, ";"))
	if semicolons > 0 {
		s += clamp(semicolons*0.05, 0.0, 0.1)
	}

	return clamp(s, 0.0, 1.0)
}

// commonEnglishWords is a small set of very common words to help measure jargon density.
var commonEnglishWords = map[string]bool{
	"write": true, "read": true, "create": true, "build": true, "make": true,
	"find": true, "look": true, "want": true, "need": true, "code": true,
	"file": true, "data": true, "type": true, "name": true, "list": true,
	"please": true, "change": true, "update": true, "move": true, "copy": true,
	"delete": true, "remove": true, "edit": true, "open": true, "close": true,
	"start": true, "stop": true, "test": true, "check": true, "user": true,
	"page": true, "text": true, "line": true, "word": true, "number": true,
	"function": true, "class": true, "method": true, "variable": true, "value": true,
	"string": true, "array": true, "object": true, "error": true, "message": true,
	"time": true, "date": true, "year": true, "month": true, "week": true,
	"simple": true, "basic": true, "small": true, "large": true, "fast": true,
	"easy": true, "hard": true, "high": true, "same": true, "different": true,
	"project": true, "system": true, "server": true, "client": true, "program": true,
	"here": true, "there": true, "what": true, "which": true, "this": true,
	"that": true, "these": true, "those": true, "with": true, "from": true,
}
