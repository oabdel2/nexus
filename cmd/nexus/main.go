package main

import (
"bufio"
"context"
"encoding/json"
"flag"
"fmt"
"io"
"log/slog"
"net"
"net/http"
"os"
"os/signal"
"runtime"
"strings"
"syscall"
"time"

"github.com/nexus-gateway/nexus/internal/config"
"github.com/nexus-gateway/nexus/internal/gateway"
"github.com/nexus-gateway/nexus/internal/router"
"github.com/nexus-gateway/nexus/internal/security"

"gopkg.in/yaml.v3"
)

var (
version   = "0.1.0"
buildDate = "unknown"
gitCommit = "unknown"
banner    = `
 _   _
| \ | | _____  ___   _ ___
|  \| |/ _ \ \/ / | | / __|
| |\  |  __/>  <| |_| \__ \
|_| \_|\___/_/\_\\__,_|___/

Agentic-First Inference Optimization Gateway v%s
`
)

func main() {
if len(os.Args) < 2 {
runServe(os.Args[1:])
return
}

subcmd := os.Args[1]

if subcmd == "-version" || subcmd == "--version" {
printVersion()
return
}

switch subcmd {
case "serve":
runServe(os.Args[2:])
case "init":
runInit(os.Args[2:])
case "status":
runStatus(os.Args[2:])
case "version":
printVersion()
case "validate":
runValidate(os.Args[2:])
case "inspect":
runInspect(os.Args[2:])
case "doctor":
runDoctor(os.Args[2:])
case "help", "-h", "--help":
printUsage()
default:
if strings.HasPrefix(subcmd, "-") {
runServe(os.Args[1:])
return
}
fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", subcmd)
printUsage()
os.Exit(1)
}
}

func printUsage() {
fmt.Print(`Usage: nexus [command] [flags]

Commands:
  serve       Start the Nexus gateway server (default)
  init        Interactive configuration wizard
  status      Show gateway health and stats
  doctor      Run system health diagnostics
  version     Show version information
  validate    Validate a configuration file
  inspect     Analyze routing for a prompt

Run 'nexus <command> -h' for command-specific help.
`)
}

func printVersion() {
fmt.Printf("nexus v%s\n", version)
fmt.Printf("  build:  %s\n", buildDate)
fmt.Printf("  commit: %s\n", gitCommit)
}

func runServe(args []string) {
fs := flag.NewFlagSet("serve", flag.ExitOnError)
configPath := fs.String("config", "configs/nexus.yaml", "path to configuration file")
port := fs.Int("port", 0, "override server port")
logLevel := fs.String("log-level", "", "log level (debug, info, warn, error)")
fs.Parse(args)

fmt.Printf(banner, version)

cfg, err := config.Load(*configPath)
if err != nil && config.HasProviderEnvVars() {
	// No config file found but env vars are set — use auto-config
	cfg = config.AutoConfig()
	fmt.Println("🔍 No config file — auto-detecting providers...")
	names := []string{}
	for _, p := range cfg.Providers {
		if p.Enabled {
			models := []string{}
			for _, m := range p.Models {
				models = append(models, m.Name)
			}
			names = append(names, fmt.Sprintf("%s (%s)", p.Name, strings.Join(models, ", ")))
		}
	}
	fmt.Printf("✅ Found %d provider(s): %s\n", len(names), strings.Join(names, "; "))
	if cfg.Cache.Enabled {
		var layers []string
		if cfg.Cache.L1Enabled || cfg.Cache.L1.Enabled {
			layers = append(layers, "L1")
		}
		if cfg.Cache.L2BM25.Enabled {
			layers = append(layers, "BM25")
		}
		fmt.Printf("✅ Cache: %s enabled\n", strings.Join(layers, " + "))
	}
	if cfg.Compression.Enabled {
		fmt.Println("✅ Compression: enabled (whitespace + code + boilerplate)")
	}
	fmt.Printf("✅ Dashboard: http://localhost:%d/dashboard\n", cfg.Server.Port)
	fmt.Println()
	fmt.Printf("Nexus is ready! Send requests to http://localhost:%d/v1/chat/completions\n", cfg.Server.Port)
	fmt.Println()
} else if err != nil {
slog.Warn("config file not found, using defaults", "path", *configPath, "error", err)
cfg = config.DefaultConfig()
}

if *port > 0 {
cfg.Server.Port = *port
}

level := slog.LevelInfo
logLvl := cfg.Telemetry.LogLevel
if *logLevel != "" {
logLvl = *logLevel
}
switch logLvl {
case "debug":
level = slog.LevelDebug
case "warn":
level = slog.LevelWarn
case "error":
level = slog.LevelError
}

var handler slog.Handler
if cfg.Telemetry.LogFormat == "json" {
handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
} else {
handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
}
logger := slog.New(handler)

cfg.ExpandSecrets()

srv := gateway.New(cfg, logger)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

go func() {
<-sigCh
logger.Info("shutting down...")
shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
defer shutdownCancel()
srv.Shutdown(shutdownCtx)
cancel()
}()

if err := srv.Start(ctx); err != nil && err.Error() != "http: Server closed" {
logger.Error("server error", "error", err)
os.Exit(1)
}

logger.Info("nexus gateway stopped")
}

