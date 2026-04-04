package cache

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SynonymRegistry manages a 3-tier synonym system that grows from usage.
type SynonymRegistry struct {
	mu         sync.RWMutex
	base       map[string]string           // Tier 1: compiled, read-only
	learned    map[string]string           // Tier 2: persisted to disk
	candidates map[string]*SynonymCandidate // Tier 3: pending promotion

	// Key noun registry (also grows dynamically)
	baseKeyNouns    map[string]bool
	learnedKeyNouns map[string]bool

	// Config
	promotionThreshold int           // confirmations needed to promote (default: 3)
	staleTimeout       time.Duration // evict candidates after this (default: 7 days)
	dataDir            string        // directory for persistence files
	saveInterval       time.Duration // how often to save to disk (default: 5 min)
	stopCh             chan struct{} // stop background goroutines
	doneCh             chan struct{} // signals background loop has exited
}

// SynonymCandidate tracks a potential synonym awaiting promotion.
type SynonymCandidate struct {
	Term          string    `json:"term"`
	Expansion     string    `json:"expansion"`
	Confirmations int       `json:"confirmations"`
	Source        string    `json:"source"` // "near_miss", "feedback", "cooccurrence"
	FirstSeen     time.Time `json:"first_seen"`
	LastConfirmed time.Time `json:"last_confirmed"`
	Examples      []string  `json:"examples"` // example prompt pairs that suggested this
}

// LearnedData is the JSON-persisted format for learned synonyms and key nouns.
type LearnedData struct {
	Synonyms   map[string]string   `json:"synonyms"`
	KeyNouns   map[string]bool     `json:"key_nouns"`
	Candidates []*SynonymCandidate `json:"candidates"`
	UpdatedAt  time.Time           `json:"updated_at"`
	Version    int                 `json:"version"`
}

// RegistryConfig configures the SynonymRegistry.
type RegistryConfig struct {
	DataDir            string        // directory to store learned data (default: "./data")
	PromotionThreshold int           // confirmations to promote (default: 3)
	StaleTimeout       time.Duration // candidate eviction timeout (default: 7 days)
	SaveInterval       time.Duration // persistence interval (default: 5 min)
}

// NewSynonymRegistry creates a registry with base synonyms and loads learned data.
func NewSynonymRegistry(cfg RegistryConfig) *SynonymRegistry {
	if cfg.DataDir == "" {
		cfg.DataDir = "./data"
	}
	if cfg.PromotionThreshold <= 0 {
		cfg.PromotionThreshold = 3
	}
	if cfg.StaleTimeout <= 0 {
		cfg.StaleTimeout = 7 * 24 * time.Hour
	}
	if cfg.SaveInterval <= 0 {
		cfg.SaveInterval = 5 * time.Minute
	}

	r := &SynonymRegistry{
		base:               getBaseSynonyms(),
		learned:            make(map[string]string),
		candidates:         make(map[string]*SynonymCandidate),
		baseKeyNouns:       getKeyNouns(),
		learnedKeyNouns:    make(map[string]bool),
		promotionThreshold: cfg.PromotionThreshold,
		staleTimeout:       cfg.StaleTimeout,
		dataDir:            cfg.DataDir,
		saveInterval:       cfg.SaveInterval,
		stopCh:             make(chan struct{}),
		doneCh:             make(chan struct{}),
	}

	// Load persisted data
	r.loadFromDisk()

	// Start background persistence and cleanup
	go r.backgroundLoop()

	return r
}

// Expand applies all synonym expansions (base + learned) to a text.
func (r *SynonymRegistry) Expand(text string) string {
	lower := strings.ToLower(text)
	expanded := lower

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Apply base synonyms
	for k, v := range r.base {
		if strings.Contains(lower, k) {
			expanded += " " + v
		}
	}

	// Apply learned synonyms
	for k, v := range r.learned {
		if strings.Contains(lower, k) {
			expanded += " " + v
		}
	}

	return expanded
}

// IsKeyNounDynamic checks both base and learned key nouns.
func (r *SynonymRegistry) IsKeyNounDynamic(word string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.baseKeyNouns[word] || r.learnedKeyNouns[word]
}

