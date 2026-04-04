package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/nexus-gateway/nexus/internal/cache"
	"github.com/nexus-gateway/nexus/internal/config"
	"github.com/nexus-gateway/nexus/internal/dashboard"
	"github.com/nexus-gateway/nexus/internal/provider"
	"github.com/nexus-gateway/nexus/internal/router"
	"github.com/nexus-gateway/nexus/internal/telemetry"
	"github.com/nexus-gateway/nexus/internal/workflow"
)

type Server struct {
	cfg          *config.Config
	router       *router.Router
	cache        *cache.Store
	tracker      *workflow.Tracker
	providers    map[string]provider.Provider
	health       *provider.HealthChecker
	metrics      *telemetry.Metrics
	costTracker  *telemetry.CostTracker
	Dashboard    *dashboard.EventBus
	logger       *slog.Logger
	httpServer   *http.Server
}

func New(cfg *config.Config, logger *slog.Logger) *Server {
	s := &Server{
		cfg:         cfg,
		providers:   make(map[string]provider.Provider),
		metrics:     telemetry.NewMetrics(),
		costTracker: telemetry.NewCostTracker(),
		Dashboard:   dashboard.NewEventBus(),
		logger:      logger,
	}

	// Init cache
	exactCache := cache.NewExactCache(cfg.Cache.TTL, cfg.Cache.MaxEntries)
	s.cache = cache.NewStore(exactCache, cfg.Cache.L1Enabled)

	// Init workflow tracker
	s.tracker = workflow.NewTracker(cfg.Router.DefaultBudget, cfg.Workflow.TTL)

	// Init providers
	s.health = provider.NewHealthChecker(logger)
	for _, pc := range cfg.Providers {
		if !pc.Enabled {
			continue
		}
		var p provider.Provider
		switch pc.Type {
		case "openai", "ollama":
			p = provider.NewOpenAI(pc.Name, pc.BaseURL, pc.APIKey, pc.Headers)
		case "anthropic":
			p = provider.NewOpenAI(pc.Name, pc.BaseURL, pc.APIKey, pc.Headers)
		}
		if p != nil {
			s.providers[pc.Name] = p
			s.health.Register(p)
		}
	}

	// Init router
	s.router = router.New(cfg.Router, cfg.Providers, logger)

	return s
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// OpenAI-compatible chat endpoint
	mux.HandleFunc("/v1/chat/completions", s.handleChat)

	// Feedback endpoint
	feedbackHandler := workflow.NewFeedbackHandler(s.tracker, s.logger)
	mux.Handle("/v1/feedback", feedbackHandler)

	// Health & metrics
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", s.metrics.Handler())

	// Dashboard
	mux.Handle("/dashboard", dashboard.Handler())
	mux.HandleFunc("/dashboard/events", s.Dashboard.ServeSSE)
	mux.HandleFunc("/dashboard/api/stats", s.Dashboard.ServeStats)

	// Info
	mux.HandleFunc("/", s.handleInfo)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.cfg.Server.Port),
		Handler:      mux,
		ReadTimeout:  s.cfg.Server.ReadTimeout,
		WriteTimeout: s.cfg.Server.WriteTimeout,
	}

	// Start health checker
	go s.health.Start(ctx)

	s.logger.Info("nexus gateway starting",
		"port", s.cfg.Server.Port,
		"providers", len(s.providers),
		"cache_l1", s.cfg.Cache.L1Enabled,
	)

	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	start := time.Now()

	// Parse request
	var req provider.ChatRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Extract Nexus headers
	workflowID := r.Header.Get("X-Workflow-ID")
	if workflowID == "" {
		workflowID = fmt.Sprintf("auto-%d", time.Now().UnixNano())
	}
	agentRole := r.Header.Get("X-Agent-Role")
	team := r.Header.Get("X-Team")

	// Get or create workflow state
	ws := s.tracker.GetOrCreate(workflowID)

	// Build prompt text for classification
	promptText := extractPromptText(req.Messages)
	contextLen := len(promptText)

	// Check cache first
	cacheKey := ""
	if s.cfg.Cache.Enabled {
		if cached, hit, source := s.cache.Lookup(promptText, req.Model); hit {
			s.logger.Info("cache hit",
				"workflow_id", workflowID,
				"source", source,
			)

			ws.AddStep(workflow.StepRecord{
				Model:    "cached",
				Tier:     "cached",
				CacheHit: true,
			})

			s.metrics.RecordRequest("cache", source, "cached", 0, 0, time.Since(start).Milliseconds(), true)
			s.costTracker.RecordStep(workflowID, team, 0, 0, true, 0.005)

			s.Dashboard.Push(dashboard.RequestEvent{
				Timestamp:       time.Now(),
				WorkflowID:      workflowID,
				Step:            ws.CurrentStep,
				ComplexityScore: 0,
				TierSelected:    "cached",
				ModelUsed:       "cached/" + source,
				LatencyMs:       time.Since(start).Milliseconds(),
				Cost:            0,
				CacheHit:        true,
			})
			s.Dashboard.UpdateWorkflow(workflowID, ws.Budget, ws.BudgetLeft, ws.GetBudgetRatio(), ws.CurrentStep, ws.TotalCost)

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Nexus-Cache", source)
			w.Write(cached)
			return
		}
		_ = cacheKey
	}

	// Route the request
	selection := s.router.Route(promptText, agentRole, ws.GetStepRatio(), ws.GetBudgetRatio(), contextLen)

	// Get the provider
	p, ok := s.providers[selection.Provider]
	if !ok {
		http.Error(w, fmt.Sprintf("provider %q not available", selection.Provider), http.StatusServiceUnavailable)
		return
	}

	// Update request model
	req.Model = selection.Model

	// Check if streaming requested
	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Nexus-Model", selection.Model)
		w.Header().Set("X-Nexus-Tier", selection.Tier)
		w.Header().Set("X-Nexus-Provider", selection.Provider)

		usage, err := p.SendStream(r.Context(), req, w)
		latencyMs := time.Since(start).Milliseconds()
		if err != nil {
			s.logger.Error("stream error", "error", err, "provider", selection.Provider)
			s.health.RecordFailure(selection.Provider, err)
			return
		}
		s.health.RecordSuccess(selection.Provider)

		tokens := 0
		if usage != nil {
			tokens = usage.TotalTokens
		}
		cost := float64(tokens) / 1000.0 * s.router.GetModelCost(selection.Provider, selection.Model)

		ws.AddStep(workflow.StepRecord{
			Model:     selection.Model,
			Tier:      selection.Tier,
			Tokens:    tokens,
			Cost:      cost,
			LatencyMs: latencyMs,
		})

		s.metrics.RecordRequest(selection.Provider, selection.Model, selection.Tier, tokens, cost, latencyMs, false)
		s.costTracker.RecordStep(workflowID, team, cost, tokens, false, 0)

		s.Dashboard.Push(dashboard.RequestEvent{
			Timestamp:       time.Now(),
			WorkflowID:      workflowID,
			Step:            ws.CurrentStep,
			ComplexityScore: selection.Score.FinalScore,
			TierSelected:    selection.Tier,
			ModelUsed:       selection.Model,
			LatencyMs:       latencyMs,
			Cost:            cost,
			CacheHit:        false,
		})
		s.Dashboard.UpdateWorkflow(workflowID, ws.Budget, ws.BudgetLeft, ws.GetBudgetRatio(), ws.CurrentStep, ws.TotalCost)
		return
	}

	// Non-streaming request
	resp, err := p.Send(r.Context(), req)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		s.logger.Error("request error", "error", err, "provider", selection.Provider)
		s.health.RecordFailure(selection.Provider, err)
		http.Error(w, fmt.Sprintf("provider error: %v", err), http.StatusBadGateway)
		return
	}
	s.health.RecordSuccess(selection.Provider)

	tokens := resp.Usage.TotalTokens
	cost := float64(tokens) / 1000.0 * s.router.GetModelCost(selection.Provider, selection.Model)

	// Record step
	ws.AddStep(workflow.StepRecord{
		Model:     selection.Model,
		Tier:      selection.Tier,
		Tokens:    tokens,
		Cost:      cost,
		LatencyMs: latencyMs,
	})

	s.metrics.RecordRequest(selection.Provider, selection.Model, selection.Tier, tokens, cost, latencyMs, false)
	s.costTracker.RecordStep(workflowID, team, cost, tokens, false, 0)

	s.Dashboard.Push(dashboard.RequestEvent{
		Timestamp:       time.Now(),
		WorkflowID:      workflowID,
		Step:            ws.CurrentStep,
		ComplexityScore: selection.Score.FinalScore,
		TierSelected:    selection.Tier,
		ModelUsed:       selection.Model,
		LatencyMs:       latencyMs,
		Cost:            cost,
		CacheHit:        false,
	})
	s.Dashboard.UpdateWorkflow(workflowID, ws.Budget, ws.BudgetLeft, ws.GetBudgetRatio(), ws.CurrentStep, ws.TotalCost)

	// Cache the response
	respBody, _ := json.Marshal(resp)
	s.cache.StoreResponse(promptText, selection.Model, respBody)

	// Return response with Nexus headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Nexus-Model", selection.Model)
	w.Header().Set("X-Nexus-Tier", selection.Tier)
	w.Header().Set("X-Nexus-Provider", selection.Provider)
	w.Header().Set("X-Nexus-Cost", fmt.Sprintf("%.6f", cost))
	w.Header().Set("X-Nexus-Workflow-ID", workflowID)
	w.Header().Set("X-Nexus-Workflow-Step", fmt.Sprintf("%d", ws.CurrentStep))
	w.Write(respBody)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := s.health.GetStatus()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":    "ok",
		"providers": status,
	})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	hits, misses, cacheSize := s.cache.Stats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"name":    "nexus",
		"version": "0.1.0",
		"description": "Agentic-first inference optimization gateway",
		"endpoints": []string{
			"/v1/chat/completions",
			"/v1/feedback",
			"/health",
			"/metrics",
		},
		"cache": map[string]any{
			"hits":   hits,
			"misses": misses,
			"size":   cacheSize,
		},
		"requests_total": s.metrics.RequestsTotal.Load(),
	})
}

func extractPromptText(messages []provider.Message) string {
	var parts []string
	for _, m := range messages {
		parts = append(parts, m.Content)
	}
	return strings.Join(parts, "\n")
}
