package search

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/johnnygreco/beacon/internal/textindex"
)

type SearchQuery struct {
	Query          string
	Limit          int
	MinScore       float64
	SessionID      string
	EventKinds     []string
	NodeIDs        []string
	CollectorIDs   []string
	SourceIDs      []string
	SourceNames    []string
	Runtimes       []string
	ProjectKeys    []string
	FromTime       time.Time
	ToTime         time.Time
	ExcludeMCPSelf bool
	SkipQueryLog   bool
	SortBy         string // "relevance" (default), "newest", "oldest"
}

type SearchResult struct {
	EventUID    string    `json:"event_uid"`
	SessionID   string    `json:"session_id"`
	NodeID      string    `json:"node_id,omitempty"`
	CollectorID string    `json:"collector_id,omitempty"`
	SourceID    string    `json:"source_id,omitempty"`
	SourceName  string    `json:"source_name,omitempty"`
	Runtime     string    `json:"runtime,omitempty"`
	ProjectKey  string    `json:"project_key,omitempty"`
	ProjectPath string    `json:"project_path,omitempty"`
	EventKind   string    `json:"event_kind"`
	TextPreview string    `json:"text_preview"`
	Score       float64   `json:"score"`
	Timestamp   time.Time `json:"timestamp"`
	ToolName    string    `json:"tool_name"`
	Model       string    `json:"model"`
	Provider    string    `json:"provider"`
}

type Searcher struct {
	db               *sql.DB
	logger           *slog.Logger
	maxResults       int
	logSem           chan struct{}
	droppedQueryLogs atomic.Uint64
	mu               sync.RWMutex
	lastIndexBuild   time.Time
	indexExists      bool
	statsRefreshed   time.Time
	totalDocs        int64
	avgDocLen        float64
	rebuildInterval  time.Duration
}

const (
	searchStatsTTL     = 30 * time.Second
	searchProbeTimeout = 2 * time.Second
)

func NewSearcher(db *sql.DB, logger *slog.Logger, maxResults int, rebuildInterval time.Duration) *Searcher {
	return &Searcher{
		db:              db,
		logger:          logger,
		maxResults:      maxResults,
		logSem:          make(chan struct{}, 4),
		rebuildInterval: rebuildInterval,
	}
}

// MonitorIndex periodically probes the ingest-built search index tables.
func (s *Searcher) MonitorIndex(ctx context.Context) {
	s.probeIndex(ctx)
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
			s.probeIndex(ctx)
		}
	}
}

func (s *Searcher) ProbeIndex() {
	s.probeIndex(context.Background())
}

func (s *Searcher) probeIndex(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	probeCtx, cancel := context.WithTimeout(ctx, searchProbeTimeout)
	defer cancel()
	if _, _, err := s.refreshStats(probeCtx); err != nil {
		s.mu.Lock()
		s.indexExists = false
		s.mu.Unlock()
	}
}

func (s *Searcher) Search(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	q = s.normalize(q)
	tokens := textindex.Tokenize(q.Query)
	if len(tokens) == 0 {
		return s.Browse(ctx, q)
	}
	tokens = uniq(tokens)

	start := time.Now()
	results, err := s.postingsSearch(ctx, q, tokens)
	if err != nil {
		return nil, err
	}

	if !q.SkipQueryLog {
		s.logQuery(q.Query, tokens, len(results), time.Since(start))
	}

	s.logger.Debug("search complete", "query", q.Query, "tokens", tokens, "results", len(results), "duration", time.Since(start))
	return results, nil
}