func runInit(args []string) {
fs := flag.NewFlagSet("init", flag.ExitOnError)
outPath := fs.String("output", "configs/nexus.yaml", "output config file path")
fs.Parse(args)

reader := bufio.NewReader(os.Stdin)
prompt := func(label, defaultVal string) string {
fmt.Printf("? %s [%s]: ", label, defaultVal)
line, _ := reader.ReadString('\n')
line = strings.TrimSpace(line)
if line == "" {
return defaultVal
}
return line
}
promptBool := func(label string, defaultYes bool) bool {
hint := "Y/n"
if !defaultYes {
hint = "y/N"
}
fmt.Printf("? %s [%s]: ", label, hint)
line, _ := reader.ReadString('\n')
line = strings.TrimSpace(strings.ToLower(line))
if line == "" {
return defaultYes
}
return line == "y" || line == "yes"
}

fmt.Println()
fmt.Println("\U0001f527 Nexus Configuration Wizard")
fmt.Println()

portStr := prompt("Server port", "8080")
port := 8080
if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
port = 8080
}

enableOllama := promptBool("Enable Ollama?", true)
ollamaURL := "http://localhost:11434/v1"
if enableOllama {
ollamaURL = prompt("Ollama URL", "http://localhost:11434/v1")
}

enableOpenAI := promptBool("Enable OpenAI?", false)
enableCache := promptBool("Enable cache?", true)
enableCompression := promptBool("Enable prompt compression?", true)
enableCascade := promptBool("Enable cascade routing?", false)

cfg := config.DefaultConfig()
cfg.Server.Port = port
cfg.Cache.Enabled = enableCache
cfg.Cache.L1Enabled = enableCache
cfg.Cache.L1.Enabled = enableCache
cfg.Compression.Enabled = enableCompression
cfg.Cascade.Enabled = config.BoolPtr(enableCascade)

cfg.Providers = nil
if enableOllama {
cfg.Providers = append(cfg.Providers, config.ProviderConfig{
Name:     "ollama",
Type:     "ollama",
BaseURL:  ollamaURL,
Enabled:  true,
Priority: 1,
Models: []config.ModelConfig{
{Name: "llama3.1", Tier: "cheap", CostPer1K: 0.0, MaxTokens: 8192},
},
})
}
if enableOpenAI {
cfg.Providers = append(cfg.Providers, config.ProviderConfig{
Name:     "openai",
Type:     "openai",
BaseURL:  "https://api.openai.com/v1",
APIKey:   "${OPENAI_API_KEY}",
Enabled:  true,
Priority: 2,
Models: []config.ModelConfig{
{Name: "gpt-4o-mini", Tier: "cheap", CostPer1K: 0.00015, MaxTokens: 16384},
{Name: "gpt-4o", Tier: "mid", CostPer1K: 0.005, MaxTokens: 16384},
{Name: "o3", Tier: "premium", CostPer1K: 0.01, MaxTokens: 100000},
},
})
}

