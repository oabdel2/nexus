package compress

import (
	"strings"
	"testing"
)

// ==================== Whitespace Tests ====================

func TestWhitespace_MultipleNewlines(t *testing.T) {
	input := "hello\n\n\n\n\nworld"
	got := WhitespaceCompress(input)
	if strings.Count(got, "\n") > 2 {
		t.Errorf("expected at most 2 newlines, got %q", got)
	}
}

func TestWhitespace_TrailingSpaces(t *testing.T) {
	input := "hello   \nworld  \t\n"
	got := WhitespaceCompress(input)
	for i, line := range strings.Split(got, "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Errorf("line %d still has trailing whitespace: %q", i, line)
		}
	}
}

func TestWhitespace_TabCollapse(t *testing.T) {
	input := "hello\t\t\tworld"
	got := WhitespaceCompress(input)
	if strings.Contains(got, "\t\t") {
		t.Errorf("expected tabs collapsed, got %q", got)
	}
}

func TestWhitespace_MultipleSpaces(t *testing.T) {
	input := "hello     world   foo"
	got := WhitespaceCompress(input)
	if strings.Contains(got, "  ") {
		t.Errorf("expected multiple spaces collapsed, got %q", got)
	}
}

func TestWhitespace_AlreadyClean(t *testing.T) {
	input := "already clean text\nwith one newline"
	got := WhitespaceCompress(input)
	if got != input {
		t.Errorf("clean text should be unchanged, got %q", got)
	}
}

// ==================== Code Block Tests ====================

func TestCode_GoCommentRemoval(t *testing.T) {
	input := "```go\n// this is a comment\nfmt.Println(\"hello\")\n// another comment\n```"
	got := CodeBlockCompress(input)
	if strings.Contains(got, "// this is a comment") {
		t.Errorf("expected Go comments removed, got %q", got)
	}
	if !strings.Contains(got, "fmt.Println") {
		t.Errorf("expected code preserved, got %q", got)
	}
}

func TestCode_PythonCommentRemoval(t *testing.T) {
	input := "```python\n# this is a comment\nprint('hello')\n# another\n```"
	got := CodeBlockCompress(input)
	if strings.Contains(got, "# this is a comment") {
		t.Errorf("expected Python comments removed, got %q", got)
	}
	if !strings.Contains(got, "print('hello')") {
		t.Errorf("expected code preserved, got %q", got)
	}
}

func TestCode_BlockCommentRemoval(t *testing.T) {
	input := "```go\n/* block\ncomment */\nfmt.Println(\"hello\")\n```"
	got := CodeBlockCompress(input)
	if strings.Contains(got, "block") && strings.Contains(got, "comment */") {
		t.Errorf("expected block comment removed, got %q", got)
	}
}

func TestCode_BlankLineRemoval(t *testing.T) {
	input := "```go\nfmt.Println(\"a\")\n\n\nfmt.Println(\"b\")\n```"
	got := CodeBlockCompress(input)
	if strings.Contains(got, "\n\n") {
		t.Errorf("expected blank lines removed in code, got %q", got)
	}
}

func TestCode_ImportCollapse(t *testing.T) {
	input := "```go\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"strings\"\n)\n```"
	got := CodeBlockCompress(input)
	if strings.Contains(got, "\t\"fmt\"\n\t\"os\"") {
		t.Errorf("expected import block collapsed, got %q", got)
	}
}

func TestCode_NoCodeBlocks(t *testing.T) {
	input := "This is plain text with no code blocks whatsoever."
	got := CodeBlockCompress(input)
	if got != input {
		t.Errorf("text without code blocks should be unchanged")
	}
}

// ==================== History Truncation Tests ====================

func makeMessages(n int, prefix string) []Message {
	msgs := []Message{{Role: "system", Content: "You are a helpful assistant."}}
	for i := 1; i <= n; i++ {
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		msgs = append(msgs, Message{Role: role, Content: prefix + " message " + strings.Repeat("x", i)})
	}
	return msgs
}

func TestHistory_TruncateTo5(t *testing.T) {
	msgs := makeMessages(20, "test")
	got := HistoryTruncate(msgs, 5)
	// system + summary + 5 last = 7
	if len(got) != 7 {
		t.Errorf("expected 7 messages, got %d", len(got))
	}
}

func TestHistory_SystemPreserved(t *testing.T) {
	msgs := makeMessages(20, "test")
	got := HistoryTruncate(msgs, 5)
	if got[0].Role != "system" || got[0].Content != "You are a helpful assistant." {
		t.Errorf("system message not preserved: %+v", got[0])
	}
}

