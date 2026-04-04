package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Providers []ProviderConfig `yaml:"providers"`
	Router    RouterConfig    `yaml:"router"`
	Cache     CacheConfig     `yaml:"cache"`
	Workflow  WorkflowConfig  `yaml:"workflow"`
	Telemetry TelemetryConfig `yaml:"telemetry"`
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
}