data, err := yaml.Marshal(cfg)
if err != nil {
fmt.Fprintf(os.Stderr, "Error marshaling config: %v\n", err)
os.Exit(1)
}

dir := dirOf(*outPath)
if dir != "" {
if err := os.MkdirAll(dir, 0755); err != nil {
fmt.Fprintf(os.Stderr, "Error creating directory: %v\n", err)
os.Exit(1)
}
}

if err := os.WriteFile(*outPath, data, 0644); err != nil {
fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
os.Exit(1)
}

fmt.Println()
fmt.Printf("\u2705 Config written to %s\n", *outPath)
fmt.Println("   Start Nexus with: nexus serve")
}

func dirOf(path string) string {
for i := len(path) - 1; i >= 0; i-- {
if path[i] == '/' || path[i] == '\\' {
return path[:i]
}
}
return ""
}

func runStatus(args []string) {
fs := flag.NewFlagSet("status", flag.ExitOnError)
addr := fs.String("addr", "http://localhost:8080", "gateway address")
fs.Parse(args)

client := &http.Client{Timeout: 5 * time.Second}

fmt.Println()
fmt.Println("Nexus Gateway Status")
fmt.Println("\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501")

info, infoErr := fetchJSON(client, *addr+"/")
if infoErr != nil {
fmt.Printf("  Status:     \u274c Unreachable (%v)\n", infoErr)
fmt.Println()
fmt.Println("  Possible fixes:")
fmt.Println("    1. Start the gateway:  nexus serve -config configs/nexus.yaml")
fmt.Println("    2. Check the address:  nexus status -addr http://host:port")
fmt.Println("    3. Validate config:    nexus validate -config configs/nexus.yaml")
fmt.Println()
os.Exit(1)
}

ver, _ := info["version"].(string)
if ver == "" {
ver = "unknown"
}
fmt.Printf("  Status:     \u2705 Running\n")
fmt.Printf("  Version:    %s\n", ver)

if total, ok := info["requests_total"].(float64); ok {
fmt.Printf("\n  Requests:   %s total\n", formatNumber(int64(total)))
}

if cacheMap, ok := info["cache"].(map[string]any); ok {
hits, _ := cacheMap["hits"].(float64)
size, _ := cacheMap["size"].(float64)
misses, _ := cacheMap["misses"].(float64)
total := hits + misses
pct := 0.0
if total > 0 {
pct = hits / total * 100
}
fmt.Printf("  Cache Hits: %s (%.1f%%)\n", formatNumber(int64(hits)), pct)
fmt.Printf("  Cache Size: %s entries\n", formatNumber(int64(size)))
}

ready, readyErr := fetchJSON(client, *addr+"/health/ready")
if readyErr == nil {
if checks, ok := ready["checks"].(map[string]any); ok {
if prov, ok := checks["providers"].(map[string]any); ok {
fmt.Printf("\n  Providers:  %v configured\n", prov["count"])
}
}
}

cbs, cbErr := fetchJSON(client, *addr+"/api/circuit-breakers")
if cbErr == nil {
fmt.Println("\n  Circuit Breakers:")
for name, v := range cbs {
state := "unknown"
if m, ok := v.(map[string]any); ok {
if s, ok := m["state"].(string); ok {
state = s
}
}
icon := "\u2705"
if state == "open" {
icon = "\u274c"
}
fmt.Printf("    %-12s %s CB: %s\n", name, icon, state)
}
}

fmt.Println()
}

