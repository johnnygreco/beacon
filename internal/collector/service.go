package collector

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/johnnygreco/beacon/internal/capture"
	"github.com/johnnygreco/beacon/internal/ingest"
	"github.com/johnnygreco/beacon/internal/models"
)

type ServiceConfig struct {
	Sources           []capture.WatchSource
	Identity          capture.FleetIdentity
	Spool             *Spool
	State             *StateStore
	Client            *Client
	BatchSize         int
	MaxBatchBodyBytes int
	ScanInterval      time.Duration
	RetryMin          time.Duration
	RetryMax          time.Duration
	HeartbeatInterval time.Duration
	Logger            *slog.Logger
}

type Service struct {
	cfg       ServiceConfig
	mu        sync.Mutex
	status    Status
	nextRetry time.Duration
}

type Status struct {
	Spool              SpoolStats `json:"spool"`
	BlockedSpoolFull   bool       `json:"blocked_spool_full"`
	LastBatchID        string     `json:"last_batch_id,omitempty"`
	LastAckAt          time.Time  `json:"last_ack_at,omitempty"`
	LastError          string     `json:"last_error,omitempty"`
	LastScanAt         time.Time  `json:"last_scan_at,omitempty"`
	LastHeartbeatAt    time.Time  `json:"last_heartbeat_at,omitempty"`
	LastHeartbeatError string     `json:"last_heartbeat_error,omitempty"`
	BlockedTerminal    bool       `json:"blocked_terminal"`
}

var ErrTerminalBlocked = errors.New("collector is blocked on terminal ingest failure")
var ErrBatchTooLarge = errors.New("collector batch exceeds ingest limit")

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Spool == nil {
		return nil, fmt.Errorf("collector spool is required")
	}
	if cfg.State == nil {
		return nil, fmt.Errorf("collector state is required")
	}
	if cfg.Client == nil {
		return nil, fmt.Errorf("collector ingest client is required")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 500
	}
	if cfg.MaxBatchBodyBytes <= 0 {
		cfg.MaxBatchBodyBytes = ingest.MaxBodyBytes
	}
	if cfg.ScanInterval <= 0 {
		cfg.ScanInterval = 30 * time.Second
	}
	if cfg.RetryMin <= 0 {
		cfg.RetryMin = time.Second
	}
	if cfg.RetryMax < cfg.RetryMin {
		cfg.RetryMax = cfg.RetryMin
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Service{cfg: cfg, nextRetry: cfg.RetryMin}, nil
}

func (s *Service) Run(ctx context.Context) error {
	scanTicker := time.NewTicker(s.cfg.ScanInterval)
	defer scanTicker.Stop()
	heartbeatTicker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer heartbeatTicker.Stop()

	shouldScan := true
	if err := s.SendPending(ctx); err != nil {
		s.setError(err)
		shouldScan = retryableSendError(err)
	}
	if shouldScan {
		if err := s.ScanOnce(ctx); err != nil {
			s.setError(err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-scanTicker.C:
			if err := s.SendPending(ctx); err != nil {
				s.setError(err)
				if retryableSendError(err) {
					if scanErr := s.ScanOnce(ctx); scanErr != nil {
						s.setError(scanErr)
					}
				}
				if err := s.sleepRetry(ctx); err != nil {
					return err
				}
				continue
			}
			if err := s.ScanOnce(ctx); err != nil {
				s.setError(err)
			}
		case <-heartbeatTicker.C:
			if err := s.SendHeartbeat(ctx); err != nil {
				s.setHeartbeatError(err)
			}
		}
	}
}

func (s *Service) ScanOnce(ctx context.Context) error {
	if err := s.recoverSpooledStateFromSpool(); err != nil {
		return err
	}
	stats, err := s.cfg.Spool.Stats()
	if err != nil {
		return err
	}
	if stats.CorruptCount > 0 || s.terminalBlocked() {
		return nil
	}

	for _, src := range s.cfg.Sources {
		files := resolveSourceGlobs(src.Globs)
		for _, file := range files {
			windowSize := s.cfg.BatchSize
			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				cp := s.cfg.State.SpooledCheckpoint(src.Name, file)
				result, err := capture.ReadSourceFileWindow(ctx, src, file, cp, s.cfg.Logger, windowSize)
				if err != nil {
					s.cfg.Logger.Warn("collector read source file failed", "source", src.Name, "file", file, "error", err)
					break
				}
				if len(result.Events) == 0 && len(result.CaptureErrors) == 0 && result.Checkpoint == nil {
					break
				}
				if err := s.spoolReadResult(ctx, src, result); err != nil {
					if errors.Is(err, ErrBatchTooLarge) {
						if windowSize > 1 {
							windowSize = max(1, windowSize/2)
							continue
						}
						if err := s.spoolOversizeReadResult(ctx, src, result); err != nil {
							return err
						}
						windowSize = s.cfg.BatchSize
						if !result.HasMore {
							break
						}
						continue
					}
					if err == ErrSpoolFull {
						s.setSpoolFull(true)
						return nil
					}
					return err
				}
				windowSize = s.cfg.BatchSize
				if !result.HasMore {
					break
				}
			}
		}
	}
	s.setScanned()
	return nil
}

