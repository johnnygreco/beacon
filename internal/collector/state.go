package collector

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/johnnygreco/beacon/internal/models"
)

type StateStore struct {
	path               string
	mu                 sync.Mutex
	loaded             bool
	NextSequence       uint64                       `json:"next_sequence"`
	AckedNextSequence  uint64                       `json:"acked_next_sequence"`
	Checkpoints        map[string]models.Checkpoint `json:"checkpoints"`
	SpooledCheckpoints map[string]models.Checkpoint `json:"spooled_checkpoints"`
}

type stateStoreData struct {
	NextSequence       uint64                       `json:"next_sequence"`
	AckedNextSequence  uint64                       `json:"acked_next_sequence"`
	Checkpoints        map[string]models.Checkpoint `json:"checkpoints"`
	SpooledCheckpoints map[string]models.Checkpoint `json:"spooled_checkpoints"`
}

func OpenStateStore(path string) (*StateStore, error) {
	if path == "" {
		return nil, fmt.Errorf("collector state path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create collector state directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("secure collector state directory: %w", err)
	}
	store := &StateStore{
		path:               path,
		NextSequence:       1,
		AckedNextSequence:  1,
		Checkpoints:        make(map[string]models.Checkpoint),
		SpooledCheckpoints: make(map[string]models.Checkpoint),
	}
	if err := store.loadLocked(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *StateStore) Next() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.NextSequence == 0 {
		s.NextSequence = max(1, s.AckedNextSequence)
	}
	return s.NextSequence
}

func (s *StateStore) MarkSpooled(nextSequence uint64, checkpoints []models.Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.NextSequence
	if nextSequence > next {
		next = nextSequence
	}
	ackedNext := normalizedSequence(s.AckedNextSequence)
	next = max(next, ackedNext)
	acked := cloneCheckpointMap(s.Checkpoints)
	spooled := cloneCheckpointMap(s.SpooledCheckpoints)
	for _, checkpoint := range checkpoints {
		if checkpoint.SourceFile == "" {
			continue
		}
		spooled[checkpointKey(checkpoint.SourceName, checkpoint.SourceFile)] = checkpoint
	}
	if err := s.saveDataLocked(next, ackedNext, acked, spooled); err != nil {
		return err
	}
	s.NextSequence = next
	s.AckedNextSequence = ackedNext
	s.Checkpoints = acked
	s.SpooledCheckpoints = spooled
	return nil
}

func (s *StateStore) MarkAcked(nextSequence uint64, checkpoints []models.Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.NextSequence
	if nextSequence > next {
		next = nextSequence
	}
	ackedNext := normalizedSequence(s.AckedNextSequence)
	if nextSequence > ackedNext {
		ackedNext = nextSequence
	}
	next = max(next, ackedNext)
	acked := cloneCheckpointMap(s.Checkpoints)
	spooled := cloneCheckpointMap(s.SpooledCheckpoints)
	for _, checkpoint := range checkpoints {
		if checkpoint.SourceFile == "" {
			continue
		}
		key := checkpointKey(checkpoint.SourceName, checkpoint.SourceFile)
		acked[key] = checkpoint
		if spooledCheckpoint, ok := spooled[key]; ok && checkpointCovers(checkpoint, spooledCheckpoint) {
			delete(spooled, key)
		}
	}
	if len(spooled) == 0 {
		next = ackedNext
	}
	if err := s.saveDataLocked(next, ackedNext, acked, spooled); err != nil {
		return err
	}
	s.NextSequence = next
	s.AckedNextSequence = ackedNext
	s.Checkpoints = acked
	s.SpooledCheckpoints = spooled
	return nil
}

func (s *StateStore) ReplaceSpooled(nextSequence uint64, checkpoints []models.Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ackedNext := normalizedSequence(s.AckedNextSequence)
	next := ackedNext
	if nextSequence > next {
		next = nextSequence
	}
	acked := cloneCheckpointMap(s.Checkpoints)
	spooled := make(map[string]models.Checkpoint)
	for _, checkpoint := range checkpoints {
		if checkpoint.SourceFile == "" {
			continue
		}
		spooled[checkpointKey(checkpoint.SourceName, checkpoint.SourceFile)] = checkpoint
	}
	if err := s.saveDataLocked(next, ackedNext, acked, spooled); err != nil {
		return err
	}
	s.NextSequence = next
	s.AckedNextSequence = ackedNext
	s.Checkpoints = acked
	s.SpooledCheckpoints = spooled
	return nil
}

func (s *StateStore) Checkpoint(sourceName, sourceFile string) *models.Checkpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	checkpoint, ok := s.Checkpoints[checkpointKey(sourceName, sourceFile)]
	if !ok {
		return nil
	}
	cp := checkpoint
	return &cp
}

