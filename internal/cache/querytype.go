package cache

import (
	"strings"
)

// QueryType represents the category of a prompt for adaptive thresholding.
type QueryType int

const (
	QueryTypeGeneral     QueryType = iota
	QueryTypeFactual               // "What is X", definitions
	QueryTypeHowTo                 // "How to do X", tutorials
	QueryTypeCode                  // Code generation, implementation
	QueryTypeDebug                 // Debugging, error fixing
	QueryTypeComparison            // "X vs Y", comparisons
	QueryTypeArchitecture          // Design, architecture decisions
)

// ClassifyQueryType determines the type of query for adaptive threshold selection.
func ClassifyQueryType(prompt string) QueryType {
	lower := strings.ToLower(prompt)

	// Comparison patterns (check first — they're distinctive)
	comparisonPatterns := []string{
		" vs ", " versus ", "compare ", "comparison ", "difference between",
		"differences between", "which is better", "pros and cons",
		" or ", "tradeoff", "trade-off", "advantages of",
	}
	for _, p := range comparisonPatterns {
		if strings.Contains(lower, p) {
			return QueryTypeComparison
		}
	}

	// Debug/error patterns
	debugPatterns := []string{
		"debug", "error", "bug", "fix", "issue", "problem",
		"crash", "exception", "traceback", "stack trace",
		"not working", "doesn't work", "broken", "failing",
		"segfault", "panic", "undefined", "null pointer",
		"memory leak", "race condition", "deadlock",
	}
	for _, p := range debugPatterns {
		if strings.Contains(lower, p) {
			return QueryTypeDebug
		}
	}

	// Architecture patterns
	archPatterns := []string{
		"architect", "design pattern", "system design", "scalab",
		"microservice", "monolith", "distributed",
		"infrastructure", "deployment strategy", "high availability",
		"fault toleran", "load balanc", "event driven",
		"cqrs", "event sourcing", "domain driven",
	}
	for _, p := range archPatterns {
		if strings.Contains(lower, p) {
			return QueryTypeArchitecture
		}
	}

	// Code generation patterns
	codePatterns := []string{
		"write a ", "implement ", "create a function",
		"write code", "code to ", "program that",
		"script to ", "class that ", "method that",
		"generate ", "build a ", "make a ",
		"function to ", "algorithm for ",
	}
	for _, p := range codePatterns {
		if strings.Contains(lower, p) {
			return QueryTypeCode
		}
	}

	// Factual/definitional patterns
	factualPatterns := []string{
		"what is ", "what are ", "define ", "definition of",
		"meaning of ", "explain what", "what does ",
		"who created", "when was ", "where is ",
		"how many ", "how much ",
	}
	for _, p := range factualPatterns {
		if strings.Contains(lower, p) {
			return QueryTypeFactual
		}
	}

	// How-to patterns
	howToPatterns := []string{
		"how to ", "how do ", "how can ",
		"tutorial", "guide to ", "steps to ",
		"best way to ", "best practice",
		"explain how", "show me how",
		"walk me through", "set up ", "setup ",
		"configure ", "install ",
	}
	for _, p := range howToPatterns {
		if strings.Contains(lower, p) {
			return QueryTypeHowTo
		}
	}

	return QueryTypeGeneral
}

// AdaptiveThreshold returns the optimal similarity threshold for a given query type.
func AdaptiveThreshold(qt QueryType, baseThreshold float64) float64 {
	adjustments := map[QueryType]float64{
		QueryTypeFactual:      0.10,
		QueryTypeComparison:   0.15,
		QueryTypeCode:         0.05,
		QueryTypeArchitecture: 0.02,
		QueryTypeGeneral:      0.0,
		QueryTypeHowTo:        0.0,
		QueryTypeDebug:        -0.05,
	}

	adj, ok := adjustments[qt]
	if !ok {
		return baseThreshold
	}

	threshold := baseThreshold + adj
	if threshold > 0.95 {
		threshold = 0.95
	}
	if threshold < 0.55 {
		threshold = 0.55
	}
	return threshold
}

// QueryTypeName returns a human-readable name for the query type.
func QueryTypeName(qt QueryType) string {
	names := map[QueryType]string{
		QueryTypeGeneral:      "general",
		QueryTypeFactual:      "factual",
		QueryTypeHowTo:        "how-to",
		QueryTypeCode:         "code",
		QueryTypeDebug:        "debug",
		QueryTypeComparison:   "comparison",
		QueryTypeArchitecture: "architecture",
	}
	if name, ok := names[qt]; ok {
		return name
	}
	return "unknown"
}
