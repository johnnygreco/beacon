package collector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/johnnygreco/beacon/internal/models"
)

func TestStateStoreMarkSpooledSaveFailureDoesNotAdvanceMemory(t *testing.T) {
	dir := t.TempDir()
	state, err := OpenStateStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("OpenStateStore: %v", err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatalf("chmod state dir read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	err = state.MarkSpooled(2, []models.Checkpoint{{
		SourceName: "codex",
		SourceFile: "session.jsonl",
		LastOffset: 42,
		LastLineNo: 1,
	}})
	if err == nil {
		t.Fatal("MarkSpooled returned nil error after state directory was made read-only")
	}
	if got := state.Next(); got != 1 {
		t.Fatalf("NextSequence = %d, want unchanged 1", got)
	}
	if cp := state.SpooledCheckpoint("codex", "session.jsonl"); cp != nil {
		t.Fatalf("spooled checkpoint advanced in memory after failed save: %#v", cp)
	}
}

func TestRecoverSpooledStateClearsMissingSpoolCheckpoints(t *testing.T) {
	dir := t.TempDir()
	spool, err := OpenSpool(filepath.Join(dir, "spool"), 1<<20)
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	state, err := OpenStateStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("OpenStateStore: %v", err)
	}
	if err := state.MarkSpooled(2, []models.Checkpoint{{
		SourceName: "codex",
		SourceFile: "session.jsonl",
		LastOffset: 42,
		LastLineNo: 1,
	}}); err != nil {
		t.Fatalf("MarkSpooled: %v", err)
	}
	service := &Service{cfg: ServiceConfig{Spool: spool, State: state}}
	if err := service.recoverSpooledStateFromSpool(); err != nil {
		t.Fatalf("recoverSpooledStateFromSpool: %v", err)
	}
	if cp := state.SpooledCheckpoint("codex", "session.jsonl"); cp != nil {
		t.Fatalf("stale spooled checkpoint survived empty spool recovery: %#v", cp)
	}
	if got := state.Next(); got != 1 {
		t.Fatalf("next sequence after empty spool recovery = %d, want committed sequence 1", got)
	}
}
