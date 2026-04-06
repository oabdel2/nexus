package router

import (
	"math"
	"testing"
)

// ==================== TF-IDF Core Tests ====================

func TestTFIDF_TrainWithoutError(t *testing.T) {
	tc := NewTFIDFClassifier()
	if !tc.IsTrained() {
		t.Error("NewTFIDFClassifier should be trained with built-in corpus")
	}
}

func TestTFIDF_TrainEmpty(t *testing.T) {
	tc := &TFIDFClassifier{
		idfCache:   make(map[string]float64),
		vocabulary: make(map[string]int),
	}
	tc.Train(nil)
	if tc.IsTrained() {
		t.Error("training with nil should leave classifier untrained")
	}
}

func TestTFIDF_EconomyPrompt(t *testing.T) {
	tc := NewTFIDFClassifier()
	tier, conf := tc.Classify("hi")
	if tier != "economy" {
		t.Errorf("'hi' should classify as economy, got %q (conf=%.2f)", tier, conf)
	}
}

func TestTFIDF_EconomyPrompt2(t *testing.T) {
	tc := NewTFIDFClassifier()
	tier, _ := tc.Classify("hello thanks")
	if tier != "economy" {
		t.Errorf("'hello thanks' should classify as economy, got %q", tier)
	}
}

func TestTFIDF_PremiumPrompt(t *testing.T) {
	tc := NewTFIDFClassifier()
	tier, _ := tc.Classify("debug this race condition in the concurrent cache implementation")
	if tier != "premium" {
		t.Errorf("premium prompt should classify as premium, got %q", tier)
	}
}

func TestTFIDF_PremiumPrompt2(t *testing.T) {
	tc := NewTFIDFClassifier()
	tier, _ := tc.Classify("design a fault-tolerant distributed consensus algorithm")
	if tier != "premium" {
		t.Errorf("distributed consensus prompt should classify as premium, got %q", tier)
	}
}

func TestTFIDF_CheapPrompt(t *testing.T) {
	tc := NewTFIDFClassifier()
	tier, _ := tc.Classify("explain what a REST API is")
	if tier != "cheap" {
		t.Errorf("'explain REST API' should classify as cheap, got %q", tier)
	}
}

func TestTFIDF_MidPrompt(t *testing.T) {
	tc := NewTFIDFClassifier()
	tier, _ := tc.Classify("review this pull request for bugs and style issues")
	if tier != "mid" {
		t.Errorf("pull request review should classify as mid, got %q", tier)
	}
}

func TestTFIDF_UnknownPromptReasonable(t *testing.T) {
	tc := NewTFIDFClassifier()
	tier, _ := tc.Classify("blargfizzle xyzzy plugh")
	// Unknown gibberish should still return a valid tier
	valid := map[string]bool{"economy": true, "cheap": true, "mid": true, "premium": true}
	if !valid[tier] {
		t.Errorf("unknown prompt should return a valid tier, got %q", tier)
	}
}

// Key improvement: "analyze" in a simple context should NOT be classified as premium
func TestTFIDF_AnalyzeSimpleContext_NotPremium(t *testing.T) {
	tc := NewTFIDFClassifier()
	tier, _ := tc.Classify("can you help me analyze this")
	if tier == "premium" {
		t.Errorf("'can you help me analyze this' should NOT be premium (simple request), got %q", tier)
	}
}

func TestTFIDF_LongComplexNoKeywords(t *testing.T) {
	tc := NewTFIDFClassifier()
	prompt := "architect a multi-region active-active database replication strategy with failover"
	tier, _ := tc.Classify(prompt)
	if tier != "premium" && tier != "mid" {
		t.Errorf("complex long prompt should classify as premium or mid, got %q", tier)
	}
}

// ==================== Online Learning Tests ====================

func TestTFIDF_OnlineLearning(t *testing.T) {
	tc := NewTFIDFClassifier()

	// Add a novel phrase as economy
	tc.AddExample("frobnicate the widget", "economy")
	tc.AddExample("frobnicate something", "economy")
	tc.AddExample("frobnicate it please", "economy")

	tier, _ := tc.Classify("frobnicate the thingy")
	if tier != "economy" {
		t.Errorf("after adding frobnicate examples as economy, classify should return economy, got %q", tier)
	}
}

