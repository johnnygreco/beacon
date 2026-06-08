package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	BatchStateReceiving         = "receiving"
	BatchStateWritingPrimary    = "writing_primary"
	BatchStateRefreshingDerived = "refreshing_derived"
	BatchStateCommitted         = "committed"
	BatchStateFailedRetryable   = "failed_retryable"
	BatchStateFailedTerminal    = "failed_terminal"
)

var (
	ErrIngestBatchDigestMismatch = errors.New("ingest batch payload digest mismatch")
	ErrIngestBatchSequenceGap    = errors.New("ingest batch sequence gap")
	ErrIngestBatchStaleSequence  = errors.New("ingest batch sequence is stale")
)

type IngestBatchMeta struct {
	CollectorID       string
	BatchID           string
	NodeID            string
	Sequence          uint64
	ControlPlaneEpoch string
	PayloadDigest     string
	RedactionVersion  string
	CreatedAt         time.Time
}

type IngestBatchRecord struct {
	IngestBatchMeta
	ReceivedAt       time.Time
	StateVersion     uint64
	EventCount       int
	RawCount         int
	ToolPayloadCount int
	CheckpointCount  int
	Status           string
	ErrorMessage     string
	CommittedAt      *time.Time
	UpdatedAt        time.Time
}

type IngestBatchAck struct {
	BatchID           string
	PayloadDigest     string
	EventsWritten     int
	RawRecordsWritten int
	NextSequence      uint64
	ControlPlaneEpoch string
}

func (s *Store) CommitIngestBatch(ctx context.Context, meta IngestBatchMeta, rows RowBatch) (IngestBatchAck, error) {
	meta.CreatedAt = nonZeroTime(meta.CreatedAt, time.Now().UTC())
	if existing, ok, err := s.GetIngestBatch(ctx, meta.CollectorID, meta.BatchID); err != nil {
		return IngestBatchAck{}, err
	} else if ok {
		if existing.PayloadDigest != meta.PayloadDigest {
			return IngestBatchAck{}, ErrIngestBatchDigestMismatch
		}
		if existing.Status == BatchStateCommitted {
			return ackFromRecord(existing), nil
		}
	}

	stateVersion, err := s.nextIngestBatchStateVersion(ctx, meta.CollectorID, meta.BatchID)
	if err != nil {
		return IngestBatchAck{}, err
	}
	if err := s.validateBatchSequence(ctx, meta); err != nil {
		record := batchRecordFromRows(meta, rows, stateVersion, BatchStateFailedTerminal, err.Error())
		_ = s.insertIngestBatchRecord(ctx, record)
		return IngestBatchAck{}, err
	}
	if err := s.insertIngestBatchRecord(ctx, batchRecordFromRows(meta, rows, stateVersion, BatchStateReceiving, "")); err != nil {
		return IngestBatchAck{}, err
	}
	stateVersion++
	if err := s.insertIngestBatchRecord(ctx, batchRecordFromRows(meta, rows, stateVersion, BatchStateWritingPrimary, "")); err != nil {
		return IngestBatchAck{}, err
	}
	stateVersion++
	if err := s.insertIngestBatchRecord(ctx, batchRecordFromRows(meta, rows, stateVersion, BatchStateRefreshingDerived, "")); err != nil {
		return IngestBatchAck{}, err
	}
	if err := s.Flush(ctx, rows); err != nil {
		stateVersion++
		record := batchRecordFromRows(meta, rows, stateVersion, BatchStateFailedRetryable, err.Error())
		_ = s.insertIngestBatchRecord(ctx, record)
		return IngestBatchAck{}, err
	}
	stateVersion++
	committed := batchRecordFromRows(meta, rows, stateVersion, BatchStateCommitted, "")
	now := time.Now().UTC()
	committed.CommittedAt = &now
	if err := s.insertIngestBatchRecord(ctx, committed); err != nil {
		return IngestBatchAck{}, err
	}
	return ackFromRecord(committed), nil
}

