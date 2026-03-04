package database

import (
	"context"
	"github.com/technodrome-ai/technodrome/internal/models"
)

// InsertSession inserts a session using the write connection.
func InsertSession(ctx context.Context, conn *DB, s *models.Session) error {
	_, err := conn.writeConn.ExecContext(ctx,
		`INSERT INTO sessions (id, actor_id, source, started_at, ended_at, cwd, git_repo, total_cost)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET ended_at = excluded.ended_at, total_cost = excluded.total_cost`,
		s.ID, s.ActorID, s.Source, s.StartedAt, s.EndedAt, s.CWD, s.GitRepo, s.TotalCost,
	)
	return err
}

// InsertTurn inserts a turn.
func InsertTurn(ctx context.Context, conn *DB, t *models.Turn) error {
	_, err := conn.writeConn.ExecContext(ctx,
		`INSERT INTO turns (id, session_id, turn_number, user_prompt, started_at, ended_at, input_tokens, output_tokens, cache_read, cache_create, cost_usd)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET ended_at = excluded.ended_at, input_tokens = excluded.input_tokens, output_tokens = excluded.output_tokens, cost_usd = excluded.cost_usd`,
		t.ID, t.SessionID, t.TurnNumber, t.UserPrompt, t.StartedAt, t.EndedAt,
		t.InputTokens, t.OutputTokens, t.CacheRead, t.CacheCreate, t.CostUSD,
	)
	return err
}

// InsertModelCall inserts a model call.
func InsertModelCall(ctx context.Context, conn *DB, mc *models.ModelCall) error {
	_, err := conn.writeConn.ExecContext(ctx,
		`INSERT INTO model_calls (id, session_id, turn_id, model, provider, input_tokens, output_tokens, cache_read, cache_create, duration_ms, status_code, cost_usd, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mc.ID, mc.SessionID, mc.TurnID, mc.Model, mc.Provider,
		mc.InputTokens, mc.OutputTokens, mc.CacheRead, mc.CacheCreate,
		mc.DurationMs, mc.StatusCode, mc.CostUSD, mc.CreatedAt,
	)
	return err
}

// InsertToolCall inserts a tool call.
func InsertToolCall(ctx context.Context, conn *DB, tc *models.ToolCall) error {
	_, err := conn.writeConn.ExecContext(ctx,
		`INSERT INTO tool_calls (id, session_id, turn_id, tool_name, input, output, success, duration_ms, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tc.ID, tc.SessionID, tc.TurnID, tc.ToolName, tc.Input, tc.Output,
		tc.Success, tc.DurationMs, tc.CreatedAt,
	)
	return err
}

// InsertApiError inserts an API error.
func InsertApiError(ctx context.Context, conn *DB, ae *models.ApiError) error {
	_, err := conn.writeConn.ExecContext(ctx,
		`INSERT INTO api_errors (id, session_id, turn_id, error_code, error_class, message, provider, retry_count, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ae.ID, ae.SessionID, ae.TurnID, ae.ErrorCode, ae.ErrorClass, ae.Message,
		ae.Provider, ae.RetryCount, ae.CreatedAt,
	)
	return err
}

// InsertContextSnapshot inserts a context snapshot.
func InsertContextSnapshot(ctx context.Context, conn *DB, cs *models.ContextSnapshot) error {
	_, err := conn.writeConn.ExecContext(ctx,
		`INSERT INTO context_snapshots (id, session_id, turn_id, tokens_in_context, max_tokens, headroom, compaction_event, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		cs.ID, cs.SessionID, cs.TurnID, cs.TokensInContext, cs.MaxTokens,
		cs.Headroom, cs.CompactionEvent, cs.CreatedAt,
	)
	return err
}

// InsertDocument inserts a document (without embedding initially).
func InsertDocument(ctx context.Context, conn *DB, d *models.Document) error {
	_, err := conn.writeConn.ExecContext(ctx,
		`INSERT INTO documents (id, session_id, turn_id, doc_type, content, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		d.ID, d.SessionID, d.TurnID, d.DocType, d.Content, d.CreatedAt,
	)
	return err
}


// InsertRawEvent inserts a raw event.
func InsertRawEvent(ctx context.Context, conn *DB, re *models.RawEvent) error {
	_, err := conn.writeConn.ExecContext(ctx,
		`INSERT INTO raw_events (id, session_id, source, event_type, payload, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		re.ID, re.SessionID, re.Source, re.EventType, re.Payload, re.CreatedAt,
	)
	return err
}

// InsertActor inserts or updates an actor.
func InsertActor(ctx context.Context, conn *DB, a *models.Actor) error {
	_, err := conn.writeConn.ExecContext(ctx,
		`INSERT INTO actors (id, user_id, org_team, machine_id, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO NOTHING`,
		a.ID, a.UserID, a.OrgTeam, a.MachineID, a.CreatedAt,
	)
	return err
}