// HasDifferentKeyNounDynamic checks using both base and learned key nouns.
func (r *SynonymRegistry) HasDifferentKeyNounDynamic(text1, text2 string) bool {
	w1 := tokenizeWords(text1)
	w2 := tokenizeWords(text2)

	r.mu.RLock()
	defer r.mu.RUnlock()

	var kn1, kn2 []string
	for _, w := range w1 {
		if r.baseKeyNouns[w] || r.learnedKeyNouns[w] {
			kn1 = append(kn1, w)
		}
	}
	for _, w := range w2 {
		if r.baseKeyNouns[w] || r.learnedKeyNouns[w] {
			kn2 = append(kn2, w)
		}
	}

	if len(kn1) == 0 || len(kn2) == 0 {
		return false
	}

	hasUnique1, hasUnique2 := false, false
	for _, k := range kn1 {
		found := false
		for _, k2 := range kn2 {
			if k == k2 {
				found = true
				break
			}
		}
		if !found {
			hasUnique1 = true
			break
		}
	}
	for _, k := range kn2 {
		found := false
		for _, k1 := range kn1 {
			if k == k1 {
				found = true
				break
			}
		}
		if !found {
			hasUnique2 = true
			break
		}
	}

	return hasUnique1 && hasUnique2
}

// RecordNearMiss records a near-miss pair for synonym candidate mining.
// Called when cosine similarity is in the "almost" zone (e.g., 0.55-0.70).
func (r *SynonymRegistry) RecordNearMiss(query, cachedPrompt string, similarity float64) {
	qWords := tokenizeWords(query)
	cWords := tokenizeWords(cachedPrompt)

	qSet := make(map[string]bool)
	for _, w := range qWords {
		if len(w) > 2 {
			qSet[w] = true
		}
	}
	cSet := make(map[string]bool)
	for _, w := range cWords {
		if len(w) > 2 {
			cSet[w] = true
		}
	}

	// Find words unique to each side — these are synonym candidates
	var queryOnly, cachedOnly []string
	for w := range qSet {
		if !cSet[w] && !stopwords[w] {
			queryOnly = append(queryOnly, w)
		}
	}
	for w := range cSet {
		if !qSet[w] && !stopwords[w] {
			cachedOnly = append(cachedOnly, w)
		}
	}

	if len(queryOnly) == 0 || len(cachedOnly) == 0 {
		return
	}

	// Sort for deterministic expansion strings
	sort.Strings(cachedOnly)

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, qw := range queryOnly {
		expansion := strings.Join(cachedOnly, " ")
		example := query + " ↔ " + cachedPrompt

		if existing, ok := r.candidates[qw]; ok {
			existing.Confirmations++
			existing.LastConfirmed = time.Now()
			if len(existing.Examples) < 5 {
				existing.Examples = append(existing.Examples, example)
			}
		} else {
			r.candidates[qw] = &SynonymCandidate{
				Term:          qw,
				Expansion:     expansion,
				Confirmations: 1,
				Source:        "near_miss",
				FirstSeen:     time.Now(),
				LastConfirmed: time.Now(),
				Examples:      []string{example},
			}
		}

		// Auto-promote if enough confirmations
		if r.candidates[qw].Confirmations >= r.promotionThreshold {
			r.learned[qw] = r.candidates[qw].Expansion
			delete(r.candidates, qw)
		}
	}
}

