package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestLoad_Defaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(func() { viper.Reset() })

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
	viper.Reset()
	t.Cleanup(func() { viper.Reset() })

	tmpFile := filepath.Join(t.TempDir(), "beacon.toml")
	if err := os.WriteFile(tmpFile, []byte(`
[server]
host = "127.0.0.1"
port = 8080
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

	// Verify other defaults still apply
	if cfg.Database.ReadPoolSize != 8 {
		t.Errorf("Database.ReadPoolSize = %d, want %d (default)", cfg.Database.ReadPoolSize, 8)
	}
	if cfg.Capture.Enabled != true {
		t.Errorf("Capture.Enabled = %v, want true (default)", cfg.Capture.Enabled)
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
}

func TestLoad_DefaultCaptureSources(t *testing.T) {
	viper.Reset()
	t.Cleanup(func() { viper.Reset() })

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
