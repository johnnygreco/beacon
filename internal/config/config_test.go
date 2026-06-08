package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") returned error: %v", err)
	}

	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "0.0.0.0")
	}
	if cfg.Server.Port != 4600 {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 4600)
	}
	if cfg.Database.ReadPoolSize != 8 {
		t.Errorf("Database.ReadPoolSize = %d, want %d", cfg.Database.ReadPoolSize, 8)
	}
	if cfg.Database.Database != "beacon" {
		t.Errorf("Database.Database = %q, want beacon", cfg.Database.Database)
	}
	if cfg.Capture.Enabled != true {
		t.Errorf("Capture.Enabled = %v, want true", cfg.Capture.Enabled)
	}
	if cfg.Capture.DebounceMs != 50 {
		t.Errorf("Capture.DebounceMs = %d, want %d", cfg.Capture.DebounceMs, 50)
	}
	if cfg.Capture.BackfillOnStart != true {
		t.Errorf("Capture.BackfillOnStart = %v, want true", cfg.Capture.BackfillOnStart)
	}
	if cfg.Capture.BackfillWorkers != 4 {
		t.Errorf("Capture.BackfillWorkers = %d, want %d", cfg.Capture.BackfillWorkers, 4)
	}
	if cfg.SSE.SubscriberBuffer != 64 {
		t.Errorf("SSE.SubscriberBuffer = %d, want %d", cfg.SSE.SubscriberBuffer, 64)
	}
	if cfg.Search.MaxResults != 25 {
		t.Errorf("Search.MaxResults = %d, want %d", cfg.Search.MaxResults, 25)
	}
	if cfg.Pricing.DefaultInputCost != 3.0 {
		t.Errorf("Pricing.DefaultInputCost = %f, want %f", cfg.Pricing.DefaultInputCost, 3.0)
	}
	if cfg.Pricing.DefaultOutputCost != 15.0 {
		t.Errorf("Pricing.DefaultOutputCost = %f, want %f", cfg.Pricing.DefaultOutputCost, 15.0)
	}
	if cfg.MCP.MaxResults != 25 {
		t.Errorf("MCP.MaxResults = %d, want %d", cfg.MCP.MaxResults, 25)
	}
	if cfg.MCP.ContextWindow != 3 {
		t.Errorf("MCP.ContextWindow = %d, want %d", cfg.MCP.ContextWindow, 3)
	}
	if cfg.Dashboard.Name != "" {
		t.Errorf("Dashboard.Name = %q, want empty default", cfg.Dashboard.Name)
	}
	if cfg.Fleet.Role != FleetRoleBoth {
		t.Errorf("Fleet.Role = %q, want %q", cfg.Fleet.Role, FleetRoleBoth)
	}
	wantMetadataPath := filepath.Join(os.Getenv("HOME"), ".beacon", "control-plane.db")
	if cfg.Fleet.MetadataPath != wantMetadataPath {
		t.Errorf("Fleet.MetadataPath = %q, want %q", cfg.Fleet.MetadataPath, wantMetadataPath)
	}
	if cfg.Fleet.NodeName == "" {
		t.Error("Fleet.NodeName is empty, want default hostname/local value")
	}
	if cfg.Fleet.ControlPlaneURL != "" || cfg.Fleet.NodeID != "" || cfg.Fleet.CollectorID != "" {
		t.Errorf("Fleet defaults = %#v, want only role/metadata_path/node_name populated", cfg.Fleet)
	}

	if len(cfg.Capture.Sources) != 5 {
		t.Fatalf("Capture.Sources has %d entries, want 5", len(cfg.Capture.Sources))
	}
	if cfg.Capture.Sources[0].Name != "claude" {
		t.Errorf("Capture.Sources[0].Name = %q, want %q", cfg.Capture.Sources[0].Name, "claude")
	}
	if cfg.Capture.Sources[1].Name != "codex" {
		t.Errorf("Capture.Sources[1].Name = %q, want %q", cfg.Capture.Sources[1].Name, "codex")
	}
	if cfg.Capture.Sources[2].Name != "hermes" {
		t.Errorf("Capture.Sources[2].Name = %q, want %q", cfg.Capture.Sources[2].Name, "hermes")
	}
	if cfg.Capture.Sources[3].Name != "opencode" {
		t.Errorf("Capture.Sources[3].Name = %q, want %q", cfg.Capture.Sources[3].Name, "opencode")
	}
	if cfg.Capture.Sources[4].Name != "pi" {
		t.Errorf("Capture.Sources[4].Name = %q, want %q", cfg.Capture.Sources[4].Name, "pi")
	}
}

