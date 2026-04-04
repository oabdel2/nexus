// Package benchmarks provides a simulation harness for testing the Nexus
// gateway's routing, caching and workflow behaviour against a live server.
//
// The exported types and functions are designed to be called from Go tests
// or from any Go program that imports this package.
package benchmarks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ── Scenario definitions ────────────────────────────────────────────────────

// ScenarioStep represents one prompt in a workflow scenario.
type ScenarioStep struct {
	Prompt       string `json:"prompt"`
	Role         string `json:"role"`
	ExpectedTier string `json:"expected_tier"`
}

// WorkflowScenario describes an end-to-end workflow with ordered steps.
type WorkflowScenario struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Steps       []ScenarioStep `json:"steps"`
}

// ── Predefined scenarios ────────────────────────────────────────────────────

// SimpleBot returns a 5-step scenario with only simple tasks.
// Expected tiers computed for dry-run: stepRatio=i/5, budgetRatio=1.0, contextLen=0.
func SimpleBot() WorkflowScenario {
	return WorkflowScenario{
		Name:        "SimpleBot",
		Description: "Simple assistant tasks: greetings, formatting, listing",
		Steps: []ScenarioStep{
			{Prompt: "Hello, how are you?", Role: "", ExpectedTier: "cheap"},       // "?" struct bump → cheap
			{Prompt: "List the top 5 programming languages", Role: "", ExpectedTier: "economy"}, // low kw "list"
			{Prompt: "Format this JSON nicely", Role: "formatter", ExpectedTier: "economy"},     // low kw "format"
			{Prompt: "Summarize this paragraph for me", Role: "summarizer", ExpectedTier: "economy"}, // low kw "summarize"
			{Prompt: "Thank you for your help", Role: "", ExpectedTier: "economy"},              // low kw "thank","help"
		},
	}
}

// CodeReview returns an 8-step mixed-complexity code review scenario.
// Expected tiers computed for dry-run: stepRatio=i/8, budgetRatio=1.0, contextLen=0.
func CodeReview() WorkflowScenario {
	return WorkflowScenario{
		Name:        "CodeReview",
		Description: "Code review workflow: read → analyse → fix → test",
		Steps: []ScenarioStep{
			{Prompt: "List the files changed in this PR", Role: "", ExpectedTier: "economy"},            // low kw "list"
			{Prompt: "Summarize what this pull request does", Role: "summarizer", ExpectedTier: "economy"}, // low kw "summarize"
			{Prompt: "Review this code for potential issues", Role: "reviewer", ExpectedTier: "cheap"},     // mid kw "review"
			{Prompt: "Analyze the algorithm complexity of the sort function", Role: "analyst", ExpectedTier: "mid"}, // high kw
			{Prompt: "Debug this race condition in the worker pool", Role: "engineer", ExpectedTier: "mid"},         // high kw
			{Prompt: "Implement a fix for the deadlock issue", Role: "developer", ExpectedTier: "mid"},              // high kw
			{Prompt: "Write unit tests for the new handler", Role: "tester", ExpectedTier: "cheap"},                 // mid kw "write","test"
			{Prompt: "Write a commit message for these changes", Role: "", ExpectedTier: "economy"},                 // mid kw + low kw
		},
	}
}

// SecurityAudit returns a 10-step high-complexity security scenario.
// Expected tiers computed for dry-run: stepRatio=i/10, budgetRatio=1.0, contextLen=0.
func SecurityAudit() WorkflowScenario {
	return WorkflowScenario{
		Name:        "SecurityAudit",
		Description: "Security audit: threat model → scan → exploit → remediate → report",
		Steps: []ScenarioStep{
			{Prompt: "Analyze the attack surface of the authentication module", Role: "analyst", ExpectedTier: "mid"},
			{Prompt: "Identify security vulnerabilities in the session handler", Role: "engineer", ExpectedTier: "mid"},
			{Prompt: "Debug the race condition in the token refresh flow", Role: "engineer", ExpectedTier: "mid"},
			{Prompt: "Analyze the SQL injection vulnerability in the query builder", Role: "engineer", ExpectedTier: "mid"},
			{Prompt: "Implement a fix for the critical authentication bypass", Role: "developer", ExpectedTier: "mid"},
			{Prompt: "Architect a distributed rate-limiting system", Role: "architect", ExpectedTier: "mid"},
			{Prompt: "Optimize the performance of the WAF rules engine", Role: "engineer", ExpectedTier: "mid"},
			{Prompt: "Implement production-grade CSRF protection", Role: "developer", ExpectedTier: "mid"},
			{Prompt: "Refactor the security middleware for better isolation", Role: "engineer", ExpectedTier: "mid"},
			{Prompt: "Summarize the audit findings for the report", Role: "summarizer", ExpectedTier: "economy"},
		},
	}
}

// ── Result types ────────────────────────────────────────────────────────────

// StepResult captures the outcome of a single scenario step sent via HTTP.
type StepResult struct {
	StepNumber   int           `json:"step_number"`
	Prompt       string        `json:"prompt"`
	ExpectedTier string        `json:"expected_tier"`
	ActualTier   string        `json:"actual_tier"`
	Model        string        `json:"model"`
	Provider     string        `json:"provider"`
	CacheHit     bool          `json:"cache_hit"`
	Latency      time.Duration `json:"latency"`
	Cost         float64       `json:"cost"`
	Tokens       int           `json:"tokens"`
	Error        string        `json:"error,omitempty"`
}

