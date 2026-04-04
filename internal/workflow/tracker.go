package workflow

import (
	"sync"
	"time"
)

type StepRecord struct {
	StepNumber int       `json:"step_number"`
	Model      string    `json:"model"`
	Tier       string    `json:"tier"`
	Tokens     int       `json:"tokens"`
	Cost       float64   `json:"cost"`
	LatencyMs  int64     `json:"latency_ms"`
	CacheHit   bool      `json:"cache_hit"`
	Outcome    string    `json:"outcome"` // pending, success, failure
	Timestamp  time.Time `json:"timestamp"`
}

type WorkflowState struct {
	ID            string       `json:"id"`
	TotalSteps    int          `json:"total_steps"`
	CurrentStep   int          `json:"current_step"`
	TotalCost     float64      `json:"total_cost"`
	Budget        float64      `json:"budget"`
	BudgetLeft    float64      `json:"budget_left"`
	Steps         []StepRecord `json:"steps"`
	CreatedAt     time.Time    `json:"created_at"`
	LastActivity  time.Time    `json:"last_activity"`
	mu            sync.Mutex
}

func (w *WorkflowState) AddStep(step StepRecord) {
	w.mu.Lock()
	defer w.mu.Unlock()

	step.StepNumber = w.CurrentStep + 1
	step.Timestamp = time.Now()
	w.Steps = append(w.Steps, step)
	w.CurrentStep = step.StepNumber
	w.TotalCost += step.Cost
	w.BudgetLeft = w.Budget - w.TotalCost
	w.LastActivity = time.Now()
}

func (w *WorkflowState) GetBudgetRatio() float64 {
	if w.Budget <= 0 {
		return 1.0
	}
	return w.BudgetLeft / w.Budget
}

func (w *WorkflowState) GetStepRatio() float64 {
	if w.TotalSteps <= 0 {
		return 0.5
	}
	return float64(w.CurrentStep) / float64(w.TotalSteps)
}

type Tracker struct {
	workflows map[string]*WorkflowState
	mu        sync.RWMutex
	defaultBudget float64
	ttl       time.Duration
}

func NewTracker(defaultBudget float64, ttl time.Duration) *Tracker {
	t := &Tracker{
		workflows:     make(map[string]*WorkflowState),
		defaultBudget: defaultBudget,
		ttl:           ttl,
	}
	go t.cleanup()
	return t
}

func (t *Tracker) GetOrCreate(workflowID string) *WorkflowState {
	t.mu.Lock()
	defer t.mu.Unlock()

	if ws, ok := t.workflows[workflowID]; ok {
		return ws
	}

	ws := &WorkflowState{
		ID:           workflowID,
		Budget:       t.defaultBudget,
		BudgetLeft:   t.defaultBudget,
		Steps:        make([]StepRecord, 0),
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
	}
	t.workflows[workflowID] = ws
	return ws
}

func (t *Tracker) Get(workflowID string) (*WorkflowState, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ws, ok := t.workflows[workflowID]
	return ws, ok
}

func (t *Tracker) RecordFeedback(workflowID string, stepNumber int, outcome string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	ws, ok := t.workflows[workflowID]
	if !ok {
		return false
	}

	ws.mu.Lock()
	defer ws.mu.Unlock()

	for i := range ws.Steps {
		if ws.Steps[i].StepNumber == stepNumber {
			ws.Steps[i].Outcome = outcome
			return true
		}
	}
	return false
}

func (t *Tracker) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		t.mu.Lock()
		now := time.Now()
		for id, ws := range t.workflows {
			if now.Sub(ws.LastActivity) > t.ttl {
				delete(t.workflows, id)
			}
		}
		t.mu.Unlock()
	}
}
