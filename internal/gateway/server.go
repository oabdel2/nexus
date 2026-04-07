package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nexus-gateway/nexus/internal/billing"
	"github.com/nexus-gateway/nexus/internal/cache"
	"github.com/nexus-gateway/nexus/internal/compress"
	"github.com/nexus-gateway/nexus/internal/config"
	"github.com/nexus-gateway/nexus/internal/dashboard"
	"github.com/nexus-gateway/nexus/internal/eval"
	"github.com/nexus-gateway/nexus/internal/events"
	"github.com/nexus-gateway/nexus/internal/experiment"
	"github.com/nexus-gateway/nexus/internal/mcp"
	"github.com/nexus-gateway/nexus/internal/notification"
	"github.com/nexus-gateway/nexus/internal/plugin"
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
	cbPool        *provider.Pool
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
	compressor       *compress.Compressor
	confidenceScorer *eval.ConfidenceScorer
	confidenceMap    *eval.ConfidenceMap
	cascade          *router.CascadeRouter
	adaptiveRouter   *router.AdaptiveRouter
	experimentMgr    *experiment.Manager
	eventBus         *events.EventBus
	pluginRegistry   *plugin.Registry
	mcpServer        *mcp.Server
	requestSem       chan struct{}
	shadowSem        chan struct{} // limits concurrent shadow eval goroutines
	warmupDone       bool         // true after startup warmup completes
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
		cbPool: provider.NewPool(provider.CircuitBreakerConfig{
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
			p = provider.NewAnthropic(pc.Name, pc.BaseURL, pc.APIKey)
		}
		if p != nil {
			s.providers[pc.Name] = p
			s.health.Register(p)
			s.cbPool.Register(pc.Name)
		}
	}

	// Init router
	s.router = router.New(cfg.Router, cfg.Providers, logger)

	// Init compressor
	if cfg.Compression.Enabled {
		s.compressor = compress.New(compress.CompressorConfig{
			EnableWhitespace:    cfg.Compression.Whitespace,
			EnableCodeStrip:     cfg.Compression.CodeStrip,
			EnableHistoryTrunc:  cfg.Compression.HistoryTruncate,
			EnableBoilerplate:   cfg.Compression.Boilerplate,
			EnableJSONMinify:    cfg.Compression.JSONMinify,
			EnableDeduplication: cfg.Compression.Deduplication,
			MaxHistoryTurns:     cfg.Compression.MaxHistoryTurns,
			PreserveLastN:       cfg.Compression.PreserveLastN,
		})
	}

	// Init eval
	if cfg.Eval.Enabled {
		s.confidenceScorer = eval.NewScorer(eval.ScorerConfig{
			HedgingPenalty:   cfg.Eval.HedgingPenalty,
			BrevityThreshold: 20,
			StructureBonus:   0.10,
		})
		s.confidenceMap = eval.NewConfidenceMap()
		// Try loading existing confidence data
		cmPath := filepath.Join(cfg.Eval.DataDir, "confidence_map.json")
		if err := s.confidenceMap.Load(cmPath); err != nil {
			logger.Info("no existing confidence map, starting fresh", "path", cmPath)
		}
	}

	// Init cascade router
	if cfg.Cascade.IsEnabled() {
		s.cascade = router.NewCascadeRouter(s.router, cfg.Cascade.ConfidenceThreshold, cfg.Cascade.MaxLatencyMs, cfg.Cascade.SampleRate)
	}

	// Init adaptive router
	if cfg.Adaptive.Enabled && s.confidenceMap != nil {
		s.adaptiveRouter = router.NewAdaptiveRouter(s.router, s.confidenceMap, cfg.Adaptive)
	}

	// Init experiment manager
	if cfg.Experiment.Enabled {
		s.experimentMgr = experiment.NewManager()
		if cfg.Experiment.AutoStart {
			s.experimentMgr.RegisterExperiment(experiment.CascadeThresholdExperiment())
			s.experimentMgr.RegisterExperiment(experiment.CompressionExperiment())
			s.experimentMgr.RegisterExperiment(experiment.TierThresholdExperiment())
			s.experimentMgr.RegisterExperiment(experiment.CacheAggressivenessExperiment())
		}
	}

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

	// Init event bus (if enabled)
	if cfg.Events.Enabled {
		var hooks []events.WebhookConfig
		for _, url := range cfg.Events.WebhookURLs {
			hooks = append(hooks, events.WebhookConfig{
				URL:    url,
				Events: []string{"*"},
				Secret: cfg.Events.WebhookSecret,
			})
		}
		s.eventBus = events.NewEventBus(hooks)
	}

	// Init plugin registry (if enabled)
	if cfg.Plugins.Enabled {
		s.pluginRegistry = plugin.NewRegistry()
	}

	// Init request semaphore for admission control
	maxConcurrent := cfg.Server.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 500
	}
	s.requestSem = make(chan struct{}, maxConcurrent)
	s.shadowSem = make(chan struct{}, 50) // limit concurrent shadow evals

	// Wire circuit breaker state changes to events
	if s.eventBus != nil {
		for name := range s.providers {
			cb := s.cbPool.Get(name)
			if cb != nil {
				providerName := name
				cb.OnStateChange = func(prov string, from, to provider.CBState) {
					if to == provider.StateOpen {
						s.eventBus.Emit(events.ProviderUnhealthy, map[string]interface{}{
							"provider":       providerName,
							"previous_state": from.String(),
						})
					} else if to == provider.StateClosed && from != provider.StateClosed {
						s.eventBus.Emit(events.ProviderRecovered, map[string]interface{}{
							"provider":       providerName,
							"previous_state": from.String(),
						})
					}
				}
			}
		}
	}

	// MCP server initialization
	if cfg.MCP.Enabled {
		s.mcpServer = mcp.NewServer(logger)
		s.registerMCPTools()
	}

	return s
}

