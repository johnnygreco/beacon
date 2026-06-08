package controlplane

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureLocalPersistsMetadataAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control-plane.db")
	boot := Bootstrap{
		NodeID:      "node-mac-mini",
		NodeName:    "Mac Mini",
		CollectorID: "collector-mac-mini",
		Sources: []SourceRegistration{
			{Name: "codex", Runtime: "codex", Provider: "openai", Format: "jsonl", WatchRoot: "~/.codex/sessions"},
			{Name: "hermes", Runtime: "hermes-agent", Provider: "multi", Format: "sqlite", WatchRoot: "~/.hermes"},
		},
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	first, err := store.EnsureLocal(context.Background(), boot)
	if err != nil {
		t.Fatalf("EnsureLocal first: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open reopened: %v", err)
	}
	defer reopened.Close()
	second, err := reopened.EnsureLocal(context.Background(), boot)
	if err != nil {
		t.Fatalf("EnsureLocal second: %v", err)
	}

	if first.OwnerInstanceID == "" || second.OwnerInstanceID != first.OwnerInstanceID {
		t.Fatalf("owner instance ID = %q then %q, want stable non-empty", first.OwnerInstanceID, second.OwnerInstanceID)
	}
	if second.SchemaEpoch != InitialSchemaEpoch {
		t.Fatalf("schema epoch = %q, want %q", second.SchemaEpoch, InitialSchemaEpoch)
	}
	if len(second.Nodes) != 1 || second.Nodes[0].ID != "node-mac-mini" {
		t.Fatalf("nodes = %#v, want persisted node", second.Nodes)
	}
	if len(second.Collectors) != 1 || second.Collectors[0].ID != "collector-mac-mini" || second.Collectors[0].NodeID != "node-mac-mini" {
		t.Fatalf("collectors = %#v, want persisted collector binding", second.Collectors)
	}
	if len(second.Sources) != 2 {
		t.Fatalf("sources = %#v, want two persisted source assignments", second.Sources)
	}
	if first.Sources[0].ID != second.Sources[0].ID || first.Sources[1].ID != second.Sources[1].ID {
		t.Fatalf("source IDs changed across restart: %#v -> %#v", first.Sources, second.Sources)
	}
}

func TestEnsureLocalRejectsCollectorIDCollision(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if _, err := store.EnsureLocal(context.Background(), Bootstrap{
		NodeID:      "node-a",
		NodeName:    "Node A",
		CollectorID: "collector-shared",
		Sources:     []SourceRegistration{{Name: "codex", Runtime: "codex", Provider: "openai", Format: "jsonl", WatchRoot: "/tmp/codex"}},
	}); err != nil {
		t.Fatalf("EnsureLocal first: %v", err)
	}
	_, err = store.EnsureLocal(context.Background(), Bootstrap{
		NodeID:      "node-b",
		NodeName:    "Node B",
		CollectorID: "collector-shared",
		Sources:     []SourceRegistration{{Name: "codex", Runtime: "codex", Provider: "openai", Format: "jsonl", WatchRoot: "/tmp/codex"}},
	})
	if err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("EnsureLocal collision error = %v, want already bound", err)
	}
}

func TestEnsureLocalAssignsDeterministicLocalIDs(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	boot := Bootstrap{
		NodeName: "workstation",
		Sources:  []SourceRegistration{{Name: "claude", Runtime: "claude-code", Provider: "anthropic", Format: "jsonl", WatchRoot: "/tmp/claude"}},
	}
	first, err := store.EnsureLocal(context.Background(), boot)
	if err != nil {
		t.Fatalf("EnsureLocal first: %v", err)
	}
	second, err := store.EnsureLocal(context.Background(), boot)
	if err != nil {
		t.Fatalf("EnsureLocal second: %v", err)
	}
	if first.Nodes[0].ID == "" || first.Collectors[0].ID == "" || first.Sources[0].ID == "" {
		t.Fatalf("generated IDs must be non-empty: %#v", first)
	}
	if first.Nodes[0].ID != second.Nodes[0].ID ||
		first.Collectors[0].ID != second.Collectors[0].ID ||
		first.Sources[0].ID != second.Sources[0].ID {
		t.Fatalf("generated IDs changed: %#v -> %#v", first, second)
	}
}

func TestMetadataStoreSurvivesCapturedDataResetBoundary(t *testing.T) {
	home := t.TempDir()
	metadataPath := filepath.Join(home, ".beacon", "control-plane.db")
	clickHouseDir := filepath.Join(home, ".beacon", "clickhouse")
	if err := os.MkdirAll(filepath.Join(clickHouseDir, "data"), 0755); err != nil {
		t.Fatalf("create clickhouse dir: %v", err)
	}

	store, err := Open(metadataPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	initial, err := store.EnsureLocal(context.Background(), Bootstrap{
		NodeID:      "node-local",
		NodeName:    "Local",
		CollectorID: "collector-local",
		Sources:     []SourceRegistration{{Name: "claude", Runtime: "claude-code", Provider: "anthropic", Format: "jsonl", WatchRoot: "/tmp/claude"}},
	})
	if err != nil {
		t.Fatalf("EnsureLocal: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := os.RemoveAll(clickHouseDir); err != nil {
		t.Fatalf("remove simulated captured-data dir: %v", err)
	}

	reopened, err := Open(metadataPath)
	if err != nil {
		t.Fatalf("Open reopened: %v", err)
	}
	defer reopened.Close()
	snapshot, err := reopened.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snapshot.OwnerInstanceID != initial.OwnerInstanceID || snapshot.SchemaEpoch != initial.SchemaEpoch {
		t.Fatalf("metadata changed after captured-data reset boundary: %#v -> %#v", initial, snapshot)
	}
	if len(snapshot.Collectors) != 1 || snapshot.Collectors[0].ID != "collector-local" {
		t.Fatalf("collector metadata lost after captured-data reset boundary: %#v", snapshot.Collectors)
	}
}
