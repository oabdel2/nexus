package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nexus-gateway/nexus/internal/compress"
	"github.com/nexus-gateway/nexus/internal/dashboard"
	"github.com/nexus-gateway/nexus/internal/eval"
	"github.com/nexus-gateway/nexus/internal/events"
	"github.com/nexus-gateway/nexus/internal/experiment"
	"github.com/nexus-gateway/nexus/internal/plugin"
	"github.com/nexus-gateway/nexus/internal/provider"
	"github.com/nexus-gateway/nexus/internal/router"
	"github.com/nexus-gateway/nexus/internal/security"
	"github.com/nexus-gateway/nexus/internal/workflow"
)

// chatContext carries all mutable state for a single chat request through
// the handler pipeline. Each phase reads and/or writes fields here.
type chatContext struct {
	req           provider.ChatRequest
	promptText    string
	guardText     string
	contextLen    int
	originalModel string
	workflowID    string
	agentRole     string
	team          string
	ws            *workflow.WorkflowState
	selection     router.ModelSelection
	originalTier  string
	start         time.Time
	cascadeResp   *provider.ChatResponse
	httpReq       *http.Request
}

// parseRequest decodes the JSON body, extracts headers, and initialises the
// workflow state. Returns an error suitable for the client on failure.
func (s *Server) parseRequest(ctx *chatContext, r *http.Request) error {
	if err := json.NewDecoder(r.Body).Decode(&ctx.req); err != nil {
		return fmt.Errorf("Invalid JSON in request body")
	}

	ctx.workflowID = r.Header.Get("X-Workflow-ID")
	if ctx.workflowID == "" {
		ctx.workflowID = fmt.Sprintf("auto-%d", time.Now().UnixNano())
	}
	ctx.agentRole = r.Header.Get("X-Agent-Role")
	ctx.team = r.Header.Get("X-Team")
	ctx.ws = s.tracker.GetOrCreate(ctx.workflowID)
	ctx.httpReq = r
	return nil
}

// compressMessages applies prompt compression when enabled and records
// savings in metrics and dashboard.
func (s *Server) compressMessages(ctx *chatContext) {
	if !s.cfg.Compression.Enabled || s.compressor == nil {
		return
	}
	_, compSpan := s.tracer.StartSpan(ctx.httpReq.Context(), "compress.messages")
	compMessages := providerToCompressMessages(ctx.req.Messages)
	compressedMsgs, compResult := s.compressor.CompressMessages(compMessages)
	ctx.req.Messages = compressToProviderMessages(compressedMsgs)
	tokensSaved := compResult.OriginalTokens - compResult.CompressedTokens
	if tokensSaved > 0 {
		s.metrics.RecordCompressionSaved(tokensSaved)
		s.Dashboard.RecordCompressionSaved(tokensSaved)
	}
	compSpan.SetAttribute("compress.original_tokens", fmt.Sprintf("%d", compResult.OriginalTokens))
	compSpan.SetAttribute("compress.compressed_tokens", fmt.Sprintf("%d", compResult.CompressedTokens))
	compSpan.SetAttribute("compress.ratio", fmt.Sprintf("%.2f", compResult.Ratio))
	s.tracer.EndSpan(compSpan)
}

// checkPromptGuard runs injection detection on the full prompt text.
// Returns true if the prompt was blocked.
func (s *Server) checkPromptGuard(ctx *chatContext) bool {
	ctx.promptText = extractPromptText(ctx.req.Messages)
	ctx.guardText = fullPromptText(ctx.req.Messages)
	ctx.contextLen = len(ctx.guardText)
	ctx.originalModel = ctx.req.Model

	guard := security.GetPromptGuard(ctx.httpReq.Context())
	if guard == nil {
		return false
	}
	result := guard.Check(ctx.guardText)
	if result.Blocked {
		s.logger.Warn("prompt injection blocked",
			"threats", result.Threats,
			"risk_score", result.RiskScore,
		)
		return true
	}
	return false
}