func (s *Server) Start(ctx context.Context) error {
	// Pre-flight validation and warmup
	validator := NewStartupValidator(s.logger)

	warnings := validator.ValidateConfig(s)
	for _, w := range warnings {
		s.logger.Warn(w)
	}

	validator.CheckProviderReachability(s)
	validator.CheckOllamaModels(s)
	validator.WarmupModels(s)
	s.warmupDone = true

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

	// Eval and compression stats endpoints
	mux.HandleFunc("/api/eval/stats", s.handleEvalStats)
	mux.HandleFunc("/api/compression/stats", s.handleCompressionStats)

	// Shadow evaluation stats
	mux.HandleFunc("/api/shadow/stats", s.handleShadowStats)

	// Adaptive routing stats
	mux.HandleFunc("/api/adaptive/stats", s.handleAdaptiveStats)

	// Experiment endpoints
	mux.HandleFunc("/api/experiments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			s.handleExperimentCreate(w, r)
		} else {
			s.handleExperiments(w, r)
		}
	})
	mux.HandleFunc("/api/experiments/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/results") {
			s.handleExperimentResults(w, r)
		} else if strings.HasSuffix(path, "/toggle") {
			s.handleExperimentToggle(w, r)
		} else {
			http.NotFound(w, r)
		}
	})

	// Request inspector endpoint
	mux.HandleFunc("/api/inspect", s.handleInspect)

	// Events endpoints
	if s.eventBus != nil {
		mux.HandleFunc("/api/events/recent", s.handleEventsRecent)
		mux.HandleFunc("/api/events/stats", s.handleEventsStats)
	}

	// Plugins endpoint
	if s.pluginRegistry != nil {
		mux.HandleFunc("/api/plugins", s.handlePlugins)
	}

	// MCP endpoint
	if s.mcpServer != nil {
		mux.HandleFunc("/mcp", s.mcpServer.HandleHTTP)
	}

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

	// Admin endpoint protection: defense-in-depth guard for /api/admin/*
	middlewares = append(middlewares, security.AdminRequired())

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

	// Error sanitizer: intercepts 5xx responses leaking internal details
	middlewares = append(middlewares, security.ErrorSanitizer(s.logger))

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

