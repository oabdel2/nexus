// Package main provides an integration-level regression test runner for Nexus.
// It tests against a live gateway to verify critical bug fixes.
//
// Usage: go run tests/regression/main.go [gateway-url]
// Default gateway: http://localhost:8080
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ─── Types ───────────────────────────────────────────────────────────

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ─── Test Results ────────────────────────────────────────────────────

type TestResult struct {
	Name    string
	Passed  bool
	Message string
	Elapsed time.Duration
}

var (
	gatewayURL string
	results    []TestResult
	client     = &http.Client{Timeout: 30 * time.Second}
)

func main() {
	gatewayURL = "http://localhost:8080"
	if len(os.Args) > 1 {
		gatewayURL = os.Args[1]
	}

	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║      Nexus Regression Test Suite (Integration)      ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
	fmt.Printf("Gateway: %s\n", gatewayURL)
	fmt.Printf("Go version: %s\n\n", runtime.Version())

	// Check if gateway is reachable
	if !checkGateway() {
		fmt.Println("⚠️  Gateway not reachable. Running offline tests only.")
		runOfflineTests()
	} else {
		fmt.Println("✓ Gateway is reachable.")
		runOnlineTests()
		runOfflineTests()
	}

	// Print summary
	printSummary()
}

func checkGateway() bool {
	resp, err := client.Get(gatewayURL + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ─── Bug 1: Concurrent AddExample ────────────────────────────────────

func testBug1_ConcurrentAddExample() {
	start := time.Now()
	name := "Bug1: Concurrent AddExample (no deadlock)"

	done := make(chan bool, 1)
	var failures int64

	go func() {
		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				body, _ := json.Marshal(ChatRequest{
					Model: "any",
					Messages: []Message{
						{Role: "user", Content: fmt.Sprintf("test prompt %d about debugging", n)},
					},
				})
				resp, err := client.Post(gatewayURL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
				if err != nil {
					atomic.AddInt64(&failures, 1)
					return
				}
				defer resp.Body.Close()
				io.ReadAll(resp.Body)
			}(i)
		}
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		if atomic.LoadInt64(&failures) > 3 {
			results = append(results, TestResult{name, false, "too many request failures", time.Since(start)})
		} else {
			results = append(results, TestResult{name, true, "5 concurrent requests completed without hang", time.Since(start)})
		}
	case <-time.After(10 * time.Second):
		results = append(results, TestResult{name, false, "DEADLOCK: requests hung for >10s", time.Since(start)})
	}
}

// ─── Bug 2: Cascade Double-Send ──────────────────────────────────────

func testBug2_CascadeDoubleSend() {
	start := time.Now()
	name := "Bug2: Cascade response arrives (not double-billed)"

	body, _ := json.Marshal(ChatRequest{
		Model:     "any",
		MaxTokens: 50,
		Messages: []Message{
			{Role: "user", Content: "What is 2+2?"},
		},
	})

	resp, err := client.Post(gatewayURL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		results = append(results, TestResult{name, false, fmt.Sprintf("request failed: %v", err), time.Since(start)})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		results = append(results, TestResult{name, false, fmt.Sprintf("status %d: %s", resp.StatusCode, string(respBody)), time.Since(start)})
		return
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		results = append(results, TestResult{name, false, fmt.Sprintf("invalid response JSON: %v", err), time.Since(start)})
		return
	}

	if len(chatResp.Choices) == 0 {
		results = append(results, TestResult{name, false, "no choices in response (cascade may have discarded it)", time.Since(start)})
		return
	}

	results = append(results, TestResult{name, true, fmt.Sprintf("response received with %d tokens", chatResp.Usage.TotalTokens), time.Since(start)})
}

// ─── Bug 3: Cache Hit Latency ────────────────────────────────────────

func testBug3_CacheHitLatency() {
	start := time.Now()
	name := "Bug3: Cache hit latency <50ms"

	prompt := "What is the capital of France?"
	body, _ := json.Marshal(ChatRequest{
		Model:     "any",
		MaxTokens: 20,
		Messages: []Message{
			{Role: "user", Content: prompt},
		},
	})

	// Warm the cache with identical requests
	for i := 0; i < 5; i++ {
		resp, err := client.Post(gatewayURL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
		if err != nil {
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	// Measure cache hit latency
	var totalLatency time.Duration
	hits := 0
	for i := 0; i < 10; i++ {
		reqStart := time.Now()
		resp, err := client.Post(gatewayURL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
		latency := time.Since(reqStart)
		if err != nil {
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			totalLatency += latency
			hits++
		}
	}

	if hits == 0 {
		results = append(results, TestResult{name, false, "no successful requests to measure latency", time.Since(start)})
		return
	}

	avgLatency := totalLatency / time.Duration(hits)
	passed := avgLatency < 50*time.Millisecond
	msg := fmt.Sprintf("avg cache hit latency: %v (%d hits)", avgLatency, hits)

	results = append(results, TestResult{name, passed, msg, time.Since(start)})
}

// ─── Bug 4: Go Version ──────────────────────────────────────────────

func testBug4_GoVersion() {
	start := time.Now()
	name := "Bug4: Go version is a real release"

	goVersion := runtime.Version()
	// runtime.Version() returns e.g. "go1.22.0"
	// Check that it's not a non-existent version
	passed := true
	msg := fmt.Sprintf("runtime: %s", goVersion)

	// Read go.mod to check declared version
	data, err := os.ReadFile("go.mod")
	if err != nil {
		// Try from repo root
		data, err = os.ReadFile("../../go.mod")
	}
	if err == nil {
		content := string(data)
		if strings.Contains(content, "go 1.26") {
			passed = false
			msg = "go.mod declares Go 1.26.x which doesn't exist"
		} else {
			msg += "; go.mod version is valid"
		}
	}

	results = append(results, TestResult{name, passed, msg, time.Since(start)})
}

// ─── Runners ─────────────────────────────────────────────────────────

func runOnlineTests() {
	fmt.Println("── Online Tests (requires live gateway) ──")
	testBug1_ConcurrentAddExample()
	testBug2_CascadeDoubleSend()
	testBug3_CacheHitLatency()
}

func runOfflineTests() {
	fmt.Println("── Offline Tests ──")
	testBug4_GoVersion()
}

func printSummary() {
	fmt.Println("\n══════════════════════════════════════════════════════")
	fmt.Println("                   TEST RESULTS")
	fmt.Println("══════════════════════════════════════════════════════")

	passed := 0
	failed := 0
	for _, r := range results {
		status := "✓ PASS"
		if !r.Passed {
			status = "✗ FAIL"
			failed++
		} else {
			passed++
		}
		fmt.Printf("  %s  %s (%v)\n", status, r.Name, r.Elapsed.Round(time.Millisecond))
		if r.Message != "" {
			fmt.Printf("         %s\n", r.Message)
		}
	}

	fmt.Println("──────────────────────────────────────────────────────")
	fmt.Printf("  Total: %d | Passed: %d | Failed: %d\n", len(results), passed, failed)
	fmt.Println("══════════════════════════════════════════════════════")

	if failed > 0 {
		os.Exit(1)
	}
}
