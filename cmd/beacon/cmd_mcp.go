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
		return fmt.Errorf("loading config: %w", err)
	}

	opts := storeOptionsFromConfig(cfg)
	if mcpClickHouseAddr != "" {
		opts.Addrs = []string{mcpClickHouseAddr}
	}

	maxResults := cfg.MCP.MaxResults
	if maxResults <= 0 {
		maxResults = 25
	}

	backend := mcp.NewClickHouseBackend(opts, logger, maxResults)
	defer backend.Close()

	server := mcp.NewServerWithBackend(backend, logger)
	server.SetDefaultContextWindow(cfg.MCP.ContextWindow)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bg := newBackgroundGroup(ctx, cancel, logger)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	bg.Go("signal handler", signalCancelWorker(sigCh, cancel, logger, "shutting down mcp..."))

	err = server.Run(ctx)
	cancel()
	bgErr := bg.Wait()
	return commandLifecycleError(err, bgErr)
}
