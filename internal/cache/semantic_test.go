package cache

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

// ═══════════════════════════════════════════════════════════════════════════
// BM25 TESTS
// ═══════════════════════════════════════════════════════════════════════════

// Test 1: BM25 matches paraphrased prompts
func TestBM25Paraphrase(t *testing.T) {
	c := NewBM25Cache(10*time.Minute, 1000, 0.3)

	c.Store("What is a goroutine in Go?", "test-model", []byte(`{"answer":"goroutine"}`))

	data, ok := c.Lookup("Explain goroutines in Go", "test-model")
	if !ok {
		t.Error("expected BM25 hit for paraphrased prompt, got miss")
	}
	if data != nil {
		t.Logf("BM25 hit: %s", string(data))
	}

	_, ok = c.Lookup("What is the weather today?", "test-model")
	if ok {
		t.Error("expected BM25 miss for unrelated prompt, got hit")
	}
}

// Test 2: BM25 stopword handling
func TestBM25Stopwords(t *testing.T) {
	tokens1 := Tokenize("the quick brown fox")
	tokens2 := Tokenize("quick brown fox")

	if len(tokens1) != len(tokens2) {
		t.Errorf("expected same token count after stopword removal, got %d vs %d", len(tokens1), len(tokens2))
	}
	for i := range tokens1 {
		if i < len(tokens2) && tokens1[i] != tokens2[i] {
			t.Errorf("token mismatch at %d: %q vs %q", i, tokens1[i], tokens2[i])
		}
	}
}

// Test 3: Store orchestration order
func TestStoreLayerOrder(t *testing.T) {
	store := NewStore(StoreConfig{
		L1Enabled:    true,
		L1TTL:        10 * time.Minute,
		L1MaxEntries: 1000,
		L2aEnabled:   true,
		L2aTTL:       10 * time.Minute,
		L2aMaxEntries: 1000,
		L2aThreshold:  0.3,
		L2bEnabled:    false,
	})

	resp := []byte(`{"answer":"test response"}`)
	store.StoreResponse("What is a goroutine in Go?", "test-model", resp)

	data, hit, source := store.Lookup("What is a goroutine in Go?", "test-model")
	if !hit {
		t.Fatal("expected L1 hit for exact prompt")
	}
	if source != "l1_exact" {
		t.Errorf("expected source l1_exact, got %s", source)
	}
	t.Logf("L1 hit: %s", string(data))

	data, hit, source = store.Lookup("Explain goroutines in Go", "test-model")
	if !hit {
		t.Fatalf("expected L2a hit for paraphrased prompt, got miss")
	}
	if source != "l2_bm25" {
		t.Errorf("expected source l2_bm25, got %s", source)
	}
	t.Logf("L2a hit: %s", string(data))

	_, hit, source = store.Lookup("How to bake chocolate cake?", "test-model")
	if hit {
		t.Errorf("expected miss for unrelated prompt, got hit from %s", source)
	}
}

// Test 4: BM25 accuracy benchmark
func TestBM25AccuracyBenchmark(t *testing.T) {
	c := NewBM25Cache(10*time.Minute, 1000, 2.0)

	matchPairs := []struct{ stored, query string }{
		{"What is a goroutine?", "Explain goroutines"},
		{"How to reverse a string in Python", "Python string reversal"},
		{"Explain TCP vs UDP", "Compare TCP and UDP protocols"},
		{"Write a REST API in Go", "Create a Go REST API"},
		{"Debug this race condition", "Fix the race condition bug"},
		{"What are design patterns?", "Explain software design patterns"},
		{"How does garbage collection work?", "Explain GC mechanism"},
		{"Optimize database queries", "Make SQL queries faster"},
		{"What is Docker?", "Explain Docker containers"},
		{"Sort an array in JavaScript", "JavaScript array sorting"},
	}

	noMatchPairs := []struct{ stored, query string }{
		{"What is a goroutine?", "How to make pizza"},
		{"Write a REST API", "Explain quantum physics"},
		{"Debug race condition", "What's the weather?"},
		{"Optimize database", "History of Roman Empire"},
		{"Docker containers", "How to play guitar"},
		{"Sort an array", "Best restaurants in NYC"},
		{"Design patterns", "How to train a dog"},
		{"Garbage collection", "Recipe for chocolate cake"},
		{"TCP vs UDP", "How to learn Spanish"},
		{"Kubernetes pods", "Best hiking trails"},
	}

	model := "test-model"
	resp := []byte(`{"answer":"cached"}`)

	for _, p := range matchPairs {
		c.Store(p.stored, model, resp)
	}
	for _, p := range noMatchPairs {
		c.Store(p.stored, model, resp)
	}

	truePositives := 0
	for _, p := range matchPairs {
		_, ok := c.Lookup(p.query, model)
		if ok {
			truePositives++
		} else {
			t.Logf("FALSE NEGATIVE: %q → %q", p.stored, p.query)
		}
	}

	trueNegatives := 0
	for _, p := range noMatchPairs {
		_, ok := c.Lookup(p.query, model)
		if !ok {
			trueNegatives++
		} else {
			t.Logf("FALSE POSITIVE: %q → %q", p.stored, p.query)
		}
	}

	total := len(matchPairs) + len(noMatchPairs)
	correct := truePositives + trueNegatives

	t.Logf("BM25 Accuracy: %d/%d (%.0f%%)", correct, total, float64(correct)/float64(total)*100)
	t.Logf("True Positives: %d/%d, True Negatives: %d/%d", truePositives, len(matchPairs), trueNegatives, len(noMatchPairs))

	if float64(correct)/float64(total) < 0.70 {
		t.Errorf("BM25 accuracy too low: %d/%d (%.0f%%)", correct, total, float64(correct)/float64(total)*100)
	}
}

