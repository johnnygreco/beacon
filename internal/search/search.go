package search

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type SearchQuery struct {
	Query          string
	Limit          int
	MinScore       float64
	SessionID      string
	EventKinds     []string
	FromTime       time.Time
	ToTime         time.Time
	ExcludeMCPSelf bool
	SortBy         string // "relevance" (default), "newest", "oldest"
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
	Provider    string    `json:"provider"`
}

type Searcher struct {
	db              *sql.DB
	ftsConn         *sql.Conn // dedicated connection for FTS operations
	logger          *slog.Logger
	rebuildInterval time.Duration
	maxResults      int
	mu             sync.RWMutex
	lastIndexBuild time.Time
	indexExists    bool
	lastEventCount atomic.Int64
}

func NewSearcher(db *sql.DB, logger *slog.Logger, maxResults int, rebuildInterval time.Duration) *Searcher {
	return &Searcher{
		db:              db,
		logger:          logger,
		maxResults:      maxResults,
		rebuildInterval: rebuildInterval,
	}
}

// initFTSConn pins a dedicated connection and loads the FTS extension on it.
// Both index rebuilds and BM25 queries use this same connection.
func (s *Searcher) initFTSConn(ctx context.Context) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("pin FTS conn: %w", err)
	}
	conn.ExecContext(ctx, "INSTALL fts") //nolint:errcheck // may already be installed
	if _, err := conn.ExecContext(ctx, "LOAD fts"); err != nil {
		conn.Close()
		return fmt.Errorf("LOAD fts: %w", err)
	}
	s.ftsConn = conn
	return nil
}

// RunIndexer rebuilds the FTS index periodically. Call from a goroutine.
func (s *Searcher) RunIndexer(ctx context.Context) {
	if err := s.initFTSConn(ctx); err != nil {
		s.logger.Error("failed to init FTS connection", "error", err)
		return
	}

	s.RebuildIndex(ctx)

	if s.rebuildInterval <= 0 {
		return
	}

	ticker := time.NewTicker(s.rebuildInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if s.ftsConn != nil {
				s.ftsConn.Close()
			}
			return
		case <-ticker.C:
			s.RebuildIndex(ctx)
		}
	}
}

// RebuildIndex rebuilds the FTS index if the event count has changed.
func (s *Searcher) RebuildIndex(ctx context.Context) {
	if s.ftsConn == nil {
		return
	}

	// Check if there are any rows
	var count int64
	if err := s.ftsConn.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE text_content IS NOT NULL AND text_content != ''").Scan(&count); err != nil {
		s.logger.Error("FTS row count check failed", "error", err)
		return
	}
	if count == 0 {
		return
	}
	if count == s.lastEventCount.Load() {
		return
	}

	_, err := s.ftsConn.ExecContext(ctx, "PRAGMA create_fts_index('events', 'event_uid', 'text_content', overwrite=1)")
	if err != nil {
		s.logger.Error("FTS index rebuild failed", "error", err)
		return
	}

	s.mu.Lock()
	s.lastIndexBuild = time.Now()
	s.indexExists = true
	s.mu.Unlock()
	s.lastEventCount.Store(count)

	s.logger.Info("FTS index rebuilt", "documents", count)
}

