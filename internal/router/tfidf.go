package router

import (
	"math"
	"strings"
	"sync"
	"unicode"
)

// TrainingExample is a labeled example for TF-IDF training.
type TrainingExample struct {
	Text string
	Tier string // "economy", "cheap", "mid", "premium"
}

type classifiedDoc struct {
	text  string
	tier  string
	tfidf []float64 // sparse TF-IDF vector indexed by vocabulary
}

// TFIDFClassifier implements TF-IDF based prompt classification.
type TFIDFClassifier struct {
	mu         sync.RWMutex
	documents  []classifiedDoc
	idfCache   map[string]float64
	vocabulary map[string]int // term → index
	trained    bool
}

// NewTFIDFClassifier creates a classifier pre-loaded with built-in training examples.
func NewTFIDFClassifier() *TFIDFClassifier {
	tc := &TFIDFClassifier{
		idfCache:   make(map[string]float64),
		vocabulary: make(map[string]int),
	}
	tc.Train(builtinTrainingCorpus())
	return tc
}

// Train builds the IDF cache and computes TF-IDF vectors for all documents.
func (tc *TFIDFClassifier) Train(docs []TrainingExample) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.trainLocked(docs)
}

// trainLocked performs the actual training. Caller must hold tc.mu.Lock().
func (tc *TFIDFClassifier) trainLocked(docs []TrainingExample) {
	// Reset state
	tc.documents = make([]classifiedDoc, 0, len(docs))
	tc.idfCache = make(map[string]float64)
	tc.vocabulary = make(map[string]int)

	if len(docs) == 0 {
		tc.trained = false
		return
	}

	// Tokenize all documents and build vocabulary
	tokenized := make([][]string, len(docs))
	docFreq := make(map[string]int) // number of docs containing term

	for i, doc := range docs {
		tokens := tokenize(doc.Text)
		tokenized[i] = tokens
		seen := make(map[string]bool)
		for _, t := range tokens {
			if !seen[t] {
				docFreq[t]++
				seen[t] = true
			}
			if _, ok := tc.vocabulary[t]; !ok {
				tc.vocabulary[t] = len(tc.vocabulary)
			}
		}
	}

	// Compute IDF: log(N / df)
	n := float64(len(docs))
	for term, df := range docFreq {
		tc.idfCache[term] = math.Log(n / float64(df))
	}

	// Compute TF-IDF vectors for each document
	vocabSize := len(tc.vocabulary)
	for i, doc := range docs {
		vec := computeTFIDF(tokenized[i], tc.vocabulary, tc.idfCache, vocabSize)
		tc.documents = append(tc.documents, classifiedDoc{
			text:  doc.Text,
			tier:  doc.Tier,
			tfidf: vec,
		})
	}

	tc.trained = true
}

// IsTrained returns whether the classifier has training data.
func (tc *TFIDFClassifier) IsTrained() bool {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return tc.trained
}

// AddExample adds a single labeled example and retrains incrementally.
func (tc *TFIDFClassifier) AddExample(text, tier string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Collect existing examples
	examples := make([]TrainingExample, 0, len(tc.documents)+1)
	for _, d := range tc.documents {
		examples = append(examples, TrainingExample{Text: d.text, Tier: d.tier})
	}
	examples = append(examples, TrainingExample{Text: text, Tier: tier})

	// Retrain while already holding the lock
	tc.trainLocked(examples)
}