// RecordFeedbackMiss records a user-reported false negative for synonym learning.
func (r *SynonymRegistry) RecordFeedbackMiss(query, expectedMatch string) {
	qWords := tokenizeWords(query)
	cWords := tokenizeWords(expectedMatch)

	qSet := make(map[string]bool)
	for _, w := range qWords {
		if len(w) > 2 {
			qSet[w] = true
		}
	}
	cSet := make(map[string]bool)
	for _, w := range cWords {
		if len(w) > 2 {
			cSet[w] = true
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for w := range qSet {
		if cSet[w] || stopwords[w] {
			continue
		}
		var otherSide []string
		for cw := range cSet {
			if !qSet[cw] {
				otherSide = append(otherSide, cw)
			}
		}
		if len(otherSide) == 0 {
			continue
		}

		sort.Strings(otherSide)
		expansion := strings.Join(otherSide, " ")
		example := query + " ↔ " + expectedMatch

		if existing, ok := r.candidates[w]; ok {
			// Feedback is a stronger signal: add 2 confirmations
			existing.Confirmations += 2
			existing.LastConfirmed = time.Now()
			existing.Source = "feedback"
			if len(existing.Examples) < 5 {
				existing.Examples = append(existing.Examples, example)
			}
		} else {
			r.candidates[w] = &SynonymCandidate{
				Term:          w,
				Expansion:     expansion,
				Confirmations: 2, // feedback counts double
				Source:        "feedback",
				FirstSeen:     time.Now(),
				LastConfirmed: time.Now(),
				Examples:      []string{example},
			}
		}

		// Auto-promote
		if r.candidates[w] != nil && r.candidates[w].Confirmations >= r.promotionThreshold {
			r.learned[w] = r.candidates[w].Expansion
			delete(r.candidates, w)
		}
	}
}

// RecordFalsePositive records a false positive for key noun learning.
func (r *SynonymRegistry) RecordFalsePositive(query, wrongMatch string) {
	qWords := tokenizeWords(query)
	cWords := tokenizeWords(wrongMatch)

	qSet := make(map[string]bool)
	for _, w := range qWords {
		qSet[w] = true
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Words unique to each side should become key nouns
	for _, w := range cWords {
		if !qSet[w] && len(w) > 3 && !r.baseKeyNouns[w] {
			r.learnedKeyNouns[w] = true
		}
	}
	for _, w := range qWords {
		if len(w) > 3 && !r.baseKeyNouns[w] {
			found := false
			for _, cw := range cWords {
				if cw == w {
					found = true
					break
				}
			}
			if !found {
				r.learnedKeyNouns[w] = true
			}
		}
	}
}

// Stats returns registry statistics.
func (r *SynonymRegistry) Stats() RegistryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return RegistryStats{
		BaseSynonyms:      len(r.base),
		LearnedSynonyms:   len(r.learned),
		CandidateSynonyms: len(r.candidates),
		TotalSynonyms:     len(r.base) + len(r.learned),
		BaseKeyNouns:      len(r.baseKeyNouns),
		LearnedKeyNouns:   len(r.learnedKeyNouns),
		TotalKeyNouns:     len(r.baseKeyNouns) + len(r.learnedKeyNouns),
	}
}

// RegistryStats provides statistics about the synonym registry.
type RegistryStats struct {
	BaseSynonyms      int `json:"base_synonyms"`
	LearnedSynonyms   int `json:"learned_synonyms"`
	CandidateSynonyms int `json:"candidate_synonyms"`
	TotalSynonyms     int `json:"total_synonyms"`
	BaseKeyNouns      int `json:"base_key_nouns"`
	LearnedKeyNouns   int `json:"learned_key_nouns"`
	TotalKeyNouns     int `json:"total_key_nouns"`
}

// GetCandidates returns current synonym candidates for review.
func (r *SynonymRegistry) GetCandidates() []*SynonymCandidate {
	r.mu.RLock()
	defer r.mu.RUnlock()

	candidates := make([]*SynonymCandidate, 0, len(r.candidates))
	for _, c := range r.candidates {
		cp := *c
		candidates = append(candidates, &cp)
	}

	// Sort by confirmations descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Confirmations > candidates[j].Confirmations
	})
	return candidates
}

// GetLearnedSynonyms returns all learned synonyms.
func (r *SynonymRegistry) GetLearnedSynonyms() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]string, len(r.learned))
	for k, v := range r.learned {
		result[k] = v
	}
	return result
}

// ManualPromote forces a candidate to be promoted to learned status.
func (r *SynonymRegistry) ManualPromote(term string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if c, ok := r.candidates[term]; ok {
		r.learned[term] = c.Expansion
		delete(r.candidates, term)
		return true
	}
	return false
}

// ManualAdd adds a synonym directly to the learned tier.
func (r *SynonymRegistry) ManualAdd(term, expansion string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.learned[strings.ToLower(term)] = strings.ToLower(expansion)
}

// ManualAddKeyNoun adds a key noun directly to the learned tier.
func (r *SynonymRegistry) ManualAddKeyNoun(noun string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.learnedKeyNouns[strings.ToLower(noun)] = true
}

