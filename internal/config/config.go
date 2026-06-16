package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-viper/mapstructure/v2"
	"github.com/johnnygreco/beacon/internal/models"
	"github.com/spf13/viper"
)

type Config struct {
	Server    ServerConfig
	Database  DatabaseConfig
	Capture   CaptureConfig
	SSE       SSEConfig
	Search    SearchConfig
	Pricing   PricingConfig
	MCP       MCPConfig
	Dashboard DashboardConfig
	Redaction RedactionConfig
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
	Globs     []string
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

const DashboardNameMaxLength = 80

type DashboardConfig struct {
	Name string
}

type RedactionConfig struct {
	PathMasks    []string `mapstructure:"path_masks"`
	EnvMasks     []string `mapstructure:"env_masks"`
	LiteralMasks []string `mapstructure:"literal_masks"`
}

func Load(cfgFile string) (*Config, error) {
	v := viper.New()
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.SetConfigName("beacon")
		v.SetConfigType("toml")
		if home, err := os.UserHomeDir(); err == nil {
			v.AddConfigPath(filepath.Join(home, ".beacon"))
		} else {
			v.AddConfigPath("$HOME/.beacon")
		}
	}

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		if cfgFile != "" {
			return nil, err
		}
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}
	var cfg Config
	if err := v.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.ErrorUnused = true
	}); err != nil {
		return nil, err
	}

	if len(cfg.Capture.Sources) == 0 {
		cfg.Capture.Sources = DefaultCaptureSources()
	}
	if err := Validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.host", "127.0.0.1")
	v.SetDefault("server.port", 4600)
	v.SetDefault("database.addrs", []string{"127.0.0.1:9000"})
	v.SetDefault("database.database", "beacon")
	v.SetDefault("database.username", "default")
	v.SetDefault("database.password", "")
	v.SetDefault("database.secure", false)
	v.SetDefault("database.read_pool_size", 8)
	v.SetDefault("capture.enabled", true)
	v.SetDefault("capture.debounce_ms", 50)
	v.SetDefault("capture.reconcile_interval", "30s")
	v.SetDefault("capture.backfill_on_start", true)
	v.SetDefault("capture.backfill_workers", 4)
	v.SetDefault("sse.subscriber_buffer", 64)
	v.SetDefault("search.max_results", 25)
	v.SetDefault("search.rebuild_interval", "5m")
	v.SetDefault("pricing.default_input_cost", 3.0)
	v.SetDefault("pricing.default_output_cost", 15.0)
	v.SetDefault("mcp.max_results", 25)
	v.SetDefault("mcp.context_window", 3)
	v.SetDefault("dashboard.name", "")
	v.SetDefault("redaction.path_masks", []string{})
	v.SetDefault("redaction.env_masks", []string{
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"GITHUB_TOKEN",
		"GH_TOKEN",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
	})
	v.SetDefault("redaction.literal_masks", []string{})
}

func DefaultCaptureSources() []SourceConfig {
	return []SourceConfig{
		{Name: "claude", Runtime: models.RuntimeClaudeCode, Provider: models.ProviderAnthropic, Glob: "~/.claude/projects/**/*.jsonl", WatchRoot: "~/.claude/projects", Format: models.FormatJSONL},
		{Name: "codex", Runtime: models.RuntimeCodex, Provider: models.ProviderOpenAI, Glob: "~/.codex/sessions/**/*.jsonl", WatchRoot: "~/.codex/sessions", Format: models.FormatJSONL},
		{Name: "hermes", Runtime: models.RuntimeHermesAgent, Provider: models.ProviderMulti, Glob: "~/.hermes/state.db", WatchRoot: "~/.hermes", Format: models.FormatSQLite},
		{Name: "opencode", Runtime: models.RuntimeOpenCode, Provider: models.ProviderMulti, Glob: "~/.local/share/opencode/opencode*.db", WatchRoot: "~/.local/share/opencode", Format: models.FormatSQLite},
		{Name: "pi", Runtime: models.RuntimePiCodingAgent, Provider: models.ProviderMulti, Glob: "~/.pi/agent/sessions/**/*.jsonl", WatchRoot: "~/.pi/agent/sessions", Format: models.FormatJSONL},
	}
}

