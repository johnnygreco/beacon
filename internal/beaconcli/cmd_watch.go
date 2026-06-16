package beaconcli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/johnnygreco/beacon/internal/capture"
	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/spf13/cobra"
)

func newWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Run capture only (headless, no web server)",
		RunE:  runWatch,
	}
}

func runWatch(cmd *cobra.Command, args []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	pidFile, err := acquirePIDFile()
	if err != nil {
		return fmt.Errorf("acquire beacon pidfile: %w", err)
	} else {
		defer pidFile.Close()
	}

	sources, err := buildSources(cfg)
	if err != nil {
		return fmt.Errorf("capture source config: %w", err)
	}

	storeOpts := storeOptionsFromConfig(cfg)
	if err := ensureLocalClickHouse(storeOpts); err != nil {
		return fmt.Errorf("starting clickhouse: %w", err)
	}

	ch, err := store.Open(context.Background(), storeOpts)
	if err != nil {
		return storeOpenError("opening clickhouse store", err)
	}
	defer ch.Close()

	redactionPolicy := redactionPolicyFromConfig(cfg)
	batcher := capture.NewBatcher(
		ch, 500, 2*time.Second,
		cfg.Pricing.DefaultInputCost, cfg.Pricing.DefaultOutputCost,
		nil, // no SSE notify
		logger,
		capture.WithRedactionPolicy(redactionPolicy),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher := capture.NewWatcher(
		sources, batcher.EventCh(), ch, logger,
		time.Duration(cfg.Capture.DebounceMs)*time.Millisecond,
		cfg.Capture.ReconcileInterval,
		cfg.Capture.BackfillOnStart,
		cfg.Capture.BackfillWorkers,
		capture.WithWatcherRedactionPolicy(redactionPolicy),
	)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	bg := newBackgroundGroup(ctx, cancel, logger)
	bg.Go("signal handler", signalCancelWorker(sigCh, cancel, logger, "shutting down capture..."))

	logger.Info("starting headless capture")
	return runWatchServices(bg, batcher.Run, watcher.Run)
}