// checkCache looks up the prompt in the cache. If a hit is found it writes
// the cached response directly to w and returns true.
func (s *Server) checkCache(ctx *chatContext, w http.ResponseWriter) bool {
	if !s.cfg.Cache.Enabled {
		return false
	}
	reqCtx, cacheSpan := s.tracer.StartSpan(ctx.httpReq.Context(), "cache.lookup")
	cached, hit, source := s.cache.Lookup(ctx.promptText, ctx.originalModel)
	if !hit {
		cacheSpan.SetAttribute("cache.hit", "false")
		s.tracer.EndSpan(cacheSpan)
		ctx.httpReq = ctx.httpReq.WithContext(reqCtx)
		return false
	}
	cacheSpan.SetAttribute("cache.hit", "true")
	cacheSpan.SetAttribute("cache.source", source)
	s.tracer.EndSpan(cacheSpan)

	s.logger.Info("cache hit", "workflow_id", ctx.workflowID, "source", source)

	ctx.ws.AddStep(workflow.StepRecord{Model: "cached", Tier: "cached", CacheHit: true})
	snap := ctx.ws.Snapshot()

	s.metrics.RecordRequest("cache", source, "cached", 0, 0, time.Since(ctx.start).Milliseconds(), true)
	s.costTracker.RecordStep(ctx.workflowID, ctx.team, 0, 0, true, 0.005)

	s.Dashboard.Push(dashboard.RequestEvent{
		Timestamp: time.Now(), WorkflowID: ctx.workflowID, Step: snap.CurrentStep,
		TierSelected: "cached", ModelUsed: "cached/" + source,
		LatencyMs: time.Since(ctx.start).Milliseconds(), CacheHit: true, Provider: "cache",
	})
	s.Dashboard.UpdateWorkflow(ctx.workflowID, snap.Budget, snap.BudgetLeft, ctx.ws.GetBudgetRatio(), snap.CurrentStep, snap.TotalCost)

	if s.eventBus != nil {
		s.eventBus.Emit(events.RequestCached, map[string]interface{}{
			"source": source, "latency_saved": time.Since(ctx.start).Milliseconds(), "workflow_id": ctx.workflowID,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Nexus-Cache", source)
	w.Header().Set("X-Nexus-Model", "cached")
	w.Header().Set("X-Nexus-Tier", "cached")
	w.Header().Set("X-Nexus-Provider", "cache/"+source)
	w.Header().Set("X-Nexus-Cost", "0.000000")
	w.Header().Set("X-Nexus-Workflow-ID", ctx.workflowID)
	w.Write(cached)
	return true
}

// routeRequest classifies the prompt, selects a model, fires plugin hooks,
// and emits budget/tier events.
func (s *Server) routeRequest(ctx *chatContext) {
	// Plugin PreRoute hook
	if s.pluginRegistry != nil {
		s.pluginRegistry.EmitRequest(ctx.httpReq.Context(), &plugin.RequestEvent{
			WorkflowID: ctx.workflowID, Step: ctx.ws.CurrentStep,
			APIKey: ctx.httpReq.Header.Get("Authorization"), Team: ctx.team,
		})
	}

	// Route the request
	_, routeSpan := s.tracer.StartSpan(ctx.httpReq.Context(), "router.classify")
	if s.adaptiveRouter != nil {
		ctx.selection = s.adaptiveRouter.Route(ctx.promptText, ctx.agentRole, ctx.ws.GetStepRatio(), ctx.ws.GetBudgetRatio(), ctx.contextLen)
	} else {
		ctx.selection = s.router.Route(ctx.promptText, ctx.agentRole, ctx.ws.GetStepRatio(), ctx.ws.GetBudgetRatio(), ctx.contextLen)
	}
	ctx.originalTier = ctx.selection.Tier
	routeSpan.SetAttribute("router.tier", ctx.selection.Tier)
	routeSpan.SetAttribute("router.model", ctx.selection.Model)
	routeSpan.SetAttribute("router.provider", ctx.selection.Provider)
	routeSpan.SetAttribute("router.score", fmt.Sprintf("%.4f", ctx.selection.Score.FinalScore))
	s.tracer.EndSpan(routeSpan)

	s.emitBudgetEvents(ctx)
}

// emitBudgetEvents fires budget and tier-downgrade events when appropriate.
func (s *Server) emitBudgetEvents(ctx *chatContext) {
	if s.eventBus == nil {
		return
	}
	budgetRatio := ctx.ws.GetBudgetRatio()
	if budgetRatio <= 0 {
		s.eventBus.Emit(events.BudgetExhausted, map[string]interface{}{
			"workflow_id": ctx.workflowID, "team": ctx.team,
			"budget": ctx.ws.Budget, "spent": ctx.ws.TotalCost,
		})
	} else if budgetRatio < 0.05 {
		s.eventBus.Emit(events.BudgetCritical, map[string]interface{}{
			"workflow_id": ctx.workflowID, "team": ctx.team,
			"budget_ratio": budgetRatio, "budget": ctx.ws.Budget, "spent": ctx.ws.TotalCost,
		})
	} else if budgetRatio < 0.15 {
		s.eventBus.Emit(events.BudgetWarning, map[string]interface{}{
			"workflow_id": ctx.workflowID, "team": ctx.team,
			"budget_ratio": budgetRatio, "budget": ctx.ws.Budget, "spent": ctx.ws.TotalCost,
		})
	}

	if ctx.selection.Tier != ctx.originalTier {
		s.eventBus.Emit(events.TierDowngrade, map[string]interface{}{
			"workflow_id": ctx.workflowID, "original_tier": ctx.originalTier,
			"new_tier": ctx.selection.Tier, "budget_ratio": budgetRatio,
		})
	}
}

// handleCascade attempts cascade routing (cheap-first). If the cascade is
// accepted and the request is non-streaming, the response is stored in
// ctx.cascadeResp for reuse. Returns true if the cascade produced a final
// response that was already written to w (currently never — cascade responses
// flow through the normal send path via ctx.cascadeResp).
func (s *Server) handleCascade(ctx *chatContext) {
	if !s.cfg.Cascade.IsEnabled() || ctx.req.Stream || s.cascade == nil {
		return
	}
	if !s.cascade.ShouldCascade(ctx.selection.Score, ctx.selection.Tier) {
		return
	}

	cascadeResult, cheapResp := s.tryCheapFirst(ctx.httpReq.Context(), ctx.req, ctx.selection, ctx.promptText)
	if cascadeResult == nil {
		return
	}

	if !cascadeResult.Escalated {
		s.metrics.RecordCascadeAttempt("accepted")
		s.Dashboard.RecordCascade(true)
		s.logger.Info("cascade accepted cheap model",
			"confidence", cascadeResult.CheapConfidence, "cost_saved", cascadeResult.CostSaved)
		ctx.selection = s.cascade.CheapSelection()
		if cheapResp != nil {
			ctx.cascadeResp = cheapResp
		}
	} else {
		s.metrics.RecordCascadeAttempt("escalated")
		s.metrics.RecordCascadeWaste(cascadeResult.WastedTokens)
		s.Dashboard.RecordCascade(false)
		s.logger.Info("cascade escalated to original tier",
			"confidence", cascadeResult.CheapConfidence, "original_tier", ctx.selection.Tier,
			"wasted_tokens", cascadeResult.WastedTokens)
		if s.eventBus != nil && cascadeResult.CheapConfidence < 0.3 {
			s.eventBus.Emit(events.CostAnomaly, map[string]interface{}{
				"workflow_id": ctx.workflowID, "cheap_confidence": cascadeResult.CheapConfidence,
				"original_tier": ctx.selection.Tier, "reason": "cascade escalation with very low cheap model confidence",
			})
		}
	}
}

// selectProvider picks the target provider (with circuit-breaker failover).
// Returns nil and writes an error to w if no provider is available.
func (s *Server) selectProvider(ctx *chatContext, w http.ResponseWriter) provider.Provider {
	p, ok := s.providers[ctx.selection.Provider]
	if !ok || !s.cbPool.IsAvailable(ctx.selection.Provider) {
		p, ctx.selection = s.findFallbackProvider(ctx.selection)
		if p == nil {
			writeNexusError(w, errProviderUnavailable(), http.StatusServiceUnavailable)
			return nil
		}
		s.logger.Info("circuit breaker failover",
			"original", ctx.selection.Provider, "fallback", ctx.selection.Provider)
	}
	ctx.req.Model = ctx.selection.Model
	return p
}

// handleStreaming processes a streaming chat request.
func (s *Server) handleStreaming(ctx *chatContext, w http.ResponseWriter, p provider.Provider) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Nexus-Model", ctx.selection.Model)
	w.Header().Set("X-Nexus-Tier", ctx.selection.Tier)
	w.Header().Set("X-Nexus-Provider", ctx.selection.Provider)

	_, streamSpan := s.tracer.StartSpan(ctx.httpReq.Context(), "provider.send")
	streamSpan.SetAttribute("provider.name", ctx.selection.Provider)
	streamSpan.SetAttribute("provider.model", ctx.selection.Model)
	streamSpan.SetAttribute("provider.stream", "true")

	streamBuf := provider.NewStreamBuffer()
	teeWriter := &streamTeeWriter{w: w, buf: streamBuf}

	usage, err := p.SendStream(ctx.httpReq.Context(), ctx.req, teeWriter)
	latencyMs := time.Since(ctx.start).Milliseconds()
	if err != nil {
		streamSpan.SetStatus("error")
		streamSpan.SetAttribute("error", err.Error())
		s.tracer.EndSpan(streamSpan)
		s.logger.Error("stream error", "error", err, "provider", ctx.selection.Provider)
		s.health.RecordFailure(ctx.selection.Provider, err)
		if cb := s.cbPool.Get(ctx.selection.Provider); cb != nil {
			cb.RecordFailure()
		}
		if s.eventBus != nil {
			s.eventBus.Emit(events.RequestError, map[string]interface{}{
				"provider": ctx.selection.Provider, "error": err.Error(),
				"workflow_id": ctx.workflowID, "model": ctx.selection.Model, "stream": true,
			})
		}
		return
	}
	s.health.RecordSuccess(ctx.selection.Provider)
	if cb := s.cbPool.Get(ctx.selection.Provider); cb != nil {
		cb.RecordSuccess()
	}

	tokens := 0
	if usage != nil {
		tokens = usage.TotalTokens
	}
	cost := float64(tokens) / 1000.0 * s.router.GetModelCost(ctx.selection.Provider, ctx.selection.Model)

	streamSpan.SetAttribute("provider.tokens", fmt.Sprintf("%d", tokens))
	streamSpan.SetAttribute("provider.cost", fmt.Sprintf("%.6f", cost))
	s.tracer.EndSpan(streamSpan)

	ctx.ws.AddStep(workflow.StepRecord{
		Model: ctx.selection.Model, Tier: ctx.selection.Tier,
		Tokens: tokens, Cost: cost, LatencyMs: latencyMs,
	})
	streamSnap := ctx.ws.Snapshot()

	s.metrics.RecordRequest(ctx.selection.Provider, ctx.selection.Model, ctx.selection.Tier, tokens, cost, latencyMs, false)
	s.costTracker.RecordStep(ctx.workflowID, ctx.team, cost, tokens, false, 0)

	s.Dashboard.Push(dashboard.RequestEvent{
		Timestamp: time.Now(), WorkflowID: ctx.workflowID, Step: streamSnap.CurrentStep,
		ComplexityScore: ctx.selection.Score.FinalScore, TierSelected: ctx.selection.Tier,
		ModelUsed: ctx.selection.Model, LatencyMs: latencyMs, Cost: cost, Provider: ctx.selection.Provider,
	})
	s.Dashboard.UpdateWorkflow(ctx.workflowID, streamSnap.Budget, streamSnap.BudgetLeft, ctx.ws.GetBudgetRatio(), streamSnap.CurrentStep, streamSnap.TotalCost)

	if s.eventBus != nil {
		s.eventBus.Emit(events.RequestCompleted, map[string]interface{}{
			"tier": ctx.selection.Tier, "model": ctx.selection.Model, "cost": cost,
			"latency_ms": latencyMs, "cache_hit": false, "workflow_id": ctx.workflowID,
			"provider": ctx.selection.Provider, "stream": true,
		})
	}

	if s.pluginRegistry != nil {
		s.pluginRegistry.EmitResponse(ctx.httpReq.Context(), &plugin.ResponseEvent{
			WorkflowID: ctx.workflowID, Step: streamSnap.CurrentStep,
			Model: ctx.selection.Model, Tier: ctx.selection.Tier,
			LatencyMs: float64(latencyMs), Cost: cost, TokensOut: tokens,
		})
	}

	if s.cfg.Cache.Enabled {
		if cacheData, ok := streamBuf.CacheableJSON(); ok {
			s.cache.StoreResponse(ctx.promptText, ctx.originalModel, cacheData)
		}
	}
}

// handleNonStreaming processes a non-streaming chat request.
func (s *Server) handleNonStreaming(ctx *chatContext, w http.ResponseWriter, p provider.Provider) {
	_, sendSpan := s.tracer.StartSpan(ctx.httpReq.Context(), "provider.send")
	sendSpan.SetAttribute("provider.name", ctx.selection.Provider)
	sendSpan.SetAttribute("provider.model", ctx.selection.Model)
	sendSpan.SetAttribute("provider.stream", "false")

	var resp *provider.ChatResponse
	if ctx.cascadeResp != nil {
		resp = ctx.cascadeResp
	} else {
		err := provider.RetryWithBackoff(provider.RetryConfig{
			MaxRetries: 2, BaseDelay: 100 * time.Millisecond, MaxDelay: 2 * time.Second,
		}, func() error {
			var sendErr error
			resp, sendErr = p.Send(ctx.httpReq.Context(), ctx.req)
			return sendErr
		})
		if err != nil {
			sendSpan.SetStatus("error")
			sendSpan.SetAttribute("error", err.Error())
			s.tracer.EndSpan(sendSpan)
			s.logger.Error("request error", "error", err, "provider", ctx.selection.Provider)
			s.health.RecordFailure(ctx.selection.Provider, err)
			if cb := s.cbPool.Get(ctx.selection.Provider); cb != nil {
				cb.RecordFailure()
			}
			if s.eventBus != nil {
				s.eventBus.Emit(events.RequestError, map[string]interface{}{
					"provider": ctx.selection.Provider, "error": err.Error(),
					"workflow_id": ctx.workflowID, "model": ctx.selection.Model,
				})
			}
			writeNexusError(w, errProviderError(""), http.StatusBadGateway)
			return
		}
		s.health.RecordSuccess(ctx.selection.Provider)
		if cb := s.cbPool.Get(ctx.selection.Provider); cb != nil {
			cb.RecordSuccess()
		}
	}
	latencyMs := time.Since(ctx.start).Milliseconds()
	tokens := resp.Usage.TotalTokens
	cost := float64(tokens) / 1000.0 * s.router.GetModelCost(ctx.selection.Provider, ctx.selection.Model)

	if resp.Usage.CachedTokens > 0 {
		s.metrics.RecordProviderCachedTokens(resp.Usage.CachedTokens)
	}

	sendSpan.SetAttribute("provider.tokens", fmt.Sprintf("%d", tokens))
	sendSpan.SetAttribute("provider.cost", fmt.Sprintf("%.6f", cost))
	sendSpan.SetAttribute("provider.cached_tokens", fmt.Sprintf("%d", resp.Usage.CachedTokens))
	s.tracer.EndSpan(sendSpan)

	ctx.ws.AddStep(workflow.StepRecord{
		Model: ctx.selection.Model, Tier: ctx.selection.Tier,
		Tokens: tokens, Cost: cost, LatencyMs: latencyMs,
	})
	reqSnap := ctx.ws.Snapshot()

	s.metrics.RecordRequest(ctx.selection.Provider, ctx.selection.Model, ctx.selection.Tier, tokens, cost, latencyMs, false)
	s.costTracker.RecordStep(ctx.workflowID, ctx.team, cost, tokens, false, 0)

	s.Dashboard.Push(dashboard.RequestEvent{
		Timestamp: time.Now(), WorkflowID: ctx.workflowID, Step: reqSnap.CurrentStep,
		ComplexityScore: ctx.selection.Score.FinalScore, TierSelected: ctx.selection.Tier,
		ModelUsed: ctx.selection.Model, LatencyMs: latencyMs, Cost: cost, Provider: ctx.selection.Provider,
	})
	s.Dashboard.UpdateWorkflow(ctx.workflowID, reqSnap.Budget, reqSnap.BudgetLeft, ctx.ws.GetBudgetRatio(), reqSnap.CurrentStep, reqSnap.TotalCost)

	// Eval scoring
	var evalResult *eval.ConfidenceResult
	if s.cfg.Eval.Enabled && s.confidenceScorer != nil && len(resp.Choices) > 0 {
		_, evalSpan := s.tracer.StartSpan(ctx.httpReq.Context(), "eval.score")
		er := s.confidenceScorer.CombinedScore(
			resp.Choices[0].Message.Content, resp.Usage.PromptTokens,
			resp.Usage.CompletionTokens, resp.Choices[0].FinishReason,
		)
		evalResult = &er
		if s.confidenceMap != nil {
			taskType := eval.ClassifyTaskType(ctx.promptText)
			s.confidenceMap.Record(taskType, ctx.selection.Tier, evalResult.Score)
		}
		s.metrics.RecordEvalConfidence(evalResult.Score)
		evalSpan.SetAttribute("eval.score", fmt.Sprintf("%.3f", evalResult.Score))
		evalSpan.SetAttribute("eval.recommendation", evalResult.Recommendation)
		s.tracer.EndSpan(evalSpan)
		w.Header().Set("X-Nexus-Confidence", fmt.Sprintf("%.3f", evalResult.Score))
	}

	// Shadow evaluation
	if s.cfg.Eval.ShadowEnabled && s.cfg.Eval.ShadowSampleRate > 0 {
		if shouldShadow(ctx.workflowID, s.cfg.Eval.ShadowSampleRate) {
			select {
			case s.shadowSem <- struct{}{}:
				go func() {
					defer func() { <-s.shadowSem }()
					s.runShadowEval(ctx.httpReq.Context(), ctx.req, ctx.promptText, ctx.selection, resp)
				}()
			default:
			}
		}
	}

	// A/B experiment
	var expAssignment *experiment.Assignment
	if s.experimentMgr != nil {
		expAssignment = s.experimentMgr.GetAssignment(ctx.workflowID)
	}
	if expAssignment != nil {
		var evalScore float64
		if evalResult != nil {
			evalScore = evalResult.Score
		}
		s.experimentMgr.RecordMetric(ctx.workflowID, experiment.MetricEvent{
			Cost: cost, Tokens: int64(tokens), LatencyMs: latencyMs,
			Confidence: evalScore,
		})
	}

	// Cache store
	_, storeSpan := s.tracer.StartSpan(ctx.httpReq.Context(), "cache.store")
	respBody, _ := json.Marshal(resp)
	s.cache.StoreResponse(ctx.promptText, ctx.originalModel, respBody)
	storeSpan.SetAttribute("cache.model", ctx.selection.Model)
	s.tracer.EndSpan(storeSpan)

	// X-Nexus-Explain
	if ctx.httpReq.Header.Get("X-Nexus-Explain") == "true" {
		var respMap map[string]any
		if err := json.Unmarshal(respBody, &respMap); err == nil {
			explanation := map[string]any{
				"complexity_score": ctx.selection.Score, "tier_decision": ctx.selection.Tier,
				"reason": ctx.selection.Reason, "provider": ctx.selection.Provider,
				"model": ctx.selection.Model, "cache_checked": s.cfg.Cache.Enabled,
				"cache_result": "miss",
			}
			if evalResult != nil {
				explanation["confidence"] = map[string]any{
					"score": evalResult.Score, "recommendation": evalResult.Recommendation,
					"signals": evalResult.Signals,
				}
			}
			explanation["compression"] = s.cfg.Compression.Enabled
			explanation["cascade_enabled"] = s.cfg.Cascade.IsEnabled()
			respMap["nexus_explain"] = explanation
			respBody, _ = json.Marshal(respMap)
		}
	}

	// Emit RequestCompleted event
	if s.eventBus != nil {
		s.eventBus.Emit(events.RequestCompleted, map[string]interface{}{
			"tier": ctx.selection.Tier, "model": ctx.selection.Model, "cost": cost,
			"latency_ms": latencyMs, "cache_hit": false, "workflow_id": ctx.workflowID,
			"provider": ctx.selection.Provider,
		})
	}

	// Plugin PostResponse hook
	if s.pluginRegistry != nil {
		s.pluginRegistry.EmitResponse(ctx.httpReq.Context(), &plugin.ResponseEvent{
			WorkflowID: ctx.workflowID, Step: reqSnap.CurrentStep,
			Model: ctx.selection.Model, Tier: ctx.selection.Tier,
			LatencyMs: float64(latencyMs), Cost: cost,
			TokensIn: resp.Usage.PromptTokens, TokensOut: resp.Usage.CompletionTokens,
		})
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Nexus-Model", ctx.selection.Model)
	w.Header().Set("X-Nexus-Tier", ctx.selection.Tier)
	w.Header().Set("X-Nexus-Provider", ctx.selection.Provider)
	w.Header().Set("X-Nexus-Cost", fmt.Sprintf("%.6f", cost))
	w.Header().Set("X-Nexus-Workflow-ID", ctx.workflowID)
	w.Header().Set("X-Nexus-Workflow-Step", fmt.Sprintf("%d", reqSnap.CurrentStep))
	w.Write(respBody)
}

// Ensure unused imports are consumed. The compress package is used
// indirectly via providerToCompressMessages in handler_helpers.go, but
// we import it here to satisfy the compiler since compressMessages
// references compress.Message through the helper functions.
var _ compress.CompressionResult
