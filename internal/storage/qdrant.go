package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// QdrantStore implements VectorStore using Qdrant vector database.
type QdrantStore struct {
	host       string
	port       int
	collection string
	apiKey     string
	dimension  int
	client     *http.Client
}

// NewQdrantStore creates a new Qdrant vector store.
func NewQdrantStore(host string, port int, collection, apiKey string, dimension int) (*QdrantStore, error) {
	if host == "" {
		host = "localhost"
	}
	if port == 0 {
		port = 6333
	}
	if collection == "" {
		collection = "nexus_cache"
	}
	if dimension == 0 {
		dimension = 1024
	}

	q := &QdrantStore{
		host:       host,
		port:       port,
		collection: collection,
		apiKey:     apiKey,
		dimension:  dimension,
		client:     &http.Client{Timeout: 10 * time.Second},
	}

	// Ensure collection exists
	if err := q.ensureCollection(); err != nil {
		return nil, fmt.Errorf("qdrant collection setup failed: %w", err)
	}

	return q, nil
}

func (q *QdrantStore) baseURL() string {
	return fmt.Sprintf("http://%s:%d", q.host, q.port)
}

func (q *QdrantStore) doRequest(method, path string, body interface{}) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, q.baseURL()+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if q.apiKey != "" {
		req.Header.Set("api-key", q.apiKey)
	}

	return q.client.Do(req)
}

func (q *QdrantStore) ensureCollection() error {
	// Check if collection exists
	resp, err := q.doRequest("GET", "/collections/"+q.collection, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	// Create collection
	payload := map[string]interface{}{
		"vectors": map[string]interface{}{
			"size":     q.dimension,
			"distance": "Cosine",
		},
	}

	resp, err = q.doRequest("PUT", "/collections/"+q.collection, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create collection failed: %d %s", resp.StatusCode, string(body))
	}

	return nil
}

func (q *QdrantStore) Store(entry VectorEntry) error {
	payload := map[string]interface{}{
		"points": []map[string]interface{}{
			{
				"id":     hashToUint64(entry.ID),
				"vector": entry.Embedding,
				"payload": map[string]interface{}{
					"prompt":     entry.Prompt,
					"model":      entry.Model,
					"response":   string(entry.Response),
					"created_at": entry.CreatedAt.Unix(),
					"id":         entry.ID,
				},
			},
		},
	}

	resp, err := q.doRequest("PUT", "/collections/"+q.collection+"/points", payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("qdrant upsert failed: %d %s", resp.StatusCode, string(body))
	}

	return nil
}

func (q *QdrantStore) Search(embedding []float64, topK int, threshold float64, filter map[string]string) ([]SearchResult, error) {
	payload := map[string]interface{}{
		"vector":          embedding,
		"limit":           topK,
		"score_threshold": threshold,
		"with_payload":    true,
	}

	if len(filter) > 0 {
		must := make([]map[string]interface{}, 0, len(filter))
		for k, v := range filter {
			must = append(must, map[string]interface{}{
				"key":   k,
				"match": map[string]interface{}{"value": v},
			})
		}
		payload["filter"] = map[string]interface{}{"must": must}
	}

	resp, err := q.doRequest("POST", "/collections/"+q.collection+"/points/search", payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("qdrant search failed: %d %s", resp.StatusCode, string(body))
	}

	var result struct {
		Result []struct {
			ID      interface{}            `json:"id"`
			Score   float64                `json:"score"`
			Payload map[string]interface{} `json:"payload"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var results []SearchResult
	for _, r := range result.Result {
		entry := VectorEntry{
			Prompt: fmt.Sprint(r.Payload["prompt"]),
			Model:  fmt.Sprint(r.Payload["model"]),
		}
		if respStr, ok := r.Payload["response"].(string); ok {
			entry.Response = []byte(respStr)
		}
		if id, ok := r.Payload["id"].(string); ok {
			entry.ID = id
		}

		results = append(results, SearchResult{
			Entry: entry,
			Score: r.Score,
		})
	}

	return results, nil
}

func (q *QdrantStore) Delete(id string) error {
	payload := map[string]interface{}{
		"points": []uint64{hashToUint64(id)},
	}

	resp, err := q.doRequest("POST", "/collections/"+q.collection+"/points/delete", payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (q *QdrantStore) Count() (int64, error) {
	resp, err := q.doRequest("GET", "/collections/"+q.collection, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		Result struct {
			PointsCount int64 `json:"points_count"`
		} `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Result.PointsCount, nil
}

func (q *QdrantStore) Close() error { return nil }

func (q *QdrantStore) Healthy() bool {
	resp, err := q.doRequest("GET", "/", nil)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// hashToUint64 converts a string ID to a uint64 for Qdrant.
func hashToUint64(id string) uint64 {
	var h uint64 = 14695981039346656037 // FNV offset basis
	for _, c := range id {
		h ^= uint64(c)
		h *= 1099511628211 // FNV prime
	}
	return h
}
