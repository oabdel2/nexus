package dashboard

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

//go:embed index.html
var content embed.FS

// Handler returns an http.Handler that serves the embedded dashboard HTML.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := content.ReadFile("index.html")
		if err != nil {
			http.Error(w, "dashboard not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(data)
	})
}

// RequestEvent represents a single routed request for the dashboard feed.
type RequestEvent struct {
	Timestamp       time.Time `json:"timestamp"`
	WorkflowID      string    `json:"workflow_id"`
	Step            int       `json:"step"`
	ComplexityScore float64   `json:"complexity_score"`
	TierSelected    string    `json:"tier_selected"`
	ModelUsed       string    `json:"model_used"`
	LatencyMs       int64     `json:"latency_ms"`
	Cost            float64   `json:"cost"`
	CacheHit        bool      `json:"cache_hit"`
	Provider        string    `json:"provider,omitempty"`
}

// WorkflowBudget represents a workflow's current budget status.
type WorkflowBudget struct {
	ID           string  `json:"id"`
	Budget       float64 `json:"budget"`
	BudgetLeft   float64 `json:"budget_left"`
	BudgetRatio  float64 `json:"budget_ratio"`
	CurrentStep  int     `json:"current_step"`
	TotalCost    float64 `json:"total_cost"`
}

