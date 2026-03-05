package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/technodrome-ai/technodrome/internal/config"
	"github.com/technodrome-ai/technodrome/internal/database"
	"github.com/technodrome-ai/technodrome/internal/ingestion"
)

func newWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Run the JSONL watcher only (headless, no web server)",
		RunE:  runWatch,
	}
}

func runWatch(cmd *cobra.Command, args []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	dbPath := resolveDBPath(cfg)

	db, err := database.Open(dbPath, cfg.Database.ReadPoolSize)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	batcher := ingestion.NewBatcher(
		db, 500, 2*time.Second,
		cfg.Pricing.DefaultInputCost, cfg.Pricing.DefaultOutputCost,
		nil, // no SSE notify
		logger,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go batcher.Run(ctx)

	sources := buildSources(cfg)
	watcher := ingestion.NewWatcher(
		sources, batcher.EventCh(), db, logger,
		time.Duration(cfg.Watch.DebounceMs)*time.Millisecond,
		cfg.Watch.ReconcileInterval,
	)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("shutting down watcher...")
		cancel()
	}()

	logger.Info("starting headless watcher")
	return watcher.Run(ctx)
}
