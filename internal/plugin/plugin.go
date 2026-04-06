package plugin

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// ClassifyRequest contains the data needed for complexity classification
type ClassifyRequest struct {
	Messages     []Message
	SystemPrompt string
	WorkflowID   string
	Step         int
	Role         string
}

// Message represents a chat message
type Message struct {
	Role    string
	Content string
}

// RouteRequest contains the data needed for a routing decision
type RouteRequest struct {
	Score        float64
	Budget       *BudgetState
	AllowedTiers []string
	WorkflowID   string
	Step         int
}

// BudgetState represents current budget consumption
type BudgetState struct {
	Total     float64
	Spent     float64
	Remaining float64
	Fraction  float64 // 0.0 to 1.0
}

// RequestEvent is emitted when a request is received
type RequestEvent struct {
	WorkflowID string
	Step       int
	Model      string
	Tier       string
	Score      float64
	APIKey     string
	Team       string
}

// ResponseEvent is emitted when a response is sent
type ResponseEvent struct {
	WorkflowID string
	Step       int
	Model      string
	Tier       string
	LatencyMs  float64
	Cost       float64
	CacheHit   bool
	TokensIn   int
	TokensOut  int
}

// BudgetEvent is emitted on budget alerts
type BudgetEvent struct {
	WorkflowID string
	Team       string
	Budget     float64
	Spent      float64
	Fraction   float64
	Level      string // "warning", "critical", "exhausted"
}

// Classifier scores request complexity (0.0 to 1.0)
type Classifier interface {
	Name() string
	Score(ctx context.Context, req *ClassifyRequest) (float64, error)
}

// Router decides which model tier to use
type Router interface {
	Name() string
	Route(ctx context.Context, req *RouteRequest) (tier string, err error)
}

// Hook reacts to lifecycle events
type Hook interface {
	Name() string
	OnRequest(ctx context.Context, event *RequestEvent) error
	OnResponse(ctx context.Context, event *ResponseEvent) error
	OnBudgetAlert(ctx context.Context, event *BudgetEvent) error
}

// Registry manages all registered plugins
type Registry struct {
	mu               sync.RWMutex
	classifiers      map[string]Classifier
	routers          map[string]Router
	hooks            map[string]Hook
	activeClassifier string
	activeRouter     string
}

// NewRegistry creates a new plugin registry
func NewRegistry() *Registry {
	return &Registry{
		classifiers: make(map[string]Classifier),
		routers:     make(map[string]Router),
		hooks:       make(map[string]Hook),
	}
}

// RegisterClassifier adds a classifier plugin
func (r *Registry) RegisterClassifier(c Classifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.classifiers[c.Name()] = c
	if r.activeClassifier == "" {
		r.activeClassifier = c.Name()
	}
	log.Printf("[plugin] registered classifier: %s", c.Name())
}

// RegisterRouter adds a router plugin
func (r *Registry) RegisterRouter(rt Router) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routers[rt.Name()] = rt
	if r.activeRouter == "" {
		r.activeRouter = rt.Name()
	}
	log.Printf("[plugin] registered router: %s", rt.Name())
}

// RegisterHook adds a hook plugin
func (r *Registry) RegisterHook(h Hook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks[h.Name()] = h
	log.Printf("[plugin] registered hook: %s", h.Name())
}

// SetActiveClassifier sets which classifier to use
func (r *Registry) SetActiveClassifier(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.classifiers[name]; !ok {
		return fmt.Errorf("classifier %q not found", name)
	}
	r.activeClassifier = name
	return nil
}

// SetActiveRouter sets which router to use
func (r *Registry) SetActiveRouter(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.routers[name]; !ok {
		return fmt.Errorf("router %q not found", name)
	}
	r.activeRouter = name
	return nil
}

// Classify runs the active classifier
func (r *Registry) Classify(ctx context.Context, req *ClassifyRequest) (float64, error) {
	r.mu.RLock()
	name := r.activeClassifier
	c, ok := r.classifiers[name]
	r.mu.RUnlock()
	if !ok {
		return 0, fmt.Errorf("no active classifier")
	}
	return c.Score(ctx, req)
}

// Route runs the active router
func (r *Registry) Route(ctx context.Context, req *RouteRequest) (string, error) {
	r.mu.RLock()
	name := r.activeRouter
	rt, ok := r.routers[name]
	r.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("no active router")
	}
	return rt.Route(ctx, req)
}

// EmitRequest fires all hook OnRequest handlers
func (r *Registry) EmitRequest(ctx context.Context, event *RequestEvent) {
	r.mu.RLock()
	hooks := make([]Hook, 0, len(r.hooks))
	for _, h := range r.hooks {
		hooks = append(hooks, h)
	}
	r.mu.RUnlock()

	for _, h := range hooks {
		if err := h.OnRequest(ctx, event); err != nil {
			log.Printf("[plugin] hook %s OnRequest error: %v", h.Name(), err)
		}
	}
}

// EmitResponse fires all hook OnResponse handlers
func (r *Registry) EmitResponse(ctx context.Context, event *ResponseEvent) {
	r.mu.RLock()
	hooks := make([]Hook, 0, len(r.hooks))
	for _, h := range r.hooks {
		hooks = append(hooks, h)
	}
	r.mu.RUnlock()

	for _, h := range hooks {
		if err := h.OnResponse(ctx, event); err != nil {
			log.Printf("[plugin] hook %s OnResponse error: %v", h.Name(), err)
		}
	}
}

// EmitBudgetAlert fires all hook OnBudgetAlert handlers
func (r *Registry) EmitBudgetAlert(ctx context.Context, event *BudgetEvent) {
	r.mu.RLock()
	hooks := make([]Hook, 0, len(r.hooks))
	for _, h := range r.hooks {
		hooks = append(hooks, h)
	}
	r.mu.RUnlock()

	for _, h := range hooks {
		if err := h.OnBudgetAlert(ctx, event); err != nil {
			log.Printf("[plugin] hook %s OnBudgetAlert error: %v", h.Name(), err)
		}
	}
}

// ListPlugins returns all registered plugins
func (r *Registry) ListPlugins() map[string][]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := map[string][]string{
		"classifiers": {},
		"routers":     {},
		"hooks":       {},
	}
	for name := range r.classifiers {
		result["classifiers"] = append(result["classifiers"], name)
	}
	for name := range r.routers {
		result["routers"] = append(result["routers"], name)
	}
	for name := range r.hooks {
		result["hooks"] = append(result["hooks"], name)
	}
	return result
}

// List returns all registered plugins with their active status.
func (r *Registry) List() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	classifiers := make([]map[string]interface{}, 0, len(r.classifiers))
	for name := range r.classifiers {
		classifiers = append(classifiers, map[string]interface{}{
			"name":   name,
			"active": name == r.activeClassifier,
		})
	}

	routers := make([]map[string]interface{}, 0, len(r.routers))
	for name := range r.routers {
		routers = append(routers, map[string]interface{}{
			"name":   name,
			"active": name == r.activeRouter,
		})
	}

	hooks := make([]string, 0, len(r.hooks))
	for name := range r.hooks {
		hooks = append(hooks, name)
	}

	return map[string]interface{}{
		"classifiers": classifiers,
		"routers":     routers,
		"hooks":       hooks,
	}
}
