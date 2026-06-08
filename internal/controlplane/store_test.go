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

func TestEnsureLocalKeepsGeneratedIDsWhenDisplayNameChanges(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	first, err := store.EnsureLocal(context.Background(), Bootstrap{
		NodeName: "Workstation A",
		Sources:  []SourceRegistration{{Name: "codex", Runtime: "codex", Provider: "openai", Format: "jsonl", WatchRoot: "/tmp/codex"}},
	})
	if err != nil {
		t.Fatalf("EnsureLocal first: %v", err)
	}
	second, err := store.EnsureLocal(context.Background(), Bootstrap{
		NodeName: "Workstation B",
		Sources:  []SourceRegistration{{Name: "codex", Runtime: "codex", Provider: "openai", Format: "jsonl", WatchRoot: "/tmp/codex"}},
	})
	if err != nil {
		t.Fatalf("EnsureLocal second: %v", err)
	}
	if first.Nodes[0].ID != second.Nodes[0].ID || first.Collectors[0].ID != second.Collectors[0].ID || first.Sources[0].ID != second.Sources[0].ID {
		t.Fatalf("identity changed after display-name update: %#v -> %#v", first, second)
	}
	if second.Nodes[0].DisplayName != "Workstation B" || second.Collectors[0].DisplayName != "Workstation B" {
		t.Fatalf("display names not updated: nodes=%#v collectors=%#v", second.Nodes, second.Collectors)
	}
}

func TestEnsureLocalReconcilesRemovedSources(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	boot := Bootstrap{
		NodeID:      "node-local",
		NodeName:    "Local",
		CollectorID: "collector-local",
		Sources: []SourceRegistration{
			{Name: "codex", Runtime: "codex", Provider: "openai", Format: "jsonl", WatchRoot: "/tmp/codex"},
			{Name: "claude", Runtime: "claude-code", Provider: "anthropic", Format: "jsonl", WatchRoot: "/tmp/claude"},
		},
	}
	if _, err := store.EnsureLocal(context.Background(), boot); err != nil {
		t.Fatalf("EnsureLocal first: %v", err)
	}
	boot.Sources = boot.Sources[:1]
	snapshot, err := store.EnsureLocal(context.Background(), boot)
	if err != nil {
		t.Fatalf("EnsureLocal second: %v", err)
	}
	if len(snapshot.Sources) != 1 || snapshot.Sources[0].Name != "codex" {
		t.Fatalf("sources after reconcile = %#v, want only codex", snapshot.Sources)
	}
}

func TestOpenRestrictsExistingPermissiveMetadataPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".beacon")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create metadata dir: %v", err)
	}
	path := filepath.Join(dir, "control-plane.db")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.EnsureLocal(context.Background(), Bootstrap{
		NodeID:      "node-local",
		NodeName:    "Local",
		CollectorID: "collector-local",
		Sources:     []SourceRegistration{{Name: "codex", Runtime: "codex", Provider: "openai", Format: "jsonl", WatchRoot: "/tmp/codex"}},
	}); err != nil {
		t.Fatalf("EnsureLocal: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	assertMode(t, dir, 0700)
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(candidate); err == nil {
			assertMode(t, candidate, 0600)
		}
	}
}

func TestOpenDoesNotChmodArbitraryExistingParent(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatalf("chmod temp dir: %v", err)
	}
	path := filepath.Join(dir, "custom-control-plane.db")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	assertMode(t, dir, 0755)
}

func TestOpenRejectsSQLiteDSNPath(t *testing.T) {
	for _, path := range []string{
		":memory:",
		"file:/tmp/control-plane.db?mode=memory",
		filepath.Join(t.TempDir(), "control-plane.db?_journal_mode=OFF"),
	} {
		t.Run(path, func(t *testing.T) {
			store, err := Open(path)
			if err == nil {
				store.Close()
				t.Fatal("Open returned nil error for SQLite DSN path")
			}
			if !strings.Contains(err.Error(), "SQLite DSN") {
				t.Fatalf("Open error = %v, want SQLite DSN", err)
			}
		})
	}
}

