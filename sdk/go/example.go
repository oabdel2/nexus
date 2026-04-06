// Nexus Gateway — Drop-in Example (Go)
//
// Nexus is fully OpenAI-compatible. This example uses only the standard
// library (net/http + encoding/json) — no external dependencies required.
//
//     go run example.go
//
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ── Configuration ───────────────────────────────────────────────────────

const nexusBaseURL = "https://nexus-gateway.example.com" // ← your Nexus gateway

func apiKey() string {
	if k := os.Getenv("NEXUS_API_KEY"); k != "" {
		return k
	}
	return "your-api-key"
}

// ── OpenAI-compatible types ─────────────────────────────────────────────

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatResponse struct {
	ID      string   `json:"id"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index   int     `json:"index"`
	Message Message `json:"message"`
	Delta   *Delta  `json:"delta,omitempty"`
}

type Delta struct {
	Content string `json:"content,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ── Helper: send a request and return (body, http.Response, error) ──────

func nexusPost(path string, body interface{}, extraHeaders map[string]string) (*http.Response, []byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest("POST", nexusBaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, fmt.Errorf("new request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey())
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("do: %w", err)
	}

	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return resp, nil, fmt.Errorf("read body: %w", err)
	}

	return resp, data, nil
}

// ── 1. Non-streaming chat completion ────────────────────────────────────

func basicCompletion() {
	fmt.Println("=== Non-Streaming Response ===")

	_, data, err := nexusPost("/v1/chat/completions", ChatRequest{
		Model: "auto", // let Nexus pick the best model
		Messages: []Message{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Explain the CAP theorem in two sentences."},
		},
		Temperature: 0.7,
		MaxTokens:   256,
	}, nil)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	var cr ChatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		fmt.Println("Unmarshal error:", err)
		return
	}

	fmt.Printf("Content : %s\n", cr.Choices[0].Message.Content)
	fmt.Printf("Model   : %s\n", cr.Model)
	fmt.Printf("Tokens  : %d+%d\n", cr.Usage.PromptTokens, cr.Usage.CompletionTokens)
}

// ── 2. Streaming chat completion ────────────────────────────────────────

func streamingCompletion() {
	fmt.Println("\n=== Streaming Response ===")

	payload, _ := json.Marshal(ChatRequest{
		Model: "auto",
		Messages: []Message{
			{Role: "user", Content: "Write a haiku about distributed systems."},
		},
		Stream: true,
	})

	req, _ := http.NewRequest("POST", nexusBaseURL+"/v1/chat/completions", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey())

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}

		var chunk ChatResponse
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
			fmt.Print(chunk.Choices[0].Delta.Content)
		}
	}
	fmt.Println()
}

// ── 3. Using Nexus-specific headers + reading response headers ──────────
//
// Request headers:
//   X-Workflow-ID   — Group related requests into a workflow for cost tracking.
//   X-Agent-Role    — Hint the router about the task type ("architect",
//                     "researcher", "chat", "tester"). Affects model selection.
//   X-Team          — Team identifier for billing / cost attribution.
//   X-Budget        — Maximum USD budget for this workflow.
//   X-Request-ID    — Optional trace ID (auto-generated if omitted).
//
// Response headers:
//   X-Nexus-Model         — Model Nexus actually used (e.g. "gpt-4o").
//   X-Nexus-Tier          — Routing tier: "cheap", "mid", or "premium".
//   X-Nexus-Provider      — Backend provider: "openai", "anthropic", "cache/L1".
//   X-Nexus-Cost          — Estimated cost in USD (e.g. "0.003200").
//   X-Nexus-Cache         — Cache layer if served from cache ("L1", "L2a", "L2b").
//   X-Nexus-Confidence    — Response quality score (0–1).
//   X-Nexus-Workflow-ID   — Echoed workflow ID.
//   X-Nexus-Workflow-Step — Current step in workflow.

