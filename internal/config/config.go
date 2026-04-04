package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig      `yaml:"server"`
	Providers []ProviderConfig  `yaml:"providers"`
	Router    RouterConfig      `yaml:"router"`
	Cache     CacheConfig       `yaml:"cache"`
	Workflow  WorkflowConfig    `yaml:"workflow"`
	Telemetry TelemetryConfig   `yaml:"telemetry"`
	Tracing   TracingConfig     `yaml:"tracing"`
	Security  SecurityConfig    `yaml:"security"`
	Storage   StorageYAMLConfig `yaml:"storage"`
}

type SecurityConfig struct {
	TLS       TLSYAMLConfig         `yaml:"tls"`
	PromptGuard PromptGuardYAMLConfig `yaml:"prompt_guard"`
	OIDC      OIDCYAMLConfig        `yaml:"oidc"`
	RBAC      RBACYAMLConfig        `yaml:"rbac"`
	RateLimit RateLimitYAMLConfig   `yaml:"rate_limit"`
	CORS      CORSYAMLConfig        `yaml:"cors"`
	AuditLog  bool                  `yaml:"audit_log"`
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
	Port         int           `yaml:"port"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
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

func DefaultConfig() *Config {
	cfg := &Config{}
	setDefaults(cfg)
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
}
