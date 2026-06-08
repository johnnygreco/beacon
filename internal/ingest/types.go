package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/capture"
	"github.com/johnnygreco/beacon/internal/models"
)

const (
	SchemaV1 = "beacon.ingest.v1"

	MaxBodyBytes = 32 << 20

	RedactionVersionV1 = "redact-v1"

	StatusCommitted       = "committed"
	StatusRetryable       = "retryable"
	StatusTerminalFailure = "terminal_failure"
)

type EnrollRequest struct {
	Schema              string          `json:"schema"`
	Bootstrap           EnrollBootstrap `json:"bootstrap"`
	ExistingIngestToken string          `json:"existing_ingest_token,omitempty"`
}

type EnrollBootstrap struct {
	NodeID        string                     `json:"node_id,omitempty"`
	NodeName      string                     `json:"node_name,omitempty"`
	CollectorID   string                     `json:"collector_id,omitempty"`
	CollectorName string                     `json:"collector_name,omitempty"`
	Sources       []EnrollSourceRegistration `json:"sources,omitempty"`
}

type EnrollSourceRegistration struct {
	Name      string `json:"name"`
	Runtime   string `json:"runtime"`
	Provider  string `json:"provider"`
	Format    string `json:"format"`
	WatchRoot string `json:"watch_root"`
}

type EnrollResponse struct {
	Schema      string           `json:"schema"`
	Assignment  EnrollAssignment `json:"assignment"`
	IngestToken string           `json:"ingest_token"`
}

type EnrollAssignment struct {
	NodeID            string                   `json:"node_id"`
	CollectorID       string                   `json:"collector_id"`
	SourceIDs         []string                 `json:"source_ids"`
	ControlPlaneEpoch string                   `json:"control_plane_epoch"`
	Sources           []EnrollSourceAssignment `json:"sources"`
}

type EnrollSourceAssignment struct {
	Name     string `json:"name"`
	SourceID string `json:"source_id"`
}

type BatchRequest struct {
	Schema            string                    `json:"schema"`
	BatchID           string                    `json:"batch_id"`
	CollectorID       string                    `json:"collector_id"`
	NodeID            string                    `json:"node_id"`
	ControlPlaneEpoch string                    `json:"control_plane_epoch"`
	CreatedAt         time.Time                 `json:"created_at"`
	Sequence          uint64                    `json:"sequence"`
	PayloadDigest     string                    `json:"payload_digest"`
	RedactionVersion  string                    `json:"redaction_version"`
	SourceIDs         []string                  `json:"source_ids"`
	Events            []capture.NormalizedEvent `json:"events"`
	CaptureErrors     []models.CaptureError     `json:"capture_errors,omitempty"`
	Checkpoints       []models.Checkpoint       `json:"checkpoints,omitempty"`
}

type BatchAck struct {
	Status            string `json:"status"`
	BatchID           string `json:"batch_id"`
	PayloadDigest     string `json:"payload_digest"`
	EventsWritten     int    `json:"events_written"`
	RawRecordsWritten int    `json:"raw_records_written"`
	NextSequence      uint64 `json:"next_sequence"`
	ControlPlaneEpoch string `json:"control_plane_epoch"`
}

type HeartbeatRequest struct {
	Schema            string            `json:"schema"`
	CollectorID       string            `json:"collector_id"`
	NodeID            string            `json:"node_id"`
	ControlPlaneEpoch string            `json:"control_plane_epoch"`
	QueueDepth        int               `json:"queue_depth"`
	SpoolBytes        int64             `json:"spool_bytes"`
	ActiveFiles       int               `json:"active_files"`
	Sources           []HeartbeatSource `json:"sources,omitempty"`
}

type HeartbeatSource struct {
	SourceID    string     `json:"source_id"`
	Status      string     `json:"status"`
	LastEventAt *time.Time `json:"last_event_at,omitempty"`
	ErrorCount  int        `json:"error_count,omitempty"`
}

type HeartbeatResponse struct {
	Schema            string `json:"schema"`
	Status            string `json:"status"`
	ControlPlaneEpoch string `json:"control_plane_epoch"`
}

func NormalizeBatch(req BatchRequest) BatchRequest {
	req.Schema = strings.TrimSpace(req.Schema)
	req.BatchID = strings.TrimSpace(req.BatchID)
	req.CollectorID = strings.TrimSpace(req.CollectorID)
	req.NodeID = strings.TrimSpace(req.NodeID)
	req.ControlPlaneEpoch = strings.TrimSpace(req.ControlPlaneEpoch)
	req.PayloadDigest = strings.TrimSpace(req.PayloadDigest)
	req.RedactionVersion = strings.TrimSpace(req.RedactionVersion)
	req.SourceIDs = normalizeStrings(req.SourceIDs)
	if !req.CreatedAt.IsZero() {
		req.CreatedAt = req.CreatedAt.UTC()
	}
	return req
}

func ValidateBatch(req BatchRequest) error {
	req = NormalizeBatch(req)
	if req.Schema != SchemaV1 {
		return fmt.Errorf("schema must be %q", SchemaV1)
	}
	if req.BatchID == "" {
		return fmt.Errorf("batch_id is required")
	}
	if req.CollectorID == "" {
		return fmt.Errorf("collector_id is required")
	}
	if req.NodeID == "" {
		return fmt.Errorf("node_id is required")
	}
	if req.ControlPlaneEpoch == "" {
		return fmt.Errorf("control_plane_epoch is required")
	}
	if req.CreatedAt.IsZero() {
		return fmt.Errorf("created_at is required")
	}
	if req.Sequence == 0 {
		return fmt.Errorf("sequence must be positive")
	}
	if len(req.SourceIDs) == 0 {
		return fmt.Errorf("source_ids must contain at least one source")
	}
	if len(req.Events) == 0 && len(req.CaptureErrors) == 0 && len(req.Checkpoints) == 0 {
		return fmt.Errorf("batch must contain events, capture_errors, or checkpoints")
	}
	if req.RedactionVersion != RedactionVersionV1 {
		return fmt.Errorf("redaction_version must be %q", RedactionVersionV1)
	}
	want, err := ComputeBatchDigest(req)
	if err != nil {
		return err
	}
	if req.PayloadDigest == "" {
		return fmt.Errorf("payload_digest is required")
	}
	if req.PayloadDigest != want {
		return fmt.Errorf("payload_digest mismatch")
	}
	return nil
}

func ComputeBatchDigest(req BatchRequest) (string, error) {
	req = NormalizeBatch(req)
	if req.CreatedAt.IsZero() {
		return "", fmt.Errorf("created_at is required")
	}
	req.PayloadDigest = ""
	payload, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("encode batch digest payload: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func normalizeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
