package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

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

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeNexusError(w, errMethodNotAllowed(), http.StatusMethodNotAllowed)
		return
	}

	// Admission control: limit concurrent requests
	select {
	case s.requestSem <- struct{}{}:
		defer func() { <-s.requestSem }()
	default:
		writeNexusError(w, errServiceOverloaded(), http.StatusServiceUnavailable)
		return
	}

	start := time.Now()

	// Parse request using streaming decoder to avoid full-body copy
	var req provider.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeNexusError(w, errInvalidRequest("Invalid JSON in request body"), http.StatusBadRequest)
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

	// Compress messages before processing
	if s.cfg.Compression.Enabled && s.compressor != nil {
		_, compSpan := s.tracer.StartSpan(r.Context(), "compress.messages")
		compMessages := providerToCompressMessages(req.Messages)
		compressedMsgs, compResult := s.compressor.CompressMessages(compMessages)
		req.Messages = compressToProviderMessages(compressedMsgs)
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

	// Build prompt text for classification (from potentially compressed messages)
	promptText := extractPromptText(req.Messages)
	guardText := fullPromptText(req.Messages)
	contextLen := len(guardText)

	// Check for prompt injection
	if guard := security.GetPromptGuard(r.Context()); guard != nil {
		result := guard.Check(guardText)
		if result.Blocked {
			s.logger.Warn("prompt injection blocked",
				"threats", result.Threats,
				"risk_score", result.RiskScore,
			)
			writeNexusError(w, errPromptBlocked(), http.StatusBadRequest)
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
			snap := ws.Snapshot()

			s.metrics.RecordRequest("cache", source, "cached", 0, 0, time.Since(start).Milliseconds(), true)
			s.costTracker.RecordStep(workflowID, team, 0, 0, true, 0.005)

			s.Dashboard.Push(dashboard.RequestEvent{
				Timestamp:       time.Now(),
				WorkflowID:      workflowID,
				Step:            snap.CurrentStep,
				ComplexityScore: 0,
				TierSelected:    "cached",
				ModelUsed:       "cached/" + source,
				LatencyMs:       time.Since(start).Milliseconds(),
				Cost:            0,
				CacheHit:        true,
				Provider:        "cache",
			})
			s.Dashboard.UpdateWorkflow(workflowID, snap.Budget, snap.BudgetLeft, ws.GetBudgetRatio(), snap.CurrentStep, snap.TotalCost)

			// Emit cache hit event
			if s.eventBus != nil {
				latencySaved := time.Since(start).Milliseconds()
				s.eventBus.Emit(events.RequestCached, map[string]interface{}{
					"source":        source,
					"latency_saved": latencySaved,
					"workflow_id":   workflowID,
				})
			}

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Nexus-Cache", source)
			w.Header().Set("X-Nexus-Model", "cached")
			w.Header().Set("X-Nexus-Tier", "cached")
			w.Header().Set("X-Nexus-Provider", "cache/"+source)
			w.Header().Set("X-Nexus-Cost", "0.000000")
			w.Header().Set("X-Nexus-Workflow-ID", workflowID)
			w.Write(cached)
			return
		}
		cacheSpan.SetAttribute("cache.hit", "false")
		s.tracer.EndSpan(cacheSpan)
		r = r.WithContext(ctx)
		_ = cacheKey
	}

	// Plugin PreRoute hook: let plugins modify messages or override tier before routing
	if s.pluginRegistry != nil {
		s.pluginRegistry.EmitRequest(r.Context(), &plugin.RequestEvent{
			WorkflowID: workflowID,
			Step:       ws.CurrentStep,
			Tier:       "",
			Score:      0,
			APIKey:     r.Header.Get("Authorization"),
			Team:       team,
		})
	}

	// Route the request
	_, routeSpan := s.tracer.StartSpan(r.Context(), "router.classify")
	var selection router.ModelSelection
	if s.adaptiveRouter != nil {
		selection = s.adaptiveRouter.Route(promptText, agentRole, ws.GetStepRatio(), ws.GetBudgetRatio(), contextLen)
	} else {
		selection = s.router.Route(promptText, agentRole, ws.GetStepRatio(), ws.GetBudgetRatio(), contextLen)
	}
	originalTier := selection.Tier
	routeSpan.SetAttribute("router.tier", selection.Tier)
	routeSpan.SetAttribute("router.model", selection.Model)
	routeSpan.SetAttribute("router.provider", selection.Provider)
	routeSpan.SetAttribute("router.score", fmt.Sprintf("%.4f", selection.Score.FinalScore))
	s.tracer.EndSpan(routeSpan)

	// Emit budget events based on current budget ratio
	budgetRatio := ws.GetBudgetRatio()
	if s.eventBus != nil {
		if budgetRatio <= 0 {
			s.eventBus.Emit(events.BudgetExhausted, map[string]interface{}{
				"workflow_id": workflowID,
				"team":        team,
				"budget":      ws.Budget,
				"spent":       ws.TotalCost,
			})
		} else if budgetRatio < 0.05 {
			s.eventBus.Emit(events.BudgetCritical, map[string]interface{}{
				"workflow_id":  workflowID,
				"team":         team,
				"budget_ratio": budgetRatio,
				"budget":       ws.Budget,
				"spent":        ws.TotalCost,
			})
		} else if budgetRatio < 0.15 {
			s.eventBus.Emit(events.BudgetWarning, map[string]interface{}{
				"workflow_id":  workflowID,
				"team":         team,
				"budget_ratio": budgetRatio,
				"budget":       ws.Budget,
				"spent":        ws.TotalCost,
			})
		}
	}

	// Emit tier downgrade event if budget pressure changed the tier
	if s.eventBus != nil && selection.Tier != originalTier {
		s.eventBus.Emit(events.TierDowngrade, map[string]interface{}{
			"workflow_id":   workflowID,
			"original_tier": originalTier,
			"new_tier":      selection.Tier,
			"budget_ratio":  budgetRatio,
		})
	}

	// Cascade: try cheap model first if router picked expensive tier (non-streaming only)
	var cascadeResp *provider.ChatResponse
	if s.cfg.Cascade.Enabled && !req.Stream && s.cascade != nil && s.cascade.ShouldCascade(selection.Score, selection.Tier) {
		cascadeResult, cheapResp := s.tryCheapFirst(r.Context(), req, selection, promptText)
		if cascadeResult != nil && !cascadeResult.Escalated {
			s.metrics.RecordCascadeAttempt("accepted")
			s.Dashboard.RecordCascade(true)
			s.logger.Info("cascade accepted cheap model",
				"confidence", cascadeResult.CheapConfidence,
				"cost_saved", cascadeResult.CostSaved,
			)
			// Cheap model was good enough — update selection to cheap
			selection = s.cascade.CheapSelection()
			// Reuse cheap response for non-streaming to avoid double-send
			if !req.Stream && cheapResp != nil {
				cascadeResp = cheapResp
			}
		} else if cascadeResult != nil {
			s.metrics.RecordCascadeAttempt("escalated")
			s.metrics.RecordCascadeWaste(cascadeResult.WastedTokens)
			s.Dashboard.RecordCascade(false)
			s.logger.Info("cascade escalated to original tier",
				"confidence", cascadeResult.CheapConfidence,
				"original_tier", selection.Tier,
				"wasted_tokens", cascadeResult.WastedTokens,
			)
			// Emit CostAnomaly if cheap model confidence was very low
			if s.eventBus != nil && cascadeResult.CheapConfidence < 0.3 {
				s.eventBus.Emit(events.CostAnomaly, map[string]interface{}{
					"workflow_id":      workflowID,
					"cheap_confidence": cascadeResult.CheapConfidence,
					"original_tier":    selection.Tier,
					"reason":           "cascade escalation with very low cheap model confidence",
				})
			}
		}
	}

	// Get the provider with circuit breaker check
	p, ok := s.providers[selection.Provider]
	if !ok || !s.cbPool.IsAvailable(selection.Provider) {
		// Try failover to any available provider with same tier
		p, selection = s.findFallbackProvider(selection)
		if p == nil {
			writeNexusError(w, errProviderUnavailable(), http.StatusServiceUnavailable)
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

		// Tee stream to both client and buffer for caching
		streamBuf := provider.NewStreamBuffer()
		teeWriter := &streamTeeWriter{w: w, buf: streamBuf}

		usage, err := p.SendStream(r.Context(), req, teeWriter)
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
			if s.eventBus != nil {
				s.eventBus.Emit(events.RequestError, map[string]interface{}{
					"provider":    selection.Provider,
					"error":       err.Error(),
					"workflow_id": workflowID,
					"model":       selection.Model,
					"stream":      true,
				})
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
		streamSnap := ws.Snapshot()

		s.metrics.RecordRequest(selection.Provider, selection.Model, selection.Tier, tokens, cost, latencyMs, false)
		s.costTracker.RecordStep(workflowID, team, cost, tokens, false, 0)

		s.Dashboard.Push(dashboard.RequestEvent{
			Timestamp:       time.Now(),
			WorkflowID:      workflowID,
			Step:            streamSnap.CurrentStep,
			ComplexityScore: selection.Score.FinalScore,
			TierSelected:    selection.Tier,
			ModelUsed:       selection.Model,
			LatencyMs:       latencyMs,
			Cost:            cost,
			CacheHit:        false,
			Provider:        selection.Provider,
		})
		s.Dashboard.UpdateWorkflow(workflowID, streamSnap.Budget, streamSnap.BudgetLeft, ws.GetBudgetRatio(), streamSnap.CurrentStep, streamSnap.TotalCost)

		// Emit stream RequestCompleted event
		if s.eventBus != nil {
			s.eventBus.Emit(events.RequestCompleted, map[string]interface{}{
				"tier":        selection.Tier,
				"model":       selection.Model,
				"cost":        cost,
				"latency_ms":  latencyMs,
				"cache_hit":   false,
				"workflow_id": workflowID,
				"provider":    selection.Provider,
				"stream":      true,
			})
		}

		// Plugin PostResponse hook (streaming)
		if s.pluginRegistry != nil {
			s.pluginRegistry.EmitResponse(r.Context(), &plugin.ResponseEvent{
				WorkflowID: workflowID,
				Step:       streamSnap.CurrentStep,
				Model:      selection.Model,
				Tier:       selection.Tier,
				LatencyMs:  float64(latencyMs),
				Cost:       cost,
				CacheHit:   false,
				TokensIn:   0,
				TokensOut:  tokens,
			})
		}

		// Cache the assembled streaming response
		if s.cfg.Cache.Enabled {
			if cacheData, ok := streamBuf.CacheableJSON(); ok {
				s.cache.StoreResponse(promptText, selection.Model, cacheData)
			}
		}
		return
	}

	// Non-streaming request with retry
	_, sendSpan := s.tracer.StartSpan(r.Context(), "provider.send")
	sendSpan.SetAttribute("provider.name", selection.Provider)
	sendSpan.SetAttribute("provider.model", selection.Model)
	sendSpan.SetAttribute("provider.stream", "false")

	var resp *provider.ChatResponse
	if cascadeResp != nil {
		// Reuse the cascade response — avoid double-send to cheap model
		resp = cascadeResp
	} else {
		err := provider.RetryWithBackoff(provider.RetryConfig{
			MaxRetries: 2,
			BaseDelay:  100 * time.Millisecond,
			MaxDelay:   2 * time.Second,
		}, func() error {
			var sendErr error
			resp, sendErr = p.Send(r.Context(), req)
			return sendErr
		})
		if err != nil {
			sendSpan.SetStatus("error")
			sendSpan.SetAttribute("error", err.Error())
			s.tracer.EndSpan(sendSpan)
			s.logger.Error("request error", "error", err, "provider", selection.Provider)
			s.health.RecordFailure(selection.Provider, err)
			if cb := s.cbPool.Get(selection.Provider); cb != nil {
				cb.RecordFailure()
			}
			if s.eventBus != nil {
				s.eventBus.Emit(events.RequestError, map[string]interface{}{
					"provider":    selection.Provider,
					"error":       err.Error(),
					"workflow_id": workflowID,
					"model":       selection.Model,
				})
			}
			writeNexusError(w, errProviderError(err.Error()), http.StatusBadGateway)
			return
		}
		s.health.RecordSuccess(selection.Provider)
		if cb := s.cbPool.Get(selection.Provider); cb != nil {
			cb.RecordSuccess()
		}
	}
	latencyMs := time.Since(start).Milliseconds()

	tokens := resp.Usage.TotalTokens
	cost := float64(tokens) / 1000.0 * s.router.GetModelCost(selection.Provider, selection.Model)

	// Track provider-side prefix caching (OpenAI cached_tokens, Anthropic cache_read)
	if resp.Usage.CachedTokens > 0 {
		s.metrics.RecordProviderCachedTokens(resp.Usage.CachedTokens)
	}

	sendSpan.SetAttribute("provider.tokens", fmt.Sprintf("%d", tokens))
	sendSpan.SetAttribute("provider.cost", fmt.Sprintf("%.6f", cost))
	sendSpan.SetAttribute("provider.cached_tokens", fmt.Sprintf("%d", resp.Usage.CachedTokens))
	s.tracer.EndSpan(sendSpan)

	// Record step
	ws.AddStep(workflow.StepRecord{
		Model:     selection.Model,
		Tier:      selection.Tier,
		Tokens:    tokens,
		Cost:      cost,
		LatencyMs: latencyMs,
	})
	reqSnap := ws.Snapshot()

	s.metrics.RecordRequest(selection.Provider, selection.Model, selection.Tier, tokens, cost, latencyMs, false)
	s.costTracker.RecordStep(workflowID, team, cost, tokens, false, 0)

	s.Dashboard.Push(dashboard.RequestEvent{
		Timestamp:       time.Now(),
		WorkflowID:      workflowID,
		Step:            reqSnap.CurrentStep,
		ComplexityScore: selection.Score.FinalScore,
		TierSelected:    selection.Tier,
		ModelUsed:       selection.Model,
		LatencyMs:       latencyMs,
		Cost:            cost,
		CacheHit:        false,
		Provider:        selection.Provider,
	})
	s.Dashboard.UpdateWorkflow(workflowID, reqSnap.Budget, reqSnap.BudgetLeft, ws.GetBudgetRatio(), reqSnap.CurrentStep, reqSnap.TotalCost)

	// Score response confidence
	var evalResult *eval.ConfidenceResult
	if s.cfg.Eval.Enabled && s.confidenceScorer != nil && len(resp.Choices) > 0 {
		_, evalSpan := s.tracer.StartSpan(r.Context(), "eval.score")
		er := s.confidenceScorer.CombinedScore(
			resp.Choices[0].Message.Content,
			resp.Usage.PromptTokens,
			resp.Usage.CompletionTokens,
			resp.Choices[0].FinishReason,
		)
		evalResult = &er
		if s.confidenceMap != nil {
			taskType := eval.ClassifyTaskType(promptText)
			s.confidenceMap.Record(taskType, selection.Tier, evalResult.Score)
		}
		s.metrics.RecordEvalConfidence(evalResult.Score)
		evalSpan.SetAttribute("eval.score", fmt.Sprintf("%.3f", evalResult.Score))
		evalSpan.SetAttribute("eval.recommendation", evalResult.Recommendation)
		s.tracer.EndSpan(evalSpan)

		w.Header().Set("X-Nexus-Confidence", fmt.Sprintf("%.3f", evalResult.Score))
	}

	// Shadow evaluation: compare primary vs comparison tier on sample % of requests
	if s.cfg.Eval.ShadowEnabled && s.cfg.Eval.ShadowSampleRate > 0 {
		if shouldShadow(workflowID, s.cfg.Eval.ShadowSampleRate) {
			select {
			case s.shadowSem <- struct{}{}:
				go func() {
					defer func() { <-s.shadowSem }()
					s.runShadowEval(r.Context(), req, promptText, selection, resp)
				}()
			default:
				// Shadow eval dropped — backpressure
			}
		}
	}

	// A/B experiment: record metrics for assigned variant
	var expAssignment *experiment.Assignment
	if s.experimentMgr != nil {
		expAssignment = s.experimentMgr.GetAssignment(workflowID)
	}
	if expAssignment != nil {
		var evalScore float64
		if evalResult != nil {
			evalScore = evalResult.Score
		}
		s.experimentMgr.RecordMetric(workflowID, experiment.MetricEvent{
			Cost:       cost,
			Tokens:     int64(tokens),
			LatencyMs:  latencyMs,
			CacheHit:   false,
			Escalation: false,
			Confidence: evalScore,
			Error:      false,
		})
	}

	// Cache the response
	_, storeSpan := s.tracer.StartSpan(r.Context(), "cache.store")
	respBody, _ := json.Marshal(resp)
	s.cache.StoreResponse(promptText, selection.Model, respBody)
	storeSpan.SetAttribute("cache.model", selection.Model)
	s.tracer.EndSpan(storeSpan)

	// X-Nexus-Explain: attach routing explanation to the response
	if r.Header.Get("X-Nexus-Explain") == "true" {
		var respMap map[string]any
		if err := json.Unmarshal(respBody, &respMap); err == nil {
			explanation := map[string]any{
				"complexity_score": selection.Score,
				"tier_decision":   selection.Tier,
				"reason":          selection.Reason,
				"provider":        selection.Provider,
				"model":           selection.Model,
				"cache_checked":   s.cfg.Cache.Enabled,
				"cache_result":    "miss",
			}
			if evalResult != nil {
				explanation["confidence"] = map[string]any{
					"score":          evalResult.Score,
					"recommendation": evalResult.Recommendation,
					"signals":        evalResult.Signals,
				}
			}
			explanation["compression"] = s.cfg.Compression.Enabled
			explanation["cascade_enabled"] = s.cfg.Cascade.Enabled
			respMap["nexus_explain"] = explanation
			respBody, _ = json.Marshal(respMap)
		}
	}

	// Emit RequestCompleted event (non-streaming)
	if s.eventBus != nil {
		s.eventBus.Emit(events.RequestCompleted, map[string]interface{}{
			"tier":        selection.Tier,
			"model":       selection.Model,
			"cost":        cost,
			"latency_ms":  latencyMs,
			"cache_hit":   false,
			"workflow_id": workflowID,
			"provider":    selection.Provider,
		})
	}

	// Plugin PostResponse hook (non-streaming)
	if s.pluginRegistry != nil {
		s.pluginRegistry.EmitResponse(r.Context(), &plugin.ResponseEvent{
			WorkflowID: workflowID,
			Step:       reqSnap.CurrentStep,
			Model:      selection.Model,
			Tier:       selection.Tier,
			LatencyMs:  float64(latencyMs),
			Cost:       cost,
			CacheHit:   false,
			TokensIn:   resp.Usage.PromptTokens,
			TokensOut:  resp.Usage.CompletionTokens,
		})
	}

	// Return response with Nexus headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Nexus-Model", selection.Model)
	w.Header().Set("X-Nexus-Tier", selection.Tier)
	w.Header().Set("X-Nexus-Provider", selection.Provider)
	w.Header().Set("X-Nexus-Cost", fmt.Sprintf("%.6f", cost))
	w.Header().Set("X-Nexus-Workflow-ID", workflowID)
	w.Header().Set("X-Nexus-Workflow-Step", fmt.Sprintf("%d", reqSnap.CurrentStep))
	w.Write(respBody)
}
