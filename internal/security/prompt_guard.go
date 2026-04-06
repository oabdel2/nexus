package security

import (
	"context"
	"net/http"
	"regexp"
	"strings"
)

// PromptGuard detects and blocks prompt injection attacks.
type PromptGuard struct {
	enabled         bool
	mode            string // "block" or "sanitize"
	maxPromptLength int
	patterns        []*regexp.Regexp
	blockedPhrases  []string
}

// PromptGuardConfig configures prompt injection protection.
type PromptGuardConfig struct {
	Enabled         bool     `yaml:"enabled"`
	Mode            string   `yaml:"mode"`
	MaxPromptLength int      `yaml:"max_prompt_length"`
	CustomPatterns  []string `yaml:"custom_patterns"`
	CustomPhrases   []string `yaml:"custom_phrases"`
}

// NewPromptGuard creates a new prompt injection detector.
func NewPromptGuard(cfg PromptGuardConfig) *PromptGuard {
	if cfg.MaxPromptLength <= 0 {
		cfg.MaxPromptLength = 32000
	}
	if cfg.Mode == "" {
		cfg.Mode = "block"
	}

	// Built-in injection patterns
	builtinPatterns := []string{
		`(?i)ignore\s+(all\s+)?previous\s+(instructions?|prompts?|rules?)`,
		`(?i)disregard\s+(all\s+)?(your\s+)?previous\s+(instructions?|prompts?)`,
		`(?i)forget\s+(all\s+)?(your\s+)?previous\s+(instructions?|context)`,
		`(?i)you\s+are\s+now\s+(a|an|the)\s+`,
		`(?i)new\s+instruction[s]?\s*:`,
		`(?i)system\s*:\s*you\s+are`,
		`(?i)override\s+(your\s+)?(system|safety|content)\s+(prompt|filter|policy)`,
		`(?i)bypass\s+(your\s+)?(safety|content|security)\s+(filter|policy|restriction)`,
		`(?i)act\s+as\s+if\s+you\s+have\s+no\s+(restrictions?|limitations?|rules?)`,
		`(?i)pretend\s+(you\s+are|to\s+be)\s+an?\s+(unrestricted|unfiltered|uncensored)`,
		`(?i)jailbreak`,
		`(?i)(reveal|show|print|output|display)\s+(your\s+)?(system\s+prompt|instructions?|rules?)`,
		`(?i)what\s+(are|is)\s+your\s+(system\s+)?(prompt|instructions?|rules?)`,
		`(?i)<\s*\/?script\s*>`,
		`(?i)\{\{.*\}\}`,
		`(?i)\$\{.*\}`,
	}

	// Compile patterns
	var compiled []*regexp.Regexp
	for _, p := range builtinPatterns {
		if re, err := regexp.Compile(p); err == nil {
			compiled = append(compiled, re)
		}
	}
	for _, p := range cfg.CustomPatterns {
		if re, err := regexp.Compile(p); err == nil {
			compiled = append(compiled, re)
		}
	}

	// Built-in blocked phrases
	blockedPhrases := []string{
		"ignore previous instructions",
		"disregard your instructions",
		"override safety",
		"bypass content filter",
		"you are now DAN",
		"developer mode enabled",
		"sudo mode",
		"admin override",
	}
	blockedPhrases = append(blockedPhrases, cfg.CustomPhrases...)

	return &PromptGuard{
		enabled:         cfg.Enabled,
		mode:            cfg.Mode,
		maxPromptLength: cfg.MaxPromptLength,
		patterns:        compiled,
		blockedPhrases:  blockedPhrases,
	}
}

// CheckResult contains the result of a prompt injection check.
type CheckResult struct {
	Blocked   bool     `json:"blocked"`
	Threats   []string `json:"threats,omitempty"`
	Sanitized string   `json:"sanitized,omitempty"`
	RiskScore float64  `json:"risk_score"`
}

// Check analyzes a prompt for injection attempts.
func (pg *PromptGuard) Check(prompt string) CheckResult {
	if !pg.enabled {
		return CheckResult{Blocked: false, RiskScore: 0}
	}

	result := CheckResult{}
	lower := strings.ToLower(prompt)

	// Check length
	if len(prompt) > pg.maxPromptLength {
		result.Threats = append(result.Threats, "prompt_too_long")
		result.RiskScore = 1.0
		result.Blocked = true
		return result
	}

	// Check regex patterns
	for _, re := range pg.patterns {
		if re.MatchString(prompt) {
			patStr := re.String()
			if len(patStr) > 50 {
				patStr = patStr[:50]
			}
			result.Threats = append(result.Threats, "pattern_match: "+patStr)
		}
	}

	// Check blocked phrases
	for _, phrase := range pg.blockedPhrases {
		if strings.Contains(lower, strings.ToLower(phrase)) {
			result.Threats = append(result.Threats, "blocked_phrase: "+phrase)
		}
	}

	// Calculate risk score
	threatCount := len(result.Threats)
	if threatCount > 0 {
		score := float64(threatCount) * 0.3
		if score > 1.0 {
			score = 1.0
		}
		result.RiskScore = score
	}

	// Block or sanitize based on mode
	if threatCount > 0 {
		if pg.mode == "block" {
			result.Blocked = true
		} else {
			// Sanitize mode: strip detected patterns
			sanitized := prompt
			for _, re := range pg.patterns {
				sanitized = re.ReplaceAllString(sanitized, "[REDACTED]")
			}
			result.Sanitized = sanitized
		}
	}

	return result
}

// Middleware returns an HTTP middleware that checks prompts for injection.
func (pg *PromptGuard) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only check POST requests to chat endpoints
			if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/chat/completions") {
				next.ServeHTTP(w, r)
				return
			}
			// The actual prompt check happens in handleChat after parsing the body.
			// This middleware sets a flag to enable checking.
			ctx := context.WithValue(r.Context(), contextKey("prompt_guard"), pg)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetPromptGuard retrieves the PromptGuard from request context.
func GetPromptGuard(ctx context.Context) *PromptGuard {
	pg, _ := ctx.Value(contextKey("prompt_guard")).(*PromptGuard)
	return pg
}

// PatternCount returns the total number of compiled injection patterns (built-in + custom).
func (pg *PromptGuard) PatternCount() int {
	return len(pg.patterns)
}
