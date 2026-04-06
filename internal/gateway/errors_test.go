package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- NexusError Tests ---

func TestNexusError_ErrorMethod(t *testing.T) {
	e := &NexusError{Code: "TEST", Message: "test message"}
	if e.Error() != "test message" {
		t.Errorf("Error() = %q, want %q", e.Error(), "test message")
	}
}

func TestNexusError_ErrorCatalog(t *testing.T) {
	tests := []struct {
		name     string
		err      *NexusError
		wantCode string
	}{
		{"provider unavailable", errProviderUnavailable(), "PROVIDER_UNAVAILABLE"},
		{"rate limited zero rpm", errRateLimited(0), "RATE_LIMITED"},
		{"rate limited with rpm", errRateLimited(60), "RATE_LIMITED"},
		{"invalid request empty", errInvalidRequest(""), "INVALID_REQUEST"},
		{"invalid request detail", errInvalidRequest("bad json"), "INVALID_REQUEST"},
		{"prompt blocked", errPromptBlocked(), "PROMPT_BLOCKED"},
		{"provider error empty", errProviderError(""), "PROVIDER_ERROR"},
		{"provider error detail", errProviderError("timeout"), "PROVIDER_ERROR"},
		{"method not allowed", errMethodNotAllowed(), "INVALID_REQUEST"},
		{"service overloaded", errServiceOverloaded(), "SERVICE_OVERLOADED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", tt.err.Code, tt.wantCode)
			}
			if tt.err.Message == "" {
				t.Error("Message should not be empty")
			}
			if tt.err.Suggestion == "" {
				t.Error("Suggestion should not be empty")
			}
			if tt.err.DocsURL == "" {
				t.Error("DocsURL should not be empty")
			}
		})
	}
}

func TestErrRateLimited_IncludesRPM(t *testing.T) {
	e := errRateLimited(120)
	if !strings.Contains(e.Message, "120") {
		t.Errorf("expected RPM in message, got %q", e.Message)
	}
}

func TestErrInvalidRequest_DefaultMessage(t *testing.T) {
	e := errInvalidRequest("")
	if e.Message != "Invalid request" {
		t.Errorf("expected default message, got %q", e.Message)
	}
}

func TestErrInvalidRequest_CustomDetail(t *testing.T) {
	e := errInvalidRequest("Missing messages field")
	if e.Message != "Missing messages field" {
		t.Errorf("expected custom detail, got %q", e.Message)
	}
}

func TestErrProviderError_DefaultMessage(t *testing.T) {
	e := errProviderError("")
	if e.Message != "Provider returned an error" {
		t.Errorf("expected default message, got %q", e.Message)
	}
}

func TestErrProviderError_CustomDetail(t *testing.T) {
	e := errProviderError("connection refused")
	if !strings.Contains(e.Message, "connection refused") {
		t.Errorf("expected detail in message, got %q", e.Message)
	}
}

// --- writeNexusError Tests ---

func TestWriteNexusError_SetsContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	writeNexusError(rec, errProviderUnavailable(), 503)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
}

func TestWriteNexusError_SetsStatusCode(t *testing.T) {
	rec := httptest.NewRecorder()
	writeNexusError(rec, errProviderUnavailable(), 503)

	if rec.Code != 503 {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestWriteNexusError_IncludesRequestID(t *testing.T) {
	rec := httptest.NewRecorder()
	writeNexusError(rec, errRateLimited(60), 429)

	var resp NexusError
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp.RequestID == "" {
		t.Error("expected auto-generated request ID")
	}
	if !strings.HasPrefix(resp.RequestID, "nex-") {
		t.Errorf("expected nex- prefix, got %q", resp.RequestID)
	}
}

func TestWriteNexusError_PreservesExistingRequestID(t *testing.T) {
	rec := httptest.NewRecorder()
	ne := errPromptBlocked()
	ne.RequestID = "custom-req-id"
	writeNexusError(rec, ne, 400)

	var resp NexusError
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.RequestID != "custom-req-id" {
		t.Errorf("expected custom-req-id, got %q", resp.RequestID)
	}
}

func TestWriteNexusError_ValidJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeNexusError(rec, errProviderUnavailable(), 503)

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if _, ok := resp["code"]; !ok {
		t.Error("response should contain 'code' field")
	}
	if _, ok := resp["message"]; !ok {
		t.Error("response should contain 'message' field")
	}
}

// --- generateRequestID Tests ---

func TestGenerateRequestID_HasPrefix(t *testing.T) {
	id := generateRequestID()
	if !strings.HasPrefix(id, "nex-") {
		t.Errorf("expected nex- prefix, got %q", id)
	}
}

func TestGenerateRequestID_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateRequestID()
		if seen[id] {
			t.Fatalf("duplicate request ID: %s", id)
		}
		seen[id] = true
	}
}