func TestOpenRejectsMetadataSidecarSymlink(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".beacon")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("create metadata dir: %v", err)
	}
	path := filepath.Join(dir, "control-plane.db")
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("target"), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Chmod(target, 0644); err != nil {
		t.Fatalf("chmod target: %v", err)
	}
	if err := os.Symlink(target, path+"-wal"); err != nil {
		t.Fatalf("create sidecar symlink: %v", err)
	}

	store, err := Open(path)
	if err == nil {
		store.Close()
		t.Fatal("Open returned nil error for sidecar symlink")
	}
	if !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("Open error = %v, want symlink rejection", err)
	}
	assertMode(t, target, 0644)
}

func TestOpenRejectsMainMetadataSymlink(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".beacon")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("create metadata dir: %v", err)
	}
	path := filepath.Join(dir, "control-plane.db")
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("target"), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Chmod(target, 0644); err != nil {
		t.Fatalf("chmod target: %v", err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("create main DB symlink: %v", err)
	}

	store, err := Open(path)
	if err == nil {
		store.Close()
		t.Fatal("Open returned nil error for main DB symlink")
	}
	if !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("Open error = %v, want symlink rejection", err)
	}
	assertMode(t, target, 0644)
}

func TestOpenRejectsNonRegularMetadataFiles(t *testing.T) {
	tests := []struct {
		name       string
		createPath func(path string) error
		openPath   func(dir string) string
	}{
		{
			name: "main db directory",
			createPath: func(path string) error {
				return os.Mkdir(path, 0700)
			},
			openPath: func(dir string) string {
				return filepath.Join(dir, "control-plane.db")
			},
		},
		{
			name: "sidecar directory",
			createPath: func(path string) error {
				return os.Mkdir(path+"-wal", 0700)
			},
			openPath: func(dir string) string {
				return filepath.Join(dir, "control-plane.db")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), ".beacon")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatalf("create metadata dir: %v", err)
			}
			path := tt.openPath(dir)
			if err := tt.createPath(path); err != nil {
				t.Fatalf("create non-regular metadata path: %v", err)
			}

			store, err := Open(path)
			if err == nil {
				store.Close()
				t.Fatal("Open returned nil error for non-regular metadata path")
			}
			if !strings.Contains(err.Error(), "must be a regular file") {
				t.Fatalf("Open error = %v, want regular file rejection", err)
			}
		})
	}
}

func TestPrepareMetadataFileRestrictsPreexistingRegularSidecars(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".beacon")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("create metadata dir: %v", err)
	}
	path := filepath.Join(dir, "control-plane.db")
	for _, sidecar := range []string{path + "-wal", path + "-shm"} {
		if err := os.WriteFile(sidecar, []byte("sidecar"), 0644); err != nil {
			t.Fatalf("write sidecar %s: %v", sidecar, err)
		}
		if err := os.Chmod(sidecar, 0644); err != nil {
			t.Fatalf("chmod sidecar %s: %v", sidecar, err)
		}
	}

	if err := prepareMetadataFile(path); err != nil {
		t.Fatalf("prepareMetadataFile: %v", err)
	}
	assertMode(t, path, 0600)
	assertMode(t, path+"-wal", 0600)
	assertMode(t, path+"-shm", 0600)
}

func TestOpenRejectsMetadataDirectorySymlink(t *testing.T) {
	base := t.TempDir()
	targetDir := filepath.Join(base, "target")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("create target dir: %v", err)
	}
	if err := os.Chmod(targetDir, 0755); err != nil {
		t.Fatalf("chmod target dir: %v", err)
	}
	linkDir := filepath.Join(base, ".beacon")
	if err := os.Symlink(targetDir, linkDir); err != nil {
		t.Fatalf("create metadata dir symlink: %v", err)
	}

	store, err := Open(filepath.Join(linkDir, "control-plane.db"))
	if err == nil {
		store.Close()
		t.Fatal("Open returned nil error for metadata directory symlink")
	}
	if !strings.Contains(err.Error(), "directory") || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("Open error = %v, want metadata directory symlink rejection", err)
	}
	assertMode(t, targetDir, 0755)
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

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode %s = %v, want %v", path, got, want)
	}
}
