package telemetry

import (
	"sync"
)

type WorkflowCost struct {
	WorkflowID   string  `json:"workflow_id"`
	TotalCost    float64 `json:"total_cost"`
	TotalTokens  int     `json:"total_tokens"`
	StepCount    int     `json:"step_count"`
	CacheHits    int     `json:"cache_hits"`
	CacheSavings float64 `json:"cache_savings"`
}

type CostTracker struct {
	workflows map[string]*WorkflowCost
	teams     map[string]float64
	mu        sync.RWMutex
}

func NewCostTracker() *CostTracker {
	return &CostTracker{
		workflows: make(map[string]*WorkflowCost),
		teams:     make(map[string]float64),
	}
}

func (c *CostTracker) RecordStep(workflowID string, team string, cost float64, tokens int, cacheHit bool, estimatedFullCost float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	wc, ok := c.workflows[workflowID]
	if !ok {
		wc = &WorkflowCost{WorkflowID: workflowID}
		c.workflows[workflowID] = wc
	}

	wc.TotalCost += cost
	wc.TotalTokens += tokens
	wc.StepCount++
	if cacheHit {
		wc.CacheHits++
		wc.CacheSavings += estimatedFullCost
	}

	if team != "" {
		c.teams[team] += cost
	}
}

func (c *CostTracker) GetWorkflowCost(workflowID string) *WorkflowCost {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if wc, ok := c.workflows[workflowID]; ok {
		copy := *wc
		return &copy
	}
	return nil
}

func (c *CostTracker) GetTeamCosts() map[string]float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]float64)
	for k, v := range c.teams {
		result[k] = v
	}
	return result
}