// Classify returns the predicted tier and confidence for a prompt using k-NN (k=5).
func (tc *TFIDFClassifier) Classify(prompt string) (tier string, confidence float64) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	if !tc.trained || len(tc.documents) == 0 {
		return "mid", 0.0
	}

	tokens := tokenize(prompt)
	vec := computeTFIDF(tokens, tc.vocabulary, tc.idfCache, len(tc.vocabulary))

	// Compute cosine similarity to every training document
	type scoredDoc struct {
		tier       string
		similarity float64
	}
	scores := make([]scoredDoc, len(tc.documents))
	for i, doc := range tc.documents {
		scores[i] = scoredDoc{
			tier:       doc.tier,
			similarity: cosineSimilarity(vec, doc.tfidf),
		}
	}

	// k-NN with k=5, weighted vote by similarity
	k := 5
	if k > len(scores) {
		k = len(scores)
	}

	// Partial sort: find top-k by similarity
	topK := make([]scoredDoc, k)
	copy(topK, scores[:k])
	for i := k; i < len(scores); i++ {
		// Find minimum in topK
		minIdx := 0
		for j := 1; j < k; j++ {
			if topK[j].similarity < topK[minIdx].similarity {
				minIdx = j
			}
		}
		if scores[i].similarity > topK[minIdx].similarity {
			topK[minIdx] = scores[i]
		}
	}

	// Weighted vote
	tierWeights := make(map[string]float64)
	totalWeight := 0.0
	for _, s := range topK {
		w := s.similarity
		if w < 0 {
			w = 0
		}
		tierWeights[s.tier] += w
		totalWeight += w
	}

	// Find winning tier
	bestTier := "mid"
	bestWeight := 0.0
	for t, w := range tierWeights {
		if w > bestWeight {
			bestWeight = w
			bestTier = t
		}
	}

	if totalWeight > 0 {
		confidence = bestWeight / totalWeight
	}

	return bestTier, confidence
}

// tokenize lowercases, splits on non-alphanumeric boundaries, and removes stop words.
func tokenize(text string) []string {
	lower := strings.ToLower(text)
	// Split on any non-letter, non-digit character
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	result := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) < 2 {
			continue
		}
		if stopWords[w] {
			continue
		}
		result = append(result, w)
	}
	return result
}

// computeTFIDF computes a TF-IDF vector for the given tokens.
func computeTFIDF(tokens []string, vocab map[string]int, idf map[string]float64, vocabSize int) []float64 {
	vec := make([]float64, vocabSize)
	if len(tokens) == 0 {
		return vec
	}

	// Term frequency
	tf := make(map[string]int)
	for _, t := range tokens {
		tf[t]++
	}
	total := float64(len(tokens))

	for term, count := range tf {
		idx, ok := vocab[term]
		if !ok {
			continue // term not in vocabulary
		}
		termTF := float64(count) / total
		termIDF := idf[term] // 0 if not found
		vec[idx] = termTF * termIDF
	}
	return vec
}

// cosineSimilarity computes cosine similarity between two vectors.
func cosineSimilarity(a, b []float64) float64 {
	// Handle mismatched lengths by using the shorter length
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return 0.0
	}

	var dot, normA, normB float64
	for i := 0; i < n; i++ {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	// Account for remaining elements in longer vector
	for i := n; i < len(a); i++ {
		normA += a[i] * a[i]
	}
	for i := n; i < len(b); i++ {
		normB += b[i] * b[i]
	}

	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0.0
	}
	return dot / denom
}

