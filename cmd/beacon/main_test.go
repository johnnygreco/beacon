package main

import (
	"bytes"
	"io"
	"os"
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
	if !strings.Contains(out.String(), "default: ~/.beacon/beacon.toml") {
		t.Fatalf("expected default config path in help output, got %q", out.String())
	}
}

func TestRootCommandShowsVersion(t *testing.T) {
	oldVersion := version
	version = "1.2.3-test"
	t.Cleanup(func() {
		version = oldVersion
	})

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	want := "beacon version 1.2.3-test\n"
	if out.String() != want {
		t.Fatalf("version output = %q, want %q", out.String(), want)
	}
}

func TestRootCommandExposesCanonicalSubcommands(t *testing.T) {
	cmd := newRootCmd()
	want := []string{"init", "enroll", "up", "down", "collect", "watch", "mcp", "usage", "status", "db"}

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
		{Name: "hermes", Runtime: "hermes-agent", Format: "sqlite", WatchRoot: "~/.hermes"},
		{Name: "opencode", Runtime: "opencode", Format: "sqlite", WatchRoot: "~/.local/share/opencode"},
		{Name: "pi", Runtime: "pi-coding-agent", Format: "jsonl", WatchRoot: "~/.pi"},
		{Name: "claude", Runtime: "claude-code", Format: "jsonl", WatchRoot: "~/.claude"},
	}

	sources, err := buildSources(cfg)
	if err != nil {
		t.Fatalf("buildSources: %v", err)
	}
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
		t.Fatal("claude source should use a line parser only")
	}
}

func TestBuildSourcesCoversDefaultSources(t *testing.T) {
	cfg := &config.Config{}
	cfg.Capture.Sources = []config.SourceConfig{
		{Name: "claude", Runtime: "claude-code", Format: "jsonl"},
		{Name: "codex", Runtime: "codex", Format: "jsonl"},
		{Name: "hermes", Runtime: "hermes-agent", Format: "sqlite"},
		{Name: "opencode", Runtime: "opencode", Format: "sqlite"},
		{Name: "pi", Runtime: "pi-coding-agent", Format: "jsonl"},
	}

	sources, err := buildSources(cfg)
	if err != nil {
		t.Fatalf("buildSources default sources: %v", err)
	}
	for _, source := range sources {
		if source.Parser == nil && source.FileParser == nil {
			t.Fatalf("%s source has no parser", source.Name)
		}
		if source.Parser != nil && source.FileParser != nil {
			t.Fatalf("%s source has both line and file parsers", source.Name)
		}
	}
}

func TestParserRegistryMatchesConfigRuntimeFormats(t *testing.T) {
	var got []string
	for key := range captureParserRegistry {
		got = append(got, key.runtime+"/"+key.format)
	}
	slices.Sort(got)
	want := config.SupportedSourceRuntimeFormatPairs()
	if !slices.Equal(got, want) {
		t.Fatalf("parser registry pairs = %v, want config-supported pairs %v", got, want)
	}
}

func TestBuildSourcesRejectsUnsupportedRuntimeFormat(t *testing.T) {
	cfg := &config.Config{}
	cfg.Capture.Sources = []config.SourceConfig{
		{Name: "bad", Runtime: "mystery-agent", Format: "jsonl"},
	}

	_, err := buildSources(cfg)
	if err == nil {
		t.Fatal("buildSources returned nil error for unsupported source")
	}
	if !strings.Contains(err.Error(), `unsupported capture source "bad" runtime/format "mystery-agent"/"jsonl"`) ||
		!strings.Contains(err.Error(), "claude-code/jsonl") {
		t.Fatalf("error = %q, want unsupported source with supported pairs", err.Error())
	}
}

func TestRunWatchValidatesSourcesBeforeClickHouse(t *testing.T) {
	cfgPath := t.TempDir() + "/beacon.toml"
	if err := os.WriteFile(cfgPath, []byte(`
[database]
addrs = ["127.0.0.1:1"]

[[capture.sources]]
name = "bad"
runtime = "mystery-agent"
format = "jsonl"
glob = "/tmp/nope.jsonl"
watch_root = "/tmp"
`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	oldCfgFile := cfgFile
	cfgFile = cfgPath
	t.Cleanup(func() {
		cfgFile = oldCfgFile
	})

	err := runWatch(newWatchCmd(), nil)
	if err == nil {
		t.Fatal("runWatch returned nil error")
	}
	if !strings.Contains(err.Error(), "loading config") ||
		!strings.Contains(err.Error(), `capture.sources[0] runtime/format "mystery-agent"/"jsonl" is unsupported`) {
		t.Fatalf("runWatch error = %q, want config validation error", err.Error())
	}
	if strings.Contains(err.Error(), "clickhouse") {
		t.Fatalf("runWatch error = %q, validation should happen before ClickHouse", err.Error())
	}
}
