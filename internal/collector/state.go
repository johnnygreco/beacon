package collector

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/johnnygreco/beacon/internal/models"
)

type StateStore struct {
	path                    string
	mu                      sync.Mutex
	loaded                  bool
	ControlPlaneEpoch       string                         `json:"control_plane_epoch,omitempty"`
	NextSequence            uint64                         `json:"next_sequence"`
	AckedNextSequence       uint64                         `json:"acked_next_sequence"`
	Checkpoints             map[string]models.Checkpoint   `json:"checkpoints"`
	SpooledCheckpoints      map[string]models.Checkpoint   `json:"spooled_checkpoints"`
	SpooledBatchCheckpoints map[string][]models.Checkpoint `json:"spooled_batch_checkpoints,omitempty"`
}

type stateStoreData struct {
	ControlPlaneEpoch       string                         `json:"control_plane_epoch,omitempty"`
	NextSequence            uint64                         `json:"next_sequence"`
	AckedNextSequence       uint64                         `json:"acked_next_sequence"`
	Checkpoints             map[string]models.Checkpoint   `json:"checkpoints"`
	SpooledCheckpoints      map[string]models.Checkpoint   `json:"spooled_checkpoints"`
	SpooledBatchCheckpoints map[string][]models.Checkpoint `json:"spooled_batch_checkpoints,omitempty"`
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
		path:                    path,
		NextSequence:            1,
		AckedNextSequence:       1,
		Checkpoints:             make(map[string]models.Checkpoint),
		SpooledCheckpoints:      make(map[string]models.Checkpoint),
		SpooledBatchCheckpoints: make(map[string][]models.Checkpoint),
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

func (s *StateStore) AckedNext() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return normalizedSequence(s.AckedNextSequence)
}

func (s *StateStore) Epoch() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ControlPlaneEpoch
}

func (s *StateStore) NeedsEpochReset(epoch string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.needsEpochResetLocked(epoch)
}

func (s *StateStore) EnsureEpoch(epoch string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if epoch == "" || s.ControlPlaneEpoch == epoch {
		return false, nil
	}
	if !s.needsEpochResetLocked(epoch) {
		if err := s.saveDataLocked(epoch, normalizedSequence(s.NextSequence), normalizedSequence(s.AckedNextSequence), s.Checkpoints, s.SpooledCheckpoints, s.SpooledBatchCheckpoints); err != nil {
			return false, err
		}
		s.ControlPlaneEpoch = epoch
		return false, nil
	}
	if err := s.saveDataLocked(epoch, 1, 1, nil, nil, nil); err != nil {
		return false, err
	}
	s.ControlPlaneEpoch = epoch
	s.NextSequence = 1
	s.AckedNextSequence = 1
	s.Checkpoints = make(map[string]models.Checkpoint)
	s.SpooledCheckpoints = make(map[string]models.Checkpoint)
	s.SpooledBatchCheckpoints = make(map[string][]models.Checkpoint)
	return true, nil
}

func (s *StateStore) needsEpochResetLocked(epoch string) bool {
	if epoch == "" || s.ControlPlaneEpoch == epoch {
		return false
	}
	return !(s.ControlPlaneEpoch == "" &&
		len(s.Checkpoints) == 0 &&
		len(s.SpooledCheckpoints) == 0 &&
		normalizedSequence(s.AckedNextSequence) == 1)
}

func (s *StateStore) MarkSpooled(nextSequence uint64, checkpoints []models.Checkpoint) error {
	sequence := normalizedSequence(nextSequence)
	if sequence > 1 {
		sequence--
	}
	return s.MarkSpooledBatch(sequence, nextSequence, checkpoints)
}

