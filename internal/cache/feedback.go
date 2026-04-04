package cache

import (
	"sync"
	"time"
)

// FeedbackStore collects user feedback on cache hit quality for continuous learning.
type FeedbackStore struct {
	mu                 sync.RWMutex
	entries            []FeedbackEntry
	maxSize            int
	synonymSuggestions map[string]string
	keyNounSuggestions map[string]bool
}

// FeedbackEntry records whether a cache hit was helpful.
type FeedbackEntry struct {
	Query       string    `json:"query"`
	CachedQuery string    `json:"cached_query"`
	Helpful     bool      `json:"helpful"`
	Similarity  float64   `json:"similarity"`
	CacheLayer  string    `json:"cache_layer"`
	QueryType   string    `json:"query_type"`
	Timestamp   time.Time `json:"timestamp"`
}

// FeedbackStats provides aggregate statistics on cache quality.
type FeedbackStats struct {
	TotalFeedback      int     `json:"total_feedback"`
	HelpfulCount       int     `json:"helpful_count"`
	UnhelpfulCount     int     `json:"unhelpful_count"`
	HelpfulRate        float64 `json:"helpful_rate"`
	AvgSimilarity      float64 `json:"avg_similarity"`
	FalsePositiveRate  float64 `json:"false_positive_rate"`
	SynonymSuggestions int     `json:"synonym_suggestions"`
	KeyNounSuggestions int     `json:"key_noun_suggestions"`
}

// NewFeedbackStore creates a new feedback collection store.
func NewFeedbackStore(maxSize int) *FeedbackStore {
	if maxSize <= 0 {
		maxSize = 10000
	}
	return &FeedbackStore{
		maxSize:            maxSize,
		synonymSuggestions: make(map[string]string),
		keyNounSuggestions: make(map[string]bool),
	}
}

// Record stores a feedback entry and triggers learning analysis.
func (fs *FeedbackStore) Record(entry FeedbackEntry) {
	entry.Timestamp = time.Now()

	fs.mu.Lock()
	defer fs.mu.Unlock()

	if len(fs.entries) >= fs.maxSize {
		cutoff := fs.maxSize / 10
		fs.entries = fs.entries[cutoff:]
	}

	fs.entries = append(fs.entries, entry)

	if !entry.Helpful && entry.CachedQuery != "" {
		fs.analyzeFailure(entry)
	}
}

// analyzeFailure examines an unhelpful cache hit to learn improvements.
func (fs *FeedbackStore) analyzeFailure(entry FeedbackEntry) {
	qWords := tokenizeWords(entry.Query)
	cWords := tokenizeWords(entry.CachedQuery)

	qSet := make(map[string]bool)
	for _, w := range qWords {
		qSet[w] = true
	}
	cSet := make(map[string]bool)
	for _, w := range cWords {
		cSet[w] = true
	}

	for w := range qSet {
		if !cSet[w] && len(w) > 3 {
			fs.keyNounSuggestions[w] = true
		}
	}
	for w := range cSet {
		if !qSet[w] && len(w) > 3 {
			fs.keyNounSuggestions[w] = true
		}
	}
}

// Stats returns aggregate feedback statistics.
func (fs *FeedbackStore) Stats() FeedbackStats {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	stats := FeedbackStats{
		TotalFeedback:      len(fs.entries),
		SynonymSuggestions: len(fs.synonymSuggestions),
		KeyNounSuggestions: len(fs.keyNounSuggestions),
	}

	if len(fs.entries) == 0 {
		return stats
	}

	totalSim := 0.0
	for _, e := range fs.entries {
		if e.Helpful {
			stats.HelpfulCount++
		} else {
			stats.UnhelpfulCount++
		}
		totalSim += e.Similarity
	}

	stats.HelpfulRate = float64(stats.HelpfulCount) / float64(stats.TotalFeedback)
	stats.AvgSimilarity = totalSim / float64(stats.TotalFeedback)
	stats.FalsePositiveRate = float64(stats.UnhelpfulCount) / float64(stats.TotalFeedback)

	return stats
}

// GetSuggestions returns learned synonym and key noun suggestions.
func (fs *FeedbackStore) GetSuggestions() (synonyms map[string]string, keyNouns []string) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	synonyms = make(map[string]string, len(fs.synonymSuggestions))
	for k, v := range fs.synonymSuggestions {
		synonyms[k] = v
	}

	keyNouns = make([]string, 0, len(fs.keyNounSuggestions))
	for k := range fs.keyNounSuggestions {
		keyNouns = append(keyNouns, k)
	}

	return synonyms, keyNouns
}

// RecentFalsePositives returns recent unhelpful cache hits for review.
func (fs *FeedbackStore) RecentFalsePositives(limit int) []FeedbackEntry {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var fps []FeedbackEntry
	for i := len(fs.entries) - 1; i >= 0 && len(fps) < limit; i-- {
		if !fs.entries[i].Helpful {
			fps = append(fps, fs.entries[i])
		}
	}
	return fps
}
