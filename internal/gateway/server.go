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

	"github.com/nexus-gateway/nexus/internal/billing"
	"github.com/nexus-gateway/nexus/internal/cache"
	"github.com/nexus-gateway/nexus/internal/config"
	"github.com/nexus-gateway/nexus/internal/dashboard"
	"github.com/nexus-gateway/nexus/internal/notification"
	"github.com/nexus-gateway/nexus/internal/provider"
	"github.com/nexus-gateway/nexus/internal/router"
	"github.com/nexus-gateway/nexus/internal/security"
	"github.com/nexus-gateway/nexus/internal/telemetry"
	"github.com/nexus-gateway/nexus/internal/workflow"
)

type Server struct {
	cfg           *config.Config
	router        *router.Router
	cache         *cache.Store
	tracker       *workflow.Tracker
	providers     map[string]provider.Provider
	health        *provider.HealthChecker
	cbPool        *provider.ProviderPool
	metrics       *telemetry.Metrics
	costTracker   *telemetry.CostTracker
	tracer        *telemetry.Tracer
	Dashboard     *dashboard.EventBus
	logger        *slog.Logger
	httpServer    *http.Server
	subStore      *billing.SubscriptionStore
	keyStore      *billing.APIKeyStore
	deviceTracker *billing.DeviceTracker
	notifier      *notification.Notifier
	eventLog      *notification.EventLog
	webhookHandler *billing.StripeWebhookHandler
}

