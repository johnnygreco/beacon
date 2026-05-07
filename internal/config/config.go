package config

import (
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Capture  CaptureConfig
	SSE      SSEConfig
	Search   SearchConfig
	Pricing  PricingConfig
	MCP      MCPConfig
}

type ServerConfig struct {
	Host string
	Port int
}

type DatabaseConfig struct {
	Addrs        []string
	Database     string
	Username     string
	Password     string
	Secure       bool
	ReadPoolSize int `mapstructure:"read_pool_size"`
}

type CaptureConfig struct {
	Enabled           bool
	DebounceMs        int           `mapstructure:"debounce_ms"`
	ReconcileInterval time.Duration `mapstructure:"reconcile_interval"`
	BackfillOnStart   bool          `mapstructure:"backfill_on_start"`
	BackfillWorkers   int           `mapstructure:"backfill_workers"`
	Sources           []SourceConfig
}

type SourceConfig struct {
	Name      string
	Runtime   string
	Provider  string
	Glob      string
	WatchRoot string `mapstructure:"watch_root"`
	Format    string
}

type SSEConfig struct {
	SubscriberBuffer int `mapstructure:"subscriber_buffer"`
}

type SearchConfig struct {
	MaxResults      int           `mapstructure:"max_results"`
	RebuildInterval time.Duration `mapstructure:"rebuild_interval"`
}

type PricingConfig struct {
	DefaultInputCost  float64 `mapstructure:"default_input_cost"`
	DefaultOutputCost float64 `mapstructure:"default_output_cost"`
}

type MCPConfig struct {
	MaxResults    int `mapstructure:"max_results"`
	ContextWindow int `mapstructure:"context_window"`
}

func Load(cfgFile string) (*Config, error) {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("beacon")
		viper.SetConfigType("toml")
		viper.AddConfigPath("$HOME/.beacon")
	}

	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 4600)
	viper.SetDefault("database.addrs", []string{"127.0.0.1:9000"})
	viper.SetDefault("database.database", "beacon")
	viper.SetDefault("database.username", "default")
	viper.SetDefault("database.password", "")
	viper.SetDefault("database.secure", false)
	viper.SetDefault("database.read_pool_size", 8)
	viper.SetDefault("capture.enabled", true)
	viper.SetDefault("capture.debounce_ms", 50)
	viper.SetDefault("capture.reconcile_interval", "30s")
	viper.SetDefault("capture.backfill_on_start", true)
	viper.SetDefault("capture.backfill_workers", 4)
	viper.SetDefault("sse.subscriber_buffer", 64)
	viper.SetDefault("search.max_results", 25)
	viper.SetDefault("search.rebuild_interval", "5m")
	viper.SetDefault("pricing.default_input_cost", 3.0)
	viper.SetDefault("pricing.default_output_cost", 15.0)
	viper.SetDefault("mcp.max_results", 25)
	viper.SetDefault("mcp.context_window", 3)

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	// Default capture sources if none configured
	if len(cfg.Capture.Sources) == 0 {
		cfg.Capture.Sources = []SourceConfig{
			{Name: "claude", Runtime: "claude-code", Provider: "anthropic", Glob: "~/.claude/projects/**/*.jsonl", WatchRoot: "~/.claude/projects", Format: "jsonl"},
			{Name: "codex", Runtime: "codex", Provider: "openai", Glob: "~/.codex/sessions/**/*.jsonl", WatchRoot: "~/.codex/sessions", Format: "jsonl"},
		}
	}

	return &cfg, nil
}