func (s *Service) SendPending(ctx context.Context) error {
	for {
		if err := s.requeueInflight(); err != nil {
			return err
		}
		pending, err := s.cfg.Spool.Pending()
		if err != nil {
			return err
		}
		stats, err := s.cfg.Spool.Stats()
		if err != nil {
			return err
		}
		if stats.CorruptCount > 0 {
			return nil
		}
		if len(pending) == 0 {
			s.setSpoolFull(false)
			s.clearTerminalBlocked()
			return nil
		}
		if s.terminalBlocked() {
			return ErrTerminalBlocked
		}
		batch := pending[0]
		inflight, err := s.cfg.Spool.MarkInflight(batch)
		if err != nil {
			return err
		}
		ack, err := s.cfg.Client.SendBatch(ctx, inflight.Request)
		if err != nil {
			if sendErr, ok := err.(*SendError); ok && !sendErr.Retryable {
				if _, moveErr := s.cfg.Spool.MarkPending(inflight); moveErr != nil {
					return moveErr
				}
				s.setTerminalBlocked(err)
				return err
			}
			if _, moveErr := s.cfg.Spool.MarkPending(inflight); moveErr != nil {
				return moveErr
			}
			return err
		}
		if ack.PayloadDigest != inflight.Request.PayloadDigest || ack.BatchID != inflight.Request.BatchID {
			if _, moveErr := s.cfg.Spool.MarkPending(inflight); moveErr != nil {
				return moveErr
			}
			err := fmt.Errorf("ingest ack did not match batch")
			s.setTerminalBlocked(err)
			return err
		}
		if err := s.cfg.State.MarkAcked(ack.NextSequence, inflight.Request.Checkpoints); err != nil {
			if _, moveErr := s.cfg.Spool.MarkPending(inflight); moveErr != nil {
				return fmt.Errorf("%w; additionally failed to return batch to pending: %v", err, moveErr)
			}
			return err
		}
		if err := s.cfg.Spool.Ack(inflight); err != nil {
			if _, moveErr := s.cfg.Spool.MarkPending(inflight); moveErr != nil {
				return fmt.Errorf("%w; additionally failed to return batch to pending: %v", err, moveErr)
			}
			return err
		}
		s.setAcked(ack.BatchID)
		s.nextRetry = s.cfg.RetryMin
	}
}

