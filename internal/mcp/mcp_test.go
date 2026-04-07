package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer() *Server {
	logger := slog.Default()
	s := NewServer(logger)

	s.RegisterTool(Tool{
		Name:        "nexus_chat",
		Description: "Send a chat completion through Nexus",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{"type": "string", "description": "The prompt to send"},
				"model":  map[string]interface{}{"type": "string", "description": "Target model (optional)"},
			},
			"required": []string{"prompt"},
		},
		Handler: func(params map[string]interface{}) (interface{}, error) {
			prompt, _ := params["prompt"].(string)
			if prompt == "" {
				return nil, fmt.Errorf("prompt is required")
			}
			return map[string]interface{}{
				"response": "test response for: " + prompt,
				"model":    "gpt-4o-mini",
				"tokens":   42,
			}, nil
		},
	})

	s.RegisterTool(Tool{
		Name:        "nexus_inspect",
		Description: "Analyze how Nexus would route a prompt",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{"type": "string", "description": "The prompt to analyze"},
				"role":   map[string]interface{}{"type": "string", "description": "Agent role (optional)"},
			},
			"required": []string{"prompt"},
		},
		Handler: func(params map[string]interface{}) (interface{}, error) {
			prompt, _ := params["prompt"].(string)
			if prompt == "" {
				return nil, fmt.Errorf("prompt is required")
			}
			return map[string]interface{}{
				"complexity_score": 0.45,
				"tier":            "simple",
				"model":           "gpt-4o-mini",
				"provider":        "openai",
			}, nil
		},
	})

	s.RegisterTool(Tool{
		Name:        "nexus_stats",
		Description: "Get gateway stats",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: func(params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{
				"requests_total": 1500,
				"cache_hits":     300,
				"cache_misses":   1200,
			}, nil
		},
	})

	s.RegisterTool(Tool{
		Name:        "nexus_health",
		Description: "Check provider health and circuit breaker status",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: func(params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{
				"status": "ok",
				"providers": map[string]interface{}{
					"openai": map[string]interface{}{"healthy": true},
				},
			}, nil
		},
	})

	s.RegisterTool(Tool{
		Name:        "nexus_experiments",
		Description: "List active A/B experiments and their results",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: func(params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{
				"experiments": []interface{}{},
			}, nil
		},
	})

	s.RegisterTool(Tool{
		Name:        "nexus_confidence",
		Description: "Query the confidence map for a task type",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task_type": map[string]interface{}{"type": "string", "description": "Task type to look up"},
				"tier":      map[string]interface{}{"type": "string", "description": "Model tier"},
			},
			"required": []string{"task_type", "tier"},
		},
		Handler: func(params map[string]interface{}) (interface{}, error) {
			taskType, _ := params["task_type"].(string)
			tier, _ := params["tier"].(string)
			if taskType == "" || tier == "" {
				return nil, fmt.Errorf("task_type and tier are required")
			}
			return map[string]interface{}{
				"task_type":          taskType,
				"tier":              tier,
				"average_confidence": 0.85,
				"sample_count":      120,
				"found":             true,
			}, nil
		},
	})

	s.RegisterTool(Tool{
		Name:        "nexus_providers",
		Description: "List available providers and their status",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: func(params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{
				"providers": []interface{}{
					map[string]interface{}{"name": "openai", "healthy": true},
					map[string]interface{}{"name": "anthropic", "healthy": true},
				},
			}, nil
		},
	})

	return s
}

func rpcCall(t *testing.T, s *Server, method string, params interface{}) Response {
	t.Helper()
	req := Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	rr := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	s.HandleHTTP(rr, httpReq)

	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func TestInitialize(t *testing.T) {
	s := newTestServer()
	resp := rpcCall(t, s, "initialize", nil)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("result should be a map")
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("expected protocolVersion 2024-11-05, got %v", result["protocolVersion"])
	}
	serverInfo, ok := result["serverInfo"].(map[string]interface{})
	if !ok {
		t.Fatal("serverInfo should be a map")
	}
	if serverInfo["name"] != "nexus-mcp" {
		t.Errorf("expected server name nexus-mcp, got %v", serverInfo["name"])
	}
	caps, ok := result["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatal("capabilities should be a map")
	}
	if _, ok := caps["tools"]; !ok {
		t.Error("capabilities should include tools")
	}
}

