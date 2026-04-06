package cache

import (
	"strings"
)

// expandSynonyms appends expanded terms to the text for better embedding matching.
// This handles abbreviations, jargon, and domain-specific terms.
// Uses the dynamic registry if available, falling back to a static map.
func expandSynonyms(text string, registry *SynonymRegistry) string {
	// Use dynamic registry if available
	if registry != nil {
		return registry.Expand(text)
	}

	// Fallback: static expansion (original behavior)
	lower := strings.ToLower(text)
	expanded := lower

	synonyms := getBaseSynonyms()

	for k, v := range synonyms {
		if strings.Contains(lower, k) {
			expanded = expanded + " " + v
		}
	}

	return expanded
}

// hasOppositeIntent detects if two prompts have opposite actions/intents.
// Returns true if the prompts contain antonym verb pairs.
func hasOppositeIntent(text1, text2 string) bool {
	opposites := map[string][]string{
		"create":     {"kill", "delete", "remove", "destroy", "drop"},
		"add":        {"remove", "delete", "subtract", "drop"},
		"enable":     {"disable", "turn off"},
		"start":      {"stop", "halt", "kill", "terminate", "shutdown"},
		"install":    {"uninstall", "remove"},
		"encrypt":    {"decrypt"},
		"read":       {"write"},
		"deploy":     {"rollback", "undeploy", "revert"},
		"ascending":  {"descending"},
		"login":      {"logout"},
		"connect":    {"disconnect"},
		"open":       {"close", "shut"},
		"increase":   {"decrease", "reduce"},
		"allow":      {"deny", "block", "reject", "forbid"},
		"accept":     {"reject", "decline"},
		"push":       {"pull", "pop"},
		"upload":     {"download"},
		"import":     {"export"},
		"insert":     {"delete", "remove"},
		"grant":      {"revoke"},
		"lock":       {"unlock"},
		"mount":      {"unmount"},
		"subscribe":  {"unsubscribe"},
		"compress":   {"decompress", "extract"},
		"encode":     {"decode"},
		"serialize":  {"deserialize"},
		"marshal":    {"unmarshal"},
		"attach":     {"detach"},
		"register":   {"unregister", "deregister"},
		"activate":   {"deactivate"},
		"show":       {"hide"},
		"expand":     {"collapse", "shrink"},
		"increment":  {"decrement"},
		"send":       {"receive"},
		"merge":      {"split"},
		"sum":        {"product"},
		"even":       {"odd"},
		"load":       {"unload"},
		"raise":      {"lower"},
		"before":     {"after"},
		"above":      {"below"},
		"horizontal": {"vertical"},
		"input":      {"output"},
		"ingress":    {"egress"},
		"inbound":    {"outbound"},
		"public":     {"private"},
		"internal":   {"external"},
		"head":       {"tail"},
		"first":      {"last"},
		"begin":      {"end"},
		"enqueue":    {"dequeue"},
		"cache":      {"evict"},
		// New opposite pairs
		"scale up":  {"scale down"},
		"whitelist": {"blacklist", "blocklist"},
		"approve":   {"reject", "deny"},
		"join":      {"leave", "part"},
		"bind":      {"unbind"},
		"wrap":      {"unwrap"},
		"obfuscate": {"deobfuscate", "clarify"},
		"minify":    {"prettify", "beautify", "format"},
		"promote":   {"demote"},
		"upgrade":   {"downgrade"},
		"backup":    {"restore"},
		"freeze":    {"unfreeze", "thaw"},
		"mute":      {"unmute"},
		"pin":       {"unpin"},
		"follow":    {"unfollow"},
		"archive":   {"unarchive"},
		"pause":     {"resume", "unpause"},
		"ban":       {"unban"},
		"block":     {"unblock"},
		"sync":      {"async"},
		"prefix":    {"suffix", "postfix"},
		"prepend":   {"append"},
	}

	w1 := tokenizeWords(text1)
	w2 := tokenizeWords(text2)

	for _, word := range w1 {
		if antonyms, ok := opposites[word]; ok {
			for _, w := range w2 {
				for _, ant := range antonyms {
					if w == ant {
						return true
					}
				}
			}
		}
	}
	for _, word := range w2 {
		if antonyms, ok := opposites[word]; ok {
			for _, w := range w1 {
				for _, ant := range antonyms {
					if w == ant {
						return true
					}
				}
			}
		}
	}
	return false
}