func (s *Service) requeueInflight() error {
	inflight, err := s.cfg.Spool.Inflight()
	if err != nil {
		return err
	}
	for _, batch := range inflight {
		if _, err := s.cfg.Spool.MarkPending(batch); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) SendHeartbeat(ctx context.Context) error {
	stats, err := s.cfg.Spool.Stats()
	if err != nil {
		return err
	}
	_, sourceIDs := sourceIDSet(s.cfg.Sources, s.cfg.Identity)
	sources := make([]ingest.HeartbeatSource, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		sources = append(sources, ingest.HeartbeatSource{SourceID: sourceID, Status: "healthy"})
	}
	_, err = s.cfg.Client.SendHeartbeat(ctx, ingest.HeartbeatRequest{
		Schema:            ingest.SchemaV1,
		CollectorID:       s.cfg.Identity.CollectorID,
		NodeID:            s.cfg.Identity.NodeID,
		ControlPlaneEpoch: s.cfg.Identity.ControlPlaneEpoch,
		QueueDepth:        stats.PendingCount + stats.InflightCount,
		SpoolBytes:        stats.ActiveBytes,
		Sources:           sources,
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.status.LastHeartbeatAt = time.Now().UTC()
	s.status.LastHeartbeatError = ""
	s.mu.Unlock()
	return nil
}

func (s *Service) Status() Status {
	stats, err := s.cfg.Spool.Stats()
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status
	if err == nil {
		status.Spool = stats
	}
	return status
}

func (s *Service) spoolReadResult(ctx context.Context, src capture.WatchSource, result capture.SourceReadResult) error {
	sourceID := s.cfg.Identity.Sources[src.Name].SourceID
	if sourceID == "" {
		return fmt.Errorf("source %q has no source_id assignment", src.Name)
	}
	events := RedactEvents(result.Events)
	captureErrors := RedactCaptureErrors(result.CaptureErrors)
	checkpoints := enrichCollectorCheckpoints(result.Checkpoint, s.cfg.Identity, src.Name, sourceID)
	if len(events) == 0 && len(captureErrors) == 0 && len(checkpoints) == 0 {
		return nil
	}
	sequence := s.cfg.State.Next()
	req, err := s.buildBatchRequest(sequence, sourceID, events, captureErrors, checkpoints)
	if err != nil {
		return err
	}
	if err := s.validateBatchBodySize(req); err != nil {
		return err
	}
	written, err := s.cfg.Spool.WritePending(ctx, req)
	if err != nil {
		return err
	}
	if err := s.cfg.State.MarkSpooled(sequence+1, req.Checkpoints); err != nil {
		_ = s.cfg.Spool.Discard(*written)
		return err
	}
	return nil
}

func (s *Service) spoolOversizeReadResult(ctx context.Context, src capture.WatchSource, result capture.SourceReadResult) error {
	if result.Checkpoint == nil {
		return fmt.Errorf("%w: oversized source result has no checkpoint", ErrBatchTooLarge)
	}
	errRow, ok := oversizedCaptureError(src, result)
	if !ok {
		return fmt.Errorf("%w: oversized source result has no record to skip", ErrBatchTooLarge)
	}
	return s.spoolReadResult(ctx, src, capture.SourceReadResult{
		CaptureErrors: []models.CaptureError{errRow},
		Checkpoint:    result.Checkpoint,
	})
}

func oversizedCaptureError(src capture.WatchSource, result capture.SourceReadResult) (models.CaptureError, bool) {
	if len(result.Events) > 0 {
		event := result.Events[0]
		fragment := firstNonEmptyCollector(event.RawPayload, event.TextContent, event.ToolInput, event.ToolOutput, event.ErrorMessage)
		return models.CaptureError{
			ID:              oversizedCaptureErrorID(src.Name, event.SourceFile, event.SourceLineNo, event.SourceOffset, event.SourceGeneration),
			SourceName:      src.Name,
			SourceFile:      event.SourceFile,
			SourceLineNo:    event.SourceLineNo,
			SourceOffset:    event.SourceOffset,
			ErrorClass:      "oversize_record",
			ErrorMessage:    "capture record exceeds ingest batch size limit and was skipped",
			ContextFragment: truncateCollectorFragment(fragment, 500),
		}, true
	}
	if len(result.CaptureErrors) > 0 {
		errRow := result.CaptureErrors[0]
		errRow.ID = oversizedCaptureErrorID(src.Name, errRow.SourceFile, errRow.SourceLineNo, errRow.SourceOffset, 0)
		errRow.SourceName = src.Name
		errRow.ErrorClass = "oversize_record"
		errRow.ErrorMessage = "capture error exceeds ingest batch size limit and was skipped"
		errRow.ContextFragment = truncateCollectorFragment(errRow.ContextFragment, 500)
		return errRow, true
	}
	return models.CaptureError{}, false
}

func oversizedCaptureErrorID(sourceName, file string, lineNo int, offset int64, sourceGeneration int) string {
	h := sha256.New()
	fmt.Fprintf(h, "oversize-record\x00%s\x00%s\x00%d\x00%d\x00%d", sourceName, file, lineNo, offset, sourceGeneration)
	return "capture_error_" + hex.EncodeToString(h.Sum(nil))[:32]
}

func firstNonEmptyCollector(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func truncateCollectorFragment(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

func (s *Service) buildBatchRequest(sequence uint64, sourceID string, events []capture.NormalizedEvent, captureErrors []models.CaptureError, checkpoints []models.Checkpoint) (ingest.BatchRequest, error) {
	req := ingest.BatchRequest{
		Schema:            ingest.SchemaV1,
		BatchID:           collectorBatchID(s.cfg.Identity, sequence, events, checkpoints),
		CollectorID:       s.cfg.Identity.CollectorID,
		NodeID:            s.cfg.Identity.NodeID,
		ControlPlaneEpoch: s.cfg.Identity.ControlPlaneEpoch,
		CreatedAt:         time.Now().UTC(),
		Sequence:          sequence,
		RedactionVersion:  RedactionVersion,
		SourceIDs:         []string{sourceID},
		Events:            events,
		CaptureErrors:     captureErrors,
		Checkpoints:       checkpoints,
	}
	digest, err := ingest.ComputeBatchDigest(req)
	if err != nil {
		return ingest.BatchRequest{}, err
	}
	req.PayloadDigest = digest
	return req, nil
}

func (s *Service) validateBatchBodySize(req ingest.BatchRequest) error {
	return validateBatchBodySizeLimit(req, s.cfg.MaxBatchBodyBytes)
}

func validateBatchBodySizeLimit(req ingest.BatchRequest, maxBytes int) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return validateEncodedBatchBodySize(body, maxBytes)
}

func validateEncodedBatchBodySize(body []byte, maxBytes int) error {
	if len(body) > maxBytes {
		return fmt.Errorf("%w: JSON body %d > %d", ErrBatchTooLarge, len(body), maxBytes)
	}
	compressedLen, err := gzipEncodedLen(body)
	if err != nil {
		return err
	}
	if compressedLen > maxBytes {
		return fmt.Errorf("%w: gzip body exceeds ingest limit: %d > %d", ErrBatchTooLarge, compressedLen, maxBytes)
	}
	return nil
}

func gzipEncodedLen(body []byte) (int, error) {
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(body); err != nil {
		_ = gz.Close()
		return 0, err
	}
	if err := gz.Close(); err != nil {
		return 0, err
	}
	return compressed.Len(), nil
}

func (s *Service) recoverSpooledStateFromSpool() error {
	active, err := s.cfg.Spool.Active()
	if err != nil {
		return err
	}
	var next uint64
	var checkpoints []models.Checkpoint
	for _, batch := range active {
		if batch.Request.Sequence >= next {
			next = batch.Request.Sequence + 1
		}
		checkpoints = append(checkpoints, batch.Request.Checkpoints...)
	}
	if next == 0 && len(checkpoints) == 0 {
		return nil
	}
	return s.cfg.State.MarkSpooled(next, checkpoints)
}

func enrichCollectorCheckpoints(cp *models.Checkpoint, identity capture.FleetIdentity, sourceName, sourceID string) []models.Checkpoint {
	if cp == nil {
		return nil
	}
	next := *cp
	next.NodeID = identity.NodeID
	next.CollectorID = identity.CollectorID
	next.SourceID = sourceID
	next.SourceName = sourceName
	return []models.Checkpoint{next}
}

func collectorBatchID(identity capture.FleetIdentity, sequence uint64, events []capture.NormalizedEvent, checkpoints []models.Checkpoint) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%d", identity.CollectorID, identity.NodeID, identity.ControlPlaneEpoch, sequence)
	for _, event := range events {
		fmt.Fprintf(h, "\x00%s\x00%d\x00%d\x00%s", event.SourceFile, event.SourceLineNo, event.SourceOffset, event.RawPayload)
	}
	for _, checkpoint := range checkpoints {
		fmt.Fprintf(h, "\x00%s\x00%s\x00%d", checkpoint.SourceName, checkpoint.SourceFile, checkpoint.LastOffset)
	}
	return "batch_" + hex.EncodeToString(h.Sum(nil))[:32]
}

func resolveSourceGlobs(globs []string) []string {
	seen := map[string]struct{}{}
	var files []string
	for _, glob := range globs {
		matches, err := doublestar.FilepathGlob(expandHome(glob))
		if err != nil {
			continue
		}
		for _, match := range matches {
			if _, ok := seen[match]; ok {
				continue
			}
			seen[match] = struct{}{}
			files = append(files, match)
		}
	}
	sort.Strings(files)
	return files
}

func sourceIDSet(sources []capture.WatchSource, identity capture.FleetIdentity) (map[string]struct{}, []string) {
	set := map[string]struct{}{}
	for _, source := range sources {
		if sourceID := identity.Sources[source.Name].SourceID; sourceID != "" {
			set[sourceID] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for sourceID := range set {
		out = append(out, sourceID)
	}
	sort.Strings(out)
	return set, out
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func (s *Service) retryDelay() time.Duration {
	delay := s.nextRetry
	if delay <= 0 {
		delay = s.cfg.RetryMin
	}
	s.nextRetry *= 2
	if s.nextRetry > s.cfg.RetryMax {
		s.nextRetry = s.cfg.RetryMax
	}
	return delay
}

func (s *Service) sleepRetry(ctx context.Context) error {
	delay := s.retryDelay()
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryableSendError(err error) bool {
	var sendErr *SendError
	return err != nil && errors.As(err, &sendErr) && sendErr.Retryable
}

func (s *Service) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.status.LastError = err.Error()
	}
}

func (s *Service) setHeartbeatError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.status.LastHeartbeatError = err.Error()
	}
}

func (s *Service) setSpoolFull(full bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.BlockedSpoolFull = full
	if full {
		s.status.LastError = ErrSpoolFull.Error()
	}
}

func (s *Service) setScanned() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.LastScanAt = time.Now().UTC()
}

func (s *Service) setAcked(batchID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.LastBatchID = batchID
	s.status.LastAckAt = time.Now().UTC()
	s.status.LastError = ""
	s.status.BlockedSpoolFull = false
	s.status.BlockedTerminal = false
}

func (s *Service) setTerminalBlocked(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.BlockedTerminal = true
	if err != nil {
		s.status.LastError = err.Error()
	}
}

func (s *Service) clearTerminalBlocked() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.BlockedTerminal = false
}

func (s *Service) terminalBlocked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status.BlockedTerminal
}