func New(cfg *config.Config, logger *slog.Logger) *Server {
	s := &Server{
		cfg:         cfg,
		providers:   make(map[string]provider.Provider),
		metrics:     telemetry.NewMetrics(),
		costTracker: telemetry.NewCostTracker(),
		tracer: telemetry.NewTracer(telemetry.TracerConfig{
			Enabled:     cfg.Tracing.Enabled,
			ServiceName: cfg.Tracing.ServiceName,
			SampleRate:  cfg.Tracing.SampleRate,
			ExportURL:   cfg.Tracing.ExportURL,
			LogSpans:    cfg.Tracing.LogSpans,
		}),
		cbPool: provider.NewProviderPool(provider.CircuitBreakerConfig{
			FailureThreshold: 5,
			SuccessThreshold: 2,
			Timeout:          30 * time.Second,
			MaxTimeout:       5 * time.Minute,
			HalfOpenMax:      1,
		}),
		Dashboard:   dashboard.NewEventBus(),
		logger:      logger,
	}

	// Init cache
	s.cache = cache.NewStore(cache.StoreConfig{
		L1Enabled:         cfg.Cache.L1Enabled || cfg.Cache.L1.Enabled,
		L1TTL:             cfg.Cache.L1.TTL,
		L1MaxEntries:      cfg.Cache.L1.MaxEntries,
		L2aEnabled:        cfg.Cache.L2BM25.Enabled,
		L2aTTL:            cfg.Cache.L2BM25.TTL,
		L2aMaxEntries:     cfg.Cache.L2BM25.MaxEntries,
		L2aThreshold:      cfg.Cache.L2BM25.Threshold,
		L2bEnabled:        cfg.Cache.L2Semantic.Enabled,
		L2bTTL:            cfg.Cache.L2Semantic.TTL,
		L2bMaxEntries:     cfg.Cache.L2Semantic.MaxEntries,
		L2bThreshold:      cfg.Cache.L2Semantic.Threshold,
		L2bBackend:        cfg.Cache.L2Semantic.Backend,
		L2bModel:          cfg.Cache.L2Semantic.Model,
		L2bEndpoint:       cfg.Cache.L2Semantic.Endpoint,
		L2bAPIKey:         cfg.Cache.L2Semantic.APIKey,
		RerankerEnabled:   cfg.Cache.L2Semantic.Reranker.Enabled,
		RerankerModel:     cfg.Cache.L2Semantic.Reranker.Model,
		RerankerEndpoint:  cfg.Cache.L2Semantic.Reranker.Endpoint,
		RerankerThreshold: cfg.Cache.L2Semantic.Reranker.Threshold,
		FeedbackEnabled:   cfg.Cache.Feedback.Enabled,
		FeedbackMaxSize:   cfg.Cache.Feedback.MaxSize,
		ShadowEnabled:         cfg.Cache.Shadow.Enabled,
		ShadowMaxResults:      cfg.Cache.Shadow.MaxResults,
		SynonymDataDir:        cfg.Cache.Synonym.DataDir,
		SynonymPromoThreshold: cfg.Cache.Synonym.PromotionThreshold,
	})

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
			s.cbPool.Register(pc.Name)
		}
	}

	// Init router
	s.router = router.New(cfg.Router, cfg.Providers, logger)

	// Init billing (if enabled)
	if cfg.Billing.Enabled {
		s.eventLog = notification.NewEventLog(cfg.Billing.DataDir)
		s.notifier = notification.NewNotifier(notification.NotifierConfig{
			SMTPHost:     cfg.Notification.SMTPHost,
			SMTPPort:     cfg.Notification.SMTPPort,
			SMTPUser:     cfg.Notification.SMTPUser,
			SMTPPassword: cfg.Notification.SMTPPass,
			FromEmail:    cfg.Notification.FromEmail,
			FromName:     cfg.Notification.FromName,
			Enabled:      cfg.Notification.Enabled,
		}, s.eventLog, logger)
		s.subStore = billing.NewSubscriptionStore(cfg.Billing.DataDir, logger)
		s.keyStore = billing.NewAPIKeyStore(cfg.Billing.DataDir, s.subStore, logger)
		s.deviceTracker = billing.NewDeviceTracker(cfg.Billing.DataDir, logger)
		s.webhookHandler = billing.NewStripeWebhookHandler(
			s.subStore, s.keyStore, cfg.Billing.StripeWebhookSecret, logger,
		)
		s.subStore.StartLifecycleChecker()
	}

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
	mux.HandleFunc("/health/live", s.handleLiveness)
	mux.HandleFunc("/health/ready", s.handleReadiness)
	mux.HandleFunc("/metrics", s.metrics.Handler())

	// Dashboard
	mux.Handle("/dashboard", dashboard.Handler())
	mux.HandleFunc("/dashboard/events", s.Dashboard.ServeSSE)
	mux.HandleFunc("/dashboard/api/stats", s.Dashboard.ServeStats)

	// Info
	mux.HandleFunc("/", s.handleInfo)

	// Synonym admin API
	mux.HandleFunc("/api/synonyms/stats", s.handleSynonymStats)
	mux.HandleFunc("/api/synonyms/candidates", s.handleSynonymCandidates)
	mux.HandleFunc("/api/synonyms/learned", s.handleSynonymLearned)
	mux.HandleFunc("/api/synonyms/promote", s.handleSynonymPromote)
	mux.HandleFunc("/api/synonyms/add", s.handleSynonymAdd)

	// Circuit breaker status
	mux.HandleFunc("/api/circuit-breakers", s.handleCircuitBreakers)

	// Billing endpoints (only registered if billing enabled)
	if s.cfg.Billing.Enabled {
		mux.HandleFunc("/webhooks/stripe", s.handleStripeWebhook)
		mux.HandleFunc("/api/admin/subscriptions", s.handleAdminSubscriptions)
		mux.HandleFunc("/api/admin/keys/", s.handleAdminKeys)
		mux.HandleFunc("/api/admin/devices/", s.handleAdminDevices)
		mux.HandleFunc("/api/keys/generate", s.handleKeyGenerate)
		mux.HandleFunc("/api/keys/revoke", s.handleKeyRevoke)
		mux.HandleFunc("/api/usage", s.handleUsage)
	}

	// Build security middleware chain
	// Order: BillingAuth → Tracing → PanicRecovery → BodySizeLimit →
	//   RequestTimeout → SecurityHeaders → RequestID → RequestLogger →
	//   CORS → IPAllowlist → RateLimit → OIDC → InputValidator →
	//   PromptGuard → AuditLog
	var middlewares []security.Middleware

	// Billing API key auth: first in chain (before other security)
	if s.cfg.Billing.Enabled {
		middlewares = append(middlewares, s.billingAuthMiddleware())
	}

	// Tracing: early in chain to capture full request lifecycle
	if s.cfg.Tracing.Enabled {
		middlewares = append(middlewares, security.Middleware(telemetry.TraceMiddleware(s.tracer)))
	}

	// Panic recovery
	if s.cfg.Security.PanicRecovery {
		middlewares = append(middlewares, security.PanicRecovery(s.logger))
	}

	// Body size limit
	if s.cfg.Security.BodySizeLimit > 0 {
		middlewares = append(middlewares, security.BodySizeLimit(s.cfg.Security.BodySizeLimit))
	}

	// Request timeout
	if s.cfg.Security.RequestTimeout != "" {
		if timeout, err := time.ParseDuration(s.cfg.Security.RequestTimeout); err == nil {
			middlewares = append(middlewares, security.RequestTimeout(timeout))
		}
	}

	// Security headers + request ID
	middlewares = append(middlewares, security.SecurityHeaders())
	middlewares = append(middlewares, security.RequestID())

	// Request logger
	if s.cfg.Security.RequestLogging {
		middlewares = append(middlewares, security.RequestLogger(s.logger))
	}

	// CORS
	if len(s.cfg.Security.CORS.AllowedOrigins) > 0 {
		middlewares = append(middlewares, security.CORS(s.cfg.Security.CORS.AllowedOrigins))
	}

	// IP allowlist
	middlewares = append(middlewares, security.IPAllowlist(security.IPAllowlistConfig{
		Enabled:    s.cfg.Security.IPAllowlist.Enabled,
		AllowedIPs: s.cfg.Security.IPAllowlist.AllowedIPs,
		Paths:      s.cfg.Security.IPAllowlist.Paths,
	}))

	// Rate limiting
	rateLimiter := security.NewRateLimiter(security.RateLimiterConfig{
		Enabled:    s.cfg.Security.RateLimit.Enabled,
		DefaultRPM: s.cfg.Security.RateLimit.DefaultRPM,
		BurstSize:  s.cfg.Security.RateLimit.BurstSize,
	})
	middlewares = append(middlewares, rateLimiter.Middleware())

	// OIDC SSO
	if s.cfg.Security.OIDC.Enabled {
		oidcProvider, err := security.NewOIDCProvider(security.OIDCConfig{
			Enabled:        true,
			Issuer:         s.cfg.Security.OIDC.Issuer,
			ClientID:       s.cfg.Security.OIDC.ClientID,
			ClientSecret:   s.cfg.Security.OIDC.ClientSecret,
			AllowedDomains: s.cfg.Security.OIDC.AllowedDomains,
		})
		if err == nil {
			middlewares = append(middlewares, oidcProvider.Middleware())
		}
	}

	// Input validation
	if s.cfg.Security.InputValidation {
		middlewares = append(middlewares, security.InputValidator())
	}

	// Prompt guard
	promptGuard := security.NewPromptGuard(security.PromptGuardConfig{
		Enabled:         s.cfg.Security.PromptGuard.Enabled,
		Mode:            s.cfg.Security.PromptGuard.Mode,
		MaxPromptLength: s.cfg.Security.PromptGuard.MaxPromptLength,
		CustomPatterns:  s.cfg.Security.PromptGuard.CustomPatterns,
		CustomPhrases:   s.cfg.Security.PromptGuard.CustomPhrases,
	})
	middlewares = append(middlewares, promptGuard.Middleware())

	// Audit logging
	if s.cfg.Security.AuditLog {
		middlewares = append(middlewares, security.AuditLog(s.logger))
	}

	// Apply middleware chain
	handler := security.Chain(mux, middlewares...)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.cfg.Server.Port),
		Handler:      handler,
		ReadTimeout:  s.cfg.Server.ReadTimeout,
		WriteTimeout: s.cfg.Server.WriteTimeout,
	}

	// Start health checker
	go s.health.Start(ctx)

	s.logger.Info("nexus gateway starting",
		"port", s.cfg.Server.Port,
		"providers", len(s.providers),
		"cache_l1", s.cfg.Cache.L1Enabled,
		"tls", s.cfg.Security.TLS.Enabled,
		"rate_limit", s.cfg.Security.RateLimit.Enabled,
		"prompt_guard", s.cfg.Security.PromptGuard.Enabled,
		"audit_log", s.cfg.Security.AuditLog,
	)

	if s.cfg.Security.TLS.Enabled {
		tlsCfg, err := security.BuildTLSConfig(security.TLSConfig{
			Enabled:    true,
			CertFile:   s.cfg.Security.TLS.CertFile,
			KeyFile:    s.cfg.Security.TLS.KeyFile,
			CAFile:     s.cfg.Security.TLS.CAFile,
			MinVersion: s.cfg.Security.TLS.MinVersion,
			MutualTLS:  s.cfg.Security.TLS.MutualTLS,
		})
		if err != nil {
			return fmt.Errorf("TLS configuration failed: %w", err)
		}
		s.httpServer.TLSConfig = tlsCfg
		return s.httpServer.ListenAndServeTLS(s.cfg.Security.TLS.CertFile, s.cfg.Security.TLS.KeyFile)
	}
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.subStore != nil {
		s.subStore.Stop()
	}
	if s.notifier != nil {
		s.notifier.Stop()
	}
	if s.keyStore != nil {
		s.keyStore.Save()
	}
	if s.deviceTracker != nil {
		s.deviceTracker.Save()
	}
	if s.eventLog != nil {
		s.eventLog.Save()
	}
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

	// Check for prompt injection
	if guard := security.GetPromptGuard(r.Context()); guard != nil {
		result := guard.Check(promptText)
		if result.Blocked {
			s.logger.Warn("prompt injection blocked",
				"threats", result.Threats,
				"risk_score", result.RiskScore,
			)
			http.Error(w, `{"error":"prompt rejected by security filter"}`, http.StatusBadRequest)
			return
		}
	}

	// Check cache first
	cacheKey := ""
	if s.cfg.Cache.Enabled {
		ctx, cacheSpan := s.tracer.StartSpan(r.Context(), "cache.lookup")
		if cached, hit, source := s.cache.Lookup(promptText, req.Model); hit {
			cacheSpan.SetAttribute("cache.hit", "true")
			cacheSpan.SetAttribute("cache.source", source)
			s.tracer.EndSpan(cacheSpan)

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
		cacheSpan.SetAttribute("cache.hit", "false")
		s.tracer.EndSpan(cacheSpan)
		r = r.WithContext(ctx)
		_ = cacheKey
	}

	// Route the request
	_, routeSpan := s.tracer.StartSpan(r.Context(), "router.classify")
	selection := s.router.Route(promptText, agentRole, ws.GetStepRatio(), ws.GetBudgetRatio(), contextLen)
	routeSpan.SetAttribute("router.tier", selection.Tier)
	routeSpan.SetAttribute("router.model", selection.Model)
	routeSpan.SetAttribute("router.provider", selection.Provider)
	routeSpan.SetAttribute("router.score", fmt.Sprintf("%.4f", selection.Score.FinalScore))
	s.tracer.EndSpan(routeSpan)

	// Get the provider with circuit breaker check
	p, ok := s.providers[selection.Provider]
	if !ok || !s.cbPool.IsAvailable(selection.Provider) {
		// Try failover to any available provider with same tier
		p, selection = s.findFallbackProvider(selection)
		if p == nil {
			http.Error(w, `{"error":"all providers unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		s.logger.Info("circuit breaker failover",
			"original", selection.Provider,
			"fallback", selection.Provider,
		)
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

		_, streamSpan := s.tracer.StartSpan(r.Context(), "provider.send")
		streamSpan.SetAttribute("provider.name", selection.Provider)
		streamSpan.SetAttribute("provider.model", selection.Model)
		streamSpan.SetAttribute("provider.stream", "true")

		usage, err := p.SendStream(r.Context(), req, w)
		latencyMs := time.Since(start).Milliseconds()
		if err != nil {
			streamSpan.SetStatus("error")
			streamSpan.SetAttribute("error", err.Error())
			s.tracer.EndSpan(streamSpan)
			s.logger.Error("stream error", "error", err, "provider", selection.Provider)
			s.health.RecordFailure(selection.Provider, err)
			if cb := s.cbPool.Get(selection.Provider); cb != nil {
				cb.RecordFailure()
			}
			return
		}
		s.health.RecordSuccess(selection.Provider)
		if cb := s.cbPool.Get(selection.Provider); cb != nil {
			cb.RecordSuccess()
		}

		tokens := 0
		if usage != nil {
			tokens = usage.TotalTokens
		}
		cost := float64(tokens) / 1000.0 * s.router.GetModelCost(selection.Provider, selection.Model)

		streamSpan.SetAttribute("provider.tokens", fmt.Sprintf("%d", tokens))
		streamSpan.SetAttribute("provider.cost", fmt.Sprintf("%.6f", cost))
		s.tracer.EndSpan(streamSpan)

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

	// Non-streaming request with retry
	_, sendSpan := s.tracer.StartSpan(r.Context(), "provider.send")
	sendSpan.SetAttribute("provider.name", selection.Provider)
	sendSpan.SetAttribute("provider.model", selection.Model)
	sendSpan.SetAttribute("provider.stream", "false")

	var resp *provider.ChatResponse
	err = provider.RetryWithBackoff(provider.RetryConfig{
		MaxRetries: 2,
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   2 * time.Second,
	}, func() error {
		var sendErr error
		resp, sendErr = p.Send(r.Context(), req)
		return sendErr
	})
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		sendSpan.SetStatus("error")
		sendSpan.SetAttribute("error", err.Error())
		s.tracer.EndSpan(sendSpan)
		s.logger.Error("request error", "error", err, "provider", selection.Provider)
		s.health.RecordFailure(selection.Provider, err)
		if cb := s.cbPool.Get(selection.Provider); cb != nil {
			cb.RecordFailure()
		}
		http.Error(w, fmt.Sprintf(`{"error":"provider error: %v"}`, err), http.StatusBadGateway)
		return
	}
	s.health.RecordSuccess(selection.Provider)
	if cb := s.cbPool.Get(selection.Provider); cb != nil {
		cb.RecordSuccess()
	}

	tokens := resp.Usage.TotalTokens
	cost := float64(tokens) / 1000.0 * s.router.GetModelCost(selection.Provider, selection.Model)

	sendSpan.SetAttribute("provider.tokens", fmt.Sprintf("%d", tokens))
	sendSpan.SetAttribute("provider.cost", fmt.Sprintf("%.6f", cost))
	s.tracer.EndSpan(sendSpan)

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
	_, storeSpan := s.tracer.StartSpan(r.Context(), "cache.store")
	respBody, _ := json.Marshal(resp)
	s.cache.StoreResponse(promptText, selection.Model, respBody)
	storeSpan.SetAttribute("cache.model", selection.Model)
	s.tracer.EndSpan(storeSpan)

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

func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
	})
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	checks := map[string]any{}
	ready := true

	// Check: at least one provider configured
	providerCount := len(s.providers)
	providerOK := providerCount > 0
	checks["providers"] = map[string]any{
		"ok":    providerOK,
		"count": providerCount,
	}
	if !providerOK {
		ready = false
	}

	// Check: cache initialized (if enabled)
	if s.cfg.Cache.Enabled {
		cacheOK := s.cache != nil
		checks["cache"] = map[string]any{
			"ok":      cacheOK,
			"enabled": true,
		}
		if !cacheOK {
			ready = false
		}
	} else {
		checks["cache"] = map[string]any{
			"ok":      true,
			"enabled": false,
		}
	}

	// Check: server is accepting connections (if we got here, it is)
	checks["server"] = map[string]any{
		"ok": true,
	}

	status := "ok"
	statusCode := http.StatusOK
	if !ready {
		status = "not_ready"
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]any{
		"status": status,
		"checks": checks,
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

func (s *Server) handleSynonymStats(w http.ResponseWriter, r *http.Request) {
	registry := s.cache.Registry()
	if registry == nil {
		http.Error(w, "synonym registry not enabled", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(registry.Stats())
}

func (s *Server) handleSynonymCandidates(w http.ResponseWriter, r *http.Request) {
	registry := s.cache.Registry()
	if registry == nil {
		http.Error(w, "synonym registry not enabled", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(registry.GetCandidates())
}

func (s *Server) handleSynonymLearned(w http.ResponseWriter, r *http.Request) {
	registry := s.cache.Registry()
	if registry == nil {
		http.Error(w, "synonym registry not enabled", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(registry.GetLearnedSynonyms())
}

func (s *Server) handleSynonymPromote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	registry := s.cache.Registry()
	if registry == nil {
		http.Error(w, "synonym registry not enabled", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Term string `json:"term"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if registry.ManualPromote(req.Term) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "promoted", "term": req.Term})
	} else {
		http.Error(w, "candidate not found", http.StatusNotFound)
	}
}