// ScenarioResult aggregates results for a completed scenario.
type ScenarioResult struct {
	Scenario     string        `json:"scenario"`
	Steps        []StepResult  `json:"steps"`
	TotalLatency time.Duration `json:"total_latency"`
	TotalCost    float64       `json:"total_cost"`
	TotalTokens  int           `json:"total_tokens"`
	Accuracy     float64       `json:"accuracy"`
	CacheHitRate float64       `json:"cache_hit_rate"`
}

// ── HTTP harness ────────────────────────────────────────────────────────────

// chatReq is the OpenAI-compatible request body sent to the gateway.
type chatReq struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RunScenario executes a WorkflowScenario against a live Nexus gateway at
// the given baseURL (e.g. "http://localhost:8080"). It returns the
// aggregated ScenarioResult.
func RunScenario(scenario WorkflowScenario, baseURL string) ScenarioResult {
	result := ScenarioResult{Scenario: scenario.Name}
	client := &http.Client{Timeout: 60 * time.Second}
	workflowID := fmt.Sprintf("bench-%s-%d", scenario.Name, time.Now().UnixNano())

	match, cacheHits := 0, 0

	for i, step := range scenario.Steps {
		sr := runStep(client, baseURL, workflowID, step, i+1)
		result.Steps = append(result.Steps, sr)
		result.TotalLatency += sr.Latency
		result.TotalCost += sr.Cost
		result.TotalTokens += sr.Tokens
		if sr.ActualTier == step.ExpectedTier {
			match++
		}
		if sr.CacheHit {
			cacheHits++
		}
	}

	n := len(scenario.Steps)
	if n > 0 {
		result.Accuracy = float64(match) / float64(n) * 100
		result.CacheHitRate = float64(cacheHits) / float64(n) * 100
	}
	return result
}

func runStep(client *http.Client, baseURL, workflowID string, step ScenarioStep, stepNum int) StepResult {
	sr := StepResult{
		StepNumber:   stepNum,
		Prompt:       step.Prompt,
		ExpectedTier: step.ExpectedTier,
	}

	body, _ := json.Marshal(chatReq{
		Model:    "auto",
		Messages: []chatMessage{{Role: "user", Content: step.Prompt}},
	})

	req, err := http.NewRequest("POST", baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		sr.Error = err.Error()
		return sr
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workflow-ID", workflowID)
	if step.Role != "" {
		req.Header.Set("X-Agent-Role", step.Role)
	}

	start := time.Now()
	resp, err := client.Do(req)
	sr.Latency = time.Since(start)

	if err != nil {
		sr.Error = err.Error()
		return sr
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) // drain

	sr.ActualTier = resp.Header.Get("X-Nexus-Tier")
	sr.Model = resp.Header.Get("X-Nexus-Model")
	sr.Provider = resp.Header.Get("X-Nexus-Provider")
	sr.CacheHit = resp.Header.Get("X-Nexus-Cache") != ""

	if costStr := resp.Header.Get("X-Nexus-Cost"); costStr != "" {
		fmt.Sscanf(costStr, "%f", &sr.Cost)
	}

	return sr
}

// ── Report ──────────────────────────────────────────────────────────────────

// PrintReport renders an ASCII table of ScenarioResults to stdout.
func PrintReport(results []ScenarioResult) {
	sep := strings.Repeat("─", 100)

	fmt.Println()
	fmt.Println(sep)
	fmt.Println("                        NEXUS BENCHMARK REPORT")
	fmt.Println(sep)

	for _, res := range results {
		fmt.Printf("\n▸ Scenario: %s\n", res.Scenario)
		fmt.Println(strings.Repeat("─", 100))
		fmt.Printf("  %-4s  %-50s  %-8s  %-8s  %-8s  %s\n",
			"Step", "Prompt", "Expected", "Actual", "Match", "Latency")
		fmt.Println(strings.Repeat("─", 100))

		for _, s := range res.Steps {
			matchStr := "✓"
			if s.ActualTier != s.ExpectedTier {
				matchStr = "✗"
			}
			prompt := s.Prompt
			if len(prompt) > 48 {
				prompt = prompt[:45] + "..."
			}
			latStr := s.Latency.Truncate(time.Millisecond).String()
			if s.Error != "" {
				latStr = "ERR"
			}
			fmt.Printf("  %-4d  %-50s  %-8s  %-8s  %-8s  %s\n",
				s.StepNumber, prompt, s.ExpectedTier, s.ActualTier, matchStr, latStr)
		}

		fmt.Println(strings.Repeat("─", 100))
		fmt.Printf("  Accuracy:      %.0f%%\n", res.Accuracy)
		fmt.Printf("  Cache Hit:     %.0f%%\n", res.CacheHitRate)
		fmt.Printf("  Total Latency: %s\n", res.TotalLatency.Truncate(time.Millisecond))
		fmt.Printf("  Total Cost:    $%.6f\n", res.TotalCost)
		fmt.Printf("  Total Tokens:  %d\n", res.TotalTokens)
	}

	fmt.Println()
	fmt.Println(sep)
	fmt.Println("                         END OF REPORT")
	fmt.Println(sep)
	fmt.Println()
}

// ── Convenience runner ──────────────────────────────────────────────────────

// RunAllScenarios executes all predefined scenarios against the given
// baseURL and prints a consolidated report. Suitable for calling from
// a test function or a CLI wrapper.
func RunAllScenarios(baseURL string) []ScenarioResult {
	scenarios := []WorkflowScenario{
		SimpleBot(),
		CodeReview(),
		SecurityAudit(),
	}

	var results []ScenarioResult
	for _, sc := range scenarios {
		fmt.Printf("Running scenario %q ...\n", sc.Name)
		res := RunScenario(sc, baseURL)
		results = append(results, res)
	}

	PrintReport(results)
	return results
}
