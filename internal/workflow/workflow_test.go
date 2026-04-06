package workflow

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestGetOrCreate_New(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	ws := tr.GetOrCreate("wf-1")
	if ws.ID != "wf-1" {
		t.Fatalf("expected ID wf-1, got %s", ws.ID)
	}
	if ws.Budget != 10.0 {
		t.Fatalf("expected budget 10.0, got %f", ws.Budget)
	}
	if ws.BudgetLeft != 10.0 {
		t.Fatalf("expected budget_left 10.0, got %f", ws.BudgetLeft)
	}
}

func TestGetOrCreate_Existing(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	ws1 := tr.GetOrCreate("wf-1")
	ws1.AddStep(StepRecord{Cost: 1.0})
	ws2 := tr.GetOrCreate("wf-1")
	if ws1 != ws2 {
		t.Fatal("expected same pointer for existing workflow")
	}
	if ws2.TotalCost != 1.0 {
		t.Fatalf("expected TotalCost 1.0, got %f", ws2.TotalCost)
	}
}

func TestGet_Found(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	tr.GetOrCreate("wf-1")
	ws, ok := tr.Get("wf-1")
	if !ok || ws == nil {
		t.Fatal("expected workflow to be found")
	}
}

func TestGet_NotFound(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	_, ok := tr.Get("nonexistent")
	if ok {
		t.Fatal("expected workflow not found")
	}
}

func TestAddStep(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	ws := tr.GetOrCreate("wf-1")
	ws.AddStep(StepRecord{Cost: 2.0, Model: "gpt-4", Tier: "premium"})
	ws.AddStep(StepRecord{Cost: 1.0, Model: "gpt-3.5", Tier: "cheap"})

	if ws.CurrentStep != 2 {
		t.Fatalf("expected current step 2, got %d", ws.CurrentStep)
	}
	if ws.TotalCost != 3.0 {
		t.Fatalf("expected total cost 3.0, got %f", ws.TotalCost)
	}
	if ws.BudgetLeft != 7.0 {
		t.Fatalf("expected budget left 7.0, got %f", ws.BudgetLeft)
	}
	if len(ws.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(ws.Steps))
	}
	if ws.Steps[0].StepNumber != 1 || ws.Steps[1].StepNumber != 2 {
		t.Fatal("step numbers not auto-incremented correctly")
	}
}

func TestGetBudgetRatio(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	ws := tr.GetOrCreate("wf-1")
	if ws.GetBudgetRatio() != 1.0 {
		t.Fatalf("expected ratio 1.0, got %f", ws.GetBudgetRatio())
	}
	ws.AddStep(StepRecord{Cost: 5.0})
	if ws.GetBudgetRatio() != 0.5 {
		t.Fatalf("expected ratio 0.5, got %f", ws.GetBudgetRatio())
	}
}

func TestGetBudgetRatio_ZeroBudget(t *testing.T) {
	tr := NewTracker(0, time.Hour)
	ws := tr.GetOrCreate("wf-1")
	if ws.GetBudgetRatio() != 1.0 {
		t.Fatalf("expected ratio 1.0 for zero budget, got %f", ws.GetBudgetRatio())
	}
}

func TestGetStepRatio(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	ws := tr.GetOrCreate("wf-1")
	ws.TotalSteps = 10

	if ws.GetStepRatio() != 0.0 {
		t.Fatalf("expected step ratio 0.0, got %f", ws.GetStepRatio())
	}
	ws.AddStep(StepRecord{Cost: 0.1})
	if ws.GetStepRatio() != 0.1 {
		t.Fatalf("expected step ratio 0.1, got %f", ws.GetStepRatio())
	}
}

func TestGetStepRatio_ZeroTotalSteps(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	ws := tr.GetOrCreate("wf-1")
	// TotalSteps defaults to 0
	if ws.GetStepRatio() != 0.5 {
		t.Fatalf("expected fallback step ratio 0.5, got %f", ws.GetStepRatio())
	}
}

func TestRecordFeedback_Success(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	ws := tr.GetOrCreate("wf-1")
	ws.AddStep(StepRecord{Cost: 1.0})
	ws.AddStep(StepRecord{Cost: 2.0})

	ok := tr.RecordFeedback("wf-1", 1, "success")
	if !ok {
		t.Fatal("expected feedback recorded")
	}
	if ws.Steps[0].Outcome != "success" {
		t.Fatalf("expected outcome 'success', got %s", ws.Steps[0].Outcome)
	}
}

func TestRecordFeedback_StepNotFound(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	ws := tr.GetOrCreate("wf-1")
	ws.AddStep(StepRecord{Cost: 1.0})

	ok := tr.RecordFeedback("wf-1", 99, "success")
	if ok {
		t.Fatal("expected feedback not recorded for non-existent step")
	}
}

