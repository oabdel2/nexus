package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// CLI flags
// ---------------------------------------------------------------------------

var (
	flagURL        = flag.String("url", "http://localhost:18080", "Nexus gateway base URL")
	flagRPS        = flag.Int("rps", 50, "Target requests per second")
	flagDuration   = flag.Duration("duration", 30*time.Second, "Test duration")
	flagConcurrent = flag.Int("concurrent", 10, "Concurrent workers")
	flagScenario   = flag.String("scenario", "steady", "Scenario: steady|ramp|cache-warmup|circuit-breaker|concurrent-workflows|mixed|burst")
	flagVerbose    = flag.Bool("v", false, "Verbose per-request logging")
)

// ---------------------------------------------------------------------------
// Request / response types (OpenAI-compatible subset)
// ---------------------------------------------------------------------------

type chatRequest struct {
	Model    string    `json:"model,omitempty"`
	Messages []message `json:"messages"`
	Stream   bool      `json:"stream,omitempty"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ---------------------------------------------------------------------------
// Result tracking
// ---------------------------------------------------------------------------

type result struct {
	StatusCode int
	Latency    time.Duration
	CacheHit   bool
	CacheType  string
	Tier       string
	Err        error
}

type stats struct {
	mu         sync.Mutex
	latencies  []time.Duration
	statusMap  map[int]int
	cacheHits  int
	cacheMiss  int
	tierMap    map[string]int
	errors     int
	total      int
	startTime  time.Time
}

func newStats() *stats {
	return &stats{
		statusMap: make(map[int]int),
		tierMap:   make(map[string]int),
		startTime: time.Now(),
	}
}

func (s *stats) record(r result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	if r.Err != nil {
		s.errors++
		return
	}
	s.latencies = append(s.latencies, r.Latency)
	s.statusMap[r.StatusCode]++
	if r.CacheHit {
		s.cacheHits++
	} else {
		s.cacheMiss++
	}
	if r.Tier != "" {
		s.tierMap[r.Tier]++
	}
}

func (s *stats) report(scenario string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	elapsed := time.Since(s.startTime)
	successCount := 0
	for code, cnt := range s.statusMap {
		if code >= 200 && code < 300 {
			successCount += cnt
		}
	}

	fmt.Println()
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("  Load Test Results — scenario: %s\n", scenario)
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("  Duration:           %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  Total requests:     %d\n", s.total)
	fmt.Printf("  Successful (2xx):   %d\n", successCount)
	fmt.Printf("  Errors (conn/io):   %d\n", s.errors)
	fmt.Printf("  Success rate:       %.2f%%\n", pct(successCount, s.total))
	fmt.Printf("  Achieved RPS:       %.1f\n", float64(s.total)/elapsed.Seconds())
	fmt.Println()

	if len(s.latencies) > 0 {
		sort.Slice(s.latencies, func(i, j int) bool { return s.latencies[i] < s.latencies[j] })
		fmt.Println("  Latency percentiles:")
		fmt.Printf("    P50:   %s\n", percentile(s.latencies, 0.50))
		fmt.Printf("    P90:   %s\n", percentile(s.latencies, 0.90))
		fmt.Printf("    P95:   %s\n", percentile(s.latencies, 0.95))
		fmt.Printf("    P99:   %s\n", percentile(s.latencies, 0.99))
		fmt.Printf("    Max:   %s\n", s.latencies[len(s.latencies)-1])
		fmt.Printf("    Min:   %s\n", s.latencies[0])
		avg := avgDuration(s.latencies)
		fmt.Printf("    Avg:   %s\n", avg)
		fmt.Println()
	}

	cacheTotal := s.cacheHits + s.cacheMiss
	if cacheTotal > 0 {
		fmt.Printf("  Cache hit rate:     %.2f%% (%d/%d)\n", pct(s.cacheHits, cacheTotal), s.cacheHits, cacheTotal)
	}

	if len(s.tierMap) > 0 {
		fmt.Println("  Tier distribution:")
		for tier, cnt := range s.tierMap {
			fmt.Printf("    %-12s %d (%.1f%%)\n", tier, cnt, pct(cnt, s.total))
		}
	}

	if len(s.statusMap) > 0 {
		fmt.Println("  Status codes:")
		for code, cnt := range s.statusMap {
			fmt.Printf("    %d: %d\n", code, cnt)
		}
	}
	fmt.Println(strings.Repeat("=", 60))
}

// ---------------------------------------------------------------------------
// Percentile helpers
// ---------------------------------------------------------------------------

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func avgDuration(d []time.Duration) time.Duration {
	if len(d) == 0 {
		return 0
	}
	var sum time.Duration
	for _, v := range d {
		sum += v
	}
	return sum / time.Duration(len(d))
}

func pct(num, denom int) float64 {
	if denom == 0 {
		return 0
	}
	return float64(num) / float64(denom) * 100
}

// ---------------------------------------------------------------------------
// HTTP client
// ---------------------------------------------------------------------------

var httpClient = &http.Client{
	Timeout: 120 * time.Second,
	Transport: &http.Transport{
		MaxIdleConnsPerHost: 200,
		MaxConnsPerHost:     200,
		IdleConnTimeout:     90 * time.Second,
	},
}

func sendChat(baseURL string, req chatRequest) result {
	body, _ := json.Marshal(req)
	start := time.Now()

	resp, err := httpClient.Post(baseURL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	lat := time.Since(start)
	if err != nil {
		return result{Err: err, Latency: lat}
	}
	defer resp.Body.Close()
	// drain body
	buf := make([]byte, 32*1024)
	for {
		_, readErr := resp.Body.Read(buf)
		if readErr != nil {
			break
		}
	}

	r := result{
		StatusCode: resp.StatusCode,
		Latency:    lat,
		Tier:       resp.Header.Get("X-Nexus-Tier"),
	}
	cacheHeader := resp.Header.Get("X-Nexus-Cache")
	if cacheHeader != "" {
		r.CacheHit = true
		r.CacheType = cacheHeader
	}
	return r
}

// ---------------------------------------------------------------------------
// Prompt generators
// ---------------------------------------------------------------------------

var simplePrompts = []string{
	"What is 2+2?",
	"Say hello",
	"What color is the sky?",
	"Name a fruit",
	"What is Go?",
	"Define API",
	"What is HTTP?",
	"Name a planet",
	"What is JSON?",
	"Count to 5",
}

var mediumPrompts = []string{
	"Explain the difference between TCP and UDP in networking.",
	"Describe how a hash map works internally.",
	"What are the SOLID principles in software design?",
	"How does garbage collection work in Go?",
	"Explain the CAP theorem for distributed systems.",
}

var complexPrompts = []string{
	"Design a distributed caching system that handles cache invalidation across multiple data centers. Include considerations for consistency, partition tolerance, and latency. Provide a high-level architecture with component interactions.",
	"Write a detailed analysis of the trade-offs between microservices and monolithic architectures. Cover deployment complexity, data consistency, team organization, and performance implications. Include recommendations for when to use each approach.",
	"Explain how a modern compiler pipeline works from source code to machine code. Cover lexical analysis, parsing, semantic analysis, optimization passes, and code generation. Include examples of common optimizations.",
}

func randomSimple() chatRequest {
	return chatRequest{
		Messages: []message{{Role: "user", Content: simplePrompts[rand.Intn(len(simplePrompts))]}},
	}
}

func randomMedium() chatRequest {
	return chatRequest{
		Messages: []message{{Role: "user", Content: mediumPrompts[rand.Intn(len(mediumPrompts))]}},
	}
}

func randomComplex() chatRequest {
	return chatRequest{
		Messages: []message{{Role: "user", Content: complexPrompts[rand.Intn(len(complexPrompts))]}},
	}
}

func randomMixed() chatRequest {
	r := rand.Float64()
	switch {
	case r < 0.60:
		return randomSimple()
	case r < 0.90:
		return randomMedium()
	default:
		return randomComplex()
	}
}

// ---------------------------------------------------------------------------
// Worker pool with rate limiter
// ---------------------------------------------------------------------------

func runWithRateLimit(rps int, duration time.Duration, workers int, reqFn func() chatRequest, st *stats) {
	var wg sync.WaitGroup
	reqCh := make(chan chatRequest, workers*2)

	// spawn workers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for req := range reqCh {
				res := sendChat(*flagURL, req)
				st.record(res)
				if *flagVerbose {
					status := "OK"
					if res.Err != nil {
						status = res.Err.Error()
					}
					fmt.Printf("  [%s] status=%d lat=%s cache=%v tier=%s\n",
						status, res.StatusCode, res.Latency.Round(time.Millisecond), res.CacheHit, res.Tier)
				}
			}
		}()
	}

	// feed requests at target rate
	interval := time.Second / time.Duration(rps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	deadline := time.After(duration)

	for {
		select {
		case <-deadline:
			close(reqCh)
			wg.Wait()
			return
		case <-ticker.C:
			reqCh <- reqFn()
		}
	}
}

// ---------------------------------------------------------------------------
// Scenarios
// ---------------------------------------------------------------------------

func scenarioSteady(st *stats) {
	fmt.Printf("▶ Steady: %d RPS for %s with %d workers\n", *flagRPS, *flagDuration, *flagConcurrent)
	runWithRateLimit(*flagRPS, *flagDuration, *flagConcurrent, randomMixed, st)
}

func scenarioRamp(st *stats) {
	maxRPS := *flagRPS
	steps := 10
	stepDur := *flagDuration / time.Duration(steps)
	fmt.Printf("▶ Ramp: 0 → %d RPS over %s (%d steps of %s)\n", maxRPS, *flagDuration, steps, stepDur)

	for i := 1; i <= steps; i++ {
		currentRPS := maxRPS * i / steps
		if currentRPS < 1 {
			currentRPS = 1
		}
		fmt.Printf("  Step %d/%d: %d RPS\n", i, steps, currentRPS)
		runWithRateLimit(currentRPS, stepDur, *flagConcurrent, randomMixed, st)
	}
}

func scenarioCacheWarmup(st *stats) {
	// Send same prompt N times to observe cache behavior
	prompt := "Explain what a load balancer does in one paragraph."
	total := 100
	fmt.Printf("▶ Cache Warmup: sending same prompt %d times\n", total)

	var wg sync.WaitGroup
	sem := make(chan struct{}, *flagConcurrent)

	for i := 0; i < total; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			req := chatRequest{
				Messages: []message{{Role: "user", Content: prompt}},
			}
			res := sendChat(*flagURL, req)
			st.record(res)
			if *flagVerbose {
				fmt.Printf("  req lat=%s cache=%v\n", res.Latency.Round(time.Millisecond), res.CacheHit)
			}
		}()
		// Small delay between sends for cache to store
		if i == 0 {
			time.Sleep(500 * time.Millisecond)
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	}
	wg.Wait()
}

func scenarioCircuitBreaker(st *stats) {
	// Target the fake-down provider by using its model name
	total := 60
	fmt.Printf("▶ Circuit Breaker: sending %d requests to fake-model (should trigger CB)\n", total)

	var wg sync.WaitGroup
	sem := make(chan struct{}, *flagConcurrent)

	for i := 0; i < total; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			req := chatRequest{
				Model:    "fake-model",
				Messages: []message{{Role: "user", Content: fmt.Sprintf("Test request %d", idx)}},
			}
			res := sendChat(*flagURL, req)
			st.record(res)
			if *flagVerbose || idx%10 == 0 {
				status := fmt.Sprintf("status=%d", res.StatusCode)
				if res.Err != nil {
					status = res.Err.Error()
				}
				fmt.Printf("  req #%d: %s lat=%s\n", idx, status, res.Latency.Round(time.Millisecond))
			}
		}(i)
		time.Sleep(100 * time.Millisecond)
	}
	wg.Wait()
}

func scenarioConcurrentWorkflows(st *stats) {
	numWorkflows := *flagConcurrent
	stepsPerWorkflow := 5
	fmt.Printf("▶ Concurrent Workflows: %d workflows × %d steps\n", numWorkflows, stepsPerWorkflow)

	var wg sync.WaitGroup
	for w := 0; w < numWorkflows; w++ {
		wg.Add(1)
		go func(wID int) {
			defer wg.Done()
			workflowID := fmt.Sprintf("loadtest-wf-%d-%d", time.Now().UnixNano(), wID)
			for step := 0; step < stepsPerWorkflow; step++ {
				req := chatRequest{
					Messages: []message{{Role: "user", Content: fmt.Sprintf("Workflow %d step %d: %s", wID, step, mediumPrompts[step%len(mediumPrompts)])}},
				}
				body, _ := json.Marshal(req)
				start := time.Now()
				httpReq, _ := http.NewRequest("POST", *flagURL+"/v1/chat/completions", bytes.NewReader(body))
				httpReq.Header.Set("Content-Type", "application/json")
				httpReq.Header.Set("X-Workflow-ID", workflowID)
				httpReq.Header.Set("X-Agent-Role", "load-tester")

				resp, err := httpClient.Do(httpReq)
				lat := time.Since(start)
				if err != nil {
					st.record(result{Err: err, Latency: lat})
					continue
				}
				buf := make([]byte, 32*1024)
				for {
					_, readErr := resp.Body.Read(buf)
					if readErr != nil {
						break
					}
				}
				resp.Body.Close()
				r := result{
					StatusCode: resp.StatusCode,
					Latency:    lat,
					Tier:       resp.Header.Get("X-Nexus-Tier"),
				}
				if ch := resp.Header.Get("X-Nexus-Cache"); ch != "" {
					r.CacheHit = true
					r.CacheType = ch
				}
				st.record(r)
				if *flagVerbose {
					fmt.Printf("  wf=%d step=%d lat=%s tier=%s\n", wID, step, lat.Round(time.Millisecond), r.Tier)
				}
			}
		}(w)
	}
	wg.Wait()
}

func scenarioBurst(st *stats) {
	burstSize := *flagRPS * 3
	fmt.Printf("▶ Burst: quiet 5s → burst of %d requests → quiet 5s → burst of %d\n", burstSize, burstSize)

	doBurst := func(n int) {
		var wg sync.WaitGroup
		var sent atomic.Int64
		sem := make(chan struct{}, *flagConcurrent)
		for i := 0; i < n; i++ {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				res := sendChat(*flagURL, randomMixed())
				st.record(res)
				sent.Add(1)
			}()
		}
		wg.Wait()
		fmt.Printf("  Burst sent: %d requests\n", sent.Load())
	}

	// quiet period — just health checks
	fmt.Println("  Quiet period (5s)...")
	time.Sleep(5 * time.Second)

	fmt.Println("  BURST 1!")
	doBurst(burstSize)

	fmt.Println("  Quiet period (5s)...")
	time.Sleep(5 * time.Second)

	fmt.Println("  BURST 2!")
	doBurst(burstSize)
}

// ---------------------------------------------------------------------------
// Preflight: check server is reachable
// ---------------------------------------------------------------------------

func preflight() error {
	resp, err := http.Get(*flagURL + "/health")
	if err != nil {
		return fmt.Errorf("cannot reach %s/health: %w", *flagURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("health check returned %d", resp.StatusCode)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	flag.Parse()

	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║        Nexus Load Tester (stdlib only)       ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Printf("  Target:     %s\n", *flagURL)
	fmt.Printf("  Scenario:   %s\n", *flagScenario)
	fmt.Printf("  RPS:        %d\n", *flagRPS)
	fmt.Printf("  Duration:   %s\n", *flagDuration)
	fmt.Printf("  Workers:    %d\n", *flagConcurrent)
	fmt.Println()

	// Preflight check
	fmt.Print("Preflight health check... ")
	if err := preflight(); err != nil {
		fmt.Printf("FAILED: %v\n", err)
		fmt.Println("Hint: start Nexus with: go run ./cmd/nexus -config configs/nexus.test.yaml")
		os.Exit(1)
	}
	fmt.Println("OK")

	st := newStats()

	switch *flagScenario {
	case "steady":
		scenarioSteady(st)
	case "ramp":
		scenarioRamp(st)
	case "cache-warmup":
		scenarioCacheWarmup(st)
	case "circuit-breaker":
		scenarioCircuitBreaker(st)
	case "concurrent-workflows":
		scenarioConcurrentWorkflows(st)
	case "mixed":
		fmt.Printf("▶ Mixed: 60%% simple / 30%% medium / 10%% complex at %d RPS\n", *flagRPS)
		runWithRateLimit(*flagRPS, *flagDuration, *flagConcurrent, randomMixed, st)
	case "burst":
		scenarioBurst(st)
	default:
		fmt.Fprintf(os.Stderr, "unknown scenario: %s\n", *flagScenario)
		flag.Usage()
		os.Exit(1)
	}

	st.report(*flagScenario)
}
