package collector

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/ingest"
)

const (
	spoolFileVersion = 1

	spoolPending    = "pending"
	spoolInflight   = "inflight"
	spoolAcked      = "acked"
	spoolQuarantine = "quarantine"
	spoolTmp        = "tmp"
)

var ErrSpoolFull = errors.New("collector spool is full")

type Spool struct {
	root     string
	maxBytes int64
}

type SpoolBatch struct {
	Path    string
	State   string
	Size    int64
	Request ingest.BatchRequest
}

type SpoolStats struct {
	PendingCount  int   `json:"pending_count"`
	InflightCount int   `json:"inflight_count"`
	ActiveBytes   int64 `json:"active_bytes"`
	CorruptCount  int   `json:"corrupt_count"`
	MaxBytes      int64 `json:"max_bytes"`
	Full          bool  `json:"full"`
}

type spoolEnvelope struct {
	Version  int                 `json:"version"`
	Checksum string              `json:"checksum"`
	Batch    ingest.BatchRequest `json:"batch"`
}

func OpenSpool(root string, maxBytes int64) (*Spool, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("spool root is required")
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("spool max bytes must be positive")
	}
	spool := &Spool{root: root, maxBytes: maxBytes}
	for _, dir := range []string{"", spoolPending, spoolInflight, spoolAcked, spoolQuarantine, spoolTmp} {
		path := filepath.Join(root, dir)
		if err := os.MkdirAll(path, 0700); err != nil {
			return nil, fmt.Errorf("create spool directory %q: %w", path, err)
		}
		if err := os.Chmod(path, 0700); err != nil {
			return nil, fmt.Errorf("secure spool directory %q: %w", path, err)
		}
	}
	if err := spool.recoverInflight(); err != nil {
		return nil, err
	}
	return spool, nil
}

func (s *Spool) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *Spool) WritePending(ctx context.Context, req ingest.BatchRequest) (*SpoolBatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, err := encodeSpoolEnvelope(req)
	if err != nil {
		return nil, err
	}
	activeBytes, err := s.activeBytes()
	if err != nil {
		return nil, err
	}
	if activeBytes+int64(len(payload)) > s.maxBytes {
		return nil, ErrSpoolFull
	}

	batch, err := s.writePendingPayload(ctx, req, payload)
	if err != nil {
		return nil, err
	}
	return &batch, nil
}

func (s *Spool) Pending() ([]SpoolBatch, error) {
	batches, err := s.readBatches(spoolPending)
	if err != nil {
		return nil, err
	}
	sortBatches(batches)
	return batches, nil
}

func (s *Spool) Inflight() ([]SpoolBatch, error) {
	batches, err := s.readBatches(spoolInflight)
	if err != nil {
		return nil, err
	}
	sortBatches(batches)
	return batches, nil
}

func (s *Spool) Active() ([]SpoolBatch, error) {
	pending, err := s.readBatches(spoolPending)
	if err != nil {
		return nil, err
	}
	inflight, err := s.readBatches(spoolInflight)
	if err != nil {
		return nil, err
	}
	batches := append(pending, inflight...)
	sortBatches(batches)
	return batches, nil
}

func (s *Spool) MarkInflight(batch SpoolBatch) (SpoolBatch, error) {
	return s.move(batch, spoolInflight)
}

func (s *Spool) MarkPending(batch SpoolBatch) (SpoolBatch, error) {
	return s.move(batch, spoolPending)
}

func (s *Spool) Ack(batch SpoolBatch) error {
	if batch.Path == "" {
		return fmt.Errorf("spool batch path is required")
	}
	if err := os.Remove(batch.Path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(batch.Path))
}

func (s *Spool) Quarantine(batch SpoolBatch) error {
	_, err := s.move(batch, spoolQuarantine)
	return err
}

