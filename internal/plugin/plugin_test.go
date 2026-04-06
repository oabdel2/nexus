package plugin

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// --- mock implementations ---

type mockClassifier struct {
	name  string
	score float64
	err   error
}

func (m *mockClassifier) Name() string { return m.name }
func (m *mockClassifier) Score(_ context.Context, _ *ClassifyRequest) (float64, error) {
	return m.score, m.err
}

type mockRouter struct {
	name string
	tier string
	err  error
}

func (m *mockRouter) Name() string { return m.name }
func (m *mockRouter) Route(_ context.Context, _ *RouteRequest) (string, error) {
	return m.tier, m.err
}

type mockHook struct {
	name       string
	onReqErr   error
	onRespErr  error
	onBudgErr  error
	reqCalls   atomic.Int32
	respCalls  atomic.Int32
	budgCalls  atomic.Int32
}

func (m *mockHook) Name() string { return m.name }
func (m *mockHook) OnRequest(_ context.Context, _ *RequestEvent) error {
	m.reqCalls.Add(1)
	return m.onReqErr
}
func (m *mockHook) OnResponse(_ context.Context, _ *ResponseEvent) error {
	m.respCalls.Add(1)
	return m.onRespErr
}
func (m *mockHook) OnBudgetAlert(_ context.Context, _ *BudgetEvent) error {
	m.budgCalls.Add(1)
	return m.onBudgErr
}

// --- tests ---

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	plugins := r.ListPlugins()
	if len(plugins["classifiers"]) != 0 || len(plugins["routers"]) != 0 || len(plugins["hooks"]) != 0 {
		t.Fatal("expected empty registry")
	}
}

func TestRegisterClassifier(t *testing.T) {
	r := NewRegistry()
	c := &mockClassifier{name: "test-classifier", score: 0.5}
	r.RegisterClassifier(c)

	plugins := r.ListPlugins()
	if len(plugins["classifiers"]) != 1 || plugins["classifiers"][0] != "test-classifier" {
		t.Fatalf("expected test-classifier, got %v", plugins["classifiers"])
	}
}

func TestRegisterClassifier_FirstBecomesActive(t *testing.T) {
	r := NewRegistry()
	r.RegisterClassifier(&mockClassifier{name: "c1", score: 0.1})
	r.RegisterClassifier(&mockClassifier{name: "c2", score: 0.9})

	score, err := r.Classify(context.Background(), &ClassifyRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score != 0.1 {
		t.Fatalf("expected first classifier (0.1), got %f", score)
	}
}

func TestSetActiveClassifier(t *testing.T) {
	r := NewRegistry()
	r.RegisterClassifier(&mockClassifier{name: "c1", score: 0.1})
	r.RegisterClassifier(&mockClassifier{name: "c2", score: 0.9})

	if err := r.SetActiveClassifier("c2"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	score, _ := r.Classify(context.Background(), &ClassifyRequest{})
	if score != 0.9 {
		t.Fatalf("expected c2 (0.9), got %f", score)
	}
}

func TestSetActiveClassifier_NotFound(t *testing.T) {
	r := NewRegistry()
	if err := r.SetActiveClassifier("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent classifier")
	}
}

func TestClassify_NoActive(t *testing.T) {
	r := NewRegistry()
	_, err := r.Classify(context.Background(), &ClassifyRequest{})
	if err == nil {
		t.Fatal("expected error when no active classifier")
	}
}

func TestClassify_ErrorPropagation(t *testing.T) {
	r := NewRegistry()
	r.RegisterClassifier(&mockClassifier{name: "bad", err: fmt.Errorf("broken")})
	_, err := r.Classify(context.Background(), &ClassifyRequest{})
	if err == nil || err.Error() != "broken" {
		t.Fatalf("expected 'broken' error, got %v", err)
	}
}

func TestRegisterRouter(t *testing.T) {
	r := NewRegistry()
	r.RegisterRouter(&mockRouter{name: "test-router", tier: "mid"})
	plugins := r.ListPlugins()
	if len(plugins["routers"]) != 1 {
		t.Fatalf("expected 1 router, got %d", len(plugins["routers"]))
	}
}

func TestRoute_FirstBecomesActive(t *testing.T) {
	r := NewRegistry()
	r.RegisterRouter(&mockRouter{name: "r1", tier: "cheap"})
	r.RegisterRouter(&mockRouter{name: "r2", tier: "premium"})

	tier, err := r.Route(context.Background(), &RouteRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tier != "cheap" {
		t.Fatalf("expected 'cheap', got %s", tier)
	}
}

func TestSetActiveRouter(t *testing.T) {
	r := NewRegistry()
	r.RegisterRouter(&mockRouter{name: "r1", tier: "cheap"})
	r.RegisterRouter(&mockRouter{name: "r2", tier: "premium"})

	if err := r.SetActiveRouter("r2"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tier, _ := r.Route(context.Background(), &RouteRequest{})
	if tier != "premium" {
		t.Fatalf("expected 'premium', got %s", tier)
	}
}

func TestSetActiveRouter_NotFound(t *testing.T) {
	r := NewRegistry()
	if err := r.SetActiveRouter("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent router")
	}
}

func TestRoute_NoActive(t *testing.T) {
	r := NewRegistry()
	_, err := r.Route(context.Background(), &RouteRequest{})
	if err == nil {
		t.Fatal("expected error when no active router")
	}
}

func TestRoute_ErrorPropagation(t *testing.T) {
	r := NewRegistry()
	r.RegisterRouter(&mockRouter{name: "bad", err: fmt.Errorf("route-fail")})
	_, err := r.Route(context.Background(), &RouteRequest{})
	if err == nil || err.Error() != "route-fail" {
		t.Fatalf("expected 'route-fail', got %v", err)
	}
}

func TestRegisterHook(t *testing.T) {
	r := NewRegistry()
	r.RegisterHook(&mockHook{name: "h1"})
	plugins := r.ListPlugins()
	if len(plugins["hooks"]) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(plugins["hooks"]))
	}
}

