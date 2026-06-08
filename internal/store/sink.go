package store

import (
	"context"
	"fmt"

	"github.com/johnnygreco/beacon/internal/models"
)

type RowBatch struct {
	RawRecords     []models.RawRecord
	ActivityEvents []models.Event
	EventLinks     []models.EventLink
	ToolPayloads   []models.ToolPayload
	CaptureErrors  []models.CaptureError
	Checkpoints    []models.Checkpoint
}

func (s *Store) Flush(ctx context.Context, rows RowBatch) error {
	if len(rows.RawRecords) > 0 {
		if err := s.insertRawRecords(ctx, rows.RawRecords); err != nil {
			return fmt.Errorf("insert raw records: %w", err)
		}
	}
	if len(rows.ActivityEvents) > 0 {
		if err := s.insertActivityEvents(ctx, rows.ActivityEvents); err != nil {
			return fmt.Errorf("insert activity events: %w", err)
		}
	}
	if len(rows.EventLinks) > 0 {
		if err := s.insertEventLinks(ctx, rows.EventLinks); err != nil {
			return fmt.Errorf("insert event links: %w", err)
		}
	}
	if len(rows.ToolPayloads) > 0 {
		if err := s.insertToolPayloads(ctx, rows.ToolPayloads); err != nil {
			return fmt.Errorf("insert tool payloads: %w", err)
		}
	}
	if len(rows.CaptureErrors) > 0 {
		if err := s.insertCaptureErrors(ctx, rows.CaptureErrors); err != nil {
			return fmt.Errorf("insert capture errors: %w", err)
		}
	}
	if len(rows.Checkpoints) > 0 {
		if err := s.insertCheckpoints(ctx, rows.Checkpoints); err != nil {
			return fmt.Errorf("insert checkpoints: %w", err)
		}
	}
	if len(rows.ActivityEvents) > 0 {
		ids := sessionIDs(rows.ActivityEvents)
		if err := s.RefreshSessionProjections(ctx, ids); err != nil {
			return fmt.Errorf("refresh session projections: %w", err)
		}
		if err := s.RefreshAnalyticsProjections(ctx, ids); err != nil {
			return fmt.Errorf("refresh analytics projections: %w", err)
		}
		if _, err := s.RefreshSearchIndexForSessions(ctx, ids, 0); err != nil {
			return fmt.Errorf("refresh search index: %w", err)
		}
	}
	return nil
}

func (s *Store) InsertCaptureError(ctx context.Context, errRow models.CaptureError) error {
	return s.insertCaptureErrors(ctx, []models.CaptureError{errRow})
}
