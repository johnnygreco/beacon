package store

import (
	"context"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
)

func (s *Store) InsertCaptureHeartbeats(ctx context.Context, heartbeats []models.CaptureHeartbeat) error {
	if len(heartbeats) == 0 {
		return nil
	}
	batch, err := s.native.PrepareBatch(ctx, `INSERT INTO capture_heartbeats (
			node_id, collector_id, source_id, source_name, control_plane_epoch,
			status, queue_depth, spool_bytes, active_files, error_count,
			last_event_at, append_to_visible_ms, created_at
		)`)
	if err != nil {
		return err
	}
	defer batch.Close()
	now := time.Now().UTC()
	for _, h := range heartbeats {
		if err := batch.Append(
			h.NodeID,
			h.CollectorID,
			h.SourceID,
			h.SourceName,
			h.ControlPlaneEpoch,
			h.Status,
			uint32(nonNegativeInt(h.QueueDepth)),
			uint64(nonNegativeInt64(h.SpoolBytes)),
			uint32(nonNegativeInt(h.ActiveFiles)),
			uint64(nonNegativeInt(h.ErrorCount)),
			h.LastEventAt,
			uint64(0),
			nonZeroTime(h.CreatedAt, now),
		); err != nil {
			return err
		}
	}
	return batch.Send()
}
