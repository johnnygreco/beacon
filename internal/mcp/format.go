package mcp

import (
	"encoding/json"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/usage"
)

type openRef struct {
	Type      string        `json:"type"`
	EventID   string        `json:"event_id,omitempty"`
	MessageID string        `json:"message_id,omitempty"`
	SessionID string        `json:"session_id,omitempty"`
	Anchor    string        `json:"anchor,omitempty"`
	Scope     *ScopeFilters `json:"scope,omitempty"`
}

func eventOpenRef(eventUID, sessionID string) openRef {
	return openRef{Type: "event", EventID: beaconEventID(eventUID), SessionID: beaconSessionID(sessionID)}
}

func messageOpenRef(eventUID, sessionID string) openRef {
	return openRef{Type: "message", MessageID: beaconMessageID(eventUID), SessionID: beaconSessionID(sessionID)}
}

func sessionLatestOpenRef(sessionID string) openRef {
	return openRef{Type: "session_latest", SessionID: beaconSessionID(sessionID), Anchor: "latest"}
}

func scopedEventOpenRef(eventUID, sessionID string, scope ScopeMetadata) openRef {
	ref := eventOpenRef(eventUID, sessionID)
	ref.Scope = openRefScope(scope.Filters)
	return ref
}

func scopedMessageOpenRef(eventUID, sessionID string, scope ScopeMetadata) openRef {
	ref := messageOpenRef(eventUID, sessionID)
	ref.Scope = openRefScope(scope.Filters)
	return ref
}

func scopedSessionLatestOpenRef(sessionID string, scope ScopeMetadata) openRef {
	ref := sessionLatestOpenRef(sessionID)
	ref.Scope = openRefScope(scope.Filters)
	return ref
}

func openRefScope(scope ScopeFilters) *ScopeFilters {
	scope = normalizeScopeFilters(scope)
	if !hasScopeFilters(scope) {
		return nil
	}
	return &scope
}

func beaconEventID(eventUID string) string {
	if eventUID == "" {
		return ""
	}
	return "event:" + eventUID
}

func beaconSessionID(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	return "session:" + sessionID
}

func beaconMessageID(eventUID string) string {
	if eventUID == "" {
		return ""
	}
	return "message:" + eventUID
}

func FormatSearchResults(results []search.SearchResult, metadataOpt ...ScopeMetadata) string {
	type result struct {
		EventID     string    `json:"event_id"`
		SessionID   string    `json:"session_id"`
		SourceName  string    `json:"source_name,omitempty"`
		Runtime     string    `json:"runtime,omitempty"`
		ProjectKey  string    `json:"project_key,omitempty"`
		ProjectPath string    `json:"project_path,omitempty"`
		EventKind   string    `json:"event_kind"`
		TextPreview string    `json:"text_preview"`
		Score       float64   `json:"score"`
		Timestamp   time.Time `json:"timestamp"`
		ToolName    string    `json:"tool_name,omitempty"`
		Model       string    `json:"model,omitempty"`
		Provider    string    `json:"provider,omitempty"`
		OpenRef     openRef   `json:"open_ref"`
	}
	scope := ScopeMetadata{Filters: ScopeFilters{}}
	if len(metadataOpt) > 0 {
		scope = metadataOpt[0]
	}

	payload := struct {
		Schema      string         `json:"schema"`
		Tool        string         `json:"tool"`
		Scope       ScopeMetadata  `json:"scope"`
		Results     []result       `json:"results"`
		Warnings    []string       `json:"warnings"`
		Performance map[string]any `json:"performance"`
	}{
		Schema:      "beacon.mcp.search_sessions.v1",
		Tool:        "search_sessions",
		Scope:       scope,
		Results:     []result{},
		Warnings:    []string{},
		Performance: map[string]any{"result_count": len(results)},
	}

	for _, r := range results {
		payload.Results = append(payload.Results, result{
			EventID:     beaconEventID(r.EventUID),
			SessionID:   beaconSessionID(r.SessionID),
			SourceName:  r.SourceName,
			Runtime:     r.Runtime,
			ProjectKey:  r.ProjectKey,
			ProjectPath: r.ProjectPath,
			EventKind:   r.EventKind,
			TextPreview: r.TextPreview,
			Score:       r.Score,
			Timestamp:   r.Timestamp,
			ToolName:    r.ToolName,
			Model:       r.Model,
			Provider:    r.Provider,
			OpenRef:     scopedEventOpenRef(r.EventUID, r.SessionID, scope),
		})
	}
	return mustJSON(payload)
}