// Stop stops the background persistence loop and waits for it to finish.
func (r *SynonymRegistry) Stop() {
	close(r.stopCh)
	<-r.doneCh
}

// backgroundLoop periodically saves to disk and cleans stale candidates.
func (r *SynonymRegistry) backgroundLoop() {
	defer close(r.doneCh)

	saveTicker := time.NewTicker(r.saveInterval)
	cleanTicker := time.NewTicker(1 * time.Hour)
	defer saveTicker.Stop()
	defer cleanTicker.Stop()

	for {
		select {
		case <-r.stopCh:
			r.saveToDisk() // final save on shutdown
			return
		case <-saveTicker.C:
			r.saveToDisk()
		case <-cleanTicker.C:
			r.cleanStaleCandidates()
		}
	}
}

// saveToDisk persists learned data to a JSON file.
func (r *SynonymRegistry) saveToDisk() {
	r.mu.RLock()
	data := LearnedData{
		Synonyms:  make(map[string]string, len(r.learned)),
		KeyNouns:  make(map[string]bool, len(r.learnedKeyNouns)),
		UpdatedAt: time.Now(),
		Version:   1,
	}
	for k, v := range r.learned {
		data.Synonyms[k] = v
	}
	for k, v := range r.learnedKeyNouns {
		data.KeyNouns[k] = v
	}
	data.Candidates = make([]*SynonymCandidate, 0, len(r.candidates))
	for _, c := range r.candidates {
		cp := *c
		data.Candidates = append(data.Candidates, &cp)
	}
	r.mu.RUnlock()

	// Ensure data directory exists
	if err := os.MkdirAll(r.dataDir, 0755); err != nil {
		log.Printf("[synonym-registry] failed to create data dir: %v", err)
		return
	}

	path := filepath.Join(r.dataDir, "learned_synonyms.json")
	tmpPath := path + ".tmp"

	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Printf("[synonym-registry] failed to marshal data: %v", err)
		return
	}

	// Write to temp file then rename (atomic on most filesystems).
	// On Windows, remove the target first since rename over existing file fails.
	if err := os.WriteFile(tmpPath, jsonBytes, 0644); err != nil {
		log.Printf("[synonym-registry] failed to write temp file: %v", err)
		return
	}
	_ = os.Remove(path) // ignore error if file doesn't exist yet
	if err := os.Rename(tmpPath, path); err != nil {
		log.Printf("[synonym-registry] failed to rename file: %v", err)
		return
	}
}

// loadFromDisk loads previously persisted learned data.
func (r *SynonymRegistry) loadFromDisk() {
	path := filepath.Join(r.dataDir, "learned_synonyms.json")

	jsonBytes, err := os.ReadFile(path)
	if err != nil {
		// File doesn't exist yet — that's fine for first run
		return
	}

	var data LearnedData
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		log.Printf("[synonym-registry] failed to parse learned data: %v", err)
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for k, v := range data.Synonyms {
		r.learned[k] = v
	}
	for k, v := range data.KeyNouns {
		r.learnedKeyNouns[k] = v
	}
	for _, c := range data.Candidates {
		r.candidates[c.Term] = c
	}

	log.Printf("[synonym-registry] loaded %d learned synonyms, %d key nouns, %d candidates",
		len(data.Synonyms), len(data.KeyNouns), len(data.Candidates))
}

// cleanStaleCandidates removes candidates that haven't been confirmed recently.
func (r *SynonymRegistry) cleanStaleCandidates() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for key, c := range r.candidates {
		if now.Sub(c.LastConfirmed) > r.staleTimeout {
			delete(r.candidates, key)
		}
	}
}

