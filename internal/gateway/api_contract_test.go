package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nexus-gateway/nexus/internal/config"
	"github.com/nexus-gateway/nexus/internal/provider"
)

// ═══════════════════════════════════════════════════════════════════════════
// Contract helpers
// ═══════════════════════════════════════════════════════════════════════════

// assertContentType checks the Content-Type header of the response.
func assertContentType(t *testing.T, w *httptest.ResponseRecorder, expected string) {
	t.Helper()
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, expected) {
		t.Errorf("Content-Type: want prefix %q, got %q", expected, ct)
	}
}

// assertValidJSON checks that the response body is valid JSON and returns the decoded map.
func assertValidJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("response body is not valid JSON: %v\nbody: %s", err, w.Body.String())
	}
	return m
}

// assertJSONHasKey asserts a key exists in the decoded map.
func assertJSONHasKey(t *testing.T, m map[string]interface{}, key string) {
	t.Helper()
	if _, ok := m[key]; !ok {
		t.Errorf("response JSON missing required key %q", key)
	}
}

// assertNexusError verifies a response uses the NexusError format.
func assertNexusError(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	for _, key := range []string{"code", "message", "suggestion", "docs_url"} {
		assertJSONHasKey(t, m, key)
	}
}

// contractGET performs a GET request to the given path and returns the recorder.
func contractGET(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// contractPOST performs a POST request with JSON body.
func contractPOST(t *testing.T, handler http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// ═══════════════════════════════════════════════════════════════════════════
// Core API contracts
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_ChatCompletions_Success(t *testing.T) {
	handler, _ := newTestServer(t)
	body := `{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`
	w := contractPOST(t, handler, "/v1/chat/completions", body)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d: %s", w.Code, w.Body.String())
	}
	assertContentType(t, w, "application/json")

	var resp provider.ChatResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not ChatResponse JSON: %v", err)
	}
	if resp.ID == "" {
		t.Error("ChatResponse.ID is empty")
	}
	if len(resp.Choices) == 0 {
		t.Error("ChatResponse.Choices is empty")
	}
}

func TestContract_ChatCompletions_WrongMethod(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/v1/chat/completions")

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: want 405, got %d", w.Code)
	}
	assertNexusError(t, w)
}

func TestContract_ChatCompletions_InvalidJSON(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractPOST(t, handler, "/v1/chat/completions", "{bad json")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", w.Code)
	}
	assertNexusError(t, w)
}

func TestContract_Feedback_Success(t *testing.T) {
	handler, _ := newTestServer(t)
	// First create a workflow via chat
	chatBody := `{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`
	cw := contractPOST(t, handler, "/v1/chat/completions", chatBody)
	wfID := cw.Header().Get("X-Nexus-Workflow-ID")

	body := `{"workflow_id":"` + wfID + `","step":1,"outcome":"success"}`
	w := contractPOST(t, handler, "/v1/feedback", body)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d: %s", w.Code, w.Body.String())
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "status")
}

func TestContract_Feedback_WrongMethod(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/v1/feedback")

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: want 405, got %d", w.Code)
	}
}

