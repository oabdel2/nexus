package gateway

import (
	"testing"

	"github.com/nexus-gateway/nexus/internal/compress"
	"github.com/nexus-gateway/nexus/internal/provider"
)

// --- extractPromptText Tests ---

func TestExtractPromptText_LastUserMessage(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "You are a helper."},
		{Role: "user", Content: "first question"},
		{Role: "assistant", Content: "first answer"},
		{Role: "user", Content: "second question"},
	}
	got := extractPromptText(msgs)
	if got != "second question" {
		t.Errorf("expected last user message, got %q", got)
	}
}

func TestExtractPromptText_NoUserMessage(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "assistant", Content: "assistant message"},
	}
	got := extractPromptText(msgs)
	if got != "system prompt\nassistant message" {
		t.Errorf("expected concatenation when no user message, got %q", got)
	}
}

func TestExtractPromptText_EmptyMessages(t *testing.T) {
	got := extractPromptText(nil)
	if got != "" {
		t.Errorf("expected empty string for nil messages, got %q", got)
	}
}

func TestExtractPromptText_SingleUserMessage(t *testing.T) {
	msgs := []provider.Message{
		{Role: "user", Content: "hello"},
	}
	got := extractPromptText(msgs)
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestExtractPromptText_EmptyUserContentSkipped(t *testing.T) {
	msgs := []provider.Message{
		{Role: "user", Content: "real question"},
		{Role: "user", Content: ""},
	}
	got := extractPromptText(msgs)
	if got != "real question" {
		t.Errorf("expected non-empty user message, got %q", got)
	}
}

// --- fullPromptText Tests ---

func TestFullPromptText_ConcatenatesAll(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "usr"},
		{Role: "assistant", Content: "ast"},
	}
	got := fullPromptText(msgs)
	if got != "sys\nusr\nast" {
		t.Errorf("expected full concatenation, got %q", got)
	}
}

func TestFullPromptText_Empty(t *testing.T) {
	got := fullPromptText(nil)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestFullPromptText_SingleMessage(t *testing.T) {
	msgs := []provider.Message{
		{Role: "user", Content: "only message"},
	}
	got := fullPromptText(msgs)
	if got != "only message" {
		t.Errorf("expected 'only message', got %q", got)
	}
}

// --- Message conversion Tests ---

func TestProviderToCompressMessages(t *testing.T) {
	input := []provider.Message{
		{Role: "system", Content: "sys prompt"},
		{Role: "user", Content: "user msg"},
	}
	result := providerToCompressMessages(input)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Role != "system" || result[0].Content != "sys prompt" {
		t.Errorf("first message mismatch: %+v", result[0])
	}
	if result[1].Role != "user" || result[1].Content != "user msg" {
		t.Errorf("second message mismatch: %+v", result[1])
	}
}

func TestCompressToProviderMessages(t *testing.T) {
	input := []compress.Message{
		{Role: "assistant", Content: "response"},
	}
	result := compressToProviderMessages(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Role != "assistant" || result[0].Content != "response" {
		t.Errorf("message mismatch: %+v", result[0])
	}
}

func TestProviderToCompressMessages_Empty(t *testing.T) {
	result := providerToCompressMessages(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 messages, got %d", len(result))
	}
}

func TestCompressToProviderMessages_Empty(t *testing.T) {
	result := compressToProviderMessages(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 messages, got %d", len(result))
	}
}

func TestMessageRoundTrip(t *testing.T) {
	original := []provider.Message{
		{Role: "system", Content: "Be helpful."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}
	compressed := providerToCompressMessages(original)
	restored := compressToProviderMessages(compressed)

	if len(restored) != len(original) {
		t.Fatalf("round-trip length mismatch: %d vs %d", len(restored), len(original))
	}
	for i := range original {
		if restored[i].Role != original[i].Role || restored[i].Content != original[i].Content {
			t.Errorf("round-trip mismatch at %d: %+v vs %+v", i, restored[i], original[i])
		}
	}
}