// getBaseSynonyms returns the compiled base synonym map.
// These are the same entries from the original expandSynonyms function.
func getBaseSynonyms() map[string]string {
	return map[string]string{
		// Original entries
		"k8s":           "kubernetes",
		"gc":            "garbage collection",
		"ci/cd":         "continuous integration continuous deployment",
		"ssl":           "tls https encryption",
		"tls":           "ssl https encryption",
		"db":            "database",
		"postgres":      "postgresql",
		"js":            "javascript",
		"ts":            "typescript",
		"py":            "python",
		"goroutine":     "go concurrency lightweight thread",
		"dockerfile":    "docker container image build",
		"throttling":    "rate limiting",
		"rate limiting": "throttling",
		"load balancer": "traffic distribution reverse proxy",
		"bcrypt":        "password hashing",
		"jwt":           "json web token authentication",
		"websocket":     "real-time bidirectional socket connection",
		"regex":         "regular expression pattern matching",
		"orm":           "object relational mapping database",
		"cdn":           "content delivery network caching",
		"dns":           "domain name system resolution nameserver",
		"ssh":           "secure shell remote access",
		"cors":          "cross origin resource sharing",
		"csrf":          "cross site request forgery",
		"xss":           "cross site scripting",
		"api":           "application programming interface endpoint",
		"sdk":           "software development kit library",
		"cli":           "command line interface terminal",
		"gui":           "graphical user interface",
		"crud":          "create read update delete operations",
		"mutex":         "mutual exclusion lock synchronization",
		"env":           "environment variable configuration",
		"repo":          "repository codebase",
		"pr":            "pull request code review",
		"ci":            "continuous integration build test",
		"cd":            "continuous deployment delivery release",
		"di":            "dependency injection inversion control",
		"hashmap":       "hash table dictionary map",
		"hash map":      "hash table dictionary",
		"hashtable":     "hash map dictionary",
		"hash table":    "hash map dictionary",
		// Go / concurrency
		"golang":      "go programming language",
		"goroutines":  "go concurrency lightweight thread",
		"concurrency": "parallel concurrent goroutine thread",
		"async":       "asynchronous non-blocking",
		"await":       "asynchronous promise",
		// Docker / Kubernetes
		"helm":    "kubernetes package manager chart",
		"ingress": "kubernetes networking traffic routing",
		// Cloud networking
		"vpc":  "virtual private cloud network",
		"iam":  "identity access management permissions",
		"s3":   "simple storage service object bucket aws",
		"ec2":  "elastic compute cloud instance aws",
		"rds":  "relational database service aws managed",
		"sqs":  "simple queue service aws messaging",
		"sns":  "simple notification service aws pubsub",
		"ecs":  "elastic container service aws docker",
		"eks":  "elastic kubernetes service aws",
		"gke":  "google kubernetes engine gcp",
		"aks":  "azure kubernetes service",
		"rbac": "role based access control permissions",
		"cidr": "classless inter domain routing network subnet",
		"nat":  "network address translation gateway",
		"vpn":  "virtual private network tunnel encrypted",
		"waf":  "web application firewall security",
		"ddos": "distributed denial of service attack",
		"mitm": "man in the middle attack security",
		// Database
		"sql injection": "sqli database attack vulnerability",
		"nosql":         "non-relational database document store",
		"acid":          "atomicity consistency isolation durability transaction",
		"cap theorem":   "consistency availability partition tolerance distributed",
		// Architecture patterns
		"cqrs": "command query responsibility segregation pattern",
		"ddd":  "domain driven design architecture",
		// Development methodologies
		"tdd": "test driven development methodology",
		"bdd": "behavior driven development testing",
		"mvp": "minimum viable product lean startup",
		// Cloud service models
		"saas": "software as a service cloud",
		"paas": "platform as a service cloud",
		"iaas": "infrastructure as a service cloud",
		"faas": "function as a service serverless",
		// Data engineering
		"etl": "extract transform load data pipeline",
		"elt": "extract load transform data pipeline",
		"dag": "directed acyclic graph workflow",
		// AI / ML
		"ml":  "machine learning artificial intelligence",
		"nlp": "natural language processing text",
		"llm": "large language model ai",
		"rag": "retrieval augmented generation",
		// Hardware
		"gpu": "graphics processing unit compute cuda",
		"cpu": "central processing unit processor",
		"ssd": "solid state drive storage",
		// Programming paradigms
		"oop": "object oriented programming classes",
		"fp":  "functional programming immutable",
		"ioc": "inversion of control dependency injection",
		"aop": "aspect oriented programming cross cutting",
		// Frontend
		"spa":  "single page application frontend",
		"ssr":  "server side rendering",
		"csr":  "client side rendering",
		"ssg":  "static site generation",
		"pwa":  "progressive web app offline",
		"wasm": "webassembly binary format browser",
	}
}
