package provider

import (
	"encoding/json"
	"testing"
)

func TestStreamBuffer_OpenAIChunks(t *testing.T) {
	buf := NewStreamBuffer()

	// Simulate OpenAI SSE stream
	lines := []string{
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"},"usage":null]}`,
		`data: [DONE]`,
	}

	for _, line := range lines {
		buf.WriteChunk(line)
	}

	resp := buf.AssembleResponse()

	if resp.ID != "chatcmpl-1" {
		t.Errorf("id: got %q", resp.ID)
	}
	if resp.Model != "gpt-4" {
		t.Errorf("model: got %q", resp.Model)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices: got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "Hello world" {
		t.Errorf("content: got %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("role: got %q", resp.Choices[0].Message.Role)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason: got %q", resp.Choices[0].FinishReason)
	}
}

func TestStreamBuffer_AnthropicReEmittedChunks(t *testing.T) {
	// After Anthropic provider re-emits as OpenAI-compatible SSE,
	// the stream buffer should still work.
	buf := NewStreamBuffer()

	lines := []string{
		`data: {"id":"msg_123","object":"chat.completion.chunk","model":"claude-sonnet-4-20250514","choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":""}]}`,
		`data: {"id":"msg_123","object":"chat.completion.chunk","model":"claude-sonnet-4-20250514","choices":[{"index":0,"delta":{"content":" there"},"finish_reason":""}]}`,
		`data: {"id":"msg_123","object":"chat.completion.chunk","model":"claude-sonnet-4-20250514","choices":[{"index":0,"delta":{"content":""},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	}

	for _, line := range lines {
		buf.WriteChunk(line)
	}

	resp := buf.AssembleResponse()

	if resp.Choices[0].Message.Content != "Hi there" {
		t.Errorf("content: got %q", resp.Choices[0].Message.Content)
	}
	if resp.Model != "claude-sonnet-4-20250514" {
		t.Errorf("model: got %q", resp.Model)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason: got %q", resp.Choices[0].FinishReason)
	}
}

func TestStreamBuffer_EmptyStream(t *testing.T) {
	buf := NewStreamBuffer()

	resp := buf.AssembleResponse()

	if resp.Choices[0].Message.Content != "" {
		t.Errorf("content should be empty: got %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason should default to stop: got %q", resp.Choices[0].FinishReason)
	}

	// CacheableJSON should return false for empty
	_, ok := buf.CacheableJSON()
	if ok {
		t.Error("CacheableJSON should return false for empty stream")
	}
}

func TestStreamBuffer_UsageExtraction(t *testing.T) {
	buf := NewStreamBuffer()

	// Some providers send usage in the final chunk
	lines := []string{
		`data: {"id":"c1","model":"gpt-4","choices":[{"index":0,"delta":{"content":"test"},"finish_reason":null}]}`,
		`data: {"id":"c1","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":5,"total_tokens":25}}`,
		`data: [DONE]`,
	}

	for _, line := range lines {
		buf.WriteChunk(line)
	}

	resp := buf.AssembleResponse()

	if resp.Usage.PromptTokens != 20 {
		t.Errorf("prompt_tokens: got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("completion_tokens: got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 25 {
		t.Errorf("total_tokens: got %d", resp.Usage.TotalTokens)
	}
}

func TestStreamBuffer_CacheableJSON(t *testing.T) {
	buf := NewStreamBuffer()

	lines := []string{
		`data: {"id":"c1","model":"gpt-4","choices":[{"index":0,"delta":{"content":"cached content"},"finish_reason":null}]}`,
		`data: {"id":"c1","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	}

	for _, line := range lines {
		buf.WriteChunk(line)
	}

	data, ok := buf.CacheableJSON()
	if !ok {
		t.Fatal("CacheableJSON should return true for non-empty stream")
	}

	var resp ChatResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("CacheableJSON produced invalid JSON: %v", err)
	}

	if resp.Choices[0].Message.Content != "cached content" {
		t.Errorf("content: got %q", resp.Choices[0].Message.Content)
	}
	if resp.Object != "chat.completion" {
		t.Errorf("object: got %q", resp.Object)
	}
}

func TestStreamBuffer_NonDataLines(t *testing.T) {
	buf := NewStreamBuffer()

	// Non-data lines (event:, empty lines) should be captured but not parsed
	lines := []string{
		"event: message_start",
		`data: {"id":"c1","model":"gpt-4","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}`,
		"",
		"event: done",
		`data: [DONE]`,
	}

	for _, line := range lines {
		buf.WriteChunk(line)
	}

	resp := buf.AssembleResponse()
	if resp.Choices[0].Message.Content != "ok" {
		t.Errorf("content: got %q", resp.Choices[0].Message.Content)
	}

	if len(buf.chunks) != 5 {
		t.Errorf("should capture all lines including non-data: got %d", len(buf.chunks))
	}
}
