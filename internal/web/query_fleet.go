package web

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/johnnygreco/beacon/internal/views"
)

const (
	fleetCollectorStaleAfter   = 2 * time.Minute
	fleetCollectorOfflineAfter = 10 * time.Minute
)

type fleetSessionAggregate struct {
	NodeID            string
	Collectors        []string
	Sources           []string
	Runtimes          []string
	Projects          []string
	ActiveSessions    int64
	AttentionSessions int64
	TotalSessions     int64
	TotalTokens       int64
	ErrorCount        int64
	LastEventAt       time.Time
}

type fleetHeartbeatAggregate struct {
	NodeID          string
	CollectorID     string
	SourceID        string
	SourceName      string
	Status          string
	QueueDepth      int64
	SpoolBytes      int64
	ActiveFiles     int64
	ErrorCount      int64
	LastEventAt     *time.Time
	LastHeartbeatAt time.Time
}

type fleetNodeBuilder struct {
	node APIDashboardFleetNode

	collectors map[string]struct{}
	sources    map[string]struct{}
	runtimes   map[string]struct{}
	projects   map[string]struct{}

	onlineCollectors  map[string]struct{}
	staleCollectors   map[string]struct{}
	offlineCollectors map[string]struct{}
}

func QueryDashboardFleet(ctx context.Context, db *sql.DB, scope APIScopeFilters, snapshot *controlplane.Snapshot) APIDashboardFleetResponse {
	now := time.Now()
	builders := map[string]*fleetNodeBuilder{}
	seedFleetEnrollment(builders, scope, snapshot)
	for _, row := range queryFleetSessionAggregates(ctx, db, scope, now) {
		builder := fleetBuilderFor(builders, row.NodeID)
		builder.node.ActiveSessions += row.ActiveSessions
		builder.node.AttentionSessions += row.AttentionSessions
		builder.node.TotalSessions += row.TotalSessions
		builder.node.TotalTokens += row.TotalTokens
		builder.node.ErrorCount += row.ErrorCount
		if row.LastEventAt.After(time.Time{}) {
			setMaxTimePtr(&builder.node.LastEventAt, row.LastEventAt)
		}
		addMany(builder.collectors, row.Collectors)
		addMany(builder.sources, row.Sources)
		addMany(builder.runtimes, row.Runtimes)
		addMany(builder.projects, row.Projects)
	}
	collectorMetrics := map[string]fleetHeartbeatAggregate{}
	heartbeatScope := fleetHeartbeatQueryScope(scope, snapshot)
	enrollmentSourcesByCollector := fleetEnrollmentSourcesByCollector(snapshot)
	for _, row := range queryFleetHeartbeatAggregates(ctx, db, heartbeatScope) {
		if !fleetHeartbeatMatchesEnrollmentScope(row, scope, snapshot, enrollmentSourcesByCollector) {
			continue
		}
		builder := fleetBuilderFor(builders, row.NodeID)
		addNonEmpty(builder.collectors, row.CollectorID)
		addNonEmpty(builder.sources, firstNonEmpty(row.SourceName, row.SourceID))
		builder.node.HeartbeatErrorCount += row.ErrorCount
		if row.LastEventAt != nil {
			setMaxTimePtr(&builder.node.LastEventAt, *row.LastEventAt)
		}
		if !row.LastHeartbeatAt.IsZero() {
			setMaxTimePtr(&builder.node.LastHeartbeatAt, row.LastHeartbeatAt)
		}
		status := fleetHeartbeatStatus(row, now)
		upsertFleetSourceDetail(&builder.node, APIDashboardFleetSource{
			CollectorID:     row.CollectorID,
			SourceID:        row.SourceID,
			SourceName:      row.SourceName,
			Status:          status,
			QueueDepth:      row.QueueDepth,
			SpoolBytes:      row.SpoolBytes,
			ActiveFiles:     row.ActiveFiles,
			ErrorCount:      row.ErrorCount,
			LastEventAt:     row.LastEventAt,
			LastHeartbeatAt: nonZeroTimePtr(row.LastHeartbeatAt),
		})
		switch status {
		case "online":
			addNonEmpty(builder.onlineCollectors, row.CollectorID)
		case "stale":
			addNonEmpty(builder.staleCollectors, row.CollectorID)
		default:
			addNonEmpty(builder.offlineCollectors, row.CollectorID)
		}
		key := fleetCollectorKey(row.NodeID, row.CollectorID)
		if key != "" {
			if current, ok := collectorMetrics[key]; !ok || row.LastHeartbeatAt.After(current.LastHeartbeatAt) {
				collectorMetrics[key] = row
			}
		}
	}
	for _, row := range collectorMetrics {
		builder := fleetBuilderFor(builders, row.NodeID)
		builder.node.QueueDepth += row.QueueDepth
		builder.node.SpoolBytes += row.SpoolBytes
		builder.node.ActiveFiles += row.ActiveFiles
	}

	nodes := make([]APIDashboardFleetNode, 0, len(builders))
	totals := APIDashboardFleetTotals{}
	allCollectors := map[string]struct{}{}
	healthCollectors := map[string]string{}
	for _, builder := range builders {
		node := builder.node
		node.Collectors = sortedMapValues(builder.collectors)
		node.Sources = sortedMapValues(builder.sources)
		node.Runtimes = sortedMapValues(builder.runtimes)
		node.Projects = sortedMapValues(builder.projects)
		node.CollectorCount = len(node.Collectors)
		if node.CollectorCount == 0 && node.NodeID != "" {
			node.CollectorCount = 1
		}
		node.MissingHeartbeats = fleetMissingHeartbeatCollectorCount(builder, node.Collectors)
		node.Status = fleetNodeStatus(builder)
		if node.MissingHeartbeats > 0 && node.LastHeartbeatAt == nil {
			node.HeartbeatStatus = "missing"
		} else {
			node.HeartbeatStatus = node.Status
		}
		if node.LastHeartbeatAt != nil {
			node.LastSeenLabel = views.RelativeTime(*node.LastHeartbeatAt)
		} else if node.LastEventAt != nil {
			node.LastSeenLabel = views.RelativeTime(*node.LastEventAt)
		}
		sort.Slice(node.SourcesDetail, func(i, j int) bool {
			a, b := node.SourcesDetail[i], node.SourcesDetail[j]
			if a.CollectorID != b.CollectorID {
				return a.CollectorID < b.CollectorID
			}
			return firstNonEmpty(a.SourceName, a.SourceID) < firstNonEmpty(b.SourceName, b.SourceID)
		})
		nodes = append(nodes, node)

		totals.ActiveSessions += node.ActiveSessions
		totals.AttentionSessions += node.AttentionSessions
		totals.TotalSessions += node.TotalSessions
		totals.TotalTokens += node.TotalTokens
		totals.QueueDepth += node.QueueDepth
		totals.SpoolBytes += node.SpoolBytes
		totals.HeartbeatErrorCount += node.HeartbeatErrorCount
		for _, collector := range node.Collectors {
			if collector != "" {
				allCollectors[collector] = struct{}{}
				status := fleetCollectorStatus(builder, collector)
				if status != "missing" {
					healthCollectors[collector] = mergeFleetCollectorStatus(healthCollectors[collector], status)
				}
			}
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if a.ActiveSessions != b.ActiveSessions {
			return a.ActiveSessions > b.ActiveSessions
		}
		if a.AttentionSessions != b.AttentionSessions {
			return a.AttentionSessions > b.AttentionSessions
		}
		if a.LastHeartbeatAt != nil && b.LastHeartbeatAt != nil && !a.LastHeartbeatAt.Equal(*b.LastHeartbeatAt) {
			return a.LastHeartbeatAt.After(*b.LastHeartbeatAt)
		}
		return a.Label < b.Label
	})
	for _, status := range healthCollectors {
		switch status {
		case "online":
			totals.OnlineCollectors++
		case "stale":
			totals.StaleCollectors++
		default:
			totals.OfflineCollectors++
		}
	}
	totals.NodeCount = len(nodes)
	totals.CollectorCount = len(allCollectors)
	totals.MissingHeartbeats = len(allCollectors) - len(healthCollectors)
	return APIDashboardFleetResponse{Scope: scope.metadata(), Totals: totals, Nodes: nodes}
}

