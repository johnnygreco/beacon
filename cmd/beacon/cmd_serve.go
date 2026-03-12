package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	beacon "github.com/johnnygreco/beacon"
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
		for _, s := range sources {
			logger.Info("watch source configured", "name", s.Name, "provider", s.Provider, "globs", s.Globs)
		}
		watcher := ingestion.NewWatcher(
			sources,
			batcher.EventCh(),
			db,
			logger,
			time.Duration(cfg.Watch.DebounceMs)*time.Millisecond,
			cfg.Watch.ReconcileInterval,
		)
		go func() {
			if err := watcher.Run(ctx); err != nil {
				logger.Error("watcher stopped", "error", err)
			}
		}()
	}

	// Start FTS indexer
	searcher := search.NewSearcher(db.ReadPool, logger, cfg.Search.MaxResults, cfg.Search.RebuildInterval)
	go searcher.RunIndexer(ctx)

	// Web server
	handlers := web.NewHandlers(db.ReadPool, searcher, logger, updater)
	apiHandlers := web.NewAPIHandlers(db.ReadPool, searcher, logger)
	staticFS, err := fs.Sub(beacon.StaticFS, "static")
	if err != nil {
		return fmt.Errorf("preparing static filesystem: %w", err)
	}
	router := web.NewRouter(staticFS, broker, handlers, apiHandlers)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	// Write pidfile
	pidPath := pidfilePath()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		logger.Warn("failed to write pidfile", "path", pidPath, "error", err)
	} else {
		defer os.Remove(pidPath)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("shutting down...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown error", "error", err)
		}
		cancel()
	}()

	logger.Info("server listening", "addr", addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}

	logger.Info("server stopped")
	return nil
}

// pidfilePath returns the path to the beacon pidfile.
func pidfilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/beacon.pid"
	}
	return filepath.Join(home, ".beacon", "beacon.pid")
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