func TestToolsList(t *testing.T) {
	s := newTestServer()
	resp := rpcCall(t, s, "tools/list", nil)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("result should be a map")
	}
	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Fatal("tools should be an array")
	}
	if len(tools) != 7 {
		t.Errorf("expected 7 tools, got %d", len(tools))
	}

	// Verify tool names (sorted)
	expected := []string{
		"nexus_chat", "nexus_confidence", "nexus_experiments",
		"nexus_health", "nexus_inspect", "nexus_providers", "nexus_stats",
	}
	for i, tool := range tools {
		toolMap := tool.(map[string]interface{})
		if toolMap["name"] != expected[i] {
			t.Errorf("tool[%d]: expected %s, got %s", i, expected[i], toolMap["name"])
		}
		if toolMap["description"] == nil || toolMap["description"] == "" {
			t.Errorf("tool %s should have a description", expected[i])
		}
		if toolMap["inputSchema"] == nil {
			t.Errorf("tool %s should have an inputSchema", expected[i])
		}
	}
}

func TestToolsCallChat(t *testing.T) {
	s := newTestServer()
	resp := rpcCall(t, s, "tools/call", map[string]interface{}{
		"name":      "nexus_chat",
		"arguments": map[string]interface{}{"prompt": "hello world"},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("result should be a map")
	}
	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatal("result should have content array")
	}
	entry := content[0].(map[string]interface{})
	text, _ := entry["text"].(string)
	if !strings.Contains(text, "hello world") {
		t.Errorf("expected response to contain 'hello world', got %s", text)
	}
}

func TestToolsCallChatMissingPrompt(t *testing.T) {
	s := newTestServer()
	resp := rpcCall(t, s, "tools/call", map[string]interface{}{
		"name":      "nexus_chat",
		"arguments": map[string]interface{}{},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %v", resp.Error)
	}
	result := resp.Result.(map[string]interface{})
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Error("expected isError to be true for missing prompt")
	}
}