func seedFleetEnrollment(builders map[string]*fleetNodeBuilder, scope APIScopeFilters, snapshot *controlplane.Snapshot) {
	if snapshot == nil || len(compactScopeValues(scope.ProjectKeys)) > 0 {
		return
	}
	nodesByID := fleetEnrollmentNodesByID(snapshot)
	sourcesByCollector := fleetEnrollmentSourcesByCollector(snapshot)
	for _, collector := range snapshot.Collectors {
		collectorID := strings.TrimSpace(collector.ID)
		if collectorID == "" {
			continue
		}
		nodeID := normalizeFleetNodeID(collector.NodeID)
		if !scopeMatchesValue(nodeID, scope.NodeIDs) || !scopeMatchesValue(collectorID, scope.CollectorIDs) {
			continue
		}
		sources, ok := fleetEnrollmentMatchingSources(sourcesByCollector[collectorID], scope)
		if !ok {
			continue
		}
		builder := fleetBuilderFor(builders, nodeID)
		if node, ok := nodesByID[nodeID]; ok {
			applyFleetNodeMetadata(builder, node)
		}
		addNonEmpty(builder.collectors, collectorID)
		for _, source := range sources {
			addNonEmpty(builder.sources, firstNonEmpty(source.Name, source.ID))
			addNonEmpty(builder.runtimes, source.Runtime)
			upsertFleetSourceDetail(&builder.node, APIDashboardFleetSource{
				CollectorID: strings.TrimSpace(source.CollectorID),
				SourceID:    strings.TrimSpace(source.ID),
				SourceName:  strings.TrimSpace(source.Name),
				Status:      "missing",
			})
		}
	}
}