// ProviderStat holds per-provider aggregate stats for the dashboard.
type ProviderStat struct {
	Name         string  `json:"name"`
	Requests     int64   `json:"requests"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	TotalLatency int64   `json:"-"`
}

// AggregateStats holds the dashboard's aggregate counters.
type AggregateStats struct {
	TotalRequests           int64   `json:"total_requests"`
	CacheHits               int64   `json:"cache_hits"`
	CacheHitRate            float64 `json:"cache_hit_rate"`
	TotalCost               float64 `json:"total_cost"`
	TotalSavings            float64 `json:"total_savings"`
	AvgLatency              float64 `json:"avg_latency"`
	EconomyCount            int64   `json:"economy_count"`
	CheapCount              int64   `json:"cheap_count"`
	MidCount                int64   `json:"mid_count"`
	PremiumCount            int64   `json:"premium_count"`
	CachedCount             int64   `json:"cached_count"`
	CascadeAttempts         int64   `json:"cascade_attempts"`
	CascadeAccepted         int64   `json:"cascade_accepted"`
	CascadeAcceptRate       float64 `json:"cascade_accept_rate"`
	CompressionTokensSaved  int64   `json:"compression_tokens_saved"`
}

// DashboardUpdate is the SSE payload sent to the browser.
type DashboardUpdate struct {
	Type           string           `json:"type"` // "request", "stats", "init"
	Request        *RequestEvent    `json:"request,omitempty"`
	Stats          *AggregateStats  `json:"stats,omitempty"`
	Workflows      []WorkflowBudget `json:"workflows,omitempty"`
	RecentRequests []RequestEvent   `json:"recent_requests,omitempty"`
	ProviderStats  []ProviderStat   `json:"provider_stats,omitempty"`
}

// premiumCostPerToken is used to estimate "what you would have paid" at premium tier.
const premiumCostPerToken = 0.03

// EventBus collects request events and streams them to SSE clients.
type EventBus struct {
	mu             sync.RWMutex
	recentRequests []RequestEvent
	workflows      map[string]*WorkflowBudget

	// aggregates
	totalRequests  int64
	cacheHits      int64
	totalCost      float64
	totalSavings   float64
	totalLatency   int64
	economyCount   int64
	cheapCount     int64
	midCount       int64
	premiumCount   int64
	cachedCount    int64

	// cascade tracking
	cascadeAttempts  int64
	cascadeAccepted  int64

	// compression tracking
	compressionTokensSaved int64

	// per-provider stats
	providerStats map[string]*ProviderStat

	// SSE subscribers
	subMu       sync.Mutex
	subscribers map[chan []byte]struct{}
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		recentRequests: make([]RequestEvent, 0, 100),
		workflows:      make(map[string]*WorkflowBudget),
		providerStats:  make(map[string]*ProviderStat),
		subscribers:    make(map[chan []byte]struct{}),
	}
}

// Push records a new request event and broadcasts it to all SSE clients.
func (eb *EventBus) Push(evt RequestEvent) {
	eb.mu.Lock()

	// Add to recent requests ring buffer (keep last 100)
	if len(eb.recentRequests) >= 100 {
		eb.recentRequests = eb.recentRequests[1:]
	}
	eb.recentRequests = append(eb.recentRequests, evt)

	// Update aggregates
	eb.totalRequests++
	if evt.CacheHit {
		eb.cacheHits++
	}
	eb.totalCost += evt.Cost
	eb.totalLatency += evt.LatencyMs

	// Estimate savings: premium cost minus actual cost
	// Use a rough estimate of what premium would cost
	premiumEstimate := evt.Cost * 3.0 // rough 3x multiplier for premium vs actual
	if evt.CacheHit {
		premiumEstimate = 0.005 // cache hit would have cost ~$0.005 at premium
	}
	switch evt.TierSelected {
	case "premium":
		premiumEstimate = evt.Cost // no savings
	case "mid":
		premiumEstimate = evt.Cost * 1.8
	case "cheap":
		premiumEstimate = evt.Cost * 3.0
	case "economy":
		premiumEstimate = evt.Cost * 5.0
	case "cached":
		premiumEstimate = 0.005
	}
	eb.totalSavings += premiumEstimate - evt.Cost

	// Per-tier counts
	switch evt.TierSelected {
	case "economy":
		eb.economyCount++
	case "cheap":
		eb.cheapCount++
	case "mid":
		eb.midCount++
	case "premium":
		eb.premiumCount++
	case "cached":
		eb.cachedCount++
	}

	// Per-provider stats
	provName := evt.Provider
	if provName == "" {
		provName = "unknown"
	}
	ps, ok := eb.providerStats[provName]
	if !ok {
		ps = &ProviderStat{Name: provName}
		eb.providerStats[provName] = ps
	}
	ps.Requests++
	ps.TotalLatency += evt.LatencyMs
	if ps.Requests > 0 {
		ps.AvgLatencyMs = float64(ps.TotalLatency) / float64(ps.Requests)
	}

	// Update workflow budget
	eb.workflows[evt.WorkflowID] = &WorkflowBudget{
		ID: evt.WorkflowID,
	}

	stats := eb.getStatsLocked()
	workflows := eb.getWorkflowsLocked()
	provStats := eb.getProviderStatsLocked()
	eb.mu.Unlock()

	// Build SSE message
	update := DashboardUpdate{
		Type:          "request",
		Request:       &evt,
		Stats:         stats,
		Workflows:     workflows,
		ProviderStats: provStats,
	}
	data, err := json.Marshal(update)
	if err != nil {
		return
	}

	eb.broadcast(data)
}

// RecordCascade records a cascade attempt result.
func (eb *EventBus) RecordCascade(accepted bool) {
	eb.mu.Lock()
	eb.cascadeAttempts++
	if accepted {
		eb.cascadeAccepted++
	}
	eb.mu.Unlock()
}

// RecordCompressionSaved records tokens saved by compression.
func (eb *EventBus) RecordCompressionSaved(tokens int) {
	eb.mu.Lock()
	eb.compressionTokensSaved += int64(tokens)
	eb.mu.Unlock()
}

// UpdateWorkflow updates the budget status for a workflow.
func (eb *EventBus) UpdateWorkflow(id string, budget, budgetLeft float64, budgetRatio float64, currentStep int, totalCost float64) {
	eb.mu.Lock()
	eb.workflows[id] = &WorkflowBudget{
		ID:          id,
		Budget:      budget,
		BudgetLeft:  budgetLeft,
		BudgetRatio: budgetRatio,
		CurrentStep: currentStep,
		TotalCost:   totalCost,
	}
	eb.mu.Unlock()
}

func (eb *EventBus) getStatsLocked() *AggregateStats {
	hitRate := 0.0
	if eb.totalRequests > 0 {
		hitRate = float64(eb.cacheHits) / float64(eb.totalRequests)
	}
	avgLatency := 0.0
	if eb.totalRequests > 0 {
		avgLatency = float64(eb.totalLatency) / float64(eb.totalRequests)
	}
	cascadeRate := 0.0
	if eb.cascadeAttempts > 0 {
		cascadeRate = float64(eb.cascadeAccepted) / float64(eb.cascadeAttempts)
	}
	return &AggregateStats{
		TotalRequests:          eb.totalRequests,
		CacheHits:              eb.cacheHits,
		CacheHitRate:           hitRate,
		TotalCost:              eb.totalCost,
		TotalSavings:           eb.totalSavings,
		AvgLatency:             avgLatency,
		EconomyCount:           eb.economyCount,
		CheapCount:             eb.cheapCount,
		MidCount:               eb.midCount,
		PremiumCount:           eb.premiumCount,
		CachedCount:            eb.cachedCount,
		CascadeAttempts:        eb.cascadeAttempts,
		CascadeAccepted:        eb.cascadeAccepted,
		CascadeAcceptRate:      cascadeRate,
		CompressionTokensSaved: eb.compressionTokensSaved,
	}
}

func (eb *EventBus) getWorkflowsLocked() []WorkflowBudget {
	wfs := make([]WorkflowBudget, 0, len(eb.workflows))
	for _, w := range eb.workflows {
		wfs = append(wfs, *w)
	}
	// Sort by total cost descending (top workflows)
	sort.Slice(wfs, func(i, j int) bool {
		return wfs[i].TotalCost > wfs[j].TotalCost
	})
	return wfs
}

func (eb *EventBus) getProviderStatsLocked() []ProviderStat {
	stats := make([]ProviderStat, 0, len(eb.providerStats))
	for _, ps := range eb.providerStats {
		stats = append(stats, *ps)
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Requests > stats[j].Requests
	})
	return stats
}

func (eb *EventBus) getRecentRequestsLocked() []RequestEvent {
	n := len(eb.recentRequests)
	if n > 50 {
		n = 50
	}
	out := make([]RequestEvent, n)
	copy(out, eb.recentRequests[len(eb.recentRequests)-n:])
	return out
}

func (eb *EventBus) broadcast(data []byte) {
	eb.subMu.Lock()
	defer eb.subMu.Unlock()
	for ch := range eb.subscribers {
		select {
		case ch <- data:
		default:
			// slow client, skip
		}
	}
}

func (eb *EventBus) subscribe() chan []byte {
	ch := make(chan []byte, 32)
	eb.subMu.Lock()
	eb.subscribers[ch] = struct{}{}
	eb.subMu.Unlock()
	return ch
}

func (eb *EventBus) unsubscribe(ch chan []byte) {
	eb.subMu.Lock()
	delete(eb.subscribers, ch)
	eb.subMu.Unlock()
	close(ch)
}

// ServeSSE is the HTTP handler for the SSE endpoint (/dashboard/events).
func (eb *EventBus) ServeSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Send initial state
	eb.mu.RLock()
	initUpdate := DashboardUpdate{
		Type:           "init",
		Stats:          eb.getStatsLocked(),
		Workflows:      eb.getWorkflowsLocked(),
		RecentRequests: eb.getRecentRequestsLocked(),
		ProviderStats:  eb.getProviderStatsLocked(),
	}
	eb.mu.RUnlock()

	initData, _ := json.Marshal(initUpdate)
	fmt.Fprintf(w, "data: %s\n\n", initData)
	flusher.Flush()

	ch := eb.subscribe()
	defer eb.unsubscribe(ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// ServeStats is the HTTP handler for the REST stats endpoint (/dashboard/api/stats).
func (eb *EventBus) ServeStats(w http.ResponseWriter, r *http.Request) {
	eb.mu.RLock()
	stats := eb.getStatsLocked()
	workflows := eb.getWorkflowsLocked()
	recent := eb.getRecentRequestsLocked()
	provStats := eb.getProviderStatsLocked()
	eb.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"stats":           stats,
		"workflows":       workflows,
		"recent_requests": recent,
		"provider_stats":  provStats,
	})
}