// ProbeIndex checks if an FTS index exists (for read-only connections).
func (s *Searcher) ProbeIndex() {
	row := s.db.QueryRow("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = 'fts_main_events'")
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
	s.mu.RUnlock()

	searchStart := time.Now()
	var bm25Results []SearchResult
	var recencyResults []SearchResult

	// BM25 path
	bm25Start := time.Now()
	if hasIndex && s.ftsConn != nil {
		results, err := s.bm25Search(ctx, q)
		if err != nil {
			s.logger.Warn("BM25 search failed, falling back to ILIKE", "error", err)
		} else {
			bm25Results = results
		}
	}
	bm25Dur := time.Since(bm25Start)

	// Skip ILIKE when BM25 already returned enough results. BM25 only
	// indexes text_content, so tool_name/error_message matches need the
	// ILIKE path — but if BM25 filled the limit, extra results won't help.
	var ilikeDur time.Duration
	if len(bm25Results) < q.Limit {
		ilikeStart := time.Now()
		results, err := s.ilikeSearch(ctx, q, time.Time{})
		if err != nil {
			s.logger.Warn("ILIKE search failed", "error", err)
		} else {
			recencyResults = results
		}
		ilikeDur = time.Since(ilikeStart)
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

	// Apply sort order
	switch q.SortBy {
	case "newest":
		sort.Slice(merged, func(i, j int) bool {
			return merged[i].Timestamp.After(merged[j].Timestamp)
		})
	case "oldest":
		sort.Slice(merged, func(i, j int) bool {
			return merged[i].Timestamp.Before(merged[j].Timestamp)
		})
	default: // "relevance" — BM25 scores first, then by timestamp for unscored
		sort.SliceStable(merged, func(i, j int) bool {
			si, sj := merged[i].Score, merged[j].Score
			if si != sj {
				return si > sj
			}
			return merged[i].Timestamp.After(merged[j].Timestamp)
		})
	}

	if len(merged) > q.Limit {
		merged = merged[:q.Limit]
	}

	s.logger.Debug("Search complete",
		"query", q.Query,
		"bm25_results", len(bm25Results), "bm25_duration", bm25Dur,
		"ilike_results", len(recencyResults), "ilike_duration", ilikeDur,
		"merged_results", len(merged), "total_duration", time.Since(searchStart))

	return merged, nil
}

// LegacySearch provides a simple interface for web handlers.
func (s *Searcher) LegacySearch(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return s.Search(ctx, SearchQuery{Query: query, Limit: limit})
}

func (s *Searcher) bm25Search(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	// DuckDB v1.4+ FTS creates a schema (not a table) named fts_main_events.
	// Use fts_main_events.match_bm25() as a scalar function on the events table.
	filterSQL, filterArgs := s.buildFilters(q, 3) // $1=query, $2=limit
	query := fmt.Sprintf(
		`SELECT e.event_uid, e.session_id, e.event_kind, e.text_preview,
		        fts_main_events.match_bm25(e.event_uid, $1, fields := 'text_content') AS score,
		        e.timestamp, COALESCE(e.tool_name, ''), COALESCE(e.model, ''), COALESCE(e.provider, '')
		 FROM events e
		 WHERE score IS NOT NULL %s
		 ORDER BY score DESC
		 LIMIT $2`,
		filterSQL,
	)

	args := append([]any{q.Query, q.Limit}, filterArgs...)
	rows, err := s.ftsConn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanResults(rows)
}

// escapeLike escapes LIKE/ILIKE special characters so they match literally.
// Backslash must be escaped first to avoid double-escaping.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}

func (s *Searcher) ilikeSearch(ctx context.Context, q SearchQuery, since time.Time) ([]SearchResult, error) {
	pattern := "%" + escapeLike(q.Query) + "%"

	var whereExtra string
	var args []any
	args = append(args, pattern)
	nextParam := 2

	if !since.IsZero() {
		whereExtra = fmt.Sprintf(" AND e.timestamp > $%d", nextParam)
		args = append(args, since)
		nextParam++
	}

	filterSQL, filterArgs := s.buildFilters(q, nextParam)
	args = append(args, filterArgs...)
	nextParam += len(filterArgs)
	whereExtra += " " + filterSQL

	// Search text_content, tool_name, and error_message so that
	// tool_call and error events are discoverable.
	// Join tool_io for richer snippets on tool_call events.
	query := fmt.Sprintf(
		`SELECT e.event_uid, e.session_id, e.event_kind,
		        CASE WHEN e.event_kind = 'tool_call'
		             THEN COALESCE(e.tool_name || ': ' || NULLIF(tio.input_preview, ''), e.tool_name, '')
		             ELSE COALESCE(NULLIF(e.text_preview, ''), e.tool_name, '')
		        END AS preview,
		        0.0 AS score, e.timestamp, COALESCE(e.tool_name, ''), COALESCE(e.model, ''), COALESCE(e.provider, '')
		 FROM events e
		 LEFT JOIN tool_io tio ON e.event_uid = tio.event_uid
		 WHERE (e.text_content ILIKE $1
		        OR e.tool_name ILIKE $1
		        OR e.error_message ILIKE $1) %s
		 ORDER BY e.timestamp DESC
		 LIMIT $%d`,
		whereExtra, nextParam,
	)
	args = append(args, q.Limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanResults(rows)
}

// buildFilters constructs parameterized WHERE clauses from SearchQuery filters.
// nextParam is the next available $N placeholder number. Returns the SQL
// fragment and the corresponding parameter values.
func (s *Searcher) buildFilters(q SearchQuery, nextParam int) (string, []any) {
	var clauses []string
	var args []any

	if q.SessionID != "" {
		// Prefix match supports both partial (8-char truncated) and full UUIDs
		clauses = append(clauses, fmt.Sprintf("AND e.session_id LIKE $%d", nextParam))
		args = append(args, escapeLike(q.SessionID)+"%")
		nextParam++
	}

	if len(q.EventKinds) > 0 {
		placeholders := make([]string, len(q.EventKinds))
		for i, k := range q.EventKinds {
			placeholders[i] = fmt.Sprintf("$%d", nextParam)
			args = append(args, k)
			nextParam++
		}
		clauses = append(clauses, fmt.Sprintf("AND e.event_kind IN (%s)", strings.Join(placeholders, ",")))
	}

	if !q.FromTime.IsZero() {
		clauses = append(clauses, fmt.Sprintf("AND e.timestamp >= $%d", nextParam))
		args = append(args, q.FromTime.UTC())
		nextParam++
	}
	if !q.ToTime.IsZero() {
		clauses = append(clauses, fmt.Sprintf("AND e.timestamp <= $%d", nextParam))
		args = append(args, q.ToTime.UTC())
		nextParam++
	}

	if q.ExcludeMCPSelf {
		clauses = append(clauses, fmt.Sprintf("AND e.text_content NOT ILIKE $%d", nextParam))
		args = append(args, "%beacon%")
		nextParam++ //nolint:ineffassign // keep nextParam consistent so future filters get the right $N
		clauses = append(clauses, "AND (e.tool_name IS NULL OR e.tool_name NOT IN ('search', 'open', 'list_sessions'))")
	}

	return strings.Join(clauses, " "), args
}

// Browse returns events matching filters without requiring a text query.
// Used when session_id or other filters are set but no search term is entered.
func (s *Searcher) Browse(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	if q.Limit <= 0 {
		q.Limit = s.maxResults
	}
	if q.Limit <= 0 {
		q.Limit = 25
	}

	filterSQL, filterArgs := s.buildFilters(q, 1)
	nextParam := 1 + len(filterArgs)

	query := fmt.Sprintf(
		`SELECT e.event_uid, e.session_id, e.event_kind,
		        COALESCE(NULLIF(e.text_preview, ''), e.tool_name, '') AS preview,
		        0.0 AS score, e.timestamp, COALESCE(e.tool_name, ''), COALESCE(e.model, ''), COALESCE(e.provider, '')
		 FROM events e
		 WHERE 1=1 %s
		 ORDER BY e.timestamp %s
		 LIMIT $%d`,
		filterSQL, browseSortOrder(q.SortBy), nextParam,
	)

	args := append(filterArgs, q.Limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanResults(rows)
}

func browseSortOrder(sortBy string) string {
	switch sortBy {
	case "oldest":
		return "ASC"
	default:
		return "DESC"
	}
}

func scanResults(rows *sql.Rows) ([]SearchResult, error) {
	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.EventUID, &r.SessionID, &r.EventKind, &r.TextPreview, &r.Score, &r.Timestamp, &r.ToolName, &r.Model, &r.Provider); err != nil {
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