func fleetEnrollmentNodesByID(snapshot *controlplane.Snapshot) map[string]controlplane.Node {
	nodes := map[string]controlplane.Node{}
	if snapshot == nil {
		return nodes
	}
	for _, node := range snapshot.Nodes {
		nodeID := normalizeFleetNodeID(node.ID)
		if nodeID == "" {
			continue
		}
		nodes[nodeID] = node
	}
	return nodes
}

func fleetEnrollmentSourcesByCollector(snapshot *controlplane.Snapshot) map[string][]controlplane.Source {
	sources := map[string][]controlplane.Source{}
	if snapshot == nil {
		return sources
	}
	for _, source := range snapshot.Sources {
		collectorID := strings.TrimSpace(source.CollectorID)
		if collectorID == "" {
			continue
		}
		sources[collectorID] = append(sources[collectorID], source)
	}
	return sources
}

func fleetEnrollmentMatchingSources(sources []controlplane.Source, scope APIScopeFilters) ([]controlplane.Source, bool) {
	if len(sources) == 0 {
		return nil, !fleetScopeHasSourceMetadataFilters(scope)
	}
	if !fleetScopeHasSourceMetadataFilters(scope) {
		return sources, true
	}
	var matched []controlplane.Source
	for _, source := range sources {
		if !scopeMatchesValue(source.ID, scope.SourceIDs) {
			continue
		}
		if !scopeMatchesValue(source.Name, scope.SourceNames) {
			continue
		}
		if !scopeMatchesValue(source.Runtime, scope.Runtimes) {
			continue
		}
		matched = append(matched, source)
	}
	return matched, len(matched) > 0
}

func fleetHeartbeatQueryScope(scope APIScopeFilters, snapshot *controlplane.Snapshot) APIScopeFilters {
	if snapshot == nil || len(compactScopeValues(scope.ProjectKeys)) > 0 || len(compactScopeValues(scope.Runtimes)) == 0 {
		return scope
	}
	scoped := scope
	scoped.Runtimes = nil
	return scoped
}

func fleetHeartbeatMatchesEnrollmentScope(row fleetHeartbeatAggregate, scope APIScopeFilters, snapshot *controlplane.Snapshot, sourcesByCollector map[string][]controlplane.Source) bool {
	if snapshot == nil || len(compactScopeValues(scope.ProjectKeys)) > 0 {
		return true
	}
	if !scopeMatchesValue(normalizeFleetNodeID(row.NodeID), scope.NodeIDs) {
		return false
	}
	if !scopeMatchesValue(row.CollectorID, scope.CollectorIDs) {
		return false
	}
	source, ok := fleetHeartbeatEnrollmentSource(row, sourcesByCollector[strings.TrimSpace(row.CollectorID)])
	if !ok {
		return false
	}
	if !fleetScopeHasSourceMetadataFilters(scope) {
		return true
	}
	if !scopeMatchesValue(source.ID, scope.SourceIDs) {
		return false
	}
	if !scopeMatchesValue(source.Name, scope.SourceNames) {
		return false
	}
	return scopeMatchesValue(source.Runtime, scope.Runtimes)
}