func TestToolsCallInspect(t *testing.T) {
	s := newTestServer()
	resp := rpcCall(t, s, "tools/call", map[string]interface{}{
		"name":      "nexus_inspect",
		"arguments": map[string]interface{}{"prompt": "write a go function"},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result := resp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "complexity_score") {
		t.Errorf("expected inspect result to contain complexity_score, got %s", text)
	}
}

func TestToolsCallStats(t *testing.T) {
	s := newTestServer()
	resp := rpcCall(t, s, "tools/call", map[string]interface{}{
		"name":      "nexus_stats",
		"arguments": map[string]interface{}{},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result := resp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "requests_total") {
		t.Errorf("expected stats result to contain requests_total, got %s", text)
	}
}

func TestToolsCallHealth(t *testing.T) {
	s := newTestServer()
	resp := rpcCall(t, s, "tools/call", map[string]interface{}{
		"name":      "nexus_health",
		"arguments": map[string]interface{}{},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result := resp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "ok") {
		t.Errorf("expected health result to contain 'ok', got %s", text)
	}
}

func TestToolsCallExperiments(t *testing.T) {
	s := newTestServer()
	resp := rpcCall(t, s, "tools/call", map[string]interface{}{
		"name":      "nexus_experiments",
		"arguments": map[string]interface{}{},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result := resp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "experiments") {
		t.Errorf("expected experiments result, got %s", text)
	}
}

func TestToolsCallConfidence(t *testing.T) {
	s := newTestServer()
	resp := rpcCall(t, s, "tools/call", map[string]interface{}{
		"name": "nexus_confidence",
		"arguments": map[string]interface{}{
			"task_type": "coding",
			"tier":      "simple",
		},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result := resp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "average_confidence") {
		t.Errorf("expected confidence result, got %s", text)
	}
}

func TestToolsCallConfidenceMissingParams(t *testing.T) {
	s := newTestServer()
	resp := rpcCall(t, s, "tools/call", map[string]interface{}{
		"name":      "nexus_confidence",
		"arguments": map[string]interface{}{},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %v", resp.Error)
	}
	result := resp.Result.(map[string]interface{})
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Error("expected isError to be true for missing params")
	}
}

func TestToolsCallProviders(t *testing.T) {
	s := newTestServer()
	resp := rpcCall(t, s, "tools/call", map[string]interface{}{
		"name":      "nexus_providers",
		"arguments": map[string]interface{}{},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result := resp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "openai") {
		t.Errorf("expected providers result to contain 'openai', got %s", text)
	}
}

func TestInvalidMethod(t *testing.T) {
	s := newTestServer()
	resp := rpcCall(t, s, "nonexistent/method", nil)

	if resp.Error == nil {
		t.Fatal("expected error for invalid method")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("expected code %d, got %d", CodeMethodNotFound, resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "nonexistent/method") {
		t.Errorf("error message should mention the method, got %s", resp.Error.Message)
	}
}

func TestInvalidParams(t *testing.T) {
	s := newTestServer()

	// tools/call with string params instead of object
	resp := rpcCall(t, s, "tools/call", "not an object")
	if resp.Error == nil {
		t.Fatal("expected error for invalid params")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("expected code %d, got %d", CodeInvalidParams, resp.Error.Code)
	}
}

func TestInvalidParamsMissingName(t *testing.T) {
	s := newTestServer()

	resp := rpcCall(t, s, "tools/call", map[string]interface{}{
		"arguments": map[string]interface{}{},
	})
	if resp.Error == nil {
		t.Fatal("expected error for missing tool name")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("expected code %d, got %d", CodeInvalidParams, resp.Error.Code)
	}
}

func TestToolNotFound(t *testing.T) {
	s := newTestServer()

	resp := rpcCall(t, s, "tools/call", map[string]interface{}{
		"name": "nonexistent_tool",
	})
	if resp.Error == nil {
		t.Fatal("expected error for nonexistent tool")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("expected code %d, got %d", CodeMethodNotFound, resp.Error.Code)
	}
}

func TestInvalidJSON(t *testing.T) {
	s := newTestServer()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{invalid"))
	s.HandleHTTP(rr, req)

	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if resp.Error.Code != CodeParseError {
		t.Errorf("expected code %d, got %d", CodeParseError, resp.Error.Code)
	}
}

func TestInvalidJSONRPCVersion(t *testing.T) {
	s := newTestServer()
	rr := httptest.NewRecorder()
	body := `{"jsonrpc":"1.0","id":1,"method":"initialize"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	s.HandleHTTP(rr, req)

	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for wrong jsonrpc version")
	}
	if resp.Error.Code != CodeInvalidRequest {
		t.Errorf("expected code %d, got %d", CodeInvalidRequest, resp.Error.Code)
	}
}

func TestGetMethodRejected(t *testing.T) {
	s := newTestServer()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	s.HandleHTTP(rr, req)

	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for GET request")
	}
	if resp.Error.Code != CodeInvalidRequest {
		t.Errorf("expected code %d, got %d", CodeInvalidRequest, resp.Error.Code)
	}
}

func TestToolsCallWithNilArguments(t *testing.T) {
	s := newTestServer()
	resp := rpcCall(t, s, "tools/call", map[string]interface{}{
		"name": "nexus_stats",
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result := resp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "requests_total") {
		t.Errorf("expected stats result even without arguments, got %s", text)
	}
}

func TestResponseIDMatchesRequestID(t *testing.T) {
	s := newTestServer()
	for _, id := range []interface{}{float64(42), "req-abc", nil} {
		req := Request{JSONRPC: "2.0", ID: id, Method: "initialize"}
		body, _ := json.Marshal(req)
		rr := httptest.NewRecorder()
		httpReq := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
		s.HandleHTTP(rr, httpReq)

		var resp Response
		json.NewDecoder(rr.Body).Decode(&resp)
		// JSON numbers decode as float64
		if fmt.Sprintf("%v", resp.ID) != fmt.Sprintf("%v", id) {
			t.Errorf("response ID %v should match request ID %v", resp.ID, id)
		}
	}
}
