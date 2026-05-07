package capture

import (
	"context"
	"os"
	"sync"
	"syscall"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/store"
)

// CheckpointManager loads and saves file processing checkpoints.
type CheckpointManager struct {
	store      *store.Store
	sourceName string
	mu         sync.RWMutex
	cache      map[string]*models.Checkpoint
}

// NewCheckpointManager creates a checkpoint manager for a given source.
func NewCheckpointManager(ch *store.Store, sourceName string) *CheckpointManager {
	return &CheckpointManager{
		store:      ch,
		sourceName: sourceName,
		cache:      make(map[string]*models.Checkpoint),
	}
}

// Load reads all checkpoints for this source from the database.
func (cm *CheckpointManager) Load(ctx context.Context) error {
	cps, err := cm.store.LoadCheckpoints(ctx, cm.sourceName)
	if err != nil {
		return err
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.cache = cps
	return nil
}

// Get returns the checkpoint for a file, or nil if none exists.
func (cm *CheckpointManager) Get(file string) *models.Checkpoint {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.cache[file]
}

// Save persists a checkpoint to the database and updates the cache.
func (cm *CheckpointManager) Save(ctx context.Context, cp *models.Checkpoint) error {
	if err := cm.store.UpsertCheckpoint(ctx, *cp); err != nil {
		return err
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.cache[cp.SourceFile] = cp
	return nil
}

// CheckRotation detects log rotation by comparing inode and file size.
// Returns true if the file was rotated/truncated and the checkpoint should reset.
func (cm *CheckpointManager) CheckRotation(file string, fi os.FileInfo) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	cp := cm.cache[file]
	if cp == nil {
		return false
	}

	inode := fileInode(fi)
	if inode > 0 && cp.SourceInode > 0 && inode != cp.SourceInode {
		return true
	}

	if fi.Size() < cp.LastOffset {
		return true
	}

	return false
}

// fileInode extracts the inode number from FileInfo (unix only).
func fileInode(fi os.FileInfo) int64 {
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int64(stat.Ino)
	}
	return 0
}
