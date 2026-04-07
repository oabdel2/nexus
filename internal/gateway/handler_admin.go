package gateway

import (
	"encoding/json"
	"net/http"
)

func errSynonymRegistryDisabled() *NexusError {
	return &NexusError{
		Code:       "FEATURE_DISABLED",
		Message:    "Synonym registry is not enabled",
		Suggestion: "Enable L2 BM25 or semantic cache in config to activate synonym learning",
		DocsURL:    "https://nexus-gateway.dev/docs/config#cache",
	}
}

func (s *Server) handleSynonymStats(w http.ResponseWriter, r *http.Request) {
	registry := s.cache.Registry()
	if registry == nil {
		writeNexusError(w, errSynonymRegistryDisabled(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(registry.Stats())
}

func (s *Server) handleSynonymCandidates(w http.ResponseWriter, r *http.Request) {
	registry := s.cache.Registry()
	if registry == nil {
		writeNexusError(w, errSynonymRegistryDisabled(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(registry.GetCandidates())
}

func (s *Server) handleSynonymLearned(w http.ResponseWriter, r *http.Request) {
	registry := s.cache.Registry()
	if registry == nil {
		writeNexusError(w, errSynonymRegistryDisabled(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(registry.GetLearnedSynonyms())
}

func (s *Server) handleSynonymPromote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeNexusError(w, errMethodNotAllowed(), http.StatusMethodNotAllowed)
		return
	}
	registry := s.cache.Registry()
	if registry == nil {
		writeNexusError(w, errSynonymRegistryDisabled(), http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Term string `json:"term"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeNexusError(w, errInvalidRequest("Invalid JSON in request body"), http.StatusBadRequest)
		return
	}
	if registry.ManualPromote(req.Term) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "promoted", "term": req.Term})
	} else {
		writeNexusError(w, &NexusError{
			Code:       "NOT_FOUND",
			Message:    "Synonym candidate not found: " + req.Term,
			Suggestion: "Check available candidates at GET /api/synonyms/candidates",
			DocsURL:    "https://nexus-gateway.dev/docs/api#synonyms",
		}, http.StatusNotFound)
	}
}

func (s *Server) handleSynonymAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeNexusError(w, errMethodNotAllowed(), http.StatusMethodNotAllowed)
		return
	}
	registry := s.cache.Registry()
	if registry == nil {
		writeNexusError(w, errSynonymRegistryDisabled(), http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Term      string `json:"term"`
		Expansion string `json:"expansion"`
		Type      string `json:"type"` // "synonym" or "key_noun"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeNexusError(w, errInvalidRequest("Invalid JSON in request body"), http.StatusBadRequest)
		return
	}

	// Validate term and expansion to prevent cache poisoning
	if req.Term == "" || len(req.Term) > 100 {
		http.Error(w, `{"error":"term must be 1-100 characters"}`, http.StatusBadRequest)
		return
	}
	if req.Type != "key_noun" && (req.Expansion == "" || len(req.Expansion) > 200) {
		http.Error(w, `{"error":"expansion must be 1-200 characters"}`, http.StatusBadRequest)
		return
	}
	for _, c := range req.Term {
		if c < 0x20 || c == 0x7f {
			http.Error(w, `{"error":"term contains invalid characters"}`, http.StatusBadRequest)
			return
		}
	}
	for _, c := range req.Expansion {
		if c < 0x20 || c == 0x7f {
			http.Error(w, `{"error":"expansion contains invalid characters"}`, http.StatusBadRequest)
			return
		}
	}

	if req.Type == "key_noun" {
		registry.ManualAddKeyNoun(req.Term)
	} else {
		registry.ManualAdd(req.Term, req.Expansion)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "added", "term": req.Term})
}

func (s *Server) handleCircuitBreakers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.cbPool.AllStats())
}

func (s *Server) handleEvalStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.confidenceMap == nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "eval not enabled"})
		return
	}
	// Return all known task-type/tier combos
	type entry struct {
		TaskType          string  `json:"task_type"`
		Tier              string  `json:"tier"`
		AverageConfidence float64 `json:"average_confidence"`
		SampleCount       int     `json:"sample_count"`
	}
	var entries []entry
	for _, tt := range []string{"coding", "analysis", "creative", "operational", "informational", "general"} {
		for _, tier := range []string{"economy", "cheap", "mid", "premium"} {
			r := s.confidenceMap.Lookup(tt, tier)
			if r.Found {
				entries = append(entries, entry{
					TaskType:          tt,
					Tier:              tier,
					AverageConfidence: r.AverageConfidence,
					SampleCount:       r.SampleCount,
				})
			}
		}
	}
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"entries": entries,
	})
}

func (s *Server) handleAdaptiveStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.adaptiveRouter == nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "adaptive routing not enabled"})
		return
	}
	json.NewEncoder(w).Encode(s.adaptiveRouter.Stats())
}

func (s *Server) handleCompressionStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"enabled": s.cfg.Compression.Enabled,
		"config": map[string]any{
			"whitespace":        s.cfg.Compression.Whitespace,
			"code_strip":        s.cfg.Compression.CodeStrip,
			"history_truncate":  s.cfg.Compression.HistoryTruncate,
			"max_history_turns": s.cfg.Compression.MaxHistoryTurns,
			"preserve_last_n":   s.cfg.Compression.PreserveLastN,
		},
	})
}

// handleInspect accepts a prompt via POST and returns the routing decision
// that would be made WITHOUT actually sending the request.
func (s *Server) handleInspect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeNexusError(w, errMethodNotAllowed(), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Prompt string `json:"prompt"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeNexusError(w, errInvalidRequest("Invalid JSON in request body"), http.StatusBadRequest)
		return
	}
	if req.Prompt == "" {
		writeNexusError(w, errInvalidRequest("prompt field is required"), http.StatusBadRequest)
		return
	}

	// Route without sending
	selection := s.router.Route(req.Prompt, req.Role, 0.0, 1.0, len(req.Prompt))

	wouldCascade := false
	if s.cfg.Cascade.IsEnabled() && s.cascade != nil {
		wouldCascade = s.cascade.ShouldCascade(selection.Score, selection.Tier)
	}

	estimatedCost := 0.005 // default estimate
	modelCost := s.router.GetModelCost(selection.Provider, selection.Model)
	if modelCost > 0 {
		// Rough estimate: assume ~500 tokens for a typical request
		estimatedCost = 500.0 / 1000.0 * modelCost
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"complexity_score":    selection.Score,
		"tier":               selection.Tier,
		"reason":             selection.Reason,
		"would_cascade":      wouldCascade,
		"estimated_model":    selection.Model,
		"estimated_provider": selection.Provider,
		"estimated_cost":     estimatedCost,
		"cache_enabled":      s.cfg.Cache.Enabled,
		"compression_enabled": s.cfg.Compression.Enabled,
		"cascade_enabled":    s.cfg.Cascade.IsEnabled(),
	})
}

func (s *Server) handleEventsRecent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.eventBus == nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "events not enabled"})
		return
	}
	json.NewEncoder(w).Encode(s.eventBus.Recent())
}

func (s *Server) handleEventsStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.eventBus == nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "events not enabled"})
		return
	}
	json.NewEncoder(w).Encode(s.eventBus.Stats())
}

func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.pluginRegistry == nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "plugins not enabled"})
		return
	}
	json.NewEncoder(w).Encode(s.pluginRegistry.ListPlugins())
}
