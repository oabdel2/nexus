package cache

import (
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

// BM25Cache provides keyword-based similarity matching.
// It indexes cached prompts using BM25 scoring and finds
// the most similar cached prompt for a given query.
//
// BM25 formula: score(q,d) = Σ IDF(qi) * (tf * (k1+1)) / (tf + k1 * (1 - b + b * |d|/avgdl))
// where k1=1.5, b=0.75 are standard parameters
type BM25Cache struct {
	mu            sync.RWMutex
	docs          []bm25Doc
	df            map[string]int // document frequency per term
	invertedIdx   map[string][]int // term → doc indices for O(1) candidate lookup
	totalTokenLen int              // running sum of doc token lengths for O(1) avgdl
	ttl           time.Duration
	maxEntries    int
	threshold     float64
	hits          int64
	misses        int64
}

type bm25Doc struct {
	prompt    string
	model     string
	tokens    []string
	termFreq  map[string]int
	response  []byte
	createdAt time.Time
}

var stopwords = map[string]bool{
	"the": true, "is": true, "a": true, "an": true, "in": true,
	"on": true, "at": true, "to": true, "for": true, "of": true,
	"and": true, "or": true, "but": true, "not": true, "with": true,
	"this": true, "that": true, "from": true, "by": true, "as": true,
	"it": true, "be": true, "are": true, "was": true, "were": true,
	"been": true, "being": true, "have": true, "has": true, "had": true,
	"do": true, "does": true, "did": true, "will": true, "would": true,
	"could": true, "should": true, "can": true, "may": true, "might": true,
	"shall": true,
}

func NewBM25Cache(ttl time.Duration, maxEntries int, threshold float64) *BM25Cache {
	c := &BM25Cache{
		df:          make(map[string]int),
		invertedIdx: make(map[string][]int),
		ttl:         ttl,
		maxEntries:  maxEntries,
		threshold:   threshold,
	}
	go c.cleanup()
	return c
}

// Tokenize splits text into lowercase tokens, removes stopwords and punctuation,
// and applies simple suffix stemming for better matching.
func Tokenize(text string) []string {
	lower := strings.ToLower(text)
	// Split on non-alphanumeric characters
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var tokens []string
	for _, w := range words {
		if len(w) > 1 && !stopwords[w] {
			tokens = append(tokens, simpleStem(w))
		}
	}
	return tokens
}

// simpleStem applies basic suffix stripping for English words.
func simpleStem(word string) string {
	// Handle "ies" → "y" replacement (e.g., "queries" → "query")
	if len(word) > 4 && strings.HasSuffix(word, "ies") {
		return word[:len(word)-3] + "y"
	}
	// Handle "es" only after s, x, z, sh, ch (e.g., "boxes" → "box")
	if len(word) > 3 && strings.HasSuffix(word, "es") {
		pre := word[:len(word)-2]
		last := pre[len(pre)-1]
		if last == 's' || last == 'x' || last == 'z' {
			return pre
		}
		if len(pre) >= 2 && (strings.HasSuffix(pre, "sh") || strings.HasSuffix(pre, "ch")) {
			return pre
		}
	}
	// Handle common suffixes (longest first)
	suffixes := []string{"ation", "tion", "ing", "ment", "ness", "ous", "ive", "ize", "ed", "ly", "er", "al"}
	for _, suffix := range suffixes {
		if len(word) > len(suffix)+2 && strings.HasSuffix(word, suffix) {
			return strings.TrimSuffix(word, suffix)
		}
	}
	// General "s" plural (e.g., "goroutines" → "goroutine")
	if len(word) > 3 && strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss") {
		return word[:len(word)-1]
	}
	return word
}

func termFrequency(tokens []string) map[string]int {
	tf := make(map[string]int, len(tokens))
	for _, t := range tokens {
		tf[t]++
	}
	return tf
}

func (c *BM25Cache) Store(prompt, model string, response []byte) {
	tokens := Tokenize(prompt)
	tf := termFrequency(tokens)

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.docs) >= c.maxEntries {
		c.evictOldest()
	}

	// Update document frequencies for new terms
	seen := make(map[string]bool, len(tf))
	for term := range tf {
		if !seen[term] {
			c.df[term]++
			seen[term] = true
		}
	}

	c.docs = append(c.docs, bm25Doc{
		prompt:    prompt,
		model:     model,
		tokens:    tokens,
		termFreq:  tf,
		response:  response,
		createdAt: time.Now(),
	})
	c.totalTokenLen += len(tokens)
	c.rebuildInvertedIndex()
}

