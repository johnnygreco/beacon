package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/mcp"
	"github.com/johnnygreco/beacon/internal/search"

	_ "github.com/duckdb/duckdb-go/v2"
)

var mcpDBPath string

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start the MCP server (stdin/stdout JSON-RPC)",
		RunE:  runMCP,
	}
	cmd.Flags().StringVar(&mcpDBPath, "db", "", "database path (overrides config)")
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

	if mcpDBPath != "" {
		cfg.Database.Path = mcpDBPath
	}
	dbPath := resolveDBPath(cfg)

	// Open DB read-only
	db, err := sql.Open("duckdb", dbPath+"?access_mode=read_only")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	// Load FTS extension
	if _, err := db.Exec("INSTALL fts"); err != nil {
		logger.Debug("fts install", "error", err)
	}
	if _, err := db.Exec("LOAD fts"); err != nil {
		logger.Debug("fts load", "error", err)
	}

	maxResults := cfg.MCP.MaxResults
	if maxResults <= 0 {
		maxResults = 25
	}

	searcher := search.NewSearcher(db, logger, maxResults, 0)
	searcher.ProbeIndex()

	server := mcp.NewServer(db, searcher, logger)

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
