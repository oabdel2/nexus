package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildAnthropicRequest_SystemExtraction(t *testing.T) {
	req := ChatRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 512,
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
	}

	ar := buildAnthropicRequest(req)

	if ar.System != "You are helpful." {
		t.Errorf("expected system = %q, got %q", "You are helpful.", ar.System)
	}
	if len(ar.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(ar.Messages))
	}
	if ar.Messages[0].Role != "user" || ar.Messages[0].Content != "Hello" {
		t.Errorf("unexpected message: %+v", ar.Messages[0])
	}
	if ar.Model != "claude-sonnet-4-20250514" {
		t.Errorf("model not preserved: %s", ar.Model)
	}
	if ar.MaxTokens != 512 {
		t.Errorf("max_tokens not preserved: %d", ar.MaxTokens)
	}
}

func TestBuildAnthropicRequest_NoSystem(t *testing.T) {
	req := ChatRequest{
		Model: "claude-haiku",
		Messages: []Message{
			{Role: "user", Content: "Hi"},
		},
	}

	ar := buildAnthropicRequest(req)

	if ar.System != "" {
		t.Errorf("expected empty system, got %q", ar.System)
	}
	if len(ar.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(ar.Messages))
	}
	// Default max_tokens
	if ar.MaxTokens != 1024 {
		t.Errorf("expected default max_tokens 1024, got %d", ar.MaxTokens)
	}
}

func TestBuildAnthropicRequest_MultipleSystemMessages(t *testing.T) {
	req := ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "system", Content: "Part A."},
			{Role: "system", Content: "Part B."},
			{Role: "user", Content: "Hello"},
		},
	}

	ar := buildAnthropicRequest(req)

	if ar.System != "Part A.\nPart B." {
		t.Errorf("expected concatenated system, got %q", ar.System)
	}
	if len(ar.Messages) != 1 {
		t.Fatalf("expected 1 non-system message, got %d", len(ar.Messages))
	}
}