func (s *Store) GetIngestBatch(ctx context.Context, collectorID, batchID string) (IngestBatchRecord, bool, error) {
	var row IngestBatchRecord
	var committedAt sql.NullTime
	err := s.DB.QueryRowContext(ctx,
		`SELECT
				argMax(node_id, state_version),
				argMax(sequence, state_version),
				argMax(control_plane_epoch, state_version),
				argMax(payload_digest, state_version),
				argMax(redaction_version, state_version),
				argMax(created_at, state_version),
				argMax(received_at, state_version),
				max(state_version),
				argMax(event_count, state_version),
				argMax(raw_count, state_version),
				argMax(tool_payload_count, state_version),
				argMax(checkpoint_count, state_version),
				argMax(status, state_version),
				argMax(error_message, state_version),
				argMax(committed_at, state_version),
				argMax(updated_at, state_version)
			 FROM ingest_batches
			 WHERE collector_id = ? AND batch_id = ?
			 GROUP BY collector_id, batch_id`,
		collectorID,
		batchID,
	).Scan(
		&row.NodeID,
		&row.Sequence,
		&row.ControlPlaneEpoch,
		&row.PayloadDigest,
		&row.RedactionVersion,
		&row.CreatedAt,
		&row.ReceivedAt,
		&row.StateVersion,
		&row.EventCount,
		&row.RawCount,
		&row.ToolPayloadCount,
		&row.CheckpointCount,
		&row.Status,
		&row.ErrorMessage,
		&committedAt,
		&row.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return IngestBatchRecord{}, false, nil
	}
	if err != nil {
		return IngestBatchRecord{}, false, err
	}
	row.CollectorID = collectorID
	row.BatchID = batchID
	if committedAt.Valid {
		row.CommittedAt = &committedAt.Time
	}
	return row, true, nil
}

func (s *Store) nextIngestBatchStateVersion(ctx context.Context, collectorID, batchID string) (uint64, error) {
	var maxVersion uint64
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(max(state_version), 0)
		 FROM ingest_batches
		 WHERE collector_id = ? AND batch_id = ?`,
		collectorID,
		batchID,
	).Scan(&maxVersion); err != nil {
		return 0, err
	}
	return maxVersion + 1, nil
}

func (s *Store) validateBatchSequence(ctx context.Context, meta IngestBatchMeta) error {
	var maxSeq uint64
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(max(sequence), 0)
		 FROM ingest_batches
		 WHERE collector_id = ? AND status = ?`,
		meta.CollectorID,
		BatchStateCommitted,
	).Scan(&maxSeq); err != nil {
		return err
	}
	switch {
	case meta.Sequence == maxSeq+1:
		return nil
	case meta.Sequence <= maxSeq:
		return ErrIngestBatchStaleSequence
	default:
		return fmt.Errorf("%w: got %d want %d", ErrIngestBatchSequenceGap, meta.Sequence, maxSeq+1)
	}
}

func (s *Store) insertIngestBatchRecord(ctx context.Context, record IngestBatchRecord) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO ingest_batches (
				collector_id, batch_id, node_id, sequence, control_plane_epoch,
				payload_digest, redaction_version, created_at, received_at,
				state_version, event_count, raw_count, tool_payload_count, checkpoint_count,
				status, error_message, committed_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.CollectorID,
		record.BatchID,
		record.NodeID,
		record.Sequence,
		record.ControlPlaneEpoch,
		record.PayloadDigest,
		record.RedactionVersion,
		nonZeroTime(record.CreatedAt, time.Now().UTC()),
		nonZeroTime(record.ReceivedAt, time.Now().UTC()),
		record.StateVersion,
		uint64(nonNegativeInt(record.EventCount)),
		uint64(nonNegativeInt(record.RawCount)),
		uint64(nonNegativeInt(record.ToolPayloadCount)),
		uint64(nonNegativeInt(record.CheckpointCount)),
		record.Status,
		record.ErrorMessage,
		record.CommittedAt,
		nonZeroTime(record.UpdatedAt, time.Now().UTC()),
	)
	return err
}

func batchRecordFromRows(meta IngestBatchMeta, rows RowBatch, stateVersion uint64, status, errorMessage string) IngestBatchRecord {
	now := time.Now().UTC()
	return IngestBatchRecord{
		IngestBatchMeta:  meta,
		ReceivedAt:       now,
		StateVersion:     stateVersion,
		EventCount:       len(rows.ActivityEvents),
		RawCount:         len(rows.RawRecords),
		ToolPayloadCount: len(rows.ToolPayloads),
		CheckpointCount:  len(rows.Checkpoints),
		Status:           status,
		ErrorMessage:     errorMessage,
		UpdatedAt:        now,
	}
}

func ackFromRecord(record IngestBatchRecord) IngestBatchAck {
	return IngestBatchAck{
		BatchID:           record.BatchID,
		PayloadDigest:     record.PayloadDigest,
		EventsWritten:     record.EventCount,
		RawRecordsWritten: record.RawCount,
		NextSequence:      record.Sequence + 1,
		ControlPlaneEpoch: record.ControlPlaneEpoch,
	}
}