// ==================== Cosine Similarity Tests ====================

func TestCosineSimilarity_Identical(t *testing.T) {
	a := []float64{1.0, 2.0, 3.0}
	sim := cosineSimilarity(a, a)
	if math.Abs(sim-1.0) > 1e-9 {
		t.Errorf("identical vectors should have cosine similarity 1.0, got %f", sim)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float64{1.0, 0.0}
	b := []float64{0.0, 1.0}
	sim := cosineSimilarity(a, b)
	if math.Abs(sim) > 1e-9 {
		t.Errorf("orthogonal vectors should have cosine similarity 0.0, got %f", sim)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	a := []float64{0.0, 0.0, 0.0}
	b := []float64{1.0, 2.0, 3.0}
	sim := cosineSimilarity(a, b)
	if sim != 0.0 {
		t.Errorf("zero vector cosine similarity should be 0.0, got %f", sim)
	}
}

func TestCosineSimilarity_MismatchedLength(t *testing.T) {
	a := []float64{1.0, 0.0, 0.0}
	b := []float64{1.0, 0.0}
	sim := cosineSimilarity(a, b)
	if math.Abs(sim-1.0) > 1e-9 {
		t.Errorf("mismatched-length vectors [1,0,0] and [1,0] should be 1.0, got %f", sim)
	}
}

// ==================== Tokenizer Tests ====================

func TestTokenize_Punctuation(t *testing.T) {
	tokens := tokenize("Hello, world! How's it going?")
	for _, tok := range tokens {
		if tok == "," || tok == "!" || tok == "?" || tok == "'" {
			t.Errorf("tokenizer should strip punctuation, found %q", tok)
		}
	}
}

func TestTokenize_Uppercase(t *testing.T) {
	tokens := tokenize("DEBUG This RACE Condition")
	for _, tok := range tokens {
		for _, r := range tok {
			if r >= 'A' && r <= 'Z' {
				t.Errorf("tokenizer should lowercase all tokens, found %q", tok)
			}
		}
	}
}

func TestTokenize_StopWords(t *testing.T) {
	tokens := tokenize("the quick brown fox jumps over the lazy dog")
	for _, tok := range tokens {
		if stopWords[tok] {
			t.Errorf("tokenizer should remove stop words, found %q", tok)
		}
	}
}

func TestTokenize_ShortWords(t *testing.T) {
	tokens := tokenize("I a x do it")
	// Single-char words should be filtered
	for _, tok := range tokens {
		if len(tok) < 2 {
			t.Errorf("tokenizer should filter single-char words, found %q", tok)
		}
	}
}

// ==================== SmartClassifier Tests ====================

func TestSmartClassifier_CombinesTFIDFAndKeywords(t *testing.T) {
	sc := NewSmartClassifier()
	score := sc.Classify("debug race condition in distributed system", "", 0.5, 0.5, 0)
	if score.PromptScore < 0.5 {
		t.Errorf("complex prompt with TF-IDF should have high PromptScore, got %f", score.PromptScore)
	}
}

func TestSmartClassifier_FallsBackToKeywords(t *testing.T) {
	// Create a SmartClassifier with untrained TF-IDF
	sc := &SmartClassifier{
		tfidf:    &TFIDFClassifier{idfCache: make(map[string]float64), vocabulary: make(map[string]int)},
		keywords: true,
		weights:  DefaultSmartWeights(),
	}
	// With untrained TF-IDF, should fall back to keyword scoring
	score := sc.Classify("debug this issue", "", 0.5, 0.5, 0)
	keyScore := keywordPromptScore("debug this issue")
	if math.Abs(score.PromptScore-keyScore) > 0.01 {
		t.Errorf("untrained TF-IDF should fall back to keyword score (%.2f), got %.2f", keyScore, score.PromptScore)
	}
}

func TestSmartClassifier_StructuralCodeBlock(t *testing.T) {
	prompt := "fix this:\n```go\nfunc main() {}\n```"
	s := structuralScore(prompt)
	if s <= 0.0 {
		t.Errorf("prompt with code block should have positive structural score, got %f", s)
	}
}

func TestSmartClassifier_StructuralMultipleQuestions(t *testing.T) {
	prompt := "What does this do? How can I fix it? Why is it slow?"
	s := structuralScore(prompt)
	if s <= 0.0 {
		t.Errorf("prompt with multiple questions should have positive structural score, got %f", s)
	}
}

func TestSmartClassifier_StructuralNegation(t *testing.T) {
	prompt := "implement this without using recursion and don't use global variables"
	s := structuralScore(prompt)
	if s <= 0.0 {
		t.Errorf("prompt with negation constraints should have positive structural score, got %f", s)
	}
}

func TestSmartClassifier_StructuralList(t *testing.T) {
	prompt := "1. set up the database\n2. create the schema\n3. write migrations\n- test everything"
	s := structuralScore(prompt)
	if s <= 0.0 {
		t.Errorf("prompt with list should have positive structural score, got %f", s)
	}
}

func TestSmartClassifier_StructuralConditional(t *testing.T) {
	prompt := "if the user is authenticated then show the dashboard, otherwise redirect to login"
	s := structuralScore(prompt)
	if s <= 0.0 {
		t.Errorf("prompt with conditionals should have positive structural score, got %f", s)
	}
}

func TestSmartClassifier_AllFieldsPopulated(t *testing.T) {
	sc := NewSmartClassifier()
	score := sc.Classify("build a web app", "engineer", 0.2, 0.5, 2048)
	if score.RoleScore != 0.85 {
		t.Errorf("engineer RoleScore should be 0.85, got %f", score.RoleScore)
	}
	if score.PositionScore != 0.7 {
		t.Errorf("early step PositionScore should be 0.7, got %f", score.PositionScore)
	}
	if math.Abs(score.BudgetScore-0.5) > 0.001 {
		t.Errorf("BudgetScore should be 0.5, got %f", score.BudgetScore)
	}
	if math.Abs(score.ContextScore-0.5) > 0.001 {
		t.Errorf("ContextScore should be ~0.5, got %f", score.ContextScore)
	}
}

// ==================== Integration: Router with SmartClassifier ====================

func TestRoute_SmartClassifier_Enabled(t *testing.T) {
	cfg := defaultRouterCfg()
	cfg.SmartClassifier = true
	r := New(cfg, allTierProviders(), testLogger())
	if r.smartClassifier == nil {
		t.Fatal("SmartClassifier should be enabled when config flag is true")
	}
	sel := r.Route("hi", "", 0.5, 0.5, 0)
	if sel.Tier == "premium" {
		t.Errorf("'hi' with SmartClassifier should not route to premium, got %s", sel.Tier)
	}
}

func TestRoute_SmartClassifier_Disabled(t *testing.T) {
	cfg := defaultRouterCfg()
	cfg.SmartClassifier = false
	r := New(cfg, allTierProviders(), testLogger())
	if r.smartClassifier != nil {
		t.Error("SmartClassifier should be nil when config flag is false")
	}
	// Should still work with old classifier
	sel := r.Route("hi", "", 0.5, 0.5, 0)
	if sel.Provider == "" {
		t.Error("routing should still work without SmartClassifier")
	}
}

// ==================== Benchmark ====================

func BenchmarkClassify_TFIDF(b *testing.B) {
	tc := NewTFIDFClassifier()
	prompts := []string{
		"hi",
		"explain what a REST API is",
		"review this pull request for bugs and style issues",
		"debug the race condition in the concurrent cache implementation",
		"write a simple sorting algorithm in Python",
		"design a fault-tolerant distributed consensus algorithm for our microservices",
		"can you help me analyze this",
		"implement OAuth2 authorization code flow from scratch",
		"hello",
		"what time is it",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tc.Classify(prompts[i%len(prompts)])
	}
}

func BenchmarkClassify_Smart(b *testing.B) {
	sc := NewSmartClassifier()
	prompts := []string{
		"hi",
		"explain what a REST API is",
		"review this pull request for bugs and style issues",
		"debug the race condition in the concurrent cache implementation",
		"write a simple sorting algorithm in Python",
		"design a fault-tolerant distributed consensus algorithm for our microservices",
		"can you help me analyze this",
		"implement OAuth2 authorization code flow from scratch",
		"hello",
		"what time is it",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sc.Classify(prompts[i%len(prompts)], "", 0.5, 0.5, 0)
	}
}