// builtinTrainingCorpus returns 60+ labeled training examples.
func builtinTrainingCorpus() []TrainingExample {
	return []TrainingExample{
		// ── Economy tier (15+ examples) ──
		{Text: "hi", Tier: "economy"},
		{Text: "hello", Tier: "economy"},
		{Text: "thanks", Tier: "economy"},
		{Text: "what time is it", Tier: "economy"},
		{Text: "list the colors", Tier: "economy"},
		{Text: "count to 10", Tier: "economy"},
		{Text: "rename this variable", Tier: "economy"},
		{Text: "fix this typo", Tier: "economy"},
		{Text: "add a comment here", Tier: "economy"},
		{Text: "what's the date", Tier: "economy"},
		{Text: "show me the version", Tier: "economy"},
		{Text: "format this JSON", Tier: "economy"},
		{Text: "translate hello to Spanish", Tier: "economy"},
		{Text: "convert celsius to fahrenheit", Tier: "economy"},
		{Text: "write a commit message for this change", Tier: "economy"},
		{Text: "say goodbye", Tier: "economy"},
		{Text: "what is 2 plus 2", Tier: "economy"},
		{Text: "analyze this list of names", Tier: "economy"},

		// ── Cheap tier (15+ examples) ──
		{Text: "explain what a REST API is", Tier: "cheap"},
		{Text: "summarize this paragraph", Tier: "cheap"},
		{Text: "write a unit test for this function", Tier: "cheap"},
		{Text: "compare Python and JavaScript", Tier: "cheap"},
		{Text: "create a simple HTML page", Tier: "cheap"},
		{Text: "set up a basic Express server", Tier: "cheap"},
		{Text: "write a README for this project", Tier: "cheap"},
		{Text: "explain git branching", Tier: "cheap"},
		{Text: "configure nginx as a reverse proxy", Tier: "cheap"},
		{Text: "what is the difference between TCP and UDP", Tier: "cheap"},
		{Text: "write a simple sorting algorithm", Tier: "cheap"},
		{Text: "explain how HTTP cookies work", Tier: "cheap"},
		{Text: "create a basic CSS stylesheet for a blog", Tier: "cheap"},
		{Text: "write a bash script to backup files", Tier: "cheap"},
		{Text: "explain what Docker containers are", Tier: "cheap"},
		{Text: "can you help me analyze this short log", Tier: "cheap"},
		{Text: "analyze these two options for me", Tier: "cheap"},
		{Text: "help me analyze this error message", Tier: "cheap"},

		// ── Mid tier (15+ examples) ──
		{Text: "review this pull request for bugs and style issues", Tier: "mid"},
		{Text: "write an integration test that covers the authentication flow", Tier: "mid"},
		{Text: "build a React component that handles pagination with error states", Tier: "mid"},
		{Text: "explain the trade-offs between SQL and NoSQL for this use case", Tier: "mid"},
		{Text: "create a CI/CD pipeline for a Go project with Docker", Tier: "mid"},
		{Text: "refactor this module to use dependency injection", Tier: "mid"},
		{Text: "design a database schema for an e-commerce system with orders and inventory", Tier: "mid"},
		{Text: "implement a caching layer with TTL expiration and LRU eviction", Tier: "mid"},
		{Text: "write a middleware that handles rate limiting per user", Tier: "mid"},
		{Text: "set up monitoring and alerting for a production Kubernetes cluster", Tier: "mid"},
		{Text: "create an API versioning strategy for backward compatibility", Tier: "mid"},
		{Text: "build a WebSocket chat server with room support", Tier: "mid"},
		{Text: "implement OAuth2 authorization code flow from scratch", Tier: "mid"},
		{Text: "write comprehensive error handling for this REST API", Tier: "mid"},
		{Text: "optimize these database queries that are causing slow page loads", Tier: "mid"},
		{Text: "design a message queue consumer with retry and dead letter handling", Tier: "mid"},

		// ── Premium tier (15+ examples) ──
		{Text: "debug this race condition in the concurrent cache implementation", Tier: "premium"},
		{Text: "design a fault-tolerant distributed consensus algorithm for our microservices", Tier: "premium"},
		{Text: "analyze this memory leak in production — here's the heap dump", Tier: "premium"},
		{Text: "architect a multi-region active-active database replication strategy", Tier: "premium"},
		{Text: "implement a zero-downtime migration from monolith to microservices", Tier: "premium"},
		{Text: "find and fix the security vulnerability in this authentication flow", Tier: "premium"},
		{Text: "design a distributed transaction system with saga pattern and compensation", Tier: "premium"},
		{Text: "implement a custom garbage collector for our memory-constrained embedded system", Tier: "premium"},
		{Text: "architect an event sourcing system with CQRS for high-throughput financial transactions", Tier: "premium"},
		{Text: "debug the intermittent deadlock between the connection pool and the transaction manager", Tier: "premium"},
		{Text: "design a Byzantine fault-tolerant consensus protocol for our blockchain network", Tier: "premium"},
		{Text: "implement end-to-end encryption with perfect forward secrecy and key rotation", Tier: "premium"},
		{Text: "analyze and fix the cascading failure pattern across our microservice mesh", Tier: "premium"},
		{Text: "build a real-time anomaly detection system for network intrusion with sub-millisecond latency", Tier: "premium"},
		{Text: "design a multi-tenant isolation architecture with shared-nothing guarantees and cross-tenant analytics", Tier: "premium"},
		{Text: "write a comprehensive fault-tolerant distributed consensus algorithm with leader election", Tier: "premium"},
	}
}
