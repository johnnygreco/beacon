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
	if cfg.Database.ReadPoolSize != 4 {
		t.Errorf("Database.ReadPoolSize = %d, want %d", cfg.Database.ReadPoolSize, 4)
	}
	if cfg.Watch.Enabled != true {
		t.Errorf("Watch.Enabled = %v, want true", cfg.Watch.Enabled)
	}
	if cfg.Watch.DebounceMs != 50 {
		t.Errorf("Watch.DebounceMs = %d, want %d", cfg.Watch.DebounceMs, 50)
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

	if len(cfg.Watch.Sources) != 2 {
		t.Fatalf("Watch.Sources has %d entries, want 2", len(cfg.Watch.Sources))
	}
	if cfg.Watch.Sources[0].Name != "claude" {
		t.Errorf("Watch.Sources[0].Name = %q, want %q", cfg.Watch.Sources[0].Name, "claude")
	}
	if cfg.Watch.Sources[1].Name != "codex" {
		t.Errorf("Watch.Sources[1].Name = %q, want %q", cfg.Watch.Sources[1].Name, "codex")
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
	if cfg.Database.ReadPoolSize != 4 {
		t.Errorf("Database.ReadPoolSize = %d, want %d (default)", cfg.Database.ReadPoolSize, 4)
	}
	if cfg.Watch.Enabled != true {
		t.Errorf("Watch.Enabled = %v, want true (default)", cfg.Watch.Enabled)
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

func TestLoad_DefaultWatchSources(t *testing.T) {
	viper.Reset()
	t.Cleanup(func() { viper.Reset() })

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") returned error: %v", err)
	}

	if len(cfg.Watch.Sources) != 2 {
		t.Fatalf("Watch.Sources has %d entries, want 2", len(cfg.Watch.Sources))
	}

	claude := cfg.Watch.Sources[0]
	if claude.Name != "claude" {
		t.Errorf("Sources[0].Name = %q, want %q", claude.Name, "claude")
	}
	if claude.Provider != "anthropic" {
		t.Errorf("Sources[0].Provider = %q, want %q", claude.Provider, "anthropic")
	}
	if claude.Glob != "~/.claude/projects/**/*.jsonl" {
		t.Errorf("Sources[0].Glob = %q, want %q", claude.Glob, "~/.claude/projects/**/*.jsonl")
	}

	codex := cfg.Watch.Sources[1]
	if codex.Name != "codex" {
		t.Errorf("Sources[1].Name = %q, want %q", codex.Name, "codex")
	}
	if codex.Provider != "openai" {
		t.Errorf("Sources[1].Provider = %q, want %q", codex.Provider, "openai")
	}
	if codex.Glob != "~/.codex/sessions/**/*.jsonl" {
		t.Errorf("Sources[1].Glob = %q, want %q", codex.Glob, "~/.codex/sessions/**/*.jsonl")
	}
}
