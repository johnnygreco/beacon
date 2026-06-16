package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("Server.Host = %q, want 127.0.0.1", cfg.Server.Host)
	}
	if cfg.Server.Port != 4600 {
		t.Errorf("Server.Port = %d, want 4600", cfg.Server.Port)
	}
	if cfg.Database.Database != "beacon" || cfg.Database.ReadPoolSize != 8 {
		t.Errorf("Database defaults = %#v", cfg.Database)
	}
	if !cfg.Capture.Enabled || cfg.Capture.DebounceMs != 50 || !cfg.Capture.BackfillOnStart || cfg.Capture.BackfillWorkers != 4 {
		t.Errorf("Capture defaults = %#v", cfg.Capture)
	}
	if cfg.SSE.SubscriberBuffer != 64 {
		t.Errorf("SSE.SubscriberBuffer = %d, want 64", cfg.SSE.SubscriberBuffer)
	}
	if cfg.Search.MaxResults != 25 {
		t.Errorf("Search.MaxResults = %d, want 25", cfg.Search.MaxResults)
	}
	if cfg.Pricing.DefaultInputCost != 3.0 || cfg.Pricing.DefaultOutputCost != 15.0 {
		t.Errorf("Pricing defaults = %#v", cfg.Pricing)
	}
	if cfg.MCP.MaxResults != 25 || cfg.MCP.ContextWindow != 3 {
		t.Errorf("MCP defaults = %#v", cfg.MCP)
	}
	if cfg.Dashboard.Name != "" {
		t.Errorf("Dashboard.Name = %q, want empty", cfg.Dashboard.Name)
	}
	if len(cfg.Redaction.PathMasks) != 0 || len(cfg.Redaction.LiteralMasks) != 0 {
		t.Errorf("Redaction path/literal defaults = %#v", cfg.Redaction)
	}
	if !containsString(cfg.Redaction.EnvMasks, "OPENAI_API_KEY") {
		t.Errorf("Redaction.EnvMasks = %#v, want common credential env defaults", cfg.Redaction.EnvMasks)
	}
	if len(cfg.Capture.Sources) != 5 {
		t.Fatalf("Capture.Sources has %d entries, want 5", len(cfg.Capture.Sources))
	}
	if cfg.Capture.Sources[0].Name != "claude" || cfg.Capture.Sources[1].Name != "codex" {
		t.Errorf("Capture.Sources starts with %#v", cfg.Capture.Sources[:2])
	}
}

func TestLoadCustomConfigFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpFile := filepath.Join(t.TempDir(), "beacon.toml")
	if err := os.WriteFile(tmpFile, []byte(`
[server]
host = "127.0.0.1"
port = 8080

[database]
addrs = ["clickhouse.internal:9440"]
database = "beacon_custom"
read_pool_size = 12

[capture]
reconcile_interval = "45s"

[dashboard]
name = " Workstation A "

[redaction]
path_masks = [" ~/private/beacon "]
env_masks = [" BEACON_CUSTOM_SECRET "]
literal_masks = [" literal-fixture-secret "]

[[capture.sources]]
name = "custom-codex"
runtime = "codex"
provider = "openai"
glob = "/tmp/codex/**/*.jsonl"
watch_root = "/tmp/codex"
format = "jsonl"
`), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Load(%q) returned error: %v", tmpFile, err)
	}

	if cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != 8080 {
		t.Errorf("Server = %#v", cfg.Server)
	}
	if len(cfg.Database.Addrs) != 1 || cfg.Database.Addrs[0] != "clickhouse.internal:9440" {
		t.Fatalf("Database.Addrs = %v, want custom addr", cfg.Database.Addrs)
	}
	if cfg.Database.Database != "beacon_custom" || cfg.Database.ReadPoolSize != 12 {
		t.Errorf("Database = %#v", cfg.Database)
	}
	if len(cfg.Capture.Sources) != 1 || cfg.Capture.Sources[0].Name != "custom-codex" {
		t.Fatalf("Capture.Sources = %#v, want custom source only", cfg.Capture.Sources)
	}
	if cfg.Dashboard.Name != "Workstation A" {
		t.Errorf("Dashboard.Name = %q, want Workstation A", cfg.Dashboard.Name)
	}
	if len(cfg.Redaction.PathMasks) != 1 || cfg.Redaction.PathMasks[0] != filepath.Join(os.Getenv("HOME"), "private", "beacon") {
		t.Errorf("Redaction.PathMasks = %#v, want expanded clean path", cfg.Redaction.PathMasks)
	}
	if len(cfg.Redaction.EnvMasks) != 1 || cfg.Redaction.EnvMasks[0] != "BEACON_CUSTOM_SECRET" {
		t.Errorf("Redaction.EnvMasks = %#v, want trimmed custom env mask", cfg.Redaction.EnvMasks)
	}
	if len(cfg.Redaction.LiteralMasks) != 1 || cfg.Redaction.LiteralMasks[0] != "literal-fixture-secret" {
		t.Errorf("Redaction.LiteralMasks = %#v, want trimmed custom literal mask", cfg.Redaction.LiteralMasks)
	}
}

