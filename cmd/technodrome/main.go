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
	"github.com/technodrome-ai/technodrome/internal/config"
	"github.com/technodrome-ai/technodrome/internal/database"
	"github.com/technodrome-ai/technodrome/internal/ingestion"
	"github.com/technodrome-ai/technodrome/internal/search"
	"github.com/technodrome-ai/technodrome/internal/sse"
	"github.com/technodrome-ai/technodrome/internal/web"
)

var cfgFile string

func main() {
	rootCmd := &cobra.Command{
		Use:   "technodrome",
		Short: "Real-time AI coding agent monitoring dashboard",
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: technodrome.toml)")

	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the Technodrome server",
		RunE:  runServe,
	}

	rootCmd.AddCommand(serveCmd)

	// Default to serve if no subcommand
	rootCmd.RunE = runServe

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runServe(cmd *cobra.Command, args []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Load config
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger.Info("starting technodrome",
		"host", cfg.Server.Host,
		"port", cfg.Server.Port,
		"db", cfg.Database.Path,
	)

	// Open DuckDB
	db, err := database.Open(cfg.Database.Path, cfg.Database.ReadPoolSize)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	// SSE broker
	broker := sse.NewBroker(cfg.SSE.SubscriberBuffer, logger)

	// Dashboard updater - renders templ partials and broadcasts via SSE after each flush
	updater := web.NewUpdater(db.ReadPool, broker, logger)

	// Batcher - uses updater.NotifyDashboard as the flush callback
	batcher := ingestion.NewBatcher(
		db,
		cfg.Ingestion.BatchSize,
		cfg.Ingestion.FlushInterval,
		cfg.Pricing.DefaultInputCost,
		cfg.Pricing.DefaultOutputCost,
		updater.NotifyDashboard,
		logger,
	)

	// Embedding provider
	var embProvider search.EmbeddingProvider
	switch cfg.Search.Provider {
	case "ollama":
		embProvider = search.NewOllamaProvider(cfg.Search.Ollama.URL, cfg.Search.Model)
	case "openai":
		embProvider = search.NewOpenAIProvider(cfg.Search.OpenAI.APIKey, cfg.Search.OpenAI.Model, cfg.Search.OpenAI.Dimensions)
	}

	// Searcher
	searcher := search.NewSearcher(db.ReadPool, embProvider)

	// Embedding worker
	embWorker := search.NewWorker(db.ReadPool, embProvider, batcher.EventCh(), logger)

	// OTLP handler
	otlpHandler := ingestion.NewOTLPHandler(batcher.EventCh(), cfg.Ingestion.MaxBodyBytes, logger)

	// Web handlers
	handlers := web.NewHandlers(db.ReadPool, searcher, logger)
	apiHandlers := web.NewAPIHandlers(db.ReadPool, searcher, logger)

	// Router
	router := web.NewRouter(otlpHandler, broker, handlers, apiHandlers)

	// HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start batcher
	go batcher.Run(ctx)

	// Start embedding worker
	go embWorker.Run(ctx)

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("shutting down...")

		// Stop HTTP server
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)

		// Cancel context to stop batcher and embedding worker
		cancel()
	}()

	logger.Info("server listening", "addr", addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}

	logger.Info("server stopped")
	return nil
}