func (s *StateStore) MarkSpooledBatch(sequence, nextSequence uint64, checkpoints []models.Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sequence = normalizedSequence(sequence)
	next := s.NextSequence
	if nextSequence > next {
		next = nextSequence
	}
	ackedNext := normalizedSequence(s.AckedNextSequence)
	next = max(next, ackedNext)
	acked := cloneCheckpointMap(s.Checkpoints)
	spooled := cloneCheckpointMap(s.SpooledCheckpoints)
	spooledBatches := cloneCheckpointBatchMap(s.SpooledBatchCheckpoints)
	for _, checkpoint := range checkpoints {
		if checkpoint.SourceFile == "" {
			continue
		}
		spooled[checkpointKey(checkpoint.SourceName, checkpoint.SourceFile)] = checkpoint
	}
	spooledBatches[sequenceKey(sequence)] = cloneCheckpointSlice(checkpoints)
	if err := s.saveDataLocked(s.ControlPlaneEpoch, next, ackedNext, acked, spooled, spooledBatches); err != nil {
		return err
	}
	s.NextSequence = next
	s.AckedNextSequence = ackedNext
	s.Checkpoints = acked
	s.SpooledCheckpoints = spooled
	s.SpooledBatchCheckpoints = spooledBatches
	return nil
}

func (s *StateStore) UnmarkSpooledBatch(sequence uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sequence = normalizedSequence(sequence)
	ackedNext := normalizedSequence(s.AckedNextSequence)
	next := ackedNext
	acked := cloneCheckpointMap(s.Checkpoints)
	spooledBatches := cloneCheckpointBatchMap(s.SpooledBatchCheckpoints)
	delete(spooledBatches, sequenceKey(sequence))
	spooled := checkpointsFromBatches(spooledBatches)
	for key := range spooledBatches {
		batchSequence, err := strconv.ParseUint(key, 10, 64)
		if err == nil && batchSequence >= next {
			next = batchSequence + 1
		}
	}
	if err := s.saveDataLocked(s.ControlPlaneEpoch, next, ackedNext, acked, spooled, spooledBatches); err != nil {
		return err
	}
	s.NextSequence = next
	s.AckedNextSequence = ackedNext
	s.Checkpoints = acked
	s.SpooledCheckpoints = spooled
	s.SpooledBatchCheckpoints = spooledBatches
	return nil
}

func (s *StateStore) HasSpooledBatch(sequence uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.SpooledBatchCheckpoints[sequenceKey(sequence)]
	return ok
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
	spooledBatches := cloneCheckpointBatchMap(s.SpooledBatchCheckpoints)
	applyAckedCheckpoints(acked, spooled, checkpoints)
	deleteAckedBatchCheckpoints(spooledBatches, nextSequence)
	if len(spooled) == 0 && len(spooledBatches) == 0 {
		next = ackedNext
	}
	if err := s.saveDataLocked(s.ControlPlaneEpoch, next, ackedNext, acked, spooled, spooledBatches); err != nil {
		return err
	}
	s.NextSequence = next
	s.AckedNextSequence = ackedNext
	s.Checkpoints = acked
	s.SpooledCheckpoints = spooled
	s.SpooledBatchCheckpoints = spooledBatches
	return nil
}

func (s *StateStore) MarkBatchAcked(nextSequence, sequence uint64, fallbackCheckpoints []models.Checkpoint) error {
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
	spooledBatches := cloneCheckpointBatchMap(s.SpooledBatchCheckpoints)
	checkpoints := spooledBatches[sequenceKey(sequence)]
	applyAckedCheckpoints(acked, spooled, checkpoints)
	delete(spooledBatches, sequenceKey(sequence))
	deleteAckedBatchCheckpoints(spooledBatches, nextSequence)
	if len(spooled) == 0 && len(spooledBatches) == 0 {
		next = ackedNext
	}
	if err := s.saveDataLocked(s.ControlPlaneEpoch, next, ackedNext, acked, spooled, spooledBatches); err != nil {
		return err
	}
	s.NextSequence = next
	s.AckedNextSequence = ackedNext
	s.Checkpoints = acked
	s.SpooledCheckpoints = spooled
	s.SpooledBatchCheckpoints = spooledBatches
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
	if err := s.saveDataLocked(s.ControlPlaneEpoch, next, ackedNext, acked, spooled, nil); err != nil {
		return err
	}
	s.NextSequence = next
	s.AckedNextSequence = ackedNext
	s.Checkpoints = acked
	s.SpooledCheckpoints = spooled
	s.SpooledBatchCheckpoints = make(map[string][]models.Checkpoint)
	return nil
}