func fleetHeartbeatEnrollmentSource(row fleetHeartbeatAggregate, sources []controlplane.Source) (controlplane.Source, bool) {
	for _, source := range sources {
		if fleetEnrollmentSourceMatchesHeartbeat(row, source) {
			return source, true
		}
	}
	return controlplane.Source{}, false
}

func fleetEnrollmentSourceMatchesHeartbeat(row fleetHeartbeatAggregate, source controlplane.Source) bool {
	rowSourceID := strings.TrimSpace(row.SourceID)
	rowSourceName := strings.TrimSpace(row.SourceName)
	sourceID := strings.TrimSpace(source.ID)
	sourceName := strings.TrimSpace(source.Name)
	if rowSourceID != "" && sourceID != "" {
		return rowSourceID == sourceID
	}
	if rowSourceName != "" && sourceName != "" {
		return rowSourceName == sourceName
	}
	return rowSourceID == "" && rowSourceName == ""
}

func fleetScopeHasSourceMetadataFilters(scope APIScopeFilters) bool {
	return len(compactScopeValues(scope.SourceIDs)) > 0 ||
		len(compactScopeValues(scope.SourceNames)) > 0 ||
		len(compactScopeValues(scope.Runtimes)) > 0
}

func applyFleetNodeMetadata(builder *fleetNodeBuilder, node controlplane.Node) {
	if builder == nil {
		return
	}
	if label := firstNonEmpty(node.DisplayName, node.Hostname, node.ID); label != "" {
		builder.node.Label = label
	}
}

func queryFleetSessionAggregates(ctx context.Context, db *sql.DB, scope APIScopeFilters, now time.Time) []fleetSessionAggregate {
	if db == nil {
		return nil
	}
	activeCutoff := now.Add(-idleThreshold)
	sessionSource, sourceArgs := sessionProjectionSubqueryForScope("", scope)
	scopeClause, scopeArgs := scope.withoutProjectKeys().sqlAndClause("")
	args := activeSessionPredicateArgs(scope, activeCutoff)
	args = append(args, sourceArgs...)
	args = append(args, scopeArgs...)
	query := `SELECT COALESCE(NULLIF(node_id, ''), 'local') AS node_key,
		        groupUniqArrayIf(COALESCE(collector_id, ''), collector_id != ''),
		        groupUniqArrayIf(COALESCE(source_name, ''), source_name != ''),
		        groupUniqArrayIf(COALESCE(runtime, ''), runtime != ''),
		        groupUniqArrayIf(COALESCE(project_key, ''), project_key != ''),
		        countIf(` + activeSessionPredicateScoped(scope) + `),
		        countIf(COALESCE(attention_score, 0) > 0 OR COALESCE(error_count, 0) > 0),
		        count(),
		        COALESCE(SUM(total_tokens), 0),
		        COALESCE(SUM(error_count), 0),
		        max(ended_at)
		 FROM ` + sessionSource + `
		 WHERE 1 = 1` + scopeClause + `
		 GROUP BY node_key`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		logQueryError("dashboard fleet sessions", err)
		return nil
	}
	defer rows.Close()

	var out []fleetSessionAggregate
	for rows.Next() {
		var row fleetSessionAggregate
		if err := rows.Scan(&row.NodeID, &row.Collectors, &row.Sources, &row.Runtimes, &row.Projects,
			&row.ActiveSessions, &row.AttentionSessions, &row.TotalSessions, &row.TotalTokens,
			&row.ErrorCount, &row.LastEventAt); err != nil {
			logQueryScanError("dashboard fleet sessions", err)
			continue
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		logQueryError("dashboard fleet sessions rows", err)
		return nil
	}
	return out
}

