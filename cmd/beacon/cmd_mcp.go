package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/mcp"
	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/spf13/cobra"
)

var mcpClickHouseAddr string

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start the MCP server (stdin/stdout JSON-RPC)",
		RunE:  runMCP,
	}
	cmd.Flags().StringVar(&mcpClickHouseAddr, "clickhouse", "", "ClickHouse address (overrides config)")
	return cmd
}

func runMCP(cmd *cobra.Command, args []string) error {
	// All logging to stderr (stdout is the transport)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cfg, err := config.Load(cfgFile)
	if err != nil {
		// Don't fail on missing config for MCP
		cfg = &config.Config{}
	}

	opts := storeOptionsFromConfig(cfg)
	if mcpClickHouseAddr != "" {
		opts.Addrs = []string{mcpClickHouseAddr}
	}
	ch, err := store.OpenReadOnly(context.Background(), opts)
	if err != nil {
		return fmt.Errorf("opening read-only clickhouse store: %w", err)
	}
	defer ch.Close()

	maxResults := cfg.MCP.MaxResults
	if maxResults <= 0 {
		maxResults = 25
	}

	searcher := search.NewSearcher(ch.DB, logger, maxResults, 0)
	searcher.ProbeIndex()

	server := mcp.NewServer(ch.DB, searcher, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	return server.Run(ctx)
}
