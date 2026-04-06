package compress

import (
	"strings"
	"testing"
)

// ==================== Property-Based Tests ====================

// Property: compressed output is never longer than input (token estimate).
func TestProperty_CompressedNeverLonger(t *testing.T) {
	inputs := [][]Message{
		{{Role: "user", Content: "hello"}},
		{{Role: "user", Content: strings.Repeat("word ", 200)}},
		{{Role: "user", Content: "```go\n// comment\nfmt.Println(\"hello\")\n```"}},
		{{Role: "user", Content: "   lots   of   spaces   and\n\n\n\nnewlines   "}},
		{{Role: "system", Content: "You are helpful."}, {Role: "user", Content: "Tell me a joke."}},
		makeMessages(30, "conversation"),
	}

	c := New(DefaultConfig())
	for i, msgs := range inputs {
		_, result := c.CompressMessages(msgs)
		if result.CompressedTokens > result.OriginalTokens {
			t.Errorf("case %d: compressed (%d) > original (%d) tokens",
				i, result.CompressedTokens, result.OriginalTokens)
		}
	}
}

// Property: compression is idempotent (compressing twice gives same result).
func TestProperty_CompressionIdempotent(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are a Go expert."},
		{Role: "user", Content: "Please   review   this:\n\n\n```go\n// comment\nfmt.Println(\"hello\")\n// another\n```\n\nThanks   a   lot.   "},
	}

	c := New(CompressorConfig{
		EnableWhitespace:   true,
		EnableCodeStrip:    true,
		EnableHistoryTrunc: false, // disable truncation for this test
	})

	compressed1, result1 := c.CompressMessages(msgs)
	compressed2, result2 := c.CompressMessages(compressed1)

	if len(compressed1) != len(compressed2) {
		t.Fatalf("idempotent: message count changed: %d → %d", len(compressed1), len(compressed2))
	}
	for i := range compressed1 {
		if compressed1[i].Content != compressed2[i].Content {
			t.Errorf("idempotent: message %d content differs after second compression:\n  first:  %q\n  second: %q",
				i, compressed1[i].Content, compressed2[i].Content)
		}
	}
	if result1.CompressedTokens != result2.CompressedTokens {
		t.Errorf("idempotent: token counts differ: %d vs %d",
			result1.CompressedTokens, result2.CompressedTokens)
	}
}

// Property: system messages are always preserved.
func TestProperty_SystemMessagesPreserved(t *testing.T) {
	systemContent := "You are a helpful Go coding assistant."
	msgs := []Message{
		{Role: "system", Content: systemContent},
	}
	for i := 0; i < 30; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs = append(msgs, Message{Role: role, Content: "message " + strings.Repeat("x", 50)})
	}

	c := New(CompressorConfig{
		EnableWhitespace:   true,
		EnableCodeStrip:    true,
		EnableHistoryTrunc: true,
		MaxHistoryTurns:    20,
		PreserveLastN:      5,
	})

	compressed, _ := c.CompressMessages(msgs)

	// The original system message must still be the first message
	if compressed[0].Role != "system" || compressed[0].Content != systemContent {
		t.Errorf("system message not preserved as first message: got role=%q content=%q",
			compressed[0].Role, compressed[0].Content)
	}
}

// Property: last N messages are always preserved when history truncation is enabled.
func TestProperty_LastNPreserved(t *testing.T) {
	preserveN := 4
	msgs := makeMessages(20, "topic")
	original := make([]Message, len(msgs))
	copy(original, msgs)

	truncated := HistoryTruncate(msgs, preserveN)

	// The last preserveN messages of the original should be the last preserveN of the truncated
	origTail := original[len(original)-preserveN:]
	truncTail := truncated[len(truncated)-preserveN:]

	for i := 0; i < preserveN; i++ {
		if origTail[i].Content != truncTail[i].Content {
			t.Errorf("last message %d not preserved: expected %q, got %q",
				i, origTail[i].Content, truncTail[i].Content)
		}
	}
}

