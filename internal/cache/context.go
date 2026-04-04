package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ContextFingerprint generates a fingerprint from conversation context.
// This allows the same prompt to have different cache entries when asked
// in different conversation contexts.
type ContextFingerprint struct {
	MaxTurns int
}

// NewContextFingerprint creates a new context fingerprint generator.
func NewContextFingerprint(maxTurns int) *ContextFingerprint {
	if maxTurns <= 0 {
		maxTurns = 3
	}
	return &ContextFingerprint{MaxTurns: maxTurns}
}

// Fingerprint generates a context hash from conversation history.
// Messages should be in chronological order (oldest first).
// Only the last MaxTurns messages are considered (excluding the current query).
func (cf *ContextFingerprint) Fingerprint(messages []ChatMessage) string {
	if len(messages) <= 1 {
		return ""
	}

	contextMsgs := messages[:len(messages)-1]
	start := 0
	if len(contextMsgs) > cf.MaxTurns {
		start = len(contextMsgs) - cf.MaxTurns
	}
	contextMsgs = contextMsgs[start:]

	var parts []string
	for _, msg := range contextMsgs {
		terms := extractKeyTerms(msg.Content)
		parts = append(parts, msg.Role+":"+strings.Join(terms, ","))
	}

	contextStr := strings.Join(parts, "|")
	hash := sha256.Sum256([]byte(contextStr))
	return hex.EncodeToString(hash[:8])
}

// ChatMessage represents a message in a conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// extractKeyTerms pulls the most important terms from a message for context fingerprinting.
func extractKeyTerms(text string) []string {
	words := tokenizeWords(text)

	actionVerbs := map[string]bool{
		"create": true, "delete": true, "update": true, "build": true,
		"deploy": true, "test": true, "debug": true, "optimize": true,
		"explain": true, "compare": true, "implement": true, "design": true,
		"migrate": true, "configure": true, "setup": true, "install": true,
		"analyze": true, "review": true, "fix": true, "refactor": true,
	}

	importantWords := make(map[string]bool)
	var terms []string
	for _, w := range words {
		if len(w) < 3 {
			continue
		}
		if actionVerbs[w] || isKeyNoun(w) {
			if !importantWords[w] {
				importantWords[w] = true
				terms = append(terms, w)
			}
		}
	}

	if len(terms) > 10 {
		terms = terms[:10]
	}
	return terms
}

// isKeyNoun checks if a word is in the key nouns set.
func isKeyNoun(word string) bool {
	return getKeyNouns()[word]
}

// ContextAwareLookupKey combines prompt with context fingerprint for cache lookup.
func ContextAwareLookupKey(prompt string, contextFP string) string {
	if contextFP == "" {
		return prompt
	}
	return "[ctx:" + contextFP + "] " + prompt
}