func (s *Server) handleSynonymAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	registry := s.cache.Registry()
	if registry == nil {
		http.Error(w, "synonym registry not enabled", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Term      string `json:"term"`
		Expansion string `json:"expansion"`
		Type      string `json:"type"` // "synonym" or "key_noun"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Type == "key_noun" {
		registry.ManualAddKeyNoun(req.Term)
	} else {
		registry.ManualAdd(req.Term, req.Expansion)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "added", "term": req.Term})
}

// findFallbackProvider tries all other available providers when the primary is circuit-broken.
func (s *Server) findFallbackProvider(original router.ModelSelection) (provider.Provider, router.ModelSelection) {
	for name, p := range s.providers {
		if name == original.Provider {
			continue
		}
		if s.cbPool.IsAvailable(name) {
			// Use the first available provider with its first model
			original.Provider = name
			original.Reason = "circuit-breaker failover"
			return p, original
		}
	}
	return nil, original
}

func (s *Server) handleCircuitBreakers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.cbPool.AllStats())
}

// billingContextKey is used to store billing info in request context.
type billingContextKey string

const billingKeyHashCtx billingContextKey = "billing_key_hash"
const billingUserIDCtx billingContextKey = "billing_user_id"

// billingAuthMiddleware validates API keys on protected routes.
func (s *Server) billingAuthMiddleware() security.Middleware {
	skipPaths := map[string]bool{
		"/health":       true,
		"/health/live":  true,
		"/health/ready": true,
		"/metrics":      true,
		"/dashboard":    true,
		"/":             true,
		"/webhooks/stripe": true,
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if skipPaths[path] || strings.HasPrefix(path, "/dashboard/") {
				next.ServeHTTP(w, r)
				return
			}

			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer nxs_") {
				// No billing key — let the request through for non-billing auth
				next.ServeHTTP(w, r)
				return
			}

			rawKey := strings.TrimPrefix(auth, "Bearer ")
			apiKey, err := s.keyStore.ValidateKey(rawKey)
			if err != nil {
				if strings.Contains(err.Error(), "subscription") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusPaymentRequired)
					json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid API key"})
				return
			}

			// Check quota
			quota := s.keyStore.CheckQuota(apiKey.KeyHash)
			if !quota.Allowed {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", quota.ResetAt.Format(time.RFC3339))
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "monthly quota exceeded",
					"reset": quota.ResetAt.Format(time.RFC3339),
				})
				return
			}

			// Check device limit
			sub, found := s.subStore.Get(apiKey.SubscriptionID)
			if found {
				plan, planFound := s.subStore.GetPlan(sub.PlanID)
				if planFound && plan.MaxDevices > 0 {
					s.deviceTracker.RecordDevice(apiKey.UserID, r)
					devResult := s.deviceTracker.CheckDeviceLimit(apiKey.UserID, plan.MaxDevices)
					if !devResult.Allowed {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusForbidden)
						json.NewEncoder(w).Encode(map[string]string{
							"error": fmt.Sprintf("Device limit reached. Your plan allows %d devices. Upgrade or remove inactive devices.", devResult.Max),
						})
						return
					}
				} else if planFound {
					s.deviceTracker.RecordDevice(apiKey.UserID, r)
				}
			}

			// Record usage
			s.keyStore.RecordUsage(apiKey.KeyHash)

			// Store key info in context
			ctx := context.WithValue(r.Context(), billingKeyHashCtx, apiKey.KeyHash)
			ctx = context.WithValue(ctx, billingUserIDCtx, apiKey.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	sigHeader := r.Header.Get("Stripe-Signature")
	if err := billing.VerifyStripeSignature(body, sigHeader, s.cfg.Billing.StripeWebhookSecret); err != nil {
		s.logger.Warn("stripe webhook signature verification failed", "error", err)
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}

	var event billing.StripeEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if err := s.webhookHandler.HandleEvent(event); err != nil {
		s.logger.Error("stripe webhook handling failed", "error", err, "event_type", event.Type)
		http.Error(w, "webhook processing failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleAdminSubscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	subs := s.subStore.ListAll()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(subs)
}

func (s *Server) handleAdminKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimPrefix(r.URL.Path, "/api/admin/keys/")
	if userID == "" {
		http.Error(w, "user ID required", http.StatusBadRequest)
		return
	}
	keys := s.keyStore.ListByUser(userID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

func (s *Server) handleAdminDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimPrefix(r.URL.Path, "/api/admin/devices/")
	if userID == "" {
		http.Error(w, "user ID required", http.StatusBadRequest)
		return
	}
	devices := s.deviceTracker.ListByUser(userID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(devices)
}

func (s *Server) handleKeyGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		UserID         string   `json:"user_id"`
		SubscriptionID string   `json:"subscription_id"`
		Name           string   `json:"name"`
		Scopes         []string `json:"scopes"`
		IsTest         bool     `json:"is_test"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Use context user if available
	if ctxUser, ok := r.Context().Value(billingUserIDCtx).(string); ok && req.UserID == "" {
		req.UserID = ctxUser
	}

	if req.UserID == "" || req.SubscriptionID == "" {
		http.Error(w, "user_id and subscription_id required", http.StatusBadRequest)
		return
	}

	key, rawKey, err := s.keyStore.GenerateKey(req.UserID, req.SubscriptionID, req.Name, req.Scopes, req.IsTest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"key":      rawKey,
		"key_hash": key.KeyHash,
		"name":     key.Name,
		"scopes":   key.Scopes,
		"message":  "Save this key — it will not be shown again.",
	})
}

func (s *Server) handleKeyRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		KeyHash string `json:"key_hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.KeyHash == "" {
		http.Error(w, "key_hash required", http.StatusBadRequest)
		return
	}

	// Verify ownership
	key, found := s.keyStore.GetByHash(req.KeyHash)
	if !found {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	if ctxUser, ok := r.Context().Value(billingUserIDCtx).(string); ok {
		if key.UserID != ctxUser {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	if err := s.keyStore.RevokeKey(req.KeyHash); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}

	keyHash, _ := r.Context().Value(billingKeyHashCtx).(string)
	if keyHash == "" {
		http.Error(w, "API key required", http.StatusUnauthorized)
		return
	}

	key, found := s.keyStore.GetByHash(keyHash)
	if !found {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	quota := s.keyStore.CheckQuota(keyHash)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"key_prefix":    key.KeyPrefix,
		"request_count": key.RequestCount,
		"monthly_usage": key.MonthlyUsage,
		"monthly_reset": key.MonthlyReset.Format(time.RFC3339),
		"quota_allowed": quota.Allowed,
		"quota_remaining": quota.Remaining,
	})
}
