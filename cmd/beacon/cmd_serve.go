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

	beacon "github.com/johnnygreco/beacon"
	"github.com/johnnygreco/beacon/internal/capture"
	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/sse"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/johnnygreco/beacon/internal/web"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the web dashboard and capture service",
		RunE:  runServe,
	}
}

func runServe(cmd *cobra.Command, args []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	storeOpts := storeOptionsFromConfig(cfg)
	if err := ensureLocalClickHouse(storeOpts); err != nil {
		return fmt.Errorf("starting clickhouse: %w", err)
	}

	logger.Info("starting beacon",
		"host", cfg.Server.Host,
		"port", cfg.Server.Port,
		"clickhouse", storeOpts.Addrs,
		"database", storeOpts.Database,
	)

	ch, err := store.Open(context.Background(), storeOpts)
	if err != nil {
		return fmt.Errorf("opening clickhouse store: %w", err)
	}
	defer ch.Close()

	broker := sse.NewBroker(cfg.SSE.SubscriberBuffer, logger)
	updater := web.NewUpdater(broker, logger)

	batcher := capture.NewBatcher(
		ch,
		500,
		2*time.Second,
		cfg.Pricing.DefaultInputCost,
		cfg.Pricing.DefaultOutputCost,
		updater.MarkDirty,
		logger,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start batcher
	go batcher.Run(ctx)

	// Start updater (debounced dirty-signal loop + periodic refresh)
	go updater.Run(ctx)

	// Start capture watcher
	if cfg.Capture.Enabled {
		sources := buildSources(cfg)
		for _, s := range sources {
			logger.Info("capture source configured", "name", s.Name, "runtime", s.Runtime, "provider", s.Provider, "globs", s.Globs)
		}
		watcher := capture.NewWatcher(
			sources,
			batcher.EventCh(),
			ch,
			logger,
			time.Duration(cfg.Capture.DebounceMs)*time.Millisecond,
			cfg.Capture.ReconcileInterval,
			cfg.Capture.BackfillOnStart,
			cfg.Capture.BackfillWorkers,
		)
		go func() {
			if err := watcher.Run(ctx); err != nil {
				logger.Error("capture stopped", "error", err)
			}
		}()
	}

	searcher := search.NewSearcher(ch.DB, logger, cfg.Search.MaxResults, cfg.Search.RebuildInterval)
	go searcher.RunIndexer(ctx)

	// Web server
	handlers := web.NewHandlers(ch.DB, searcher, logger)
	apiHandlers := web.NewAPIHandlers(ch.DB, searcher, logger)
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

func buildSources(cfg *config.Config) []capture.WatchSource {
	var sources []capture.WatchSource
	for _, sc := range cfg.Capture.Sources {
		var parser func(line []byte, file string, lineNo int, offset int64) ([]capture.NormalizedEvent, error)
		switch sc.Runtime {
		case "codex":
			parser = capture.ParseCodexJSONL
		default:
			parser = capture.ParseClaudeJSONL
		}
		sources = append(sources, capture.WatchSource{
			Name:     sc.Name,
			Runtime:  sc.Runtime,
			Provider: sc.Provider,
			Format:   sc.Format,
			Globs:    []string{sc.Glob},
			Parser:   parser,
		})
	}
	return sources
}