type contextEvent struct {
	EventUID    string
	SessionID   string
	EventKind   string
	ActorRole   string
	TextPreview string
	ToolName    string
	Model       string
	Tokens      int64
	Timestamp   time.Time
	SourceName  string
	Runtime     string
	ProjectKey  string
	ProjectPath string
}

func FormatOpenContext(events []contextEvent, targetIdx int, metadataOpt ...ScopeMetadata) string {
	type event struct {
		EventID     string    `json:"event_id"`
		SessionID   string    `json:"session_id,omitempty"`
		SourceName  string    `json:"source_name,omitempty"`
		Runtime     string    `json:"runtime,omitempty"`
		ProjectKey  string    `json:"project_key,omitempty"`
		ProjectPath string    `json:"project_path,omitempty"`
		EventKind   string    `json:"event_kind"`
		ActorRole   string    `json:"actor_role"`
		TextPreview string    `json:"text_preview"`
		ToolName    string    `json:"tool_name,omitempty"`
		Model       string    `json:"model,omitempty"`
		Tokens      int64     `json:"tokens"`
		Timestamp   time.Time `json:"timestamp"`
		Target      bool      `json:"target"`
		OpenRef     openRef   `json:"open_ref"`
	}
	scope := ScopeMetadata{Filters: ScopeFilters{}}
	if len(metadataOpt) > 0 {
		scope = metadataOpt[0]
	}
	payload := struct {
		Schema   string        `json:"schema"`
		Tool     string        `json:"tool"`
		Scope    ScopeMetadata `json:"scope"`
		Events   []event       `json:"events"`
		Warnings []string      `json:"warnings"`
	}{
		Schema:   "beacon.mcp.open.v1",
		Tool:     "open",
		Scope:    scope,
		Events:   []event{},
		Warnings: []string{},
	}
	for i, e := range events {
		payload.Events = append(payload.Events, event{
			EventID:     beaconEventID(e.EventUID),
			SessionID:   beaconSessionID(e.SessionID),
			SourceName:  e.SourceName,
			Runtime:     e.Runtime,
			ProjectKey:  e.ProjectKey,
			ProjectPath: e.ProjectPath,
			EventKind:   e.EventKind,
			ActorRole:   e.ActorRole,
			TextPreview: e.TextPreview,
			ToolName:    e.ToolName,
			Model:       e.Model,
			Tokens:      e.Tokens,
			Timestamp:   e.Timestamp,
			Target:      i == targetIdx,
			OpenRef:     scopedEventOpenRef(e.EventUID, e.SessionID, scope),
		})
	}
	return mustJSON(payload)
}

type sessionInfo struct {
	SessionID     string
	SourceName    string
	Runtime       string
	ProjectKey    string
	ProjectPath   string
	Provider      string
	StartedAt     time.Time
	EndedAt       time.Time
	EventCount    int64
	TurnCount     int64
	TotalTokens   int64
	ToolCallCount int64
	MCPCallCount  int64
	ErrorCount    int64
	LastModel     string
	WorkingDir    string
}

type sessionListMetadata struct {
	ResultCount        int
	TotalMatchingCount int64
	Limit              int
	Cursor             string
	ResultComplete     bool
	NextCursor         string
}

