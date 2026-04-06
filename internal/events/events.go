package events

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// EventType represents the type of event
type EventType string

const (
	RequestCompleted  EventType = "request.completed"
	RequestCached     EventType = "request.cached"
	RequestError      EventType = "request.error"
	BudgetWarning     EventType = "budget.warning"
	BudgetCritical    EventType = "budget.critical"
	BudgetExhausted   EventType = "budget.exhausted"
	WorkflowStarted   EventType = "workflow.started"
	WorkflowCompleted EventType = "workflow.completed"
	CostAnomaly       EventType = "cost.anomaly"
	TierDowngrade     EventType = "tier.downgrade"
	ProviderUnhealthy EventType = "provider.unhealthy"
	ProviderRecovered EventType = "provider.recovered"
)

// Event is the payload sent to webhook subscribers
type Event struct {
	ID        string                 `json:"id"`
	Type      EventType              `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// WebhookConfig defines a webhook subscription
type WebhookConfig struct {
	URL     string            `yaml:"url" json:"url"`
	Events  []string          `yaml:"events" json:"events"` // event types or "*" for all
	Secret  string            `yaml:"secret" json:"-"`
	Headers map[string]string `yaml:"headers" json:"-"`
}

// EventBus manages event dispatch to webhook subscribers
type EventBus struct {
	mu      sync.RWMutex
	hooks   []WebhookConfig
	client  *http.Client
	queue   chan Event
	counter uint64

	// Recent events ring buffer for /api/events/recent
	recentMu sync.RWMutex
	recent   []Event

	// Stats counters for /api/events/stats
	statsMu sync.RWMutex
	stats   map[EventType]int64
}

// NewEventBus creates a new event bus with the given webhook configs
func NewEventBus(hooks []WebhookConfig) *EventBus {
	eb := &EventBus{
		hooks: hooks,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		queue:  make(chan Event, 1000),
		recent: make([]Event, 0, 100),
		stats:  make(map[EventType]int64),
	}
	// Start worker pool for async dispatch
	for i := 0; i < 3; i++ {
		go eb.worker()
	}
	return eb
}

// Emit sends an event to all matching webhook subscribers
func (eb *EventBus) Emit(eventType EventType, data map[string]interface{}) {
	eb.mu.Lock()
	eb.counter++
	id := fmt.Sprintf("evt_%d_%d", time.Now().UnixNano(), eb.counter)
	eb.mu.Unlock()

	event := Event{
		ID:        id,
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		Data:      data,
	}

	// Track stats
	eb.statsMu.Lock()
	eb.stats[eventType]++
	eb.statsMu.Unlock()

	// Track recent events (ring buffer, max 100)
	eb.recentMu.Lock()
	if len(eb.recent) >= 100 {
		eb.recent = eb.recent[1:]
	}
	eb.recent = append(eb.recent, event)
	eb.recentMu.Unlock()

	select {
	case eb.queue <- event:
	default:
		log.Printf("[events] queue full, dropping event %s", id)
	}
}

func (eb *EventBus) worker() {
	for event := range eb.queue {
		eb.dispatch(event)
	}
}

func (eb *EventBus) dispatch(event Event) {
	eb.mu.RLock()
	hooks := make([]WebhookConfig, len(eb.hooks))
	copy(hooks, eb.hooks)
	eb.mu.RUnlock()

	for _, hook := range hooks {
		if !eb.matches(hook, event.Type) {
			continue
		}
		go eb.send(hook, event)
	}
}

func (eb *EventBus) matches(hook WebhookConfig, eventType EventType) bool {
	for _, e := range hook.Events {
		if e == "*" || e == string(eventType) {
			return true
		}
	}
	return false
}

func (eb *EventBus) send(hook WebhookConfig, event Event) {
	body, err := json.Marshal(event)
	if err != nil {
		log.Printf("[events] marshal error: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", hook.URL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[events] request error: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Nexus-Event", string(event.Type))
	req.Header.Set("X-Nexus-Event-ID", event.ID)

	// Add HMAC signature if secret is configured
	if hook.Secret != "" {
		mac := hmac.New(sha256.New, []byte(hook.Secret))
		mac.Write(body)
		sig := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Nexus-Signature", "sha256="+sig)
	}

	// Add custom headers
	for k, v := range hook.Headers {
		req.Header.Set(k, v)
	}

	resp, err := eb.client.Do(req)
	if err != nil {
		log.Printf("[events] webhook %s failed: %v", hook.URL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Printf("[events] webhook %s returned %d", hook.URL, resp.StatusCode)
	}
}

// Recent returns the last 100 events.
func (eb *EventBus) Recent() []Event {
	eb.recentMu.RLock()
	defer eb.recentMu.RUnlock()
	out := make([]Event, len(eb.recent))
	copy(out, eb.recent)
	return out
}

// Stats returns event counts by type.
func (eb *EventBus) Stats() map[string]int64 {
	eb.statsMu.RLock()
	defer eb.statsMu.RUnlock()
	out := make(map[string]int64, len(eb.stats))
	for k, v := range eb.stats {
		out[string(k)] = v
	}
	return out
}

// Close shuts down the event bus
func (eb *EventBus) Close() {
	close(eb.queue)
}