func (s *Spool) Discard(batch SpoolBatch) error {
	if batch.Path == "" {
		return fmt.Errorf("spool batch path is required")
	}
	if err := os.Remove(batch.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDir(filepath.Dir(batch.Path))
}

func (s *Spool) DiscardActive() error {
	active, err := s.Active()
	if err != nil {
		return err
	}
	synced := map[string]struct{}{}
	for _, batch := range active {
		dir := filepath.Dir(batch.Path)
		if err := os.Remove(batch.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		synced[dir] = struct{}{}
	}
	for dir := range synced {
		if err := syncDir(dir); err != nil {
			return err
		}
	}
	return nil
}

func (s *Spool) HasActiveEpochMismatch(epoch string) (bool, error) {
	active, err := s.Active()
	if err != nil {
		return false, err
	}
	for _, batch := range active {
		if batch.Request.ControlPlaneEpoch != epoch {
			return true, nil
		}
	}
	return false, nil
}

func (s *Spool) Stats() (SpoolStats, error) {
	pendingCount, pendingBytes, err := s.countState(spoolPending)
	if err != nil {
		return SpoolStats{}, err
	}
	inflightCount, inflightBytes, err := s.countState(spoolInflight)
	if err != nil {
		return SpoolStats{}, err
	}
	corruptCount, _, err := s.countState(spoolQuarantine)
	if err != nil {
		return SpoolStats{}, err
	}
	activeBytes := pendingBytes + inflightBytes
	return SpoolStats{
		PendingCount:  pendingCount,
		InflightCount: inflightCount,
		ActiveBytes:   activeBytes,
		CorruptCount:  corruptCount,
		MaxBytes:      s.maxBytes,
		Full:          activeBytes >= s.maxBytes,
	}, nil
}

func (s *Spool) HasUnacked() (bool, error) {
	stats, err := s.Stats()
	if err != nil {
		return false, err
	}
	return stats.PendingCount+stats.InflightCount+stats.CorruptCount > 0, nil
}

func (s *Spool) readBatches(state string) ([]SpoolBatch, error) {
	dir := filepath.Join(s.root, state)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var batches []SpoolBatch
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		batch, err := readSpoolBatch(path, state)
		if err != nil {
			if moveErr := movePath(path, filepath.Join(s.root, spoolQuarantine, entry.Name()+"."+time.Now().UTC().Format("20060102150405"))); moveErr != nil {
				return nil, fmt.Errorf("quarantine corrupt spool file %q: %w", path, moveErr)
			}
			continue
		}
		batches = append(batches, batch)
	}
	return batches, nil
}

func (s *Spool) recoverInflight() error {
	batches, err := s.Inflight()
	if err != nil {
		return err
	}
	for _, batch := range batches {
		if _, err := s.move(batch, spoolPending); err != nil {
			return err
		}
	}
	return nil
}

func (s *Spool) move(batch SpoolBatch, state string) (SpoolBatch, error) {
	if batch.Path == "" {
		return SpoolBatch{}, fmt.Errorf("spool batch path is required")
	}
	nextPath := filepath.Join(s.root, state, filepath.Base(batch.Path))
	if nextPath == batch.Path {
		batch.State = state
		return batch, nil
	}
	if err := movePath(batch.Path, nextPath); err != nil {
		return SpoolBatch{}, err
	}
	batch.Path = nextPath
	batch.State = state
	return batch, nil
}

func (s *Spool) activeBytes() (int64, error) {
	_, pending, err := s.countState(spoolPending)
	if err != nil {
		return 0, err
	}
	_, inflight, err := s.countState(spoolInflight)
	if err != nil {
		return 0, err
	}
	return pending + inflight, nil
}

func (s *Spool) writePendingPayload(ctx context.Context, req ingest.BatchRequest, payload []byte) (SpoolBatch, error) {
	if err := ctx.Err(); err != nil {
		return SpoolBatch{}, err
	}
	name := spoolFileName(req)
	tmp, err := os.CreateTemp(filepath.Join(s.root, spoolTmp), name+".*.tmp")
	if err != nil {
		return SpoolBatch{}, fmt.Errorf("create spool temp file: %w", err)
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
		return SpoolBatch{}, fmt.Errorf("secure spool temp file: %w", err)
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return SpoolBatch{}, fmt.Errorf("write spool temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return SpoolBatch{}, fmt.Errorf("sync spool temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return SpoolBatch{}, fmt.Errorf("close spool temp file: %w", err)
	}
	finalPath := filepath.Join(s.root, spoolPending, name)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return SpoolBatch{}, fmt.Errorf("commit spool file: %w", err)
	}
	cleanup = false
	if err := syncDir(filepath.Dir(finalPath)); err != nil {
		return SpoolBatch{}, err
	}
	return SpoolBatch{Path: finalPath, State: spoolPending, Size: int64(len(payload)), Request: req}, nil
}

func (s *Spool) countState(state string) (int, int64, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, state))
	if err != nil {
		return 0, 0, err
	}
	var count int
	var bytes int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return 0, 0, err
		}
		count++
		bytes += info.Size()
	}
	return count, bytes, nil
}

func sortBatches(batches []SpoolBatch) {
	sort.Slice(batches, func(i, j int) bool {
		if batches[i].Request.Sequence == batches[j].Request.Sequence {
			return batches[i].Request.BatchID < batches[j].Request.BatchID
		}
		return batches[i].Request.Sequence < batches[j].Request.Sequence
	})
}

func readSpoolBatch(path, state string) (SpoolBatch, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SpoolBatch{}, err
	}
	var envelope spoolEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return SpoolBatch{}, err
	}
	if envelope.Version != spoolFileVersion {
		return SpoolBatch{}, fmt.Errorf("unsupported spool file version %d", envelope.Version)
	}
	checksum, err := checksumBatch(envelope.Batch)
	if err != nil {
		return SpoolBatch{}, err
	}
	if checksum != envelope.Checksum {
		return SpoolBatch{}, fmt.Errorf("spool checksum mismatch")
	}
	return SpoolBatch{
		Path:    path,
		State:   state,
		Size:    int64(len(data)),
		Request: envelope.Batch,
	}, nil
}

func encodeSpoolEnvelope(req ingest.BatchRequest) ([]byte, error) {
	checksum, err := checksumBatch(req)
	if err != nil {
		return nil, err
	}
	return json.Marshal(spoolEnvelope{
		Version:  spoolFileVersion,
		Checksum: checksum,
		Batch:    req,
	})
}

func checksumBatch(req ingest.BatchRequest) (string, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func spoolFileName(req ingest.BatchRequest) string {
	return fmt.Sprintf("%020d-%s.json", req.Sequence, safeFileToken(req.BatchID))
}

func safeFileToken(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= 160 {
			break
		}
	}
	if b.Len() == 0 {
		return "batch"
	}
	return b.String()
}

func movePath(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return err
	}
	if err := syncDir(filepath.Dir(from)); err != nil {
		return err
	}
	return syncDir(filepath.Dir(to))
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