type SourceRuntimeFormat struct {
	Runtime string
	Format  string
}

var supportedSourceRuntimeFormats = map[SourceRuntimeFormat]struct{}{
	{Runtime: models.RuntimeClaudeCode, Format: models.FormatJSONL}:    {},
	{Runtime: models.RuntimeCodex, Format: models.FormatJSONL}:         {},
	{Runtime: models.RuntimeHermesAgent, Format: models.FormatSQLite}:  {},
	{Runtime: models.RuntimeOpenCode, Format: models.FormatSQLite}:     {},
	{Runtime: models.RuntimePiCodingAgent, Format: models.FormatJSONL}: {},
}

func IsSupportedSourceRuntimeFormat(runtime, format string) bool {
	_, ok := supportedSourceRuntimeFormats[SourceRuntimeFormat{
		Runtime: strings.TrimSpace(runtime),
		Format:  strings.TrimSpace(format),
	}]
	return ok
}

func SupportedSourceRuntimeFormatPairs() []string {
	pairs := make([]string, 0, len(supportedSourceRuntimeFormats))
	for key := range supportedSourceRuntimeFormats {
		pairs = append(pairs, key.Runtime+"/"+key.Format)
	}
	sort.Strings(pairs)
	return pairs
}

var databaseNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	cfg.Server.Host = strings.TrimSpace(cfg.Server.Host)
	if cfg.Server.Host == "" {
		return fmt.Errorf("server.host is required")
	}
	if !IsLoopbackURLHost(cfg.Server.Host) {
		return fmt.Errorf("server.host %q is not loopback; Beacon supports local dashboards only", cfg.Server.Host)
	}
	if err := validatePort("server.port", cfg.Server.Port); err != nil {
		return err
	}

	if len(cfg.Database.Addrs) == 0 {
		return fmt.Errorf("database.addrs must contain at least one address")
	}
	for i, addr := range cfg.Database.Addrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			return fmt.Errorf("database.addrs[%d] is required", i)
		}
		if err := validateHostPort(fmt.Sprintf("database.addrs[%d]", i), addr); err != nil {
			return err
		}
		cfg.Database.Addrs[i] = addr
	}
	cfg.Database.Database = strings.TrimSpace(cfg.Database.Database)
	if cfg.Database.Database == "" {
		return fmt.Errorf("database.database is required")
	}
	if !databaseNamePattern.MatchString(cfg.Database.Database) {
		return fmt.Errorf("database.database %q must match %s", cfg.Database.Database, databaseNamePattern.String())
	}
	if cfg.Database.ReadPoolSize <= 0 {
		return fmt.Errorf("database.read_pool_size must be positive")
	}
	if cfg.Database.ReadPoolSize > 1024 {
		return fmt.Errorf("database.read_pool_size must be <= 1024")
	}

	if cfg.Capture.DebounceMs <= 0 {
		return fmt.Errorf("capture.debounce_ms must be positive")
	}
	if cfg.Capture.ReconcileInterval <= 0 {
		return fmt.Errorf("capture.reconcile_interval must be positive")
	}
	if cfg.Capture.BackfillWorkers <= 0 {
		return fmt.Errorf("capture.backfill_workers must be positive")
	}
	if cfg.Capture.BackfillWorkers > 256 {
		return fmt.Errorf("capture.backfill_workers must be <= 256")
	}
	if err := validateSources(cfg.Capture.Sources); err != nil {
		return err
	}

	if cfg.SSE.SubscriberBuffer <= 0 {
		return fmt.Errorf("sse.subscriber_buffer must be positive")
	}
	if cfg.SSE.SubscriberBuffer > 100000 {
		return fmt.Errorf("sse.subscriber_buffer must be <= 100000")
	}

	if cfg.Search.MaxResults <= 0 {
		return fmt.Errorf("search.max_results must be positive")
	}
	if cfg.Search.MaxResults > 10000 {
		return fmt.Errorf("search.max_results must be <= 10000")
	}
	if cfg.Search.RebuildInterval <= 0 {
		return fmt.Errorf("search.rebuild_interval must be positive")
	}

	if cfg.Pricing.DefaultInputCost < 0 {
		return fmt.Errorf("pricing.default_input_cost must be non-negative")
	}
	if cfg.Pricing.DefaultOutputCost < 0 {
		return fmt.Errorf("pricing.default_output_cost must be non-negative")
	}

	if cfg.MCP.MaxResults <= 0 {
		return fmt.Errorf("mcp.max_results must be positive")
	}
	if cfg.MCP.MaxResults > 10000 {
		return fmt.Errorf("mcp.max_results must be <= 10000")
	}
	if cfg.MCP.ContextWindow < 0 {
		return fmt.Errorf("mcp.context_window must be non-negative")
	}
	if cfg.MCP.ContextWindow > 1000 {
		return fmt.Errorf("mcp.context_window must be <= 1000")
	}

	cfg.Dashboard.Name = normalizeDashboardName(cfg.Dashboard.Name)
	if utf8.RuneCountInString(cfg.Dashboard.Name) > DashboardNameMaxLength {
		return fmt.Errorf("dashboard.name must be <= %d characters", DashboardNameMaxLength)
	}
	validateRedaction(&cfg.Redaction)
	return nil
}

