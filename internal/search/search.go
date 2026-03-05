package search

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type SearchQuery struct {
	Query          string
	Limit          int
	MinScore       float64
	SessionID      string
	EventKinds     []string
	ExcludeMCPSelf bool
}

type SearchResult struct {
	EventUID    string    `json:"event_uid"`
	SessionID   string    `json:"session_id"`
	EventKind   string    `json:"event_kind"`
	TextPreview string    `json:"text_preview"`
	Score       float64   `json:"score"`
	Timestamp   time.Time `json:"timestamp"`
	ToolName    string    `json:"tool_name"`
	Model       string    `json:"model"`
}

type Searcher struct {
	db              *sql.DB
	logger          *slog.Logger
	rebuildInterval time.Duration
	maxResults      int
	mu              sync.RWMutex
	lastIndexBuild  time.Time
	indexExists     bool
}

func NewSearcher(db *sql.DB, logger *slog.Logger, maxResults int, rebuildInterval time.Duration) *Searcher {
	return &Searcher{
		db:              db,
		logger:          logger,
		maxResults:      maxResults,
		rebuildInterval: rebuildInterval,
	}
}

// RunIndexer rebuilds the FTS index periodically. Call from a goroutine.
func (s *Searcher) RunIndexer(ctx context.Context) {
	s.rebuildIndex()

	if s.rebuildInterval <= 0 {
		return
	}

	ticker := time.NewTicker(s.rebuildInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.rebuildIndex()
		}
	}
}

func (s *Searcher) rebuildIndex() {
	// Check if there are any rows
	var count int64
	if err := s.db.QueryRow("SELECT COUNT(*) FROM events WHERE text_content IS NOT NULL AND text_content != ''").Scan(&count); err != nil {
		s.logger.Error("FTS row count check failed", "error", err)
		return
	}
	if count == 0 {
		return
	}

	if _, err := s.db.Exec("INSTALL fts"); err != nil {
		// ignore
	}
	if _, err := s.db.Exec("LOAD fts"); err != nil {
		s.logger.Error("LOAD fts failed", "error", err)
		return
	}

	_, err := s.db.Exec("PRAGMA create_fts_index('events', 'event_uid', 'text_content', overwrite=1)")
	if err != nil {
		s.logger.Error("FTS index rebuild failed", "error", err)
		return
	}

	s.mu.Lock()
	s.lastIndexBuild = time.Now()
	s.indexExists = true
	s.mu.Unlock()

	s.logger.Info("FTS index rebuilt", "documents", count)
}

// ProbeIndex checks if an FTS index exists (for read-only connections).
func (s *Searcher) ProbeIndex() {
	row := s.db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'fts_main_events'")
	var count int
	if err := row.Scan(&count); err == nil && count > 0 {
		s.mu.Lock()
		s.indexExists = true
		s.mu.Unlock()
	}
}

// Search performs BM25 + ILIKE recency search.
func (s *Searcher) Search(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	if q.Limit <= 0 {
		q.Limit = s.maxResults
	}
	if q.Limit <= 0 {
		q.Limit = 25
	}

	s.mu.RLock()
	hasIndex := s.indexExists
	lastBuild := s.lastIndexBuild
	s.mu.RUnlock()

	var bm25Results []SearchResult
	var recencyResults []SearchResult

	// BM25 path
	if hasIndex {
		results, err := s.bm25Search(ctx, q)
		if err != nil {
			s.logger.Warn("BM25 search failed, falling back to ILIKE", "error", err)
		} else {
			bm25Results = results
		}
	}

	// Recency path: ILIKE for events newer than last index build
	if !lastBuild.IsZero() || !hasIndex {
		results, err := s.ilikeSearch(ctx, q, lastBuild)
		if err != nil {
			s.logger.Warn("ILIKE search failed", "error", err)
		} else {
			recencyResults = results
		}
	}

	// Merge: BM25 first, then recency (deduped)
	seen := make(map[string]bool)
	var merged []SearchResult
	for _, r := range bm25Results {
		if !seen[r.EventUID] {
			seen[r.EventUID] = true
			merged = append(merged, r)
		}
	}
	for _, r := range recencyResults {
		if !seen[r.EventUID] {
			seen[r.EventUID] = true
			merged = append(merged, r)
		}
	}

	if len(merged) > q.Limit {
		merged = merged[:q.Limit]
	}

	return merged, nil
}