func fetchJSON(client *http.Client, url string) (map[string]any, error) {
resp, err := client.Get(url)
if err != nil {
return nil, err
}
defer resp.Body.Close()
body, err := io.ReadAll(resp.Body)
if err != nil {
return nil, err
}
var result map[string]any
if err := json.Unmarshal(body, &result); err != nil {
return nil, err
}
return result, nil
}

func formatNumber(n int64) string {
if n < 1000 {
return fmt.Sprintf("%d", n)
}
if n < 1_000_000 {
return fmt.Sprintf("%d,%03d", n/1000, n%1000)
}
return fmt.Sprintf("%d,%03d,%03d", n/1_000_000, (n%1_000_000)/1000, n%1000)
}

func runValidate(args []string) {
fs := flag.NewFlagSet("validate", flag.ExitOnError)
configPath := fs.String("config", "configs/nexus.yaml", "config file to validate")
fs.Parse(args)

cfg, err := config.Load(*configPath)
if err != nil && config.HasProviderEnvVars() {
	fmt.Println()
	fmt.Println("🔍 No config file — validating auto-detected config from environment...")
	cfg = config.AutoConfig()
} else if err != nil {
fmt.Printf("❌ Config invalid: %v\n", err)
os.Exit(1)
}

fmt.Println()
fmt.Printf("\u2705 Config is valid\n")

enabledCount := 0
for _, p := range cfg.Providers {
if p.Enabled {
enabledCount++
}
}
fmt.Printf("  Providers: %d configured, %d enabled\n", len(cfg.Providers), enabledCount)

var layers []string
if cfg.Cache.L1Enabled || cfg.Cache.L1.Enabled {
layers = append(layers, "L1")
}
if cfg.Cache.L2BM25.Enabled {
layers = append(layers, "BM25")
}
if cfg.Cache.L2Semantic.Enabled {
layers = append(layers, "Semantic")
}
if len(layers) > 0 {
fmt.Printf("  Cache: %s enabled\n", strings.Join(layers, " + "))
} else if cfg.Cache.Enabled {
fmt.Printf("  Cache: enabled (no layers configured)\n")
} else {
fmt.Printf("  Cache: disabled\n")
}

secParts := []string{}
if cfg.Security.RateLimit.Enabled {
secParts = append(secParts, "rate limiting ON")
}
if cfg.Security.PromptGuard.Enabled {
secParts = append(secParts, "prompt guard ON")
}
if cfg.Security.OIDC.Enabled {
secParts = append(secParts, "OIDC ON")
}
if len(secParts) > 0 {
fmt.Printf("  Security: %s\n", strings.Join(secParts, ", "))
}

if !cfg.Security.TLS.Enabled {
fmt.Printf("  \u26a0 Warning: TLS disabled (OK for development)\n")
}
if !cfg.Billing.Enabled {
fmt.Printf("  \u26a0 Warning: Billing disabled\n")
}
if enabledCount == 0 {
fmt.Printf("  \u26a0 Warning: No providers enabled\n")
}

fmt.Println()
}

