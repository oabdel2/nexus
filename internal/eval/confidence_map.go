package eval

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// ConfidenceStats tracks running statistics for a task-type/tier combination.
type ConfidenceStats struct {
	TotalConfidence float64 `json:"total_confidence"`
	SampleCount     int     `json:"sample_count"`
}

// Average returns the mean confidence, or 0.0 if no samples.
func (cs ConfidenceStats) Average() float64 {
	if cs.SampleCount == 0 {
		return 0.0
	}
	return cs.TotalConfidence / float64(cs.SampleCount)
}

// LookupResult is returned by Lookup.
type LookupResult struct {
	AverageConfidence float64 `json:"average_confidence"`
	SampleCount       int     `json:"sample_count"`
	Found             bool    `json:"found"`
}

// ConfidenceMap tracks observed confidence scores per task_type and tier.
// It is thread-safe via sync.RWMutex.
type ConfidenceMap struct {
	mu   sync.RWMutex
	data map[string]map[string]*ConfidenceStats // task_type → tier → stats
}

// NewConfidenceMap creates an empty ConfidenceMap.
func NewConfidenceMap() *ConfidenceMap {
	return &ConfidenceMap{
		data: make(map[string]map[string]*ConfidenceStats),
	}
}

// Record adds a confidence observation for the given task type and tier.
func (cm *ConfidenceMap) Record(taskType, tier string, confidence float64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.data[taskType] == nil {
		cm.data[taskType] = make(map[string]*ConfidenceStats)
	}
	if cm.data[taskType][tier] == nil {
		cm.data[taskType][tier] = &ConfidenceStats{}
	}
	cm.data[taskType][tier].TotalConfidence += confidence
	cm.data[taskType][tier].SampleCount++
}

// Lookup returns the average confidence and sample count for a task type and tier.
func (cm *ConfidenceMap) Lookup(taskType, tier string) LookupResult {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	tiers, ok := cm.data[taskType]
	if !ok {
		return LookupResult{Found: false}
	}
	stats, ok := tiers[tier]
	if !ok {
		return LookupResult{Found: false}
	}
	return LookupResult{
		AverageConfidence: stats.Average(),
		SampleCount:       stats.SampleCount,
		Found:             true,
	}
}

// persistData is the JSON-serializable format.
type persistData struct {
	Data map[string]map[string]*ConfidenceStats `json:"data"`
}

// Save writes the confidence map to a JSON file.
func (cm *ConfidenceMap) Save(path string) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	pd := persistData{Data: cm.data}
	b, err := json.MarshalIndent(pd, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

// Load reads a confidence map from a JSON file.
func (cm *ConfidenceMap) Load(path string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var pd persistData
	if err := json.Unmarshal(b, &pd); err != nil {
		return err
	}
	cm.data = pd.Data
	return nil
}

// Task type classification keywords (replicated from internal/router/classifier.go).

var taskTypeKeywords = map[string][]string{
	"coding": {
		"implement", "code", "function", "class", "method", "variable",
		"compile", "build", "debug", "fix", "refactor", "optimize",
		"algorithm", "data structure", "api", "endpoint", "library",
		"package", "module", "import", "interface", "struct",
	},
	"analysis": {
		"analyze", "review", "compare", "evaluate", "assess",
		"investigate", "examine", "diagnose", "audit", "inspect",
		"performance", "security", "vulnerability", "architecture",
	},
	"creative": {
		"write", "create", "design", "generate", "compose",
		"draft", "brainstorm", "ideate", "suggest", "propose",
		"name", "story", "description",
	},
	"operational": {
		"deploy", "configure", "setup", "install", "migrate",
		"monitor", "scale", "backup", "restore", "maintain",
		"docker", "kubernetes", "ci", "cd", "pipeline",
	},
	"informational": {
		"explain", "describe", "summarize", "list", "define",
		"what is", "how does", "why", "tell me", "help",
		"documentation", "readme", "guide", "tutorial",
	},
}

// ClassifyTaskType determines the most likely task type from prompt content.
func ClassifyTaskType(prompt string) string {
	lower := strings.ToLower(prompt)
	bestType := "general"
	bestScore := 0

	for taskType, keywords := range taskTypeKeywords {
		score := 0
		for _, kw := range keywords {
			score += strings.Count(lower, kw)
		}
		if score > bestScore {
			bestScore = score
			bestType = taskType
		}
	}
	return bestType
}

// RecordFromPrompt auto-classifies the task type and records the observation.
func (cm *ConfidenceMap) RecordFromPrompt(prompt, tier string, confidence float64) {
	taskType := ClassifyTaskType(prompt)
	cm.Record(taskType, tier, confidence)
}

// TaskTypes returns the list of all observed task types.
func (cm *ConfidenceMap) TaskTypes() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	types := make([]string, 0, len(cm.data))
	for t := range cm.data {
		types = append(types, t)
	}
	return types
}