// Shutdown gracefully drains in-flight requests and stops all subsystems.
// It first stops accepting new HTTP connections via httpServer.Shutdown, then
// waits for all in-flight requests (tracked by requestSem) to complete or
// until ctx is cancelled.
func (s *Server) Shutdown(ctx context.Context) error {
	// Stop accepting new connections first
	err := s.httpServer.Shutdown(ctx)

	// Wait for in-flight requests to drain (each request holds a semaphore slot)
	s.drainRequests(ctx)

	// Stop cache cleanup goroutines
	if s.cache != nil {
		s.cache.Stop()
	}

	if s.tracker != nil {
		s.tracker.Stop()
	}
	if s.confidenceMap != nil {
		cmPath := filepath.Join(s.cfg.Eval.DataDir, "confidence_map.json")
		if err := os.MkdirAll(s.cfg.Eval.DataDir, 0755); err == nil {
			if err := s.confidenceMap.Save(cmPath); err != nil {
				s.logger.Error("failed to save confidence map", "error", err)
			}
		}
	}
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
	if s.eventBus != nil {
		s.eventBus.Close()
	}
	return err
}

// drainRequests waits until all in-flight requests have released the semaphore
// or ctx expires. It fills the semaphore to capacity, meaning all slots are idle.
func (s *Server) drainRequests(ctx context.Context) {
	capacity := cap(s.requestSem)
	drained := 0
	for drained < capacity {
		select {
		case s.requestSem <- struct{}{}:
			drained++
		case <-ctx.Done():
			// Drain timeout expired — release any slots we acquired
			for i := 0; i < drained; i++ {
				<-s.requestSem
			}
			s.logger.Warn("drain timeout: some requests may not have completed",
				"drained", drained, "capacity", capacity)
			return
		}
	}
	// Release all slots we acquired
	for i := 0; i < drained; i++ {
		<-s.requestSem
	}
}

// SemaphoreUsage returns the fraction of the request semaphore currently in use (0.0–1.0).
func (s *Server) SemaphoreUsage() float64 {
	capacity := cap(s.requestSem)
	if capacity == 0 {
		return 0
	}
	inUse := len(s.requestSem)
	return float64(inUse) / float64(capacity)
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

	// Check: warmup complete (startup readiness delay)
	if !s.warmupDone {
		checks["warmup"] = map[string]any{
			"ok": false,
		}
		ready = false
	} else {
		checks["warmup"] = map[string]any{
			"ok": true,
		}
	}

	// Check: semaphore load — degrade to 503 when >80% full (overloaded)
	semUsage := s.SemaphoreUsage()
	semOverloaded := semUsage > 0.80
	checks["semaphore"] = map[string]any{
		"ok":    !semOverloaded,
		"usage": fmt.Sprintf("%.0f%%", semUsage*100),
	}
	if semOverloaded {
		ready = false
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

	providerNames := make([]string, 0, len(s.providers))
	for name := range s.providers {
		providerNames = append(providerNames, name)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"service":     "nexus",
		"name":        "nexus",
		"version":     "0.1.0",
		"description": "Agentic-first inference optimization gateway",
		"status":      "operational",
		"providers":   providerNames,
		"endpoints": []string{
			"/v1/chat/completions",
			"/v1/feedback",
			"/health",
			"/health/live",
			"/health/ready",
			"/metrics",
			"/dashboard",
			"/api/synonyms/stats",
			"/api/circuit-breakers",
			"/api/inspect",
		},
		"cache": map[string]any{
			"hits":   hits,
			"misses": misses,
			"size":   cacheSize,
			"layers": map[string]bool{
				"l1_exact":    s.cfg.Cache.L1Enabled || s.cfg.Cache.L1.Enabled,
				"l2_bm25":    s.cfg.Cache.L2BM25.Enabled,
				"l2_semantic": s.cfg.Cache.L2Semantic.Enabled,
			},
		},
		"security": map[string]bool{
			"tls":          s.cfg.Security.TLS.Enabled,
			"rate_limit":   s.cfg.Security.RateLimit.Enabled,
			"prompt_guard": s.cfg.Security.PromptGuard.Enabled,
			"oidc":         s.cfg.Security.OIDC.Enabled,
		},
		"requests_total": s.metrics.RequestsTotal.Load(),
	})
}