func queryFleetHeartbeatAggregates(ctx context.Context, db *sql.DB, scope APIScopeFilters) []fleetHeartbeatAggregate {
	if db == nil {
		return nil
	}
	scopeClause, scopeArgs := fleetHeartbeatScopeClause(scope)
	rows, err := db.QueryContext(ctx,
		`SELECT COALESCE(NULLIF(h.node_id, ''), 'local') AS node_key,
		        COALESCE(h.collector_id, ''),
		        COALESCE(h.source_id, ''),
		        COALESCE(h.source_name, ''),
		        COALESCE(h.status, ''),
		        COALESCE(h.queue_depth, 0),
		        COALESCE(h.spool_bytes, 0),
		        COALESCE(h.active_files, 0),
		        COALESCE(h.error_count, 0),
		        h.last_event_at,
		        h.created_at
		 FROM capture_heartbeats AS h
		 INNER JOIN (
			SELECT collector_id,
			       source_id,
			       max(created_at) AS created_at
			FROM capture_heartbeats
			GROUP BY collector_id, source_id
		 ) AS latest ON latest.collector_id = h.collector_id
		           AND latest.source_id = h.source_id
		           AND latest.created_at = h.created_at
		 WHERE 1 = 1`+scopeClause+`
		 ORDER BY h.node_id, h.collector_id, h.source_name`, scopeArgs...)
	if err != nil {
		logQueryError("dashboard fleet heartbeats", err)
		return nil
	}
	defer rows.Close()

	var out []fleetHeartbeatAggregate
	for rows.Next() {
		var row fleetHeartbeatAggregate
		var lastEvent sql.NullTime
		if err := rows.Scan(&row.NodeID, &row.CollectorID, &row.SourceID, &row.SourceName, &row.Status,
			&row.QueueDepth, &row.SpoolBytes, &row.ActiveFiles, &row.ErrorCount, &lastEvent,
			&row.LastHeartbeatAt); err != nil {
			logQueryScanError("dashboard fleet heartbeats", err)
			continue
		}
		if lastEvent.Valid {
			row.LastEventAt = &lastEvent.Time
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		logQueryError("dashboard fleet heartbeats rows", err)
		return nil
	}
	return out
}

func fleetHeartbeatScopeClause(scope APIScopeFilters) (string, []any) {
	scopeClause, args := scope.withoutProjectKeysAndRuntimes().sqlAndClause("h")
	if len(compactScopeValues(scope.Runtimes)) == 0 && len(compactScopeValues(scope.ProjectKeys)) == 0 {
		return scopeClause, args
	}

	sessionSource, sourceArgs := sessionProjectionSubqueryForScope("", scope)
	sessionScope := scope.withoutProjectKeys()
	sessionClause, sessionArgs := sessionScope.sqlAndClause("")
	args = append(args, sourceArgs...)
	args = append(args, sessionArgs...)
	return scopeClause + ` AND (h.collector_id, h.source_id) IN (
			SELECT collector_id, source_id
			FROM ` + sessionSource + `
			WHERE collector_id != '' AND source_id != ''` + sessionClause + `
			GROUP BY collector_id, source_id
		)`, args
}

func fleetCollectorKey(nodeID, collectorID string) string {
	collectorID = strings.TrimSpace(collectorID)
	if collectorID == "" {
		return ""
	}
	return normalizeFleetNodeID(nodeID) + "\x00" + collectorID
}

func fleetBuilderFor(builders map[string]*fleetNodeBuilder, nodeID string) *fleetNodeBuilder {
	nodeID = normalizeFleetNodeID(nodeID)
	if builder := builders[nodeID]; builder != nil {
		return builder
	}
	builder := &fleetNodeBuilder{
		node: APIDashboardFleetNode{
			NodeID: nodeID,
			Label:  fleetNodeLabel(nodeID),
			Status: "offline",
		},
		collectors:        map[string]struct{}{},
		sources:           map[string]struct{}{},
		runtimes:          map[string]struct{}{},
		projects:          map[string]struct{}{},
		onlineCollectors:  map[string]struct{}{},
		staleCollectors:   map[string]struct{}{},
		offlineCollectors: map[string]struct{}{},
	}
	builders[nodeID] = builder
	return builder
}

func upsertFleetSourceDetail(node *APIDashboardFleetNode, detail APIDashboardFleetSource) {
	if node == nil {
		return
	}
	key := fleetSourceDetailKey(detail)
	if key == "" {
		return
	}
	for idx, existing := range node.SourcesDetail {
		if fleetSourceDetailKey(existing) == key {
			node.SourcesDetail[idx] = detail
			return
		}
	}
	node.SourcesDetail = append(node.SourcesDetail, detail)
}

func fleetSourceDetailKey(detail APIDashboardFleetSource) string {
	collectorID := strings.TrimSpace(detail.CollectorID)
	sourceID := strings.TrimSpace(firstNonEmpty(detail.SourceID, detail.SourceName))
	if collectorID == "" || sourceID == "" {
		return ""
	}
	return collectorID + "\x00" + sourceID
}

func normalizeFleetNodeID(nodeID string) string {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return "local"
	}
	return nodeID
}

