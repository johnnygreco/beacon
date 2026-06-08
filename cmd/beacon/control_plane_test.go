package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
)

func TestControlPlaneBootstrapMapsConfigSources(t *testing.T) {
	cfg := &config.Config{
		Fleet: config.FleetConfig{
			NodeID:      "node-test",
			NodeName:    "Test Node",
			CollectorID: "collector-test",
		},
		Capture: config.CaptureConfig{
			Sources: []config.SourceConfig{
				{Name: "codex", Runtime: "codex", Provider: "openai", Format: "jsonl", WatchRoot: "/tmp/codex"},
			},
		},
	}

	boot := controlPlaneBootstrap(cfg)
	if boot.NodeID != "node-test" || boot.NodeName != "Test Node" || boot.CollectorID != "collector-test" {
		t.Fatalf("bootstrap identity = %#v, want configured values", boot)
	}
	if len(boot.Sources) != 1 {
		t.Fatalf("bootstrap sources = %#v, want one source", boot.Sources)
	}
	source := boot.Sources[0]
	if source.Name != "codex" || source.Runtime != "codex" || source.Provider != "openai" ||
		source.Format != "jsonl" || source.WatchRoot != "/tmp/codex" {
		t.Fatalf("bootstrap source = %#v, want config source fields", source)
	}
}

func TestControlPlaneStatusDoesNotCreateMissingStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "control-plane.db")
	cfg := &config.Config{Fleet: config.FleetConfig{MetadataPath: path}}

	if _, err := controlPlaneStatus(context.Background(), cfg); err == nil {
		t.Fatal("controlPlaneStatus returned nil error for missing store")
	}
	if controlplane.Exists(path) {
		t.Fatal("controlPlaneStatus created a missing metadata store")
	}
}

func TestInitializeControlPlaneCreatesSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control-plane.db")
	cfg := &config.Config{
		Fleet: config.FleetConfig{
			MetadataPath: path,
			NodeID:       "node-test",
			NodeName:     "Test Node",
			CollectorID:  "collector-test",
		},
		Capture: config.CaptureConfig{
			Sources: []config.SourceConfig{
				{Name: "claude", Runtime: "claude-code", Provider: "anthropic", Format: "jsonl", WatchRoot: "/tmp/claude"},
			},
		},
	}

	store, snapshot, err := initializeControlPlane(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("initializeControlPlane: %v", err)
	}
	defer store.Close()
	if snapshot.SchemaEpoch != controlplane.InitialSchemaEpoch {
		t.Fatalf("schema epoch = %q, want %q", snapshot.SchemaEpoch, controlplane.InitialSchemaEpoch)
	}
	if snapshot.LocalNodeID != "node-test" || snapshot.LocalCollectorID != "collector-test" {
		t.Fatalf("local identity = node %q collector %q, want configured IDs", snapshot.LocalNodeID, snapshot.LocalCollectorID)
	}
	if len(snapshot.Nodes) != 1 || len(snapshot.Collectors) != 1 || len(snapshot.Sources) != 1 {
		t.Fatalf("snapshot = %#v, want one node, collector, and source", snapshot)
	}
}

func TestInitializeControlPlaneRoleDoesNotCreateLocalCollector(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control-plane.db")
	cfg := &config.Config{
		Fleet: config.FleetConfig{
			Role:         config.FleetRoleControlPlane,
			MetadataPath: path,
			NodeID:       "node-test",
			NodeName:     "Test Node",
			CollectorID:  "collector-test",
		},
		Capture: config.CaptureConfig{
			Sources: []config.SourceConfig{
				{Name: "claude", Runtime: "claude-code", Provider: "anthropic", Format: "jsonl", WatchRoot: "/tmp/claude"},
			},
		},
	}

	store, snapshot, err := initializeControlPlane(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("initializeControlPlane: %v", err)
	}
	defer store.Close()
	if snapshot.SchemaEpoch != controlplane.InitialSchemaEpoch {
		t.Fatalf("schema epoch = %q, want %q", snapshot.SchemaEpoch, controlplane.InitialSchemaEpoch)
	}
	if snapshot.LocalNodeID != "" || snapshot.LocalCollectorID != "" ||
		len(snapshot.Nodes) != 0 || len(snapshot.Collectors) != 0 || len(snapshot.Sources) != 0 {
		t.Fatalf("control-plane role snapshot created local collector data: %#v", snapshot)
	}
}

func TestCaptureFleetIdentityOnlyUsesLocalCollectorSources(t *testing.T) {
	snapshot := &controlplane.Snapshot{
		LocalNodeID:      "node-local",
		LocalCollectorID: "collector-local",
		SchemaEpoch:      "7",
		Sources: []controlplane.Source{
			{ID: "source-remote", CollectorID: "collector-remote", Name: "codex"},
			{ID: "source-local", CollectorID: "collector-local", Name: "codex"},
		},
	}

	identity := captureFleetIdentity(snapshot)
	source := identity.Sources["codex"]
	if identity.NodeID != "node-local" || identity.CollectorID != "collector-local" || identity.ControlPlaneEpoch != "7" {
		t.Fatalf("identity = %#v, want local node/collector/epoch", identity)
	}
	if source.SourceID != "source-local" {
		t.Fatalf("source identity = %#v, want local collector source", source)
	}
}