func TestLoad_CustomConfigFile(t *testing.T) {
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

[fleet]
role = "collector"
metadata_path = "~/custom-control-plane.db"
control_plane_url = "https://beacon.example"
node_id = "node.mac-mini"
node_name = " Mac Mini "
collector_id = "collector.mac-mini"

[[capture.sources]]
name = "custom-codex"
runtime = "codex"
provider = "openai"
glob = "/tmp/codex/**/*.jsonl"
watch_root = "/tmp/codex"
format = "jsonl"
`), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Load(%q) returned error: %v", tmpFile, err)
	}

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "127.0.0.1")
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 8080)
	}
	if len(cfg.Database.Addrs) != 1 || cfg.Database.Addrs[0] != "clickhouse.internal:9440" {
		t.Fatalf("Database.Addrs = %v, want custom addr", cfg.Database.Addrs)
	}
	if cfg.Database.Database != "beacon_custom" {
		t.Errorf("Database.Database = %q, want beacon_custom", cfg.Database.Database)
	}
	if cfg.Database.ReadPoolSize != 12 {
		t.Errorf("Database.ReadPoolSize = %d, want 12", cfg.Database.ReadPoolSize)
	}
	if len(cfg.Capture.Sources) != 1 || cfg.Capture.Sources[0].Name != "custom-codex" {
		t.Fatalf("Capture.Sources = %#v, want custom source only", cfg.Capture.Sources)
	}
	if cfg.SSE.SubscriberBuffer != 64 {
		t.Errorf("SSE.SubscriberBuffer = %d, want %d (default)", cfg.SSE.SubscriberBuffer, 64)
	}
	if cfg.Search.MaxResults != 25 {
		t.Errorf("Search.MaxResults = %d, want %d (default)", cfg.Search.MaxResults, 25)
	}
	if cfg.MCP.MaxResults != 25 {
		t.Errorf("MCP.MaxResults = %d, want %d (default)", cfg.MCP.MaxResults, 25)
	}
	if cfg.Dashboard.Name != "Workstation A" {
		t.Errorf("Dashboard.Name = %q, want %q", cfg.Dashboard.Name, "Workstation A")
	}
	if cfg.Fleet.Role != FleetRoleCollector {
		t.Errorf("Fleet.Role = %q, want %q", cfg.Fleet.Role, FleetRoleCollector)
	}
	if cfg.Fleet.MetadataPath != filepath.Join(os.Getenv("HOME"), "custom-control-plane.db") {
		t.Errorf("Fleet.MetadataPath = %q, want expanded custom path", cfg.Fleet.MetadataPath)
	}
	if cfg.Fleet.ControlPlaneURL != "https://beacon.example" {
		t.Errorf("Fleet.ControlPlaneURL = %q, want https://beacon.example", cfg.Fleet.ControlPlaneURL)
	}
	if cfg.Fleet.NodeID != "node.mac-mini" || cfg.Fleet.NodeName != "Mac Mini" || cfg.Fleet.CollectorID != "collector.mac-mini" {
		t.Errorf("Fleet identity = %#v, want trimmed custom identity", cfg.Fleet)
	}
}

func TestLoad_DefaultCaptureSources(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") returned error: %v", err)
	}

	if len(cfg.Capture.Sources) != 5 {
		t.Fatalf("Capture.Sources has %d entries, want 5", len(cfg.Capture.Sources))
	}

	want := []SourceConfig{
		{Name: "claude", Runtime: "claude-code", Provider: "anthropic", Glob: "~/.claude/projects/**/*.jsonl", WatchRoot: "~/.claude/projects", Format: "jsonl"},
		{Name: "codex", Runtime: "codex", Provider: "openai", Glob: "~/.codex/sessions/**/*.jsonl", WatchRoot: "~/.codex/sessions", Format: "jsonl"},
		{Name: "hermes", Runtime: "hermes-agent", Provider: "multi", Glob: "~/.hermes/state.db", WatchRoot: "~/.hermes", Format: "sqlite"},
		{Name: "opencode", Runtime: "opencode", Provider: "multi", Glob: "~/.local/share/opencode/opencode*.db", WatchRoot: "~/.local/share/opencode", Format: "sqlite"},
		{Name: "pi", Runtime: "pi-coding-agent", Provider: "multi", Glob: "~/.pi/agent/sessions/**/*.jsonl", WatchRoot: "~/.pi/agent/sessions", Format: "jsonl"},
	}
	for i, expected := range want {
		got := cfg.Capture.Sources[i]
		if got.Name != expected.Name {
			t.Errorf("Sources[%d].Name = %q, want %q", i, got.Name, expected.Name)
		}
		if got.Runtime != expected.Runtime {
			t.Errorf("Sources[%d].Runtime = %q, want %q", i, got.Runtime, expected.Runtime)
		}
		if got.Provider != expected.Provider {
			t.Errorf("Sources[%d].Provider = %q, want %q", i, got.Provider, expected.Provider)
		}
		if got.Glob != expected.Glob {
			t.Errorf("Sources[%d].Glob = %q, want %q", i, got.Glob, expected.Glob)
		}
		if got.WatchRoot != expected.WatchRoot {
			t.Errorf("Sources[%d].WatchRoot = %q, want %q", i, got.WatchRoot, expected.WatchRoot)
		}
		if got.Format != expected.Format {
			t.Errorf("Sources[%d].Format = %q, want %q", i, got.Format, expected.Format)
		}
	}
}

func TestLoad_RepositoryExampleConfig(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "beacon.toml"))
	if err != nil {
		t.Fatalf("Load repository beacon.toml: %v", err)
	}
	if len(cfg.Capture.Sources) != 5 {
		t.Fatalf("Capture.Sources has %d entries, want 5", len(cfg.Capture.Sources))
	}
}

func TestLoad_CustomFilesDoNotBleedState(t *testing.T) {
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

func TestLoad_ExplicitMissingConfigFileFails(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err == nil {
		t.Fatal("Load missing explicit config returned nil error")
	}
}

func TestLoad_InvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "server port",
			body:    "[server]\nport = 0\n",
			wantErr: "server.port must be between 1 and 65535",
		},
		{
			name:    "database address",
			body:    "[database]\naddrs = [\"127.0.0.1\"]\n",
			wantErr: "database.addrs[0] must be host:port",
		},
		{
			name:    "database name",
			body:    "[database]\ndatabase = \"beacon-prod\"\n",
			wantErr: "database.database",
		},
		{
			name:    "reconcile duration",
			body:    "[capture]\nreconcile_interval = \"0s\"\n",
			wantErr: "capture.reconcile_interval must be positive",
		},
		{
			name:    "debounce",
			body:    "[capture]\ndebounce_ms = 0\n",
			wantErr: "capture.debounce_ms must be positive",
		},
		{
			name:    "workers",
			body:    "[capture]\nbackfill_workers = 0\n",
			wantErr: "capture.backfill_workers must be positive",
		},
		{
			name:    "search max results",
			body:    "[search]\nmax_results = -1\n",
			wantErr: "search.max_results must be positive",
		},
		{
			name:    "mcp context",
			body:    "[mcp]\ncontext_window = -1\n",
			wantErr: "mcp.context_window must be non-negative",
		},
		{
			name:    "dashboard name too long",
			body:    "[dashboard]\nname = \"" + strings.Repeat("x", DashboardNameMaxLength+1) + "\"\n",
			wantErr: "dashboard.name must be <= 80 characters",
		},
		{
			name:    "fleet role",
			body:    "[fleet]\nrole = \"enterprise\"\n",
			wantErr: "fleet.role must be one of",
		},
		{
			name:    "fleet metadata path",
			body:    "[fleet]\nmetadata_path = \" \"\n",
			wantErr: "fleet.metadata_path is required",
		},
		{
			name:    "fleet collector url required",
			body:    "[fleet]\nrole = \"collector\"\n",
			wantErr: "fleet.control_plane_url is required",
		},
		{
			name:    "fleet collector url scheme",
			body:    "[fleet]\nrole = \"collector\"\ncontrol_plane_url = \"ssh://beacon.example\"\n",
			wantErr: "fleet.control_plane_url must use http or https",
		},
		{
			name:    "fleet node id",
			body:    "[fleet]\nnode_id = \"not valid\"\n",
			wantErr: "fleet.node_id",
		},
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

func TestValidate_InvalidFields(t *testing.T) {
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
		{name: "fleet collector id", mutate: func(c *Config) { c.Fleet.CollectorID = "bad id" }, wantErr: "fleet.collector_id"},
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

func TestValidate_NormalizesTrimmedFields(t *testing.T) {
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
	cfg.Fleet.Role = " both "
	cfg.Fleet.MetadataPath = " /tmp/control-plane.db "
	cfg.Fleet.NodeID = " node.local "
	cfg.Fleet.NodeName = "  Local\n\tNode  "
	cfg.Fleet.CollectorID = " collector.local "
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
	if cfg.Fleet.Role != FleetRoleBoth ||
		cfg.Fleet.MetadataPath != "/tmp/control-plane.db" ||
		cfg.Fleet.NodeID != "node.local" ||
		cfg.Fleet.NodeName != "Local Node" ||
		cfg.Fleet.CollectorID != "collector.local" {
		t.Fatalf("fleet fields not normalized: %#v", cfg.Fleet)
	}
}

func validTestConfig() Config {
	return Config{
		Server: ServerConfig{Host: "0.0.0.0", Port: 4600},
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
		SSE:     SSEConfig{SubscriberBuffer: 64},
		Search:  SearchConfig{MaxResults: 25, RebuildInterval: 5},
		Pricing: PricingConfig{DefaultInputCost: 3, DefaultOutputCost: 15},
		MCP:     MCPConfig{MaxResults: 25, ContextWindow: 3},
		Fleet: FleetConfig{
			Role:         FleetRoleBoth,
			MetadataPath: "/tmp/control-plane.db",
			NodeName:     "local",
		},
	}
}