func TestRecordFeedback_WorkflowNotFound(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	ok := tr.RecordFeedback("nonexistent", 1, "success")
	if ok {
		t.Fatal("expected false for nonexistent workflow")
	}
}

func TestConcurrentGetOrCreate(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ws := tr.GetOrCreate("shared")
			ws.AddStep(StepRecord{Cost: 0.01})
		}()
	}
	wg.Wait()

	ws, _ := tr.Get("shared")
	if ws.CurrentStep != 50 {
		t.Fatalf("expected 50 steps, got %d", ws.CurrentStep)
	}
}

func TestConcurrentDifferentWorkflows(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "wf-" + string(rune('A'+i))
			ws := tr.GetOrCreate(id)
			ws.AddStep(StepRecord{Cost: 1.0})
		}(i)
	}
	wg.Wait()
}

// --- FeedbackHandler tests ---

func newTestHandler() (*FeedbackHandler, *Tracker) {
	tr := NewTracker(10.0, time.Hour)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	h := NewFeedbackHandler(tr, logger)
	return h, tr
}

func TestFeedbackHandler_MethodNotAllowed(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/feedback", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestFeedbackHandler_InvalidJSON(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/feedback", bytes.NewBufferString("{bad json"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestFeedbackHandler_MissingFields(t *testing.T) {
	h, _ := newTestHandler()
	body, _ := json.Marshal(FeedbackRequest{WorkflowID: "wf-1"}) // missing step and outcome
	req := httptest.NewRequest(http.MethodPost, "/feedback", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestFeedbackHandler_WorkflowNotFound(t *testing.T) {
	h, _ := newTestHandler()
	body, _ := json.Marshal(FeedbackRequest{WorkflowID: "nope", Step: 1, Outcome: "success"})
	req := httptest.NewRequest(http.MethodPost, "/feedback", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestFeedbackHandler_Success(t *testing.T) {
	h, tr := newTestHandler()
	ws := tr.GetOrCreate("wf-1")
	ws.AddStep(StepRecord{Cost: 1.0})

	body, _ := json.Marshal(FeedbackRequest{WorkflowID: "wf-1", Step: 1, Outcome: "success"})
	req := httptest.NewRequest(http.MethodPost, "/feedback", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp FeedbackResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "ok" {
		t.Fatalf("expected status ok, got %s", resp.Status)
	}
}

func TestFeedbackHandler_StepNotFound(t *testing.T) {
	h, tr := newTestHandler()
	ws := tr.GetOrCreate("wf-1")
	ws.AddStep(StepRecord{Cost: 1.0})

	body, _ := json.Marshal(FeedbackRequest{WorkflowID: "wf-1", Step: 99, Outcome: "failure"})
	req := httptest.NewRequest(http.MethodPost, "/feedback", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestFeedbackHandler_ResponseJSON(t *testing.T) {
	h, _ := newTestHandler()
	body, _ := json.Marshal(FeedbackRequest{WorkflowID: "wf-1"})
	req := httptest.NewRequest(http.MethodPost, "/feedback", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}
}

func TestWorkflowState_StepTimestamp(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	ws := tr.GetOrCreate("wf-1")
	before := time.Now()
	ws.AddStep(StepRecord{Cost: 1.0})
	after := time.Now()

	ts := ws.Steps[0].Timestamp
	if ts.Before(before) || ts.After(after) {
		t.Fatal("step timestamp not in expected range")
	}
}

func TestWorkflowState_LastActivityUpdated(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	ws := tr.GetOrCreate("wf-1")
	initial := ws.LastActivity
	time.Sleep(10 * time.Millisecond)
	ws.AddStep(StepRecord{Cost: 1.0})
	if !ws.LastActivity.After(initial) {
		t.Fatal("expected LastActivity to be updated after AddStep")
	}
}

// === NEW A+ AUDIT TESTS ===

// --- AutoDetector Tests ---

func TestAutoDetector_NewWorkflow(t *testing.T) {
	ad := NewAutoDetector(30*time.Second, 5*time.Minute)
	wfID, step := ad.Detect("key1", "system prompt", "10.0.0.1", "agent/1.0")
	if wfID == "" {
		t.Error("expected non-empty workflow ID")
	}
	if step != 1 {
		t.Errorf("expected step 1 for new workflow, got %d", step)
	}
}

func TestAutoDetector_SameFingerprint_IncrementStep(t *testing.T) {
	ad := NewAutoDetector(30*time.Second, 5*time.Minute)
	wfID1, step1 := ad.Detect("key1", "sys", "10.0.0.1", "agent")
	wfID2, step2 := ad.Detect("key1", "sys", "10.0.0.1", "agent")

	if wfID1 != wfID2 {
		t.Errorf("same fingerprint should produce same workflow: %s vs %s", wfID1, wfID2)
	}
	if step1 != 1 || step2 != 2 {
		t.Errorf("expected steps 1 and 2, got %d and %d", step1, step2)
	}
}

func TestAutoDetector_DifferentFingerprint_NewWorkflow(t *testing.T) {
	ad := NewAutoDetector(30*time.Second, 5*time.Minute)
	wfID1, _ := ad.Detect("key1", "sys", "10.0.0.1", "agent")
	wfID2, _ := ad.Detect("key2", "sys", "10.0.0.2", "agent") // different key+IP

	if wfID1 == wfID2 {
		t.Error("different fingerprints should produce different workflow IDs")
	}
}

func TestAutoDetector_Stats(t *testing.T) {
	ad := NewAutoDetector(30*time.Second, 5*time.Minute)
	ad.Detect("key1", "sys", "10.0.0.1", "agent")
	ad.Detect("key1", "sys", "10.0.0.1", "agent") // same workflow, step 2
	ad.Detect("key2", "sys", "10.0.0.2", "agent") // new workflow

	sessions, total := ad.Stats()
	if sessions != 2 {
		t.Errorf("expected 2 sessions, got %d", sessions)
	}
	if total != 3 {
		t.Errorf("expected 3 total steps, got %d", total)
	}
}

func TestAutoDetector_DefaultWindow(t *testing.T) {
	ad := NewAutoDetector(0, 0)
	if ad.window != 30*time.Second {
		t.Errorf("expected default window 30s, got %v", ad.window)
	}
	if ad.maxAge != 5*time.Minute {
		t.Errorf("expected default maxAge 5m, got %v", ad.maxAge)
	}
}

// --- Snapshot Tests ---

func TestWorkflowState_Snapshot(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	ws := tr.GetOrCreate("wf-snap")
	ws.AddStep(StepRecord{Cost: 2.0})
	ws.AddStep(StepRecord{Cost: 3.0})

	snap := ws.Snapshot()
	if snap.CurrentStep != 2 {
		t.Errorf("expected step 2, got %d", snap.CurrentStep)
	}
	if snap.TotalCost != 5.0 {
		t.Errorf("expected total cost 5.0, got %f", snap.TotalCost)
	}
	if snap.Budget != 10.0 {
		t.Errorf("expected budget 10.0, got %f", snap.Budget)
	}
	if snap.BudgetLeft != 5.0 {
		t.Errorf("expected budget left 5.0, got %f", snap.BudgetLeft)
	}
}

func TestWorkflowState_Snapshot_Concurrent(t *testing.T) {
	tr := NewTracker(100.0, time.Hour)
	ws := tr.GetOrCreate("wf-snap-conc")

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			ws.AddStep(StepRecord{Cost: 0.01})
		}()
		go func() {
			defer wg.Done()
			_ = ws.Snapshot()
		}()
	}
	wg.Wait()
}

// --- Tracker Stop Tests ---

func TestTracker_Stop(t *testing.T) {
	tr := NewTracker(10.0, time.Hour)
	tr.GetOrCreate("wf-1")

	// Stop should not panic
	tr.Stop()
	// Double stop should not panic
	tr.Stop()
}

// --- FeedbackHandler edge cases ---

func TestFeedbackHandler_ContentType(t *testing.T) {
	h, tr := newTestHandler()
	ws := tr.GetOrCreate("wf-1")
	ws.AddStep(StepRecord{Cost: 1.0})

	body, _ := json.Marshal(FeedbackRequest{WorkflowID: "wf-1", Step: 1, Outcome: "success"})
	req := httptest.NewRequest(http.MethodPost, "/feedback", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}
}

func TestFeedbackHandler_MultipleOutcomes(t *testing.T) {
	h, tr := newTestHandler()
	ws := tr.GetOrCreate("wf-1")
	ws.AddStep(StepRecord{Cost: 1.0})
	ws.AddStep(StepRecord{Cost: 2.0})

	outcomes := []string{"success", "failure", "0.75"}
	for i, outcome := range outcomes {
		body, _ := json.Marshal(FeedbackRequest{
			WorkflowID: "wf-1",
			Step:       i + 1,
			Outcome:    outcome,
		})
		// For step 3, there's no step to update
		req := httptest.NewRequest(http.MethodPost, "/feedback", bytes.NewBuffer(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}

	if ws.Steps[0].Outcome != "success" {
		t.Errorf("step 1 outcome = %q, want 'success'", ws.Steps[0].Outcome)
	}
	if ws.Steps[1].Outcome != "failure" {
		t.Errorf("step 2 outcome = %q, want 'failure'", ws.Steps[1].Outcome)
	}
}
