package billing

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Plan defines a billing plan with quotas and limits.
type Plan struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MaxRequests  int64  `json:"max_requests"`
	MaxRPM       int    `json:"max_rpm"`
	MaxDevices   int    `json:"max_devices"`
	PriceMonthly int    `json:"price_monthly"`
}

// DefaultPlans returns the built-in plan definitions.
func DefaultPlans() map[string]Plan {
	return map[string]Plan{
		"free": {
			ID: "free", Name: "Free",
			MaxRequests: 1000, MaxRPM: 10, MaxDevices: 1, PriceMonthly: 0,
		},
		"starter": {
			ID: "starter", Name: "Starter",
			MaxRequests: 50000, MaxRPM: 60, MaxDevices: 3, PriceMonthly: 2900,
		},
		"team": {
			ID: "team", Name: "Team",
			MaxRequests: 500000, MaxRPM: 300, MaxDevices: 10, PriceMonthly: 9900,
		},
		"enterprise": {
			ID: "enterprise", Name: "Enterprise",
			MaxRequests: 0, MaxRPM: 0, MaxDevices: 0, PriceMonthly: 0,
		},
	}
}

// Subscription represents a user's billing subscription.
type Subscription struct {
	ID                 string     `json:"id"`
	UserID             string     `json:"user_id"`
	Email              string     `json:"email"`
	PlanID             string     `json:"plan_id"`
	Status             string     `json:"status"` // active, past_due, canceled, expired, trialing
	StripeCustomerID   string     `json:"stripe_customer_id"`
	StripeSubID        string     `json:"stripe_sub_id"`
	CurrentPeriodStart time.Time  `json:"current_period_start"`
	CurrentPeriodEnd   time.Time  `json:"current_period_end"`
	CanceledAt         *time.Time `json:"canceled_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// SubscriptionEvent is emitted when subscription state changes.
type SubscriptionEvent struct {
	Type         string
	Subscription *Subscription
}

// SubscriptionStore manages subscription persistence and lifecycle.
type SubscriptionStore struct {
	mu       sync.RWMutex
	subs     map[string]*Subscription // keyed by ID
	dataDir  string
	plans    map[string]Plan
	logger   *slog.Logger
	eventCh  chan SubscriptionEvent
	stopCh   chan struct{}
}

// NewSubscriptionStore creates a new store, loading existing data from disk.
func NewSubscriptionStore(dataDir string, logger *slog.Logger) *SubscriptionStore {
	s := &SubscriptionStore{
		subs:    make(map[string]*Subscription),
		dataDir: dataDir,
		plans:   DefaultPlans(),
		logger:  logger,
		eventCh: make(chan SubscriptionEvent, 100),
		stopCh:  make(chan struct{}),
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		logger.Error("failed to create billing data dir", "error", err)
	}
	_ = s.load()
	return s
}

// EventChannel returns the channel where subscription events are published.
func (s *SubscriptionStore) EventChannel() <-chan SubscriptionEvent {
	return s.eventCh
}

// GetPlan returns a plan by ID.
func (s *SubscriptionStore) GetPlan(id string) (Plan, bool) {
	p, ok := s.plans[id]
	return p, ok
}

// Create adds a new subscription.
func (s *SubscriptionStore) Create(sub *Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.subs[sub.ID]; exists {
		return fmt.Errorf("subscription %s already exists", sub.ID)
	}
	now := time.Now()
	sub.CreatedAt = now
	sub.UpdatedAt = now
	s.subs[sub.ID] = sub
	s.emitEvent("created", sub)
	return s.saveLocked()
}

// Get returns a subscription by ID.
func (s *SubscriptionStore) Get(id string) (*Subscription, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sub, ok := s.subs[id]
	if !ok {
		return nil, false
	}
	copy := *sub
	return &copy, true
}

// GetByUserID returns the subscription for a given user.
func (s *SubscriptionStore) GetByUserID(userID string) (*Subscription, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sub := range s.subs {
		if sub.UserID == userID {
			copy := *sub
			return &copy, true
		}
	}
	return nil, false
}

// GetByStripeSubID returns the subscription matching a Stripe subscription ID.
func (s *SubscriptionStore) GetByStripeSubID(stripeSubID string) (*Subscription, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sub := range s.subs {
		if sub.StripeSubID == stripeSubID {
			copy := *sub
			return &copy, true
		}
	}
	return nil, false
}

// GetByStripeCustomerID returns the subscription for a Stripe customer.
func (s *SubscriptionStore) GetByStripeCustomerID(customerID string) (*Subscription, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sub := range s.subs {
		if sub.StripeCustomerID == customerID {
			copy := *sub
			return &copy, true
		}
	}
	return nil, false
}

// Update replaces a subscription record.
func (s *SubscriptionStore) Update(sub *Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.subs[sub.ID]; !exists {
		return fmt.Errorf("subscription %s not found", sub.ID)
	}
	sub.UpdatedAt = time.Now()
	s.subs[sub.ID] = sub
	s.emitEvent("updated", sub)
	return s.saveLocked()
}

// ListAll returns all subscriptions.
func (s *SubscriptionStore) ListAll() []*Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Subscription, 0, len(s.subs))
	for _, sub := range s.subs {
		copy := *sub
		result = append(result, &copy)
	}
	return result
}

// GetExpiring returns subscriptions ending within the given duration.
func (s *SubscriptionStore) GetExpiring(within time.Duration) []*Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cutoff := time.Now().Add(within)
	now := time.Now()
	var result []*Subscription
	for _, sub := range s.subs {
		if sub.Status == "active" && sub.CurrentPeriodEnd.After(now) && sub.CurrentPeriodEnd.Before(cutoff) {
			copy := *sub
			result = append(result, &copy)
		}
	}
	return result
}

// GetExpired returns subscriptions past their period end that are still active.
func (s *SubscriptionStore) GetExpired() []*Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	var result []*Subscription
	for _, sub := range s.subs {
		if sub.Status == "active" && sub.CurrentPeriodEnd.Before(now) {
			copy := *sub
			result = append(result, &copy)
		}
	}
	return result
}

// StartLifecycleChecker runs a background goroutine that checks for
// expiring and expired subscriptions every hour.
func (s *SubscriptionStore) StartLifecycleChecker() {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.checkLifecycle()
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop shuts down the background lifecycle checker.
func (s *SubscriptionStore) Stop() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
}

func (s *SubscriptionStore) checkLifecycle() {
	// Check expiring within 7, 3, 1 days
	for _, d := range []time.Duration{7 * 24 * time.Hour, 3 * 24 * time.Hour, 24 * time.Hour} {
		for _, sub := range s.GetExpiring(d) {
			s.emitEvent("expiring", sub)
		}
	}

	// Mark truly expired
	for _, sub := range s.GetExpired() {
		s.mu.Lock()
		if stored, ok := s.subs[sub.ID]; ok && stored.Status == "active" {
			stored.Status = "expired"
			stored.UpdatedAt = time.Now()
			s.emitEvent("expired", stored)
			_ = s.saveLocked()
		}
		s.mu.Unlock()
	}
}

func (s *SubscriptionStore) emitEvent(eventType string, sub *Subscription) {
	copy := *sub
	select {
	case s.eventCh <- SubscriptionEvent{Type: eventType, Subscription: &copy}:
	default:
		s.logger.Warn("subscription event channel full, dropping event", "type", eventType, "sub_id", sub.ID)
	}
}

func (s *SubscriptionStore) load() error {
	path := filepath.Join(s.dataDir, "subscriptions.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var subs []*Subscription
	if err := json.Unmarshal(data, &subs); err != nil {
		return err
	}
	for _, sub := range subs {
		s.subs[sub.ID] = sub
	}
	return nil
}

func (s *SubscriptionStore) saveLocked() error {
	subs := make([]*Subscription, 0, len(s.subs))
	for _, sub := range s.subs {
		subs = append(subs, sub)
	}
	data, err := json.MarshalIndent(subs, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.dataDir, "subscriptions.json"), data)
}

// atomicWrite writes data to a temp file then renames it into place.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp := filepath.Join(dir, ".tmp_"+filepath.Base(path))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
