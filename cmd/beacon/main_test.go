package main

import (
	"bytes"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/spf13/cobra"
)

func TestRootCommandShowsHelpWithoutSubcommand(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if !strings.Contains(out.String(), "Available Commands:") {
		t.Fatalf("expected help output, got %q", out.String())
	}
}

func TestRootCommandExposesCanonicalSubcommands(t *testing.T) {
	cmd := newRootCmd()
	want := []string{"up", "down", "watch", "mcp", "status", "db"}

	var got []string
	for _, sub := range cmd.Commands() {
		if sub.Hidden {
			continue
		}
		got = append(got, sub.Name())
	}

	for _, name := range want {
		if !slices.Contains(got, name) {
			t.Fatalf("missing canonical command %q; got %v", name, got)
		}
	}
	for _, removed := range []string{"serve", "stop", "run"} {
		if slices.Contains(got, removed) {
			t.Fatalf("removed duplicate command %q is still exposed; got %v", removed, got)
		}
	}
}

func TestRemovedDuplicateCommandsReturnErrors(t *testing.T) {
	tests := [][]string{
		{"serve"},
		{"stop"},
		{"run"},
		{"run", "web"},
		{"run", "capture"},
		{"run", "mcp"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(args)

			if err := cmd.Execute(); err == nil {
				t.Fatalf("expected %q to be unavailable", strings.Join(args, " "))
			}
		})
	}
}

func TestCommandTreeDoesNotExposeAliases(t *testing.T) {
	var inspect func(path []string, cmd *cobra.Command)
	inspect = func(path []string, cmd *cobra.Command) {
		name := cmd.Name()
		if name != "" {
			path = append(path, name)
		}
		if len(cmd.Aliases) > 0 {
			t.Fatalf("%s exposes aliases %v", strings.Join(path, " "), cmd.Aliases)
		}
		for _, sub := range cmd.Commands() {
			inspect(path, sub)
		}
	}

	inspect(nil, newRootCmd())
}

func TestStoreOptionsFromConfigMapsDatabaseSettings(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Addrs = []string{"clickhouse.internal:9440"}
	cfg.Database.Database = "beacon_test"
	cfg.Database.Username = "writer"
	cfg.Database.Password = "secret"
	cfg.Database.Secure = true
	cfg.Database.ReadPoolSize = 17

	opts := storeOptionsFromConfig(cfg)
	if !slices.Equal(opts.Addrs, cfg.Database.Addrs) {
		t.Fatalf("Addrs = %v, want %v", opts.Addrs, cfg.Database.Addrs)
	}
	if opts.Database != cfg.Database.Database {
		t.Fatalf("Database = %q, want %q", opts.Database, cfg.Database.Database)
	}
	if opts.Username != cfg.Database.Username {
		t.Fatalf("Username = %q, want %q", opts.Username, cfg.Database.Username)
	}
	if opts.Password != cfg.Database.Password {
		t.Fatalf("Password = %q, want %q", opts.Password, cfg.Database.Password)
	}
	if !opts.Secure {
		t.Fatal("Secure = false, want true")
	}
	if opts.ReadPoolSize != cfg.Database.ReadPoolSize {
		t.Fatalf("ReadPoolSize = %d, want %d", opts.ReadPoolSize, cfg.Database.ReadPoolSize)
	}
}

func TestStoreOptionsFromConfigKeepsDefaultsForEmptyConfig(t *testing.T) {
	defaults := store.DefaultOptions()

	opts := storeOptionsFromConfig(&config.Config{})
	if !slices.Equal(opts.Addrs, defaults.Addrs) {
		t.Fatalf("Addrs = %v, want %v", opts.Addrs, defaults.Addrs)
	}
	if opts.Database != defaults.Database {
		t.Fatalf("Database = %q, want %q", opts.Database, defaults.Database)
	}
	if opts.Username != defaults.Username {
		t.Fatalf("Username = %q, want %q", opts.Username, defaults.Username)
	}
	if opts.ReadPoolSize != defaults.ReadPoolSize {
		t.Fatalf("ReadPoolSize = %d, want %d", opts.ReadPoolSize, defaults.ReadPoolSize)
	}
}

func TestBuildSourcesMapsRuntimeParsersAndGlobs(t *testing.T) {
	cfg := &config.Config{}
	cfg.Capture.Sources = []config.SourceConfig{
		{
			Name:      "codex",
			Runtime:   "codex",
			Provider:  "openai",
			Format:    "jsonl",
			Globs:     []string{"~/.codex/sessions/**/*.jsonl"},
			Glob:      "~/.codex/latest.jsonl",
			WatchRoot: "~/.codex",
		},
		{Name: "hermes", Runtime: "hermes-agent", WatchRoot: "~/.hermes"},
		{Name: "opencode", Runtime: "opencode", WatchRoot: "~/.local/share/opencode"},
		{Name: "pi", Runtime: "pi-coding-agent", WatchRoot: "~/.pi"},
		{Name: "claude", Runtime: "claude-code", WatchRoot: "~/.claude"},
	}

	sources := buildSources(cfg)
	if len(sources) != len(cfg.Capture.Sources) {
		t.Fatalf("sources length = %d, want %d", len(sources), len(cfg.Capture.Sources))
	}

	codex := sources[0]
	if codex.Parser == nil || codex.FileParser != nil {
		t.Fatal("codex source should use a line parser only")
	}
	if !slices.Equal(codex.Globs, []string{"~/.codex/sessions/**/*.jsonl", "~/.codex/latest.jsonl"}) {
		t.Fatalf("codex globs = %v", codex.Globs)
	}
	if !slices.Equal(codex.WatchRoots, []string{"~/.codex"}) {
		t.Fatalf("codex watch roots = %v", codex.WatchRoots)
	}

	for _, idx := range []int{1, 2, 3} {
		if sources[idx].FileParser == nil || sources[idx].Parser != nil {
			t.Fatalf("%s source should use a file parser only", sources[idx].Name)
		}
	}
	if sources[4].Parser == nil || sources[4].FileParser != nil {
		t.Fatal("claude source should default to a line parser only")
	}
}
