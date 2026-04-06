package gateway

import (
	"testing"
)

func TestShouldShadow_Deterministic(t *testing.T) {
	// Same input should always return the same result.
	wf := "workflow-abc-123"
	rate := 0.50
	first := shouldShadow(wf, rate)
	for i := 0; i < 100; i++ {
		if shouldShadow(wf, rate) != first {
			t.Fatal("shouldShadow is not deterministic")
		}
	}
}

func TestShouldShadow_RespectsRate(t *testing.T) {
	// With rate 0.0, no workflow should be shadowed.
	for i := 0; i < 100; i++ {
		if shouldShadow("wf-"+string(rune('A'+i%26)), 0.0) {
			t.Fatal("shouldShadow returned true at rate 0.0")
		}
	}

	// With rate 1.0, all workflows should be shadowed.
	for i := 0; i < 100; i++ {
		if !shouldShadow("wf-"+string(rune('A'+i%26)), 1.0) {
			t.Fatal("shouldShadow returned false at rate 1.0")
		}
	}
}

func TestShouldShadow_ApproximateRate(t *testing.T) {
	rate := 0.30
	sampled := 0
	total := 10000
	for i := 0; i < total; i++ {
		id := "workflow-" + itoa(i)
		if shouldShadow(id, rate) {
			sampled++
		}
	}
	actual := float64(sampled) / float64(total)
	if actual < 0.20 || actual > 0.40 {
		t.Fatalf("expected ~30%% sample rate, got %.2f%%", actual*100)
	}
}

func TestGetComparisonTier(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"economy", "premium"},
		{"cheap", "premium"},
		{"mid", "premium"},
		{"premium", "cheap"},
		{"unknown", "premium"},
		{"", "premium"},
	}
	for _, tt := range tests {
		got := getComparisonTier(tt.input)
		if got != tt.expected {
			t.Errorf("getComparisonTier(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestComputeResponseSimilarity_Identical(t *testing.T) {
	s := computeResponseSimilarity("hello world", "hello world")
	if s != 1.0 {
		t.Errorf("identical strings should have similarity 1.0, got %f", s)
	}
}

func TestComputeResponseSimilarity_BothEmpty(t *testing.T) {
	s := computeResponseSimilarity("", "")
	if s != 1.0 {
		t.Errorf("both empty should return 1.0, got %f", s)
	}
}

func TestComputeResponseSimilarity_CompletelyDifferent(t *testing.T) {
	s := computeResponseSimilarity("alpha beta gamma", "delta epsilon zeta")
	if s > 0.35 {
		t.Errorf("completely different words should have low similarity, got %f", s)
	}
}

func TestComputeResponseSimilarity_Similar(t *testing.T) {
	a := "The quick brown fox jumps over the lazy dog"
	b := "The fast brown fox leaps over the lazy dog"
	s := computeResponseSimilarity(a, b)
	if s < 0.50 || s > 0.95 {
		t.Errorf("similar sentences should have moderate similarity, got %f", s)
	}
}

func TestComputeResponseSimilarity_LengthRatio(t *testing.T) {
	short := "hello"
	long := "hello world this is a much longer sentence with many extra words"
	s := computeResponseSimilarity(short, long)
	if s > 0.60 {
		t.Errorf("very different lengths should penalize similarity, got %f", s)
	}
}

func TestTokenize_Basic(t *testing.T) {
	tokens := tokenize("Hello, World! Testing 123.")
	expected := map[string]bool{"hello": true, "world": true, "testing": true, "123": true}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(tokens), tokens)
	}
	for _, tok := range tokens {
		if !expected[tok] {
			t.Errorf("unexpected token: %q", tok)
		}
	}
}

func TestTokenize_Punctuation(t *testing.T) {
	tokens := tokenize("it's a test--with punctuation! and (parens)")
	for _, tok := range tokens {
		for _, r := range tok {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
				t.Errorf("token %q contains non-alphanumeric rune %c", tok, r)
			}
		}
	}
}

func TestTokenize_CaseInsensitive(t *testing.T) {
	tokens := tokenize("ABC DEF abc def")
	for _, tok := range tokens {
		if tok != "abc" && tok != "def" {
			t.Errorf("unexpected token: %q (should be lowercase)", tok)
		}
	}
}

func TestTokenize_Empty(t *testing.T) {
	tokens := tokenize("")
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens for empty string, got %d", len(tokens))
	}
}

func TestClassifyTaskType(t *testing.T) {
	tests := []struct {
		prompt   string
		expected string
	}{
		{"implement a function to sort arrays", "coding"},
		{"explain how TCP works", "informational"},
		{"deploy the application to kubernetes", "operational"},
		{"write a short story about a dog", "creative"},
		{"analyze the performance of this algorithm", "analysis"},
		{"hello", "general"},
	}
	for _, tt := range tests {
		got := classifyTaskType(tt.prompt)
		if got != tt.expected {
			t.Errorf("classifyTaskType(%q) = %q, want %q", tt.prompt, got, tt.expected)
		}
	}
}
