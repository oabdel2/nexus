package auth

import (
	"crypto/subtle"
	"fmt"
	"sync"
	"time"
)

// APIKey represents a tenant's API key with associated policies
type APIKey struct {
	Key           string   `yaml:"key" json:"-"`
	Name          string   `yaml:"name" json:"name"`
	Team          string   `yaml:"team" json:"team"`
	MonthlyBudget float64  `yaml:"monthly_budget" json:"monthly_budget"`
	AlertAt       float64  `yaml:"alert_at" json:"alert_at"` // fraction (0.8 = 80%)
	RPM           int      `yaml:"rpm" json:"rpm"`            // requests per minute
	AllowedTiers  []string `yaml:"allowed_tiers" json:"allowed_tiers"`
	AllowedModels []string `yaml:"allowed_models" json:"allowed_models"`
	Enabled       bool     `yaml:"enabled" json:"enabled"`
}

// KeyManager handles API key validation and per-key tracking
type KeyManager struct {
	mu      sync.RWMutex
	keys    map[string]*APIKey   // key string → APIKey
	usage   map[string]*KeyUsage // key string → usage tracking
	enabled bool
}

// KeyUsage tracks per-key consumption
type KeyUsage struct {
	RequestCount   int64
	TotalCost      float64
	MonthStarted   time.Time
	MinuteRequests []time.Time // sliding window for RPM
}

// NewKeyManager creates a key manager from config
func NewKeyManager(keys []APIKey) *KeyManager {
	km := &KeyManager{
		keys:    make(map[string]*APIKey),
		usage:   make(map[string]*KeyUsage),
		enabled: len(keys) > 0,
	}
	for i := range keys {
		k := &keys[i]
		if !k.Enabled {
			continue
		}
		km.keys[k.Key] = k
		km.usage[k.Key] = &KeyUsage{
			MonthStarted:   time.Now().Truncate(24 * time.Hour),
			MinuteRequests: make([]time.Time, 0, 100),
		}
	}
	return km
}

// Validate checks if an API key is valid and returns its config.
// Returns nil if auth is disabled (no keys configured).
func (km *KeyManager) Validate(key string) (*APIKey, error) {
	if !km.enabled {
		return nil, nil // auth disabled, allow all
	}
	if key == "" {
		return nil, fmt.Errorf("API key required")
	}

	km.mu.RLock()
	defer km.mu.RUnlock()

	for storedKey, config := range km.keys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(storedKey)) == 1 {
			return config, nil
		}
	}
	return nil, fmt.Errorf("invalid API key")
}

// CheckRateLimit returns true if the key is within its RPM limit
func (km *KeyManager) CheckRateLimit(key string) bool {
	km.mu.Lock()
	defer km.mu.Unlock()

	usage, ok := km.usage[key]
	if !ok {
		return true
	}

	config := km.keys[key]
	if config == nil || config.RPM == 0 {
		return true
	}

	// Clean old entries outside the 1-minute window
	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)
	clean := make([]time.Time, 0, len(usage.MinuteRequests))
	for _, t := range usage.MinuteRequests {
		if t.After(cutoff) {
			clean = append(clean, t)
		}
	}
	usage.MinuteRequests = clean

	if len(usage.MinuteRequests) >= config.RPM {
		return false
	}

	usage.MinuteRequests = append(usage.MinuteRequests, now)
	return true
}

// CheckBudget returns true if the key is within its monthly budget
func (km *KeyManager) CheckBudget(key string) (ok bool, remaining float64) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	config := km.keys[key]
	usage := km.usage[key]
	if config == nil || usage == nil || config.MonthlyBudget == 0 {
		return true, -1 // unlimited
	}

	// Reset monthly tracking if new month
	now := time.Now()
	if now.Month() != usage.MonthStarted.Month() || now.Year() != usage.MonthStarted.Year() {
		usage.TotalCost = 0
		usage.MonthStarted = now.Truncate(24 * time.Hour)
	}

	remaining = config.MonthlyBudget - usage.TotalCost
	return remaining > 0, remaining
}

// RecordUsage records a request's cost against the key
func (km *KeyManager) RecordUsage(key string, cost float64) {
	km.mu.Lock()
	defer km.mu.Unlock()

	usage, ok := km.usage[key]
	if !ok {
		return
	}
	usage.RequestCount++
	usage.TotalCost += cost
}

// IsTierAllowed checks if a key is allowed to use a specific tier
func (km *KeyManager) IsTierAllowed(key, tier string) bool {
	km.mu.RLock()
	defer km.mu.RUnlock()

	config := km.keys[key]
	if config == nil || len(config.AllowedTiers) == 0 {
		return true // no restrictions
	}
	for _, t := range config.AllowedTiers {
		if t == tier {
			return true
		}
	}
	return false
}

// GetUsageReport returns usage stats for a key
func (km *KeyManager) GetUsageReport(key string) map[string]interface{} {
	km.mu.RLock()
	defer km.mu.RUnlock()

	config := km.keys[key]
	usage := km.usage[key]
	if config == nil || usage == nil {
		return nil
	}

	report := map[string]interface{}{
		"name":           config.Name,
		"team":           config.Team,
		"requests":       usage.RequestCount,
		"total_cost":     usage.TotalCost,
		"monthly_budget": config.MonthlyBudget,
		"budget_used":    0.0,
	}
	if config.MonthlyBudget > 0 {
		report["budget_used"] = usage.TotalCost / config.MonthlyBudget
	}
	return report
}

// ListKeys returns all configured keys (without the actual key values)
func (km *KeyManager) ListKeys() []map[string]interface{} {
	km.mu.RLock()
	defer km.mu.RUnlock()

	result := make([]map[string]interface{}, 0, len(km.keys))
	for key, config := range km.keys {
		usage := km.usage[key]
		entry := map[string]interface{}{
			"name":           config.Name,
			"team":           config.Team,
			"enabled":        config.Enabled,
			"monthly_budget": config.MonthlyBudget,
		}
		if usage != nil {
			entry["requests"] = usage.RequestCount
			entry["total_cost"] = usage.TotalCost
		}
		result = append(result, entry)
	}
	return result
}
