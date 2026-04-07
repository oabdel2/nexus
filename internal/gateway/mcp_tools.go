package gateway

import (
	"encoding/json"
	"fmt"

	"github.com/nexus-gateway/nexus/internal/mcp"
)

// registerMCPTools registers all Nexus tools with the MCP server.
func (s *Server) registerMCPTools() {
	s.mcpServer.RegisterTool(mcp.Tool{
		Name:        "nexus_chat",
		Description: "Send a chat completion through the Nexus gateway. Routes to the optimal model based on prompt complexity.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{"type": "string", "description": "The prompt to send for completion"},
				"model":  map[string]interface{}{"type": "string", "description": "Target model override (optional, Nexus auto-routes if omitted)"},
				"role":   map[string]interface{}{"type": "string", "description": "Agent role for routing context (e.g. planner, coder, reviewer)"},
			},
			"required": []string{"prompt"},
		},
		Handler: s.mcpChat,
	})

	s.mcpServer.RegisterTool(mcp.Tool{
		Name:        "nexus_inspect",
		Description: "Analyze how Nexus would route a prompt without sending it. Returns complexity score, tier, estimated model and provider.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{"type": "string", "description": "The prompt to analyze"},
				"role":   map[string]interface{}{"type": "string", "description": "Agent role for routing context"},
			},
			"required": []string{"prompt"},
		},
		Handler: s.mcpInspect,
	})

	s.mcpServer.RegisterTool(mcp.Tool{
		Name:        "nexus_stats",
		Description: "Get gateway statistics including total requests, cache hit/miss counts, and cost data.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: s.mcpStats,
	})

	s.mcpServer.RegisterTool(mcp.Tool{
		Name:        "nexus_health",
		Description: "Check provider health and circuit breaker status for all configured providers.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: s.mcpHealth,
	})

	s.mcpServer.RegisterTool(mcp.Tool{
		Name:        "nexus_experiments",
		Description: "List active A/B experiments and their results, including variant stats and traffic splits.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: s.mcpExperiments,
	})

	s.mcpServer.RegisterTool(mcp.Tool{
		Name:        "nexus_confidence",
		Description: "Query the confidence map for a task type and tier. Returns average confidence and sample count.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task_type": map[string]interface{}{"type": "string", "description": "Task type (coding, analysis, creative, operational, informational)"},
				"tier":      map[string]interface{}{"type": "string", "description": "Model tier (simple, moderate, complex)"},
			},
			"required": []string{"task_type", "tier"},
		},
		Handler: s.mcpConfidence,
	})

	s.mcpServer.RegisterTool(mcp.Tool{
		Name:        "nexus_providers",
		Description: "List all configured providers, their models, and current health status.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: s.mcpProviders,
	})
}

func (s *Server) mcpChat(params map[string]interface{}) (interface{}, error) {
	prompt, _ := params["prompt"].(string)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	role, _ := params["role"].(string)
	selection := s.router.Route(prompt, role, 0.0, 1.0, len(prompt))

	return map[string]interface{}{
		"routed_to":   selection.Provider,
		"model":       selection.Model,
		"tier":        selection.Tier,
		"score":       selection.Score,
		"reason":      selection.Reason,
		"cache_enabled": s.cfg.Cache.Enabled,
		"note":        "Use /v1/chat/completions for actual inference. This tool shows routing decisions.",
	}, nil
}

func (s *Server) mcpInspect(params map[string]interface{}) (interface{}, error) {
	prompt, _ := params["prompt"].(string)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	role, _ := params["role"].(string)
	selection := s.router.Route(prompt, role, 0.0, 1.0, len(prompt))

	wouldCascade := false
	if s.cfg.Cascade.IsEnabled() && s.cascade != nil {
		wouldCascade = s.cascade.ShouldCascade(selection.Score, selection.Tier)
	}

	estimatedCost := 0.005
	modelCost := s.router.GetModelCost(selection.Provider, selection.Model)
	if modelCost > 0 {
		estimatedCost = 500.0 / 1000.0 * modelCost
	}

	return map[string]interface{}{
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
	}, nil
}

func (s *Server) mcpStats(params map[string]interface{}) (interface{}, error) {
	hits, misses, cacheSize := s.cache.Stats()

	return map[string]interface{}{
		"requests_total": s.metrics.RequestsTotal.Load(),
		"cache": map[string]interface{}{
			"hits":   hits,
			"misses": misses,
			"size":   cacheSize,
		},
		"team_costs": s.costTracker.GetTeamCosts(),
	}, nil
}

func (s *Server) mcpHealth(params map[string]interface{}) (interface{}, error) {
	status := s.health.GetStatus()
	providers := make(map[string]interface{})
	for name, hs := range status {
		providers[name] = map[string]interface{}{
			"healthy":       hs.Healthy,
			"last_check":    hs.LastCheck,
			"last_error":    hs.LastError,
			"failure_count": hs.FailureCount,
			"circuit_open":  hs.CircuitOpen,
		}
	}
	return map[string]interface{}{
		"status":    "ok",
		"providers": providers,
	}, nil
}

func (s *Server) mcpExperiments(params map[string]interface{}) (interface{}, error) {
	if s.experimentMgr == nil {
		return map[string]interface{}{
			"status":      "experiments not enabled",
			"experiments": []interface{}{},
		}, nil
	}

	exps := s.experimentMgr.AllExperiments()
	out := make([]map[string]interface{}, 0, len(exps))
	for _, e := range exps {
		expData := map[string]interface{}{
			"id":          e.ID,
			"name":        e.Name,
			"description": e.Description,
			"enabled":     e.Enabled,
			"active":      e.IsActive(),
			"variants":    len(e.Variants),
		}
		if results := s.experimentMgr.GetResults(e.ID); results != nil {
			variantData := make(map[string]interface{})
			for vid, vs := range results.VariantStats {
				data, _ := json.Marshal(vs)
				var m map[string]interface{}
				json.Unmarshal(data, &m)
				variantData[vid] = m
			}
			expData["results"] = variantData
		}
		out = append(out, expData)
	}

	return map[string]interface{}{
		"experiments": out,
	}, nil
}

func (s *Server) mcpConfidence(params map[string]interface{}) (interface{}, error) {
	taskType, _ := params["task_type"].(string)
	tier, _ := params["tier"].(string)
	if taskType == "" || tier == "" {
		return nil, fmt.Errorf("task_type and tier are required")
	}

	if s.confidenceMap == nil {
		return map[string]interface{}{
			"task_type": taskType,
			"tier":      tier,
			"found":     false,
			"note":      "confidence map not enabled",
		}, nil
	}

	result := s.confidenceMap.Lookup(taskType, tier)
	return map[string]interface{}{
		"task_type":          taskType,
		"tier":              tier,
		"average_confidence": result.AverageConfidence,
		"sample_count":      result.SampleCount,
		"found":             result.Found,
	}, nil
}

func (s *Server) mcpProviders(params map[string]interface{}) (interface{}, error) {
	status := s.health.GetStatus()
	providerList := make([]map[string]interface{}, 0, len(s.providers))
	for name := range s.providers {
		info := map[string]interface{}{
			"name": name,
		}
		if hs, ok := status[name]; ok {
			info["healthy"] = hs.Healthy
			info["circuit_open"] = hs.CircuitOpen
			info["failure_count"] = hs.FailureCount
		}
		providerList = append(providerList, info)
	}

	return map[string]interface{}{
		"providers": providerList,
	}, nil
}
