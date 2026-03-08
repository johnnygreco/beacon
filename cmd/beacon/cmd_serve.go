package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/database"
	"github.com/johnnygreco/beacon/internal/ingestion"
	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/sse"
	"github.com/johnnygreco/beacon/internal/web"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the web dashboard and JSONL watcher",
		RunE:  runServe,
	}
}

func runServe(cmd *cobra.Command, args []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	dbPath := resolveDBPath(cfg)

	logger.Info("starting beacon",
		"host", cfg.Server.Host,
		"port", cfg.Server.Port,
		"db", dbPath,
	)

	db, err := database.Open(dbPath, cfg.Database.ReadPoolSize)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	broker := sse.NewBroker(cfg.SSE.SubscriberBuffer, logger)
	updater := web.NewUpdater(db.ReadPool, broker, logger)

	batcher := ingestion.NewBatcher(
		db,
		500,
		2*time.Second,
		cfg.Pricing.DefaultInputCost,
		cfg.Pricing.DefaultOutputCost,
		updater.NotifyDashboard,
		logger,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start batcher
	go batcher.Run(ctx)

	// Start watcher
	if cfg.Watch.Enabled {
		sources := buildSources(cfg)
		watcher := ingestion.NewWatcher(
			sources,
			batcher.EventCh(),
			db,
			logger,
			time.Duration(cfg.Watch.DebounceMs)*time.Millisecond,
			cfg.Watch.ReconcileInterval,
		)
		go watcher.Run(ctx)
	}

	// Start FTS indexer
	searcher := search.NewSearcher(db.ReadPool, logger, cfg.Search.MaxResults, cfg.Search.RebuildInterval)
	go searcher.RunIndexer(ctx)

	// Web server
	handlers := web.NewHandlers(db.ReadPool, searcher, logger)
	apiHandlers := web.NewAPIHandlers(db.ReadPool, searcher, logger)
	router := web.NewRouter(broker, handlers, apiHandlers)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("shutting down...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
		cancel()
	}()

	logger.Info("server listening", "addr", addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}

	logger.Info("server stopped")
	return nil
}

func buildSources(cfg *config.Config) []ingestion.WatchSource {
	var sources []ingestion.WatchSource
	for _, sc := range cfg.Watch.Sources {
		var parser func(line []byte, file string, lineNo int, offset int64) ([]ingestion.NormalizedEvent, error)
		switch sc.Name {
		case "codex":
			parser = ingestion.ParseCodexJSONL
		default:
			parser = ingestion.ParseClaudeJSONL
		}
		sources = append(sources, ingestion.WatchSource{
			Name:     sc.Name,
			Provider: sc.Provider,
			Globs:    []string{sc.Glob},
			Parser:   parser,
		})
	}
	return sources
}