func (s *StateStore) ReplaceSpooledBatches(nextSequence uint64, active []SpoolBatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ackedNext := normalizedSequence(s.AckedNextSequence)
	next := ackedNext
	if nextSequence > next {
		next = nextSequence
	}
	acked := cloneCheckpointMap(s.Checkpoints)
	existingBatches := cloneCheckpointBatchMap(s.SpooledBatchCheckpoints)
	spooled := make(map[string]models.Checkpoint)
	spooledBatches := make(map[string][]models.Checkpoint)
	for _, batch := range active {
		key := sequenceKey(batch.Request.Sequence)
		checkpoints := existingBatches[key]
		spooledBatches[key] = cloneCheckpointSlice(checkpoints)
		for _, checkpoint := range checkpoints {
			if checkpoint.SourceFile == "" {
				continue
			}
			spooled[checkpointKey(checkpoint.SourceName, checkpoint.SourceFile)] = checkpoint
		}
	}
	if err := s.saveDataLocked(s.ControlPlaneEpoch, next, ackedNext, acked, spooled, spooledBatches); err != nil {
		return err
	}
	s.NextSequence = next
	s.AckedNextSequence = ackedNext
	s.Checkpoints = acked
	s.SpooledCheckpoints = spooled
	s.SpooledBatchCheckpoints = spooledBatches
	return nil
}

func checkpointsFromBatches(batches map[string][]models.Checkpoint) map[string]models.Checkpoint {
	spooled := make(map[string]models.Checkpoint)
	for _, checkpoints := range batches {
		for _, checkpoint := range checkpoints {
			if checkpoint.SourceFile == "" {
				continue
			}
			spooled[checkpointKey(checkpoint.SourceName, checkpoint.SourceFile)] = checkpoint
		}
	}
	return spooled
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
	if s.SpooledBatchCheckpoints == nil {
		s.SpooledBatchCheckpoints = make(map[string][]models.Checkpoint)
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
	if s.SpooledBatchCheckpoints == nil {
		s.SpooledBatchCheckpoints = make(map[string][]models.Checkpoint)
	}
	return s.saveDataLocked(s.ControlPlaneEpoch, s.NextSequence, s.AckedNextSequence, s.Checkpoints, s.SpooledCheckpoints, s.SpooledBatchCheckpoints)
}

func (s *StateStore) saveDataLocked(controlPlaneEpoch string, nextSequence, ackedNextSequence uint64, checkpoints, spooledCheckpoints map[string]models.Checkpoint, spooledBatchCheckpoints map[string][]models.Checkpoint) error {
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
	if spooledBatchCheckpoints == nil {
		spooledBatchCheckpoints = make(map[string][]models.Checkpoint)
	}
	data, err := json.MarshalIndent(stateStoreData{
		ControlPlaneEpoch:       controlPlaneEpoch,
		NextSequence:            nextSequence,
		AckedNextSequence:       ackedNextSequence,
		Checkpoints:             checkpoints,
		SpooledCheckpoints:      spooledCheckpoints,
		SpooledBatchCheckpoints: spooledBatchCheckpoints,
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

func cloneCheckpointBatchMap(in map[string][]models.Checkpoint) map[string][]models.Checkpoint {
	out := make(map[string][]models.Checkpoint, len(in))
	for key, value := range in {
		out[key] = cloneCheckpointSlice(value)
	}
	return out
}

func cloneCheckpointSlice(in []models.Checkpoint) []models.Checkpoint {
	if len(in) == 0 {
		return nil
	}
	out := make([]models.Checkpoint, len(in))
	copy(out, in)
	return out
}

func applyAckedCheckpoints(acked, spooled map[string]models.Checkpoint, checkpoints []models.Checkpoint) {
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
}

func deleteAckedBatchCheckpoints(spooledBatches map[string][]models.Checkpoint, nextSequence uint64) {
	nextSequence = normalizedSequence(nextSequence)
	for key := range spooledBatches {
		sequence, err := strconv.ParseUint(key, 10, 64)
		if err != nil || sequence < nextSequence {
			delete(spooledBatches, key)
		}
	}
}

func normalizedSequence(sequence uint64) uint64 {
	if sequence == 0 {
		return 1
	}
	return sequence
}

func sequenceKey(sequence uint64) string {
	return strconv.FormatUint(normalizedSequence(sequence), 10)
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
