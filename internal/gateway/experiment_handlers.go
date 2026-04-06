package gateway

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nexus-gateway/nexus/internal/experiment"
)

// handleExperiments lists all experiments with their status.
func (s *Server) handleExperiments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	if s.experimentMgr == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "experiments not enabled"})
		return
	}

	exps := s.experimentMgr.AllExperiments()
	type expInfo struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Enabled     bool   `json:"enabled"`
		Active      bool   `json:"active"`
		Variants    int    `json:"variant_count"`
	}
	out := make([]expInfo, 0, len(exps))
	for _, e := range exps {
		out = append(out, expInfo{
			ID:          e.ID,
			Name:        e.Name,
			Description: e.Description,
			Enabled:     e.Enabled,
			Active:      e.IsActive(),
			Variants:    len(e.Variants),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":      "ok",
		"experiments": out,
	})
}

// handleExperimentResults returns results for a specific experiment.
func (s *Server) handleExperimentResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	if s.experimentMgr == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "experiments not enabled"})
		return
	}

	expID := strings.TrimPrefix(r.URL.Path, "/api/experiments/")
	expID = strings.TrimSuffix(expID, "/results")
	if expID == "" {
		http.Error(w, "experiment ID required", http.StatusBadRequest)
		return
	}

	results := s.experimentMgr.GetResults(expID)
	if results == nil {
		http.Error(w, "experiment not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"results": results,
	})
}

// handleExperimentCreate creates a new experiment from a JSON body.
func (s *Server) handleExperimentCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if s.experimentMgr == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "experiments not enabled"})
		return
	}

	var exp experiment.Experiment
	if err := json.NewDecoder(r.Body).Decode(&exp); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if exp.ID == "" {
		http.Error(w, "experiment id required", http.StatusBadRequest)
		return
	}

	s.experimentMgr.RegisterExperiment(exp)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "created",
		"id":     exp.ID,
	})
}

// handleExperimentToggle enables or disables an experiment.
func (s *Server) handleExperimentToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if s.experimentMgr == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "experiments not enabled"})
		return
	}

	expID := strings.TrimPrefix(r.URL.Path, "/api/experiments/")
	expID = strings.TrimSuffix(expID, "/toggle")
	if expID == "" {
		http.Error(w, "experiment ID required", http.StatusBadRequest)
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if !s.experimentMgr.ToggleExperiment(expID, req.Enabled) {
		http.Error(w, "experiment not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "toggled",
		"id":     expID,
	})
}

// handleShadowStats returns shadow evaluation statistics.
func (s *Server) handleShadowStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	shadow := s.cache.Shadow()
	stats := shadow.Stats()
	disagreements := shadow.RecentDisagreements(10)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":               "ok",
		"shadow_enabled":       s.cfg.Eval.ShadowEnabled,
		"shadow_sample_rate":   s.cfg.Eval.ShadowSampleRate,
		"stats":                stats,
		"recent_disagreements": disagreements,
	})
}