func (s *Searcher) logQuery(query string, tokens []string, resultCount int, duration time.Duration) {
	if s.db == nil || s.logSem == nil {
		return
	}
	select {
	case s.logSem <- struct{}{}:
	default:
		dropped := s.droppedQueryLogs.Add(1)
		s.logger.Debug("search query log dropped", "query", query, "dropped_total", dropped)
		return
	}

	tokenCopy := append([]string(nil), tokens...)
	// Searcher owns this short-lived logging goroutine. logSem bounds the
	// number of concurrent writes, and the context timeout guarantees release
	// even if ClickHouse is slow or unavailable.
	go func() {
		defer func() { <-s.logSem }()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO search_query_log (query, normalized_terms, result_count, duration_ms)
			 VALUES (?, ?, ?, ?)`,
			query, tokenCopy, uint32(resultCount), uint64(duration.Milliseconds())); err != nil {
			s.logger.Debug("search query log insert failed", "query", query, "error", err)
		}
	}()
}

// DroppedQueryLogCount returns async query-log records skipped because the
// bounded logging queue was saturated. Search results are unaffected.
func (s *Searcher) DroppedQueryLogCount() uint64 {
	return s.droppedQueryLogs.Load()
}

func (s *Searcher) postingsSearch(ctx context.Context, q SearchQuery, tokens []string) ([]SearchResult, error) {
	totalDocs, avgDocLen, err := s.cachedStats(ctx)
	if err != nil {
		return nil, err
	}
	tokenPlaceholders := placeholders(len(tokens))
	filterSQL, filterArgs := buildPostingFilters(q)

	query := fmt.Sprintf(`
		WITH
			toFloat64(?) AS total_docs,
			toFloat64(?) AS avg_doc_len
		SELECT
			p.event_uid,
			any(p.session_id) AS session_id,
			any(p.node_id) AS node_id,
			any(p.collector_id) AS collector_id,
			any(p.source_id) AS source_id,
			any(p.source_name) AS source_name,
			any(p.runtime) AS runtime,
			any(p.project_key) AS project_key,
			any(p.project_path) AS project_path,
			any(p.event_kind) AS event_kind,
			any(p.text_preview) AS text_preview,
			sum(log(1 + ((greatest(total_docs, p.doc_freq) - p.doc_freq + 0.5) / (p.doc_freq + 0.5))) *
			    ((p.term_frequency * 2.2) /
			     (p.term_frequency + 1.2 * (0.25 + 0.75 * (p.document_len / avg_doc_len))))) AS score,
			max(p.timestamp) AS timestamp,
			any(p.tool_name) AS tool_name,
			any(p.model) AS model,
			any(p.provider) AS provider
		FROM (
			SELECT p.*,
			       toFloat64(count() OVER (PARTITION BY p.token)) AS doc_freq
			FROM (SELECT * FROM search_postings FINAL) AS p
			INNER JOIN (SELECT event_uid, updated_at FROM search_documents FINAL) AS d ON d.event_uid = p.event_uid
			WHERE p.token IN (%s)
			  AND p.updated_at >= d.updated_at %s
		) p
		GROUP BY p.event_uid
		HAVING score >= ?
		ORDER BY %s
		LIMIT ?`,
		tokenPlaceholders,
		filterSQL,
		searchSortOrder(q.SortBy),
	)

	args := make([]any, 0, len(tokens)+len(filterArgs)+4)
	args = append(args, float64(totalDocs), avgDocLen)
	for _, token := range tokens {
		args = append(args, token)
	}
	args = append(args, filterArgs...)
	args = append(args, q.MinScore, q.Limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanResults(rows)
}

func (s *Searcher) cachedStats(ctx context.Context) (int64, float64, error) {
	s.mu.RLock()
	if !s.statsRefreshed.IsZero() && time.Since(s.statsRefreshed) < searchStatsTTL {
		totalDocs := s.totalDocs
		avgDocLen := s.avgDocLen
		s.mu.RUnlock()
		return totalDocs, avgDocLen, nil
	}
	s.mu.RUnlock()
	return s.refreshStats(ctx)
}

func (s *Searcher) refreshStats(ctx context.Context) (int64, float64, error) {
	var documents int64
	var avgDocLen float64
	err := s.db.QueryRowContext(ctx,
		`SELECT count() AS documents,
		        if(count() = 0, 1, greatest(avg(document_len), 1)) AS avg_doc_len
		 FROM search_documents FINAL`).Scan(&documents, &avgDocLen)
	if err != nil {
		return 1, 1, err
	}
	totalDocs := documents
	if totalDocs < 1 {
		totalDocs = 1
	}
	if avgDocLen < 1 {
		avgDocLen = 1
	}

	s.mu.Lock()
	s.totalDocs = totalDocs
	s.avgDocLen = avgDocLen
	s.statsRefreshed = time.Now()
	s.indexExists = documents > 0
	if s.indexExists {
		s.lastIndexBuild = s.statsRefreshed
	}
	s.mu.Unlock()

	return totalDocs, avgDocLen, nil
}

func (s *Searcher) Browse(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	q = s.normalize(q)
	filterSQL, args := buildDocumentFilters(q)

	query := fmt.Sprintf(`
		SELECT event_uid, session_id, node_id, collector_id, source_id, source_name,
		       runtime, project_key, project_path, event_kind, text_preview, 0.0 AS score,
		       timestamp, tool_name, model, provider
		FROM search_documents FINAL
		WHERE 1 = 1 %s
		ORDER BY timestamp %s
		LIMIT ?`,
		filterSQL,
		browseSortOrder(q.SortBy),
	)
	args = append(args, q.Limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanResults(rows)
}

func (s *Searcher) normalize(q SearchQuery) SearchQuery {
	if q.Limit <= 0 {
		q.Limit = s.maxResults
	}
	if q.Limit <= 0 {
		q.Limit = 25
	}
	if q.MinScore < 0 {
		q.MinScore = 0
	}
	return q
}

func buildPostingFilters(q SearchQuery) (string, []any) {
	return buildFilters("p", q)
}

func buildDocumentFilters(q SearchQuery) (string, []any) {
	return buildFilters("", q)
}

func buildFilters(alias string, q SearchQuery) (string, []any) {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}

	var clauses []string
	var args []any

	if q.SessionID != "" {
		clauses = append(clauses, "AND startsWith("+prefix+"session_id, ?)")
		args = append(args, q.SessionID)
	}

	if len(q.EventKinds) > 0 {
		clauses = append(clauses, "AND "+prefix+"event_kind IN ("+placeholders(len(q.EventKinds))+")")
		for _, kind := range q.EventKinds {
			args = append(args, kind)
		}
	}
	appendInFilter(&clauses, &args, prefix+"node_id", q.NodeIDs)
	appendInFilter(&clauses, &args, prefix+"collector_id", q.CollectorIDs)
	appendInFilter(&clauses, &args, prefix+"source_id", q.SourceIDs)
	appendInFilter(&clauses, &args, prefix+"source_name", q.SourceNames)
	appendInFilter(&clauses, &args, prefix+"runtime", q.Runtimes)
	appendInFilter(&clauses, &args, prefix+"project_key", q.ProjectKeys)

	if !q.FromTime.IsZero() {
		clauses = append(clauses, "AND "+prefix+"timestamp >= ?")
		args = append(args, q.FromTime.UTC())
	}
	if !q.ToTime.IsZero() {
		clauses = append(clauses, "AND "+prefix+"timestamp <= ?")
		args = append(args, q.ToTime.UTC())
	}

	if q.ExcludeMCPSelf {
		clauses = append(clauses, "AND "+prefix+"tool_name NOT IN ('search_sessions', 'open', 'list_sessions')")
		clauses = append(clauses, "AND positionCaseInsensitive("+prefix+"text_preview, 'beacon') = 0")
	}

	return " " + strings.Join(clauses, " "), args
}

func appendInFilter(clauses *[]string, args *[]any, column string, values []string) {
	values = compactStrings(values)
	if len(values) == 0 {
		return
	}
	*clauses = append(*clauses, "AND "+column+" IN ("+placeholders(len(values))+")")
	for _, value := range values {
		*args = append(*args, value)
	}
}

func searchSortOrder(sortBy string) string {
	switch sortBy {
	case "newest":
		return "timestamp DESC"
	case "oldest":
		return "timestamp ASC"
	default:
		return "score DESC, timestamp DESC"
	}
}

func browseSortOrder(sortBy string) string {
	switch sortBy {
	case "oldest":
		return "ASC"
	default:
		return "DESC"
	}
}

type resultRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanResults(rows resultRows) ([]SearchResult, error) {
	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.EventUID, &r.SessionID, &r.NodeID, &r.CollectorID, &r.SourceID, &r.SourceName, &r.Runtime, &r.ProjectKey, &r.ProjectPath, &r.EventKind, &r.TextPreview, &r.Score, &r.Timestamp, &r.ToolName, &r.Model, &r.Provider); err != nil {
			return results, fmt.Errorf("scan search result: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (s *Searcher) LastIndexBuild() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastIndexBuild
}

func (s *Searcher) IndexExists() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.indexExists
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

func uniq(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