func validateRedaction(redaction *RedactionConfig) {
	for i := range redaction.PathMasks {
		redaction.PathMasks[i] = strings.TrimSpace(redaction.PathMasks[i])
		if redaction.PathMasks[i] == "" {
			continue
		}
		redaction.PathMasks[i] = expandHomePath(redaction.PathMasks[i])
		if filepath.IsAbs(redaction.PathMasks[i]) {
			redaction.PathMasks[i] = filepath.Clean(redaction.PathMasks[i])
		}
	}
	for i := range redaction.EnvMasks {
		redaction.EnvMasks[i] = strings.TrimSpace(redaction.EnvMasks[i])
	}
	for i := range redaction.LiteralMasks {
		redaction.LiteralMasks[i] = strings.TrimSpace(redaction.LiteralMasks[i])
	}
}

func normalizeDashboardName(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			if unicode.IsSpace(r) {
				return ' '
			}
			return -1
		}
		return r
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

func validateSources(sources []SourceConfig) error {
	seen := map[string]struct{}{}
	for i := range sources {
		source := &sources[i]
		prefix := fmt.Sprintf("capture.sources[%d]", i)
		source.Name = strings.TrimSpace(source.Name)
		if source.Name == "" {
			return fmt.Errorf("%s.name is required", prefix)
		}
		if _, ok := seen[source.Name]; ok {
			return fmt.Errorf("%s.name %q is duplicated", prefix, source.Name)
		}
		seen[source.Name] = struct{}{}

		source.Runtime = strings.TrimSpace(source.Runtime)
		source.Format = strings.TrimSpace(source.Format)
		if !IsSupportedSourceRuntimeFormat(source.Runtime, source.Format) {
			return fmt.Errorf("%s runtime/format %q/%q is unsupported; supported runtime/format pairs: %s",
				prefix,
				source.Runtime,
				source.Format,
				strings.Join(SupportedSourceRuntimeFormatPairs(), ", "),
			)
		}

		source.Provider = strings.TrimSpace(source.Provider)
		if source.Provider == "" {
			return fmt.Errorf("%s.provider is required", prefix)
		}
		source.Glob = strings.TrimSpace(source.Glob)
		source.WatchRoot = strings.TrimSpace(source.WatchRoot)
		if source.WatchRoot == "" {
			return fmt.Errorf("%s.watch_root is required", prefix)
		}
		for j, glob := range source.Globs {
			source.Globs[j] = strings.TrimSpace(glob)
			if source.Globs[j] == "" {
				return fmt.Errorf("%s.globs[%d] is required", prefix, j)
			}
		}
		if source.Glob == "" && len(source.Globs) == 0 {
			return fmt.Errorf("%s must set glob or globs", prefix)
		}
	}
	return nil
}

func expandHomePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func validatePort(field string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", field)
	}
	return nil
}

func validateHostPort(field, value string) error {
	host, portStr, err := net.SplitHostPort(value)
	if err != nil {
		return fmt.Errorf("%s must be host:port: %w", field, err)
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("%s host is required", field)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("%s port must be numeric", field)
	}
	return validatePort(field+" port", port)
}

func IsLoopbackURLHost(hostport string) bool {
	host := strings.TrimSpace(hostport)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