func FormatSessionList(sessions []sessionInfo, options ...any) string {
	listMetadata := sessionListMetadata{
		ResultCount:        len(sessions),
		TotalMatchingCount: int64(len(sessions)),
		Limit:              len(sessions),
		ResultComplete:     true,
	}
	scope := ScopeMetadata{Filters: ScopeFilters{}}
	for _, option := range options {
		switch value := option.(type) {
		case sessionListMetadata:
			listMetadata = value
		case ScopeMetadata:
			scope = value
		}
	}
	type session struct {
		SessionID     string    `json:"session_id"`
		SourceName    string    `json:"source_name"`
		Runtime       string    `json:"runtime,omitempty"`
		ProjectKey    string    `json:"project_key,omitempty"`
		ProjectPath   string    `json:"project_path,omitempty"`
		Provider      string    `json:"provider,omitempty"`
		StartedAt     time.Time `json:"started_at"`
		EndedAt       time.Time `json:"ended_at"`
		EventCount    int64     `json:"event_count"`
		TurnCount     int64     `json:"turn_count"`
		TotalTokens   int64     `json:"total_tokens"`
		ToolCallCount int64     `json:"tool_call_count"`
		MCPCallCount  int64     `json:"mcp_call_count"`
		ErrorCount    int64     `json:"error_count"`
		LastModel     string    `json:"last_model,omitempty"`
		WorkingDir    string    `json:"working_dir,omitempty"`
		OpenRef       openRef   `json:"open_ref"`
	}
	type metadataPayload struct {
		ResultCount        int    `json:"result_count"`
		TotalMatchingCount int64  `json:"total_matching_count"`
		Limit              int    `json:"limit"`
		Cursor             string `json:"cursor,omitempty"`
		ResultComplete     bool   `json:"result_complete"`
		NextCursor         string `json:"next_cursor"`
	}
	payload := struct {
		Schema   string          `json:"schema"`
		Tool     string          `json:"tool"`
		Scope    ScopeMetadata   `json:"scope"`
		Results  []session       `json:"results"`
		Metadata metadataPayload `json:"metadata"`
		Warnings []string        `json:"warnings"`
	}{
		Schema:   "beacon.mcp.list_sessions.v1",
		Tool:     "list_sessions",
		Scope:    scope,
		Results:  []session{},
		Metadata: metadataPayload(listMetadata),
		Warnings: []string{},
	}
	for _, s := range sessions {
		payload.Results = append(payload.Results, session{
			SessionID:     beaconSessionID(s.SessionID),
			SourceName:    s.SourceName,
			Runtime:       s.Runtime,
			ProjectKey:    s.ProjectKey,
			ProjectPath:   s.ProjectPath,
			Provider:      s.Provider,
			StartedAt:     s.StartedAt,
			EndedAt:       s.EndedAt,
			EventCount:    s.EventCount,
			TurnCount:     s.TurnCount,
			TotalTokens:   s.TotalTokens,
			ToolCallCount: s.ToolCallCount,
			MCPCallCount:  s.MCPCallCount,
			ErrorCount:    s.ErrorCount,
			LastModel:     s.LastModel,
			WorkingDir:    s.WorkingDir,
			OpenRef:       scopedSessionLatestOpenRef(s.SessionID, scope),
		})
	}
	return mustJSON(payload)
}

func FormatUsageSummary(result usage.Result, scope ScopeMetadata) string {
	type usageGroup struct {
		Keys    map[string]string `json:"keys"`
		Totals  usage.Totals      `json:"totals"`
		OpenRef *openRef          `json:"open_ref,omitempty"`
	}
	groups := make([]usageGroup, 0, len(result.Groups))
	for _, group := range result.Groups {
		item := usageGroup{Keys: group.Keys, Totals: group.Totals}
		if sessionID := group.Keys["session_id"]; sessionID != "" {
			ref := scopedSessionLatestOpenRef(sessionID, scope)
			item.OpenRef = &ref
		}
		groups = append(groups, item)
	}
	payload := struct {
		Schema                  string         `json:"schema"`
		Tool                    string         `json:"tool"`
		Scope                   ScopeMetadata  `json:"scope"`
		Window                  usage.Window   `json:"window"`
		Filters                 usage.Filters  `json:"filters"`
		GroupBy                 []string       `json:"group_by"`
		TokenMode               string         `json:"token_mode"`
		TotalDefinition         string         `json:"total_definition"`
		SelectedTotalDefinition string         `json:"selected_total_definition"`
		Summary                 usage.Totals   `json:"summary"`
		Groups                  []usageGroup   `json:"groups"`
		Metadata                usage.Metadata `json:"metadata"`
	}{
		Schema:                  "beacon.mcp.usage_summary.v1",
		Tool:                    "usage_summary",
		Scope:                   scope,
		Window:                  result.Window,
		Filters:                 result.Filters,
		GroupBy:                 result.GroupBy,
		TokenMode:               result.TokenMode,
		TotalDefinition:         result.TotalDefinition,
		SelectedTotalDefinition: result.SelectedTotalDefinition,
		Summary:                 result.Summary,
		Groups:                  groups,
		Metadata:                result.Metadata,
	}
	return mustJSON(payload)
}

type AnnotationListMetadata struct {
	ResultCount    int  `json:"result_count"`
	Limit          int  `json:"limit"`
	Offset         int  `json:"offset"`
	ResultComplete bool `json:"result_complete"`
}

