package usage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultWindowMode = "event_timestamp"
	TokenModeIOOnly   = "io_only"
	TokenModeAll      = "include_cache"
	DefaultLimit      = 10
	MaxLimit          = 100
)

const (
	TotalDefinitionIOOnly = "input_tokens + output_tokens"
	TotalDefinitionAll    = "input_tokens + output_tokens + cache_read_tokens + cache_create_tokens"
)

type Request struct {
	Since      string
	Until      string
	WindowMode string
	TokenMode  string
	SourceName string
	Model      string
	Provider   string
	WorkingDir string
	GroupBy    []string
	Limit      int
}

type Result struct {
	Window                  Window   `json:"window"`
	Filters                 Filters  `json:"filters"`
	GroupBy                 []string `json:"group_by"`
	TokenMode               string   `json:"token_mode"`
	TotalDefinition         string   `json:"total_definition"`
	SelectedTotalDefinition string   `json:"selected_total_definition"`
	Summary                 Totals   `json:"summary"`
	Groups                  []Group  `json:"groups"`
	Metadata                Metadata `json:"metadata"`
}

type Window struct {
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`
	Mode  string    `json:"mode"`
}

type Filters struct {
	SourceName string `json:"source_name,omitempty"`
	Model      string `json:"model,omitempty"`
	Provider   string `json:"provider,omitempty"`
	WorkingDir string `json:"working_dir,omitempty"`
}

type Totals struct {
	SessionCount        int64 `json:"session_count"`
	EventCount          int64 `json:"event_count"`
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CacheCreateTokens   int64 `json:"cache_create_tokens"`
	SelectedTotalTokens int64 `json:"selected_total_tokens"`
}

type Group struct {
	Keys   map[string]string `json:"keys"`
	Totals Totals            `json:"totals"`
}

type Metadata struct {
	ResultCount        int    `json:"result_count"`
	TotalMatchingCount int64  `json:"total_matching_count"`
	Limit              int    `json:"limit"`
	ResultComplete     bool   `json:"result_complete"`
	TruncatedByLimit   bool   `json:"truncated_by_limit"`
	NextCursor         string `json:"next_cursor"`
}

type UserError struct {
	message string
}

func (e UserError) Error() string {
	return e.message
}

func IsUserError(err error) bool {
	var userErr UserError
	return errors.As(err, &userErr)
}

func Validate(req Request, now time.Time) error {
	_, err := normalize(req, now)
	return err
}

func Summarize(ctx context.Context, db *sql.DB, req Request, now time.Time) (Result, error) {
	if db == nil {
		return Result{}, fmt.Errorf("database is not configured")
	}
	normalized, err := normalize(req, now)
	if err != nil {
		return Result{}, err
	}

	summaryQuery, summaryArgs := usageSummarySQL(normalized)
	summary, err := scanTotals(db.QueryRowContext(ctx, summaryQuery, summaryArgs...))
	if err != nil {
		return Result{}, fmt.Errorf("query usage summary: %w", err)
	}

	groups, totalMatching, err := queryGroups(ctx, db, normalized)
	if err != nil {
		return Result{}, err
	}
	resultCount := len(groups)
	resultComplete := totalMatching <= int64(resultCount)
	if len(normalized.GroupBy) == 0 {
		totalMatching = 0
		resultComplete = true
	}

	return Result{
		Window:                  normalized.Window,
		Filters:                 normalized.Filters,
		GroupBy:                 normalized.GroupBy,
		TokenMode:               normalized.TokenMode,
		TotalDefinition:         TotalDefinitionIOOnly,
		SelectedTotalDefinition: selectedTotalDefinition(normalized.TokenMode),
		Summary:                 summary,
		Groups:                  groups,
		Metadata: Metadata{
			ResultCount:        resultCount,
			TotalMatchingCount: totalMatching,
			Limit:              normalized.Limit,
			ResultComplete:     resultComplete,
			TruncatedByLimit:   !resultComplete,
			NextCursor:         "",
		},
	}, nil
}

type normalizedRequest struct {
	Window    Window
	Filters   Filters
	GroupBy   []string
	TokenMode string
	Limit     int
}

func normalize(req Request, now time.Time) (normalizedRequest, error) {
	windowMode := strings.TrimSpace(req.WindowMode)
	if windowMode == "" {
		windowMode = DefaultWindowMode
	}
	if windowMode != DefaultWindowMode {
		return normalizedRequest{}, UserError{message: fmt.Sprintf("unsupported window_mode: %s", windowMode)}
	}

	tokenMode := strings.TrimSpace(req.TokenMode)
	if tokenMode == "" {
		tokenMode = TokenModeIOOnly
	}
	switch tokenMode {
	case TokenModeIOOnly, TokenModeAll:
	default:
		return normalizedRequest{}, UserError{message: fmt.Sprintf("unsupported token_mode: %s", tokenMode)}
	}

	window, err := resolveWindow(req.Since, req.Until, now)
	if err != nil {
		return normalizedRequest{}, err
	}

	groupBy, err := normalizeGroupBy(req.GroupBy)
	if err != nil {
		return normalizedRequest{}, err
	}

	limit := req.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	return normalizedRequest{
		Window:    window,
		Filters:   normalizeFilters(req),
		GroupBy:   groupBy,
		TokenMode: tokenMode,
		Limit:     limit,
	}, nil
}

func resolveWindow(sinceRaw, untilRaw string, now time.Time) (Window, error) {
	now = now.UTC()
	untilRaw = strings.TrimSpace(untilRaw)
	if untilRaw == "" {
		untilRaw = "now"
	}
	until, err := parseTimeExpr(untilRaw, now)
	if err != nil {
		return Window{}, UserError{message: "invalid until timestamp"}
	}

	sinceRaw = strings.TrimSpace(sinceRaw)
	var since time.Time
	if sinceRaw == "" {
		since = until.Add(-24 * time.Hour)
	} else {
		since, err = parseTimeExpr(sinceRaw, now)
		if err != nil {
			return Window{}, UserError{message: "invalid since timestamp"}
		}
	}
	if !since.Before(until) {
		return Window{}, UserError{message: "since must be before until"}
	}
	return Window{Since: since.UTC(), Until: until.UTC(), Mode: DefaultWindowMode}, nil
}

func parseTimeExpr(raw string, now time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "now" {
		return now.UTC(), nil
	}
	if strings.HasPrefix(raw, "now-") {
		d, err := time.ParseDuration(strings.TrimPrefix(raw, "now-"))
		if err != nil {
			return time.Time{}, err
		}
		return now.Add(-d).UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func normalizeFilters(req Request) Filters {
	return Filters{
		SourceName: strings.TrimSpace(req.SourceName),
		Model:      strings.TrimSpace(req.Model),
		Provider:   strings.TrimSpace(req.Provider),
		WorkingDir: strings.TrimSpace(req.WorkingDir),
	}
}

func normalizeGroupBy(raw []string) ([]string, error) {
	groupBy := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, field := range raw {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if _, ok := groupBySQL[field]; !ok {
			return nil, UserError{message: fmt.Sprintf("unsupported group_by field: %s", field)}
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		groupBy = append(groupBy, field)
	}
	return groupBy, nil
}

func queryGroups(ctx context.Context, db *sql.DB, req normalizedRequest) ([]Group, int64, error) {
	if len(req.GroupBy) == 0 {
		return []Group{}, 0, nil
	}

	countQuery, countArgs := usageGroupCountSQL(req)
	var totalMatching int64
	if err := db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalMatching); err != nil {
		return nil, 0, fmt.Errorf("query usage group count: %w", err)
	}

	groupQuery, groupArgs := usageGroupSQL(req)
	rows, err := db.QueryContext(ctx, groupQuery, groupArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query usage groups: %w", err)
	}
	defer rows.Close()

	groups := make([]Group, 0, req.Limit)
	for rows.Next() {
		keys := make([]string, len(req.GroupBy))
		dest := make([]any, 0, len(req.GroupBy)+8)
		for i := range keys {
			dest = append(dest, &keys[i])
		}
		var totals Totals
		dest = append(dest,
			&totals.SessionCount,
			&totals.EventCount,
			&totals.InputTokens,
			&totals.OutputTokens,
			&totals.TotalTokens,
			&totals.CacheReadTokens,
			&totals.CacheCreateTokens,
			&totals.SelectedTotalTokens,
		)
		if err := rows.Scan(dest...); err != nil {
			return nil, 0, fmt.Errorf("scan usage group: %w", err)
		}
		keyMap := make(map[string]string, len(req.GroupBy))
		for i, field := range req.GroupBy {
			keyMap[field] = keys[i]
		}
		groups = append(groups, Group{Keys: keyMap, Totals: totals})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read usage groups: %w", err)
	}
	return groups, totalMatching, nil
}

func scanTotals(row *sql.Row) (Totals, error) {
	var totals Totals
	err := row.Scan(
		&totals.SessionCount,
		&totals.EventCount,
		&totals.InputTokens,
		&totals.OutputTokens,
		&totals.TotalTokens,
		&totals.CacheReadTokens,
		&totals.CacheCreateTokens,
		&totals.SelectedTotalTokens,
	)
	return totals, err
}

func usageSummarySQL(req normalizedRequest) (string, []any) {
	where, args := usageWhereSQL(req)
	return `SELECT
			uniqExact(e.session_id) AS session_count,
			count() AS event_count,
			sum(e.input_tokens) AS input_tokens,
			sum(e.output_tokens) AS output_tokens,
			sum(e.input_tokens + e.output_tokens) AS total_tokens,
			sum(e.cache_read_tokens) AS cache_read_tokens,
			sum(e.cache_create_tokens) AS cache_create_tokens,
			` + selectedTotalSQL(req.TokenMode) + ` AS selected_total_tokens
		FROM ` + usageSourceSQL() + `
		WHERE ` + where, args
}

func usageGroupCountSQL(req normalizedRequest) (string, []any) {
	where, args := usageWhereSQL(req)
	groupSelect := groupSelectSQL(req.GroupBy)
	groupBy := groupByClauseSQL(req.GroupBy)
	return `SELECT count()
		FROM (
			SELECT ` + groupSelect + `
			FROM ` + usageSourceSQL() + `
			WHERE ` + where + `
			GROUP BY ` + groupBy + `
		)`, args
}

func usageGroupSQL(req normalizedRequest) (string, []any) {
	where, args := usageWhereSQL(req)
	groupSelect := groupSelectSQL(req.GroupBy)
	groupBy := groupByClauseSQL(req.GroupBy)
	args = append(args, req.Limit)
	return `SELECT
			` + groupSelect + `,
			uniqExact(e.session_id) AS session_count,
			count() AS event_count,
			sum(e.input_tokens) AS input_tokens,
			sum(e.output_tokens) AS output_tokens,
			sum(e.input_tokens + e.output_tokens) AS total_tokens,
			sum(e.cache_read_tokens) AS cache_read_tokens,
			sum(e.cache_create_tokens) AS cache_create_tokens,
			` + selectedTotalSQL(req.TokenMode) + ` AS selected_total_tokens
		FROM ` + usageSourceSQL() + `
		WHERE ` + where + `
		GROUP BY ` + groupBy + `
		ORDER BY selected_total_tokens DESC, event_count DESC
		LIMIT ?`, args
}

func usageWhereSQL(req normalizedRequest) (string, []any) {
	clauses := []string{
		"e.timestamp >= ?",
		"e.timestamp < ?",
		"e.session_id != ''",
	}
	args := []any{req.Window.Since, req.Window.Until}
	if req.Filters.SourceName != "" {
		clauses = append(clauses, "e.source_name = ?")
		args = append(args, req.Filters.SourceName)
	}
	if req.Filters.Model != "" {
		clauses = append(clauses, "e.model = ?")
		args = append(args, req.Filters.Model)
	}
	if req.Filters.Provider != "" {
		clauses = append(clauses, "e.provider = ?")
		args = append(args, req.Filters.Provider)
	}
	if req.Filters.WorkingDir != "" {
		clauses = append(clauses, "COALESCE(e.session_working_dir, '') = ?")
		args = append(args, req.Filters.WorkingDir)
	}
	return strings.Join(clauses, " AND "), args
}

func usageSourceSQL() string {
	return `(WITH latest_events AS (
			SELECT
				event_uid,
				argMax(session_id, captured_at) AS session_id,
				argMax(source_name, captured_at) AS source_name,
				argMax(provider, captured_at) AS provider,
				argMax(model, captured_at) AS model,
				argMax(timestamp, captured_at) AS timestamp,
				argMax(cwd, captured_at) AS cwd,
				argMax(input_tokens, captured_at) AS input_tokens,
				argMax(output_tokens, captured_at) AS output_tokens,
				argMax(cache_read_tokens, captured_at) AS cache_read_tokens,
				argMax(cache_create_tokens, captured_at) AS cache_create_tokens
			FROM activity_events
			GROUP BY event_uid
		),
		session_working_dirs AS (
			SELECT
				session_id,
				argMaxIf(cwd, timestamp, cwd != '') AS working_dir
			FROM latest_events
			WHERE session_id != ''
			GROUP BY session_id
		)
		SELECT e.*, swd.working_dir AS session_working_dir
		FROM latest_events AS e
		LEFT JOIN session_working_dirs AS swd ON swd.session_id = e.session_id) AS e`
}

func groupSelectSQL(groupBy []string) string {
	parts := make([]string, 0, len(groupBy))
	for _, field := range groupBy {
		parts = append(parts, groupBySQL[field]+" AS "+field)
	}
	return strings.Join(parts, ", ")
}

func groupByClauseSQL(groupBy []string) string {
	parts := make([]string, 0, len(groupBy))
	for _, field := range groupBy {
		parts = append(parts, groupBySQL[field])
	}
	return strings.Join(parts, ", ")
}

func selectedTotalSQL(tokenMode string) string {
	if tokenMode == TokenModeAll {
		return "sum(e.input_tokens + e.output_tokens + e.cache_read_tokens + e.cache_create_tokens)"
	}
	return "sum(e.input_tokens + e.output_tokens)"
}

func selectedTotalDefinition(tokenMode string) string {
	if tokenMode == TokenModeAll {
		return TotalDefinitionAll
	}
	return TotalDefinitionIOOnly
}

var groupBySQL = map[string]string{
	"source_name": "COALESCE(NULLIF(e.source_name, ''), 'unknown')",
	"provider":    "COALESCE(NULLIF(e.provider, ''), 'unknown')",
	"model":       "COALESCE(NULLIF(e.model, ''), 'unknown')",
	"session_id":  "e.session_id",
	"working_dir": "COALESCE(NULLIF(e.session_working_dir, ''), 'unknown')",
}