func (s *StateStore) SpooledCheckpoint(sourceName, sourceFile string) *models.Checkpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := checkpointKey(sourceName, sourceFile)
	if checkpoint, ok := s.SpooledCheckpoints[key]; ok {
		cp := checkpoint
		return &cp
	}
	if checkpoint, ok := s.Checkpoints[key]; ok {
		cp := checkpoint
		return &cp
	}
	return nil
}

func (s *StateStore) loadLocked() error {
	if s.loaded {
		return nil
	}
	s.loaded = true
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s.saveLocked()
	}
	if err != nil {
		return fmt.Errorf("read collector state: %w", err)
	}
	if len(data) == 0 {
		return s.saveLocked()
	}
	if err := json.Unmarshal(data, s); err != nil {
		return fmt.Errorf("decode collector state: %w", err)
	}
	if s.NextSequence == 0 {
		s.NextSequence = 1
	}
	if s.AckedNextSequence == 0 {
		s.AckedNextSequence = legacyAckedNextSequence(s.NextSequence, s.SpooledCheckpoints)
	}
	if s.NextSequence < s.AckedNextSequence {
		s.NextSequence = s.AckedNextSequence
	}
	if s.Checkpoints == nil {
		s.Checkpoints = make(map[string]models.Checkpoint)
	}
	if s.SpooledCheckpoints == nil {
		s.SpooledCheckpoints = make(map[string]models.Checkpoint)
	}
	return nil
}

func (s *StateStore) saveLocked() error {
	if s.NextSequence == 0 {
		s.NextSequence = 1
	}
	if s.AckedNextSequence == 0 {
		s.AckedNextSequence = 1
	}
	if s.NextSequence < s.AckedNextSequence {
		s.NextSequence = s.AckedNextSequence
	}
	if s.Checkpoints == nil {
		s.Checkpoints = make(map[string]models.Checkpoint)
	}
	if s.SpooledCheckpoints == nil {
		s.SpooledCheckpoints = make(map[string]models.Checkpoint)
	}
	return s.saveDataLocked(s.NextSequence, s.AckedNextSequence, s.Checkpoints, s.SpooledCheckpoints)
}

func (s *StateStore) saveDataLocked(nextSequence, ackedNextSequence uint64, checkpoints, spooledCheckpoints map[string]models.Checkpoint) error {
	if nextSequence == 0 {
		nextSequence = 1
	}
	if ackedNextSequence == 0 {
		ackedNextSequence = 1
	}
	if nextSequence < ackedNextSequence {
		nextSequence = ackedNextSequence
	}
	if checkpoints == nil {
		checkpoints = make(map[string]models.Checkpoint)
	}
	if spooledCheckpoints == nil {
		spooledCheckpoints = make(map[string]models.Checkpoint)
	}
	data, err := json.MarshalIndent(stateStoreData{
		NextSequence:       nextSequence,
		AckedNextSequence:  ackedNextSequence,
		Checkpoints:        checkpoints,
		SpooledCheckpoints: spooledCheckpoints,
	}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create collector state temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return err
	}
	cleanup = false
	if err := os.Chmod(s.path, 0600); err != nil {
		return err
	}
	return syncDir(filepath.Dir(s.path))
}

func cloneCheckpointMap(in map[string]models.Checkpoint) map[string]models.Checkpoint {
	out := make(map[string]models.Checkpoint, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func normalizedSequence(sequence uint64) uint64 {
	if sequence == 0 {
		return 1
	}
	return sequence
}

func legacyAckedNextSequence(nextSequence uint64, spooled map[string]models.Checkpoint) uint64 {
	nextSequence = normalizedSequence(nextSequence)
	if len(spooled) == 0 {
		return nextSequence
	}
	return 1
}

func checkpointKey(sourceName, sourceFile string) string {
	return sourceName + "\x00" + sourceFile
}

func checkpointCovers(acked, spooled models.Checkpoint) bool {
	if acked.SourceGeneration != spooled.SourceGeneration {
		return acked.SourceGeneration > spooled.SourceGeneration
	}
	if acked.LastOffset != spooled.LastOffset {
		return acked.LastOffset >= spooled.LastOffset
	}
	return acked.LastLineNo >= spooled.LastLineNo
}
