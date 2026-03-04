package config

import (
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server    ServerConfig
	Database  DatabaseConfig
	Ingestion IngestionConfig
	SSE       SSEConfig
	Search    SearchConfig
	Pricing   PricingConfig
}

type ServerConfig struct {
	Host string
	Port int
}

type DatabaseConfig struct {
	Path         string
	ReadPoolSize int `mapstructure:"read_pool_size"`
}

type IngestionConfig struct {
	BatchSize    int           `mapstructure:"batch_size"`
	FlushInterval time.Duration `mapstructure:"flush_interval"`
	MaxBodyBytes  int64         `mapstructure:"max_body_bytes"`
}

type SSEConfig struct {
	SubscriberBuffer int `mapstructure:"subscriber_buffer"`
}

type SearchConfig struct {
	Provider   string
	Model      string
	Dimensions int
	Ollama     OllamaConfig
	OpenAI     OpenAIConfig
}

type OllamaConfig struct {
	URL string
}

type OpenAIConfig struct {
	APIKey     string `mapstructure:"api_key"`
	Model      string
	Dimensions int
}

type PricingConfig struct {
	DefaultInputCost  float64 `mapstructure:"default_input_cost"`
	DefaultOutputCost float64 `mapstructure:"default_output_cost"`
}

func Load(cfgFile string) (*Config, error) {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("technodrome")
		viper.SetConfigType("toml")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.config/technodrome")
	}

	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 4600)
	viper.SetDefault("database.path", "technodrome.duckdb")
	viper.SetDefault("database.read_pool_size", 4)
	viper.SetDefault("ingestion.batch_size", 500)
	viper.SetDefault("ingestion.flush_interval", "2s")
	viper.SetDefault("ingestion.max_body_bytes", 4194304)
	viper.SetDefault("sse.subscriber_buffer", 64)
	viper.SetDefault("search.provider", "ollama")
	viper.SetDefault("search.model", "nomic-embed-text")
	viper.SetDefault("search.dimensions", 768)
	viper.SetDefault("search.ollama.url", "http://localhost:11434")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
