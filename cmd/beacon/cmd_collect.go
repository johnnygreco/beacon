package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/johnnygreco/beacon/internal/collector"
	"github.com/johnnygreco/beacon/internal/config"
	"github.com/spf13/cobra"
)

func newCollectCmd() *cobra.Command {
	var once bool
	var controlPlaneURL string
	cmd := &cobra.Command{
		Use:   "collect",
		Short: "Run remote-safe collector spooling and HTTP ingest",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCollect(cmd, once, controlPlaneURL)
		},
	}
	cmd.Flags().BoolVar(&once, "once", false, "run one send/scan cycle and exit")
	cmd.Flags().StringVar(&controlPlaneURL, "control-plane-url", "", "override fleet.control_plane_url for this collector run")
	return cmd
}

func runCollect(cmd *cobra.Command, once bool, controlPlaneURL string) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if strings.TrimSpace(controlPlaneURL) != "" {
		cfg.Fleet.ControlPlaneURL = strings.TrimRight(strings.TrimSpace(controlPlaneURL), "/")
	}
	service, cleanup, err := buildCollectorService(commandContext(cmd), cfg, logger)
	if err != nil {
		return err
	}
	defer cleanup()

	ctx, cancel := context.WithCancel(commandContext(cmd))
	defer cancel()
	if once {
		if err := service.SendPending(ctx); err != nil {
			return err
		}
		if err := service.ScanOnce(ctx); err != nil {
			return err
		}
		return service.SendPending(ctx)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-ctx.Done():
		case <-sigCh:
			cancel()
		}
	}()

	logger.Info("starting beacon collector", "control_plane_url", cfg.Fleet.ControlPlaneURL, "spool_dir", cfg.Fleet.SpoolDir)
	err = service.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func buildCollectorService(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*collector.Service, func(), error) {
	if cfg.Fleet.ControlPlaneURL == "" {
		return nil, func() {}, fmt.Errorf("fleet.control_plane_url is required for beacon collect")
	}
	sources, err := buildSources(cfg)
	if err != nil {
		return nil, func() {}, fmt.Errorf("capture source config: %w", err)
	}
	controlStore, snapshot, err := initializeControlPlane(ctx, cfg, logger)
	if err != nil {
		return nil, func() {}, fmt.Errorf("initializing local collector metadata: %w", err)
	}
	cleanup := func() { _ = controlStore.Close() }
	identity := captureFleetIdentity(snapshot)
	if identity.NodeID == "" || identity.CollectorID == "" || identity.ControlPlaneEpoch == "" {
		cleanup()
		return nil, func() {}, fmt.Errorf("collector identity is incomplete; run beacon enroll")
	}
	for _, source := range sources {
		if identity.Sources[source.Name].SourceID == "" {
			cleanup()
			return nil, func() {}, fmt.Errorf("source %q has no source_id assignment; run beacon enroll", source.Name)
		}
	}
	token, err := readIngestToken(cfg)
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	client, err := collector.NewClient(cfg.Fleet.ControlPlaneURL, token, cfg.Fleet.RetryMax)
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	spool, err := collector.OpenSpool(cfg.Fleet.SpoolDir, cfg.Fleet.SpoolMaxBytes)
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	state, err := collector.OpenStateStore(collectorStatePath(cfg))
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	service, err := collector.NewService(collector.ServiceConfig{
		Sources:           sources,
		Identity:          identity,
		Spool:             spool,
		State:             state,
		Client:            client,
		BatchSize:         cfg.Fleet.SpoolBatchSize,
		ScanInterval:      cfg.Capture.ReconcileInterval,
		RetryMin:          cfg.Fleet.RetryMin,
		RetryMax:          cfg.Fleet.RetryMax,
		HeartbeatInterval: cfg.Fleet.HeartbeatInterval,
		Logger:            logger,
	})
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return service, cleanup, nil
}

func readIngestToken(cfg *config.Config) (string, error) {
	if cfg.Fleet.IngestTokenEnv != "" {
		if token := strings.TrimSpace(os.Getenv(cfg.Fleet.IngestTokenEnv)); token != "" {
			return token, nil
		}
	}
	if cfg.Fleet.IngestTokenFile != "" {
		data, err := os.ReadFile(cfg.Fleet.IngestTokenFile)
		if err == nil {
			if token := strings.TrimSpace(string(data)); token != "" {
				return token, nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("read ingest token file: %w", err)
		}
	}
	return "", fmt.Errorf("ingest token is required in %s or %s", cfg.Fleet.IngestTokenEnv, cfg.Fleet.IngestTokenFile)
}

func writeIngestTokenFile(path, token string) error {
	path = strings.TrimSpace(path)
	token = strings.TrimSpace(token)
	if path == "" || token == "" {
		return fmt.Errorf("ingest token file path and token are required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token+"\n"), 0600)
}

func collectorStatePath(cfg *config.Config) string {
	return filepath.Join(cfg.Fleet.SpoolDir, "collector-state.json")
}
