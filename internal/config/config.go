package config

import (
	"net/http"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Providers    []ProviderConfig   `yaml:"providers"`
	Router       RouterConfig       `yaml:"router"`
	Cache        CacheConfig        `yaml:"cache"`
	Workflow     WorkflowConfig     `yaml:"workflow"`
	Telemetry    TelemetryConfig    `yaml:"telemetry"`
	Tracing      TracingConfig      `yaml:"tracing"`
	Security     SecurityConfig     `yaml:"security"`
	Storage      StorageYAMLConfig  `yaml:"storage"`
	Billing      BillingConfig      `yaml:"billing"`
	Notification NotificationConfig `yaml:"notification"`
	Compression  CompressionConfig  `yaml:"compression"`
	Cascade      CascadeConfig      `yaml:"cascade"`
	Eval         EvalConfig         `yaml:"eval"`
	Experiment   ExperimentConfig   `yaml:"experiment"`
	Adaptive     AdaptiveConfig     `yaml:"adaptive"`
	Events       EventsConfig       `yaml:"events"`
	Plugins      PluginsConfig      `yaml:"plugins"`
}

type CompressionConfig struct {
	Enabled         bool `yaml:"enabled"`
	Whitespace      bool `yaml:"whitespace"`
	CodeStrip       bool `yaml:"code_strip"`
	HistoryTruncate bool `yaml:"history_truncate"`
	Boilerplate     bool `yaml:"boilerplate"`
	JSONMinify      bool `yaml:"json_minify"`
	Deduplication   bool `yaml:"deduplication"`
	MaxHistoryTurns int  `yaml:"max_history_turns"`
	PreserveLastN   int  `yaml:"preserve_last_n"`
}

type CascadeConfig struct {
	Enabled             bool    `yaml:"enabled"`
	ConfidenceThreshold float64 `yaml:"confidence_threshold"`
	MaxLatencyMs        int     `yaml:"max_latency_ms"`
	SampleRate          float64 `yaml:"sample_rate"`
}

type EvalConfig struct {
	Enabled          bool    `yaml:"enabled"`
	DataDir          string  `yaml:"data_dir"`
	HedgingPenalty   float64 `yaml:"hedging_penalty"`
	SampleRate       float64 `yaml:"sample_rate"`
	ShadowEnabled    bool    `yaml:"shadow_enabled"`
	ShadowSampleRate float64 `yaml:"shadow_sample_rate"`
}

type ExperimentConfig struct {
	Enabled   bool `yaml:"enabled"`
	AutoStart bool `yaml:"auto_start"`
}

type AdaptiveConfig struct {
	Enabled        bool    `yaml:"enabled"`
	MinSamples     int     `yaml:"min_samples"`
	HighConfidence float64 `yaml:"high_confidence"`
	LowConfidence  float64 `yaml:"low_confidence"`
}

type EventsConfig struct {
	Enabled       bool     `yaml:"enabled"`
	WebhookURLs   []string `yaml:"webhook_urls"`
	WebhookSecret string   `yaml:"webhook_secret"`
}

type PluginsConfig struct {
	Enabled bool `yaml:"enabled"`
}

type BillingConfig struct {
	Enabled             bool   `yaml:"enabled"`
	DataDir             string `yaml:"data_dir"`
	StripeWebhookSecret string `yaml:"stripe_webhook_secret"`
	DefaultPlan         string `yaml:"default_plan"`
}

type NotificationConfig struct {
	Enabled    bool   `yaml:"enabled"`
	SMTPHost   string `yaml:"smtp_host"`
	SMTPPort   int    `yaml:"smtp_port"`
	SMTPUser   string `yaml:"smtp_user"`
	SMTPPass   string `yaml:"smtp_password"`
	FromEmail  string `yaml:"from_email"`
	FromName   string `yaml:"from_name"`
}