func TestHistory_SingleMessage(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hello"}}
	got := HistoryTruncate(msgs, 5)
	if len(got) != 1 {
		t.Errorf("single message should be unchanged, got %d messages", len(got))
	}
}

func TestHistory_SummaryContent(t *testing.T) {
	msgs := makeMessages(20, "coding")
	got := HistoryTruncate(msgs, 3)
	// The summary message should mention message count
	found := false
	for _, m := range got {
		if strings.Contains(m.Content, "Previous context:") {
			found = true
			if !strings.Contains(m.Content, "messages about") {
				t.Errorf("summary should mention topics, got %q", m.Content)
			}
		}
	}
	if !found {
		t.Error("expected a summary message in output")
	}
}

func TestHistory_ExactlyPreserveN(t *testing.T) {
	msgs := makeMessages(5, "test")
	got := HistoryTruncate(msgs, 5)
	// system + 5 = 6 messages; preserveLastN=5 so no truncation needed
	if len(got) != 6 {
		t.Errorf("expected 6 messages (no truncation), got %d", len(got))
	}
}

// ==================== Combined Strategy Tests ====================

func TestCombined_RealisticCodingPrompt(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are a Go expert."},
		{Role: "user", Content: "Please review this code:\n\n```go\npackage main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n// Main function\nfunc main() {\n\t// Print hello\n\tfmt.Println(\"hello\")\n\n\n\tos.Exit(0)\n}\n```\n\nLet me know   if there are   any issues.   "},
	}
	c := New(DefaultConfig())
	compressed, result := c.CompressMessages(msgs)
	if result.Ratio >= 1.0 {
		t.Errorf("expected compression ratio < 1.0, got %f", result.Ratio)
	}
	if len(result.StrategiesUsed) == 0 {
		t.Error("expected at least one strategy used")
	}
	// Code should still be present
	if !strings.Contains(compressed[1].Content, "fmt.Println") {
		t.Error("code content should be preserved")
	}
}

func TestCombined_SimpleQuestion(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "What is Go?"},
	}
	c := New(DefaultConfig())
	_, result := c.CompressMessages(msgs)
	// Simple question should have ratio ~1.0 (minimal compression)
	if result.Ratio < 0.5 {
		t.Errorf("simple question shouldn't be heavily compressed, ratio=%f", result.Ratio)
	}
}

func TestCombined_LongConversation(t *testing.T) {
	msgs := makeMessages(15, "conversation topic about coding")
	c := New(CompressorConfig{
		EnableWhitespace:   true,
		EnableCodeStrip:    true,
		EnableHistoryTrunc: true,
		MaxHistoryTurns:    20,
		PreserveLastN:      3,
	})
	compressed, result := c.CompressMessages(msgs)
	if len(compressed) > 6 {
		t.Errorf("expected truncated conversation, got %d messages", len(compressed))
	}
	if result.Ratio >= 1.0 {
		t.Errorf("expected compression, got ratio=%f", result.Ratio)
	}
	if !contains(result.StrategiesUsed, "history_truncate") {
		t.Error("expected history_truncate strategy")
	}
}

// ==================== Edge Case Tests ====================

func TestEdge_EmptyInput(t *testing.T) {
	c := New(DefaultConfig())
	compressed, result := c.CompressMessages(nil)
	if len(compressed) != 0 {
		t.Error("expected empty result for nil input")
	}
	if result.OriginalTokens != 0 {
		t.Error("expected 0 original tokens for empty input")
	}
}

func TestEdge_EmptyMessageContent(t *testing.T) {
	msgs := []Message{{Role: "user", Content: ""}}
	c := New(DefaultConfig())
	compressed, result := c.CompressMessages(msgs)
	if len(compressed) != 1 {
		t.Errorf("expected 1 message, got %d", len(compressed))
	}
	if result.OriginalTokens != 0 {
		t.Error("expected 0 tokens for empty content")
	}
}

func TestEdge_WhitespaceOnlyContent(t *testing.T) {
	got := WhitespaceCompress("   \t  \n\n\n   \t  ")
	got = strings.TrimSpace(got)
	if len(got) > 0 {
		t.Errorf("whitespace-only content should collapse to empty or near-empty, got %q", got)
	}
}

func TestEstimateTokens(t *testing.T) {
	if estimateTokens("") != 0 {
		t.Error("empty string should be 0 tokens")
	}
	// "ab" → len=2, 2/4=0, should return 1 minimum
	if estimateTokens("ab") != 1 {
		t.Errorf("short string should be at least 1 token, got %d", estimateTokens("ab"))
	}
	// "hello world" → len=11, 11/4 = 2
	if estimateTokens("hello world") < 1 {
		t.Error("expected at least 1 token for 'hello world'")
	}
}
