package mcp

import (
	"context"
	"strings"

	"github.com/johnnygreco/beacon/internal/search"
)

type ScopeFilters struct {
	NodeIDs      []string `json:"node_ids,omitempty"`
	CollectorIDs []string `json:"collector_ids,omitempty"`
	SourceIDs    []string `json:"source_ids,omitempty"`
	SourceNames  []string `json:"source_names,omitempty"`
	Runtimes     []string `json:"runtimes,omitempty"`
	ProjectKeys  []string `json:"project_keys,omitempty"`

	denyAll bool
}

type ScopeMetadata struct {
	AuthScopeApplied bool         `json:"auth_scope_applied"`
	Filters          ScopeFilters `json:"filters"`
}

type scopeContextKey struct{}

func ContextWithAuthScope(ctx context.Context, scope ScopeFilters) context.Context {
	return context.WithValue(ctx, scopeContextKey{}, authScopeContext{scope: normalizeScopeFilters(scope), applied: true})
}

func AuthScopeFromContext(ctx context.Context) (ScopeFilters, bool) {
	auth, ok := ctx.Value(scopeContextKey{}).(authScopeContext)
	if !ok {
		return ScopeFilters{}, false
	}
	return auth.scope, auth.applied
}

type authScopeContext struct {
	scope   ScopeFilters
	applied bool
}

type scopeArgs struct {
	NodeID       string   `json:"node_id"`
	NodeIDs      []string `json:"node_ids"`
	CollectorID  string   `json:"collector_id"`
	CollectorIDs []string `json:"collector_ids"`
	SourceID     string   `json:"source_id"`
	SourceIDs    []string `json:"source_ids"`
	SourceName   string   `json:"source_name"`
	SourceNames  []string `json:"source_names"`
	Runtime      string   `json:"runtime"`
	Runtimes     []string `json:"runtimes"`
	ProjectKey   string   `json:"project_key"`
	ProjectKeys  []string `json:"project_keys"`
}

func (a scopeArgs) filters() ScopeFilters {
	return normalizeScopeFilters(ScopeFilters{
		NodeIDs:      append([]string{a.NodeID}, a.NodeIDs...),
		CollectorIDs: append([]string{a.CollectorID}, a.CollectorIDs...),
		SourceIDs:    append([]string{a.SourceID}, a.SourceIDs...),
		SourceNames:  append([]string{a.SourceName}, a.SourceNames...),
		Runtimes:     append([]string{a.Runtime}, a.Runtimes...),
		ProjectKeys:  append([]string{a.ProjectKey}, a.ProjectKeys...),
	})
}

func (s *Server) effectiveScope(ctx context.Context, requested ScopeFilters) (ScopeFilters, ScopeMetadata) {
	requested = normalizeScopeFilters(requested)
	auth, applied := AuthScopeFromContext(ctx)
	if !applied {
		return requested, ScopeMetadata{AuthScopeApplied: false, Filters: requested}
	}
	scope := intersectScopes(auth, requested)
	return scope, ScopeMetadata{AuthScopeApplied: true, Filters: scope}
}

func normalizeScopeFilters(scope ScopeFilters) ScopeFilters {
	scope.NodeIDs = compactScopeValues(scope.NodeIDs)
	scope.CollectorIDs = compactScopeValues(scope.CollectorIDs)
	scope.SourceIDs = compactScopeValues(scope.SourceIDs)
	scope.SourceNames = compactScopeValues(scope.SourceNames)
	scope.Runtimes = compactScopeValues(scope.Runtimes)
	scope.ProjectKeys = compactScopeValues(scope.ProjectKeys)
	return scope
}

func intersectScopes(auth, requested ScopeFilters) ScopeFilters {
	if auth.denyAll || requested.denyAll {
		return ScopeFilters{denyAll: true}
	}
	var denied bool
	out := ScopeFilters{
		NodeIDs:      intersectScopeDimension(auth.NodeIDs, requested.NodeIDs, &denied),
		CollectorIDs: intersectScopeDimension(auth.CollectorIDs, requested.CollectorIDs, &denied),
		SourceIDs:    intersectScopeDimension(auth.SourceIDs, requested.SourceIDs, &denied),
		SourceNames:  intersectScopeDimension(auth.SourceNames, requested.SourceNames, &denied),
		Runtimes:     intersectScopeDimension(auth.Runtimes, requested.Runtimes, &denied),
		ProjectKeys:  intersectScopeDimension(auth.ProjectKeys, requested.ProjectKeys, &denied),
	}
	if denied {
		return ScopeFilters{denyAll: true}
	}
	return normalizeScopeFilters(out)
}