type SecurityConfig struct {
	TLS             TLSYAMLConfig         `yaml:"tls"`
	PromptGuard     PromptGuardYAMLConfig `yaml:"prompt_guard"`
	OIDC            OIDCYAMLConfig        `yaml:"oidc"`
	RBAC            RBACYAMLConfig        `yaml:"rbac"`
	RateLimit       RateLimitYAMLConfig   `yaml:"rate_limit"`
	CORS            CORSYAMLConfig        `yaml:"cors"`
	AuditLog        bool                  `yaml:"audit_log"`
	BodySizeLimit   int64                 `yaml:"body_size_limit"`
	RequestTimeout  string                `yaml:"request_timeout"`
	PanicRecovery   bool                  `yaml:"panic_recovery"`
	IPAllowlist     IPAllowlistYAMLConfig `yaml:"ip_allowlist"`
	InputValidation bool                  `yaml:"input_validation"`
	RequestLogging  bool                  `yaml:"request_logging"`
}

type IPAllowlistYAMLConfig struct {
	Enabled    bool     `yaml:"enabled"`
	AllowedIPs []string `yaml:"allowed_ips"`
	Paths      []string `yaml:"paths"`
}

type TLSYAMLConfig struct {
	Enabled    bool   `yaml:"enabled"`
	CertFile   string `yaml:"cert_file"`
	KeyFile    string `yaml:"key_file"`
	CAFile     string `yaml:"ca_file"`
	MinVersion string `yaml:"min_version"`
	MutualTLS  bool   `yaml:"mutual_tls"`
}

type PromptGuardYAMLConfig struct {
	Enabled         bool     `yaml:"enabled"`
	Mode            string   `yaml:"mode"`
	MaxPromptLength int      `yaml:"max_prompt_length"`
	CustomPatterns  []string `yaml:"custom_patterns"`
	CustomPhrases   []string `yaml:"custom_phrases"`
}

type OIDCYAMLConfig struct {
	Enabled        bool     `yaml:"enabled"`
	Issuer         string   `yaml:"issuer"`
	ClientID       string   `yaml:"client_id"`
	ClientSecret   string   `yaml:"client_secret"`
	RedirectURL    string   `yaml:"redirect_url"`
	Scopes         []string `yaml:"scopes"`
	AllowedDomains []string `yaml:"allowed_domains"`
}

type RBACYAMLConfig struct {
	Enabled bool                `yaml:"enabled"`
	Roles   map[string]RoleYAML `yaml:"roles"`
}

type RoleYAML struct {
	Permissions  []string `yaml:"permissions"`
	MaxRPM       int      `yaml:"max_rpm"`
	MaxBudget    float64  `yaml:"max_budget"`
	AllowedTiers []string `yaml:"allowed_tiers"`
}

type RateLimitYAMLConfig struct {
	Enabled    bool `yaml:"enabled"`
	DefaultRPM int  `yaml:"default_rpm"`
	BurstSize  int  `yaml:"burst_size"`
}

type CORSYAMLConfig struct {
	AllowedOrigins []string `yaml:"allowed_origins"`
}

type StorageYAMLConfig struct {
	VectorBackend    string `yaml:"vector_backend"`
	KVBackend        string `yaml:"kv_backend"`
	QdrantHost       string `yaml:"qdrant_host"`
	QdrantPort       int    `yaml:"qdrant_port"`
	QdrantCollection string `yaml:"qdrant_collection"`
	QdrantAPIKey     string `yaml:"qdrant_api_key"`
	QdrantDimension  int    `yaml:"qdrant_dimension"`
	RedisAddr        string `yaml:"redis_addr"`
	RedisPassword    string `yaml:"redis_password"`
	RedisDB          int    `yaml:"redis_db"`
	RedisTLS         bool   `yaml:"redis_tls"`
}

type ServerConfig struct {
	Port          int           `yaml:"port"`
	ReadTimeout   time.Duration `yaml:"read_timeout"`
	WriteTimeout  time.Duration `yaml:"write_timeout"`
	MaxConcurrent int           `yaml:"max_concurrent"`
}