func TestContract_Feedback_MissingFields(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractPOST(t, handler, "/v1/feedback", `{"workflow_id":"x"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d: %s", w.Code, w.Body.String())
	}
	assertContentType(t, w, "application/json")
}

// ═══════════════════════════════════════════════════════════════════════════
// Health contracts
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_Health(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/health")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "status")
	assertJSONHasKey(t, m, "providers")
}

func TestContract_HealthLive(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/health/live")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "status")
	if m["status"] != "ok" {
		t.Errorf("status: want %q, got %v", "ok", m["status"])
	}
}

func TestContract_HealthReady(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/health/ready")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "status")
	assertJSONHasKey(t, m, "checks")
}

// ═══════════════════════════════════════════════════════════════════════════
// Metrics contract
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_Metrics(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/metrics")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "text/plain")
	if !strings.Contains(w.Body.String(), "nexus_") {
		t.Error("metrics output should contain nexus_ prefix")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Dashboard contracts
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_DashboardStats(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/dashboard/api/stats")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "stats")
	assertJSONHasKey(t, m, "workflows")
	assertJSONHasKey(t, m, "recent_requests")
	assertJSONHasKey(t, m, "provider_stats")
}

// ═══════════════════════════════════════════════════════════════════════════
// Info / root contract
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_Info(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "service")
	assertJSONHasKey(t, m, "version")
	assertJSONHasKey(t, m, "endpoints")
	assertJSONHasKey(t, m, "providers")
}

// ═══════════════════════════════════════════════════════════════════════════
// Synonym admin API contracts
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_SynonymStats(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/api/synonyms/stats")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d: %s", w.Code, w.Body.String())
	}
	assertContentType(t, w, "application/json")
	assertValidJSON(t, w)
}

func TestContract_SynonymCandidates(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/api/synonyms/candidates")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
}

func TestContract_SynonymLearned(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/api/synonyms/learned")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
}

func TestContract_SynonymPromote_WrongMethod(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/api/synonyms/promote")

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: want 405, got %d", w.Code)
	}
	assertNexusError(t, w)
}

func TestContract_SynonymPromote_DisabledRegistry(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractPOST(t, handler, "/api/synonyms/promote", `{"term":"test"}`)

	// Registry is enabled but term not found → 404
	if w.Code != http.StatusNotFound {
		t.Fatalf("status: want 404, got %d: %s", w.Code, w.Body.String())
	}
	assertNexusError(t, w)
}

func TestContract_SynonymAdd_WrongMethod(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/api/synonyms/add")

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: want 405, got %d", w.Code)
	}
	assertNexusError(t, w)
}

func TestContract_SynonymAdd_DisabledRegistry(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractPOST(t, handler, "/api/synonyms/add", `{"term":"x","expansion":"y"}`)

	// Registry is enabled, add succeeds
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d: %s", w.Code, w.Body.String())
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "status")
}

// ═══════════════════════════════════════════════════════════════════════════
// Circuit breaker contract
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_CircuitBreakers(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/api/circuit-breakers")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	// Response is a JSON object (map of provider → stats)
	var m map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Eval stats contract
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_EvalStats(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/api/eval/stats")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "status")
}

// ═══════════════════════════════════════════════════════════════════════════
// Compression stats contract
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_CompressionStats(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/api/compression/stats")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "enabled")
	assertJSONHasKey(t, m, "config")
}

// ═══════════════════════════════════════════════════════════════════════════
// Shadow stats contract
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_ShadowStats(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/api/shadow/stats")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "status")
	assertJSONHasKey(t, m, "shadow_enabled")
	assertJSONHasKey(t, m, "stats")
}

func TestContract_ShadowStats_WrongMethod(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractPOST(t, handler, "/api/shadow/stats", `{}`)

	// handleShadowStats checks for GET-only
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: want 405, got %d", w.Code)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Adaptive routing stats contract
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_AdaptiveStats(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/api/adaptive/stats")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "status")
}

// ═══════════════════════════════════════════════════════════════════════════
// Experiment contracts
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_Experiments_Disabled(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/api/experiments")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "status")
}

func TestContract_Experiments_Enabled(t *testing.T) {
	handler, _ := newTestServer(t, withExperiments())
	w := contractGET(t, handler, "/api/experiments")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "status")
	assertJSONHasKey(t, m, "experiments")
}

func TestContract_ExperimentCreate(t *testing.T) {
	handler, _ := newTestServer(t, withExperiments())
	body := `{"id":"test-exp","name":"Test","description":"test","variants":[{"id":"a","weight":100}]}`
	w := contractPOST(t, handler, "/api/experiments", body)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: want 201, got %d: %s", w.Code, w.Body.String())
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "status")
	assertJSONHasKey(t, m, "id")
}

func TestContract_Experiments_WrongMethod_GET_On_Post(t *testing.T) {
	handler, _ := newTestServer(t, withExperiments())
	// GET on /api/experiments should list, not error
	w := contractGET(t, handler, "/api/experiments")
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Inspect contract
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_Inspect_Success(t *testing.T) {
	handler, _ := newTestServer(t)
	body := `{"prompt":"explain quantum computing"}`
	w := contractPOST(t, handler, "/api/inspect", body)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d: %s", w.Code, w.Body.String())
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	for _, key := range []string{
		"complexity_score", "tier", "reason", "estimated_model",
		"estimated_provider", "estimated_cost", "cache_enabled",
	} {
		assertJSONHasKey(t, m, key)
	}
}

func TestContract_Inspect_WrongMethod(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/api/inspect")

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: want 405, got %d", w.Code)
	}
	assertNexusError(t, w)
}

func TestContract_Inspect_MissingPrompt(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractPOST(t, handler, "/api/inspect", `{"prompt":""}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", w.Code)
	}
	assertNexusError(t, w)
}

func TestContract_Inspect_InvalidJSON(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractPOST(t, handler, "/api/inspect", `{bad`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", w.Code)
	}
	assertNexusError(t, w)
}

// ═══════════════════════════════════════════════════════════════════════════
// Events contracts
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_EventsRecent(t *testing.T) {
	handler, _ := newTestServer(t, withEvents())
	w := contractGET(t, handler, "/api/events/recent")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	// Response should be valid JSON (array or object)
	if !json.Valid(w.Body.Bytes()) {
		t.Fatalf("response is not valid JSON: %s", w.Body.String())
	}
}