// keyNounsMap is the package-level key nouns set used by hasDifferentKeyNoun and isKeyNoun.
var keyNounsMap = map[string]bool{
	// Sort algorithms
	"quicksort": true, "mergesort": true, "bubblesort": true, "heapsort": true, "timsort": true, "radixsort": true,
	// Cloud providers
	"aws": true, "azure": true, "gcp": true, "digitalocean": true, "heroku": true, "vercel": true, "netlify": true, "cloudflare": true,
	// Cloud services
	"lambda": true, "fargate": true, "ecs": true, "ec2": true, "s3": true, "cloudrun": true, "appengine": true,
	// Languages
	"go": true, "rust": true, "python": true, "java": true, "javascript": true, "typescript": true, "ruby": true,
	"scala": true, "kotlin": true, "swift": true, "cpp": true, "csharp": true, "php": true, "elixir": true, "haskell": true,
	// Frontend frameworks
	"react": true, "vue": true, "angular": true, "svelte": true, "nextjs": true, "nuxt": true, "remix": true, "astro": true, "solidjs": true,
	// Databases
	"mysql": true, "postgresql": true, "mongodb": true, "redis": true, "cassandra": true, "dynamodb": true,
	"sqlite": true, "mariadb": true, "cockroachdb": true, "neo4j": true,
	// Container/orchestration
	"docker": true, "kubernetes": true, "podman": true, "vagrant": true, "nomad": true,
	// Git platforms
	"github": true, "gitlab": true, "bitbucket": true, "codecommit": true,
	// Test frameworks
	"jest": true, "pytest": true, "mocha": true, "rspec": true, "junit": true, "vitest": true, "cypress": true, "playwright": true,
	// API styles
	"rest": true, "graphql": true, "grpc": true, "soap": true, "trpc": true,
	// Auth methods
	"jwt": true, "session": true, "oauth": true, "saml": true, "oidc": true, "cookies": true,
	// Data formats
	"csv": true, "json": true, "xml": true, "yaml": true, "toml": true, "parquet": true, "protobuf": true, "avro": true,
	// OS
	"linux": true, "windows": true, "macos": true, "ubuntu": true, "debian": true, "alpine": true,
	// Web servers
	"nginx": true, "apache": true, "caddy": true, "traefik": true, "haproxy": true,
	// Message queues
	"kafka": true, "rabbitmq": true, "sqs": true, "nats": true, "pulsar": true,
	// Search engines
	"elasticsearch": true, "solr": true, "opensearch": true, "algolia": true,
	// IaC
	"terraform": true, "pulumi": true, "ansible": true, "chef": true, "puppet": true,
	// Monitoring
	"prometheus": true, "grafana": true, "datadog": true, "newrelic": true,
	// Math operations
	"sum": true, "product": true, "average": true, "count": true, "max": true, "min": true, "median": true, "mean": true,
	// Parity
	"even": true, "odd": true,
	// Order
	"ascending": true, "descending": true,
	// Architecture
	"monolithic": true, "microservices": true, "serverless": true, "modular": true,
	// Data structures
	"list": true, "dictionary": true, "dict": true, "tuple": true, "set": true, "array": true,
	"map": true, "queue": true, "stack": true, "heap": true, "tree": true, "graph": true,
	// Types
	"integer": true, "string": true, "float": true, "boolean": true, "char": true, "byte": true,
	// Protocols
	"tcp": true, "udp": true, "http": true, "https": true, "ftp": true, "smtp": true,
	// Test types
	"unit": true, "integration": true, "e2e": true, "regression": true, "load": true, "stress": true, "smoke": true,
	// DB types
	"sql": true, "nosql": true,
	// ML frameworks
	"tensorflow": true, "pytorch": true, "keras": true, "scikit": true, "pandas": true, "numpy": true,
	"huggingface": true, "langchain": true, "llamaindex": true,
	// CI/CD tools
	"jenkins": true, "circleci": true, "travisci": true, "argocd": true, "spinnaker": true, "tekton": true,
	// Package managers
	"npm": true, "yarn": true, "pnpm": true, "pip": true, "cargo": true, "maven": true, "gradle": true, "bundler": true, "composer": true,
	// Caching
	"memcached": true, "varnish": true,
	// Service mesh
	"istio": true, "envoy": true, "linkerd": true, "consul": true,
	// Cloud services (more)
	"aurora": true, "bigquery": true, "redshift": true, "snowflake": true, "databricks": true,
	"kinesis": true, "eventbridge": true, "stepfunctions": true, "apigateway": true,
	"cloudfront": true, "route53": true, "cognito": true, "amplify": true,
	// Operating system concepts
	"process": true, "thread": true, "fiber": true, "coroutine": true,
	// Editors/IDEs
	"vscode": true, "vim": true, "neovim": true, "emacs": true, "intellij": true, "webstorm": true,
	// CSS frameworks
	"tailwind": true, "bootstrap": true, "bulma": true, "materialui": true,
	// Runtime/platform
	"node": true, "deno": true, "bun": true, "jvm": true, "dotnet": true,
	// Auth/security tools
	"vault": true, "keycloak": true, "auth0": true, "okta": true,
	// Observability
	"jaeger": true, "zipkin": true, "opentelemetry": true, "loki": true, "fluentd": true, "logstash": true,
	// Additional algorithms
	"dijkstra": true, "bellmanford": true, "bfs": true, "dfs": true, "astar": true,
	"fibonacci": true, "factorial": true, "binarysearch": true, "linearsearch": true,
	// Additional data structures
	"linkedlist": true, "doublylinked": true, "trie": true, "btree": true, "avl": true, "redblack": true, "skiplist": true,
	// Design patterns
	"singleton": true, "factory": true, "observer": true, "strategy": true, "decorator": true, "adapter": true, "proxy": true, "facade": true, "builder": true,
	// HTTP methods
	"get": true, "post": true, "put": true, "patch": true, "delete": true,
}