func runInspect(args []string) {
fs := flag.NewFlagSet("inspect", flag.ExitOnError)
configPath := fs.String("config", "configs/nexus.yaml", "config file")
fs.Parse(args)

remaining := fs.Args()
if len(remaining) == 0 {
fmt.Fprintf(os.Stderr, "Usage: nexus inspect <prompt>\n")
os.Exit(1)
}
prompt := strings.Join(remaining, " ")

cfg, err := config.Load(*configPath)
if err != nil {
slog.Warn("config not found, using defaults", "error", err)
cfg = config.DefaultConfig()
}

logger := slog.New(slog.NewTextHandler(io.Discard, nil))
r := router.New(cfg.Router, cfg.Providers, logger)
selection := r.Route(prompt, "", 0.0, 1.0, len(prompt))

fmt.Println()
fmt.Println("Nexus Routing Analysis")
fmt.Println("\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501")

displayPrompt := prompt
if len(displayPrompt) > 60 {
displayPrompt = displayPrompt[:57] + "..."
}
fmt.Printf("  Prompt:      \"%s\"\n", displayPrompt)
fmt.Println()
fmt.Printf("  Complexity Score: %.3f\n", selection.Score.FinalScore)
fmt.Printf("    Keywords:   %.2f", selection.Score.PromptScore)

matched := matchedKeywords(prompt)
if len(matched) > 0 {
fmt.Printf(" (%s)", strings.Join(matched, ", "))
}
fmt.Println()
fmt.Printf("    Length:     %.2f\n", selection.Score.LengthScore)
fmt.Printf("    Structure:  %.2f\n", selection.Score.StructScore)
fmt.Printf("    Context:    %.2f\n", selection.Score.ContextScore)
fmt.Printf("    Role:       %.2f\n", selection.Score.RoleScore)
fmt.Printf("    Position:   %.2f\n", selection.Score.PositionScore)
fmt.Printf("    Budget:     %.2f\n", selection.Score.BudgetScore)
fmt.Println()
fmt.Printf("  Tier Decision: %s\n", selection.Tier)
fmt.Printf("  Reason:       %s\n", selection.Reason)
if selection.Provider != "" {
fmt.Printf("  Provider:     %s -> %s\n", selection.Provider, selection.Model)
}

if cfg.Cascade.IsEnabled() {
threshold := cfg.Cascade.ConfidenceThreshold
wouldCascade := selection.Score.FinalScore < threshold &&
selection.Tier != "economy" && selection.Tier != "cheap"
fmt.Println()
if wouldCascade {
fmt.Printf("  Would cascade: yes (score < %.2f threshold)\n", threshold)
fmt.Printf("    -> Try cheap first, escalate if confidence < %.2f\n", threshold)
} else {
fmt.Printf("  Would cascade: no\n")
}
} else {
fmt.Println()
fmt.Printf("  Cascade: disabled\n")
}

fmt.Println()
}

func matchedKeywords(prompt string) []string {
lower := strings.ToLower(prompt)
high := []string{
"analyze", "debug", "fix", "refactor", "optimize", "architect",
"security", "vulnerability", "race condition", "deadlock",
"concurrent", "distributed", "algorithm", "prove", "derive",
"implement", "design pattern", "trade-off", "critical",
"production", "migrate", "performance",
"memory leak", "scaling", "sharding", "consensus",
"encryption", "authentication", "zero-day", "exploit",
"backward compatible", "fault tolerant", "load balancing",
"thread safe", "mutex", "semaphore",
}
var found []string
for _, kw := range high {
if strings.Contains(lower, kw) {
found = append(found, kw)
}
}
return found
}