func TestToOpenAIResponse(t *testing.T) {
	ar := &anthropicResponse{
		ID:    "msg_123",
		Type:  "message",
		Role:  "assistant",
		Model: "claude-sonnet-4-20250514",
		Content: []anthropicContentBlock{
			{Type: "text", Text: "Hello "},
			{Type: "text", Text: "world!"},
		},
		Usage: anthropicUsage{
			InputTokens:  10,
			OutputTokens: 5,
		},
		StopReason: "end_turn",
	}

	resp := toOpenAIResponse(ar)

	if resp.ID != "msg_123" {
		t.Errorf("id: got %q", resp.ID)
	}
	if resp.Model != "claude-sonnet-4-20250514" {
		t.Errorf("model: got %q", resp.Model)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices: got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "Hello world!" {
		t.Errorf("content: got %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("role: got %q", resp.Choices[0].Message.Role)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason: got %q", resp.Choices[0].FinishReason)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("prompt_tokens: got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("completion_tokens: got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("total_tokens: got %d", resp.Usage.TotalTokens)
	}
}

func TestMapStopReason(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"end_turn", "stop"},
		{"max_tokens", "length"},
		{"stop_sequence", "stop"},
		{"", "stop"},
		{"something_else", "something_else"},
	}
	for _, tt := range tests {
		if got := mapStopReason(tt.input); got != tt.expected {
			t.Errorf("mapStopReason(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestAnthropicProvider_Send(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key header: got %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("anthropic-version header: got %q", r.Header.Get("anthropic-version"))
		}
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path: got %q", r.URL.Path)
		}

		// Verify request body
		body, _ := io.ReadAll(r.Body)
		var ar anthropicRequest
		if err := json.Unmarshal(body, &ar); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if ar.System != "Be helpful" {
			t.Errorf("system: got %q", ar.System)
		}
		if len(ar.Messages) != 1 || ar.Messages[0].Role != "user" {
			t.Errorf("messages: got %+v", ar.Messages)
		}

		// Write Anthropic response
		resp := anthropicResponse{
			ID:    "msg_test",
			Type:  "message",
			Role:  "assistant",
			Model: "claude-sonnet-4-20250514",
			Content: []anthropicContentBlock{
				{Type: "text", Text: "Hi there!"},
			},
			Usage: anthropicUsage{
				InputTokens:  8,
				OutputTokens: 3,
			},
			StopReason: "end_turn",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewAnthropic("test", server.URL, "test-key")
	resp, err := p.Send(context.Background(), ChatRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 100,
		Messages: []Message{
			{Role: "system", Content: "Be helpful"},
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if resp.Choices[0].Message.Content != "Hi there!" {
		t.Errorf("content: got %q", resp.Choices[0].Message.Content)
	}
	if resp.Usage.PromptTokens != 8 {
		t.Errorf("prompt_tokens: got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 3 {
		t.Errorf("completion_tokens: got %d", resp.Usage.CompletionTokens)
	}
}

func TestAnthropicProvider_SendStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		events := []string{
			`event: message_start` + "\n" +
				`data: {"type":"message_start","message":{"id":"msg_s1","model":"claude-sonnet-4-20250514","usage":{"input_tokens":12}}}`,
			`event: content_block_start` + "\n" +
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta` + "\n" +
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			`event: content_block_delta` + "\n" +
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
			`event: content_block_stop` + "\n" +
				`data: {"type":"content_block_stop","index":0}`,
			`event: message_delta` + "\n" +
				`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
			`event: message_stop` + "\n" +
				`data: {"type":"message_stop"}`,
		}

		for _, e := range events {
			fmt.Fprintf(w, "%s\n\n", e)
			flusher.Flush()
		}
	}))
	defer server.Close()

	p := NewAnthropic("test", server.URL, "test-key")
	var buf strings.Builder
	usage, err := p.SendStream(context.Background(), ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: "Hi"},
		},
	}, &buf)
	if err != nil {
		t.Fatalf("SendStream: %v", err)
	}

	output := buf.String()

	// Should contain OpenAI-compatible SSE chunks
	if !strings.Contains(output, "data: ") {
		t.Error("output should contain SSE data lines")
	}
	if !strings.Contains(output, "[DONE]") {
		t.Error("output should contain [DONE]")
	}
	// Should contain the text content
	if !strings.Contains(output, "Hello") {
		t.Error("output should contain 'Hello'")
	}
	if !strings.Contains(output, " world") {
		t.Error("output should contain ' world'")
	}

	// Check usage mapping
	if usage.PromptTokens != 12 {
		t.Errorf("prompt_tokens: got %d", usage.PromptTokens)
	}
	if usage.CompletionTokens != 4 {
		t.Errorf("completion_tokens: got %d", usage.CompletionTokens)
	}
	if usage.TotalTokens != 16 {
		t.Errorf("total_tokens: got %d", usage.TotalTokens)
	}

	// Verify the emitted chunks are valid OpenAI JSON
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Errorf("invalid SSE chunk JSON: %v — %q", err, data)
		}
		if chunk["object"] != "chat.completion.chunk" {
			t.Errorf("chunk object: got %q", chunk["object"])
		}
	}
}

func TestAnthropicProvider_HealthCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key" {
			t.Error("missing api key in health check")
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	p := NewAnthropic("test", server.URL, "key")
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Errorf("HealthCheck should pass for non-5xx: %v", err)
	}
}

func TestAnthropicProvider_HealthCheck_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := NewAnthropic("test", server.URL, "key")
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Error("HealthCheck should fail for 500")
	}
}

func TestAnthropicProvider_Send_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid key"}`))
	}))
	defer server.Close()

	p := NewAnthropic("test", server.URL, "bad-key")
	_, err := p.Send(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-20250514",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status code: %v", err)
	}
}