type formattedAnnotation struct {
	AnnotationID  string     `json:"annotation_id"`
	Revision      uint64     `json:"revision"`
	TargetType    string     `json:"target_type"`
	SessionID     string     `json:"session_id"`
	EventID       string     `json:"event_id,omitempty"`
	MessageID     string     `json:"message_id,omitempty"`
	OpenRef       *openRef   `json:"open_ref,omitempty"`
	AuthorType    string     `json:"author_type"`
	AuthorID      string     `json:"author_id,omitempty"`
	AuthorName    string     `json:"author_name,omitempty"`
	Source        string     `json:"source"`
	Category      string     `json:"category,omitempty"`
	Outcome       string     `json:"outcome,omitempty"`
	QualityScore  int        `json:"quality_score,omitempty"`
	Confidence    int        `json:"confidence,omitempty"`
	NeedsFollowup bool       `json:"needs_followup"`
	Labels        []string   `json:"labels"`
	Note          string     `json:"note"`
	MetadataJSON  string     `json:"metadata_json,omitempty"`
	Status        string     `json:"status"`
	SchemaVersion int        `json:"schema_version"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
}

func FormatAnnotationResult(tool string, annotation models.TraceAnnotation, scope ScopeMetadata) string {
	payload := struct {
		Schema     string              `json:"schema"`
		Tool       string              `json:"tool"`
		Scope      ScopeMetadata       `json:"scope"`
		Annotation formattedAnnotation `json:"annotation"`
		Warnings   []string            `json:"warnings"`
	}{
		Schema:     "beacon.mcp." + tool + ".v1",
		Tool:       tool,
		Scope:      scope,
		Annotation: formatAnnotation(annotation, scope),
		Warnings:   []string{},
	}
	return mustJSON(payload)
}

func FormatAnnotationList(annotations []models.TraceAnnotation, metadata AnnotationListMetadata, scope ScopeMetadata) string {
	payload := struct {
		Schema      string                 `json:"schema"`
		Tool        string                 `json:"tool"`
		Scope       ScopeMetadata          `json:"scope"`
		Annotations []formattedAnnotation  `json:"annotations"`
		Metadata    AnnotationListMetadata `json:"metadata"`
		Warnings    []string               `json:"warnings"`
	}{
		Schema:      "beacon.mcp.list_annotations.v1",
		Tool:        "list_annotations",
		Scope:       scope,
		Annotations: []formattedAnnotation{},
		Metadata:    metadata,
		Warnings:    []string{},
	}
	for _, annotation := range annotations {
		payload.Annotations = append(payload.Annotations, formatAnnotation(annotation, scope))
	}
	return mustJSON(payload)
}

func formatAnnotation(annotation models.TraceAnnotation, scope ScopeMetadata) formattedAnnotation {
	labels := append([]string(nil), annotation.Labels...)
	if labels == nil {
		labels = []string{}
	}
	item := formattedAnnotation{
		AnnotationID:  annotation.AnnotationID,
		Revision:      annotation.Revision,
		TargetType:    annotation.TargetType,
		SessionID:     beaconSessionID(annotation.SessionID),
		AuthorType:    annotation.AuthorType,
		AuthorID:      annotation.AuthorID,
		AuthorName:    annotation.AuthorName,
		Source:        annotation.Source,
		Category:      annotation.Category,
		Outcome:       annotation.Outcome,
		QualityScore:  annotation.QualityScore,
		Confidence:    annotation.Confidence,
		NeedsFollowup: annotation.NeedsFollowup,
		Labels:        labels,
		Note:          annotation.Note,
		MetadataJSON:  annotation.MetadataJSON,
		Status:        annotation.Status,
		SchemaVersion: annotation.SchemaVersion,
		CreatedAt:     annotation.CreatedAt,
		UpdatedAt:     annotation.UpdatedAt,
		DeletedAt:     annotation.DeletedAt,
	}
	if annotation.EventUID != "" {
		item.EventID = beaconEventID(annotation.EventUID)
		if annotation.TargetType == models.AnnotationTargetMessage {
			ref := scopedMessageOpenRef(annotation.EventUID, annotation.SessionID, scope)
			item.MessageID = beaconMessageID(annotation.EventUID)
			item.OpenRef = &ref
		} else {
			ref := scopedEventOpenRef(annotation.EventUID, annotation.SessionID, scope)
			item.OpenRef = &ref
		}
	} else {
		ref := scopedSessionLatestOpenRef(annotation.SessionID, scope)
		item.OpenRef = &ref
	}
	return item
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{"schema":"beacon.mcp.error.v1","error":"json marshal failed"}`
	}
	return string(data)
}