func TestLoadDefaultCaptureSources(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	want := []SourceConfig{
		{Name: "claude", Runtime: "claude-code", Provider: "anthropic", Glob: "~/.claude/projects/**/*.jsonl", WatchRoot: "~/.claude/projects", Format: "jsonl"},
		{Name: "codex", Runtime: "codex", Provider: "openai", Glob: "~/.codex/sessions/**/*.jsonl", WatchRoot: "~/.codex/sessions", Format: "jsonl"},
		{Name: "hermes", Runtime: "hermes-agent", Provider: "multi", Glob: "~/.hermes/state.db", WatchRoot: "~/.hermes", Format: "sqlite"},
		{Name: "opencode", Runtime: "opencode", Provider: "multi", Glob: "~/.local/share/opencode/opencode*.db", WatchRoot: "~/.local/share/opencode", Format: "sqlite"},
		{Name: "pi", Runtime: "pi-coding-agent", Provider: "multi", Glob: "~/.pi/agent/sessions/**/*.jsonl", WatchRoot: "~/.pi/agent/sessions", Format: "jsonl"},
	}
	if len(cfg.Capture.Sources) != len(want) {
		t.Fatalf("Capture.Sources has %d entries, want %d", len(cfg.Capture.Sources), len(want))
	}
	for i, expected := range want {
		got := cfg.Capture.Sources[i]
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("Sources[%d] = %#v, want %#v", i, got, expected)
		}
	}
}

func TestLoadRepositoryExampleConfig(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "beacon.toml"))
	if err != nil {
		t.Fatalf("Load repository beacon.toml: %v", err)
	}
	if len(cfg.Capture.Sources) != 5 {
		t.Fatalf("Capture.Sources has %d entries, want 5", len(cfg.Capture.Sources))
	}
}

func TestLoadCustomFilesDoNotBleedState(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.toml")
	second := filepath.Join(dir, "second.toml")
	if err := os.WriteFile(first, []byte(`
[server]
host = "127.0.0.1"
port = 7777
`), 0644); err != nil {
		t.Fatalf("write first config: %v", err)
	}
	if err := os.WriteFile(second, []byte(`
[server]
host = "127.0.0.2"
`), 0644); err != nil {
		t.Fatalf("write second config: %v", err)
	}

	firstCfg, err := Load(first)
	if err != nil {
		t.Fatalf("Load(first): %v", err)
	}
	secondCfg, err := Load(second)
	if err != nil {
		t.Fatalf("Load(second): %v", err)
	}
	if firstCfg.Server.Port != 7777 {
		t.Fatalf("first port = %d, want 7777", firstCfg.Server.Port)
	}
	if secondCfg.Server.Port != 4600 {
		t.Fatalf("second port = %d, want default 4600 without bleed", secondCfg.Server.Port)
	}
}

func TestLoadExplicitMissingConfigFileFails(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err == nil {
		t.Fatal("Load missing explicit config returned nil error")
	}
}

func TestLoadInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "server port", body: "[server]\nport = 0\n", wantErr: "server.port must be between 1 and 65535"},
		{name: "database address", body: "[database]\naddrs = [\"127.0.0.1\"]\n", wantErr: "database.addrs[0] must be host:port"},
		{name: "database name", body: "[database]\ndatabase = \"beacon-prod\"\n", wantErr: "database.database"},
		{name: "reconcile duration", body: "[capture]\nreconcile_interval = \"0s\"\n", wantErr: "capture.reconcile_interval must be positive"},
		{name: "debounce", body: "[capture]\ndebounce_ms = 0\n", wantErr: "capture.debounce_ms must be positive"},
		{name: "workers", body: "[capture]\nbackfill_workers = 0\n", wantErr: "capture.backfill_workers must be positive"},
		{name: "search max results", body: "[search]\nmax_results = -1\n", wantErr: "search.max_results must be positive"},
		{name: "mcp context", body: "[mcp]\ncontext_window = -1\n", wantErr: "mcp.context_window must be non-negative"},
		{name: "dashboard name too long", body: "[dashboard]\nname = \"" + strings.Repeat("x", DashboardNameMaxLength+1) + "\"\n", wantErr: "dashboard.name must be <= 80 characters"},
		{name: "unknown config key", body: "[unknown]\nvalue = true\n", wantErr: "invalid keys"},
		{
			name: "unsupported source pair",
			body: `
[[capture.sources]]
name = "bad"
runtime = "codex"
provider = "openai"
glob = "/tmp/**/*.jsonl"
watch_root = "/tmp"
format = "sqlite"
`,
			wantErr: `capture.sources[0] runtime/format "codex"/"sqlite" is unsupported`,
		},
		{
			name: "missing source glob",
			body: `
[[capture.sources]]
name = "bad"
runtime = "codex"
provider = "openai"
watch_root = "/tmp"
format = "jsonl"
`,
			wantErr: "capture.sources[0] must set glob or globs",
		},
		{
			name: "missing source watch root",
			body: `
[[capture.sources]]
name = "bad"
runtime = "codex"
provider = "openai"
glob = "/tmp/**/*.jsonl"
format = "jsonl"
`,
			wantErr: "capture.sources[0].watch_root is required",
		},
		{
			name: "duplicate source name",
			body: `
[[capture.sources]]
name = "dup"
runtime = "codex"
provider = "openai"
glob = "/tmp/a/**/*.jsonl"
watch_root = "/tmp/a"
format = "jsonl"

[[capture.sources]]
name = "dup"
runtime = "claude-code"
provider = "anthropic"
glob = "/tmp/b/**/*.jsonl"
watch_root = "/tmp/b"
format = "jsonl"
`,
			wantErr: `capture.sources[1].name "dup" is duplicated`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "beacon.toml")
			if err := os.WriteFile(path, []byte(tt.body), 0644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil {
				t.Fatal("Load returned nil error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateInvalidFields(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "nil", mutate: nil, wantErr: "config is nil"},
		{name: "server host", mutate: func(c *Config) { c.Server.Host = " " }, wantErr: "server.host is required"},
		{name: "empty addrs", mutate: func(c *Config) { c.Database.Addrs = nil }, wantErr: "database.addrs must contain"},
		{name: "empty addr", mutate: func(c *Config) { c.Database.Addrs = []string{" "} }, wantErr: "database.addrs[0] is required"},
		{name: "addr host", mutate: func(c *Config) { c.Database.Addrs = []string{":9000"} }, wantErr: "database.addrs[0] host is required"},
		{name: "addr port numeric", mutate: func(c *Config) { c.Database.Addrs = []string{"127.0.0.1:nope"} }, wantErr: "database.addrs[0] port must be numeric"},
		{name: "addr port range", mutate: func(c *Config) { c.Database.Addrs = []string{"127.0.0.1:0"} }, wantErr: "database.addrs[0] port must be between"},
		{name: "database required", mutate: func(c *Config) { c.Database.Database = " " }, wantErr: "database.database is required"},
		{name: "read pool too high", mutate: func(c *Config) { c.Database.ReadPoolSize = 2048 }, wantErr: "database.read_pool_size must be <= 1024"},
		{name: "debounce negative", mutate: func(c *Config) { c.Capture.DebounceMs = -1 }, wantErr: "capture.debounce_ms must be positive"},
		{name: "workers too high", mutate: func(c *Config) { c.Capture.BackfillWorkers = 300 }, wantErr: "capture.backfill_workers must be <= 256"},
		{name: "source name", mutate: func(c *Config) { c.Capture.Sources[0].Name = "" }, wantErr: "capture.sources[0].name is required"},
		{name: "source provider", mutate: func(c *Config) { c.Capture.Sources[0].Provider = " " }, wantErr: "capture.sources[0].provider is required"},
		{name: "source empty globs", mutate: func(c *Config) {
			c.Capture.Sources[0].Glob = ""
			c.Capture.Sources[0].Globs = []string{" "}
		}, wantErr: "capture.sources[0].globs[0] is required"},
		{name: "sse buffer", mutate: func(c *Config) { c.SSE.SubscriberBuffer = 0 }, wantErr: "sse.subscriber_buffer must be positive"},
		{name: "sse buffer high", mutate: func(c *Config) { c.SSE.SubscriberBuffer = 100001 }, wantErr: "sse.subscriber_buffer must be <= 100000"},
		{name: "search high", mutate: func(c *Config) { c.Search.MaxResults = 10001 }, wantErr: "search.max_results must be <= 10000"},
		{name: "search interval", mutate: func(c *Config) { c.Search.RebuildInterval = 0 }, wantErr: "search.rebuild_interval must be positive"},
		{name: "input pricing", mutate: func(c *Config) { c.Pricing.DefaultInputCost = -0.1 }, wantErr: "pricing.default_input_cost must be non-negative"},
		{name: "output pricing", mutate: func(c *Config) { c.Pricing.DefaultOutputCost = -0.1 }, wantErr: "pricing.default_output_cost must be non-negative"},
		{name: "mcp max high", mutate: func(c *Config) { c.MCP.MaxResults = 10001 }, wantErr: "mcp.max_results must be <= 10000"},
		{name: "mcp context high", mutate: func(c *Config) { c.MCP.ContextWindow = 1001 }, wantErr: "mcp.context_window must be <= 1000"},
		{name: "dashboard name high", mutate: func(c *Config) { c.Dashboard.Name = strings.Repeat("x", DashboardNameMaxLength+1) }, wantErr: "dashboard.name must be <= 80 characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.mutate == nil {
				err = Validate(nil)
			} else {
				cfg := validTestConfig()
				tt.mutate(&cfg)
				err = Validate(&cfg)
			}
			if err == nil {
				t.Fatal("Validate returned nil error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateNormalizesTrimmedFields(t *testing.T) {
	cfg := validTestConfig()
	cfg.Server.Host = " 127.0.0.1 "
	cfg.Database.Addrs = []string{" 127.0.0.1:9000 "}
	cfg.Database.Database = " beacon_test "
	cfg.Capture.Sources = []SourceConfig{
		{
			Name:      " codex ",
			Runtime:   " codex ",
			Provider:  " openai ",
			Format:    " jsonl ",
			Globs:     []string{" /tmp/codex/**/*.jsonl "},
			WatchRoot: " /tmp/codex ",
		},
	}
	cfg.Dashboard.Name = "  Workstation\n\tA  "

	if err := Validate(&cfg); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if cfg.Server.Host != "127.0.0.1" || cfg.Database.Addrs[0] != "127.0.0.1:9000" || cfg.Database.Database != "beacon_test" {
		t.Fatalf("top-level fields not normalized: %#v", cfg)
	}
	source := cfg.Capture.Sources[0]
	if source.Name != "codex" || source.Runtime != "codex" || source.Provider != "openai" ||
		source.Format != "jsonl" || source.Globs[0] != "/tmp/codex/**/*.jsonl" || source.WatchRoot != "/tmp/codex" {
		t.Fatalf("source fields not normalized: %#v", source)
	}
	if cfg.Dashboard.Name != "Workstation A" {
		t.Fatalf("dashboard name not normalized: %q", cfg.Dashboard.Name)
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func validTestConfig() Config {
	return Config{
		Server: ServerConfig{Host: "127.0.0.1", Port: 4600},
		Database: DatabaseConfig{
			Addrs:        []string{"127.0.0.1:9000"},
			Database:     "beacon",
			Username:     "default",
			ReadPoolSize: 8,
		},
		Capture: CaptureConfig{
			Enabled:           true,
			DebounceMs:        50,
			ReconcileInterval: 30,
			BackfillOnStart:   true,
			BackfillWorkers:   4,
			Sources:           DefaultCaptureSources(),
		},
		SSE:       SSEConfig{SubscriberBuffer: 64},
		Search:    SearchConfig{MaxResults: 25, RebuildInterval: 5},
		Pricing:   PricingConfig{DefaultInputCost: 3, DefaultOutputCost: 15},
		MCP:       MCPConfig{MaxResults: 25, ContextWindow: 3},
		Dashboard: DashboardConfig{},
	}
}