func intersectScopeDimension(auth, requested []string, denied *bool) []string {
	auth = compactScopeValues(auth)
	requested = compactScopeValues(requested)
	if len(auth) == 0 {
		return requested
	}
	if len(requested) == 0 {
		return auth
	}
	authSet := make(map[string]struct{}, len(auth))
	for _, value := range auth {
		authSet[value] = struct{}{}
	}
	var out []string
	for _, value := range requested {
		if _, ok := authSet[value]; ok {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		*denied = true
	}
	return out
}

func (s ScopeFilters) applyToSearchQuery(q *search.SearchQuery) {
	if q == nil {
		return
	}
	if s.denyAll {
		q.NodeIDs = []string{scopeImpossibleValue}
		return
	}
	q.NodeIDs = compactScopeValues(append(q.NodeIDs, s.NodeIDs...))
	q.CollectorIDs = compactScopeValues(append(q.CollectorIDs, s.CollectorIDs...))
	q.SourceIDs = compactScopeValues(append(q.SourceIDs, s.SourceIDs...))
	q.SourceNames = compactScopeValues(append(q.SourceNames, s.SourceNames...))
	q.Runtimes = compactScopeValues(append(q.Runtimes, s.Runtimes...))
	q.ProjectKeys = compactScopeValues(append(q.ProjectKeys, s.ProjectKeys...))
}

func (s ScopeFilters) sqlAndClause(alias string) (string, []any) {
	predicates, args := s.sqlPredicates(alias)
	if len(predicates) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(predicates, " AND "), args
}

func (s ScopeFilters) eventSQLAndClause(alias, cwdExpr string) (string, []any) {
	predicates, args := s.eventSQLPredicates(alias, cwdExpr)
	if len(predicates) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(predicates, " AND "), args
}

func (s ScopeFilters) eventAndSessionProjectSQLAndClause(eventAlias, cwdExpr, sessionAlias string) (string, []any) {
	rawScope := s
	rawScope.ProjectKeys = nil
	predicates, args := rawScope.eventSQLPredicates(eventAlias, cwdExpr)
	projectKeys := compactScopeValues(s.ProjectKeys)
	if len(projectKeys) > 0 {
		projectExpr := projectKeyExpr(cwdExpr)
		if strings.TrimSpace(sessionAlias) != "" {
			projectExpr = "COALESCE(NULLIF(" + projectExpr + ", ''), if(COALESCE(" + sessionAlias + ".project_count, 0) <= 1, NULLIF(" + sessionAlias + ".project_key, ''), ''))"
		}
		appendScopePredicate(&predicates, &args, projectExpr, projectKeys)
	}
	if len(predicates) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(predicates, " AND "), args
}

func (s ScopeFilters) sqlPredicates(alias string) ([]string, []any) {
	if s.denyAll {
		return []string{"0 = 1"}, nil
	}
	prefix := ""
	if strings.TrimSpace(alias) != "" {
		prefix = alias + "."
	}
	var predicates []string
	var args []any
	appendScopePredicate(&predicates, &args, nodeScopeExpr(prefix+"node_id"), s.NodeIDs)
	appendScopePredicate(&predicates, &args, prefix+"collector_id", s.CollectorIDs)
	appendScopePredicate(&predicates, &args, prefix+"source_id", s.SourceIDs)
	appendScopePredicate(&predicates, &args, prefix+"source_name", s.SourceNames)
	appendScopePredicate(&predicates, &args, prefix+"runtime", s.Runtimes)
	appendScopePredicate(&predicates, &args, prefix+"project_key", s.ProjectKeys)
	return predicates, args
}

func (s ScopeFilters) eventSQLPredicates(alias, cwdExpr string) ([]string, []any) {
	if s.denyAll {
		return []string{"0 = 1"}, nil
	}
	prefix := ""
	if strings.TrimSpace(alias) != "" {
		prefix = alias + "."
	}
	var predicates []string
	var args []any
	appendScopePredicate(&predicates, &args, nodeScopeExpr(prefix+"node_id"), s.NodeIDs)
	appendScopePredicate(&predicates, &args, prefix+"collector_id", s.CollectorIDs)
	appendScopePredicate(&predicates, &args, prefix+"source_id", s.SourceIDs)
	appendScopePredicate(&predicates, &args, prefix+"source_name", s.SourceNames)
	appendScopePredicate(&predicates, &args, prefix+"runtime", s.Runtimes)
	projectExpr := strings.TrimSpace(cwdExpr)
	if projectExpr == "" {
		projectExpr = prefix + "cwd"
	}
	appendScopePredicate(&predicates, &args, projectKeyExpr(projectExpr), s.ProjectKeys)
	return predicates, args
}

func appendScopePredicate(predicates *[]string, args *[]any, column string, values []string) {
	values = compactScopeValues(values)
	if len(values) == 0 {
		return
	}
	*predicates = append(*predicates, column+" IN ("+sqlPlaceholders(len(values))+")")
	for _, value := range values {
		*args = append(*args, value)
	}
}

func nodeScopeExpr(column string) string {
	column = strings.TrimSpace(column)
	if column == "" {
		column = "node_id"
	}
	return "COALESCE(NULLIF(" + column + ", ''), 'local')"
}

func projectKeyExpr(pathExpr string) string {
	pathExpr = strings.TrimSpace(pathExpr)
	if pathExpr == "" {
		pathExpr = "cwd"
	}
	return `if(` + pathExpr + ` = '', '',
		replaceRegexpOne(
			if(position(` + pathExpr + `, '/.claude/worktrees/') > 0,
				substring(` + pathExpr + `, 1, position(` + pathExpr + `, '/.claude/worktrees/') - 1),
				replaceRegexpOne(` + pathExpr + `, '/+$', '')
			),
			'^.*/',
			''
		)
	)`
}

func compactScopeValues(values []string) []string {
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

func sqlPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

func hasScopeFilters(scope ScopeFilters) bool {
	return scope.denyAll ||
		len(scope.NodeIDs) > 0 ||
		len(scope.CollectorIDs) > 0 ||
		len(scope.SourceIDs) > 0 ||
		len(scope.SourceNames) > 0 ||
		len(scope.Runtimes) > 0 ||
		len(scope.ProjectKeys) > 0
}

const scopeImpossibleValue = "\x00beacon-no-scope-match"
