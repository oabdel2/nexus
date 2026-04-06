package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

// NexusError provides actionable error responses to API consumers.
type NexusError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion"`
	DocsURL    string `json:"docs_url"`
	RequestID  string `json:"request_id,omitempty"`
}

func (e *NexusError) Error() string {
	return e.Message
}

func writeNexusError(w http.ResponseWriter, ne *NexusError, status int) {
	if ne.RequestID == "" {
		ne.RequestID = generateRequestID()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ne)
}

func generateRequestID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "nex-" + hex.EncodeToString(b)
}

// Error catalog — pre-defined actionable errors.

func errProviderUnavailable() *NexusError {
	return &NexusError{
		Code:       "PROVIDER_UNAVAILABLE",
		Message:    "All providers are currently unavailable",
		Suggestion: "Check provider health at /health/ready. Circuit breaker may be open — see /api/circuit-breakers",
		DocsURL:    "https://nexus-gateway.dev/docs/errors#provider-unavailable",
	}
}

func errRateLimited(rpm int) *NexusError {
	msg := "Rate limit exceeded"
	if rpm > 0 {
		msg = "Rate limit exceeded (" + itoa(rpm) + " RPM)"
	}
	return &NexusError{
		Code:       "RATE_LIMITED",
		Message:    msg,
		Suggestion: "Wait and retry, or increase rate_limit.default_rpm in config",
		DocsURL:    "https://nexus-gateway.dev/docs/errors#rate-limited",
	}
}

func errBodyTooLarge() *NexusError {
	return &NexusError{
		Code:       "BODY_TOO_LARGE",
		Message:    "Request body exceeds 1MB limit",
		Suggestion: "Reduce prompt size or increase security.body_size_limit",
		DocsURL:    "https://nexus-gateway.dev/docs/errors#body-too-large",
	}
}

func errInvalidRequest(detail string) *NexusError {
	msg := "Invalid request"
	if detail != "" {
		msg = detail
	}
	return &NexusError{
		Code:       "INVALID_REQUEST",
		Message:    msg,
		Suggestion: "Include messages array per OpenAI chat completions spec",
		DocsURL:    "https://nexus-gateway.dev/docs/errors#invalid-request",
	}
}

func errPromptBlocked() *NexusError {
	return &NexusError{
		Code:       "PROMPT_BLOCKED",
		Message:    "Prompt rejected by security filter",
		Suggestion: "Review prompt for injection patterns. Contact admin if false positive.",
		DocsURL:    "https://nexus-gateway.dev/docs/errors#prompt-blocked",
	}
}

func errAuthFailed() *NexusError {
	return &NexusError{
		Code:       "AUTH_FAILED",
		Message:    "Invalid or expired API key",
		Suggestion: "Generate new key at /api/keys/generate or check subscription status",
		DocsURL:    "https://nexus-gateway.dev/docs/errors#auth-failed",
	}
}

func errQuotaExceeded() *NexusError {
	return &NexusError{
		Code:       "QUOTA_EXCEEDED",
		Message:    "Monthly quota exceeded",
		Suggestion: "Upgrade plan or wait for quota reset",
		DocsURL:    "https://nexus-gateway.dev/docs/errors#quota-exceeded",
	}
}

func errConfigError() *NexusError {
	return &NexusError{
		Code:       "CONFIG_ERROR",
		Message:    "Invalid configuration",
		Suggestion: "Run nexus validate to check config",
		DocsURL:    "https://nexus-gateway.dev/docs/errors#config-error",
	}
}

func errProviderError(detail string) *NexusError {
	msg := "Provider returned an error"
	if detail != "" {
		msg = "Provider error: " + detail
	}
	return &NexusError{
		Code:       "PROVIDER_ERROR",
		Message:    msg,
		Suggestion: "Check provider health at /health/ready. The request may be retried automatically.",
		DocsURL:    "https://nexus-gateway.dev/docs/errors#provider-error",
	}
}

func errMethodNotAllowed() *NexusError {
	return &NexusError{
		Code:       "INVALID_REQUEST",
		Message:    "Method not allowed",
		Suggestion: "Use POST for /v1/chat/completions",
		DocsURL:    "https://nexus-gateway.dev/docs/errors#invalid-request",
	}
}

func errServiceOverloaded() *NexusError {
	return &NexusError{
		Code:       "SERVICE_OVERLOADED",
		Message:    "Too many concurrent requests",
		Suggestion: "Retry with exponential backoff. Current limit: configured in server.max_concurrent",
		DocsURL:    "https://nexus-gateway.dev/docs/troubleshooting#overloaded",
	}
}

// itoa is a simple int-to-string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
