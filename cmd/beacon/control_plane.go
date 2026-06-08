package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/johnnygreco/beacon/internal/capture"
	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
)

func initializeControlPlane(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*controlplane.Store, *controlplane.Snapshot, error) {
	store, err := controlplane.Open(cfg.Fleet.MetadataPath)
	if err != nil {
		return nil, nil, err
	}
	var snapshot *controlplane.Snapshot
	if cfg.Fleet.Role == config.FleetRoleControlPlane {
		snapshot, err = store.EnsureControlPlane(ctx)
	} else {
		snapshot, err = store.EnsureLocal(ctx, controlPlaneBootstrap(cfg))
	}
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	if logger != nil {
		logger.Info("control-plane metadata initialized",
			"path", snapshot.Path,
			"schema_epoch", snapshot.SchemaEpoch,
			"nodes", len(snapshot.Nodes),
			"collectors", len(snapshot.Collectors),
			"sources", len(snapshot.Sources),
		)
	}
	return store, snapshot, nil
}

func initializeCollectorControlPlane(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*controlplane.Store, *controlplane.Snapshot, error) {
	store, err := controlplane.Open(cfg.Fleet.MetadataPath)
	if err != nil {
		return nil, nil, err
	}
	boot := controlPlaneBootstrap(cfg)
	if snapshot, err := store.Snapshot(ctx); err == nil && snapshot.LocalNodeID != "" && snapshot.LocalCollectorID != "" {
		boot.NodeID = snapshot.LocalNodeID
		boot.CollectorID = snapshot.LocalCollectorID
	}
	snapshot, err := store.EnsureLocal(ctx, boot)
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	if logger != nil {
		logger.Info("collector metadata initialized",
			"path", snapshot.Path,
			"schema_epoch", snapshot.SchemaEpoch,
			"node_id", snapshot.LocalNodeID,
			"collector_id", snapshot.LocalCollectorID,
			"sources", len(snapshot.Sources),
		)
	}
	return store, snapshot, nil
}

func controlPlaneBootstrap(cfg *config.Config) controlplane.Bootstrap {
	boot := controlplane.Bootstrap{
		NodeID:      cfg.Fleet.NodeID,
		NodeName:    cfg.Fleet.NodeName,
		CollectorID: cfg.Fleet.CollectorID,
		Sources:     make([]controlplane.SourceRegistration, 0, len(cfg.Capture.Sources)),
	}
	for _, source := range cfg.Capture.Sources {
		boot.Sources = append(boot.Sources, controlplane.SourceRegistration{
			Name:      source.Name,
			Runtime:   source.Runtime,
			Provider:  source.Provider,
			Format:    source.Format,
			WatchRoot: source.WatchRoot,
		})
	}
	return boot
}

func controlPlaneStatus(ctx context.Context, cfg *config.Config) (*controlplane.Snapshot, error) {
	if !controlplane.Exists(cfg.Fleet.MetadataPath) {
		return nil, fmt.Errorf("not initialized at %s", cfg.Fleet.MetadataPath)
	}
	store, err := controlplane.Open(cfg.Fleet.MetadataPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.Snapshot(ctx)
}

func captureFleetIdentity(snapshot *controlplane.Snapshot) capture.FleetIdentity {
	identity := capture.FleetIdentity{Sources: map[string]capture.FleetSourceIdentity{}}
	if snapshot == nil {
		return identity
	}
	identity.NodeID = snapshot.LocalNodeID
	identity.CollectorID = snapshot.LocalCollectorID
	identity.ControlPlaneEpoch = snapshot.SchemaEpoch
	for _, source := range snapshot.Sources {
		if source.CollectorID != snapshot.LocalCollectorID {
			continue
		}
		identity.Sources[source.Name] = capture.FleetSourceIdentity{SourceID: source.ID}
	}
	return identity
}