// LegacySearch provides a simple interface for web handlers.
func (s *Searcher) LegacySearch(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return s.Search(ctx, SearchQuery{Query: query, Limit: limit})
}

func (s *Searcher) bm25Search(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	query := fmt.Sprintf(
		`SELECT e.event_uid, e.session_id, e.event_kind, e.text_preview,
		        fts.score, e.timestamp, COALESCE(e.tool_name, ''), COALESCE(e.model, '')
		 FROM (SELECT event_uid, fts_main_events.match_bm25(event_uid, $1, fields := 'text_content') AS score
		       FROM fts_main_events) fts
		 JOIN events e ON e.event_uid = fts.event_uid
		 WHERE fts.score IS NOT NULL %s
		 ORDER BY fts.score DESC
		 LIMIT $2`,
		s.buildFilters(q),
	)

	rows, err := s.db.QueryContext(ctx, query, q.Query, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanResults(rows)
}

func (s *Searcher) ilikeSearch(ctx context.Context, q SearchQuery, since time.Time) ([]SearchResult, error) {
	pattern := "%" + strings.ReplaceAll(strings.ReplaceAll(q.Query, "%", "\\%"), "_", "\\_") + "%"

	var whereExtra string
	var args []any
	args = append(args, pattern)

	if !since.IsZero() {
		whereExtra = " AND e.created_at > $2"
		args = append(args, since)
	}
	whereExtra += " " + s.buildFilters(q)

	query := fmt.Sprintf(
		`SELECT e.event_uid, e.session_id, e.event_kind, e.text_preview,
		        0.0 AS score, e.timestamp, COALESCE(e.tool_name, ''), COALESCE(e.model, '')
		 FROM events e
		 WHERE e.text_content ILIKE $1 %s
		 ORDER BY e.timestamp DESC
		 LIMIT %d`,
		whereExtra, q.Limit,
	)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanResults(rows)
}

func (s *Searcher) buildFilters(q SearchQuery) string {
	var clauses []string

	if q.SessionID != "" {
		clauses = append(clauses, fmt.Sprintf("AND e.session_id = '%s'", strings.ReplaceAll(q.SessionID, "'", "''")))
	}

	if len(q.EventKinds) > 0 {
		quoted := make([]string, len(q.EventKinds))
		for i, k := range q.EventKinds {
			quoted[i] = fmt.Sprintf("'%s'", strings.ReplaceAll(k, "'", "''"))
		}
		clauses = append(clauses, fmt.Sprintf("AND e.event_kind IN (%s)", strings.Join(quoted, ",")))
	}

	if q.ExcludeMCPSelf {
		clauses = append(clauses, "AND e.text_content NOT ILIKE '%technodrome%'")
		clauses = append(clauses, "AND (e.tool_name IS NULL OR e.tool_name NOT IN ('search', 'open', 'list_sessions'))")
	}

	return strings.Join(clauses, " ")
}

func scanResults(rows *sql.Rows) ([]SearchResult, error) {
	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.EventUID, &r.SessionID, &r.EventKind, &r.TextPreview, &r.Score, &r.Timestamp, &r.ToolName, &r.Model); err != nil {
			continue
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// LastIndexBuild returns when the FTS index was last rebuilt.
func (s *Searcher) LastIndexBuild() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastIndexBuild
}

// IndexExists returns whether an FTS index exists.
func (s *Searcher) IndexExists() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.indexExists
}