type ProviderConfig struct {
	Name     string            `yaml:"name"`
	Type     string            `yaml:"type"` // openai, anthropic, ollama
	BaseURL  string            `yaml:"base_url"`
	APIKey   string            `yaml:"api_key"`
	Headers  map[string]string `yaml:"headers"`
	Models   []ModelConfig     `yaml:"models"`
	Enabled  bool              `yaml:"enabled"`
	Priority int               `yaml:"priority"`
}

type ModelConfig struct {
	Name       string  `yaml:"name"`
	Tier       string  `yaml:"tier"` // economy, cheap, mid, premium
	CostPer1K  float64 `yaml:"cost_per_1k_tokens"`
	MaxTokens  int     `yaml:"max_tokens"`
}

type RouterConfig struct {
	Threshold        float64 `yaml:"threshold"`
	DefaultTier      string  `yaml:"default_tier"`
	BudgetEnabled    bool    `yaml:"budget_enabled"`
	DefaultBudget    float64 `yaml:"default_budget"`
	ComplexityWeights ComplexityWeights `yaml:"complexity_weights"`
	SmartClassifier  bool    `yaml:"smart_classifier"` // enable TF-IDF hybrid classifier (default true)
}

type ComplexityWeights struct {
	PromptComplexity float64 `yaml:"prompt_complexity"`
	ContextLength    float64 `yaml:"context_length"`
	AgentRole        float64 `yaml:"agent_role"`
	StepPosition     float64 `yaml:"step_position"`
	BudgetPressure   float64 `yaml:"budget_pressure"`
}

type CacheConfig struct {
	Enabled       bool          `yaml:"enabled"`
	L1Enabled     bool          `yaml:"l1_enabled"`
	L2Enabled     bool          `yaml:"l2_enabled"`
	TTL           time.Duration `yaml:"ttl"`
	MaxEntries    int           `yaml:"max_entries"`
	SimilarityMin float64       `yaml:"similarity_min"`
	L1            L1CacheConfig `yaml:"l1"`
	L2BM25        L2BM25Config  `yaml:"l2_bm25"`
	L2Semantic    L2SemanticConfig `yaml:"l2_semantic"`
	Feedback      FeedbackConfig `yaml:"feedback"`
	Shadow        ShadowConfig   `yaml:"shadow"`
	Synonym       SynonymConfig  `yaml:"synonym"`
}

type SynonymConfig struct {
	DataDir            string `yaml:"data_dir"`
	PromotionThreshold int    `yaml:"promotion_threshold"`
}

type FeedbackConfig struct {
	Enabled bool `yaml:"enabled"`
	MaxSize int  `yaml:"max_size"`
}

type ShadowConfig struct {
	Enabled    bool `yaml:"enabled"`
	MaxResults int  `yaml:"max_results"`
}

type L1CacheConfig struct {
	Enabled    bool          `yaml:"enabled"`
	TTL        time.Duration `yaml:"ttl"`
	MaxEntries int           `yaml:"max_entries"`
}

type L2BM25Config struct {
	Enabled    bool          `yaml:"enabled"`
	TTL        time.Duration `yaml:"ttl"`
	MaxEntries int           `yaml:"max_entries"`
	Threshold  float64       `yaml:"threshold"`
}

type L2SemanticConfig struct {
	Enabled    bool          `yaml:"enabled"`
	TTL        time.Duration `yaml:"ttl"`
	MaxEntries int           `yaml:"max_entries"`
	Threshold  float64       `yaml:"threshold"`
	Backend    string        `yaml:"backend"`
	Model      string        `yaml:"model"`
	Endpoint   string        `yaml:"endpoint"`
	APIKey     string        `yaml:"api_key"`
	Reranker   RerankerYAMLConfig `yaml:"reranker"`
}

type RerankerYAMLConfig struct {
	Enabled   bool    `yaml:"enabled"`
	Model     string  `yaml:"model"`
	Endpoint  string  `yaml:"endpoint"`
	Threshold float64 `yaml:"threshold"`
}