func (c *BM25Cache) Lookup(prompt, model string) ([]byte, bool) {
	queryTokens := Tokenize(prompt)
	if len(queryTokens) == 0 {
		atomic.AddInt64(&c.misses, 1)
		return nil, false
	}

	c.mu.RLock()
	n := len(c.docs)
	if n == 0 {
		c.mu.RUnlock()
		atomic.AddInt64(&c.misses, 1)
		return nil, false
	}

	// Use inverted index to find candidate docs sharing at least one query term
	candidates := make(map[int]struct{})
	for _, qt := range queryTokens {
		if indices, ok := c.invertedIdx[qt]; ok {
			for _, idx := range indices {
				candidates[idx] = struct{}{}
			}
		}
	}

	// Calculate average document length from running total
	avgdl := float64(c.totalTokenLen) / float64(n)

	const k1 = 1.5
	const b = 0.75

	bestScore := -1.0
	bestIdx := -1
	now := time.Now()

	for i := range candidates {
		doc := &c.docs[i]
		// Skip expired entries
		if now.Sub(doc.createdAt) > c.ttl {
			continue
		}
		// Skip model mismatch
		if doc.model != model {
			continue
		}

		dl := float64(len(doc.tokens))
		score := 0.0

		for _, qt := range queryTokens {
			df, exists := c.df[qt]
			if !exists {
				continue
			}
			// IDF = ln((N - df + 0.5) / (df + 0.5) + 1)
			idf := math.Log((float64(n)-float64(df)+0.5)/(float64(df)+0.5) + 1.0)

			tf := float64(doc.termFreq[qt])
			tfNorm := (tf * (k1 + 1)) / (tf + k1*(1-b+b*dl/avgdl))
			score += idf * tfNorm
		}

		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	// Copy response while still holding RLock (prevents TOCTOU race)
	var resp []byte
	if bestIdx >= 0 && bestScore >= c.threshold {
		resp = make([]byte, len(c.docs[bestIdx].response))
		copy(resp, c.docs[bestIdx].response)
	}
	c.mu.RUnlock()

	if resp != nil {
		atomic.AddInt64(&c.hits, 1)
		return resp, true
	}

	atomic.AddInt64(&c.misses, 1)
	return nil, false
}

func (c *BM25Cache) Stats() (hits, misses int64, size int) {
	c.mu.RLock()
	size = len(c.docs)
	c.mu.RUnlock()
	return atomic.LoadInt64(&c.hits), atomic.LoadInt64(&c.misses), size
}

func (c *BM25Cache) evictOldest() {
	if len(c.docs) == 0 {
		return
	}
	oldestIdx := 0
	for i := 1; i < len(c.docs); i++ {
		if c.docs[i].createdAt.Before(c.docs[oldestIdx].createdAt) {
			oldestIdx = i
		}
	}
	// Update running totals
	c.totalTokenLen -= len(c.docs[oldestIdx].tokens)
	// Remove DF counts for the evicted doc
	for term := range c.docs[oldestIdx].termFreq {
		c.df[term]--
		if c.df[term] <= 0 {
			delete(c.df, term)
		}
	}
	c.docs = append(c.docs[:oldestIdx], c.docs[oldestIdx+1:]...)
	c.rebuildInvertedIndex()
}

func (c *BM25Cache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		changed := false
		i := 0
		for i < len(c.docs) {
			if now.Sub(c.docs[i].createdAt) > c.ttl {
				c.totalTokenLen -= len(c.docs[i].tokens)
				// Remove DF counts
				for term := range c.docs[i].termFreq {
					c.df[term]--
					if c.df[term] <= 0 {
						delete(c.df, term)
					}
				}
				c.docs = append(c.docs[:i], c.docs[i+1:]...)
				changed = true
			} else {
				i++
			}
		}
		if changed {
			c.rebuildInvertedIndex()
		}
		c.mu.Unlock()
	}
}

// rebuildInvertedIndex reconstructs the term → doc indices mapping.
// Caller must hold c.mu write lock.
func (c *BM25Cache) rebuildInvertedIndex() {
	c.invertedIdx = make(map[string][]int, len(c.df))
	for i, doc := range c.docs {
		for term := range doc.termFreq {
			c.invertedIdx[term] = append(c.invertedIdx[term], i)
		}
	}
}
