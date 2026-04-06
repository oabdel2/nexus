package billing

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// APIKey represents a managed API key tied to a subscription.
type APIKey struct {
	Key            string     `json:"-"`                       // raw key, never persisted
	KeyHash        string     `json:"key_hash"`                // SHA-256 hex
	KeyPrefix      string     `json:"key_prefix"`              // first 12 chars for identification
	UserID         string     `json:"user_id"`
	SubscriptionID string     `json:"subscription_id"`
	Name           string     `json:"name"`
	Scopes         []string   `json:"scopes"`
	IsTest         bool       `json:"is_test"`
	Active         bool       `json:"active"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	RequestCount   int64      `json:"request_count"`
	MonthlyUsage   int64      `json:"monthly_usage"`
	MonthlyReset   time.Time  `json:"monthly_reset"`
}

// QuotaResult contains the result of a quota check.
type QuotaResult struct {
	Allowed   bool      `json:"allowed"`
	Remaining int64     `json:"remaining"`
	ResetAt   time.Time `json:"reset_at"`
}

// APIKeyStore manages API keys with thread-safe operations.
type APIKeyStore struct {
	mu       sync.RWMutex
	keys     map[string]*APIKey // keyed by KeyHash
	dataDir  string
	subStore *SubscriptionStore
	logger   *slog.Logger
}

// NewAPIKeyStore creates a new API key store.
func NewAPIKeyStore(dataDir string, subStore *SubscriptionStore, logger *slog.Logger) *APIKeyStore {
	s := &APIKeyStore{
		keys:     make(map[string]*APIKey),
		dataDir:  dataDir,
		subStore: subStore,
		logger:   logger,
	}
	_ = s.load()
	return s
}

// GenerateKey creates a new API key for a user/subscription.
// Returns the raw key (shown once) and the stored key record.
func (s *APIKeyStore) GenerateKey(userID, subID string, name string, scopes []string, isTest bool) (*APIKey, string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, "", fmt.Errorf("failed to generate key: %w", err)
	}
	hexPart := hex.EncodeToString(b) // 32 hex chars

	prefix := "nxs_live_"
	if isTest {
		prefix = "nxs_test_"
	}
	rawKey := prefix + hexPart

	hash := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(hash[:])

	if scopes == nil {
		scopes = []string{"chat"}
	}

	now := time.Now()
	key := &APIKey{
		KeyHash:        keyHash,
		KeyPrefix:      rawKey[:12],
		UserID:         userID,
		SubscriptionID: subID,
		Name:           name,
		Scopes:         scopes,
		IsTest:         isTest,
		Active:         true,
		CreatedAt:      now,
		MonthlyReset:   now.AddDate(0, 1, 0),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[keyHash] = key
	if err := s.saveLocked(); err != nil {
		return nil, "", err
	}

	return key, rawKey, nil
}

// HashKey computes the SHA-256 hash of a raw API key.
func HashKey(rawKey string) string {
	hash := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(hash[:])
}

// ValidateKey validates a raw API key and returns the key record.
// Checks: exists, active, not expired, subscription active.
func (s *APIKeyStore) ValidateKey(rawKey string) (*APIKey, error) {
	computedHash := HashKey(rawKey)

	s.mu.RLock()
	var key *APIKey
	for _, k := range s.keys {
		if subtle.ConstantTimeCompare([]byte(computedHash), []byte(k.KeyHash)) == 1 {
			key = k
		}
	}
	s.mu.RUnlock()

	if key == nil {
		return nil, fmt.Errorf("invalid API key")
	}
	if !key.Active {
		return nil, fmt.Errorf("API key has been revoked")
	}
	if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
		return nil, fmt.Errorf("API key has expired")
	}

	// Check subscription is active
	if s.subStore != nil {
		sub, found := s.subStore.Get(key.SubscriptionID)
		if !found {
			return nil, fmt.Errorf("subscription not found")
		}
		if sub.Status != "active" && sub.Status != "trialing" {
			return nil, fmt.Errorf("subscription is %s", sub.Status)
		}
	}

	copy := *key
	return &copy, nil
}

// RevokeKey disables a key by its hash.
func (s *APIKeyStore) RevokeKey(keyHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.keys[keyHash]
	if !ok {
		return fmt.Errorf("key not found")
	}
	key.Active = false
	return s.saveLocked()
}

// RevokeBySubscription disables all keys for a subscription.
func (s *APIKeyStore) RevokeBySubscription(subID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range s.keys {
		if key.SubscriptionID == subID && key.Active {
			key.Active = false
		}
	}
	return s.saveLocked()
}

// RecordUsage increments the usage counters for a key.
func (s *APIKeyStore) RecordUsage(keyHash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.keys[keyHash]
	if !ok {
		return
	}
	now := time.Now()
	key.RequestCount++
	key.MonthlyUsage++
	key.LastUsedAt = &now

	// Periodic save (every 100 requests to reduce I/O)
	if key.RequestCount%100 == 0 {
		_ = s.saveLocked()
	}
}

// CheckQuota verifies whether a key is within its subscription quota.
func (s *APIKeyStore) CheckQuota(keyHash string) QuotaResult {
	s.mu.RLock()
	key, ok := s.keys[keyHash]
	s.mu.RUnlock()

	if !ok {
		return QuotaResult{Allowed: false}
	}

	// Check if monthly reset is due
	if time.Now().After(key.MonthlyReset) {
		s.mu.Lock()
		key.MonthlyUsage = 0
		key.MonthlyReset = time.Now().AddDate(0, 1, 0)
		s.mu.Unlock()
	}

	// Get plan limits
	if s.subStore == nil {
		return QuotaResult{Allowed: true, Remaining: -1, ResetAt: key.MonthlyReset}
	}

	sub, found := s.subStore.Get(key.SubscriptionID)
	if !found {
		return QuotaResult{Allowed: false}
	}

	plan, ok := s.subStore.GetPlan(sub.PlanID)
	if !ok {
		return QuotaResult{Allowed: false}
	}

	// Unlimited plan
	if plan.MaxRequests == 0 {
		return QuotaResult{Allowed: true, Remaining: -1, ResetAt: key.MonthlyReset}
	}

	remaining := plan.MaxRequests - key.MonthlyUsage
	return QuotaResult{
		Allowed:   remaining > 0,
		Remaining: remaining,
		ResetAt:   key.MonthlyReset,
	}
}

// ResetMonthlyUsage resets monthly counters for all keys.
func (s *APIKeyStore) ResetMonthlyUsage() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, key := range s.keys {
		key.MonthlyUsage = 0
		key.MonthlyReset = now.AddDate(0, 1, 0)
	}
	_ = s.saveLocked()
}

// ResetMonthlyUsageBySubscription resets counters for a specific subscription's keys.
func (s *APIKeyStore) ResetMonthlyUsageBySubscription(subID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, key := range s.keys {
		if key.SubscriptionID == subID {
			key.MonthlyUsage = 0
			key.MonthlyReset = now.AddDate(0, 1, 0)
		}
	}
	_ = s.saveLocked()
}

// ListByUser returns all keys belonging to a user.
func (s *APIKeyStore) ListByUser(userID string) []*APIKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*APIKey
	for _, key := range s.keys {
		if key.UserID == userID {
			copy := *key
			result = append(result, &copy)
		}
	}
	return result
}

// GetByHash returns a key by its hash.
func (s *APIKeyStore) GetByHash(keyHash string) (*APIKey, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key, ok := s.keys[keyHash]
	if !ok {
		return nil, false
	}
	copy := *key
	return &copy, true
}

// Save forces a persist to disk.
func (s *APIKeyStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *APIKeyStore) load() error {
	path := filepath.Join(s.dataDir, "apikeys.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var keys []*APIKey
	if err := json.Unmarshal(data, &keys); err != nil {
		return err
	}
	for _, key := range keys {
		s.keys[key.KeyHash] = key
	}
	return nil
}

func (s *APIKeyStore) saveLocked() error {
	keys := make([]*APIKey, 0, len(s.keys))
	for _, key := range s.keys {
		keys = append(keys, key)
	}
	data, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.dataDir, "apikeys.json"), data)
}
