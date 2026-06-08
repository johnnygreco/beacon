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
	activitySessionIDs := sessionIDs(rows.ActivityEvents)
	previousSearchProjects, err := s.sessionSearchProjects(ctx, activitySessionIDs)
	if err != nil {
		return fmt.Errorf("query search project metadata: %w", err)
	}

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
		if err := s.RefreshSessionProjections(ctx, activitySessionIDs); err != nil {
			return fmt.Errorf("refresh session projections: %w", err)
		}
		if err := s.RefreshAnalyticsProjections(ctx, activitySessionIDs); err != nil {
			return fmt.Errorf("refresh analytics projections: %w", err)
		}
		currentSearchProjects, err := s.sessionSearchProjects(ctx, activitySessionIDs)
		if err != nil {
			return fmt.Errorf("query updated search project metadata: %w", err)
		}
		changedSearchProjectSessions := changedSessionSearchProjects(activitySessionIDs, previousSearchProjects, currentSearchProjects)
		if len(changedSearchProjectSessions) > 0 {
			if _, err := s.RefreshSearchIndexForSessions(ctx, changedSearchProjectSessions, 0); err != nil {
				return fmt.Errorf("refresh search index: %w", err)
			}
		}
		if events := eventsOutsideSessions(rows.ActivityEvents, changedSearchProjectSessions); len(events) > 0 {
			if _, err := s.RefreshSearchIndexForEvents(ctx, events, 0); err != nil {
				return fmt.Errorf("refresh search index: %w", err)
			}
		}
	}
	return nil
}

type sessionSearchProject struct {
	projectPath string
	projectKey  string
	single      bool
}

func (s *Store) sessionSearchProjects(ctx context.Context, ids []string) (map[string]sessionSearchProject, error) {
	ids = uniqStrings(ids)
	projects := make(map[string]sessionSearchProject, len(ids))
	for _, id := range ids {
		projects[id] = sessionSearchProject{}
	}
	if len(ids) == 0 || s == nil || s.DB == nil {
		return projects, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT session_id, project_path, project_key, single_project
		 FROM `+sessionProjectFallbackSQL(placeholders(len(ids)))+` AS fallback`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sessionID string
		var project sessionSearchProject
		var single uint8
		if err := rows.Scan(&sessionID, &project.projectPath, &project.projectKey, &single); err != nil {
			return nil, err
		}
		project.single = single != 0
		projects[sessionID] = project
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return projects, nil
}

func changedSessionSearchProjects(ids []string, previous, current map[string]sessionSearchProject) []string {
	ids = uniqStrings(ids)
	changed := make([]string, 0, len(ids))
	for _, id := range ids {
		if previous[id] != current[id] {
			changed = append(changed, id)
		}
	}
	return changed
}

func eventsOutsideSessions(events []models.Event, sessionIDs []string) []models.Event {
	if len(events) == 0 || len(sessionIDs) == 0 {
		return events
	}
	excluded := make(map[string]struct{}, len(sessionIDs))
	for _, id := range sessionIDs {
		excluded[id] = struct{}{}
	}
	filtered := make([]models.Event, 0, len(events))
	for _, event := range events {
		if _, ok := excluded[event.SessionID]; ok {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func (s *Store) InsertCaptureError(ctx context.Context, errRow models.CaptureError) error {
	return s.insertCaptureErrors(ctx, []models.CaptureError{errRow})
}