func TestEmitRequest_CallsAllHooks(t *testing.T) {
	r := NewRegistry()
	h1 := &mockHook{name: "h1"}
	h2 := &mockHook{name: "h2"}
	r.RegisterHook(h1)
	r.RegisterHook(h2)

	r.EmitRequest(context.Background(), &RequestEvent{WorkflowID: "w1"})

	if h1.reqCalls.Load() != 1 || h2.reqCalls.Load() != 1 {
		t.Fatalf("expected both hooks called: h1=%d h2=%d", h1.reqCalls.Load(), h2.reqCalls.Load())
	}
}

func TestEmitResponse_CallsAllHooks(t *testing.T) {
	r := NewRegistry()
	h := &mockHook{name: "h1"}
	r.RegisterHook(h)
	r.EmitResponse(context.Background(), &ResponseEvent{WorkflowID: "w1"})
	if h.respCalls.Load() != 1 {
		t.Fatalf("expected OnResponse called once, got %d", h.respCalls.Load())
	}
}

func TestEmitBudgetAlert_CallsAllHooks(t *testing.T) {
	r := NewRegistry()
	h := &mockHook{name: "h1"}
	r.RegisterHook(h)
	r.EmitBudgetAlert(context.Background(), &BudgetEvent{WorkflowID: "w1"})
	if h.budgCalls.Load() != 1 {
		t.Fatalf("expected OnBudgetAlert called once, got %d", h.budgCalls.Load())
	}
}

func TestEmitRequest_ErrorDoesNotStopChain(t *testing.T) {
	r := NewRegistry()
	h1 := &mockHook{name: "failing", onReqErr: fmt.Errorf("fail")}
	h2 := &mockHook{name: "ok"}
	r.RegisterHook(h1)
	r.RegisterHook(h2)

	r.EmitRequest(context.Background(), &RequestEvent{})

	if h1.reqCalls.Load() != 1 {
		t.Fatal("expected failing hook to be called")
	}
	// h2 may or may not have been called depending on iteration order,
	// but both should be attempted. Since map order is nondeterministic,
	// we just verify no panic and total calls >= 1.
	total := h1.reqCalls.Load() + h2.reqCalls.Load()
	if total < 2 {
		t.Fatalf("expected 2 total hook calls, got %d", total)
	}
}

func TestEmitResponse_ErrorDoesNotPanic(t *testing.T) {
	r := NewRegistry()
	h := &mockHook{name: "failing", onRespErr: fmt.Errorf("fail")}
	r.RegisterHook(h)
	r.EmitResponse(context.Background(), &ResponseEvent{}) // should not panic
}

func TestEmitBudgetAlert_ErrorDoesNotPanic(t *testing.T) {
	r := NewRegistry()
	h := &mockHook{name: "failing", onBudgErr: fmt.Errorf("fail")}
	r.RegisterHook(h)
	r.EmitBudgetAlert(context.Background(), &BudgetEvent{}) // should not panic
}

func TestListPlugins_AllTypes(t *testing.T) {
	r := NewRegistry()
	r.RegisterClassifier(&mockClassifier{name: "c1"})
	r.RegisterRouter(&mockRouter{name: "r1"})
	r.RegisterHook(&mockHook{name: "h1"})

	plugins := r.ListPlugins()
	if len(plugins["classifiers"]) != 1 {
		t.Fatal("expected 1 classifier")
	}
	if len(plugins["routers"]) != 1 {
		t.Fatal("expected 1 router")
	}
	if len(plugins["hooks"]) != 1 {
		t.Fatal("expected 1 hook")
	}
}

func TestConcurrentRegisterAndClassify(t *testing.T) {
	r := NewRegistry()
	r.RegisterClassifier(&mockClassifier{name: "c1", score: 0.5})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			r.RegisterClassifier(&mockClassifier{name: fmt.Sprintf("c-%d", i), score: 0.5})
		}(i)
		go func() {
			defer wg.Done()
			r.Classify(context.Background(), &ClassifyRequest{})
		}()
	}
	wg.Wait()
}

func TestConcurrentEmitHooks(t *testing.T) {
	r := NewRegistry()
	h := &mockHook{name: "h1"}
	r.RegisterHook(h)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.EmitRequest(context.Background(), &RequestEvent{})
		}()
	}
	wg.Wait()
	if h.reqCalls.Load() != 50 {
		t.Fatalf("expected 50 calls, got %d", h.reqCalls.Load())
	}
}

func TestContextPassedToClassifier(t *testing.T) {
	type ctxKey string
	r := NewRegistry()
	var gotCtx context.Context
	c := &contextCapturingClassifier{capture: &gotCtx}
	r.RegisterClassifier(c)

	ctx := context.WithValue(context.Background(), ctxKey("key"), "value")
	r.Classify(ctx, &ClassifyRequest{})

	if gotCtx == nil {
		t.Fatal("expected context to be passed")
	}
	if gotCtx.Value(ctxKey("key")) != "value" {
		t.Fatal("expected context value to be preserved")
	}
}

type contextCapturingClassifier struct {
	capture *context.Context
}

func (c *contextCapturingClassifier) Name() string { return "ctx-capture" }
func (c *contextCapturingClassifier) Score(ctx context.Context, _ *ClassifyRequest) (float64, error) {
	*c.capture = ctx
	return 0.5, nil
}
