package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Reranker provides cross-encoder verification for semantic cache candidates.
// It jointly processes query+document pairs for deeper relevance scoring.
type Reranker struct {
	endpoint  string
	model     string
	threshold float64
	client    *http.Client
	enabled   bool
}

// RerankerConfig holds configuration for the cross-encoder reranker.
type RerankerConfig struct {
	Enabled   bool    `yaml:"enabled"`
	Model     string  `yaml:"model"`
	Endpoint  string  `yaml:"endpoint"`
	Threshold float64 `yaml:"threshold"`
}

// NewReranker creates a new cross-encoder reranker.
func NewReranker(cfg RerankerConfig) *Reranker {
	if !cfg.Enabled {
		return &Reranker{enabled: false}
	}
	if cfg.Threshold == 0 {
		cfg.Threshold = 0.5
	}
	if cfg.Model == "" {
		cfg.Model = "bge-reranker-v2-m3"
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "http://localhost:11434"
	}
	return &Reranker{
		endpoint:  cfg.Endpoint,
		model:     cfg.Model,
		threshold: cfg.Threshold,
		client:    &http.Client{Timeout: 5 * time.Second},
		enabled:   true,
	}
}

// Verify checks if a query-document pair is a genuine match using cross-encoder scoring.
// Returns true if the reranker confirms the match, false if it rejects.
// If the reranker is disabled or unavailable, returns true (permissive fallback).
func (r *Reranker) Verify(query, document string) bool {
	if !r.enabled {
		return true
	}

	score, err := r.rerank(query, document)
	if err != nil {
		return true
	}

	return score >= r.threshold
}

// Score returns the relevance score for a query-document pair.
// Returns -1 on error.
func (r *Reranker) Score(query, document string) float64 {
	if !r.enabled {
		return 1.0
	}
	score, err := r.rerank(query, document)
	if err != nil {
		return -1.0
	}
	return score
}

// rerank calls the Ollama rerank API or falls back to heuristic comparison.
func (r *Reranker) rerank(query, document string) (float64, error) {
	score, err := r.ollamaRerank(query, document)
	if err == nil {
		return score, nil
	}

	return r.heuristicRerank(query, document), nil
}

// ollamaRerank uses Ollama's /api/rerank endpoint (available since Ollama 0.6+).
func (r *Reranker) ollamaRerank(query, document string) (float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	payload := map[string]interface{}{
		"model":     r.model,
		"query":     query,
		"documents": []string{document},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	url := r.endpoint + "/api/rerank"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("rerank returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Results []struct {
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	if len(result.Results) == 0 {
		return 0, fmt.Errorf("reranker returned no results")
	}

	return result.Results[0].RelevanceScore, nil
}

// heuristicRerank provides a lightweight reranking fallback using token analysis.
func (r *Reranker) heuristicRerank(query, document string) float64 {
	qTokens := tokenizeWords(query)
	dTokens := tokenizeWords(document)

	if len(qTokens) == 0 || len(dTokens) == 0 {
		return 0.0
	}

	qSet := make(map[string]bool, len(qTokens))
	for _, t := range qTokens {
		qSet[t] = true
	}
	dSet := make(map[string]bool, len(dTokens))
	for _, t := range dTokens {
		dSet[t] = true
	}

	intersection := 0
	for t := range qSet {
		if dSet[t] {
			intersection++
		}
	}

	union := len(qSet)
	for t := range dSet {
		if !qSet[t] {
			union++
		}
	}

	if union == 0 {
		return 0.0
	}

	jaccard := float64(intersection) / float64(union)

	contentOverlap := 0
	contentTotal := 0
	for t := range qSet {
		if len(t) > 3 {
			contentTotal++
			if dSet[t] {
				contentOverlap++
			}
		}
	}

	contentScore := 0.0
	if contentTotal > 0 {
		contentScore = float64(contentOverlap) / float64(contentTotal)
	}

	return 0.4*jaccard + 0.6*contentScore
}
