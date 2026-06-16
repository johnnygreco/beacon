package web

import (
	"context"
	"net/url"
	"strings"

	"github.com/johnnygreco/beacon/internal/search"
)

type APIScopeFilters struct {
	SourceNames []string `json:"source_names,omitempty"`
	Runtimes    []string `json:"runtimes,omitempty"`
	ProjectKeys []string `json:"project_keys,omitempty"`

	denyAll bool
}

type APIScopeMetadata struct {
	AuthScopeApplied bool            `json:"auth_scope_applied"`
	Filters          APIScopeFilters `json:"filters"`
}

var apiScopeParamNames = []string{
	"source_name",
	"source_names",
	"runtime",
	"runtimes",
	"project_key",
	"project_keys",
}

func parseAPIScopeFilters(values url.Values) APIScopeFilters {
	return normalizeAPIScopeFilters(APIScopeFilters{
		SourceNames: parseAPIScopeValues(values, "source_name", "source_names"),
		Runtimes:    parseAPIScopeValues(values, "runtime", "runtimes"),
		ProjectKeys: parseAPIScopeValues(values, "project_key", "project_keys"),
	})
}

func parseAPIScopeValues(values url.Values, names ...string) []string {
	var out []string
	for _, name := range names {
		out = append(out, parseAPICSVParam(values, name)...)
	}
	return compactScopeValues(out)
}

func scopeQueryString(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	scoped := url.Values{}
	for _, name := range apiScopeParamNames {
		for _, value := range values[name] {
			scoped.Add(name, value)
		}
	}
	return scoped.Encode()
}

func (s APIScopeFilters) metadata() APIScopeMetadata {
	return APIScopeMetadata{AuthScopeApplied: false, Filters: s}
}

type apiAuthScopeContextKey struct{}

type apiAuthScope struct {
	scope   APIScopeFilters
	applied bool
}

func ContextWithAPIScope(ctx context.Context, scope APIScopeFilters) context.Context {
	return context.WithValue(ctx, apiAuthScopeContextKey{}, apiAuthScope{scope: normalizeAPIScopeFilters(scope), applied: true})
}

func apiScopeFromContext(ctx context.Context) (APIScopeFilters, bool) {
	auth, ok := ctx.Value(apiAuthScopeContextKey{}).(apiAuthScope)
	if !ok {
		return APIScopeFilters{}, false
	}
	return auth.scope, auth.applied
}

func APIScopeFromContext(ctx context.Context) (APIScopeFilters, bool) {
	return apiScopeFromContext(ctx)
}

func scopeForRequest(ctx context.Context, requested APIScopeFilters) (APIScopeFilters, APIScopeMetadata) {
	requested = normalizeAPIScopeFilters(requested)
	auth, applied := apiScopeFromContext(ctx)
	if !applied {
		return requested, requested.metadata()
	}
	scope := intersectAPIScopes(auth, requested)
	return scope, APIScopeMetadata{AuthScopeApplied: true, Filters: scope}
}

func normalizeAPIScopeFilters(scope APIScopeFilters) APIScopeFilters {
	scope.SourceNames = compactScopeValues(scope.SourceNames)
	scope.Runtimes = compactScopeValues(scope.Runtimes)
	scope.ProjectKeys = compactScopeValues(scope.ProjectKeys)
	return scope
}

func intersectAPIScopes(auth, requested APIScopeFilters) APIScopeFilters {
	if auth.denyAll || requested.denyAll {
		return APIScopeFilters{denyAll: true}
	}
	var denied bool
	out := APIScopeFilters{
		SourceNames: intersectAPIScopeDimension(auth.SourceNames, requested.SourceNames, &denied),
		Runtimes:    intersectAPIScopeDimension(auth.Runtimes, requested.Runtimes, &denied),
		ProjectKeys: intersectAPIScopeDimension(auth.ProjectKeys, requested.ProjectKeys, &denied),
	}
	if denied {
		return APIScopeFilters{denyAll: true}
	}
	return normalizeAPIScopeFilters(out)
}

func intersectAPIScopeDimension(auth, requested []string, denied *bool) []string {
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

func (s APIScopeFilters) withoutProjectKeys() APIScopeFilters {
	s.ProjectKeys = nil
	return s
}

func (s APIScopeFilters) applyToSearchQuery(q *search.SearchQuery) {
	if q == nil {
		return
	}
	if s.denyAll {
		q.SourceNames = []string{apiScopeImpossibleValue}
		return
	}
	q.SourceNames = compactScopeValues(append(q.SourceNames, s.SourceNames...))
	q.Runtimes = compactScopeValues(append(q.Runtimes, s.Runtimes...))
	q.ProjectKeys = compactScopeValues(append(q.ProjectKeys, s.ProjectKeys...))
}

func (s APIScopeFilters) sqlAndClause(alias string) (string, []any) {
	predicates, args := s.sqlPredicates(alias)
	if len(predicates) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(predicates, " AND "), args
}

func (s APIScopeFilters) eventSQLAndClause(alias, cwdExpr string) (string, []any) {
	predicates, args := s.eventSQLPredicates(alias, cwdExpr)
	if len(predicates) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(predicates, " AND "), args
}

func (s APIScopeFilters) eventAndSessionProjectSQLAndClause(eventAlias, cwdExpr, sessionAlias string) (string, []any) {
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

func (s APIScopeFilters) sqlPredicates(alias string) ([]string, []any) {
	if s.denyAll {
		return []string{"0 = 1"}, nil
	}
	prefix := ""
	if strings.TrimSpace(alias) != "" {
		prefix = alias + "."
	}
	var predicates []string
	var args []any
	appendScopePredicate(&predicates, &args, prefix+"source_name", s.SourceNames)
	appendScopePredicate(&predicates, &args, prefix+"runtime", s.Runtimes)
	appendScopePredicate(&predicates, &args, prefix+"project_key", s.ProjectKeys)
	return predicates, args
}

func (s APIScopeFilters) eventSQLPredicates(alias, cwdExpr string) ([]string, []any) {
	if s.denyAll {
		return []string{"0 = 1"}, nil
	}
	prefix := ""
	if strings.TrimSpace(alias) != "" {
		prefix = alias + "."
	}
	var predicates []string
	var args []any
	appendScopePredicate(&predicates, &args, prefix+"source_name", s.SourceNames)
	appendScopePredicate(&predicates, &args, prefix+"runtime", s.Runtimes)
	projectExpr := strings.TrimSpace(cwdExpr)
	if projectExpr == "" {
		projectExpr = prefix + "cwd"
	}
	appendScopePredicate(&predicates, &args, projectKeyExpr(projectExpr), s.ProjectKeys)
	return predicates, args
}

const apiScopeImpossibleValue = "\x00beacon-no-scope-match"

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
