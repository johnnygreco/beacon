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

	return runMCPServerLifecycle(ctxWithSignals(logger, "shutting down mcp..."), server.Run)
}

type mcpLifecycleContext struct {
	ctx    context.Context
	cancel context.CancelFunc
	bg     *backgroundGroup
	sigCh  chan os.Signal
}

func ctxWithSignals(logger *slog.Logger, message string) mcpLifecycleContext {
	ctx, cancel := context.WithCancel(context.Background())
	bg := newBackgroundGroup(ctx, cancel, logger)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	bg.Go("signal handler", signalCancelWorker(sigCh, cancel, logger, message))
	return mcpLifecycleContext{ctx: ctx, cancel: cancel, bg: bg, sigCh: sigCh}
}

func runMCPServerLifecycle(lifecycle mcpLifecycleContext, run func(context.Context) error) error {
	defer signal.Stop(lifecycle.sigCh)
	defer lifecycle.cancel()
	err := run(lifecycle.ctx)
	lifecycle.cancel()
	bgErr := lifecycle.bg.Wait()
	return commandLifecycleError(err, bgErr)
}
