package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// StartupValidator performs pre-flight checks before the gateway accepts traffic.
type StartupValidator struct {
	logger *slog.Logger
	client *http.Client
}

func NewStartupValidator(logger *slog.Logger) *StartupValidator {
	return &StartupValidator{
		logger: logger,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// ValidateConfig checks that the configuration is internally consistent.
func (v *StartupValidator) ValidateConfig(s *Server) []string {
	var warnings []string

	// Check: at least one provider enabled
	enabledCount := 0
	for _, p := range s.cfg.Providers {
		if p.Enabled {
			enabledCount++
		}
	}
	if enabledCount == 0 {
		warnings = append(warnings, "⚠ no providers enabled — all requests will fail")
	}

	// Check: each enabled provider has at least one model
	for _, p := range s.cfg.Providers {
		if !p.Enabled {
			continue
		}
		if len(p.Models) == 0 {
			warnings = append(warnings, fmt.Sprintf("⚠ provider '%s' has no models configured", p.Name))
		}
	}

	// Check: cache enabled but semantic backend requires Ollama
	if s.cfg.Cache.L2Semantic.Enabled {
		if s.cfg.Cache.L2Semantic.Endpoint == "" {
			warnings = append(warnings, "⚠ semantic cache enabled but no embedding endpoint configured")
		}
		if s.cfg.Cache.L2Semantic.Model == "" {
			warnings = append(warnings, "⚠ semantic cache enabled but no embedding model configured")
		}
	}

	// Check: router threshold sanity
	if s.cfg.Router.Threshold <= 0 || s.cfg.Router.Threshold > 1 {
		warnings = append(warnings, fmt.Sprintf("⚠ router threshold %.2f is outside [0,1] — routing may be unpredictable", s.cfg.Router.Threshold))
	}

	return warnings
}

// CheckProviderReachability tests that each enabled provider is reachable.
func (v *StartupValidator) CheckProviderReachability(s *Server) {
	for name, p := range s.providers {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := p.HealthCheck(ctx)
		cancel()

		if err != nil {
			v.logger.Warn("provider unreachable at startup",
				"provider", name,
				"error", err,
			)
		} else {
			v.logger.Info("provider reachable",
				"provider", name,
			)
		}
	}
}

// CheckOllamaModels verifies that configured Ollama models are actually pulled.
func (v *StartupValidator) CheckOllamaModels(s *Server) {
	for _, pc := range s.cfg.Providers {
		if !pc.Enabled || pc.Type != "ollama" {
			continue
		}

		// Extract Ollama base (strip /v1 suffix)
		ollamaBase := strings.TrimSuffix(pc.BaseURL, "/v1")
		resp, err := v.client.Get(ollamaBase + "/api/tags")
		if err != nil {
			v.logger.Warn("cannot reach Ollama to verify models",
				"provider", pc.Name,
				"url", ollamaBase,
				"error", err,
			)
			continue
		}
		defer resp.Body.Close()

		var tags struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
			continue
		}

		available := make(map[string]bool)
		for _, m := range tags.Models {
			// Ollama returns names like "qwen2.5:1.5b" and also with :latest
			available[m.Name] = true
			parts := strings.SplitN(m.Name, ":", 2)
			if len(parts) > 0 {
				available[parts[0]] = true
			}
		}

		for _, m := range pc.Models {
			if !available[m.Name] {
				v.logger.Warn("Ollama model not found — pull with: ollama pull "+m.Name,
					"provider", pc.Name,
					"model", m.Name,
				)
			} else {
				v.logger.Info("Ollama model verified",
					"provider", pc.Name,
					"model", m.Name,
				)
			}
		}
	}

	// Also check embedding model for semantic cache
	if s.cfg.Cache.L2Semantic.Enabled && s.cfg.Cache.L2Semantic.Backend == "ollama" {
		endpoint := s.cfg.Cache.L2Semantic.Endpoint
		model := s.cfg.Cache.L2Semantic.Model
		resp, err := v.client.Get(endpoint + "/api/tags")
		if err != nil {
			v.logger.Warn("cannot reach Ollama for embedding model check",
				"endpoint", endpoint,
			)
			return
		}
		defer resp.Body.Close()

		var tags struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
			return
		}

		found := false
		for _, m := range tags.Models {
			if strings.HasPrefix(m.Name, model) {
				found = true
				break
			}
		}
		if !found {
			v.logger.Warn("embedding model not found — pull with: ollama pull "+model,
				"model", model,
				"endpoint", endpoint,
			)
		} else {
			v.logger.Info("embedding model verified",
				"model", model,
			)
		}
	}
}

// WarmupModels sends a tiny request to each Ollama provider to preload models into GPU memory.
func (v *StartupValidator) WarmupModels(s *Server) {
	for _, pc := range s.cfg.Providers {
		if !pc.Enabled || pc.Type != "ollama" {
			continue
		}
		if len(pc.Models) == 0 {
			continue
		}

		model := pc.Models[0].Name
		v.logger.Info("warming up model (preloading to GPU)...",
			"provider", pc.Name,
			"model", model,
		)

		start := time.Now()

		// Send minimal completion request to force model load
		warmupReq := map[string]interface{}{
			"model": model,
			"messages": []map[string]string{
				{"role": "user", "content": "hi"},
			},
			"max_tokens": 1,
			"stream":     false,
		}
		body, _ := json.Marshal(warmupReq)

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		httpReq, err := http.NewRequestWithContext(ctx, "POST", pc.BaseURL+"/chat/completions", strings.NewReader(string(body)))
		if err != nil {
			v.logger.Warn("warmup request creation failed", "error", err)
			continue
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if pc.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+pc.APIKey)
		}

		resp, err := v.client.Do(httpReq)
		elapsed := time.Since(start)
		if err != nil {
			v.logger.Warn("model warmup failed",
				"provider", pc.Name,
				"model", model,
				"duration", elapsed.Round(time.Millisecond),
				"error", err,
			)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			v.logger.Info("model warmed up successfully ✓",
				"provider", pc.Name,
				"model", model,
				"duration", elapsed.Round(time.Millisecond),
			)
		} else {
			v.logger.Warn("model warmup returned non-200",
				"provider", pc.Name,
				"model", model,
				"status", resp.StatusCode,
				"duration", elapsed.Round(time.Millisecond),
			)
		}
	}

	// Also warm up embedding model
	if s.cfg.Cache.L2Semantic.Enabled && s.cfg.Cache.L2Semantic.Backend == "ollama" {
		endpoint := s.cfg.Cache.L2Semantic.Endpoint
		model := s.cfg.Cache.L2Semantic.Model
		v.logger.Info("warming up embedding model...",
			"model", model,
		)

		start := time.Now()
		embReq := map[string]interface{}{
			"model":  model,
			"prompt": "warmup",
		}
		body, _ := json.Marshal(embReq)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		httpReq, _ := http.NewRequestWithContext(ctx, "POST", endpoint+"/api/embeddings", strings.NewReader(string(body)))
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := v.client.Do(httpReq)
		elapsed := time.Since(start)
		if err != nil {
			v.logger.Warn("embedding warmup failed", "error", err)
		} else {
			resp.Body.Close()
			v.logger.Info("embedding model warmed up ✓",
				"model", model,
				"duration", elapsed.Round(time.Millisecond),
			)
		}
	}
}