// getKeyNouns returns the package-level key nouns map for reuse by other packages.
func getKeyNouns() map[string]bool {
	return keyNounsMap
}

// hasDifferentKeyNoun detects if two prompts refer to different specific technologies,
// tools, languages, or algorithms. Returns true if they have mutually exclusive key nouns.
// Uses the dynamic registry if available, falling back to static key nouns.
func hasDifferentKeyNoun(text1, text2 string, registry *SynonymRegistry) bool {
	// Use dynamic registry if available
	if registry != nil {
		return registry.HasDifferentKeyNounDynamic(text1, text2)
	}

	// Fallback: static key nouns (original behavior)
	w1 := tokenizeWords(text1)
	w2 := tokenizeWords(text2)

	var kn1, kn2 []string
	for _, w := range w1 {
		if keyNounsMap[w] {
			kn1 = append(kn1, w)
		}
	}
	for _, w := range w2 {
		if keyNounsMap[w] {
			kn2 = append(kn2, w)
		}
	}

	if len(kn1) == 0 || len(kn2) == 0 {
		return false
	}

	// Find key nouns unique to each side
	hasUnique1 := false
	hasUnique2 := false
	for _, k := range kn1 {
		found := false
		for _, k2 := range kn2 {
			if k == k2 {
				found = true
				break
			}
		}
		if !found {
			hasUnique1 = true
			break
		}
	}
	for _, k := range kn2 {
		found := false
		for _, k1 := range kn1 {
			if k == k1 {
				found = true
				break
			}
		}
		if !found {
			hasUnique2 = true
			break
		}
	}

	return hasUnique1 && hasUnique2
}

// tokenizeWords splits text into lowercase words.
func tokenizeWords(text string) []string {
	lower := strings.ToLower(text)
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	result := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) > 0 {
			result = append(result, f)
		}
	}
	return result
}