func nexusRoutedCompletion() {
	fmt.Println("\n=== Nexus-Routed Response with Headers ===")

	resp, data, err := nexusPost("/v1/chat/completions", ChatRequest{
		Model: "auto",
		Messages: []Message{
			{Role: "system", Content: "You are a senior software architect."},
			{Role: "user", Content: "Design a rate-limiting service for 10M RPM."},
		},
		MaxTokens: 1024,
	}, map[string]string{
		"X-Workflow-ID": "design-session-42",
		"X-Agent-Role":  "architect",
		"X-Team":        "platform-eng",
		"X-Budget":      "2.50",
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	var cr ChatResponse
	json.Unmarshal(data, &cr)

	// Read Nexus response headers
	h := func(key string) string {
		v := resp.Header.Get(key)
		if v == "" {
			return "n/a"
		}
		return v
	}

	fmt.Printf("Model      : %s\n", h("X-Nexus-Model"))
	fmt.Printf("Tier       : %s\n", h("X-Nexus-Tier"))
	fmt.Printf("Provider   : %s\n", h("X-Nexus-Provider"))
	fmt.Printf("Cost (USD) : %s\n", h("X-Nexus-Cost"))
	fmt.Printf("Cache      : %s\n", h("X-Nexus-Cache"))
	fmt.Printf("Confidence : %s\n", h("X-Nexus-Confidence"))
	fmt.Printf("Workflow   : %s\n", h("X-Nexus-Workflow-ID"))
	fmt.Printf("Step       : %s\n", h("X-Nexus-Workflow-Step"))

	if len(cr.Choices) > 0 {
		content := cr.Choices[0].Message.Content
		if len(content) > 120 {
			content = content[:120] + "..."
		}
		fmt.Printf("Answer     : %s\n", content)
	}
}

// ── 4. Multi-step workflow ──────────────────────────────────────────────

func workflowExample() {
	fmt.Println("\n=== Multi-Step Workflow ===")

	workflowID := "onboarding-flow-99"
	steps := []struct {
		role   string
		prompt string
	}{
		{"researcher", "List the top 3 Python web frameworks and their strengths."},
		{"architect", "Given those frameworks, which is best for a high-traffic API?"},
		{"tester", "Write a pytest test for a FastAPI health endpoint."},
	}

	for i, s := range steps {
		resp, data, err := nexusPost("/v1/chat/completions", ChatRequest{
			Model:     "auto",
			Messages:  []Message{{Role: "user", Content: s.prompt}},
			MaxTokens: 512,
		}, map[string]string{
			"X-Workflow-ID":  workflowID,
			"X-Agent-Role":   s.role,
			"X-Step-Number":  fmt.Sprintf("%d", i+1),
			"X-Budget":       "5.00",
		})
		if err != nil {
			fmt.Printf("  Step %d error: %v\n", i+1, err)
			continue
		}

		var cr ChatResponse
		json.Unmarshal(data, &cr)

		tier := resp.Header.Get("X-Nexus-Tier")
		cost := resp.Header.Get("X-Nexus-Cost")
		model := resp.Header.Get("X-Nexus-Model")
		if tier == "" {
			tier = "?"
		}
		if cost == "" {
			cost = "?"
		}
		if model == "" {
			model = "?"
		}

		preview := ""
		if len(cr.Choices) > 0 {
			preview = strings.ReplaceAll(cr.Choices[0].Message.Content, "\n", " ")
			if len(preview) > 80 {
				preview = preview[:80]
			}
		}

		fmt.Printf("  Step %d [%12s] → tier=%s, model=%s, cost=$%s\n", i+1, s.role, tier, model, cost)
		fmt.Printf("    %s...\n", preview)
	}
}

// ── 5. Health check (GET) ───────────────────────────────────────────────

func healthCheck() {
	fmt.Println("\n=== Health Check ===")

	resp, err := http.Get(nexusBaseURL + "/health")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	fmt.Println(string(data))
}

// ── Main ────────────────────────────────────────────────────────────────

func main() {
	basicCompletion()
	streamingCompletion()
	nexusRoutedCompletion()
	workflowExample()
	healthCheck()
}