// Property: empty messages are handled gracefully.
func TestProperty_EmptyMessagesGraceful(t *testing.T) {
	cases := [][]Message{
		nil,
		{},
		{{Role: "user", Content: ""}},
		{{Role: "system", Content: ""}, {Role: "user", Content: ""}},
		{{Role: "user", Content: "   "}},
	}

	c := New(DefaultConfig())
	for i, msgs := range cases {
		compressed, result := c.CompressMessages(msgs)
		// Should not panic
		_ = compressed
		if result.CompressedTokens < 0 {
			t.Errorf("case %d: negative compressed tokens: %d", i, result.CompressedTokens)
		}
	}
}

// Property: ratio is always between 0.0 and 1.0 for non-empty input.
func TestProperty_RatioBounded(t *testing.T) {
	inputs := [][]Message{
		{{Role: "user", Content: "hello world"}},
		{{Role: "user", Content: strings.Repeat("debug analyze optimize ", 100)}},
		makeMessages(15, "conversation"),
		{{Role: "user", Content: "```go\n// lots of comments\n// more comments\n// even more\nfmt.Println(\"x\")\n```"}},
	}

	c := New(DefaultConfig())
	for i, msgs := range inputs {
		_, result := c.CompressMessages(msgs)
		if result.OriginalTokens > 0 && (result.Ratio < 0.0 || result.Ratio > 1.0001) {
			t.Errorf("case %d: ratio out of bounds: %f", i, result.Ratio)
		}
	}
}

// Property: WhitespaceCompress never introduces new content.
func TestProperty_WhitespaceNoNewContent(t *testing.T) {
	inputs := []string{
		"hello   world",
		"line1\n\n\n\nline2",
		"\t\t  tabs  \t  and  spaces  ",
		"already clean",
	}
	for _, input := range inputs {
		result := WhitespaceCompress(input)
		// Every word in result should exist in input
		for _, word := range strings.Fields(result) {
			if !strings.Contains(input, word) {
				t.Errorf("WhitespaceCompress introduced new word %q from input %q", word, input)
			}
		}
	}
}

// Property: CodeBlockCompress preserves non-code text untouched.
func TestProperty_CodeBlockPreservesNonCode(t *testing.T) {
	nonCode := "This is regular text that has no code blocks."
	input := nonCode + "\n\n```go\n// comment\nfmt.Println(\"x\")\n```\n\n" + nonCode
	result := CodeBlockCompress(input)

	// The non-code text segments should appear unchanged
	if !strings.Contains(result, nonCode) {
		t.Errorf("non-code text was altered by CodeBlockCompress")
	}
}

// Property: HistoryTruncate with preserveLastN=0 still keeps system message.
func TestProperty_TruncateZeroPreserve(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
	}
	result := HistoryTruncate(msgs, 0)
	if len(result) == 0 {
		t.Fatal("result should not be empty")
	}
	// First should be original system
	if result[0].Role != "system" || result[0].Content != "sys" {
		t.Errorf("system message not preserved with preserveLastN=0")
	}
}

// Test large input doesn't cause issues.
func TestProperty_LargeInputStability(t *testing.T) {
	var msgs []Message
	msgs = append(msgs, Message{Role: "system", Content: "You are helpful."})
	for i := 0; i < 100; i++ {
		msgs = append(msgs, Message{
			Role:    "user",
			Content: strings.Repeat("important data point ", 50),
		})
		msgs = append(msgs, Message{
			Role:    "assistant",
			Content: strings.Repeat("response data ", 50),
		})
	}

	c := New(DefaultConfig())
	compressed, result := c.CompressMessages(msgs)
	if len(compressed) == 0 {
		t.Error("compression of large input returned empty")
	}
	if result.CompressedTokens > result.OriginalTokens {
		t.Errorf("compressed (%d) should not exceed original (%d)",
			result.CompressedTokens, result.OriginalTokens)
	}
}
