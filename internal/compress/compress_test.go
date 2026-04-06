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

// ==================== Boilerplate Removal Tests ====================

func TestBoilerplate_RemovePrefix(t *testing.T) {
	input := "Sure, I can help with that. Here is the answer."
	got := BoilerplateRemove(input)
	if strings.Contains(strings.ToLower(got), "sure, i can help") {
		t.Errorf("expected boilerplate prefix removed, got %q", got)
	}
	if !strings.Contains(got, "Here is the answer.") {
		t.Errorf("expected content preserved, got %q", got)
	}
}

func TestBoilerplate_RemoveSuffix(t *testing.T) {
	input := "The answer is 42. I hope this helps!"
	got := BoilerplateRemove(input)
	if strings.Contains(strings.ToLower(got), "i hope this helps") {
		t.Errorf("expected boilerplate suffix removed, got %q", got)
	}
	if !strings.Contains(got, "The answer is 42.") {
		t.Errorf("expected content preserved, got %q", got)
	}
}

func TestBoilerplate_NoChange(t *testing.T) {
	input := "The Kubernetes pod is crashing due to OOM."
	got := BoilerplateRemove(input)
	if got != input {
		t.Errorf("non-boilerplate text should be unchanged, got %q", got)
	}
}

// ==================== JSON Minification Tests ====================

func TestJSONMinify_ValidJSON(t *testing.T) {
	input := "```json\n{\n  \"name\": \"test\",\n  \"value\": 42\n}\n```"
	got := JSONMinify(input)
	if strings.Contains(got, "  ") {
		t.Errorf("expected JSON minified, got %q", got)
	}
	if !strings.Contains(got, `"name":"test"`) {
		t.Errorf("expected minified JSON content, got %q", got)
	}
}

func TestJSONMinify_InvalidJSON(t *testing.T) {
	input := "```json\nnot valid json\n```"
	got := JSONMinify(input)
	if got != input {
		t.Errorf("invalid JSON should be unchanged, got %q", got)
	}
}

func TestJSONMinify_NoJSONBlocks(t *testing.T) {
	input := "Regular text without JSON blocks."
	got := JSONMinify(input)
	if got != input {
		t.Errorf("text without JSON blocks should be unchanged")
	}
}

// ==================== Instruction Deduplication Tests ====================

func TestDedup_RemovesDuplicateInstructions(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "Remember to use TypeScript. Always use strict mode."},
		{Role: "user", Content: "Write a function. Remember to use TypeScript."},
	}
	got := DeduplicateInstructions(msgs)
	// The second "Remember to use TypeScript." should be removed
	count := strings.Count(strings.ToLower(got[0].Content+got[1].Content), "remember to use typescript")
	if count > 1 {
		t.Errorf("expected duplicate instruction removed, found %d occurrences", count)
	}
}

func TestDedup_PreservesNonInstructions(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "Hello world. How are you?"},
		{Role: "user", Content: "Hello world. What's the weather?"},
	}
	got := DeduplicateInstructions(msgs)
	// Non-instruction sentences should be preserved even if duplicated
	if !strings.Contains(got[0].Content, "Hello world") {
		t.Errorf("expected non-instruction preserved: %q", got[0].Content)
	}
	if !strings.Contains(got[1].Content, "Hello world") {
		t.Errorf("expected non-instruction preserved in second message: %q", got[1].Content)
	}
}

// ==================== Combined Strategy with New Strategies ====================

func TestCombined_BoilerplateInAssistantMessages(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are a Go expert."},
		{Role: "user", Content: "What is a goroutine?"},
		{Role: "assistant", Content: "Sure, I can help with that. A goroutine is a lightweight thread."},
		{Role: "user", Content: "Tell me more."},
	}
	c := New(DefaultConfig())
	compressed, result := c.CompressMessages(msgs)
	// Check that boilerplate was removed from assistant message
	for _, m := range compressed {
		if m.Role == "assistant" && strings.Contains(strings.ToLower(m.Content), "sure, i can help") {
			t.Error("expected boilerplate removed from assistant message")
		}
	}
	_ = result
}

// ==================== Property-Based Tests ====================

// TestCompress_PropertyNeverLonger verifies that for 100 diverse inputs,
// compressed output (via CompressMessages) is never longer than the input.
func TestCompress_PropertyNeverLonger(t *testing.T) {
	c := New(DefaultConfig())

	inputs := [][]Message{
		{{Role: "user", Content: "hi"}},
		{{Role: "user", Content: ""}},
		{{Role: "user", Content: "   \n\n\n  \t\t  "}},
		{{Role: "user", Content: strings.Repeat("hello world ", 500)}},
		{{Role: "system", Content: "You are helpful."}, {Role: "user", Content: "What is Go?"}},
		{{Role: "user", Content: "explain\n\n\n\n\nthis\n\n\n\nplease"}},
		{{Role: "user", Content: "```go\n// comment\nfunc main() {\n\t// another comment\n\tfmt.Println(\"hello\")\n}\n```"}},
		{{Role: "assistant", Content: "Sure, I'd be happy to help you with that. Here is the answer."}},
		{{Role: "user", Content: "{\"key\": \"value\",   \"nested\":   {\"a\":  1,  \"b\":  2}}"}},
	}

	// Generate more varied inputs
	for i := 0; i < 91; i++ {
		content := strings.Repeat("word ", i+1)
		if i%3 == 0 {
			content = "Please help me " + content + "\n\n\n" + strings.Repeat("debug ", i%10+1)
		}
		if i%5 == 0 {
			content = "```python\n# comment\nprint('hello')\n```\n" + content
		}
		inputs = append(inputs, []Message{{Role: "user", Content: content}})
	}

	for i, msgs := range inputs {
		originalLen := 0
		for _, m := range msgs {
			originalLen += len(m.Content)
		}

		compressed, _ := c.CompressMessages(msgs)
		compressedLen := 0
		for _, m := range compressed {
			compressedLen += len(m.Content)
		}

		if compressedLen > originalLen {
			t.Errorf("input %d: compressed (%d bytes) is longer than original (%d bytes)",
				i, compressedLen, originalLen)
		}
	}
}