func runDoctor(args []string) {
fs := flag.NewFlagSet("doctor", flag.ExitOnError)
configPath := fs.String("config", "configs/nexus.yaml", "path to configuration file")
fs.Parse(args)

fmt.Println()
fmt.Println("Nexus Doctor \u2014 System Health Check")
fmt.Println("\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501")
fmt.Println()

warnings := 0
errors := 0

// Go version
fmt.Printf("  Go version .......... \u2705 %s\n", runtime.Version())

// Config file
cfg, cfgErr := config.Load(*configPath)
if cfgErr != nil {
fmt.Printf("  Config file ......... \u274c %s (%v)\n", *configPath, cfgErr)
errors++
cfg = config.DefaultConfig()
} else {
fmt.Printf("  Config file ......... \u2705 %s (valid)\n", *configPath)
}

// Providers
fmt.Println("  Providers:")
client := &http.Client{Timeout: 5 * time.Second}
for _, p := range cfg.Providers {
if !p.Enabled {
continue
}
baseURL := strings.TrimSuffix(p.BaseURL, "/v1")
reachable := checkEndpoint(client, baseURL)
if reachable {
fmt.Printf("    %-15s \u2705 reachable (%s)\n", p.Name, baseURL)
} else {
// Check if it's a cloud provider with missing API key
apiKey := os.ExpandEnv(p.APIKey)
if apiKey == "" || strings.HasPrefix(apiKey, "${") {
fmt.Printf("    %-15s \u26a0\ufe0f  no API key configured\n", p.Name)
warnings++
} else {
fmt.Printf("    %-15s \u274c unreachable (%s)\n", p.Name, baseURL)
errors++
}
}
}
if len(cfg.Providers) == 0 {
fmt.Printf("    (none configured) \u26a0\ufe0f  no providers\n")
warnings++
}

// Embedding model
if cfg.Cache.L2Semantic.Enabled {
embModel := cfg.Cache.L2Semantic.Model
embEndpoint := cfg.Cache.L2Semantic.Endpoint
if checkOllamaModel(client, embEndpoint, embModel) {
fmt.Printf("  Embedding model ..... \u2705 %s available\n", embModel)
} else if checkEndpoint(client, embEndpoint) {
fmt.Printf("  Embedding model ..... \u26a0\ufe0f  %s not found at %s\n", embModel, embEndpoint)
warnings++
} else {
fmt.Printf("  Embedding model ..... \u274c endpoint unreachable (%s)\n", embEndpoint)
errors++
}
} else {
fmt.Printf("  Embedding model ..... \u26a0\ufe0f  semantic cache disabled\n")
warnings++
}

// Chat model
chatModelChecked := false
for _, p := range cfg.Providers {
if !p.Enabled || p.Type != "ollama" || len(p.Models) == 0 {
continue
}
baseURL := strings.TrimSuffix(p.BaseURL, "/v1")
model := p.Models[0].Name
if checkOllamaModel(client, baseURL, model) {
fmt.Printf("  Chat model .......... \u2705 %s available\n", model)
} else {
fmt.Printf("  Chat model .......... \u274c %s not found\n", model)
errors++
}
chatModelChecked = true
break
}
if !chatModelChecked {
for _, p := range cfg.Providers {
if !p.Enabled || len(p.Models) == 0 {
continue
}
fmt.Printf("  Chat model .......... \u2705 %s configured (%s)\n", p.Models[0].Name, p.Name)
chatModelChecked = true
break
}
}
if !chatModelChecked {
fmt.Printf("  Chat model .......... \u274c none configured\n")
errors++
}

// Cache
fmt.Println("  Cache:")
l1 := cfg.Cache.L1Enabled || cfg.Cache.L1.Enabled
if l1 {
fmt.Printf("    L1 exact .......... \u2705 enabled\n")
} else {
fmt.Printf("    L1 exact .......... \u274c disabled\n")
}
if cfg.Cache.L2BM25.Enabled {
fmt.Printf("    L2 BM25 ........... \u2705 enabled\n")
} else {
fmt.Printf("    L2 BM25 ........... \u274c disabled\n")
}
if cfg.Cache.L2Semantic.Enabled {
fmt.Printf("    L2 semantic ....... \u2705 enabled (%s @ %s)\n",
cfg.Cache.L2Semantic.Model, cfg.Cache.L2Semantic.Endpoint)
} else {
fmt.Printf("    L2 semantic ....... \u274c disabled\n")
}

// Security
fmt.Println("  Security:")
if cfg.Security.TLS.Enabled {
fmt.Printf("    TLS ............... \u2705 enabled\n")
} else {
fmt.Printf("    TLS ............... \u26a0\ufe0f  disabled (OK for development)\n")
warnings++
}
if cfg.Security.RateLimit.Enabled {
fmt.Printf("    Rate limiting ..... \u2705 %d RPM\n", cfg.Security.RateLimit.DefaultRPM)
} else {
fmt.Printf("    Rate limiting ..... \u274c disabled\n")
}
pg := security.NewPromptGuard(security.PromptGuardConfig{
Enabled:        cfg.Security.PromptGuard.Enabled,
CustomPatterns: cfg.Security.PromptGuard.CustomPatterns,
})
if cfg.Security.PromptGuard.Enabled {
fmt.Printf("    Prompt guard ...... \u2705 %d patterns\n", pg.PatternCount())
} else {
fmt.Printf("    Prompt guard ...... \u274c disabled\n")
}

// Compression
if cfg.Compression.Enabled {
var strategies []string
if cfg.Compression.Whitespace {
strategies = append(strategies, "whitespace")
}
if cfg.Compression.CodeStrip {
strategies = append(strategies, "code")
}
if cfg.Compression.HistoryTruncate {
strategies = append(strategies, "history")
}
if cfg.Compression.Boilerplate {
strategies = append(strategies, "boilerplate")
}
if cfg.Compression.JSONMinify {
strategies = append(strategies, "json")
}
if cfg.Compression.Deduplication {
strategies = append(strategies, "dedup")
}
label := "enabled"
if len(strategies) > 0 {
label = "enabled (" + strings.Join(strategies, " + ") + ")"
}
fmt.Printf("  Compression ......... \u2705 %s\n", label)
} else {
fmt.Printf("  Compression ......... \u274c disabled\n")
}

// Cascade routing
if cfg.Cascade.IsEnabled() {
fmt.Printf("  Cascade routing ..... \u2705 enabled (threshold=%.2f)\n", cfg.Cascade.ConfidenceThreshold)
} else {
fmt.Printf("  Cascade routing ..... \u274c disabled\n")
}

// Eval scoring
if cfg.Eval.Enabled {
fmt.Printf("  Eval scoring ........ \u2705 enabled\n")
} else {
fmt.Printf("  Eval scoring ........ \u274c disabled\n")
}

// Data directory
dataDir := cfg.Cache.Synonym.DataDir
if dataDir == "" {
dataDir = "./data"
}
if isDirWritable(dataDir) {
fmt.Printf("  Data directory ...... \u2705 %s (writable)\n", dataDir)
} else {
fmt.Printf("  Data directory ...... \u26a0\ufe0f  %s (not writable or missing)\n", dataDir)
warnings++
}

// Port availability
port := cfg.Server.Port
if isPortAvailable(port) {
fmt.Printf("  Port %d ........... \u2705 available\n", port)
} else {
fmt.Printf("  Port %d ........... \u26a0\ufe0f  in use\n", port)
warnings++
}

// Overall
fmt.Println()
if errors > 0 {
fmt.Printf("  Overall: %d error(s), %d warning(s)\n", errors, warnings)
} else if warnings > 0 {
fmt.Printf("  Overall: Ready to start (%d warning(s))\n", warnings)
} else {
fmt.Printf("  Overall: Ready to start \u2705\n")
}
fmt.Println()
}

func checkEndpoint(client *http.Client, baseURL string) bool {
resp, err := client.Get(baseURL)
if err != nil {
return false
}
resp.Body.Close()
return true
}

func checkOllamaModel(client *http.Client, endpoint, model string) bool {
resp, err := client.Get(endpoint + "/api/tags")
if err != nil {
return false
}
defer resp.Body.Close()

var tags struct {
Models []struct {
Name string `json:"name"`
} `json:"models"`
}
if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
return false
}
for _, m := range tags.Models {
if m.Name == model || strings.HasPrefix(m.Name, model+":") {
return true
}
}
return false
}

func isDirWritable(dir string) bool {
info, err := os.Stat(dir)
if err != nil {
// Try to create it
if mkErr := os.MkdirAll(dir, 0755); mkErr != nil {
return false
}
return true
}
if !info.IsDir() {
return false
}
// Try creating a temp file to test writability
f, err := os.CreateTemp(dir, ".nexus-doctor-*")
if err != nil {
return false
}
name := f.Name()
f.Close()
os.Remove(name)
return true
}

func isPortAvailable(port int) bool {
ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
if err != nil {
return false
}
ln.Close()
return true
}
