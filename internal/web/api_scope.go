package web

import (
	"net/url"
	"strings"

	"github.com/johnnygreco/beacon/internal/search"
)

type APIScopeFilters struct {
	NodeIDs      []string `json:"node_ids,omitempty"`
	CollectorIDs []string `json:"collector_ids,omitempty"`
	SourceIDs    []string `json:"source_ids,omitempty"`
	SourceNames  []string `json:"source_names,omitempty"`
	Runtimes     []string `json:"runtimes,omitempty"`
	ProjectKeys  []string `json:"project_keys,omitempty"`
}

type APIScopeMetadata struct {
	AuthScopeApplied bool            `json:"auth_scope_applied"`
	Filters          APIScopeFilters `json:"filters"`
}

var apiScopeParamNames = []string{
	"node_id",
	"node_ids",
	"collector_id",
	"collector_ids",
	"source_id",
	"source_ids",
	"source_name",
	"source_names",
	"runtime",
	"runtimes",
	"project_key",
	"project_keys",
}

func parseAPIScopeFilters(values url.Values) APIScopeFilters {
	return APIScopeFilters{
		NodeIDs:      parseAPIScopeValues(values, "node_id", "node_ids"),
		CollectorIDs: parseAPIScopeValues(values, "collector_id", "collector_ids"),
		SourceIDs:    parseAPIScopeValues(values, "source_id", "source_ids"),
		SourceNames:  parseAPIScopeValues(values, "source_name", "source_names"),
		Runtimes:     parseAPIScopeValues(values, "runtime", "runtimes"),
		ProjectKeys:  parseAPIScopeValues(values, "project_key", "project_keys"),
	}
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

func (s APIScopeFilters) withoutProjectKeys() APIScopeFilters {
	s.ProjectKeys = nil
	return s
}

func (s APIScopeFilters) withoutProjectKeysAndRuntimes() APIScopeFilters {
	s.ProjectKeys = nil
	s.Runtimes = nil
	return s
}

func (s APIScopeFilters) applyToSearchQuery(q *search.SearchQuery) {
	if q == nil {
		return
	}
	q.NodeIDs = compactScopeValues(append(q.NodeIDs, s.NodeIDs...))
	q.CollectorIDs = compactScopeValues(append(q.CollectorIDs, s.CollectorIDs...))
	q.SourceIDs = compactScopeValues(append(q.SourceIDs, s.SourceIDs...))
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

func (s APIScopeFilters) eventSQLPredicates(alias, cwdExpr string) ([]string, []any) {
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