type WorkflowConfig struct {
	TTL           time.Duration `yaml:"ttl"`
	MaxSteps      int           `yaml:"max_steps"`
}

type TelemetryConfig struct {
	MetricsEnabled bool   `yaml:"metrics_enabled"`
	MetricsPort    int    `yaml:"metrics_port"`
	LogLevel       string `yaml:"log_level"`
	LogFormat      string `yaml:"log_format"` // json, text
}

type TracingConfig struct {
	Enabled     bool    `yaml:"enabled"`
	ServiceName string  `yaml:"service_name"`
	SampleRate  float64 `yaml:"sample_rate"`
	ExportURL   string  `yaml:"export_url"`
	LogSpans    bool    `yaml:"log_spans"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	setDefaults(cfg)
	return cfg, nil
}

// ExpandSecrets replaces ${ENV_VAR} references in all secret fields
// with their environment variable values so secrets never need to be
// stored in config files.
func (cfg *Config) ExpandSecrets() {
	for i := range cfg.Providers {
		cfg.Providers[i].APIKey = os.ExpandEnv(cfg.Providers[i].APIKey)
	}
	cfg.Billing.StripeWebhookSecret = os.ExpandEnv(cfg.Billing.StripeWebhookSecret)
	cfg.Security.OIDC.ClientSecret = os.ExpandEnv(cfg.Security.OIDC.ClientSecret)
	cfg.Notification.SMTPPass = os.ExpandEnv(cfg.Notification.SMTPPass)
	cfg.Storage.RedisPassword = os.ExpandEnv(cfg.Storage.RedisPassword)
	cfg.Storage.QdrantAPIKey = os.ExpandEnv(cfg.Storage.QdrantAPIKey)
	cfg.Events.WebhookSecret = os.ExpandEnv(cfg.Events.WebhookSecret)
	cfg.Cache.L2Semantic.APIKey = os.ExpandEnv(cfg.Cache.L2Semantic.APIKey)
}

func DefaultConfig() *Config {
	cfg := &Config{}
	setDefaults(cfg)
	return cfg
}

// HasProviderEnvVars returns true if any known provider API key is set in
// the environment, signalling that AutoConfig should be used.
func HasProviderEnvVars() bool {
	return os.Getenv("OPENAI_API_KEY") != "" ||
		os.Getenv("ANTHROPIC_API_KEY") != ""
}

// AutoConfig creates a fully working config from just environment variables.
// No YAML file needed. Detects available providers from env vars and a local
// Ollama instance.
func AutoConfig() *Config {
	cfg := DefaultConfig()

	// Auto-detect providers from env vars
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		cfg.Providers = append(cfg.Providers, ProviderConfig{
			Name: "openai", Type: "openai",
			BaseURL: "https://api.openai.com/v1", APIKey: key, Enabled: true, Priority: 1,
			Models: []ModelConfig{
				{Name: "gpt-4o-mini", Tier: "cheap", CostPer1K: 0.00015, MaxTokens: 16384},
				{Name: "gpt-4o", Tier: "mid", CostPer1K: 0.005, MaxTokens: 16384},
				{Name: "o3-mini", Tier: "premium", CostPer1K: 0.01, MaxTokens: 100000},
			},
		})
	}

	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		cfg.Providers = append(cfg.Providers, ProviderConfig{
			Name: "anthropic", Type: "anthropic",
			BaseURL: "https://api.anthropic.com/v1", APIKey: key, Enabled: true, Priority: 2,
			Models: []ModelConfig{
				{Name: "claude-haiku-4", Tier: "cheap", CostPer1K: 0.0008, MaxTokens: 8192},
				{Name: "claude-sonnet-4", Tier: "mid", CostPer1K: 0.003, MaxTokens: 64000},
				{Name: "claude-opus-4", Tier: "premium", CostPer1K: 0.015, MaxTokens: 200000},
			},
		})
	}

	// Check for local Ollama
	client := &http.Client{Timeout: 2 * time.Second}
	if resp, err := client.Get("http://localhost:11434/api/tags"); err == nil {
		resp.Body.Close()
		cfg.Providers = append(cfg.Providers, ProviderConfig{
			Name: "ollama", Type: "ollama",
			BaseURL: "http://localhost:11434/v1", Enabled: true, Priority: 3,
			Models: []ModelConfig{
				{Name: "auto", Tier: "cheap", CostPer1K: 0.0, MaxTokens: 4096},
			},
		})
	}

	// Enable all non-network features by default
	cfg.Cache.Enabled = true
	cfg.Cache.L1Enabled = true
	cfg.Cache.L1.Enabled = true
	cfg.Cache.L2BM25.Enabled = true
	cfg.Compression.Enabled = true
	cfg.Compression.Whitespace = true
	cfg.Compression.CodeStrip = true
	cfg.Compression.Boilerplate = true
	cfg.Security.PromptGuard.Enabled = true
	cfg.Security.RateLimit.Enabled = true
	cfg.Security.RateLimit.DefaultRPM = 60
	cfg.Security.RateLimit.BurstSize = 10
	cfg.Eval.Enabled = true
	cfg.Telemetry.LogFormat = "text"

	return cfg
}

func setDefaults(cfg *Config) {
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.ReadTimeout == 0 {
		cfg.Server.ReadTimeout = 30 * time.Second
	}
	if cfg.Server.WriteTimeout == 0 {
		cfg.Server.WriteTimeout = 120 * time.Second
	}
	if cfg.Router.Threshold == 0 {
		cfg.Router.Threshold = 0.7
	}
	if cfg.Router.DefaultTier == "" {
		cfg.Router.DefaultTier = "mid"
	}
	if cfg.Router.DefaultBudget == 0 {
		cfg.Router.DefaultBudget = 1.0
	}
	if cfg.Router.ComplexityWeights == (ComplexityWeights{}) {
		cfg.Router.ComplexityWeights = ComplexityWeights{
			PromptComplexity: 0.30,
			ContextLength:    0.15,
			AgentRole:        0.20,
			StepPosition:     0.15,
			BudgetPressure:   0.20,
		}
	}
	if cfg.Cache.TTL == 0 {
		cfg.Cache.TTL = 1 * time.Hour
	}
	if cfg.Cache.MaxEntries == 0 {
		cfg.Cache.MaxEntries = 10000
	}
	if cfg.Cache.SimilarityMin == 0 {
		cfg.Cache.SimilarityMin = 0.95
	}
	// L1 defaults
	if cfg.Cache.L1.TTL == 0 {
		cfg.Cache.L1.TTL = 15 * time.Minute
	}
	if cfg.Cache.L1.MaxEntries == 0 {
		cfg.Cache.L1.MaxEntries = 10000
	}
	// L2 BM25 defaults
	if cfg.Cache.L2BM25.TTL == 0 {
		cfg.Cache.L2BM25.TTL = 1 * time.Hour
	}
	if cfg.Cache.L2BM25.MaxEntries == 0 {
		cfg.Cache.L2BM25.MaxEntries = 50000
	}
	if cfg.Cache.L2BM25.Threshold == 0 {
		cfg.Cache.L2BM25.Threshold = 15.0
	}
	// L2 Semantic defaults
	if cfg.Cache.L2Semantic.TTL == 0 {
		cfg.Cache.L2Semantic.TTL = 1 * time.Hour
	}
	if cfg.Cache.L2Semantic.MaxEntries == 0 {
		cfg.Cache.L2Semantic.MaxEntries = 50000
	}
	if cfg.Cache.L2Semantic.Threshold == 0 {
		cfg.Cache.L2Semantic.Threshold = 0.92
	}
	if cfg.Cache.L2Semantic.Backend == "" {
		cfg.Cache.L2Semantic.Backend = "ollama"
	}
	if cfg.Cache.L2Semantic.Model == "" {
		cfg.Cache.L2Semantic.Model = "bge-m3"
	}
	if cfg.Cache.L2Semantic.Endpoint == "" {
		cfg.Cache.L2Semantic.Endpoint = "http://localhost:11434"
	}
	if cfg.Cache.Synonym.DataDir == "" {
		cfg.Cache.Synonym.DataDir = "./data"
	}
	if cfg.Cache.Synonym.PromotionThreshold == 0 {
		cfg.Cache.Synonym.PromotionThreshold = 3
	}
	if cfg.Workflow.TTL == 0 {
		cfg.Workflow.TTL = 1 * time.Hour
	}
	if cfg.Workflow.MaxSteps == 0 {
		cfg.Workflow.MaxSteps = 100
	}
	if cfg.Telemetry.MetricsPort == 0 {
		cfg.Telemetry.MetricsPort = 9090
	}
	if cfg.Telemetry.LogLevel == "" {
		cfg.Telemetry.LogLevel = "info"
	}
	if cfg.Telemetry.LogFormat == "" {
		cfg.Telemetry.LogFormat = "json"
	}
	// Tracing defaults
	if cfg.Tracing.ServiceName == "" {
		cfg.Tracing.ServiceName = "nexus-gateway"
	}
	if cfg.Tracing.SampleRate == 0 {
		cfg.Tracing.SampleRate = 1.0
	}
	// Billing defaults
	if cfg.Billing.DataDir == "" {
		cfg.Billing.DataDir = "./data/billing"
	}
	if cfg.Billing.DefaultPlan == "" {
		cfg.Billing.DefaultPlan = "free"
	}
	// Notification defaults
	if cfg.Notification.SMTPPort == 0 {
		cfg.Notification.SMTPPort = 587
	}
	if cfg.Notification.FromEmail == "" {
		cfg.Notification.FromEmail = "noreply@nexus-gateway.com"
	}
	if cfg.Notification.FromName == "" {
		cfg.Notification.FromName = "Nexus Gateway"
	}
	// Security hardening defaults
	if cfg.Security.BodySizeLimit == 0 {
		cfg.Security.BodySizeLimit = 1 << 20 // 1MB
	}
	if cfg.Security.RequestTimeout == "" {
		cfg.Security.RequestTimeout = "30s"
	}
	// Compression defaults
	if cfg.Compression.MaxHistoryTurns == 0 {
		cfg.Compression.MaxHistoryTurns = 20
	}
	if cfg.Compression.PreserveLastN == 0 {
		cfg.Compression.PreserveLastN = 5
	}
	// Cascade defaults
	if cfg.Cascade.ConfidenceThreshold == 0 {
		cfg.Cascade.ConfidenceThreshold = 0.78
	}
	if cfg.Cascade.MaxLatencyMs == 0 {
		cfg.Cascade.MaxLatencyMs = 5000
	}
	if cfg.Cascade.SampleRate == 0 {
		cfg.Cascade.SampleRate = 1.0
	}
	// Eval defaults
	if cfg.Eval.DataDir == "" {
		cfg.Eval.DataDir = "./data/eval"
	}
	if cfg.Eval.HedgingPenalty == 0 {
		cfg.Eval.HedgingPenalty = 0.15
	}
	if cfg.Eval.SampleRate == 0 {
		cfg.Eval.SampleRate = 1.0
	}
	if cfg.Eval.ShadowSampleRate == 0 && cfg.Eval.ShadowEnabled {
		cfg.Eval.ShadowSampleRate = 0.10
	}
	// Adaptive defaults
	if cfg.Adaptive.MinSamples == 0 {
		cfg.Adaptive.MinSamples = 50
	}
	if cfg.Adaptive.HighConfidence == 0 {
		cfg.Adaptive.HighConfidence = 0.90
	}
	if cfg.Adaptive.LowConfidence == 0 {
		cfg.Adaptive.LowConfidence = 0.50
	}
}