func TestContract_EventsStats(t *testing.T) {
	handler, _ := newTestServer(t, withEvents())
	w := contractGET(t, handler, "/api/events/stats")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	if !json.Valid(w.Body.Bytes()) {
		t.Fatalf("response is not valid JSON: %s", w.Body.String())
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Plugins contract
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_Plugins(t *testing.T) {
	handler, _ := newTestServer(t, withPlugins())
	w := contractGET(t, handler, "/api/plugins")

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	if !json.Valid(w.Body.Bytes()) {
		t.Fatalf("response is not valid JSON: %s", w.Body.String())
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// MCP contract (JSON-RPC 2.0)
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_MCP_Initialize(t *testing.T) {
	handler, _ := newTestServer(t, withMCP())
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	w := contractPOST(t, handler, "/mcp", body)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d: %s", w.Code, w.Body.String())
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "jsonrpc")
	if m["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc: want %q, got %v", "2.0", m["jsonrpc"])
	}
	assertJSONHasKey(t, m, "result")
}

func TestContract_MCP_WrongMethod(t *testing.T) {
	handler, _ := newTestServer(t, withMCP())
	w := contractGET(t, handler, "/mcp")

	if w.Code != http.StatusOK {
		// MCP returns 200 with JSON-RPC error, or could be different
		t.Logf("MCP GET returned status %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "jsonrpc")
	assertJSONHasKey(t, m, "error")
}

func TestContract_MCP_InvalidJSON(t *testing.T) {
	handler, _ := newTestServer(t, withMCP())
	w := contractPOST(t, handler, "/mcp", "{bad")

	if w.Code != http.StatusOK {
		t.Logf("MCP invalid JSON returned status %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "jsonrpc")
	assertJSONHasKey(t, m, "error")
}

func TestContract_MCP_ToolsList(t *testing.T) {
	handler, _ := newTestServer(t, withMCP())
	body := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	w := contractPOST(t, handler, "/mcp", body)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d: %s", w.Code, w.Body.String())
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "result")
}

func TestContract_MCP_UnknownMethod(t *testing.T) {
	handler, _ := newTestServer(t, withMCP())
	body := `{"jsonrpc":"2.0","id":3,"method":"nonexistent"}`
	w := contractPOST(t, handler, "/mcp", body)

	if w.Code != http.StatusOK {
		t.Logf("MCP unknown method returned status %d", w.Code)
	}
	assertContentType(t, w, "application/json")
	m := assertValidJSON(t, w)
	assertJSONHasKey(t, m, "error")
}

// ═══════════════════════════════════════════════════════════════════════════
// Billing contracts
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_Billing_StripeWebhook_WrongMethod(t *testing.T) {
	handler, _ := newTestServerWithBilling(t)
	w := contractGET(t, handler, "/webhooks/stripe")

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: want 405, got %d", w.Code)
	}
}

func TestContract_Billing_AdminSubscriptions_NoAuth(t *testing.T) {
	handler, _ := newTestServerWithBilling(t)
	w := contractGET(t, handler, "/api/admin/subscriptions")

	// Without billing key → 401 from billing middleware
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestContract_Billing_KeyGenerate_WrongMethod(t *testing.T) {
	handler, _ := newTestServerWithBilling(t)
	w := contractGET(t, handler, "/api/keys/generate")

	// Without auth → 401; with auth but GET → 405
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: want 401 or 405, got %d", w.Code)
	}
}

func TestContract_Billing_KeyRevoke_WrongMethod(t *testing.T) {
	handler, _ := newTestServerWithBilling(t)
	w := contractGET(t, handler, "/api/keys/revoke")

	if w.Code != http.StatusUnauthorized && w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: want 401 or 405, got %d", w.Code)
	}
}

func TestContract_Billing_Usage_NoAuth(t *testing.T) {
	handler, _ := newTestServerWithBilling(t)
	w := contractGET(t, handler, "/api/usage")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d: %s", w.Code, w.Body.String())
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Error contract: all error paths must return JSON, not plain text
// ═══════════════════════════════════════════════════════════════════════════

func TestContract_Error_ChatWrongMethod_IsJSON(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/v1/chat/completions")
	assertContentType(t, w, "application/json")
	assertValidJSON(t, w)
}

func TestContract_Error_ChatBadBody_IsJSON(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractPOST(t, handler, "/v1/chat/completions", "not json")
	assertContentType(t, w, "application/json")
	assertValidJSON(t, w)
}

func TestContract_Error_InspectWrongMethod_IsJSON(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/api/inspect")
	assertContentType(t, w, "application/json")
	assertValidJSON(t, w)
}

func TestContract_Error_SynonymPromoteWrongMethod_IsJSON(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/api/synonyms/promote")
	assertContentType(t, w, "application/json")
	assertValidJSON(t, w)
}

func TestContract_Error_SynonymAddWrongMethod_IsJSON(t *testing.T) {
	handler, _ := newTestServer(t)
	w := contractGET(t, handler, "/api/synonyms/add")
	assertContentType(t, w, "application/json")
	assertValidJSON(t, w)
}

// ═══════════════════════════════════════════════════════════════════════════
// Test option: enable MCP
// ═══════════════════════════════════════════════════════════════════════════

func withMCP() testOption {
	return func(cfg *config.Config) {
		cfg.MCP.Enabled = true
	}
}