func fleetNodeLabel(nodeID string) string {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" || nodeID == "local" {
		return "Local"
	}
	return nodeID
}

func fleetHeartbeatStatus(row fleetHeartbeatAggregate, now time.Time) string {
	if row.LastHeartbeatAt.IsZero() {
		return "offline"
	}
	age := now.Sub(row.LastHeartbeatAt)
	if age > fleetCollectorOfflineAfter {
		return "offline"
	}
	status := strings.ToLower(strings.TrimSpace(row.Status))
	if status != "" && status != "healthy" && status != "ok" && status != "online" {
		return "stale"
	}
	if row.ErrorCount > 0 || age > fleetCollectorStaleAfter {
		return "stale"
	}
	return "online"
}

func fleetNodeStatus(builder *fleetNodeBuilder) string {
	if builder == nil {
		return "offline"
	}
	hasOnline := false
	hasOffline := false
	hasMissing := false
	for collector := range builder.collectors {
		switch fleetCollectorStatus(builder, collector) {
		case "stale":
			return "stale"
		case "online":
			hasOnline = true
		case "offline":
			hasOffline = true
		case "missing":
			hasMissing = true
		}
	}
	if hasOnline && (hasOffline || hasMissing) {
		return "stale"
	}
	if hasOnline {
		return "online"
	}
	if hasOffline || builder.node.LastHeartbeatAt != nil {
		return "offline"
	}
	if builder.node.ActiveSessions > 0 {
		return "active"
	}
	return "offline"
}

func fleetCollectorStatus(builder *fleetNodeBuilder, collector string) string {
	if builder == nil {
		return "offline"
	}
	if _, ok := builder.staleCollectors[collector]; ok {
		return "stale"
	}
	_, online := builder.onlineCollectors[collector]
	_, offline := builder.offlineCollectors[collector]
	if online && offline {
		return "stale"
	}
	if online {
		return "online"
	}
	if offline {
		return "offline"
	}
	return "missing"
}

func fleetMissingHeartbeatCollectorCount(builder *fleetNodeBuilder, collectors []string) int {
	var missing int
	for _, collector := range collectors {
		if collector != "" && fleetCollectorStatus(builder, collector) == "missing" {
			missing++
		}
	}
	return missing
}

func mergeFleetCollectorStatus(current, next string) string {
	if current == "stale" || next == "stale" {
		return "stale"
	}
	if (current == "online" && next == "offline") || (current == "offline" && next == "online") {
		return "stale"
	}
	if current == "online" || next == "online" {
		return "online"
	}
	return "offline"
}

func addMany(target map[string]struct{}, values []string) {
	for _, value := range values {
		addNonEmpty(target, value)
	}
}

func addNonEmpty(target map[string]struct{}, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	target[value] = struct{}{}
}

func scopeMatchesValue(value string, selected []string) bool {
	selected = compactScopeValues(selected)
	if len(selected) == 0 {
		return true
	}
	value = strings.TrimSpace(value)
	for _, candidate := range selected {
		if value == candidate {
			return true
		}
	}
	return false
}

func sortedMapValues(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func setMaxTimePtr(target **time.Time, candidate time.Time) {
	if candidate.IsZero() {
		return
	}
	if *target == nil || candidate.After(**target) {
		value := candidate
		*target = &value
	}
}

func nonZeroTimePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