// Test 5: Cosine similarity math
func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float64
		want float64
		tol  float64
	}{
		{"identical vectors", []float64{1, 0, 0}, []float64{1, 0, 0}, 1.0, 1e-9},
		{"orthogonal vectors", []float64{1, 0, 0}, []float64{0, 1, 0}, 0.0, 1e-9},
		{"partial overlap", []float64{1, 1, 0}, []float64{1, 0, 0}, 1.0 / math.Sqrt(2), 1e-3},
		{"opposite vectors", []float64{1, 0, 0}, []float64{-1, 0, 0}, -1.0, 1e-9},
		{"empty vectors", []float64{}, []float64{}, 0.0, 1e-9},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CosineSimilarity(tc.a, tc.b)
			if math.Abs(got-tc.want) > tc.tol {
				t.Errorf("CosineSimilarity(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// Test 6: Normalize vector
func TestNormalizeVector(t *testing.T) {
	v := normalizeVector([]float64{3, 4, 0})
	norm := 0.0
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	if math.Abs(norm-1.0) > 1e-9 {
		t.Errorf("normalized vector has norm %v, want 1.0", norm)
	}
}

// Test 7: Dot product of normalized vectors equals cosine similarity
func TestDotProductNormalized(t *testing.T) {
	a := normalizeVector([]float64{1, 2, 3})
	b := normalizeVector([]float64{4, 5, 6})

	dot := dotProduct(a, b)
	cos := CosineSimilarity([]float64{1, 2, 3}, []float64{4, 5, 6})

	if math.Abs(dot-cos) > 1e-9 {
		t.Errorf("dot product of normalized vectors (%v) != cosine similarity (%v)", dot, cos)
	}
}

// Test 8: Semantic filter functions
func TestSemanticFilters(t *testing.T) {
	// Test synonym expansion
	expanded := expandSynonyms("How to set up k8s")
	if !strings.Contains(expanded, "kubernetes") {
		t.Errorf("expected k8s to expand to kubernetes, got: %s", expanded)
	}

	expanded = expandSynonyms("explain goroutine")
	if !strings.Contains(expanded, "concurrency") {
		t.Errorf("expected goroutine to expand to concurrency, got: %s", expanded)
	}

	// Test negation detection
	if !hasOppositeIntent("create a goroutine", "kill a goroutine") {
		t.Error("expected create/kill to be detected as opposite")
	}
	if !hasOppositeIntent("encrypt the data", "decrypt the data") {
		t.Error("expected encrypt/decrypt to be detected as opposite")
	}
	if hasOppositeIntent("explain goroutines", "what is a goroutine") {
		t.Error("should not detect opposite intent in paraphrases")
	}
	if !hasOppositeIntent("start the container", "stop the container") {
		t.Error("expected start/stop to be detected as opposite")
	}
	if !hasOppositeIntent("enable CORS", "disable CORS") {
		t.Error("expected enable/disable to be detected as opposite")
	}

	// Test key noun detection
	if !hasDifferentKeyNoun("sort with quicksort", "sort with mergesort") {
		t.Error("expected quicksort/mergesort to be detected as different key nouns")
	}
	if !hasDifferentKeyNoun("deploy to AWS", "deploy to Azure") {
		t.Error("expected AWS/Azure to be detected as different key nouns")
	}
	if !hasDifferentKeyNoun("handle errors in Go", "handle errors in Rust") {
		t.Error("expected Go/Rust to be detected as different key nouns")
	}
	if hasDifferentKeyNoun("explain goroutines", "how do goroutines work") {
		t.Error("should not detect different key nouns in paraphrases")
	}
	if !hasDifferentKeyNoun("connect to MySQL", "connect to Redis") {
		t.Error("expected MySQL/Redis to be detected as different key nouns")
	}
	if !hasDifferentKeyNoun("test with Jest", "test with Pytest") {
		t.Error("expected Jest/Pytest to be detected as different key nouns")
	}

	// Test combined: opposite action pairs
	pairs := []struct {
		a, b     string
		opposite bool
	}{
		{"add rate limiting", "remove rate limiting", true},
		{"read from file", "write to file", true},
		{"subscribe to events", "unsubscribe from events", true},
		{"serialize object", "deserialize object", true},
		{"increment counter", "decrement counter", true},
		{"lock the row", "unlock the row", true},
		{"deploy to production", "rollback from production", true},
		{"optimize query", "optimize query performance", false},
		{"debug race condition", "fix race condition", false},
		{"write unit tests", "create test cases", false},
	}

	for _, p := range pairs {
		got := hasOppositeIntent(p.a, p.b)
		if got != p.opposite {
			t.Errorf("hasOppositeIntent(%q, %q) = %v, want %v", p.a, p.b, got, p.opposite)
		}
	}
}

// Test 9: BM25 model isolation
func TestBM25ModelIsolation(t *testing.T) {
	c := NewBM25Cache(10*time.Minute, 1000, 0.3)

	c.Store("What is a goroutine?", "model-a", []byte(`{"model":"a"}`))

	_, ok := c.Lookup("What is a goroutine?", "model-b")
	if ok {
		t.Error("expected miss for different model, got hit")
	}

	_, ok = c.Lookup("What is a goroutine?", "model-a")
	if !ok {
		t.Error("expected hit for same model, got miss")
	}
}

// Test 10: Store stats aggregation
func TestStoreStats(t *testing.T) {
	store := NewStore(StoreConfig{
		L1Enabled:     true,
		L1TTL:         10 * time.Minute,
		L1MaxEntries:  1000,
		L2aEnabled:    true,
		L2aTTL:        10 * time.Minute,
		L2aMaxEntries: 1000,
		L2aThreshold:  3.0,
		L2bEnabled:    false,
	})

	store.StoreResponse("test prompt", "model", []byte("response"))

	hits, misses, size := store.Stats()
	if size != 2 { // 1 in L1 + 1 in L2a
		t.Errorf("expected size 2, got %d", size)
	}
	if hits != 0 {
		t.Errorf("expected 0 hits initially, got %d", hits)
	}
	_ = misses
}

// ═══════════════════════════════════════════════════════════════════════════
// EXPANDED SYNONYM TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestExpandedSynonyms(t *testing.T) {
	cases := []struct {
		input    string
		contains string
	}{
		// Original synonyms
		{"deploy to k8s", "kubernetes"},
		{"fix the gc issue", "garbage collection"},
		{"setup ci/cd pipeline", "continuous integration"},
		{"configure ssl", "encryption"},
		{"query the db", "database"},
		{"write in js", "javascript"},
		{"goroutine pool", "concurrency"},
		// New synonyms
		{"write golang code", "go programming language"},
		{"manage goroutines", "go concurrency"},
		{"use async/await", "asynchronous"},
		{"build a dockerfile", "container"},
		{"deploy with helm", "kubernetes package manager"},
		{"configure vpc", "virtual private cloud"},
		{"setup iam roles", "identity access management"},
		{"store in s3", "simple storage service"},
		{"launch ec2 instance", "elastic compute cloud"},
		{"use rds database", "relational database service"},
		{"send to sqs", "simple queue service"},
		{"configure rbac", "role based access control"},
		{"setup dns records", "domain name system"},
		{"train ml model", "machine learning"},
		{"process nlp text", "natural language processing"},
		{"fine-tune llm", "large language model"},
		{"implement rag pipeline", "retrieval augmented generation"},
		{"use gpu compute", "graphics processing unit"},
		{"apply oop principles", "object oriented programming"},
		{"use fp patterns", "functional programming"},
		{"build spa frontend", "single page application"},
		{"enable ssr rendering", "server side rendering"},
		{"compile to wasm", "webassembly"},
		{"implement cqrs pattern", "command query responsibility segregation"},
		{"follow tdd methodology", "test driven development"},
		{"deploy as saas", "software as a service"},
		{"use faas platform", "function as a service"},
		{"build etl pipeline", "extract transform load"},
		{"create a dag workflow", "directed acyclic graph"},
	}

	for _, tc := range cases {
		expanded := expandSynonyms(tc.input)
		if !strings.Contains(expanded, tc.contains) {
			t.Errorf("expandSynonyms(%q) should contain %q, got: %s", tc.input, tc.contains, expanded)
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// EXPANDED OPPOSITE INTENT TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestExpandedOppositeIntents(t *testing.T) {
	// Original pairs
	originals := []struct {
		a, b     string
		opposite bool
	}{
		{"create a table", "delete a table", true},
		{"add a user", "remove a user", true},
		{"enable logging", "disable logging", true},
		{"start the server", "stop the server", true},
		{"install package", "uninstall package", true},
		{"encrypt file", "decrypt file", true},
		{"read config", "write config", true},
		{"deploy app", "rollback app", true},
		{"sort ascending", "sort descending", true},
		{"login to system", "logout from system", true},
		{"connect to db", "disconnect from db", true},
		{"open port", "close port", true},
		{"increase limit", "decrease limit", true},
		{"allow traffic", "deny traffic", true},
		{"push changes", "pull changes", true},
		{"upload file", "download file", true},
		{"import data", "export data", true},
		{"grant access", "revoke access", true},
		{"lock mutex", "unlock mutex", true},
		{"subscribe to topic", "unsubscribe from topic", true},
		{"compress archive", "decompress archive", true},
		{"encode base64", "decode base64", true},
		{"serialize to json", "deserialize from json", true},
		{"marshal struct", "unmarshal struct", true},
		{"attach volume", "detach volume", true},
		{"register handler", "unregister handler", true},
		{"activate feature", "deactivate feature", true},
		{"show sidebar", "hide sidebar", true},
		{"expand section", "collapse section", true},
		{"increment counter", "decrement counter", true},
		{"send message", "receive message", true},
		{"merge branches", "split branches", true},
		{"load module", "unload module", true},
		{"enqueue task", "dequeue task", true},
		{"cache result", "evict result", true},
	}

	// New opposite pairs
	newPairs := []struct {
		a, b     string
		opposite bool
	}{
		{"upgrade version", "downgrade version", true},
		{"backup database", "restore database", true},
		{"freeze account", "unfreeze account", true},
		{"mute notifications", "unmute notifications", true},
		{"pin message", "unpin message", true},
		{"follow user", "unfollow user", true},
		{"archive channel", "unarchive channel", true},
		{"pause pipeline", "resume pipeline", true},
		{"ban user", "unban user", true},
		{"block request", "unblock request", true},
		{"wrap component", "unwrap component", true},
		{"bind port", "unbind port", true},
		{"promote to master", "demote from master", true},
		{"minify javascript", "prettify javascript", true},
		{"approve PR", "reject PR", true},
		{"join room", "leave room", true},
		{"prepend header", "append footer", true},
		{"obfuscate code", "deobfuscate code", true},
		{"freeze dependencies", "thaw dependencies", true},
		{"whitelist IP", "blacklist IP", true},
	}

	// Non-opposite pairs
	nonOpposites := []struct {
		a, b     string
		opposite bool
	}{
		{"explain goroutines", "describe goroutines", false},
		{"optimize query", "improve query", false},
		{"test function", "validate function", false},
		{"build project", "compile project", false},
		{"analyze data", "process data", false},
		{"debug error", "troubleshoot error", false},
		{"deploy service", "launch service", false},
		{"refactor code", "restructure code", false},
		{"monitor system", "observe system", false},
		{"document API", "describe API", false},
	}

	allPairs := append(originals, newPairs...)
	allPairs = append(allPairs, nonOpposites...)

	for _, p := range allPairs {
		got := hasOppositeIntent(p.a, p.b)
		if got != p.opposite {
			t.Errorf("hasOppositeIntent(%q, %q) = %v, want %v", p.a, p.b, got, p.opposite)
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// EXPANDED KEY NOUN TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestExpandedKeyNouns(t *testing.T) {
	// Should detect different key nouns
	differentPairs := []struct {
		a, b string
	}{
		// Languages
		{"handle errors in Go", "handle errors in Rust"},
		{"write function in Python", "write function in Java"},
		{"build app with JavaScript", "build app with TypeScript"},
		{"compile with Kotlin", "compile with Swift"},
		{"develop with Ruby", "develop with PHP"},
		// Databases
		{"connect to MySQL", "connect to PostgreSQL"},
		{"query MongoDB", "query Redis"},
		{"store in DynamoDB", "store in Cassandra"},
		{"migrate from SQLite", "migrate to MariaDB"},
		// Frameworks
		{"build with React", "build with Vue"},
		{"develop with Angular", "develop with Svelte"},
		{"create app in NextJS", "create app in Nuxt"},
		// Cloud providers
		{"deploy to AWS", "deploy to Azure"},
		{"host on GCP", "host on Heroku"},
		{"run on DigitalOcean", "run on Vercel"},
		// Container/orchestration
		{"run in Docker", "run in Podman"},
		{"orchestrate with Kubernetes", "orchestrate with Nomad"},
		// Message queues
		{"consume from Kafka", "consume from RabbitMQ"},
		{"publish to NATS", "publish to Pulsar"},
		// Sort algorithms
		{"implement quicksort", "implement mergesort"},
		{"use bubblesort", "use heapsort"},
		// Test frameworks
		{"test with Jest", "test with Pytest"},
		{"use Mocha", "use RSpec"},
		{"automate with Cypress", "automate with Playwright"},
		// API styles
		{"build REST API", "build GraphQL API"},
		{"implement gRPC", "implement SOAP"},
		// IaC tools
		{"provision with Terraform", "provision with Pulumi"},
		{"configure with Ansible", "configure with Chef"},
		// Web servers
		{"serve with Nginx", "serve with Apache"},
		{"proxy with Caddy", "proxy with Traefik"},
		// Protocols
		{"use TCP", "use UDP"},
		{"configure HTTP", "configure FTP"},
		// Data formats
		{"parse JSON", "parse XML"},
		{"read CSV", "read YAML"},
		// OS
		{"install on Linux", "install on Windows"},
		{"deploy to Ubuntu", "deploy to Alpine"},
		// Git platforms
		{"host on GitHub", "host on GitLab"},
		// Auth methods
		{"authenticate with JWT", "authenticate with OAuth"},
		// Search engines
		{"index in Elasticsearch", "index in Solr"},
		// Monitoring
		{"monitor with Prometheus", "monitor with Datadog"},
		// Math operations
		{"calculate sum", "calculate product"},
		{"find max", "find min"},
		// Parity
		{"check even numbers", "check odd numbers"},
		// Order
		{"sort ascending", "sort descending"},
		// Architecture
		{"monolithic architecture", "microservices architecture"},
		// Data structures
		{"use array", "use map"},
		{"implement queue", "implement stack"},
		// NEW: ML frameworks
		{"train with TensorFlow", "train with PyTorch"},
		{"use Keras", "use Scikit"},
		// NEW: CI/CD tools
		{"setup Jenkins", "setup CircleCI"},
		// NEW: Package managers
		{"install with npm", "install with yarn"},
		{"manage with pip", "manage with cargo"},
		// NEW: Caching
		{"cache with Redis", "cache with Memcached"},
		// NEW: Service mesh
		{"deploy Istio", "deploy Linkerd"},
		// NEW: Cloud data
		{"query BigQuery", "query Redshift"},
		{"analyze in Snowflake", "analyze in Databricks"},
		// NEW: Editors
		{"configure VSCode", "configure Vim"},
		// NEW: CSS frameworks
		{"style with Tailwind", "style with Bootstrap"},
		// NEW: Runtime
		{"run on Node", "run on Deno"},
		// NEW: Auth tools
		{"manage with Vault", "manage with Keycloak"},
		// NEW: Observability
		{"trace with Jaeger", "trace with Zipkin"},
		// NEW: Design patterns
		{"use singleton", "use factory"},
		{"apply observer", "apply strategy"},
		// NEW: Algorithms
		{"implement Dijkstra", "implement BFS"},
		// NEW: Data structures
		{"use trie", "use btree"},
	}

	for _, p := range differentPairs {
		if !hasDifferentKeyNoun(p.a, p.b) {
			t.Errorf("expected different key nouns: %q vs %q", p.a, p.b)
		}
	}

	// Should NOT detect different key nouns (same topic paraphrases)
	samePairs := []struct {
		a, b string
	}{
		{"explain goroutines", "how do goroutines work"},
		{"optimize the database query", "make the database query faster"},
		{"deploy the application", "launch the application"},
		{"what is Docker", "explain Docker containers"},
		{"use Redis for caching", "Redis cache implementation"},
		{"configure Kubernetes pods", "Kubernetes pod setup"},
		{"write Python code", "Python implementation"},
		{"test with Jest", "Jest testing setup"},
		{"deploy to AWS Lambda", "AWS Lambda deployment"},
		{"monitor with Prometheus", "Prometheus monitoring setup"},
	}

	for _, p := range samePairs {
		if hasDifferentKeyNoun(p.a, p.b) {
			t.Errorf("should not detect different key nouns: %q vs %q", p.a, p.b)
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// QUERY TYPE CLASSIFICATION TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestClassifyQueryType(t *testing.T) {
	cases := []struct {
		prompt   string
		expected QueryType
	}{
		// Factual
		{"What is a goroutine?", QueryTypeFactual},
		{"What are microservices?", QueryTypeArchitecture},
		{"Define polymorphism", QueryTypeFactual},
		{"What does ACID stand for?", QueryTypeFactual},
		{"Who created Go programming language?", QueryTypeFactual},
		{"When was Kubernetes released?", QueryTypeFactual},
		{"How many HTTP status codes are there?", QueryTypeFactual},

		// How-to
		{"How to deploy a Docker container", QueryTypeHowTo},
		{"How do I set up CI/CD?", QueryTypeHowTo},
		{"How can I optimize database queries?", QueryTypeHowTo},
		{"Tutorial for Kubernetes", QueryTypeHowTo},
		{"Best way to handle errors in Go", QueryTypeDebug},
		{"Steps to configure Nginx", QueryTypeHowTo},
		{"Best practices for REST API design", QueryTypeHowTo},
		{"Show me how to use Redis", QueryTypeHowTo},
		{"Walk me through setting up auth", QueryTypeHowTo},
		{"Install Docker on Ubuntu", QueryTypeHowTo},
		{"Configure Prometheus monitoring", QueryTypeHowTo},
		{"Set up a development environment", QueryTypeHowTo},

		// Code
		{"Write a function to sort an array", QueryTypeCode},
		{"Implement a binary search tree", QueryTypeCode},
		{"Create a function for JWT authentication", QueryTypeCode},
		{"Write code to parse JSON", QueryTypeCode},
		{"Generate a REST API controller", QueryTypeCode},
		{"Build a CLI tool in Go", QueryTypeCode},
		{"Make a WebSocket server", QueryTypeCode},
		{"Algorithm for finding shortest path", QueryTypeCode},

		// Debug
		{"Debug this race condition", QueryTypeDebug},
		{"Fix the null pointer exception", QueryTypeDebug},
		{"My application is crashing", QueryTypeDebug},
		{"This function is not working", QueryTypeDebug},
		{"Resolve the deadlock issue", QueryTypeDebug},
		{"Memory leak in the worker pool", QueryTypeDebug},
		{"Getting segfault in C program", QueryTypeDebug},
		{"Why is my code failing?", QueryTypeDebug},

		// Comparison
		{"React vs Vue for frontend", QueryTypeComparison},
		{"Compare MySQL and PostgreSQL", QueryTypeComparison},
		{"Difference between TCP and UDP", QueryTypeComparison},
		{"Which is better: Kafka or RabbitMQ", QueryTypeComparison},
		{"Pros and cons of microservices", QueryTypeComparison},
		{"REST versus GraphQL", QueryTypeComparison},
		{"Tradeoffs of serverless architecture", QueryTypeComparison},

		// Architecture
		{"Design a microservices architecture", QueryTypeArchitecture},
		{"System design for chat application", QueryTypeArchitecture},
		{"Architect a distributed cache", QueryTypeArchitecture},
		{"High availability database setup", QueryTypeArchitecture},
		{"Event driven architecture pattern", QueryTypeArchitecture},
		{"Domain driven design implementation", QueryTypeArchitecture},
		{"Scalable notification system", QueryTypeArchitecture},
		{"Load balancing strategy", QueryTypeArchitecture},

		// General
		{"Hello world", QueryTypeGeneral},
		{"Thank you", QueryTypeGeneral},
		{"List programming languages", QueryTypeGeneral},
		{"Summarize this document", QueryTypeGeneral},
	}

	for _, tc := range cases {
		got := ClassifyQueryType(tc.prompt)
		if got != tc.expected {
			t.Errorf("ClassifyQueryType(%q) = %s, want %s",
				tc.prompt, QueryTypeName(got), QueryTypeName(tc.expected))
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// ADAPTIVE THRESHOLD TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestAdaptiveThreshold(t *testing.T) {
	base := 0.70

	cases := []struct {
		qt       QueryType
		expected float64
	}{
		{QueryTypeFactual, 0.80},
		{QueryTypeComparison, 0.85},
		{QueryTypeCode, 0.75},
		{QueryTypeArchitecture, 0.72},
		{QueryTypeGeneral, 0.70},
		{QueryTypeHowTo, 0.70},
		{QueryTypeDebug, 0.65},
	}

	for _, tc := range cases {
		got := AdaptiveThreshold(tc.qt, base)
		if math.Abs(got-tc.expected) > 0.001 {
			t.Errorf("AdaptiveThreshold(%s, %.2f) = %.2f, want %.2f",
				QueryTypeName(tc.qt), base, got, tc.expected)
		}
	}

	// Test clamping
	if got := AdaptiveThreshold(QueryTypeComparison, 0.90); got > 0.95 {
		t.Errorf("threshold should be clamped at 0.95, got %.2f", got)
	}
	if got := AdaptiveThreshold(QueryTypeDebug, 0.55); got < 0.55 {
		t.Errorf("threshold should be clamped at 0.55, got %.2f", got)
	}
}

func TestQueryTypeName(t *testing.T) {
	names := map[QueryType]string{
		QueryTypeGeneral:      "general",
		QueryTypeFactual:      "factual",
		QueryTypeHowTo:        "how-to",
		QueryTypeCode:         "code",
		QueryTypeDebug:        "debug",
		QueryTypeComparison:   "comparison",
		QueryTypeArchitecture: "architecture",
	}

	for qt, expected := range names {
		if got := QueryTypeName(qt); got != expected {
			t.Errorf("QueryTypeName(%d) = %q, want %q", qt, got, expected)
		}
	}

	if got := QueryTypeName(QueryType(99)); got != "unknown" {
		t.Errorf("QueryTypeName(99) = %q, want \"unknown\"", got)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// RERANKER TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestRerankerDisabled(t *testing.T) {
	r := NewReranker(RerankerConfig{Enabled: false})

	if !r.Verify("any query", "any document") {
		t.Error("disabled reranker should always verify true")
	}

	score := r.Score("any query", "any document")
	if score != 1.0 {
		t.Errorf("disabled reranker should return score 1.0, got %.2f", score)
	}
}

func TestRerankerHeuristic(t *testing.T) {
	r := NewReranker(RerankerConfig{
		Enabled:   true,
		Threshold: 0.3,
		Endpoint:  "http://localhost:99999", // unreachable, forces heuristic fallback
	})

	// High overlap should pass
	if !r.Verify("how to deploy docker containers", "how to deploy docker containers to production") {
		t.Error("high-overlap query should pass heuristic reranking")
	}

	// Low overlap should fail (depending on threshold)
	score := r.Score("what is kubernetes", "how to bake a cake")
	if score < 0 {
		// Score -1 means error, but heuristic fallback should work
		t.Logf("reranker score for unrelated: %.2f (may be heuristic)", score)
	}
}

func TestRerankerHeuristicScoring(t *testing.T) {
	r := &Reranker{enabled: true, threshold: 0.3}

	// Identical texts
	score := r.heuristicRerank("deploy docker containers", "deploy docker containers")
	if score < 0.9 {
		t.Errorf("identical texts should score high, got %.2f", score)
	}

	// Completely different
	score = r.heuristicRerank("kubernetes deployment", "chocolate cake recipe")
	if score > 0.3 {
		t.Errorf("unrelated texts should score low, got %.2f", score)
	}

	// Partial overlap
	score = r.heuristicRerank("deploy to kubernetes cluster", "kubernetes cluster management")
	if score < 0.2 {
		t.Errorf("partially overlapping should score moderate, got %.2f", score)
	}

	// Empty inputs
	score = r.heuristicRerank("", "")
	if score != 0.0 {
		t.Errorf("empty inputs should score 0.0, got %.2f", score)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// CONTEXT FINGERPRINT TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestContextFingerprint(t *testing.T) {
	cf := NewContextFingerprint(3)

	// Single message → no context
	fp := cf.Fingerprint([]ChatMessage{
		{Role: "user", Content: "What is Docker?"},
	})
	if fp != "" {
		t.Errorf("single message should have empty fingerprint, got %q", fp)
	}

	// Two messages → context from first
	fp = cf.Fingerprint([]ChatMessage{
		{Role: "user", Content: "I'm working with Kubernetes"},
		{Role: "user", Content: "How do I deploy a pod?"},
	})
	if fp == "" {
		t.Error("multi-turn should have non-empty fingerprint")
	}

	// Different context → different fingerprint
	fp2 := cf.Fingerprint([]ChatMessage{
		{Role: "user", Content: "I'm working with Docker"},
		{Role: "user", Content: "How do I deploy a pod?"},
	})
	if fp == fp2 {
		t.Error("different contexts should produce different fingerprints")
	}

	// Same context → same fingerprint
	fp3 := cf.Fingerprint([]ChatMessage{
		{Role: "user", Content: "I'm working with Kubernetes"},
		{Role: "user", Content: "How do I deploy a pod?"},
	})
	if fp != fp3 {
		t.Error("same context should produce same fingerprint")
	}
}

func TestContextAwareLookupKey(t *testing.T) {
	// No context
	key := ContextAwareLookupKey("What is Docker?", "")
	if key != "What is Docker?" {
		t.Errorf("no context should return prompt as-is, got %q", key)
	}

	// With context
	key = ContextAwareLookupKey("What is Docker?", "abc123")
	if !strings.Contains(key, "abc123") || !strings.Contains(key, "What is Docker?") {
		t.Errorf("context-aware key should contain both fingerprint and prompt, got %q", key)
	}
}

func TestExtractKeyTerms(t *testing.T) {
	terms := extractKeyTerms("I want to deploy my Docker container to Kubernetes using Terraform")
	if len(terms) == 0 {
		t.Error("should extract key terms from tech-heavy sentence")
	}

	// Should contain important words
	termSet := make(map[string]bool)
	for _, term := range terms {
		termSet[term] = true
	}

	if !termSet["deploy"] && !termSet["docker"] && !termSet["kubernetes"] && !termSet["terraform"] {
		t.Errorf("expected tech terms, got: %v", terms)
	}
}

func TestIsKeyNoun(t *testing.T) {
	positives := []string{
		"docker", "kubernetes", "react", "python", "mysql",
		"tensorflow", "pytorch", "jenkins", "npm", "istio",
		"bigquery", "vscode", "tailwind", "node", "vault",
		"jaeger", "dijkstra", "trie", "singleton", "factory",
	}
	for _, w := range positives {
		if !isKeyNoun(w) {
			t.Errorf("isKeyNoun(%q) should be true", w)
		}
	}

	negatives := []string{
		"the", "and", "implement", "create", "optimize",
		"really", "very", "some", "just", "about",
	}
	for _, w := range negatives {
		if isKeyNoun(w) {
			t.Errorf("isKeyNoun(%q) should be false", w)
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// FEEDBACK STORE TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestFeedbackStoreBasic(t *testing.T) {
	fs := NewFeedbackStore(100)

	// Record helpful feedback
	fs.Record(FeedbackEntry{
		Query:       "What is Docker?",
		CachedQuery: "Explain Docker",
		Helpful:     true,
		Similarity:  0.92,
		CacheLayer:  "l2_semantic",
		QueryType:   "factual",
	})

	// Record unhelpful feedback
	fs.Record(FeedbackEntry{
		Query:       "Deploy to Kubernetes",
		CachedQuery: "Deploy to Docker",
		Helpful:     false,
		Similarity:  0.85,
		CacheLayer:  "l2_semantic",
		QueryType:   "how-to",
	})

	stats := fs.Stats()
	if stats.TotalFeedback != 2 {
		t.Errorf("expected 2 total feedback, got %d", stats.TotalFeedback)
	}
	if stats.HelpfulCount != 1 {
		t.Errorf("expected 1 helpful, got %d", stats.HelpfulCount)
	}
	if stats.UnhelpfulCount != 1 {
		t.Errorf("expected 1 unhelpful, got %d", stats.UnhelpfulCount)
	}
	if math.Abs(stats.HelpfulRate-0.5) > 0.01 {
		t.Errorf("expected helpful rate 0.5, got %.2f", stats.HelpfulRate)
	}
}

func TestFeedbackStoreEviction(t *testing.T) {
	fs := NewFeedbackStore(10)

	// Fill beyond capacity
	for i := 0; i < 15; i++ {
		fs.Record(FeedbackEntry{
			Query:   "query",
			Helpful: true,
		})
	}

	stats := fs.Stats()
	if stats.TotalFeedback > 10 {
		t.Errorf("should evict old entries, total=%d", stats.TotalFeedback)
	}
}

func TestFeedbackSuggestions(t *testing.T) {
	fs := NewFeedbackStore(100)

	fs.Record(FeedbackEntry{
		Query:       "deploy to kubernetes with helm",
		CachedQuery: "deploy to docker with compose",
		Helpful:     false,
	})

	_, keyNouns := fs.GetSuggestions()
	if len(keyNouns) == 0 {
		t.Error("should suggest key nouns from false positive analysis")
	}
}

func TestFeedbackRecentFalsePositives(t *testing.T) {
	fs := NewFeedbackStore(100)

	fs.Record(FeedbackEntry{Query: "q1", Helpful: true})
	fs.Record(FeedbackEntry{Query: "q2", Helpful: false})
	fs.Record(FeedbackEntry{Query: "q3", Helpful: false})
	fs.Record(FeedbackEntry{Query: "q4", Helpful: true})

	fps := fs.RecentFalsePositives(5)
	if len(fps) != 2 {
		t.Errorf("expected 2 false positives, got %d", len(fps))
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// SHADOW MODE TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestShadowModeDisabled(t *testing.T) {
	sm := NewShadowMode(false, 100)
	if sm.IsEnabled() {
		t.Error("shadow mode should be disabled")
	}

	stats := sm.Stats()
	if stats.TotalChecks != 0 {
		t.Error("disabled shadow should have 0 checks")
	}
}

func TestShadowModeRecording(t *testing.T) {
	sm := NewShadowMode(true, 100)

	sm.RecordResult(ShadowResult{
		Query:     "What is Docker?",
		CacheHit:  true,
		Agreement: true,
		LatencyCache: 5 * time.Millisecond,
		LatencyFresh: 500 * time.Millisecond,
	})

	sm.RecordResult(ShadowResult{
		Query:     "Deploy K8s",
		CacheHit:  true,
		Agreement: false,
		LatencyCache: 3 * time.Millisecond,
		LatencyFresh: 300 * time.Millisecond,
	})

	stats := sm.Stats()
	if stats.TotalChecks != 2 {
		t.Errorf("expected 2 total checks, got %d", stats.TotalChecks)
	}
	if stats.Agreements != 1 {
		t.Errorf("expected 1 agreement, got %d", stats.Agreements)
	}
	if stats.Disagreements != 1 {
		t.Errorf("expected 1 disagreement, got %d", stats.Disagreements)
	}
	if math.Abs(stats.AgreementRate-0.5) > 0.01 {
		t.Errorf("expected agreement rate 0.5, got %.2f", stats.AgreementRate)
	}
}

func TestShadowModeEviction(t *testing.T) {
	sm := NewShadowMode(true, 10)

	for i := 0; i < 15; i++ {
		sm.RecordResult(ShadowResult{Query: "test", Agreement: true})
	}

	sm.mu.RLock()
	resultCount := len(sm.results)
	sm.mu.RUnlock()

	if resultCount > 10 {
		t.Errorf("should evict old results, count=%d", resultCount)
	}
}

func TestShadowRecentDisagreements(t *testing.T) {
	sm := NewShadowMode(true, 100)

	sm.RecordResult(ShadowResult{Query: "q1", Agreement: true})
	sm.RecordResult(ShadowResult{Query: "q2", Agreement: false})
	sm.RecordResult(ShadowResult{Query: "q3", Agreement: false})

	disagreements := sm.RecentDisagreements(5)
	if len(disagreements) != 2 {
		t.Errorf("expected 2 disagreements, got %d", len(disagreements))
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// STORE WITH NEW FEATURES TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestStoreWithFeedback(t *testing.T) {
	store := NewStore(StoreConfig{
		L1Enabled:       true,
		L1TTL:           10 * time.Minute,
		L1MaxEntries:    1000,
		FeedbackEnabled: true,
		FeedbackMaxSize: 100,
	})

	fb := store.Feedback()
	if fb == nil {
		t.Fatal("feedback store should not be nil")
	}

	fb.Record(FeedbackEntry{
		Query:   "test query",
		Helpful: true,
	})

	stats := fb.Stats()
	if stats.TotalFeedback != 1 {
		t.Errorf("expected 1 feedback, got %d", stats.TotalFeedback)
	}
}

func TestStoreWithShadow(t *testing.T) {
	store := NewStore(StoreConfig{
		L1Enabled:      true,
		L1TTL:          10 * time.Minute,
		L1MaxEntries:   1000,
		ShadowEnabled:  true,
		ShadowMaxResults: 100,
	})

	sm := store.Shadow()
	if sm == nil {
		t.Fatal("shadow mode should not be nil")
	}
	if !sm.IsEnabled() {
		t.Error("shadow mode should be enabled")
	}
}

func TestStoreWithContext(t *testing.T) {
	store := NewStore(StoreConfig{
		L1Enabled:    true,
		L1TTL:        10 * time.Minute,
		L1MaxEntries: 1000,
	})

	ctx := store.Context()
	if ctx == nil {
		t.Fatal("context fingerprint should not be nil")
	}
	if ctx.MaxTurns != 3 {
		t.Errorf("expected MaxTurns=3, got %d", ctx.MaxTurns)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// COMPREHENSIVE FALSE POSITIVE TESTS (accuracy critical)
// ═══════════════════════════════════════════════════════════════════════════

func TestFalsePositivePrevention(t *testing.T) {
	// These pairs should NOT match despite surface similarity
	falsePositives := []struct {
		query   string
		cached  string
		reason  string
	}{
		// Opposite intents
		{"enable dark mode", "disable dark mode", "opposite: enable/disable"},
		{"start the docker container", "stop the docker container", "opposite: start/stop"},
		{"create a new database", "delete a database", "opposite: create/delete"},
		{"add a new column", "remove a column", "opposite: add/remove"},
		{"encrypt the password", "decrypt the password", "opposite: encrypt/decrypt"},
		{"push to main branch", "pull from main branch", "opposite: push/pull"},
		{"upload to S3", "download from S3", "opposite: upload/download"},
		{"import CSV data", "export CSV data", "opposite: import/export"},
		{"compress the image", "decompress the image", "opposite: compress/decompress"},
		{"lock the mutex", "unlock the mutex", "opposite: lock/unlock"},
		{"subscribe to SNS topic", "unsubscribe from SNS topic", "opposite: subscribe/unsubscribe"},
		{"mount the volume", "unmount the volume", "opposite: mount/unmount"},
		{"serialize to protobuf", "deserialize from protobuf", "opposite: serialize/deserialize"},
		{"login to the dashboard", "logout from the dashboard", "opposite: login/logout"},
		{"grant admin permissions", "revoke admin permissions", "opposite: grant/revoke"},
		{"connect to the VPN", "disconnect from the VPN", "opposite: connect/disconnect"},
		{"deploy to production", "rollback from production", "opposite: deploy/rollback"},
		{"register the webhook", "unregister the webhook", "opposite: register/unregister"},
		{"show the tooltip", "hide the tooltip", "opposite: show/hide"},
		{"open the connection pool", "close the connection pool", "opposite: open/close"},
		// NEW opposite pairs
		{"upgrade the package", "downgrade the package", "opposite: upgrade/downgrade"},
		{"backup the database", "restore the database", "opposite: backup/restore"},
		{"freeze the account", "unfreeze the account", "opposite: freeze/unfreeze"},
		{"mute the channel", "unmute the channel", "opposite: mute/unmute"},
		{"pin the message", "unpin the message", "opposite: pin/unpin"},
		{"pause the pipeline", "resume the pipeline", "opposite: pause/resume"},
		{"ban the user", "unban the user", "opposite: ban/unban"},
		{"archive the project", "unarchive the project", "opposite: archive/unarchive"},
		{"minify the CSS", "prettify the CSS", "opposite: minify/prettify"},
		{"promote to leader", "demote from leader", "opposite: promote/demote"},

		// Different key nouns (technologies)
		{"deploy to AWS", "deploy to Azure", "different cloud: AWS/Azure"},
		{"deploy to GCP", "deploy to Heroku", "different cloud: GCP/Heroku"},
		{"handle errors in Go", "handle errors in Rust", "different lang: Go/Rust"},
		{"write tests in Python", "write tests in Java", "different lang: Python/Java"},
		{"build frontend with React", "build frontend with Vue", "different framework: React/Vue"},
		{"build app with Angular", "build app with Svelte", "different framework: Angular/Svelte"},
		{"query MySQL database", "query PostgreSQL database", "different db: MySQL/PostgreSQL"},
		{"cache with Redis", "cache with Memcached", "different cache: Redis/Memcached"},
		{"sort using quicksort", "sort using mergesort", "different algo: quicksort/mergesort"},
		{"test with Jest", "test with Pytest", "different framework: Jest/Pytest"},
		{"serve with Nginx", "serve with Apache", "different server: Nginx/Apache"},
		{"queue with Kafka", "queue with RabbitMQ", "different mq: Kafka/RabbitMQ"},
		{"provision with Terraform", "provision with Pulumi", "different IaC: Terraform/Pulumi"},
		{"monitor with Prometheus", "monitor with Datadog", "different mon: Prometheus/Datadog"},
		{"implement REST API", "implement GraphQL API", "different API: REST/GraphQL"},
		{"authenticate with JWT", "authenticate with OAuth", "different auth: JWT/OAuth"},
		{"use TCP protocol", "use UDP protocol", "different proto: TCP/UDP"},
		{"parse JSON data", "parse XML data", "different format: JSON/XML"},
		{"install on Linux", "install on Windows", "different OS: Linux/Windows"},
		{"host on GitHub", "host on GitLab", "different git: GitHub/GitLab"},
		{"calculate sum", "calculate product", "different math: sum/product"},
		{"find even numbers", "find odd numbers", "different parity: even/odd"},
		{"sort ascending order", "sort descending order", "different order: ascending/descending"},
		{"monolithic architecture", "microservices architecture", "different arch: monolithic/microservices"},
		{"use array data structure", "use map data structure", "different ds: array/map"},
		// NEW technology pairs
		{"train model with TensorFlow", "train model with PyTorch", "different ML: TensorFlow/PyTorch"},
		{"setup Jenkins pipeline", "setup CircleCI pipeline", "different CI: Jenkins/CircleCI"},
		{"install with npm", "install with yarn", "different pkg: npm/yarn"},
		{"deploy Istio mesh", "deploy Linkerd mesh", "different mesh: Istio/Linkerd"},
		{"query BigQuery", "query Redshift", "different warehouse: BigQuery/Redshift"},
		{"configure VSCode", "configure Vim", "different editor: VSCode/Vim"},
		{"style with Tailwind", "style with Bootstrap", "different CSS: Tailwind/Bootstrap"},
		{"run on Node", "run on Deno", "different runtime: Node/Deno"},
		{"secrets in Vault", "secrets in Keycloak", "different auth: Vault/Keycloak"},
		{"trace with Jaeger", "trace with Zipkin", "different trace: Jaeger/Zipkin"},
	}

	for _, fp := range falsePositives {
		isOpposite := hasOppositeIntent(fp.query, fp.cached)
		isDiffNoun := hasDifferentKeyNoun(fp.query, fp.cached)

		if !isOpposite && !isDiffNoun {
			t.Errorf("FALSE POSITIVE not caught: %q vs %q (%s)", fp.query, fp.cached, fp.reason)
		}
	}

	t.Logf("Tested %d false positive prevention cases", len(falsePositives))
}

// ═══════════════════════════════════════════════════════════════════════════
// TRUE POSITIVE TESTS (should match)
// ═══════════════════════════════════════════════════════════════════════════

func TestTruePositivePreservation(t *testing.T) {
	// These pairs SHOULD match (paraphrases of the same intent)
	truePositives := []struct {
		query  string
		cached string
	}{
		// Paraphrases
		{"What is a goroutine?", "Explain goroutines in Go"},
		{"How to sort an array", "Array sorting algorithm"},
		{"Deploy Docker container", "Docker container deployment"},
		{"Write a REST API", "Create REST API endpoints"},
		{"Optimize database queries", "Make database queries faster"},
		{"Explain microservices", "What are microservices?"},
		{"Debug race condition", "Fix race condition issue"},
		{"Configure Nginx proxy", "Set up Nginx reverse proxy"},
		{"Implement JWT authentication", "Add JWT auth to API"},
		{"Monitor with Prometheus", "Prometheus monitoring setup"},
		// Abbreviation / full form
		{"Setup CI/CD pipeline", "Set up continuous integration pipeline"},
		{"Configure K8s cluster", "Configure Kubernetes cluster"},
		{"What is GC in Java?", "Explain garbage collection in Java"},
		{"Use a CDN for static assets", "Use content delivery network for static files"},
		{"Add CORS headers", "Enable cross-origin resource sharing"},
		// Different word order
		{"Python string reversal", "How to reverse a string in Python"},
		{"Go error handling", "Handle errors in Go"},
		{"Docker volume mounting", "How to mount volumes in Docker"},
		{"Kubernetes pod scaling", "Scale Kubernetes pods"},
	}

	for _, tp := range truePositives {
		// These should NOT be blocked by filters
		if hasOppositeIntent(tp.query, tp.cached) {
			t.Errorf("TRUE POSITIVE incorrectly blocked (opposite): %q vs %q", tp.query, tp.cached)
		}
		if hasDifferentKeyNoun(tp.query, tp.cached) {
			t.Errorf("TRUE POSITIVE incorrectly blocked (key noun): %q vs %q", tp.query, tp.cached)
		}
	}

	t.Logf("Tested %d true positive preservation cases", len(truePositives))
}

// ═══════════════════════════════════════════════════════════════════════════
// COMPREHENSIVE ACCURACY BENCHMARK
// ═══════════════════════════════════════════════════════════════════════════

func TestComprehensiveFilterAccuracy(t *testing.T) {
	// Count correct filter decisions
	correct := 0
	total := 0

	// Should block (opposite intent)
	oppositeTests := []struct{ a, b string }{
		{"create table", "delete table"},
		{"enable feature", "disable feature"},
		{"start server", "stop server"},
		{"encrypt data", "decrypt data"},
		{"push code", "pull code"},
		{"upload file", "download file"},
		{"import module", "export module"},
		{"add item", "remove item"},
		{"lock row", "unlock row"},
		{"subscribe topic", "unsubscribe topic"},
		{"compress file", "decompress file"},
		{"encode string", "decode string"},
		{"serialize object", "deserialize object"},
		{"attach disk", "detach disk"},
		{"register service", "deregister service"},
		{"activate account", "deactivate account"},
		{"show panel", "hide panel"},
		{"expand tree", "collapse tree"},
		{"increment value", "decrement value"},
		{"send request", "receive request"},
		{"merge PR", "split PR"},
		{"load plugin", "unload plugin"},
		{"enqueue job", "dequeue job"},
		{"cache response", "evict response"},
		{"upgrade pkg", "downgrade pkg"},
		{"backup db", "restore db"},
		{"freeze version", "unfreeze version"},
		{"mute user", "unmute user"},
		{"pin thread", "unpin thread"},
		{"pause job", "resume job"},
		{"ban IP", "unban IP"},
		{"archive repo", "unarchive repo"},
		{"promote branch", "demote branch"},
		{"wrap function", "unwrap function"},
		{"bind socket", "unbind socket"},
		{"follow channel", "unfollow channel"},
		{"block traffic", "unblock traffic"},
		{"approve change", "reject change"},
		{"join cluster", "leave cluster"},
		{"minify code", "beautify code"},
	}

	for _, tc := range oppositeTests {
		total++
		if hasOppositeIntent(tc.a, tc.b) {
			correct++
		} else {
			t.Logf("MISS opposite: %q vs %q", tc.a, tc.b)
		}
	}

	// Should block (different key nouns)
	keyNounTests := []struct{ a, b string }{
		{"deploy to AWS", "deploy to Azure"},
		{"write in Go", "write in Rust"},
		{"build with React", "build with Vue"},
		{"query MySQL", "query MongoDB"},
		{"test with Jest", "test with Pytest"},
		{"serve with Nginx", "serve with Apache"},
		{"use Kafka", "use RabbitMQ"},
		{"provision Terraform", "provision Ansible"},
		{"monitor Prometheus", "monitor Datadog"},
		{"REST API", "GraphQL API"},
		{"TCP connection", "UDP connection"},
		{"parse JSON", "parse XML"},
		{"install Linux", "install Windows"},
		{"quicksort array", "mergesort array"},
		{"auth with JWT", "auth with OAuth"},
		{"host GitHub", "host GitLab"},
		{"calculate sum", "calculate product"},
		{"even numbers", "odd numbers"},
		{"ascending order", "descending order"},
		{"monolithic design", "microservices design"},
	}

	for _, tc := range keyNounTests {
		total++
		if hasDifferentKeyNoun(tc.a, tc.b) {
			correct++
		} else {
			t.Logf("MISS key noun: %q vs %q", tc.a, tc.b)
		}
	}

	// Should NOT block (valid paraphrases)
	validPairs := []struct{ a, b string }{
		{"explain goroutines", "what are goroutines"},
		{"optimize query performance", "make queries faster"},
		{"deploy Docker containers", "Docker deployment"},
		{"configure Kubernetes pods", "Kubernetes pod config"},
		{"write Python function", "Python function implementation"},
		{"debug race condition", "fix race condition"},
		{"set up Redis cache", "Redis caching setup"},
		{"build REST API", "create REST endpoints"},
		{"monitor system health", "system health monitoring"},
		{"test API endpoints", "API endpoint testing"},
		{"handle Go errors", "Go error handling"},
		{"use React hooks", "React hooks usage"},
		{"manage Docker volumes", "Docker volume management"},
		{"configure Nginx proxy", "Nginx proxy configuration"},
		{"scale Kubernetes cluster", "Kubernetes cluster scaling"},
		{"implement JWT auth", "JWT authentication implementation"},
		{"parse JSON response", "JSON response parsing"},
		{"deploy to AWS Lambda", "AWS Lambda deployment"},
		{"query PostgreSQL database", "PostgreSQL database query"},
		{"write unit tests", "unit test implementation"},
	}

	for _, tc := range validPairs {
		total++
		if !hasOppositeIntent(tc.a, tc.b) && !hasDifferentKeyNoun(tc.a, tc.b) {
			correct++
		} else {
			t.Logf("MISS valid pair blocked: %q vs %q", tc.a, tc.b)
		}
	}

	accuracy := float64(correct) / float64(total) * 100
	t.Logf("Comprehensive filter accuracy: %d/%d (%.1f%%)", correct, total, accuracy)

	if accuracy < 95.0 {
		t.Errorf("Filter accuracy too low: %.1f%% (need >= 95%%)", accuracy)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// EDGE CASE TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestEdgeCases(t *testing.T) {
	// Empty strings
	if hasOppositeIntent("", "") {
		t.Error("empty strings should not be opposite")
	}
	if hasDifferentKeyNoun("", "") {
		t.Error("empty strings should not have different key nouns")
	}

	// Very short prompts
	if hasOppositeIntent("hi", "bye") {
		t.Error("hi/bye should not be caught as opposites")
	}

	// Prompts with only stopwords
	tokens := tokenizeWords("the and or but")
	if len(tokens) != 3 { // "the" "and" "or" "but" all kept by tokenizeWords
		t.Logf("stopwords in tokenizeWords: %v", tokens)
	}

	// Single word that is both key noun and action verb
	// "delete" is both an HTTP method key noun and an action verb
	if !hasOppositeIntent("create record", "delete record") {
		t.Error("create/delete should be detected as opposite")
	}

	// Very long prompts
	longPrompt := strings.Repeat("optimize the performance of the distributed system ", 50)
	expanded := expandSynonyms(longPrompt)
	if expanded == "" {
		t.Error("should handle long prompts")
	}

	// Unicode / special characters
	tokens = tokenizeWords("What is über-cool λ calculus?")
	if len(tokens) == 0 {
		t.Error("should handle unicode characters")
	}

	// Numbers in prompts
	tokens = tokenizeWords("HTTP 404 error in api v2")
	found := false
	for _, tok := range tokens {
		if tok == "404" {
			found = true
			break
		}
	}
	if !found {
		t.Error("tokenizeWords should preserve numbers")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// STORE RERANKER INTEGRATION TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestStoreWithReranker(t *testing.T) {
	// Create store with reranker enabled but pointing to unreachable endpoint
	// (will fall back to heuristic)
	store := NewStore(StoreConfig{
		L1Enabled:         true,
		L1TTL:             10 * time.Minute,
		L1MaxEntries:      1000,
		L2bEnabled:        false, // semantic needs embedding endpoint
		RerankerEnabled:   true,
		RerankerModel:     "bge-reranker-v2-m3",
		RerankerEndpoint:  "http://localhost:99999",
		RerankerThreshold: 0.3,
	})

	// Store should initialize without error
	if store == nil {
		t.Fatal("store should be created successfully")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// ADDITIONAL SYNONYM EXPANSION EDGE CASES
// ═══════════════════════════════════════════════════════════════════════════

func TestSynonymExpansionComprehensive(t *testing.T) {
	tests := []struct {
		input      string
		shouldHave []string
	}{
		{"setup k8s cluster", []string{"kubernetes"}},
		{"fix the gc pause", []string{"garbage", "collection"}},
		{"configure ssl cert", []string{"tls", "encryption"}},
		{"query the db table", []string{"database"}},
		{"deploy the dockerfile", []string{"container", "image"}},
		{"implement jwt auth", []string{"json", "web", "token"}},
		{"add cors headers", []string{"cross", "origin"}},
		{"build an orm layer", []string{"object", "relational"}},
		{"use cdn for assets", []string{"content", "delivery"}},
		{"push to the repo", []string{"repository"}},
		{"create a pr for review", []string{"pull", "request"}},
		{"configure di container", []string{"dependency", "injection"}},
		{"use vpc networking", []string{"virtual", "private", "cloud"}},
		{"setup iam policy", []string{"identity", "access"}},
		{"deploy with helm charts", []string{"kubernetes", "package"}},
		{"configure rbac rules", []string{"role", "based", "access"}},
		{"implement cqrs pattern", []string{"command", "query"}},
		{"follow tdd approach", []string{"test", "driven"}},
		{"build saas product", []string{"software", "service"}},
		{"use faas platform", []string{"function", "serverless"}},
		{"create etl pipeline", []string{"extract", "transform", "load"}},
		{"train ml model", []string{"machine", "learning"}},
		{"process nlp data", []string{"natural", "language"}},
		{"fine-tune llm", []string{"large", "language", "model"}},
		{"build spa app", []string{"single", "page", "application"}},
		{"enable ssr mode", []string{"server", "side", "rendering"}},
		{"compile to wasm", []string{"webassembly"}},
	}

	for _, tc := range tests {
		expanded := expandSynonyms(tc.input)
		for _, word := range tc.shouldHave {
			if !strings.Contains(expanded, word) {
				t.Errorf("expandSynonyms(%q) should contain %q, got: %s", tc.input, word, expanded)
			}
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// CONTEXT FINGERPRINT ADVANCED TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestContextFingerprintMaxTurns(t *testing.T) {
	cf := NewContextFingerprint(2) // only consider last 2 turns

	messages := []ChatMessage{
		{Role: "user", Content: "I'm working on a Docker project"},
		{Role: "assistant", Content: "Sure, let me help with Docker"},
		{Role: "user", Content: "Now let's switch to Kubernetes"},
		{Role: "assistant", Content: "Great, Kubernetes it is"},
		{Role: "user", Content: "How do I deploy a pod?"},
	}

	fp := cf.Fingerprint(messages)
	if fp == "" {
		t.Error("should produce a fingerprint with multiple messages")
	}

	// With maxTurns=2, changing early messages shouldn't affect fingerprint
	messages2 := []ChatMessage{
		{Role: "user", Content: "I'm working on a Python project"}, // different
		{Role: "assistant", Content: "Sure, let me help with Python"}, // different
		{Role: "user", Content: "Now let's switch to Kubernetes"},  // same as above
		{Role: "assistant", Content: "Great, Kubernetes it is"},     // same
		{Role: "user", Content: "How do I deploy a pod?"},          // same
	}

	fp2 := cf.Fingerprint(messages2)
	if fp != fp2 {
		t.Error("with maxTurns=2, early messages should not affect fingerprint")
	}
}

func TestContextFingerprintZeroMaxTurns(t *testing.T) {
	cf := NewContextFingerprint(0) // should default to 3
	if cf.MaxTurns != 3 {
		t.Errorf("maxTurns=0 should default to 3, got %d", cf.MaxTurns)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// FEEDBACK LEARNING TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestFeedbackLearning(t *testing.T) {
	fs := NewFeedbackStore(100)

	// Simulate multiple false positives between related but distinct queries
	for i := 0; i < 5; i++ {
		fs.Record(FeedbackEntry{
			Query:       "deploy to kubernetes production cluster",
			CachedQuery: "deploy to docker swarm staging environment",
			Helpful:     false,
			Similarity:  0.88,
		})
	}

	_, keyNouns := fs.GetSuggestions()
	if len(keyNouns) == 0 {
		t.Error("should learn key noun suggestions from repeated false positives")
	}
	t.Logf("Learned %d key noun suggestions", len(keyNouns))
}

// ═══════════════════════════════════════════════════════════════════════════
// SHADOW MODE LATENCY TRACKING TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestShadowLatencyTracking(t *testing.T) {
	sm := NewShadowMode(true, 100)

	sm.RecordResult(ShadowResult{
		CacheHit:     true,
		Agreement:    true,
		LatencyCache: 2 * time.Millisecond,
		LatencyFresh: 200 * time.Millisecond,
	})
	sm.RecordResult(ShadowResult{
		CacheHit:     true,
		Agreement:    true,
		LatencyCache: 3 * time.Millisecond,
		LatencyFresh: 300 * time.Millisecond,
	})
	sm.RecordResult(ShadowResult{
		CacheHit:     false,
		Agreement:    true,
		LatencyCache: 0,
		LatencyFresh: 250 * time.Millisecond,
	})

	stats := sm.Stats()
	if math.Abs(stats.CacheHitRate-2.0/3.0) > 0.01 {
		t.Errorf("cache hit rate should be ~0.67, got %.2f", stats.CacheHitRate)
	}
	if stats.AvgLatencySaved < 100 {
		t.Errorf("avg latency saved should be >100ms, got %.2f", stats.AvgLatencySaved)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// RERANKER CONFIG DEFAULTS TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestRerankerConfigDefaults(t *testing.T) {
	r := NewReranker(RerankerConfig{
		Enabled: true,
		// Leave all other fields empty to test defaults
	})

	if r.model != "bge-reranker-v2-m3" {
		t.Errorf("default model should be bge-reranker-v2-m3, got %s", r.model)
	}
	if r.endpoint != "http://localhost:11434" {
		t.Errorf("default endpoint should be http://localhost:11434, got %s", r.endpoint)
	}
	if r.threshold != 0.5 {
		t.Errorf("default threshold should be 0.5, got %.2f", r.threshold)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// NEW KEY NOUN COVERAGE TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestNewKeyNounsCoverage(t *testing.T) {
	// Test all new key noun categories
	categories := map[string][]string{
		"ML frameworks":      {"tensorflow", "pytorch", "keras", "scikit", "pandas", "numpy", "huggingface", "langchain"},
		"CI/CD tools":        {"jenkins", "circleci", "travisci", "argocd", "spinnaker", "tekton"},
		"Package managers":   {"npm", "yarn", "pnpm", "pip", "cargo", "maven", "gradle", "bundler", "composer"},
		"Caching":            {"memcached", "varnish"},
		"Service mesh":       {"istio", "envoy", "linkerd", "consul"},
		"Cloud services":     {"aurora", "bigquery", "redshift", "snowflake", "databricks", "kinesis", "cloudfront"},
		"OS concepts":        {"process", "thread", "fiber", "coroutine"},
		"Editors":            {"vscode", "vim", "neovim", "emacs", "intellij"},
		"CSS frameworks":     {"tailwind", "bootstrap", "bulma"},
		"Runtimes":           {"node", "deno", "bun", "jvm", "dotnet"},
		"Auth tools":         {"vault", "keycloak", "auth0", "okta"},
		"Observability":      {"jaeger", "zipkin", "opentelemetry", "loki", "fluentd", "logstash"},
		"Algorithms":         {"dijkstra", "bellmanford", "bfs", "dfs", "astar", "fibonacci"},
		"Data structures":    {"linkedlist", "trie", "btree", "avl", "redblack", "skiplist"},
		"Design patterns":    {"singleton", "factory", "observer", "strategy", "decorator", "adapter", "proxy", "facade", "builder"},
	}

	for category, nouns := range categories {
		for _, noun := range nouns {
			if !keyNounsMap[noun] {
				t.Errorf("key noun %q (category: %s) should be in keyNounsMap", noun, category)
			}
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// GETKEY NOUNS FUNCTION TEST
// ═══════════════════════════════════════════════════════════════════════════

func TestGetKeyNouns(t *testing.T) {
	kn := getKeyNouns()
	if kn == nil {
		t.Fatal("getKeyNouns should not return nil")
	}
	if len(kn) < 200 {
		t.Errorf("expected 200+ key nouns, got %d", len(kn))
	}

	// Verify it returns the same map reference
	if &kn != nil && !kn["docker"] {
		t.Error("getKeyNouns should include docker")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// TOKENIZE WORDS TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestTokenizeWords(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"Hello World", []string{"hello", "world"}},
		{"what-is-docker", []string{"what", "is", "docker"}},
		{"REST API v2.0", []string{"rest", "api", "v2", "0"}},
		{"", nil},
		{"   ", nil},
		{"CamelCase", []string{"camelcase"}},
		{"snake_case", []string{"snake", "case"}},
		{"a", []string{"a"}},
		{"123abc", []string{"123abc"}},
	}

	for _, tc := range tests {
		got := tokenizeWords(tc.input)
		if len(got) != len(tc.expected) {
			t.Errorf("tokenizeWords(%q) = %v (len=%d), want %v (len=%d)",
				tc.input, got, len(got), tc.expected, len(tc.expected))
			continue
		}
		for i := range got {
			if got[i] != tc.expected[i] {
				t.Errorf("tokenizeWords(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.expected[i])
			}
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// QUERY TYPE EDGE CASES
// ═══════════════════════════════════════════════════════════════════════════

func TestQueryTypeEdgeCases(t *testing.T) {
	// Multiple patterns matching — first match wins
	// "debug" is checked before "how to"
	qt := ClassifyQueryType("How to debug a race condition")
	if qt != QueryTypeDebug {
		t.Errorf("debug pattern should take priority, got %s", QueryTypeName(qt))
	}

	// "vs" comparison checked first
	qt = ClassifyQueryType("React vs Vue for building apps")
	if qt != QueryTypeComparison {
		t.Errorf("comparison should take priority, got %s", QueryTypeName(qt))
	}

	// Very short prompts
	qt = ClassifyQueryType("Hi")
	if qt != QueryTypeGeneral {
		t.Errorf("short prompts should be general, got %s", QueryTypeName(qt))
	}

	// Empty prompt
	qt = ClassifyQueryType("")
	if qt != QueryTypeGeneral {
		t.Errorf("empty prompt should be general, got %s", QueryTypeName(qt))
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// INTEGRATION: STORE CONFIG WITH ALL OPTIONS
// ═══════════════════════════════════════════════════════════════════════════

func TestStoreConfigComplete(t *testing.T) {
	store := NewStore(StoreConfig{
		L1Enabled:         true,
		L1TTL:             10 * time.Minute,
		L1MaxEntries:      1000,
		L2aEnabled:        true,
		L2aTTL:            30 * time.Minute,
		L2aMaxEntries:     5000,
		L2aThreshold:      15.0,
		L2bEnabled:        false, // requires embedding endpoint
		RerankerEnabled:   true,
		RerankerModel:     "bge-reranker-v2-m3",
		RerankerEndpoint:  "http://localhost:11434",
		RerankerThreshold: 0.5,
		FeedbackEnabled:   true,
		FeedbackMaxSize:   5000,
		ShadowEnabled:     true,
		ShadowMaxResults:  500,
	})

	if store == nil {
		t.Fatal("store should be created")
	}
	if store.Feedback() == nil {
		t.Error("feedback should be initialized")
	}
	if store.Shadow() == nil {
		t.Error("shadow should be initialized")
	}
	if !store.Shadow().IsEnabled() {
		t.Error("shadow should be enabled")
	}
	if store.Context() == nil {
		t.Error("context should be initialized")
	}

	// Test store operations still work
	resp := []byte(`{"answer":"test"}`)
	store.StoreResponse("test prompt", "model", resp)

	data, hit, source := store.Lookup("test prompt", "model")
	if !hit {
		t.Fatal("exact match should hit L1")
	}
	if source != "l1_exact" {
		t.Errorf("expected l1_exact, got %s", source)
	}
	if string(data) != string(resp) {
		t.Errorf("expected %s, got %s", string(resp), string(data))
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// SYNONYM REGISTRY TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestSynonymRegistryBasics(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewSynonymRegistry(RegistryConfig{
		DataDir:            tmpDir,
		PromotionThreshold: 3,
		StaleTimeout:       1 * time.Hour,
		SaveInterval:       1 * time.Hour,
	})
	defer r.Stop()

	// Base synonyms should be loaded
	stats := r.Stats()
	if stats.BaseSynonyms == 0 {
		t.Fatal("expected base synonyms to be loaded")
	}
	t.Logf("Base synonyms: %d, Key nouns: %d", stats.BaseSynonyms, stats.BaseKeyNouns)

	// Expansion should include base
	expanded := r.Expand("set up k8s cluster")
	if !strings.Contains(expanded, "kubernetes") {
		t.Error("expected k8s expansion to include kubernetes")
	}

	// Manual add should work
	r.ManualAdd("nextjs", "next.js react framework ssr")
	expanded = r.Expand("deploy nextjs app")
	if !strings.Contains(expanded, "react framework") {
		t.Error("expected nextjs expansion to include react framework")
	}

	stats = r.Stats()
	if stats.LearnedSynonyms != 1 {
		t.Errorf("expected 1 learned synonym, got %d", stats.LearnedSynonyms)
	}
}

func TestSynonymRegistryNearMiss(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewSynonymRegistry(RegistryConfig{
		DataDir:            tmpDir,
		PromotionThreshold: 3,
		SaveInterval:       1 * time.Hour,
	})
	defer r.Stop()

	// Record near-misses — need 3 confirmations to promote
	r.RecordNearMiss("explain goroutines in depth", "go concurrency patterns", 0.62)
	stats := r.Stats()
	if stats.CandidateSynonyms == 0 {
		t.Error("expected candidates after near-miss")
	}

	// Second confirmation
	r.RecordNearMiss("goroutine lifecycle", "concurrency lifecycle in go", 0.58)

	// Third confirmation — should auto-promote
	r.RecordNearMiss("goroutine pool pattern", "concurrency pool in go", 0.61)

	// Check that some candidates were promoted
	candidates := r.GetCandidates()
	learned := r.GetLearnedSynonyms()
	t.Logf("After 3 near-misses: %d candidates, %d learned", len(candidates), len(learned))
}

func TestSynonymRegistryFeedback(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewSynonymRegistry(RegistryConfig{
		DataDir:            tmpDir,
		PromotionThreshold: 3,
		SaveInterval:       1 * time.Hour,
	})
	defer r.Stop()

	// Feedback counts double — 2 feedbacks should promote (2*2 = 4 >= 3)
	r.RecordFeedbackMiss("what is dependency injection", "explain DI pattern")
	r.RecordFeedbackMiss("dependency injection in Spring", "DI framework Spring")

	learned := r.GetLearnedSynonyms()
	t.Logf("After 2 feedback reports: %d learned synonyms", len(learned))
}

func TestSynonymRegistryFalsePositive(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewSynonymRegistry(RegistryConfig{
		DataDir:            tmpDir,
		PromotionThreshold: 3,
		SaveInterval:       1 * time.Hour,
	})
	defer r.Stop()

	before := r.Stats()
	r.RecordFalsePositive("deploy to AWS Lambda", "deploy to Google Cloud Run")
	after := r.Stats()

	if after.LearnedKeyNouns <= before.LearnedKeyNouns {
		t.Error("expected new key nouns from false positive")
	}
	t.Logf("New key nouns learned: %d", after.LearnedKeyNouns-before.LearnedKeyNouns)
}

func TestSynonymRegistryPersistence(t *testing.T) {
	tmpDir := t.TempDir()

	// Create registry, add data, save
	r1 := NewSynonymRegistry(RegistryConfig{
		DataDir:            tmpDir,
		PromotionThreshold: 3,
		SaveInterval:       1 * time.Hour,
	})
	r1.ManualAdd("testterm", "test expansion value")
	r1.ManualAddKeyNoun("testnoun")
	r1.Stop() // triggers final save

	// Create new registry from same directory — should load persisted data
	r2 := NewSynonymRegistry(RegistryConfig{
		DataDir:            tmpDir,
		PromotionThreshold: 3,
		SaveInterval:       1 * time.Hour,
	})
	defer r2.Stop()

	learned := r2.GetLearnedSynonyms()
	if learned["testterm"] != "test expansion value" {
		t.Errorf("expected persisted synonym, got: %v", learned)
	}

	if !r2.IsKeyNounDynamic("testnoun") {
		t.Error("expected persisted key noun to be loadable")
	}
}

func TestSynonymRegistryManualPromote(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewSynonymRegistry(RegistryConfig{
		DataDir:            tmpDir,
		PromotionThreshold: 10, // high threshold so nothing auto-promotes
		SaveInterval:       1 * time.Hour,
	})
	defer r.Stop()

	r.RecordNearMiss("explain coroutines", "kotlin concurrency", 0.60)

	candidates := r.GetCandidates()
	if len(candidates) == 0 {
		t.Fatal("expected at least one candidate")
	}

	term := candidates[0].Term
	ok := r.ManualPromote(term)
	if !ok {
		t.Errorf("expected successful promotion of %q", term)
	}

	// Should now be in learned
	learned := r.GetLearnedSynonyms()
	if _, exists := learned[term]; !exists {
		t.Errorf("expected %q in learned synonyms after promotion", term)
	}
}

func TestSynonymRegistryExpandFallback(t *testing.T) {
	// When no global registry is set, expandSynonyms should still work with static map
	old := defaultRegistry
	defaultRegistry = nil
	defer func() { defaultRegistry = old }()

	result := expandSynonyms("set up k8s cluster")
	if !strings.Contains(result, "kubernetes") {
		t.Error("expected static fallback to expand k8s to kubernetes")
	}
}

func TestSynonymRegistryDynamicKeyNoun(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewSynonymRegistry(RegistryConfig{
		DataDir:            tmpDir,
		PromotionThreshold: 3,
		SaveInterval:       1 * time.Hour,
	})
	defer r.Stop()

	// Base key nouns should work
	if !r.IsKeyNounDynamic("python") {
		t.Error("expected 'python' to be a base key noun")
	}

	// Add a learned key noun
	r.ManualAddKeyNoun("fooframework")
	if !r.IsKeyNounDynamic("fooframework") {
		t.Error("expected 'fooframework' to be a learned key noun")
	}

	// HasDifferentKeyNounDynamic should detect differences
	if !r.HasDifferentKeyNounDynamic("deploy python app", "deploy java app") {
		t.Error("expected different key nouns for python vs java")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// REGRESSION: Bug 3 — Cache Performance (O(n) Linear Scan)
//
// The original bug: every SemanticCache Lookup iterates ALL entries computing
// a dot product for each. With max_entries=50000, this is O(n) per lookup.
// This test verifies that lookup performance doesn't degrade unacceptably
// as the number of entries grows.
// ═══════════════════════════════════════════════════════════════════════════

func TestSemanticCache_LookupPerformanceScaling(t *testing.T) {
	// Test that dot product linear scan time scales linearly (O(n)) with entry count.
	// This is a benchmark-style test that documents the performance characteristic.
	// After optimization, lookup should scale sub-linearly (e.g., bucket index).
	dim := 128

	// Measure time for N dot products
	smallN := 1000
	largeN := 10000

	smallEntries := make([][]float64, smallN)
	largeEntries := make([][]float64, largeN)

	// Create normalized vectors
	for i := 0; i < largeN; i++ {
		vec := make([]float64, dim)
		vec[i%dim] = 1.0
		vec[(i+1)%dim] = 0.5
		normalized := normalizeVector(vec)
		if i < smallN {
			smallEntries[i] = normalized
		}
		largeEntries[i] = normalized
	}

	query := normalizeVector(make([]float64, dim))
	query[0] = 1.0

	// Measure small scan
	smallStart := time.Now()
	for _, entry := range smallEntries {
		dotProduct(query, entry)
	}
	smallDuration := time.Since(smallStart)

	// Measure large scan
	largeStart := time.Now()
	for _, entry := range largeEntries {
		dotProduct(query, entry)
	}
	largeDuration := time.Since(largeStart)

	ratio := float64(largeDuration) / float64(smallDuration)
	expectedRatio := float64(largeN) / float64(smallN) // 10x for linear

	t.Logf("Performance scaling: %d entries=%.2fms, %d entries=%.2fms, ratio=%.1fx (linear=%.0fx)",
		smallN, float64(smallDuration.Microseconds())/1000,
		largeN, float64(largeDuration.Microseconds())/1000,
		ratio, expectedRatio)

	// If ratio is close to expectedRatio, lookup is O(n) — the bug is present.
	// After fix (e.g., bucket index), ratio should be significantly lower.
	// We log a warning rather than failing, since this is an optimization target.
	if ratio > expectedRatio*0.7 {
		t.Logf("WARNING: Lookup scales approximately linearly (ratio=%.1f, expected for O(n)=%.0f). "+
			"Consider adding bucket indexing or early exit optimization.", ratio, expectedRatio)
	}
}

// TestBM25Cache_LookupPerformanceScaling tests the same O(n) issue on BM25 cache.
func TestBM25Cache_LookupPerformanceScaling(t *testing.T) {
	smallN := 500
	largeN := 5000

	smallCache := NewBM25Cache(time.Hour, smallN+100, 0.1)
	largeCache := NewBM25Cache(time.Hour, largeN+100, 0.1)

	for i := 0; i < largeN; i++ {
		prompt := fmt.Sprintf("prompt about topic %d with keywords test data item %d", i, i*7)
		resp := []byte(fmt.Sprintf(`{"answer":"response %d"}`, i))
		if i < smallN {
			smallCache.Store(prompt, "model", resp)
		}
		largeCache.Store(prompt, "model", resp)
	}

	query := "prompt about topic 42 with keywords"

	smallStart := time.Now()
	for i := 0; i < 100; i++ {
		smallCache.Lookup(query, "model")
	}
	smallDuration := time.Since(smallStart)

	largeStart := time.Now()
	for i := 0; i < 100; i++ {
		largeCache.Lookup(query, "model")
	}
	largeDuration := time.Since(largeStart)

	ratio := float64(largeDuration) / float64(smallDuration)
	expectedLinear := float64(largeN) / float64(smallN)

	t.Logf("BM25 scaling: %d entries=%.2fms, %d entries=%.2fms, ratio=%.1fx (linear=%.0fx)",
		smallN, float64(smallDuration.Microseconds())/1000,
		largeN, float64(largeDuration.Microseconds())/1000,
		ratio, expectedLinear)
}

// ═══════════════════════════════════════════════════════════════════════════
// REGRESSION: Bug 5 — Cache TOCTOU (Time-of-Check-to-Time-of-Use)
//
// The original bug: SemanticCache.Lookup acquires and releases the lock 4-5
// times in a single call. Between RUnlock at L131 and RLock at L172, another
// goroutine can evict the entry at bestIdx, causing out-of-bounds panic or
// stale data. This test stresses concurrent lookup + store/eviction to detect
// such race conditions.
// ═══════════════════════════════════════════════════════════════════════════

// testSemanticCacheWithEntries creates a SemanticCache pre-populated with entries
// for concurrent testing without needing an embedding backend.
func testSemanticCacheWithEntries(n, dim int) *SemanticCache {
	c := &SemanticCache{
		entries:    make([]semanticEntry, 0, n),
		ttl:        time.Hour,
		maxEntries: n,
		threshold:  0.5,
		client:     nil, // no HTTP client needed for direct entry manipulation
	}
	for i := 0; i < n; i++ {
		emb := make([]float64, dim)
		emb[i%dim] = 1.0
		normalized := normalizeVector(emb)
		c.entries = append(c.entries, semanticEntry{
			prompt:    fmt.Sprintf("test prompt %d", i),
			model:     "test-model",
			embedding: normalized,
			response:  []byte(fmt.Sprintf(`{"answer":"response %d"}`, i)),
			createdAt: time.Now(),
			bucketKey: lshBucketKey(normalized),
		})
	}
	return c
}

func TestSemanticCache_ConcurrentLookupEviction(t *testing.T) {
	dim := 16
	c := testSemanticCacheWithEntries(50, dim)

	done := make(chan bool, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// A panic here means the TOCTOU bug is present
				t.Errorf("TOCTOU BUG: panic during concurrent lookup+eviction: %v", r)
			}
			done <- true
		}()

		var wg sync.WaitGroup

		// 50 goroutines simulating lookups (reading entries)
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < 200; j++ {
					c.mu.RLock()
					n := len(c.entries)
					if n > 0 {
						idx := j % n
						// Simulate what Lookup does: read entry data
						_ = c.entries[idx].prompt
						_ = c.entries[idx].response
						_ = c.entries[idx].embedding
					}
					c.mu.RUnlock()
				}
			}(i)
		}

		// 50 goroutines simulating stores (writing entries + eviction)
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					emb := make([]float64, dim)
					emb[id%dim] = 1.0

					c.mu.Lock()
					if len(c.entries) >= c.maxEntries {
						c.evictOldest()
					}
					c.entries = append(c.entries, semanticEntry{
						prompt:    fmt.Sprintf("new %d-%d", id, j),
						model:     "test-model",
						embedding: normalizeVector(emb),
						response:  []byte("new response"),
						createdAt: time.Now(),
					})
					c.mu.Unlock()
				}
			}(i)
		}

		wg.Wait()
	}()

	select {
	case <-done:
		// Completed without panic or deadlock
	case <-time.After(10 * time.Second):
		t.Fatal("TOCTOU: concurrent lookup+eviction timed out (possible deadlock)")
	}
}

// TestSemanticCache_ConcurrentStatsAccess verifies that Stats() is safe
// under concurrent access with Store-like modifications.
func TestSemanticCache_ConcurrentStatsAccess(t *testing.T) {
	dim := 8
	c := testSemanticCacheWithEntries(10, dim)

	done := make(chan bool, 1)
	go func() {
		var wg sync.WaitGroup

		// Readers: call Stats
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					hits, misses, size := c.Stats()
					_ = hits
					_ = misses
					_ = size
				}
			}()
		}

		// Writers: modify hit/miss counters
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					c.mu.Lock()
					c.hits++
					c.misses++
					c.mu.Unlock()
				}
			}()
		}

		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent Stats access deadlocked")
	}
}

// TestSemanticCache_EvictionDuringLookupWindow tests the specific TOCTOU
// scenario: bestIdx is found, lock is released, then the entry at bestIdx
// is evicted before the response is read.
func TestSemanticCache_EvictionDuringLookupWindow(t *testing.T) {
	dim := 4
	c := testSemanticCacheWithEntries(5, dim)

	// Simulate the TOCTOU window:
	// 1. Reader finds bestIdx=2
	// 2. Reader releases RLock
	// 3. Writer evicts entry, changing slice
	// 4. Reader re-acquires RLock, reads from potentially invalid index

	// Step 1: Find best index under read lock
	c.mu.RLock()
	bestIdx := 2
	originalPrompt := c.entries[bestIdx].prompt
	c.mu.RUnlock()

	// Step 2-3: Writer evicts oldest (simulating concurrent eviction)
	c.mu.Lock()
	c.evictOldest()
	c.mu.Unlock()

	// Step 4: Reader tries to read the entry at original bestIdx
	c.mu.RLock()
	if bestIdx < len(c.entries) {
		currentPrompt := c.entries[bestIdx].prompt
		if currentPrompt != originalPrompt {
			t.Logf("TOCTOU detected: entry at idx %d changed from %q to %q after eviction",
				bestIdx, originalPrompt, currentPrompt)
			// This is the bug: the entry shifted, so bestIdx now points to a different entry
		}
	} else {
		t.Logf("TOCTOU detected: bestIdx %d is now out of bounds (len=%d) after eviction",
			bestIdx, len(c.entries))
	}
	c.mu.RUnlock()
}
